package providers

import (
	"context"
	"github.com/kart-io/goagent/utils/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kart-io/goagent/interfaces"
	"github.com/kart-io/goagent/llm"
)

// ==============================================================================
// ADDITIONAL DEEPSEEK PROVIDER TESTS - IsAvailable and Advanced Features
// ==============================================================================

// TestDeepSeekProvider_IsAvailable tests provider availability check
func TestDeepSeekProvider_IsAvailable(t *testing.T) {
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := DeepSeekResponse{
			ID:      "chatcmpl-test",
			Object:  "chat.completion",
			Created: time.Now().Unix(),
			Model:   "deepseek-chat",
			Choices: []DeepSeekChoice{
				{
					Index: 0,
					Message: DeepSeekMessage{
						Role:    "assistant",
						Content: "test",
					},
					FinishReason: "stop",
				},
			},
			Usage: DeepSeekUsage{TotalTokens: 5},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer mockServer.Close()

	provider, err := NewDeepSeek(&llm.Config{
		Provider: llm.ProviderDeepSeek,
		APIKey:   "test-key",
		BaseURL:  mockServer.URL,
		Timeout:  5,
	})
	require.NoError(t, err)

	isAvailable := provider.IsAvailable()
	assert.True(t, isAvailable)
}

// TestDeepSeekProvider_IsAvailable_Unavailable tests unavailable provider
func TestDeepSeekProvider_IsAvailable_Unavailable(t *testing.T) {
	provider, err := NewDeepSeek(&llm.Config{
		Provider: llm.ProviderDeepSeek,
		APIKey:   "test-key",
		BaseURL:  "http://invalid-host-that-does-not-exist.test",
		Timeout:  1,
	})
	require.NoError(t, err)

	isAvailable := provider.IsAvailable()
	assert.False(t, isAvailable)
}

// TestDeepSeekProvider_Stream_WithContext tests streaming with context cancellation
func TestDeepSeekProvider_Stream_WithContext(t *testing.T) {
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		resp := DeepSeekStreamResponse{
			ID:    "stream-test",
			Model: "deepseek-chat",
			Choices: []DeepSeekChoice{
				{
					Index: 0,
					Delta: DeepSeekMessage{Content: "test"},
				},
			},
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer mockServer.Close()

	provider, err := NewDeepSeek(&llm.Config{
		Provider: llm.ProviderDeepSeek,
		APIKey:   "test-key",
		BaseURL:  mockServer.URL,
		Timeout:  5,
	})
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	tokens, err := provider.Stream(ctx, "test")
	require.NoError(t, err)

	var count int
	for range tokens {
		count++
	}
	assert.Greater(t, count, 0)
}

// TestDeepSeekProvider_CompleteWithAllFields tests complete with all fields
func TestDeepSeekProvider_CompleteWithAllFields(t *testing.T) {
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req DeepSeekRequest
		json.NewDecoder(r.Body).Decode(&req)

		// Verify all fields are populated
		assert.NotEmpty(t, req.Model)
		assert.NotEmpty(t, req.Messages)
		assert.Greater(t, req.MaxTokens, 0)
		assert.Greater(t, req.Temperature, 0.0)

		resp := DeepSeekResponse{
			ID:      "chatcmpl-complete",
			Object:  "chat.completion",
			Created: time.Now().Unix(),
			Model:   req.Model,
			Choices: []DeepSeekChoice{
				{
					Index: 0,
					Message: DeepSeekMessage{
						Role:    "assistant",
						Content: "Complete response",
					},
					FinishReason: "stop",
				},
			},
			Usage: DeepSeekUsage{
				PromptTokens:     20,
				CompletionTokens: 15,
				TotalTokens:      35,
			},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer mockServer.Close()

	provider, err := NewDeepSeek(&llm.Config{
		Provider:    llm.ProviderDeepSeek,
		APIKey:      "test-key",
		BaseURL:     mockServer.URL,
		Model:       "deepseek-chat",
		MaxTokens:   1000,
		Temperature: 0.8,
		Timeout:     5,
	})
	require.NoError(t, err)

	resp, err := provider.Complete(context.Background(), &llm.CompletionRequest{
		Messages: []llm.Message{
			llm.SystemMessage("You are helpful"),
			llm.UserMessage("What is AI?"),
			llm.AssistantMessage("AI is artificial intelligence"),
			llm.UserMessage("Tell me more"),
		},
		MaxTokens:   1500,
		Temperature: 0.9,
		TopP:        0.95,
		Stop:        []string{"stop"},
	})

	assert.NoError(t, err)
	assert.NotNil(t, resp)
	assert.Equal(t, "Complete response", resp.Content)
	assert.Equal(t, 35, resp.TokensUsed)
	assert.Equal(t, "stop", resp.FinishReason)
}

// TestDeepSeekProvider_StreamWithTools_InvalidJSON tests streaming with invalid JSON args
func TestDeepSeekProvider_StreamWithTools_InvalidJSON(t *testing.T) {
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		// Stream with invalid JSON in arguments
		streamResponses := []DeepSeekStreamResponse{
			{
				ID:    "tool-stream-1",
				Model: "deepseek-chat",
				Choices: []DeepSeekChoice{
					{
						Index: 0,
						Delta: DeepSeekMessage{Content: "Calling tool..."},
					},
				},
			},
			{
				ID:    "tool-stream-2",
				Model: "deepseek-chat",
				Choices: []DeepSeekChoice{
					{
						Index: 0,
						Delta: DeepSeekMessage{
							ToolCalls: []DeepSeekToolCall{
								{
									ID:   "call_123",
									Type: "function",
									Function: DeepSeekFunctionCall{
										Name:      "test",
										Arguments: `{invalid`,
									},
								},
							},
						},
					},
				},
			},
			{
				ID:    "tool-stream-3",
				Model: "deepseek-chat",
				Choices: []DeepSeekChoice{
					{
						Index:        0,
						FinishReason: "tool_calls",
					},
				},
			},
		}

		for _, resp := range streamResponses {
			json.NewEncoder(w).Encode(resp)
		}
	}))
	defer mockServer.Close()

	provider, err := NewDeepSeek(&llm.Config{
		Provider: llm.ProviderDeepSeek,
		APIKey:   "test-key",
		BaseURL:  mockServer.URL,
		Timeout:  5,
	})
	require.NoError(t, err)

	chunks, err := provider.StreamWithTools(context.Background(), "test", []interfaces.Tool{&MockTool{}})
	require.NoError(t, err)

	// Should still process the stream, skipping invalid JSON
	var chunkCount int
	for range chunks {
		chunkCount++
	}
	assert.Greater(t, chunkCount, 0)
}

// TestDeepSeekProvider_ChatWithMultipleMessages tests chat with many messages
func TestDeepSeekProvider_ChatWithMultipleMessages(t *testing.T) {
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req DeepSeekRequest
		json.NewDecoder(r.Body).Decode(&req)

		// Verify all messages are passed through
		assert.Len(t, req.Messages, 4)

		resp := DeepSeekResponse{
			ID:      "chatcmpl-multi",
			Object:  "chat.completion",
			Created: time.Now().Unix(),
			Model:   "deepseek-chat",
			Choices: []DeepSeekChoice{
				{
					Index: 0,
					Message: DeepSeekMessage{
						Role:    "assistant",
						Content: "Multi-turn response",
					},
					FinishReason: "stop",
				},
			},
			Usage: DeepSeekUsage{TotalTokens: 50},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer mockServer.Close()

	provider, err := NewDeepSeek(&llm.Config{
		Provider: llm.ProviderDeepSeek,
		APIKey:   "test-key",
		BaseURL:  mockServer.URL,
		Timeout:  5,
	})
	require.NoError(t, err)

	messages := []llm.Message{
		llm.SystemMessage("System instruction"),
		llm.UserMessage("First user message"),
		llm.AssistantMessage("First assistant response"),
		llm.UserMessage("Second user message"),
	}

	resp, err := provider.Chat(context.Background(), messages)
	assert.NoError(t, err)
	assert.NotNil(t, resp)
	assert.Equal(t, "Multi-turn response", resp.Content)
}

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

	provider, err := NewDeepSeek(&llm.Config{
		Provider: llm.ProviderDeepSeek,
		APIKey:   "test-key",
		BaseURL:  mockServer.URL,
		Timeout:  5,
	})
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
		w.Header().Set("Content-Type", "application/json")

		// Send one good response, then close connection
		resp := DeepSeekStreamResponse{
			ID:    "stream-error",
			Model: "deepseek-chat",
			Choices: []DeepSeekChoice{
				{
					Index: 0,
					Delta: DeepSeekMessage{Content: "token"},
				},
			},
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer mockServer.Close()

	provider, err := NewDeepSeekStreaming(&llm.Config{
		Provider: llm.ProviderDeepSeek,
		APIKey:   "test-key",
		BaseURL:  mockServer.URL,
		Timeout:  5,
	})
	require.NoError(t, err)

	tokens, err := provider.StreamWithMetadata(context.Background(), "test")
	require.NoError(t, err)

	var tokenCount int
	for t := range tokens {
		if t.Type == "token" {
			tokenCount++
		}
	}
	assert.Greater(t, tokenCount, 0)
}

// TestDeepSeekStreamingProvider_Inheritance tests inherited methods from base provider
func TestDeepSeekStreamingProvider_Inheritance(t *testing.T) {
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := DeepSeekResponse{
			ID:      "test",
			Choices: []DeepSeekChoice{{Index: 0, Message: DeepSeekMessage{Content: "response"}}},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer mockServer.Close()

	provider, err := NewDeepSeekStreaming(&llm.Config{
		Provider: llm.ProviderDeepSeek,
		APIKey:   "test-key",
		BaseURL:  mockServer.URL,
		Timeout:  5,
	})
	require.NoError(t, err)

	// Test inherited Chat method
	resp, err := provider.Chat(context.Background(), []llm.Message{llm.UserMessage("test")})
	assert.NoError(t, err)
	assert.NotNil(t, resp)

	// Test inherited Provider method
	assert.Equal(t, llm.ProviderDeepSeek, provider.Provider())

	// Test inherited ModelName method
	assert.NotEmpty(t, provider.ModelName())
}

// ==============================================================================
// OPENAI PROVIDER TESTS WITH MOCKS
// ==============================================================================

// TestOpenAIProvider_ConvertToolsLarge tests conversion of many tools
func TestOpenAIProvider_ConvertToolsLarge(t *testing.T) {
	provider, err := NewOpenAI(&llm.Config{
		Provider: llm.ProviderOpenAI,
		APIKey:   "test-key",
	})
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
	config := &llm.Config{
		Provider: llm.ProviderGemini,
		APIKey:   "test-key",
		Model:    "gemini-pro",
	}

	provider, err := NewGemini(config)
	require.NoError(t, err)
	assert.Equal(t, "gemini-pro", provider.ModelName())
}

// TestGeminiProvider_MaxTokens tests max tokens retrieval
func TestGeminiProvider_MaxTokens(t *testing.T) {
	config := &llm.Config{
		Provider:  llm.ProviderGemini,
		APIKey:    "test-key",
		MaxTokens: 3000,
	}

	provider, err := NewGemini(config)
	require.NoError(t, err)
	assert.Equal(t, 3000, provider.MaxTokens())
}

// TestGeminiProvider_DefaultModel tests default model assignment
func TestGeminiProvider_DefaultModel(t *testing.T) {
	config := &llm.Config{
		Provider: llm.ProviderGemini,
		APIKey:   "test-key",
	}

	provider, err := NewGemini(config)
	require.NoError(t, err)
	assert.Equal(t, "gemini-pro", provider.modelName)
}

// TestGeminiProvider_ConvertToolsToFunctions tests tool conversion
func TestGeminiProvider_ConvertToolsToFunctions(t *testing.T) {
	provider, err := NewGemini(&llm.Config{
		Provider: llm.ProviderGemini,
		APIKey:   "test-key",
	})
	require.NoError(t, err)

	mockTool := &MockTool{}
	functions := provider.convertToolsToFunctions([]interfaces.Tool{mockTool})

	assert.Len(t, functions, 1)
	assert.Equal(t, "mock_tool", functions[0].Name)
	assert.Equal(t, "A mock tool for testing", functions[0].Description)
	assert.NotNil(t, functions[0].Parameters)
}

// TestGeminiProvider_ToolSchemaToGeminiSchema tests schema conversion
func TestGeminiProvider_ToolSchemaToGeminiSchema(t *testing.T) {
	provider, err := NewGemini(&llm.Config{
		Provider: llm.ProviderGemini,
		APIKey:   "test-key",
	})
	require.NoError(t, err)

	schema := provider.toolSchemaToGeminiSchema(nil)
	assert.NotNil(t, schema)
	assert.NotNil(t, schema.Properties)
	assert.NotEmpty(t, schema.Required)
}

// TestGeminiStreamingProvider_Creation tests streaming provider creation
func TestGeminiStreamingProvider_CreationComprehensive(t *testing.T) {
	config := &llm.Config{
		Provider: llm.ProviderGemini,
		APIKey:   "test-key",
		Model:    "gemini-pro",
	}

	provider, err := NewGeminiStreaming(config)
	assert.NoError(t, err)
	assert.NotNil(t, provider)
	assert.NotNil(t, provider.GeminiProvider)
}

// TestGeminiStreamingProvider_Inheritance tests inherited methods
func TestGeminiStreamingProvider_Inheritance(t *testing.T) {
	config := &llm.Config{
		Provider: llm.ProviderGemini,
		APIKey:   "test-key",
		Model:    "gemini-pro",
	}

	provider, err := NewGeminiStreaming(config)
	require.NoError(t, err)

	// Test inherited methods
	assert.Equal(t, llm.ProviderGemini, provider.Provider())
	assert.Equal(t, "gemini-pro", provider.ModelName())
	assert.Greater(t, provider.MaxTokens(), 0)
}

// ==============================================================================
// DEEPSEEK ADVANCED FEATURES
// ==============================================================================

// TestDeepSeekProvider_CompleteWithModelOverride tests model override in request
func TestDeepSeekProvider_CompleteWithModelOverride(t *testing.T) {
	modelUsed := ""

	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req DeepSeekRequest
		json.NewDecoder(r.Body).Decode(&req)
		modelUsed = req.Model

		resp := DeepSeekResponse{
			ID:      "test",
			Model:   req.Model,
			Choices: []DeepSeekChoice{{Index: 0, Message: DeepSeekMessage{Content: "test"}}},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer mockServer.Close()

	provider, err := NewDeepSeek(&llm.Config{
		Provider: llm.ProviderDeepSeek,
		APIKey:   "test-key",
		BaseURL:  mockServer.URL,
		Model:    "deepseek-chat",
		Timeout:  5,
	})
	require.NoError(t, err)

	_, err = provider.Complete(context.Background(), &llm.CompletionRequest{
		Messages: []llm.Message{llm.UserMessage("test")},
		Model:    "deepseek-coder",
	})

	assert.NoError(t, err)
	assert.Equal(t, "deepseek-coder", modelUsed)
}

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
			provider, err := NewDeepSeek(&llm.Config{
				Provider:    llm.ProviderDeepSeek,
				APIKey:      "test-key",
				Temperature: tt.temperature,
			})
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
			provider, err := NewDeepSeek(&llm.Config{
				Provider:  llm.ProviderDeepSeek,
				APIKey:    "test-key",
				MaxTokens: tt.maxTokens,
			})
			require.NoError(t, err)
			assert.NotNil(t, provider)
		})
	}
}

// TestDeepSeekProvider_EmptyMessages tests handling of empty message list
func TestDeepSeekProvider_EmptyMessages(t *testing.T) {
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := DeepSeekResponse{
			ID:      "test",
			Choices: []DeepSeekChoice{{Index: 0, Message: DeepSeekMessage{Content: "test"}}},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer mockServer.Close()

	provider, err := NewDeepSeek(&llm.Config{
		Provider: llm.ProviderDeepSeek,
		APIKey:   "test-key",
		BaseURL:  mockServer.URL,
		Timeout:  5,
	})
	require.NoError(t, err)

	resp, err := provider.Complete(context.Background(), &llm.CompletionRequest{
		Messages: []llm.Message{},
	})

	assert.NoError(t, err)
	assert.NotNil(t, resp)
}
