package providers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/sashabaranov/go-openai"

	"github.com/kart-io/goagent/llm"
	"github.com/kart-io/goagent/tools"
)

// OpenAIProvider implements LLM interface for OpenAI
type OpenAIProvider struct {
	client      *openai.Client
	config      *llm.Config
	model       string
	maxTokens   int
	temperature float64
}

// NewOpenAI creates a new OpenAI provider
func NewOpenAI(config *llm.Config) (*OpenAIProvider, error) {
	if config.APIKey == "" {
		return nil, fmt.Errorf("OpenAI API key is required")
	}

	clientConfig := openai.DefaultConfig(config.APIKey)
	if config.BaseURL != "" {
		clientConfig.BaseURL = config.BaseURL
	}

	provider := &OpenAIProvider{
		client:      openai.NewClientWithConfig(clientConfig),
		config:      config,
		model:       config.Model,
		maxTokens:   config.MaxTokens,
		temperature: config.Temperature,
	}

	// Set defaults if not provided
	if provider.model == "" {
		provider.model = openai.GPT4TurboPreview
	}
	if provider.maxTokens == 0 {
		provider.maxTokens = 2000
	}
	if provider.temperature == 0 {
		provider.temperature = 0.7
	}

	return provider, nil
}

// Complete implements basic text completion
func (p *OpenAIProvider) Complete(ctx context.Context, req *llm.CompletionRequest) (*llm.CompletionResponse, error) {
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
		return nil, fmt.Errorf("OpenAI completion failed: %w", err)
	}

	if len(resp.Choices) == 0 {
		return nil, fmt.Errorf("no completion choices returned")
	}

	return &llm.CompletionResponse{
		Content:      resp.Choices[0].Message.Content,
		Model:        resp.Model,
		TokensUsed:   resp.Usage.TotalTokens,
		FinishReason: string(resp.Choices[0].FinishReason),
		Provider:     string(llm.ProviderOpenAI),
	}, nil
}

// Chat implements chat conversation
func (p *OpenAIProvider) Chat(ctx context.Context, messages []llm.Message) (*llm.CompletionResponse, error) {
	return p.Complete(ctx, &llm.CompletionRequest{
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
		return nil, fmt.Errorf("failed to create stream: %w", err)
	}

	go func() {
		defer close(tokens)
		defer stream.Close()

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
				tokens <- response.Choices[0].Delta.Content
			}
		}
	}()

	return tokens, nil
}

// GenerateWithTools implements tool calling
func (p *OpenAIProvider) GenerateWithTools(ctx context.Context, prompt string, tools []tools.Tool) (*ToolCallResponse, error) {
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
		return nil, fmt.Errorf("OpenAI tool calling failed: %w", err)
	}

	if len(resp.Choices) == 0 {
		return nil, fmt.Errorf("no choices returned")
	}

	choice := resp.Choices[0]
	result := &ToolCallResponse{
		Content: choice.Message.Content,
	}

	// Parse function calls
	if choice.Message.FunctionCall != nil {
		var args map[string]interface{}
		if err := json.Unmarshal([]byte(choice.Message.FunctionCall.Arguments), &args); err != nil {
			return nil, fmt.Errorf("failed to parse function arguments: %w", err)
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
func (p *OpenAIProvider) StreamWithTools(ctx context.Context, prompt string, tools []tools.Tool) (<-chan ToolChunk, error) {
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
		return nil, fmt.Errorf("failed to create tool stream: %w", err)
	}

	go func() {
		defer close(chunks)
		defer stream.Close()

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
							chunks <- ToolChunk{Type: "tool_call", Value: currentCall}
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
				chunks <- ToolChunk{Type: "content", Value: choice.Delta.Content}
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
							chunks <- ToolChunk{Type: "tool_call", Value: currentCall}
						}
					}

					currentCall = &ToolCall{
						ID:   generateCallID(),
						Name: choice.Delta.FunctionCall.Name,
					}
					argsBuffer = ""
					chunks <- ToolChunk{Type: "tool_name", Value: choice.Delta.FunctionCall.Name}
				}

				if choice.Delta.FunctionCall.Arguments != "" {
					argsBuffer += choice.Delta.FunctionCall.Arguments
					chunks <- ToolChunk{Type: "tool_args", Value: choice.Delta.FunctionCall.Arguments}
				}
			}
		}
	}()

	return chunks, nil
}

// Embed generates embeddings for text
func (p *OpenAIProvider) Embed(ctx context.Context, text string) ([]float64, error) {
	resp, err := p.client.CreateEmbeddings(ctx, openai.EmbeddingRequest{
		Input: []string{text},
		Model: openai.AdaEmbeddingV2,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create embeddings: %w", err)
	}

	if len(resp.Data) == 0 {
		return nil, fmt.Errorf("no embeddings returned")
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
func (p *OpenAIProvider) Provider() llm.Provider {
	return llm.ProviderOpenAI
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
func (p *OpenAIProvider) convertToolsToFunctions(tools []tools.Tool) []openai.FunctionDefinition {
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

// Helper types for tool calling

// ToolCallResponse represents the response from LLM with tool calls
type ToolCallResponse struct {
	Content   string
	ToolCalls []ToolCall
}

// ToolCall represents a single tool invocation
type ToolCall struct {
	ID        string
	Name      string
	Arguments map[string]interface{}
}

// ToolChunk represents a streaming chunk of tool call
type ToolChunk struct {
	Type  string // "content", "tool_name", "tool_args", "tool_call"
	Value interface{}
}

// generateCallID generates a unique ID for tool calls
func generateCallID() string {
	return fmt.Sprintf("call_%d", time.Now().UnixNano())
}

// OpenAIStreamingProvider extends OpenAIProvider with advanced streaming
type OpenAIStreamingProvider struct {
	*OpenAIProvider
}

// NewOpenAIStreaming creates a streaming-optimized provider
func NewOpenAIStreaming(config *llm.Config) (*OpenAIStreamingProvider, error) {
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
		defer stream.Close()

		tokenCount := 0
		for {
			response, err := stream.Recv()
			if err != nil {
				if errors.Is(err, io.EOF) {
					tokens <- TokenWithMetadata{
						Type: "finish",
						Metadata: map[string]interface{}{
							"total_tokens": tokenCount,
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

			if len(response.Choices) > 0 {
				choice := response.Choices[0]
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
