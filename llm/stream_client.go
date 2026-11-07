package llm

import (
	"context"
	"time"

	shared2 "github.com/kart-io/goagent/constants"
	"github.com/kart-io/goagent/core/usage"
	"github.com/kart-io/goagent/interfaces"
)

// ToolStreamClient 在一次请求内选择工具或直接流式回答，保留原生消息和用量。
// 工具列表只在无错误的 Done 块中返回完整值；不能执行未完成的参数分片。
type ToolStreamClient interface {
	Client
	CompleteToolsStream(context.Context, *CompletionRequest, []interfaces.Tool) (<-chan *StreamChunk, error)
}

// StreamChunk LLM 流式响应块
type StreamChunk struct {
	// ResponseModel is the supplier-reported model/version; empty means unreported.
	ResponseModel string         `json:"response_model,omitempty"`
	ProviderState *ProviderState `json:"provider_state,omitzero"`
	// ToolCalls 仅在成功终止块中包含本轮完整的工具调用。
	ToolCalls []ToolCall `json:"tool_calls,omitempty"`
	// ReasoningContent 仅用于工具回合的原生续接，不作为答案文本增量展示。
	ReasoningContent string `json:"reasoning_content,omitempty"`
	// Content 完整内容（累积）
	Content string `json:"content"`

	// Delta 增量内容
	Delta string `json:"delta"`

	// Role 角色
	Role string `json:"role,omitempty"`

	// FinishReason 结束原因
	FinishReason string `json:"finish_reason,omitempty"`

	// Usage 使用情况（仅在最后一个块）
	Usage *Usage `json:"usage,omitzero"`

	// Index 块序号
	Index int `json:"index"`

	// Timestamp 时间戳
	Timestamp time.Time `json:"timestamp"`

	// Done 是否完成
	Done bool `json:"done"`

	// Error 错误信息
	Error error `json:"error,omitzero"`
}

// Usage 使用情况
type Usage struct {
	PromptTokens        int `json:"prompt_tokens"`
	CompletionTokens    int `json:"completion_tokens"`
	TotalTokens         int `json:"total_tokens"`
	CachedTokens        int `json:"cached_tokens,omitzero"`
	ReasoningTokens     int `json:"reasoning_tokens,omitzero"`
	ToolUsePromptTokens int `json:"tool_use_prompt_tokens,omitzero"`
	// KnownFields nil preserves legacy three-field semantics. An explicit zero mask means unknown.
	KnownFields *usage.Fields `json:"known_fields,omitzero"`
}

// Tokens returns an independent token value without deriving or adding totals.
func (u *Usage) Tokens() interfaces.TokenUsage {
	if u == nil {
		return interfaces.TokenUsage{}
	}
	return interfaces.TokenUsage{PromptTokens: u.PromptTokens, CompletionTokens: u.CompletionTokens, TotalTokens: u.TotalTokens, CachedTokens: u.CachedTokens, ReasoningTokens: u.ReasoningTokens, ToolUsePromptTokens: u.ToolUsePromptTokens}
}

// Fields reports available fields; legacy usage values report the basic fields.
func (u *Usage) Fields() usage.Fields {
	if u == nil {
		return 0
	}
	if u.KnownFields == nil {
		return usage.BasicFields
	}
	return *u.KnownFields
}

// StreamOptions 流式选项
type StreamOptions struct {
	// BufferSize 缓冲区大小
	BufferSize int `json:"buffer_size"`

	// IncludeUsage 是否包含使用情况
	IncludeUsage bool `json:"include_usage"`

	// OnChunk 块处理回调
	OnChunk func(*StreamChunk) error `json:"-"`

	// OnComplete 完成回调
	OnComplete func(string) error `json:"-"`

	// OnError 错误回调
	OnError func(error) error `json:"-"`
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
	provider shared2.Provider
}

// NewMockStreamClient 创建模拟流式客户端
func NewMockStreamClient() *MockStreamClient {
	return &MockStreamClient{provider: shared2.ProviderCustom}
}

// CompleteStream 流式补全
func (m *MockStreamClient) CompleteStream(ctx context.Context, req *CompletionRequest) (<-chan *StreamChunk, error) {
	// Increased buffer size to reduce blocking probability
	// Buffer should accommodate the full response to prevent goroutine leaks
	out := make(chan *StreamChunk, 100)

	go func() {
		defer close(out)

		// 模拟流式输出
		response := "This is a mock streaming response from the LLM."
		accumulated := ""

		for i, char := range response {
			// Check context cancellation before processing
			if ctx.Err() != nil {
				// Non-blocking send of cancellation error
				select {
				case out <- &StreamChunk{
					Content: accumulated,
					Delta:   "",
					Index:   i,
					Done:    true,
					Error:   ctx.Err(),
				}:
				case <-ctx.Done():
					// Context already cancelled, exit immediately
				}
				return
			}

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

			// Non-blocking send with context cancellation check
			select {
			case out <- chunk:
				// Chunk sent successfully
			case <-ctx.Done():
				// Context cancelled while sending, send error chunk
				select {
				case out <- &StreamChunk{
					Content: accumulated,
					Delta:   "",
					Index:   i,
					Done:    true,
					Error:   ctx.Err(),
				}:
				default:
					// Channel full or consumer gone, exit gracefully
				}
				return
			}

			// 模拟延迟 (check context during sleep)
			select {
			case <-time.After(50 * time.Millisecond):
			case <-ctx.Done():
				// Context cancelled during sleep
				select {
				case out <- &StreamChunk{
					Content: accumulated,
					Delta:   "",
					Index:   i + 1,
					Done:    true,
					Error:   ctx.Err(),
				}:
				default:
					// Channel full or consumer gone, exit gracefully
				}
				return
			}
		}

		// 发送最后一个块 (non-blocking send)
		finalChunk := &StreamChunk{
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

		select {
		case out <- finalChunk:
			// Final chunk sent successfully
		case <-ctx.Done():
			// Context cancelled, exit gracefully
		}
	}()

	return out, nil
}

// Provider 返回提供商类型
func (m *MockStreamClient) Provider() shared2.Provider {
	return m.provider
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
