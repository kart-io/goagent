package core

import (
	"context"
	"errors"
	"io"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type streamCallback struct {
	*BaseCallback
	starts, ends, failures atomic.Int32
	endErr                 error
}

func (c *streamCallback) OnStart(context.Context, any) error   { c.starts.Add(1); return nil }
func (c *streamCallback) OnEnd(context.Context, any) error     { c.ends.Add(1); return c.endErr }
func (c *streamCallback) OnError(context.Context, error) error { c.failures.Add(1); return nil }

func TestStreamRunnableExecutionContract(t *testing.T) {
	var calls atomic.Int32
	source := NewStreamRunnable(StreamModeTokens, func(ctx context.Context, input int, emit func(StreamChunk[int]) error) error {
		calls.Add(1)
		if err := emit(StreamChunk[int]{Event: &StreamEvent{Type: "text", Delta: "片段"}}); err != nil {
			return err
		}
		return emit(StreamChunk[int]{Done: true, Data: input * 2})
	})
	cb := &streamCallback{BaseCallback: NewBaseCallback()}
	r := source.WithConfig(RunnableConfig{MaxConcurrency: 2}).WithCallbacks(cb)
	output, err := r.Invoke(context.Background(), 3)
	require.NoError(t, err)
	require.Equal(t, 6, output)
	stream, err := r.Stream(context.Background(), 4)
	require.NoError(t, err)
	var chunks []StreamChunk[int]
	for c := range stream {
		chunks = append(chunks, c)
	}
	require.Len(t, chunks, 2)
	require.Equal(t, "片段", chunks[0].Event.Delta)
	require.True(t, chunks[1].Done)
	require.Equal(t, 8, chunks[1].Data)
	batch, err := r.Batch(context.Background(), []int{1, 2, 3})
	require.NoError(t, err)
	require.Equal(t, []int{2, 4, 6}, batch)
	pipe := r.Pipe(NewRunnableFunc(func(ctx context.Context, input int) (any, error) { return input + 1, nil }))
	outputAny, err := pipe.Invoke(context.Background(), 5)
	require.NoError(t, err)
	require.Equal(t, 11, outputAny)
	require.EqualValues(t, 6, calls.Load())
	require.EqualValues(t, 6, cb.starts.Load())
	require.EqualValues(t, 6, cb.ends.Load())
	require.EqualValues(t, 0, cb.failures.Load())
	require.Empty(t, source.GetConfig().Callbacks)
	require.Equal(t, StreamModeTokens, r.(StreamCapability).StreamMode())
}

func TestStreamRunnableRejectsFalseSuccess(t *testing.T) {
	failure := errors.New("执行失败")
	for _, scenario := range []string{"无最终块", "完成后失败", "重复完成", "块内错误", "忽略发送错误", "panic", "回调错误"} {
		t.Run(scenario, func(t *testing.T) {
			cb := &streamCallback{BaseCallback: NewBaseCallback()}
			r := NewStreamRunnable(StreamModeFinal, func(ctx context.Context, input int, emit func(StreamChunk[int]) error) error {
				switch scenario {
				case "无最终块":
					return nil
				case "完成后失败":
					_ = emit(StreamChunk[int]{Done: true, Data: 1})
					return failure
				case "重复完成":
					_ = emit(StreamChunk[int]{Done: true})
					return emit(StreamChunk[int]{Done: true})
				case "块内错误":
					return emit(StreamChunk[int]{Error: failure})
				case "忽略发送错误":
					_ = emit(StreamChunk[int]{Error: failure})
					_ = emit(StreamChunk[int]{Done: true})
					return nil
				case "panic":
					panic("执行崩溃")
				default:
					return emit(StreamChunk[int]{Done: true})
				}
			})
			if scenario == "回调错误" {
				cb.endErr = failure
			}
			stream, err := r.WithCallbacks(cb).Stream(context.Background(), 0)
			require.NoError(t, err)
			var chunks []StreamChunk[int]
			for c := range stream {
				chunks = append(chunks, c)
			}
			require.Len(t, chunks, 1)
			require.True(t, chunks[0].Done)
			require.Error(t, chunks[0].Error)
			if scenario == "无最终块" {
				require.ErrorIs(t, chunks[0].Error, io.ErrUnexpectedEOF)
			}
			require.EqualValues(t, 1, cb.failures.Load())
		})
	}
}

func TestStreamRunnableCancellationStopsProducer(t *testing.T) {
	stopped := make(chan struct{})
	r := NewStreamRunnable(StreamModeTokens, func(ctx context.Context, input int, emit func(StreamChunk[int]) error) error {
		defer close(stopped)
		for {
			if err := emit(StreamChunk[int]{Data: 1}); err != nil {
				return err
			}
		}
	})
	ctx, cancel := context.WithCancel(context.Background())
	stream, err := r.Stream(ctx, 0)
	require.NoError(t, err)
	<-stream
	cancel()
	select {
	case <-stopped:
	case <-time.After(time.Second):
		t.Fatal("取消后生产者未退出")
	}
	for range stream {
	}
	_, err = r.Invoke(ctx, 0)
	require.ErrorIs(t, err, context.Canceled)
}
