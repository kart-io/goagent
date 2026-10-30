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

type externalStreamRunnable struct {
	*RunnableFunc[any, any]
	stream func(context.Context, any) (<-chan StreamChunk[any], error)
}

func (r externalStreamRunnable) Stream(ctx context.Context, in any) (<-chan StreamChunk[any], error) {
	return r.stream(ctx, in)
}
func compositionIdentity() *RunnableFunc[any, any] {
	return NewRunnableFunc(func(_ context.Context, in any) (any, error) { return in, nil })
}
func compositions(last Runnable[any, any]) map[string]Runnable[any, any] {
	return map[string]Runnable[any, any]{"pipe": NewRunnablePipe[any, any, any](compositionIdentity(), last), "sequence": NewRunnableSequence(compositionIdentity(), last)}
}
func TestCompositionRejectsMalformedStreams(t *testing.T) {
	failure := errors.New("late failure")
	cases := map[string][]StreamChunk[any]{
		"missing":    {{Data: "partial"}},
		"duplicate":  {{Done: true, Data: "final"}, {Done: true}},
		"late error": {{Done: true, Data: "final"}, {Error: failure}},
		"late event": {{Done: true, Data: "final"}, {Data: "late"}},
	}
	for scenario, chunks := range cases {
		t.Run(scenario, func(t *testing.T) {
			downstream := externalStreamRunnable{RunnableFunc: compositionIdentity(), stream: func(context.Context, any) (<-chan StreamChunk[any], error) {
				ch := make(chan StreamChunk[any], len(chunks))
				for _, chunk := range chunks {
					ch <- chunk
				}
				close(ch)
				return ch, nil
			}}
			for name, r := range compositions(downstream) {
				t.Run(name, func(t *testing.T) {
					cb := &streamCallback{BaseCallback: NewBaseCallback()}
					stream, err := r.WithCallbacks(cb).Stream(context.Background(), "input")
					require.NoError(t, err)
					var terminal []StreamChunk[any]
					for chunk := range stream {
						if chunk.Done || chunk.Error != nil {
							terminal = append(terminal, chunk)
						}
					}
					require.Len(t, terminal, 1)
					require.True(t, terminal[0].Done)
					require.Error(t, terminal[0].Error)
					if scenario == "missing" {
						require.ErrorIs(t, terminal[0].Error, io.ErrUnexpectedEOF)
					}
					if scenario == "late error" {
						require.ErrorIs(t, terminal[0].Error, failure)
					}
					require.EqualValues(t, 1, cb.starts.Load())
					require.EqualValues(t, 0, cb.ends.Load())
					require.EqualValues(t, 1, cb.failures.Load())
				})
			}
		})
	}
}
func TestEmptySequenceStreamsIdentity(t *testing.T) {
	stream, err := NewRunnableSequence().Stream(context.Background(), "identity")
	require.NoError(t, err)
	var chunks []StreamChunk[any]
	for chunk := range stream {
		chunks = append(chunks, chunk)
	}
	require.Equal(t, []StreamChunk[any]{{Data: "identity", Done: true}}, chunks)
}
func TestCompositionCallbacksRemainIndependent(t *testing.T) {
	for name, r := range compositions(compositionIdentity()) {
		t.Run(name, func(t *testing.T) {
			parent := &streamCallback{BaseCallback: NewBaseCallback()}
			configured := r.WithConfig(RunnableConfig{Callbacks: []Callback{parent}, MaxConcurrency: 1})
			result, err := configured.Invoke(context.Background(), "input")
			require.NoError(t, err)
			require.Equal(t, "input", result)
			stream, err := configured.Stream(context.Background(), "stream")
			require.NoError(t, err)
			for range stream {
			}
			result, err = r.Invoke(context.Background(), "original")
			require.NoError(t, err)
			require.Equal(t, "original", result)
			require.EqualValues(t, 2, parent.starts.Load())
			require.EqualValues(t, 2, parent.ends.Load())
		})
	}
}
func TestCompositionCallbackClonesDoNotShareBackingSlice(t *testing.T) {
	for name, r := range compositions(compositionIdentity()) {
		t.Run(name, func(t *testing.T) {
			original := &streamCallback{BaseCallback: NewBaseCallback()}
			a := &streamCallback{BaseCallback: NewBaseCallback()}
			b := &streamCallback{BaseCallback: NewBaseCallback()}
			callbacks := make([]Callback, 1, 4)
			callbacks[0] = original
			base := r.WithConfig(RunnableConfig{Callbacks: callbacks})
			left := base.WithCallbacks(a)
			_ = base.WithCallbacks(b)
			callbacks[0] = b
			_, err := left.Invoke(context.Background(), 1)
			require.NoError(t, err)
			require.EqualValues(t, 1, original.starts.Load())
			require.EqualValues(t, 1, a.starts.Load())
			require.EqualValues(t, 0, b.starts.Load())
		})
	}
}
func TestCompositionCancellationStopsDownstream(t *testing.T) {
	for _, name := range []string{"pipe", "sequence"} {
		t.Run(name, func(t *testing.T) {
			stopped := make(chan struct{})
			downstream := NewStreamRunnable(StreamModeTokens, func(ctx context.Context, in any, emit func(StreamChunk[any]) error) error {
				defer close(stopped)
				for {
					if err := emit(StreamChunk[any]{Data: in}); err != nil {
						return err
					}
				}
			})
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			stream, err := compositions(downstream)[name].Stream(ctx, 1)
			require.NoError(t, err)
			<-stream
			cancel()
			select {
			case <-stopped:
			case <-time.After(time.Second):
				t.Fatal("producer stuck after cancel")
			}
			for range stream {
			}
		})
	}
}

func TestCompositionBatchUsesConfiguredConcurrencyAndPreservesPartialResults(t *testing.T) {
	// Batch now executes each input through Invoke, bounded by parent MaxConcurrency.
	// Unlike stage-wide child Batch, failures preserve ordered partial results and
	// return the first observed error without preventing other inputs from running.
	failure := errors.New("bad input")
	for _, name := range []string{"pipe", "sequence"} {
		t.Run(name, func(t *testing.T) {
			var active, maxActive atomic.Int32
			child := &streamCallback{BaseCallback: NewBaseCallback()}
			downstream := NewRunnableFunc(func(_ context.Context, in any) (any, error) {
				n := active.Add(1)
				defer active.Add(-1)
				for old := maxActive.Load(); n > old; old = maxActive.Load() {
					if maxActive.CompareAndSwap(old, n) {
						break
					}
				}
				time.Sleep(time.Millisecond)
				if in == 2 {
					return nil, failure
				}
				return in, nil
			}).WithCallbacks(child)
			parent := &streamCallback{BaseCallback: NewBaseCallback()}
			r := compositions(downstream)[name].WithConfig(RunnableConfig{MaxConcurrency: 1, Callbacks: []Callback{parent}})
			out, err := r.Batch(context.Background(), []any{1, 2, 3})
			require.ErrorIs(t, err, failure)
			require.Equal(t, []any{1, nil, 3}, out)
			require.EqualValues(t, 1, maxActive.Load())
			for _, cb := range []*streamCallback{parent, child} {
				require.EqualValues(t, 3, cb.starts.Load())
				require.EqualValues(t, 2, cb.ends.Load())
				require.EqualValues(t, 1, cb.failures.Load())
			}
		})
	}
}
func TestCompositionAlreadyCanceledSkipsExecution(t *testing.T) {
	var calls atomic.Int32
	upstream := NewRunnableFunc(func(_ context.Context, in any) (any, error) { calls.Add(1); return in, nil })
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	for name, r := range map[string]Runnable[any, any]{"empty": NewRunnableSequence(), "pipe": NewRunnablePipe[any, any, any](upstream, upstream), "sequence": NewRunnableSequence(upstream)} {
		t.Run(name, func(t *testing.T) {
			cb := &streamCallback{BaseCallback: NewBaseCallback()}
			r = r.WithCallbacks(cb)
			_, err := r.Invoke(ctx, 1)
			require.ErrorIs(t, err, context.Canceled)
			stream, err := r.Stream(ctx, 1)
			require.ErrorIs(t, err, context.Canceled)
			require.Nil(t, stream)
			require.Zero(t, cb.starts.Load())
		})
	}
	require.Zero(t, calls.Load())
}
func TestCompositionUpstreamFailureSkipsDownstream(t *testing.T) {
	failure := errors.New("upstream")
	upstream := NewRunnableFunc(func(context.Context, any) (any, error) { return nil, failure })
	var calls atomic.Int32
	downstream := NewStreamRunnable(StreamModeFinal, func(_ context.Context, in any, emit func(StreamChunk[any]) error) error {
		calls.Add(1)
		return emit(StreamChunk[any]{Done: true, Data: in})
	})
	for name, r := range map[string]Runnable[any, any]{"pipe": NewRunnablePipe[any, any, any](upstream, downstream), "sequence": NewRunnableSequence(upstream, downstream)} {
		t.Run(name, func(t *testing.T) {
			cb := &streamCallback{BaseCallback: NewBaseCallback()}
			stream, err := r.WithCallbacks(cb).Stream(context.Background(), 1)
			require.NoError(t, err)
			var chunks []StreamChunk[any]
			for c := range stream {
				chunks = append(chunks, c)
			}
			require.Len(t, chunks, 1)
			require.True(t, chunks[0].Done)
			require.ErrorIs(t, chunks[0].Error, failure)
			require.EqualValues(t, 1, cb.starts.Load())
			require.EqualValues(t, 1, cb.failures.Load())
			require.Zero(t, cb.ends.Load())
		})
	}
	require.Zero(t, calls.Load())
}

func TestCompositionStreamsOnlyLastStageAndRunsIndependentCallbacks(t *testing.T) {
	for _, name := range []string{"pipe", "sequence"} {
		t.Run(name, func(t *testing.T) {
			child := &streamCallback{BaseCallback: NewBaseCallback()}
			upstream := externalStreamRunnable{RunnableFunc: NewRunnableFunc(func(_ context.Context, in any) (any, error) { return in.(int) + 1, nil }), stream: func(context.Context, any) (<-chan StreamChunk[any], error) {
				return nil, errors.New("upstream must Invoke")
			}}
			downstream := externalStreamRunnable{RunnableFunc: NewRunnableFunc(func(context.Context, any) (any, error) { return nil, errors.New("last stage must Stream") }), stream: func(ctx context.Context, in any) (<-chan StreamChunk[any], error) {
				r := NewStreamRunnable(StreamModeTokens, func(_ context.Context, in any, emit func(StreamChunk[any]) error) error {
					if err := emit(StreamChunk[any]{Event: &StreamEvent{Type: "text", Delta: "delta"}}); err != nil {
						return err
					}
					return emit(StreamChunk[any]{Data: in.(int) * 2, Done: true})
				}).WithCallbacks(child)
				return r.Stream(ctx, in)
			}}
			var r Runnable[any, any]
			if name == "pipe" {
				r = NewRunnablePipe[any, any, any](upstream, downstream)
			} else {
				r = NewRunnableSequence(upstream, downstream)
			}
			parent := &streamCallback{BaseCallback: NewBaseCallback()}
			stream, err := r.WithCallbacks(parent).Stream(context.Background(), 3)
			require.NoError(t, err)
			var chunks []StreamChunk[any]
			for c := range stream {
				chunks = append(chunks, c)
			}
			require.Len(t, chunks, 2)
			require.Equal(t, "delta", chunks[0].Event.Delta)
			require.Equal(t, 8, chunks[1].Data)
			require.True(t, chunks[1].Done)
			require.NoError(t, chunks[1].Error)
			for _, cb := range []*streamCallback{parent, child} {
				require.EqualValues(t, 1, cb.starts.Load())
				require.EqualValues(t, 1, cb.ends.Load())
				require.Zero(t, cb.failures.Load())
			}
		})
	}
}
func TestCompositionParentEndFailureSuppressesSuccess(t *testing.T) {
	failure := errors.New("parent rejects end")
	for name, r := range compositions(compositionIdentity()) {
		t.Run(name, func(t *testing.T) {
			parent := &streamCallback{BaseCallback: NewBaseCallback(), endErr: failure}
			stream, err := r.WithCallbacks(parent).Stream(context.Background(), 1)
			require.NoError(t, err)
			var chunks []StreamChunk[any]
			for c := range stream {
				chunks = append(chunks, c)
			}
			require.Len(t, chunks, 1)
			require.True(t, chunks[0].Done)
			require.ErrorIs(t, chunks[0].Error, failure)
			require.EqualValues(t, 1, parent.ends.Load())
			require.EqualValues(t, 1, parent.failures.Load())
		})
	}
}
