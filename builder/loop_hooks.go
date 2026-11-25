package builder

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/kart-io/goagent/codec/json"
	"github.com/kart-io/goagent/constants"
	"github.com/kart-io/goagent/core/durable"
	agentErrors "github.com/kart-io/goagent/errors"
	"github.com/kart-io/goagent/interfaces"
	"github.com/kart-io/goagent/llm"
	"github.com/kart-io/goagent/memory"
	toolspkg "github.com/kart-io/goagent/tools"
)

// 循环扩展点只作用于原生工具流（llm.ToolStreamClient 且配置了工具）。持久运行中，PreStep、TurnStopping、
// 提示词段落与上下文、收件箱的输入按步骤记入日志，恢复时回放记录而不重新计算；Request 钩子须是确定性的；
// RequestError 钩子在同一条回合记录内原样重试，替换消息或客户端只在非持久运行中生效。
// 例外：RequestError 的原样重试也作用于没有工具的流式回答。

// Step is the state before one model request.
type Step struct {
	// Index is the index of the model request within the turn, starting at 0.
	Index int
	// Messages are the messages to be sent; PreStep may rewrite them (for
	// example, to compact). The rewritten messages become the history for
	// subsequent steps.
	Messages []llm.Message
	// Inputs is the number of user inputs newly received this step at the end
	// of Messages: turn input, Steer, Inject, and Redirect corrections,
	// excluding TurnStopping continuation messages.
	Inputs int
	// Final reports whether this is the last model request of the turn: tools
	// are no longer offered (except ReturnDirectTools) and the model must
	// answer directly.
	Final bool
}

// PreStepResult is the result of a PreStep. When Halt is true, the model is not
// requested and the turn ends with Answer as its final reply.
type PreStepResult struct {
	Halt   bool   `json:"halt,omitzero"`
	Answer string `json:"answer,omitempty"`
}

// PreStepHook runs after inbox input is claimed and before the request is
// assembled; returning an error aborts the run.
type PreStepHook func(ctx context.Context, step *Step) (PreStepResult, error)

// preStep 是注册的 PreStep 钩子与其身份。
type preStep struct {
	identity string
	hook     PreStepHook
}

// ModelRequest is the model request about to be sent. Request hooks may replace
// Client (routing, switching models) or modify the parameters of Request; Tools
// is read-only.
type ModelRequest struct {
	Step int
	// Final is the same as Step.Final: this is the last request of the turn,
	// and Tools contains only ReturnDirectTools.
	Final   bool
	Client  llm.ToolStreamClient
	Request *llm.CompletionRequest
	Tools   []interfaces.Tool
}

// RequestHook runs before a model request is sent; returning an error aborts
// the run.
type RequestHook func(ctx context.Context, req *ModelRequest) error

// RequestErrorAction is the action taken after a model request fails. The zero
// value is RequestFail.
type RequestErrorAction int

const (
	// RequestFail defers to the next hook; when all hooks give up, the run
	// fails with the original error.
	RequestFail RequestErrorAction = iota
	// RequestRetry retries this step after applying the RequestErrorDecision.
	RequestRetry
)

// RequestErrorDecision is a RequestError hook's decision. A non-nil Messages
// replaces the messages before retrying (compaction), a non-nil Client switches
// to that client (fallback), and Delay is the wait before retrying.
type RequestErrorDecision struct {
	Action   RequestErrorAction
	Messages []llm.Message
	Client   llm.ToolStreamClient
	Delay    time.Duration
}

// RequestFailure is one failed model request. Attempt is the number of times
// this step has already recovered based on hook decisions.
type RequestFailure struct {
	Step, Attempt int
	Err           error
	Class         llm.ClassifiedError
	Messages      []llm.Message
	// Emitted reports whether the failed request has already emitted content to
	// the consumer, so retrying as-is would duplicate output; it is always
	// false when WithReplayablePartialOutput is enabled.
	Emitted bool
}

// RequestErrorHook runs when a model request fails; the first decision
// returning RequestRetry takes effect.
type RequestErrorHook func(ctx context.Context, failure *RequestFailure) (RequestErrorDecision, error)

func defaultRetryBackoff(retry int) time.Duration {
	return min(constants.DefaultTransientRetryBaseDelay<<min(retry-1, 5), constants.DefaultTransientRetryMaxDelay)
}

// RetryTransient returns a RequestError hook that retries transient failures
// as-is: it retries only failures llm.ClassifyError deems retryable (429, 5xx,
// and connection errors), and does not retry once the failed request has
// emitted content, to avoid duplicate output. When the server sends Retry-After
// it waits accordingly; otherwise it uses backoff (when nil, exponential growth
// starting at 500ms, capped at 8s). It gives up when the wait would pass the
// ctx deadline or Retry-After exceeds 1 minute. retries is the number of
// retries after the first request, so 0 never retries; it caps the recoveries
// already made in this step and shares WithRequestError's maxRecoveries with
// recoveries by other hooks.
func RetryTransient(retries int, backoff func(retry int) time.Duration) RequestErrorHook {
	if backoff == nil {
		backoff = defaultRetryBackoff
	}
	return func(ctx context.Context, f *RequestFailure) (RequestErrorDecision, error) {
		if f.Emitted || f.Attempt >= retries || !f.Class.Retryable {
			return RequestErrorDecision{}, nil
		}
		var delay time.Duration
		if pe := (*llm.ProviderError)(nil); errors.As(f.Err, &pe) && pe.RetryAfter > 0 {
			if pe.RetryAfter > constants.MaxModelRetryAfter {
				return RequestErrorDecision{}, nil
			}
			delay = pe.RetryAfter
		} else {
			delay = max(backoff(f.Attempt+1), 0)
		}
		if deadline, ok := ctx.Deadline(); ok && time.Until(deadline) <= delay {
			return RequestErrorDecision{}, nil
		}
		return RequestErrorDecision{Action: RequestRetry, Delay: delay}, nil
	}
}

// Stopping is the state when the turn is about to end.
type Stopping struct {
	Step   int
	Answer string
	// Continued is the number of times the turn has already continued (Steer
	// wake-ups and TurnStopping).
	Continued int
	// Blocked is the number of continuations initiated by TurnStopping in this
	// turn, excluding Steer wake-ups; hooks use it to limit retries and avoid
	// continuing forever.
	Blocked int
}

// TurnStoppingHook runs when the model gives its final reply and the inbox has
// no pending Steer; returning a non-empty string appends it as a user message
// and continues the turn (goal-driven, like the Claude Code Stop hook). At
// least one more step with tools must remain after continuing; otherwise it
// does not run, the turn ends with the original reply, and
// turn_stopping_skipped is recorded in the result metadata.
type TurnStoppingHook func(ctx context.Context, stopping *Stopping) (continueWith string, err error)

// PromptScope is the information available when computing prompt sections and
// contexts.
type PromptScope struct {
	// SessionID is the same session ID that tool calls see: a session declared
	// by the host in ctx takes precedence; otherwise it is the value from
	// WithSessionID.
	SessionID string
	Step      int
	// Final is the same as Step.Final. Final and Deadline are provided only to
	// PromptContext; sections must not change within a turn, so both are zero
	// values when computing them.
	Final bool
	// Deadline is the run's deadline (WithTimeout or the caller's ctx), or the
	// zero value when there is none. In a durable run it is recorded in the
	// journal with the context.
	Deadline time.Time
}

// PromptSection goes into the system message. It is computed once at the start
// of each run turn and stays byte-identical within a turn, keeping the prompt
// cache prefix stable. It suits identity, rules, and project notes. Sections
// are sorted by Order, then Name.
type PromptSection struct {
	Name  string
	Order int
	Text  func(PromptScope) string
}

// PromptContext is recomputed every step and attached as one user message at
// the end of that request; it does not enter later history. It suits volatile
// state such as the working directory, approval mode, or a to-do summary.
type PromptContext struct {
	Name  string
	Order int
	Text  func(PromptScope) string
}

// WithPreStep appends PreStep hooks.
func (b *AgentBuilder[S]) WithPreStep(hooks ...PreStepHook) *AgentBuilder[S] {
	for _, hook := range hooks {
		b.config.PreStep = append(b.config.PreStep, preStep{hook: hook})
	}
	return b
}

// WithIdentifiedPreStep appends a PreStep hook with an identity. identity
// identifies the hook's configuration (such as a version and configuration
// digest) and becomes part of the intent of the PreStep record in the durable
// journal: after the configuration changes, resuming an old run fails with
// intent changed instead of replaying results computed under the old
// configuration.
func (b *AgentBuilder[S]) WithIdentifiedPreStep(identity string, hook PreStepHook) *AgentBuilder[S] {
	b.config.PreStep = append(b.config.PreStep, preStep{identity: identity, hook: hook})
	return b
}

// WithRequestHook appends Request hooks.
func (b *AgentBuilder[S]) WithRequestHook(hooks ...RequestHook) *AgentBuilder[S] {
	b.config.RequestHooks = append(b.config.RequestHooks, hooks...)
	return b
}

// WithRequestError appends RequestError hooks; maxRecoveries is the per-step
// number of retries hooks may decide, not counting the first request. 0 means
// no recovery (hooks are not consulted) and negative values are rejected by
// Build. The previous default for 0 was 3.
func (b *AgentBuilder[S]) WithRequestError(maxRecoveries int, hooks ...RequestErrorHook) *AgentBuilder[S] {
	b.config.RequestErrorHooks = append(b.config.RequestErrorHooks, hooks...)
	b.config.MaxRequestRecoveries = maxRecoveries
	return b
}

// WithTurnStopping appends TurnStopping hooks.
func (b *AgentBuilder[S]) WithTurnStopping(hooks ...TurnStoppingHook) *AgentBuilder[S] {
	b.config.TurnStopping = append(b.config.TurnStopping, hooks...)
	return b
}

// WithPromptSections appends prompt sections to the system message.
func (b *AgentBuilder[S]) WithPromptSections(sections ...PromptSection) *AgentBuilder[S] {
	b.config.PromptSections = append(b.config.PromptSections, sections...)
	return b
}

// WithPromptContexts appends prompt contexts recomputed every step.
func (b *AgentBuilder[S]) WithPromptContexts(contexts ...PromptContext) *AgentBuilder[S] {
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
// 计算中途崩溃或被取消时没有结果可回放，恢复时重新计算（RetryInterrupted），与 Dispatcher 的默认策略一致。
func journaledIntent[T any](ctx context.Context, id string, intent json.RawMessage, compute func(context.Context) (T, error)) (T, error) {
	var zero T
	if !durable.HasJournal(ctx) {
		return compute(ctx)
	}
	record, err := durable.Do(ctx, id, intent, durable.OperationPolicy{MaxAttempts: 3, RetryFailed: true, RetryInterrupted: true},
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

func (a *ConfigurableAgent[S]) sectionText(scope PromptScope) string {
	items := make([]promptText, len(a.config.PromptSections))
	for i, s := range a.config.PromptSections {
		items[i] = promptText{s.Name, s.Order, s.Text}
	}
	return renderPrompt(items, scope)
}

func (a *ConfigurableAgent[S]) contextText(scope PromptScope) string {
	items := make([]promptText, len(a.config.PromptContexts))
	for i, c := range a.config.PromptContexts {
		items[i] = promptText{c.Name, c.Order, c.Text}
	}
	return renderPrompt(items, scope)
}

// stepContextPrefix 标记附在请求末尾的上下文消息。
const stepContextPrefix = "[运行上下文]\n"

// stepContextMessage 是附在请求末尾的上下文消息。
func stepContextMessage(text string) llm.Message {
	return llm.UserMessage(stepContextPrefix + text)
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
func (a *ConfigurableAgent[S]) preStepIntent() (json.RawMessage, error) {
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

func (a *ConfigurableAgent[S]) runPreStep(ctx context.Context, round, inputs int, final bool, messages []llm.Message) ([]llm.Message, PreStepResult, error) {
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

func (a *ConfigurableAgent[S]) runTurnStopping(ctx context.Context, stopping *Stopping) (string, error) {
	return journaled(ctx, fmt.Sprintf("loop/stopping/%d", stopping.Step), func(ctx context.Context) (string, error) {
		for _, hook := range a.config.TurnStopping {
			var next string
			if err := callHook("TurnStopping", func() (e error) {
				next, e = hook(ctx, stopping)
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

// requestRecovery 返回本步模型请求失败时依次询问 RequestError 钩子的函数，取第一个重试决定；*attempt 累计本步的恢复次数，
// 达到 MaxRequestRecoveries 后不再询问。messages 是钩子看到的本步对话。
func (a *ConfigurableAgent[S]) requestRecovery(step int, messages *[]llm.Message, attempt *int) requestRecovery {
	if len(a.config.RequestErrorHooks) == 0 {
		return nil
	}
	return func(ctx context.Context, emitted bool, err error) (*RequestErrorDecision, error) {
		if *attempt >= a.config.MaxRequestRecoveries {
			return nil, nil
		}
		failure := &RequestFailure{Step: step, Attempt: *attempt, Err: err, Class: llm.ClassifyError(err), Messages: *messages, Emitted: emitted && !a.config.ReplayablePartialOutput}
		for _, hook := range a.config.RequestErrorHooks {
			var d RequestErrorDecision
			if herr := callHook("RequestError", func() (e error) { d, e = hook(ctx, failure); return }); herr != nil {
				return nil, herr
			}
			if d.Action == RequestRetry {
				*attempt++
				return &d, nil
			}
		}
		return nil, nil
	}
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
// 原样重试在模型操作内进行，替换消息或客户端时在这里重新组装，替换的消息作为新的历史返回。被 Redirect 取消时返回 redirection。
func (a *ConfigurableAgent[S]) requestTurn(ctx context.Context, box *inbox, round int, client llm.ToolStreamClient, messages []llm.Message, tools []interfaces.Tool, final, allowEmpty bool, recall *memory.Snapshot, emit func(AgentEvent) error) (*llm.StreamChunk, *redirection, []llm.Message, error) {
	slot, err := a.hydrateRecall(ctx, recall, round)
	if err != nil {
		return nil, nil, messages, err
	}
	// 召回块与运行上下文只进入本步请求，不进入历史：替换消息后重新组装时再次插入。
	withContext := func(msgs []llm.Message) ([]llm.Message, error) {
		out := slices.Clone(msgs)
		slot.index = len(out)
		if slot.text != "" {
			out = append(out, llm.UserMessage(slot.text))
		}
		if len(a.config.PromptContexts) == 0 {
			return out, nil
		}
		text, err := journaled(ctx, fmt.Sprintf("loop/context/%d", round), func(context.Context) (string, error) {
			deadline, _ := ctx.Deadline()
			return a.contextText(PromptScope{SessionID: toolspkg.SessionIDFrom(ctx), Step: round, Final: final, Deadline: deadline}), nil
		})
		if err != nil || text == "" {
			return out, err
		}
		return append(out, stepContextMessage(text)), nil
	}
	reqMessages, err := withContext(messages)
	if err != nil {
		return nil, nil, messages, err
	}
	req := &ModelRequest{Step: round, Final: final, Client: client, Request: &llm.CompletionRequest{Reasoning: a.config.Reasoning.Clone(), Messages: reqMessages, MaxTokens: a.config.MaxTokens, Temperature: a.config.Temperature, TemperatureSet: a.config.TemperatureSet}, Tools: tools}
	for _, hook := range a.config.RequestHooks {
		if err := callHook("Request", func() error { return hook(ctx, req) }); err != nil {
			return nil, nil, messages, err
		}
	}
	if req.Client == nil || req.Request == nil {
		return nil, nil, messages, agentErrors.Newf(agentErrors.CodeInvalidOutput, "模型客户端或请求被 Request 钩子清空").WithComponent("builder").WithOperation("request_turn")
	}
	redirectable := box != nil && !durable.HasJournal(ctx)
	attempt := 0
	for {
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
		turn, err := readJournaledTurn(withInterruptedModelResend(withRequestRecovery(turnCtx, a.requestRecovery(round, &messages, &attempt)), a.config.InterruptedModelResends), round, req.Client, req.Request, tools, allowEmpty, a.config.ToolErrorFeedback, slot, stepEmit)
		var redirects []string
		if redirectable {
			redirects = box.endModel()
		}
		cause := context.Cause(turnCtx)
		cancel(nil)
		if len(redirects) > 0 {
			if err != nil && errors.Is(cause, agentErrors.ErrModelRedirected) && ctx.Err() == nil {
				return nil, &redirection{partial: partial.String(), messages: redirects}, messages, nil
			}
			// 请求在取消前已经完成：纠正按 Steer 在下一步领取。
			box.requeue(redirects)
		}
		if err == nil {
			return turn, nil, messages, nil
		}
		var replan *replanRequest
		if !errors.As(err, &replan) || ctx.Err() != nil {
			return nil, nil, messages, err
		}
		decision := replan.decision
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
