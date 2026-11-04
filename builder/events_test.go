package builder

import (
	"context"
	"errors"
	"io"
	"testing"
	"time"

	"github.com/kart-io/goagent/core/middleware"
	"github.com/kart-io/goagent/llm"
	"github.com/stretchr/testify/require"
)

type eventModel struct {
	*MockLLMClient
	request *llm.CompletionRequest
	prompt  string
	chunks  []*llm.StreamChunk
	stall   bool
	started chan struct{}
}

func (m *eventModel) stream(ctx context.Context, msg []llm.Message) (<-chan *llm.StreamChunk, error) {
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
	return m.stream(ctx, r.Messages)
}

type endingMiddleware struct{ *middleware.BaseMiddleware }

func (m endingMiddleware) OnAfter(ctx context.Context, r *middleware.MiddlewareResponse) (*middleware.MiddlewareResponse, error) {
	r.Metadata["after"] = true
	return r, nil
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
