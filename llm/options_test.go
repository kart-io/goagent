package llm

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestDefaultConfig 测试默认配置
func TestDefaultConfig(t *testing.T) {
	config := DefaultClientConfig()

	assert.Equal(t, ProviderOpenAI, config.Provider)
	assert.Equal(t, 2000, config.MaxTokens)
	assert.Equal(t, 0.7, config.Temperature)
	assert.Equal(t, 60, config.Timeout)
	assert.Equal(t, 1.0, config.TopP)
}

// TestBasicOptions 测试基本选项
func TestBasicOptions(t *testing.T) {
	config := NewConfigWithOptions(
		WithProvider(ProviderAnthropic),
		WithAPIKey("test-key"),
		WithModel("claude-3"),
		WithMaxTokens(3000),
		WithTemperature(0.5),
		WithTimeout(120*time.Second),
	)

	assert.Equal(t, ProviderAnthropic, config.Provider)
	assert.Equal(t, "test-key", config.APIKey)
	assert.Equal(t, "claude-3", config.Model)
	assert.Equal(t, 3000, config.MaxTokens)
	assert.Equal(t, 0.5, config.Temperature)
	assert.Equal(t, 120, config.Timeout)
}

// TestPresetOptions 测试预设配置
func TestPresetOptions(t *testing.T) {
	testCases := []struct {
		name        string
		preset      PresetOption
		checkModel  string
		checkTokens int
	}{
		{
			name:        "Development",
			preset:      PresetDevelopment,
			checkModel:  "gpt-3.5-turbo",
			checkTokens: 1000,
		},
		{
			name:        "Production",
			preset:      PresetProduction,
			checkModel:  "gpt-4",
			checkTokens: 2000,
		},
		{
			name:        "LowCost",
			preset:      PresetLowCost,
			checkModel:  "gpt-3.5-turbo",
			checkTokens: 500,
		},
		{
			name:        "HighQuality",
			preset:      PresetHighQuality,
			checkModel:  "gpt-4-turbo-preview",
			checkTokens: 4000,
		},
		{
			name:        "Fast",
			preset:      PresetFast,
			checkModel:  "gpt-3.5-turbo-16k",
			checkTokens: 1000,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			config := NewConfigWithOptions(
				WithPreset(tc.preset),
			)

			assert.Equal(t, tc.checkModel, config.Model)
			assert.Equal(t, tc.checkTokens, config.MaxTokens)
		})
	}
}

// TestProviderPresets 测试提供商预设
func TestProviderPresets(t *testing.T) {
	providers := []struct {
		provider Provider
		model    string
		baseURL  string
	}{
		{
			provider: ProviderOpenAI,
			model:    "gpt-4-turbo-preview",
		},
		{
			provider: ProviderAnthropic,
			model:    "claude-3-sonnet-20240229",
		},
		{
			provider: ProviderGemini,
			model:    "gemini-pro",
		},
		{
			provider: ProviderDeepSeek,
			model:    "deepseek-chat",
			baseURL:  "https://api.deepseek.com/v1",
		},
		{
			provider: ProviderKimi,
			model:    "moonshot-v1-8k",
			baseURL:  "https://api.moonshot.cn/v1",
		},
		{
			provider: ProviderOllama,
			model:    "llama2",
			baseURL:  "http://localhost:11434",
		},
	}

	for _, p := range providers {
		t.Run(string(p.provider), func(t *testing.T) {
			config := NewConfigWithOptions(
				WithProviderPreset(p.provider),
			)

			assert.Equal(t, p.provider, config.Provider)
			assert.Equal(t, p.model, config.Model)
			if p.baseURL != "" {
				assert.Equal(t, p.baseURL, config.BaseURL)
			}
		})
	}
}

// TestUseCaseOptions 测试使用场景配置
func TestUseCaseOptions(t *testing.T) {
	testCases := []struct {
		useCase     UseCase
		temperature float64
		maxTokens   int
		topP        float64
	}{
		{
			useCase:     UseCaseChat,
			temperature: 0.7,
			maxTokens:   1500,
			topP:        0.9,
		},
		{
			useCase:     UseCaseCodeGeneration,
			temperature: 0.2,
			maxTokens:   2500,
			topP:        0.95,
		},
		{
			useCase:     UseCaseTranslation,
			temperature: 0.3,
			maxTokens:   2000,
			topP:        1.0,
		},
		{
			useCase:     UseCaseSummarization,
			temperature: 0.3,
			maxTokens:   500,
			topP:        0.9,
		},
		{
			useCase:     UseCaseAnalysis,
			temperature: 0.5,
			maxTokens:   3000,
			topP:        0.95,
		},
		{
			useCase:     UseCaseCreativeWriting,
			temperature: 0.9,
			maxTokens:   4000,
			topP:        0.95,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.useCase.String(), func(t *testing.T) {
			config := NewConfigWithOptions(
				WithUseCase(tc.useCase),
			)

			assert.Equal(t, tc.temperature, config.Temperature)
			assert.Equal(t, tc.maxTokens, config.MaxTokens)
			assert.Equal(t, tc.topP, config.TopP)
		})
	}
}

// TestAdvancedOptions 测试高级选项
func TestAdvancedOptions(t *testing.T) {
	headers := map[string]string{
		"X-Custom-Header": "value",
		"Authorization":   "Bearer token",
	}

	config := NewConfigWithOptions(
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
	config := NewConfigWithOptions(
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

// TestPresetWithOverride 测试预设与覆盖
func TestPresetWithOverride(t *testing.T) {
	config := NewConfigWithOptions(
		WithPreset(PresetDevelopment), // 设置 model="gpt-3.5-turbo", maxTokens=1000
		WithModel("gpt-4"),            // 覆盖模型
		WithMaxTokens(3000),           // 覆盖 token 限制
	)

	assert.Equal(t, "gpt-4", config.Model)
	assert.Equal(t, 3000, config.MaxTokens)
	assert.Equal(t, 0.5, config.Temperature) // 来自预设，未被覆盖
}

// TestApplyOptionsToExistingConfig 测试应用选项到现有配置
func TestApplyOptionsToExistingConfig(t *testing.T) {
	// 现有配置
	config := &Config{
		Provider:    ProviderOpenAI,
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
	config1 := NewConfigWithOptions(
		WithTemperature(-0.5), // 无效，应该被忽略
	)
	assert.Equal(t, 0.7, config1.Temperature) // 使用默认值

	config2 := NewConfigWithOptions(
		WithTemperature(3.0), // 无效，应该被忽略
	)
	assert.Equal(t, 0.7, config2.Temperature) // 使用默认值

	config3 := NewConfigWithOptions(
		WithTemperature(1.5), // 有效
	)
	assert.Equal(t, 1.5, config3.Temperature)

	// TopP 应该在 0-1.0 范围内
	config4 := NewConfigWithOptions(
		WithTopP(1.5), // 无效，应该被忽略
	)
	assert.Equal(t, 1.0, config4.TopP) // 使用默认值

	config5 := NewConfigWithOptions(
		WithTopP(0.8), // 有效
	)
	assert.Equal(t, 0.8, config5.TopP)

	// MaxTokens 应该大于 0
	config6 := NewConfigWithOptions(
		WithMaxTokens(-100), // 无效，应该被忽略
	)
	assert.Equal(t, 2000, config6.MaxTokens) // 使用默认值

	config7 := NewConfigWithOptions(
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
		config := &Config{}
		err := validateConfig(config)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "provider is required")
	})

	t.Run("MissingAPIKey", func(t *testing.T) {
		config := &Config{
			Provider: ProviderOpenAI,
		}
		err := validateConfig(config)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "requires API key")
	})

	t.Run("OllamaNoAPIKey", func(t *testing.T) {
		// Ollama 不需要 API key
		config := &Config{
			Provider: ProviderOllama,
			BaseURL:  "http://localhost:11434",
		}
		err := validateConfig(config)
		require.NoError(t, err)
	})

	t.Run("ValidConfig", func(t *testing.T) {
		config := &Config{
			Provider: ProviderOpenAI,
			APIKey:   "test-key",
		}
		err := validateConfig(config)
		require.NoError(t, err)
	})
}

// String 方法用于测试输出
func (u UseCase) String() string {
	names := []string{
		"Chat",
		"CodeGeneration",
		"Translation",
		"Summarization",
		"Analysis",
		"CreativeWriting",
	}
	if int(u) < len(names) {
		return names[u]
	}
	return "Unknown"
}
