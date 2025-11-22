package main

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/kart-io/goagent/llm"
	"github.com/kart-io/goagent/llm/providers"
)

// Example1: 基础使用 - 创建简单的 LLM 客户端
func Example1_BasicUsage() {
	// 使用工厂创建客户端
	factory := providers.NewClientFactory()

	// 方式1: 使用选项模式创建
	client, err := factory.CreateClientWithOptions(
		llm.WithProvider(llm.ProviderOpenAI),
		llm.WithAPIKey("your-api-key"),
		llm.WithModel("gpt-3.5-turbo"),
		llm.WithMaxTokens(1000),
		llm.WithTemperature(0.7),
	)
	if err != nil {
		log.Fatal(err)
	}

	// 使用客户端
	ctx := context.Background()
	response, err := client.Chat(ctx, []llm.Message{
		llm.SystemMessage("You are a helpful assistant"),
		llm.UserMessage("What is the capital of France?"),
	})
	if err != nil {
		log.Fatal(err)
	}

	fmt.Println("Response:", response.Content)
}

// Example2: 生产环境配置
func Example2_ProductionSetup() {
	// 创建生产环境优化的客户端
	client, err := providers.CreateProductionClient(
		llm.ProviderOpenAI,
		"your-api-key",
		llm.WithModel("gpt-4"),
		llm.WithMaxTokens(2000),
		llm.WithSystemPrompt("You are a professional assistant"),
	)
	if err != nil {
		log.Fatal(err)
	}

	// 生产环境客户端自动包含：
	// - 重试机制（3次重试）
	// - 缓存（10分钟TTL）
	// - 生产预设配置

	ctx := context.Background()
	response, err := client.Chat(ctx, []llm.Message{
		llm.UserMessage("Analyze this data..."),
	})
	if err != nil {
		log.Fatal(err)
	}

	fmt.Println("Analysis:", response.Content)
}

// Example3: 针对特定使用场景优化
func Example3_UseCaseOptimization() {
	// 为代码生成优化的客户端
	codeGenClient, err := providers.CreateClientForUseCase(
		llm.ProviderOpenAI,
		"your-api-key",
		llm.UseCaseCodeGeneration,
		llm.WithModel("gpt-4"), // 覆盖默认模型
	)
	if err != nil {
		log.Fatal(err)
	}

	ctx := context.Background()
	response, err := codeGenClient.Chat(ctx, []llm.Message{
		llm.UserMessage("Write a function to sort an array in Go"),
	})
	if err != nil {
		log.Fatal(err)
	}

	fmt.Println("Generated Code:", response.Content)

	// 为创意写作优化的客户端
	creativeClient, err := providers.CreateClientForUseCase(
		llm.ProviderOpenAI,
		"your-api-key",
		llm.UseCaseCreativeWriting,
		llm.WithMaxTokens(4000),
	)
	if err != nil {
		log.Fatal(err)
	}

	response, err = creativeClient.Chat(ctx, []llm.Message{
		llm.UserMessage("Write a short story about a robot learning to paint"),
	})
	if err != nil {
		log.Fatal(err)
	}

	fmt.Println("Creative Story:", response.Content)
}

// Example4: 使用 Builder 模式
func Example4_BuilderPattern() {
	// 使用 Builder 模式构建 OpenAI 客户端
	client, err := providers.NewOpenAIBuilder().
		WithAPIKey("your-api-key").
		WithModel("gpt-4-turbo-preview").
		WithTemperature(0.7).
		WithMaxTokens(4000).
		WithPreset(llm.PresetHighQuality).
		WithRetry(3, 2*time.Second).
		WithCache(15 * time.Minute).
		WithUseCase(llm.UseCaseAnalysis).
		Build()
	if err != nil {
		log.Fatal(err)
	}

	ctx := context.Background()
	response, err := client.Chat(ctx, []llm.Message{
		llm.UserMessage("Analyze the trends in AI development"),
	})
	if err != nil {
		log.Fatal(err)
	}

	fmt.Println("Analysis:", response.Content)
}

// Example5: 使用不同的 LLM 提供商
func Example5_MultipleProviders() {
	// OpenAI
	openAIClient, err := providers.CreateOpenAIClient(
		"openai-api-key",
		llm.WithModel("gpt-4"),
		llm.WithUseCase(llm.UseCaseChat),
	)
	if err != nil {
		log.Fatal(err)
	}

	// Anthropic Claude
	anthropicClient, err := providers.CreateAnthropicClient(
		"anthropic-api-key",
		llm.WithModel("claude-3-opus-20240229"),
		llm.WithMaxTokens(4000),
	)
	if err != nil {
		log.Fatal(err)
	}

	// Google Gemini
	geminiClient, err := providers.CreateGeminiClient(
		"google-api-key",
		llm.WithModel("gemini-pro"),
		llm.WithTemperature(0.9),
	)
	if err != nil {
		log.Fatal(err)
	}

	// Ollama (本地运行)
	ollamaClient, err := providers.CreateOllamaClient(
		"llama2",
		llm.WithBaseURL("http://localhost:11434"),
		llm.WithMaxTokens(2048),
	)
	if err != nil {
		log.Fatal(err)
	}

	// 使用不同的客户端
	clients := []llm.Client{openAIClient, anthropicClient, geminiClient, ollamaClient}

	ctx := context.Background()
	question := "What is machine learning?"

	for _, client := range clients {
		fmt.Printf("Provider: %s\n", client.Provider())
		response, err := client.Chat(ctx, []llm.Message{
			llm.UserMessage(question),
		})
		if err != nil {
			fmt.Printf("Error: %v\n", err)
			continue
		}
		fmt.Printf("Response: %s\n\n", response.Content)
	}
}

// Example6: 高级配置示例
func Example6_AdvancedConfiguration() {
	factory := providers.NewClientFactory()

	// 创建具有完整高级配置的客户端
	config := llm.NewConfigWithOptions(
		// 基础配置
		llm.WithProvider(llm.ProviderOpenAI),
		llm.WithAPIKey("your-api-key"),
		llm.WithModel("gpt-4"),

		// 应用预设
		llm.WithPreset(llm.PresetProduction),

		// 使用场景优化
		llm.WithUseCase(llm.UseCaseAnalysis),

		// 生成参数
		llm.WithMaxTokens(3000),
		llm.WithTemperature(0.5),
		llm.WithTopP(0.95),

		// 网络和重试
		llm.WithTimeout(60*time.Second),
		llm.WithRetryCount(3),
		llm.WithRetryDelay(2*time.Second),

		// 缓存
		llm.WithCache(true, 30*time.Minute),

		// 速率限制
		llm.WithRateLimiting(100), // 100 RPM

		// 系统提示
		llm.WithSystemPrompt("You are an expert data analyst with deep knowledge in statistics and machine learning"),

		// 自定义请求头
		llm.WithCustomHeaders(map[string]string{
			"X-Request-ID": "analysis-123",
			"X-User-ID":    "user-456",
		}),
	)

	// 准备配置（验证和设置默认值）
	if err := llm.PrepareConfig(config); err != nil {
		log.Fatal(err)
	}

	// 创建客户端
	client, err := factory.CreateClient(config)
	if err != nil {
		log.Fatal(err)
	}

	// 使用客户端进行复杂分析
	ctx := context.Background()
	messages := []llm.Message{
		llm.UserMessage(`Analyze the following sales data and provide insights:
		Q1: $1.2M (growth: 15%)
		Q2: $1.5M (growth: 25%)
		Q3: $1.3M (growth: -13%)
		Q4: $1.8M (growth: 38%)

		Provide:
		1. Trend analysis
		2. Key insights
		3. Recommendations for next year`),
	}

	response, err := client.Chat(ctx, messages)
	if err != nil {
		log.Fatal(err)
	}

	fmt.Println("Analysis Report:")
	fmt.Println(response.Content)

	// 显示使用的 tokens
	if response.Usage != nil {
		fmt.Printf("\nToken Usage:\n")
		fmt.Printf("  Prompt Tokens: %d\n", response.Usage.PromptTokens)
		fmt.Printf("  Completion Tokens: %d\n", response.Usage.CompletionTokens)
		fmt.Printf("  Total Tokens: %d\n", response.Usage.TotalTokens)
	}
}

// Example7: 开发环境 vs 生产环境
func Example7_EnvironmentBased() {
	isProduction := false // 从环境变量或配置文件读取

	var client llm.Client
	var err error

	if isProduction {
		// 生产环境配置
		client, err = providers.CreateProductionClient(
			llm.ProviderOpenAI,
			"", // 从环境变量 OPENAI_API_KEY 读取
			llm.WithModel("gpt-4"),
			llm.WithCache(true, 1*time.Hour),
			llm.WithRateLimiting(1000),
		)
	} else {
		// 开发环境配置
		client, err = providers.CreateDevelopmentClient(
			llm.ProviderOpenAI,
			"",                             // 从环境变量读取
			llm.WithModel("gpt-3.5-turbo"), // 开发环境使用更便宜的模型
		)
	}

	if err != nil {
		log.Fatal(err)
	}

	// 使用客户端
	ctx := context.Background()
	response, err := client.Chat(ctx, []llm.Message{
		llm.UserMessage("Test message"),
	})
	if err != nil {
		log.Fatal(err)
	}

	fmt.Println("Response:", response.Content)
}

// Example8: 错误处理和重试
func Example8_ErrorHandling() {
	// 创建带有增强重试功能的 OpenAI 客户端
	builder := providers.NewOpenAIBuilder().
		WithAPIKey("your-api-key").
		WithModel("gpt-4").
		WithRetry(5, 3*time.Second) // 5次重试，间隔3秒

	client, err := builder.Build()
	if err != nil {
		log.Fatal("Failed to create client:", err)
	}

	ctx := context.Background()

	// 使用带超时的 context
	ctxWithTimeout, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	messages := []llm.Message{
		llm.UserMessage("Generate a complex analysis..."),
	}

	// 增强版客户端会自动重试
	response, err := client.ChatWithSystemPrompt(ctxWithTimeout, messages)
	if err != nil {
		// 处理错误
		switch {
		case context.DeadlineExceeded == err:
			fmt.Println("Request timed out")
		case context.Canceled == err:
			fmt.Println("Request was cancelled")
		default:
			fmt.Printf("Error after retries: %v\n", err)
		}
		return
	}

	fmt.Println("Success:", response.Content)
}

func main() {
	// 运行示例
	fmt.Println("=== Example 1: Basic Usage ===")
	// Example1_BasicUsage()

	fmt.Println("\n=== Example 2: Production Setup ===")
	// Example2_ProductionSetup()

	fmt.Println("\n=== Example 3: Use Case Optimization ===")
	// Example3_UseCaseOptimization()

	fmt.Println("\n=== Example 4: Builder Pattern ===")
	// Example4_BuilderPattern()

	fmt.Println("\n=== Example 5: Multiple Providers ===")
	// Example5_MultipleProviders()

	fmt.Println("\n=== Example 6: Advanced Configuration ===")
	// Example6_AdvancedConfiguration()

	fmt.Println("\n=== Example 7: Environment Based ===")
	// Example7_EnvironmentBased()

	fmt.Println("\n=== Example 8: Error Handling ===")
	// Example8_ErrorHandling()

	fmt.Println("\nNote: Uncomment the example functions you want to run")
}
