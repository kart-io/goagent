package providers

import (
	"context"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/kart-io/goagent/llm/constants"
	"github.com/kart-io/goagent/utils/json"

	"github.com/sashabaranov/go-openai"

	agentErrors "github.com/kart-io/goagent/errors"
	"github.com/kart-io/goagent/interfaces"
	agentllm "github.com/kart-io/goagent/llm"
)

// OpenAIProvider implements LLM interface for OpenAI
type OpenAIProvider struct {
	*BaseProvider
	client *openai.Client
}

// NewOpenAIWithOptions creates a new OpenAI provider using options pattern
func NewOpenAIWithOptions(opts ...agentllm.ClientOption) (*OpenAIProvider, error) {
	// 创建 BaseProvider，统一处理 Options
	base := NewBaseProvider(opts...)

	// 应用 Provider 特定的默认值
	base.ApplyProviderDefaults(
		constants.ProviderOpenAI,
		"https://api.openai.com/v1",
		openai.GPT4TurboPreview,
		constants.EnvOpenAIBaseURL,
		constants.EnvOpenAIModel,
	)

	// 统一处理 API Key
	if err := base.EnsureAPIKey(constants.EnvOpenAIAPIKey, constants.ProviderOpenAI); err != nil {
		return nil, err
	}

	// 创建 OpenAI 客户端配置
	clientConfig := openai.DefaultConfig(base.Config.APIKey)
	if base.Config.BaseURL != "" {
		clientConfig.BaseURL = base.Config.BaseURL
	}

	provider := &OpenAIProvider{
		BaseProvider: base,
		client:       openai.NewClientWithConfig(clientConfig),
	}

	return provider, nil
}

// NewOpenAI creates a new OpenAI provider (backward compatible)
func NewOpenAI(config *agentllm.LLMOptions) (*OpenAIProvider, error) {
	// 将现有配置转换为 Options，使用 Options 模式创建 Provider
	return NewOpenAIWithOptions(ConfigToOptions(config)...)
}

// Complete implements basic text completion
func (p *OpenAIProvider) Complete(ctx context.Context, req *agentllm.CompletionRequest) (*agentllm.CompletionResponse, error) {
	messages := make([]openai.ChatCompletionMessage, len(req.Messages))
	for i, msg := range req.Messages {
		messages[i] = openai.ChatCompletionMessage{
			Role:    msg.Role,
			Content: msg.Content,
			Name:    msg.Name,
		}
	}

	// 使用 BaseProvider 的统一参数处理方法
	model := p.GetModel(req.Model)
	maxTokens := p.GetMaxTokens(req.MaxTokens)
	temperature := p.GetTemperature(req.Temperature)

	resp, err := p.client.CreateChatCompletion(ctx, openai.ChatCompletionRequest{
		Model:       model,
		Messages:    messages,
		MaxTokens:   maxTokens,
		Temperature: float32(temperature),
		Stop:        req.Stop,
		TopP:        float32(req.TopP),
	})
	if err != nil {
		return nil, agentErrors.NewLLMRequestError(string(constants.ProviderOpenAI), model, err)
	}

	if len(resp.Choices) == 0 {
		return nil, agentErrors.NewLLMResponseError(string(constants.ProviderOpenAI), model, "no choices in response")
	}

	return &agentllm.CompletionResponse{
		Content:      resp.Choices[0].Message.Content,
		Model:        resp.Model,
		TokensUsed:   resp.Usage.TotalTokens,
		FinishReason: string(resp.Choices[0].FinishReason),
		Provider:     string(constants.ProviderOpenAI),
		Usage: &interfaces.TokenUsage{
			PromptTokens:     resp.Usage.PromptTokens,
			CompletionTokens: resp.Usage.CompletionTokens,
			TotalTokens:      resp.Usage.TotalTokens,
		},
	}, nil
}

// Chat implements chat conversation
func (p *OpenAIProvider) Chat(ctx context.Context, messages []agentllm.Message) (*agentllm.CompletionResponse, error) {
	return p.Complete(ctx, &agentllm.CompletionRequest{
		Messages: messages,
	})
}

// Stream implements streaming generation
func (p *OpenAIProvider) Stream(ctx context.Context, prompt string) (<-chan string, error) {
	tokens := make(chan string, 100)

	model := p.GetModel("")
	maxTokens := p.GetMaxTokens(0)
	temperature := p.GetTemperature(0)

	stream, err := p.client.CreateChatCompletionStream(ctx, openai.ChatCompletionRequest{
		Model: model,
		Messages: []openai.ChatCompletionMessage{
			{Role: openai.ChatMessageRoleUser, Content: prompt},
		},
		MaxTokens:   maxTokens,
		Temperature: float32(temperature),
		Stream:      true,
	})
	if err != nil {
		return nil, agentErrors.NewLLMRequestError(string(constants.ProviderOpenAI), model, err).
			WithContext("stream", true)
	}

	go func() {
		defer close(tokens)
		defer func() { _ = stream.Close() }()

		for {
			response, err := stream.Recv()
			if err != nil {
				if errors.Is(err, io.EOF) {
					return
				}
				// Log error but don't crash the stream
				fmt.Printf("Stream error: %v\n", err)
				return
			}

			if len(response.Choices) > 0 && response.Choices[0].Delta.Content != "" {
				select {
				case tokens <- response.Choices[0].Delta.Content:
					// Successfully sent
				case <-ctx.Done():
					// Context cancelled, exit immediately
					return
				}
			}
		}
	}()

	return tokens, nil
}

// GenerateWithTools implements tool calling
func (p *OpenAIProvider) GenerateWithTools(ctx context.Context, prompt string, tools []interfaces.Tool) (*ToolCallResponse, error) {
	// Convert tools to OpenAI function format
	functions := p.convertToolsToFunctions(tools)

	messages := []openai.ChatCompletionMessage{
		{Role: openai.ChatMessageRoleUser, Content: prompt},
	}

	model := p.GetModel("")
	maxTokens := p.GetMaxTokens(0)
	temperature := p.GetTemperature(0)

	resp, err := p.client.CreateChatCompletion(ctx, openai.ChatCompletionRequest{
		Model:       model,
		Messages:    messages,
		MaxTokens:   maxTokens,
		Temperature: float32(temperature),
		Functions:   functions,
	})
	if err != nil {
		return nil, agentErrors.NewLLMRequestError(string(constants.ProviderOpenAI), model, err).
			WithContext("tool_calling", true)
	}

	if len(resp.Choices) == 0 {
		return nil, agentErrors.NewLLMResponseError(string(constants.ProviderOpenAI), model, "no choices in response")
	}

	choice := resp.Choices[0]
	result := &ToolCallResponse{
		Content: choice.Message.Content,
	}

	// Parse function calls
	if choice.Message.FunctionCall != nil {
		var args map[string]interface{}
		if err := json.Unmarshal([]byte(choice.Message.FunctionCall.Arguments), &args); err != nil {
			return nil, agentErrors.NewParserInvalidJSONError(choice.Message.FunctionCall.Arguments, err).
				WithContext("function_name", choice.Message.FunctionCall.Name)
		}

		result.ToolCalls = []ToolCall{
			{
				ID:        generateCallID(),
				Name:      choice.Message.FunctionCall.Name,
				Arguments: args,
			},
		}
	}

	return result, nil
}

// StreamWithTools implements streaming tool calls
func (p *OpenAIProvider) StreamWithTools(ctx context.Context, prompt string, tools []interfaces.Tool) (<-chan ToolChunk, error) {
	chunks := make(chan ToolChunk, 100)
	functions := p.convertToolsToFunctions(tools)

	model := p.GetModel("")
	maxTokens := p.GetMaxTokens(0)
	temperature := p.GetTemperature(0)

	stream, err := p.client.CreateChatCompletionStream(ctx, openai.ChatCompletionRequest{
		Model: model,
		Messages: []openai.ChatCompletionMessage{
			{Role: openai.ChatMessageRoleUser, Content: prompt},
		},
		Functions:   functions,
		MaxTokens:   maxTokens,
		Temperature: float32(temperature),
		Stream:      true,
	})
	if err != nil {
		return nil, agentErrors.NewLLMRequestError(string(constants.ProviderOpenAI), model, err).
			WithContext("stream", true).
			WithContext("tool_calling", true)
	}

	go func() {
		defer close(chunks)
		defer func() { _ = stream.Close() }()

		var currentCall *ToolCall
		var argsBuffer string

		for {
			response, err := stream.Recv()
			if err != nil {
				if errors.Is(err, io.EOF) {
					// Finalize last tool call if exists
					if currentCall != nil && argsBuffer != "" {
						var args map[string]interface{}
						if err := json.Unmarshal([]byte(argsBuffer), &args); err == nil {
							currentCall.Arguments = args
							select {
							case chunks <- ToolChunk{Type: "tool_call", Value: currentCall}:
								// Successfully sent
							case <-ctx.Done():
								// Context cancelled, exit immediately
								return
							}
						}
					}
					return
				}
				return
			}

			if len(response.Choices) == 0 {
				continue
			}

			choice := response.Choices[0]

			// Handle content
			if choice.Delta.Content != "" {
				select {
				case chunks <- ToolChunk{Type: "content", Value: choice.Delta.Content}:
					// Successfully sent
				case <-ctx.Done():
					// Context cancelled, exit immediately
					return
				}
			}

			// Handle function calls
			if choice.Delta.FunctionCall != nil {
				if choice.Delta.FunctionCall.Name != "" {
					// New function call
					if currentCall != nil && argsBuffer != "" {
						// Finalize previous call
						var args map[string]interface{}
						if err := json.Unmarshal([]byte(argsBuffer), &args); err == nil {
							currentCall.Arguments = args
							select {
							case chunks <- ToolChunk{Type: "tool_call", Value: currentCall}:
								// Successfully sent
							case <-ctx.Done():
								// Context cancelled, exit immediately
								return
							}
						}
					}

					currentCall = &ToolCall{
						ID:   generateCallID(),
						Name: choice.Delta.FunctionCall.Name,
					}
					argsBuffer = ""
					select {
					case chunks <- ToolChunk{Type: "tool_name", Value: choice.Delta.FunctionCall.Name}:
						// Successfully sent
					case <-ctx.Done():
						// Context cancelled, exit immediately
						return
					}
				}

				if choice.Delta.FunctionCall.Arguments != "" {
					argsBuffer += choice.Delta.FunctionCall.Arguments
					select {
					case chunks <- ToolChunk{Type: "tool_args", Value: choice.Delta.FunctionCall.Arguments}:
						// Successfully sent
					case <-ctx.Done():
						// Context cancelled, exit immediately
						return
					}
				}
			}
		}
	}()

	return chunks, nil
}

// Embed generates embeddings for text
func (p *OpenAIProvider) Embed(ctx context.Context, text string) ([]float64, error) {
	textPreview := text
	if len(text) > 100 {
		textPreview = text[:100] + "..."
	}

	resp, err := p.client.CreateEmbeddings(ctx, openai.EmbeddingRequest{
		Input: []string{text},
		Model: openai.AdaEmbeddingV2,
	})
	if err != nil {
		return nil, agentErrors.NewRetrievalEmbeddingError(textPreview, err).
			WithContext("model", string(openai.AdaEmbeddingV2))
	}

	if len(resp.Data) == 0 {
		return nil, agentErrors.NewLLMResponseError(string(constants.ProviderOpenAI), string(openai.AdaEmbeddingV2), "no embeddings in response")
	}

	// Convert float32 to float64
	embedding := resp.Data[0].Embedding
	result := make([]float64, len(embedding))
	for i, v := range embedding {
		result[i] = float64(v)
	}

	return result, nil
}

// Provider returns the provider type
func (p *OpenAIProvider) Provider() constants.Provider {
	return constants.ProviderOpenAI
}

// IsAvailable checks if the provider is available
func (p *OpenAIProvider) IsAvailable() bool {
	// NOTE: Using background context with timeout for availability check is acceptable
	// as this is a non-critical health check operation that should be independent
	// of any specific request context
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Try a simple completion to check availability
	_, err := p.client.CreateChatCompletion(ctx, openai.ChatCompletionRequest{
		Model: openai.GPT3Dot5Turbo,
		Messages: []openai.ChatCompletionMessage{
			{Role: openai.ChatMessageRoleUser, Content: "test"},
		},
		MaxTokens: 1,
	})

	return err == nil
}

// ModelName returns the model name
func (p *OpenAIProvider) ModelName() string {
	return p.GetModel("")
}

// MaxTokens returns the max tokens setting
func (p *OpenAIProvider) MaxTokens() int {
	return p.GetMaxTokens(0)
}

// convertToolsToFunctions converts our tools to OpenAI function format
func (p *OpenAIProvider) convertToolsToFunctions(tools []interfaces.Tool) []openai.FunctionDefinition {
	functions := make([]openai.FunctionDefinition, len(tools))

	for i, tool := range tools {
		functions[i] = openai.FunctionDefinition{
			Name:        tool.Name(),
			Description: tool.Description(),
			Parameters:  p.toolSchemaToJSON(tool.ArgsSchema()),
		}
	}

	return functions
}

// toolSchemaToJSON converts tool schema to JSON schema
func (p *OpenAIProvider) toolSchemaToJSON(schema interface{}) interface{} {
	// This is a simplified version - in production you'd want
	// to properly convert the schema to OpenAI's expected format
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

// Helper types for tool calling are defined in types.go

// OpenAIStreamingProvider extends OpenAIProvider with advanced streaming
type OpenAIStreamingProvider struct {
	*OpenAIProvider
}

// NewOpenAIStreaming creates a streaming-optimized provider
func NewOpenAIStreaming(config *agentllm.LLMOptions) (*OpenAIStreamingProvider, error) {
	base, err := NewOpenAI(config)
	if err != nil {
		return nil, err
	}

	return &OpenAIStreamingProvider{
		OpenAIProvider: base,
	}, nil
}

// StreamTokensWithMetadata streams tokens with metadata
func (p *OpenAIStreamingProvider) StreamTokensWithMetadata(ctx context.Context, prompt string) (<-chan TokenWithMetadata, error) {
	tokens := make(chan TokenWithMetadata, 100)

	model := p.GetModel("")
	maxTokens := p.GetMaxTokens(0)
	temperature := p.GetTemperature(0)

	stream, err := p.client.CreateChatCompletionStream(ctx, openai.ChatCompletionRequest{
		Model: model,
		Messages: []openai.ChatCompletionMessage{
			{Role: openai.ChatMessageRoleUser, Content: prompt},
		},
		MaxTokens:   maxTokens,
		Temperature: float32(temperature),
		Stream:      true,
	})
	if err != nil {
		return nil, err
	}

	go func() {
		defer close(tokens)
		defer func() { _ = stream.Close() }()

		tokenCount := 0
		for {
			response, err := stream.Recv()
			if err != nil {
				if errors.Is(err, io.EOF) {
					select {
					case tokens <- TokenWithMetadata{
						Type: "finish",
						Metadata: map[string]interface{}{
							"total_tokens": tokenCount,
						},
					}:
						// Successfully sent
					case <-ctx.Done():
						// Context cancelled, exit immediately
					}
					return
				}
				select {
				case tokens <- TokenWithMetadata{
					Type:  "error",
					Error: err,
				}:
					// Successfully sent
				case <-ctx.Done():
					// Context cancelled, exit immediately
				}
				return
			}

			if len(response.Choices) > 0 {
				choice := response.Choices[0]
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
			}
		}
	}()

	return tokens, nil
}

// TokenWithMetadata represents a streaming token with additional info
type TokenWithMetadata struct {
	Type     string // "token", "error", "finish"
	Content  string
	Error    error
	Metadata map[string]interface{}
}
