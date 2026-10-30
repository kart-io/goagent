package observe

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
)

type queuedEvent struct {
	ctx   context.Context
	event Event
}

// AsyncObserver 通过有界队列和固定数量的工作协程交付观察事件。
// Observe 不等待观察者；队列溢出或 Close 后收到事件时增加 Dropped。
// 工作协程多于一个时，观察者必须支持并发调用。阻塞的观察者只占用其工作协程，
// 但若忽略上下文取消，则无法强制停止。
// 已入队事件保留上下文值，不继承调用者的取消信号。
// Close 等待已入队事件交付完毕；其 ctx 取消时会取消观察者上下文，并丢弃待交付事件。
// 可以再次调用 Close 等待工作协程退出。观察者内部不得同步调用自身适配器的 Close。
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

// NewAsyncObserver 要求提供观察者，队列容量和工作协程数量均须为正数。
func NewAsyncObserver(sink Observer, capacity, workers int) (*AsyncObserver, error) {
	if sink == nil || capacity <= 0 || workers <= 0 {
		return nil, fmt.Errorf("async observer requires a sink and positive capacity/workers")
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
	case a.queue <- queuedEvent{ctx: context.WithoutCancel(ctx), event: event}:
	default:
		a.dropped.Add(1)
	}
}

// Dropped 统计队列溢出、Close 后收到的事件，以及取消时丢弃的待交付事件。
// 观察者的 panic 会被隔离，不计入队列丢弃数量。
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

// Close 立即停止接收事件。ctx 取消时结束等待并取消观察者上下文，
// 不会为等待阻塞的观察者而创建额外协程。
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
