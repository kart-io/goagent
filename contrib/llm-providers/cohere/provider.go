package cohere

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/kart-io/goagent/llm/constants"
	"github.com/kart-io/goagent/utils/json"

	"github.com/go-resty/resty/v2"

	agentErrors "github.com/kart-io/goagent/errors"
	"github.com/kart-io/goagent/interfaces"
	agentllm "github.com/kart-io/goagent/llm"
	"github.com/kart-io/goagent/llm/common"
	"github.com/kart-io/goagent/llm/registry"
	"github.com/kart-io/goagent/utils/httpclient"
)

func init() {
	// 自动注册 Cohere provider 到全局注册表
	registry.Register(constants.ProviderCohere, func(opts ...agentllm.ClientOption) (agentllm.Client, error) {
		return New(opts...)
	})
}

// Provider implements LLM interface for Cohere
type Provider struct {
	*common.BaseProvider
	*common.ProviderCapabilities
	client  *httpclient.Client
	apiKey  string
	baseURL string
}

// Request represents a request to Cohere API
type Request struct {
	Model            string    `json:"model"`
	Message          string    `json:"message"`
	ChatHistory      []Message `json:"chat_history,omitempty"`
	Temperature      float64   `json:"temperature,omitempty"`
	MaxTokens        int       `json:"max_tokens,omitempty"`
	P                float64   `json:"p,omitempty"` // Top-p
	K                int       `json:"k,omitempty"` // Top-k
	Stream           bool      `json:"stream,omitempty"`
	StopSequences    []string  `json:"stop_sequences,omitempty"`
	PresencePenalty  float64   `json:"presence_penalty,omitempty"`
	FrequencyPenalty float64   `json:"frequency_penalty,omitempty"`
}

// Message represents a message in Cohere format
type Message struct {
	Role    string `json:"role"` // "USER", "CHATBOT", "SYSTEM"
	Message string `json:"message"`
}

// Response represents a response from Cohere API
type Response struct {
	ResponseID   string    `json:"response_id"`
	Text         string    `json:"text"`
	GenerationID string    `json:"generation_id"`
	FinishReason string    `json:"finish_reason"`
	TokenCount   Tokens    `json:"token_count"`
	ChatHistory  []Message `json:"chat_history,omitempty"`
}

// Tokens represents token usage
type Tokens struct {
	PromptTokens   int `json:"prompt_tokens"`
	ResponseTokens int `json:"response_tokens"`
	TotalTokens    int `json:"total_tokens"`
	BilledTokens   int `json:"billed_tokens,omitempty"`
}

// StreamEvent represents a streaming event
type StreamEvent struct {
	EventType    string    `json:"event_type"` // "stream-start", "text-generation", "stream-end"
	Text         string    `json:"text,omitempty"`
	FinishReason string    `json:"finish_reason,omitempty"`
	Response     *Response `json:"response,omitempty"`
}

// ErrorResponse represents an error response
type ErrorResponse struct {
	Message string `json:"message"`
}

// New creates a new Cohere provider using options pattern
func New(opts ...agentllm.ClientOption) (*Provider, error) {
	// 创建 BaseProvider，统一处理 Options
	base := common.NewBaseProvider(opts...)

	// 应用 Provider 特定的默认值
	base.ApplyProviderDefaults(
		constants.ProviderCohere,
		constants.CohereBaseURL,
		constants.CohereDefaultModel,
		constants.EnvCohereBaseURL,
		constants.EnvCohereModel,
	)

	// 统一处理 API Key
	if err := base.EnsureAPIKey(constants.EnvCohereAPIKey, constants.ProviderCohere); err != nil {
		return nil, err
	}

	// 使用 BaseProvider 的 NewHTTPClient 方法创建 HTTP 客户端
	client := base.NewHTTPClient(common.HTTPClientConfig{
		Timeout: base.GetTimeout(),
		Headers: map[string]string{
			constants.HeaderContentType:   constants.ContentTypeJSON,
			constants.HeaderAuthorization: constants.AuthBearerPrefix + base.Config.APIKey,
		},
		BaseURL: base.Config.BaseURL,
	})

	provider := &Provider{
		BaseProvider: base,
		ProviderCapabilities: common.NewProviderCapabilities(
			agentllm.CapabilityCompletion,
			agentllm.CapabilityChat,
			agentllm.CapabilityStreaming,
		),
		client:  client,
		apiKey:  base.Config.APIKey,
		baseURL: base.Config.BaseURL,
	}

	return provider, nil
}

// Complete implements basic text completion
func (p *Provider) Complete(ctx context.Context, req *agentllm.CompletionRequest) (*agentllm.CompletionResponse, error) {
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

// buildRequest converts agentllm.CompletionRequest to Request
func (p *Provider) buildRequest(req *agentllm.CompletionRequest) *Request {
	// Convert messages to Cohere format
	// Last user message becomes the message field
	// Previous messages become chat history
	var message string
	var chatHistory []Message

	for _, msg := range req.Messages {
		cohereRole := p.convertRole(msg.Role)

		if msg.Role == "user" && message == "" {
			// Use the last user message as the main message
			message = msg.Content
		} else {
			// Add to chat history
			chatHistory = append(chatHistory, Message{
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

	// 使用 BaseProvider 的统一参数处理方法
	model := p.GetModel(req.Model)
	maxTokens := p.GetMaxTokens(req.MaxTokens)
	temperature := p.GetTemperature(req.Temperature)

	return &Request{
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
func (p *Provider) convertRole(role string) string {
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
func (p *Provider) execute(ctx context.Context, req *Request) (*Response, error) {
	// Execute request using resty
	resp, err := p.client.R().
		SetContext(ctx).
		SetBody(req).
		Post(p.baseURL + constants.CohereChatPath)

	model := p.GetModel("")
	if err != nil {
		return nil, agentErrors.NewLLMRequestError(p.ProviderName(), model, err)
	}

	// Check status code
	if !resp.IsSuccess() {
		return nil, p.handleHTTPError(resp, req.Model)
	}

	// Deserialize response
	var cohereResp Response
	if err := json.NewDecoder(strings.NewReader(resp.String())).Decode(&cohereResp); err != nil {
		return nil, agentErrors.NewLLMResponseError(p.ProviderName(), req.Model, constants.ErrFailedDecodeResponse)
	}

	return &cohereResp, nil
}

// handleHTTPError maps HTTP errors to AgentError
func (p *Provider) handleHTTPError(resp *resty.Response, model string) error {
	// Try to parse error response
	var errResp ErrorResponse
	if err := json.NewDecoder(strings.NewReader(resp.String())).Decode(&errResp); err == nil && errResp.Message != "" {
		// Use error message from API
		switch resp.StatusCode() {
		case 400:
			return agentErrors.NewInvalidInputError(p.ProviderName(), "request", errResp.Message)
		case 401:
			return agentErrors.NewInvalidConfigError(p.ProviderName(), constants.ErrorFieldAPIKey, errResp.Message)
		case 403:
			return agentErrors.NewInvalidConfigError(p.ProviderName(), constants.ErrorFieldAPIKey, errResp.Message)
		case 404:
			return agentErrors.NewLLMResponseError(p.ProviderName(), model, errResp.Message)
		case 429:
			retryAfter := common.ParseRetryAfter(resp.Header().Get("Retry-After"))
			return agentErrors.NewLLMRateLimitError(p.ProviderName(), model, retryAfter)
		case 500, 502, 503, 504:
			return agentErrors.NewLLMRequestError(p.ProviderName(), model, fmt.Errorf("server error: %s", errResp.Message))
		}
	}

	// Fallback error handling
	switch resp.StatusCode() {
	case 400:
		return agentErrors.NewInvalidInputError(p.ProviderName(), "request", constants.StatusBadRequest)
	case 401:
		return agentErrors.NewInvalidConfigError(p.ProviderName(), constants.ErrorFieldAPIKey, constants.StatusInvalidAPIKey)
	case 403:
		return agentErrors.NewInvalidConfigError(p.ProviderName(), constants.ErrorFieldAPIKey, constants.StatusAPIKeyLacksPermissions)
	case 404:
		return agentErrors.NewLLMResponseError(p.ProviderName(), model, constants.StatusEndpointNotFound)
	case 429:
		retryAfter := common.ParseRetryAfter(resp.Header().Get("Retry-After"))
		return agentErrors.NewLLMRateLimitError(p.ProviderName(), model, retryAfter)
	case 500, 502, 503, 504:
		return agentErrors.NewLLMRequestError(p.ProviderName(), model, fmt.Errorf("server error: %d", resp.StatusCode()))
	default:
		return agentErrors.NewLLMRequestError(p.ProviderName(), model, fmt.Errorf("unexpected status: %d", resp.StatusCode()))
	}
}

// executeWithRetry executes request with exponential backoff using the shared retry logic
func (p *Provider) executeWithRetry(ctx context.Context, req *Request) (*Response, error) {
	return common.ExecuteWithRetry(ctx, common.DefaultRetryConfig(), p.ProviderName(), func(ctx context.Context) (*Response, error) {
		return p.execute(ctx, req)
	})
}

// convertResponse converts Response to agentllm.CompletionResponse
func (p *Provider) convertResponse(resp *Response) *agentllm.CompletionResponse {
	return &agentllm.CompletionResponse{
		Content:      resp.Text,
		Model:        p.GetModel(""), // Cohere doesn't return model in response
		TokensUsed:   resp.TokenCount.TotalTokens,
		FinishReason: resp.FinishReason,
		Provider:     p.ProviderName(),
		Usage: &interfaces.TokenUsage{
			PromptTokens:     resp.TokenCount.PromptTokens,
			CompletionTokens: resp.TokenCount.ResponseTokens,
			TotalTokens:      resp.TokenCount.TotalTokens,
		},
	}
}

// Chat implements chat conversation
func (p *Provider) Chat(ctx context.Context, messages []agentllm.Message) (*agentllm.CompletionResponse, error) {
	return p.Complete(ctx, &agentllm.CompletionRequest{
		Messages: messages,
	})
}

// Provider returns the provider type
func (p *Provider) Provider() constants.Provider {
	return constants.ProviderCohere
}

// ProviderName returns the provider name as a string
func (p *Provider) ProviderName() string {
	return string(constants.ProviderCohere)
}

// IsAvailable checks if the provider is available
func (p *Provider) IsAvailable() bool {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Try a minimal completion
	_, err := p.Complete(ctx, &agentllm.CompletionRequest{
		Messages: []agentllm.Message{{Role: constants.RoleUser, Content: "test"}},
	})

	return err == nil
}

// Stream implements streaming generation
func (p *Provider) Stream(ctx context.Context, prompt string) (<-chan string, error) {
	tokens := make(chan string, 100)

	model := p.GetModel("")
	maxTokens := p.GetMaxTokens(0)
	temperature := p.GetTemperature(0)

	// Build streaming request
	req := &Request{
		Model:       model,
		Message:     prompt,
		Temperature: temperature,
		MaxTokens:   maxTokens,
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
		return nil, agentErrors.NewLLMRequestError(p.ProviderName(), model, err)
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

			// Parse Cohere SSE format
			var event StreamEvent
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
func (p *Provider) ModelName() string {
	return p.GetModel("")
}

// MaxTokens returns the max tokens setting
func (p *Provider) MaxTokens() int {
	return p.GetMaxTokens(0)
}
