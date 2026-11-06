package builder

import (
	"context"
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"
	"sync"

	"github.com/kart-io/goagent/codec/json"

	"github.com/kart-io/goagent/core/budget"
	"github.com/kart-io/goagent/core/durable"
	"github.com/kart-io/goagent/core/middleware"
	"github.com/kart-io/goagent/core/usage"
	agentErrors "github.com/kart-io/goagent/errors"
	"github.com/kart-io/goagent/interfaces"
	"github.com/kart-io/goagent/llm"
	"github.com/kart-io/goagent/tools"
	"github.com/kart-io/goagent/tools/toolargs"
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
func (a *ConfigurableAgent[C, S]) streamMessages(ctx context.Context, input, evidence string) ([]llm.Message, error) {
	system := a.buildSystemPromptContent()
	if a.retriever != nil {
		system += "\n" + retrievalInstruction
	}
	messages := []llm.Message{}
	if system != "" {
		messages = append(messages, llm.SystemMessage(system))
	}
	// Handle 的 Followup 轮次续接上一轮的对话，不再读取记忆。
	if history, ok := ctx.Value(historyKey{}).([]llm.Message); ok {
		messages = append(messages, history...)
	} else if a.memoryManager != nil && a.config.SessionID != "" {
		history, err := readMemory(ctx, a.memoryManager, a.config)
		if err != nil {
			return nil, err
		}
		for _, entry := range history {
			messages = append(messages, llm.Message{Role: entry.Role, Content: entry.Content})
		}
	}
	if evidence != "" {
		input = evidence + "\n用户请求：" + input
	}
	return append(messages, llm.UserMessage(input)), nil
}

// runToolEvents 每轮直接选择工具或输出答案，MaxIterations 包含最终答案请求。
func (a *ConfigurableAgent[C, S]) runToolEvents(ctx context.Context, request *middleware.MiddlewareRequest, input string, client llm.ToolStreamClient, messages []llm.Message, metadata map[string]interface{}, emit func(AgentEvent) error) (response *middleware.MiddlewareResponse, err error) {
	// 会话级审批授权按会话隔离；宿主已声明会话时沿用宿主的值。
	if tools.SessionIDFrom(ctx) == "" && a.config.SessionID != "" {
		ctx = tools.WithSessionID(ctx, a.config.SessionID)
	}
	ctx, release := a.offloadScope(ctx)
	if release != nil {
		defer func() {
			if e := release(ctx); e != nil {
				response, err = nil, errors.Join(err, e)
			}
		}()
	}
	usage := &interfaces.TokenUsage{}
	reports := 0
	box := inboxFrom(ctx)
	if len(a.config.PromptSections) > 0 {
		sections, err := journaled(ctx, "loop/sections", func(context.Context) (string, error) {
			return a.sectionText(PromptScope{SessionID: tools.SessionIDFrom(ctx)}), nil
		})
		if err != nil {
			return nil, err
		}
		messages = withSections(messages, sections)
	}
	slog := sessionTurnFrom(ctx)
	if err := slog.begin(ctx, messages, a.tools); err != nil {
		return nil, fmt.Errorf("会话日志: %w", err)
	}
	// finish 结束本轮：记下对话供 Handle 的 Followup 续接。
	finish := func(answer string, final llm.Message) (*middleware.MiddlewareResponse, error) {
		if err := slog.end(ctx, append(slices.Clone(messages), final), answer, usage); err != nil {
			return nil, fmt.Errorf("会话日志: %w", err)
		}
		if box != nil {
			transcript := slices.Clone(messages)
			if len(transcript) > 0 && transcript[0].Role == "system" {
				transcript = transcript[1:]
			}
			box.mu.Lock()
			box.transcript = append(transcript, final)
			box.mu.Unlock()
		}
		return a.finishEvents(ctx, request, input, answer, usage, metadata, emit)
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
	// inputs 是尚未交给 PreStep 的用户输入条数，起始为轮次输入。
	inputs := 1
	for round := 0; round < a.config.MaxIterations; round++ {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if box != nil {
			items, err := journaled(ctx, fmt.Sprintf("loop/inbox/%d", round), func(context.Context) ([]inboxItem, error) { return box.drain(), nil })
			if err != nil {
				return nil, err
			}
			var seqs []uint64
			for _, item := range items {
				messages = append(messages, llm.UserMessage(item.Text))
				if item.Seq != 0 {
					seqs = append(seqs, item.Seq)
				}
			}
			inputs += len(items)
			// 领取记录落盘后才确认；回放时同样确认，崩溃在两者之间的输入不会被再次领取。
			if err := box.ack(seqs); err != nil {
				return nil, fmt.Errorf("确认收件箱输入: %w", err)
			}
		}
		if err := emit(AgentEvent{Type: "step", Step: "model_start"}); err != nil {
			return nil, err
		}
		tools := a.tools
		// 已有结果的最后一轮只能回答或使用显式配置的终态工具。
		// 首轮及尚有更多额度时保留完整工具集，支持连续查询。
		final := round > 0 && round+1 == a.config.MaxIterations
		if final {
			tools = nil
			for _, tool := range a.tools {
				if slices.Contains(a.config.ReturnDirectTools, tool.Name()) {
					tools = append(tools, tool)
				}
			}
			metadata["final_answer_only"] = len(tools) == 0
		}
		var pre PreStepResult
		if messages, pre, err = a.runPreStep(ctx, round, inputs, final, messages); err != nil {
			return nil, err
		}
		a.trackOffloads(ctx, messages)
		inputs = 0
		if pre.Halt {
			metadata["answer_source"] = "pre_step"
			return finish(pre.Answer, llm.Message{Role: "assistant", Content: pre.Answer})
		}
		if err := slog.sync(ctx, messages); err != nil {
			return nil, fmt.Errorf("会话日志: %w", err)
		}
		// 重问需要下一轮额度，最后一轮的空回答按原方式报错。
		allowEmpty := emptyRetries < a.config.EmptyAnswerRetries && round+1 < a.config.MaxIterations
		turn, redirected, recovered, err := a.requestTurn(ctx, box, round, client, messages, tools, final, allowEmpty, emit)
		if err != nil {
			return nil, err
		}
		messages = recovered
		if redirected != nil {
			// 保留已流出的文本，追加纠正后重新请求；重新请求占用一次额度。
			if redirected.partial != "" {
				messages = append(messages, llm.Message{Role: "assistant", Content: redirected.partial})
			}
			for _, msg := range redirected.messages {
				messages = append(messages, llm.UserMessage(msg))
			}
			inputs += len(redirected.messages)
			if round+1 >= a.config.MaxIterations {
				return nil, exhausted()
			}
			continue
		}
		if turn.Usage != nil {
			copy := turn.Usage.Tokens()
			usage.Add(&copy)
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
			final := llm.Message{Role: "assistant", Content: turn.Content, ReasoningContent: turn.ReasoningContent, ProviderState: turn.ProviderState}
			// 收件箱中的 Steer 唤醒续跑，下一步领取；否则询问 TurnStopping。TurnStopping 续跑后至少还要有一步带工具，
			// 模型才能按反馈修改；额度不足时不询问，按原答复结束并标记 turn_stopping_skipped。
			wake := false
			if box != nil && round+1 < a.config.MaxIterations {
				if wake, err = journaled(ctx, fmt.Sprintf("loop/wake/%d", round), func(context.Context) (bool, error) { return box.wakes(), nil }); err != nil {
					return nil, err
				}
			}
			next := ""
			if !wake && len(a.config.TurnStopping) > 0 {
				if round+2 >= a.config.MaxIterations {
					metadata["turn_stopping_skipped"] = true
				} else if next, err = a.runTurnStopping(ctx, &Stopping{Step: round, Answer: turn.Content,
					Continued: metadataInt(metadata, "continued_turns"), Blocked: metadataInt(metadata, "stopping_continued")}); err != nil {
					return nil, err
				}
			}
			if wake || next != "" {
				messages = append(messages, final)
				if next != "" {
					messages = append(messages, llm.UserMessage(next))
					metadata["stopping_continued"] = metadataInt(metadata, "stopping_continued") + 1
				}
				metadata["continued_turns"] = metadataInt(metadata, "continued_turns") + 1
				continue
			}
			return finish(turn.Content, final)
		}
		direct := len(turn.ToolCalls) == 1 && slices.Contains(a.config.ReturnDirectTools, turn.ToolCalls[0].Function.Name)
		// 直接返回无需下一次模型请求；其余路径预留最终答案额度。
		if !direct && round+1 >= a.config.MaxIterations {
			return nil, exhausted()
		}
		calls := make([]ToolCall, len(turn.ToolCalls))
		ids := map[string]bool{}
		// invalid 记录交还模型的无效调用，这些调用不执行。
		invalid := map[int]string{}
		for i, tc := range turn.ToolCalls {
			if tc.ID == "" || ids[tc.ID] || tc.Function.Name == "" {
				return nil, fmt.Errorf("工具调用 ID 或名称无效")
			}
			ids[tc.ID] = true
			calls[i] = ToolCall{Name: tc.Function.Name}
			if !slices.ContainsFunc(tools, func(t interfaces.Tool) bool { return t.Name() == tc.Function.Name }) {
				if !a.config.ToolErrorFeedback {
					return nil, fmt.Errorf("model requested unoffered tool %q", tc.Function.Name)
				}
				invalid[i] = unofferedToolMessage(tc.Function.Name, tools)
				continue
			}
			if durable.HasJournal(ctx) {
				calls[i].ID = fmt.Sprintf("turn/%d/call/%s", round, tc.ID)
			}
			// 从解析起只有一份参数：校验、授权、审批与执行看到同一个值，大整数不经 float64 舍入。
			args, ok := toolargs.Parse(tc.Function.Arguments)
			calls[i].Input = args
			if !ok {
				if !a.config.ToolErrorFeedback {
					return nil, fmt.Errorf("工具 %s 的参数必须为 JSON 对象", tc.Function.Name)
				}
				invalid[i] = fmt.Sprintf("工具 %s 的参数必须为 JSON 对象，请修正后重新调用", tc.Function.Name)
			}
		}
		messages = append(messages, llm.Message{Role: "assistant", Content: turn.Content, ReasoningContent: turn.ReasoningContent, ToolCalls: turn.ToolCalls, ProviderState: turn.ProviderState})
		invoke := func(ctx context.Context, call ToolCall) (*interfaces.ToolOutput, error) {
			return a.invokeToolCall(ctx, call)
		}
		// 并发时 tool_start 在派发前按序发出，tool_end 在全部结束后按序发出，事件顺序与执行时序无关；
		// Dispatcher 的结果回调同样推迟到全部结束后按序触发。
		parallel := len(invalid) == 0 && a.concurrentCalls(calls)
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
			for i := range results {
				if out, ok := a.errorFeedback(ctx, results[i].err); ok {
					results[i] = toolCallResult{output: out}
				}
			}
			if err := parallelFailure(results); err != nil {
				return nil, err
			}
		}
		var additional []string
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
				if message, bad := invalid[i]; bad {
					output = rejectedOutput(message)
				} else if output, err = invoke(ctx, call); err != nil {
					if out, ok := a.errorFeedback(ctx, err); ok {
						output, err = out, nil
					}
				}
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
			slog.toolResult(turn.ToolCalls[i].ID, call.Name, output.Success)
			additional = append(additional, output.AdditionalContext...)
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
				return finish(answer, llm.Message{Role: "assistant", Content: answer})
			}
		}
		// 调用后钩子追加的上下文在本批工具结果之后作为一条 user 消息注入；assistant(tool_calls)→tool→user 在各 provider 上都合法。
		if len(additional) > 0 {
			messages = append(messages, llm.Message{Role: "user", Content: additionalContextMessage(additional)})
		}
	}
	return nil, exhausted()
}

type toolCallResult struct {
	output *interfaces.ToolOutput
	err    error
}

// concurrentCalls 判断本轮调用能否并发：显式开启并发、多个调用且每个工具都声明支持并发调用。
func (a *ConfigurableAgent[C, S]) concurrentCalls(calls []ToolCall) bool {
	if len(calls) < 2 || a.config.ParallelToolCalls < 2 {
		return false
	}
	for _, call := range calls {
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
// errorFeedback 在开启 ToolErrorFeedback 时把可恢复的工具 error 转为交给模型的结果。
// 持久运行不转换：失败结果不入日志，恢复时工具会重新执行，模型看到的历史将与日志不一致。
func (a *ConfigurableAgent[C, S]) errorFeedback(ctx context.Context, err error) (*interfaces.ToolOutput, bool) {
	if err == nil || !a.config.ToolErrorFeedback || durable.HasJournal(ctx) || ctx.Err() != nil {
		return nil, false
	}
	var panicErr *agentErrors.PanicError
	if durable.Waits(err) != nil || errors.Is(err, budget.ErrBudgetExceeded) || errors.As(err, &panicErr) {
		return nil, false
	}
	return tools.Rejected("工具执行失败："+err.Error(), nil), true
}

// unofferedToolMessage 告诉模型本轮可用的工具，便于它改用正确的名称。
func unofferedToolMessage(name string, offered []interfaces.Tool) string {
	names := make([]string, len(offered))
	for i, t := range offered {
		names[i] = t.Name()
	}
	if len(names) == 0 {
		return fmt.Sprintf("工具 %q 不存在，本轮不能调用工具，请直接回答", name)
	}
	return fmt.Sprintf("工具 %q 不存在，可用工具：%s", name, strings.Join(names, ", "))
}

func isArgumentErrorResult(out *interfaces.ToolOutput) bool {
	_, ok := tools.ArgumentErrorResult(out)
	return ok
}

func isRejectedResult(out *interfaces.ToolOutput) bool {
	_, ok := tools.RejectedResult(out)
	return ok
}

func rejectedOutput(message string) *interfaces.ToolOutput { return tools.Rejected(message, nil) }

// rejectedMessage 同样位于包级，避免被循环内的 tools 变量遮蔽。
func rejectedMessage(out *interfaces.ToolOutput) (string, bool) { return tools.RejectedResult(out) }

// toolResultText 不做 HTML 转义，模型与直接答案看到的是原始 <>&；键按序输出，回放时文本一致。
// 类型化 nil 的 map、切片编码为 null（v2 默认为 {}、[]），据此识别空结果。
func toolResultText(result any) (string, error) {
	b, err := json.Marshal(result, json.NoEscapeHTML)
	return string(b), err
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
				// ctx 结束后提供方可以不发终态直接关闭流，这时报告 ctx 的错误而不是连接中断。
				if err := ctx.Err(); err != nil {
					return partial(), err
				}
				return partial(), io.ErrUnexpectedEOF
			}
			if chunk == nil {
				continue
			}
			usage := result.Usage
			responseModel := result.ResponseModel
			result = *chunk
			if result.ResponseModel == "" {
				result.ResponseModel = responseModel
			}
			if chunk.Usage != nil {
				usage = mergeTurnUsage(usage, chunk.Usage)
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

// Usage frames are cumulative evidence, not deltas. Later known fields replace
// prior values; missing fields must not erase already received evidence.
func mergeTurnUsage(previous, next *llm.Usage) *llm.Usage {
	copy := *next
	fields := next.Fields()
	if previous != nil {
		if fields&usage.Prompt == 0 {
			copy.PromptTokens = previous.PromptTokens
		}
		if fields&usage.Completion == 0 {
			copy.CompletionTokens = previous.CompletionTokens
		}
		if fields&usage.Total == 0 {
			copy.TotalTokens = previous.TotalTokens
		}
		if fields&usage.Cached == 0 {
			copy.CachedTokens = previous.CachedTokens
		}
		if fields&usage.Reasoning == 0 {
			copy.ReasoningTokens = previous.ReasoningTokens
		}
		if fields&usage.ToolUsePrompt == 0 {
			copy.ToolUsePromptTokens = previous.ToolUsePromptTokens
		}
		fields |= previous.Fields()
	}
	if next.KnownFields != nil || (previous != nil && previous.KnownFields != nil) || fields != usage.BasicFields {
		copy.KnownFields = &fields
	}
	return &copy
}

func additionalContextMessage(items []string) string {
	var sb strings.Builder
	sb.WriteString("[工具附加上下文]")
	for _, item := range items {
		sb.WriteString("\n- ")
		sb.WriteString(item)
	}
	return sb.String()
}
