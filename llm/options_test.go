package llm

import (
	"testing"
	"time"

	"github.com/kart-io/goagent/llm/constants"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestDefaultConfig 测试默认配置
func TestDefaultConfig(t *testing.T) {
	config := DefaultClientConfig()

	assert.Equal(t, constants.ProviderCustom, config.Provider)
	assert.Equal(t, 2000, config.MaxTokens)
	assert.Equal(t, 0.7, config.Temperature)
	assert.Equal(t, 60, config.Timeout)
	assert.Equal(t, 1.0, config.TopP)
}

// TestBasicOptions 测试基本选项
func TestBasicOptions(t *testing.T) {
	config := NewLLMOptionsWithOptions(
		WithProvider(constants.ProviderAnthropic),
		WithAPIKey("test-key"),
		WithModel("claude-3"),
		WithMaxTokens(3000),
		WithTemperature(0.5),
		WithTimeout(120*time.Second),
	)

	assert.Equal(t, constants.ProviderAnthropic, config.Provider)
	assert.Equal(t, "test-key", config.APIKey)
	assert.Equal(t, "claude-3", config.Model)
	assert.Equal(t, 3000, config.MaxTokens)
	assert.Equal(t, 0.5, config.Temperature)
	assert.Equal(t, 120, config.Timeout)
}

// TestAdvancedOptions 测试高级选项
func TestAdvancedOptions(t *testing.T) {
	headers := map[string]string{
		"X-Custom-Header": "value",
		"Authorization":   "Bearer token",
	}

	config := NewLLMOptionsWithOptions(
		WithRetryCount(5),
		WithRetryDelay(3*time.Second),
		WithRateLimiting(100),
		WithCache(true, 15*time.Minute),
		WithProxy("http://proxy.example.com:8080"),
		WithSystemPrompt("You are helpful"),
		WithStreamingEnabled(true),
		WithCustomHeaders(headers),
		WithOrganizationID("org-123"),
		WithTopP(0.95),
	)

	assert.Equal(t, 5, config.RetryCount)
	assert.Equal(t, 3*time.Second, config.RetryDelay)
	assert.Equal(t, 100, config.RateLimitRPM)
	assert.True(t, config.CacheEnabled)
	assert.Equal(t, 15*time.Minute, config.CacheTTL)
	assert.Equal(t, "http://proxy.example.com:8080", config.ProxyURL)
	assert.Equal(t, "You are helpful", config.SystemPrompt)
	assert.True(t, config.StreamingEnabled)
	assert.Equal(t, headers, config.CustomHeaders)
	assert.Equal(t, "org-123", config.OrganizationID)
	assert.Equal(t, 0.95, config.TopP)
}

// TestOptionOverride 测试选项覆盖
func TestOptionOverride(t *testing.T) {
	// 后面的选项应该覆盖前面的选项
	config := NewLLMOptionsWithOptions(
		WithModel("gpt-3.5-turbo"),
		WithMaxTokens(1000),
		WithTemperature(0.5),
		// 覆盖前面的设置
		WithModel("gpt-4"),
		WithMaxTokens(2000),
		WithTemperature(0.7),
	)

	assert.Equal(t, "gpt-4", config.Model)
	assert.Equal(t, 2000, config.MaxTokens)
	assert.Equal(t, 0.7, config.Temperature)
}

// TestApplyOptionsToExistingConfig 测试应用选项到现有配置
func TestApplyOptionsToExistingConfig(t *testing.T) {
	// 现有配置
	config := &LLMOptions{
		Provider:    constants.ProviderOpenAI,
		APIKey:      "original-key",
		Model:       "gpt-3.5-turbo",
		MaxTokens:   1000,
		Temperature: 0.5,
		Timeout:     30,
	}

	// 应用新选项
	updatedConfig := ApplyOptions(
		config,
		WithModel("gpt-4"),
		WithMaxTokens(2000),
		WithCache(true, 10*time.Minute),
		WithRetryCount(3),
	)

	// 验证更新的字段
	assert.Equal(t, "gpt-4", updatedConfig.Model)
	assert.Equal(t, 2000, updatedConfig.MaxTokens)
	assert.True(t, updatedConfig.CacheEnabled)
	assert.Equal(t, 10*time.Minute, updatedConfig.CacheTTL)
	assert.Equal(t, 3, updatedConfig.RetryCount)

	// 验证未更新的字段保持不变
	assert.Equal(t, "original-key", updatedConfig.APIKey)
	assert.Equal(t, 0.5, updatedConfig.Temperature)
}

// TestValidation 测试参数验证
func TestValidation(t *testing.T) {
	// Temperature 应该在 0-2.0 范围内
	config1 := NewLLMOptionsWithOptions(
		WithTemperature(-0.5), // 无效，应该被忽略
	)
	assert.Equal(t, 0.7, config1.Temperature) // 使用默认值

	config2 := NewLLMOptionsWithOptions(
		WithTemperature(3.0), // 无效，应该被忽略
	)
	assert.Equal(t, 0.7, config2.Temperature) // 使用默认值

	config3 := NewLLMOptionsWithOptions(
		WithTemperature(1.5), // 有效
	)
	assert.Equal(t, 1.5, config3.Temperature)

	// TopP 应该在 0-1.0 范围内
	config4 := NewLLMOptionsWithOptions(
		WithTopP(1.5), // 无效，应该被忽略
	)
	assert.Equal(t, 1.0, config4.TopP) // 使用默认值

	config5 := NewLLMOptionsWithOptions(
		WithTopP(0.8), // 有效
	)
	assert.Equal(t, 0.8, config5.TopP)

	// MaxTokens 应该大于 0
	config6 := NewLLMOptionsWithOptions(
		WithMaxTokens(-100), // 无效，应该被忽略
	)
	assert.Equal(t, 2000, config6.MaxTokens) // 使用默认值

	config7 := NewLLMOptionsWithOptions(
		WithMaxTokens(5000), // 有效
	)
	assert.Equal(t, 5000, config7.MaxTokens)
}

// TestValidateConfig 测试配置验证函数
func TestValidateConfig(t *testing.T) {
	t.Run("NilConfig", func(t *testing.T) {
		err := validateConfig(nil)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "config is nil")
	})

	t.Run("MissingProvider", func(t *testing.T) {
		config := &LLMOptions{}
		err := validateConfig(config)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "provider is required")
	})

	t.Run("MissingAPIKey", func(t *testing.T) {
		config := &LLMOptions{
			Provider: constants.ProviderOpenAI,
		}
		err := validateConfig(config)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "requires API key")
	})

	t.Run("OllamaNoAPIKey", func(t *testing.T) {
		// Ollama 不需要 API key
		config := &LLMOptions{
			Provider: constants.ProviderOllama,
			BaseURL:  "http://localhost:11434",
		}
		err := validateConfig(config)
		require.NoError(t, err)
	})

	t.Run("ValidConfig", func(t *testing.T) {
		config := &LLMOptions{
			Provider: constants.ProviderOpenAI,
			APIKey:   "test-key",
		}
		err := validateConfig(config)
		require.NoError(t, err)
	})
}
