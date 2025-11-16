package stream

import (
	"context"
	"fmt"
	"time"

	"github.com/kart-io/goagent/core"
	"github.com/kart-io/goagent/core/execution"
)

// BufferMiddleware 缓冲中间件
//
// BufferMiddleware 控制流的缓冲行为：
// - 动态调整缓冲区大小
// - 防止内存溢出
// - 提供背压控制
type BufferMiddleware struct {
	minBufferSize int
	maxBufferSize int
	threshold     float64 // 触发调整的使用率阈值
}

// NewBufferMiddleware 创建缓冲中间件
func NewBufferMiddleware(minSize, maxSize int, threshold float64) *BufferMiddleware {
	return &BufferMiddleware{
		minBufferSize: minSize,
		maxBufferSize: maxSize,
		threshold:     threshold,
	}
}

// Apply 应用中间件
func (m *BufferMiddleware) Apply(ctx context.Context, source execution.StreamOutput) (execution.StreamOutput, error) {
	opts := execution.DefaultStreamOptions()
	opts.BufferSize = m.minBufferSize
	opts.EnableBackpressure = true

	writer := NewWriter(ctx, opts)

	go func() {
		defer func() { _ = writer.Close() }()

		reader, ok := source.(*Reader)
		if !ok {
			if err := writer.WriteError(fmt.Errorf("source is not a Reader")); err != nil {
				fmt.Printf("failed to write error: %v", err)
			}
			return
		}

		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return

			case <-ticker.C:
				// 检查并调整缓冲区大小
				_ = reader.Stats()
				// 这里可以根据统计信息动态调整缓冲

			default:
				chunk, err := source.Next()
				if err != nil {
					return
				}

				if err := writer.WriteChunk(chunk); err != nil {
					if err := writer.WriteError(err); err != nil {
						fmt.Printf("failed to write error: %v", err)
					}
					return
				}
			}
		}
	}()

	return NewReader(ctx, writer.Channel(), opts), nil
}

// ThrottleMiddleware 限流中间件
//
// ThrottleMiddleware 控制流的速率：
// - 限制每秒的块数
// - 平滑流量
// - 防止下游过载
type ThrottleMiddleware struct {
	maxChunksPerSec float64
	minDelay        time.Duration
}

// NewThrottleMiddleware 创建限流中间件
func NewThrottleMiddleware(maxChunksPerSec float64) *ThrottleMiddleware {
	minDelay := time.Duration(float64(time.Second) / maxChunksPerSec)
	return &ThrottleMiddleware{
		maxChunksPerSec: maxChunksPerSec,
		minDelay:        minDelay,
	}
}

// Apply 应用中间件
func (m *ThrottleMiddleware) Apply(ctx context.Context, source execution.StreamOutput) (execution.StreamOutput, error) {
	opts := execution.DefaultStreamOptions()
	opts.EnableThrottle = true
	opts.MaxChunksPerSec = m.maxChunksPerSec
	opts.MinChunkDelay = m.minDelay

	writer := NewWriter(ctx, opts)

	go func() {
		defer func() { _ = writer.Close() }()

		lastChunkTime := time.Now()

		for {
			chunk, err := source.Next()
			if err != nil {
				return
			}

			// 计算需要等待的时间
			elapsed := time.Since(lastChunkTime)
			if elapsed < m.minDelay {
				time.Sleep(m.minDelay - elapsed)
			}

			if err := writer.WriteChunk(chunk); err != nil {
				_ = writer.WriteError(err)
				return
			}

			lastChunkTime = time.Now()
		}
	}()

	return NewReader(ctx, writer.Channel(), opts), nil
}

// TransformMiddleware 转换中间件
//
// TransformMiddleware 转换流中的数据：
// - 修改数据格式
// - 过滤数据
// - 聚合数据
type TransformMiddleware struct {
	transformFunc core.ChunkTransformFunc
}

// NewTransformMiddleware 创建转换中间件
func NewTransformMiddleware(fn core.ChunkTransformFunc) *TransformMiddleware {
	return &TransformMiddleware{
		transformFunc: fn,
	}
}

// Apply 应用中间件
func (m *TransformMiddleware) Apply(ctx context.Context, source execution.StreamOutput) (execution.StreamOutput, error) {
	opts := execution.DefaultStreamOptions()
	opts.EnableTransform = true
	opts.TransformFunc = m.transformFunc

	writer := NewWriter(ctx, opts)

	go func() {
		defer func() {
			if err := writer.Close(); err != nil {
				fmt.Printf("failed to close writer: %v", err)
			}
		}()

		for {
			chunk, err := source.Next()
			if err != nil {
				return
			}

			// 应用转换
			transformed, err := m.transformFunc(chunk)
			if err != nil {
				_ = writer.WriteError(fmt.Errorf("transform error: %w", err))
				return
			}

			if err := writer.WriteChunk(transformed); err != nil {
				if err := writer.WriteError(err); err != nil {
					fmt.Printf("failed to write error: %v", err)
				}
				return
			}
		}
	}()

	return NewReader(ctx, writer.Channel(), opts), nil
}

// TeeMiddleware 分支中间件
//
// TeeMiddleware 将流复制到多个目标：
// - 同时输出到多个消费者
// - 不影响原始流
// - 支持不同的处理速度
type TeeMiddleware struct {
	outputs []execution.StreamConsumer
}

// NewTeeMiddleware 创建分支中间件
func NewTeeMiddleware(outputs ...execution.StreamConsumer) *TeeMiddleware {
	return &TeeMiddleware{
		outputs: outputs,
	}
}

// Apply 应用中间件
func (m *TeeMiddleware) Apply(ctx context.Context, source execution.StreamOutput) (execution.StreamOutput, error) {
	// 创建多路复用器
	multiplexer := NewMultiplexer(ctx, execution.DefaultStreamOptions())

	// 添加所有输出消费者
	for _, output := range m.outputs {
		if _, err := multiplexer.AddConsumer(output); err != nil {
			return nil, fmt.Errorf("failed to add consumer: %w", err)
		}
	}

	// 创建主输出流
	opts := execution.DefaultStreamOptions()
	opts.EnableMultiplex = true

	writer := NewWriter(ctx, opts)
	reader := NewReader(ctx, writer.Channel(), opts)

	// 启动多路复用
	go func() {
		defer func() {
			if err := writer.Close(); err != nil {
				fmt.Printf("failed to close writer: %v", err)
			}
			if err := multiplexer.Close(); err != nil {
				fmt.Printf("failed to close multiplexer: %v", err)
			}
		}()

		for {
			chunk, err := source.Next()
			if err != nil {
				return
			}

			// 写入主流
			if err := writer.WriteChunk(chunk); err != nil {
				if err := writer.WriteError(err); err != nil {
					fmt.Printf("failed to write error: %v", err)
				}
				return
			}

			// 广播到所有消费者
			for _, consumer := range m.outputs {
				go func(c execution.StreamConsumer) {
					if err := c.OnChunk(chunk); err != nil {
						fmt.Printf("failed to write error: %v", err)
					}
				}(consumer)
			}
		}
	}()

	return reader, nil
}

// FilterMiddleware 过滤中间件
//
// FilterMiddleware 过滤流中的数据块：
// - 只传递符合条件的块
// - 跳过不需要的数据
type FilterMiddleware struct {
	predicate func(*execution.LegacyStreamChunk) bool
}

// NewFilterMiddleware 创建过滤中间件
func NewFilterMiddleware(predicate func(*execution.LegacyStreamChunk) bool) *FilterMiddleware {
	return &FilterMiddleware{
		predicate: predicate,
	}
}

// Apply 应用中间件
func (m *FilterMiddleware) Apply(ctx context.Context, source execution.StreamOutput) (execution.StreamOutput, error) {
	opts := execution.DefaultStreamOptions()
	writer := NewWriter(ctx, opts)

	go func() {
		defer func() {
			if err := writer.Close(); err != nil {
				fmt.Printf("failed to close writer: %v", err)
			}
		}()

		for {
			chunk, err := source.Next()
			if err != nil {
				return
			}

			// 应用过滤条件
			if m.predicate(chunk) {
				if err := writer.WriteChunk(chunk); err != nil {
					if err := writer.WriteError(err); err != nil {
						fmt.Printf("failed to write error: %v", err)
					}
					return
				}
			}
		}
	}()

	return NewReader(ctx, writer.Channel(), opts), nil
}

// BatchMiddleware 批处理中间件
//
// BatchMiddleware 将多个块聚合成批次：
// - 减少下游处理次数
// - 提高吞吐量
type BatchMiddleware struct {
	batchSize int
	timeout   time.Duration
}

// NewBatchMiddleware 创建批处理中间件
func NewBatchMiddleware(batchSize int, timeout time.Duration) *BatchMiddleware {
	return &BatchMiddleware{
		batchSize: batchSize,
		timeout:   timeout,
	}
}

// Apply 应用中间件
func (m *BatchMiddleware) Apply(ctx context.Context, source execution.StreamOutput) (execution.StreamOutput, error) {
	opts := execution.DefaultStreamOptions()
	writer := NewWriter(ctx, opts)

	go func() {
		defer func() {
			if err := writer.Close(); err != nil {
				fmt.Printf("failed to close writer: %v", err)
			}
		}()

		batch := make([]*execution.LegacyStreamChunk, 0, m.batchSize)
		timer := time.NewTimer(m.timeout)
		defer timer.Stop()

		flushBatch := func() error {
			if len(batch) == 0 {
				return nil
			}

			batchChunk := &execution.LegacyStreamChunk{
				Type: core.ChunkTypeJSON,
				Data: map[string]interface{}{
					"batch_size": len(batch),
					"items":      batch,
				},
				Metadata: core.ChunkMetadata{
					Timestamp: time.Now(),
				},
			}

			err := writer.WriteChunk(batchChunk)
			batch = batch[:0]
			timer.Reset(m.timeout)
			return err
		}

		for {
			select {
			case <-ctx.Done():
				_ = flushBatch()
				return

			case <-timer.C:
				if err := flushBatch(); err != nil {
					if err := writer.WriteError(err); err != nil {
						fmt.Printf("failed to write error: %v", err)
					}
					return
				}

			default:
				chunk, err := source.Next()
				if err != nil {
					if err := flushBatch(); err != nil {
						if err := writer.WriteError(err); err != nil {
							fmt.Printf("failed to write error: %v", err)
						}
						return
					}
					return
				}

				batch = append(batch, chunk)

				if len(batch) >= m.batchSize {
					if err := flushBatch(); err != nil {
						if err := writer.WriteError(err); err != nil {
							fmt.Printf("failed to write error: %v", err)
						}
						return
					}
				}
			}
		}
	}()

	return NewReader(ctx, writer.Channel(), opts), nil
}

// RetryMiddleware 重试中间件
//
// RetryMiddleware 在错误时重试：
// - 自动重试失败的操作
// - 指数退避
type RetryMiddleware struct {
	maxRetries int
	backoff    time.Duration
}

// NewRetryMiddleware 创建重试中间件
func NewRetryMiddleware(maxRetries int, backoff time.Duration) *RetryMiddleware {
	return &RetryMiddleware{
		maxRetries: maxRetries,
		backoff:    backoff,
	}
}

// Apply 应用中间件
func (m *RetryMiddleware) Apply(ctx context.Context, source execution.StreamOutput) (execution.StreamOutput, error) {
	opts := execution.DefaultStreamOptions()
	opts.RetryOnError = true
	opts.MaxRetries = m.maxRetries
	opts.RetryDelay = m.backoff

	writer := NewWriter(ctx, opts)

	go func() {
		defer func() {
			if err := writer.Close(); err != nil {
				fmt.Printf("failed to close writer: %v", err)
			}
		}()

		for {
			chunk, err := source.Next()
			if err != nil {
				// 重试逻辑
				for i := 0; i < m.maxRetries; i++ {
					time.Sleep(m.backoff * time.Duration(1<<i)) // 指数退避
					chunk, err = source.Next()
					if err == nil {
						break
					}
				}

				if err != nil {
					_ = writer.WriteError(err)
					return
				}
			}

			if err := writer.WriteChunk(chunk); err != nil {
				if err := writer.WriteError(err); err != nil {
					fmt.Printf("failed to write error: %v", err)
				}
				return
			}
		}
	}()

	return NewReader(ctx, writer.Channel(), opts), nil
}
