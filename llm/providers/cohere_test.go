package providers

import (
	"context"
	"fmt"
	"github.com/kart-io/goagent/utils/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	agentErrors "github.com/kart-io/goagent/errors"
	"github.com/kart-io/goagent/llm"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestNewCohere tests the constructor
func TestNewCohere(t *testing.T) {
	tests := []struct {
		name        string
		config      *llm.Config
		envAPIKey   string
		envBaseURL  string
		envModel    string
		wantErr     bool
		errCode     agentErrors.ErrorCode
		checkResult func(*testing.T, *CohereProvider)
	}{
		{
			name: "valid config with all parameters",
			config: &llm.Config{
				APIKey:      "test-key",
				BaseURL:     "https://custom.cohere.com",
				Model:       "command-r",
				MaxTokens:   4000,
				Temperature: 0.8,
				Timeout:     120,
			},
			wantErr: false,
			checkResult: func(t *testing.T, p *CohereProvider) {
				assert.Equal(t, "test-key", p.apiKey)
				assert.Equal(t, "https://custom.cohere.com", p.baseURL)
				assert.Equal(t, "command-r", p.model)
				assert.Equal(t, 4000, p.maxTokens)
				assert.Equal(t, 0.8, p.temperature)
			},
		},
		{
			name: "minimal config with defaults",
			config: &llm.Config{
				APIKey: "test-key",
			},
			wantErr: false,
			checkResult: func(t *testing.T, p *CohereProvider) {
				assert.Equal(t, "test-key", p.apiKey)
				assert.Equal(t, "https://api.cohere.ai", p.baseURL)
				assert.Equal(t, "command-r-plus", p.model)
				assert.Equal(t, 1000, p.maxTokens)
				assert.Equal(t, 0.7, p.temperature)
			},
		},
		{
			name: "config with env var API key",
			config: &llm.Config{
				Model: "command-light",
			},
			envAPIKey: "env-api-key",
			wantErr:   false,
			checkResult: func(t *testing.T, p *CohereProvider) {
				assert.Equal(t, "env-api-key", p.apiKey)
				assert.Equal(t, "command-light", p.model)
			},
		},
		{
			name: "config with env var base URL",
			config: &llm.Config{
				APIKey: "test-key",
			},
			envBaseURL: "https://env.cohere.com",
			wantErr:    false,
			checkResult: func(t *testing.T, p *CohereProvider) {
				assert.Equal(t, "https://env.cohere.com", p.baseURL)
			},
		},
		{
			name: "config with env var model",
			config: &llm.Config{
				APIKey: "test-key",
			},
			envModel: "command-r-plus",
			wantErr:  false,
			checkResult: func(t *testing.T, p *CohereProvider) {
				assert.Equal(t, "command-r-plus", p.model)
			},
		},
		{
			name:    "missing API key",
			config:  &llm.Config{},
			wantErr: true,
			errCode: agentErrors.CodeInvalidConfig,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Clean up any existing env vars first
			os.Unsetenv("COHERE_API_KEY")
			os.Unsetenv("COHERE_BASE_URL")
			os.Unsetenv("COHERE_MODEL")

			// Set env vars if provided
			if tt.envAPIKey != "" {
				os.Setenv("COHERE_API_KEY", tt.envAPIKey)
				defer os.Unsetenv("COHERE_API_KEY")
			}
			if tt.envBaseURL != "" {
				os.Setenv("COHERE_BASE_URL", tt.envBaseURL)
				defer os.Unsetenv("COHERE_BASE_URL")
			}
			if tt.envModel != "" {
				os.Setenv("COHERE_MODEL", tt.envModel)
				defer os.Unsetenv("COHERE_MODEL")
			}

			provider, err := NewCohere(tt.config)

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

// TestCohereComplete tests the Complete method
func TestCohereComplete(t *testing.T) {
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
			mockResponse: CohereResponse{
				ResponseID:   "resp_123",
				Text:         "Hello! How can I help you today?",
				GenerationID: "gen_456",
				FinishReason: "COMPLETE",
				TokenCount: CohereTokens{
					PromptTokens:   10,
					ResponseTokens: 15,
					TotalTokens:    25,
					BilledTokens:   25,
				},
			},
			mockStatusCode: http.StatusOK,
			wantErr:        false,
			checkResult: func(t *testing.T, resp *llm.CompletionResponse) {
				assert.Equal(t, "Hello! How can I help you today?", resp.Content)
				assert.Equal(t, 25, resp.TokensUsed)
				assert.Equal(t, "COMPLETE", resp.FinishReason)
				assert.Equal(t, "cohere", resp.Provider)
				require.NotNil(t, resp.Usage)
				assert.Equal(t, 10, resp.Usage.PromptTokens)
				assert.Equal(t, 15, resp.Usage.CompletionTokens)
				assert.Equal(t, 25, resp.Usage.TotalTokens)
			},
		},
		{
			name: "completion with chat history",
			request: &llm.CompletionRequest{
				Messages: []llm.Message{
					{Role: "user", Content: "Hi"},
					{Role: "assistant", Content: "Hello!"},
					{Role: "user", Content: "How are you?"},
				},
			},
			mockResponse: CohereResponse{
				ResponseID:   "resp_789",
				Text:         "I'm doing well, thank you!",
				GenerationID: "gen_012",
				FinishReason: "COMPLETE",
				TokenCount: CohereTokens{
					PromptTokens:   20,
					ResponseTokens: 10,
					TotalTokens:    30,
				},
			},
			mockStatusCode: http.StatusOK,
			wantErr:        false,
		},
		{
			name: "completion with custom parameters",
			request: &llm.CompletionRequest{
				Messages:    []llm.Message{{Role: "user", Content: "Test"}},
				Model:       "command-r",
				MaxTokens:   1000,
				Temperature: 0.5,
				TopP:        0.9,
				Stop:        []string{"END"},
			},
			mockResponse: CohereResponse{
				ResponseID:   "resp_custom",
				Text:         "Response",
				GenerationID: "gen_custom",
				FinishReason: "COMPLETE",
				TokenCount: CohereTokens{
					PromptTokens:   5,
					ResponseTokens: 10,
					TotalTokens:    15,
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
				// Verify headers (Bearer token, not x-api-key)
				assert.Equal(t, "application/json", r.Header.Get("Content-Type"))
				assert.Contains(t, r.Header.Get("Authorization"), "Bearer")

				// Verify request body
				var req CohereRequest
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
			provider, err := NewCohere(&llm.Config{
				APIKey:  "test-key",
				BaseURL: server.URL,
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

// TestCohereChat tests the Chat method
func TestCohereChat(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(CohereResponse{
			ResponseID:   "resp_chat",
			Text:         "Chat response",
			GenerationID: "gen_chat",
			FinishReason: "COMPLETE",
			TokenCount: CohereTokens{
				PromptTokens:   10,
				ResponseTokens: 5,
				TotalTokens:    15,
			},
		})
	}))
	defer server.Close()

	provider, err := NewCohere(&llm.Config{
		APIKey:  "test-key",
		BaseURL: server.URL,
	})
	require.NoError(t, err)

	resp, err := provider.Chat(context.Background(), []llm.Message{
		{Role: "user", Content: "Hello"},
	})

	require.NoError(t, err)
	assert.Equal(t, "Chat response", resp.Content)
}

// TestCohereErrorHandling tests error scenarios
func TestCohereErrorHandling(t *testing.T) {
	tests := []struct {
		name            string
		statusCode      int
		responseBody    interface{}
		expectedErrCode agentErrors.ErrorCode
	}{
		{
			name:       "400 bad request",
			statusCode: 400,
			responseBody: CohereErrorResponse{
				Message: "Invalid request parameters",
			},
			expectedErrCode: agentErrors.CodeInvalidInput,
		},
		{
			name:       "401 unauthorized",
			statusCode: 401,
			responseBody: CohereErrorResponse{
				Message: "Invalid API key",
			},
			expectedErrCode: agentErrors.CodeInvalidConfig,
		},
		{
			name:       "403 forbidden",
			statusCode: 403,
			responseBody: CohereErrorResponse{
				Message: "Access forbidden",
			},
			expectedErrCode: agentErrors.CodeInvalidConfig,
		},
		{
			name:       "404 not found",
			statusCode: 404,
			responseBody: CohereErrorResponse{
				Message: "Endpoint not found",
			},
			expectedErrCode: agentErrors.CodeLLMResponse,
		},
		{
			name:            "429 rate limit",
			statusCode:      429,
			expectedErrCode: agentErrors.CodeLLMRateLimit,
		},
		{
			name:            "500 server error",
			statusCode:      500,
			expectedErrCode: agentErrors.CodeLLMRequest,
		},
		{
			name:            "503 service unavailable",
			statusCode:      503,
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

			provider, err := NewCohere(&llm.Config{
				APIKey:  "test-key",
				BaseURL: server.URL,
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

// TestCohereRetry tests retry logic
func TestCohereRetry(t *testing.T) {
	attempts := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		if attempts < 3 {
			// Return 503 for first 2 attempts
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		// Succeed on 3rd attempt
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(CohereResponse{
			ResponseID:   "resp_retry",
			Text:         "Success after retry",
			GenerationID: "gen_retry",
			FinishReason: "COMPLETE",
			TokenCount: CohereTokens{
				PromptTokens:   5,
				ResponseTokens: 10,
				TotalTokens:    15,
			},
		})
	}))
	defer server.Close()

	provider, err := NewCohere(&llm.Config{
		APIKey:  "test-key",
		BaseURL: server.URL,
	})
	require.NoError(t, err)

	resp, err := provider.Complete(context.Background(), &llm.CompletionRequest{
		Messages: []llm.Message{{Role: "user", Content: "test"}},
	})

	require.NoError(t, err)
	assert.Equal(t, "Success after retry", resp.Content)
	assert.Equal(t, 3, attempts, "Should have made 3 attempts")
}

// TestCohereRetryExhausted tests when retries are exhausted
func TestCohereRetryExhausted(t *testing.T) {
	attempts := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer server.Close()

	provider, err := NewCohere(&llm.Config{
		APIKey:  "test-key",
		BaseURL: server.URL,
	})
	require.NoError(t, err)

	_, err = provider.Complete(context.Background(), &llm.CompletionRequest{
		Messages: []llm.Message{{Role: "user", Content: "test"}},
	})

	require.Error(t, err)
	assert.Equal(t, 3, attempts, "Should have made 3 attempts")
}

// TestCohereContextCancellation tests context cancellation
func TestCohereContextCancellation(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Delay to allow context cancellation
		time.Sleep(100 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	provider, err := NewCohere(&llm.Config{
		APIKey:  "test-key",
		BaseURL: server.URL,
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

// TestCohereStream tests streaming
func TestCohereStream(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)

		flusher := w.(http.Flusher)

		// Send stream events in Cohere format
		events := []CohereStreamEvent{
			{EventType: "stream-start"},
			{EventType: "text-generation", Text: "Hello"},
			{EventType: "text-generation", Text: " world"},
			{EventType: "text-generation", Text: "!"},
			{EventType: "stream-end", FinishReason: "COMPLETE"},
		}

		for _, event := range events {
			data, _ := json.Marshal(event)
			fmt.Fprintf(w, "%s\n", string(data))
			flusher.Flush()
			time.Sleep(10 * time.Millisecond)
		}
	}))
	defer server.Close()

	provider, err := NewCohere(&llm.Config{
		APIKey:  "test-key",
		BaseURL: server.URL,
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

// TestCohereStreamError tests streaming with error
func TestCohereStreamError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer server.Close()

	provider, err := NewCohere(&llm.Config{
		APIKey:  "test-key",
		BaseURL: server.URL,
	})
	require.NoError(t, err)

	_, err = provider.Stream(context.Background(), "test")
	require.Error(t, err)
	assert.True(t, agentErrors.IsCode(err, agentErrors.CodeInvalidConfig))
}

// TestCohereProvider tests Provider method
func TestCohereProvider(t *testing.T) {
	provider, err := NewCohere(&llm.Config{
		APIKey: "test-key",
	})
	require.NoError(t, err)

	assert.Equal(t, llm.ProviderCohere, provider.Provider())
}

// TestCohereModelName tests ModelName method
func TestCohereModelName(t *testing.T) {
	provider, err := NewCohere(&llm.Config{
		APIKey: "test-key",
		Model:  "command-r-plus",
	})
	require.NoError(t, err)

	assert.Equal(t, "command-r-plus", provider.ModelName())
}

// TestCohereMaxTokens tests MaxTokens method
func TestCohereMaxTokens(t *testing.T) {
	provider, err := NewCohere(&llm.Config{
		APIKey:    "test-key",
		MaxTokens: 4000,
	})
	require.NoError(t, err)

	assert.Equal(t, 4000, provider.MaxTokens())
}

// TestCohereIsAvailable tests IsAvailable method
func TestCohereIsAvailable(t *testing.T) {
	t.Run("available", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
			json.NewEncoder(w).Encode(CohereResponse{
				ResponseID:   "resp_test",
				Text:         "test",
				GenerationID: "gen_test",
				FinishReason: "COMPLETE",
				TokenCount: CohereTokens{
					PromptTokens:   5,
					ResponseTokens: 5,
					TotalTokens:    10,
				},
			})
		}))
		defer server.Close()

		provider, err := NewCohere(&llm.Config{
			APIKey:  "test-key",
			BaseURL: server.URL,
		})
		require.NoError(t, err)

		assert.True(t, provider.IsAvailable())
	})

	t.Run("not available", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusUnauthorized)
		}))
		defer server.Close()

		provider, err := NewCohere(&llm.Config{
			APIKey:  "test-key",
			BaseURL: server.URL,
		})
		require.NoError(t, err)

		assert.False(t, provider.IsAvailable())
	})
}

// TestCohereConvertRole tests role conversion
func TestCohereConvertRole(t *testing.T) {
	provider := &CohereProvider{}

	tests := []struct {
		input    string
		expected string
	}{
		{"user", "USER"},
		{"assistant", "CHATBOT"},
		{"system", "SYSTEM"},
		{"unknown", "USER"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := provider.convertRole(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}
