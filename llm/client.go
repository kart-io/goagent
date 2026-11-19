package llm

import (
	"context"
	"net/http"
	"time"

	"github.com/kart-io/goagent/codec/json"
	shared "github.com/kart-io/goagent/constants"
	"github.com/kart-io/goagent/core/usage"
	"github.com/kart-io/goagent/interfaces"
)

// Client is the only required interface for an LLM client: streaming
// completion plus the provider identity. Use Complete to collect the stream
// when a full response is needed; callers must cancel ctx when they stop
// reading.
type Client interface {
	CompleteStream(ctx context.Context, req *CompletionRequest) (<-chan *StreamChunk, error)
	Provider() shared.Provider
}

// Message defines a chat message.
type Message struct {
	Role    string `json:"role"`           // "system", "user", "assistant"
	Content string `json:"content"`        // message content
	Name    string `json:"name,omitempty"` // optional message name
	// ToolCalls and ToolCallID preserve the native assistant/tool conversation
	// relationship.
	ToolCalls        []ToolCall `json:"tool_calls,omitempty"`
	ReasoningContent string     `json:"reasoning_content,omitempty"`
	ToolCallID       string     `json:"tool_call_id,omitempty"`
	// ProviderState preserves opaque response parts (e.g. Gemini signatures).
	// Replay unchanged with the matching assistant content and tool calls.
	ProviderState *ProviderState `json:"provider_state,omitzero"`
	// Images 随消息一起发送；仅 OpenAI 兼容族支持，其余提供商会拒绝。
	Images []ImagePart `json:"images,omitempty"`
}

// ImagePart 是消息携带的一张图片。
type ImagePart struct {
	// URL 可以是 https 地址或 data: URI。
	URL string `json:"url"`
}

type ProviderState struct {
	Provider shared.Provider `json:"provider"`
	Data     json.RawMessage `json:"data"`
}

// CompletionRequest defines a completion request.
type CompletionRequest struct {
	Reasoning *usage.ReasoningConfig `json:"reasoning,omitzero"`
	// TemperatureSet makes Temperature=0 explicitly override the client default;
	// JSON maintains it automatically based on whether the field is present.
	TemperatureSet bool      `json:"-"`
	Messages       []Message `json:"messages"`             // message list
	Temperature    float64   `json:"temperature,omitzero"` // temperature (0.0-2.0)
	MaxTokens      int       `json:"max_tokens,omitzero"`  // maximum tokens
	Model          string    `json:"model,omitempty"`      // model name
	Stop           []string  `json:"stop,omitempty"`       // stop sequences
	TopP           float64   `json:"top_p,omitzero"`       // top-p sampling
}

// CompletionResponse defines a completion response.
type CompletionResponse struct {
	ResponseModel string `json:"response_model,omitempty"`
	Content       string `json:"content"` // generated content
	Model         string `json:"model"`   // model used

	TokensUsed   int                    `json:"tokens_used,omitzero"`    // tokens used
	FinishReason string                 `json:"finish_reason,omitempty"` // finish reason
	Provider     string                 `json:"provider,omitempty"`      // provider
	Usage        *interfaces.TokenUsage `json:"usage,omitzero"`          // detailed token usage
}

// ResponseFormat defines the response format type.
type ResponseFormat struct {
	Type string `json:"type"` // "text" or "json_object"
}

// Predefined response format constants.
var (
	// ResponseFormatText is the plain text format.
	ResponseFormatText = &ResponseFormat{Type: "text"}
	// ResponseFormatJSON is the JSON object format.
	ResponseFormatJSON = &ResponseFormat{Type: "json_object"}
)

// LLMOptions defines the LLM configuration.
type LLMOptions struct {
	Reasoning *usage.ReasoningConfig `json:"reasoning,omitzero"`
	// TemperatureSet distinguishes an explicit zero from an unset value;
	// WithTemperature and JSON decoding maintain it automatically.
	TemperatureSet bool `json:"-"`
	// Basic settings.
	Provider       shared.Provider `json:"provider"`                  // provider
	APIKey         string          `json:"api_key"`                   // API key
	BaseURL        string          `json:"base_url,omitempty"`        // custom API endpoint
	Model          string          `json:"model"`                     // default model
	EmbeddingModel string          `json:"embedding_model,omitempty"` // Separate embedding model, used by OpenAI and Gemini.
	// EmbeddingDimensions requests a vector size from OpenAI-family and Gemini embeddings; 0 leaves it to the model.
	EmbeddingDimensions int `json:"embedding_dimensions,omitempty"`

	// Generation parameters.
	MaxTokens      int             `json:"max_tokens"`               // default maximum tokens
	Temperature    float64         `json:"temperature"`              // default temperature (0.0-2.0)
	TopP           float64         `json:"top_p,omitzero"`           // top-p sampling (0.0-1.0)
	ResponseFormat *ResponseFormat `json:"response_format,omitzero"` // response format (json_object or text)

	// Network settings.
	Timeout int `json:"timeout"` // request timeout in seconds
	// StreamIdleTimeout is the maximum gap between two data receipts in a streaming
	// response, and StreamTimeout is the upper bound on the total duration of one
	// streaming request; when exceeded, the stream is aborted with a retryable
	// timeout error. 0 means no limit. They currently apply to OpenAI-compatible
	// tool streams (including DeepSeek).
	StreamIdleTimeout time.Duration `json:"stream_idle_timeout,omitzero"`
	StreamTimeout     time.Duration `json:"stream_timeout,omitzero"`
	// DisableStreamUsage stops OpenAI-compatible streaming requests (including
	// DeepSeek) from sending stream_options; see WithStreamUsage.
	DisableStreamUsage bool `json:"disable_stream_usage,omitzero"`

	// Other settings.
	SystemPrompt  string            `json:"system_prompt,omitempty"`  // default system prompt
	CustomHeaders map[string]string `json:"custom_headers,omitempty"` // custom HTTP headers

	// HTTPClient supplies the transport and redirect policy for provider requests;
	// see WithHTTPClient. Never serialized.
	HTTPClient *http.Client `json:"-"`
}

// DefaultLLMOptions returns the default configuration.
func DefaultLLMOptions() *LLMOptions {
	return &LLMOptions{
		Provider:    shared.ProviderOpenAI,
		MaxTokens:   shared.DefaultClientMaxTokens,
		Temperature: shared.DefaultTemperature,
		Timeout:     shared.DefaultLLMTimeoutSeconds,
		TopP:        shared.DefaultTopP,
	}
}

// NewMessage creates a new message.
func NewMessage(role, content string) Message {
	return Message{
		Role:    role,
		Content: content,
	}
}

// SystemMessage creates a system message.
func SystemMessage(content string) Message {
	return NewMessage(shared.RoleSystem, content)
}

// UserMessage creates a user message.
func UserMessage(content string) Message {
	return NewMessage(shared.RoleUser, content)
}

// AssistantMessage creates an assistant message.
func AssistantMessage(content string) Message {
	return NewMessage(shared.RoleAssistant, content)
}
