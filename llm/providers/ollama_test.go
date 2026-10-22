package providers

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	agentllm "github.com/kart-io/goagent/llm"
	"github.com/kart-io/goagent/llm/constants"
	"github.com/kart-io/goagent/utils/json"
)

// TestOllama_NewOllamaWithOptions_Success 测试成功创建 Ollama client
func TestOllama_NewOllamaWithOptions_Success(t *testing.T) {
	client, err := NewOllamaWithOptions(
		agentllm.WithModel("llama2"),
		agentllm.WithBaseURL("http://localhost:11434"),
		agentllm.WithMaxTokens(1000),
		agentllm.WithTemperature(0.7),
	)

	require.NoError(t, err)
	require.NotNil(t, client)
	assert.NotNil(t, client.BaseProvider)
}

// TestOllama_NewOllamaClientSimple 测试简化构造函数
func TestOllama_NewOllamaClientSimple(t *testing.T) {
	client, err := NewOllamaClientSimple("llama2")

	require.NoError(t, err)
	require.NotNil(t, client)
	assert.NotNil(t, client.BaseProvider)
}

// TestOllama_NewOllamaWithOptions_DefaultBaseURL 测试默认 BaseURL
func TestOllama_NewOllamaWithOptions_DefaultBaseURL(t *testing.T) {
	client, err := NewOllamaWithOptions(
		agentllm.WithModel("llama2"),
	)

	require.NoError(t, err)
	assert.NotNil(t, client.BaseProvider)
}

// TestOllama_Provider 测试 Provider 方法
func TestOllama_Provider(t *testing.T) {
	client, err := NewOllamaWithOptions(
		agentllm.WithModel("llama2"),
	)
	require.NoError(t, err)

	assert.Equal(t, constants.ProviderOllama, client.Provider())
}

// TestOllama_IsAvailable 测试 IsAvailable 方法
func TestOllama_IsAvailable(t *testing.T) {
	client, err := NewOllamaWithOptions(
		agentllm.WithModel("llama2"),
		agentllm.WithBaseURL("http://invalid-host:11434"),
	)
	require.NoError(t, err)

	// 应该返回 false，因为没有真实的 Ollama 服务
	available := client.IsAvailable()
	assert.False(t, available)
}

// TestOllama_Complete_Success 测试成功的 Complete 调用
func TestOllama_Complete_Success(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "POST", r.Method)
		assert.Equal(t, "/api/generate", r.URL.Path)

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		response := map[string]interface{}{
			"model":          "llama2",
			"response":       "Ollama test response",
			"done":           true,
			"context":        []int{1, 2, 3},
			"total_duration": 1000000,
		}
		json.NewEncoder(w).Encode(response)
	}))
	defer server.Close()

	client, err := NewOllamaWithOptions(
		agentllm.WithModel("llama2"),
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
	assert.Equal(t, "Ollama test response", result.Content)
}

// TestOllama_Complete_HTTPError 测试 HTTP 错误处理
func TestOllama_Complete_HTTPError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(`{"error": "Internal server error"}`))
	}))
	defer server.Close()

	client, err := NewOllamaWithOptions(
		agentllm.WithModel("llama2"),
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

// TestOllama_Chat 测试 Chat 方法
func TestOllama_Chat(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/api/chat", r.URL.Path)

		w.Header().Set("Content-Type", "application/json")
		response := map[string]interface{}{
			"model": "llama2",
			"message": map[string]interface{}{
				"role":    "assistant",
				"content": "Chat response",
			},
			"done": true,
		}
		json.NewEncoder(w).Encode(response)
	}))
	defer server.Close()

	client, err := NewOllamaWithOptions(
		agentllm.WithModel("llama2"),
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

// TestOllama_GetFinishReason 测试 getFinishReason 方法
func TestOllama_GetFinishReason(t *testing.T) {
	client, err := NewOllamaWithOptions(
		agentllm.WithModel("llama2"),
	)
	require.NoError(t, err)

	tests := []struct {
		name     string
		done     bool
		expected string
	}{
		{"完成", true, "stop"},
		{"未完成", false, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := client.getFinishReason(tt.done)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// TestOllama_Configuration 测试不同模型配置
func TestOllama_Configuration(t *testing.T) {
	tests := []struct {
		name  string
		model string
	}{
		{"llama2", "llama2"},
		{"mistral", "mistral"},
		{"codellama", "codellama"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client, err := NewOllamaWithOptions(
				agentllm.WithModel(tt.model),
			)
			require.NoError(t, err)
			assert.NotNil(t, client)
		})
	}
}
