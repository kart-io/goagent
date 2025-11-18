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
	"github.com/kart-io/goagent/llm"
)

// HuggingFaceProvider implements LLM interface for Hugging Face
type HuggingFaceProvider struct {
	config      *llm.Config
	httpClient  *http.Client
	apiKey      string
	baseURL     string
	model       string
	maxTokens   int
	temperature float64
}

// HuggingFaceRequest represents a request to Hugging Face API
type HuggingFaceRequest struct {
	Inputs     string                     `json:"inputs"`
	Parameters HuggingFaceParameters      `json:"parameters,omitempty"`
	Options    HuggingFaceOptions         `json:"options,omitempty"`
}

// HuggingFaceParameters represents request parameters
type HuggingFaceParameters struct {
	Temperature       float64  `json:"temperature,omitempty"`
	MaxNewTokens      int      `json:"max_new_tokens,omitempty"`
	TopP              float64  `json:"top_p,omitempty"`
	TopK              int      `json:"top_k,omitempty"`
	RepetitionPenalty float64  `json:"repetition_penalty,omitempty"`
	StopSequences     []string `json:"stop_sequences,omitempty"`
	ReturnFullText    bool     `json:"return_full_text,omitempty"`
}

// HuggingFaceOptions represents request options
type HuggingFaceOptions struct {
	UseCache     bool `json:"use_cache"`
	WaitForModel bool `json:"wait_for_model"`
}

// HuggingFaceResponse represents a response from Hugging Face API
type HuggingFaceResponse struct {
	GeneratedText string                  `json:"generated_text"`
	Details       *HuggingFaceDetails     `json:"details,omitempty"`
}

// HuggingFaceDetails represents generation details
type HuggingFaceDetails struct {
	FinishReason    string `json:"finish_reason"`
	GeneratedTokens int    `json:"generated_tokens"`
	Seed            int64  `json:"seed,omitempty"`
}

// HuggingFaceStreamResponse represents a streaming response
type HuggingFaceStreamResponse struct {
	Token         HuggingFaceToken    `json:"token"`
	GeneratedText string              `json:"generated_text,omitempty"`
	Details       *HuggingFaceDetails `json:"details,omitempty"`
}

// HuggingFaceToken represents a single token
type HuggingFaceToken struct {
	ID      int     `json:"id"`
	Text    string  `json:"text"`
	LogProb float64 `json:"logprob"`
	Special bool    `json:"special"`
}

// HuggingFaceErrorResponse represents an error response
type HuggingFaceErrorResponse struct {
	Error         string  `json:"error"`
	EstimatedTime float64 `json:"estimated_time,omitempty"` // For model loading
}

// NewHuggingFace creates a new Hugging Face provider
func NewHuggingFace(config *llm.Config) (*HuggingFaceProvider, error) {
	// Get API key from config or env
	apiKey := config.APIKey
	if apiKey == "" {
		apiKey = os.Getenv("HUGGINGFACE_API_KEY")
	}

	if apiKey == "" {
		return nil, agentErrors.NewInvalidConfigError("huggingface", "api_key", "API key must be provided via config or HUGGINGFACE_API_KEY env var")
	}

	// Set base URL with fallback
	baseURL := config.BaseURL
	if baseURL == "" {
		baseURL = os.Getenv("HUGGINGFACE_BASE_URL")
	}
	if baseURL == "" {
		baseURL = "https://api-inference.huggingface.co"
	}

	// Set model with fallback
	model := config.Model
	if model == "" {
		model = os.Getenv("HUGGINGFACE_MODEL")
	}
	if model == "" {
		model = "meta-llama/Meta-Llama-3-8B-Instruct" // Default model
	}

	// Set other parameters with defaults
	maxTokens := config.MaxTokens
	if maxTokens == 0 {
		maxTokens = 2000
	}

	temperature := config.Temperature
	if temperature == 0 {
		temperature = 0.7
	}

	timeout := time.Duration(config.Timeout) * time.Second
	if timeout == 0 {
		timeout = 120 * time.Second // Longer timeout for model loading
	}

	// Create HTTP client with connection pooling
	httpClient := &http.Client{
		Timeout: timeout,
		Transport: &http.Transport{
			MaxIdleConns:        100,
			MaxIdleConnsPerHost: 10,
			IdleConnTimeout:     90 * time.Second,
		},
	}

	provider := &HuggingFaceProvider{
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
func (p *HuggingFaceProvider) Complete(ctx context.Context, req *llm.CompletionRequest) (*llm.CompletionResponse, error) {
	// Build Hugging Face request
	hfReq := p.buildRequest(req)

	// Execute with retry (includes model loading retry)
	resp, err := p.executeWithRetry(ctx, hfReq)
	if err != nil {
		return nil, err
	}

	// Convert to standard response
	return p.convertResponse(resp), nil
}

// buildRequest converts llm.CompletionRequest to HuggingFaceRequest
func (p *HuggingFaceProvider) buildRequest(req *llm.CompletionRequest) *HuggingFaceRequest {
	// Combine all messages into a single input string
	var inputs strings.Builder
	for _, msg := range req.Messages {
		if msg.Role == "system" {
			inputs.WriteString(fmt.Sprintf("System: %s\n", msg.Content))
		} else if msg.Role == "user" {
			inputs.WriteString(fmt.Sprintf("User: %s\n", msg.Content))
		} else if msg.Role == "assistant" {
			inputs.WriteString(fmt.Sprintf("Assistant: %s\n", msg.Content))
		}
	}
	inputs.WriteString("Assistant: ") // Prompt for response

	// Use request parameters or provider defaults
	maxTokens := p.maxTokens
	if req.MaxTokens > 0 {
		maxTokens = req.MaxTokens
	}

	temperature := p.temperature
	if req.Temperature > 0 {
		temperature = req.Temperature
	}

	return &HuggingFaceRequest{
		Inputs: inputs.String(),
		Parameters: HuggingFaceParameters{
			Temperature:    temperature,
			MaxNewTokens:   maxTokens,
			TopP:           req.TopP,
			StopSequences:  req.Stop,
			ReturnFullText: false, // Only return generated text
		},
		Options: HuggingFaceOptions{
			UseCache:     false,
			WaitForModel: true, // Wait for model to load
		},
	}
}

// execute performs a single HTTP request to Hugging Face API
func (p *HuggingFaceProvider) execute(ctx context.Context, req *HuggingFaceRequest) (*HuggingFaceResponse, error) {
	// Serialize request
	body, err := json.Marshal(req)
	if err != nil {
		return nil, agentErrors.Wrap(err, agentErrors.CodeInternal, "failed to marshal request")
	}

	// Create HTTP request with model ID in URL
	endpoint := fmt.Sprintf("%s/models/%s", p.baseURL, p.model)
	httpReq, err := http.NewRequestWithContext(ctx, "POST", endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, agentErrors.Wrap(err, agentErrors.CodeInternal, "failed to create request")
	}

	// Set headers (Bearer token)
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+p.apiKey)

	// Execute request
	httpResp, err := p.httpClient.Do(httpReq)
	if err != nil {
		return nil, agentErrors.NewLLMRequestError("huggingface", p.model, err)
	}
	defer func() {
		_ = httpResp.Body.Close()
	}()

	// Check status code
	if httpResp.StatusCode != http.StatusOK {
		return nil, p.handleHTTPError(httpResp, p.model)
	}

	// Deserialize response (array format)
	var respArray []HuggingFaceResponse
	if err := json.NewDecoder(httpResp.Body).Decode(&respArray); err != nil {
		return nil, agentErrors.NewLLMResponseError("huggingface", p.model, "failed to decode response")
	}

	if len(respArray) == 0 {
		return nil, agentErrors.NewLLMResponseError("huggingface", p.model, "empty response array")
	}

	return &respArray[0], nil
}

// handleHTTPError maps HTTP errors to AgentError
func (p *HuggingFaceProvider) handleHTTPError(resp *http.Response, model string) error {
	// Try to parse error response
	var errResp HuggingFaceErrorResponse
	if err := json.NewDecoder(resp.Body).Decode(&errResp); err == nil && errResp.Error != "" {
		// Use error message from API
		switch resp.StatusCode {
		case 400:
			return agentErrors.NewInvalidInputError("huggingface", "request", errResp.Error)
		case 401:
			return agentErrors.NewInvalidConfigError("huggingface", "api_key", errResp.Error)
		case 403:
			return agentErrors.NewInvalidConfigError("huggingface", "api_key", errResp.Error)
		case 404:
			return agentErrors.NewLLMResponseError("huggingface", model, errResp.Error)
		case 429:
			retryAfter := parseRetryAfter(resp.Header.Get("Retry-After"))
			return agentErrors.NewLLMRateLimitError("huggingface", model, retryAfter)
		case 503:
			// Model is loading - this is retryable
			estimatedTime := int(errResp.EstimatedTime)
			if estimatedTime == 0 {
				estimatedTime = 20 // Default 20 seconds
			}
			return agentErrors.NewLLMRequestError("huggingface", model,
				fmt.Errorf("model loading (estimated time: %d seconds)", estimatedTime))
		case 500, 502, 504:
			return agentErrors.NewLLMRequestError("huggingface", model, fmt.Errorf("server error: %s", errResp.Error))
		}
	}

	// Fallback error handling
	switch resp.StatusCode {
	case 400:
		return agentErrors.NewInvalidInputError("huggingface", "request", "bad request")
	case 401:
		return agentErrors.NewInvalidConfigError("huggingface", "api_key", "invalid API key")
	case 403:
		return agentErrors.NewInvalidConfigError("huggingface", "api_key", "API key lacks permissions")
	case 404:
		return agentErrors.NewLLMResponseError("huggingface", model, "model not found")
	case 429:
		retryAfter := parseRetryAfter(resp.Header.Get("Retry-After"))
		return agentErrors.NewLLMRateLimitError("huggingface", model, retryAfter)
	case 503:
		return agentErrors.NewLLMRequestError("huggingface", model, fmt.Errorf("model loading"))
	case 500, 502, 504:
		return agentErrors.NewLLMRequestError("huggingface", model, fmt.Errorf("server error: %d", resp.StatusCode))
	default:
		return agentErrors.NewLLMRequestError("huggingface", model, fmt.Errorf("unexpected status: %d", resp.StatusCode))
	}
}

// executeWithRetry executes request with extended retry for model loading
func (p *HuggingFaceProvider) executeWithRetry(ctx context.Context, req *HuggingFaceRequest) (*HuggingFaceResponse, error) {
	maxAttempts := 5 // More attempts for model loading
	baseDelay := 2 * time.Second // Longer base delay

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

		// Exponential backoff with jitter (longer delays for model loading)
		delay := baseDelay * time.Duration(1<<uint(attempt-1))
		// Cap at 60 seconds
		if delay > 60*time.Second {
			delay = 60 * time.Second
		}
		jitter := time.Duration(rand.Int63n(int64(delay) / 2))

		select {
		case <-ctx.Done():
			return nil, agentErrors.NewContextCanceledError("llm_request")
		case <-time.After(delay + jitter):
			// Continue to next attempt
		}
	}

	return nil, agentErrors.NewInternalError("huggingface", "execute_with_retry", fmt.Errorf("max retries exceeded"))
}

// convertResponse converts HuggingFaceResponse to llm.CompletionResponse
func (p *HuggingFaceProvider) convertResponse(resp *HuggingFaceResponse) *llm.CompletionResponse {
	// Estimate token usage (HF doesn't always provide it)
	var promptTokens, completionTokens int
	if resp.Details != nil {
		completionTokens = resp.Details.GeneratedTokens
		// Rough estimate for prompt tokens (4 chars per token)
		promptTokens = len(resp.GeneratedText) / 4
	}

	finishReason := "complete"
	if resp.Details != nil && resp.Details.FinishReason != "" {
		finishReason = resp.Details.FinishReason
	}

	return &llm.CompletionResponse{
		Content:      resp.GeneratedText,
		Model:        p.model,
		TokensUsed:   promptTokens + completionTokens,
		FinishReason: finishReason,
		Provider:     string(llm.ProviderHuggingFace),
		Usage: &interfaces.TokenUsage{
			PromptTokens:     promptTokens,
			CompletionTokens: completionTokens,
			TotalTokens:      promptTokens + completionTokens,
		},
	}
}

// Chat implements chat conversation
func (p *HuggingFaceProvider) Chat(ctx context.Context, messages []llm.Message) (*llm.CompletionResponse, error) {
	return p.Complete(ctx, &llm.CompletionRequest{
		Messages: messages,
	})
}

// Provider returns the provider type
func (p *HuggingFaceProvider) Provider() llm.Provider {
	return llm.ProviderHuggingFace
}

// IsAvailable checks if the provider is available
func (p *HuggingFaceProvider) IsAvailable() bool {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Try a minimal completion
	_, err := p.Complete(ctx, &llm.CompletionRequest{
		Messages: []llm.Message{{Role: "user", Content: "test"}},
	})

	return err == nil
}

// Stream implements streaming generation
func (p *HuggingFaceProvider) Stream(ctx context.Context, prompt string) (<-chan string, error) {
	tokens := make(chan string, 100)

	// Build streaming request
	req := &HuggingFaceRequest{
		Inputs: prompt,
		Parameters: HuggingFaceParameters{
			Temperature:    p.temperature,
			MaxNewTokens:   p.maxTokens,
			ReturnFullText: false,
		},
		Options: HuggingFaceOptions{
			UseCache:     false,
			WaitForModel: true,
		},
	}

	// Create HTTP request
	body, err := json.Marshal(req)
	if err != nil {
		return nil, agentErrors.Wrap(err, agentErrors.CodeInternal, "failed to marshal request")
	}

	endpoint := fmt.Sprintf("%s/models/%s", p.baseURL, p.model)
	httpReq, err := http.NewRequestWithContext(ctx, "POST", endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, agentErrors.NewLLMRequestError("huggingface", p.model, err)
	}

	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+p.apiKey)
	httpReq.Header.Set("Accept", "text/event-stream")

	// Execute request
	httpResp, err := p.httpClient.Do(httpReq)
	if err != nil {
		return nil, agentErrors.NewLLMRequestError("huggingface", p.model, err)
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

			// Skip empty lines
			if strings.TrimSpace(line) == "" {
				continue
			}

			// Parse Hugging Face stream format
			var streamResp HuggingFaceStreamResponse
			if err := json.Unmarshal([]byte(line), &streamResp); err != nil {
				continue
			}

			// Extract text from token
			if streamResp.Token.Text != "" && !streamResp.Token.Special {
				tokens <- streamResp.Token.Text
			}

			// Stop if we have details (final event)
			if streamResp.Details != nil {
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
func (p *HuggingFaceProvider) ModelName() string {
	return p.model
}

// MaxTokens returns the max tokens setting
func (p *HuggingFaceProvider) MaxTokens() int {
	return p.maxTokens
}
