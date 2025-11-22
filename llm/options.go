package llm

import (
	"time"
)

// ClientOption 定义 LLM 客户端配置选项
type ClientOption func(*Config)

// DefaultClientConfig 返回默认的客户端配置
func DefaultClientConfig() *Config {
	return &Config{
		Provider:    ProviderOpenAI,
		MaxTokens:   2000,
		Temperature: 0.7,
		Timeout:     60,
		TopP:        1.0,
	}
}

// WithProvider 设置 LLM 提供商
func WithProvider(provider Provider) ClientOption {
	return func(c *Config) {
		c.Provider = provider
	}
}

// WithAPIKey 设置 API 密钥
func WithAPIKey(apiKey string) ClientOption {
	return func(c *Config) {
		c.APIKey = apiKey
	}
}

// WithBaseURL 设置自定义 API 端点
func WithBaseURL(baseURL string) ClientOption {
	return func(c *Config) {
		c.BaseURL = baseURL
	}
}

// WithModel 设置默认模型
func WithModel(model string) ClientOption {
	return func(c *Config) {
		c.Model = model
	}
}

// WithMaxTokens 设置最大 token 数
func WithMaxTokens(maxTokens int) ClientOption {
	return func(c *Config) {
		if maxTokens > 0 {
			c.MaxTokens = maxTokens
		}
	}
}

// WithTemperature 设置温度参数
func WithTemperature(temperature float64) ClientOption {
	return func(c *Config) {
		if temperature >= 0 && temperature <= 2.0 {
			c.Temperature = temperature
		}
	}
}

// WithTimeout 设置请求超时时间
func WithTimeout(timeout time.Duration) ClientOption {
	return func(c *Config) {
		c.Timeout = int(timeout.Seconds())
	}
}

// WithTopP 设置 Top-P 采样参数
func WithTopP(topP float64) ClientOption {
	return func(c *Config) {
		if topP >= 0 && topP <= 1.0 {
			c.TopP = topP
		}
	}
}

// WithOrganizationID 设置组织 ID（用于 OpenAI）
func WithOrganizationID(orgID string) ClientOption {
	return func(c *Config) {
		c.OrganizationID = orgID
	}
}

// WithRetryCount 设置重试次数
func WithRetryCount(retryCount int) ClientOption {
	return func(c *Config) {
		if retryCount >= 0 {
			c.RetryCount = retryCount
		}
	}
}

// WithRetryDelay 设置重试延迟
func WithRetryDelay(delay time.Duration) ClientOption {
	return func(c *Config) {
		c.RetryDelay = delay
	}
}

// WithRateLimiting 配置速率限制
func WithRateLimiting(requestsPerMinute int) ClientOption {
	return func(c *Config) {
		c.RateLimitRPM = requestsPerMinute
	}
}

// WithProxy 设置代理 URL
func WithProxy(proxyURL string) ClientOption {
	return func(c *Config) {
		c.ProxyURL = proxyURL
	}
}

// WithSystemPrompt 设置默认系统提示
func WithSystemPrompt(prompt string) ClientOption {
	return func(c *Config) {
		c.SystemPrompt = prompt
	}
}

// WithCache 启用响应缓存
func WithCache(enabled bool, ttl time.Duration) ClientOption {
	return func(c *Config) {
		c.CacheEnabled = enabled
		c.CacheTTL = ttl
	}
}

// WithStreamingEnabled 启用流式响应
func WithStreamingEnabled(enabled bool) ClientOption {
	return func(c *Config) {
		c.StreamingEnabled = enabled
	}
}

// WithCustomHeaders 设置自定义 HTTP 头
func WithCustomHeaders(headers map[string]string) ClientOption {
	return func(c *Config) {
		if c.CustomHeaders == nil {
			c.CustomHeaders = make(map[string]string)
		}
		for k, v := range headers {
			c.CustomHeaders[k] = v
		}
	}
}

// PresetOption 预设配置选项类型
type PresetOption int

const (
	// PresetDevelopment 开发环境预设
	PresetDevelopment PresetOption = iota
	// PresetProduction 生产环境预设
	PresetProduction
	// PresetLowCost 低成本预设
	PresetLowCost
	// PresetHighQuality 高质量预设
	PresetHighQuality
	// PresetFast 快速响应预设
	PresetFast
)

// WithPreset 应用预设配置
func WithPreset(preset PresetOption) ClientOption {
	return func(c *Config) {
		switch preset {
		case PresetDevelopment:
			c.Model = "gpt-3.5-turbo"
			c.MaxTokens = 1000
			c.Temperature = 0.5
			c.Timeout = 30
			c.RetryCount = 1

		case PresetProduction:
			c.Model = "gpt-4"
			c.MaxTokens = 2000
			c.Temperature = 0.7
			c.Timeout = 60
			c.RetryCount = 3
			c.CacheEnabled = true
			c.CacheTTL = 5 * time.Minute

		case PresetLowCost:
			c.Model = "gpt-3.5-turbo"
			c.MaxTokens = 500
			c.Temperature = 0.3
			c.CacheEnabled = true
			c.CacheTTL = 10 * time.Minute

		case PresetHighQuality:
			c.Model = "gpt-4-turbo-preview"
			c.MaxTokens = 4000
			c.Temperature = 0.8
			c.TopP = 0.95
			c.Timeout = 120

		case PresetFast:
			c.Model = "gpt-3.5-turbo-16k"
			c.MaxTokens = 1000
			c.Temperature = 0.3
			c.Timeout = 15
			c.StreamingEnabled = true
		}
	}
}

// ProviderPreset 针对特定提供商的预设配置
func WithProviderPreset(provider Provider) ClientOption {
	return func(c *Config) {
		c.Provider = provider

		switch provider {
		case ProviderOpenAI:
			c.Model = "gpt-4-turbo-preview"
			c.MaxTokens = 4096

		case ProviderAnthropic:
			c.Model = "claude-3-sonnet-20240229"
			c.MaxTokens = 4096

		case ProviderGemini:
			c.Model = "gemini-pro"
			c.MaxTokens = 8192

		case ProviderDeepSeek:
			c.Model = "deepseek-chat"
			c.MaxTokens = 4096
			c.BaseURL = "https://api.deepseek.com/v1"

		case ProviderKimi:
			c.Model = "moonshot-v1-8k"
			c.MaxTokens = 8192
			c.BaseURL = "https://api.moonshot.cn/v1"

		case ProviderSiliconFlow:
			c.Model = "Qwen/Qwen2-7B-Instruct"
			c.MaxTokens = 4096
			c.BaseURL = "https://api.siliconflow.cn/v1"

		case ProviderOllama:
			c.Model = "llama2"
			c.MaxTokens = 2048
			c.BaseURL = "http://localhost:11434"

		case ProviderCohere:
			c.Model = "command-r-plus"
			c.MaxTokens = 4096

		case ProviderHuggingFace:
			c.Model = "meta-llama/Llama-2-7b-chat-hf"
			c.MaxTokens = 2048
		}
	}
}

// UseCase 定义使用场景
type UseCase int

const (
	// UseCaseChat 聊天对话
	UseCaseChat UseCase = iota
	// UseCaseCodeGeneration 代码生成
	UseCaseCodeGeneration
	// UseCaseTranslation 翻译
	UseCaseTranslation
	// UseCaseSummarization 摘要生成
	UseCaseSummarization
	// UseCaseAnalysis 分析任务
	UseCaseAnalysis
	// UseCaseCreativeWriting 创意写作
	UseCaseCreativeWriting
)

// WithUseCase 根据使用场景优化配置
func WithUseCase(useCase UseCase) ClientOption {
	return func(c *Config) {
		switch useCase {
		case UseCaseChat:
			c.Temperature = 0.7
			c.MaxTokens = 1500
			c.TopP = 0.9

		case UseCaseCodeGeneration:
			c.Temperature = 0.2
			c.MaxTokens = 2500
			c.TopP = 0.95
			c.Model = "gpt-4" // 代码生成推荐使用更强的模型

		case UseCaseTranslation:
			c.Temperature = 0.3
			c.MaxTokens = 2000
			c.TopP = 1.0

		case UseCaseSummarization:
			c.Temperature = 0.3
			c.MaxTokens = 500
			c.TopP = 0.9

		case UseCaseAnalysis:
			c.Temperature = 0.5
			c.MaxTokens = 3000
			c.TopP = 0.95

		case UseCaseCreativeWriting:
			c.Temperature = 0.9
			c.MaxTokens = 4000
			c.TopP = 0.95
		}
	}
}

// ApplyOptions 应用选项到配置
func ApplyOptions(config *Config, opts ...ClientOption) *Config {
	if config == nil {
		config = DefaultClientConfig()
	}

	for _, opt := range opts {
		opt(config)
	}

	return config
}

// NewConfigWithOptions 使用选项创建新配置
func NewConfigWithOptions(opts ...ClientOption) *Config {
	config := DefaultClientConfig()
	return ApplyOptions(config, opts...)
}
