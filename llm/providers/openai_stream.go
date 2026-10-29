package providers

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/sashabaranov/go-openai"

	agentErrors "github.com/kart-io/goagent/errors"
	"github.com/kart-io/goagent/interfaces"
	agentllm "github.com/kart-io/goagent/llm"
	"github.com/kart-io/goagent/utils/json"
)

var _ agentllm.ToolStreamClient = (*OpenAIProvider)(nil)

// ChatStream streams a conversation, preserving deltas, usage, and errors.
func (p *OpenAIProvider) ChatStream(ctx context.Context, messages []agentllm.Message) (<-chan *agentllm.StreamChunk, error) {
	return p.CompleteStream(ctx, &agentllm.CompletionRequest{Messages: messages})
}

// CompleteStream implements the structured streaming contract. FinishReason
// does not terminate the stream: a subsequent usage-only frame may still arrive.
// Callers must cancel ctx when they stop consuming the returned channel.
func (p *OpenAIProvider) CompleteStream(ctx context.Context, req *agentllm.CompletionRequest) (<-chan *agentllm.StreamChunk, error) {
	return p.completeStream(ctx, req, nil)
}

// CompleteToolsStream 同时提供原生工具选择与答案增量，无需 READY 确认轮。
func (p *OpenAIProvider) CompleteToolsStream(ctx context.Context, req *agentllm.CompletionRequest, tools []interfaces.Tool) (<-chan *agentllm.StreamChunk, error) {
	return p.completeStream(ctx, req, tools)
}

func (p *OpenAIProvider) completeStream(ctx context.Context, req *agentllm.CompletionRequest, tools []interfaces.Tool) (<-chan *agentllm.StreamChunk, error) {
	if req == nil {
		return nil, agentErrors.New(agentErrors.CodeInvalidInput, "completion request is required")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	messages := make([]openai.ChatCompletionMessage, 0, len(req.Messages)+1)
	if p.Config.SystemPrompt != "" {
		messages = append(messages, openai.ChatCompletionMessage{Role: openai.ChatMessageRoleSystem, Content: p.Config.SystemPrompt})
	}
	for _, msg := range req.Messages {
		message := openai.ChatCompletionMessage{Role: msg.Role, Content: msg.Content, Name: msg.Name, ToolCallID: msg.ToolCallID, ReasoningContent: msg.ReasoningContent}
		for _, call := range msg.ToolCalls {
			message.ToolCalls = append(message.ToolCalls, openai.ToolCall{ID: call.ID, Type: openai.ToolTypeFunction, Function: openai.FunctionCall{Name: call.Function.Name, Arguments: call.Function.Arguments}})
		}
		messages = append(messages, message)
	}
	model := p.GetModel(req.Model)
	ctx, retryAfter := withRetryAfterSlot(ctx)
	stream, err := p.client.CreateChatCompletionStream(ctx, openai.ChatCompletionRequest{
		Model: model, Messages: messages, Tools: p.convertTools(tools), MaxTokens: p.GetMaxTokens(req.MaxTokens),
		Temperature: float32(p.GetTemperature(req.Temperature)), TopP: float32(req.TopP),
		Stop: req.Stop, Stream: true, StreamOptions: &openai.StreamOptions{IncludeUsage: true},
	})
	if err != nil {
		return nil, agentErrors.NewLLMRequestError(p.ProviderName(), model, openAIProviderError(err, *retryAfter)).WithContext("stream", true)
	}
	out := make(chan *agentllm.StreamChunk)
	go func() {
		defer close(out)
		defer func() { _ = stream.Close() }()
		var content strings.Builder
		var reasoning strings.Builder
		var usage *agentllm.Usage
		var finishReason string
		calls := map[int]*agentllm.ToolCall{}
		completeBatchSeen := false
		order := []int{}
		index := 0
		send := func(chunk *agentllm.StreamChunk) bool {
			chunk.Index = index
			index++
			chunk.Timestamp = time.Now()
			select {
			case out <- chunk:
				return true
			case <-ctx.Done():
				return false
			}
		}
		for {
			response, err := stream.Recv()
			if err != nil {
				terminal := &agentllm.StreamChunk{Content: content.String(), ReasoningContent: reasoning.String(), FinishReason: finishReason, Usage: usage, Done: true}
				if !errors.Is(err, io.EOF) {
					terminal.Error = agentErrors.NewLLMRequestError(p.ProviderName(), model, openAIProviderError(err, 0))
				} else if finishReason == "" {
					terminal.Error = io.ErrUnexpectedEOF
				} else {
					for _, index := range order {
						call := calls[index]
						if call.ID == "" || call.Function.Name == "" {
							terminal.Error = fmt.Errorf("工具调用缺少 ID 或名称")
							terminal.ToolCalls = nil
							break
						}
						terminal.ToolCalls = append(terminal.ToolCalls, *call)
					}
				}
				send(terminal)
				return
			}
			if response.Usage != nil {
				usage = &agentllm.Usage{
					PromptTokens:     response.Usage.PromptTokens,
					CompletionTokens: response.Usage.CompletionTokens,
					TotalTokens:      response.Usage.TotalTokens,
				}
			}
			if len(response.Choices) == 0 {
				continue
			}
			choice := response.Choices[0]
			reasoning.WriteString(choice.Delta.ReasoningContent)
			if choice.Delta.FunctionCall != nil {
				send(&agentllm.StreamChunk{Done: true, Error: fmt.Errorf("原生工具流需要 tool_calls 响应，服务返回了旧 function_call 协议")})
				return
			}
			if len(choice.Delta.ToolCalls) > 0 {
				if completeBatchSeen {
					send(&agentllm.StreamChunk{Done: true, Error: fmt.Errorf("完整工具调用结束帧后仍收到工具分片")})
					return
				}
				completeBatch, err := indexCompleteToolBatch(choice.Delta.ToolCalls, string(choice.FinishReason), len(calls) > 0)
				if err != nil {
					send(&agentllm.StreamChunk{Done: true, Error: err})
					return
				}
				completeBatchSeen = completeBatch
			}
			for _, delta := range choice.Delta.ToolCalls {
				if delta.Index == nil || *delta.Index < 0 {
					send(&agentllm.StreamChunk{Done: true, Error: fmt.Errorf("工具分片缺少有效索引")})
					return
				}
				i := *delta.Index
				call := calls[i]
				if call == nil {
					call = &agentllm.ToolCall{Type: "function"}
					calls[i] = call
					order = append(order, i)
				}
				if delta.ID != "" {
					call.ID = delta.ID
				}
				call.Function.Name += delta.Function.Name
				call.Function.Arguments += delta.Function.Arguments
			}
			content.WriteString(choice.Delta.Content)
			if choice.FinishReason != "" {
				finishReason = string(choice.FinishReason)
			}
			if !send(&agentllm.StreamChunk{
				Delta: choice.Delta.Content, Content: content.String(),
				Role: choice.Delta.Role, FinishReason: string(choice.FinishReason),
			}) {
				return
			}
		}
	}()
	return out, nil
}

// Google may send complete tool calls without indices in a single terminal frame.
// Only that unambiguous form is accepted; fragmented/mixed batches still fail.
func indexCompleteToolBatch(calls []openai.ToolCall, finish string, hasPrevious bool) (bool, error) {
	missing := false
	for _, call := range calls {
		missing = missing || call.Index == nil
	}
	if !missing {
		return false, nil
	}
	invalid := func() (bool, error) {
		return false, fmt.Errorf("缺少索引的工具调用必须为独立、完整的结束帧")
	}
	if hasPrevious || finish != "tool_calls" {
		return invalid()
	}
	ids := make(map[string]bool, len(calls))
	for _, call := range calls {
		var args map[string]json.RawMessage
		if call.Index != nil || call.ID == "" || ids[call.ID] || call.Type != "function" || call.Function.Name == "" || json.Unmarshal([]byte(call.Function.Arguments), &args) != nil || args == nil {
			return invalid()
		}
		ids[call.ID] = true
	}
	for i := range calls {
		index := i
		calls[i].Index = &index
	}
	return true, nil
}
