package providers

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	agentllm "github.com/kart-io/goagent/llm"
	"github.com/kart-io/goagent/llm/constants"
	"github.com/kart-io/goagent/utils/json"

	agentErrors "github.com/kart-io/goagent/errors"
	"github.com/kart-io/goagent/llm"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestNewHuggingFace tests the constructor
func TestNewHuggingFace(t *testing.T) {
	tests := []struct {
		name        string
		config      *agentllm.LLMOptions
		envAPIKey   string
		envBaseURL  string
		envModel    string
		wantErr     bool
		errCode     agentErrors.ErrorCode
		checkResult func(*testing.T, *HuggingFaceProvider)
	}{
		{
			name: "valid config with all parameters",
			config: &agentllm.LLMOptions{
				APIKey:      "test-key",
				BaseURL:     "https://custom.hf.co",
				Model:       "mistralai/Mixtral-8x7B-Instruct-v0.1",
				MaxTokens:   4000,
				Temperature: 0.8,
				Timeout:     180,
			},
			wantErr: false,
			checkResult: func(t *testing.T, p *HuggingFaceProvider) {
				assert.Equal(t, "test-key", p.apiKey)
				assert.Equal(t, "https://custom.hf.co", p.baseURL)
				assert.Equal(t, "mistralai/Mixtral-8x7B-Instruct-v0.1", p.model)
				assert.Equal(t, 4000, p.maxTokens)
				assert.Equal(t, 0.8, p.temperature)
			},
		},
		{
			name: "minimal config with defaults",
			config: &agentllm.LLMOptions{
				APIKey: "test-key",
			},
			wantErr: false,
			checkResult: func(t *testing.T, p *HuggingFaceProvider) {
				assert.Equal(t, "test-key", p.apiKey)
				assert.Equal(t, "https://api-inference.huggingface.co", p.baseURL)
				assert.Equal(t, "meta-llama/Meta-Llama-3-8B-Instruct", p.model)
				assert.Equal(t, 2000, p.maxTokens)
				assert.Equal(t, 0.7, p.temperature)
			},
		},
		{
			name: "config with env var API key",
			config: &agentllm.LLMOptions{
				Model: "google/flan-t5-xxl",
			},
			envAPIKey: "env-api-key",
			wantErr:   false,
			checkResult: func(t *testing.T, p *HuggingFaceProvider) {
				assert.Equal(t, "env-api-key", p.apiKey)
				assert.Equal(t, "google/flan-t5-xxl", p.model)
			},
		},
		{
			name: "config with env var base URL",
			config: &agentllm.LLMOptions{
				APIKey: "test-key",
			},
			envBaseURL: "https://env.hf.co",
			wantErr:    false,
			checkResult: func(t *testing.T, p *HuggingFaceProvider) {
				assert.Equal(t, "https://env.hf.co", p.baseURL)
			},
		},
		{
			name: "config with env var model",
			config: &agentllm.LLMOptions{
				APIKey: "test-key",
			},
			envModel: "bigscience/bloom",
			wantErr:  false,
			checkResult: func(t *testing.T, p *HuggingFaceProvider) {
				assert.Equal(t, "bigscience/bloom", p.model)
			},
		},
		{
			name:    "missing API key",
			config:  &agentllm.LLMOptions{},
			wantErr: true,
			errCode: agentErrors.CodeInvalidConfig,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Clean up any existing env vars first
			os.Unsetenv("HUGGINGFACE_API_KEY")
			os.Unsetenv("HUGGINGFACE_BASE_URL")
			os.Unsetenv("HUGGINGFACE_MODEL")

			// Set env vars if provided
			if tt.envAPIKey != "" {
				os.Setenv("HUGGINGFACE_API_KEY", tt.envAPIKey)
				defer os.Unsetenv("HUGGINGFACE_API_KEY")
			}
			if tt.envBaseURL != "" {
				os.Setenv("HUGGINGFACE_BASE_URL", tt.envBaseURL)
				defer os.Unsetenv("HUGGINGFACE_BASE_URL")
			}
			if tt.envModel != "" {
				os.Setenv("HUGGINGFACE_MODEL", tt.envModel)
				defer os.Unsetenv("HUGGINGFACE_MODEL")
			}

			provider, err := NewHuggingFace(tt.config)

			if tt.wantErr {
				require.Error(t, err)
				assert.True(t, agentErrors.IsCode(err, tt.errCode))
			} else {
				require.NoError(t, err)
				require.NotNil(t, provider)
				if tt.checkResult != nil {
					tt.checkResult(t, provider)
				}
			}
		})
	}
}

// TestHuggingFaceComplete tests the Complete method
func TestHuggingFaceComplete(t *testing.T) {
	tests := []struct {
		name           string
		request        *llm.CompletionRequest
		mockResponse   interface{}
		mockStatusCode int
		wantErr        bool
		errCode        agentErrors.ErrorCode
		checkResult    func(*testing.T, *llm.CompletionResponse)
	}{
		{
			name: "successful completion",
			request: &llm.CompletionRequest{
				Messages: []llm.Message{
					{Role: "user", Content: "Hello"},
				},
			},
			mockResponse: []HuggingFaceResponse{
				{
					GeneratedText: "Hello! How can I help you today?",
					Details: &HuggingFaceDetails{
						FinishReason:    "length",
						GeneratedTokens: 15,
					},
				},
			},
			mockStatusCode: http.StatusOK,
			wantErr:        false,
			checkResult: func(t *testing.T, resp *llm.CompletionResponse) {
				assert.Equal(t, "Hello! How can I help you today?", resp.Content)
				assert.Equal(t, "length", resp.FinishReason)
				assert.Equal(t, "huggingface", resp.Provider)
				require.NotNil(t, resp.Usage)
				assert.Equal(t, 15, resp.Usage.CompletionTokens)
			},
		},
		{
			name: "completion with multiple messages",
			request: &llm.CompletionRequest{
				Messages: []llm.Message{
					{Role: "system", Content: "You are helpful"},
					{Role: "user", Content: "Hi"},
					{Role: "assistant", Content: "Hello!"},
					{Role: "user", Content: "How are you?"},
				},
			},
			mockResponse: []HuggingFaceResponse{
				{
					GeneratedText: "I'm doing well, thank you!",
					Details: &HuggingFaceDetails{
						FinishReason:    "eos_token",
						GeneratedTokens: 10,
					},
				},
			},
			mockStatusCode: http.StatusOK,
			wantErr:        false,
		},
		{
			name: "completion with custom parameters",
			request: &llm.CompletionRequest{
				Messages:    []llm.Message{{Role: "user", Content: "Test"}},
				MaxTokens:   1000,
				Temperature: 0.5,
				TopP:        0.9,
				Stop:        []string{"END"},
			},
			mockResponse: []HuggingFaceResponse{
				{
					GeneratedText: "Response",
					Details: &HuggingFaceDetails{
						FinishReason:    "stop_sequence",
						GeneratedTokens: 10,
					},
				},
			},
			mockStatusCode: http.StatusOK,
			wantErr:        false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create mock server
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				// Verify headers (Bearer token)
				assert.Equal(t, "application/json", r.Header.Get("Content-Type"))
				assert.Contains(t, r.Header.Get("Authorization"), "Bearer")

				// Verify URL contains model
				assert.Contains(t, r.URL.Path, "/models/")

				// Verify request body
				var req HuggingFaceRequest
				err := json.NewDecoder(r.Body).Decode(&req)
				require.NoError(t, err)

				// Return mock response
				w.WriteHeader(tt.mockStatusCode)
				if tt.mockResponse != nil {
					json.NewEncoder(w).Encode(tt.mockResponse)
				}
			}))
			defer server.Close()

			// Create provider
			provider, err := NewHuggingFace(&agentllm.LLMOptions{
				APIKey:  "test-key",
				BaseURL: server.URL,
				Model:   "test-model",
			})
			require.NoError(t, err)

			// Test Complete
			resp, err := provider.Complete(context.Background(), tt.request)

			if tt.wantErr {
				require.Error(t, err)
				assert.True(t, agentErrors.IsCode(err, tt.errCode))
			} else {
				require.NoError(t, err)
				require.NotNil(t, resp)
				if tt.checkResult != nil {
					tt.checkResult(t, resp)
				}
			}
		})
	}
}

// TestHuggingFaceChat tests the Chat method
func TestHuggingFaceChat(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode([]HuggingFaceResponse{
			{
				GeneratedText: "Chat response",
				Details: &HuggingFaceDetails{
					FinishReason:    "length",
					GeneratedTokens: 5,
				},
			},
		})
	}))
	defer server.Close()

	provider, err := NewHuggingFace(&agentllm.LLMOptions{
		APIKey:  "test-key",
		BaseURL: server.URL,
		Model:   "test-model",
	})
	require.NoError(t, err)

	resp, err := provider.Chat(context.Background(), []llm.Message{
		{Role: "user", Content: "Hello"},
	})

	require.NoError(t, err)
	assert.Equal(t, "Chat response", resp.Content)
}

// TestHuggingFaceErrorHandling tests error scenarios
func TestHuggingFaceErrorHandling(t *testing.T) {
	tests := []struct {
		name            string
		statusCode      int
		responseBody    interface{}
		expectedErrCode agentErrors.ErrorCode
	}{
		{
			name:       "400 bad request",
			statusCode: 400,
			responseBody: HuggingFaceErrorResponse{
				Error: "Invalid request parameters",
			},
			expectedErrCode: agentErrors.CodeInvalidInput,
		},
		{
			name:       "401 unauthorized",
			statusCode: 401,
			responseBody: HuggingFaceErrorResponse{
				Error: "Invalid API token",
			},
			expectedErrCode: agentErrors.CodeInvalidConfig,
		},
		{
			name:       "403 forbidden",
			statusCode: 403,
			responseBody: HuggingFaceErrorResponse{
				Error: "Access forbidden",
			},
			expectedErrCode: agentErrors.CodeInvalidConfig,
		},
		{
			name:       "404 not found",
			statusCode: 404,
			responseBody: HuggingFaceErrorResponse{
				Error: "Model not found",
			},
			expectedErrCode: agentErrors.CodeLLMResponse,
		},
		{
			name:            "429 rate limit",
			statusCode:      429,
			expectedErrCode: agentErrors.CodeLLMRateLimit,
		},
		{
			name:       "503 model loading",
			statusCode: 503,
			responseBody: HuggingFaceErrorResponse{
				Error:         "Model is loading",
				EstimatedTime: 20.0,
			},
			expectedErrCode: agentErrors.CodeLLMRequest,
		},
		{
			name:            "500 server error",
			statusCode:      500,
			expectedErrCode: agentErrors.CodeLLMRequest,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if tt.statusCode == 429 {
					w.Header().Set("Retry-After", "60")
				}
				w.WriteHeader(tt.statusCode)
				if tt.responseBody != nil {
					json.NewEncoder(w).Encode(tt.responseBody)
				}
			}))
			defer server.Close()

			provider, err := NewHuggingFace(&agentllm.LLMOptions{
				APIKey:  "test-key",
				BaseURL: server.URL,
				Model:   "test-model",
			})
			require.NoError(t, err)

			// Use context with shorter retry delays for tests
			ctx := context.WithValue(context.Background(), "test_retry_delay", 10*time.Millisecond)
			_, err = provider.Complete(ctx, &llm.CompletionRequest{
				Messages: []llm.Message{{Role: "user", Content: "test"}},
			})

			require.Error(t, err)
			assert.True(t, agentErrors.IsCode(err, tt.expectedErrCode),
				"Expected error code %s but got %s", tt.expectedErrCode, agentErrors.GetCode(err))
		})
	}
}

// TestHuggingFaceModelLoading tests model loading retry
func TestHuggingFaceModelLoading(t *testing.T) {
	attempts := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		if attempts < 3 {
			// Return 503 for first 2 attempts (model loading)
			w.WriteHeader(http.StatusServiceUnavailable)
			json.NewEncoder(w).Encode(HuggingFaceErrorResponse{
				Error:         "Model is loading",
				EstimatedTime: 10.0,
			})
			return
		}
		// Succeed on 3rd attempt
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode([]HuggingFaceResponse{
			{
				GeneratedText: "Success after model loaded",
				Details: &HuggingFaceDetails{
					FinishReason:    "length",
					GeneratedTokens: 10,
				},
			},
		})
	}))
	defer server.Close()

	provider, err := NewHuggingFace(&agentllm.LLMOptions{
		APIKey:  "test-key",
		BaseURL: server.URL,
		Model:   "test-model",
	})
	require.NoError(t, err)

	resp, err := provider.Complete(context.Background(), &llm.CompletionRequest{
		Messages: []llm.Message{{Role: "user", Content: "test"}},
	})

	require.NoError(t, err)
	assert.Equal(t, "Success after model loaded", resp.Content)
	assert.Equal(t, 3, attempts, "Should have made 3 attempts")
}

// TestHuggingFaceRetryExhausted tests when retries are exhausted
func TestHuggingFaceRetryExhausted(t *testing.T) {
	attempts := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		w.WriteHeader(http.StatusServiceUnavailable)
		json.NewEncoder(w).Encode(HuggingFaceErrorResponse{
			Error: "Model loading",
		})
	}))
	defer server.Close()

	provider, err := NewHuggingFace(&agentllm.LLMOptions{
		APIKey:  "test-key",
		BaseURL: server.URL,
		Model:   "test-model",
	})
	require.NoError(t, err)

	_, err = provider.Complete(context.Background(), &llm.CompletionRequest{
		Messages: []llm.Message{{Role: "user", Content: "test"}},
	})

	require.Error(t, err)
	assert.Equal(t, 5, attempts, "Should have made 5 attempts (more for HF)")
}

// TestHuggingFaceContextCancellation tests context cancellation
func TestHuggingFaceContextCancellation(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Delay to allow context cancellation
		time.Sleep(100 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	provider, err := NewHuggingFace(&agentllm.LLMOptions{
		APIKey:  "test-key",
		BaseURL: server.URL,
		Model:   "test-model",
	})
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	_, err = provider.Complete(ctx, &llm.CompletionRequest{
		Messages: []llm.Message{{Role: "user", Content: "test"}},
	})

	require.Error(t, err)
	assert.True(t, agentErrors.IsCode(err, agentErrors.CodeLLMRequest) ||
		agentErrors.IsCode(err, agentErrors.CodeContextCanceled))
}

// TestHuggingFaceStream tests streaming
func TestHuggingFaceStream(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)

		flusher := w.(http.Flusher)

		// Send stream events in HuggingFace format
		events := []HuggingFaceStreamResponse{
			{Token: HuggingFaceToken{ID: 1, Text: "Hello", LogProb: -0.1, Special: false}},
			{Token: HuggingFaceToken{ID: 2, Text: " world", LogProb: -0.2, Special: false}},
			{Token: HuggingFaceToken{ID: 3, Text: "!", LogProb: -0.3, Special: false}},
			{
				Token: HuggingFaceToken{ID: 4, Text: "", Special: true},
				Details: &HuggingFaceDetails{
					FinishReason:    "eos_token",
					GeneratedTokens: 3,
				},
			},
		}

		for _, event := range events {
			data, _ := json.Marshal(event)
			fmt.Fprintf(w, "%s\n", string(data))
			flusher.Flush()
			time.Sleep(10 * time.Millisecond)
		}
	}))
	defer server.Close()

	provider, err := NewHuggingFace(&agentllm.LLMOptions{
		APIKey:  "test-key",
		BaseURL: server.URL,
		Model:   "test-model",
	})
	require.NoError(t, err)

	tokens, err := provider.Stream(context.Background(), "test")
	require.NoError(t, err)

	var result []string
	for token := range tokens {
		result = append(result, token)
	}

	assert.Equal(t, []string{"Hello", " world", "!"}, result)
}

// TestHuggingFaceStreamError tests streaming with error
func TestHuggingFaceStreamError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer server.Close()

	provider, err := NewHuggingFace(&agentllm.LLMOptions{
		APIKey:  "test-key",
		BaseURL: server.URL,
		Model:   "test-model",
	})
	require.NoError(t, err)

	_, err = provider.Stream(context.Background(), "test")
	require.Error(t, err)
	assert.True(t, agentErrors.IsCode(err, agentErrors.CodeInvalidConfig))
}

// TestHuggingFaceProvider tests Provider method
func TestHuggingFaceProvider(t *testing.T) {
	provider, err := NewHuggingFace(&agentllm.LLMOptions{
		APIKey: "test-key",
	})
	require.NoError(t, err)

	assert.Equal(t, constants.ProviderHuggingFace, provider.Provider())
}

// TestHuggingFaceModelName tests ModelName method
func TestHuggingFaceModelName(t *testing.T) {
	provider, err := NewHuggingFace(&agentllm.LLMOptions{
		APIKey: "test-key",
		Model:  "bigscience/bloom",
	})
	require.NoError(t, err)

	assert.Equal(t, "bigscience/bloom", provider.ModelName())
}

// TestHuggingFaceMaxTokens tests MaxTokens method
func TestHuggingFaceMaxTokens(t *testing.T) {
	provider, err := NewHuggingFace(&agentllm.LLMOptions{
		APIKey:    "test-key",
		MaxTokens: 4000,
	})
	require.NoError(t, err)

	assert.Equal(t, 4000, provider.MaxTokens())
}

// TestHuggingFaceIsAvailable tests IsAvailable method
func TestHuggingFaceIsAvailable(t *testing.T) {
	t.Run("available", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
			json.NewEncoder(w).Encode([]HuggingFaceResponse{
				{
					GeneratedText: "test",
					Details: &HuggingFaceDetails{
						FinishReason:    "length",
						GeneratedTokens: 1,
					},
				},
			})
		}))
		defer server.Close()

		provider, err := NewHuggingFace(&llm.LLMOptions{
			APIKey:  "test-key",
			BaseURL: server.URL,
			Model:   "test-model",
		})
		require.NoError(t, err)

		assert.True(t, provider.IsAvailable())
	})

	t.Run("not available", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusUnauthorized)
		}))
		defer server.Close()

		provider, err := NewHuggingFace(&llm.LLMOptions{
			APIKey:  "test-key",
			BaseURL: server.URL,
			Model:   "test-model",
		})
		require.NoError(t, err)

		assert.False(t, provider.IsAvailable())
	})
}
