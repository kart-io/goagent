package providers

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"math/rand"
	"os"
	"strings"
	"time"

	"github.com/kart-io/goagent/llm/constants"
	"github.com/kart-io/goagent/utils/json"

	"github.com/go-resty/resty/v2"

	agentErrors "github.com/kart-io/goagent/errors"
	"github.com/kart-io/goagent/interfaces"
	agentllm "github.com/kart-io/goagent/llm"
	"github.com/kart-io/goagent/utils/httpclient"
)

// CohereProvider implements LLM interface for Cohere
type CohereProvider struct {
	config      *agentllm.LLMOptions
	client      *httpclient.Client
	apiKey      string
	baseURL     string
	model       string
	maxTokens   int
	temperature float64
}

// CohereRequest represents a request to Cohere API
type CohereRequest struct {
	Model            string          `json:"model"`
	Message          string          `json:"message"`
	ChatHistory      []CohereMessage `json:"chat_history,omitempty"`
	Temperature      float64         `json:"temperature,omitempty"`
	MaxTokens        int             `json:"max_tokens,omitempty"`
	P                float64         `json:"p,omitempty"` // Top-p
	K                int             `json:"k,omitempty"` // Top-k
	Stream           bool            `json:"stream,omitempty"`
	StopSequences    []string        `json:"stop_sequences,omitempty"`
	PresencePenalty  float64         `json:"presence_penalty,omitempty"`
	FrequencyPenalty float64         `json:"frequency_penalty,omitempty"`
}

// CohereMessage represents a message in Cohere format
type CohereMessage struct {
	Role    string `json:"role"` // "USER", "CHATBOT", "SYSTEM"
	Message string `json:"message"`
}

// CohereResponse represents a response from Cohere API
type CohereResponse struct {
	ResponseID   string          `json:"response_id"`
	Text         string          `json:"text"`
	GenerationID string          `json:"generation_id"`
	FinishReason string          `json:"finish_reason"`
	TokenCount   CohereTokens    `json:"token_count"`
	ChatHistory  []CohereMessage `json:"chat_history,omitempty"`
}

// CohereTokens represents token usage
type CohereTokens struct {
	PromptTokens   int `json:"prompt_tokens"`
	ResponseTokens int `json:"response_tokens"`
	TotalTokens    int `json:"total_tokens"`
	BilledTokens   int `json:"billed_tokens,omitempty"`
}

// CohereStreamEvent represents a streaming event
type CohereStreamEvent struct {
	EventType    string          `json:"event_type"` // "stream-start", "text-generation", "stream-end"
	Text         string          `json:"text,omitempty"`
	FinishReason string          `json:"finish_reason,omitempty"`
	Response     *CohereResponse `json:"response,omitempty"`
}

// CohereErrorResponse represents an error response
type CohereErrorResponse struct {
	Message string `json:"message"`
}

// NewCohere creates a new Cohere provider
func NewCohere(config *agentllm.LLMOptions) (*CohereProvider, error) {
	// Get API key from config or env
	apiKey := config.APIKey
	if apiKey == "" {
		apiKey = os.Getenv(constants.EnvCohereAPIKey)
	}

	if apiKey == "" {
		return nil, agentErrors.NewInvalidConfigError(string(constants.ProviderCohere), constants.ErrorFieldAPIKey, fmt.Sprintf(constants.ErrAPIKeyMissing, "COHERE"))
	}

	// Set base URL with fallback
	baseURL := config.BaseURL
	if baseURL == "" {
		baseURL = os.Getenv(constants.EnvCohereBaseURL)
	}
	if baseURL == "" {
		baseURL = constants.CohereBaseURL
	}

	// Set model with fallback
	model := config.Model
	if model == "" {
		model = os.Getenv(constants.EnvCohereModel)
	}
	if model == "" {
		model = constants.CohereDefaultModel
	}

	// Set other parameters with defaults
	maxTokens := config.MaxTokens
	if maxTokens == 0 {
		maxTokens = constants.DefaultMaxTokens
	}

	temperature := config.Temperature
	if temperature == 0 {
		temperature = constants.DefaultTemperature
	}

	timeout := time.Duration(config.Timeout) * time.Second
	if timeout == 0 {
		timeout = constants.DefaultTimeout
	}

	// Create httpclient
	client := httpclient.NewClient(&httpclient.Config{
		Timeout: timeout,
		Headers: map[string]string{
			constants.HeaderContentType:   constants.ContentTypeJSON,
			constants.HeaderAuthorization: constants.AuthBearerPrefix + apiKey,
		},
	})

	provider := &CohereProvider{
		config:      config,
		client:      client,
		apiKey:      apiKey,
		baseURL:     baseURL,
		model:       model,
		maxTokens:   maxTokens,
		temperature: temperature,
	}

	return provider, nil
}

// Complete implements basic text completion
func (p *CohereProvider) Complete(ctx context.Context, req *agentllm.CompletionRequest) (*agentllm.CompletionResponse, error) {
	// Build Cohere request
	cohereReq := p.buildRequest(req)

	// Execute with retry
	resp, err := p.executeWithRetry(ctx, cohereReq)
	if err != nil {
		return nil, err
	}

	// Convert to standard response
	return p.convertResponse(resp), nil
}

// buildRequest converts agentllm.CompletionRequest to CohereRequest
func (p *CohereProvider) buildRequest(req *agentllm.CompletionRequest) *CohereRequest {
	// Convert messages to Cohere format
	// Last user message becomes the message field
	// Previous messages become chat history
	var message string
	var chatHistory []CohereMessage

	for _, msg := range req.Messages {
		cohereRole := p.convertRole(msg.Role)

		if msg.Role == "user" && message == "" {
			// Use the last user message as the main message
			message = msg.Content
		} else {
			// Add to chat history
			chatHistory = append(chatHistory, CohereMessage{
				Role:    cohereRole,
				Message: msg.Content,
			})
		}
	}

	// If no user message found, use the last message
	if message == "" && len(req.Messages) > 0 {
		lastMsg := req.Messages[len(req.Messages)-1]
		message = lastMsg.Content
		// Remove last from history
		if len(chatHistory) > 0 {
			chatHistory = chatHistory[:len(chatHistory)-1]
		}
	}

	// Use request parameters or provider defaults
	model := p.model
	if req.Model != "" {
		model = req.Model
	}

	maxTokens := p.maxTokens
	if req.MaxTokens > 0 {
		maxTokens = req.MaxTokens
	}

	temperature := p.temperature
	if req.Temperature > 0 {
		temperature = req.Temperature
	}

	return &CohereRequest{
		Model:         model,
		Message:       message,
		ChatHistory:   chatHistory,
		Temperature:   temperature,
		MaxTokens:     maxTokens,
		P:             req.TopP,
		StopSequences: req.Stop,
	}
}

// convertRole converts standard role to Cohere role
func (p *CohereProvider) convertRole(role string) string {
	switch role {
	case constants.RoleUser:
		return constants.CohereRoleUser
	case constants.RoleAssistant:
		return constants.CohereRoleChatbot
	case constants.RoleSystem:
		return constants.CohereRoleSystem
	default:
		return constants.CohereRoleUser
	}
}

// execute performs a single HTTP request to Cohere API
func (p *CohereProvider) execute(ctx context.Context, req *CohereRequest) (*CohereResponse, error) {
	// Execute request using resty
	resp, err := p.client.R().
		SetContext(ctx).
		SetBody(req).
		Post(p.baseURL + constants.CohereChatPath)

	if err != nil {
		return nil, agentErrors.NewLLMRequestError(string(constants.ProviderCohere), p.model, err)
	}

	// Check status code
	if !resp.IsSuccess() {
		return nil, p.handleHTTPError(resp, req.Model)
	}

	// Deserialize response
	var cohereResp CohereResponse
	if err := json.NewDecoder(strings.NewReader(resp.String())).Decode(&cohereResp); err != nil {
		return nil, agentErrors.NewLLMResponseError(string(constants.ProviderCohere), req.Model, constants.ErrFailedDecodeResponse)
	}

	return &cohereResp, nil
}

// handleHTTPError maps HTTP errors to AgentError
func (p *CohereProvider) handleHTTPError(resp *resty.Response, model string) error {
	// Try to parse error response
	var errResp CohereErrorResponse
	if err := json.NewDecoder(strings.NewReader(resp.String())).Decode(&errResp); err == nil && errResp.Message != "" {
		// Use error message from API
		switch resp.StatusCode() {
		case 400:
			return agentErrors.NewInvalidInputError(string(constants.ProviderCohere), "request", errResp.Message)
		case 401:
			return agentErrors.NewInvalidConfigError(string(constants.ProviderCohere), constants.ErrorFieldAPIKey, errResp.Message)
		case 403:
			return agentErrors.NewInvalidConfigError(string(constants.ProviderCohere), constants.ErrorFieldAPIKey, errResp.Message)
		case 404:
			return agentErrors.NewLLMResponseError(string(constants.ProviderCohere), model, errResp.Message)
		case 429:
			retryAfter := parseRetryAfter(resp.Header().Get("Retry-After"))
			return agentErrors.NewLLMRateLimitError(string(constants.ProviderCohere), model, retryAfter)
		case 500, 502, 503, 504:
			return agentErrors.NewLLMRequestError(string(constants.ProviderCohere), model, fmt.Errorf("server error: %s", errResp.Message))
		}
	}

	// Fallback error handling
	switch resp.StatusCode() {
	case 400:
		return agentErrors.NewInvalidInputError(string(constants.ProviderCohere), "request", constants.StatusBadRequest)
	case 401:
		return agentErrors.NewInvalidConfigError(string(constants.ProviderCohere), constants.ErrorFieldAPIKey, constants.StatusInvalidAPIKey)
	case 403:
		return agentErrors.NewInvalidConfigError(string(constants.ProviderCohere), constants.ErrorFieldAPIKey, constants.StatusAPIKeyLacksPermissions)
	case 404:
		return agentErrors.NewLLMResponseError(string(constants.ProviderCohere), model, constants.StatusEndpointNotFound)
	case 429:
		retryAfter := parseRetryAfter(resp.Header().Get("Retry-After"))
		return agentErrors.NewLLMRateLimitError(string(constants.ProviderCohere), model, retryAfter)
	case 500, 502, 503, 504:
		return agentErrors.NewLLMRequestError(string(constants.ProviderCohere), model, fmt.Errorf("server error: %d", resp.StatusCode()))
	default:
		return agentErrors.NewLLMRequestError(string(constants.ProviderCohere), model, fmt.Errorf("unexpected status: %d", resp.StatusCode()))
	}
}

// executeWithRetry executes request with exponential backoff
func (p *CohereProvider) executeWithRetry(ctx context.Context, req *CohereRequest) (*CohereResponse, error) {
	maxAttempts := constants.DefaultMaxAttempts
	baseDelay := constants.DefaultBaseDelay

	// Use shorter delays in test environment
	if testDelay, ok := ctx.Value("test_retry_delay").(time.Duration); ok && testDelay > 0 {
		baseDelay = testDelay
	} else if os.Getenv("GO_TEST_MODE") == "true" {
		// Automatic fast retries in test mode
		baseDelay = 10 * time.Millisecond
	}

	for attempt := 1; attempt <= maxAttempts; attempt++ {
		resp, err := p.execute(ctx, req)
		if err == nil {
			return resp, nil
		}

		// Check if error is retryable
		if !isRetryable(err) {
			return nil, err
		}

		// Last attempt failed
		if attempt == maxAttempts {
			return nil, agentErrors.ErrorWithRetry(err, attempt, maxAttempts)
		}

		// Exponential backoff with jitter
		delay := baseDelay * time.Duration(1<<uint(attempt-1))
		jitter := time.Duration(rand.Int63n(int64(delay) / 2))

		select {
		case <-ctx.Done():
			return nil, agentErrors.NewContextCanceledError("llm_request")
		case <-time.After(delay + jitter):
			// Continue to next attempt
		}
	}

	return nil, agentErrors.NewInternalError(string(constants.ProviderCohere), "execute_with_retry", fmt.Errorf("%s", constants.ErrMaxRetriesExceeded))
}

// convertResponse converts CohereResponse to agentllm.CompletionResponse
func (p *CohereProvider) convertResponse(resp *CohereResponse) *agentllm.CompletionResponse {
	return &agentllm.CompletionResponse{
		Content:      resp.Text,
		Model:        p.model, // Cohere doesn't return model in response
		TokensUsed:   resp.TokenCount.TotalTokens,
		FinishReason: resp.FinishReason,
		Provider:     string(constants.ProviderCohere),
		Usage: &interfaces.TokenUsage{
			PromptTokens:     resp.TokenCount.PromptTokens,
			CompletionTokens: resp.TokenCount.ResponseTokens,
			TotalTokens:      resp.TokenCount.TotalTokens,
		},
	}
}

// Chat implements chat conversation
func (p *CohereProvider) Chat(ctx context.Context, messages []agentllm.Message) (*agentllm.CompletionResponse, error) {
	return p.Complete(ctx, &agentllm.CompletionRequest{
		Messages: messages,
	})
}

// Provider returns the provider type
func (p *CohereProvider) Provider() constants.Provider {
	return constants.ProviderCohere
}

// IsAvailable checks if the provider is available
func (p *CohereProvider) IsAvailable() bool {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Try a minimal completion
	_, err := p.Complete(ctx, &agentllm.CompletionRequest{
		Messages: []agentllm.Message{{Role: constants.RoleUser, Content: "test"}},
	})

	return err == nil
}

// Stream implements streaming generation
func (p *CohereProvider) Stream(ctx context.Context, prompt string) (<-chan string, error) {
	tokens := make(chan string, 100)

	// Build streaming request
	req := &CohereRequest{
		Model:       p.model,
		Message:     prompt,
		Temperature: p.temperature,
		MaxTokens:   p.maxTokens,
		Stream:      true,
	}

	// Create streaming request with Accept header
	streamClient := p.client.R().
		SetContext(ctx).
		SetHeader(constants.HeaderAccept, constants.ContentTypeEventStream).
		SetBody(req)

	// Execute streaming request
	resp, err := streamClient.Post(p.baseURL + constants.CohereChatPath)
	if err != nil {
		return nil, agentErrors.NewLLMRequestError(string(constants.ProviderCohere), p.model, err)
	}

	if !resp.IsSuccess() {
		return nil, p.handleHTTPError(resp, p.model)
	}

	// Start goroutine to read stream
	go func() {
		defer close(tokens)

		scanner := bufio.NewScanner(strings.NewReader(resp.String()))
		for scanner.Scan() {
			line := scanner.Text()

			// Skip empty lines
			if strings.TrimSpace(line) == "" {
				continue
			}

			// Parse Cohere SSE format
			var event CohereStreamEvent
			if err := json.Unmarshal([]byte(line), &event); err != nil {
				continue
			}

			// Extract text from text-generation events
			if event.EventType == constants.EventTextGeneration && event.Text != "" {
				// Use select to handle context cancellation
				select {
				case tokens <- event.Text:
					// Successfully sent
				case <-ctx.Done():
					// Context cancelled, exit immediately
					return
				}
			}

			// Stop on stream-end
			if event.EventType == constants.EventStreamEnd {
				return
			}
		}

		if err := scanner.Err(); err != nil && err != io.EOF {
			// Log error but don't crash stream
			fmt.Printf("Stream error: %v\n", err)
		}
	}()

	return tokens, nil
}

// ModelName returns the model name
func (p *CohereProvider) ModelName() string {
	return p.model
}

// MaxTokens returns the max tokens setting
func (p *CohereProvider) MaxTokens() int {
	return p.maxTokens
}
