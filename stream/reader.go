package stream

import (
	"context"
	"errors"
	"io"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/kart-io/goagent/core"
	agentErrors "github.com/kart-io/goagent/errors"
)

// Reader 流读取器实现
type Reader struct {
	ctx    context.Context
	cancel context.CancelFunc
	ch     <-chan *core.LegacyStreamChunk
	opts   *core.StreamOptions

	closed   atomic.Bool
	state    atomic.Value // core.StreamState
	sequence atomic.Int64

	mu        sync.RWMutex
	stats     ReaderStats
	buffer    *RingBuffer
	lastChunk *core.LegacyStreamChunk
	lastError error
}

// ReaderStats 读取器统计信息
type ReaderStats struct {
	ChunksRead   int64
	BytesRead    int64
	ErrorCount   int64
	StartTime    time.Time
	LastReadTime time.Time
	ElapsedTime  time.Duration
}

// NewReader 创建新的流读取器
func NewReader(ctx context.Context, ch <-chan *core.LegacyStreamChunk, opts *core.StreamOptions) *Reader {
	if opts == nil {
		opts = core.DefaultStreamOptions()
	}

	ctx, cancel := context.WithCancel(ctx)

	r := &Reader{
		ctx:    ctx,
		cancel: cancel,
		ch:     ch,
		opts:   opts,
		stats: ReaderStats{
			StartTime: time.Now(),
		},
	}

	r.state.Store(core.StreamStateRunning)

	// 启用缓冲
	if opts.EnableBuffer {
		r.buffer = NewRingBuffer(opts.BufferSize)
	}

	return r
}

// Next 读取下一个数据块
func (r *Reader) Next() (*core.LegacyStreamChunk, error) {
	if r.closed.Load() {
		return nil, io.EOF
	}

	// 检查超时
	var timeout <-chan time.Time
	if r.opts.ChunkTimeout > 0 {
		timeout = time.After(r.opts.ChunkTimeout)
	}

	// 从缓冲读取
	if r.buffer != nil && !r.buffer.IsEmpty() {
		chunk := r.buffer.Pop()
		r.updateStats(chunk)
		return chunk, nil
	}

	// 从通道读取
	select {
	case chunk, ok := <-r.ch:
		if !ok {
			r.closed.Store(true)
			r.state.Store(core.StreamStateClosed)
			return nil, io.EOF
		}

		// 检查是否是最后一个块
		if chunk.IsLast {
			r.closed.Store(true)
			r.state.Store(core.StreamStateComplete)
			// Return the last chunk, next call will return EOF
			r.updateStats(chunk)
			r.lastChunk = chunk
			return chunk, nil
		}

		// 检查错误块
		if chunk.Type == core.ChunkTypeError {
			r.mu.Lock()
			r.lastError = chunk.Error
			r.stats.ErrorCount++
			r.mu.Unlock()

			r.state.Store(core.StreamStateError)

			// 根据配置决定是否重试
			if r.opts.RetryOnError && r.stats.ErrorCount <= int64(r.opts.MaxRetries) {
				time.Sleep(r.opts.RetryDelay)
				return r.Next()
			}

			return nil, chunk.Error
		}

		r.updateStats(chunk)
		r.lastChunk = chunk

		// Don't buffer the chunk we're about to return - buffering is for unread chunks
		// Only buffer if we're not returning it immediately

		return chunk, nil

	case <-timeout:
		r.state.Store(core.StreamStateError)
		return nil, agentErrors.New(agentErrors.CodeContextTimeout, "read timeout").
			WithComponent("stream_reader").
			WithOperation("Next").
			WithContext("timeout", r.opts.ChunkTimeout.String())

	case <-r.ctx.Done():
		r.closed.Store(true)
		r.state.Store(core.StreamStateClosed)
		return nil, r.ctx.Err()
	}
}

// Close 关闭读取器
func (r *Reader) Close() error {
	if !r.closed.CompareAndSwap(false, true) {
		return agentErrors.New(agentErrors.CodeStreamRead, "reader already closed").
			WithComponent("stream_reader").
			WithOperation("Close")
	}

	r.cancel()
	r.state.Store(core.StreamStateClosed)

	// 清空缓冲
	if r.buffer != nil {
		r.buffer.Clear()
	}

	return nil
}

// IsClosed 检查读取器是否已关闭
func (r *Reader) IsClosed() bool {
	return r.closed.Load()
}

// Context 返回读取器的上下文
func (r *Reader) Context() context.Context {
	return r.ctx
}

// Status 获取流状态
func (r *Reader) Status() *core.StreamStatus {
	r.mu.RLock()
	defer r.mu.RUnlock()

	state := r.state.Load().(core.StreamState)

	status := &core.StreamStatus{
		State:       state,
		ChunksRead:  r.stats.ChunksRead,
		BytesRead:   r.stats.BytesRead,
		StartTime:   r.stats.StartTime,
		ElapsedTime: time.Since(r.stats.StartTime),
		ErrorCount:  int(r.stats.ErrorCount),
		LastError:   r.lastError,
	}

	// 计算进度
	if r.lastChunk != nil && r.lastChunk.Metadata.Total > 0 {
		status.Progress = float64(r.lastChunk.Metadata.Current) / float64(r.lastChunk.Metadata.Total) * 100
	}

	return status
}

// Pause 暂停流
func (r *Reader) Pause() error {
	r.state.Store(core.StreamStatePaused)
	return nil
}

// Resume 恢复流
func (r *Reader) Resume() error {
	r.state.Store(core.StreamStateRunning)
	return nil
}

// Cancel 取消流
func (r *Reader) Cancel() error {
	return r.Close()
}

// IsRunning 检查流是否运行中
func (r *Reader) IsRunning() bool {
	state := r.state.Load().(core.StreamState)
	return state == core.StreamStateRunning
}

// IsPaused 检查流是否暂停
func (r *Reader) IsPaused() bool {
	state := r.state.Load().(core.StreamState)
	return state == core.StreamStatePaused
}

// Stats 获取统计信息
func (r *Reader) Stats() ReaderStats {
	r.mu.RLock()
	defer r.mu.RUnlock()
	stats := r.stats
	stats.ElapsedTime = time.Since(r.stats.StartTime)
	return stats
}

// updateStats 更新统计信息
func (r *Reader) updateStats(chunk *core.LegacyStreamChunk) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.stats.ChunksRead++
	r.stats.LastReadTime = time.Now()
	r.sequence.Add(1)

	// 估算字节数
	if chunk.Text != "" {
		r.stats.BytesRead += int64(len(chunk.Text))
	} else if data, ok := chunk.Data.([]byte); ok {
		r.stats.BytesRead += int64(len(data))
	}
}

// Drain 耗尽所有剩余数据
func (r *Reader) Drain() error {
	for {
		_, err := r.Next()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return err
		}
	}
}

// Collect 收集所有数据块
// 注意：此方法会将所有数据加载到内存，受 MaxCollectSize 限制防止 OOM
func (r *Reader) Collect() ([]*core.LegacyStreamChunk, error) {
	var chunks []*core.LegacyStreamChunk
	var totalBytes int64

	maxSize := r.opts.MaxCollectSize
	if maxSize <= 0 {
		maxSize = 100 * 1024 * 1024 // 默认 100MB
	}

	for {
		chunk, err := r.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return chunks, err
		}

		// 估算并检查大小限制
		chunkSize := estimateChunkSize(chunk)
		if totalBytes+chunkSize > maxSize {
			return chunks, agentErrors.New(agentErrors.CodeStreamRead, "collect size limit exceeded").
				WithComponent("stream_reader").
				WithOperation("Collect").
				WithContext("max_size", maxSize).
				WithContext("current_size", totalBytes).
				WithContext("chunk_size", chunkSize)
		}

		chunks = append(chunks, chunk)
		totalBytes += chunkSize
	}

	return chunks, nil
}

// estimateChunkSize 估算数据块的内存大小
func estimateChunkSize(chunk *core.LegacyStreamChunk) int64 {
	size := int64(len(chunk.Text))

	if data, ok := chunk.Data.([]byte); ok {
		size += int64(len(data))
	} else if str, ok := chunk.Data.(string); ok {
		size += int64(len(str))
	}

	// 为其他字段添加一些估算开销（元数据、指针等）
	size += 256

	return size
}

// CollectText 收集所有文本数据
// 使用 strings.Builder 提高性能，并受 MaxCollectSize 限制防止 OOM
func (r *Reader) CollectText() (string, error) {
	var builder strings.Builder
	var totalBytes int64

	maxSize := r.opts.MaxCollectSize
	if maxSize <= 0 {
		maxSize = 100 * 1024 * 1024 // 默认 100MB
	}

	for {
		chunk, err := r.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return builder.String(), err
		}

		if chunk.Type == core.ChunkTypeText && chunk.Text != "" {
			textSize := int64(len(chunk.Text))
			if totalBytes+textSize > maxSize {
				return builder.String(), agentErrors.New(agentErrors.CodeStreamRead, "collect text size limit exceeded").
					WithComponent("stream_reader").
					WithOperation("CollectText").
					WithContext("max_size", maxSize).
					WithContext("current_size", totalBytes).
					WithContext("text_size", textSize)
			}

			builder.WriteString(chunk.Text)
			totalBytes += textSize
		}
	}

	return builder.String(), nil
}
