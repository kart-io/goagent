package providers

import (
	"context"
	"fmt"
	"io"
	"strings"
	"time"

	agentllm "github.com/kart-io/goagent/llm"
	"github.com/kart-io/goagent/llm/constants"
	"github.com/kart-io/goagent/utils/json"

	"github.com/go-resty/resty/v2"

	agentErrors "github.com/kart-io/goagent/errors"
	"github.com/kart-io/goagent/interfaces"
	"github.com/kart-io/goagent/utils/httpclient"
)

// DeepSeekProvider implements LLM interface for DeepSeek
type DeepSeekProvider struct {
	*BaseProvider
	client  *httpclient.Client
	apiKey  string
	baseURL string
}

// DeepSeekRequest represents a request to DeepSeek API
type DeepSeekRequest struct {
	Model       string            `json:"model"`
	Messages    []DeepSeekMessage `json:"messages"`
	Temperature float64           `json:"temperature,omitempty"`
	MaxTokens   int               `json:"max_tokens,omitempty"`
	TopP        float64           `json:"top_p,omitempty"`
	Stream      bool              `json:"stream,omitempty"`
	Tools       []DeepSeekTool    `json:"tools,omitempty"`
	ToolChoice  interface{}       `json:"tool_choice,omitempty"`
	Stop        []string          `json:"stop,omitempty"`
}

// DeepSeekMessage represents a message in DeepSeek format
type DeepSeekMessage struct {
	Role       string             `json:"role"`
	Content    string             `json:"content"`
	Name       string             `json:"name,omitempty"`
	ToolCalls  []DeepSeekToolCall `json:"tool_calls,omitempty"`
	ToolCallID string             `json:"tool_call_id,omitempty"`
}

// DeepSeekTool represents a tool in DeepSeek format
type DeepSeekTool struct {
	Type     string           `json:"type"`
	Function DeepSeekFunction `json:"function"`
}

// DeepSeekFunction represents a function definition
type DeepSeekFunction struct {
	Name        string                 `json:"name"`
	Description string                 `json:"description"`
	Parameters  map[string]interface{} `json:"parameters"`
}

// DeepSeekToolCall represents a tool call
type DeepSeekToolCall struct {
	ID       string               `json:"id"`
	Type     string               `json:"type"`
	Function DeepSeekFunctionCall `json:"function"`
}

// DeepSeekFunctionCall represents a function call
type DeepSeekFunctionCall struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

// DeepSeekResponse represents a response from DeepSeek API
type DeepSeekResponse struct {
	ID      string           `json:"id"`
	Object  string           `json:"object"`
	Created int64            `json:"created"`
	Model   string           `json:"model"`
	Choices []DeepSeekChoice `json:"choices"`
	Usage   DeepSeekUsage    `json:"usage"`
}

// DeepSeekChoice represents a choice in the response
type DeepSeekChoice struct {
	Index        int             `json:"index"`
	Message      DeepSeekMessage `json:"message"`
	Delta        DeepSeekMessage `json:"delta,omitempty"`
	FinishReason string          `json:"finish_reason"`
}

// DeepSeekUsage represents token usage
type DeepSeekUsage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

// DeepSeekStreamResponse represents a streaming response
type DeepSeekStreamResponse struct {
	ID      string           `json:"id"`
	Object  string           `json:"object"`
	Created int64            `json:"created"`
	Model   string           `json:"model"`
	Choices []DeepSeekChoice `json:"choices"`
}

// NewDeepSeekWithOptions creates a new DeepSeek provider using options pattern
func NewDeepSeekWithOptions(opts ...agentllm.ClientOption) (*DeepSeekProvider, error) {
	// 创建 BaseProvider，统一处理 Options
	base := NewBaseProvider(opts...)

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

	// 获取超时时间
	timeout := base.GetTimeout()

	client := httpclient.NewClient(&httpclient.Config{
		Timeout: timeout,
		Headers: map[string]string{
			"Content-Type":  "application/json",
			"Accept":        "application/json",
			"Authorization": "Bearer " + base.Config.APIKey,
		},
	})

	provider := &DeepSeekProvider{
		BaseProvider: base,
		client:       client,
		apiKey:       base.Config.APIKey,
		baseURL:      base.Config.BaseURL,
	}

	return provider, nil
}

// NewDeepSeek creates a new DeepSeek provider (backward compatible)
func NewDeepSeek(config *agentllm.LLMOptions) (*DeepSeekProvider, error) {
	// 将现有配置转换为 Options，使用 Options 模式创建 Provider
	return NewDeepSeekWithOptions(ConfigToOptions(config)...)
}

// Complete implements basic text completion
func (p *DeepSeekProvider) Complete(ctx context.Context, req *agentllm.CompletionRequest) (*agentllm.CompletionResponse, error) {
	// Convert messages to DeepSeek format
	messages := make([]DeepSeekMessage, len(req.Messages))
	for i, msg := range req.Messages {
		messages[i] = DeepSeekMessage{
			Role:    msg.Role,
			Content: msg.Content,
			Name:    msg.Name,
		}
	}

	// Prepare request
	model := p.GetModel(req.Model)
	dsReq := DeepSeekRequest{
		Model:       model,
		Messages:    messages,
		Temperature: p.GetTemperature(req.Temperature),
		MaxTokens:   p.GetMaxTokens(req.MaxTokens),
		TopP:        req.TopP,
		Stop:        req.Stop,
		Stream:      false,
	}

	// Make API call
	resp, err := p.callAPI(ctx, "/chat/completions", dsReq)
	if err != nil {
		return nil, agentErrors.NewLLMRequestError(string(constants.ProviderDeepSeek), model, err)
	}

	// Parse response
	var dsResp DeepSeekResponse
	if err := json.NewDecoder(strings.NewReader(resp.String())).Decode(&dsResp); err != nil {
		return nil, agentErrors.NewParserInvalidJSONError("response body", err).
			WithContext("provider", "deepseek")
	}

	if len(dsResp.Choices) == 0 {
		return nil, agentErrors.NewLLMResponseError("deepseek", model, "no choices in response")
	}

	return &agentllm.CompletionResponse{
		Content:      dsResp.Choices[0].Message.Content,
		Model:        dsResp.Model,
		TokensUsed:   dsResp.Usage.TotalTokens,
		FinishReason: dsResp.Choices[0].FinishReason,
		Provider:     string(constants.ProviderDeepSeek),
		Usage: &interfaces.TokenUsage{
			PromptTokens:     dsResp.Usage.PromptTokens,
			CompletionTokens: dsResp.Usage.CompletionTokens,
			TotalTokens:      dsResp.Usage.TotalTokens,
		},
	}, nil
}

// Chat implements chat conversation
func (p *DeepSeekProvider) Chat(ctx context.Context, messages []agentllm.Message) (*agentllm.CompletionResponse, error) {
	return p.Complete(ctx, &agentllm.CompletionRequest{
		Messages: messages,
	})
}

// Stream implements streaming generation
func (p *DeepSeekProvider) Stream(ctx context.Context, prompt string) (<-chan string, error) {
	tokens := make(chan string, 100)

	model := p.GetModel("")
	maxTokens := p.GetMaxTokens(0)
	temperature := p.GetTemperature(0)

	// Prepare request
	dsReq := DeepSeekRequest{
		Model: model,
		Messages: []DeepSeekMessage{
			{Role: "user", Content: prompt},
		},
		Temperature: temperature,
		MaxTokens:   maxTokens,
		Stream:      true,
	}

	// Make streaming API call
	resp, err := p.callAPI(ctx, "/chat/completions", dsReq)
	if err != nil {
		return nil, agentErrors.NewLLMRequestError("deepseek", model, err).
			WithContext("operation", "stream")
	}

	go func() {
		defer close(tokens)

		decoder := json.NewDecoder(strings.NewReader(resp.String()))
		for {
			var streamResp DeepSeekStreamResponse
			if err := decoder.Decode(&streamResp); err != nil {
				if err == io.EOF {
					return
				}
				// Log error but continue
				fmt.Printf("DeepSeek stream decode error: %v\n", err)
				return
			}

			if len(streamResp.Choices) > 0 && streamResp.Choices[0].Delta.Content != "" {
				// Use select to handle context cancellation
				select {
				case tokens <- streamResp.Choices[0].Delta.Content:
					// Successfully sent
				case <-ctx.Done():
					// Context cancelled, exit immediately
					return
				}
			}

			// Check for completion
			if len(streamResp.Choices) > 0 && streamResp.Choices[0].FinishReason != "" {
				return
			}
		}
	}()

	return tokens, nil
}

// GenerateWithTools implements tool calling
func (p *DeepSeekProvider) GenerateWithTools(ctx context.Context, prompt string, tools []interfaces.Tool) (*ToolCallResponse, error) {
	// Convert tools to DeepSeek format
	dsTools := p.convertToolsToDeepSeek(tools)

	model := p.GetModel("")
	maxTokens := p.GetMaxTokens(0)
	temperature := p.GetTemperature(0)

	// Prepare request
	dsReq := DeepSeekRequest{
		Model: model,
		Messages: []DeepSeekMessage{
			{Role: "user", Content: prompt},
		},
		Temperature: temperature,
		MaxTokens:   maxTokens,
		Tools:       dsTools,
		ToolChoice:  "auto",
	}

	// Make API call
	resp, err := p.callAPI(ctx, "/chat/completions", dsReq)
	if err != nil {
		return nil, agentErrors.NewLLMRequestError("deepseek", model, err).
			WithContext("operation", "tool_calling")
	}

	// Parse response
	var dsResp DeepSeekResponse
	if err := json.NewDecoder(strings.NewReader(resp.String())).Decode(&dsResp); err != nil {
		return nil, agentErrors.NewParserInvalidJSONError("tool response body", err).
			WithContext("provider", "deepseek")
	}

	if len(dsResp.Choices) == 0 {
		return nil, agentErrors.NewLLMResponseError("deepseek", model, "no choices in tool response")
	}

	// Convert to our format
	result := &ToolCallResponse{
		Content: dsResp.Choices[0].Message.Content,
	}

	// Parse tool calls
	for _, tc := range dsResp.Choices[0].Message.ToolCalls {
		var args map[string]interface{}
		if err := json.Unmarshal([]byte(tc.Function.Arguments), &args); err != nil {
			continue // Skip invalid arguments
		}

		result.ToolCalls = append(result.ToolCalls, ToolCall{
			ID:        tc.ID,
			Name:      tc.Function.Name,
			Arguments: args,
		})
	}

	return result, nil
}

// StreamWithTools implements streaming tool calls
func (p *DeepSeekProvider) StreamWithTools(ctx context.Context, prompt string, tools []interfaces.Tool) (<-chan ToolChunk, error) {
	chunks := make(chan ToolChunk, 100)

	// Convert tools to DeepSeek format
	dsTools := p.convertToolsToDeepSeek(tools)

	model := p.GetModel("")
	maxTokens := p.GetMaxTokens(0)
	temperature := p.GetTemperature(0)

	// Prepare request
	dsReq := DeepSeekRequest{
		Model: model,
		Messages: []DeepSeekMessage{
			{Role: "user", Content: prompt},
		},
		Temperature: temperature,
		MaxTokens:   maxTokens,
		Tools:       dsTools,
		ToolChoice:  "auto",
		Stream:      true,
	}

	// Make streaming API call
	resp, err := p.callAPI(ctx, "/chat/completions", dsReq)
	if err != nil {
		return nil, agentErrors.NewLLMRequestError("deepseek", model, err).
			WithContext("operation", "stream_with_tools")
	}

	go func() {
		defer close(chunks)

		decoder := json.NewDecoder(strings.NewReader(resp.String()))
		var currentToolCall *ToolCall
		var argsBuffer string

		for {
			var streamResp DeepSeekStreamResponse
			if err := decoder.Decode(&streamResp); err != nil {
				if err == io.EOF {
					// Finalize last tool call
					if currentToolCall != nil && argsBuffer != "" {
						var args map[string]interface{}
						if unmarshalErr := json.Unmarshal([]byte(argsBuffer), &args); unmarshalErr == nil {
							currentToolCall.Arguments = args
							chunks <- ToolChunk{Type: "tool_call", Value: currentToolCall}
						}
					}
					return
				}
				chunks <- ToolChunk{Type: "error", Value: err}
				return
			}

			if len(streamResp.Choices) > 0 {
				choice := streamResp.Choices[0]

				// Handle content
				if choice.Delta.Content != "" {
					chunks <- ToolChunk{Type: "content", Value: choice.Delta.Content}
				}

				// Handle tool calls
				for _, tc := range choice.Delta.ToolCalls {
					if tc.Function.Name != "" {
						// New tool call
						if currentToolCall != nil && argsBuffer != "" {
							// Finalize previous call
							var args map[string]interface{}
							if err := json.Unmarshal([]byte(argsBuffer), &args); err == nil {
								currentToolCall.Arguments = args
								chunks <- ToolChunk{Type: "tool_call", Value: currentToolCall}
							}
						}

						currentToolCall = &ToolCall{
							ID:   tc.ID,
							Name: tc.Function.Name,
						}
						argsBuffer = tc.Function.Arguments
						chunks <- ToolChunk{Type: "tool_name", Value: tc.Function.Name}
					} else if tc.Function.Arguments != "" {
						// Continue arguments
						argsBuffer += tc.Function.Arguments
						chunks <- ToolChunk{Type: "tool_args", Value: tc.Function.Arguments}
					}
				}

				// Check for completion
				if choice.FinishReason != "" {
					return
				}
			}
		}
	}()

	return chunks, nil
}

// Embed generates embeddings for text
func (p *DeepSeekProvider) Embed(ctx context.Context, text string) ([]float64, error) {
	// DeepSeek embeddings API
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
			WithContext("provider", "deepseek")
	}

	var embedResp EmbedResponse
	if err := json.NewDecoder(strings.NewReader(resp.String())).Decode(&embedResp); err != nil {
		return nil, agentErrors.NewParserInvalidJSONError("embeddings response", err).
			WithContext("provider", "deepseek")
	}

	if len(embedResp.Data) == 0 {
		return nil, agentErrors.NewLLMResponseError("deepseek", "deepseek-embedding", "no embeddings in response")
	}

	return embedResp.Data[0].Embedding, nil
}

// Provider returns the provider type
func (p *DeepSeekProvider) Provider() constants.Provider {
	return constants.ProviderDeepSeek
}

// IsAvailable checks if the provider is available
func (p *DeepSeekProvider) IsAvailable() bool {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Try a simple completion
	_, err := p.Complete(ctx, &agentllm.CompletionRequest{
		Messages: []agentllm.Message{
			agentllm.UserMessage("test"),
		},
		MaxTokens: 1,
	})

	return err == nil
}

// ModelName returns the model name
func (p *DeepSeekProvider) ModelName() string {
	return p.GetModel("")
}

// MaxTokens returns the max tokens setting
func (p *DeepSeekProvider) MaxTokens() int {
	return p.GetMaxTokens(0)
}

// Helper methods

// callAPI makes an API call to DeepSeek
func (p *DeepSeekProvider) callAPI(ctx context.Context, endpoint string, payload interface{}) (*resty.Response, error) {
	url := p.baseURL + endpoint

	resp, err := p.client.R().
		SetContext(ctx).
		SetBody(payload).
		Post(url)

	model := p.GetModel("")
	if err != nil {
		return nil, agentErrors.NewLLMRequestError("deepseek", model, err).
			WithContext("endpoint", endpoint)
	}

	if !resp.IsSuccess() {
		return nil, agentErrors.NewLLMResponseError("deepseek", model,
			fmt.Sprintf("API returned status %d: %s", resp.StatusCode(), resp.String())).
			WithContext("endpoint", endpoint)
	}

	return resp, nil
}

// convertToolsToDeepSeek converts our tools to DeepSeek format
func (p *DeepSeekProvider) convertToolsToDeepSeek(tools []interfaces.Tool) []DeepSeekTool {
	dsTools := make([]DeepSeekTool, len(tools))

	for i, tool := range tools {
		dsTools[i] = DeepSeekTool{
			Type: "function",
			Function: DeepSeekFunction{
				Name:        tool.Name(),
				Description: tool.Description(),
				Parameters:  p.toolSchemaToJSON(tool.ArgsSchema()),
			},
		}
	}

	return dsTools
}

// toolSchemaToJSON converts tool schema to JSON schema
func (p *DeepSeekProvider) toolSchemaToJSON(schema interface{}) map[string]interface{} {
	// This is a simplified version
	// In production, you'd properly convert the schema
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

// DeepSeekStreamingProvider extends DeepSeekProvider with advanced streaming
type DeepSeekStreamingProvider struct {
	*DeepSeekProvider
}

// NewDeepSeekStreaming creates a streaming-optimized provider
func NewDeepSeekStreaming(config *agentllm.LLMOptions) (*DeepSeekStreamingProvider, error) {
	base, err := NewDeepSeek(config)
	if err != nil {
		return nil, err
	}

	return &DeepSeekStreamingProvider{
		DeepSeekProvider: base,
	}, nil
}

// StreamWithMetadata streams tokens with additional metadata
func (p *DeepSeekStreamingProvider) StreamWithMetadata(ctx context.Context, prompt string) (<-chan TokenWithMetadata, error) {
	tokens := make(chan TokenWithMetadata, 100)

	model := p.GetModel("")
	maxTokens := p.GetMaxTokens(0)
	temperature := p.GetTemperature(0)

	// Prepare request
	dsReq := DeepSeekRequest{
		Model: model,
		Messages: []DeepSeekMessage{
			{Role: "user", Content: prompt},
		},
		Temperature: temperature,
		MaxTokens:   maxTokens,
		Stream:      true,
	}

	// Make streaming API call
	resp, err := p.callAPI(ctx, "/chat/completions", dsReq)
	if err != nil {
		return nil, err
	}

	go func() {
		defer close(tokens)

		decoder := json.NewDecoder(strings.NewReader(resp.String()))
		tokenCount := 0

		for {
			var streamResp DeepSeekStreamResponse
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
						// Successfully sent
					case <-ctx.Done():
						// Context cancelled, exit immediately
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
