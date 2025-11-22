package main

import (
	"fmt"
	"time"

	"github.com/kart-io/goagent/llm"
	"github.com/kart-io/goagent/llm/providers"
)

func main() {
	fmt.Println("🚀 GoAgent LLM Option Pattern Demo")
	fmt.Println("=====================================")

	// Demo 1: 基本 Option 模式使用
	demo1BasicOptions()

	// Demo 2: 预设配置
	demo2Presets()

	// Demo 3: 使用场景优化
	demo3UseCases()

	// Demo 4: Builder 模式
	demo4BuilderPattern()

	// Demo 5: 工厂方法
	demo5FactoryMethods()

	// Demo 6: 配置验证
	demo6ConfigValidation()

	// Demo 7: 迁移示例
	demo7Migration()

	fmt.Println("\n=====================================")
	fmt.Println("✅ 所有演示完成！")
	fmt.Println("\n关键要点:")
	fmt.Println("1. Option 模式提供灵活的配置方式")
	fmt.Println("2. 预设配置简化常见场景")
	fmt.Println("3. 使用场景优化自动调整参数")
	fmt.Println("4. Builder 模式提供流畅的 API")
	fmt.Println("5. 工厂方法简化客户端创建")
	fmt.Println("6. 配置验证确保参数有效")
	fmt.Println("7. 平滑迁移路径支持旧代码")
}

func demo1BasicOptions() {
	fmt.Println("📌 Demo 1: 基本 Option 模式使用")
	fmt.Println("-----------------------------------")

	// 使用 Option 创建配置
	config := llm.NewConfigWithOptions(
		llm.WithProvider(llm.ProviderOpenAI),
		llm.WithAPIKey("demo-api-key"),
		llm.WithModel("gpt-4"),
		llm.WithMaxTokens(2000),
		llm.WithTemperature(0.7),
		llm.WithTopP(0.95),
		llm.WithTimeout(60*time.Second),
		llm.WithRetryCount(3),
		llm.WithRetryDelay(2*time.Second),
		llm.WithSystemPrompt("You are a helpful assistant"),
	)

	fmt.Printf("创建的配置:\n")
	fmt.Printf("  Provider: %s\n", config.Provider)
	fmt.Printf("  Model: %s\n", config.Model)
	fmt.Printf("  MaxTokens: %d\n", config.MaxTokens)
	fmt.Printf("  Temperature: %.2f\n", config.Temperature)
	fmt.Printf("  TopP: %.2f\n", config.TopP)
	fmt.Printf("  Timeout: %d seconds\n", config.Timeout)
	fmt.Printf("  RetryCount: %d\n", config.RetryCount)
	fmt.Printf("  SystemPrompt: %s\n", config.SystemPrompt)
}

func demo2Presets() {
	fmt.Println("\n📌 Demo 2: 预设配置")
	fmt.Println("-----------------------------------")

	presets := []struct {
		name   string
		preset llm.PresetOption
	}{
		{"开发环境", llm.PresetDevelopment},
		{"生产环境", llm.PresetProduction},
		{"低成本", llm.PresetLowCost},
		{"高质量", llm.PresetHighQuality},
		{"快速响应", llm.PresetFast},
	}

	for _, p := range presets {
		config := llm.NewConfigWithOptions(
			llm.WithProvider(llm.ProviderOpenAI),
			llm.WithAPIKey("demo-key"),
			llm.WithPreset(p.preset),
		)

		fmt.Printf("\n%s 预设:\n", p.name)
		fmt.Printf("  Model: %s\n", config.Model)
		fmt.Printf("  MaxTokens: %d\n", config.MaxTokens)
		fmt.Printf("  Temperature: %.2f\n", config.Temperature)
		fmt.Printf("  Timeout: %ds\n", config.Timeout)
		fmt.Printf("  Cache: %v", config.CacheEnabled)
		if config.CacheEnabled {
			fmt.Printf(" (TTL: %v)", config.CacheTTL)
		}
		fmt.Printf("\n  Retry: %d times\n", config.RetryCount)
	}
}

func demo3UseCases() {
	fmt.Println("\n📌 Demo 3: 使用场景优化")
	fmt.Println("-----------------------------------")

	useCases := []struct {
		name    string
		useCase llm.UseCase
	}{
		{"聊天对话", llm.UseCaseChat},
		{"代码生成", llm.UseCaseCodeGeneration},
		{"翻译", llm.UseCaseTranslation},
		{"摘要生成", llm.UseCaseSummarization},
		{"数据分析", llm.UseCaseAnalysis},
		{"创意写作", llm.UseCaseCreativeWriting},
	}

	fmt.Println("\n场景优化参数对比:")
	fmt.Println("场景          | 温度  | 最大Token | TopP")
	fmt.Println("-------------|-------|----------|------")

	for _, uc := range useCases {
		config := llm.NewConfigWithOptions(
			llm.WithProvider(llm.ProviderOpenAI),
			llm.WithAPIKey("demo"),
			llm.WithUseCase(uc.useCase),
		)

		fmt.Printf("%-12s | %.2f  | %-8d | %.2f\n",
			uc.name, config.Temperature, config.MaxTokens, config.TopP)
	}
}

func demo4BuilderPattern() {
	fmt.Println("\n📌 Demo 4: Builder 模式")
	fmt.Println("-----------------------------------")

	// 使用 Builder 创建配置
	builder := providers.NewOpenAIBuilder().
		WithAPIKey("demo-key").
		WithModel("gpt-4-turbo-preview").
		WithTemperature(0.7).
		WithMaxTokens(4000).
		WithPreset(llm.PresetHighQuality).
		WithRetry(3, 2*time.Second).
		WithCache(15 * time.Minute).
		WithUseCase(llm.UseCaseAnalysis)

	fmt.Println("Builder 链式调用配置:")
	fmt.Println("  builder := providers.NewOpenAIBuilder().")
	fmt.Println("    WithAPIKey(\"your-key\").")
	fmt.Println("    WithModel(\"gpt-4-turbo-preview\").")
	fmt.Println("    WithTemperature(0.7).")
	fmt.Println("    WithMaxTokens(4000).")
	fmt.Println("    WithPreset(PresetHighQuality).")
	fmt.Println("    WithRetry(3, 2*time.Second).")
	fmt.Println("    WithCache(15*time.Minute).")
	fmt.Println("    WithUseCase(UseCaseAnalysis).")
	fmt.Println("    Build()")

	fmt.Println("\n✅ Builder 配置完成（未实际创建客户端以避免 API 错误）")
	_ = builder
}

func demo5FactoryMethods() {
	fmt.Println("\n📌 Demo 5: 工厂方法")
	fmt.Println("-----------------------------------")

	fmt.Println("可用的工厂方法:")

	fmt.Println("1️⃣ 基础创建:")
	fmt.Println("   factory.CreateClient(config)")
	fmt.Println("   factory.CreateClientWithOptions(opts...)")

	fmt.Println("\n2️⃣ 提供商特定:")
	fmt.Println("   CreateOpenAIClient(apiKey, opts...)")
	fmt.Println("   CreateAnthropicClient(apiKey, opts...)")
	fmt.Println("   CreateGeminiClient(apiKey, opts...)")
	fmt.Println("   CreateOllamaClient(model, opts...)")

	fmt.Println("\n3️⃣ 使用场景优化:")
	fmt.Println("   CreateClientForUseCase(provider, apiKey, useCase, opts...)")

	fmt.Println("\n4️⃣ 环境特定:")
	fmt.Println("   CreateProductionClient(provider, apiKey, opts...)")
	fmt.Println("   CreateDevelopmentClient(provider, apiKey, opts...)")

	// 演示生产环境配置
	fmt.Println("\n生产环境客户端自动包含:")
	fmt.Println("  ✅ 重试机制 (3次)")
	fmt.Println("  ✅ 响应缓存 (10分钟 TTL)")
	fmt.Println("  ✅ 生产预设配置")
	fmt.Println("  ✅ 错误处理增强")
}

func demo6ConfigValidation() {
	fmt.Println("\n📌 Demo 6: 配置验证")
	fmt.Println("-----------------------------------")

	testCases := []struct {
		name   string
		config *llm.Config
	}{
		{
			name: "✅ 有效的 OpenAI 配置",
			config: &llm.Config{
				Provider:  llm.ProviderOpenAI,
				APIKey:    "test-key",
				Model:     "gpt-4",
				MaxTokens: 2000,
			},
		},
		{
			name: "✅ 有效的 Ollama 配置（无需 API Key）",
			config: &llm.Config{
				Provider: llm.ProviderOllama,
				Model:    "llama2",
				BaseURL:  "http://localhost:11434",
			},
		},
		{
			name: "❌ 缺少 API Key（OpenAI）",
			config: &llm.Config{
				Provider: llm.ProviderOpenAI,
				Model:    "gpt-4",
			},
		},
		{
			name: "⚠️ 温度超出范围（将使用默认值）",
			config: &llm.Config{
				Provider:    llm.ProviderOpenAI,
				APIKey:      "test-key",
				Temperature: 3.0, // 超出 0-2.0 范围
			},
		},
	}

	for _, tc := range testCases {
		err := llm.PrepareConfig(tc.config)
		if err != nil {
			fmt.Printf("%s: %v\n", tc.name, err)
		} else {
			fmt.Printf("%s\n", tc.name)
			if tc.config.Temperature == 0.7 {
				fmt.Printf("  (温度已设置为默认值: %.2f)\n", tc.config.Temperature)
			}
		}
	}
}

func demo7Migration() {
	fmt.Println("\n📌 Demo 7: 从旧配置迁移到 Option 模式")
	fmt.Println("-----------------------------------")

	// 模拟旧的配置结构
	type OldConfig struct {
		Provider    string
		APIKey      string
		Model       string
		MaxTokens   int
		Temperature float64
		Timeout     int
	}

	oldConfig := &OldConfig{
		Provider:    "openai",
		APIKey:      "old-api-key",
		Model:       "gpt-3.5-turbo",
		MaxTokens:   1500,
		Temperature: 0.8,
		Timeout:     30,
	}

	fmt.Println("旧配置:")
	fmt.Printf("  Provider: %s\n", oldConfig.Provider)
	fmt.Printf("  Model: %s\n", oldConfig.Model)
	fmt.Printf("  MaxTokens: %d\n", oldConfig.MaxTokens)
	fmt.Printf("  Temperature: %.2f\n", oldConfig.Temperature)

	// 转换为新的 Option 模式
	fmt.Println("\n迁移到 Option 模式...")

	newConfig := llm.NewConfigWithOptions(
		llm.WithProvider(llm.Provider(oldConfig.Provider)),
		llm.WithAPIKey(oldConfig.APIKey),
		llm.WithModel(oldConfig.Model),
		llm.WithMaxTokens(oldConfig.MaxTokens),
		llm.WithTemperature(oldConfig.Temperature),
		llm.WithTimeout(time.Duration(oldConfig.Timeout)*time.Second),
		// 新配置可以轻松添加更多选项
		llm.WithRetryCount(3),
		llm.WithCache(true, 10*time.Minute),
	)

	fmt.Println("\n新配置（通过 Options）:")
	fmt.Printf("  Provider: %s\n", newConfig.Provider)
	fmt.Printf("  Model: %s\n", newConfig.Model)
	fmt.Printf("  MaxTokens: %d\n", newConfig.MaxTokens)
	fmt.Printf("  Temperature: %.2f\n", newConfig.Temperature)
	fmt.Printf("  Timeout: %d seconds\n", newConfig.Timeout)
	fmt.Printf("  + RetryCount: %d (新增)\n", newConfig.RetryCount)
	fmt.Printf("  + Cache: %v (新增)\n", newConfig.CacheEnabled)

	fmt.Println("\n✅ 迁移成功！新配置支持更多功能且向后兼容。")
}
