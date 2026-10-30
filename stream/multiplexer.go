package stream

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"

	"github.com/google/uuid"

	"github.com/kart-io/goagent/core"
	agentErrors "github.com/kart-io/goagent/errors"
)

// Multiplexer 流多路复用器实现
//
// Multiplexer 允许多个消费者同时读取同一个流：
// - 广播数据到所有消费者
// - 独立的错误处理
// - 背压管理
type Multiplexer struct {
	ctx    context.Context
	cancel context.CancelFunc

	mu        sync.RWMutex
	consumers map[string]*consumerState
	opts      *core.StreamOptions

	running bool
	closed  bool
	// pending 是 OnStart 尚未返回、已占用 MaxConsumers 名额的消费者数，受 mu 保护
	pending int
	// run 是本次 Start 的运行状态，受 mu 保护；源结束、各消费者通道关闭后置 nil，此后加入的消费者留给下一次 Start
	run *multiplexerRun
}

// multiplexerRun 记录一次 Start 的处理协程，运行中加入的消费者也登记到这里，Start 等它们全部处理完才返回。
type multiplexerRun struct {
	ctx    context.Context
	wg     sync.WaitGroup
	states []*consumerState
}

// consumerState 消费者状态
type consumerState struct {
	id       string
	consumer core.StreamConsumer
	ch       chan *core.LegacyStreamChunk
	// errors 由广播协程（丢块）与处理协程（回调报错）累加、Stats 并发读取，因此用原子计数
	errors atomic.Int64
	active bool

	// 以下字段受 Multiplexer.mu 保护：chClosed 表示 ch 已关闭；finishing 表示 ch 由 Start 在源结束时关闭，
	// 处理完缓冲数据后以 finishErr 收尾
	chClosed  bool
	finishing bool
	finishErr error

	// detached 表示已被 RemoveConsumer/Close 移除；处理协程每次回调前检查，置位后不再回调，包括缓冲数据与结束回调
	detached atomic.Bool

	// 以下字段只由该消费者的处理协程读写：每个消费者至多有一个处理协程，OnStart 在它启动前返回，
	// 回调因此天然串行，panicked 置位后的回调一律跳过
	panicked bool
	// panicErr 记录处理协程中回调的 panic，由 Start 在等待处理协程结束后读取并返回
	panicErr error
}

// NewMultiplexer 创建新的多路复用器
func NewMultiplexer(ctx context.Context, opts *core.StreamOptions) *Multiplexer {
	if opts == nil {
		opts = core.DefaultStreamOptions()
	}

	// Create context with optional timeout
	var cancel context.CancelFunc
	if opts.StreamTimeout > 0 {
		ctx, cancel = context.WithTimeout(ctx, opts.StreamTimeout)
	} else {
		ctx, cancel = context.WithCancel(ctx)
	}

	return &Multiplexer{
		ctx:       ctx,
		cancel:    cancel,
		consumers: make(map[string]*consumerState),
		opts:      opts,
	}
}

// AddConsumer 添加消费者
//
// OnStart 在锁外调用，其中可以调用多路复用器的方法；OnStart 返回后消费者才被加入，期间多路复用器被关闭则返回错误。
// Start 运行中加入的消费者从加入时起接收后续数据，源结束时与其他消费者一样在处理完缓冲数据后收到 OnError/OnComplete，
// Start 等它处理完才返回；源结束之后（即使 Start 尚未返回）加入的消费者不属于本次运行，由下一次 Start 处理。
func (m *Multiplexer) AddConsumer(consumer core.StreamConsumer) (string, error) {
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return "", errMultiplexerClosed()
	}

	// 检查消费者数量限制；OnStart 期间先占用名额，避免并发加入超过上限
	if current := len(m.consumers) + m.pending; m.opts.MaxConsumers > 0 && current >= m.opts.MaxConsumers {
		m.mu.Unlock()
		return "", agentErrors.New(agentErrors.CodeInvalidConfig, "max consumers limit reached").
			WithComponent("stream_multiplexer").
			WithOperation("AddConsumer").
			WithContext("max_consumers", m.opts.MaxConsumers).
			WithContext("current_consumers", current)
	}
	m.pending++
	m.mu.Unlock()

	id := uuid.New().String()

	// 通知消费者开始；OnStart 自身 panic 时返回 PanicError，语义同 core.NotifyCallbacks
	panicked, err := core.CallSafely(m.ctx, "stream_multiplexer", "on_start", consumer.OnStart)

	m.mu.Lock()
	defer m.mu.Unlock()
	m.pending--
	if err != nil {
		if panicked {
			return "", err
		}
		return "", agentErrors.Wrap(err, agentErrors.CodeStreamWrite, "consumer OnStart failed").
			WithComponent("stream_multiplexer").
			WithOperation("AddConsumer").
			WithContext("consumer_id", id)
	}
	if m.closed {
		return "", errMultiplexerClosed()
	}

	state := &consumerState{
		id:       id,
		consumer: consumer,
		ch:       make(chan *core.LegacyStreamChunk, m.opts.BufferSize),
		active:   true,
	}
	m.consumers[id] = state

	// 持锁登记到运行中的 Start：与源结束时关闭通道互斥，要么在关闭前登记并随其他消费者收尾，要么留给下一次 Start
	if m.run != nil {
		m.attachConsumer(m.run, state)
	}

	return id, nil
}

func errMultiplexerClosed() error {
	return agentErrors.New(agentErrors.CodeStreamWrite, "multiplexer is closed").
		WithComponent("stream_multiplexer").
		WithOperation("AddConsumer")
}

// RemoveConsumer 移除消费者
//
// 被移除的消费者不再收到任何回调，包括已缓冲的数据与 OnError/OnComplete；可以在该消费者自己的回调中调用。
// 从其他协程调用时，调用前已开始的那次回调不受影响。
func (m *Multiplexer) RemoveConsumer(id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	state, exists := m.consumers[id]
	if !exists {
		return agentErrors.New(agentErrors.CodeStreamWrite, "consumer not found").
			WithComponent("stream_multiplexer").
			WithOperation("RemoveConsumer").
			WithContext("consumer_id", id)
	}

	state.active = false
	state.detached.Store(true)
	closeConsumerCh(state)
	delete(m.consumers, id)

	return nil
}

// Consumers 返回所有消费者 ID
func (m *Multiplexer) Consumers() []string {
	m.mu.RLock()
	defer m.mu.RUnlock()

	ids := make([]string, 0, len(m.consumers))
	for id := range m.consumers {
		ids = append(ids, id)
	}

	return ids
}

// Start 开始多路复用
//
// 源结束（返回错误，包括 EOF）时关闭各消费者通道，每个消费者处理完已缓冲的数据后依次收到 OnError(该错误) 与 OnComplete，
// 全部处理完 Start 才返回；ctx 结束时不再处理缓冲数据，消费者收到 OnError(ctx 的错误) 与 OnComplete 后 Start 返回。
// 消费者回调 panic 时只停止向该消费者投递，其他消费者照常接收；全部处理结束后返回 OnChunk 的 panic 的 PanicError，
// OnError/OnComplete 的 panic 只交给全局 PanicHandler 记录。
func (m *Multiplexer) Start(ctx context.Context, source core.StreamOutput) error {
	m.mu.Lock()
	if m.running {
		m.mu.Unlock()
		return agentErrors.New(agentErrors.CodeStreamWrite, "multiplexer already running").
			WithComponent("stream_multiplexer").
			WithOperation("Start")
	}
	m.running = true

	// 启动每个消费者的处理协程；通道已关闭的消费者已结束，不再处理
	run := &multiplexerRun{ctx: ctx, states: make([]*consumerState, 0, len(m.consumers))}
	for _, state := range m.consumers {
		if state.chClosed {
			continue
		}
		m.attachConsumer(run, state)
	}
	m.run = run
	m.mu.Unlock()

	defer func() {
		m.mu.Lock()
		m.running = false
		m.mu.Unlock()
	}()

	// 从源读取并广播，直到源结束或 ctx 结束
	var endErr error
	for {
		if endErr = ctx.Err(); endErr != nil {
			break
		}
		if endErr = m.ctx.Err(); endErr != nil {
			break
		}

		chunk, err := source.Next()
		if err != nil {
			endErr = err
			break
		}

		// 广播数据块
		if err := m.broadcast(chunk); err != nil {
			endErr = err
			break
		}
	}

	// 关闭消费者通道：结束信号排在已缓冲的数据之后，由 processConsumer 处理完数据后收尾
	m.mu.Lock()
	m.run = nil
	states := run.states
	for _, state := range states {
		if !state.chClosed {
			state.finishing = true
			state.finishErr = endErr
			closeConsumerCh(state)
		}
	}
	m.mu.Unlock()

	// 等待所有消费者处理完成；m.run 已置 nil，不会再有 wg.Add
	run.wg.Wait()

	var panics []error
	for _, state := range states {
		if state.panicErr != nil {
			panics = append(panics, state.panicErr)
		}
	}
	return errors.Join(panics...)
}

// Close 关闭多路复用器
//
// 被关闭的消费者不再收到任何回调，包括 OnError/OnComplete。
func (m *Multiplexer) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return agentErrors.New(agentErrors.CodeStreamWrite, "multiplexer already closed").
			WithComponent("stream_multiplexer").
			WithOperation("Close")
	}

	m.closed = true

	// 先标记移除再取消 ctx：处理协程看到 ctx 结束时一定也能看到 detached，因此不会给它们收尾
	for _, state := range m.consumers {
		state.active = false
		state.detached.Store(true)
		closeConsumerCh(state)
	}
	m.cancel()

	m.consumers = make(map[string]*consumerState)

	return nil
}

// attachConsumer 为消费者启动本次运行的处理协程；调用方须持有 m.mu。
func (m *Multiplexer) attachConsumer(run *multiplexerRun, state *consumerState) {
	run.states = append(run.states, state)
	run.wg.Add(1)
	go m.processConsumer(run.ctx, state, &run.wg)
}

// closeConsumerCh 关闭消费者通道，可重复调用；调用方须持有 m.mu。
func closeConsumerCh(state *consumerState) {
	if !state.chClosed {
		state.chClosed = true
		close(state.ch)
	}
}

// broadcast 广播数据块到所有消费者
func (m *Multiplexer) broadcast(chunk *core.LegacyStreamChunk) error {
	m.mu.RLock()
	defer m.mu.RUnlock()

	for _, state := range m.consumers {
		if !state.active || state.chClosed {
			continue
		}

		select {
		case state.ch <- chunk:
		case <-m.ctx.Done():
			return m.ctx.Err()
		default:
			// 通道已满，跳过此消费者（背压处理）
			if m.opts.EnableBackpressure {
				state.errors.Add(1)
			}
		}
	}

	return nil
}

// processConsumer 处理单个消费者：按顺序处理通道中的数据块，通道由 Start 关闭或 ctx 结束时调用结束回调后退出，
// 因此同一消费者的结束回调总在它已处理的数据块之后。
func (m *Multiplexer) processConsumer(ctx context.Context, state *consumerState, wg *sync.WaitGroup) {
	defer wg.Done()

	for {
		select {
		case chunk, ok := <-state.ch:
			if !ok {
				// 通道关闭先于接收发生，finishing/finishErr 在关闭前写入，这里无需加锁
				if state.finishing {
					m.finishConsumer(ctx, state, state.finishErr)
				}
				return
			}

			panicked, err := m.callConsumer(ctx, state, "consume", func() error {
				if err := state.consumer.OnChunk(chunk); err != nil {
					state.errors.Add(1)
					// OnChunk 中可能已把自己移除
					if state.detached.Load() {
						return nil
					}
					if err := state.consumer.OnError(err); err != nil {
						fmt.Printf("failed to broadcast error: %v", err)
					}
				}
				return nil
			})
			if panicked {
				state.panicErr = err
				return
			}

		case <-ctx.Done():
			m.finishConsumer(ctx, state, ctx.Err())
			return

		case <-m.ctx.Done():
			m.finishConsumer(ctx, state, m.ctx.Err())
			return
		}
	}
}

// finishConsumer 依次调用 OnError(err)（err 非 nil 时）与 OnComplete；panic 没有调用方接收，只由全局 PanicHandler 记录。
func (m *Multiplexer) finishConsumer(ctx context.Context, state *consumerState, err error) {
	if err != nil {
		if panicked, cbErr := m.callConsumer(ctx, state, "on_error", func() error { return state.consumer.OnError(err) }); cbErr != nil && !panicked {
			fmt.Printf("failed to broadcast error: %v", cbErr)
		}
	}
	if panicked, cbErr := m.callConsumer(ctx, state, "on_complete", state.consumer.OnComplete); cbErr != nil && !panicked {
		fmt.Printf("failed to broadcast complete: %v", cbErr)
	}
}

// callConsumer 调用消费者回调，只在该消费者的处理协程中调用；消费者已 panic 或已被移除时跳过。
// 回调 panic 时将其标记为 panicked 与非活跃并返回 PanicError，同一协程上之后的回调因此全部跳过。
func (m *Multiplexer) callConsumer(ctx context.Context, state *consumerState, operation string, call func() error) (bool, error) {
	if state.panicked || state.detached.Load() {
		return false, nil
	}
	panicked, err := core.CallSafely(ctx, "stream_multiplexer", operation, call)
	if panicked {
		state.panicked = true
		m.mu.Lock()
		state.active = false
		m.mu.Unlock()
	}
	return panicked, err
}

// Stats 获取多路复用器统计信息
func (m *Multiplexer) Stats() MultiplexerStats {
	m.mu.RLock()
	defer m.mu.RUnlock()

	stats := MultiplexerStats{
		ConsumerCount: len(m.consumers),
		Running:       m.running,
	}

	for _, state := range m.consumers {
		if state.active {
			stats.ActiveConsumers++
		}
		stats.TotalErrors += int(state.errors.Load())
	}

	return stats
}

// MultiplexerStats 多路复用器统计信息
type MultiplexerStats struct {
	ConsumerCount   int
	ActiveConsumers int
	TotalErrors     int
	Running         bool
}
