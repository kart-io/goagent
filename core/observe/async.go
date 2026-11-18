package observe

import (
	"context"
	"sync"
	"sync/atomic"

	agentErrors "github.com/kart-io/goagent/errors"
)

type queuedEvent struct {
	ctx   context.Context
	event Event
}

// AsyncObserver delivers observation events through a bounded queue and a
// fixed number of worker goroutines. Observe does not wait for the observer;
// Dropped is incremented when the queue overflows or an event arrives after
// Close. With more than one worker, the observer must support concurrent
// calls. A blocked observer only occupies its own worker, but cannot be forced
// to stop if it ignores context cancellation. Enqueued events keep context
// values but do not inherit the caller's cancellation. Close waits for enqueued
// events to be delivered; when its ctx is canceled it cancels the observer
// context and drops pending events. Close can be called again to wait for the
// workers to exit. The observer must not synchronously call Close on its own
// adapter.
type AsyncObserver struct {
	sink      Observer
	queue     chan queuedEvent
	mu        sync.Mutex
	closed    bool
	ctx       context.Context
	cancel    context.CancelFunc
	done      chan struct{}
	remaining atomic.Int32
	dropped   atomic.Uint64
}

// NewAsyncObserver requires an observer, and both the queue capacity and the
// number of workers must be positive.
func NewAsyncObserver(sink Observer, capacity, workers int) (*AsyncObserver, error) {
	if sink == nil || capacity <= 0 || workers <= 0 {
		return nil, agentErrors.Newf(agentErrors.CodeInvalidConfig, "async observer requires a sink and positive capacity/workers").WithComponent("async_observer").WithOperation("new_async_observer")
	}
	ctx, cancel := context.WithCancel(context.Background())
	a := &AsyncObserver{sink: sink, queue: make(chan queuedEvent, capacity), ctx: ctx, cancel: cancel, done: make(chan struct{})}
	a.remaining.Store(int32(workers))
	for i := 0; i < workers; i++ {
		go a.work()
	}
	return a, nil
}

func (a *AsyncObserver) Observe(ctx context.Context, event Event) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.closed {
		a.dropped.Add(1)
		return
	}
	select {
	case a.queue <- queuedEvent{ctx: context.WithoutCancel(ctx), event: cloneEvent(event)}:
	default:
		a.dropped.Add(1)
	}
}

// Dropped counts events lost to queue overflow, events received after Close,
// and pending events dropped on cancellation. Observer panics are isolated and
// not counted as queue drops.
func (a *AsyncObserver) Dropped() uint64 { return a.dropped.Load() }

func (a *AsyncObserver) work() {
	defer func() {
		if a.remaining.Add(-1) == 0 {
			a.cancel()
			close(a.done)
		}
	}()
	for item := range a.queue {
		if a.ctx.Err() != nil {
			a.dropped.Add(1)
			continue
		}
		a.deliver(item)
	}
}

type deliveryContext struct {
	context.Context
	values context.Context
}

func (c deliveryContext) Value(key any) any { return c.values.Value(key) }
func (a *AsyncObserver) deliver(item queuedEvent) {
	defer func() { _ = recover() }()
	a.sink.Observe(deliveryContext{Context: a.ctx, values: item.ctx}, item.event)
}

// Close stops accepting events immediately. When ctx is canceled it stops
// waiting and cancels the observer context, without creating extra goroutines
// to wait on blocked observers.
func (a *AsyncObserver) Close(ctx context.Context) error {
	a.mu.Lock()
	if !a.closed {
		a.closed = true
		close(a.queue)
	}
	a.mu.Unlock()
	if err := ctx.Err(); err != nil {
		a.cancel()
		return err
	}
	select {
	case <-a.done:
		return nil
	case <-ctx.Done():
		a.cancel()
		return ctx.Err()
	}
}
