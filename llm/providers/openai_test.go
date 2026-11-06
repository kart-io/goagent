package providers

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/kart-io/goagent/codec/json"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kart-io/goagent/interfaces"
	agentllm "github.com/kart-io/goagent/llm"
)

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
			result, err := agentllm.Complete(ctx, provider, &agentllm.CompletionRequest{
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

	result, err := agentllm.Complete(ctx, provider, &agentllm.CompletionRequest{
		Messages: []agentllm.Message{
			{Role: "user", Content: "Test"},
		},
	})

	assert.Error(t, err)
	assert.Nil(t, result)
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
		json.Decode(r.Body, &reqBody)

		// 提取参数
		if temp, ok := reqBody["temperature"].(float64); ok {
			receivedTemp = float32(temp)
		}
		if maxTokens, ok := reqBody["max_tokens"].(float64); ok {
			receivedMaxTokens = int(maxTokens)
		}

		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "data: {\"choices\":[{\"index\":0,\"delta\":{\"content\":\"Response\"},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n")
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
	_, err = agentllm.Complete(ctx, provider, &agentllm.CompletionRequest{
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
		json.Decode(r.Body, &reqBody)
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
		json.Encode(w, response)
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

func TestOpenAI_EmbedUsesEmbeddingModel(t *testing.T) {
	var model string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var reqBody map[string]interface{}
		_ = json.Decode(r.Body, &reqBody)
		model, _ = reqBody["model"].(string)
		w.Header().Set("Content-Type", "application/json")
		_ = json.Encode(w, map[string]interface{}{
			"object": "list",
			"data":   []map[string]interface{}{{"object": "embedding", "index": 0, "embedding": []float64{0.1}}},
		})
	}))
	defer server.Close()

	provider, err := NewOpenAIWithOptions(
		agentllm.WithAPIKey("test-api-key"),
		agentllm.WithBaseURL(server.URL+"/v1"),
		agentllm.WithEmbeddingModel("text-embedding-3-small"),
	)
	require.NoError(t, err)

	_, err = provider.Embed(context.Background(), "text")
	require.NoError(t, err)
	assert.Equal(t, "text-embedding-3-small", model)
}
