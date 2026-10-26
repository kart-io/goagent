package builder

import (
	"context"
	"errors"
	"io"
	"testing"
	"time"

	"github.com/kart-io/goagent/core"
	"github.com/kart-io/goagent/llm"
	"github.com/stretchr/testify/require"
)

func TestBuilderRunnableUsesEventExecution(t *testing.T) {
	model := &eventModel{MockLLMClient: NewMockLLMClient(), chunks: []*llm.StreamChunk{{Delta: "答案"}, {Done: true, Usage: &llm.Usage{TotalTokens: 3}}}}
	a, err := NewSimpleBuilder(model).Build()
	require.NoError(t, err)
	r := a.AsRunnable()
	var _ core.Runnable[any, *AgentOutput] = r
	output, err := r.Invoke(context.Background(), "问题")
	require.NoError(t, err)
	require.Equal(t, "答案", output.Result)
	require.Equal(t, 3, output.TokenUsage.TotalTokens)
	stream, err := r.Stream(context.Background(), "问题")
	require.NoError(t, err)
	var final *AgentOutput
	var text string
	for c := range stream {
		require.NoError(t, c.Error)
		if c.Event != nil {
			text += c.Event.Delta
		}
		if c.Done {
			final = c.Data
		}
	}
	require.NotNil(t, final)
	require.Equal(t, output.Result, final.Result)
	require.Equal(t, "答案", text)
	results, err := r.WithConfig(core.RunnableConfig{MaxConcurrency: 3}).Batch(context.Background(), []any{"一", "二", "三"})
	require.NoError(t, err)
	require.Len(t, results, 3)
	value, err := r.Pipe(core.NewRunnableFunc(func(ctx context.Context, o *AgentOutput) (any, error) { return o.Result, nil })).Invoke(context.Background(), "问题")
	require.NoError(t, err)
	require.Equal(t, "答案", value)
}

func TestBuilderRunnableErrorsAndSharedGate(t *testing.T) {
	model := &eventModel{MockLLMClient: NewMockLLMClient(), chunks: []*llm.StreamChunk{{Delta: "未完成"}}}
	a, err := NewSimpleBuilder(model).Build()
	require.NoError(t, err)
	_, err = a.AsRunnable().Invoke(context.Background(), "问题")
	require.ErrorIs(t, err, io.ErrUnexpectedEOF)
	a.executionGate <- struct{}{}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	_, err = a.AsRunnable().Invoke(ctx, "排队")
	require.ErrorIs(t, err, context.DeadlineExceeded)
	<-a.executionGate
	model.chunks = []*llm.StreamChunk{{Done: true}}
	_, err = a.AsRunnable().Invoke(context.Background(), "重试")
	require.NoError(t, err)
	model.chunks = []*llm.StreamChunk{{Error: errors.New("上游失败")}}
	_, err = a.AsRunnable().Invoke(context.Background(), "问题")
	require.ErrorContains(t, err, "上游失败")
}
