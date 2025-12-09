package providers

import (
	"os"
	"testing"
	"time"

	agentllm "github.com/kart-io/goagent/llm"
	"github.com/kart-io/goagent/llm/common"
	"github.com/kart-io/goagent/llm/constants"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewBaseProvider(t *testing.T) {
	opts := []agentllm.ClientOption{
		agentllm.WithAPIKey("test-key"),
		agentllm.WithModel("test-model"),
		agentllm.WithMaxTokens(100),
	}

	bp := common.NewBaseProvider(opts...)

	require.NotNil(t, bp)
	require.NotNil(t, bp.Config)
	assert.Equal(t, "test-key", bp.Config.APIKey)
	assert.Equal(t, "test-model", bp.Config.Model)
	assert.Equal(t, 100, bp.Config.MaxTokens)
}

func TestNewBaseProviderWithOptions(t *testing.T) {
	config := &agentllm.LLMOptions{
		APIKey:    "test-key",
		Model:     "test-model",
		MaxTokens: 200,
	}

	bp := common.NewBaseProvider(common.ConfigToOptions(config)...)

	require.NotNil(t, bp)
	assert.Equal(t, "test-key", bp.Config.APIKey)
	assert.Equal(t, "test-model", bp.Config.Model)
	assert.Equal(t, 200, bp.Config.MaxTokens)
}

func TestNewBaseProviderWithNilOptions(t *testing.T) {
	bp := common.NewBaseProvider()

	require.NotNil(t, bp)
	require.NotNil(t, bp.Config)
}

func TestApplyProviderDefaults(t *testing.T) {
	bp := common.NewBaseProvider()

	bp.ApplyProviderDefaults(
		constants.ProviderOpenAI,
		"https://api.openai.com/v1",
		"gpt-4",
		"OPENAI_BASE_URL",
		"OPENAI_MODEL",
	)

	assert.Equal(t, constants.ProviderOpenAI, bp.Config.Provider)
	assert.Equal(t, "https://api.openai.com/v1", bp.Config.BaseURL)
	assert.Equal(t, "gpt-4", bp.Config.Model)
}

func TestConfigToOptions(t *testing.T) {
	config := &agentllm.LLMOptions{
		Provider:         constants.ProviderOpenAI,
		APIKey:           "test-key",
		BaseURL:          "https://api.test.com",
		Model:            "test-model",
		MaxTokens:        100,
		Temperature:      0.7,
		Timeout:          30,
		TopP:             0.9,
		ProxyURL:         "http://proxy:8080",
		RetryCount:       3,
		RetryDelay:       2,
		RateLimitRPM:     60,
		SystemPrompt:     "You are helpful",
		CacheEnabled:     true,
		CacheTTL:         300,
		StreamingEnabled: true,
		OrganizationID:   "org-123",
		CustomHeaders: map[string]string{
			"X-Custom": "value",
		},
	}

	opts := common.ConfigToOptions(config)

	assert.NotEmpty(t, opts)
	// Recreate config from options to verify
	newConfig := agentllm.NewLLMOptionsWithOptions(opts...)
	assert.Equal(t, config.Provider, newConfig.Provider)
	assert.Equal(t, config.APIKey, newConfig.APIKey)
	assert.Equal(t, config.BaseURL, newConfig.BaseURL)
	assert.Equal(t, config.Model, newConfig.Model)
	assert.Equal(t, config.MaxTokens, newConfig.MaxTokens)
	assert.Equal(t, config.Temperature, newConfig.Temperature)
	assert.Equal(t, config.SystemPrompt, newConfig.SystemPrompt)
}

func TestConfigToOptions_NilConfig(t *testing.T) {
	opts := common.ConfigToOptions(nil)
	assert.Nil(t, opts)
}

func TestEnsureAPIKey_FromConfig(t *testing.T) {
	bp := common.NewBaseProvider(agentllm.WithAPIKey("test-key"))

	err := bp.EnsureAPIKey("TEST_API_KEY", constants.ProviderOpenAI)

	require.NoError(t, err)
	assert.Equal(t, "test-key", bp.Config.APIKey)
}

func TestEnsureAPIKey_FromEnv(t *testing.T) {
	os.Setenv("TEST_API_KEY", "env-key")
	defer os.Unsetenv("TEST_API_KEY")

	bp := common.NewBaseProvider()

	err := bp.EnsureAPIKey("TEST_API_KEY", constants.ProviderOpenAI)

	require.NoError(t, err)
	assert.Equal(t, "env-key", bp.Config.APIKey)
}

func TestEnsureAPIKey_Missing(t *testing.T) {
	os.Unsetenv("TEST_API_KEY")

	bp := common.NewBaseProvider()

	err := bp.EnsureAPIKey("TEST_API_KEY", constants.ProviderOpenAI)

	require.Error(t, err)
}

func TestEnsureBaseURL_FromConfig(t *testing.T) {
	bp := common.NewBaseProvider(agentllm.WithBaseURL("https://config.url"))

	bp.EnsureBaseURL("TEST_BASE_URL", "https://default.url")

	assert.Equal(t, "https://config.url", bp.Config.BaseURL)
}

func TestEnsureBaseURL_FromEnv(t *testing.T) {
	os.Setenv("TEST_BASE_URL", "https://env.url")
	defer os.Unsetenv("TEST_BASE_URL")

	bp := common.NewBaseProvider()

	bp.EnsureBaseURL("TEST_BASE_URL", "https://default.url")

	assert.Equal(t, "https://env.url", bp.Config.BaseURL)
}

func TestEnsureBaseURL_Default(t *testing.T) {
	os.Unsetenv("TEST_BASE_URL")

	bp := common.NewBaseProvider()

	bp.EnsureBaseURL("TEST_BASE_URL", "https://default.url")

	assert.Equal(t, "https://default.url", bp.Config.BaseURL)
}

func TestEnsureModel_FromConfig(t *testing.T) {
	bp := common.NewBaseProvider(agentllm.WithModel("config-model"))

	bp.EnsureModel("TEST_MODEL", "default-model")

	assert.Equal(t, "config-model", bp.Config.Model)
}

func TestEnsureModel_FromEnv(t *testing.T) {
	os.Setenv("TEST_MODEL", "env-model")
	defer os.Unsetenv("TEST_MODEL")

	bp := common.NewBaseProvider()

	bp.EnsureModel("TEST_MODEL", "default-model")

	assert.Equal(t, "env-model", bp.Config.Model)
}

func TestEnsureModel_Default(t *testing.T) {
	os.Unsetenv("TEST_MODEL")

	bp := common.NewBaseProvider()

	bp.EnsureModel("TEST_MODEL", "default-model")

	assert.Equal(t, "default-model", bp.Config.Model)
}

func TestGetModel(t *testing.T) {
	bp := common.NewBaseProvider(agentllm.WithModel("config-model"))

	// Request model takes precedence
	assert.Equal(t, "request-model", bp.GetModel("request-model"))
	// Falls back to config model
	assert.Equal(t, "config-model", bp.GetModel(""))
}

func TestGetMaxTokens(t *testing.T) {
	bp := common.NewBaseProvider(agentllm.WithMaxTokens(100))

	// Request tokens take precedence
	assert.Equal(t, 200, bp.GetMaxTokens(200))
	// Falls back to config
	assert.Equal(t, 100, bp.GetMaxTokens(0))

	// 测试默认值回退 - common.NewBaseProvider() 使用 DefaultLLMOptions()，该函数设置 MaxTokens 为 2000
	bp2 := common.NewBaseProvider()
	assert.Equal(t, 2000, bp2.GetMaxTokens(0))

	// 测试零配置时的兜底默认值（应使用 constants.DefaultMaxTokens）
	// 需要创建一个空配置（不使用 DefaultLLMOptions）
	bp3 := &common.BaseProvider{Config: &agentllm.LLMOptions{}}
	assert.Equal(t, constants.DefaultMaxTokens, bp3.GetMaxTokens(0))
}

func TestGetTemperature(t *testing.T) {
	bp := common.NewBaseProvider(agentllm.WithTemperature(0.7))

	// Request temperature takes precedence
	assert.Equal(t, 0.9, bp.GetTemperature(0.9))
	// Falls back to config
	assert.Equal(t, 0.7, bp.GetTemperature(0))

	// Test default fallback
	bp2 := common.NewBaseProvider()
	assert.Equal(t, 0.7, bp2.GetTemperature(0)) // DefaultLLMOptions sets it to 0.7

	// Test with zero config (should use constants.DefaultTemperature)
	bp3 := common.NewBaseProvider(common.ConfigToOptions(&agentllm.LLMOptions{Temperature: 0})...)
	assert.Equal(t, constants.DefaultTemperature, bp3.GetTemperature(0))
}

func TestGetTimeout(t *testing.T) {
	bp := common.NewBaseProvider(agentllm.WithTimeout(30 * time.Second))

	timeout := bp.GetTimeout()
	assert.Equal(t, 30*time.Second, timeout)

	// 测试默认值回退 - common.NewBaseProvider() 使用 DefaultLLMOptions()，该函数设置 Timeout 为 60
	bp2 := common.NewBaseProvider()
	assert.Equal(t, 60*time.Second, bp2.GetTimeout())

	// 测试零配置时的兜底默认值（应使用 constants.DefaultTimeout）
	// 需要创建一个空配置（不使用 DefaultLLMOptions）
	bp3 := &common.BaseProvider{Config: &agentllm.LLMOptions{}}
	assert.Equal(t, constants.DefaultTimeout, bp3.GetTimeout())
}

func TestGetTopP(t *testing.T) {
	bp := common.NewBaseProvider(agentllm.WithTopP(0.9))

	// Request TopP takes precedence
	assert.Equal(t, 0.8, bp.GetTopP(0.8))
	// Falls back to config
	assert.Equal(t, 0.9, bp.GetTopP(0))

	// Test default fallback
	bp2 := common.NewBaseProvider()
	assert.Equal(t, 1.0, bp2.GetTopP(0)) // DefaultLLMOptions sets it to 1.0

	// Test with zero config (should use constants.DefaultTopP)
	bp3 := common.NewBaseProvider(common.ConfigToOptions(&agentllm.LLMOptions{TopP: 0})...)
	assert.Equal(t, constants.DefaultTopP, bp3.GetTopP(0))
}

func TestModelName(t *testing.T) {
	bp := common.NewBaseProvider(agentllm.WithModel("test-model"))

	assert.Equal(t, "test-model", bp.ModelName())
}

func TestMaxTokensValue(t *testing.T) {
	bp := common.NewBaseProvider(agentllm.WithMaxTokens(500))

	assert.Equal(t, 500, bp.MaxTokensValue())
}

func TestProviderName(t *testing.T) {
	bp := common.NewBaseProvider(agentllm.WithProvider(constants.ProviderOpenAI))

	assert.Equal(t, "openai", bp.ProviderName())
}

func TestNewHTTPClient(t *testing.T) {
	bp := common.NewBaseProvider(
		agentllm.WithTimeout(30*time.Second),
		agentllm.WithCustomHeaders(map[string]string{
			"X-Custom": "config-value",
		}),
	)

	cfg := common.HTTPClientConfig{
		Timeout: 60 * time.Second,
		Headers: map[string]string{
			"Authorization": "Bearer token",
		},
		BaseURL: "https://api.test.com",
	}

	client := bp.NewHTTPClient(cfg)

	require.NotNil(t, client)
}

func TestNewHTTPClient_DefaultTimeout(t *testing.T) {
	bp := common.NewBaseProvider(agentllm.WithTimeout(30 * time.Second))

	cfg := common.HTTPClientConfig{
		Headers: map[string]string{
			"Authorization": "Bearer token",
		},
	}

	client := bp.NewHTTPClient(cfg)

	require.NotNil(t, client)
}

func TestNewHTTPClient_HeaderMerging(t *testing.T) {
	bp := common.NewBaseProvider(
		agentllm.WithCustomHeaders(map[string]string{
			"X-Custom":     "config-value",
			"X-Override":   "config",
			"X-ConfigOnly": "only-in-config",
		}),
	)

	cfg := common.HTTPClientConfig{
		Headers: map[string]string{
			"Authorization": "Bearer token",
			"X-Override":    "request", // Should not override config
		},
	}

	client := bp.NewHTTPClient(cfg)

	require.NotNil(t, client)
}
