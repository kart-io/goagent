package llm

import (
	"time"

	"github.com/kart-io/goagent/llm/constants"
)

// ClientOption 定义 LLM 客户端配置选项
type ClientOption func(*LLMOptions)

// DefaultClientConfig 返回默认的客户端配置
func DefaultClientConfig() *LLMOptions {
	return &LLMOptions{
		Provider:    constants.ProviderCustom,
		MaxTokens:   2000,
		Temperature: 0.7,
		Timeout:     60,
		TopP:        1.0,
	}
}

// WithProvider 设置 LLM 提供商
func WithProvider(provider constants.Provider) ClientOption {
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
		}
	}
}

// WithTimeout 设置请求超时时间
func WithTimeout(timeout time.Duration) ClientOption {
	return func(c *LLMOptions) {
		c.Timeout = int(timeout.Seconds())
	}
}

// WithTopP 设置 Top-P 采样参数
func WithTopP(topP float64) ClientOption {
	return func(c *LLMOptions) {
		if topP >= 0 && topP <= 1.0 {
			c.TopP = topP
		}
	}
}

// WithOrganizationID 设置组织 ID（用于 OpenAI）
func WithOrganizationID(orgID string) ClientOption {
	return func(c *LLMOptions) {
		c.OrganizationID = orgID
	}
}

// WithRetryCount 设置重试次数
func WithRetryCount(retryCount int) ClientOption {
	return func(c *LLMOptions) {
		if retryCount >= 0 {
			c.RetryCount = retryCount
		}
	}
}

// WithRetryDelay 设置重试延迟
func WithRetryDelay(delay time.Duration) ClientOption {
	return func(c *LLMOptions) {
		c.RetryDelay = delay
	}
}

// WithRateLimiting 配置速率限制
func WithRateLimiting(requestsPerMinute int) ClientOption {
	return func(c *LLMOptions) {
		c.RateLimitRPM = requestsPerMinute
	}
}

// WithProxy 设置代理 URL
func WithProxy(proxyURL string) ClientOption {
	return func(c *LLMOptions) {
		c.ProxyURL = proxyURL
	}
}

// WithSystemPrompt 设置默认系统提示
func WithSystemPrompt(prompt string) ClientOption {
	return func(c *LLMOptions) {
		c.SystemPrompt = prompt
	}
}

// WithCache 启用响应缓存
func WithCache(enabled bool, ttl time.Duration) ClientOption {
	return func(c *LLMOptions) {
		c.CacheEnabled = enabled
		c.CacheTTL = ttl
	}
}

// WithStreamingEnabled 启用流式响应
func WithStreamingEnabled(enabled bool) ClientOption {
	return func(c *LLMOptions) {
		c.StreamingEnabled = enabled
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
