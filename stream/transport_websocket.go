package stream

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"

	"github.com/gorilla/websocket"

	"github.com/kart-io/goagent/core"
)

// WebSocketStreamer WebSocket 流支持
//
// WebSocketStreamer 提供双向流式通信：
// - 实时双向通信
// - 低延迟
// - 支持二进制数据
type WebSocketStreamer struct {
	conn   *websocket.Conn
	mu     sync.Mutex
	closed bool
}

// NewWebSocketStreamer 创建 WebSocket 流
func NewWebSocketStreamer(conn *websocket.Conn) *WebSocketStreamer {
	return &WebSocketStreamer{
		conn: conn,
	}
}

// WriteChunk 写入数据块
func (w *WebSocketStreamer) WriteChunk(chunk *core.LegacyStreamChunk) error {
	w.mu.Lock()
	defer w.mu.Unlock()

	if w.closed {
		return fmt.Errorf("streamer is closed")
	}

	data, err := json.Marshal(chunk)
	if err != nil {
		return err
	}

	return w.conn.WriteMessage(websocket.TextMessage, data)
}

// WriteText 写入文本
func (w *WebSocketStreamer) WriteText(text string) error {
	chunk := core.NewTextChunk(text)
	return w.WriteChunk(chunk)
}

// WriteProgress 写入进度
func (w *WebSocketStreamer) WriteProgress(progress float64, message string) error {
	chunk := core.NewProgressChunk(progress, message)
	return w.WriteChunk(chunk)
}

// WriteError 写入错误
func (w *WebSocketStreamer) WriteError(err error) error {
	chunk := core.NewErrorChunk(err)
	return w.WriteChunk(chunk)
}

// WriteBinary 写入二进制数据
func (w *WebSocketStreamer) WriteBinary(data []byte) error {
	w.mu.Lock()
	defer w.mu.Unlock()

	if w.closed {
		return fmt.Errorf("streamer is closed")
	}

	return w.conn.WriteMessage(websocket.BinaryMessage, data)
}

// ReadChunk 读取数据块
func (w *WebSocketStreamer) ReadChunk() (*core.LegacyStreamChunk, error) {
	messageType, data, err := w.conn.ReadMessage()
	if err != nil {
		return nil, err
	}

	if messageType == websocket.BinaryMessage {
		return &core.LegacyStreamChunk{
			Type: core.ChunkTypeBinary,
			Data: data,
		}, nil
	}

	var chunk core.LegacyStreamChunk
	if err := json.Unmarshal(data, &chunk); err != nil {
		return nil, err
	}

	return &chunk, nil
}

// Close 关闭连接
func (w *WebSocketStreamer) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()

	if w.closed {
		return nil
	}

	w.closed = true

	// 发送关闭消息
	closeMsg := websocket.FormatCloseMessage(websocket.CloseNormalClosure, "stream closed")
	w.conn.WriteMessage(websocket.CloseMessage, closeMsg)

	return w.conn.Close()
}

// IsClosed 检查是否已关闭
func (w *WebSocketStreamer) IsClosed() bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.closed
}

// StreamToWebSocket 将 StreamOutput 转换为 WebSocket 流
func StreamToWebSocket(ctx context.Context, conn *websocket.Conn, source core.StreamOutput) error {
	streamer := NewWebSocketStreamer(conn)
	defer streamer.Close()

	// 发送开始消息
	startChunk := &core.LegacyStreamChunk{
		Type: core.ChunkTypeControl,
		Data: map[string]interface{}{
			"event":   "start",
			"message": "stream started",
		},
	}
	streamer.WriteChunk(startChunk)

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()

		default:
			chunk, err := source.Next()
			if err != nil {
				if err.Error() == "EOF" {
					// 发送结束消息
					endChunk := &core.LegacyStreamChunk{
						Type: core.ChunkTypeControl,
						Data: map[string]interface{}{
							"event":   "end",
							"message": "stream ended",
						},
					}
					streamer.WriteChunk(endChunk)
					return nil
				}
				streamer.WriteError(err)
				return err
			}

			if err := streamer.WriteChunk(chunk); err != nil {
				return err
			}
		}
	}
}

// WebSocketBidirectionalStream 双向 WebSocket 流
type WebSocketBidirectionalStream struct {
	streamer *WebSocketStreamer
	input    chan *core.LegacyStreamChunk
	output   chan *core.LegacyStreamChunk
	ctx      context.Context
	cancel   context.CancelFunc
	wg       sync.WaitGroup
}

// NewWebSocketBidirectionalStream 创建双向流
func NewWebSocketBidirectionalStream(conn *websocket.Conn) *WebSocketBidirectionalStream {
	ctx, cancel := context.WithCancel(context.Background())

	stream := &WebSocketBidirectionalStream{
		streamer: NewWebSocketStreamer(conn),
		input:    make(chan *core.LegacyStreamChunk, 100),
		output:   make(chan *core.LegacyStreamChunk, 100),
		ctx:      ctx,
		cancel:   cancel,
	}

	// 启动读取协程
	stream.wg.Add(1)
	go stream.readLoop()

	// 启动写入协程
	stream.wg.Add(1)
	go stream.writeLoop()

	return stream
}

// readLoop 读取循环
func (s *WebSocketBidirectionalStream) readLoop() {
	defer s.wg.Done()

	for {
		select {
		case <-s.ctx.Done():
			return
		default:
			chunk, err := s.streamer.ReadChunk()
			if err != nil {
				s.cancel()
				return
			}

			select {
			case s.input <- chunk:
			case <-s.ctx.Done():
				return
			}
		}
	}
}

// writeLoop 写入循环
func (s *WebSocketBidirectionalStream) writeLoop() {
	defer s.wg.Done()

	for {
		select {
		case <-s.ctx.Done():
			return
		case chunk := <-s.output:
			if err := s.streamer.WriteChunk(chunk); err != nil {
				s.cancel()
				return
			}
		}
	}
}

// Input 返回输入通道
func (s *WebSocketBidirectionalStream) Input() <-chan *core.LegacyStreamChunk {
	return s.input
}

// Output 返回输出通道
func (s *WebSocketBidirectionalStream) Output() chan<- *core.LegacyStreamChunk {
	return s.output
}

// Close 关闭流
func (s *WebSocketBidirectionalStream) Close() error {
	s.cancel()
	s.wg.Wait()
	return s.streamer.Close()
}

// WebSocketUpgrader WebSocket 升级器
var WebSocketUpgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	CheckOrigin: func(r *http.Request) bool {
		return true // 生产环境应该验证 Origin
	},
}

// WebSocketStreamHandler WebSocket 流处理器
func WebSocketStreamHandler(handler func(ctx context.Context, input *core.AgentInput) (core.StreamOutput, error)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// 升级到 WebSocket
		conn, err := WebSocketUpgrader.Upgrade(w, r, nil)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		defer conn.Close()

		ctx := r.Context()
		streamer := NewWebSocketStreamer(conn)

		// 读取输入
		chunk, err := streamer.ReadChunk()
		if err != nil {
			streamer.WriteError(err)
			return
		}

		// 解析输入
		inputData, ok := chunk.Data.(map[string]interface{})
		if !ok {
			streamer.WriteError(fmt.Errorf("invalid input format"))
			return
		}

		inputJSON, _ := json.Marshal(inputData)
		var input core.AgentInput
		if err := json.Unmarshal(inputJSON, &input); err != nil {
			streamer.WriteError(err)
			return
		}

		// 执行流式任务
		source, err := handler(ctx, &input)
		if err != nil {
			streamer.WriteError(err)
			return
		}
		defer source.Close()

		// 转换为 WebSocket 流
		if err := StreamToWebSocket(ctx, conn, source); err != nil {
			// 错误已经通过 WebSocket 发送
			return
		}
	}
}
