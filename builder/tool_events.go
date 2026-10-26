package builder

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"slices"
	"strings"

	"github.com/kart-io/goagent/core/middleware"
	"github.com/kart-io/goagent/interfaces"
	"github.com/kart-io/goagent/llm"
)

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
func (a *ConfigurableAgent[C, S]) runToolEvents(ctx context.Context, request *middleware.MiddlewareRequest, client llm.ToolStreamClient, messages []llm.Message, metadata map[string]interface{}, emit func(AgentEvent) error) (*middleware.MiddlewareResponse, error) {
	usage := &interfaces.TokenUsage{}
	reports := 0
	for round := 0; round < a.config.MaxIterations; round++ {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if err := emit(AgentEvent{Type: "step", Step: "model_start"}); err != nil {
			return nil, err
		}
		turnCtx, cancel := context.WithCancel(ctx)
		tools := a.tools
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
		stream, err := client.CompleteToolsStream(turnCtx, &llm.CompletionRequest{Messages: messages, MaxTokens: a.config.MaxTokens, Temperature: a.config.Temperature}, tools)
		if err != nil {
			cancel()
			return nil, err
		}
		turn, err := readToolTurn(turnCtx, stream, emit)
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
			return nil, fmt.Errorf("最终回答回合未提供工具，模型仍返回工具调用")
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
			return nil, fmt.Errorf("模型调用达到 MaxIterations 上限，尚未生成答案")
		}
		calls := make([]ToolCall, len(turn.ToolCalls))
		ids := map[string]bool{}
		for i, tc := range turn.ToolCalls {
			if tc.ID == "" || ids[tc.ID] || tc.Function.Name == "" {
				return nil, fmt.Errorf("工具调用 ID 或名称无效")
			}
			ids[tc.ID] = true
			calls[i] = ToolCall{Name: tc.Function.Name}
			if err := json.Unmarshal([]byte(tc.Function.Arguments), &calls[i].Input); err != nil || calls[i].Input == nil {
				return nil, fmt.Errorf("工具 %s 的参数必须为 JSON 对象", tc.Function.Name)
			}
		}
		messages = append(messages, llm.Message{Role: "assistant", Content: turn.Content, ReasoningContent: turn.ReasoningContent, ToolCalls: turn.ToolCalls})
		for i, call := range calls {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			if err := emit(AgentEvent{Type: "step", Step: "tool_start", Tool: call.Name}); err != nil {
				return nil, err
			}
			output, err := a.invokeToolCall(ctx, call)
			if err != nil {
				return nil, err
			}
			if direct && !output.Success {
				return nil, fmt.Errorf("直接返回工具 %s 未成功完成", call.Name)
			}
			text, isText := output.Result.(string)
			if !isText {
				data, err := json.Marshal(output.Result)
				if err != nil {
					return nil, err
				}
				text = string(data)
			}
			// 字符串已是工具正文，不再包一层 JSON 引号或转义。
			messages = append(messages, llm.Message{Role: "tool", Content: text, ToolCallID: turn.ToolCalls[i].ID})
			if err := emit(AgentEvent{Type: "step", Step: "tool_end", Tool: call.Name}); err != nil {
				return nil, err
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
	return nil, fmt.Errorf("模型调用达到 MaxIterations 上限，尚未生成答案")
}

func readToolTurn(ctx context.Context, stream <-chan *llm.StreamChunk, emit func(AgentEvent) error) (*llm.StreamChunk, error) {
	if stream == nil {
		return nil, fmt.Errorf("模型返回空流")
	}
	var content strings.Builder
	var usage *llm.Usage
	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case chunk, ok := <-stream:
			if !ok {
				return nil, io.ErrUnexpectedEOF
			}
			if chunk == nil {
				continue
			}
			if chunk.Error != nil {
				return nil, chunk.Error
			}
			if chunk.Delta != "" {
				content.WriteString(chunk.Delta)
				if err := emit(AgentEvent{Type: "text", Delta: chunk.Delta}); err != nil {
					return nil, err
				}
			}
			if chunk.Usage != nil {
				copy := *chunk.Usage
				usage = &copy
			}
			if chunk.Done {
				if chunk.FinishReason == "length" || chunk.FinishReason == "content_filter" {
					return nil, fmt.Errorf("模型输出未完整结束：%s", chunk.FinishReason)
				}
				result := *chunk
				result.Content = content.String()
				result.Usage = usage
				return &result, nil
			}
		}
	}
}
