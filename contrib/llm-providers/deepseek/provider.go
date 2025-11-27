package deepseek

import (
	"context"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/go-resty/resty/v2"

	agentErrors "github.com/kart-io/goagent/errors"
	"github.com/kart-io/goagent/interfaces"
	agentllm "github.com/kart-io/goagent/llm"
	"github.com/kart-io/goagent/llm/common"
	"github.com/kart-io/goagent/llm/constants"
	"github.com/kart-io/goagent/llm/registry"
	"github.com/kart-io/goagent/utils/httpclient"
	"github.com/kart-io/goagent/utils/json"
)

func init() {
	// 自动注册 DeepSeek provider 到全局注册表
	registry.Register(constants.ProviderDeepSeek, func(opts ...agentllm.ClientOption) (agentllm.Client, error) {
		return New(opts...)
	})
}

// Provider implements LLM interface for DeepSeek
type Provider struct {
	*common.BaseProvider
	*common.ProviderCapabilities
	client  *httpclient.Client
	apiKey  string
	baseURL string
}

// Request represents a request to DeepSeek API
type Request struct {
	Model       string      `json:"model"`
	Messages    []Message   `json:"messages"`
	Temperature float64     `json:"temperature,omitempty"`
	MaxTokens   int         `json:"max_tokens,omitempty"`
	TopP        float64     `json:"top_p,omitempty"`
	Stream      bool        `json:"stream,omitempty"`
	Tools       []Tool      `json:"tools,omitempty"`
	ToolChoice  interface{} `json:"tool_choice,omitempty"`
	Stop        []string    `json:"stop,omitempty"`
}

// Message represents a message in DeepSeek format
type Message struct {
	Role       string     `json:"role"`
	Content    string     `json:"content"`
	Name       string     `json:"name,omitempty"`
	ToolCalls  []ToolCall `json:"tool_calls,omitempty"`
	ToolCallID string     `json:"tool_call_id,omitempty"`
}

// Tool represents a tool in DeepSeek format
type Tool struct {
	Type     string   `json:"type"`
	Function Function `json:"function"`
}

// Function represents a function definition
type Function struct {
	Name        string                 `json:"name"`
	Description string                 `json:"description"`
	Parameters  map[string]interface{} `json:"parameters"`
}

// ToolCall represents a tool call
type ToolCall struct {
	ID       string       `json:"id"`
	Type     string       `json:"type"`
	Function FunctionCall `json:"function"`
}

// FunctionCall represents a function call
type FunctionCall struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

// Response represents a response from DeepSeek API
type Response struct {
	ID      string   `json:"id"`
	Object  string   `json:"object"`
	Created int64    `json:"created"`
	Model   string   `json:"model"`
	Choices []Choice `json:"choices"`
	Usage   Usage    `json:"usage"`
}

// Choice represents a choice in the response
type Choice struct {
	Index        int     `json:"index"`
	Message      Message `json:"message"`
	Delta        Message `json:"delta,omitempty"`
	FinishReason string  `json:"finish_reason"`
}

// Usage represents token usage
type Usage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

// StreamResponse represents a streaming response
type StreamResponse struct {
	ID      string   `json:"id"`
	Object  string   `json:"object"`
	Created int64    `json:"created"`
	Model   string   `json:"model"`
	Choices []Choice `json:"choices"`
}

// New creates a new DeepSeek provider using options pattern
func New(opts ...agentllm.ClientOption) (*Provider, error) {
	base := common.NewBaseProvider(opts...)

	base.ApplyProviderDefaults(
		constants.ProviderDeepSeek,
		constants.DeepSeekBaseURL,
		constants.DeepSeekDefaultModel,
		constants.EnvDeepSeekBaseURL,
		constants.EnvDeepSeekModel,
	)

	if err := base.EnsureAPIKey(constants.EnvDeepSeekAPIKey, constants.ProviderDeepSeek); err != nil {
		return nil, err
	}

	client := base.NewHTTPClient(common.HTTPClientConfig{
		Timeout: base.GetTimeout(),
		Headers: map[string]string{
			"Content-Type":  "application/json",
			"Accept":        "application/json",
			"Authorization": "Bearer " + base.Config.APIKey,
		},
		BaseURL: base.Config.BaseURL,
	})

	provider := &Provider{
		BaseProvider: base,
		ProviderCapabilities: common.NewProviderCapabilities(
			agentllm.CapabilityCompletion,
			agentllm.CapabilityChat,
			agentllm.CapabilityStreaming,
			agentllm.CapabilityToolCalling,
			agentllm.CapabilityEmbedding,
		),
		client:  client,
		apiKey:  base.Config.APIKey,
		baseURL: base.Config.BaseURL,
	}

	return provider, nil
}

// Complete implements basic text completion
func (p *Provider) Complete(ctx context.Context, req *agentllm.CompletionRequest) (*agentllm.CompletionResponse, error) {
	messages := common.ConvertMessages(req.Messages, func(msg agentllm.Message) Message {
		return Message{
			Role:    msg.Role,
			Content: msg.Content,
			Name:    msg.Name,
		}
	})

	model := p.GetModel(req.Model)
	dsReq := Request{
		Model:       model,
		Messages:    messages,
		Temperature: p.GetTemperature(req.Temperature),
		MaxTokens:   p.GetMaxTokens(req.MaxTokens),
		TopP:        req.TopP,
		Stop:        req.Stop,
		Stream:      false,
	}

	resp, err := p.callAPI(ctx, "/chat/completions", dsReq)
	if err != nil {
		return nil, agentErrors.NewLLMRequestError(p.ProviderName(), model, err)
	}

	var dsResp Response
	if err := json.NewDecoder(strings.NewReader(resp.String())).Decode(&dsResp); err != nil {
		return nil, agentErrors.NewParserInvalidJSONError("response body", err).
			WithContext("provider", p.ProviderName())
	}

	if len(dsResp.Choices) == 0 {
		return nil, agentErrors.NewLLMResponseError(p.ProviderName(), model, "no choices in response")
	}

	return &agentllm.CompletionResponse{
		Content:      dsResp.Choices[0].Message.Content,
		Model:        dsResp.Model,
		TokensUsed:   dsResp.Usage.TotalTokens,
		FinishReason: dsResp.Choices[0].FinishReason,
		Provider:     p.ProviderName(),
		Usage: &interfaces.TokenUsage{
			PromptTokens:     dsResp.Usage.PromptTokens,
			CompletionTokens: dsResp.Usage.CompletionTokens,
			TotalTokens:      dsResp.Usage.TotalTokens,
		},
	}, nil
}

// Chat implements chat conversation
func (p *Provider) Chat(ctx context.Context, messages []agentllm.Message) (*agentllm.CompletionResponse, error) {
	return p.Complete(ctx, &agentllm.CompletionRequest{
		Messages: messages,
	})
}

// Stream implements streaming generation
func (p *Provider) Stream(ctx context.Context, prompt string) (<-chan string, error) {
	tokens := make(chan string, 100)

	model := p.GetModel("")
	maxTokens := p.GetMaxTokens(0)
	temperature := p.GetTemperature(0)

	dsReq := Request{
		Model: model,
		Messages: []Message{
			{Role: "user", Content: prompt},
		},
		Temperature: temperature,
		MaxTokens:   maxTokens,
		Stream:      true,
	}

	resp, err := p.callAPI(ctx, "/chat/completions", dsReq)
	if err != nil {
		return nil, agentErrors.NewLLMRequestError(p.ProviderName(), model, err).
			WithContext("operation", "stream")
	}

	go func() {
		defer close(tokens)

		decoder := json.NewDecoder(strings.NewReader(resp.String()))
		for {
			var streamResp StreamResponse
			if err := decoder.Decode(&streamResp); err != nil {
				if err == io.EOF {
					return
				}
				fmt.Printf("DeepSeek stream decode error: %v\n", err)
				return
			}

			if len(streamResp.Choices) > 0 && streamResp.Choices[0].Delta.Content != "" {
				select {
				case tokens <- streamResp.Choices[0].Delta.Content:
				case <-ctx.Done():
					return
				}
			}

			if len(streamResp.Choices) > 0 && streamResp.Choices[0].FinishReason != "" {
				return
			}
		}
	}()

	return tokens, nil
}

// GenerateWithTools implements tool calling
func (p *Provider) GenerateWithTools(ctx context.Context, prompt string, tools []interfaces.Tool) (*common.ToolCallResponse, error) {
	dsTools := p.convertToolsToDeepSeek(tools)

	model := p.GetModel("")
	maxTokens := p.GetMaxTokens(0)
	temperature := p.GetTemperature(0)

	dsReq := Request{
		Model: model,
		Messages: []Message{
			{Role: "user", Content: prompt},
		},
		Temperature: temperature,
		MaxTokens:   maxTokens,
		Tools:       dsTools,
		ToolChoice:  "auto",
	}

	resp, err := p.callAPI(ctx, "/chat/completions", dsReq)
	if err != nil {
		return nil, agentErrors.NewLLMRequestError(p.ProviderName(), model, err).
			WithContext("operation", "tool_calling")
	}

	var dsResp Response
	if err := json.NewDecoder(strings.NewReader(resp.String())).Decode(&dsResp); err != nil {
		return nil, agentErrors.NewParserInvalidJSONError("tool response body", err).
			WithContext("provider", p.ProviderName())
	}

	if len(dsResp.Choices) == 0 {
		return nil, agentErrors.NewLLMResponseError(p.ProviderName(), model, "no choices in tool response")
	}

	result := &common.ToolCallResponse{
		Content: dsResp.Choices[0].Message.Content,
	}

	for _, tc := range dsResp.Choices[0].Message.ToolCalls {
		var args map[string]interface{}
		if err := json.Unmarshal([]byte(tc.Function.Arguments), &args); err != nil {
			continue
		}

		result.ToolCalls = append(result.ToolCalls, common.ToolCall{
			ID:        tc.ID,
			Name:      tc.Function.Name,
			Arguments: args,
		})
	}

	return result, nil
}

// StreamWithTools implements streaming tool calls
func (p *Provider) StreamWithTools(ctx context.Context, prompt string, tools []interfaces.Tool) (<-chan common.ToolChunk, error) {
	chunks := make(chan common.ToolChunk, 100)

	dsTools := p.convertToolsToDeepSeek(tools)

	model := p.GetModel("")
	maxTokens := p.GetMaxTokens(0)
	temperature := p.GetTemperature(0)

	dsReq := Request{
		Model: model,
		Messages: []Message{
			{Role: "user", Content: prompt},
		},
		Temperature: temperature,
		MaxTokens:   maxTokens,
		Tools:       dsTools,
		ToolChoice:  "auto",
		Stream:      true,
	}

	resp, err := p.callAPI(ctx, "/chat/completions", dsReq)
	if err != nil {
		return nil, agentErrors.NewLLMRequestError(p.ProviderName(), model, err).
			WithContext("operation", "stream_with_tools")
	}

	go func() {
		defer close(chunks)

		decoder := json.NewDecoder(strings.NewReader(resp.String()))
		var currentToolCall *common.ToolCall
		var argsBuffer string

		for {
			var streamResp StreamResponse
			if err := decoder.Decode(&streamResp); err != nil {
				if err == io.EOF {
					if currentToolCall != nil && argsBuffer != "" {
						var args map[string]interface{}
						if unmarshalErr := json.Unmarshal([]byte(argsBuffer), &args); unmarshalErr == nil {
							currentToolCall.Arguments = args
							chunks <- common.ToolChunk{Type: "tool_call", Value: currentToolCall}
						}
					}
					return
				}
				chunks <- common.ToolChunk{Type: "error", Value: err}
				return
			}

			if len(streamResp.Choices) > 0 {
				choice := streamResp.Choices[0]

				if choice.Delta.Content != "" {
					chunks <- common.ToolChunk{Type: "content", Value: choice.Delta.Content}
				}

				for _, tc := range choice.Delta.ToolCalls {
					if tc.Function.Name != "" {
						if currentToolCall != nil && argsBuffer != "" {
							var args map[string]interface{}
							if err := json.Unmarshal([]byte(argsBuffer), &args); err == nil {
								currentToolCall.Arguments = args
								chunks <- common.ToolChunk{Type: "tool_call", Value: currentToolCall}
							}
						}

						currentToolCall = &common.ToolCall{
							ID:   tc.ID,
							Name: tc.Function.Name,
						}
						argsBuffer = tc.Function.Arguments
						chunks <- common.ToolChunk{Type: "tool_name", Value: tc.Function.Name}
					} else if tc.Function.Arguments != "" {
						argsBuffer += tc.Function.Arguments
						chunks <- common.ToolChunk{Type: "tool_args", Value: tc.Function.Arguments}
					}
				}

				if choice.FinishReason != "" {
					return
				}
			}
		}
	}()

	return chunks, nil
}

// Embed generates embeddings for text
func (p *Provider) Embed(ctx context.Context, text string) ([]float64, error) {
	type EmbedRequest struct {
		Model string   `json:"model"`
		Input []string `json:"input"`
	}

	type EmbedResponse struct {
		Object string `json:"object"`
		Data   []struct {
			Object    string    `json:"object"`
			Embedding []float64 `json:"embedding"`
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
func (p *Provider) Provider() constants.Provider {
	return constants.ProviderDeepSeek
}

// IsAvailable checks if the provider is available
func (p *Provider) IsAvailable() bool {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, err := p.Complete(ctx, &agentllm.CompletionRequest{
		Messages: []agentllm.Message{
			agentllm.UserMessage("test"),
		},
		MaxTokens: 1,
	})

	return err == nil
}

// MaxTokens returns the max tokens setting
func (p *Provider) MaxTokens() int {
	return p.MaxTokensValue()
}

// callAPI makes an API call to DeepSeek with retry logic
func (p *Provider) callAPI(ctx context.Context, endpoint string, payload interface{}) (*resty.Response, error) {
	url := p.baseURL + endpoint
	model := p.GetModel("")

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
			return nil, agentErrors.NewLLMRequestError(p.ProviderName(), model, err).
				WithContext("endpoint", endpoint)
		}

		if !resp.IsSuccess() {
			httpErr := common.RestyResponseToHTTPError(resp)
			return nil, common.MapHTTPError(httpErr, p.ProviderName(), model, func(body string) string {
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

// convertToolsToDeepSeek converts our tools to DeepSeek format
func (p *Provider) convertToolsToDeepSeek(tools []interfaces.Tool) []Tool {
	dsTools := make([]Tool, len(tools))

	for i, tool := range tools {
		dsTools[i] = Tool{
			Type: "function",
			Function: Function{
				Name:        tool.Name(),
				Description: tool.Description(),
				Parameters:  p.toolSchemaToJSON(tool.ArgsSchema()),
			},
		}
	}

	return dsTools
}

// toolSchemaToJSON converts tool schema to JSON schema
func (p *Provider) toolSchemaToJSON(schema interface{}) map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"input": map[string]interface{}{
				"type":        "string",
				"description": "The input for the tool",
			},
		},
		"required": []string{"input"},
	}
}

// StreamingProvider extends Provider with advanced streaming
type StreamingProvider struct {
	*Provider
}

// NewStreaming creates a streaming-optimized provider
func NewStreaming(config *agentllm.LLMOptions) (*StreamingProvider, error) {
	base, err := New(common.ConfigToOptions(config)...)
	if err != nil {
		return nil, err
	}

	return &StreamingProvider{
		Provider: base,
	}, nil
}

// TokenWithMetadata represents a streaming token with additional info
type TokenWithMetadata struct {
	Type     string
	Content  string
	Error    error
	Metadata map[string]interface{}
}

// StreamWithMetadata streams tokens with additional metadata
func (p *StreamingProvider) StreamWithMetadata(ctx context.Context, prompt string) (<-chan TokenWithMetadata, error) {
	tokens := make(chan TokenWithMetadata, 100)

	model := p.GetModel("")
	maxTokens := p.GetMaxTokens(0)
	temperature := p.GetTemperature(0)

	dsReq := Request{
		Model: model,
		Messages: []Message{
			{Role: "user", Content: prompt},
		},
		Temperature: temperature,
		MaxTokens:   maxTokens,
		Stream:      true,
	}

	resp, err := p.callAPI(ctx, "/chat/completions", dsReq)
	if err != nil {
		return nil, err
	}

	go func() {
		defer close(tokens)

		decoder := json.NewDecoder(strings.NewReader(resp.String()))
		tokenCount := 0

		for {
			var streamResp StreamResponse
			if err := decoder.Decode(&streamResp); err != nil {
				if err == io.EOF {
					select {
					case tokens <- TokenWithMetadata{
						Type: "finish",
						Metadata: map[string]interface{}{
							"total_tokens": tokenCount,
							"model":        model,
						},
					}:
					case <-ctx.Done():
					}
					return
				}
				select {
				case tokens <- TokenWithMetadata{
					Type:  "error",
					Error: err,
				}:
				case <-ctx.Done():
				}
				return
			}

			if len(streamResp.Choices) > 0 {
				choice := streamResp.Choices[0]

				if choice.Delta.Content != "" {
					tokenCount++
					select {
					case tokens <- TokenWithMetadata{
						Type:    "token",
						Content: choice.Delta.Content,
						Metadata: map[string]interface{}{
							"index":         tokenCount,
							"finish_reason": choice.FinishReason,
						},
					}:
					case <-ctx.Done():
						return
					}
				}

				if choice.FinishReason != "" {
					return
				}
			}
		}
	}()

	return tokens, nil
}
