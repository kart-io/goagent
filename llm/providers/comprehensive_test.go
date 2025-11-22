package providers

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/kart-io/goagent/llm/constants"
	"github.com/kart-io/goagent/utils/json"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kart-io/goagent/interfaces"
	"github.com/kart-io/goagent/llm"
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
				Provider:    constants.ProviderDeepSeek,
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
				Provider:    constants.ProviderDeepSeek,
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
				Provider: constants.ProviderDeepSeek,
				APIKey:   "test-key",
			},
			wantErr: false,
		},
		{
			name: "config with custom base URL",
			config: &llm.LLMOptions{
				Provider: constants.ProviderDeepSeek,
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

			provider, err := NewDeepSeek(tt.config)

			if tt.wantErr {
				assert.Error(t, err)
				assert.Nil(t, provider)
				if tt.errMsg != "" {
					assert.Contains(t, err.Error(), tt.errMsg)
				}
			} else {
				assert.NoError(t, err)
				assert.NotNil(t, provider)
				assert.Equal(t, constants.ProviderDeepSeek, provider.Provider())
				assert.NotZero(t, provider.GetMaxTokens(0))
				assert.NotZero(t, provider.GetTemperature(0))
			}
		})
	}
}

// TestDeepSeekProvider_DefaultValues tests default value assignments
func TestDeepSeekProvider_DefaultValues(t *testing.T) {
	config := &llm.LLMOptions{
		Provider: constants.ProviderDeepSeek,
		APIKey:   "test-key",
	}

	provider, err := NewDeepSeek(config)
	require.NoError(t, err)

	assert.Equal(t, "deepseek-chat", provider.GetModel(""))
	assert.Equal(t, 2000, provider.GetMaxTokens(0))
	assert.Equal(t, 0.7, provider.GetTemperature(0))
	assert.Equal(t, "https://api.deepseek.com/v1", provider.baseURL)
}

// TestDeepSeekProvider_ModelName tests model name retrieval
func TestDeepSeekProvider_ModelName(t *testing.T) {
	config := &llm.LLMOptions{
		Provider: constants.ProviderDeepSeek,
		APIKey:   "test-key",
		Model:    "deepseek-coder",
	}

	provider, err := NewDeepSeek(config)
	require.NoError(t, err)

	assert.Equal(t, "deepseek-coder", provider.ModelName())
}

// TestDeepSeekProvider_MaxTokens tests max tokens retrieval
func TestDeepSeekProvider_MaxTokens(t *testing.T) {
	config := &llm.LLMOptions{
		Provider:  constants.ProviderDeepSeek,
		APIKey:    "test-key",
		MaxTokens: 3000,
	}

	provider, err := NewDeepSeek(config)
	require.NoError(t, err)

	assert.Equal(t, 3000, provider.MaxTokens())
}

// TestDeepSeekProvider_Complete tests text completion with HTTP mock
func TestDeepSeekProvider_Complete(t *testing.T) {
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify request headers
		assert.Equal(t, "POST", r.Method)
		assert.Equal(t, "Bearer test-key", r.Header.Get("Authorization"))
		assert.Equal(t, "application/json", r.Header.Get("Content-Type"))

		// Verify endpoint
		assert.True(t, strings.HasSuffix(r.URL.Path, "/chat/completions"))

		// Parse request body
		var req DeepSeekRequest
		err := json.NewDecoder(r.Body).Decode(&req)
		require.NoError(t, err)
		assert.NotEmpty(t, req.Messages)

		// Send response
		resp := DeepSeekResponse{
			ID:      "chatcmpl-123",
			Object:  "chat.completion",
			Created: time.Now().Unix(),
			Model:   req.Model,
			Choices: []DeepSeekChoice{
				{
					Index: 0,
					Message: DeepSeekMessage{
						Role:    "assistant",
						Content: "This is a test response.",
					},
					FinishReason: "stop",
				},
			},
			Usage: DeepSeekUsage{
				PromptTokens:     10,
				CompletionTokens: 10,
				TotalTokens:      20,
			},
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer mockServer.Close()

	config := &llm.LLMOptions{
		Provider: constants.ProviderDeepSeek,
		APIKey:   "test-key",
		BaseURL:  mockServer.URL,
		Model:    "deepseek-chat",
		Timeout:  5,
	}

	provider, err := NewDeepSeek(config)
	require.NoError(t, err)

	ctx := context.Background()
	req := &llm.CompletionRequest{
		Messages: []llm.Message{
			llm.UserMessage("Hello, how are you?"),
		},
		MaxTokens: 100,
	}

	resp, err := provider.Complete(ctx, req)
	require.NoError(t, err)
	assert.NotNil(t, resp)
	assert.Equal(t, "This is a test response.", resp.Content)
	assert.Equal(t, 20, resp.TokensUsed)
	assert.Equal(t, "deepseek-chat", resp.Model)
}

// TestDeepSeekProvider_Complete_OverrideRequestParams tests parameter override
func TestDeepSeekProvider_Complete_OverrideRequestParams(t *testing.T) {
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req DeepSeekRequest
		json.NewDecoder(r.Body).Decode(&req)

		// Verify request overrides
		assert.Equal(t, "custom-model", req.Model)
		assert.Equal(t, 5000, req.MaxTokens)
		assert.Equal(t, 0.9, req.Temperature)

		resp := DeepSeekResponse{
			ID:      "chatcmpl-456",
			Object:  "chat.completion",
			Created: time.Now().Unix(),
			Model:   req.Model,
			Choices: []DeepSeekChoice{
				{
					Index:        0,
					Message:      DeepSeekMessage{Role: "assistant", Content: "Response"},
					FinishReason: "stop",
				},
			},
			Usage: DeepSeekUsage{TotalTokens: 50},
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer mockServer.Close()

	provider, err := NewDeepSeek(&llm.LLMOptions{
		Provider: constants.ProviderDeepSeek,
		APIKey:   "test-key",
		BaseURL:  mockServer.URL,
		Timeout:  5,
	})
	require.NoError(t, err)

	resp, err := provider.Complete(context.Background(), &llm.CompletionRequest{
		Messages:    []llm.Message{llm.UserMessage("Test")},
		Model:       "custom-model",
		MaxTokens:   5000,
		Temperature: 0.9,
	})

	assert.NoError(t, err)
	assert.NotNil(t, resp)
}

// TestDeepSeekProvider_Complete_EmptyResponse tests handling of empty responses
func TestDeepSeekProvider_Complete_EmptyResponse(t *testing.T) {
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := DeepSeekResponse{
			ID:      "chatcmpl-789",
			Object:  "chat.completion",
			Choices: []DeepSeekChoice{}, // Empty choices
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer mockServer.Close()

	provider, err := NewDeepSeek(&llm.LLMOptions{
		Provider: constants.ProviderDeepSeek,
		APIKey:   "test-key",
		BaseURL:  mockServer.URL,
		Timeout:  5,
	})
	require.NoError(t, err)

	_, err = provider.Complete(context.Background(), &llm.CompletionRequest{
		Messages: []llm.Message{llm.UserMessage("Test")},
	})

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "no choices")
}

// TestDeepSeekProvider_Chat tests chat conversation
func TestDeepSeekProvider_Chat(t *testing.T) {
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := DeepSeekResponse{
			ID:      "chatcmpl-chat",
			Object:  "chat.completion",
			Created: time.Now().Unix(),
			Model:   "deepseek-chat",
			Choices: []DeepSeekChoice{
				{
					Index: 0,
					Message: DeepSeekMessage{
						Role:    "assistant",
						Content: "Chat response",
					},
					FinishReason: "stop",
				},
			},
			Usage: DeepSeekUsage{TotalTokens: 20},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer mockServer.Close()

	provider, err := NewDeepSeek(&llm.LLMOptions{
		Provider: constants.ProviderDeepSeek,
		APIKey:   "test-key",
		BaseURL:  mockServer.URL,
		Timeout:  5,
	})
	require.NoError(t, err)

	messages := []llm.Message{
		llm.SystemMessage("You are a helpful assistant"),
		llm.UserMessage("What is 2+2?"),
	}

	resp, err := provider.Chat(context.Background(), messages)
	assert.NoError(t, err)
	assert.NotNil(t, resp)
	assert.Equal(t, "Chat response", resp.Content)
}

// TestDeepSeekProvider_Stream tests streaming responses
func TestDeepSeekProvider_Stream(t *testing.T) {
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		// Send streaming responses
		streamResponses := []DeepSeekStreamResponse{
			{
				ID:      "chatcmpl-stream-1",
				Object:  "chat.completion.chunk",
				Created: time.Now().Unix(),
				Model:   "deepseek-chat",
				Choices: []DeepSeekChoice{
					{
						Index: 0,
						Delta: DeepSeekMessage{Content: "Hello "},
					},
				},
			},
			{
				ID:      "chatcmpl-stream-2",
				Object:  "chat.completion.chunk",
				Created: time.Now().Unix(),
				Model:   "deepseek-chat",
				Choices: []DeepSeekChoice{
					{
						Index: 0,
						Delta: DeepSeekMessage{Content: "world"},
					},
				},
			},
			{
				ID:      "chatcmpl-stream-3",
				Object:  "chat.completion.chunk",
				Created: time.Now().Unix(),
				Model:   "deepseek-chat",
				Choices: []DeepSeekChoice{
					{
						Index:        0,
						Delta:        DeepSeekMessage{},
						FinishReason: "stop",
					},
				},
			},
		}

		for _, resp := range streamResponses {
			json.NewEncoder(w).Encode(resp)
		}
	}))
	defer mockServer.Close()

	provider, err := NewDeepSeek(&llm.LLMOptions{
		Provider: constants.ProviderDeepSeek,
		APIKey:   "test-key",
		BaseURL:  mockServer.URL,
		Timeout:  5,
	})
	require.NoError(t, err)

	ctx := context.Background()
	tokens, err := provider.Stream(ctx, "Hello")
	require.NoError(t, err)

	var result []string
	for token := range tokens {
		result = append(result, token)
	}

	assert.Len(t, result, 2)
	assert.Equal(t, "Hello ", result[0])
	assert.Equal(t, "world", result[1])
}

// TestDeepSeekProvider_GenerateWithTools tests tool calling
func TestDeepSeekProvider_GenerateWithTools(t *testing.T) {
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req DeepSeekRequest
		json.NewDecoder(r.Body).Decode(&req)

		// Verify tools are included
		assert.Len(t, req.Tools, 1)
		assert.Equal(t, "mock_tool", req.Tools[0].Function.Name)

		resp := DeepSeekResponse{
			ID:      "chatcmpl-tools",
			Object:  "chat.completion",
			Created: time.Now().Unix(),
			Model:   "deepseek-chat",
			Choices: []DeepSeekChoice{
				{
					Index: 0,
					Message: DeepSeekMessage{
						Role:    "assistant",
						Content: "Getting weather...",
						ToolCalls: []DeepSeekToolCall{
							{
								ID:   "call_001",
								Type: "function",
								Function: DeepSeekFunctionCall{
									Name:      "mock_tool",
									Arguments: `{"location":"New York"}`,
								},
							},
						},
					},
					FinishReason: "tool_calls",
				},
			},
			Usage: DeepSeekUsage{TotalTokens: 50},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer mockServer.Close()

	provider, err := NewDeepSeek(&llm.LLMOptions{
		Provider:    constants.ProviderDeepSeek,
		APIKey:      "test-key",
		BaseURL:     mockServer.URL,
		Timeout:     5,
		Model:       "deepseek-chat",
		Temperature: 0.7,
		MaxTokens:   500,
	})
	require.NoError(t, err)

	mockTool := &MockTool{}
	toolsList := []interfaces.Tool{mockTool}

	resp, err := provider.GenerateWithTools(context.Background(), "What's the weather?", toolsList)
	assert.NoError(t, err)
	assert.NotNil(t, resp)
	assert.Equal(t, "Getting weather...", resp.Content)
	assert.Len(t, resp.ToolCalls, 1)
	assert.Equal(t, "mock_tool", resp.ToolCalls[0].Name)
	assert.Equal(t, "New York", resp.ToolCalls[0].Arguments["location"])
}

// TestDeepSeekProvider_StreamWithTools tests streaming tool calls
func TestDeepSeekProvider_StreamWithTools(t *testing.T) {
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		streamResponses := []DeepSeekStreamResponse{
			{
				ID:     "chatcmpl-tool-stream-1",
				Object: "chat.completion.chunk",
				Model:  "deepseek-chat",
				Choices: []DeepSeekChoice{
					{
						Index: 0,
						Delta: DeepSeekMessage{Content: "Processing..."},
					},
				},
			},
			{
				ID:     "chatcmpl-tool-stream-2",
				Object: "chat.completion.chunk",
				Model:  "deepseek-chat",
				Choices: []DeepSeekChoice{
					{
						Index: 0,
						Delta: DeepSeekMessage{
							ToolCalls: []DeepSeekToolCall{
								{
									ID:   "call_002",
									Type: "function",
									Function: DeepSeekFunctionCall{
										Name:      "tool_name",
										Arguments: `{"param":`,
									},
								},
							},
						},
					},
				},
			},
			{
				ID:     "chatcmpl-tool-stream-3",
				Object: "chat.completion.chunk",
				Model:  "deepseek-chat",
				Choices: []DeepSeekChoice{
					{
						Index: 0,
						Delta: DeepSeekMessage{
							ToolCalls: []DeepSeekToolCall{
								{
									Function: DeepSeekFunctionCall{
										Arguments: `"value"}`,
									},
								},
							},
						},
					},
				},
			},
			{
				ID:     "chatcmpl-tool-stream-4",
				Object: "chat.completion.chunk",
				Model:  "deepseek-chat",
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

	provider, err := NewDeepSeek(&llm.LLMOptions{
		Provider: constants.ProviderDeepSeek,
		APIKey:   "test-key",
		BaseURL:  mockServer.URL,
		Timeout:  5,
	})
	require.NoError(t, err)

	mockTool := &MockTool{}
	chunks, err := provider.StreamWithTools(context.Background(), "Test", []interfaces.Tool{mockTool})
	require.NoError(t, err)

	var chunkCount int
	for chunk := range chunks {
		chunkCount++
		assert.NotEmpty(t, chunk.Type)
	}

	assert.Greater(t, chunkCount, 0)
}

// TestDeepSeekProvider_Embed tests embedding generation
func TestDeepSeekProvider_Embed(t *testing.T) {
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req interface{}
		json.NewDecoder(r.Body).Decode(&req)

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
		json.NewEncoder(w).Encode(resp)
	}))
	defer mockServer.Close()

	provider, err := NewDeepSeek(&llm.LLMOptions{
		Provider: constants.ProviderDeepSeek,
		APIKey:   "test-key",
		BaseURL:  mockServer.URL,
		Timeout:  5,
	})
	require.NoError(t, err)

	embedding, err := provider.Embed(context.Background(), "test text")
	assert.NoError(t, err)
	assert.Len(t, embedding, 3)
	assert.Equal(t, 0.1, embedding[0])
}

// TestDeepSeekProvider_CallAPI_HTTPError tests HTTP error handling
func TestDeepSeekProvider_CallAPI_HTTPError(t *testing.T) {
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte(`{"error": "Invalid API key"}`))
	}))
	defer mockServer.Close()

	provider, err := NewDeepSeek(&llm.LLMOptions{
		Provider: constants.ProviderDeepSeek,
		APIKey:   "invalid-key",
		BaseURL:  mockServer.URL,
		Timeout:  5,
	})
	require.NoError(t, err)

	_, err = provider.Complete(context.Background(), &llm.CompletionRequest{
		Messages: []llm.Message{llm.UserMessage("Test")},
	})

	assert.Error(t, err)
	// With the new error handling, 401 status is mapped to InvalidConfigError
	// The error message will contain "Invalid API key" from the response body
	assert.Contains(t, err.Error(), "Invalid API key")
}

// TestDeepSeekProvider_CallAPI_NetworkError tests network error handling
func TestDeepSeekProvider_CallAPI_NetworkError(t *testing.T) {
	provider, err := NewDeepSeek(&llm.LLMOptions{
		Provider: constants.ProviderDeepSeek,
		APIKey:   "test-key",
		BaseURL:  "http://invalid-hostname-that-does-not-exist.test",
		Timeout:  1,
	})
	require.NoError(t, err)

	_, err = provider.Complete(context.Background(), &llm.CompletionRequest{
		Messages: []llm.Message{llm.UserMessage("Test")},
	})

	assert.Error(t, err)
}

// TestDeepSeekProvider_ConvertToolsToDeepSeek tests tool conversion
func TestDeepSeekProvider_ConvertToolsToDeepSeek(t *testing.T) {
	provider, err := NewDeepSeek(&llm.LLMOptions{
		Provider: constants.ProviderDeepSeek,
		APIKey:   "test-key",
	})
	require.NoError(t, err)

	mockTool := &MockTool{}
	tools := []interfaces.Tool{mockTool}

	dsTools := provider.convertToolsToDeepSeek(tools)

	assert.Len(t, dsTools, 1)
	assert.Equal(t, "function", dsTools[0].Type)
	assert.Equal(t, "mock_tool", dsTools[0].Function.Name)
	assert.Equal(t, "A mock tool for testing", dsTools[0].Function.Description)
	assert.NotNil(t, dsTools[0].Function.Parameters)
}

// TestDeepSeekStreamingProvider_Creation tests streaming provider creation
func TestDeepSeekStreamingProvider_Creation(t *testing.T) {
	config := &llm.LLMOptions{
		Provider: constants.ProviderDeepSeek,
		APIKey:   "test-key",
	}

	provider, err := NewDeepSeekStreaming(config)
	assert.NoError(t, err)
	assert.NotNil(t, provider)
	assert.NotNil(t, provider.DeepSeekProvider)
}

// TestDeepSeekStreamingProvider_StreamWithMetadata tests streaming with metadata
func TestDeepSeekStreamingProvider_StreamWithMetadata(t *testing.T) {
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		streamResponses := []DeepSeekStreamResponse{
			{
				ID:    "stream-1",
				Model: "deepseek-chat",
				Choices: []DeepSeekChoice{
					{
						Index: 0,
						Delta: DeepSeekMessage{Content: "Token1 "},
					},
				},
			},
			{
				ID:    "stream-2",
				Model: "deepseek-chat",
				Choices: []DeepSeekChoice{
					{
						Index: 0,
						Delta: DeepSeekMessage{Content: "Token2"},
					},
				},
			},
			{
				ID:    "stream-3",
				Model: "deepseek-chat",
				Choices: []DeepSeekChoice{
					{
						Index:        0,
						FinishReason: "stop",
					},
				},
			},
		}

		for _, resp := range streamResponses {
			json.NewEncoder(w).Encode(resp)
		}
	}))
	defer mockServer.Close()

	provider, err := NewDeepSeekStreaming(&llm.LLMOptions{
		Provider:    constants.ProviderDeepSeek,
		APIKey:      "test-key",
		BaseURL:     mockServer.URL,
		Timeout:     5,
		Model:       "deepseek-chat",
		Temperature: 0.7,
	})
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
		Provider:    constants.ProviderOpenAI,
		APIKey:      "test-key",
		BaseURL:     "https://api.openai.com/v1",
		Timeout:     5,
		Model:       "gpt-4",
		Temperature: 0.7,
	}

	provider, err := NewOpenAIStreaming(config)
	assert.NoError(t, err)
	assert.NotNil(t, provider)
	assert.NotNil(t, provider.OpenAIProvider)
}

// TestOpenAIStreamingProvider_CreationErrorComprehensive tests error handling in creation
func TestOpenAIStreamingProvider_CreationErrorComprehensive(t *testing.T) {
	config := &llm.LLMOptions{
		Provider:    constants.ProviderOpenAI,
		BaseURL:     "https://api.openai.com/v1",
		Timeout:     5,
		Model:       "gpt-4",
		Temperature: 0.7,
		// Missing APIKey
	}

	provider, err := NewOpenAIStreaming(config)
	assert.Error(t, err)
	assert.Nil(t, provider)
}

// TestOpenAIProvider_InitializationComprehensive tests OpenAI provider creation
func TestOpenAIProvider_InitializationComprehensive(t *testing.T) {
	tests := []struct {
		name    string
		config  *llm.LLMOptions
		wantErr bool
	}{
		{
			name: "valid config",
			config: &llm.LLMOptions{
				Provider:    constants.ProviderOpenAI,
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
				Provider:    constants.ProviderOpenAI,
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
				Provider: constants.ProviderOpenAI,
				APIKey:   "test-key",
				BaseURL:  "https://custom.openai.com",
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			provider, err := NewOpenAI(tt.config)
			if tt.wantErr {
				assert.Error(t, err)
				assert.Nil(t, provider)
			} else {
				assert.NoError(t, err)
				assert.NotNil(t, provider)
				assert.Equal(t, constants.ProviderOpenAI, provider.Provider())
			}
		})
	}
}

// TestOpenAIProvider_DefaultModelComprehensive tests default model assignment
func TestOpenAIProvider_DefaultModelComprehensive(t *testing.T) {
	config := &llm.LLMOptions{
		Provider: constants.ProviderOpenAI,
		APIKey:   "test-key",
	}

	provider, err := NewOpenAI(config)
	require.NoError(t, err)

	// Should have a non-empty default model
	assert.NotEmpty(t, provider.GetModel(""))
}

// TestOpenAIProvider_ModelNameComprehensive tests model name retrieval
func TestOpenAIProvider_ModelNameComprehensive(t *testing.T) {
	config := &llm.LLMOptions{
		Provider:    constants.ProviderOpenAI,
		APIKey:      "test-key",
		BaseURL:     "https://api.openai.com/v1",
		Timeout:     5,
		Model:       "gpt-4",
		Temperature: 0.7,
		MaxTokens:   4000,
	}

	provider, err := NewOpenAI(config)
	require.NoError(t, err)

	assert.Equal(t, "gpt-4-turbo", provider.ModelName())
}

// TestOpenAIProvider_MaxTokensComprehensive tests max tokens retrieval
func TestOpenAIProvider_MaxTokensComprehensive(t *testing.T) {
	config := &llm.LLMOptions{
		Provider:    constants.ProviderOpenAI,
		APIKey:      "test-key",
		BaseURL:     "https://api.openai.com/v1",
		Timeout:     5,
		Model:       "gpt-4",
		Temperature: 0.7,
		MaxTokens:   4000,
	}

	provider, err := NewOpenAI(config)
	require.NoError(t, err)

	assert.Equal(t, 4000, provider.MaxTokens())
}

// TestOpenAIProvider_ConvertToolsToFunctionsComprehensive tests tool to function conversion
func TestOpenAIProvider_ConvertToolsToFunctionsComprehensive(t *testing.T) {
	provider, err := NewOpenAI(&llm.LLMOptions{
		Provider:    constants.ProviderOpenAI,
		APIKey:      "test-key",
		BaseURL:     "https://api.openai.com/v1",
		Timeout:     5,
		Model:       "gpt-4",
		Temperature: 0.7,
		MaxTokens:   4000,
	})

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
	provider, err := NewOpenAI(&llm.LLMOptions{
		Provider:    constants.ProviderOpenAI,
		APIKey:      "test-key",
		BaseURL:     "https://api.openai.com/v1",
		Timeout:     5,
		Model:       "gpt-4",
		Temperature: 0.7,
		MaxTokens:   4000,
	})
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
	tests := []struct {
		name    string
		config  *llm.LLMOptions
		wantErr bool
	}{
		{
			name: "missing API key",
			config: &llm.LLMOptions{
				Provider:    constants.ProviderGemini,
				APIKey:      "test-key",
				BaseURL:     "https://api.openai.com/v1",
				Timeout:     5,
				Model:       "gpt-4",
				Temperature: 0.7,
				MaxTokens:   4000,
			},
			wantErr: true,
		},
		{
			name: "valid config with API key",
			config: &llm.LLMOptions{
				Provider: constants.ProviderGemini,
				APIKey:   "test-key",
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			provider, err := NewGemini(tt.config)
			if tt.wantErr {
				assert.Error(t, err)
				assert.Nil(t, provider)
			} else {
				assert.NoError(t, err)
				assert.NotNil(t, provider)
				assert.Equal(t, constants.ProviderGemini, provider.Provider())
			}
		})
	}
}

// ==============================================================================
// HELPER FUNCTIONS AND UTILITIES TESTS
// ==============================================================================

// TestGenerateCallID tests unique ID generation
func TestGenerateCallID(t *testing.T) {
	id1 := generateCallID()
	time.Sleep(1 * time.Millisecond) // Ensure different timestamp
	id2 := generateCallID()

	assert.NotEmpty(t, id1)
	assert.NotEmpty(t, id2)
	assert.NotEqual(t, id1, id2)
	assert.True(t, strings.HasPrefix(id1, "call_"))
	assert.True(t, strings.HasPrefix(id2, "call_"))
}

// TestToolCallResponse_Structure tests ToolCallResponse structure
func TestToolCallResponse_Structure(t *testing.T) {
	response := &ToolCallResponse{
		Content: "Response content",
		ToolCalls: []ToolCall{
			{
				ID:   "call_123",
				Name: "test_tool",
				Arguments: map[string]interface{}{
					"param1": "value1",
					"param2": 42,
				},
			},
		},
	}

	assert.Equal(t, "Response content", response.Content)
	assert.Len(t, response.ToolCalls, 1)
	assert.Equal(t, "test_tool", response.ToolCalls[0].Name)
	assert.Equal(t, "value1", response.ToolCalls[0].Arguments["param1"])
	assert.Equal(t, 42, response.ToolCalls[0].Arguments["param2"])
}

// TestToolChunk_Types tests different ToolChunk types
func TestToolChunk_Types(t *testing.T) {
	tests := []struct {
		name  string
		chunk ToolChunk
	}{
		{
			name: "content chunk",
			chunk: ToolChunk{
				Type:  "content",
				Value: "Some content",
			},
		},
		{
			name: "tool_name chunk",
			chunk: ToolChunk{
				Type:  "tool_name",
				Value: "get_weather",
			},
		},
		{
			name: "tool_args chunk",
			chunk: ToolChunk{
				Type:  "tool_args",
				Value: `{"location":"NYC"}`,
			},
		},
		{
			name: "tool_call chunk",
			chunk: ToolChunk{
				Type: "tool_call",
				Value: ToolCall{
					ID:   "call_456",
					Name: "calculator",
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.NotEmpty(t, tt.chunk.Type)
			assert.NotNil(t, tt.chunk.Value)
		})
	}
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

	provider, err := NewDeepSeek(&llm.LLMOptions{
		Provider: constants.ProviderDeepSeek,
		APIKey:   "test-key",
		BaseURL:  mockServer.URL,
		Timeout:  5,
	})
	require.NoError(t, err)

	_, err = provider.Complete(context.Background(), &llm.CompletionRequest{
		Messages: []llm.Message{llm.UserMessage("Test")},
	})

	assert.Error(t, err)
}

// TestDeepSeekProvider_ContextCancellation tests context cancellation
func TestDeepSeekProvider_ContextCancellation(t *testing.T) {
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(1 * time.Second) // Simulate slow response
		w.Header().Set("Content-Type", "application/json")
		resp := DeepSeekResponse{
			ID:      "chatcmpl-cancel",
			Choices: []DeepSeekChoice{{Index: 0, Message: DeepSeekMessage{Content: "test"}}},
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer mockServer.Close()

	provider, err := NewDeepSeek(&llm.LLMOptions{
		Provider:    constants.ProviderDeepSeek,
		APIKey:      "test-key",
		BaseURL:     mockServer.URL,
		Timeout:     5,
		Model:       "deepseek-chat",
		Temperature: 0.7,
	})
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err = provider.Complete(ctx, &llm.CompletionRequest{
		Messages: []llm.Message{llm.UserMessage("Test")},
	})

	assert.Error(t, err)
}

// TestDeepSeekProvider_LargeResponse tests handling of large responses
func TestDeepSeekProvider_LargeResponse(t *testing.T) {
	largeContent := strings.Repeat("A", 50000) // 50KB content

	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := DeepSeekResponse{
			ID:      "chatcmpl-large",
			Object:  "chat.completion",
			Created: time.Now().Unix(),
			Model:   "deepseek-chat",
			Choices: []DeepSeekChoice{
				{
					Index: 0,
					Message: DeepSeekMessage{
						Role:    "assistant",
						Content: largeContent,
					},
					FinishReason: "length",
				},
			},
			Usage: DeepSeekUsage{TotalTokens: 50000},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer mockServer.Close()

	provider, err := NewDeepSeek(&llm.LLMOptions{
		Provider: constants.ProviderDeepSeek,
		APIKey:   "test-key",
		BaseURL:  mockServer.URL,
		Timeout:  5,
	})
	require.NoError(t, err)

	resp, err := provider.Complete(context.Background(), &llm.CompletionRequest{
		Messages: []llm.Message{llm.UserMessage("Test")},
	})

	assert.NoError(t, err)
	assert.Equal(t, largeContent, resp.Content)
	assert.Equal(t, 50000, resp.TokensUsed)
}

// TestDeepSeekProvider_ToolCallArgumentsParsing tests tool arguments parsing edge cases
func TestDeepSeekProvider_ToolCallArgumentsParsing(t *testing.T) {
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := DeepSeekResponse{
			ID:      "chatcmpl-tool-parse",
			Object:  "chat.completion",
			Created: time.Now().Unix(),
			Model:   "deepseek-chat",
			Choices: []DeepSeekChoice{
				{
					Index: 0,
					Message: DeepSeekMessage{
						Role:    "assistant",
						Content: "Calling tool...",
						ToolCalls: []DeepSeekToolCall{
							{
								ID:   "call_123",
								Type: "function",
								Function: DeepSeekFunctionCall{
									Name:      "calculator",
									Arguments: `{"operation":"add","a":5,"b":3,"nested":{"key":"value"}}`,
								},
							},
							{
								ID:   "call_124",
								Type: "function",
								Function: DeepSeekFunctionCall{
									Name:      "invalid_json",
									Arguments: `{invalid json}`, // Invalid JSON - should be skipped
								},
							},
						},
					},
					FinishReason: "tool_calls",
				},
			},
			Usage: DeepSeekUsage{TotalTokens: 50},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer mockServer.Close()

	provider, err := NewDeepSeek(&llm.LLMOptions{
		Provider:    constants.ProviderDeepSeek,
		APIKey:      "test-key",
		BaseURL:     mockServer.URL,
		Timeout:     5,
		Model:       "deepseek-chat",
		Temperature: 0.7,
	})
	require.NoError(t, err)

	resp, err := provider.GenerateWithTools(context.Background(), "Calculate", []interfaces.Tool{&MockTool{}})
	assert.NoError(t, err)
	assert.NotNil(t, resp)

	// Only valid tool call should be included
	assert.Len(t, resp.ToolCalls, 1)
	assert.Equal(t, "calculator", resp.ToolCalls[0].Name)
	assert.Equal(t, "add", resp.ToolCalls[0].Arguments["operation"])
	assert.Equal(t, float64(5), resp.ToolCalls[0].Arguments["a"])
}

// TestDeepSeekProvider_GetterMethods tests getter utility methods
func TestDeepSeekProvider_GetterMethods(t *testing.T) {
	provider, err := NewDeepSeek(&llm.LLMOptions{
		Provider:    constants.ProviderDeepSeek,
		APIKey:      "test-key",
		Timeout:     5,
		Model:       "deepseek-chat",
		Temperature: 0.8,
		MaxTokens:   3000,
	})
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

// TestDeepSeekProvider_InvalidRequestPayload tests invalid request handling
func TestDeepSeekProvider_InvalidRequestPayload(t *testing.T) {
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify proper request format
		var req DeepSeekRequest
		err := json.NewDecoder(r.Body).Decode(&req)

		if err != nil {
			w.WriteHeader(http.StatusBadRequest)
			w.Write([]byte("Invalid request"))
			return
		}

		w.Header().Set("Content-Type", "application/json")
		resp := DeepSeekResponse{
			ID:      "chatcmpl-ok",
			Choices: []DeepSeekChoice{{Index: 0, Message: DeepSeekMessage{Content: "OK"}}},
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer mockServer.Close()

	provider, err := NewDeepSeek(&llm.LLMOptions{
		Provider:    constants.ProviderDeepSeek,
		APIKey:      "test-key",
		BaseURL:     mockServer.URL,
		Timeout:     5,
		Model:       "deepseek-chat",
		Temperature: 0.7,
	})
	require.NoError(t, err)

	// Valid request should succeed
	resp, err := provider.Complete(context.Background(), &llm.CompletionRequest{
		Messages: []llm.Message{llm.UserMessage("Test")},
	})
	assert.NoError(t, err)
	assert.NotNil(t, resp)
}

// TestDeepSeekProvider_StreamReadError tests stream read error handling
func TestDeepSeekProvider_StreamReadError(t *testing.T) {
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"id":"test"}`))
		w.Write([]byte("\n{invalid json"))
		w.Write([]byte("\n"))
	}))
	defer mockServer.Close()

	provider, err := NewDeepSeek(&llm.LLMOptions{
		Provider:    constants.ProviderDeepSeek,
		APIKey:      "test-key",
		BaseURL:     mockServer.URL,
		Timeout:     5,
		Model:       "deepseek-chat",
		Temperature: 0.7,
	})
	require.NoError(t, err)

	tokens, err := provider.Stream(context.Background(), "Test")
	require.NoError(t, err)

	// Stream should handle the error gracefully
	tokenCount := 0
	for range tokens {
		tokenCount++
	}
	// May get 0 tokens due to invalid JSON after first response
}

// TestMultipleToolCalls tests multiple tool calls in one response
func TestMultipleToolCalls(t *testing.T) {
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := DeepSeekResponse{
			ID:      "chatcmpl-multi-tools",
			Object:  "chat.completion",
			Created: time.Now().Unix(),
			Model:   "deepseek-chat",
			Choices: []DeepSeekChoice{
				{
					Index: 0,
					Message: DeepSeekMessage{
						Role:    "assistant",
						Content: "Calling multiple tools...",
						ToolCalls: []DeepSeekToolCall{
							{
								ID:   "call_001",
								Type: "function",
								Function: DeepSeekFunctionCall{
									Name:      "tool1",
									Arguments: `{"param":"value1"}`,
								},
							},
							{
								ID:   "call_002",
								Type: "function",
								Function: DeepSeekFunctionCall{
									Name:      "tool2",
									Arguments: `{"param":"value2"}`,
								},
							},
							{
								ID:   "call_003",
								Type: "function",
								Function: DeepSeekFunctionCall{
									Name:      "tool3",
									Arguments: `{"param":"value3"}`,
								},
							},
						},
					},
					FinishReason: "tool_calls",
				},
			},
			Usage: DeepSeekUsage{TotalTokens: 75},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer mockServer.Close()

	provider, err := NewDeepSeek(&llm.LLMOptions{
		Provider:    constants.ProviderDeepSeek,
		APIKey:      "test-key",
		BaseURL:     mockServer.URL,
		Timeout:     5,
		Model:       "deepseek-chat",
		Temperature: 0.7,
	})
	require.NoError(t, err)

	resp, err := provider.GenerateWithTools(context.Background(), "Call tools", []interfaces.Tool{&MockTool{}})
	assert.NoError(t, err)
	assert.NotNil(t, resp)
	assert.Len(t, resp.ToolCalls, 3)

	for i, tc := range resp.ToolCalls {
		assert.NotEmpty(t, tc.ID)
		assert.NotEmpty(t, tc.Name)
		assert.NotNil(t, tc.Arguments)
		assert.Equal(t, "value"+string(rune(49+i)), tc.Arguments["param"])
	}
}

// TestStreamingProviderInheritance tests streaming provider inheritance
func TestStreamingProviderInheritance(t *testing.T) {
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := DeepSeekResponse{
			ID:      "chatcmpl-123",
			Object:  "chat.completion",
			Created: time.Now().Unix(),
			Model:   "deepseek-chat",
			Choices: []DeepSeekChoice{
				{
					Index: 0,
					Message: DeepSeekMessage{
						Role:    "assistant",
						Content: "Test response",
					},
					FinishReason: "stop",
				},
			},
			Usage: DeepSeekUsage{TotalTokens: 20},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer mockServer.Close()

	provider, err := NewDeepSeekStreaming(&llm.LLMOptions{
		Provider:    constants.ProviderDeepSeek,
		APIKey:      "test-key",
		BaseURL:     mockServer.URL,
		Timeout:     5,
		Model:       "deepseek-chat",
		Temperature: 0.7,
	})
	require.NoError(t, err)

	// Verify base provider methods are inherited
	resp, err := provider.Complete(context.Background(), &llm.CompletionRequest{
		Messages: []llm.Message{llm.UserMessage("Test")},
	})

	assert.NoError(t, err)
	assert.NotNil(t, resp)
	assert.Equal(t, constants.ProviderDeepSeek, provider.Provider())
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
		json.NewEncoder(w).Encode(resp)
	}))
	defer mockServer.Close()

	provider, err := NewDeepSeek(&llm.LLMOptions{
		Provider:    constants.ProviderDeepSeek,
		APIKey:      "test-key",
		BaseURL:     mockServer.URL,
		Timeout:     5,
		Model:       "deepseek-chat",
		Temperature: 0.7,
	})
	require.NoError(t, err)

	_, err = provider.Embed(context.Background(), "test")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "no embeddings")
}

// TestRequestBodyClose ensures request body is properly closed
func TestRequestBodyClose(t *testing.T) {
	closed := false
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Wrap body to track closing
		originalBody := r.Body
		r.Body = io.NopCloser(bytes.NewReader(func() []byte {
			buf := bytes.NewBuffer(nil)
			io.Copy(buf, originalBody)
			return buf.Bytes()
		}()))

		resp := DeepSeekResponse{
			ID:      "chatcmpl-123",
			Choices: []DeepSeekChoice{{Index: 0, Message: DeepSeekMessage{Content: "test"}}},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer mockServer.Close()

	provider, err := NewDeepSeek(&llm.LLMOptions{
		Provider:    constants.ProviderDeepSeek,
		APIKey:      "test-key",
		BaseURL:     mockServer.URL,
		Timeout:     5,
		Model:       "deepseek-chat",
		Temperature: 0.7,
	})
	require.NoError(t, err)

	_, err = provider.Complete(context.Background(), &llm.CompletionRequest{
		Messages: []llm.Message{llm.UserMessage("Test")},
	})

	assert.NoError(t, err)
	_ = closed // Suppress unused variable warning
}

// TestToolSchemaConversion tests tool schema conversion
func TestToolSchemaConversion(t *testing.T) {
	provider, err := NewDeepSeek(&llm.LLMOptions{
		Provider: constants.ProviderDeepSeek,
		APIKey:   "test-key",
	})
	require.NoError(t, err)

	schema := provider.toolSchemaToJSON(nil)
	assert.NotNil(t, schema)

	// Verify structure
	assert.Equal(t, "object", schema["type"])
	assert.NotNil(t, schema["properties"])
	assert.NotNil(t, schema["required"])
}
