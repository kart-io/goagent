package providers

import (
	"context"
	"strings"
	"time"

	"github.com/go-resty/resty/v2"

	agentErrors "github.com/kart-io/goagent/errors"
	agentllm "github.com/kart-io/goagent/llm"
	"github.com/kart-io/goagent/llm/common"
	"github.com/kart-io/goagent/llm/constants"
	"github.com/kart-io/goagent/utils/httpclient"
	"github.com/kart-io/goagent/utils/json"
)

// DeepSeekProvider implements LLM interface for DeepSeek
type DeepSeekProvider struct {
	*common.BaseProvider
	*common.ProviderCapabilities
	client  *httpclient.Client
	apiKey  string
	baseURL string
}

// NewDeepSeekWithOptions creates a new DeepSeek provider using options pattern
func NewDeepSeekWithOptions(opts ...agentllm.ClientOption) (*DeepSeekProvider, error) {
	// 创建 common.BaseProvider，统一处理 Options
	base := common.NewBaseProvider(opts...)

	// 应用 Provider 特定的默认值
	base.ApplyProviderDefaults(
		constants.ProviderDeepSeek,
		constants.DeepSeekBaseURL,
		constants.DeepSeekDefaultModel,
		constants.EnvDeepSeekBaseURL,
		constants.EnvDeepSeekModel,
	)

	// 统一处理 API Key
	if err := base.EnsureAPIKey(constants.EnvDeepSeekAPIKey, constants.ProviderDeepSeek); err != nil {
		return nil, err
	}

	// Use the common.BaseProvider's NewHTTPClient method
	client := base.NewHTTPClient(common.HTTPClientConfig{
		Timeout: base.GetTimeout(),
		Headers: map[string]string{
			"Content-Type":  "application/json",
			"Accept":        "application/json",
			"Authorization": "Bearer " + base.Config.APIKey,
		},
		BaseURL: base.Config.BaseURL,
	})

	provider := &DeepSeekProvider{
		BaseProvider: base,
		ProviderCapabilities: common.NewProviderCapabilities(
			agentllm.CapabilityCompletion,
			agentllm.CapabilityChat,
			agentllm.CapabilityStreaming,
			agentllm.CapabilityStructuredStreaming,
			agentllm.CapabilityToolStreaming,
			agentllm.CapabilityToolCalling,
		),
		client:  client,
		apiKey:  base.Config.APIKey,
		baseURL: base.Config.BaseURL,
	}

	return provider, nil
}

// Embed retains a legacy OpenAI-compatible embedding request for custom
// endpoints. It is not advertised as a verified DeepSeek embedding capability.
func (p *DeepSeekProvider) Embed(ctx context.Context, text string) ([]float32, error) {
	// DeepSeek embeddings API
	type EmbedRequest struct {
		Model string   `json:"model"`
		Input []string `json:"input"`
	}

	type EmbedResponse struct {
		Object string `json:"object"`
		Data   []struct {
			Object    string    `json:"object"`
			Embedding []float32 `json:"embedding"`
			Index     int       `json:"index"`
		} `json:"data"`
		Model string `json:"model"`
		Usage struct {
			PromptTokens int `json:"prompt_tokens"`
			TotalTokens  int `json:"total_tokens"`
		} `json:"usage"`
	}

	req := EmbedRequest{
		Model: "deepseek-embedding",
		Input: []string{text},
	}

	resp, err := p.callAPI(ctx, "/embeddings", req)
	if err != nil {
		return nil, agentErrors.NewRetrievalEmbeddingError(text, err).
			WithContext("provider", p.ProviderName())
	}

	var embedResp EmbedResponse
	if err := json.NewDecoder(strings.NewReader(resp.String())).Decode(&embedResp); err != nil {
		return nil, agentErrors.NewParserInvalidJSONError("embeddings response", err).
			WithContext("provider", p.ProviderName())
	}

	if len(embedResp.Data) == 0 {
		return nil, agentErrors.NewLLMResponseError(p.ProviderName(), "deepseek-embedding", "no embeddings in response")
	}

	return embedResp.Data[0].Embedding, nil
}

// Provider returns the provider type
func (p *DeepSeekProvider) Provider() constants.Provider {
	return constants.ProviderDeepSeek
}

// MaxTokens returns the max tokens setting
func (p *DeepSeekProvider) MaxTokens() int {
	return p.MaxTokensValue()
}

// Helper methods

// callAPI makes an API call to DeepSeek with retry logic
func (p *DeepSeekProvider) callAPI(ctx context.Context, endpoint string, payload interface{}) (*resty.Response, error) {
	url := p.baseURL + endpoint
	model := p.GetModel("")

	// Use the shared retry logic from common.BaseProvider
	retryConfig := common.RetryConfig{
		MaxAttempts: 3,
		BaseDelay:   1 * time.Second,
		MaxDelay:    10 * time.Second,
	}

	return common.ExecuteWithRetry(ctx, retryConfig, p.ProviderName(), func(ctx context.Context) (*resty.Response, error) {
		resp, err := p.client.R().
			SetContext(ctx).
			SetBody(payload).
			Post(url)
		if err != nil {
			return nil, agentErrors.NewLLMRequestError(p.ProviderName(), model, &agentllm.ProviderError{Err: err}).
				WithContext("endpoint", endpoint)
		}

		if !resp.IsSuccess() {
			// Use shared HTTP error mapping
			httpErr := common.RestyResponseToHTTPError(resp)
			return nil, common.MapHTTPError(httpErr, p.ProviderName(), model, func(body string) string {
				// Extract error message from DeepSeek response if possible
				var errorResp struct {
					Error struct {
						Message string `json:"message"`
						Type    string `json:"type"`
					} `json:"error"`
				}
				if err := json.Unmarshal([]byte(body), &errorResp); err == nil && errorResp.Error.Message != "" {
					return errorResp.Error.Message
				}
				return body
			})
		}

		return resp, nil
	})
}

// DeepSeekStreamingProvider extends DeepSeekProvider with advanced streaming
type DeepSeekStreamingProvider struct {
	*DeepSeekProvider
}

// NewDeepSeekStreamingWithOptions creates a streaming-optimized provider using options pattern
func NewDeepSeekStreamingWithOptions(opts ...agentllm.ClientOption) (*DeepSeekStreamingProvider, error) {
	base, err := NewDeepSeekWithOptions(opts...)
	if err != nil {
		return nil, err
	}

	return &DeepSeekStreamingProvider{
		DeepSeekProvider: base,
	}, nil
}

// NewDeepSeekStreaming creates a streaming-optimized provider
func NewDeepSeekStreaming(config *agentllm.LLMOptions) (*DeepSeekStreamingProvider, error) {
	base, err := NewDeepSeekWithOptions(common.ConfigToOptions(config)...)
	if err != nil {
		return nil, err
	}

	return &DeepSeekStreamingProvider{
		DeepSeekProvider: base,
	}, nil
}

// StreamWithMetadata streams tokens with additional metadata over the native SSE path.
// "finish" is sent only after a complete stream; a truncated stream ends with "error".
// 超时只约束拿到响应头，流开始后不再计时；消费者不再读取时必须取消 ctx，否则 goroutine 与连接不会释放。
func (p *DeepSeekStreamingProvider) StreamWithMetadata(ctx context.Context, prompt string) (<-chan TokenWithMetadata, error) {
	model := p.GetModel("")
	chunks, err := p.nativeStreamProvider().CompleteStream(ctx, &agentllm.CompletionRequest{Messages: []agentllm.Message{agentllm.UserMessage(prompt)}})
	if err != nil {
		return nil, err
	}

	tokens := make(chan TokenWithMetadata, 100)
	go func() {
		defer close(tokens)

		tokenCount := 0
		for chunk := range chunks {
			var token TokenWithMetadata
			switch {
			case chunk.Done && chunk.Error != nil:
				token = TokenWithMetadata{Type: "error", Error: chunk.Error}
			case chunk.Done:
				token = TokenWithMetadata{
					Type: "finish",
					Metadata: map[string]interface{}{
						"total_tokens": tokenCount,
						"model":        model,
					},
				}
			case chunk.Delta != "":
				tokenCount++
				token = TokenWithMetadata{
					Type:    "token",
					Content: chunk.Delta,
					Metadata: map[string]interface{}{
						"index":         tokenCount,
						"finish_reason": chunk.FinishReason,
					},
				}
			default:
				continue
			}
			select {
			case tokens <- token:
			case <-ctx.Done():
				return
			}
		}
	}()

	return tokens, nil
}
