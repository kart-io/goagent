package reflection

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"

	"github.com/kart-io/goagent/core"
	"github.com/stretchr/testify/require"
)

func TestReflectionRequiresRealExecutor(t *testing.T) {
	a := NewSelfReflectiveAgentWithContext(context.Background(), nil, nil)
	defer func() { require.NoError(t, a.Shutdown(context.Background())) }()
	_, err := a.Execute(context.Background(), &core.AgentInput{Task: "任务"})
	require.ErrorIs(t, err, ErrTaskExecutorRequired)
	_, err = a.Invoke(context.Background(), nil)
	require.Error(t, err)
}

func TestReflectionAllEntriesDelegate(t *testing.T) {
	var calls atomic.Int32
	failure := errors.New("真实执行失败")
	executor := core.NewRunnableFunc(func(ctx context.Context, in *core.AgentInput) (*core.AgentOutput, error) {
		calls.Add(1)
		if in.Task == "失败" {
			return nil, failure
		}
		if in.Task == "空" {
			return nil, nil
		}
		return &core.AgentOutput{Result: in.Task, Status: "partial"}, nil
	})
	a := NewSelfReflectiveAgentWithContext(context.Background(), nil, nil, WithTaskExecutor(executor))
	defer func() { require.NoError(t, a.Shutdown(context.Background())) }()
	input := &core.AgentInput{Task: "真实答案"}
	result, err := a.Invoke(context.Background(), input)
	require.NoError(t, err)
	require.Equal(t, "真实答案", result.Result)
	require.Equal(t, "partial", result.Status)
	stream, err := a.Stream(context.Background(), input)
	require.NoError(t, err)
	for c := range stream {
		require.NoError(t, c.Error)
		require.Equal(t, "partial", c.Data.Status)
	}
	_, err = a.WithConfig(core.RunnableConfig{MaxConcurrency: 1}).Batch(context.Background(), []*core.AgentInput{input, input})
	require.NoError(t, err)
	_, err = a.WithCallbacks(core.NewBaseCallback()).Invoke(context.Background(), input)
	require.NoError(t, err)
	require.EqualValues(t, 5, calls.Load())
	_, err = a.Invoke(context.Background(), &core.AgentInput{Task: "失败"})
	require.ErrorIs(t, err, failure)
	_, err = a.Invoke(context.Background(), &core.AgentInput{Task: "空"})
	require.Error(t, err)
	require.Equal(t, core.StreamModeFinal, a.StreamMode())
}
