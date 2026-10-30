package flow

import (
	"context"
	"errors"
	"github.com/kart-io/goagent/core"
	"github.com/stretchr/testify/require"
	"testing"
)

func sumFold() Fold[int, int] {
	return Fold[int, int]{Init: func() int { return 0 }, Add: func(_ context.Context, a, b int) (int, error) { return a + b, nil }}
}
func TestIncrementalBeforeSourceReturns(t *testing.T) {
	first, release := make(chan struct{}), make(chan struct{})
	r, err := AsRunnable(func(context.Context, int) (Source[int], error) {
		return func(ctx context.Context, yield func(int) error) error {
			if err := yield(0); err != nil {
				return err
			}
			close(first)
			select {
			case <-release:
				return yield(2)
			case <-ctx.Done():
				return ctx.Err()
			}
		}, nil
	}, sumFold(), func(n int) (*core.StreamEvent, error) { return &core.StreamEvent{Type: "data", Delta: "preview"}, nil }, Limits[int]{})
	require.NoError(t, err)
	stream, err := r.Stream(context.Background(), 0)
	require.NoError(t, err)
	chunk := <-stream
	require.False(t, chunk.Done)
	require.NotNil(t, chunk.Event)
	<-first
	close(release)
	final := 0
	for c := range stream {
		require.NoError(t, c.Error)
		if c.Done {
			final = c.Data
		}
	}
	require.Equal(t, 2, final)
}
func TestMapAndFoldErrorsCannotBeSwallowed(t *testing.T) {
	sentinel := errors.New("late failure")
	calls := 0
	source := Source[int](func(ctx context.Context, y func(int) error) error { _ = y(0); _ = y(1); return nil })
	mapped := Map(source, func(_ context.Context, n int) (int, error) { calls++; return n, sentinel })
	r, err := AsRunnable(func(context.Context, int) (Source[int], error) { return mapped, nil }, sumFold(), nil, Limits[int]{})
	require.NoError(t, err)
	_, err = r.Invoke(context.Background(), 0)
	require.ErrorIs(t, err, sentinel)
	require.Equal(t, 1, calls)
	fold := sumFold()
	fold.Add = func(context.Context, int, int) (int, error) { return 0, sentinel }
	r, err = AsRunnable(func(context.Context, int) (Source[int], error) { return source, nil }, fold, nil, Limits[int]{})
	require.NoError(t, err)
	_, err = r.Invoke(context.Background(), 0)
	require.ErrorIs(t, err, sentinel)
}
func TestSourceCancellationAndLimits(t *testing.T) {
	source := Source[int](func(ctx context.Context, y func(int) error) error {
		for range 1025 {
			if err := y(0); err != nil {
				return err
			}
		}
		return nil
	})
	r, err := AsRunnable(func(context.Context, int) (Source[int], error) { return source, nil }, sumFold(), nil, Limits[int]{})
	require.NoError(t, err)
	_, err = r.Invoke(context.Background(), 0)
	require.ErrorContains(t, err, "item limit")
	_, err = AsRunnable(func(context.Context, int) (Source[int], error) { return source, nil }, sumFold(), nil, Limits[int]{MaxBytes: 1})
	require.Error(t, err)
	r, err = AsRunnable(func(context.Context, int) (Source[int], error) { return source, nil }, sumFold(), nil, Limits[int]{MaxBytes: 1, Size: func(int) int64 { return -1 }})
	require.NoError(t, err)
	_, err = r.Invoke(context.Background(), 0)
	require.ErrorContains(t, err, "negative")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = r.Invoke(ctx, 0)
	require.ErrorIs(t, err, context.Canceled)
}
func BenchmarkFold(b *testing.B) {
	r, _ := AsRunnable(func(context.Context, int) (Source[int], error) {
		return func(_ context.Context, y func(int) error) error {
			for i := range 32 {
				if err := y(i); err != nil {
					return err
				}
			}
			return nil
		}, nil
	}, sumFold(), nil, Limits[int]{})
	b.ReportAllocs()
	for b.Loop() {
		_, _ = r.Invoke(context.Background(), 0)
	}
}

func TestConcurrentFoldsOwnTheirAccumulator(t *testing.T) {
	r, err := AsRunnable(func(_ context.Context, n int) (Source[int], error) {
		return func(_ context.Context, y func(int) error) error { return y(n) }, nil
	}, Fold[int, *int]{Init: func() *int { return new(int) }, Add: func(_ context.Context, a *int, b int) (*int, error) { *a += b; return a, nil }}, nil, Limits[int]{})
	require.NoError(t, err)
	outputs := make(chan *int, 2)
	for range 2 {
		go func() {
			out, e := r.Invoke(context.Background(), 1)
			if e != nil {
				outputs <- nil
				return
			}
			outputs <- out
		}()
	}
	a, b := <-outputs, <-outputs
	require.NotNil(t, a)
	require.NotNil(t, b)
	require.NotSame(t, a, b)
	require.Equal(t, 1, *a)
	require.Equal(t, 1, *b)
}
