package llm_test

import (
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kart-io/goagent/llm"
	"github.com/kart-io/goagent/llm/constants"
	"github.com/kart-io/goagent/llm/providers"
)

// TestOptionPatternIntegration tests the supported provider constructor with options.
func TestOptionPatternIntegration(t *testing.T) {
	// Constructors do not send requests, so no real API credentials are needed.
	t.Setenv("OPENAI_API_KEY", "test-constructor-key")

	t.Run("CreateWithOptions", func(t *testing.T) {
		client, err := providers.NewOpenAIWithOptions(
			llm.WithProvider(constants.ProviderOpenAI),
			llm.WithModel("gpt-3.5-turbo"),
			llm.WithMaxTokens(1000),
			llm.WithTemperature(0.7),
		)

		require.NoError(t, err)
		assert.NotNil(t, client)
		assert.Equal(t, constants.ProviderOpenAI, client.Provider())
	})

	t.Run("CreateWithExplicitOptions", func(t *testing.T) {
		client, err := providers.NewOpenAIWithOptions(
			llm.WithProvider(constants.ProviderOpenAI),
			llm.WithModel("gpt-3.5-turbo"),
			llm.WithMaxTokens(1000),
			llm.WithTemperature(0.5),
			llm.WithTimeout(30*time.Second),
		)

		require.NoError(t, err)
		assert.NotNil(t, client)
	})

	t.Run("CreateWithScenarioOptions", func(t *testing.T) {
		client, err := providers.NewOpenAIWithOptions(
			llm.WithProvider(constants.ProviderOpenAI),
			llm.WithTemperature(0.2),
			llm.WithMaxTokens(2500),
			llm.WithTopP(0.95),
			llm.WithModel("gpt-4"),
		)

		require.NoError(t, err)
		assert.NotNil(t, client)
	})
}

// TestOpenAIBuilder 测试 OpenAI Builder 模式
func TestOpenAIBuilder(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "test-constructor-key")

	t.Run("OptionPattern", func(t *testing.T) {
		client, err := providers.NewOpenAIWithOptions(
			llm.WithAPIKey(os.Getenv("OPENAI_API_KEY")),
			llm.WithModel("gpt-3.5-turbo"),
			llm.WithTemperature(0.5),
			llm.WithMaxTokens(1000),
		)
		require.NoError(t, err)
		assert.NotNil(t, client)
	})

	t.Run("OptionWithExplicitConfig", func(t *testing.T) {
		client, err := providers.NewOpenAIWithOptions(
			llm.WithAPIKey(os.Getenv("OPENAI_API_KEY")),
			llm.WithModel("gpt-4"),
			llm.WithMaxTokens(2000),
			llm.WithTemperature(0.7),
			llm.WithTimeout(60*time.Second),
			llm.WithTemperature(0.7),
			llm.WithMaxTokens(1500),
			llm.WithTopP(0.9),
		)
		require.NoError(t, err)
		assert.NotNil(t, client)
	})
}

// TestConvenienceMethods 测试便捷方法
func TestConvenienceMethods(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "test-constructor-key")

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
			llm.WithSystemPrompt("You are helpful"),
		)

		assert.Equal(t, "gpt-4", newConfig.Model)
		assert.Equal(t, 2000, newConfig.MaxTokens)
		assert.Equal(t, "You are helpful", newConfig.SystemPrompt)

		// 未改变的字段保持原值
		assert.Equal(t, "test-key", newConfig.APIKey)
		assert.Equal(t, 0.5, newConfig.Temperature)
	})
}
