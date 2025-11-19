package providers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/sashabaranov/go-openai"

	agentErrors "github.com/kart-io/goagent/errors"
	"github.com/kart-io/goagent/interfaces"
	agentllm "github.com/kart-io/goagent/llm"
)

// OpenAIProvider implements LLM interface for OpenAI
type OpenAIProvider struct {
	client      *openai.Client
	config      *agentllm.Config
	model       string
	maxTokens   int
	temperature float64
}

// NewOpenAI creates a new OpenAI provider
func NewOpenAI(config *agentllm.Config) (*OpenAIProvider, error) {
	// Get API key from config or env
	apiKey := config.APIKey
	if apiKey == "" {
		apiKey = os.Getenv(agentllm.EnvOpenAIAPIKey)
	}

	if apiKey == "" {
		return nil, agentErrors.NewInvalidConfigError(ProviderOpenAI, agentllm.ErrorFieldAPIKey, "OpenAI API key is required")
	}

	clientConfig := openai.DefaultConfig(apiKey)

	// Set base URL with fallback
	baseURL := config.BaseURL
	if baseURL == "" {
		baseURL = os.Getenv(agentllm.EnvOpenAIBaseURL)
	}
	if baseURL != "" {
		clientConfig.BaseURL = baseURL
	}

	// Set model with fallback
	model := config.Model
	if model == "" {
		model = os.Getenv(agentllm.EnvOpenAIModel)
	}
	if model == "" {
		model = openai.GPT4TurboPreview
	}

	// Set other parameters with defaults
	maxTokens := config.MaxTokens
	if maxTokens == 0 {
		maxTokens = DefaultMaxTokens
	}

	temperature := config.Temperature
	if temperature == 0 {
		temperature = DefaultTemperature
	}

	provider := &OpenAIProvider{
		client:      openai.NewClientWithConfig(clientConfig),
		config:      config,
		model:       model,
		maxTokens:   maxTokens,
		temperature: temperature,
	}

	return provider, nil
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

	model := p.model
	if req.Model != "" {
		model = req.Model
	}

	maxTokens := p.maxTokens
	if req.MaxTokens > 0 {
		maxTokens = req.MaxTokens
	}

	temperature := p.temperature
	if req.Temperature > 0 {
		temperature = req.Temperature
	}

	resp, err := p.client.CreateChatCompletion(ctx, openai.ChatCompletionRequest{
		Model:       model,
		Messages:    messages,
		MaxTokens:   maxTokens,
		Temperature: float32(temperature),
		Stop:        req.Stop,
		TopP:        float32(req.TopP),
	})
	if err != nil {
		return nil, agentErrors.NewLLMRequestError(ProviderOpenAI, model, err)
	}

	if len(resp.Choices) == 0 {
		return nil, agentErrors.NewLLMResponseError(ProviderOpenAI, model, ErrNoCompletionChoices)
	}

	return &agentllm.CompletionResponse{
		Content:      resp.Choices[0].Message.Content,
		Model:        resp.Model,
		TokensUsed:   resp.Usage.TotalTokens,
		FinishReason: string(resp.Choices[0].FinishReason),
		Provider:     string(agentllm.ProviderOpenAI),
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

	stream, err := p.client.CreateChatCompletionStream(ctx, openai.ChatCompletionRequest{
		Model: p.model,
		Messages: []openai.ChatCompletionMessage{
			{Role: openai.ChatMessageRoleUser, Content: prompt},
		},
		MaxTokens:   p.maxTokens,
		Temperature: float32(p.temperature),
		Stream:      true,
	})
	if err != nil {
		return nil, agentErrors.NewLLMRequestError(ProviderOpenAI, p.model, err).
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

	resp, err := p.client.CreateChatCompletion(ctx, openai.ChatCompletionRequest{
		Model:       p.model,
		Messages:    messages,
		MaxTokens:   p.maxTokens,
		Temperature: float32(p.temperature),
		Functions:   functions,
	})
	if err != nil {
		return nil, agentErrors.NewLLMRequestError(ProviderOpenAI, p.model, err).
			WithContext("tool_calling", true)
	}

	if len(resp.Choices) == 0 {
		return nil, agentErrors.NewLLMResponseError(ProviderOpenAI, p.model, ErrNoChoicesReturned)
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

	stream, err := p.client.CreateChatCompletionStream(ctx, openai.ChatCompletionRequest{
		Model: p.model,
		Messages: []openai.ChatCompletionMessage{
			{Role: openai.ChatMessageRoleUser, Content: prompt},
		},
		Functions:   functions,
		MaxTokens:   p.maxTokens,
		Temperature: float32(p.temperature),
		Stream:      true,
	})
	if err != nil {
		return nil, agentErrors.NewLLMRequestError(ProviderOpenAI, p.model, err).
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
		return nil, agentErrors.NewLLMResponseError(ProviderOpenAI, string(openai.AdaEmbeddingV2), ErrNoEmbeddingsReturned)
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
func (p *OpenAIProvider) Provider() agentllm.Provider {
	return agentllm.ProviderOpenAI
}

// IsAvailable checks if the provider is available
func (p *OpenAIProvider) IsAvailable() bool {
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
	return p.model
}

// MaxTokens returns the max tokens setting
func (p *OpenAIProvider) MaxTokens() int {
	return p.maxTokens
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
func NewOpenAIStreaming(config *agentllm.Config) (*OpenAIStreamingProvider, error) {
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

	stream, err := p.client.CreateChatCompletionStream(ctx, openai.ChatCompletionRequest{
		Model: p.model,
		Messages: []openai.ChatCompletionMessage{
			{Role: openai.ChatMessageRoleUser, Content: prompt},
		},
		MaxTokens:   p.maxTokens,
		Temperature: float32(p.temperature),
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
