package providers

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/kart-io/goagent/interfaces"
	"github.com/kart-io/goagent/llm"
	"github.com/kart-io/goagent/tools"
)

// DeepSeekProvider implements LLM interface for DeepSeek
type DeepSeekProvider struct {
	config      *llm.Config
	httpClient  *http.Client
	apiKey      string
	baseURL     string
	model       string
	maxTokens   int
	temperature float64
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

// NewDeepSeek creates a new DeepSeek provider
func NewDeepSeek(config *llm.Config) (*DeepSeekProvider, error) {
	if config.APIKey == "" {
		return nil, fmt.Errorf("DeepSeek API key is required")
	}

	baseURL := config.BaseURL
	if baseURL == "" {
		baseURL = "https://api.deepseek.com/v1"
	}

	model := config.Model
	if model == "" {
		model = "deepseek-chat"
	}

	provider := &DeepSeekProvider{
		config:      config,
		httpClient:  &http.Client{Timeout: time.Duration(config.Timeout) * time.Second},
		apiKey:      config.APIKey,
		baseURL:     baseURL,
		model:       model,
		maxTokens:   config.MaxTokens,
		temperature: config.Temperature,
	}

	// Set defaults
	if provider.maxTokens == 0 {
		provider.maxTokens = 2000
	}
	if provider.temperature == 0 {
		provider.temperature = 0.7
	}

	return provider, nil
}

// Complete implements basic text completion
func (p *DeepSeekProvider) Complete(ctx context.Context, req *llm.CompletionRequest) (*llm.CompletionResponse, error) {
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
	dsReq := DeepSeekRequest{
		Model:       p.getModel(req.Model),
		Messages:    messages,
		Temperature: p.getTemperature(req.Temperature),
		MaxTokens:   p.getMaxTokens(req.MaxTokens),
		TopP:        req.TopP,
		Stop:        req.Stop,
		Stream:      false,
	}

	// Make API call
	resp, err := p.callAPI(ctx, "/chat/completions", dsReq)
	if err != nil {
		return nil, fmt.Errorf("DeepSeek API call failed: %w", err)
	}

	// Parse response
	var dsResp DeepSeekResponse
	if err := json.NewDecoder(resp.Body).Decode(&dsResp); err != nil {
		return nil, fmt.Errorf("failed to parse DeepSeek response: %w", err)
	}
	defer resp.Body.Close()

	if len(dsResp.Choices) == 0 {
		return nil, fmt.Errorf("no choices in DeepSeek response")
	}

	return &llm.CompletionResponse{
		Content:      dsResp.Choices[0].Message.Content,
		Model:        dsResp.Model,
		TokensUsed:   dsResp.Usage.TotalTokens,
		FinishReason: dsResp.Choices[0].FinishReason,
		Provider:     string(llm.ProviderDeepSeek),
	}, nil
}

// Chat implements chat conversation
func (p *DeepSeekProvider) Chat(ctx context.Context, messages []llm.Message) (*llm.CompletionResponse, error) {
	return p.Complete(ctx, &llm.CompletionRequest{
		Messages: messages,
	})
}

// Stream implements streaming generation
func (p *DeepSeekProvider) Stream(ctx context.Context, prompt string) (<-chan string, error) {
	tokens := make(chan string, 100)

	// Prepare request
	dsReq := DeepSeekRequest{
		Model: p.model,
		Messages: []DeepSeekMessage{
			{Role: "user", Content: prompt},
		},
		Temperature: p.temperature,
		MaxTokens:   p.maxTokens,
		Stream:      true,
	}

	// Make streaming API call
	resp, err := p.callAPI(ctx, "/chat/completions", dsReq)
	if err != nil {
		return nil, fmt.Errorf("DeepSeek stream API call failed: %w", err)
	}
	// Body will be closed by goroutine below
	// nolint:bodyclose

	go func() {
		defer close(tokens)
		defer resp.Body.Close()

		decoder := json.NewDecoder(resp.Body)
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
				tokens <- streamResp.Choices[0].Delta.Content
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
func (p *DeepSeekProvider) GenerateWithTools(ctx context.Context, prompt string, tools []tools.Tool) (*ToolCallResponse, error) {
	// Convert tools to DeepSeek format
	dsTools := p.convertToolsToDeepSeek(tools)

	// Prepare request
	dsReq := DeepSeekRequest{
		Model: p.model,
		Messages: []DeepSeekMessage{
			{Role: "user", Content: prompt},
		},
		Temperature: p.temperature,
		MaxTokens:   p.maxTokens,
		Tools:       dsTools,
		ToolChoice:  "auto",
	}

	// Make API call
	resp, err := p.callAPI(ctx, "/chat/completions", dsReq)
	if err != nil {
		return nil, fmt.Errorf("DeepSeek tool API call failed: %w", err)
	}

	// Parse response
	var dsResp DeepSeekResponse
	if err := json.NewDecoder(resp.Body).Decode(&dsResp); err != nil {
		return nil, fmt.Errorf("failed to parse DeepSeek tool response: %w", err)
	}
	defer resp.Body.Close()

	if len(dsResp.Choices) == 0 {
		return nil, fmt.Errorf("no choices in DeepSeek tool response")
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

	// Prepare request
	dsReq := DeepSeekRequest{
		Model: p.model,
		Messages: []DeepSeekMessage{
			{Role: "user", Content: prompt},
		},
		Temperature: p.temperature,
		MaxTokens:   p.maxTokens,
		Tools:       dsTools,
		ToolChoice:  "auto",
		Stream:      true,
	}

	// Make streaming API call
	resp, err := p.callAPI(ctx, "/chat/completions", dsReq)
	if err != nil {
		return nil, fmt.Errorf("DeepSeek stream tool API call failed: %w", err)
	}
	// Body will be closed by goroutine below
	// nolint:bodyclose

	go func() {
		defer close(chunks)
		defer resp.Body.Close()

		decoder := json.NewDecoder(resp.Body)
		var currentToolCall *ToolCall
		var argsBuffer string

		for {
			var streamResp DeepSeekStreamResponse
			if err := decoder.Decode(&streamResp); err != nil {
				if err == io.EOF {
					// Finalize last tool call
					if currentToolCall != nil && argsBuffer != "" {
						var args map[string]interface{}
						if err := json.Unmarshal([]byte(argsBuffer), &args); err == nil {
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
		return nil, fmt.Errorf("DeepSeek embeddings API call failed: %w", err)
	}

	var embedResp EmbedResponse
	if err := json.NewDecoder(resp.Body).Decode(&embedResp); err != nil {
		return nil, fmt.Errorf("failed to parse DeepSeek embeddings response: %w", err)
	}
	defer resp.Body.Close()

	if len(embedResp.Data) == 0 {
		return nil, fmt.Errorf("no embeddings in response")
	}

	return embedResp.Data[0].Embedding, nil
}

// Provider returns the provider type
func (p *DeepSeekProvider) Provider() llm.Provider {
	return llm.ProviderDeepSeek
}

// IsAvailable checks if the provider is available
func (p *DeepSeekProvider) IsAvailable() bool {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Try a simple completion
	_, err := p.Complete(ctx, &llm.CompletionRequest{
		Messages: []llm.Message{
			llm.UserMessage("test"),
		},
		MaxTokens: 1,
	})

	return err == nil
}

// ModelName returns the model name
func (p *DeepSeekProvider) ModelName() string {
	return p.model
}

// MaxTokens returns the max tokens setting
func (p *DeepSeekProvider) MaxTokens() int {
	return p.maxTokens
}

// Helper methods

// callAPI makes an API call to DeepSeek
func (p *DeepSeekProvider) callAPI(ctx context.Context, endpoint string, payload interface{}) (*http.Response, error) {
	url := p.baseURL + endpoint

	jsonData, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewBuffer(jsonData))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+p.apiKey)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	resp, err := p.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("HTTP request failed: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		return nil, fmt.Errorf("API returned status %d: %s", resp.StatusCode, string(body))
	}

	return resp, nil
}

// convertToolsToDeepSeek converts our tools to DeepSeek format
func (p *DeepSeekProvider) convertToolsToDeepSeek(tools []tools.Tool) []DeepSeekTool {
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

// getModel returns the model to use
func (p *DeepSeekProvider) getModel(requestModel string) string {
	if requestModel != "" {
		return requestModel
	}
	return p.model
}

// getTemperature returns the temperature to use
func (p *DeepSeekProvider) getTemperature(requestTemp float64) float64 {
	if requestTemp > 0 {
		return requestTemp
	}
	return p.temperature
}

// getMaxTokens returns the max tokens to use
func (p *DeepSeekProvider) getMaxTokens(requestTokens int) int {
	if requestTokens > 0 {
		return requestTokens
	}
	return p.maxTokens
}

// DeepSeekStreamingProvider extends DeepSeekProvider with advanced streaming
type DeepSeekStreamingProvider struct {
	*DeepSeekProvider
}

// NewDeepSeekStreaming creates a streaming-optimized provider
func NewDeepSeekStreaming(config *llm.Config) (*DeepSeekStreamingProvider, error) {
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

	// Prepare request
	dsReq := DeepSeekRequest{
		Model: p.model,
		Messages: []DeepSeekMessage{
			{Role: "user", Content: prompt},
		},
		Temperature: p.temperature,
		MaxTokens:   p.maxTokens,
		Stream:      true,
	}

	// Make streaming API call
	resp, err := p.callAPI(ctx, "/chat/completions", dsReq)
	if err != nil {
		return nil, err
	}
	// Body will be closed by goroutine below
	// nolint:bodyclose

	go func() {
		defer close(tokens)
		defer resp.Body.Close()

		decoder := json.NewDecoder(resp.Body)
		tokenCount := 0

		for {
			var streamResp DeepSeekStreamResponse
			if err := decoder.Decode(&streamResp); err != nil {
				if err == io.EOF {
					tokens <- TokenWithMetadata{
						Type: "finish",
						Metadata: map[string]interface{}{
							"total_tokens": tokenCount,
							"model":        p.model,
						},
					}
					return
				}
				tokens <- TokenWithMetadata{
					Type:  "error",
					Error: err,
				}
				return
			}

			if len(streamResp.Choices) > 0 {
				choice := streamResp.Choices[0]

				if choice.Delta.Content != "" {
					tokenCount++
					tokens <- TokenWithMetadata{
						Type:    "token",
						Content: choice.Delta.Content,
						Metadata: map[string]interface{}{
							"index":         tokenCount,
							"finish_reason": choice.FinishReason,
						},
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
