package providers

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	agentllm "github.com/kart-io/goagent/llm"
	"github.com/kart-io/goagent/llm/constants"
	"github.com/kart-io/goagent/utils/json"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestKimi_NewKimiWithOptions_Success 测试成功创建 Kimi client
func TestKimi_NewKimiWithOptions_Success(t *testing.T) {
	client, err := NewKimiWithOptions(
		agentllm.WithAPIKey("test-api-key"),
		agentllm.WithModel("moonshot-v1-8k"),
		agentllm.WithMaxTokens(1000),
		agentllm.WithTemperature(0.7),
	)

	require.NoError(t, err)
	require.NotNil(t, client)
	assert.NotNil(t, client.BaseProvider)
}

// TestKimi_NewKimiWithOptions_DefaultModel 测试默认模型
func TestKimi_NewKimiWithOptions_DefaultModel(t *testing.T) {
	client, err := NewKimiWithOptions(
		agentllm.WithAPIKey("test-api-key"),
	)

	require.NoError(t, err)
	assert.NotNil(t, client.BaseProvider)
}

// TestKimi_Provider 测试 Provider 方法
func TestKimi_Provider(t *testing.T) {
	client, err := NewKimiWithOptions(
		agentllm.WithAPIKey("test-api-key"),
	)
	require.NoError(t, err)

	assert.Equal(t, constants.ProviderKimi, client.Provider())
}

// TestKimi_IsAvailable 测试 IsAvailable 方法
func TestKimi_IsAvailable(t *testing.T) {
	client, err := NewKimiWithOptions(
		agentllm.WithAPIKey("test-api-key"),
	)
	require.NoError(t, err)

	// 应该返回 false，因为没有真实的 API 连接
	available := client.IsAvailable()
	assert.False(t, available)
}

// TestKimi_Complete_Success 测试成功的 Complete 调用
func TestKimi_Complete_Success(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "POST", r.Method)
		assert.Contains(t, r.Header.Get("Authorization"), "Bearer")

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		response := map[string]interface{}{
			"id":      "cmpl-test",
			"object":  "chat.completion",
			"created": time.Now().Unix(),
			"model":   "moonshot-v1-8k",
			"choices": []map[string]interface{}{
				{
					"index": 0,
					"message": map[string]interface{}{
						"role":    "assistant",
						"content": "Kimi test response",
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

	client, err := NewKimiWithOptions(
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
	assert.Equal(t, "Kimi test response", result.Content)
	assert.Equal(t, 30, result.TokensUsed)
}

// TestKimi_Complete_HTTPError 测试 HTTP 错误处理
func TestKimi_Complete_HTTPError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte(`{"error": {"message": "Invalid API key"}}`))
	}))
	defer server.Close()

	client, err := NewKimiWithOptions(
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

// TestKimi_Chat 测试 Chat 方法
func TestKimi_Chat(t *testing.T) {
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

	client, err := NewKimiWithOptions(
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

// TestKimi_Configuration 测试不同配置
func TestKimi_Configuration(t *testing.T) {
	tests := []struct {
		name string
		opts []agentllm.ClientOption
	}{
		{
			"moonshot-v1-8k",
			[]agentllm.ClientOption{
				agentllm.WithAPIKey("test-api-key"),
				agentllm.WithModel("moonshot-v1-8k"),
			},
		},
		{
			"moonshot-v1-32k",
			[]agentllm.ClientOption{
				agentllm.WithAPIKey("test-api-key"),
				agentllm.WithModel("moonshot-v1-32k"),
			},
		},
		{
			"默认配置",
			[]agentllm.ClientOption{
				agentllm.WithAPIKey("test-api-key"),
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client, err := NewKimiWithOptions(tt.opts...)
			require.NoError(t, err)
			assert.NotNil(t, client)
		})
	}
}
