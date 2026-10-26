package builder

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/kart-io/goagent/core/middleware"
	"github.com/kart-io/goagent/interfaces"
	"github.com/kart-io/goagent/llm"
	"github.com/kart-io/goagent/llm/common"
	"github.com/stretchr/testify/require"
)

type eventModel struct {
	*MockLLMClient
	selections int
	request    *llm.CompletionRequest
	prompt     string
	chunks     []*llm.StreamChunk
	stall      bool
	started    chan struct{}
}

func (m *eventModel) GenerateWithTools(ctx context.Context, prompt string, tools []interfaces.Tool) (*common.ToolCallResponse, error) {
	m.selections++
	if m.selections == 1 {
		return &common.ToolCallResponse{ToolCalls: []common.ToolCall{{Name: "lookup", Arguments: map[string]interface{}{}}}, Usage: &interfaces.TokenUsage{TotalTokens: 2}}, nil
	}
	return &common.ToolCallResponse{}, nil
}
func (m *eventModel) ChatStream(ctx context.Context, msg []llm.Message) (<-chan *llm.StreamChunk, error) {
	m.prompt = ""
	for _, message := range msg {
		m.prompt += message.Content
	}
	c := make(chan *llm.StreamChunk)
	go func() {
		defer close(c)
		if m.started != nil {
			close(m.started)
		}
		if m.stall {
			<-ctx.Done()
			return
		}
		for _, chunk := range m.chunks {
			select {
			case c <- chunk:
			case <-ctx.Done():
				return
			}
		}
	}()
	return c, nil
}
func (m *eventModel) CompleteStream(ctx context.Context, r *llm.CompletionRequest) (<-chan *llm.StreamChunk, error) {
	m.request = r
	return m.ChatStream(ctx, r.Messages)
}

type endingMiddleware struct{ *middleware.BaseMiddleware }

func (m endingMiddleware) OnAfter(ctx context.Context, r *middleware.MiddlewareResponse) (*middleware.MiddlewareResponse, error) {
	r.Metadata["after"] = true
	return r, nil
}
func TestUnifiedEventsGroundingToolsAndNativeStream(t *testing.T) {
	model := &eventModel{MockLLMClient: NewMockLLMClient(), chunks: []*llm.StreamChunk{{Delta: "Answer "}, {Delta: "[1]"}, {Done: true, Usage: &llm.Usage{TotalTokens: 3}}}}
	r := documentRetrieverFunc(func(context.Context, string) ([]*interfaces.Document, error) {
		return []*interfaces.Document{{ID: "evidence", PageContent: "policy"}}, nil
	})
	a, err := NewSimpleBuilder(model).WithTools(NewMockTool("lookup", "tool evidence")).WithRetriever(r).Build()
	require.NoError(t, err)
	a.chain.Use(endingMiddleware{middleware.NewBaseMiddleware("test")})
	events, err := a.ExecuteStream(context.Background(), "policy?")
	require.NoError(t, err)
	var types []string
	var text strings.Builder
	var output *AgentOutput
	for e := range events {
		require.NoError(t, e.Error)
		types = append(types, e.Type)
		text.WriteString(e.Delta)
		if e.Output != nil {
			output = e.Output
		}
	}
	require.Equal(t, []string{"sources", "step", "step", "step", "step", "text", "text", "usage", "done"}, types)
	require.Equal(t, "Answer [1]", text.String())
	require.Equal(t, text.String(), output.Result)
	require.Equal(t, 5, output.TokenUsage.TotalTokens)
	require.Equal(t, a.config.MaxTokens, model.request.MaxTokens)
	require.Equal(t, a.config.Temperature, model.request.Temperature)
	require.Equal(t, true, output.Metadata["after"])
	require.Contains(t, model.prompt, "policy")
	require.Contains(t, model.prompt, "tool evidence")
}
func TestUnifiedEventsFailureAndCancellation(t *testing.T) {
	for _, chunks := range [][]*llm.StreamChunk{{{Delta: "partial"}}, {{Error: io.ErrUnexpectedEOF}}} {
		model := &eventModel{MockLLMClient: NewMockLLMClient(), chunks: chunks}
		a, err := NewSimpleBuilder(model).Build()
		require.NoError(t, err)
		stream, err := a.ExecuteStream(context.Background(), "q")
		require.NoError(t, err)
		failed := false
		for event := range stream {
			require.NotEqual(t, "done", event.Type)
			if event.Type == "error" {
				require.ErrorIs(t, event.Error, io.ErrUnexpectedEOF)
				failed = true
			}
		}
		require.True(t, failed)
	}
	model := &eventModel{MockLLMClient: NewMockLLMClient(), stall: true, started: make(chan struct{})}
	a, err := NewSimpleBuilder(model).Build()
	require.NoError(t, err)
	ctx, cancel := context.WithCancel(context.Background())
	stream, err := a.ExecuteStream(ctx, "q")
	require.NoError(t, err)
	<-model.started
	cancel()
	closed := make(chan struct{})
	go func() {
		defer close(closed)
		for range stream {
		}
	}()
	select {
	case <-closed:
	case <-time.After(time.Second):
		t.Fatal("cancellation blocked")
	}
	_, err = a.ExecuteStream(ctx, "q")
	require.True(t, errors.Is(err, context.Canceled))
}
