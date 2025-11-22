package llm

import (
	"context"
	"fmt"
	"time"
)

// ExampleBasicUsage 展示基本用法
func ExampleBasicUsage() {
	// 方式1: 使用选项模式创建客户端
	client, err := NewClientWithOptions(
		WithProvider(ProviderOpenAI),
		WithAPIKey("your-api-key"),
		WithModel("gpt-4"),
		WithMaxTokens(2000),
		WithTemperature(0.7),
	)
	if err != nil {
		fmt.Printf("Failed to create client: %v\n", err)
		return
	}

	// 使用客户端
	ctx := context.Background()
	response, err := client.Chat(ctx, []Message{
		SystemMessage("You are a helpful assistant"),
		UserMessage("Hello, how are you?"),
	})
	if err != nil {
		fmt.Printf("Chat failed: %v\n", err)
		return
	}
	fmt.Printf("Response: %s\n", response.Content)
}

// ExampleWithPresets 展示使用预设配置
func ExampleWithPresets() {
	// 使用生产环境预设
	productionClient, _ := NewClientWithOptions(
		WithProvider(ProviderOpenAI),
		WithAPIKey("your-api-key"),
		WithPreset(PresetProduction),
	)
	_ = productionClient

	// 使用开发环境预设
	devClient, _ := NewClientWithOptions(
		WithProvider(ProviderOpenAI),
		WithAPIKey("your-api-key"),
		WithPreset(PresetDevelopment),
	)
	_ = devClient

	// 使用低成本预设
	lowCostClient, _ := NewClientWithOptions(
		WithProvider(ProviderOpenAI),
		WithAPIKey("your-api-key"),
		WithPreset(PresetLowCost),
	)
	_ = lowCostClient
}

// ExampleProviderSpecific 展示针对不同提供商的配置
func ExampleProviderSpecific() {
	// OpenAI 配置
	openAIClient, _ := NewClientWithOptions(
		WithProviderPreset(ProviderOpenAI),
		WithAPIKey("sk-..."),
		WithOrganizationID("org-..."),
		WithMaxTokens(4096),
	)
	_ = openAIClient

	// Anthropic 配置
	anthropicClient, _ := NewClientWithOptions(
		WithProviderPreset(ProviderAnthropic),
		WithAPIKey("sk-ant-..."),
		WithModel("claude-3-opus-20240229"),
	)
	_ = anthropicClient

	// Ollama 本地配置
	ollamaClient, _ := NewClientWithOptions(
		WithProviderPreset(ProviderOllama),
		WithBaseURL("http://localhost:11434"),
		WithModel("llama2"),
		WithTimeout(30*time.Second),
	)
	_ = ollamaClient
}

// ExampleUseCaseOptimized 展示针对不同使用场景的优化
func ExampleUseCaseOptimized() {
	// 代码生成场景
	codeGenClient, _ := NewClientWithOptions(
		WithProvider(ProviderOpenAI),
		WithAPIKey("your-api-key"),
		WithUseCase(UseCaseCodeGeneration),
		WithModel("gpt-4"), // 覆盖使用场景的默认模型
	)
	_ = codeGenClient

	// 创意写作场景
	creativeClient, _ := NewClientWithOptions(
		WithProvider(ProviderOpenAI),
		WithAPIKey("your-api-key"),
		WithUseCase(UseCaseCreativeWriting),
		WithMaxTokens(5000), // 覆盖默认值
	)
	_ = creativeClient

	// 摘要生成场景
	summaryClient, _ := NewClientWithOptions(
		WithProvider(ProviderOpenAI),
		WithAPIKey("your-api-key"),
		WithUseCase(UseCaseSummarization),
	)
	_ = summaryClient
}

// ExampleAdvancedConfiguration 展示高级配置选项
func ExampleAdvancedConfiguration() {
	// 配置重试、缓存和速率限制
	advancedClient, _ := NewClientWithOptions(
		WithProvider(ProviderOpenAI),
		WithAPIKey("your-api-key"),

		// 重试配置
		WithRetryCount(3),
		WithRetryDelay(2*time.Second),

		// 缓存配置
		WithCache(true, 10*time.Minute),

		// 速率限制
		WithRateLimiting(60), // 60 RPM

		// 代理配置
		WithProxy("http://proxy.example.com:8080"),

		// 自定义请求头
		WithCustomHeaders(map[string]string{
			"X-Custom-Header": "value",
			"User-Agent":      "MyApp/1.0",
		}),

		// 流式响应
		WithStreamingEnabled(true),

		// 系统提示
		WithSystemPrompt("You are an expert programmer"),
	)
	_ = advancedClient
}

// ExampleChainedConfiguration 展示链式配置
func ExampleChainedConfiguration() {
	// 可以组合多个选项
	client, _ := NewClientWithOptions(
		// 基础配置
		WithProvider(ProviderOpenAI),
		WithAPIKey("your-api-key"),

		// 应用预设
		WithPreset(PresetProduction),

		// 应用使用场景优化
		WithUseCase(UseCaseCodeGeneration),

		// 覆盖特定参数
		WithModel("gpt-4-turbo-preview"),
		WithMaxTokens(8000),

		// 添加额外功能
		WithCache(true, 15*time.Minute),
		WithRetryCount(5),
	)
	_ = client
}

// ExampleConfigMigration 展示如何从旧的 Config 结构迁移到选项模式
func ExampleConfigMigration() {
	// 旧方式: 使用 Config 结构体
	oldConfig := &Config{
		Provider:    ProviderOpenAI,
		APIKey:      "your-api-key",
		Model:       "gpt-4",
		MaxTokens:   2000,
		Temperature: 0.7,
		Timeout:     60,
	}

	// 新方式: 使用选项模式（等价配置）
	newClient, _ := NewClientWithOptions(
		WithProvider(oldConfig.Provider),
		WithAPIKey(oldConfig.APIKey),
		WithModel(oldConfig.Model),
		WithMaxTokens(oldConfig.MaxTokens),
		WithTemperature(oldConfig.Temperature),
		WithTimeout(time.Duration(oldConfig.Timeout)*time.Second),
	)
	_ = newClient

	// 或者: 应用选项到现有配置
	enhancedConfig := ApplyOptions(
		oldConfig,
		WithCache(true, 5*time.Minute),
		WithRetryCount(3),
		WithStreamingEnabled(true),
	)
	_ = enhancedConfig
}

// ExampleEnvironmentBased 展示基于环境的配置
func ExampleEnvironmentBased() {
	// API 密钥将从环境变量自动读取
	// 例如: OPENAI_API_KEY, ANTHROPIC_API_KEY 等

	// 开发环境
	devClient, _ := NewClientWithOptions(
		WithProvider(ProviderOpenAI),
		// API key 从 OPENAI_API_KEY 环境变量读取
		WithPreset(PresetDevelopment),
		WithSystemPrompt("Development mode - verbose logging enabled"),
	)
	_ = devClient

	// 生产环境
	prodClient, _ := NewClientWithOptions(
		WithProvider(ProviderOpenAI),
		// API key 从环境变量读取
		WithPreset(PresetProduction),
		WithCache(true, 30*time.Minute),
		WithRetryCount(5),
		WithRateLimiting(1000), // 生产环境更高的速率限制
	)
	_ = prodClient
}
