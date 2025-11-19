package providers

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"os"
	"strings"
	"time"

	agentErrors "github.com/kart-io/goagent/errors"
	"github.com/kart-io/goagent/interfaces"
	agentllm "github.com/kart-io/goagent/llm"
)

// AnthropicProvider implements LLM interface for Anthropic Claude
type AnthropicProvider struct {
	config      *agentllm.Config
	httpClient  *http.Client
	apiKey      string
	baseURL     string
	model       string
	maxTokens   int
	temperature float64
}

// AnthropicRequest represents a request to Anthropic API
type AnthropicRequest struct {
	Model         string             `json:"model"`
	Messages      []AnthropicMessage `json:"messages"`
	MaxTokens     int                `json:"max_tokens"`
	Temperature   float64            `json:"temperature,omitempty"`
	TopP          float64            `json:"top_p,omitempty"`
	TopK          int                `json:"top_k,omitempty"`
	Stream        bool               `json:"stream,omitempty"`
	StopSequences []string           `json:"stop_sequences,omitempty"`
	System        string             `json:"system,omitempty"`
}

// AnthropicMessage represents a message in Anthropic format
type AnthropicMessage struct {
	Role    string `json:"role"` // "user" or "assistant"
	Content string `json:"content"`
}

// AnthropicResponse represents a response from Anthropic API
type AnthropicResponse struct {
	ID           string             `json:"id"`
	Type         string             `json:"type"`
	Role         string             `json:"role"`
	Content      []AnthropicContent `json:"content"`
	Model        string             `json:"model"`
	StopReason   string             `json:"stop_reason"`
	StopSequence string             `json:"stop_sequence,omitempty"`
	Usage        AnthropicUsage     `json:"usage"`
}

// AnthropicContent represents content in response
type AnthropicContent struct {
	Type string `json:"type"` // "text"
	Text string `json:"text"`
}

// AnthropicUsage represents token usage
type AnthropicUsage struct {
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
}

// AnthropicStreamEvent represents a streaming event
type AnthropicStreamEvent struct {
	Type         string             `json:"type"`
	Message      *AnthropicResponse `json:"message,omitempty"`
	Index        int                `json:"index,omitempty"`
	Delta        *AnthropicDelta    `json:"delta,omitempty"`
	ContentBlock *AnthropicContent  `json:"content_block,omitempty"`
	Usage        *AnthropicUsage    `json:"usage,omitempty"`
}

// AnthropicDelta represents a streaming delta
type AnthropicDelta struct {
	Type string `json:"type"` // "text_delta"
	Text string `json:"text"`
}

// AnthropicErrorResponse represents an error response
type AnthropicErrorResponse struct {
	Type  string                `json:"type"` // "error"
	Error AnthropicErrorDetails `json:"error"`
}

// AnthropicErrorDetails represents error details
type AnthropicErrorDetails struct {
	Type    string `json:"type"`
	Message string `json:"message"`
}

// NewAnthropic creates a new Anthropic provider
func NewAnthropic(config *agentllm.Config) (*AnthropicProvider, error) {
	// Get API key from config or env
	apiKey := config.APIKey
	if apiKey == "" {
		apiKey = os.Getenv(agentllm.EnvAnthropicAPIKey)
	}

	if apiKey == "" {
		return nil, agentErrors.NewInvalidConfigError(ProviderAnthropic, agentllm.ErrorFieldAPIKey, fmt.Sprintf(ErrAPIKeyMissing, "ANTHROPIC"))
	}

	// Set base URL with fallback
	baseURL := config.BaseURL
	if baseURL == "" {
		baseURL = os.Getenv(agentllm.EnvAnthropicBaseURL)
	}
	if baseURL == "" {
		baseURL = AnthropicBaseURL
	}

	// Set model with fallback
	model := config.Model
	if model == "" {
		model = os.Getenv(agentllm.EnvAnthropicModel)
	}
	if model == "" {
		model = AnthropicDefaultModel
	}

	// Set other parameters with defaults
	maxTokens := config.MaxTokens
	if maxTokens == 0 {
		maxTokens = DefaultMaxTokens
	}

	temperature := config.Temperature
	if temperature == 0 {
		temperature = DefaultTemperature
	}

	timeout := time.Duration(config.Timeout) * time.Second
	if timeout == 0 {
		timeout = DefaultTimeout
	}

	// Create HTTP client with connection pooling
	httpClient := &http.Client{
		Timeout: timeout,
		Transport: &http.Transport{
			MaxIdleConns:        MaxIdleConns,
			MaxIdleConnsPerHost: MaxIdleConnsPerHost,
			IdleConnTimeout:     IdleConnTimeout,
		},
	}

	provider := &AnthropicProvider{
		config:      config,
		httpClient:  httpClient,
		apiKey:      apiKey,
		baseURL:     baseURL,
		model:       model,
		maxTokens:   maxTokens,
		temperature: temperature,
	}

	return provider, nil
}

// Complete implements basic text completion
func (p *AnthropicProvider) Complete(ctx context.Context, req *agentllm.CompletionRequest) (*agentllm.CompletionResponse, error) {
	// Build Anthropic request
	anthropicReq := p.buildRequest(req)

	// Execute with retry
	resp, err := p.executeWithRetry(ctx, anthropicReq)
	if err != nil {
		return nil, err
	}

	// Convert to standard response
	return p.convertResponse(resp), nil
}

// buildRequest converts agentllm.CompletionRequest to AnthropicRequest
func (p *AnthropicProvider) buildRequest(req *agentllm.CompletionRequest) *AnthropicRequest {
	// Separate system message from other messages
	var systemMsg string
	var messages []AnthropicMessage

	for _, msg := range req.Messages {
		if msg.Role == RoleSystem {
			systemMsg = msg.Content
		} else {
			messages = append(messages, AnthropicMessage{
				Role:    msg.Role,
				Content: msg.Content,
			})
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

	return &AnthropicRequest{
		Model:         model,
		Messages:      messages,
		MaxTokens:     maxTokens,
		Temperature:   temperature,
		TopP:          req.TopP,
		StopSequences: req.Stop,
		System:        systemMsg,
	}
}

// execute performs a single HTTP request to Anthropic API
func (p *AnthropicProvider) execute(ctx context.Context, req *AnthropicRequest) (*AnthropicResponse, error) {
	// Serialize request
	body, err := json.Marshal(req)
	if err != nil {
		return nil, agentErrors.Wrap(err, agentErrors.CodeInternal, ErrFailedMarshalRequest)
	}

	// Create HTTP request
	httpReq, err := http.NewRequestWithContext(ctx, "POST", p.baseURL+AnthropicMessagesPath, bytes.NewReader(body))
	if err != nil {
		return nil, agentErrors.Wrap(err, agentErrors.CodeInternal, ErrFailedCreateRequest)
	}

	// Set headers
	httpReq.Header.Set(HeaderContentType, ContentTypeJSON)
	httpReq.Header.Set(HeaderXAPIKey, p.apiKey)
	httpReq.Header.Set(HeaderAnthropicVersion, AnthropicAPIVersion)

	// Execute request
	httpResp, err := p.httpClient.Do(httpReq)
	if err != nil {
		return nil, agentErrors.NewLLMRequestError(ProviderAnthropic, p.model, err)
	}
	defer func() {
		_ = httpResp.Body.Close()
	}()

	// Check status code
	if httpResp.StatusCode != http.StatusOK {
		return nil, p.handleHTTPError(httpResp, req.Model)
	}

	// Deserialize response
	var resp AnthropicResponse
	if err := json.NewDecoder(httpResp.Body).Decode(&resp); err != nil {
		return nil, agentErrors.NewLLMResponseError(ProviderAnthropic, req.Model, ErrFailedDecodeResponse)
	}

	return &resp, nil
}

// handleHTTPError maps HTTP errors to AgentError
func (p *AnthropicProvider) handleHTTPError(resp *http.Response, model string) error {
	// Try to parse error response
	var errResp AnthropicErrorResponse
	if err := json.NewDecoder(resp.Body).Decode(&errResp); err == nil && errResp.Error.Message != "" {
		// Use error message from API
		switch resp.StatusCode {
		case 400:
			return agentErrors.NewInvalidInputError(ProviderAnthropic, "request", errResp.Error.Message)
		case 401:
			return agentErrors.NewInvalidConfigError(ProviderAnthropic, agentllm.ErrorFieldAPIKey, errResp.Error.Message)
		case 403:
			return agentErrors.NewInvalidConfigError(ProviderAnthropic, agentllm.ErrorFieldAPIKey, errResp.Error.Message)
		case 404:
			return agentErrors.NewLLMResponseError(ProviderAnthropic, model, errResp.Error.Message)
		case 429:
			retryAfter := parseRetryAfter(resp.Header.Get("Retry-After"))
			return agentErrors.NewLLMRateLimitError(ProviderAnthropic, model, retryAfter)
		case 500, 502, 503, 504:
			return agentErrors.NewLLMRequestError(ProviderAnthropic, model, fmt.Errorf("server error: %s", errResp.Error.Message))
		}
	}

	// Fallback error handling
	switch resp.StatusCode {
	case 400:
		return agentErrors.NewInvalidInputError(ProviderAnthropic, "request", StatusBadRequest)
	case 401:
		return agentErrors.NewInvalidConfigError(ProviderAnthropic, agentllm.ErrorFieldAPIKey, StatusInvalidAPIKey)
	case 403:
		return agentErrors.NewInvalidConfigError(ProviderAnthropic, agentllm.ErrorFieldAPIKey, StatusAPIKeyLacksPermissions)
	case 404:
		return agentErrors.NewLLMResponseError(ProviderAnthropic, model, StatusModelNotFound)
	case 429:
		retryAfter := parseRetryAfter(resp.Header.Get("Retry-After"))
		return agentErrors.NewLLMRateLimitError(ProviderAnthropic, model, retryAfter)
	case 500, 502, 503, 504:
		return agentErrors.NewLLMRequestError(ProviderAnthropic, model, fmt.Errorf("server error: %d", resp.StatusCode))
	default:
		return agentErrors.NewLLMRequestError(ProviderAnthropic, model, fmt.Errorf("unexpected status: %d", resp.StatusCode))
	}
}

// executeWithRetry executes request with exponential backoff
func (p *AnthropicProvider) executeWithRetry(ctx context.Context, req *AnthropicRequest) (*AnthropicResponse, error) {
	maxAttempts := DefaultMaxAttempts
	baseDelay := DefaultBaseDelay

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

	return nil, agentErrors.NewInternalError(ProviderAnthropic, "execute_with_retry", fmt.Errorf("%s", ErrMaxRetriesExceeded))
}

// isRetryable checks if an error is retryable
func isRetryable(err error) bool {
	if err == nil {
		return false
	}

	code := agentErrors.GetCode(err)
	return code == agentErrors.CodeLLMRateLimit ||
		code == agentErrors.CodeLLMTimeout ||
		code == agentErrors.CodeLLMRequest
}

// convertResponse converts AnthropicResponse to agentllm.CompletionResponse
func (p *AnthropicProvider) convertResponse(resp *AnthropicResponse) *agentllm.CompletionResponse {
	// Extract text content
	var content string
	if len(resp.Content) > 0 {
		content = resp.Content[0].Text
	}

	return &agentllm.CompletionResponse{
		Content:      content,
		Model:        resp.Model,
		TokensUsed:   resp.Usage.InputTokens + resp.Usage.OutputTokens,
		FinishReason: resp.StopReason,
		Provider:     string(agentllm.ProviderAnthropic),
		Usage: &interfaces.TokenUsage{
			PromptTokens:     resp.Usage.InputTokens,
			CompletionTokens: resp.Usage.OutputTokens,
			TotalTokens:      resp.Usage.InputTokens + resp.Usage.OutputTokens,
		},
	}
}

// Chat implements chat conversation
func (p *AnthropicProvider) Chat(ctx context.Context, messages []agentllm.Message) (*agentllm.CompletionResponse, error) {
	return p.Complete(ctx, &agentllm.CompletionRequest{
		Messages: messages,
	})
}

// Provider returns the provider type
func (p *AnthropicProvider) Provider() agentllm.Provider {
	return agentllm.ProviderAnthropic
}

// IsAvailable checks if the provider is available
func (p *AnthropicProvider) IsAvailable() bool {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Try a minimal completion
	_, err := p.Complete(ctx, &agentllm.CompletionRequest{
		Messages: []agentllm.Message{{Role: RoleUser, Content: "test"}},
	})

	return err == nil
}

// Stream implements streaming generation
func (p *AnthropicProvider) Stream(ctx context.Context, prompt string) (<-chan string, error) {
	tokens := make(chan string, 100)

	// Build streaming request
	req := &AnthropicRequest{
		Model:       p.model,
		Messages:    []AnthropicMessage{{Role: RoleUser, Content: prompt}},
		MaxTokens:   p.maxTokens,
		Temperature: p.temperature,
		Stream:      true,
	}

	// Create HTTP request
	body, err := json.Marshal(req)
	if err != nil {
		return nil, agentErrors.Wrap(err, agentErrors.CodeInternal, ErrFailedMarshalRequest)
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", p.baseURL+AnthropicMessagesPath, bytes.NewReader(body))
	if err != nil {
		return nil, agentErrors.NewLLMRequestError(ProviderAnthropic, p.model, err)
	}

	httpReq.Header.Set(HeaderContentType, ContentTypeJSON)
	httpReq.Header.Set(HeaderXAPIKey, p.apiKey)
	httpReq.Header.Set(HeaderAnthropicVersion, AnthropicAPIVersion)
	httpReq.Header.Set(HeaderAccept, AcceptEventStream)

	// Execute request
	httpResp, err := p.httpClient.Do(httpReq)
	if err != nil {
		return nil, agentErrors.NewLLMRequestError(ProviderAnthropic, p.model, err)
	}

	if httpResp.StatusCode != http.StatusOK {
		_ = httpResp.Body.Close()
		return nil, p.handleHTTPError(httpResp, p.model)
	}

	// Start goroutine to read stream
	go func() {
		defer close(tokens)
		defer func() {
			_ = httpResp.Body.Close()
		}()

		scanner := bufio.NewScanner(httpResp.Body)
		for scanner.Scan() {
			line := scanner.Text()

			// Parse SSE format: "data: {...}"
			if !strings.HasPrefix(line, SSEDataPrefix) {
				continue
			}

			data := strings.TrimPrefix(line, SSEDataPrefix)
			if data == SSEDoneMessage {
				return
			}

			var event AnthropicStreamEvent
			if err := json.Unmarshal([]byte(data), &event); err != nil {
				continue
			}

			// Extract text from content_block_delta events
			if event.Type == EventContentBlockDelta && event.Delta != nil {
				// Use select to handle context cancellation
				select {
				case tokens <- event.Delta.Text:
					// Successfully sent
				case <-ctx.Done():
					// Context cancelled, exit immediately
					return
				}
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
func (p *AnthropicProvider) ModelName() string {
	return p.model
}

// MaxTokens returns the max tokens setting
func (p *AnthropicProvider) MaxTokens() int {
	return p.maxTokens
}
