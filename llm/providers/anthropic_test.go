package providers

import (
	"context"
	"encoding/json"
	"fmt"
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

// TestNewAnthropic tests the constructor
func TestNewAnthropic(t *testing.T) {
	tests := []struct {
		name        string
		config      *llm.Config
		envAPIKey   string
		envBaseURL  string
		envModel    string
		wantErr     bool
		errCode     agentErrors.ErrorCode
		checkResult func(*testing.T, *AnthropicProvider)
	}{
		{
			name: "valid config with all parameters",
			config: &llm.Config{
				APIKey:      "test-key",
				BaseURL:     "https://custom.api.com",
				Model:       "claude-3-opus-20240229",
				MaxTokens:   4000,
				Temperature: 0.8,
				Timeout:     120,
			},
			wantErr: false,
			checkResult: func(t *testing.T, p *AnthropicProvider) {
				assert.Equal(t, "test-key", p.apiKey)
				assert.Equal(t, "https://custom.api.com", p.baseURL)
				assert.Equal(t, "claude-3-opus-20240229", p.model)
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
			checkResult: func(t *testing.T, p *AnthropicProvider) {
				assert.Equal(t, "test-key", p.apiKey)
				assert.Equal(t, "https://api.anthropic.com/v1", p.baseURL)
				assert.Equal(t, "claude-3-sonnet-20240229", p.model)
				assert.Equal(t, 2000, p.maxTokens)
				assert.Equal(t, 0.7, p.temperature)
			},
		},
		{
			name: "config with env var API key",
			config: &llm.Config{
				Model: "claude-3-haiku-20240307",
			},
			envAPIKey: "env-api-key",
			wantErr:   false,
			checkResult: func(t *testing.T, p *AnthropicProvider) {
				assert.Equal(t, "env-api-key", p.apiKey)
				assert.Equal(t, "claude-3-haiku-20240307", p.model)
			},
		},
		{
			name: "config with env var base URL",
			config: &llm.Config{
				APIKey: "test-key",
			},
			envBaseURL: "https://env.api.com",
			wantErr:    false,
			checkResult: func(t *testing.T, p *AnthropicProvider) {
				assert.Equal(t, "https://env.api.com", p.baseURL)
			},
		},
		{
			name: "config with env var model",
			config: &llm.Config{
				APIKey: "test-key",
			},
			envModel: "claude-3-opus-20240229",
			wantErr:  false,
			checkResult: func(t *testing.T, p *AnthropicProvider) {
				assert.Equal(t, "claude-3-opus-20240229", p.model)
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
			os.Unsetenv("ANTHROPIC_API_KEY")
			os.Unsetenv("ANTHROPIC_BASE_URL")
			os.Unsetenv("ANTHROPIC_MODEL")

			// Set env vars if provided
			if tt.envAPIKey != "" {
				os.Setenv("ANTHROPIC_API_KEY", tt.envAPIKey)
				defer os.Unsetenv("ANTHROPIC_API_KEY")
			}
			if tt.envBaseURL != "" {
				os.Setenv("ANTHROPIC_BASE_URL", tt.envBaseURL)
				defer os.Unsetenv("ANTHROPIC_BASE_URL")
			}
			if tt.envModel != "" {
				os.Setenv("ANTHROPIC_MODEL", tt.envModel)
				defer os.Unsetenv("ANTHROPIC_MODEL")
			}

			provider, err := NewAnthropic(tt.config)

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

// TestAnthropicComplete tests the Complete method
func TestAnthropicComplete(t *testing.T) {
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
			mockResponse: AnthropicResponse{
				ID:   "msg_123",
				Type: "message",
				Role: "assistant",
				Content: []AnthropicContent{
					{Type: "text", Text: "Hello! How can I help you?"},
				},
				Model:      "claude-3-sonnet-20240229",
				StopReason: "end_turn",
				Usage: AnthropicUsage{
					InputTokens:  10,
					OutputTokens: 15,
				},
			},
			mockStatusCode: http.StatusOK,
			wantErr:        false,
			checkResult: func(t *testing.T, resp *llm.CompletionResponse) {
				assert.Equal(t, "Hello! How can I help you?", resp.Content)
				assert.Equal(t, "claude-3-sonnet-20240229", resp.Model)
				assert.Equal(t, 25, resp.TokensUsed)
				assert.Equal(t, "end_turn", resp.FinishReason)
				assert.Equal(t, "anthropic", resp.Provider)
				require.NotNil(t, resp.Usage)
				assert.Equal(t, 10, resp.Usage.PromptTokens)
				assert.Equal(t, 15, resp.Usage.CompletionTokens)
				assert.Equal(t, 25, resp.Usage.TotalTokens)
			},
		},
		{
			name: "completion with system message",
			request: &llm.CompletionRequest{
				Messages: []llm.Message{
					{Role: "system", Content: "You are a helpful assistant"},
					{Role: "user", Content: "Hi"},
				},
			},
			mockResponse: AnthropicResponse{
				ID:   "msg_456",
				Type: "message",
				Role: "assistant",
				Content: []AnthropicContent{
					{Type: "text", Text: "Hi there!"},
				},
				Model:      "claude-3-sonnet-20240229",
				StopReason: "end_turn",
				Usage: AnthropicUsage{
					InputTokens:  20,
					OutputTokens: 5,
				},
			},
			mockStatusCode: http.StatusOK,
			wantErr:        false,
			checkResult: func(t *testing.T, resp *llm.CompletionResponse) {
				assert.Equal(t, "Hi there!", resp.Content)
			},
		},
		{
			name: "completion with custom parameters",
			request: &llm.CompletionRequest{
				Messages:    []llm.Message{{Role: "user", Content: "Test"}},
				Model:       "claude-3-opus-20240229",
				MaxTokens:   1000,
				Temperature: 0.5,
				TopP:        0.9,
				Stop:        []string{"END"},
			},
			mockResponse: AnthropicResponse{
				ID:   "msg_789",
				Type: "message",
				Role: "assistant",
				Content: []AnthropicContent{
					{Type: "text", Text: "Response"},
				},
				Model:      "claude-3-opus-20240229",
				StopReason: "end_turn",
				Usage: AnthropicUsage{
					InputTokens:  5,
					OutputTokens: 10,
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
				// Verify headers
				assert.Equal(t, "application/json", r.Header.Get("Content-Type"))
				assert.NotEmpty(t, r.Header.Get("x-api-key"))
				assert.Equal(t, "2023-06-01", r.Header.Get("anthropic-version"))

				// Verify request body
				var req AnthropicRequest
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
			provider, err := NewAnthropic(&llm.Config{
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

// TestAnthropicChat tests the Chat method
func TestAnthropicChat(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(AnthropicResponse{
			ID:   "msg_chat",
			Type: "message",
			Role: "assistant",
			Content: []AnthropicContent{
				{Type: "text", Text: "Chat response"},
			},
			Model:      "claude-3-sonnet-20240229",
			StopReason: "end_turn",
			Usage: AnthropicUsage{
				InputTokens:  10,
				OutputTokens: 5,
			},
		})
	}))
	defer server.Close()

	provider, err := NewAnthropic(&llm.Config{
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

// TestAnthropicErrorHandling tests error scenarios
func TestAnthropicErrorHandling(t *testing.T) {
	tests := []struct {
		name           string
		statusCode     int
		responseBody   interface{}
		expectedErrCode agentErrors.ErrorCode
	}{
		{
			name:       "400 bad request",
			statusCode: 400,
			responseBody: AnthropicErrorResponse{
				Type: "error",
				Error: AnthropicErrorDetails{
					Type:    "invalid_request_error",
					Message: "Invalid request",
				},
			},
			expectedErrCode: agentErrors.CodeInvalidInput,
		},
		{
			name:       "401 unauthorized",
			statusCode: 401,
			responseBody: AnthropicErrorResponse{
				Type: "error",
				Error: AnthropicErrorDetails{
					Type:    "authentication_error",
					Message: "Invalid API key",
				},
			},
			expectedErrCode: agentErrors.CodeInvalidConfig,
		},
		{
			name:       "403 forbidden",
			statusCode: 403,
			responseBody: AnthropicErrorResponse{
				Type: "error",
				Error: AnthropicErrorDetails{
					Type:    "permission_error",
					Message: "Permission denied",
				},
			},
			expectedErrCode: agentErrors.CodeInvalidConfig,
		},
		{
			name:       "404 not found",
			statusCode: 404,
			responseBody: AnthropicErrorResponse{
				Type: "error",
				Error: AnthropicErrorDetails{
					Type:    "not_found_error",
					Message: "Model not found",
				},
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

			provider, err := NewAnthropic(&llm.Config{
				APIKey:  "test-key",
				BaseURL: server.URL,
			})
			require.NoError(t, err)

			_, err = provider.Complete(context.Background(), &llm.CompletionRequest{
				Messages: []llm.Message{{Role: "user", Content: "test"}},
			})

			require.Error(t, err)
			assert.True(t, agentErrors.IsCode(err, tt.expectedErrCode),
				"Expected error code %s but got %s", tt.expectedErrCode, agentErrors.GetCode(err))
		})
	}
}

// TestAnthropicRetry tests retry logic
func TestAnthropicRetry(t *testing.T) {
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
		json.NewEncoder(w).Encode(AnthropicResponse{
			ID:   "msg_retry",
			Type: "message",
			Role: "assistant",
			Content: []AnthropicContent{
				{Type: "text", Text: "Success after retry"},
			},
			Model:      "claude-3-sonnet-20240229",
			StopReason: "end_turn",
			Usage: AnthropicUsage{
				InputTokens:  5,
				OutputTokens: 10,
			},
		})
	}))
	defer server.Close()

	provider, err := NewAnthropic(&llm.Config{
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

// TestAnthropicRetryExhausted tests when retries are exhausted
func TestAnthropicRetryExhausted(t *testing.T) {
	attempts := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer server.Close()

	provider, err := NewAnthropic(&llm.Config{
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

// TestAnthropicContextCancellation tests context cancellation
func TestAnthropicContextCancellation(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Delay to allow context cancellation
		time.Sleep(100 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	provider, err := NewAnthropic(&llm.Config{
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

// TestAnthropicStream tests streaming
func TestAnthropicStream(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)

		flusher := w.(http.Flusher)

		// Send stream events
		events := []string{
			`data: {"type":"message_start","message":{"id":"msg_1","type":"message","role":"assistant"}}`,
			`data: {"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`,
			`data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"Hello"}}`,
			`data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":" world"}}`,
			`data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"!"}}`,
			`data: {"type":"content_block_stop","index":0}`,
			`data: {"type":"message_delta","delta":{"stop_reason":"end_turn"}}`,
			`data: {"type":"message_stop"}`,
		}

		for _, event := range events {
			fmt.Fprintf(w, "%s\n\n", event)
			flusher.Flush()
			time.Sleep(10 * time.Millisecond)
		}
	}))
	defer server.Close()

	provider, err := NewAnthropic(&llm.Config{
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

// TestAnthropicStreamError tests streaming with error
func TestAnthropicStreamError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer server.Close()

	provider, err := NewAnthropic(&llm.Config{
		APIKey:  "test-key",
		BaseURL: server.URL,
	})
	require.NoError(t, err)

	_, err = provider.Stream(context.Background(), "test")
	require.Error(t, err)
	assert.True(t, agentErrors.IsCode(err, agentErrors.CodeInvalidConfig))
}

// TestAnthropicProvider tests Provider method
func TestAnthropicProvider(t *testing.T) {
	provider, err := NewAnthropic(&llm.Config{
		APIKey: "test-key",
	})
	require.NoError(t, err)

	assert.Equal(t, llm.ProviderAnthropic, provider.Provider())
}

// TestAnthropicModelName tests ModelName method
func TestAnthropicModelName(t *testing.T) {
	provider, err := NewAnthropic(&llm.Config{
		APIKey: "test-key",
		Model:  "claude-3-opus-20240229",
	})
	require.NoError(t, err)

	assert.Equal(t, "claude-3-opus-20240229", provider.ModelName())
}

// TestAnthropicMaxTokens tests MaxTokens method
func TestAnthropicMaxTokens(t *testing.T) {
	provider, err := NewAnthropic(&llm.Config{
		APIKey:    "test-key",
		MaxTokens: 4000,
	})
	require.NoError(t, err)

	assert.Equal(t, 4000, provider.MaxTokens())
}

// TestAnthropicIsAvailable tests IsAvailable method
func TestAnthropicIsAvailable(t *testing.T) {
	t.Run("available", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
			json.NewEncoder(w).Encode(AnthropicResponse{
				ID:   "msg_test",
				Type: "message",
				Role: "assistant",
				Content: []AnthropicContent{
					{Type: "text", Text: "test"},
				},
				Model:      "claude-3-sonnet-20240229",
				StopReason: "end_turn",
				Usage: AnthropicUsage{
					InputTokens:  5,
					OutputTokens: 5,
				},
			})
		}))
		defer server.Close()

		provider, err := NewAnthropic(&llm.Config{
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

		provider, err := NewAnthropic(&llm.Config{
			APIKey:  "test-key",
			BaseURL: server.URL,
		})
		require.NoError(t, err)

		assert.False(t, provider.IsAvailable())
	})
}
