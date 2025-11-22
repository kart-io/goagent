package llm

import (
	"fmt"
	"os"

	agentErrors "github.com/kart-io/goagent/errors"
	"github.com/kart-io/goagent/llm/constants"
)

// ClientFactory 用于创建 LLM 客户端的工厂接口
type ClientFactory interface {
	CreateClient(opts *LLMOptions) (Client, error)
}

// NewClientWithOptions 使用选项模式创建 LLM 客户端
// 注意：实际的客户端创建需要在应用层或 providers 包中实现
func NewClientWithOptions(opts ...ClientOption) (Client, error) {
	// 创建配置
	config := NewLLMOptionsWithOptions(opts...)

	// 验证配置
	if err := PrepareConfig(config); err != nil {
		return nil, err
	}

	// 实际的客户端创建需要在应用层实现
	// 以避免循环导入问题
	return nil, fmt.Errorf("client creation must be implemented at application layer to avoid circular imports - use providers package directly")
}

// PrepareConfig 准备和验证配置
func PrepareConfig(opts *LLMOptions) error {
	// 从环境变量补充配置
	if opts.APIKey == "" {
		opts.APIKey = getAPIKeyFromEnv(opts.Provider)
	}

	// 验证必要的配置
	return validateConfig(opts)
}

// NeedsEnhancedFeatures 检查是否需要增强功能（导出给 providers 包使用）
func NeedsEnhancedFeatures(opts *LLMOptions) bool {
	return opts.RetryCount > 0 ||
		opts.CacheEnabled ||
		opts.SystemPrompt != "" ||
		opts.StreamingEnabled ||
		len(opts.CustomHeaders) > 0 ||
		opts.OrganizationID != ""
}

// getAPIKeyFromEnv 从环境变量获取 API 密钥
func getAPIKeyFromEnv(provider constants.Provider) string {
	envVarMap := map[constants.Provider]string{
		constants.ProviderOpenAI:      constants.EnvOpenAIAPIKey,
		constants.ProviderAnthropic:   constants.EnvAnthropicAPIKey,
		constants.ProviderGemini:      constants.EnvGeminiAPIKey,
		constants.ProviderDeepSeek:    constants.EnvDeepSeekAPIKey,
		constants.ProviderKimi:        constants.EnvKimiAPIKey,
		constants.ProviderSiliconFlow: constants.EnvSiliconFlowAPIKey,
		constants.ProviderCohere:      constants.EnvCohereAPIKey,
		constants.ProviderHuggingFace: constants.EnvHuggingFaceAPIKey,
	}

	if envVar, ok := envVarMap[provider]; ok {
		return os.Getenv(envVar)
	}
	return ""
}

// validateConfig 验证配置的有效性
func validateConfig(opts *LLMOptions) error {
	if opts == nil {
		return agentErrors.NewInvalidConfigError("", "config", "config is nil")
	}

	// 验证提供商
	if opts.Provider == "" {
		return agentErrors.NewInvalidConfigError("", "provider", "provider is required")
	}

	// 某些提供商需要 API 密钥
	requiresAPIKey := map[constants.Provider]bool{
		constants.ProviderOpenAI:      true,
		constants.ProviderAnthropic:   true,
		constants.ProviderGemini:      true,
		constants.ProviderDeepSeek:    true,
		constants.ProviderKimi:        true,
		constants.ProviderSiliconFlow: true,
		constants.ProviderCohere:      true,
		constants.ProviderHuggingFace: true,
		constants.ProviderOllama:      false, // Ollama 不需要 API key
	}

	if requiresKey, ok := requiresAPIKey[opts.Provider]; ok && requiresKey {
		if opts.APIKey == "" {
			return agentErrors.NewInvalidConfigError(
				string(opts.Provider),
				"api_key",
				fmt.Sprintf("%s requires API key", opts.Provider),
			)
		}
	}

	// 验证参数范围
	if opts.Temperature < 0 || opts.Temperature > 2.0 {
		opts.Temperature = 0.7 // 使用默认值
	}

	if opts.TopP < 0 || opts.TopP > 1.0 {
		opts.TopP = 1.0 // 使用默认值
	}

	if opts.MaxTokens <= 0 {
		opts.MaxTokens = 2000 // 使用默认值
	}

	if opts.Timeout <= 0 {
		opts.Timeout = 60 // 默认 60 秒
	}

	return nil
}
