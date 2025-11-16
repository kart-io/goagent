package stream

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// StreamManager 流式管理器
//
// 提供流式数据处理的高级功能：
// - 缓冲管理
// - 背压控制
// - 错误处理
// - 超时控制
type StreamManager struct {
	bufferSize int
	timeout    time.Duration
}

// StreamManagerConfig 流式管理器配置
type StreamManagerConfig struct {
	BufferSize int
	Timeout    time.Duration
}

// NewStreamManager 创建流式管理器
func NewStreamManager(config StreamManagerConfig) *StreamManager {
	if config.BufferSize <= 0 {
		config.BufferSize = 100
	}

	if config.Timeout <= 0 {
		config.Timeout = 30 * time.Second
	}

	return &StreamManager{
		bufferSize: config.BufferSize,
		timeout:    config.Timeout,
	}
}

// StreamChunk 流式数据块（通用版本）
type StreamChunk struct {
	Data      interface{}
	Metadata  map[string]interface{}
	Timestamp time.Time
	ChunkID   int
	Error     error
	Done      bool
}

// NewStreamChunk 创建流式数据块
func NewStreamChunk(data interface{}) *StreamChunk {
	return &StreamChunk{
		Data:      data,
		Metadata:  make(map[string]interface{}),
		Timestamp: time.Now(),
		Done:      false,
	}
}

// StreamHandler 流式处理器接口
type StreamHandler interface {
	// OnChunk 处理数据块
	OnChunk(chunk *StreamChunk) error

	// OnComplete 完成时调用
	OnComplete() error

	// OnError 错误时调用
	OnError(err error) error
}

// FuncStreamHandler 函数式流处理器
type FuncStreamHandler struct {
	onChunk    func(*StreamChunk) error
	onComplete func() error
	onError    func(error) error
}

// NewFuncStreamHandler 创建函数式流处理器
func NewFuncStreamHandler(
	onChunk func(*StreamChunk) error,
	onComplete func() error,
	onError func(error) error,
) *FuncStreamHandler {
	return &FuncStreamHandler{
		onChunk:    onChunk,
		onComplete: onComplete,
		onError:    onError,
	}
}

// OnChunk 处理数据块
func (h *FuncStreamHandler) OnChunk(chunk *StreamChunk) error {
	if h.onChunk != nil {
		return h.onChunk(chunk)
	}
	return nil
}

// OnComplete 完成时调用
func (h *FuncStreamHandler) OnComplete() error {
	if h.onComplete != nil {
		return h.onComplete()
	}
	return nil
}

// OnError 错误时调用
func (h *FuncStreamHandler) OnError(err error) error {
	if h.onError != nil {
		return h.onError(err)
	}
	return nil
}

// Process 处理流式数据
func (m *StreamManager) Process(ctx context.Context, stream <-chan *StreamChunk, handler StreamHandler) error {
	// 创建带超时的上下文
	timeoutCtx, cancel := context.WithTimeout(ctx, m.timeout)
	defer cancel()

	for {
		select {
		case <-timeoutCtx.Done():
			err := timeoutCtx.Err()
			_ = handler.OnError(err)
			return err

		case chunk, ok := <-stream:
			if !ok {
				// 流已关闭
				return handler.OnComplete()
			}

			if chunk.Error != nil {
				_ = handler.OnError(chunk.Error)
				return chunk.Error
			}

			if err := handler.OnChunk(chunk); err != nil {
				_ = handler.OnError(err)
				return err
			}

			if chunk.Done {
				return handler.OnComplete()
			}
		}
	}
}

// Transform 转换流式数据
func (m *StreamManager) Transform(ctx context.Context, input <-chan *StreamChunk, transformer func(*StreamChunk) (*StreamChunk, error)) <-chan *StreamChunk {
	output := make(chan *StreamChunk, m.bufferSize)

	go func() {
		defer close(output)

		for chunk := range input {
			select {
			case <-ctx.Done():
				output <- &StreamChunk{
					Error: ctx.Err(),
					Done:  true,
				}
				return
			default:
				transformed, err := transformer(chunk)
				if err != nil {
					output <- &StreamChunk{
						Error: err,
						Done:  true,
					}
					return
				}

				output <- transformed

				if chunk.Done {
					return
				}
			}
		}
	}()

	return output
}

// Filter 过滤流式数据
func (m *StreamManager) Filter(ctx context.Context, input <-chan *StreamChunk, predicate func(*StreamChunk) bool) <-chan *StreamChunk {
	output := make(chan *StreamChunk, m.bufferSize)

	go func() {
		defer close(output)

		for chunk := range input {
			select {
			case <-ctx.Done():
				output <- &StreamChunk{
					Error: ctx.Err(),
					Done:  true,
				}
				return
			default:
				if predicate(chunk) {
					output <- chunk
				}

				if chunk.Done {
					return
				}
			}
		}
	}()

	return output
}

// Merge 合并多个流
func (m *StreamManager) Merge(ctx context.Context, streams ...<-chan *StreamChunk) <-chan *StreamChunk {
	output := make(chan *StreamChunk, m.bufferSize)

	var wg sync.WaitGroup
	wg.Add(len(streams))

	for _, stream := range streams {
		go func(s <-chan *StreamChunk) {
			defer wg.Done()

			for chunk := range s {
				select {
				case <-ctx.Done():
					return
				case output <- chunk:
				}
			}
		}(stream)
	}

	go func() {
		wg.Wait()
		close(output)
	}()

	return output
}

// Buffer 缓冲流式数据
func (m *StreamManager) Buffer(ctx context.Context, input <-chan *StreamChunk, size int) <-chan []*StreamChunk {
	output := make(chan []*StreamChunk, m.bufferSize)

	go func() {
		defer close(output)

		buffer := make([]*StreamChunk, 0, size)

		for chunk := range input {
			select {
			case <-ctx.Done():
				if len(buffer) > 0 {
					output <- buffer
				}
				return
			default:
				buffer = append(buffer, chunk)

				if len(buffer) >= size || chunk.Done {
					output <- buffer
					buffer = make([]*StreamChunk, 0, size)
				}

				if chunk.Done {
					return
				}
			}
		}

		// 发送剩余的块
		if len(buffer) > 0 {
			output <- buffer
		}
	}()

	return output
}

// Collect 收集所有流式数据
func (m *StreamManager) Collect(ctx context.Context, stream <-chan *StreamChunk) ([]*StreamChunk, error) {
	chunks := make([]*StreamChunk, 0)

	for {
		select {
		case <-ctx.Done():
			return chunks, ctx.Err()
		case chunk, ok := <-stream:
			if !ok {
				return chunks, nil
			}

			if chunk.Error != nil {
				return chunks, chunk.Error
			}

			chunks = append(chunks, chunk)

			if chunk.Done {
				return chunks, nil
			}
		}
	}
}

// StreamMultiplexer 流式多路复用器
//
// 将一个流广播到多个消费者
type StreamMultiplexer struct {
	input     <-chan *StreamChunk
	consumers []chan<- *StreamChunk
	mu        sync.RWMutex
}

// NewStreamMultiplexer 创建流式多路复用器
func NewStreamMultiplexer(input <-chan *StreamChunk) *StreamMultiplexer {
	return &StreamMultiplexer{
		input:     input,
		consumers: make([]chan<- *StreamChunk, 0),
	}
}

// AddConsumer 添加消费者
func (m *StreamMultiplexer) AddConsumer(bufferSize int) <-chan *StreamChunk {
	m.mu.Lock()
	defer m.mu.Unlock()

	consumer := make(chan *StreamChunk, bufferSize)
	m.consumers = append(m.consumers, consumer)

	return consumer
}

// Start 开始多路复用
func (m *StreamMultiplexer) Start(ctx context.Context) error {
	for {
		select {
		case <-ctx.Done():
			m.closeAllConsumers()
			return ctx.Err()

		case chunk, ok := <-m.input:
			if !ok {
				m.closeAllConsumers()
				return nil
			}

			m.broadcast(chunk)

			if chunk.Done {
				m.closeAllConsumers()
				return nil
			}
		}
	}
}

// broadcast 广播数据块到所有消费者
func (m *StreamMultiplexer) broadcast(chunk *StreamChunk) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	for _, consumer := range m.consumers {
		// 非阻塞发送
		select {
		case consumer <- chunk:
		default:
			// 消费者缓冲区满，跳过
		}
	}
}

// closeAllConsumers 关闭所有消费者
func (m *StreamMultiplexer) closeAllConsumers() {
	m.mu.Lock()
	defer m.mu.Unlock()

	for _, consumer := range m.consumers {
		close(consumer)
	}

	m.consumers = make([]chan<- *StreamChunk, 0)
}

// StreamRateLimiter 流式速率限制器
type StreamRateLimiter struct {
	rate     int           // 每秒块数
	interval time.Duration // 块间隔
}

// NewStreamRateLimiter 创建流式速率限制器
func NewStreamRateLimiter(rate int) *StreamRateLimiter {
	if rate <= 0 {
		rate = 10
	}

	return &StreamRateLimiter{
		rate:     rate,
		interval: time.Second / time.Duration(rate),
	}
}

// Limit 限制流速
func (l *StreamRateLimiter) Limit(ctx context.Context, input <-chan *StreamChunk) <-chan *StreamChunk {
	output := make(chan *StreamChunk, 10)

	go func() {
		defer close(output)

		ticker := time.NewTicker(l.interval)
		defer ticker.Stop()

		for chunk := range input {
			select {
			case <-ctx.Done():
				output <- &StreamChunk{
					Error: ctx.Err(),
					Done:  true,
				}
				return
			case <-ticker.C:
				output <- chunk

				if chunk.Done {
					return
				}
			}
		}
	}()

	return output
}

// StreamStats 流式统计信息
type StreamStats struct {
	ChunksProcessed int64
	BytesProcessed  int64
	ErrorsCount     int64
	StartTime       time.Time
	EndTime         time.Time
	mu              sync.RWMutex
}

// NewStreamStats 创建流式统计
func NewStreamStats() *StreamStats {
	return &StreamStats{
		StartTime: time.Now(),
	}
}

// RecordChunk 记录数据块
func (s *StreamStats) RecordChunk(size int64) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.ChunksProcessed++
	s.BytesProcessed += size
}

// RecordError 记录错误
func (s *StreamStats) RecordError() {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.ErrorsCount++
}

// Complete 标记完成
func (s *StreamStats) Complete() {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.EndTime = time.Now()
}

// Duration 计算持续时间
func (s *StreamStats) Duration() time.Duration {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if s.EndTime.IsZero() {
		return time.Since(s.StartTime)
	}
	return s.EndTime.Sub(s.StartTime)
}

// Throughput 计算吞吐量（块/秒）
func (s *StreamStats) Throughput() float64 {
	s.mu.RLock()
	defer s.mu.RUnlock()

	duration := s.Duration().Seconds()
	if duration == 0 {
		return 0
	}

	return float64(s.ChunksProcessed) / duration
}

// String 返回统计信息字符串
func (s *StreamStats) String() string {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return fmt.Sprintf("Chunks: %d, Bytes: %d, Errors: %d, Duration: %v, Throughput: %.2f chunks/s",
		s.ChunksProcessed, s.BytesProcessed, s.ErrorsCount, s.Duration(), s.Throughput())
}
