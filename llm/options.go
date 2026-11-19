package llm

import (
	"net/http"
	"time"

	shared "github.com/kart-io/goagent/constants"
	"github.com/kart-io/goagent/core/usage"
)

// ClientOption defines an LLM client configuration option.
type ClientOption func(*LLMOptions)

// DefaultClientConfig returns the default client configuration.
func DefaultClientConfig() *LLMOptions {
	return &LLMOptions{
		Provider:    shared.ProviderCustom,
		MaxTokens:   shared.DefaultClientMaxTokens,
		Temperature: shared.DefaultTemperature,
		Timeout:     shared.DefaultLLMTimeoutSeconds,
		TopP:        shared.DefaultTopP,
	}
}

// WithProvider sets the LLM provider.
func WithProvider(provider shared.Provider) ClientOption {
	return func(c *LLMOptions) {
		c.Provider = provider
	}
}

// WithAPIKey sets the API key.
func WithAPIKey(apiKey string) ClientOption {
	return func(c *LLMOptions) {
		c.APIKey = apiKey
	}
}

// WithBaseURL sets a custom API endpoint.
func WithBaseURL(baseURL string) ClientOption {
	return func(c *LLMOptions) {
		c.BaseURL = baseURL
	}
}

// WithModel sets the default model.
func WithModel(model string) ClientOption {
	return func(c *LLMOptions) {
		c.Model = model
	}
}

// WithEmbeddingModel selects a separate embedding model for providers that support it.
func WithEmbeddingModel(model string) ClientOption {
	return func(c *LLMOptions) { c.EmbeddingModel = model }
}

// WithEmbeddingDimensions requests n-dimensional embeddings; 0 (default) sends nothing, n < 0 is rejected at construction.
func WithEmbeddingDimensions(n int) ClientOption {
	return func(c *LLMOptions) { c.EmbeddingDimensions = n }
}

// WithMaxTokens sets the maximum number of tokens.
func WithMaxTokens(maxTokens int) ClientOption {
	return func(c *LLMOptions) {
		if maxTokens > 0 {
			c.MaxTokens = maxTokens
		}
	}
}

// WithTemperature sets the temperature parameter.
func WithTemperature(temperature float64) ClientOption {
	return func(c *LLMOptions) {
		if temperature >= 0 && temperature <= 2.0 {
			c.Temperature = temperature
			c.TemperatureSet = true
		}
	}
}

// WithTimeout sets the request timeout. The configuration is stored in whole
// seconds and positive durations are rounded up, so values under 1 second are
// not truncated to 0 and treated as unset.
func WithTimeout(timeout time.Duration) ClientOption {
	return func(c *LLMOptions) {
		c.Timeout = 0
		if timeout > 0 {
			c.Timeout = int((timeout + time.Second - 1) / time.Second)
		}
	}
}

// WithStreamIdleTimeout sets the maximum gap between two data receipts in a
// streaming response; when exceeded, the stream is aborted with a retryable
// timeout. An overloaded server may stall after sending only the first chunk,
// which the response header timeout (WithTimeout) does not cover. 0 means no
// limit.
func WithStreamIdleTimeout(d time.Duration) ClientOption {
	return func(c *LLMOptions) { c.StreamIdleTimeout = max(d, 0) }
}

// WithStreamTimeout sets the upper bound on the total duration of one streaming
// request; when exceeded, the stream is aborted with a retryable timeout. An
// overloaded server may trickle data so the idle timeout never fires; the bound
// should exceed the time the longest normal answer takes. 0 means no limit.
func WithStreamTimeout(d time.Duration) ClientOption {
	return func(c *LLMOptions) { c.StreamTimeout = max(d, 0) }
}

// WithStreamUsage controls whether OpenAI-compatible streaming requests
// (including DeepSeek) send stream_options.include_usage. It defaults to true;
// pass false for strict gateways that reject unknown fields, in which case the
// stream usually carries no usage.
func WithStreamUsage(enabled bool) ClientOption {
	return func(c *LLMOptions) { c.DisableStreamUsage = !enabled }
}

// WithTopP sets the Top-P sampling parameter.
func WithTopP(topP float64) ClientOption {
	return func(c *LLMOptions) {
		if topP >= 0 && topP <= 1.0 {
			c.TopP = topP
		}
	}
}

// WithSystemPrompt sets the default system prompt.
func WithSystemPrompt(prompt string) ClientOption {
	return func(c *LLMOptions) {
		c.SystemPrompt = prompt
	}
}

// WithResponseFormat sets the response format.
//
// Supported format types:
//   - "text": plain text format (default)
//   - "json_object": JSON object format (ensures the LLM returns valid JSON)
//
// OpenAI-compatible providers (OpenAI, DeepSeek, Kimi, SiliconFlow, Ollama)
// send it as response_format; Gemini maps it to responseMimeType and rejects
// other types. Anthropic has no JSON mode, so NewAnthropicWithOptions rejects
// anything but "text". OpenAI and DeepSeek require the prompt to mention JSON.
//
// Example:
//
//	// Use a predefined constant
//	llm.WithResponseFormat(llm.ResponseFormatJSON)
//
//	// Custom format
//	llm.WithResponseFormat(&llm.ResponseFormat{Type: "json_object"})
func WithResponseFormat(format *ResponseFormat) ClientOption {
	return func(c *LLMOptions) {
		c.ResponseFormat = format
	}
}

// WithJSONResponse is a convenience option that sets the response format to
// JSON.
//
// It is equivalent to WithResponseFormat(ResponseFormatJSON).
func WithJSONResponse() ClientOption {
	return func(c *LLMOptions) {
		c.ResponseFormat = ResponseFormatJSON
	}
}

// WithHTTPClient makes every provider send its requests through c.Transport
// (http.DefaultTransport when nil) and apply c.CheckRedirect, so hosts can
// enforce outbound policies such as SSRF protection. Timeouts and custom
// headers still apply; c.Timeout and c.Jar are ignored.
func WithHTTPClient(c *http.Client) ClientOption {
	return func(o *LLMOptions) { o.HTTPClient = c }
}

// WithCustomHeaders sets custom HTTP headers.
func WithCustomHeaders(headers map[string]string) ClientOption {
	return func(c *LLMOptions) {
		if c.CustomHeaders == nil {
			c.CustomHeaders = make(map[string]string)
		}
		for k, v := range headers {
			c.CustomHeaders[k] = v
		}
	}
}

// ApplyOptions applies options to the configuration.
func ApplyOptions(config *LLMOptions, opts ...ClientOption) *LLMOptions {
	if config == nil {
		config = DefaultLLMOptions()
	}

	for _, opt := range opts {
		opt(config)
	}

	return config
}

// NewLLMOptionsWithOptions creates a new configuration from options.
func NewLLMOptionsWithOptions(opts ...ClientOption) *LLMOptions {
	config := DefaultLLMOptions()
	return ApplyOptions(config, opts...)
}

// WithReasoning sets explicit reasoning controls. Providers reject unsupported
// controls before sending; service/model-specific restrictions may return API errors.
func WithReasoning(config usage.ReasoningConfig) ClientOption {
	return func(c *LLMOptions) { copy := config; c.Reasoning = &copy }
}
