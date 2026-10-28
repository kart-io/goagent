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
	"github.com/kart-io/goagent/interfaces"
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

type panicInitComponent struct{ *mockLifecycleComponent }

func (panicInitComponent) Init(context.Context, interface{}) error { sourcePanicSite(); return nil }

type panicStartComponent struct{ *mockLifecycleComponent }

func (panicStartComponent) Start(context.Context) error { sourcePanicSite(); return nil }

type panicStopComponent struct{ *mockLifecycleComponent }

func (panicStopComponent) Stop(context.Context) error { sourcePanicSite(); return nil }

type panicHealthComponent struct{ *mockLifecycleComponent }

func (panicHealthComponent) HealthCheck(context.Context) interfaces.HealthStatus {
	sourcePanicSite()
	return interfaces.HealthStatus{}
}

// lifecyclePanicHandlers 覆盖默认 handler 与返回自定义 AgentError 的 handler。
func lifecyclePanicHandlers(t *testing.T) map[string]PanicHandler {
	original := GlobalPanicHandlerRegistry().GetHandler()
	t.Cleanup(func() { SetGlobalPanicHandler(original) })
	return map[string]PanicHandler{
		"默认 handler":  &DefaultPanicHandler{},
		"自定义 handler": panicHandlerFunc(func() error { return agentErrors.New(agentErrors.CodeInternal, "自定义") }),
	}
}

// Start/Stop 的 panic 与 Init 一样转为 *PanicError，补上 component_name，组件进入 Failed 状态。
func TestLifecycleStartStopPanicIsPanicError(t *testing.T) {
	cases := map[string]struct {
		component interfaces.Lifecycle
		run       func(context.Context, *DefaultLifecycleManager) error
	}{
		"start": {
			component: panicStartComponent{newMockComponent("panicky")},
			run: func(ctx context.Context, m *DefaultLifecycleManager) error {
				if err := m.InitAll(ctx); err != nil {
					return err
				}
				return m.StartAll(ctx)
			},
		},
		"stop": {
			component: panicStopComponent{newMockComponent("panicky")},
			run: func(ctx context.Context, m *DefaultLifecycleManager) error {
				if err := m.InitAll(ctx); err != nil {
					return err
				}
				if err := m.StartAll(ctx); err != nil {
					return err
				}
				return m.StopAll(ctx)
			},
		},
	}
	for phase, tc := range cases {
		for name, handler := range lifecyclePanicHandlers(t) {
			t.Run(phase+"/"+name, func(t *testing.T) {
				SetGlobalPanicHandler(handler)
				manager := NewLifecycleManager()
				require.NoError(t, manager.Register("panicky", tc.component, 0))
				err := tc.run(context.Background(), manager)
				panicErr := requirePanicError(t, err, "源 panic", "core.sourcePanicSite")
				require.Equal(t, "lifecycle_manager."+phase, panicErr.Op)
				require.Contains(t, err.Error(), "component_name=panicky")
				state, stateErr := manager.GetState("panicky")
				require.NoError(t, stateErr)
				require.Equal(t, interfaces.StateFailed, state)
			})
		}
	}
}

// HealthCheck panic 时 Details 只带位置，与 handler 无关，不含 panic 值与调用栈。
func TestLifecycleHealthCheckPanicDetails(t *testing.T) {
	for name, handler := range lifecyclePanicHandlers(t) {
		t.Run(name, func(t *testing.T) {
			SetGlobalPanicHandler(handler)
			manager := NewLifecycleManager()
			require.NoError(t, manager.Register("panicky", panicHealthComponent{newMockComponent("panicky")}, 0))
			status := manager.HealthCheckAll(context.Background())["panicky"]
			require.Equal(t, interfaces.HealthUnhealthy, status.State)
			require.Equal(t, "panicky", status.ComponentName)
			require.Equal(t, map[string]interface{}{"error_kind": "panic", "panic_op": "lifecycle_manager.health_check"}, status.Details)
			require.NotContains(t, status.Message, "源 panic")
			require.Equal(t, "lifecycle_manager.health_check 发生 panic", status.Message)
		})
	}
}

func TestLifecycleInitPanicIsPanicError(t *testing.T) {
	original := GlobalPanicHandlerRegistry().GetHandler()
	t.Cleanup(func() { SetGlobalPanicHandler(original) })

	for name, handler := range map[string]PanicHandler{
		"默认 handler": &DefaultPanicHandler{},
		// 自定义 handler 返回的 AgentError 被包装后，仍应补上 component_name
		"自定义 handler": panicHandlerFunc(func() error { return agentErrors.New(agentErrors.CodeInternal, "自定义") }),
	} {
		t.Run(name, func(t *testing.T) {
			SetGlobalPanicHandler(handler)
			manager := NewLifecycleManager()
			require.NoError(t, manager.Register("panicky", panicInitComponent{newMockComponent("panicky")}, 0))
			err := manager.InitAll(context.Background())
			requirePanicError(t, err, "源 panic", "core.sourcePanicSite")
			require.Contains(t, err.Error(), "component_name=panicky")
		})
	}
}

type panicHandlerFunc func() error

func (f panicHandlerFunc) HandlePanic(context.Context, string, string, interface{}, string) error {
	return f()
}
