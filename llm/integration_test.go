package llm_test

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/kart-io/goagent/llm"
	"github.com/kart-io/goagent/llm/constants"
	"github.com/kart-io/goagent/llm/providers"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestOptionPatternIntegration 测试 option 模式的完整集成
func TestOptionPatternIntegration(t *testing.T) {
	// 跳过需要真实 API key 的测试
	if os.Getenv("OPENAI_API_KEY") == "" {
		t.Skip("Skipping test that requires OPENAI_API_KEY")
	}

	t.Run("CreateWithOptions", func(t *testing.T) {
		client, err := llm.NewClientWithOptions(
			llm.WithProvider(constants.ProviderOpenAI),
			llm.WithModel("gpt-3.5-turbo"),
			llm.WithMaxTokens(1000),
			llm.WithTemperature(0.7),
		)

		require.NoError(t, err)
		assert.NotNil(t, client)
		assert.Equal(t, constants.ProviderOpenAI, client.Provider())
	})

	t.Run("CreateWithPreset", func(t *testing.T) {
		client, err := llm.NewClientWithOptions(
			llm.WithProvider(constants.ProviderOpenAI),
			llm.WithPreset(llm.PresetDevelopment),
		)

		require.NoError(t, err)
		assert.NotNil(t, client)
	})

	t.Run("CreateWithUseCase", func(t *testing.T) {
		client, err := llm.NewClientWithOptions(
			llm.WithProvider(constants.ProviderOpenAI),
			llm.WithUseCase(llm.UseCaseCodeGeneration),
		)

		require.NoError(t, err)
		assert.NotNil(t, client)
	})
}

// TestOpenAIBuilder 测试 OpenAI Builder 模式
func TestOpenAIBuilder(t *testing.T) {
	if os.Getenv("OPENAI_API_KEY") == "" {
		t.Skip("Skipping test that requires OPENAI_API_KEY")
	}

	t.Run("OptionPattern", func(t *testing.T) {
		client, err := providers.NewOpenAIWithOptions(
			llm.WithAPIKey(os.Getenv("OPENAI_API_KEY")),
			llm.WithModel("gpt-3.5-turbo"),
			llm.WithTemperature(0.5),
			llm.WithMaxTokens(1000),
			llm.WithRetryCount(3),
			llm.WithRetryDelay(2*time.Second),
			llm.WithCache(true, 10*time.Minute),
		)
		require.NoError(t, err)
		assert.NotNil(t, client)
	})

	t.Run("OptionWithPreset", func(t *testing.T) {
		client, err := providers.NewOpenAIWithOptions(
			llm.WithAPIKey(os.Getenv("OPENAI_API_KEY")),
			llm.WithPreset(llm.PresetProduction),
			llm.WithUseCase(llm.UseCaseChat),
		)
		require.NoError(t, err)
		assert.NotNil(t, client)
	})
}

// TestConvenienceMethods 测试便捷方法
func TestConvenienceMethods(t *testing.T) {
	if os.Getenv("OPENAI_API_KEY") == "" {
		t.Skip("Skipping test that requires OPENAI_API_KEY")
	}

	t.Run("CreateWithOptionsForOpenAI", func(t *testing.T) {
		config := llm.NewLLMOptionsWithOptions(
			llm.WithProvider(constants.ProviderOpenAI),
			llm.WithAPIKey(os.Getenv("OPENAI_API_KEY")),
			llm.WithModel("gpt-3.5-turbo"),
			llm.WithMaxTokens(500),
		)

		require.NotNil(t, config)
		assert.Equal(t, constants.ProviderOpenAI, config.Provider)
	})

	t.Run("CreateWithOptionsForOllama", func(t *testing.T) {
		// Ollama 不需要 API key
		config := llm.NewLLMOptionsWithOptions(
			llm.WithProvider(constants.ProviderOllama),
			llm.WithBaseURL("http://localhost:11434"),
			llm.WithModel("llama2"),
			llm.WithMaxTokens(2048),
			llm.WithTemperature(0.8),
		)

		require.NotNil(t, config)
		assert.Equal(t, constants.ProviderOllama, config.Provider)
	})
}

// TestConfigMigration 测试从旧配置迁移到新配置
func TestConfigMigration(t *testing.T) {
	t.Run("ApplyOptionsToExistingConfig", func(t *testing.T) {
		// 现有配置
		oldConfig := &llm.LLMOptions{
			Provider:    constants.ProviderOpenAI,
			APIKey:      "test-key",
			Model:       "gpt-3.5-turbo",
			MaxTokens:   1000,
			Temperature: 0.5,
			Timeout:     30,
		}

		// 应用新选项
		newConfig := llm.ApplyOptions(
			oldConfig,
			llm.WithModel("gpt-4"),
			llm.WithMaxTokens(2000),
			llm.WithCache(true, 5*time.Minute),
			llm.WithRetryCount(3),
			llm.WithSystemPrompt("You are helpful"),
		)

		assert.Equal(t, "gpt-4", newConfig.Model)
		assert.Equal(t, 2000, newConfig.MaxTokens)
		assert.True(t, newConfig.CacheEnabled)
		assert.Equal(t, 5*time.Minute, newConfig.CacheTTL)
		assert.Equal(t, 3, newConfig.RetryCount)
		assert.Equal(t, "You are helpful", newConfig.SystemPrompt)

		// 未改变的字段保持原值
		assert.Equal(t, "test-key", newConfig.APIKey)
		assert.Equal(t, 0.5, newConfig.Temperature)
	})
}

// ExampleNewClientWithOptions 展示如何使用 option 模式创建客户端
func ExampleNewClientWithOptions() {
	// 基本使用
	client, err := llm.NewClientWithOptions(
		llm.WithProvider(constants.ProviderOpenAI),
		llm.WithAPIKey("your-api-key"),
		llm.WithModel("gpt-4"),
		llm.WithMaxTokens(2000),
		llm.WithTemperature(0.7),
	)
	if err != nil {
		panic(err)
	}

	ctx := context.Background()
	response, err := client.Chat(ctx, []llm.Message{
		llm.SystemMessage("You are a helpful assistant"),
		llm.UserMessage("Hello!"),
	})
	if err != nil {
		panic(err)
	}

	_ = response
}

// ExampleNewClientWithOptions_preset 展示使用预设配置
func ExampleNewClientWithOptions_preset() {
	// 使用生产预设
	client, err := llm.NewClientWithOptions(
		llm.WithProvider(constants.ProviderOpenAI),
		llm.WithAPIKey("your-api-key"),
		llm.WithPreset(llm.PresetProduction),
		llm.WithCache(true, 30*time.Minute),
	)
	if err != nil {
		panic(err)
	}

	_ = client
}

// ExampleNewClientWithOptions_useCase 展示针对使用场景优化
func ExampleNewClientWithOptions_useCase() {
	// 为代码生成优化
	client, err := llm.NewClientWithOptions(
		llm.WithProvider(constants.ProviderOpenAI),
		llm.WithAPIKey("your-api-key"),
		llm.WithUseCase(llm.UseCaseCodeGeneration),
		llm.WithModel("gpt-4"), // 覆盖使用场景的默认模型
	)
	if err != nil {
		panic(err)
	}

	ctx := context.Background()
	response, err := client.Chat(ctx, []llm.Message{
		llm.UserMessage("Write a function to sort an array in Go"),
	})
	if err != nil {
		panic(err)
	}

	_ = response
}

// TestExampleOpenAIOptions 展示使用 Option 模式
func TestExampleOpenAIOptions(t *testing.T) {
	// 仅演示用法，不实际运行
	t.Skip("Example only")
	client, err := providers.NewOpenAIWithOptions(
		llm.WithAPIKey("your-api-key"),
		llm.WithModel("gpt-4-turbo-preview"),
		llm.WithTemperature(0.7),
		llm.WithMaxTokens(4000),
		llm.WithPreset(llm.PresetHighQuality),
		llm.WithRetryCount(3),
		llm.WithRetryDelay(2*time.Second),
		llm.WithCache(true, 15*time.Minute),
	)

	if err != nil {
		panic(err)
	}

	_ = client
}

// TestExampleConfigWithOptions 展示使用便捷方法
func TestExampleConfigWithOptions(t *testing.T) {
	// 仅演示用法，不实际运行
	t.Skip("Example only")
	// 使用 option 模式创建配置
	config := llm.NewLLMOptionsWithOptions(
		llm.WithProvider(constants.ProviderOpenAI),
		llm.WithAPIKey("your-api-key"),
		llm.WithModel("gpt-4"),
		llm.WithUseCase(llm.UseCaseAnalysis),
		llm.WithMaxTokens(3000),
		llm.WithSystemPrompt("You are a data analyst"),
	)

	// 可以使用 providers 包创建实际的客户端
	// client, err := providers.NewOpenAI(config)

	_ = config
}
