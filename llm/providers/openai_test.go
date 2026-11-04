package providers

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kart-io/goagent/interfaces"
	agentllm "github.com/kart-io/goagent/llm"
	"github.com/kart-io/goagent/utils/json"
)

// TestOpenAI_Complete_Success 测试成功的 Complete 调用
func TestOpenAI_Complete_Success(t *testing.T) {
	// 创建 Mock Server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// 验证请求
		assert.Equal(t, "POST", r.Method)
		assert.Equal(t, "/v1/chat/completions", r.URL.Path)
		assert.Contains(t, r.Header.Get("Authorization"), "Bearer")

		// 读取并验证请求体
		var reqBody map[string]interface{}
		err := json.NewDecoder(r.Body).Decode(&reqBody)
		assert.NoError(t, err)
		assert.Equal(t, "gpt-4", reqBody["model"])
		assert.NotEmpty(t, reqBody["messages"])

		// 返回模拟响应
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		response := map[string]interface{}{
			"id":      "chatcmpl-123",
			"object":  "chat.completion",
			"created": time.Now().Unix(),
			"model":   "gpt-4",
			"choices": []map[string]interface{}{
				{
					"index": 0,
					"message": map[string]interface{}{
						"role":    "assistant",
						"content": "Hello! This is a test response from OpenAI.",
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

	// 创建 Provider
	provider, err := NewOpenAIWithOptions(
		agentllm.WithAPIKey("test-api-key"),
		agentllm.WithBaseURL(server.URL+"/v1"),
		agentllm.WithModel("gpt-4"),
	)
	require.NoError(t, err)
	require.NotNil(t, provider)

	// 执行测试
	ctx := context.Background()
	result, err := provider.Complete(ctx, &agentllm.CompletionRequest{
		Messages: []agentllm.Message{
			{Role: "user", Content: "Hello, GPT!"},
		},
	})

	// 验证结果
	require.NoError(t, err)
	assert.NotNil(t, result)
	assert.Equal(t, "Hello! This is a test response from OpenAI.", result.Content)
	assert.Equal(t, "gpt-4", result.Model)
	assert.Equal(t, 30, result.TokensUsed)
	assert.Equal(t, "stop", result.FinishReason)
	assert.NotNil(t, result.Usage)
	assert.Equal(t, 10, result.Usage.PromptTokens)
	assert.Equal(t, 20, result.Usage.CompletionTokens)
	assert.Equal(t, 30, result.Usage.TotalTokens)
}

// TestOpenAI_Complete_EmptyResponse 测试空响应情况
func TestOpenAI_Complete_EmptyResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		// 返回没有 choices 的响应
		response := map[string]interface{}{
			"id":      "chatcmpl-123",
			"object":  "chat.completion",
			"created": time.Now().Unix(),
			"model":   "gpt-4",
			"choices": []map[string]interface{}{},
			"usage": map[string]interface{}{
				"total_tokens": 0,
			},
		}
		json.NewEncoder(w).Encode(response)
	}))
	defer server.Close()

	provider, err := NewOpenAIWithOptions(
		agentllm.WithAPIKey("test-api-key"),
		agentllm.WithBaseURL(server.URL+"/v1"),
	)
	require.NoError(t, err)

	ctx := context.Background()
	result, err := provider.Complete(ctx, &agentllm.CompletionRequest{
		Messages: []agentllm.Message{
			{Role: "user", Content: "Test"},
		},
	})

	// 应该返回错误
	assert.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "no choices")
}

// TestOpenAI_Complete_HTTPErrors 测试各种 HTTP 错误
func TestOpenAI_Complete_HTTPErrors(t *testing.T) {
	tests := []struct {
		name           string
		statusCode     int
		responseBody   string
		expectedErrMsg string
	}{
		{
			name:           "400 Bad Request",
			statusCode:     http.StatusBadRequest,
			responseBody:   `{"error": {"message": "Invalid request", "type": "invalid_request_error"}}`,
			expectedErrMsg: "Invalid request",
		},
		{
			name:           "401 Unauthorized",
			statusCode:     http.StatusUnauthorized,
			responseBody:   `{"error": {"message": "Invalid API key", "type": "invalid_request_error"}}`,
			expectedErrMsg: "Invalid API key",
		},
		{
			name:           "429 Rate Limit",
			statusCode:     http.StatusTooManyRequests,
			responseBody:   `{"error": {"message": "Rate limit exceeded", "type": "rate_limit_error"}}`,
			expectedErrMsg: "Rate limit exceeded",
		},
		{
			name:           "500 Internal Server Error",
			statusCode:     http.StatusInternalServerError,
			responseBody:   `{"error": {"message": "Internal server error", "type": "server_error"}}`,
			expectedErrMsg: "Internal server error",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(tt.statusCode)
				w.Write([]byte(tt.responseBody))
			}))
			defer server.Close()

			provider, err := NewOpenAIWithOptions(
				agentllm.WithAPIKey("test-api-key"),
				agentllm.WithBaseURL(server.URL+"/v1"),
			)
			require.NoError(t, err)

			ctx := context.Background()
			result, err := provider.Complete(ctx, &agentllm.CompletionRequest{
				Messages: []agentllm.Message{
					{Role: "user", Content: "Test"},
				},
			})

			assert.Error(t, err)
			assert.Nil(t, result)
			assert.Contains(t, err.Error(), tt.expectedErrMsg)
		})
	}
}

// TestOpenAI_Complete_Timeout 测试超时情况
func TestOpenAI_Complete_Timeout(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// 模拟慢响应
		time.Sleep(2 * time.Second)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	provider, err := NewOpenAIWithOptions(
		agentllm.WithAPIKey("test-api-key"),
		agentllm.WithBaseURL(server.URL+"/v1"),
		agentllm.WithTimeout(1*time.Second), // 1 second timeout
	)
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	result, err := provider.Complete(ctx, &agentllm.CompletionRequest{
		Messages: []agentllm.Message{
			{Role: "user", Content: "Test"},
		},
	})

	assert.Error(t, err)
	assert.Nil(t, result)
}

// TestOpenAI_Chat 测试 Chat 方法
func TestOpenAI_Chat(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		response := map[string]interface{}{
			"id":      "chatcmpl-456",
			"object":  "chat.completion",
			"created": time.Now().Unix(),
			"model":   "gpt-4",
			"choices": []map[string]interface{}{
				{
					"message": map[string]interface{}{
						"role":    "assistant",
						"content": "Chat response",
					},
					"finish_reason": "stop",
				},
			},
			"usage": map[string]interface{}{
				"total_tokens": 25,
			},
		}
		json.NewEncoder(w).Encode(response)
	}))
	defer server.Close()

	provider, err := NewOpenAIWithOptions(
		agentllm.WithAPIKey("test-api-key"),
		agentllm.WithBaseURL(server.URL+"/v1"),
	)
	require.NoError(t, err)

	ctx := context.Background()
	messages := []agentllm.Message{
		{Role: "system", Content: "You are a helpful assistant."},
		{Role: "user", Content: "Hello!"},
	}

	result, err := provider.Chat(ctx, messages)

	require.NoError(t, err)
	assert.NotNil(t, result)
	assert.Equal(t, "Chat response", result.Content)
	assert.Equal(t, "gpt-4", result.Model)
	assert.Equal(t, 25, result.TokensUsed)
}

// TestOpenAI_ProviderInfo 测试 Provider 信息方法
func TestOpenAI_ProviderInfo(t *testing.T) {
	provider, err := NewOpenAIWithOptions(
		agentllm.WithAPIKey("test-api-key"),
		agentllm.WithModel("gpt-4-turbo"),
	)
	require.NoError(t, err)

	assert.Equal(t, "openai", string(provider.Provider()))
	assert.Equal(t, "gpt-4-turbo", provider.ModelName())
	assert.Greater(t, provider.MaxTokens(), 0)
}

// TestOpenAI_IsAvailable 测试 IsAvailable 方法
func TestOpenAI_IsAvailable(t *testing.T) {
	provider, err := NewOpenAIWithOptions(
		agentllm.WithAPIKey("test-api-key"),
	)
	require.NoError(t, err)

	// 应该返回 false，因为没有真实的 API 连接
	available := provider.IsAvailable()
	assert.False(t, available)
}

// mockTool 是用于测试的 mock tool
type mockTool struct {
	name        string
	description string
	schema      map[string]interface{}
}

func (m *mockTool) Name() string        { return m.name }
func (m *mockTool) Description() string { return m.description }
func (m *mockTool) Schema() interface{} { return m.schema }
func (m *mockTool) ArgsSchema() string  { return "" }
func (m *mockTool) Invoke(ctx context.Context, input *interfaces.ToolInput) (*interfaces.ToolOutput, error) {
	return &interfaces.ToolOutput{
		Success: true,
		Result:  "Mock tool executed",
	}, nil
}

// TestOpenAI_ConvertToolsToFunctions 测试工具转换
func TestOpenAI_ConvertToolsToFunctions(t *testing.T) {
	provider, err := NewOpenAIWithOptions(
		agentllm.WithAPIKey("test-api-key"),
	)
	require.NoError(t, err)

	tools := []interfaces.Tool{
		&mockTool{
			name:        "calculator",
			description: "Perform calculations",
			schema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"operation": map[string]interface{}{"type": "string"},
					"a":         map[string]interface{}{"type": "number"},
					"b":         map[string]interface{}{"type": "number"},
				},
			},
		},
	}

	functions := provider.convertToolsToFunctions(tools)

	assert.Len(t, functions, 1)
	assert.Equal(t, "calculator", functions[0].Name)
	assert.Equal(t, "Perform calculations", functions[0].Description)
	assert.NotNil(t, functions[0].Parameters)
}

// TestOpenAI_Complete_WithParameters 测试带参数的 Complete 调用
func TestOpenAI_Complete_WithParameters(t *testing.T) {
	var receivedTemp float32
	var receivedMaxTokens int

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var reqBody map[string]interface{}
		json.NewDecoder(r.Body).Decode(&reqBody)

		// 提取参数
		if temp, ok := reqBody["temperature"].(float64); ok {
			receivedTemp = float32(temp)
		}
		if maxTokens, ok := reqBody["max_tokens"].(float64); ok {
			receivedMaxTokens = int(maxTokens)
		}

		w.Header().Set("Content-Type", "application/json")
		response := map[string]interface{}{
			"id":     "chatcmpl-123",
			"object": "chat.completion",
			"model":  "gpt-4",
			"choices": []map[string]interface{}{
				{
					"message": map[string]interface{}{
						"role":    "assistant",
						"content": "Response",
					},
					"finish_reason": "stop",
				},
			},
			"usage": map[string]interface{}{"total_tokens": 10},
		}
		json.NewEncoder(w).Encode(response)
	}))
	defer server.Close()

	provider, err := NewOpenAIWithOptions(
		agentllm.WithAPIKey("test-api-key"),
		agentllm.WithBaseURL(server.URL+"/v1"),
		agentllm.WithTemperature(0.8),
		agentllm.WithMaxTokens(500),
	)
	require.NoError(t, err)

	ctx := context.Background()
	_, err = provider.Complete(ctx, &agentllm.CompletionRequest{
		Messages: []agentllm.Message{{Role: "user", Content: "Test"}},
	})

	require.NoError(t, err)
	assert.Equal(t, float32(0.8), receivedTemp)
	assert.Equal(t, 500, receivedMaxTokens)
}

// TestOpenAI_Embed 测试嵌入功能
func TestOpenAI_Embed(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// 验证请求
		var reqBody map[string]interface{}
		json.NewDecoder(r.Body).Decode(&reqBody)
		assert.Equal(t, "text-embedding-ada-002", reqBody["model"])
		assert.NotEmpty(t, reqBody["input"])

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)

		// 返回模拟嵌入响应
		response := map[string]interface{}{
			"object": "list",
			"data": []map[string]interface{}{
				{
					"object":    "embedding",
					"index":     0,
					"embedding": []float64{0.1, 0.2, 0.3, 0.4, 0.5},
				},
			},
			"model": "text-embedding-ada-002",
			"usage": map[string]interface{}{
				"prompt_tokens": 8,
				"total_tokens":  8,
			},
		}
		json.NewEncoder(w).Encode(response)
	}))
	defer server.Close()

	provider, err := NewOpenAIWithOptions(
		agentllm.WithAPIKey("test-api-key"),
		agentllm.WithBaseURL(server.URL+"/v1"),
	)
	require.NoError(t, err)

	ctx := context.Background()
	embedding, err := provider.Embed(ctx, "This is a test text")

	require.NoError(t, err)
	assert.NotNil(t, embedding)
	assert.Len(t, embedding, 5)
	// 检查向量值在合理范围内（考虑浮点数精度）
	for i, val := range embedding {
		assert.InDelta(t, []float64{0.1, 0.2, 0.3, 0.4, 0.5}[i], val, 0.01, "embedding 值应该接近预期")
	}
}
