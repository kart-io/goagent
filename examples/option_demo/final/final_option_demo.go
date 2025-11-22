package main

import (
	"fmt"
	"runtime"
	"time"

	"github.com/kart-io/goagent/llm"
	"github.com/kart-io/goagent/llm/constants"
	"github.com/kart-io/goagent/llm/providers"
	"github.com/kart-io/goagent/tools"
)

func main() {
	fmt.Println("🚀 GoAgent Option 模式演示")
	fmt.Println("=====================================")

	// Demo 1: 缓存 Option 模式
	demoCacheOptions()

	// Demo 2: LLM Option 模式
	demoLLMOptions()

	fmt.Println("\n=====================================")
	fmt.Println("✅ 演示完成！")
	fmt.Println("\n总结:")
	fmt.Println("1. Option 模式提供了灵活的配置方式")
	fmt.Println("2. 预设配置简化了常见用例")
	fmt.Println("3. Builder 模式提供流畅的 API")
	fmt.Println("4. 工厂方法统一客户端创建")
}

func demoCacheOptions() {
	fmt.Println("📌 Demo 1: 缓存 Option 模式")
	fmt.Println("-----------------------------------")

	fmt.Println("\n缓存配置选项:")

	// 使用默认配置创建缓存
	defaultCache := tools.NewShardedToolCacheWithOptions()
	fmt.Println("✅ 默认缓存已创建")
	defaultCache.Close()

	// 使用性能配置文件
	fmt.Println("\n性能配置文件:")
	performanceCache := tools.NewShardedToolCacheWithOptions(
		tools.WithPerformanceProfile(tools.LowLatencyProfile),
	)
	fmt.Println("  - 低延迟配置已应用")
	performanceCache.Close()

	highThroughputCache := tools.NewShardedToolCacheWithOptions(
		tools.WithPerformanceProfile(tools.HighThroughputProfile),
	)
	fmt.Println("  - 高吞吐配置已应用")
	highThroughputCache.Close()

	// 工作负载优化
	fmt.Println("\n工作负载优化:")
	readHeavyCache := tools.NewShardedToolCacheWithOptions(
		tools.WithWorkloadType(tools.ReadHeavyWorkload),
	)
	fmt.Println("  - 读密集型配置已应用")
	readHeavyCache.Close()

	writeHeavyCache := tools.NewShardedToolCacheWithOptions(
		tools.WithWorkloadType(tools.WriteHeavyWorkload),
	)
	fmt.Println("  - 写密集型配置已应用")
	writeHeavyCache.Close()

	// 自定义配置
	fmt.Println("\n自定义配置:")
	customCache := tools.NewShardedToolCacheWithOptions(
		tools.WithShardCount(64),
		tools.WithCapacity(100000),
		tools.WithCleanupInterval(5*time.Minute),
		tools.WithEvictionPolicy(tools.LRUEviction),
		tools.WithAutoTuning(true),
	)
	fmt.Println("  ✅ 自定义配置已应用:")
	fmt.Println("     - 64 分片")
	fmt.Println("     - 容量 100000")
	fmt.Println("     - 清理间隔 5 分钟")
	fmt.Println("     - LRU 驱逐策略")
	fmt.Println("     - 自动调优启用")
	customCache.Close()

	// 性能建议
	fmt.Println("\n性能配置建议:")
	fmt.Printf("  - 低流量 (<100 QPS): %d 分片\n", runtime.NumCPU())
	fmt.Printf("  - 中流量 (100-1K QPS): %d 分片\n", runtime.NumCPU()*2)
	fmt.Printf("  - 高流量 (1K-10K QPS): %d 分片\n", runtime.NumCPU()*4)
	fmt.Printf("  - 超高流量 (>10K QPS): %d 分片\n", runtime.NumCPU()*8)
}

func demoLLMOptions() {
	fmt.Println("\n📌 Demo 2: LLM Option 模式")
	fmt.Println("-----------------------------------")

	// 1. 基本配置
	fmt.Println("\n基本配置:")
	basicConfig := llm.NewLLMOptionsWithOptions(
		llm.WithProvider(constants.ProviderOpenAI),
		llm.WithAPIKey("demo-key"),
		llm.WithModel("gpt-4"),
		llm.WithMaxTokens(2000),
		llm.WithTemperature(0.7),
	)
	fmt.Printf("  Provider: %s\n", basicConfig.Provider)
	fmt.Printf("  Model: %s\n", basicConfig.Model)
	fmt.Printf("  MaxTokens: %d\n", basicConfig.MaxTokens)
	fmt.Printf("  Temperature: %.2f\n", basicConfig.Temperature)

	// 2. 预设配置
	fmt.Println("\n预设配置对比:")
	fmt.Println("预设          | Model           | Tokens | Temp | Cache")
	fmt.Println("-------------|-----------------|--------|------|-------")

	presets := []struct {
		name   string
		preset llm.PresetOption
	}{
		{"开发", llm.PresetDevelopment},
		{"生产", llm.PresetProduction},
		{"低成本", llm.PresetLowCost},
		{"高质量", llm.PresetHighQuality},
		{"快速", llm.PresetFast},
	}

	for _, p := range presets {
		config := llm.NewLLMOptionsWithOptions(
			llm.WithProvider(constants.ProviderOpenAI),
			llm.WithAPIKey("demo"),
			llm.WithPreset(p.preset),
		)
		cacheStr := "No"
		if config.CacheEnabled {
			cacheStr = fmt.Sprintf("%v", config.CacheTTL)
		}
		fmt.Printf("%-12s | %-15s | %-6d | %.2f | %s\n",
			p.name, config.Model, config.MaxTokens, config.Temperature, cacheStr)
	}

	// 3. 使用场景优化
	fmt.Println("\n使用场景优化:")
	fmt.Println("场景     | 温度 | Tokens | TopP | 说明")
	fmt.Println("---------|------|--------|------|----------")

	useCases := []struct {
		name    string
		useCase llm.UseCase
		desc    string
	}{
		{"聊天", llm.UseCaseChat, "自然对话"},
		{"代码", llm.UseCaseCodeGeneration, "一致输出"},
		{"翻译", llm.UseCaseTranslation, "准确翻译"},
		{"摘要", llm.UseCaseSummarization, "简洁总结"},
		{"分析", llm.UseCaseAnalysis, "详细分析"},
		{"创作", llm.UseCaseCreativeWriting, "创意内容"},
	}

	for _, uc := range useCases {
		config := llm.NewLLMOptionsWithOptions(
			llm.WithProvider(constants.ProviderOpenAI),
			llm.WithAPIKey("demo"),
			llm.WithUseCase(uc.useCase),
		)
		fmt.Printf("%-8s | %.2f | %-6d | %.2f | %s\n",
			uc.name, config.Temperature, config.MaxTokens, config.TopP, uc.desc)
	}

	// 4. Option 模式（推荐）
	fmt.Println("\nOption 模式（推荐）:")
	client, err := providers.NewOpenAIWithOptions(
		llm.WithAPIKey("demo-key"),
		llm.WithModel("gpt-4"),
		llm.WithTemperature(0.7),
		llm.WithMaxTokens(2000),
		llm.WithRetryCount(3),
		llm.WithRetryDelay(2*time.Second),
		llm.WithCache(true, 10*time.Minute),
	)
	if err != nil {
		fmt.Printf("Error: %v\n", err)
		return
	}
	_ = client

	fmt.Println("  OpenAI Option 模式配置:")
	fmt.Println("  ✅ API Key 设置")
	fmt.Println("  ✅ Model: gpt-4")
	fmt.Println("  ✅ Temperature: 0.7")
	fmt.Println("  ✅ MaxTokens: 2000")
	fmt.Println("  ✅ Retry: 3次，2秒延迟")
	fmt.Println("  ✅ Cache: 10分钟 TTL")

	// 5. 高级功能
	fmt.Println("\n高级功能配置:")
	advancedConfig := llm.NewLLMOptionsWithOptions(
		llm.WithProvider(constants.ProviderOpenAI),
		llm.WithAPIKey("demo-key"),
		llm.WithModel("gpt-4"),
		llm.WithRetryCount(3),
		llm.WithRetryDelay(2*time.Second),
		llm.WithCache(true, 10*time.Minute),
		llm.WithRateLimiting(100),
		llm.WithSystemPrompt("You are an expert assistant"),
		llm.WithStreamingEnabled(true),
		llm.WithCustomHeaders(map[string]string{
			"X-Request-ID": "123",
		}),
	)

	fmt.Println("  ✅ 重试机制: 3 次")
	fmt.Println("  ✅ 缓存: 启用 (10分钟)")
	fmt.Println("  ✅ 速率限制: 100 RPM")
	fmt.Println("  ✅ 系统提示: 已设置")
	fmt.Println("  ✅ 流式响应: 启用")
	fmt.Println("  ✅ 自定义头: 已添加")
	_ = advancedConfig

	// 6. 配置验证
	fmt.Println("\n配置验证:")

	// 有效配置
	validConfig := &llm.LLMOptions{
		Provider:  constants.ProviderOpenAI,
		APIKey:    "test-key",
		Model:     "gpt-4",
		MaxTokens: 2000,
	}
	if err := llm.PrepareConfig(validConfig); err == nil {
		fmt.Println("  ✅ OpenAI 配置验证通过")
	}

	// Ollama 不需要 API Key
	ollamaConfig := &llm.LLMOptions{
		Provider: constants.ProviderOllama,
		Model:    "llama2",
		BaseURL:  "http://localhost:11434",
	}
	if err := llm.PrepareConfig(ollamaConfig); err == nil {
		fmt.Println("  ✅ Ollama 配置验证通过（无需 API Key）")
	}

	// 缺少 API Key
	invalidConfig := &llm.LLMOptions{
		Provider: constants.ProviderOpenAI,
		Model:    "gpt-4",
	}
	if err := llm.PrepareConfig(invalidConfig); err != nil {
		fmt.Println("  ✅ 无效配置正确拒绝（缺少 API Key）")
	}
}
