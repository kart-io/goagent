package llm

import (
	"context"
	agentErrors "github.com/kart-io/goagent/errors"
	"io"
	"strings"
)

// Complete issues a streaming request and collects it into a full response.
// Only a successful terminal chunk counts as completion: an error on a chunk is
// returned as is, and a stream that closes before the terminal chunk is treated
// as io.ErrUnexpectedEOF. The provider is not called if the request is already
// canceled or timed out; when reading the stream and cancellation are ready at
// the same time, the ctx error is returned. The internal ctx is canceled before
// returning, which releases the provider's reader goroutine and connection.
func Complete(ctx context.Context, client Client, req *CompletionRequest) (*CompletionResponse, error) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	stream, err := client.CompleteStream(ctx, req)
	if err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if stream == nil {
		return nil, agentErrors.New(agentErrors.CodeLLMResponse, "模型返回空流").WithComponent("llm").WithOperation("complete")
	}
	var deltas strings.Builder
	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case chunk, ok := <-stream:
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			if !ok {
				return nil, io.ErrUnexpectedEOF
			}
			if chunk == nil {
				continue
			}
			if chunk.Error != nil {
				return nil, chunk.Error
			}
			deltas.WriteString(chunk.Delta)
			if !chunk.Done {
				continue
			}
			response := &CompletionResponse{
				Content:       chunk.Content,
				Model:         chunk.ResponseModel,
				ResponseModel: chunk.ResponseModel,
				FinishReason:  chunk.FinishReason,
				Provider:      string(client.Provider()),
			}
			if response.Content == "" {
				response.Content = deltas.String()
			}
			if response.Model == "" && req != nil {
				response.Model = req.Model
			}
			if chunk.Usage != nil {
				tokens := chunk.Usage.Tokens()
				response.Usage = &tokens
				response.TokensUsed = tokens.TotalTokens
			}
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			return response, nil
		}
	}
}

// Chat calls Complete with a list of messages.
func Chat(ctx context.Context, client Client, messages []Message) (*CompletionResponse, error) {
	return Complete(ctx, client, &CompletionRequest{Messages: messages})
}
