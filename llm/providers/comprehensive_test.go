package providers

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/kart-io/goagent/codec/json"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	shared "github.com/kart-io/goagent/constants"
	"github.com/kart-io/goagent/interfaces"
	"github.com/kart-io/goagent/llm"
	agentllm "github.com/kart-io/goagent/llm"
	"github.com/kart-io/goagent/llm/common"
)

// ==============================================================================
// DEEPSEEK PROVIDER TESTS
// ==============================================================================

// TestDeepSeekProvider_Initialization tests DeepSeek provider creation
func TestDeepSeekProvider_Initialization(t *testing.T) {
	tests := []struct {
		name    string
		config  *llm.LLMOptions
		wantErr bool
		errMsg  string
	}{
		{
			name: "valid config with all parameters",
			config: &llm.LLMOptions{
				Provider:    shared.ProviderDeepSeek,
				APIKey:      "test-key",
				Model:       "deepseek-coder",
				MaxTokens:   4000,
				Temperature: 0.5,
				BaseURL:     "https://api.deepseek.com/v1",
				Timeout:     30,
			},
			wantErr: false,
		},
		{
			name: "missing API key",
			config: &llm.LLMOptions{
				Provider:    shared.ProviderDeepSeek,
				Model:       "deepseek-chat",
				MaxTokens:   4000,
				Temperature: 0.5,
				BaseURL:     "https://api.deepseek.com/v1",
				Timeout:     30,
			},
			wantErr: true,
			errMsg:  "API key is required",
		},
		{
			name: "valid config with minimal parameters",
			config: &llm.LLMOptions{
				Provider: shared.ProviderDeepSeek,
				APIKey:   "test-key",
			},
			wantErr: false,
		},
		{
			name: "config with custom base URL",
			config: &llm.LLMOptions{
				Provider: shared.ProviderDeepSeek,
				APIKey:   "test-key",
				BaseURL:  "https://custom.deepseek.com",
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Clear environment variables for "missing API key" test
			if tt.wantErr && tt.errMsg == "API key is required" {
				originalAPIKey := os.Getenv("DEEPSEEK_API_KEY")
				os.Unsetenv("DEEPSEEK_API_KEY")
				defer func() {
					if originalAPIKey != "" {
						os.Setenv("DEEPSEEK_API_KEY", originalAPIKey)
					}
				}()
			}

			provider, err := NewDeepSeekWithOptions(common.ConfigToOptions(tt.config)...)

			if tt.wantErr {
				assert.Error(t, err)
				assert.Nil(t, provider)
				if tt.errMsg != "" {
					assert.Contains(t, err.Error(), tt.errMsg)
				}
			} else {
				assert.NoError(t, err)
				assert.NotNil(t, provider)
				assert.Equal(t, shared.ProviderDeepSeek, provider.Provider())
				assert.NotZero(t, provider.GetMaxTokens(0))
				assert.NotZero(t, provider.GetTemperature(0))
			}
		})
	}
}

// TestDeepSeekProvider_DefaultValues tests default value assignments
func TestDeepSeekProvider_DefaultValues(t *testing.T) {
	config := &llm.LLMOptions{
		Provider: shared.ProviderDeepSeek,
		APIKey:   "test-key",
	}

	provider, err := NewDeepSeekWithOptions(common.ConfigToOptions(config)...)
	require.NoError(t, err)

	assert.Equal(t, "deepseek-chat", provider.GetModel(""))
	assert.Equal(t, 2000, provider.GetMaxTokens(0))
	assert.Equal(t, 0.7, provider.GetTemperature(0))
	assert.Equal(t, "https://api.deepseek.com/v1", provider.baseURL)
}

// TestDeepSeekProvider_ModelName tests model name retrieval
func TestDeepSeekProvider_ModelName(t *testing.T) {
	config := &llm.LLMOptions{
		Provider: shared.ProviderDeepSeek,
		APIKey:   "test-key",
		Model:    "deepseek-coder",
	}

	provider, err := NewDeepSeekWithOptions(common.ConfigToOptions(config)...)
	require.NoError(t, err)

	assert.Equal(t, "deepseek-coder", provider.ModelName())
}

// TestDeepSeekProvider_MaxTokens tests max tokens retrieval
func TestDeepSeekProvider_MaxTokens(t *testing.T) {
	config := &llm.LLMOptions{
		Provider:  shared.ProviderDeepSeek,
		APIKey:    "test-key",
		MaxTokens: 3000,
	}

	provider, err := NewDeepSeekWithOptions(common.ConfigToOptions(config)...)
	require.NoError(t, err)

	assert.Equal(t, 3000, provider.MaxTokens())
}

// TestDeepSeekProvider_Embed tests embedding generation
func TestDeepSeekProvider_Embed(t *testing.T) {
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req interface{}
		json.Decode(r.Body, &req)

		type EmbedResponse struct {
			Object string `json:"object"`
			Data   []struct {
				Embedding []float64 `json:"embedding"`
				Index     int       `json:"index"`
			} `json:"data"`
			Model string `json:"model"`
		}

		resp := EmbedResponse{
			Object: "list",
			Data: []struct {
				Embedding []float64 `json:"embedding"`
				Index     int       `json:"index"`
			}{
				{
					Embedding: []float64{0.1, 0.2, 0.3},
					Index:     0,
				},
			},
			Model: "deepseek-embedding",
		}

		w.Header().Set("Content-Type", "application/json")
		json.Encode(w, resp)
	}))
	defer mockServer.Close()

	provider, err := NewDeepSeekWithOptions(agentllm.WithAPIKey("test-key"), agentllm.WithBaseURL(mockServer.URL), agentllm.WithTimeout(5*time.Second))
	require.NoError(t, err)

	embedding, err := provider.Embed(context.Background(), "test text")
	assert.NoError(t, err)
	assert.Len(t, embedding, 3)
	assert.Equal(t, float32(0.1), embedding[0])
}

// TestDeepSeekProvider_CallAPI_HTTPError tests HTTP error handling
func TestDeepSeekProvider_CallAPI_HTTPError(t *testing.T) {
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte(`{"error": "Invalid API key"}`))
	}))
	defer mockServer.Close()

	provider, err := NewDeepSeekWithOptions(agentllm.WithAPIKey("invalid-key"), agentllm.WithBaseURL(mockServer.URL), agentllm.WithTimeout(5*time.Second))
	require.NoError(t, err)

	_, err = llm.Complete(context.Background(), provider, &llm.CompletionRequest{
		Messages: []llm.Message{llm.UserMessage("Test")},
	})

	assert.Error(t, err)
	// With the new error handling, 401 status is mapped to InvalidConfigError
	// The error message will contain "Invalid API key" from the response body
	assert.Contains(t, err.Error(), "Invalid API key")
}

// TestDeepSeekProvider_CallAPI_NetworkError tests network error handling
func TestDeepSeekProvider_CallAPI_NetworkError(t *testing.T) {
	provider, err := NewDeepSeekWithOptions(agentllm.WithAPIKey("test-key"), agentllm.WithBaseURL("http://invalid-hostname-that-does-not-exist.test"), agentllm.WithTimeout(1*time.Second))
	require.NoError(t, err)

	_, err = llm.Complete(context.Background(), provider, &llm.CompletionRequest{
		Messages: []llm.Message{llm.UserMessage("Test")},
	})

	assert.Error(t, err)
}

// TestDeepSeekStreamingProvider_Creation tests streaming provider creation
func TestDeepSeekStreamingProvider_Creation(t *testing.T) {
	config := &llm.LLMOptions{
		Provider: shared.ProviderDeepSeek,
		APIKey:   "test-key",
	}

	provider, err := NewDeepSeekStreamingWithOptions(common.ConfigToOptions(config)...)
	assert.NoError(t, err)
	assert.NotNil(t, provider)
	assert.NotNil(t, provider.DeepSeekProvider)
}

// TestDeepSeekStreamingProvider_StreamWithMetadata tests streaming with metadata
func TestDeepSeekStreamingProvider_StreamWithMetadata(t *testing.T) {
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {

		streamResponses := []json.RawMessage{
			json.RawMessage(`{"id":"stream-1","model":"deepseek-chat","choices":[{"index":0,"delta":{"content":"Token1 "}}]}`),
			json.RawMessage(`{"id":"stream-2","model":"deepseek-chat","choices":[{"index":0,"delta":{"content":"Token2"}}]}`),
			json.RawMessage(`{"id":"stream-3","model":"deepseek-chat","choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}`),
		}

		writeDeepSeekSSE(w, true, streamResponses...)
	}))
	defer mockServer.Close()

	provider, err := NewDeepSeekStreamingWithOptions(agentllm.WithAPIKey("test-key"), agentllm.WithBaseURL(mockServer.URL), agentllm.WithTimeout(5*time.Second), agentllm.WithModel("deepseek-chat"), agentllm.WithTemperature(0.7))
	require.NoError(t, err)

	tokens, err := provider.StreamWithMetadata(context.Background(), "Test")
	require.NoError(t, err)

	var count int
	for token := range tokens {
		if token.Type == "token" {
			count++
		}
	}

	assert.Equal(t, 2, count)
}

// ==============================================================================
// OPENAI STREAMING PROVIDER TESTS
// ==============================================================================

// TestOpenAIStreamingProvider_CreationComprehensive tests OpenAI streaming provider creation
func TestOpenAIStreamingProvider_CreationComprehensive(t *testing.T) {
	config := &llm.LLMOptions{
		Provider:    shared.ProviderOpenAI,
		APIKey:      "test-key",
		BaseURL:     "https://api.openai.com/v1",
		Timeout:     5,
		Model:       "gpt-4",
		Temperature: 0.7,
	}

	provider, err := NewOpenAIWithOptions(common.ConfigToOptions(config)...)
	assert.NoError(t, err)
	assert.NotNil(t, provider)
}

// TestOpenAIStreamingProvider_CreationErrorComprehensive tests error handling in creation
func TestOpenAIStreamingProvider_CreationErrorComprehensive(t *testing.T) {
	// 缺少 API Key 的用例不能被宿主环境中的 OPENAI_API_KEY 补上。
	t.Setenv(shared.EnvOpenAIAPIKey, "")
	config := &llm.LLMOptions{
		Provider:    shared.ProviderOpenAI,
		BaseURL:     "https://api.openai.com/v1",
		Timeout:     5,
		Model:       "gpt-4",
		Temperature: 0.7,
		// Missing APIKey
	}

	provider, err := NewOpenAIWithOptions(common.ConfigToOptions(config)...)
	assert.Error(t, err)
	assert.Nil(t, provider)
}

// TestOpenAIProvider_InitializationComprehensive tests OpenAI provider creation
func TestOpenAIProvider_InitializationComprehensive(t *testing.T) {
	// 缺少 API Key 的用例不能被宿主环境中的 OPENAI_API_KEY 补上。
	t.Setenv(shared.EnvOpenAIAPIKey, "")
	tests := []struct {
		name    string
		config  *llm.LLMOptions
		wantErr bool
	}{
		{
			name: "valid config",
			config: &llm.LLMOptions{
				Provider:    shared.ProviderOpenAI,
				APIKey:      "test-key",
				Model:       "gpt-4",
				MaxTokens:   2000,
				Temperature: 0.7,
				BaseURL:     "https://api.openai.com/v1",
				Timeout:     5,
			},
			wantErr: false,
		},
		{
			name: "missing API key",
			config: &llm.LLMOptions{
				Provider:    shared.ProviderOpenAI,
				BaseURL:     "https://api.openai.com/v1",
				Timeout:     5,
				Model:       "gpt-4",
				MaxTokens:   2000,
				Temperature: 0.7,
			},
			wantErr: true,
		},
		{
			name: "with custom base URL",
			config: &llm.LLMOptions{
				Provider: shared.ProviderOpenAI,
				APIKey:   "test-key",
				BaseURL:  "https://custom.openai.com",
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			provider, err := NewOpenAIWithOptions(common.ConfigToOptions(tt.config)...)
			if tt.wantErr {
				assert.Error(t, err)
				assert.Nil(t, provider)
			} else {
				assert.NoError(t, err)
				assert.NotNil(t, provider)
				assert.Equal(t, shared.ProviderOpenAI, provider.Provider())
			}
		})
	}
}

// TestOpenAIProvider_DefaultModelComprehensive tests default model assignment
func TestOpenAIProvider_DefaultModelComprehensive(t *testing.T) {
	config := &llm.LLMOptions{
		Provider: shared.ProviderOpenAI,
		APIKey:   "test-key",
	}

	provider, err := NewOpenAIWithOptions(common.ConfigToOptions(config)...)
	require.NoError(t, err)

	// Should have a non-empty default model
	assert.NotEmpty(t, provider.GetModel(""))
}

// TestOpenAIProvider_ModelNameComprehensive tests model name retrieval
func TestOpenAIProvider_ModelNameComprehensive(t *testing.T) {
	config := &llm.LLMOptions{
		Provider:    shared.ProviderOpenAI,
		APIKey:      "test-key",
		BaseURL:     "https://api.openai.com/v1",
		Timeout:     5,
		Model:       "gpt-4",
		Temperature: 0.7,
		MaxTokens:   4000,
	}

	provider, err := NewOpenAIWithOptions(common.ConfigToOptions(config)...)
	require.NoError(t, err)

	assert.Equal(t, "gpt-4", provider.ModelName()) // Model name should match config
}

// TestOpenAIProvider_MaxTokensComprehensive tests max tokens retrieval
func TestOpenAIProvider_MaxTokensComprehensive(t *testing.T) {
	config := &llm.LLMOptions{
		Provider:    shared.ProviderOpenAI,
		APIKey:      "test-key",
		BaseURL:     "https://api.openai.com/v1",
		Timeout:     5,
		Model:       "gpt-4",
		Temperature: 0.7,
		MaxTokens:   4000,
	}

	provider, err := NewOpenAIWithOptions(common.ConfigToOptions(config)...)
	require.NoError(t, err)

	assert.Equal(t, 4000, provider.MaxTokens())
}

// TestOpenAIProvider_ConvertToolsToFunctionsComprehensive tests tool to function conversion
func TestOpenAIProvider_ConvertToolsToFunctionsComprehensive(t *testing.T) {
	provider, err := NewOpenAIWithOptions(agentllm.WithAPIKey("test-key"), agentllm.WithBaseURL("https://api.openai.com/v1"), agentllm.WithTimeout(5*time.Second), agentllm.WithModel("gpt-4"), agentllm.WithTemperature(0.7), agentllm.WithMaxTokens(4000))

	require.NoError(t, err)

	mockTool := &MockTool{}
	tools := []interfaces.Tool{mockTool}

	functions := provider.convertToolsToFunctions(tools)

	assert.Len(t, functions, 1)
	assert.Equal(t, "mock_tool", functions[0].Name)
	assert.Equal(t, "A mock tool for testing", functions[0].Description)
	assert.NotNil(t, functions[0].Parameters)
}

// TestOpenAIProvider_ToolSchemaToJSONComprehensive tests schema conversion
func TestOpenAIProvider_ToolSchemaToJSONComprehensive(t *testing.T) {
	provider, err := NewOpenAIWithOptions(agentllm.WithAPIKey("test-key"), agentllm.WithBaseURL("https://api.openai.com/v1"), agentllm.WithTimeout(5*time.Second), agentllm.WithModel("gpt-4"), agentllm.WithTemperature(0.7), agentllm.WithMaxTokens(4000))
	require.NoError(t, err)

	schema := provider.toolSchemaToJSON(nil)
	assert.NotNil(t, schema)

	// Convert to map to verify structure
	schemaMap := schema.(map[string]interface{})
	assert.Equal(t, "object", schemaMap["type"])
	assert.NotNil(t, schemaMap["properties"])
}

// ==============================================================================
// GEMINI PROVIDER TESTS
// ==============================================================================

// TestGeminiProvider_InitializationComprehensive tests Gemini provider creation
func TestGeminiProvider_InitializationComprehensive(t *testing.T) {
	// Save and clear Gemini environment variables
	savedAPIKey := os.Getenv("GOOGLE_API_KEY")
	savedGeminiKey := os.Getenv("GEMINI_API_KEY")
	os.Unsetenv("GOOGLE_API_KEY")
	os.Unsetenv("GEMINI_API_KEY")
	defer func() {
		if savedAPIKey != "" {
			os.Setenv("GOOGLE_API_KEY", savedAPIKey)
		}
		if savedGeminiKey != "" {
			os.Setenv("GEMINI_API_KEY", savedGeminiKey)
		}
	}()

	tests := []struct {
		name    string
		config  *llm.LLMOptions
		wantErr bool
	}{
		{
			name: "missing API key",
			config: &llm.LLMOptions{
				Provider: shared.ProviderGemini,
				// No API key provided
			},
			wantErr: true,
		},
		{
			name: "valid config with API key",
			config: &llm.LLMOptions{
				Provider: shared.ProviderGemini,
				APIKey:   "test-key",
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			provider, err := NewGeminiWithOptions(common.ConfigToOptions(tt.config)...)
			if tt.wantErr {
				assert.Error(t, err)
				assert.Nil(t, provider)
			} else {
				assert.NoError(t, err)
				assert.NotNil(t, provider)
				assert.Equal(t, shared.ProviderGemini, provider.Provider())
			}
		})
	}
}

// ==============================================================================
// HELPER FUNCTIONS AND UTILITIES TESTS
// ==============================================================================

// TestGenerateCallID tests unique ID generation
func TestGenerateCallID(t *testing.T) {
	id1 := common.GenerateCallID()
	time.Sleep(1 * time.Millisecond) // Ensure different timestamp
	id2 := common.GenerateCallID()

	assert.NotEmpty(t, id1)
	assert.NotEmpty(t, id2)
	assert.NotEqual(t, id1, id2)
	assert.True(t, strings.HasPrefix(id1, "call_"))
	assert.True(t, strings.HasPrefix(id2, "call_"))
}

// TestTokenWithMetadata_Structure tests TokenWithMetadata structure
func TestTokenWithMetadata_Structure(t *testing.T) {
	metadata := map[string]interface{}{
		"index": 1,
		"model": "test-model",
	}

	token := TokenWithMetadata{
		Type:     "token",
		Content:  "Test content",
		Metadata: metadata,
	}

	assert.Equal(t, "token", token.Type)
	assert.Equal(t, "Test content", token.Content)
	assert.Equal(t, 1, token.Metadata["index"])
	assert.Equal(t, "test-model", token.Metadata["model"])
}

// TestStreamEvent_Structure tests StreamEvent structure
func TestStreamEvent_Structure(t *testing.T) {
	now := time.Now()
	event := StreamEvent{
		Type:      "token",
		Content:   "Test",
		Timestamp: now,
		Metadata: map[string]interface{}{
			"count": 1,
		},
	}

	assert.Equal(t, "token", event.Type)
	assert.Equal(t, "Test", event.Content)
	assert.Equal(t, now, event.Timestamp)
	assert.Nil(t, event.Error)
}

// ==============================================================================
// ERROR HANDLING AND EDGE CASES
// ==============================================================================

// TestDeepSeekProvider_MalformedJSON tests malformed JSON handling
func TestDeepSeekProvider_MalformedJSON(t *testing.T) {
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte("invalid json"))
	}))
	defer mockServer.Close()

	provider, err := NewDeepSeekWithOptions(agentllm.WithAPIKey("test-key"), agentllm.WithBaseURL(mockServer.URL), agentllm.WithTimeout(5*time.Second))
	require.NoError(t, err)

	_, err = llm.Complete(context.Background(), provider, &llm.CompletionRequest{
		Messages: []llm.Message{llm.UserMessage("Test")},
	})

	assert.Error(t, err)
}

// TestDeepSeekProvider_ContextCancellation tests context cancellation
func TestDeepSeekProvider_ContextCancellation(t *testing.T) {
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(1 * time.Second) // Simulate slow response
		writeDeepSeekSSE(w, true, json.RawMessage(`{"choices":[{"index":0,"delta":{"content":"test"},"finish_reason":"stop"}]}`))
	}))
	defer mockServer.Close()

	provider, err := NewDeepSeekWithOptions(agentllm.WithAPIKey("test-key"), agentllm.WithBaseURL(mockServer.URL), agentllm.WithTimeout(5*time.Second), agentllm.WithModel("deepseek-chat"), agentllm.WithTemperature(0.7))
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err = llm.Complete(ctx, provider, &llm.CompletionRequest{
		Messages: []llm.Message{llm.UserMessage("Test")},
	})

	assert.Error(t, err)
}

// TestDeepSeekProvider_GetterMethods tests getter utility methods
func TestDeepSeekProvider_GetterMethods(t *testing.T) {
	provider, err := NewDeepSeekWithOptions(agentllm.WithAPIKey("test-key"), agentllm.WithTimeout(5*time.Second), agentllm.WithModel("deepseek-chat"), agentllm.WithTemperature(0.8), agentllm.WithMaxTokens(3000))
	require.NoError(t, err)

	// Test GetModel
	assert.Equal(t, "custom-model", provider.GetModel("custom-model"))
	assert.Equal(t, "deepseek-chat", provider.GetModel(""))

	// Test GetTemperature
	assert.Equal(t, 0.9, provider.GetTemperature(0.9))
	assert.Equal(t, 0.8, provider.GetTemperature(0))

	// Test GetMaxTokens
	assert.Equal(t, 5000, provider.GetMaxTokens(5000))
	assert.Equal(t, 3000, provider.GetMaxTokens(0))
}

// TestEmbeddingEmptyResponse tests embedding with no embeddings in response
func TestEmbeddingEmptyResponse(t *testing.T) {
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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
			}{},
		}

		w.Header().Set("Content-Type", "application/json")
		json.Encode(w, resp)
	}))
	defer mockServer.Close()

	provider, err := NewDeepSeekWithOptions(agentllm.WithAPIKey("test-key"), agentllm.WithBaseURL(mockServer.URL), agentllm.WithTimeout(5*time.Second), agentllm.WithModel("deepseek-chat"), agentllm.WithTemperature(0.7))
	require.NoError(t, err)

	_, err = provider.Embed(context.Background(), "test")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "no embeddings")
}
