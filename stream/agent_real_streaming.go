package stream

import (
	"context"
	"strings"
	"time"

	"github.com/kart-io/goagent/core"
	agentErrors "github.com/kart-io/goagent/errors"
	"github.com/kart-io/goagent/llm"
)

// Execute collects the real provider stream, rather than the embedded agent's
// simulated stream. Errors after partial output are returned to the caller.
func (a *StreamingLLMAgentWithRealStreaming) Execute(ctx context.Context, input *core.AgentInput) (*core.AgentOutput, error) {
	start := time.Now()
	output, err := a.ExecuteStream(ctx, input)
	if err != nil {
		return nil, err
	}
	defer func() { _ = output.Close() }()
	text, err := output.(*Reader).CollectText()
	if err != nil {
		return nil, err
	}
	return &core.AgentOutput{
		Result: text, Status: "success", Message: "LLM response generated",
		Timestamp: time.Now(), Latency: time.Since(start),
	}, nil
}

// ExecuteStream forwards provider output as it arrives. StreamClient preserves
// request parameters, usage, and asynchronous errors. TokenStreamingClient uses
// the provider's own model settings and can only report stream setup errors.
// Close the returned stream to cancel upstream work when stopping early.
// Mid-stream retries are deliberately disabled to avoid duplicating output.
func (a *StreamingLLMAgentWithRealStreaming) ExecuteStream(ctx context.Context, input *core.AgentInput) (core.StreamOutput, error) {
	if input == nil {
		return nil, agentErrors.New(agentErrors.CodeInvalidInput, "agent input is required")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	streamCtx, cancel := context.WithCancel(ctx)
	var chunks <-chan *llm.StreamChunk
	var tokens <-chan string
	var err error
	switch client := a.llmClient.(type) {
	case llm.StreamClient:
		messages := []llm.Message{}
		if input.Instruction != "" {
			messages = append(messages, llm.SystemMessage(input.Instruction))
		}
		messages = append(messages, llm.UserMessage(input.Task))
		chunks, err = client.CompleteStream(streamCtx, &llm.CompletionRequest{
			Messages: messages, Model: a.config.Model,
			Temperature: a.config.Temperature, MaxTokens: a.config.MaxTokens,
		})
	case llm.TokenStreamingClient:
		prompt := input.Task
		if input.Instruction != "" {
			prompt = input.Instruction + "\n\n" + prompt
		}
		tokens, err = client.Stream(streamCtx, prompt)
	default:
		err = agentErrors.New(agentErrors.CodeNotImplemented, "LLM client does not support streaming")
	}
	if err != nil {
		cancel()
		return nil, err
	}
	if chunks == nil && tokens == nil {
		cancel()
		return nil, agentErrors.New(agentErrors.CodeStreamRead, "LLM client returned a nil stream")
	}
	opts := core.DefaultStreamOptions()
	opts.RetryOnError = false
	writer := NewWriter(streamCtx, opts)
	// The reader uses the caller's context so producer completion cannot discard
	// buffered output. Closing the reader also cancels the upstream request.
	reader := NewReader(ctx, writer.Channel(), opts)
	readerCancel := reader.cancel
	reader.cancel = func() {
		cancel()
		readerCancel()
	}
	go func() {
		defer cancel()
		defer func() { _ = writer.Close() }()
		if tokens != nil {
			for {
				select {
				case <-streamCtx.Done():
					return
				case token, ok := <-tokens:
					if !ok {
						return
					}
					if token != "" {
						if err := writer.WriteText(token); err != nil {
							return
						}
					}
				}
			}
		}
		var content strings.Builder
		for {
			select {
			case <-streamCtx.Done():
				return
			case chunk, ok := <-chunks:
				if !ok {
					return
				}
				if chunk == nil {
					continue
				}
				if chunk.Error != nil {
					_ = writer.WriteError(chunk.Error)
					return
				}
				delta := chunk.Delta
				if delta == "" && chunk.Content != "" {
					if !strings.HasPrefix(chunk.Content, content.String()) {
						_ = writer.WriteError(agentErrors.New(agentErrors.CodeStreamRead, "non-monotonic cumulative stream content"))
						return
					}
					delta = chunk.Content[content.Len():]
				}
				if delta != "" {
					if err := writer.WriteText(delta); err != nil {
						return
					}
					content.WriteString(delta)
				}
				if chunk.Usage != nil || chunk.FinishReason != "" {
					metadata := map[string]interface{}{"finish_reason": chunk.FinishReason}
					if chunk.Usage != nil {
						usage := *chunk.Usage
						metadata["usage"] = usage
					}
					if err := writer.WriteChunk(&core.LegacyStreamChunk{Type: "metadata", Data: metadata}); err != nil {
						return
					}
				}
				if chunk.Done {
					return
				}
			}
		}
	}()
	return reader, nil
}
