package builder

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/kart-io/goagent/core/durable"
	"github.com/kart-io/goagent/interfaces"
	"github.com/kart-io/goagent/llm"
	toolspkg "github.com/kart-io/goagent/tools"
)

// 循环扩展点只作用于原生工具流（llm.ToolStreamClient 且配置了工具）。持久运行中，PreStep、TurnStopping、
// 提示词段落与上下文、收件箱的输入按步骤记入日志，恢复时回放记录而不重新计算；Request 钩子须是确定性的；
// RequestError 钩子只在非持久运行中生效，持久运行的模型失败仍交给宿主恢复。

// Step 是一次模型请求前的状态。
type Step struct {
	// Index 是本轮的第几次模型请求，从 0 开始。
	Index int
	// Messages 是将要发送的消息，PreStep 可以改写（如压缩）。改写后的消息成为之后各步的历史。
	Messages []llm.Message
	// Inputs 是 Messages 末尾本步新收到的用户输入条数：轮次输入、Steer、Inject 与 Redirect 的纠正，
	// 不含 TurnStopping 的续跑消息。
	Inputs int
	// Final 表示这是本轮最后一次模型请求：不再提供工具（ReturnDirectTools 除外），模型须直接作答。
	Final bool
}

// PreStepResult 是 PreStep 的结果。Halt 为真时不再请求模型，以 Answer 作为本轮最终答复结束。
type PreStepResult struct {
	Halt   bool   `json:"halt,omitempty"`
	Answer string `json:"answer,omitempty"`
}

// PreStepHook 在领取收件箱输入之后、组装请求之前执行；返回 error 中止运行。
type PreStepHook func(ctx context.Context, step *Step) (PreStepResult, error)

// preStep 是注册的 PreStep 钩子与其身份。
type preStep struct {
	identity string
	hook     PreStepHook
}

// ModelRequest 是即将发送的模型请求。Request 钩子可替换 Client（路由、换模型）或修改 Request 的参数；
// Tools 只读。
type ModelRequest struct {
	Step    int
	Client  llm.ToolStreamClient
	Request *llm.CompletionRequest
	Tools   []interfaces.Tool
}

// RequestHook 在发送模型请求前执行；返回 error 中止运行。
type RequestHook func(ctx context.Context, req *ModelRequest) error

// RequestErrorAction 是模型请求失败后的处置。零值为 RequestFail。
type RequestErrorAction int

const (
	// RequestFail 交给下一个钩子；全部钩子都放弃时运行以原错误失败。
	RequestFail RequestErrorAction = iota
	// RequestRetry 按 RequestErrorDecision 调整后重试本步。
	RequestRetry
)

// RequestErrorDecision 是 RequestError 钩子的决定。Messages 非 nil 时重试前替换消息（压缩），
// Client 非 nil 时改用该客户端（回退），Delay 为重试前的等待。
type RequestErrorDecision struct {
	Action   RequestErrorAction
	Messages []llm.Message
	Client   llm.ToolStreamClient
	Delay    time.Duration
}

// RequestFailure 是一次失败的模型请求。
type RequestFailure struct {
	Step, Attempt int
	Err           error
	Class         llm.ClassifiedError
	Messages      []llm.Message
}

// RequestErrorHook 在模型请求失败时执行（WithModelRetry 的重试之后），第一个返回 RequestRetry 的决定生效。
type RequestErrorHook func(ctx context.Context, failure *RequestFailure) (RequestErrorDecision, error)

// Stopping 是即将结束本轮时的状态。
type Stopping struct {
	Step   int
	Answer string
	// Continued 是本轮已经续跑的次数（Steer 唤醒与 TurnStopping），钩子可据此避免无限续跑。
	Continued int
}

// TurnStoppingHook 在模型给出最终答复、收件箱没有待处理的 Steer 时执行；返回非空字符串时把它作为 user 消息追加并续跑
// （目标驱动、Claude Code Stop 钩子）。没有剩余额度时不再续跑，按原答复结束。
type TurnStoppingHook func(ctx context.Context, stopping *Stopping) (continueWith string, err error)

// PromptScope 是计算提示词段落与上下文时可用的信息。
type PromptScope struct {
	// SessionID 与工具调用看到的会话 ID 相同：宿主在 ctx 中声明的会话优先，否则为 WithSessionID 的值。
	SessionID string
	Step      int
}

// PromptSection 进入 system 消息，每轮运行开始时计算一次，同一轮内字节不变，保持提示词缓存前缀稳定。
// 适合身份、规则、项目说明。按 Order、Name 排序。
type PromptSection struct {
	Name  string
	Order int
	Text  func(PromptScope) string
}

// PromptContext 每步重算，作为一条 user 消息附在本次请求末尾，不进入之后的历史。适合工作目录、审批模式、待办摘要等易变状态。
type PromptContext struct {
	Name  string
	Order int
	Text  func(PromptScope) string
}

// WithPreStep 追加 PreStep 钩子。
func (b *AgentBuilder[C, S]) WithPreStep(hooks ...PreStepHook) *AgentBuilder[C, S] {
	for _, hook := range hooks {
		b.config.PreStep = append(b.config.PreStep, preStep{hook: hook})
	}
	return b
}

// WithIdentifiedPreStep 追加带身份的 PreStep 钩子。identity 标识钩子的配置（如版本与配置摘要），进入持久日志中 PreStep
// 记录的意图：配置改变后恢复旧运行时以 intent changed 失败，而不是回放按旧配置算出的结果。
func (b *AgentBuilder[C, S]) WithIdentifiedPreStep(identity string, hook PreStepHook) *AgentBuilder[C, S] {
	b.config.PreStep = append(b.config.PreStep, preStep{identity: identity, hook: hook})
	return b
}

// WithRequestHook 追加 Request 钩子。
func (b *AgentBuilder[C, S]) WithRequestHook(hooks ...RequestHook) *AgentBuilder[C, S] {
	b.config.RequestHooks = append(b.config.RequestHooks, hooks...)
	return b
}

// WithRequestError 追加 RequestError 钩子；maxRecoveries 是单步内按钩子决定重试的上限（默认 3）。
func (b *AgentBuilder[C, S]) WithRequestError(maxRecoveries int, hooks ...RequestErrorHook) *AgentBuilder[C, S] {
	b.config.RequestErrorHooks = append(b.config.RequestErrorHooks, hooks...)
	if maxRecoveries <= 0 {
		maxRecoveries = 3
	}
	b.config.MaxRequestRecoveries = maxRecoveries
	return b
}

// WithTurnStopping 追加 TurnStopping 钩子。
func (b *AgentBuilder[C, S]) WithTurnStopping(hooks ...TurnStoppingHook) *AgentBuilder[C, S] {
	b.config.TurnStopping = append(b.config.TurnStopping, hooks...)
	return b
}

// WithPromptSections 追加 system 中的提示词段落。
func (b *AgentBuilder[C, S]) WithPromptSections(sections ...PromptSection) *AgentBuilder[C, S] {
	b.config.PromptSections = append(b.config.PromptSections, sections...)
	return b
}

// WithPromptContexts 追加每步重算的提示词上下文。
func (b *AgentBuilder[C, S]) WithPromptContexts(contexts ...PromptContext) *AgentBuilder[C, S] {
	b.config.PromptContexts = append(b.config.PromptContexts, contexts...)
	return b
}

// loopIntent 是循环扩展点记录的意图。
var loopIntent = json.RawMessage(`"goagent-loop-v1"`)

// journaled 在持久运行中把 compute 的结果记入日志，恢复时回放记录而不重新计算；非持久运行直接计算。
func journaled[T any](ctx context.Context, id string, compute func(context.Context) (T, error)) (T, error) {
	return journaledIntent(ctx, id, loopIntent, compute)
}

// journaledIntent 与 journaled 相同，记录的意图为 intent：恢复时意图不同即失败。
func journaledIntent[T any](ctx context.Context, id string, intent json.RawMessage, compute func(context.Context) (T, error)) (T, error) {
	var zero T
	if !durable.HasJournal(ctx) {
		return compute(ctx)
	}
	record, err := durable.Do(ctx, id, intent, durable.OperationPolicy{MaxAttempts: 3, RetryFailed: true},
		func(c context.Context) (json.RawMessage, error) {
			v, err := compute(c)
			if err != nil {
				return nil, err
			}
			return json.Marshal(v)
		})
	if err != nil {
		return zero, err
	}
	var v T
	if err := json.Unmarshal(record.Value, &v); err != nil {
		return zero, fmt.Errorf("%s: %w", id, err)
	}
	return v, nil
}

type promptText struct {
	name  string
	order int
	text  func(PromptScope) string
}

// renderPrompt 按 Order、Name 拼接非空的段落。
func renderPrompt(items []promptText, scope PromptScope) string {
	slices.SortStableFunc(items, func(a, b promptText) int {
		if a.order != b.order {
			return a.order - b.order
		}
		return strings.Compare(a.name, b.name)
	})
	var parts []string
	for _, item := range items {
		if text := strings.TrimSpace(item.text(scope)); text != "" {
			parts = append(parts, text)
		}
	}
	return strings.Join(parts, "\n\n")
}

func (a *ConfigurableAgent[C, S]) sectionText(scope PromptScope) string {
	items := make([]promptText, len(a.config.PromptSections))
	for i, s := range a.config.PromptSections {
		items[i] = promptText{s.Name, s.Order, s.Text}
	}
	return renderPrompt(items, scope)
}

func (a *ConfigurableAgent[C, S]) contextText(scope PromptScope) string {
	items := make([]promptText, len(a.config.PromptContexts))
	for i, c := range a.config.PromptContexts {
		items[i] = promptText{c.Name, c.Order, c.Text}
	}
	return renderPrompt(items, scope)
}

// stepContextMessage 是附在请求末尾的上下文消息。
func stepContextMessage(text string) llm.Message {
	return llm.UserMessage("[运行上下文]\n" + text)
}

// withSections 把提示词段落追加到 system 消息，没有 system 消息时插在最前。
func withSections(messages []llm.Message, text string) []llm.Message {
	if text == "" {
		return messages
	}
	if len(messages) > 0 && messages[0].Role == "system" {
		out := slices.Clone(messages)
		out[0].Content = strings.TrimSpace(out[0].Content + "\n\n" + text)
		return out
	}
	return append([]llm.Message{llm.SystemMessage(text)}, messages...)
}

// preStepIntent 是 PreStep 记录的意图：没有带身份的钩子时同其他扩展点，否则包含按注册顺序的全部身份。
func (a *ConfigurableAgent[C, S]) preStepIntent() (json.RawMessage, error) {
	identities := make([]string, len(a.config.PreStep))
	named := false
	for i, p := range a.config.PreStep {
		identities[i] = p.identity
		named = named || p.identity != ""
	}
	if !named {
		return loopIntent, nil
	}
	return json.Marshal(struct {
		Version string   `json:"version"`
		PreStep []string `json:"pre_step"`
	}{"goagent-loop-v1", identities})
}

func (a *ConfigurableAgent[C, S]) runPreStep(ctx context.Context, round, inputs int, final bool, messages []llm.Message) ([]llm.Message, PreStepResult, error) {
	if len(a.config.PreStep) == 0 {
		return messages, PreStepResult{}, nil
	}
	type outcome struct {
		Messages []llm.Message `json:"messages"`
		Result   PreStepResult `json:"result"`
	}
	intent, err := a.preStepIntent()
	if err != nil {
		return nil, PreStepResult{}, err
	}
	o, err := journaledIntent(ctx, fmt.Sprintf("loop/pre/%d", round), intent, func(ctx context.Context) (outcome, error) {
		step := &Step{Index: round, Messages: messages, Inputs: inputs, Final: final}
		for _, p := range a.config.PreStep {
			hook := p.hook
			var r PreStepResult
			if err := callHook("PreStep", func() (e error) { r, e = hook(ctx, step); return }); err != nil {
				return outcome{}, err
			}
			if r.Halt {
				return outcome{Messages: step.Messages, Result: r}, nil
			}
		}
		return outcome{Messages: step.Messages}, nil
	})
	return o.Messages, o.Result, err
}

func (a *ConfigurableAgent[C, S]) runTurnStopping(ctx context.Context, round, continued int, answer string) (string, error) {
	if len(a.config.TurnStopping) == 0 {
		return "", nil
	}
	return journaled(ctx, fmt.Sprintf("loop/stopping/%d", round), func(ctx context.Context) (string, error) {
		for _, hook := range a.config.TurnStopping {
			var next string
			if err := callHook("TurnStopping", func() (e error) {
				next, e = hook(ctx, &Stopping{Step: round, Answer: answer, Continued: continued})
				return
			}); err != nil {
				return "", err
			}
			if strings.TrimSpace(next) != "" {
				return next, nil
			}
		}
		return "", nil
	})
}

// recoverRequest 依次询问 RequestError 钩子，返回第一个重试决定；都放弃时返回 nil。
func (a *ConfigurableAgent[C, S]) recoverRequest(ctx context.Context, failure *RequestFailure) (*RequestErrorDecision, error) {
	for _, hook := range a.config.RequestErrorHooks {
		var d RequestErrorDecision
		if err := callHook("RequestError", func() (e error) { d, e = hook(ctx, failure); return }); err != nil {
			return nil, err
		}
		if d.Action == RequestRetry {
			return &d, nil
		}
	}
	return nil, nil
}

// callHook 把钩子的 panic 转为错误。
func callHook(name string, fn func() error) (err error) {
	defer recoverPanic(name+" 钩子", &err)
	return fn()
}

// redirection 是被 Redirect 取消的请求：已流出的文本与纠正消息。
type redirection struct {
	partial  string
	messages []string
}

// requestTurn 组装并发送本步的模型请求：附加运行上下文、执行 Request 钩子；失败时按 RequestError 钩子恢复，
// 恢复时替换的消息作为新的历史返回。被 Redirect 取消时返回 redirection。
func (a *ConfigurableAgent[C, S]) requestTurn(ctx context.Context, box *inbox, round int, client llm.ToolStreamClient, messages []llm.Message, tools []interfaces.Tool, allowEmpty bool, emit func(AgentEvent) error) (*llm.StreamChunk, *redirection, []llm.Message, error) {
	withContext := func(msgs []llm.Message) ([]llm.Message, error) {
		if len(a.config.PromptContexts) == 0 {
			return msgs, nil
		}
		text, err := journaled(ctx, fmt.Sprintf("loop/context/%d", round), func(context.Context) (string, error) {
			return a.contextText(PromptScope{SessionID: toolspkg.SessionIDFrom(ctx), Step: round}), nil
		})
		if err != nil || text == "" {
			return msgs, err
		}
		return append(slices.Clone(msgs), stepContextMessage(text)), nil
	}
	reqMessages, err := withContext(messages)
	if err != nil {
		return nil, nil, messages, err
	}
	req := &ModelRequest{Step: round, Client: client, Request: &llm.CompletionRequest{Messages: reqMessages, MaxTokens: a.config.MaxTokens, Temperature: a.config.Temperature}, Tools: tools}
	for _, hook := range a.config.RequestHooks {
		if err := callHook("Request", func() error { return hook(ctx, req) }); err != nil {
			return nil, nil, messages, err
		}
	}
	if req.Client == nil || req.Request == nil {
		return nil, nil, messages, fmt.Errorf("模型客户端或请求被 Request 钩子清空")
	}
	redirectable := box != nil && !durable.HasJournal(ctx)
	for attempt := 0; ; attempt++ {
		turnCtx, cancel := context.WithCancelCause(ctx)
		var partial strings.Builder
		stepEmit := func(e AgentEvent) error {
			if e.Type == "text" {
				partial.WriteString(e.Delta)
			}
			return emit(e)
		}
		if redirectable {
			box.beginModel(cancel)
		}
		turn, err := readJournaledTurn(turnCtx, round, req.Client, req.Request, tools, allowEmpty, stepEmit)
		var redirects []string
		if redirectable {
			redirects = box.endModel()
		}
		cause := context.Cause(turnCtx)
		cancel(nil)
		if len(redirects) > 0 {
			if err != nil && errors.Is(cause, errRedirected) && ctx.Err() == nil {
				return nil, &redirection{partial: partial.String(), messages: redirects}, messages, nil
			}
			// 请求在取消前已经完成：纠正按 Steer 在下一步领取。
			box.requeue(redirects)
		}
		if err == nil {
			return turn, nil, messages, nil
		}
		if durable.HasJournal(ctx) || len(a.config.RequestErrorHooks) == 0 || attempt >= a.config.MaxRequestRecoveries || ctx.Err() != nil {
			return nil, nil, messages, err
		}
		decision, herr := a.recoverRequest(ctx, &RequestFailure{Step: round, Attempt: attempt, Err: err, Class: llm.ClassifyError(err), Messages: messages})
		if herr != nil {
			return nil, nil, messages, herr
		}
		if decision == nil {
			return nil, nil, messages, err
		}
		if decision.Messages != nil {
			messages = decision.Messages
			if req.Request.Messages, err = withContext(messages); err != nil {
				return nil, nil, messages, err
			}
		}
		if decision.Client != nil {
			req.Client = decision.Client
		}
		if decision.Delay > 0 {
			timer := time.NewTimer(decision.Delay)
			select {
			case <-ctx.Done():
				timer.Stop()
				return nil, nil, messages, ctx.Err()
			case <-timer.C:
			}
		}
	}
}

func metadataInt(m map[string]interface{}, key string) int {
	v, _ := m[key].(int)
	return v
}
