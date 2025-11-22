package providers

import (
	"fmt"
	"os"
	"time"

	agentErrors "github.com/kart-io/goagent/errors"
	agentllm "github.com/kart-io/goagent/llm"
	"github.com/kart-io/goagent/llm/constants"
)

// BaseProvider 封装了所有 LLM Provider 共有的配置和逻辑
type BaseProvider struct {
	Config *agentllm.LLMOptions
}

// NewBaseProvider 初始化基础 Provider，统一处理 Options
func NewBaseProvider(opts ...agentllm.ClientOption) *BaseProvider {
	// 使用 llm/options.go 中的 ApplyOptions
	config := agentllm.NewLLMOptionsWithOptions(opts...)
	return &BaseProvider{
		Config: config,
	}
}

// NewBaseProviderWithConfig 从现有配置创建 BaseProvider（用于向后兼容）
func NewBaseProviderWithConfig(config *agentllm.LLMOptions) *BaseProvider {
	if config == nil {
		config = agentllm.DefaultLLMOptions()
	}
	return &BaseProvider{
		Config: config,
	}
}

// ApplyProviderDefaults 应用 Provider 特定的默认值
func (b *BaseProvider) ApplyProviderDefaults(provider constants.Provider, defaultBaseURL, defaultModel string, envBaseURL, envModel string) {
	// 设置 Provider
	b.Config.Provider = provider

	// 确保 BaseURL
	b.EnsureBaseURL(envBaseURL, defaultBaseURL)

	// 确保 Model
	b.EnsureModel(envModel, defaultModel)
}

// ConfigToOptions 将 LLMOptions 转换为 ClientOption 列表（用于向后兼容）
func ConfigToOptions(config *agentllm.LLMOptions) []agentllm.ClientOption {
	if config == nil {
		return nil
	}

	var opts []agentllm.ClientOption
	if config.Provider != "" {
		opts = append(opts, agentllm.WithProvider(config.Provider))
	}
	if config.APIKey != "" {
		opts = append(opts, agentllm.WithAPIKey(config.APIKey))
	}
	if config.BaseURL != "" {
		opts = append(opts, agentllm.WithBaseURL(config.BaseURL))
	}
	if config.Model != "" {
		opts = append(opts, agentllm.WithModel(config.Model))
	}
	if config.MaxTokens > 0 {
		opts = append(opts, agentllm.WithMaxTokens(config.MaxTokens))
	}
	if config.Temperature > 0 {
		opts = append(opts, agentllm.WithTemperature(config.Temperature))
	}
	if config.Timeout > 0 {
		opts = append(opts, agentllm.WithTimeout(time.Duration(config.Timeout)*time.Second))
	}
	if config.TopP > 0 {
		opts = append(opts, agentllm.WithTopP(config.TopP))
	}
	if config.ProxyURL != "" {
		opts = append(opts, agentllm.WithProxy(config.ProxyURL))
	}
	if config.RetryCount > 0 {
		opts = append(opts, agentllm.WithRetryCount(config.RetryCount))
	}
	if config.RetryDelay > 0 {
		opts = append(opts, agentllm.WithRetryDelay(config.RetryDelay))
	}
	if config.RateLimitRPM > 0 {
		opts = append(opts, agentllm.WithRateLimiting(config.RateLimitRPM))
	}
	if config.SystemPrompt != "" {
		opts = append(opts, agentllm.WithSystemPrompt(config.SystemPrompt))
	}
	if config.CacheEnabled {
		opts = append(opts, agentllm.WithCache(config.CacheEnabled, config.CacheTTL))
	}
	if config.StreamingEnabled {
		opts = append(opts, agentllm.WithStreamingEnabled(config.StreamingEnabled))
	}
	if config.OrganizationID != "" {
		opts = append(opts, agentllm.WithOrganizationID(config.OrganizationID))
	}
	if len(config.CustomHeaders) > 0 {
		opts = append(opts, agentllm.WithCustomHeaders(config.CustomHeaders))
	}
	return opts
}

// EnsureAPIKey 统一检查 API Key，支持环境变量回退
func (b *BaseProvider) EnsureAPIKey(envVar string, providerName constants.Provider) error {
	if b.Config.APIKey == "" {
		b.Config.APIKey = os.Getenv(envVar)
	}
	if b.Config.APIKey == "" {
		return agentErrors.NewInvalidConfigError(string(providerName), constants.ErrorFieldAPIKey, fmt.Sprintf(constants.ErrAPIKeyMissing, string(providerName)))
	}
	return nil
}

// EnsureBaseURL 统一检查 BaseURL，支持环境变量回退和默认值
func (b *BaseProvider) EnsureBaseURL(envVar string, defaultURL string) {
	if b.Config.BaseURL == "" {
		b.Config.BaseURL = os.Getenv(envVar)
	}
	if b.Config.BaseURL == "" {
		b.Config.BaseURL = defaultURL
	}
}

// EnsureModel 统一检查 Model，支持环境变量回退和默认值
func (b *BaseProvider) EnsureModel(envVar string, defaultModel string) {
	if b.Config.Model == "" {
		b.Config.Model = os.Getenv(envVar)
	}
	if b.Config.Model == "" {
		b.Config.Model = defaultModel
	}
}

// GetModel 获取模型名称，优先使用请求中的模型，否则使用配置的模型
func (b *BaseProvider) GetModel(reqModel string) string {
	if reqModel != "" {
		return reqModel
	}
	return b.Config.Model
}

// GetMaxTokens 获取最大 token 数，支持回退到默认值
func (b *BaseProvider) GetMaxTokens(reqMaxTokens int) int {
	if reqMaxTokens > 0 {
		return reqMaxTokens
	}
	if b.Config.MaxTokens > 0 {
		return b.Config.MaxTokens
	}
	return constants.DefaultMaxTokens
}

// GetTemperature 获取温度参数，支持回退到默认值
func (b *BaseProvider) GetTemperature(reqTemperature float64) float64 {
	if reqTemperature > 0 {
		return reqTemperature
	}
	if b.Config.Temperature > 0 {
		return b.Config.Temperature
	}
	return constants.DefaultTemperature
}

// GetTimeout 获取超时时间，支持回退到默认值
func (b *BaseProvider) GetTimeout() time.Duration {
	if b.Config.Timeout > 0 {
		return time.Duration(b.Config.Timeout) * time.Second
	}
	return constants.DefaultTimeout
}

// GetTopP 获取 TopP 参数，支持回退到默认值
func (b *BaseProvider) GetTopP(reqTopP float64) float64 {
	if reqTopP > 0 {
		return reqTopP
	}
	if b.Config.TopP > 0 {
		return b.Config.TopP
	}
	return constants.DefaultTopP
}
