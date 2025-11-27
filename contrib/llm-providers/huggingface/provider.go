package huggingface

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
	// 自动注册 HuggingFace provider 到全局注册表
	registry.Register(constants.ProviderHuggingFace, func(opts ...agentllm.ClientOption) (agentllm.Client, error) {
		return New(opts...)
	})
}

// Provider implements LLM interface for Hugging Face
type Provider struct {
	*common.BaseProvider
	*common.ProviderCapabilities
	client  *httpclient.Client
	apiKey  string
	baseURL string
}

// Request represents a request to Hugging Face API
type Request struct {
	Inputs     string     `json:"inputs"`
	Parameters Parameters `json:"parameters,omitempty"`
	Options    Options    `json:"options,omitempty"`
}

// Parameters represents request parameters
type Parameters struct {
	Temperature       float64  `json:"temperature,omitempty"`
	MaxNewTokens      int      `json:"max_new_tokens,omitempty"`
	TopP              float64  `json:"top_p,omitempty"`
	TopK              int      `json:"top_k,omitempty"`
	RepetitionPenalty float64  `json:"repetition_penalty,omitempty"`
	StopSequences     []string `json:"stop_sequences,omitempty"`
	ReturnFullText    bool     `json:"return_full_text,omitempty"`
}

// Options represents request options
type Options struct {
	UseCache     bool `json:"use_cache"`
	WaitForModel bool `json:"wait_for_model"`
}

// Response represents a response from Hugging Face API
type Response struct {
	GeneratedText string   `json:"generated_text"`
	Details       *Details `json:"details,omitempty"`
}

// Details represents generation details
type Details struct {
	FinishReason    string `json:"finish_reason"`
	GeneratedTokens int    `json:"generated_tokens"`
	Seed            int64  `json:"seed,omitempty"`
}

// StreamResponse represents a streaming response
type StreamResponse struct {
	Token         Token    `json:"token"`
	GeneratedText string   `json:"generated_text,omitempty"`
	Details       *Details `json:"details,omitempty"`
}

// Token represents a single token
type Token struct {
	ID      int     `json:"id"`
	Text    string  `json:"text"`
	LogProb float64 `json:"logprob"`
	Special bool    `json:"special"`
}

// ErrorResponse represents an error response
type ErrorResponse struct {
	Error         string  `json:"error"`
	EstimatedTime float64 `json:"estimated_time,omitempty"` // For model loading
}

// New creates a new Hugging Face provider using options pattern
func New(opts ...agentllm.ClientOption) (*Provider, error) {
	base := common.NewBaseProvider(opts...)

	base.ApplyProviderDefaults(
		constants.ProviderHuggingFace,
		constants.HuggingFaceBaseURL,
		constants.HuggingFaceDefaultModel,
		constants.EnvHuggingFaceBaseURL,
		constants.EnvHuggingFaceModel,
	)

	if err := base.EnsureAPIKey(constants.EnvHuggingFaceAPIKey, constants.ProviderHuggingFace); err != nil {
		return nil, err
	}

	timeout := base.GetTimeout()
	if timeout == constants.DefaultTimeout {
		timeout = constants.HuggingFaceTimeout
	}

	client := base.NewHTTPClient(common.HTTPClientConfig{
		Timeout: timeout,
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
	hfReq := p.buildRequest(req)
	resp, err := p.executeWithRetry(ctx, hfReq)
	if err != nil {
		return nil, err
	}
	return p.convertResponse(resp), nil
}

// buildRequest converts agentllm.CompletionRequest to Request
func (p *Provider) buildRequest(req *agentllm.CompletionRequest) *Request {
	inputs := common.MessagesToPrompt(req.Messages, common.DefaultPromptFormatter)
	inputs += "Assistant: "

	maxTokens := p.GetMaxTokens(req.MaxTokens)
	temperature := p.GetTemperature(req.Temperature)

	return &Request{
		Inputs: inputs,
		Parameters: Parameters{
			Temperature:    temperature,
			MaxNewTokens:   maxTokens,
			TopP:           req.TopP,
			StopSequences:  req.Stop,
			ReturnFullText: false,
		},
		Options: Options{
			UseCache:     false,
			WaitForModel: true,
		},
	}
}

// execute performs a single HTTP request to Hugging Face API
func (p *Provider) execute(ctx context.Context, req *Request) (*Response, error) {
	model := p.GetModel("")
	endpoint := fmt.Sprintf("%s/models/%s", p.baseURL, model)

	resp, err := p.client.R().
		SetContext(ctx).
		SetBody(req).
		Post(endpoint)

	if err != nil {
		return nil, agentErrors.NewLLMRequestError(p.ProviderName(), model, err)
	}

	if !resp.IsSuccess() {
		return nil, p.handleHTTPError(resp, model)
	}

	var respArray []Response
	if err := json.NewDecoder(strings.NewReader(resp.String())).Decode(&respArray); err != nil {
		return nil, agentErrors.NewLLMResponseError(p.ProviderName(), model, constants.ErrFailedDecodeResponse)
	}

	if len(respArray) == 0 {
		return nil, agentErrors.NewLLMResponseError(p.ProviderName(), model, constants.ErrEmptyResponseArray)
	}

	return &respArray[0], nil
}

// handleHTTPError maps HTTP errors to AgentError
func (p *Provider) handleHTTPError(resp *resty.Response, model string) error {
	var errResp ErrorResponse
	if err := json.NewDecoder(strings.NewReader(resp.String())).Decode(&errResp); err == nil && errResp.Error != "" {
		switch resp.StatusCode() {
		case 400:
			return agentErrors.NewInvalidInputError(p.ProviderName(), "request", errResp.Error)
		case 401:
			return agentErrors.NewInvalidConfigError(p.ProviderName(), constants.ErrorFieldAPIKey, errResp.Error)
		case 403:
			return agentErrors.NewInvalidConfigError(p.ProviderName(), constants.ErrorFieldAPIKey, errResp.Error)
		case 404:
			return agentErrors.NewLLMResponseError(p.ProviderName(), model, errResp.Error)
		case 429:
			retryAfter := common.ParseRetryAfter(resp.Header().Get("Retry-After"))
			return agentErrors.NewLLMRateLimitError(p.ProviderName(), model, retryAfter)
		case 503:
			estimatedTime := int(errResp.EstimatedTime)
			if estimatedTime == 0 {
				estimatedTime = constants.HuggingFaceDefaultEstimatedTime
			}
			return agentErrors.NewLLMRequestError(p.ProviderName(), model,
				fmt.Errorf("model loading (estimated time: %d seconds)", estimatedTime))
		case 500, 502, 504:
			return agentErrors.NewLLMRequestError(p.ProviderName(), model, fmt.Errorf("server error: %s", errResp.Error))
		}
	}

	switch resp.StatusCode() {
	case 400:
		return agentErrors.NewInvalidInputError(p.ProviderName(), "request", constants.StatusBadRequest)
	case 401:
		return agentErrors.NewInvalidConfigError(p.ProviderName(), constants.ErrorFieldAPIKey, constants.StatusInvalidAPIKey)
	case 403:
		return agentErrors.NewInvalidConfigError(p.ProviderName(), constants.ErrorFieldAPIKey, constants.StatusAPIKeyLacksPermissions)
	case 404:
		return agentErrors.NewLLMResponseError(p.ProviderName(), model, constants.StatusModelNotFound)
	case 429:
		retryAfter := common.ParseRetryAfter(resp.Header().Get("Retry-After"))
		return agentErrors.NewLLMRateLimitError(p.ProviderName(), model, retryAfter)
	case 503:
		return agentErrors.NewLLMRequestError(p.ProviderName(), model, fmt.Errorf("model loading"))
	case 500, 502, 504:
		return agentErrors.NewLLMRequestError(p.ProviderName(), model, fmt.Errorf("server error: %d", resp.StatusCode()))
	default:
		return agentErrors.NewLLMRequestError(p.ProviderName(), model, fmt.Errorf("unexpected status: %d", resp.StatusCode()))
	}
}

// executeWithRetry executes request with extended retry for model loading
func (p *Provider) executeWithRetry(ctx context.Context, req *Request) (*Response, error) {
	cfg := common.RetryConfig{
		MaxAttempts: constants.HuggingFaceMaxAttempts,
		BaseDelay:   constants.HuggingFaceBaseDelay,
		MaxDelay:    constants.HuggingFaceMaxDelay,
	}
	return common.ExecuteWithRetry(ctx, cfg, p.ProviderName(), func(ctx context.Context) (*Response, error) {
		return p.execute(ctx, req)
	})
}

// convertResponse converts Response to agentllm.CompletionResponse
func (p *Provider) convertResponse(resp *Response) *agentllm.CompletionResponse {
	var promptTokens, completionTokens int
	if resp.Details != nil {
		completionTokens = resp.Details.GeneratedTokens
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
		Provider:     p.ProviderName(),
		Usage: &interfaces.TokenUsage{
			PromptTokens:     promptTokens,
			CompletionTokens: completionTokens,
			TotalTokens:      promptTokens + completionTokens,
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
	return constants.ProviderHuggingFace
}

// ProviderName returns the provider name as a string
func (p *Provider) ProviderName() string {
	return string(constants.ProviderHuggingFace)
}

// IsAvailable checks if the provider is available
func (p *Provider) IsAvailable() bool {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

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

	req := &Request{
		Inputs: prompt,
		Parameters: Parameters{
			Temperature:    temperature,
			MaxNewTokens:   maxTokens,
			ReturnFullText: false,
		},
		Options: Options{
			UseCache:     false,
			WaitForModel: true,
		},
	}

	endpoint := fmt.Sprintf("%s/models/%s", p.baseURL, model)

	streamClient := p.client.R().
		SetContext(ctx).
		SetHeader(constants.HeaderAccept, constants.ContentTypeEventStream).
		SetBody(req)

	resp, err := streamClient.Post(endpoint)
	if err != nil {
		return nil, agentErrors.NewLLMRequestError(p.ProviderName(), model, err)
	}

	if !resp.IsSuccess() {
		return nil, p.handleHTTPError(resp, model)
	}

	go func() {
		defer close(tokens)

		scanner := bufio.NewScanner(strings.NewReader(resp.String()))
		for scanner.Scan() {
			line := scanner.Text()

			if strings.TrimSpace(line) == "" {
				continue
			}

			var streamResp StreamResponse
			if err := json.Unmarshal([]byte(line), &streamResp); err != nil {
				continue
			}

			if streamResp.Token.Text != "" && !streamResp.Token.Special {
				select {
				case tokens <- streamResp.Token.Text:
				case <-ctx.Done():
					return
				}
			}

			if streamResp.Details != nil {
				return
			}
		}

		if err := scanner.Err(); err != nil && err != io.EOF {
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
