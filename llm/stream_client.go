package llm

import (
	"context"
	"time"

	shared2 "github.com/kart-io/goagent/constants"
	"github.com/kart-io/goagent/core/usage"
	"github.com/kart-io/goagent/interfaces"
)

// ToolStreamClient either selects tools or streams an answer directly within a
// single request, preserving native messages and usage. The full tool list is
// returned only in an error-free Done chunk; incomplete argument fragments must
// not be executed.
type ToolStreamClient interface {
	Client
	CompleteToolsStream(context.Context, *CompletionRequest, []interfaces.Tool) (<-chan *StreamChunk, error)
}

// StreamChunk is an LLM streaming response chunk.
type StreamChunk struct {
	// ResponseModel is the supplier-reported model/version; empty means unreported.
	ResponseModel string         `json:"response_model,omitempty"`
	ProviderState *ProviderState `json:"provider_state,omitzero"`
	// ToolCalls contains the complete tool calls of this turn, only in a successful
	// terminal chunk.
	ToolCalls []ToolCall `json:"tool_calls,omitempty"`
	// ReasoningContent is used only for native continuation of tool turns and is
	// not shown as answer text delta.
	ReasoningContent string `json:"reasoning_content,omitempty"`
	// Content is the full (accumulated) content.
	Content string `json:"content"`

	// Delta is the incremental content.
	Delta string `json:"delta"`

	// Role is the role.
	Role string `json:"role,omitempty"`

	// FinishReason is the finish reason.
	FinishReason string `json:"finish_reason,omitempty"`

	// Usage is the usage information (only in the last chunk).
	Usage *Usage `json:"usage,omitzero"`

	// Index is the chunk sequence number.
	Index int `json:"index"`

	// Timestamp is the timestamp.
	Timestamp time.Time `json:"timestamp"`

	// Done reports whether the stream is complete.
	Done bool `json:"done"`

	// Error is the error information.
	Error error `json:"error,omitzero"`
}

// Usage is token usage information.
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

// StreamOptions holds streaming options.
type StreamOptions struct {
	// BufferSize is the buffer size.
	BufferSize int `json:"buffer_size"`

	// IncludeUsage reports whether to include usage information.
	IncludeUsage bool `json:"include_usage"`

	// OnChunk is the chunk handling callback.
	OnChunk func(*StreamChunk) error `json:"-"`

	// OnComplete is the completion callback.
	OnComplete func(string) error `json:"-"`

	// OnError is the error callback.
	OnError func(error) error `json:"-"`
}

// DefaultStreamOptions returns the default streaming options.
func DefaultStreamOptions() *StreamOptions {
	return &StreamOptions{
		BufferSize:   100,
		IncludeUsage: true,
		OnChunk:      nil,
		OnComplete:   nil,
		OnError:      nil,
	}
}

// MockStreamClient is a mock streaming client (for tests).
type MockStreamClient struct {
	provider shared2.Provider
}

// NewMockStreamClient creates a mock streaming client.
func NewMockStreamClient() *MockStreamClient {
	return &MockStreamClient{provider: shared2.ProviderCustom}
}

// CompleteStream performs a streaming completion.
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

// Provider returns the provider type.
func (m *MockStreamClient) Provider() shared2.Provider {
	return m.provider
}

// StreamReader is a stream reader.
//
// It provides convenience methods for reading a stream.
type StreamReader struct {
	stream <-chan *StreamChunk
	buffer []string
}

// NewStreamReader creates a stream reader.
func NewStreamReader(stream <-chan *StreamChunk) *StreamReader {
	return &StreamReader{
		stream: stream,
		buffer: make([]string, 0),
	}
}

// ReadAll reads all content.
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

// ReadChunks reads chunk by chunk.
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

// CollectDeltas collects all deltas.
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

// StreamWriter is a stream writer.
//
// It converts written content into stream chunks.
type StreamWriter struct {
	out chan<- *StreamChunk
}

// NewStreamWriter creates a stream writer.
func NewStreamWriter(out chan<- *StreamChunk) *StreamWriter {
	return &StreamWriter{
		out: out,
	}
}

// Write writes incremental content.
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

// WriteChunk writes a complete chunk.
func (w *StreamWriter) WriteChunk(chunk *StreamChunk) error {
	w.out <- chunk
	return nil
}

// Close closes the stream.
func (w *StreamWriter) Close() error {
	close(w.out)
	return nil
}
