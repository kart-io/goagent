package anthropic

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
	// 自动注册 Anthropic provider 到全局注册表
	registry.Register(constants.ProviderAnthropic, func(opts ...agentllm.ClientOption) (agentllm.Client, error) {
		return New(opts...)
	})
}

// Provider implements LLM interface for Anthropic Claude
type Provider struct {
	*common.BaseProvider
	*common.ProviderCapabilities
	client  *httpclient.Client
	apiKey  string
	baseURL string
}

// Request represents a request to Anthropic API
type Request struct {
	Model         string    `json:"model"`
	Messages      []Message `json:"messages"`
	MaxTokens     int       `json:"max_tokens"`
	Temperature   float64   `json:"temperature,omitempty"`
	TopP          float64   `json:"top_p,omitempty"`
	TopK          int       `json:"top_k,omitempty"`
	Stream        bool      `json:"stream,omitempty"`
	StopSequences []string  `json:"stop_sequences,omitempty"`
	System        string    `json:"system,omitempty"`
}

// Message represents a message in Anthropic format
type Message struct {
	Role    string `json:"role"` // "user" or "assistant"
	Content string `json:"content"`
}

// Response represents a response from Anthropic API
type Response struct {
	ID           string    `json:"id"`
	Type         string    `json:"type"`
	Role         string    `json:"role"`
	Content      []Content `json:"content"`
	Model        string    `json:"model"`
	StopReason   string    `json:"stop_reason"`
	StopSequence string    `json:"stop_sequence,omitempty"`
	Usage        Usage     `json:"usage"`
}

// Content represents content in response
type Content struct {
	Type string `json:"type"` // "text"
	Text string `json:"text"`
}

// Usage represents token usage
type Usage struct {
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
}

// StreamEvent represents a streaming event
type StreamEvent struct {
	Type         string    `json:"type"`
	Message      *Response `json:"message,omitempty"`
	Index        int       `json:"index,omitempty"`
	Delta        *Delta    `json:"delta,omitempty"`
	ContentBlock *Content  `json:"content_block,omitempty"`
	Usage        *Usage    `json:"usage,omitempty"`
}

// Delta represents a streaming delta
type Delta struct {
	Type string `json:"type"` // "text_delta"
	Text string `json:"text"`
}

// ErrorResponse represents an error response
type ErrorResponse struct {
	Type  string       `json:"type"` // "error"
	Error ErrorDetails `json:"error"`
}

// ErrorDetails represents error details
type ErrorDetails struct {
	Type    string `json:"type"`
	Message string `json:"message"`
}

// New creates a new Anthropic provider using options pattern.
func New(opts ...agentllm.ClientOption) (*Provider, error) {
	// Create BaseProvider with unified options handling
	base := common.NewBaseProvider(opts...)

	// Apply provider-specific default values
	base.ApplyProviderDefaults(
		constants.ProviderAnthropic,
		constants.AnthropicBaseURL,
		constants.AnthropicDefaultModel,
		constants.EnvAnthropicBaseURL,
		constants.EnvAnthropicModel,
	)

	// Validate API key
	if err := base.EnsureAPIKey(constants.EnvAnthropicAPIKey, constants.ProviderAnthropic); err != nil {
		return nil, err
	}

	// Create HTTP client using BaseProvider helper
	client := base.NewHTTPClient(common.HTTPClientConfig{
		Headers: map[string]string{
			constants.HeaderContentType:      constants.ContentTypeJSON,
			constants.HeaderXAPIKey:          base.Config.APIKey,
			constants.HeaderAnthropicVersion: constants.AnthropicAPIVersion,
		},
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

// Complete implements basic text completion.
func (p *Provider) Complete(ctx context.Context, req *agentllm.CompletionRequest) (*agentllm.CompletionResponse, error) {
	// Build Anthropic request
	anthropicReq := p.buildRequest(req)

	// Execute with retry using shared retry logic
	retryCfg := common.RetryConfig{
		MaxAttempts: constants.AnthropicMaxAttempts,
		BaseDelay:   constants.AnthropicBaseDelay,
		MaxDelay:    constants.AnthropicMaxDelay,
	}

	resp, err := common.ExecuteWithRetry(ctx, retryCfg, p.ProviderName(), func(ctx context.Context) (*Response, error) {
		return p.execute(ctx, anthropicReq)
	})
	if err != nil {
		return nil, err
	}

	// Convert to standard response
	return p.convertResponse(resp), nil
}

// buildRequest converts agentllm.CompletionRequest to Request.
func (p *Provider) buildRequest(req *agentllm.CompletionRequest) *Request {
	// Separate system message from other messages
	var systemMsg string
	var messages []Message

	for _, msg := range req.Messages {
		if msg.Role == constants.RoleSystem {
			systemMsg = msg.Content
		} else {
			messages = append(messages, Message{
				Role:    msg.Role,
				Content: msg.Content,
			})
		}
	}

	// Use BaseProvider's unified parameter handling
	model := p.GetModel(req.Model)
	maxTokens := p.GetMaxTokens(req.MaxTokens)
	temperature := p.GetTemperature(req.Temperature)

	return &Request{
		Model:         model,
		Messages:      messages,
		MaxTokens:     maxTokens,
		Temperature:   temperature,
		TopP:          req.TopP,
		StopSequences: req.Stop,
		System:        systemMsg,
	}
}

// execute performs a single HTTP request to Anthropic API.
func (p *Provider) execute(ctx context.Context, req *Request) (*Response, error) {
	resp, err := p.client.R().
		SetContext(ctx).
		SetBody(req).
		Post(p.baseURL + constants.AnthropicMessagesPath)

	if err != nil {
		return nil, agentErrors.NewLLMRequestError(p.ProviderName(), req.Model, err)
	}

	// Check status code using shared error handling
	if !resp.IsSuccess() {
		return nil, p.handleHTTPError(resp, req.Model)
	}

	// Deserialize response
	var anthropicResp Response
	if err := json.NewDecoder(strings.NewReader(resp.String())).Decode(&anthropicResp); err != nil {
		return nil, agentErrors.NewLLMResponseError(p.ProviderName(), req.Model, constants.ErrFailedDecodeResponse)
	}

	return &anthropicResp, nil
}

// handleHTTPError maps HTTP errors to AgentError using the shared MapHTTPError function.
func (p *Provider) handleHTTPError(resp *resty.Response, model string) error {
	httpErr := common.RestyResponseToHTTPError(resp)
	return common.MapHTTPError(httpErr, p.ProviderName(), model, p.parseErrorMessage)
}

// parseErrorMessage extracts error message from Anthropic error response body.
func (p *Provider) parseErrorMessage(body string) string {
	var errResp ErrorResponse
	if err := json.NewDecoder(strings.NewReader(body)).Decode(&errResp); err == nil {
		return errResp.Error.Message
	}
	return ""
}

// convertResponse converts Response to agentllm.CompletionResponse.
func (p *Provider) convertResponse(resp *Response) *agentllm.CompletionResponse {
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
		Provider:     p.ProviderName(),
		Usage: &interfaces.TokenUsage{
			PromptTokens:     resp.Usage.InputTokens,
			CompletionTokens: resp.Usage.OutputTokens,
			TotalTokens:      resp.Usage.InputTokens + resp.Usage.OutputTokens,
		},
	}
}

// Chat implements chat conversation.
func (p *Provider) Chat(ctx context.Context, messages []agentllm.Message) (*agentllm.CompletionResponse, error) {
	return p.Complete(ctx, &agentllm.CompletionRequest{
		Messages: messages,
	})
}

// Provider returns the provider type.
func (p *Provider) Provider() constants.Provider {
	return constants.ProviderAnthropic
}

// ProviderName returns the provider name as a string
func (p *Provider) ProviderName() string {
	return string(constants.ProviderAnthropic)
}

// IsAvailable checks if the provider is available.
func (p *Provider) IsAvailable() bool {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Try a minimal completion
	_, err := p.Complete(ctx, &agentllm.CompletionRequest{
		Messages: []agentllm.Message{{Role: constants.RoleUser, Content: "test"}},
	})

	return err == nil
}

// Stream implements streaming generation.
func (p *Provider) Stream(ctx context.Context, prompt string) (<-chan string, error) {
	tokens := make(chan string, 100)

	model := p.GetModel("")
	maxTokens := p.GetMaxTokens(0)
	temperature := p.GetTemperature(0)

	// Build streaming request
	req := &Request{
		Model:       model,
		Messages:    []Message{{Role: constants.RoleUser, Content: prompt}},
		MaxTokens:   maxTokens,
		Temperature: temperature,
		Stream:      true,
	}

	// Create custom client for streaming with Accept header
	streamClient := p.client.R().
		SetContext(ctx).
		SetHeader(constants.HeaderAccept, constants.AcceptEventStream).
		SetBody(req)

	// Execute streaming request
	resp, err := streamClient.Post(p.baseURL + constants.AnthropicMessagesPath)
	if err != nil {
		return nil, agentErrors.NewLLMRequestError(p.ProviderName(), model, err)
	}

	if !resp.IsSuccess() {
		return nil, p.handleHTTPError(resp, model)
	}

	// Start goroutine to read stream
	go func() {
		defer close(tokens)

		// Use scanner to read SSE stream
		scanner := bufio.NewScanner(strings.NewReader(resp.String()))
		for scanner.Scan() {
			line := scanner.Text()

			// Parse SSE format: "data: {...}"
			if !strings.HasPrefix(line, constants.SSEDataPrefix) {
				continue
			}

			data := strings.TrimPrefix(line, constants.SSEDataPrefix)
			if data == constants.SSEDoneMessage {
				return
			}

			var event StreamEvent
			if err := json.Unmarshal([]byte(data), &event); err != nil {
				continue
			}

			// Extract text from content_block_delta events
			if event.Type == constants.EventContentBlockDelta && event.Delta != nil {
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
