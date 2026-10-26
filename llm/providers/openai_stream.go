package providers

import (
	"context"
	"errors"
	"io"
	"strings"
	"time"

	"github.com/sashabaranov/go-openai"

	agentErrors "github.com/kart-io/goagent/errors"
	agentllm "github.com/kart-io/goagent/llm"
)

var _ agentllm.StreamClient = (*OpenAIProvider)(nil)

// ChatStream streams a conversation, preserving deltas, usage, and errors.
func (p *OpenAIProvider) ChatStream(ctx context.Context, messages []agentllm.Message) (<-chan *agentllm.StreamChunk, error) {
	return p.CompleteStream(ctx, &agentllm.CompletionRequest{Messages: messages})
}

// CompleteStream implements the structured streaming contract. FinishReason
// does not terminate the stream: a subsequent usage-only frame may still arrive.
// Callers must cancel ctx when they stop consuming the returned channel.
func (p *OpenAIProvider) CompleteStream(ctx context.Context, req *agentllm.CompletionRequest) (<-chan *agentllm.StreamChunk, error) {
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
		messages = append(messages, openai.ChatCompletionMessage{Role: msg.Role, Content: msg.Content, Name: msg.Name})
	}
	model := p.GetModel(req.Model)
	stream, err := p.client.CreateChatCompletionStream(ctx, openai.ChatCompletionRequest{
		Model: model, Messages: messages, MaxTokens: p.GetMaxTokens(req.MaxTokens),
		Temperature: float32(p.GetTemperature(req.Temperature)), TopP: float32(req.TopP),
		Stop: req.Stop, Stream: true, StreamOptions: &openai.StreamOptions{IncludeUsage: true},
	})
	if err != nil {
		return nil, agentErrors.NewLLMRequestError(p.ProviderName(), model, err).WithContext("stream", true)
	}
	out := make(chan *agentllm.StreamChunk)
	go func() {
		defer close(out)
		defer func() { _ = stream.Close() }()
		var content strings.Builder
		var usage *agentllm.Usage
		var finishReason string
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
				terminal := &agentllm.StreamChunk{Content: content.String(), FinishReason: finishReason, Usage: usage, Done: true}
				if !errors.Is(err, io.EOF) {
					terminal.Error = agentErrors.NewLLMRequestError(p.ProviderName(), model, err)
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
