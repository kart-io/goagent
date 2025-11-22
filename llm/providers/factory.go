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

// CreateClient 根据配置创建相应的 LLM 客户端
// 内部使用 Options 模式，确保统一的配置处理
func (f *ClientFactory) CreateClient(config *agentllm.LLMOptions) (agentllm.Client, error) {
	// 准备配置（验证、设置默认值、从环境变量读取）
	if err := agentllm.PrepareConfig(config); err != nil {
		return nil, err
	}

	// 将配置转换为 Options，使用统一的 WithOptions 版本
	opts := ConfigToOptions(config)

	// 根据提供商创建客户端，优先使用 WithOptions 版本
	switch config.Provider {
	case constants.ProviderOpenAI:
		return NewOpenAIWithOptions(opts...)

	case constants.ProviderAnthropic:
		return NewAnthropicWithOptions(opts...)

	case constants.ProviderGemini:
		return NewGeminiWithOptions(opts...)

	case constants.ProviderDeepSeek:
		return NewDeepSeekWithOptions(opts...)

	case constants.ProviderKimi:
		return NewKimiWithOptions(opts...)

	case constants.ProviderSiliconFlow:
		return NewSiliconFlowWithOptions(opts...)

	case constants.ProviderOllama:
		return NewOllamaWithOptions(opts...)

	case constants.ProviderCohere:
		return NewCohereWithOptions(opts...)

	case constants.ProviderHuggingFace:
		return NewHuggingFaceWithOptions(opts...)

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

// createClientWithProvider 通用的客户端创建辅助函数
func createClientWithProvider(provider constants.Provider, apiKey string, opts ...agentllm.ClientOption) (agentllm.Client, error) {
	factory := NewClientFactory()
	allOpts := append([]agentllm.ClientOption{
		agentllm.WithProvider(provider),
		agentllm.WithAPIKey(apiKey),
	}, opts...)

	return factory.CreateClientWithOptions(allOpts...)
}

// CreateOpenAIClient 创建 OpenAI 客户端
func CreateOpenAIClient(apiKey string, opts ...agentllm.ClientOption) (agentllm.Client, error) {
	return createClientWithProvider(constants.ProviderOpenAI, apiKey, opts...)
}

// CreateAnthropicClient 创建 Anthropic 客户端
func CreateAnthropicClient(apiKey string, opts ...agentllm.ClientOption) (agentllm.Client, error) {
	return createClientWithProvider(constants.ProviderAnthropic, apiKey, opts...)
}

// CreateGeminiClient 创建 Gemini 客户端
func CreateGeminiClient(apiKey string, opts ...agentllm.ClientOption) (agentllm.Client, error) {
	return createClientWithProvider(constants.ProviderGemini, apiKey, opts...)
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
