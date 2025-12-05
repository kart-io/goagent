package providers

import (
	"context"
	"fmt"
	"github.com/kart-io/goagent/llm/common"
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
	*common.BaseProvider
	*common.ProviderCapabilities
	client  *httpclient.Client
	apiKey  string
	baseURL string
}

// DeepSeekRequest represents a request to DeepSeek API
type DeepSeekRequest struct {
	Model          string                  `json:"model"`
	Messages       []DeepSeekMessage       `json:"messages"`
	Temperature    float64                 `json:"temperature,omitempty"`
	MaxTokens      int                     `json:"max_tokens,omitempty"`
	TopP           float64                 `json:"top_p,omitempty"`
	Stream         bool                    `json:"stream,omitempty"`
	Tools          []DeepSeekTool          `json:"tools,omitempty"`
	ToolChoice     interface{}             `json:"tool_choice,omitempty"`
	Stop           []string                `json:"stop,omitempty"`
	ResponseFormat *DeepSeekResponseFormat `json:"response_format,omitempty"`
}

// DeepSeekResponseFormat 定义响应格式
type DeepSeekResponseFormat struct {
	Type string `json:"type"` // "text" 或 "json_object"
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
func (p *DeepSeekProvider) Complete(ctx context.Context, req *agentllm.CompletionRequest) (*agentllm.CompletionResponse, error) {
	// Convert messages to DeepSeek format using shared utility
	messages := common.ConvertMessages(req.Messages, func(msg agentllm.Message) DeepSeekMessage {
		return DeepSeekMessage{
			Role:    msg.Role,
			Content: msg.Content,
			Name:    msg.Name,
		}
	})

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

	// 添加 ResponseFormat 支持（从配置中获取）
	if p.Config.ResponseFormat != nil {
		dsReq.ResponseFormat = &DeepSeekResponseFormat{
			Type: p.Config.ResponseFormat.Type,
		}
	}

	// Make API call
	resp, err := p.callAPI(ctx, "/chat/completions", dsReq)
	if err != nil {
		return nil, agentErrors.NewLLMRequestError(p.ProviderName(), model, err)
	}

	// Parse response
	var dsResp DeepSeekResponse
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
		return nil, agentErrors.NewLLMRequestError(p.ProviderName(), model, err).
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
// 返回 *agentllm.ToolCallResponse 以符合 llm.ToolCallingClient 接口
func (p *DeepSeekProvider) GenerateWithTools(ctx context.Context, prompt string, tools []interfaces.Tool) (*agentllm.ToolCallResponse, error) {
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
		return nil, agentErrors.NewLLMRequestError(p.ProviderName(), model, err).
			WithContext("operation", "tool_calling")
	}

	// Parse response
	var dsResp DeepSeekResponse
	if err := json.NewDecoder(strings.NewReader(resp.String())).Decode(&dsResp); err != nil {
		return nil, agentErrors.NewParserInvalidJSONError("tool response body", err).
			WithContext("provider", p.ProviderName())
	}

	if len(dsResp.Choices) == 0 {
		return nil, agentErrors.NewLLMResponseError(p.ProviderName(), model, "no choices in tool response")
	}

	// Convert to llm.ToolCallResponse format (符合接口定义)
	result := &agentllm.ToolCallResponse{
		Content: dsResp.Choices[0].Message.Content,
	}

	// Parse tool calls - 转换为 llm.ToolCall 格式
	for _, tc := range dsResp.Choices[0].Message.ToolCalls {
		result.ToolCalls = append(result.ToolCalls, agentllm.ToolCall{
			ID:   tc.ID,
			Type: tc.Type,
			Function: struct {
				Name      string `json:"name"`
				Arguments string `json:"arguments"`
			}{
				Name:      tc.Function.Name,
				Arguments: tc.Function.Arguments, // 保持原始 JSON 字符串
			},
		})
	}

	return result, nil
}

// StreamWithTools implements streaming tool calls
// 返回 <-chan agentllm.ToolChunk 以符合 llm.ToolCallingClient 接口
func (p *DeepSeekProvider) StreamWithTools(ctx context.Context, prompt string, tools []interfaces.Tool) (<-chan agentllm.ToolChunk, error) {
	chunks := make(chan agentllm.ToolChunk, 100)

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
		return nil, agentErrors.NewLLMRequestError(p.ProviderName(), model, err).
			WithContext("operation", "stream_with_tools")
	}

	go func() {
		defer close(chunks)

		decoder := json.NewDecoder(strings.NewReader(resp.String()))
		var currentToolCall *agentllm.ToolCall
		var argsBuffer string

		for {
			var streamResp DeepSeekStreamResponse
			if err := decoder.Decode(&streamResp); err != nil {
				if err == io.EOF {
					// Finalize last tool call
					if currentToolCall != nil && argsBuffer != "" {
						currentToolCall.Function.Arguments = argsBuffer
						chunks <- agentllm.ToolChunk{Type: "tool_call", Value: currentToolCall}
					}
					return
				}
				chunks <- agentllm.ToolChunk{Type: "error", Value: err}
				return
			}

			if len(streamResp.Choices) > 0 {
				choice := streamResp.Choices[0]

				// Handle content
				if choice.Delta.Content != "" {
					chunks <- agentllm.ToolChunk{Type: "content", Value: choice.Delta.Content}
				}

				// Handle tool calls
				for _, tc := range choice.Delta.ToolCalls {
					if tc.Function.Name != "" {
						// New tool call - finalize previous call first
						if currentToolCall != nil && argsBuffer != "" {
							currentToolCall.Function.Arguments = argsBuffer
							chunks <- agentllm.ToolChunk{Type: "tool_call", Value: currentToolCall}
						}

						// Create new tool call with llm.ToolCall format
						currentToolCall = &agentllm.ToolCall{
							ID:   tc.ID,
							Type: tc.Type,
							Function: struct {
								Name      string `json:"name"`
								Arguments string `json:"arguments"`
							}{
								Name: tc.Function.Name,
							},
						}
						argsBuffer = tc.Function.Arguments
						chunks <- agentllm.ToolChunk{Type: "tool_name", Value: tc.Function.Name}
					} else if tc.Function.Arguments != "" {
						// Continue arguments
						argsBuffer += tc.Function.Arguments
						chunks <- agentllm.ToolChunk{Type: "tool_args", Value: tc.Function.Arguments}
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
			return nil, agentErrors.NewLLMRequestError(p.ProviderName(), model, err).
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
	// 处理不同类型的 schema 输入
	switch s := schema.(type) {
	case string:
		// JSON 字符串格式的 schema
		if s != "" {
			var result map[string]interface{}
			if err := json.Unmarshal([]byte(s), &result); err == nil {
				return result
			}
		}
	case map[string]interface{}:
		// 已经是 map 格式
		return s
	}

	// 默认返回空 schema
	return map[string]interface{}{
		"type":       "object",
		"properties": map[string]interface{}{},
	}
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
