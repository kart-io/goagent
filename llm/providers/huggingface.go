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

// HuggingFaceProvider implements LLM interface for Hugging Face
type HuggingFaceProvider struct {
	*BaseProvider
	client  *httpclient.Client
	apiKey  string
	baseURL string
}

// HuggingFaceRequest represents a request to Hugging Face API
type HuggingFaceRequest struct {
	Inputs     string                `json:"inputs"`
	Parameters HuggingFaceParameters `json:"parameters,omitempty"`
	Options    HuggingFaceOptions    `json:"options,omitempty"`
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
	GeneratedText string              `json:"generated_text"`
	Details       *HuggingFaceDetails `json:"details,omitempty"`
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

// NewHuggingFaceWithOptions creates a new Hugging Face provider using options pattern
func NewHuggingFaceWithOptions(opts ...agentllm.ClientOption) (*HuggingFaceProvider, error) {
	// 创建 BaseProvider，统一处理 Options
	base := NewBaseProvider(opts...)

	// 应用 Provider 特定的默认值
	base.ApplyProviderDefaults(
		constants.ProviderHuggingFace,
		constants.HuggingFaceBaseURL,
		constants.HuggingFaceDefaultModel,
		constants.EnvHuggingFaceBaseURL,
		constants.EnvHuggingFaceModel,
	)

	// 统一处理 API Key
	if err := base.EnsureAPIKey(constants.EnvHuggingFaceAPIKey, constants.ProviderHuggingFace); err != nil {
		return nil, err
	}

	// 设置超时时间，HuggingFace 默认需要更长的超时
	timeout := base.GetTimeout()
	if timeout == constants.DefaultTimeout {
		timeout = constants.HuggingFaceTimeout
	}

	// 使用 BaseProvider 的 NewHTTPClient 方法创建 HTTP 客户端
	client := base.NewHTTPClient(HTTPClientConfig{
		Timeout: timeout,
		Headers: map[string]string{
			constants.HeaderContentType:   constants.ContentTypeJSON,
			constants.HeaderAuthorization: constants.AuthBearerPrefix + base.Config.APIKey,
		},
		BaseURL: base.Config.BaseURL,
	})

	provider := &HuggingFaceProvider{
		BaseProvider: base,
		client:       client,
		apiKey:       base.Config.APIKey,
		baseURL:      base.Config.BaseURL,
	}

	return provider, nil
}

// NewHuggingFace creates a new Hugging Face provider (backward compatible)
func NewHuggingFace(config *agentllm.LLMOptions) (*HuggingFaceProvider, error) {
	// 将现有配置转换为 Options，使用 Options 模式创建 Provider
	return NewHuggingFaceWithOptions(ConfigToOptions(config)...)
}

// Complete implements basic text completion
func (p *HuggingFaceProvider) Complete(ctx context.Context, req *agentllm.CompletionRequest) (*agentllm.CompletionResponse, error) {
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

// buildRequest converts agentllm.CompletionRequest to HuggingFaceRequest
func (p *HuggingFaceProvider) buildRequest(req *agentllm.CompletionRequest) *HuggingFaceRequest {
	// Combine all messages into a single input string
	var inputs strings.Builder
	for _, msg := range req.Messages {
		switch msg.Role {
		case constants.RoleSystem:
			inputs.WriteString(fmt.Sprintf("System: %s\n", msg.Content))
		case constants.RoleUser:
			inputs.WriteString(fmt.Sprintf("User: %s\n", msg.Content))
		case constants.RoleAssistant:
			inputs.WriteString(fmt.Sprintf("Assistant: %s\n", msg.Content))
		}
	}
	inputs.WriteString("Assistant: ") // Prompt for response

	// 使用 BaseProvider 的统一参数处理方法
	maxTokens := p.GetMaxTokens(req.MaxTokens)
	temperature := p.GetTemperature(req.Temperature)

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
	model := p.GetModel("")
	// Create HTTP request with model ID in URL
	endpoint := fmt.Sprintf("%s/models/%s", p.baseURL, model)

	// Execute request using resty
	resp, err := p.client.R().
		SetContext(ctx).
		SetBody(req).
		Post(endpoint)

	if err != nil {
		return nil, agentErrors.NewLLMRequestError(string(constants.ProviderHuggingFace), model, err)
	}

	// Check status code
	if !resp.IsSuccess() {
		return nil, p.handleHTTPError(resp, model)
	}

	// Deserialize response (array format)
	var respArray []HuggingFaceResponse
	if err := json.NewDecoder(strings.NewReader(resp.String())).Decode(&respArray); err != nil {
		return nil, agentErrors.NewLLMResponseError(string(constants.ProviderHuggingFace), model, constants.ErrFailedDecodeResponse)
	}

	if len(respArray) == 0 {
		return nil, agentErrors.NewLLMResponseError(string(constants.ProviderHuggingFace), model, constants.ErrEmptyResponseArray)
	}

	return &respArray[0], nil
}

// handleHTTPError maps HTTP errors to AgentError
func (p *HuggingFaceProvider) handleHTTPError(resp *resty.Response, model string) error {
	// Try to parse error response
	var errResp HuggingFaceErrorResponse
	if err := json.NewDecoder(strings.NewReader(resp.String())).Decode(&errResp); err == nil && errResp.Error != "" {
		// Use error message from API
		switch resp.StatusCode() {
		case 400:
			return agentErrors.NewInvalidInputError(string(constants.ProviderHuggingFace), "request", errResp.Error)
		case 401:
			return agentErrors.NewInvalidConfigError(string(constants.ProviderHuggingFace), constants.ErrorFieldAPIKey, errResp.Error)
		case 403:
			return agentErrors.NewInvalidConfigError(string(constants.ProviderHuggingFace), constants.ErrorFieldAPIKey, errResp.Error)
		case 404:
			return agentErrors.NewLLMResponseError(string(constants.ProviderHuggingFace), model, errResp.Error)
		case 429:
			retryAfter := parseRetryAfter(resp.Header().Get("Retry-After"))
			return agentErrors.NewLLMRateLimitError(string(constants.ProviderHuggingFace), model, retryAfter)
		case 503:
			// Model is loading - this is retryable
			estimatedTime := int(errResp.EstimatedTime)
			if estimatedTime == 0 {
				estimatedTime = constants.HuggingFaceDefaultEstimatedTime
			}
			return agentErrors.NewLLMRequestError(string(constants.ProviderHuggingFace), model,
				fmt.Errorf("model loading (estimated time: %d seconds)", estimatedTime))
		case 500, 502, 504:
			return agentErrors.NewLLMRequestError(string(constants.ProviderHuggingFace), model, fmt.Errorf("server error: %s", errResp.Error))
		}
	}

	// Fallback error handling
	switch resp.StatusCode() {
	case 400:
		return agentErrors.NewInvalidInputError(string(constants.ProviderHuggingFace), "request", constants.StatusBadRequest)
	case 401:
		return agentErrors.NewInvalidConfigError(string(constants.ProviderHuggingFace), constants.ErrorFieldAPIKey, constants.StatusInvalidAPIKey)
	case 403:
		return agentErrors.NewInvalidConfigError(string(constants.ProviderHuggingFace), constants.ErrorFieldAPIKey, constants.StatusAPIKeyLacksPermissions)
	case 404:
		return agentErrors.NewLLMResponseError(string(constants.ProviderHuggingFace), model, constants.StatusModelNotFound)
	case 429:
		retryAfter := parseRetryAfter(resp.Header().Get("Retry-After"))
		return agentErrors.NewLLMRateLimitError(string(constants.ProviderHuggingFace), model, retryAfter)
	case 503:
		return agentErrors.NewLLMRequestError(string(constants.ProviderHuggingFace), model, fmt.Errorf("model loading"))
	case 500, 502, 504:
		return agentErrors.NewLLMRequestError(string(constants.ProviderHuggingFace), model, fmt.Errorf("server error: %d", resp.StatusCode()))
	default:
		return agentErrors.NewLLMRequestError(string(constants.ProviderHuggingFace), model, fmt.Errorf("unexpected status: %d", resp.StatusCode()))
	}
}

// executeWithRetry executes request with extended retry for model loading
func (p *HuggingFaceProvider) executeWithRetry(ctx context.Context, req *HuggingFaceRequest) (*HuggingFaceResponse, error) {
	maxAttempts := constants.HuggingFaceMaxAttempts
	baseDelay := constants.HuggingFaceBaseDelay

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

		// Exponential backoff with jitter (longer delays for model loading)
		delay := baseDelay * time.Duration(1<<uint(attempt-1))
		// Cap at 60 seconds
		if delay > constants.HuggingFaceMaxDelay {
			delay = constants.HuggingFaceMaxDelay
		}
		jitter := time.Duration(rand.Int63n(int64(delay) / 2))

		select {
		case <-ctx.Done():
			return nil, agentErrors.NewContextCanceledError("llm_request")
		case <-time.After(delay + jitter):
			// Continue to next attempt
		}
	}

	return nil, agentErrors.NewInternalError(string(constants.ProviderHuggingFace), "execute_with_retry", fmt.Errorf("%s", constants.ErrMaxRetriesExceeded))
}

// convertResponse converts HuggingFaceResponse to agentllm.CompletionResponse
func (p *HuggingFaceProvider) convertResponse(resp *HuggingFaceResponse) *agentllm.CompletionResponse {
	// Estimate token usage (HF doesn't always provide it)
	var promptTokens, completionTokens int
	if resp.Details != nil {
		completionTokens = resp.Details.GeneratedTokens
		// Rough estimate for prompt tokens (4 chars per token)
		promptTokens = len(resp.GeneratedText) / 4
	}

	finishReason := constants.StatusComplete
	if resp.Details != nil && resp.Details.FinishReason != "" {
		finishReason = resp.Details.FinishReason
	}

	return &agentllm.CompletionResponse{
		Content:      resp.GeneratedText,
		Model:        p.GetModel(""),
		TokensUsed:   promptTokens + completionTokens,
		FinishReason: finishReason,
		Provider:     string(constants.ProviderHuggingFace),
		Usage: &interfaces.TokenUsage{
			PromptTokens:     promptTokens,
			CompletionTokens: completionTokens,
			TotalTokens:      promptTokens + completionTokens,
		},
	}
}

// Chat implements chat conversation
func (p *HuggingFaceProvider) Chat(ctx context.Context, messages []agentllm.Message) (*agentllm.CompletionResponse, error) {
	return p.Complete(ctx, &agentllm.CompletionRequest{
		Messages: messages,
	})
}

// Provider returns the provider type
func (p *HuggingFaceProvider) Provider() constants.Provider {
	return constants.ProviderHuggingFace
}

// IsAvailable checks if the provider is available
func (p *HuggingFaceProvider) IsAvailable() bool {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Try a minimal completion
	_, err := p.Complete(ctx, &agentllm.CompletionRequest{
		Messages: []agentllm.Message{{Role: constants.RoleUser, Content: "test"}},
	})

	return err == nil
}

// Stream implements streaming generation
func (p *HuggingFaceProvider) Stream(ctx context.Context, prompt string) (<-chan string, error) {
	tokens := make(chan string, 100)

	model := p.GetModel("")
	maxTokens := p.GetMaxTokens(0)
	temperature := p.GetTemperature(0)

	// Build streaming request
	req := &HuggingFaceRequest{
		Inputs: prompt,
		Parameters: HuggingFaceParameters{
			Temperature:    temperature,
			MaxNewTokens:   maxTokens,
			ReturnFullText: false,
		},
		Options: HuggingFaceOptions{
			UseCache:     false,
			WaitForModel: true,
		},
	}

	endpoint := fmt.Sprintf("%s/models/%s", p.baseURL, model)

	// Create streaming request with Accept header
	streamClient := p.client.R().
		SetContext(ctx).
		SetHeader(constants.HeaderAccept, constants.ContentTypeEventStream).
		SetBody(req)

	// Execute streaming request
	resp, err := streamClient.Post(endpoint)
	if err != nil {
		return nil, agentErrors.NewLLMRequestError(string(constants.ProviderHuggingFace), model, err)
	}

	if !resp.IsSuccess() {
		return nil, p.handleHTTPError(resp, model)
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

			// Parse Hugging Face stream format
			var streamResp HuggingFaceStreamResponse
			if err := json.Unmarshal([]byte(line), &streamResp); err != nil {
				continue
			}

			// Extract text from token
			if streamResp.Token.Text != "" && !streamResp.Token.Special {
				// Use select to handle context cancellation
				select {
				case tokens <- streamResp.Token.Text:
					// Successfully sent
				case <-ctx.Done():
					// Context cancelled, exit immediately
					return
				}
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
	return p.GetModel("")
}

// MaxTokens returns the max tokens setting
func (p *HuggingFaceProvider) MaxTokens() int {
	return p.GetMaxTokens(0)
}
