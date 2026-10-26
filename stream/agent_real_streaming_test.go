package stream

import (
	"context"
	"errors"
	"io"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/kart-io/goagent/core"
	"github.com/kart-io/goagent/llm"
)

type structuredStreamClient struct {
	*MockLLMClient
	start func(context.Context, *llm.CompletionRequest) (<-chan *llm.StreamChunk, error)
}

func (c *structuredStreamClient) CompleteStream(ctx context.Context, req *llm.CompletionRequest) (<-chan *llm.StreamChunk, error) {
	return c.start(ctx, req)
}
func (c *structuredStreamClient) ChatStream(ctx context.Context, messages []llm.Message) (<-chan *llm.StreamChunk, error) {
	return c.start(ctx, &llm.CompletionRequest{Messages: messages})
}

func TestRealStreamingForwardsDeltasAndUsage(t *testing.T) {
	var request *llm.CompletionRequest
	client := &structuredStreamClient{MockLLMClient: NewMockLLMClient(0, "must not use Chat")}
	client.start = func(_ context.Context, req *llm.CompletionRequest) (<-chan *llm.StreamChunk, error) {
		request = req
		ch := make(chan *llm.StreamChunk, 4)
		ch <- &llm.StreamChunk{Delta: "你", Content: "你"}
		ch <- &llm.StreamChunk{Content: "你好"}
		ch <- &llm.StreamChunk{Content: "你好", Done: true, Usage: &llm.Usage{TotalTokens: 10}}
		close(ch)
		return ch, nil
	}
	agent := NewStreamingLLMAgentWithRealStreaming(client, &StreamingLLMConfig{Model: "model", Temperature: 0.2, MaxTokens: 50})
	output, err := agent.ExecuteStream(context.Background(), &core.AgentInput{Task: "question", Instruction: "instruction"})
	require.NoError(t, err)
	defer output.Close()
	var text string
	var usage llm.Usage
	for {
		chunk, err := output.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		require.NoError(t, err)
		text += chunk.Text
		if chunk.Type == "metadata" {
			usage = chunk.Data.(map[string]interface{})["usage"].(llm.Usage)
		}
	}
	require.Equal(t, "你好", text, "cumulative terminal frames must not duplicate text")
	require.Equal(t, 10, usage.TotalTokens)
	require.Equal(t, "model", request.Model)
	require.Equal(t, 50, request.MaxTokens)
	require.Equal(t, []llm.Message{llm.SystemMessage("instruction"), llm.UserMessage("question")}, request.Messages)
	result, err := agent.Execute(context.Background(), &core.AgentInput{Task: "question"})
	require.NoError(t, err)
	require.Equal(t, "你好", result.Result, "Execute must use the real streaming implementation")
	require.Zero(t, client.called)
}

func TestRealStreamingCloseCancelsUpstream(t *testing.T) {
	stopped := make(chan struct{})
	client := &structuredStreamClient{MockLLMClient: NewMockLLMClient(0, "")}
	client.start = func(ctx context.Context, _ *llm.CompletionRequest) (<-chan *llm.StreamChunk, error) {
		ch := make(chan *llm.StreamChunk)
		go func() {
			defer close(ch)
			defer close(stopped)
			select {
			case ch <- &llm.StreamChunk{Delta: "first"}:
			case <-ctx.Done():
				return
			}
			<-ctx.Done()
		}()
		return ch, nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	output, err := NewStreamingLLMAgentWithRealStreaming(client, nil).ExecuteStream(ctx, &core.AgentInput{Task: "question"})
	require.NoError(t, err)
	chunk, err := output.Next()
	require.NoError(t, err)
	require.Equal(t, "first", chunk.Text)
	require.NoError(t, output.Close())
	select {
	case <-stopped:
	case <-ctx.Done():
		t.Fatal("closing reader did not cancel upstream")
	}
}

func TestRealStreamingErrorsAreNotSwallowed(t *testing.T) {
	sentinel := errors.New("stream failed")
	for _, startup := range []bool{false, true} {
		client := &structuredStreamClient{MockLLMClient: NewMockLLMClient(0, "")}
		client.start = func(context.Context, *llm.CompletionRequest) (<-chan *llm.StreamChunk, error) {
			if startup {
				return nil, sentinel
			}
			ch := make(chan *llm.StreamChunk, 2)
			ch <- &llm.StreamChunk{Delta: "partial"}
			ch <- &llm.StreamChunk{Error: sentinel}
			close(ch)
			return ch, nil
		}
		_, err := NewStreamingLLMAgentWithRealStreaming(client, nil).Execute(context.Background(), &core.AgentInput{Task: "question"})
		require.ErrorIs(t, err, sentinel)
	}
	_, err := NewStreamingLLMAgentWithRealStreaming(NewMockLLMClient(0, ""), nil).ExecuteStream(context.Background(), &core.AgentInput{})
	require.ErrorContains(t, err, "does not support streaming")
}

type tokenStreamClient struct{ *MockLLMClient }

func (c *tokenStreamClient) Stream(context.Context, string) (<-chan string, error) {
	ch := make(chan string, 2)
	ch <- "token "
	ch <- "stream"
	close(ch)
	return ch, nil
}

func TestRealStreamingTokenClientFallback(t *testing.T) {
	client := &tokenStreamClient{NewMockLLMClient(0, "")}
	result, err := NewStreamingLLMAgentWithRealStreaming(client, nil).Execute(context.Background(), &core.AgentInput{Task: "question"})
	require.NoError(t, err)
	require.Equal(t, "token stream", result.Result)
}
