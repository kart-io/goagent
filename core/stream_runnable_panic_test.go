package core

import (
	"context"
	"errors"
	"fmt"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/kart-io/goagent/core/observe"
	agentErrors "github.com/kart-io/goagent/errors"
)

// sourcePanicSite 是 panic 发生处，用于断言调用栈指向这里。
func sourcePanicSite() { panic("源 panic") }

func requirePanicError(t *testing.T, err error, value any, site string) *agentErrors.PanicError {
	t.Helper()
	var panicErr *agentErrors.PanicError
	require.ErrorAs(t, err, &panicErr)
	require.Equal(t, value, panicErr.Value)
	require.Contains(t, string(panicErr.Stack), site)
	require.Equal(t, "panic", observe.ErrorKind(err))
	return panicErr
}

func streamFinalError[O any](t *testing.T, r Runnable[int, O]) error {
	t.Helper()
	stream, err := r.Stream(context.Background(), 1)
	require.NoError(t, err)
	var last StreamChunk[O]
	for chunk := range stream {
		last = chunk
	}
	require.True(t, last.Done)
	return last.Error
}

func TestSafeInvokePanicIsPanicError(t *testing.T) {
	_, err := safeInvoke(func(context.Context, int) (int, error) {
		sourcePanicSite()
		return 0, nil
	}, context.Background(), 1)
	requirePanicError(t, err, "源 panic", "core.sourcePanicSite")
	// 默认 handler 仍返回 AgentError，PanicError 作为其 cause
	var agentErr *agentErrors.AgentError
	require.ErrorAs(t, err, &agentErr)
	require.Equal(t, agentErrors.CodeInternal, agentErr.Code)
}

func TestStreamRunnableSourcePanicIsPanicError(t *testing.T) {
	cb := &streamCallback{BaseCallback: NewBaseCallback()}
	r := NewStreamRunnable(StreamModeFinal, func(context.Context, int, func(StreamChunk[int]) error) error {
		sourcePanicSite()
		return nil
	}).WithCallbacks(cb)

	_, err := r.Invoke(context.Background(), 1)
	requirePanicError(t, err, "源 panic", "core.sourcePanicSite")
	requirePanicError(t, streamFinalError(t, r), "源 panic", "core.sourcePanicSite")
	// 执行源的 panic 与普通错误一样交给 OnError
	require.EqualValues(t, 2, cb.failures.Load())
	require.EqualValues(t, 0, cb.ends.Load())
}

func TestCustomPanicHandlerKeepsPanicError(t *testing.T) {
	original := GlobalPanicHandlerRegistry().GetHandler()
	t.Cleanup(func() { SetGlobalPanicHandler(original) })

	source := NewStreamRunnable(StreamModeFinal, func(context.Context, int, func(StreamChunk[int]) error) error {
		sourcePanicSite()
		return nil
	})
	invoke := func() error {
		_, err := safeInvoke(func(context.Context, int) (int, error) {
			sourcePanicSite()
			return 0, nil
		}, context.Background(), 1)
		return err
	}
	runInvoke := func() error {
		_, err := source.Invoke(context.Background(), 1)
		return err
	}

	t.Run("handler 返回自定义错误", func(t *testing.T) {
		handler := NewMockPanicHandler()
		SetGlobalPanicHandler(handler)
		for _, call := range []func() error{invoke, runInvoke} {
			err := call()
			requirePanicError(t, err, "源 panic", "core.sourcePanicSite")
			require.Equal(t, "mock panic: 源 panic", err.Error())
		}
		require.Equal(t, 2, handler.CallCount())
		for _, c := range handler.GetCalls() {
			require.Equal(t, "源 panic", c.PanicValue)
			require.Contains(t, c.StackTrace, "core.sourcePanicSite")
		}
	})

	t.Run("handler 返回包装后的领域错误", func(t *testing.T) {
		domainErr := errors.New("领域错误")
		handler := NewMockPanicHandler()
		handler.errorToReturn = fmt.Errorf("包装: %w", domainErr)
		SetGlobalPanicHandler(handler)
		err := invoke()
		requirePanicError(t, err, "源 panic", "core.sourcePanicSite")
		require.ErrorIs(t, err, domainErr)
	})

	t.Run("handler 返回 nil", func(t *testing.T) {
		SetGlobalPanicHandler(nilPanicHandler{})
		requirePanicError(t, invoke(), "源 panic", "core.sourcePanicSite")
		requirePanicError(t, runInvoke(), "源 panic", "core.sourcePanicSite")
	})
}

type nilPanicHandler struct{}

func (nilPanicHandler) HandlePanic(context.Context, string, string, interface{}, string) error {
	return nil
}

// panicCallback 在 panicOn 指定的回调中 panic，其余回调只计数。
type panicCallback struct {
	*BaseCallback
	panicOn                string
	starts, ends, failures atomic.Int32
}

func (c *panicCallback) OnStart(context.Context, any) error {
	c.starts.Add(1)
	if c.panicOn == "start" {
		panic("回调 panic")
	}
	return nil
}

func (c *panicCallback) OnEnd(context.Context, any) error {
	c.ends.Add(1)
	if c.panicOn == "end" {
		panic("回调 panic")
	}
	return nil
}

func (c *panicCallback) OnError(context.Context, error) error {
	c.failures.Add(1)
	if c.panicOn == "error" {
		panic("回调 panic")
	}
	return nil
}

// Chain 与 Agent 的回调复用同一组计数：开始计入 starts，结束计入 ends，错误计入 failures。
func (c *panicCallback) OnChainStart(ctx context.Context, _ string, input any) error {
	return c.OnStart(ctx, input)
}

func (c *panicCallback) OnChainEnd(ctx context.Context, _ string, output any) error {
	return c.OnEnd(ctx, output)
}

func (c *panicCallback) OnChainError(ctx context.Context, _ string, err error) error {
	return c.OnError(ctx, err)
}

func (c *panicCallback) OnAgentFinish(ctx context.Context, output any) error {
	return c.OnEnd(ctx, output)
}

func TestStreamRunnableCallbackPanicStopsCallbacks(t *testing.T) {
	sourceErr := errors.New("执行源失败")
	cases := []struct {
		panicOn, site      string
		sourceErr          error
		wantStarts         int32 // 第二个回调的计数
		wantFirstFailures  int32
		wantSecondFailures int32
	}{
		{panicOn: "start", site: "(*panicCallback).OnStart"},
		{panicOn: "end", site: "(*panicCallback).OnEnd", wantStarts: 1},
		{panicOn: "error", site: "(*panicCallback).OnError", sourceErr: sourceErr, wantStarts: 1, wantFirstFailures: 1},
	}
	for _, tc := range cases {
		t.Run(tc.panicOn, func(t *testing.T) {
			for name, run := range map[string]func(Runnable[int, int]) error{
				"Invoke": func(r Runnable[int, int]) error { _, err := r.Invoke(context.Background(), 1); return err },
				"Stream": func(r Runnable[int, int]) error { return streamFinalError(t, r) },
			} {
				t.Run(name, func(t *testing.T) {
					first := &panicCallback{BaseCallback: NewBaseCallback(), panicOn: tc.panicOn}
					second := &panicCallback{BaseCallback: NewBaseCallback()}
					r := NewStreamRunnable(StreamModeFinal, func(_ context.Context, input int, emit func(StreamChunk[int]) error) error {
						if tc.sourceErr != nil {
							return tc.sourceErr
						}
						return emit(StreamChunk[int]{Done: true, Data: input})
					}).WithCallbacks(first, second)

					err := run(r)
					requirePanicError(t, err, "回调 panic", tc.site)
					require.NotErrorIs(t, err, sourceErr, "回调 panic 替代原错误返回")
					require.EqualValues(t, 1, first.starts.Load())
					require.EqualValues(t, tc.wantStarts, second.starts.Load())
					require.EqualValues(t, 0, second.ends.Load())
					// 回调 panic 不再交给 OnError，OnError 自身 panic 后不再调用后续 OnError
					require.EqualValues(t, tc.wantFirstFailures, first.failures.Load())
					require.EqualValues(t, tc.wantSecondFailures, second.failures.Load())
				})
			}
		})
	}
}
