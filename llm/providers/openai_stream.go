package providers

import (
	"context"
	"errors"
	"io"
	"strings"
	"time"

	"github.com/kart-io/goagent/codec/json"

	"github.com/sashabaranov/go-openai"

	agentErrors "github.com/kart-io/goagent/errors"
	"github.com/kart-io/goagent/interfaces"
	agentllm "github.com/kart-io/goagent/llm"
)

var _ agentllm.ToolStreamClient = (*OpenAIProvider)(nil)

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
	effort, err := p.reasoningEffort(req)
	if err != nil {
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
	temperature := p.ResolveTemperature(req)
	ctx = withOpenAITemperature(ctx, temperature)
	ctx, retryAfter := withRetryAfterSlot(ctx)
	watch := newStreamWatchdog(ctx, p.Config.StreamIdleTimeout, p.Config.StreamTimeout)
	stream, err := p.client.CreateChatCompletionStream(watch.ctx, openai.ChatCompletionRequest{
		ReasoningEffort: effort, Model: model, Messages: messages, Tools: p.convertTools(tools), MaxTokens: p.GetMaxTokens(req.MaxTokens),
		Temperature: float32(temperature), TopP: float32(req.TopP),
		Stop: req.Stop, Stream: true, StreamOptions: &openai.StreamOptions{IncludeUsage: true},
	})
	if err != nil {
		if stall := watch.stalled(); stall != nil {
			err = stall
		}
		watch.stop()
		return nil, wrapProviderError(p.ProviderName(), model, openAIProviderError(err, *retryAfter)).WithContext("stream", true)
	}
	out := make(chan *agentllm.StreamChunk)
	go func() {
		defer close(out)
		defer watch.stop()
		defer func() { _ = stream.Close() }()
		var content strings.Builder
		var reasoning strings.Builder
		var usage *agentllm.Usage
		var finishReason string
		var responseModel string
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
		fail := func(err error) {
			send(&agentllm.StreamChunk{Done: true, Error: err, Usage: usage, ResponseModel: responseModel,
				Content: content.String(), ReasoningContent: reasoning.String(), FinishReason: finishReason})
		}
		for {
			response, err := stream.Recv()
			if err != nil {
				terminal := &agentllm.StreamChunk{ResponseModel: responseModel, Content: content.String(), ReasoningContent: reasoning.String(), FinishReason: finishReason, Usage: usage, Done: true}
				// 正常结束恰好碰上时限时仍按正常结束处理。
				if stall := watch.stalled(); stall != nil && (!errors.Is(err, io.EOF) || finishReason == "") {
					terminal.Error = wrapProviderError(p.ProviderName(), model, openAIProviderError(stall, 0))
				} else if !errors.Is(err, io.EOF) {
					terminal.Error = wrapProviderError(p.ProviderName(), model, openAIProviderError(err, 0))
				} else if finishReason == "" {
					// 连接在结束帧前关闭：包成 ProviderError，ClassifyError 才能判为可重试。
					terminal.Error = wrapProviderError(p.ProviderName(), model, openAIProviderError(io.ErrUnexpectedEOF, 0))
				} else {
					for _, index := range order {
						call := calls[index]
						if call.ID == "" || call.Function.Name == "" {
							terminal.Error = agentErrors.Newf(agentErrors.CodeLLMResponse, "工具调用缺少 ID 或名称").
								WithComponent("openai").
								WithOperation("parse_response")
							terminal.ToolCalls = nil
							break
						}
						terminal.ToolCalls = append(terminal.ToolCalls, *call)
					}
				}
				send(terminal)
				return
			}
			watch.touch()
			if response.Model != "" {
				responseModel = response.Model
			}
			if response.Usage != nil {
				usage = mergeSDKUsage(usage, openAIUsage(response.Usage))
			}

			if len(response.Choices) == 0 {
				continue
			}
			choice := response.Choices[0]
			reasoning.WriteString(choice.Delta.ReasoningContent)
			if choice.Delta.FunctionCall != nil {
				fail(agentErrors.Newf(agentErrors.CodeLLMResponse, "原生工具流需要 tool_calls 响应，服务返回了旧 function_call 协议").
					WithComponent("openai").
					WithOperation("parse_response"))
				return
			}
			if len(choice.Delta.ToolCalls) > 0 {
				if completeBatchSeen {
					fail(agentErrors.Newf(agentErrors.CodeLLMResponse, "完整工具调用结束帧后仍收到工具分片").WithComponent("openai").WithOperation("parse_response"))
					return
				}
				completeBatch, err := indexCompleteToolBatch(choice.Delta.ToolCalls, string(choice.FinishReason), len(calls) > 0)
				if err != nil {
					fail(err)
					return
				}
				completeBatchSeen = completeBatch
			}
			for _, delta := range choice.Delta.ToolCalls {
				if delta.Index == nil || *delta.Index < 0 {
					fail(agentErrors.Newf(agentErrors.CodeLLMResponse, "工具分片缺少有效索引").WithComponent("openai").WithOperation("parse_response"))
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
		return false, agentErrors.Newf(agentErrors.CodeLLMResponse, "缺少索引的工具调用必须为独立、完整的结束帧").
			WithComponent("openai").
			WithOperation("parse_response")
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
