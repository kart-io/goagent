package stream

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/kart-io/goagent/core"
)

// SSEStreamer Server-Sent Events 流支持
//
// SSEStreamer 将流输出转换为 SSE 格式：
// - 单向服务器推送
// - 自动重连
// - 标准 HTTP 协议
type SSEStreamer struct {
	writer  http.ResponseWriter
	flusher http.Flusher
	closed  bool
}

// NewSSEStreamer 创建 SSE 流
func NewSSEStreamer(w http.ResponseWriter) (*SSEStreamer, error) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		return nil, fmt.Errorf("streaming not supported")
	}

	// 设置 SSE 头
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("Access-Control-Allow-Origin", "*")

	return &SSEStreamer{
		writer:  w,
		flusher: flusher,
	}, nil
}

// WriteChunk 写入数据块
func (s *SSEStreamer) WriteChunk(chunk *core.LegacyStreamChunk) error {
	if s.closed {
		return fmt.Errorf("streamer is closed")
	}

	// 格式化为 SSE 格式
	event := s.formatSSEEvent(chunk)

	// 写入数据
	if _, err := fmt.Fprintf(s.writer, "%s", event); err != nil {
		return err
	}

	// 刷新缓冲区
	s.flusher.Flush()

	return nil
}

// formatSSEEvent 格式化 SSE 事件
func (s *SSEStreamer) formatSSEEvent(chunk *core.LegacyStreamChunk) string {
	data, _ := json.Marshal(chunk)
	return fmt.Sprintf("event: %s\ndata: %s\n\n", chunk.Type, string(data))
}

// WriteText 写入文本
func (s *SSEStreamer) WriteText(text string) error {
	chunk := core.NewTextChunk(text)
	return s.WriteChunk(chunk)
}

// WriteProgress 写入进度
func (s *SSEStreamer) WriteProgress(progress float64, message string) error {
	chunk := core.NewProgressChunk(progress, message)
	return s.WriteChunk(chunk)
}

// WriteError 写入错误
func (s *SSEStreamer) WriteError(err error) error {
	chunk := core.NewErrorChunk(err)
	return s.WriteChunk(chunk)
}

// Close 关闭流
func (s *SSEStreamer) Close() error {
	if s.closed {
		return nil
	}

	s.closed = true

	// 发送关闭事件
	if _, err := fmt.Fprintf(s.writer, "event: close\ndata: {\"message\": \"stream closed\"}\n\n"); err != nil {
		return err
	}
	s.flusher.Flush()

	return nil
}

// StreamToSSE 将 StreamOutput 转换为 SSE 流
func StreamToSSE(ctx context.Context, w http.ResponseWriter, source core.StreamOutput) error {
	streamer, err := NewSSEStreamer(w)
	if err != nil {
		return err
	}
	defer func() {
		if err := streamer.Close(); err != nil {
			fmt.Printf("failed to close streamer: %v", err)
		}
	}()

	// 发送开始事件
	if _, err := fmt.Fprintf(w, "event: start\ndata: {\"message\": \"stream started\"}\n\n"); err != nil {
		return err
	}
	streamer.flusher.Flush()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()

		default:
			chunk, err := source.Next()
			if err != nil {
				// 发送错误或结束
				if err.Error() == "EOF" {
					return nil
				}
				if err := streamer.WriteError(err); err != nil {
					fmt.Printf("failed to write error: %v", err)
				}
				return err
			}

			if err := streamer.WriteChunk(chunk); err != nil {
				return err
			}
		}
	}
}

// SSEHandler SSE HTTP 处理器
func SSEHandler(handler func(ctx context.Context, input *core.AgentInput) (core.StreamOutput, error)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()

		// 解析输入
		var input core.AgentInput
		if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		// 执行流式任务
		source, err := handler(ctx, &input)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		defer func() {
			if err := source.Close(); err != nil {
				fmt.Printf("failed to close source: %v", err)
			}
		}()

		// 转换为 SSE
		if err := StreamToSSE(ctx, w, source); err != nil {
			// 错误已经通过 SSE 发送，不需要再处理
			return
		}
	}
}

// ChunkedTransferStreamer HTTP Chunked Transfer Encoding 流支持
type ChunkedTransferStreamer struct {
	writer  http.ResponseWriter
	flusher http.Flusher
	closed  bool
}

// NewChunkedTransferStreamer 创建 Chunked Transfer 流
func NewChunkedTransferStreamer(w http.ResponseWriter) (*ChunkedTransferStreamer, error) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		return nil, fmt.Errorf("streaming not supported")
	}

	// 设置响应头
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Transfer-Encoding", "chunked")
	w.Header().Set("Cache-Control", "no-cache")

	return &ChunkedTransferStreamer{
		writer:  w,
		flusher: flusher,
	}, nil
}

// WriteChunk 写入数据块
func (c *ChunkedTransferStreamer) WriteChunk(chunk *core.LegacyStreamChunk) error {
	if c.closed {
		return fmt.Errorf("streamer is closed")
	}

	data, err := json.Marshal(chunk)
	if err != nil {
		return err
	}

	// 写入 JSON 数据和换行符
	if _, err := fmt.Fprintf(c.writer, "%s\n", string(data)); err != nil {
		return err
	}

	c.flusher.Flush()
	return nil
}

// Close 关闭流
func (c *ChunkedTransferStreamer) Close() error {
	c.closed = true
	return nil
}

// StreamToChunkedTransfer 将 StreamOutput 转换为 Chunked Transfer
func StreamToChunkedTransfer(ctx context.Context, w http.ResponseWriter, source core.StreamOutput) error {
	streamer, err := NewChunkedTransferStreamer(w)
	if err != nil {
		return err
	}
	defer func() { _ = streamer.Close() }()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()

		default:
			chunk, err := source.Next()
			if err != nil {
				return err
			}

			if err := streamer.WriteChunk(chunk); err != nil {
				return err
			}
		}
	}
}

// PollingStreamer 轮询流支持
//
// PollingStreamer 用于不支持持久连接的场景：
// - 客户端定期轮询
// - 服务器缓存最新数据
type PollingStreamer struct {
	sessionID string
	buffer    []*core.LegacyStreamChunk
	lastIndex int //nolint:unused // For future position tracking
	timeout   time.Duration
}

// NewPollingStreamer 创建轮询流
func NewPollingStreamer(sessionID string, timeout time.Duration) *PollingStreamer {
	return &PollingStreamer{
		sessionID: sessionID,
		buffer:    make([]*core.LegacyStreamChunk, 0, 100),
		timeout:   timeout,
	}
}

// WriteChunk 写入数据块
func (p *PollingStreamer) WriteChunk(chunk *core.LegacyStreamChunk) error {
	p.buffer = append(p.buffer, chunk)

	// 限制缓冲区大小
	if len(p.buffer) > 1000 {
		p.buffer = p.buffer[len(p.buffer)-1000:]
	}

	return nil
}

// Poll 轮询新数据
func (p *PollingStreamer) Poll(lastIndex int) ([]*core.LegacyStreamChunk, int, error) {
	if lastIndex < 0 || lastIndex > len(p.buffer) {
		lastIndex = 0
	}

	newChunks := p.buffer[lastIndex:]
	newIndex := len(p.buffer)

	return newChunks, newIndex, nil
}

// SessionID 返回会话 ID
func (p *PollingStreamer) SessionID() string {
	return p.sessionID
}
