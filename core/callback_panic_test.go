package core

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
)

// callbackPanicCase 描述第一个回调在哪个通知中 panic，以及第二个回调此时应观察到的调用次数。
type callbackPanicCase struct {
	panicOn            string
	failWith           error // 非空时被执行的函数或步骤返回该错误
	wantSecondStarts   int32
	wantFirstFailures  int32
	wantSecondFailures int32
}

func callbackPanicCases(err error) []callbackPanicCase {
	return []callbackPanicCase{
		{panicOn: "start"},
		{panicOn: "end", wantSecondStarts: 1},
		{panicOn: "error", failWith: err, wantSecondStarts: 1, wantFirstFailures: 1},
	}
}

// requireCallbackPanic 断言回调 panic 被转为 *PanicError 替代原错误返回（调用栈指向 panicCallback 的回调方法），且之后不再调用任何回调。
func requireCallbackPanic(t *testing.T, err error, tc callbackPanicCase, first, second *panicCallback) {
	t.Helper()
	requirePanicError(t, err, "回调 panic", "(*panicCallback).On")
	if tc.failWith != nil {
		require.NotErrorIs(t, err, tc.failWith, "回调 panic 替代原错误返回")
	}
	require.EqualValues(t, 1, first.starts.Load())
	require.EqualValues(t, tc.wantSecondStarts, second.starts.Load())
	require.EqualValues(t, 0, second.ends.Load())
	require.EqualValues(t, tc.wantFirstFailures, first.failures.Load())
	require.EqualValues(t, tc.wantSecondFailures, second.failures.Load())
}

func newPanicCallbacks(panicOn string) (*panicCallback, *panicCallback) {
	return &panicCallback{BaseCallback: NewBaseCallback(), panicOn: panicOn}, &panicCallback{BaseCallback: NewBaseCallback()}
}

func TestRunnableFuncCallbackPanicStopsCallbacks(t *testing.T) {
	fnErr := errors.New("函数失败")
	for _, tc := range callbackPanicCases(fnErr) {
		t.Run(tc.panicOn, func(t *testing.T) {
			first, second := newPanicCallbacks(tc.panicOn)
			r := NewRunnableFunc(func(_ context.Context, input int) (int, error) {
				return input, tc.failWith
			}).WithCallbacks(first, second)
			_, err := r.Invoke(context.Background(), 1)
			requireCallbackPanic(t, err, tc, first, second)
			// Stream 复用 Invoke，同样以 PanicError 结束
			requirePanicError(t, streamFinalError(t, r), "回调 panic", "(*panicCallback).On")
		})
	}
}

func TestBaseAgentCallbackPanic(t *testing.T) {
	for _, panicOn := range []string{"start", "end"} {
		t.Run(panicOn, func(t *testing.T) {
			first, second := newPanicCallbacks(panicOn)
			agent := NewBaseAgent("base", "", nil).WithCallbacks(first, second)
			_, err := agent.Invoke(context.Background(), &AgentInput{})
			requirePanicError(t, err, "回调 panic", "(*panicCallback).On")
			require.NotErrorIs(t, err, ErrNotImplemented, "回调 panic 不被 ErrNotImplemented 掩盖")
			require.EqualValues(t, 0, second.ends.Load())
		})
	}
	// 回调的普通错误仍让位于 ErrNotImplemented
	_, err := NewBaseAgent("base", "", nil).WithCallbacks(&errorFinishCallback{BaseCallback: NewBaseCallback()}).
		Invoke(context.Background(), &AgentInput{})
	require.ErrorIs(t, err, ErrNotImplemented)
}

type errorFinishCallback struct{ *BaseCallback }

func (errorFinishCallback) OnAgentFinish(context.Context, any) error {
	return errors.New("回调失败")
}

type funcStep struct{ err error }

func (s funcStep) Execute(_ context.Context, input any) (any, error) { return input, s.err }
func (funcStep) Name() string                                        { return "step" }
func (funcStep) Description() string                                 { return "" }

func TestBaseChainCallbackPanicStopsCallbacks(t *testing.T) {
	stepErr := errors.New("步骤失败")
	for _, tc := range callbackPanicCases(stepErr) {
		t.Run(tc.panicOn, func(t *testing.T) {
			for name, run := range map[string]func(Runnable[*ChainInput, *ChainOutput]) error{
				"Invoke": func(r Runnable[*ChainInput, *ChainOutput]) error {
					_, err := r.Invoke(context.Background(), &ChainInput{Options: ChainOptions{StopOnError: true}})
					return err
				},
				"Stream": func(r Runnable[*ChainInput, *ChainOutput]) error {
					stream, err := r.Stream(context.Background(), &ChainInput{Options: ChainOptions{StopOnError: true}})
					require.NoError(t, err)
					var last StreamChunk[*ChainOutput]
					for chunk := range stream {
						last = chunk
					}
					require.True(t, last.Done)
					return last.Error
				},
			} {
				t.Run(name, func(t *testing.T) {
					first, second := newPanicCallbacks(tc.panicOn)
					chain := NewBaseChain("chain", []Step{funcStep{err: tc.failWith}}).WithCallbacks(first, second)
					requireCallbackPanic(t, run(chain), tc, first, second)
				})
			}
		})
	}
}

func TestCallbackManagerCallbackPanic(t *testing.T) {
	ctx := context.Background()
	failure := errors.New("操作失败")

	first, second := newPanicCallbacks("start")
	err := NewCallbackManager(first, second).OnStart(ctx, 1)
	requirePanicError(t, err, "回调 panic", "(*panicCallback).OnStart")
	require.EqualValues(t, 0, second.starts.Load(), "panic 后不再调用后续回调")

	first, second = newPanicCallbacks("error")
	err = NewCallbackManager(first, second).NotifyError(ctx, failure)
	requirePanicError(t, err, "回调 panic", "(*panicCallback).OnError")
	require.EqualValues(t, 0, second.failures.Load())

	first, _ = newPanicCallbacks("end")
	requirePanicError(t, NewCallbackManager(first).TriggerCallbacks(func(cb Callback) error {
		return cb.OnEnd(ctx, 1)
	}), "回调 panic", "(*panicCallback).OnEnd")

	// OnError 的普通错误被忽略；nil 管理器什么也不做
	require.NoError(t, NewCallbackManager(&errorCallback{BaseCallback: NewBaseCallback()}).NotifyError(ctx, failure))
	var nilManager *CallbackManager
	require.NoError(t, nilManager.NotifyError(ctx, failure))
}

type errorCallback struct{ *BaseCallback }

func (errorCallback) OnError(context.Context, error) error { return errors.New("回调失败") }
