package providers

import (
	"fmt"
	"time"

	agentllm "github.com/kart-io/goagent/llm"
	"github.com/kart-io/goagent/llm/constants"
)

// ClientFactory 统一的客户端工厂
type ClientFactory struct{}

// NewClientFactory 创建新的客户端工厂
func NewClientFactory() *ClientFactory {
	return &ClientFactory{}
}

// needsEnhancedFeatures 检查是否需要增强功能
func needsEnhancedFeatures(config *agentllm.LLMOptions) bool {
	return config.RetryCount > 0 ||
		config.CacheEnabled ||
		config.SystemPrompt != "" ||
		config.StreamingEnabled ||
		len(config.CustomHeaders) > 0 ||
		config.OrganizationID != ""
}

// CreateClient 根据配置创建相应的 LLM 客户端
func (f *ClientFactory) CreateClient(config *agentllm.LLMOptions) (agentllm.Client, error) {
	// 准备配置（验证、设置默认值、从环境变量读取）
	if err := agentllm.PrepareConfig(config); err != nil {
		return nil, err
	}

	// 根据提供商创建客户端
	switch config.Provider {
	case constants.ProviderOpenAI:
		if needsEnhancedFeatures(config) {
			return NewEnhancedOpenAI(config)
		}
		return NewOpenAI(config)

	case constants.ProviderAnthropic:
		return NewAnthropic(config)

	case constants.ProviderGemini:
		return NewGemini(config)

	case constants.ProviderDeepSeek:
		return NewDeepSeek(config)

	case constants.ProviderKimi:
		return NewKimi(config)

	case constants.ProviderSiliconFlow:
		return NewSiliconFlow(config)

	case constants.ProviderOllama:
		return NewOllama(config)

	case constants.ProviderCohere:
		return NewCohere(config)

	case constants.ProviderHuggingFace:
		return NewHuggingFace(config)

	default:
		return nil, fmt.Errorf("unsupported provider: %s", config.Provider)
	}
}

// CreateClientWithOptions 使用选项模式创建客户端
func (f *ClientFactory) CreateClientWithOptions(opts ...agentllm.ClientOption) (agentllm.Client, error) {
	// 创建配置
	config := agentllm.NewLLMOptionsWithOptions(opts...)

	// 使用配置创建客户端
	return f.CreateClient(config)
}

// 便捷方法

// CreateOpenAIClient 创建 OpenAI 客户端
func CreateOpenAIClient(apiKey string, opts ...agentllm.ClientOption) (agentllm.Client, error) {
	factory := NewClientFactory()
	allOpts := append([]agentllm.ClientOption{
		agentllm.WithProvider(constants.ProviderOpenAI),
		agentllm.WithAPIKey(apiKey),
	}, opts...)

	return factory.CreateClientWithOptions(allOpts...)
}

// CreateAnthropicClient 创建 Anthropic 客户端
func CreateAnthropicClient(apiKey string, opts ...agentllm.ClientOption) (agentllm.Client, error) {
	factory := NewClientFactory()
	allOpts := append([]agentllm.ClientOption{
		agentllm.WithProvider(constants.ProviderAnthropic),
		agentllm.WithAPIKey(apiKey),
	}, opts...)

	return factory.CreateClientWithOptions(allOpts...)
}

// CreateGeminiClient 创建 Gemini 客户端
func CreateGeminiClient(apiKey string, opts ...agentllm.ClientOption) (agentllm.Client, error) {
	factory := NewClientFactory()
	allOpts := append([]agentllm.ClientOption{
		agentllm.WithProvider(constants.ProviderGemini),
		agentllm.WithAPIKey(apiKey),
	}, opts...)

	return factory.CreateClientWithOptions(allOpts...)
}

// CreateOllamaClient 创建 Ollama 客户端（本地运行，不需要 API key）
func CreateOllamaClient(model string, opts ...agentllm.ClientOption) (agentllm.Client, error) {
	factory := NewClientFactory()

	// Ollama 默认配置
	allOpts := append([]agentllm.ClientOption{
		agentllm.WithProvider(constants.ProviderOllama),
		agentllm.WithBaseURL("http://localhost:11434"),
		agentllm.WithModel(model),
	}, opts...)

	return factory.CreateClientWithOptions(allOpts...)
}

// CreateClientForUseCase 根据使用场景创建优化的客户端
func CreateClientForUseCase(provider constants.Provider, apiKey string, useCase agentllm.UseCase, opts ...agentllm.ClientOption) (agentllm.Client, error) {
	factory := NewClientFactory()

	// 组合选项：提供商 + API Key + 使用场景 + 自定义选项
	allOpts := append([]agentllm.ClientOption{
		agentllm.WithProvider(provider),
		agentllm.WithAPIKey(apiKey),
		agentllm.WithUseCase(useCase),
	}, opts...)

	return factory.CreateClientWithOptions(allOpts...)
}

// CreateProductionClient 创建生产环境客户端
func CreateProductionClient(provider constants.Provider, apiKey string, opts ...agentllm.ClientOption) (agentllm.Client, error) {
	factory := NewClientFactory()

	// 生产环境默认配置
	prodOpts := []agentllm.ClientOption{
		agentllm.WithProvider(provider),
		agentllm.WithAPIKey(apiKey),
		agentllm.WithPreset(agentllm.PresetProduction),
		agentllm.WithRetryCount(3),
		agentllm.WithRetryDelay(2 * time.Second),
		agentllm.WithCache(true, 10*time.Minute),
	}

	// 合并自定义选项（会覆盖默认值）
	allOpts := append(prodOpts, opts...)

	return factory.CreateClientWithOptions(allOpts...)
}

// CreateDevelopmentClient 创建开发环境客户端
func CreateDevelopmentClient(provider constants.Provider, apiKey string, opts ...agentllm.ClientOption) (agentllm.Client, error) {
	factory := NewClientFactory()

	// 开发环境默认配置
	devOpts := []agentllm.ClientOption{
		agentllm.WithProvider(provider),
		agentllm.WithAPIKey(apiKey),
		agentllm.WithPreset(agentllm.PresetDevelopment),
	}

	// 合并自定义选项
	allOpts := append(devOpts, opts...)

	return factory.CreateClientWithOptions(allOpts...)
}
