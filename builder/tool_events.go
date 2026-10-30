package builder

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/kart-io/goagent/builder/contextpolicy"
	"io"
	"slices"
	"strings"
	"sync"

	"github.com/kart-io/goagent/core/durable"
	"github.com/kart-io/goagent/core/middleware"
	"github.com/kart-io/goagent/interfaces"
	"github.com/kart-io/goagent/llm"
	"github.com/kart-io/goagent/tools"
)

// maxFailureRunes 限制 MaxIterations 报错附带的工具失败文本长度。
const maxFailureRunes = 500

// emptyAnswerPrompt 是空回答后追加的固定重问消息；固定文本保证持久恢复时意图不变。
const emptyAnswerPrompt = "上一轮没有输出，请直接给出最终回答或调用工具。"

// isEmptyAnswer 判断模型正常结束却既无正文也无工具调用。
func isEmptyAnswer(turn *llm.StreamChunk) bool {
	return turn.FinishReason == "stop" && len(turn.ToolCalls) == 0 && strings.TrimSpace(turn.Content) == ""
}

// streamMessages 保留消息角色，系统指令与证据各发送一次，避免重复包装。
func (a *ConfigurableAgent[C, S]) streamMessages(ctx context.Context, input, evidence string) []llm.Message {
	system := a.buildSystemPromptContent()
	if a.retriever != nil {
		system += "\n" + retrievalInstruction
	}
	messages := []llm.Message{}
	if system != "" {
		messages = append(messages, llm.SystemMessage(system))
	}
	if a.memoryManager != nil && a.config.SessionID != "" {
		history, err := a.memoryManager.GetConversationHistory(ctx, a.config.SessionID, a.config.MaxConversationHistory)
		if err == nil {
			for _, entry := range history {
				messages = append(messages, llm.Message{Role: entry.Role, Content: entry.Content})
			}
		}
	}
	if evidence != "" {
		input = evidence + "\n用户请求：" + input
	}
	return append(messages, llm.UserMessage(input))
}

// runToolEvents 每轮直接选择工具或输出答案，MaxIterations 包含最终答案请求。
func (a *ConfigurableAgent[C, S]) runToolEvents(ctx context.Context, request *middleware.MiddlewareRequest, client llm.ToolStreamClient, messages []llm.Message, metadata map[string]interface{}, emit func(AgentEvent) error) (response *middleware.MiddlewareResponse, err error) {
	runtime, err := a.newPolicyRuntime(ctx)
	if err != nil {
		return nil, err
	}
	ctx, ephemeral := policyContext(ctx, runtime)
	if ephemeral {
		defer func() {
			if e := runtime.release(ctx); e != nil {
				response, err = nil, errors.Join(err, e)
			}
		}()
	}
	usage := &interfaces.TokenUsage{}
	reports := 0
	policy := newToolContext(a.config.ContextPolicy)
	var summary *llm.StreamChunk
	messages, summary, err = policy.prepare(ctx, messages, a.config.MaxIterations)
	if err != nil {
		return nil, err
	}
	// 反复失败直至额度耗尽时，把最近一次工具失败带进报错，便于定位。
	lastFailure := ""
	exhausted := func() error {
		if lastFailure != "" {
			return fmt.Errorf("模型调用达到 MaxIterations 上限，尚未生成答案；最近一次工具失败：%s", lastFailure)
		}
		return fmt.Errorf("模型调用达到 MaxIterations 上限，尚未生成答案")
	}
	emptyRetries := 0
	callsUsed := 0
	if summary != nil {
		callsUsed = 1
		metadata["summary_calls"] = 1
		if summary.Usage != nil {
			reports++
			usage.Add(&interfaces.TokenUsage{PromptTokens: summary.Usage.PromptTokens, CompletionTokens: summary.Usage.CompletionTokens, TotalTokens: summary.Usage.TotalTokens})
		}
	}
	for round := callsUsed; round < a.config.MaxIterations; round++ {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if err := emit(AgentEvent{Type: "step", Step: "model_start"}); err != nil {
			return nil, err
		}
		turnCtx, cancel := context.WithCancel(ctx)
		tools := a.tools
		if len(runtime.tools) > 0 {
			tools = append(slices.Clone(tools), runtime.tools...)
		}
		if a.config.ContextPolicy != nil && a.config.ContextPolicy.MaxToolResultRunes > 0 {
			tools = append(slices.Clone(tools), policy)
		}
		if round > 0 && round+1 == a.config.MaxIterations {
			messages = policy.restore(messages)
		} else {
			messages, err = policy.reduce(ctx, messages)
		}
		if err != nil {
			cancel()
			return nil, err
		}
		transformed, applyErr := runtime.apply(ctx, contextpolicy.Turn{Round: round, RemainingCalls: a.config.MaxIterations - round, Messages: messages}, round > 0 && round+1 == a.config.MaxIterations)
		if applyErr != nil {
			cancel()
			return nil, applyErr
		}
		messages = transformed.Messages
		runtime.stats(metadata)
		if a.config.ContextPolicy != nil {
			metadata["context_offloaded_results"] = policy.offloaded
		}
		// 已有结果的最后一轮只能回答或使用显式配置的终态工具。
		// 首轮及尚有更多额度时保留完整工具集，支持连续查询。
		if round > 0 && round+1 == a.config.MaxIterations {
			tools = nil
			for _, tool := range a.tools {
				if slices.Contains(a.config.ReturnDirectTools, tool.Name()) {
					tools = append(tools, tool)
				}
			}
			metadata["final_answer_only"] = len(tools) == 0
		}
		// 重问需要下一轮额度，最后一轮的空回答按原方式报错。
		allowEmpty := emptyRetries < a.config.EmptyAnswerRetries && round+1 < a.config.MaxIterations
		turn, err := readJournaledTurn(turnCtx, round, client, &llm.CompletionRequest{Messages: messages, MaxTokens: a.config.MaxTokens, Temperature: a.config.Temperature}, tools, allowEmpty, emit)
		cancel()
		if err != nil {
			return nil, err
		}
		if turn.Usage != nil {
			usage.Add(&interfaces.TokenUsage{PromptTokens: turn.Usage.PromptTokens, CompletionTokens: turn.Usage.CompletionTokens, TotalTokens: turn.Usage.TotalTokens})
			reports++
		}
		metadata["model_calls"] = round + 1
		metadata["usage_reports"] = reports
		metadata["usage_complete"] = reports == round+1
		if len(tools) == 0 && len(turn.ToolCalls) > 0 {
			if lastFailure != "" {
				return nil, fmt.Errorf("最终回答回合未提供工具，模型仍返回工具调用；%w", exhausted())
			}
			return nil, fmt.Errorf("最终回答回合未提供工具，模型仍返回工具调用")
		}
		if allowEmpty && isEmptyAnswer(turn) {
			// 不追加空 assistant 消息：部分提供方拒绝空内容。
			emptyRetries++
			metadata["empty_answer_retries"] = emptyRetries
			messages = append(messages, llm.UserMessage(emptyAnswerPrompt))
			continue
		}
		if len(turn.ToolCalls) == 0 {
			if turn.FinishReason == "tool_calls" || strings.TrimSpace(turn.Content) == "" {
				return nil, fmt.Errorf("模型未返回最终答案或完整工具调用")
			}
			metadata["answer_usage_reported"] = turn.Usage != nil
			return a.finishEvents(ctx, request, fmt.Sprint(request.Input), turn.Content, usage, metadata, emit)
		}
		direct := len(turn.ToolCalls) == 1 && slices.Contains(a.config.ReturnDirectTools, turn.ToolCalls[0].Function.Name)
		// 直接返回无需下一次模型请求；其余路径预留最终答案额度。
		if !direct && round+1 >= a.config.MaxIterations {
			return nil, exhausted()
		}
		calls := make([]ToolCall, len(turn.ToolCalls))
		ids := map[string]bool{}
		for i, tc := range turn.ToolCalls {
			if tc.ID == "" || ids[tc.ID] || tc.Function.Name == "" {
				return nil, fmt.Errorf("工具调用 ID 或名称无效")
			}
			known := false
			for _, offered := range tools {
				if offered.Name() == tc.Function.Name {
					known = true
					break
				}
			}
			if !known {
				return nil, fmt.Errorf("model requested unoffered tool %q", tc.Function.Name)
			}
			ids[tc.ID] = true
			calls[i] = ToolCall{Name: tc.Function.Name}
			if durable.HasJournal(ctx) {
				calls[i].ID = fmt.Sprintf("turn/%d/call/%s", round, tc.ID)
			}
			if err := json.Unmarshal([]byte(tc.Function.Arguments), &calls[i].Input); err != nil || calls[i].Input == nil {
				return nil, fmt.Errorf("工具 %s 的参数必须为 JSON 对象", tc.Function.Name)
			}
		}
		messages = append(messages, llm.Message{Role: "assistant", Content: turn.Content, ReasoningContent: turn.ReasoningContent, ToolCalls: turn.ToolCalls, ProviderState: turn.ProviderState})
		invoke := func(ctx context.Context, call ToolCall) (*interfaces.ToolOutput, error) {
			if call.Name == contextReadTool && a.config.ContextPolicy != nil && a.config.ContextPolicy.MaxToolResultRunes > 0 {
				return policy.Invoke(ctx, &interfaces.ToolInput{Args: call.Input})
			}
			if tool, ok := runtime.byName[call.Name]; ok {
				return tool.Invoke(ctx, &interfaces.ToolInput{Args: call.Input, CallID: call.ID})
			}
			return a.invokeToolCall(ctx, call)
		}
		// 并发时 tool_start 在派发前按序发出，tool_end 在全部结束后按序发出，事件顺序与执行时序无关；
		// Dispatcher 的结果回调同样推迟到全部结束后按序触发。
		parallel := a.concurrentCalls(calls, runtime)
		var results []toolCallResult
		if parallel {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			for _, call := range calls {
				if err := emit(AgentEvent{Type: "step", Step: "tool_start", Tool: call.Name}); err != nil {
					return nil, err
				}
			}
			results = invokeConcurrently(ctx, calls, a.config.ParallelToolCalls, invoke)
			if err := parallelFailure(results); err != nil {
				return nil, err
			}
		}
		for i, call := range calls {
			var output *interfaces.ToolOutput
			var err error
			if parallel {
				output, err = results[i].output, results[i].err
			} else {
				if err := ctx.Err(); err != nil {
					return nil, err
				}
				if err := emit(AgentEvent{Type: "step", Step: "tool_start", Tool: call.Name}); err != nil {
					return nil, err
				}
				output, err = invoke(ctx, call)
			}
			if waits := durable.Waits(err); waits != nil {
				return nil, &WaitingError{Waits: waits}
			}
			if err != nil {
				return nil, err
			}
			if output == nil {
				return nil, fmt.Errorf("tool returned nil output")
			}
			// 参数错误与业务拒绝按普通工具结果交给模型，其他失败不能成为答案。
			if direct && !output.Success {
				if !isArgumentErrorResult(output) && !isRejectedResult(output) {
					return nil, fmt.Errorf("直接返回工具 %s 未成功完成", call.Name)
				}
			}
			text, isText := output.Result.(string)
			if !isText {
				if text, err = toolResultText(output.Result); err != nil {
					return nil, err
				}
			}
			lastFailure = ""
			if !output.Success {
				lastFailure = text
				if message, ok := rejectedMessage(output); ok {
					lastFailure = message
				} else if m, ok := output.Result.(map[string]any); ok {
					if message, ok := m["message"].(string); ok {
						lastFailure = message
					}
				}
				if r := []rune(lastFailure); len(r) > maxFailureRunes {
					lastFailure = string(r[:maxFailureRunes]) + "…"
				}
			}
			// 字符串已是工具正文，不再包一层 JSON 引号或转义。
			messages = append(messages, llm.Message{Role: "tool", Content: text, ToolCallID: turn.ToolCalls[i].ID})
			if err := emit(AgentEvent{Type: "step", Step: "tool_end", Tool: call.Name}); err != nil {
				return nil, err
			}
			if direct && !output.Success {
				if round+1 >= a.config.MaxIterations {
					return nil, exhausted()
				}
				break
			}
			if direct {
				answer := text
				if output.Result == nil || (!isText && text == "null") || strings.TrimSpace(answer) == "" {
					return nil, fmt.Errorf("直接返回工具 %s 返回空答案", call.Name)
				}
				metadata["answer_source"] = "tool"
				metadata["return_direct_tool"] = call.Name
				metadata["answer_usage_reported"] = false
				if err := emit(AgentEvent{Type: "text", Delta: answer}); err != nil {
					return nil, err
				}
				return a.finishEvents(ctx, request, fmt.Sprint(request.Input), answer, usage, metadata, emit)
			}
		}
	}
	return nil, exhausted()
}

type toolCallResult struct {
	output *interfaces.ToolOutput
	err    error
}

// concurrentCalls 判断本轮调用能否并发：显式开启并发、多个调用且每个工具都声明支持并发调用。
// 上下文策略工具会读取本轮之前写入的状态，始终按顺序执行。
func (a *ConfigurableAgent[C, S]) concurrentCalls(calls []ToolCall, runtime *policyRuntime) bool {
	if len(calls) < 2 || a.config.ParallelToolCalls < 2 {
		return false
	}
	for _, call := range calls {
		if call.Name == contextReadTool && a.config.ContextPolicy != nil && a.config.ContextPolicy.MaxToolResultRunes > 0 {
			return false
		}
		if _, ok := runtime.byName[call.Name]; ok {
			return false
		}
		i := slices.IndexFunc(a.tools, func(t interfaces.Tool) bool { return t.Name() == call.Name })
		if i < 0 {
			return false
		}
		if c, ok := a.tools[i].(interfaces.ConcurrentCallTool); !ok || !c.SupportsConcurrentCalls() {
			return false
		}
	}
	return true
}

// invokeConcurrently 按下标收集结果，并在全部调用结束后返回。不取消兄弟调用：
// 持久工具被取消会记为失败，且组合工具只允许一次尝试，恢复时需要宿主处理。
// 全部结束后按调用顺序触发推迟的结果回调，回调顺序与执行先后无关，首次执行与恢复回放一致；
// 执行失败的调用没有回调，其余调用的回调照常触发，与它们已写入的日志保持一致。
func invokeConcurrently(ctx context.Context, calls []ToolCall, limit int, invoke func(context.Context, ToolCall) (*interfaces.ToolOutput, error)) []toolCallResult {
	if limit > len(calls) {
		limit = len(calls)
	}
	results := make([]toolCallResult, len(calls))
	delivers := make([]func() error, len(calls))
	slots := make(chan struct{}, limit)
	var wg sync.WaitGroup
	for i, call := range calls {
		callCtx, deliver := tools.DeferResults(ctx)
		delivers[i] = deliver
		wg.Add(1)
		slots <- struct{}{}
		go func() {
			defer wg.Done()
			defer func() { <-slots }()
			defer recoverPanic(fmt.Sprintf("工具 %q", call.Name), &results[i].err)
			output, err := invoke(callCtx, call)
			results[i] = toolCallResult{output: output, err: err}
		}()
	}
	wg.Wait()
	for i, deliver := range delivers {
		if results[i].err != nil {
			continue
		}
		if err := deliver(); err != nil {
			results[i] = toolCallResult{err: err}
		}
	}
	return results
}

// isArgumentErrorResult 位于包级，避免被循环内的 tools 变量遮蔽。
func isArgumentErrorResult(out *interfaces.ToolOutput) bool {
	_, ok := tools.ArgumentErrorResult(out)
	return ok
}

func isRejectedResult(out *interfaces.ToolOutput) bool {
	_, ok := tools.RejectedResult(out)
	return ok
}

// rejectedMessage 同样位于包级，避免被循环内的 tools 变量遮蔽。
func rejectedMessage(out *interfaces.ToolOutput) (string, bool) { return tools.RejectedResult(out) }

// toolResultText 不做 HTML 转义，模型与直接答案看到的是原始 <>&。
func toolResultText(result any) (string, error) {
	var buf bytes.Buffer
	encoder := json.NewEncoder(&buf)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(result); err != nil {
		return "", err
	}
	return strings.TrimSuffix(buf.String(), "\n"), nil
}

func readToolTurn(ctx context.Context, stream <-chan *llm.StreamChunk, emit func(AgentEvent) error) (*llm.StreamChunk, error) {
	if stream == nil {
		return nil, fmt.Errorf("模型返回空流")
	}
	var content strings.Builder
	var result llm.StreamChunk
	partial := func() *llm.StreamChunk {
		copy := result
		if content.Len() > 0 {
			copy.Content = content.String()
		}
		return &copy
	}
	for {
		select {
		case <-ctx.Done():
			return partial(), ctx.Err()
		case chunk, ok := <-stream:
			if !ok {
				return partial(), io.ErrUnexpectedEOF
			}
			if chunk == nil {
				continue
			}
			usage := result.Usage
			result = *chunk
			if chunk.Usage != nil {
				copy := *chunk.Usage
				usage = &copy
			}
			result.Usage = usage
			if chunk.Delta != "" {
				content.WriteString(chunk.Delta)
				if err := emit(AgentEvent{Type: "text", Delta: chunk.Delta}); err != nil {
					return partial(), err
				}
			}
			if chunk.Error != nil {
				return partial(), chunk.Error
			}
			if chunk.Done {
				if chunk.FinishReason == "length" || chunk.FinishReason == "content_filter" {
					return partial(), fmt.Errorf("模型输出未完整结束：%s", chunk.FinishReason)
				}
				return partial(), nil
			}
		}
	}
}
