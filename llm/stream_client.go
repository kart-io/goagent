package llm

import (
	"context"
	"time"
)

// StreamClient 流式 LLM 客户端接口
//
// 扩展 Client 接口，提供流式补全能力
type StreamClient interface {
	Client // 继承基础接口

	// CompleteStream 流式补全
	CompleteStream(ctx context.Context, req *CompletionRequest) (<-chan *StreamChunk, error)

	// ChatStream 流式对话
	ChatStream(ctx context.Context, messages []Message) (<-chan *StreamChunk, error)
}

// StreamChunk LLM 流式响应块
type StreamChunk struct {
	// Content 完整内容（累积）
	Content string `json:"content"`

	// Delta 增量内容
	Delta string `json:"delta"`

	// Role 角色
	Role string `json:"role,omitempty"`

	// FinishReason 结束原因
	FinishReason string `json:"finish_reason,omitempty"`

	// Usage 使用情况（仅在最后一个块）
	Usage *Usage `json:"usage,omitempty"`

	// Index 块序号
	Index int `json:"index"`

	// Timestamp 时间戳
	Timestamp time.Time `json:"timestamp"`

	// Done 是否完成
	Done bool `json:"done"`

	// Error 错误信息
	Error error `json:"error,omitempty"`
}

// Usage 使用情况
type Usage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

// StreamOptions 流式选项
type StreamOptions struct {
	// BufferSize 缓冲区大小
	BufferSize int

	// IncludeUsage 是否包含使用情况
	IncludeUsage bool

	// OnChunk 块处理回调
	OnChunk func(*StreamChunk) error

	// OnComplete 完成回调
	OnComplete func(string) error

	// OnError 错误回调
	OnError func(error) error
}

// DefaultStreamOptions 返回默认流式选项
func DefaultStreamOptions() *StreamOptions {
	return &StreamOptions{
		BufferSize:   100,
		IncludeUsage: true,
		OnChunk:      nil,
		OnComplete:   nil,
		OnError:      nil,
	}
}

// MockStreamClient 模拟流式客户端（用于测试）
type MockStreamClient struct {
	provider    Provider
	isAvailable bool
}

// NewMockStreamClient 创建模拟流式客户端
func NewMockStreamClient() *MockStreamClient {
	return &MockStreamClient{
		provider:    ProviderCustom,
		isAvailable: true,
	}
}

// Complete 生成文本补全
func (m *MockStreamClient) Complete(ctx context.Context, req *CompletionRequest) (*CompletionResponse, error) {
	// 简化实现
	return &CompletionResponse{
		Content:      "Mock response",
		Model:        req.Model,
		TokensUsed:   10,
		FinishReason: "stop",
		Provider:     string(m.provider),
	}, nil
}

// Chat 进行对话
func (m *MockStreamClient) Chat(ctx context.Context, messages []Message) (*CompletionResponse, error) {
	return m.Complete(ctx, &CompletionRequest{
		Messages: messages,
	})
}

// CompleteStream 流式补全
func (m *MockStreamClient) CompleteStream(ctx context.Context, req *CompletionRequest) (<-chan *StreamChunk, error) {
	out := make(chan *StreamChunk, 10)

	go func() {
		defer close(out)

		// 模拟流式输出
		response := "This is a mock streaming response from the LLM."
		accumulated := ""

		for i, char := range response {
			select {
			case <-ctx.Done():
				out <- &StreamChunk{
					Content: accumulated,
					Delta:   "",
					Index:   i,
					Done:    true,
					Error:   ctx.Err(),
				}
				return
			default:
				delta := string(char)
				accumulated += delta

				chunk := &StreamChunk{
					Content:   accumulated,
					Delta:     delta,
					Role:      "assistant",
					Index:     i,
					Timestamp: time.Now(),
					Done:      false,
				}

				out <- chunk

				// 模拟延迟
				time.Sleep(50 * time.Millisecond)
			}
		}

		// 发送最后一个块
		out <- &StreamChunk{
			Content:      accumulated,
			Delta:        "",
			Role:         "assistant",
			FinishReason: "stop",
			Usage: &Usage{
				PromptTokens:     10,
				CompletionTokens: len(response),
				TotalTokens:      10 + len(response),
			},
			Index:     len(response),
			Timestamp: time.Now(),
			Done:      true,
		}
	}()

	return out, nil
}

// ChatStream 流式对话
func (m *MockStreamClient) ChatStream(ctx context.Context, messages []Message) (<-chan *StreamChunk, error) {
	return m.CompleteStream(ctx, &CompletionRequest{
		Messages: messages,
	})
}

// Provider 返回提供商类型
func (m *MockStreamClient) Provider() Provider {
	return m.provider
}

// IsAvailable 检查 LLM 是否可用
func (m *MockStreamClient) IsAvailable() bool {
	return m.isAvailable
}

// StreamReader 流式读取器
//
// 提供便捷的流式读取方法
type StreamReader struct {
	stream <-chan *StreamChunk
	buffer []string
}

// NewStreamReader 创建流式读取器
func NewStreamReader(stream <-chan *StreamChunk) *StreamReader {
	return &StreamReader{
		stream: stream,
		buffer: make([]string, 0),
	}
}

// ReadAll 读取所有内容
func (r *StreamReader) ReadAll(ctx context.Context) (string, error) {
	var content string

	for {
		select {
		case <-ctx.Done():
			return content, ctx.Err()
		case chunk, ok := <-r.stream:
			if !ok {
				// 流已关闭
				return content, nil
			}

			if chunk.Error != nil {
				return content, chunk.Error
			}

			content = chunk.Content

			if chunk.Done {
				return content, nil
			}
		}
	}
}

// ReadChunks 逐块读取
func (r *StreamReader) ReadChunks(ctx context.Context, handler func(*StreamChunk) error) error {
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case chunk, ok := <-r.stream:
			if !ok {
				// 流已关闭
				return nil
			}

			if chunk.Error != nil {
				return chunk.Error
			}

			if err := handler(chunk); err != nil {
				return err
			}

			if chunk.Done {
				return nil
			}
		}
	}
}

// CollectDeltas 收集所有增量
func (r *StreamReader) CollectDeltas(ctx context.Context) ([]string, error) {
	deltas := make([]string, 0)

	for {
		select {
		case <-ctx.Done():
			return deltas, ctx.Err()
		case chunk, ok := <-r.stream:
			if !ok {
				return deltas, nil
			}

			if chunk.Error != nil {
				return deltas, chunk.Error
			}

			if chunk.Delta != "" {
				deltas = append(deltas, chunk.Delta)
			}

			if chunk.Done {
				return deltas, nil
			}
		}
	}
}

// StreamWriter 流式写入器
//
// 将标准输出转换为流式块
type StreamWriter struct {
	out chan<- *StreamChunk
}

// NewStreamWriter 创建流式写入器
func NewStreamWriter(out chan<- *StreamChunk) *StreamWriter {
	return &StreamWriter{
		out: out,
	}
}

// Write 写入增量内容
func (w *StreamWriter) Write(delta string) error {
	chunk := &StreamChunk{
		Delta:     delta,
		Index:     0,
		Timestamp: time.Now(),
		Done:      false,
	}

	w.out <- chunk
	return nil
}

// WriteChunk 写入完整块
func (w *StreamWriter) WriteChunk(chunk *StreamChunk) error {
	w.out <- chunk
	return nil
}

// Close 关闭流
func (w *StreamWriter) Close() error {
	close(w.out)
	return nil
}
