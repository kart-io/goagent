package observe

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type asyncSink func(context.Context, Event)

func (f asyncSink) Observe(ctx context.Context, e Event) { f(ctx, e) }
func TestAsyncObserverBoundedQueueAndCancelableClose(t *testing.T) {
	entered := make(chan struct{})
	release := make(chan struct{})
	var calls atomic.Int32
	sink := asyncSink(func(ctx context.Context, e Event) { calls.Add(1); close(entered); <-release })
	a, err := NewAsyncObserver(sink, 1, 1)
	require.NoError(t, err)
	a.Observe(context.Background(), Event{})
	<-entered
	a.Observe(context.Background(), Event{})
	a.Observe(context.Background(), Event{})
	require.EqualValues(t, 1, a.Dropped())
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	require.ErrorIs(t, a.Close(ctx), context.Canceled)
	a.Observe(context.Background(), Event{})
	require.EqualValues(t, 2, a.Dropped())
	close(release)
	wait, cancelWait := context.WithTimeout(context.Background(), time.Second)
	defer cancelWait()
	require.NoError(t, a.Close(wait))
	require.EqualValues(t, 1, calls.Load())
	require.EqualValues(t, 3, a.Dropped())
}
func TestAsyncObserverIsolatesPanicAndDrainsOnClose(t *testing.T) {
	var calls atomic.Int32
	a, err := NewAsyncObserver(asyncSink(func(context.Context, Event) {
		if calls.Add(1) == 1 {
			panic("sink")
		}
	}), 4, 1)
	require.NoError(t, err)
	a.Observe(context.Background(), Event{})
	a.Observe(context.Background(), Event{})
	require.NoError(t, a.Close(context.Background()))
	require.EqualValues(t, 2, calls.Load())
	require.Zero(t, a.Dropped())
}
func TestAsyncObserverCloseCancelsSinkAndPreservesValues(t *testing.T) {
	type key struct{}
	entered := make(chan struct{})
	got := make(chan any, 1)
	a, err := NewAsyncObserver(asyncSink(func(ctx context.Context, e Event) { got <- ctx.Value(key{}); close(entered); <-ctx.Done() }), 1, 1)
	require.NoError(t, err)
	a.Observe(context.WithValue(context.Background(), key{}, "trace"), Event{})
	<-entered
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	require.ErrorIs(t, a.Close(ctx), context.Canceled)
	wait, cancelWait := context.WithTimeout(context.Background(), time.Second)
	defer cancelWait()
	require.NoError(t, a.Close(wait))
	require.Equal(t, "trace", <-got)
}
func TestAsyncObserverRejectsInvalidConfiguration(t *testing.T) {
	_, err := NewAsyncObserver(nil, 1, 1)
	require.Error(t, err)
	sink := asyncSink(func(context.Context, Event) {})
	_, err = NewAsyncObserver(sink, 0, 1)
	require.Error(t, err)
	_, err = NewAsyncObserver(sink, 1, 0)
	require.Error(t, err)
}

func TestAsyncObserverUsesFixedWorkerLimit(t *testing.T) {
	entered := make(chan struct{}, 2)
	release := make(chan struct{})
	var active, maxActive, calls atomic.Int32
	a, err := NewAsyncObserver(asyncSink(func(context.Context, Event) {
		calls.Add(1)
		n := active.Add(1)
		defer active.Add(-1)
		for old := maxActive.Load(); n > old; old = maxActive.Load() {
			if maxActive.CompareAndSwap(old, n) {
				break
			}
		}
		select {
		case entered <- struct{}{}:
		default:
		}
		<-release
	}), 4, 2)
	require.NoError(t, err)
	a.Observe(context.Background(), Event{})
	a.Observe(context.Background(), Event{})
	<-entered
	<-entered
	for i := 0; i < 5; i++ {
		a.Observe(context.Background(), Event{})
	}
	require.EqualValues(t, 1, a.Dropped())
	require.EqualValues(t, 2, active.Load())
	close(release)
	require.NoError(t, a.Close(context.Background()))
	require.EqualValues(t, 6, calls.Load())
	require.EqualValues(t, 2, maxActive.Load())
}
