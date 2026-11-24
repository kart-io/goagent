package builder

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sync"
	"time"

	"github.com/kart-io/goagent/constants"
	"github.com/kart-io/goagent/core/budget"
	"github.com/kart-io/goagent/core/durable"
	"github.com/kart-io/goagent/core/middleware"
	"github.com/kart-io/goagent/core/observe"
	agentErrors "github.com/kart-io/goagent/errors"
	"github.com/kart-io/goagent/interfaces"
	"github.com/kart-io/goagent/llm"
	"github.com/kart-io/goagent/memory"
	"github.com/kart-io/goagent/tools"
)

// AgentEvent is one event in ExecuteStream. Type is sources, step, text, usage,
// error, or done. Only done contains the final, middleware-processed Output.
// With a ToolStreamClient, the model chooses tools or answers directly in the
// same round. Intermediate text may contain explanations before tool calls;
// only done.Output is the final answer. When a failure occurs after a tool's
// tool_start and before its tool_end, the error event's Tool is that tool's
// name; otherwise it is empty.
type AgentEvent struct {
	Type    string                 `json:"type"`
	Sources []RetrievalSource      `json:"sources,omitempty"`
	Step    string                 `json:"step,omitempty"`
	Tool    string                 `json:"tool,omitempty"`
	Delta   string                 `json:"delta,omitempty"`
	Usage   *interfaces.TokenUsage `json:"usage,omitzero"`
	Output  *AgentOutput           `json:"output,omitzero"`
	Error   error                  `json:"-"`
	Message string                 `json:"error,omitempty"`
}

// ExecuteStream combines retrieval, native tool execution and answer streaming,
// running the configured middleware and completion callbacks.
// Cancel ctx when abandoning the stream; cancellation closes it promptly. On
// cancellation an error event is best-effort, and ctx.Err() is authoritative.
// A cached middleware response may produce only done, with no model deltas.
func (a *ConfigurableAgent[S]) ExecuteStream(ctx context.Context, input interface{}) (_ <-chan AgentEvent, err error) {
	// 工具里运行的子 Agent 继承父 Agent 的 ctx，但父 Handle 的输入和对话不属于它；Handle 自己的轮次走 executeStream。
	ctx = context.WithValue(context.WithValue(ctx, inboxKey{}, (*inbox)(nil)), historyKey{}, nil)
	return a.executeStream(ctx, input, nil)
}

// streamResult carries a turn's authoritative outcome to its owning Handle.
// The producer writes it before closing events; readers must drain events to
// closure before reading it. Public cancellation events remain best-effort.
type streamResult struct {
	output *AgentOutput
	err    error
}

func (a *ConfigurableAgent[S]) executeStream(ctx context.Context, input interface{}, result *streamResult) (_ <-chan AgentEvent, err error) {
	// 同步阶段也会调用宿主代码（如 scopeDurableExecution 中的工具与持久存储），panic 时转为错误返回。
	defer recoverPanic("Agent 流式执行启动", &err)
	// 每次执行只拥有自己打开的日志轮次；工具子调用和 Handle 都不能继承父轮次的写入权。
	ctx = context.WithValue(ctx, sessionLogKey{}, (*sessionTurn)(nil))
	if a.config.UsageHook != nil {
		ctx = budget.WithUsageHook(ctx, a.config.UsageHook)
	}
	ctx = withTokenEstimator(ctx, a.config.TokenEstimator)
	ctx = ensureModelScope(ctx)
	if err := validateModelBudget(ctx); err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	client := a.llmClient
	if _, native := a.llmClient.(llm.ToolStreamClient); len(a.tools) > 0 && !native {
		return nil, agentErrors.Newf(agentErrors.CodeInvalidConfig, "configured tools require llm.ToolStreamClient").WithComponent("builder").WithOperation("execute_stream")
	}
	// 终态事件按调用方的 ctx 投递：执行 ctx 可能因 WithTimeout 已结束，与仍在接收的调用方同时就绪时 select 会随机丢掉终态，
	// 调用方只看到流被关闭。
	callerCtx := ctx
	ctx, release, err := a.beginExecution(ctx)
	if err != nil {
		return nil, err
	}
	// 启动 goroutine 前出错或 panic 都要释放执行权，之后由 goroutine 释放。
	started := false
	defer func() {
		if !started {
			release()
		}
	}()
	ctx, err = a.scopeDurableExecution(ctx)
	if err != nil {
		return nil, err
	}
	ctx = a.bindLogger(ctx)
	ctx, err = a.prepareAgentUsage(ctx)
	if err != nil {
		return nil, err
	}
	events := make(chan AgentEvent, 1)
	started = true
	go func() {
		defer close(events)
		defer release()
		ctx, endAgent := observe.Start(ctx, observe.Event{Type: "agent", Path: "agent"})
		emit := func(e AgentEvent) error {
			select {
			case events <- e:
				return nil
			case <-callerCtx.Done():
				return callerCtx.Err()
			}
		}
		// 链内与回调的 panic 已由 execute 转为错误；这里兜底其余代码：本 goroutine 的 panic 调用方无法 recover，
		// 转为错误走观测与 error 事件。
		// running 记录本次处理已发 tool_start 未发 tool_end 的工具：工具失败时不发 tool_end，error 事件据此带上工具名。
		// 并发调用的结果按调用顺序处理，最早未结束的即失败的那个。
		var mu sync.Mutex
		var running []string
		output, err := func() (output *AgentOutput, err error) {
			defer recoverPanic("Agent 流式执行", &err)
			chain := a.chain.WithHandler(func(runCtx context.Context, request *middleware.MiddlewareRequest) (*middleware.MiddlewareResponse, error) {
				mu.Lock()
				running = nil
				mu.Unlock()
				// Respect the configured timeout for sends as well as upstream calls.
				send := func(e AgentEvent) error {
					select {
					case events <- e:
					case <-runCtx.Done():
						return runCtx.Err()
					}
					if e.Type == "step" && e.Tool != "" {
						mu.Lock()
						switch e.Step {
						case "tool_start":
							running = append(running, e.Tool)
						case "tool_end":
							if i := slices.Index(running, e.Tool); i >= 0 {
								running = slices.Delete(running, i, i+1)
							}
						}
						mu.Unlock()
					}
					return nil
				}
				return a.runEvents(runCtx, request, client, send)
			})
			return a.execute(ctx, input, chain)
		}()
		err = tools.Settle(ctx, err)
		// 先结束观测再发终态：调用方收到 done/error 即可能返回并关闭记录器。
		endAgent(err, false)
		if result != nil {
			result.output, result.err = output, err
		}
		if err != nil {
			failed := AgentEvent{Type: "error", Error: err, Message: err.Error()}
			mu.Lock()
			if len(running) > 0 {
				failed.Tool = running[0]
			}
			mu.Unlock()
			_ = emit(failed)
			return
		}
		_ = emit(AgentEvent{Type: "done", Output: output})
	}()
	return events, nil
}

func (a *ConfigurableAgent[S]) runEvents(ctx context.Context, request *middleware.MiddlewareRequest, client llm.Client, emit func(AgentEvent) error) (*middleware.MiddlewareResponse, error) {
	input, err := a.inputSerializer.Serialize(request.Input)
	if err != nil {
		return nil, agentErrors.Wrap(err, agentErrors.CodeInvalidInput, "failed to serialize input")
	}
	evidence := ""
	metadata := make(map[string]interface{}, len(request.Metadata))
	for k, v := range request.Metadata {
		metadata[k] = v
	}
	if a.retriever != nil {
		retrieved, details, err := retrieveContext(ctx, a.retriever, a.retrievalConfig, input)
		if err != nil {
			return nil, err
		}
		for k, v := range details {
			metadata[k] = v
		}
		evidence = retrieved
		if err := emit(AgentEvent{Type: "sources", Sources: details["retrieval_sources"].([]RetrievalSource)}); err != nil {
			return nil, err
		}
	}
	if a.config.SessionLog != nil {
		turn, history, err := a.openSessionTurn(ctx, input)
		if err != nil {
			return nil, fmt.Errorf("会话日志: %w", err)
		}
		if turn.completed {
			ctx = durable.WithReplayOnly(ctx)
		}
		ctx = context.WithValue(context.WithValue(ctx, historyKey{}, history), sessionLogKey{}, turn)
		if turn.memory != nil {
			ctx = memory.WithTurn(ctx, *turn.memory)
		}
	}
	ctx = withModelCacheScope(ctx, a.modelCacheScope(ctx))
	messages, err := a.streamMessages(ctx, input, evidence)
	if err != nil {
		return nil, err
	}
	if native := nativeToolLoop(client, len(a.tools)); native != nil {
		return a.runToolEvents(ctx, request, input, native, messages, metadata, emit)
	}
	// 纯聊天沿用原模型操作键；日志与工具循环共用轮次协议，不添加占位工具。
	slog := sessionTurnFrom(ctx)
	if err := slog.begin(ctx, messages, nil); err != nil {
		return nil, fmt.Errorf("会话日志: %w", err)
	}
	if err := slog.sync(ctx, messages); err != nil {
		return nil, fmt.Errorf("会话日志: %w", err)
	}
	attempt := 0
	ctx = withInterruptedModelResend(withRequestRecovery(ctx, a.requestRecovery(0, &messages, &attempt)), a.config.InterruptedModelResends)
	before, counted := modelCacheCounts(ctx)
	req := &llm.CompletionRequest{Reasoning: a.config.Reasoning.Clone(), Messages: messages, MaxTokens: a.config.MaxTokens, Temperature: a.config.Temperature, TemperatureSet: a.config.TemperatureSet}
	var turn *llm.StreamChunk
	if slog != nil && slog.recallMode == plainMemoryMode {
		slot, e := a.plainRecallSlot(ctx, messages)
		if e != nil {
			return nil, e
		}
		slot.index = len(messages)
		req.Messages = withPlainRecall(messages, slot.text)
		intentReq := *req
		// 从未注入正文的会话消息组成意图，保留用户自己发送的同文消息。
		intentReq.Messages = recallIntent(messages, recallSlot{index: slot.index, placeholder: slot.placeholder})
		// 每次实际发送重新核对已采用的引用，RequestError 重试期间可发生删除。
		currentRequest := func(callCtx context.Context) (*llm.CompletionRequest, error) {
			r, _, err := a.config.LongTermMemory.Hydrate(callCtx, *slot.snapshot, slot.refs)
			if err != nil {
				return nil, fmt.Errorf("还原长期记忆: %w", err)
			}
			current := *req
			current.Messages = withPlainRecall(messages, r.Text)
			return &current, nil
		}
		turn, err = readPlainModelTurnWithIntent(ctx, "model/answer/0", client, req, &intentReq, currentRequest, emit)
	} else {
		turn, err = readPlainModelTurn(ctx, "model/answer/0", client, req, emit)
	}
	if replan := (*replanRequest)(nil); errors.As(err, &replan) {
		return nil, fmt.Errorf("RequestError 替换消息或客户端只作用于原生工具流：%w", replan.err)
	}
	if err != nil {
		return nil, err
	}

	usage := &interfaces.TokenUsage{}
	if turn.Usage != nil {
		copy := turn.Usage.Tokens()
		usage.Add(&copy)
	}
	after, _ := modelCacheCounts(ctx)
	metadata["answer_usage_reported"] = answerUsageReported(turn, counted && after.cachedSince(before))
	if counted {
		metadata["model_cache_hits"] = after.hits
	}

	recordAnswer(request, turn.Content)
	if slog != nil {
		final := llm.Message{Role: constants.RoleAssistant, Content: turn.Content, ReasoningContent: turn.ReasoningContent, ProviderState: turn.ProviderState}
		if err := slog.end(ctx, append(slices.Clone(messages), final), turn.Content, usage); err != nil {
			return nil, fmt.Errorf("会话日志: %w", err)
		}
	}
	return a.finishEvents(ctx, request, input, turn.Content, usage, metadata, emit)
}

// answerUsageReported 判断最终回答的用量是否已报告：完全来自缓存的回合不是真实调用，没有用量可报告，视为已报告。
func answerUsageReported(turn *llm.StreamChunk, cached bool) bool {
	return turn.Usage != nil || cached
}

// recordAnswer 在记录轮次结束之前写入状态，使会话日志中的状态包含本轮回答。
func recordAnswer(request *middleware.MiddlewareRequest, answer string) {
	if request.State != nil {
		request.State.Set("last_response", answer)
		request.State.Set("last_timestamp", time.Now())
	}
}

func (a *ConfigurableAgent[S]) finishEvents(ctx context.Context, request *middleware.MiddlewareRequest, input, answer string, usage *interfaces.TokenUsage, metadata map[string]interface{}, emit func(AgentEvent) error) (*middleware.MiddlewareResponse, error) {
	if c, ok := budget.FromContext(ctx); ok {
		snapshot, err := c.Snapshot(ctx)
		if err != nil {
			return nil, err
		}
		metadata["root_budget"] = snapshot
		metadata["new_model_invocations"] = modelInvocations(ctx)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := emit(AgentEvent{Type: "usage", Usage: usage}); err != nil {
		return nil, err
	}
	return &middleware.MiddlewareResponse{Output: answer, State: request.State, Metadata: metadata, TokenUsage: usage}, nil
}
