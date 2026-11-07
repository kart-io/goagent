package llm

import (
	"github.com/kart-io/goagent/core/usage"
	"time"

	shared "github.com/kart-io/goagent/constants"
)

// ClientOption 定义 LLM 客户端配置选项
type ClientOption func(*LLMOptions)

// DefaultClientConfig 返回默认的客户端配置
func DefaultClientConfig() *LLMOptions {
	return &LLMOptions{
		Provider:    shared.ProviderCustom,
		MaxTokens:   shared.DefaultClientMaxTokens,
		Temperature: shared.DefaultTemperature,
		Timeout:     shared.DefaultLLMTimeoutSeconds,
		TopP:        shared.DefaultTopP,
	}
}

// WithProvider 设置 LLM 提供商
func WithProvider(provider shared.Provider) ClientOption {
	return func(c *LLMOptions) {
		c.Provider = provider
	}
}

// WithAPIKey 设置 API 密钥
func WithAPIKey(apiKey string) ClientOption {
	return func(c *LLMOptions) {
		c.APIKey = apiKey
	}
}

// WithBaseURL 设置自定义 API 端点
func WithBaseURL(baseURL string) ClientOption {
	return func(c *LLMOptions) {
		c.BaseURL = baseURL
	}
}

// WithModel 设置默认模型
func WithModel(model string) ClientOption {
	return func(c *LLMOptions) {
		c.Model = model
	}
}

// WithEmbeddingModel selects a separate embedding model for providers that support it.
func WithEmbeddingModel(model string) ClientOption {
	return func(c *LLMOptions) { c.EmbeddingModel = model }
}

// WithMaxTokens 设置最大 token 数
func WithMaxTokens(maxTokens int) ClientOption {
	return func(c *LLMOptions) {
		if maxTokens > 0 {
			c.MaxTokens = maxTokens
		}
	}
}

// WithTemperature 设置温度参数
func WithTemperature(temperature float64) ClientOption {
	return func(c *LLMOptions) {
		if temperature >= 0 && temperature <= 2.0 {
			c.Temperature = temperature
			c.TemperatureSet = true
		}
	}
}

// WithTimeout 设置请求超时时间。配置按整秒保存，正数时长向上取整，避免不足 1 秒的值被截断为 0 而当作未设置。
func WithTimeout(timeout time.Duration) ClientOption {
	return func(c *LLMOptions) {
		c.Timeout = 0
		if timeout > 0 {
			c.Timeout = int((timeout + time.Second - 1) / time.Second)
		}
	}
}

// WithStreamIdleTimeout 设置流式响应两次收到数据之间的最长间隔，超过时中断并报告可重试的超时。
// 服务端过载时流可能只发出首包就停住，响应头超时（WithTimeout）管不到。0 表示不限制。
func WithStreamIdleTimeout(d time.Duration) ClientOption {
	return func(c *LLMOptions) { c.StreamIdleTimeout = max(d, 0) }
}

// WithStreamTimeout 设置一次流式请求的总时长上限，超过时中断并报告可重试的超时。
// 服务端过载时数据可能一点点地来，空闲时限触发不了；上限应大于正常最长回答所需的时间。0 表示不限制。
func WithStreamTimeout(d time.Duration) ClientOption {
	return func(c *LLMOptions) { c.StreamTimeout = max(d, 0) }
}

// WithTopP 设置 Top-P 采样参数
func WithTopP(topP float64) ClientOption {
	return func(c *LLMOptions) {
		if topP >= 0 && topP <= 1.0 {
			c.TopP = topP
		}
	}
}

// WithSystemPrompt 设置默认系统提示
func WithSystemPrompt(prompt string) ClientOption {
	return func(c *LLMOptions) {
		c.SystemPrompt = prompt
	}
}

// WithResponseFormat 设置响应格式
//
// 支持的格式类型：
//   - "text": 普通文本格式（默认）
//   - "json_object": JSON 对象格式（确保 LLM 返回有效的 JSON）
//
// 使用示例：
//
//	// 使用预定义常量
//	llm.WithResponseFormat(llm.ResponseFormatJSON)
//
//	// 自定义格式
//	llm.WithResponseFormat(&llm.ResponseFormat{Type: "json_object"})
func WithResponseFormat(format *ResponseFormat) ClientOption {
	return func(c *LLMOptions) {
		c.ResponseFormat = format
	}
}

// WithJSONResponse 便捷方法：设置响应格式为 JSON
//
// 等同于 WithResponseFormat(ResponseFormatJSON)
func WithJSONResponse() ClientOption {
	return func(c *LLMOptions) {
		c.ResponseFormat = ResponseFormatJSON
	}
}

// WithCustomHeaders 设置自定义 HTTP 头
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

// ApplyOptions 应用选项到配置
func ApplyOptions(config *LLMOptions, opts ...ClientOption) *LLMOptions {
	if config == nil {
		config = DefaultLLMOptions()
	}

	for _, opt := range opts {
		opt(config)
	}

	return config
}

// NewConfigWithOptions 使用选项创建新配置
func NewLLMOptionsWithOptions(opts ...ClientOption) *LLMOptions {
	config := DefaultLLMOptions()
	return ApplyOptions(config, opts...)
}

// WithReasoning sets explicit reasoning controls. Providers reject unsupported
// controls before sending; service/model-specific restrictions may return API errors.
func WithReasoning(config usage.ReasoningConfig) ClientOption {
	return func(c *LLMOptions) { copy := config; c.Reasoning = &copy }
}
