package stream

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/kart-io/goagent/core"
)

// Writer 流写入器实现
type Writer struct {
	ctx    context.Context
	cancel context.CancelFunc
	ch     chan *core.LegacyStreamChunk
	opts   *core.StreamOptions

	closed   atomic.Bool
	sequence atomic.Int64

	mu    sync.RWMutex
	stats WriterStats
}

// WriterStats 写入器统计信息
type WriterStats struct {
	ChunksWritten int64
	BytesWritten  int64
	ErrorCount    int64
	StartTime     time.Time
	LastWriteTime time.Time
}

// NewWriter 创建新的流写入器
func NewWriter(ctx context.Context, opts *core.StreamOptions) *Writer {
	if opts == nil {
		opts = core.DefaultStreamOptions()
	}

	ctx, cancel := context.WithCancel(ctx)

	return &Writer{
		ctx:    ctx,
		cancel: cancel,
		ch:     make(chan *core.LegacyStreamChunk, opts.BufferSize),
		opts:   opts,
		stats: WriterStats{
			StartTime: time.Now(),
		},
	}
}

// Write 实现 io.Writer 接口
func (w *Writer) Write(p []byte) (n int, err error) {
	if w.closed.Load() {
		return 0, fmt.Errorf("writer is closed")
	}

	chunk := core.NewTextChunk(string(p))
	if err := w.WriteChunk(chunk); err != nil {
		return 0, err
	}

	return len(p), nil
}

// Write 写入数据块
func (w *Writer) WriteChunk(chunk *core.LegacyStreamChunk) error {
	if w.closed.Load() {
		return fmt.Errorf("writer is closed")
	}

	// 设置序列号
	chunk.Metadata.Sequence = w.sequence.Add(1)
	chunk.Metadata.Timestamp = time.Now()

	// 应用转换函数
	if w.opts.EnableTransform && w.opts.TransformFunc != nil {
		transformed, err := w.opts.TransformFunc(chunk)
		if err != nil {
			w.mu.Lock()
			w.stats.ErrorCount++
			w.mu.Unlock()
			return fmt.Errorf("transform error: %w", err)
		}
		chunk = transformed
	}

	// 写入通道
	select {
	case w.ch <- chunk:
		w.updateStats(chunk)
		return nil
	case <-w.ctx.Done():
		return w.ctx.Err()
	case <-time.After(w.opts.ChunkTimeout):
		return fmt.Errorf("write timeout")
	}
}

// WriteBatch 批量写入数据块
func (w *Writer) WriteBatch(chunks []*core.LegacyStreamChunk) error {
	for _, chunk := range chunks {
		if err := w.WriteChunk(chunk); err != nil {
			return err
		}
	}
	return nil
}

// WriteText 写入文本数据
func (w *Writer) WriteText(text string) error {
	chunk := core.NewTextChunk(text)
	return w.WriteChunk(chunk)
}

// WriteProgress 写入进度更新
func (w *Writer) WriteProgress(progress float64, message string) error {
	chunk := core.NewProgressChunk(progress, message)
	return w.WriteChunk(chunk)
}

// WriteStatus 写入状态更新
func (w *Writer) WriteStatus(status string) error {
	chunk := &core.LegacyStreamChunk{
		Type: core.ChunkTypeStatus,
		Data: status,
		Metadata: core.ChunkMetadata{
			Timestamp: time.Now(),
			Status:    status,
		},
	}
	return w.WriteChunk(chunk)
}

// WriteError 写入错误信息
func (w *Writer) WriteError(err error) error {
	chunk := core.NewErrorChunk(err)
	return w.WriteChunk(chunk)
}

// Close 关闭写入器
func (w *Writer) Close() error {
	if !w.closed.CompareAndSwap(false, true) {
		return fmt.Errorf("writer already closed")
	}

	// 发送最后一个块标记
	lastChunk := &core.LegacyStreamChunk{
		Type:   core.ChunkTypeControl,
		IsLast: true,
		Metadata: core.ChunkMetadata{
			Timestamp: time.Now(),
		},
	}

	select {
	case w.ch <- lastChunk:
	case <-time.After(time.Second):
		// 超时则强制关闭
	}

	close(w.ch)
	w.cancel()
	return nil
}

// IsClosed 检查写入器是否已关闭
func (w *Writer) IsClosed() bool {
	return w.closed.Load()
}

// Channel 返回写入通道（供 Reader 使用）
func (w *Writer) Channel() <-chan *core.LegacyStreamChunk {
	return w.ch
}

// Stats 获取统计信息
func (w *Writer) Stats() WriterStats {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.stats
}

// updateStats 更新统计信息
func (w *Writer) updateStats(chunk *core.LegacyStreamChunk) {
	w.mu.Lock()
	defer w.mu.Unlock()

	w.stats.ChunksWritten++
	w.stats.LastWriteTime = time.Now()

	// 估算字节数
	if chunk.Text != "" {
		w.stats.BytesWritten += int64(len(chunk.Text))
	} else if data, ok := chunk.Data.([]byte); ok {
		w.stats.BytesWritten += int64(len(data))
	}
}
