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
	"github.com/kart-io/goagent/llm/common"
	"github.com/kart-io/goagent/llm/constants"
	"github.com/kart-io/goagent/utils/json"
)

// ==============================================================================
// ADDITIONAL DEEPSEEK PROVIDER TESTS - IsAvailable and Advanced Features
// ==============================================================================

// TestDeepSeekProvider_EmbedMultipleChunks tests embedding with large dimensions
func TestDeepSeekProvider_EmbedMultipleChunks(t *testing.T) {
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Return large embedding vector
		embeddingSize := 1024
		embedding := make([]float64, embeddingSize)
		for i := 0; i < embeddingSize; i++ {
			embedding[i] = float64(i) / float64(embeddingSize)
		}

		type EmbedResponse struct {
			Object string `json:"object"`
			Data   []struct {
				Embedding []float64 `json:"embedding"`
			} `json:"data"`
		}

		resp := EmbedResponse{
			Object: "list",
			Data: []struct {
				Embedding []float64 `json:"embedding"`
			}{
				{Embedding: embedding},
			},
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer mockServer.Close()

	provider, err := NewDeepSeekWithOptions(agentllm.WithAPIKey("test-key"), agentllm.WithBaseURL(mockServer.URL), agentllm.WithTimeout(5*time.Second))
	require.NoError(t, err)

	embedding, err := provider.Embed(context.Background(), "test text")
	assert.NoError(t, err)
	assert.Len(t, embedding, 1024)
}

// ==============================================================================
// ADDITIONAL DEEPSEEK STREAMING PROVIDER TESTS
// ==============================================================================

// TestDeepSeekStreamingProvider_StreamWithMetadata_ErrorHandling tests error during streaming
func TestDeepSeekStreamingProvider_StreamWithMetadata_ErrorHandling(t *testing.T) {
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {

		// Send one good response, then close connection
		resp := json.RawMessage(`{"id":"stream-error","model":"deepseek-chat","choices":[{"index":0,"delta":{"content":"token"}}]}`)
		writeDeepSeekSSE(w, false, resp)
	}))
	defer mockServer.Close()

	provider, err := NewDeepSeekStreamingWithOptions(agentllm.WithAPIKey("test-key"), agentllm.WithBaseURL(mockServer.URL), agentllm.WithTimeout(5*time.Second))
	require.NoError(t, err)

	tokens, err := provider.StreamWithMetadata(context.Background(), "test")
	require.NoError(t, err)

	var tokenCount int
	var last TokenWithMetadata
	for token := range tokens {
		if token.Type == "token" {
			tokenCount++
		}
		last = token
	}
	assert.Greater(t, tokenCount, 0)
	// 缺少 finish_reason 与 [DONE] 即被截断的流必须以 error 结束，而不是伪装成正常完成
	assert.Equal(t, "error", last.Type)
	assert.Error(t, last.Error)
}

// ==============================================================================
// OPENAI PROVIDER TESTS WITH MOCKS
// ==============================================================================

// TestOpenAIProvider_ConvertToolsLarge tests conversion of many tools
func TestOpenAIProvider_ConvertToolsLarge(t *testing.T) {
	provider, err := NewOpenAIWithOptions(agentllm.WithAPIKey("test-key"))
	require.NoError(t, err)

	// Create multiple tools
	mockTools := make([]interfaces.Tool, 5)
	for i := 0; i < 5; i++ {
		mockTools[i] = &MockTool{}
	}

	functions := provider.convertToolsToFunctions(mockTools)
	assert.Len(t, functions, 5)

	for i, fn := range functions {
		assert.Equal(t, "mock_tool", fn.Name)
		assert.NotNil(t, fn.Parameters)
		_ = i
	}
}

// ==============================================================================
// GEMINI PROVIDER TESTS
// ==============================================================================

// TestGeminiProvider_ModelName tests model name retrieval
func TestGeminiProvider_ModelName(t *testing.T) {
	config := &agentllm.LLMOptions{
		Provider: constants.ProviderGemini,
		APIKey:   "test-key",
		Model:    "gemini-pro",
	}

	provider, err := NewGeminiWithOptions(common.ConfigToOptions(config)...)
	require.NoError(t, err)
	assert.Equal(t, "gemini-pro", provider.ModelName())
}

// TestGeminiProvider_MaxTokens tests max tokens retrieval
func TestGeminiProvider_MaxTokens(t *testing.T) {
	config := &agentllm.LLMOptions{
		Provider:  constants.ProviderGemini,
		APIKey:    "test-key",
		MaxTokens: 3000,
	}

	provider, err := NewGeminiWithOptions(common.ConfigToOptions(config)...)
	require.NoError(t, err)
	assert.Equal(t, 3000, provider.MaxTokens())
}

// TestGeminiProvider_DefaultModel tests default model assignment
func TestGeminiProvider_DefaultModel(t *testing.T) {
	config := &agentllm.LLMOptions{
		Provider: constants.ProviderGemini,
		APIKey:   "test-key",
	}

	provider, err := NewGeminiWithOptions(common.ConfigToOptions(config)...)
	require.NoError(t, err)
	assert.Equal(t, "gemini-2.5-flash", provider.GetModel(""))
}

// TestGeminiProvider_ConvertToolsToFunctions tests tool conversion
func TestGeminiProvider_ConvertToolsToFunctions(t *testing.T) {
	provider, err := NewGeminiWithOptions(agentllm.WithAPIKey("test-key"))
	require.NoError(t, err)

	mockTool := &MockTool{}
	functions, err := provider.convertToolsToFunctions([]interfaces.Tool{mockTool})
	require.NoError(t, err)

	assert.Len(t, functions, 1)
	assert.Equal(t, "mock_tool", functions[0].Name)
	assert.Equal(t, "A mock tool for testing", functions[0].Description)
	assert.NotNil(t, functions[0].ParametersJsonSchema)
}

// TestGeminiProvider_ToolSchemaToGeminiSchema tests schema conversion
func TestGeminiProvider_ToolSchemaToGeminiSchema(t *testing.T) {
	provider, err := NewGeminiWithOptions(agentllm.WithAPIKey("test-key"))
	require.NoError(t, err)

	schema, err := provider.toolSchemaToGeminiSchema(nil)
	require.NoError(t, err)
	assert.NotNil(t, schema)
	assert.NotNil(t, schema["properties"])
	// 默认 schema 的 Required 为空
}

// TestGeminiStreamingProvider_Creation tests streaming provider creation
func TestGeminiStreamingProvider_CreationComprehensive(t *testing.T) {
	config := &agentllm.LLMOptions{
		Provider: constants.ProviderGemini,
		APIKey:   "test-key",
		Model:    "gemini-pro",
	}

	provider, err := NewGeminiStreamingWithOptions(common.ConfigToOptions(config)...)
	assert.NoError(t, err)
	assert.NotNil(t, provider)
	assert.NotNil(t, provider.GeminiProvider)
}

// TestGeminiStreamingProvider_Inheritance tests inherited methods
func TestGeminiStreamingProvider_Inheritance(t *testing.T) {
	config := &agentllm.LLMOptions{
		Provider: constants.ProviderGemini,
		APIKey:   "test-key",
		Model:    "gemini-pro",
	}

	provider, err := NewGeminiStreamingWithOptions(common.ConfigToOptions(config)...)
	require.NoError(t, err)

	// Test inherited methods
	assert.Equal(t, constants.ProviderGemini, provider.Provider())
	assert.Equal(t, "gemini-pro", provider.ModelName())
	assert.Greater(t, provider.MaxTokens(), 0)
}

// ==============================================================================
// DEEPSEEK ADVANCED FEATURES
// ==============================================================================

// TestDeepSeekProvider_Temperature_Boundary tests temperature boundary values
func TestDeepSeekProvider_Temperature_Boundary(t *testing.T) {
	tests := []struct {
		name        string
		temperature float64
	}{
		{"zero temperature", 0},
		{"high temperature", 2.0},
		{"normal temperature", 0.7},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			provider, err := NewDeepSeekWithOptions(agentllm.WithAPIKey("test-key"), agentllm.WithTemperature(tt.temperature))
			require.NoError(t, err)
			assert.NotNil(t, provider)
		})
	}
}

// TestDeepSeekProvider_MaxTokens_Boundary tests max tokens boundary values
func TestDeepSeekProvider_MaxTokens_Boundary(t *testing.T) {
	tests := []struct {
		name      string
		maxTokens int
	}{
		{"zero max tokens", 0},
		{"small max tokens", 10},
		{"large max tokens", 128000},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			provider, err := NewDeepSeekWithOptions(agentllm.WithAPIKey("test-key"), agentllm.WithMaxTokens(tt.maxTokens))
			require.NoError(t, err)
			assert.NotNil(t, provider)
		})
	}
}
