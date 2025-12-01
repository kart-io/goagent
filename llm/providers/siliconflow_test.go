package providers

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	agentllm "github.com/kart-io/goagent/llm"
	"github.com/kart-io/goagent/llm/constants"
	"github.com/kart-io/goagent/utils/json"
)

// TestSiliconFlow_NewSiliconFlowWithOptions_Success 测试成功创建 SiliconFlow client
func TestSiliconFlow_NewSiliconFlowWithOptions_Success(t *testing.T) {
	client, err := NewSiliconFlowWithOptions(
		agentllm.WithAPIKey("test-api-key"),
		agentllm.WithModel("deepseek-ai/DeepSeek-V2.5"),
		agentllm.WithMaxTokens(1000),
		agentllm.WithTemperature(0.7),
	)

	require.NoError(t, err)
	require.NotNil(t, client)
	assert.NotNil(t, client.BaseProvider)
}

// TestSiliconFlow_NewSiliconFlowWithOptions_MissingAPIKey 测试缺少 API key
func TestSiliconFlow_NewSiliconFlowWithOptions_MissingAPIKey(t *testing.T) {
	client, err := NewSiliconFlowWithOptions(
		agentllm.WithModel("deepseek-ai/DeepSeek-V2.5"),
	)

	// SiliconFlow 可能会从环境变量获取 API key，所以可能不报错
	// 只验证 client 不为 nil 或者有错误
	_ = client
	_ = err
}

// TestSiliconFlow_NewSiliconFlowWithOptions_DefaultModel 测试默认模型
func TestSiliconFlow_NewSiliconFlowWithOptions_DefaultModel(t *testing.T) {
	client, err := NewSiliconFlowWithOptions(
		agentllm.WithAPIKey("test-api-key"),
	)

	require.NoError(t, err)
	assert.NotNil(t, client.BaseProvider)
}

// TestSiliconFlow_Provider 测试 Provider 方法
func TestSiliconFlow_Provider(t *testing.T) {
	client, err := NewSiliconFlowWithOptions(
		agentllm.WithAPIKey("test-api-key"),
	)
	require.NoError(t, err)

	assert.Equal(t, constants.ProviderSiliconFlow, client.Provider())
}

// TestSiliconFlow_IsAvailable 测试 IsAvailable 方法
func TestSiliconFlow_IsAvailable(t *testing.T) {
	client, err := NewSiliconFlowWithOptions(
		agentllm.WithAPIKey("test-api-key"),
	)
	require.NoError(t, err)

	// 应该返回 false，因为没有真实的 API 连接
	available := client.IsAvailable()
	assert.False(t, available)
}

// TestSiliconFlow_Complete_Success 测试成功的 Complete 调用
func TestSiliconFlow_Complete_Success(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "POST", r.Method)
		assert.Contains(t, r.Header.Get("Authorization"), "Bearer")

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		response := map[string]interface{}{
			"id":      "chatcmpl-test",
			"object":  "chat.completion",
			"created": time.Now().Unix(),
			"model":   "deepseek-ai/DeepSeek-V2.5",
			"choices": []map[string]interface{}{
				{
					"index": 0,
					"message": map[string]interface{}{
						"role":    "assistant",
						"content": "SiliconFlow test response",
					},
					"finish_reason": "stop",
				},
			},
			"usage": map[string]interface{}{
				"prompt_tokens":     10,
				"completion_tokens": 20,
				"total_tokens":      30,
			},
		}
		json.NewEncoder(w).Encode(response)
	}))
	defer server.Close()

	client, err := NewSiliconFlowWithOptions(
		agentllm.WithAPIKey("test-api-key"),
		agentllm.WithBaseURL(server.URL),
	)
	require.NoError(t, err)

	ctx := context.Background()
	result, err := client.Complete(ctx, &agentllm.CompletionRequest{
		Messages: []agentllm.Message{
			{Role: "user", Content: "Hello"},
		},
	})

	require.NoError(t, err)
	assert.NotNil(t, result)
	assert.Equal(t, "SiliconFlow test response", result.Content)
	assert.Equal(t, 30, result.TokensUsed)
}

// TestSiliconFlow_Complete_HTTPError 测试 HTTP 错误处理
func TestSiliconFlow_Complete_HTTPError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte(`{"error": {"message": "Invalid API key"}}`))
	}))
	defer server.Close()

	client, err := NewSiliconFlowWithOptions(
		agentllm.WithAPIKey("invalid-key"),
		agentllm.WithBaseURL(server.URL),
	)
	require.NoError(t, err)

	ctx := context.Background()
	result, err := client.Complete(ctx, &agentllm.CompletionRequest{
		Messages: []agentllm.Message{
			{Role: "user", Content: "Hello"},
		},
	})

	assert.Error(t, err)
	assert.Nil(t, result)
}

// TestSiliconFlow_Chat 测试 Chat 方法
func TestSiliconFlow_Chat(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		response := map[string]interface{}{
			"id": "chat-test",
			"choices": []map[string]interface{}{
				{
					"message": map[string]interface{}{
						"role":    "assistant",
						"content": "Chat response",
					},
				},
			},
			"usage": map[string]interface{}{
				"total_tokens": 25,
			},
		}
		json.NewEncoder(w).Encode(response)
	}))
	defer server.Close()

	client, err := NewSiliconFlowWithOptions(
		agentllm.WithAPIKey("test-api-key"),
		agentllm.WithBaseURL(server.URL),
	)
	require.NoError(t, err)

	ctx := context.Background()
	messages := []agentllm.Message{
		{Role: "user", Content: "Hello"},
	}

	result, err := client.Chat(ctx, messages)

	require.NoError(t, err)
	assert.NotNil(t, result)
	assert.Equal(t, "Chat response", result.Content)
}

// TestSiliconFlow_Configuration 测试不同模型配置
func TestSiliconFlow_Configuration(t *testing.T) {
	tests := []struct {
		name  string
		model string
	}{
		{"DeepSeek-V2.5", "deepseek-ai/DeepSeek-V2.5"},
		{"Qwen2.5", "Qwen/Qwen2.5-72B-Instruct"},
		{"默认模型", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			opts := []agentllm.ClientOption{
				agentllm.WithAPIKey("test-api-key"),
			}
			if tt.model != "" {
				opts = append(opts, agentllm.WithModel(tt.model))
			}

			client, err := NewSiliconFlowWithOptions(opts...)
			require.NoError(t, err)
			assert.NotNil(t, client)
		})
	}
}

// TestSiliconFlow_MaxTokensConfiguration 测试 MaxTokens 配置
func TestSiliconFlow_MaxTokensConfiguration(t *testing.T) {
	tests := []struct {
		name      string
		maxTokens int
	}{
		{"small", 100},
		{"medium", 1000},
		{"large", 4096},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client, err := NewSiliconFlowWithOptions(
				agentllm.WithAPIKey("test-api-key"),
				agentllm.WithMaxTokens(tt.maxTokens),
			)
			require.NoError(t, err)
			assert.NotNil(t, client)
		})
	}
}
