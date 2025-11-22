package llm

import (
	"fmt"
	"os"

	agentErrors "github.com/kart-io/goagent/errors"
)

// ClientFactory 用于创建 LLM 客户端的工厂接口
type ClientFactory interface {
	CreateClient(config *Config) (Client, error)
}

// NewClientWithOptions 使用选项模式创建 LLM 客户端
// 注意：实际的客户端创建需要在应用层或 providers 包中实现
func NewClientWithOptions(opts ...ClientOption) (Client, error) {
	// 创建配置
	config := NewConfigWithOptions(opts...)

	// 验证配置
	if err := PrepareConfig(config); err != nil {
		return nil, err
	}

	// 实际的客户端创建需要在应用层实现
	// 以避免循环导入问题
	return nil, fmt.Errorf("client creation must be implemented at application layer to avoid circular imports - use providers package directly")
}

// PrepareConfig 准备和验证配置
func PrepareConfig(config *Config) error {
	// 从环境变量补充配置
	if config.APIKey == "" {
		config.APIKey = getAPIKeyFromEnv(config.Provider)
	}

	// 验证必要的配置
	return validateConfig(config)
}

// NeedsEnhancedFeatures 检查是否需要增强功能（导出给 providers 包使用）
func NeedsEnhancedFeatures(config *Config) bool {
	return config.RetryCount > 0 ||
		config.CacheEnabled ||
		config.SystemPrompt != "" ||
		config.StreamingEnabled ||
		len(config.CustomHeaders) > 0 ||
		config.OrganizationID != ""
}

// getAPIKeyFromEnv 从环境变量获取 API 密钥
func getAPIKeyFromEnv(provider Provider) string {
	envVarMap := map[Provider]string{
		ProviderOpenAI:      "OPENAI_API_KEY",
		ProviderAnthropic:   "ANTHROPIC_API_KEY",
		ProviderGemini:      "GOOGLE_API_KEY",
		ProviderDeepSeek:    "DEEPSEEK_API_KEY",
		ProviderKimi:        "KIMI_API_KEY",
		ProviderSiliconFlow: "SILICONFLOW_API_KEY",
		ProviderCohere:      "COHERE_API_KEY",
		ProviderHuggingFace: "HUGGINGFACE_API_KEY",
	}

	if envVar, ok := envVarMap[provider]; ok {
		return os.Getenv(envVar)
	}
	return ""
}

// validateConfig 验证配置的有效性
func validateConfig(config *Config) error {
	if config == nil {
		return agentErrors.NewInvalidConfigError("", "config", "config is nil")
	}

	// 验证提供商
	if config.Provider == "" {
		return agentErrors.NewInvalidConfigError("", "provider", "provider is required")
	}

	// 某些提供商需要 API 密钥
	requiresAPIKey := map[Provider]bool{
		ProviderOpenAI:      true,
		ProviderAnthropic:   true,
		ProviderGemini:      true,
		ProviderDeepSeek:    true,
		ProviderKimi:        true,
		ProviderSiliconFlow: true,
		ProviderCohere:      true,
		ProviderHuggingFace: true,
		ProviderOllama:      false, // Ollama 不需要 API key
	}

	if requiresKey, ok := requiresAPIKey[config.Provider]; ok && requiresKey {
		if config.APIKey == "" {
			return agentErrors.NewInvalidConfigError(
				string(config.Provider),
				"api_key",
				fmt.Sprintf("%s requires API key", config.Provider),
			)
		}
	}

	// 验证参数范围
	if config.Temperature < 0 || config.Temperature > 2.0 {
		config.Temperature = 0.7 // 使用默认值
	}

	if config.TopP < 0 || config.TopP > 1.0 {
		config.TopP = 1.0 // 使用默认值
	}

	if config.MaxTokens <= 0 {
		config.MaxTokens = 2000 // 使用默认值
	}

	if config.Timeout <= 0 {
		config.Timeout = 60 // 默认 60 秒
	}

	return nil
}
