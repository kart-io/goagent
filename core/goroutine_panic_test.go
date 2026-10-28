package core

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/kart-io/goagent/testing/panictest"
)

// panicStep 在 Execute 或 Name 中 panic，calls 记录 Execute 次数。
type panicStep struct {
	panicIn string
	calls   *atomic.Int32
}

func (s panicStep) Execute(_ context.Context, input any) (any, error) {
	s.calls.Add(1)
	if s.panicIn == "execute" {
		panic("step boom")
	}
	return input, nil
}

func (s panicStep) Name() string {
	if s.panicIn == "name" {
		panic("name boom")
	}
	return "step"
}

func (panicStep) Description() string { return "" }

func lastChainChunk(t *testing.T, stream <-chan StreamChunk[*ChainOutput]) StreamChunk[*ChainOutput] {
	t.Helper()
	var last StreamChunk[*ChainOutput]
	for chunk := range stream {
		last = chunk
	}
	require.True(t, last.Done)
	return last
}

// Stream 的 goroutine 中步骤 panic 时进程不得退出：以 PanicError 终止本链（不论 StopOnError），不再执行后续步骤。
func TestBaseChainStreamStepPanicDoesNotKillProcess(t *testing.T) {
	panictest.Run(t, func(t *testing.T) {
		var first, next atomic.Int32
		chain := NewBaseChain("chain", []Step{panicStep{panicIn: "execute", calls: &first}, panicStep{calls: &next}})
		stream, err := chain.Stream(context.Background(), &ChainInput{})
		require.NoError(t, err)
		last := lastChainChunk(t, stream)
		requirePanicError(t, last.Error, "step boom", "panicStep.Execute")
		require.Equal(t, "failed", last.Data.Status)
		require.EqualValues(t, 0, next.Load(), "panic 后不执行后续步骤")

		// 步骤的其余方法（如 Name）panic 由 goroutine 兜底，同样以 PanicError 终止
		stream, err = NewBaseChain("chain", []Step{panicStep{panicIn: "name", calls: &first}}).
			Stream(context.Background(), &ChainInput{})
		require.NoError(t, err)
		requirePanicError(t, lastChainChunk(t, stream).Error, "name boom", "panicStep.Name")
	})
}

// Invoke 与 Stream 一致：步骤 panic 转为 PanicError 返回并终止本链。
func TestBaseChainInvokeStepPanicReturnsPanicError(t *testing.T) {
	var first, next atomic.Int32
	output, err := NewBaseChain("chain", []Step{panicStep{panicIn: "execute", calls: &first}, panicStep{calls: &next}}).
		Invoke(context.Background(), &ChainInput{})
	requirePanicError(t, err, "step boom", "panicStep.Execute")
	require.Equal(t, "failed", output.Status)
	require.EqualValues(t, 0, next.Load())
}

// Generator 在 ToChannel 的 goroutine 中 panic 时进程不得退出：已产生的数据照常送达，随后是 PanicError 块。
func TestToChannelGeneratorPanicDoesNotKillProcess(t *testing.T) {
	panictest.Run(t, func(t *testing.T) {
		gen := Generator[int](func(yield func(int, error) bool) {
			if yield(1, nil) {
				panic("generator boom")
			}
		})
		var chunks []StreamChunk[int]
		for chunk := range ToChannel(context.Background(), gen, 1) {
			chunks = append(chunks, chunk)
		}
		require.Len(t, chunks, 2)
		require.Equal(t, 1, chunks[0].Data)
		requirePanicError(t, chunks[1].Error, "generator boom", "TestToChannelGeneratorPanicDoesNotKillProcess")
	})
}

// Batch 的 worker goroutine 中 invoker panic 时进程不得退出：该输入得到 PanicError，其他输入照常完成。
func TestBatchInvokerPanicDoesNotKillProcess(t *testing.T) {
	panictest.Run(t, func(t *testing.T) {
		outputs, err := NewBaseRunnable[int, int]().Batch(context.Background(), []int{1, 2, 3}, func(_ context.Context, in int) (int, error) {
			if in == 2 {
				panic("batch boom")
			}
			return in * 10, nil
		})
		requirePanicError(t, err, "batch boom", "TestBatchInvokerPanicDoesNotKillProcess")
		require.Equal(t, 10, outputs[0])
		require.Equal(t, 30, outputs[2])
	})
}

// panicStreamRunnable 的 Stream 同步 panic，模拟宿主实现的 Runnable。
type panicStreamRunnable struct{ *RunnableFunc[any, any] }

func (panicStreamRunnable) Stream(context.Context, any) (<-chan StreamChunk[any], error) {
	panic("stream boom")
}

// Pipe 与 Sequence 在 goroutine 中调用下游 Stream：panic 转为流错误，进程不得退出。
func TestPipeAndSequenceStreamPanicDoesNotKillProcess(t *testing.T) {
	panictest.Run(t, func(t *testing.T) {
		identity := NewRunnableFunc(func(_ context.Context, in any) (any, error) { return in, nil })
		bad := panicStreamRunnable{RunnableFunc: identity}
		for name, r := range map[string]Runnable[any, any]{
			"pipe":     NewRunnablePipe[any, any, any](identity, bad),
			"sequence": NewRunnableSequence(identity, bad),
		} {
			stream, err := r.Stream(context.Background(), 1)
			require.NoError(t, err, name)
			var last StreamChunk[any]
			for chunk := range stream {
				last = chunk
			}
			requirePanicError(t, last.Error, "stream boom", "panicStreamRunnable.Stream")
			require.False(t, errors.Is(last.Error, context.Canceled), name)
		}
	})
}
