package providers

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kart-io/goagent/interfaces"
	agentllm "github.com/kart-io/goagent/llm"
	"github.com/kart-io/goagent/llm/common"
	"github.com/kart-io/goagent/llm/constants"
)

// TestGemini_NewGeminiWithOptions_Success 测试成功创建 Gemini provider
func TestGemini_NewGeminiWithOptions_Success(t *testing.T) {
	provider, err := NewGeminiWithOptions(
		agentllm.WithAPIKey("test-api-key"),
		agentllm.WithModel("gemini-pro"),
		agentllm.WithMaxTokens(1000),
		agentllm.WithTemperature(0.7),
	)

	require.NoError(t, err)
	require.NotNil(t, provider)
	assert.NotNil(t, provider.BaseProvider)
	assert.NotNil(t, provider.ProviderCapabilities)
	assert.NotNil(t, provider.client)
	assert.Equal(t, "gemini-pro", provider.ModelName())
}

// TestGemini_NewGeminiWithOptions_MissingAPIKey 测试缺少 API key 的错误
func TestGemini_NewGeminiWithOptions_MissingAPIKey(t *testing.T) {
	// Ensure environment variable doesn't interfere with test
	t.Setenv(constants.EnvGeminiAPIKey, "")

	provider, err := NewGeminiWithOptions(
		agentllm.WithModel("gemini-pro"),
	)

	assert.Error(t, err)
	assert.Nil(t, provider)
	assert.Contains(t, err.Error(), "API key")
}

// TestGemini_NewGeminiWithOptions_DefaultModel 测试默认模型
func TestGemini_NewGeminiWithOptions_DefaultModel(t *testing.T) {
	provider, err := NewGeminiWithOptions(
		agentllm.WithAPIKey("test-api-key"),
	)

	require.NoError(t, err)
	assert.Equal(t, "gemini-2.5-flash", provider.ModelName())
}

// TestGemini_NewGeminiWithOptions_CustomParameters 测试自定义参数
func TestGemini_NewGeminiWithOptions_CustomParameters(t *testing.T) {
	provider, err := NewGeminiWithOptions(
		agentllm.WithAPIKey("test-api-key"),
		agentllm.WithModel("gemini-1.5-pro"),
		agentllm.WithMaxTokens(2048),
		agentllm.WithTemperature(0.9),
		agentllm.WithTopP(0.95),
	)

	require.NoError(t, err)
	assert.Equal(t, "gemini-1.5-pro", provider.ModelName())
	assert.Equal(t, 2048, provider.MaxTokens())
}

// TestGemini_NewGeminiWithOptions_LargeMaxTokens 测试超大 MaxTokens（应该被限制为 int32 max）
func TestGemini_NewGeminiWithOptions_LargeMaxTokens(t *testing.T) {
	provider, err := NewGeminiWithOptions(
		agentllm.WithAPIKey("test-api-key"),
		agentllm.WithMaxTokens(int(^uint32(0))), // 超大值
	)

	require.NoError(t, err)
	// MaxTokens 应该被限制在合理范围内
	_, cfg, err := provider.request(&agentllm.CompletionRequest{Messages: []agentllm.Message{agentllm.UserMessage("test")}}, nil)
	require.NoError(t, err)
	assert.EqualValues(t, 0x7fffffff, cfg.MaxOutputTokens)
}

// TestGemini_Provider 测试 Provider 方法
func TestGemini_Provider(t *testing.T) {
	provider, err := NewGeminiWithOptions(
		agentllm.WithAPIKey("test-api-key"),
	)
	require.NoError(t, err)

	assert.Equal(t, constants.ProviderGemini, provider.Provider())
}

// TestGemini_ProviderName 测试 ProviderName 方法
func TestGemini_ProviderName(t *testing.T) {
	provider, err := NewGeminiWithOptions(
		agentllm.WithAPIKey("test-api-key"),
	)
	require.NoError(t, err)

	assert.Equal(t, string(constants.ProviderGemini), provider.ProviderName())
}

// TestGemini_ModelName 测试 ModelName 方法
func TestGemini_ModelName(t *testing.T) {
	tests := []struct {
		name     string
		model    string
		expected string
	}{
		{
			name:     "gemini-pro",
			model:    "gemini-pro",
			expected: "gemini-pro",
		},
		{
			name:     "gemini-1.5-pro",
			model:    "gemini-1.5-pro",
			expected: "gemini-1.5-pro",
		},
		{
			name:     "default model",
			model:    "",
			expected: "gemini-2.5-flash",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			opts := []agentllm.ClientOption{
				agentllm.WithAPIKey("test-api-key"),
			}
			if tt.model != "" {
				opts = append(opts, agentllm.WithModel(tt.model))
			}

			provider, err := NewGeminiWithOptions(opts...)
			require.NoError(t, err)
			assert.Equal(t, tt.expected, provider.ModelName())
		})
	}
}

// TestGemini_MaxTokens 测试 MaxTokens 方法
func TestGemini_MaxTokens(t *testing.T) {
	tests := []struct {
		name      string
		maxTokens int
	}{
		{"small", 100},
		{"medium", 1000},
		{"large", 8192},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			provider, err := NewGeminiWithOptions(
				agentllm.WithAPIKey("test-api-key"),
				agentllm.WithMaxTokens(tt.maxTokens),
			)
			require.NoError(t, err)
			assert.Equal(t, tt.maxTokens, provider.MaxTokens())
		})
	}
}

// TestGemini_IsAvailable 测试 IsAvailable 方法（不需要真实 API 调用）
func TestGemini_IsAvailable(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Error(w, "unavailable", 503) }))
	defer server.Close()
	provider, err := NewGeminiWithOptions(agentllm.WithAPIKey("test-api-key"), agentllm.WithBaseURL(server.URL))
	require.NoError(t, err)
	assert.False(t, provider.IsAvailable())
}

// TestGemini_ConvertToolsToFunctions 测试工具转换功能
func TestGemini_ConvertToolsToFunctions(t *testing.T) {
	provider, err := NewGeminiWithOptions(
		agentllm.WithAPIKey("test-api-key"),
	)
	require.NoError(t, err)

	tools := []interfaces.Tool{
		&mockTool{
			name:        "get_weather",
			description: "Get weather information",
			schema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"location": map[string]interface{}{
						"type":        "string",
						"description": "City name",
					},
					"unit": map[string]interface{}{
						"type": "string",
						"enum": []string{"celsius", "fahrenheit"},
					},
				},
				"required": []string{"location"},
			},
		},
	}

	functions, err := provider.convertToolsToFunctions(tools)

	require.NoError(t, err)
	assert.Len(t, functions, 1)
	assert.Equal(t, "get_weather", functions[0].Name)
	assert.Equal(t, "Get weather information", functions[0].Description)
	assert.NotNil(t, functions[0].ParametersJsonSchema)
}

// TestGemini_ConvertToolsToFunctions_Multiple 测试多个工具转换
func TestGemini_ConvertToolsToFunctions_Multiple(t *testing.T) {
	provider, err := NewGeminiWithOptions(
		agentllm.WithAPIKey("test-api-key"),
	)
	require.NoError(t, err)

	tools := []interfaces.Tool{
		&mockTool{name: "tool1", description: "Tool 1", schema: map[string]interface{}{"type": "object"}},
		&mockTool{name: "tool2", description: "Tool 2", schema: map[string]interface{}{"type": "object"}},
		&mockTool{name: "tool3", description: "Tool 3", schema: map[string]interface{}{"type": "object"}},
	}

	functions, err := provider.convertToolsToFunctions(tools)

	require.NoError(t, err)
	assert.Len(t, functions, 3)
	for i, fn := range functions {
		assert.NotEmpty(t, fn.Name)
		assert.NotEmpty(t, fn.Description)
		assert.NotNil(t, fn.ParametersJsonSchema)
		_ = i
	}
}

// TestGemini_ConvertToolsToFunctions_EmptyList 测试空工具列表
func TestGemini_ConvertToolsToFunctions_EmptyList(t *testing.T) {
	provider, err := NewGeminiWithOptions(
		agentllm.WithAPIKey("test-api-key"),
	)
	require.NoError(t, err)

	functions, err := provider.convertToolsToFunctions([]interfaces.Tool{})

	require.NoError(t, err)
	assert.Empty(t, functions)
}

// TestGemini_ToolSchemaToGeminiSchema_NilSchema 测试 nil schema 处理
func TestGemini_ToolSchemaToGeminiSchema_NilSchema(t *testing.T) {
	provider, err := NewGeminiWithOptions(
		agentllm.WithAPIKey("test-api-key"),
	)
	require.NoError(t, err)

	schema, err := provider.toolSchemaToGeminiSchema(nil)

	require.NoError(t, err)
	assert.NotNil(t, schema)
	assert.NotNil(t, schema["properties"])
	// 默认 schema 的 Required 为空
}

// TestGemini_ToolSchemaToGeminiSchema_ValidSchema 测试有效 schema 转换
func TestGemini_ToolSchemaToGeminiSchema_ValidSchema(t *testing.T) {
	provider, err := NewGeminiWithOptions(
		agentllm.WithAPIKey("test-api-key"),
	)
	require.NoError(t, err)

	inputSchema := map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"name": map[string]interface{}{
				"type":        "string",
				"description": "User name",
			},
			"age": map[string]interface{}{
				"type":        "number",
				"description": "User age",
			},
		},
		"required": []string{"name"},
	}

	schema, err := provider.toolSchemaToGeminiSchema(inputSchema)

	require.NoError(t, err)
	assert.NotNil(t, schema)
	assert.NotNil(t, schema["properties"])
}

// TestGemini_Complete_EmptyMessages 测试空消息列表
func TestGemini_Complete_EmptyMessages(t *testing.T) {
	provider, err := NewGeminiWithOptions(
		agentllm.WithAPIKey("test-api-key"),
	)
	require.NoError(t, err)

	ctx := context.Background()
	result, err := provider.Complete(ctx, &agentllm.CompletionRequest{
		Messages: []agentllm.Message{},
	})

	// 应该返回错误或空结果
	// 注意：这可能会因为 Gemini API 拒绝空消息而失败
	require.Error(t, err)
	require.Nil(t, result)
	// 我们不断言具体结果，因为这取决于 Gemini SDK 的行为
}

// TestGemini_Chat_EmptyMessages 测试 Chat 方法的空消息
func TestGemini_Chat_EmptyMessages(t *testing.T) {
	provider, err := NewGeminiWithOptions(
		agentllm.WithAPIKey("test-api-key"),
	)
	require.NoError(t, err)

	ctx := context.Background()
	result, err := provider.Chat(ctx, []agentllm.Message{})

	// 应该返回错误或空结果
	require.Error(t, err)
	require.Nil(t, result)
}

// ==============================================================================
// GEMINI STREAMING PROVIDER TESTS
// ==============================================================================

// TestGeminiStreaming_NewGeminiStreamingWithOptions 测试流式 provider 创建
func TestGeminiStreaming_NewGeminiStreamingWithOptions(t *testing.T) {
	provider, err := NewGeminiStreamingWithOptions(
		agentllm.WithAPIKey("test-api-key"),
		agentllm.WithModel("gemini-pro"),
	)

	require.NoError(t, err)
	require.NotNil(t, provider)
	assert.NotNil(t, provider.GeminiProvider)
}

// TestGeminiStreaming_MissingAPIKey 测试流式 provider 缺少 API key
func TestGeminiStreaming_MissingAPIKey(t *testing.T) {
	// Ensure environment variable doesn't interfere with test
	t.Setenv(constants.EnvGeminiAPIKey, "")

	provider, err := NewGeminiStreamingWithOptions(
		agentllm.WithModel("gemini-pro"),
	)

	assert.Error(t, err)
	assert.Nil(t, provider)
}

// TestGeminiStreaming_Inheritance 测试流式 provider 继承的方法
func TestGeminiStreaming_Inheritance(t *testing.T) {
	provider, err := NewGeminiStreamingWithOptions(
		agentllm.WithAPIKey("test-api-key"),
		agentllm.WithModel("gemini-pro"),
	)
	require.NoError(t, err)

	// 测试继承的方法
	assert.Equal(t, constants.ProviderGemini, provider.Provider())
	assert.Equal(t, "gemini-pro", provider.ModelName())
	assert.Greater(t, provider.MaxTokens(), 0)
}

// TestGeminiStreaming_Configuration 测试流式 provider 配置
func TestGeminiStreaming_Configuration(t *testing.T) {
	config := &agentllm.LLMOptions{
		Provider:    constants.ProviderGemini,
		APIKey:      "test-api-key",
		Model:       "gemini-1.5-pro",
		MaxTokens:   2048,
		Temperature: 0.8,
	}

	provider, err := NewGeminiStreamingWithOptions(common.ConfigToOptions(config)...)
	require.NoError(t, err)

	assert.Equal(t, "gemini-1.5-pro", provider.ModelName())
	assert.Equal(t, 2048, provider.MaxTokens())
}

// TestGeminiStreaming_DefaultValues 测试流式 provider 默认值
func TestGeminiStreaming_DefaultValues(t *testing.T) {
	provider, err := NewGeminiStreamingWithOptions(
		agentllm.WithAPIKey("test-api-key"),
	)
	require.NoError(t, err)

	// 验证默认值
	assert.Equal(t, "gemini-2.5-flash", provider.ModelName())
	assert.NotZero(t, provider.MaxTokens())
}

// TestGeminiStreaming_NewGeminiStreaming_Deprecated 测试已弃用的构造函数
func TestGeminiStreaming_NewGeminiStreaming_Deprecated(t *testing.T) {
	config := &agentllm.LLMOptions{
		Provider: constants.ProviderGemini,
		APIKey:   "test-api-key",
		Model:    "gemini-pro",
	}

	provider, err := NewGeminiStreaming(config)
	require.NoError(t, err)
	require.NotNil(t, provider)
	assert.Equal(t, "gemini-pro", provider.ModelName())
}

// TestGeminiStreaming_NewGeminiStreaming_MissingAPIKey 测试已弃用构造函数缺少 API key
func TestGeminiStreaming_NewGeminiStreaming_MissingAPIKey(t *testing.T) {
	// Ensure environment variable doesn't interfere with test
	t.Setenv(constants.EnvGeminiAPIKey, "")

	config := &agentllm.LLMOptions{
		Provider: constants.ProviderGemini,
		Model:    "gemini-pro",
	}

	provider, err := NewGeminiStreaming(config)
	assert.Error(t, err)
	assert.Nil(t, provider)
}

// TestGemini_GetModel 测试 GetModel 方法
func TestGemini_GetModel(t *testing.T) {
	provider, err := NewGeminiWithOptions(
		agentllm.WithAPIKey("test-api-key"),
		agentllm.WithModel("gemini-1.5-pro"),
	)
	require.NoError(t, err)

	// 测试返回配置的模型
	assert.Equal(t, "gemini-1.5-pro", provider.GetModel(""))

	// 测试使用默认模型
	assert.Equal(t, "default-model", provider.GetModel("default-model"))
}

// TestGemini_GetMaxTokens 测试 GetMaxTokens 方法
func TestGemini_GetMaxTokens(t *testing.T) {
	provider, err := NewGeminiWithOptions(
		agentllm.WithAPIKey("test-api-key"),
		agentllm.WithMaxTokens(2000),
	)
	require.NoError(t, err)

	// 测试返回配置的 MaxTokens
	assert.Equal(t, 2000, provider.GetMaxTokens(0))

	// 测试使用默认值
	assert.Equal(t, 1000, provider.GetMaxTokens(1000))
}

// TestGemini_GetTemperature 测试 GetTemperature 方法
func TestGemini_GetTemperature(t *testing.T) {
	provider, err := NewGeminiWithOptions(
		agentllm.WithAPIKey("test-api-key"),
		agentllm.WithTemperature(0.9),
	)
	require.NoError(t, err)

	// 测试返回配置的 Temperature
	assert.InDelta(t, 0.9, provider.GetTemperature(0), 0.01)

	// 测试使用默认值
	assert.InDelta(t, 0.5, provider.GetTemperature(0.5), 0.01)
}

// TestGemini_Configuration_Comprehensive 综合配置测试
func TestGemini_Configuration_Comprehensive(t *testing.T) {
	tests := []struct {
		name        string
		opts        []agentllm.ClientOption
		expectModel string
		expectMaxT  int
		expectTemp  float64
	}{
		{
			name: "所有参数自定义",
			opts: []agentllm.ClientOption{
				agentllm.WithAPIKey("test-key"),
				agentllm.WithModel("gemini-1.5-pro"),
				agentllm.WithMaxTokens(4096),
				agentllm.WithTemperature(0.8),
			},
			expectModel: "gemini-1.5-pro",
			expectMaxT:  4096,
			expectTemp:  0.8,
		},
		{
			name: "仅 API key",
			opts: []agentllm.ClientOption{
				agentllm.WithAPIKey("test-key"),
			},
			expectModel: "gemini-2.5-flash",
			expectMaxT:  0,
			expectTemp:  0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			provider, err := NewGeminiWithOptions(tt.opts...)
			require.NoError(t, err)
			assert.Equal(t, tt.expectModel, provider.ModelName())
			if tt.expectMaxT > 0 {
				assert.Equal(t, tt.expectMaxT, provider.MaxTokens())
			}
		})
	}
}
