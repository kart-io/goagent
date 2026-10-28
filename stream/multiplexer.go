package stream

import (
	"context"
	"errors"
	"fmt"
	"sync"

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
}

// consumerState 消费者状态
type consumerState struct {
	id       string
	consumer core.StreamConsumer
	ch       chan *core.LegacyStreamChunk
	errors   int
	active   bool

	// 以下字段受 Multiplexer.mu 保护：chClosed 表示 ch 已关闭；detached 表示已被 RemoveConsumer/Close 移除，
	// 不再收到任何回调；finishing 表示 ch 由 Start 在源结束时关闭，处理完缓冲数据后以 finishErr 收尾
	chClosed  bool
	detached  bool
	finishing bool
	finishErr error

	// callMu 串行化对该消费者的回调；panicked 在其保护下置位，之后的回调一律跳过
	callMu   sync.Mutex
	panicked bool
	// panicErr 记录处理协程中回调的 panic，由 Start 返回
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
func (m *Multiplexer) AddConsumer(consumer core.StreamConsumer) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.closed {
		return "", agentErrors.New(agentErrors.CodeStreamWrite, "multiplexer is closed").
			WithComponent("stream_multiplexer").
			WithOperation("AddConsumer")
	}

	// 检查消费者数量限制
	if m.opts.MaxConsumers > 0 && len(m.consumers) >= m.opts.MaxConsumers {
		return "", agentErrors.New(agentErrors.CodeInvalidConfig, "max consumers limit reached").
			WithComponent("stream_multiplexer").
			WithOperation("AddConsumer").
			WithContext("max_consumers", m.opts.MaxConsumers).
			WithContext("current_consumers", len(m.consumers))
	}

	id := uuid.New().String()
	state := &consumerState{
		id:       id,
		consumer: consumer,
		ch:       make(chan *core.LegacyStreamChunk, m.opts.BufferSize),
		active:   true,
	}

	m.consumers[id] = state

	// 通知消费者开始；OnStart 自身 panic 时返回 PanicError，语义同 core.NotifyCallbacks
	if panicked, err := core.CallSafely(m.ctx, "stream_multiplexer", "on_start", consumer.OnStart); err != nil {
		delete(m.consumers, id)
		if panicked {
			return "", err
		}
		return "", agentErrors.Wrap(err, agentErrors.CodeStreamWrite, "consumer OnStart failed").
			WithComponent("stream_multiplexer").
			WithOperation("AddConsumer").
			WithContext("consumer_id", id)
	}

	return id, nil
}

// RemoveConsumer 移除消费者
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
	state.detached = true
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
	var wg sync.WaitGroup
	states := make([]*consumerState, 0, len(m.consumers))
	for _, state := range m.consumers {
		if state.chClosed {
			continue
		}
		states = append(states, state)
		wg.Add(1)
		go m.processConsumer(ctx, state, &wg)
	}
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
	for _, state := range states {
		if !state.chClosed {
			state.finishing = true
			state.finishErr = endErr
			closeConsumerCh(state)
		}
	}
	m.mu.Unlock()

	// 等待所有消费者处理完成
	wg.Wait()

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

	// 先在锁内标记移除再取消 ctx：processConsumer 看到 ctx 结束后需要取锁检查 detached，因此不会给它们收尾
	for _, state := range m.consumers {
		state.active = false
		state.detached = true
		closeConsumerCh(state)
	}
	m.cancel()

	m.consumers = make(map[string]*consumerState)

	return nil
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
				state.errors++
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
					state.errors++
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
			m.finishCanceledConsumer(ctx, state, ctx.Err())
			return

		case <-m.ctx.Done():
			m.finishCanceledConsumer(ctx, state, m.ctx.Err())
			return
		}
	}
}

// finishCanceledConsumer 在 ctx 结束时为消费者收尾；已被 RemoveConsumer/Close 移除的消费者不再回调。
func (m *Multiplexer) finishCanceledConsumer(ctx context.Context, state *consumerState, err error) {
	m.mu.RLock()
	detached := state.detached
	m.mu.RUnlock()
	if !detached {
		m.finishConsumer(ctx, state, err)
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

// callConsumer 在 callMu 下调用消费者回调，消费者已 panic 过则跳过。
// 回调 panic 时在释放 callMu 之前将其标记为 panicked 与非活跃，返回 PanicError；
// 已排队等待 callMu 的完成/错误通知因此看到标记而跳过，不存在 panic 之后仍被回调的窗口。
func (m *Multiplexer) callConsumer(ctx context.Context, state *consumerState, operation string, call func() error) (bool, error) {
	state.callMu.Lock()
	defer state.callMu.Unlock()
	if state.panicked {
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
		stats.TotalErrors += state.errors
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
