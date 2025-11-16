package stream

import (
	"context"
	"fmt"
	"sync"

	"github.com/google/uuid"

	"github.com/kart-io/goagent/core"
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
	wg      sync.WaitGroup // Track all goroutines
}

// consumerState 消费者状态
type consumerState struct {
	id       string
	consumer core.StreamConsumer
	ch       chan *core.LegacyStreamChunk
	errors   int
	active   bool
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
		return "", fmt.Errorf("multiplexer is closed")
	}

	// 检查消费者数量限制
	if m.opts.MaxConsumers > 0 && len(m.consumers) >= m.opts.MaxConsumers {
		return "", fmt.Errorf("max consumers limit reached: %d", m.opts.MaxConsumers)
	}

	id := uuid.New().String()
	state := &consumerState{
		id:       id,
		consumer: consumer,
		ch:       make(chan *core.LegacyStreamChunk, m.opts.BufferSize),
		active:   true,
	}

	m.consumers[id] = state

	// 通知消费者开始
	if err := consumer.OnStart(); err != nil {
		delete(m.consumers, id)
		return "", fmt.Errorf("consumer OnStart failed: %w", err)
	}

	return id, nil
}

// RemoveConsumer 移除消费者
func (m *Multiplexer) RemoveConsumer(id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	state, exists := m.consumers[id]
	if !exists {
		return fmt.Errorf("consumer not found: %s", id)
	}

	state.active = false
	close(state.ch)
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
func (m *Multiplexer) Start(ctx context.Context, source core.StreamOutput) error {
	m.mu.Lock()
	if m.running {
		m.mu.Unlock()
		return fmt.Errorf("multiplexer already running")
	}
	m.running = true
	m.mu.Unlock()

	defer func() {
		m.mu.Lock()
		m.running = false
		m.mu.Unlock()
	}()

	// 启动每个消费者的处理协程
	var wg sync.WaitGroup
	for _, state := range m.consumers {
		wg.Add(1)
		go m.processConsumer(ctx, state, &wg)
	}

	// 从源读取并广播
	for {
		chunk, err := source.Next()
		if err != nil {
			// 通知所有消费者错误
			m.broadcastError(err)
			break
		}

		// 广播数据块
		if err := m.broadcast(chunk); err != nil {
			m.broadcastError(err)
			break
		}
	}

	// 通知所有消费者完成
	m.broadcastComplete()

	// 等待所有消费者处理完成
	wg.Wait()

	return nil
}

// Close 关闭多路复用器
func (m *Multiplexer) Close() error {
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return fmt.Errorf("multiplexer already closed")
	}

	m.closed = true
	m.cancel()

	// 关闭所有消费者通道
	for _, state := range m.consumers {
		if state.active {
			close(state.ch)
		}
	}

	m.consumers = make(map[string]*consumerState)
	m.mu.Unlock()

	// Wait for all goroutines to finish
	m.wg.Wait()

	return nil
}

// broadcast 广播数据块到所有消费者
func (m *Multiplexer) broadcast(chunk *core.LegacyStreamChunk) error {
	m.mu.RLock()
	defer m.mu.RUnlock()

	for _, state := range m.consumers {
		if !state.active {
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

// broadcastError 广播错误到所有消费者
func (m *Multiplexer) broadcastError(err error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	for _, state := range m.consumers {
		if state.active {
			// Track goroutines
			m.wg.Add(1)
			go func(s *consumerState) {
				defer m.wg.Done()
				s.consumer.OnError(err)
			}(state)
		}
	}
}

// broadcastComplete 广播完成通知到所有消费者
func (m *Multiplexer) broadcastComplete() {
	m.mu.RLock()
	defer m.mu.RUnlock()

	for _, state := range m.consumers {
		if state.active {
			// Track goroutines
			m.wg.Add(1)
			go func(s *consumerState) {
				defer m.wg.Done()
				s.consumer.OnComplete()
			}(state)
		}
	}
}

// processConsumer 处理单个消费者
func (m *Multiplexer) processConsumer(ctx context.Context, state *consumerState, wg *sync.WaitGroup) {
	defer wg.Done()

	for {
		select {
		case chunk, ok := <-state.ch:
			if !ok {
				return
			}

			if err := state.consumer.OnChunk(chunk); err != nil {
				state.errors++
				state.consumer.OnError(err)
			}

		case <-ctx.Done():
			return

		case <-m.ctx.Done():
			return
		}
	}
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
