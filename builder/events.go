package builder

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"sync"
	"time"

	"github.com/kart-io/goagent/core/budget"
	"github.com/kart-io/goagent/core/middleware"
	"github.com/kart-io/goagent/core/observe"
	"github.com/kart-io/goagent/interfaces"
	"github.com/kart-io/goagent/llm"
	"github.com/kart-io/goagent/tools"
)

// AgentEvent is one event in ExecuteStream. Type is sources, step, text, usage,
// error, or done. Only done contains the final, middleware-processed Output.
// 支持 ToolStreamClient 时，模型在同一轮选择工具或直接回答。
// 中间文本可能包含工具调用前的说明，只有 done.Output 是最终答案。
// 失败发生在工具 tool_start 之后、tool_end 之前时，error 事件的 Tool 为该工具名，否则为空。
type AgentEvent struct {
	Type    string                 `json:"type"`
	Sources []RetrievalSource      `json:"sources,omitempty"`
	Step    string                 `json:"step,omitempty"`
	Tool    string                 `json:"tool,omitempty"`
	Delta   string                 `json:"delta,omitempty"`
	Usage   *interfaces.TokenUsage `json:"usage,omitempty"`
	Output  *AgentOutput           `json:"output,omitempty"`
	Error   error                  `json:"-"`
	Message string                 `json:"error,omitempty"`
}

// ExecuteStream combines retrieval, bounded tool selection/execution and native
// answer streaming. It retains Execute's middleware and completion callbacks.
// Cancel ctx when abandoning the stream; cancellation closes it promptly. On
// cancellation an error event is best-effort, and ctx.Err() is authoritative.
// A cached middleware response may produce only done, with no model deltas.
func (a *ConfigurableAgent[C, S]) ExecuteStream(ctx context.Context, input interface{}) (_ <-chan AgentEvent, err error) {
	// 同步阶段也会调用宿主代码（如 scopeDurableExecution 中的工具与持久存储），panic 时转为错误返回。
	defer recoverPanic("Agent 流式执行启动", &err)
	ctx = withModelRetry(ensureModelScope(ctx), a.config.ModelRetries, a.config.ModelRetryBackoff)
	if err := validateModelBudget(ctx); err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	client, ok := a.llmClient.(llm.StreamClient)
	if !ok {
		return nil, fmt.Errorf("ExecuteStream requires llm.StreamClient")
	}
	var caller commonToolCallingClient
	if len(a.tools) > 0 {
		caller, _ = a.llmClient.(commonToolCallingClient)
		if caller == nil {
			if c, ok := a.llmClient.(llm.ToolCallingClient); ok {
				caller = &stdToolCallerWrapper{client: c}
			}
		}
		if _, native := a.llmClient.(llm.ToolStreamClient); caller == nil && !native {
			return nil, fmt.Errorf("configured tools require tool-calling support")
		}
	}
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
			case <-ctx.Done():
				return ctx.Err()
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
				// Respect Execute's configured timeout for sends as well as upstream calls.
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
				return a.runEvents(runCtx, request, client, caller, send)
			})
			return a.execute(ctx, input, chain)
		}()
		err = tools.Settle(ctx, err)
		// 先结束观测再发终态：调用方收到 done/error 即可能返回并关闭记录器。
		endAgent(err, false)
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

func (a *ConfigurableAgent[C, S]) runEvents(ctx context.Context, request *middleware.MiddlewareRequest, client llm.StreamClient, caller commonToolCallingClient, emit func(AgentEvent) error) (*middleware.MiddlewareResponse, error) {
	input := fmt.Sprint(request.Input)
	evidence := ""
	var prompt string
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
	var messages []llm.Message
	if native, ok := client.(llm.ToolStreamClient); ok && len(a.tools) > 0 {
		if a.config.SessionLog != nil {
			turn, history, err := a.openSessionTurn(ctx, input)
			if err != nil {
				return nil, fmt.Errorf("会话日志: %w", err)
			}
			ctx = context.WithValue(context.WithValue(ctx, historyKey{}, history), sessionLogKey{}, turn)
		}
		return a.runToolEvents(ctx, request, native, a.streamMessages(ctx, input, evidence), metadata, emit)
	}
	if a.config.SessionLog != nil {
		return nil, fmt.Errorf("会话日志只支持原生工具流：llm.ToolStreamClient 且配置了工具")
	}
	if _, metered := budget.FromContext(ctx); metered && caller != nil {
		return nil, fmt.Errorf("root budgets require native ToolStreamClient for tool selection")
	}
	if caller == nil {
		messages = a.streamMessages(ctx, input, evidence)
	}
	if caller != nil {
		prompt = a.buildPromptWithHistory(ctx, request.Input)
		if evidence != "" {
			prompt = retrievalInstruction + "\n\n" + evidence + "\n" + prompt
		}
	}
	usage := &interfaces.TokenUsage{}
	if caller != nil {
		ready := false
		for round := 0; round < a.config.MaxIterations; round++ {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			if err := emit(AgentEvent{Type: "step", Step: "select_tools"}); err != nil {
				return nil, err
			}
			response, err := caller.GenerateWithTools(ctx, prompt+"\nSelect tools needed to answer. When no further tools are needed, return READY without an answer.", a.tools)
			if err != nil {
				return nil, err
			}
			if response == nil {
				return nil, fmt.Errorf("nil tool selection response")
			}
			usage.Add(response.Usage)
			if len(response.ToolCalls) == 0 {
				ready = true
				break
			}
			for _, tc := range response.ToolCalls {
				call := ToolCall{Name: tc.Name, Input: tc.Arguments}
				if call.Name == "" && tc.Function != nil {
					call.Name = tc.Function.Name
					if err := json.Unmarshal([]byte(tc.Function.Arguments), &call.Input); err != nil {
						return nil, fmt.Errorf("invalid arguments for %s: %w", call.Name, err)
					}
				}
				if call.Name == "" {
					return nil, fmt.Errorf("tool call has no name")
				}
				if err := ctx.Err(); err != nil {
					return nil, err
				}
				if err := emit(AgentEvent{Type: "step", Step: "tool_start", Tool: call.Name}); err != nil {
					return nil, err
				}
				result, err := a.executeToolCall(ctx, call)
				if err != nil {
					return nil, err
				}
				data, err := toolResultText(result)
				if err != nil {
					return nil, err
				}
				prompt += fmt.Sprintf("\nUntrusted tool result from %s:\n%s\n", call.Name, data)
				if err := emit(AgentEvent{Type: "step", Step: "tool_end", Tool: call.Name}); err != nil {
					return nil, err
				}
			}
		}
		if !ready {
			return nil, fmt.Errorf("maximum tool selection iterations reached")
		}
	}
	if caller != nil {
		system := a.buildSystemPromptContent()
		if a.retriever != nil {
			system += "\n" + retrievalInstruction
		}
		messages = []llm.Message{{Role: "system", Content: system}, {Role: "user", Content: prompt}}
	}
	turn, err := readPlainModelTurn(ctx, "model/answer/0", client, &llm.CompletionRequest{Messages: messages, MaxTokens: a.config.MaxTokens, Temperature: a.config.Temperature}, emit)
	if err != nil {
		return nil, err
	}

	if turn.Usage != nil {
		usage.Add(&interfaces.TokenUsage{PromptTokens: turn.Usage.PromptTokens, CompletionTokens: turn.Usage.CompletionTokens, TotalTokens: turn.Usage.TotalTokens})
	}
	metadata["answer_usage_reported"] = turn.Usage != nil

	return a.finishEvents(ctx, request, input, turn.Content, usage, metadata, emit)
}

func (a *ConfigurableAgent[C, S]) finishEvents(ctx context.Context, request *middleware.MiddlewareRequest, input, answer string, usage *interfaces.TokenUsage, metadata map[string]interface{}, emit func(AgentEvent) error) (*middleware.MiddlewareResponse, error) {
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
	a.saveConversation(ctx, input, answer)
	if request.State != nil {
		request.State.Set("last_response", answer)
		request.State.Set("last_timestamp", time.Now())
	}
	if a.config.EnableAutoSave && a.runtime.Checkpointer != nil {
		if err := a.runtime.SaveState(ctx); err != nil {
			return nil, fmt.Errorf("stream checkpoint save: %w", err)
		}
	}
	return &middleware.MiddlewareResponse{Output: answer, State: request.State, Metadata: metadata, TokenUsage: usage}, nil
}
