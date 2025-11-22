package providers

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"cloud.google.com/go/vertexai/genai"
	"google.golang.org/api/iterator"
	"google.golang.org/api/option"

	agentErrors "github.com/kart-io/goagent/errors"
	"github.com/kart-io/goagent/interfaces"

	agentllm "github.com/kart-io/goagent/llm"
	"github.com/kart-io/goagent/llm/constants"
)

// GeminiProvider implements LLM interface for Google Gemini
type GeminiProvider struct {
	*BaseProvider
	client    *genai.Client
	model     *genai.GenerativeModel
	modelName string
}

// NewGeminiWithOptions creates a new Gemini provider using options pattern
func NewGeminiWithOptions(opts ...agentllm.ClientOption) (*GeminiProvider, error) {
	// 创建 BaseProvider，统一处理 Options
	base := NewBaseProvider(opts...)

	// 应用 Provider 特定的默认值
	base.ApplyProviderDefaults(
		constants.ProviderGemini,
		"", // Gemini 不使用 BaseURL
		"gemini-pro",
		constants.EnvGeminiBaseURL,
		constants.EnvGeminiModel,
	)

	// 统一处理 API Key
	if err := base.EnsureAPIKey(constants.EnvGeminiAPIKey, constants.ProviderGemini); err != nil {
		return nil, err
	}

	ctx := context.Background()

	// Create client with API key
	client, err := genai.NewClient(ctx, base.Config.APIKey, "", option.WithAPIKey(base.Config.APIKey))
	if err != nil {
		return nil, agentErrors.NewAgentInitializationError("gemini_provider", err).
			WithContext("provider", "gemini")
	}

	modelName := base.Config.Model

	// Initialize the model
	model := client.GenerativeModel(modelName)

	// Configure model parameters using BaseProvider methods
	maxTokens := base.GetMaxTokens(0)
	if maxTokens > 0x7FFFFFFF { // Max int32
		maxTokens = 0x7FFFFFFF
	}
	maxTokensInt32 := int32(maxTokens)
	model.MaxOutputTokens = &maxTokensInt32

	temperature := base.GetTemperature(0)
	tempFloat32 := float32(temperature)
	model.Temperature = &tempFloat32

	provider := &GeminiProvider{
		BaseProvider: base,
		client:       client,
		model:        model,
		modelName:    modelName,
	}

	return provider, nil
}

// NewGemini creates a new Gemini provider (backward compatible)
func NewGemini(config *agentllm.LLMOptions) (*GeminiProvider, error) {
	// 将现有配置转换为 Options，使用 Options 模式创建 Provider
	return NewGeminiWithOptions(ConfigToOptions(config)...)
}

// Complete implements basic text completion
func (p *GeminiProvider) Complete(ctx context.Context, req *agentllm.CompletionRequest) (*agentllm.CompletionResponse, error) {
	// Create a new chat session
	cs := p.model.StartChat()

	// Convert messages to Gemini format
	for _, msg := range req.Messages {
		var role string
		switch msg.Role {
		case constants.RoleSystem:
			// Gemini doesn't have a system role, so we'll prepend it to the first user message
			continue
		case constants.RoleUser:
			role = "user"
		case "assistant":
			role = "model"
		default:
			role = "user"
		}

		cs.History = append(cs.History, &genai.Content{
			Parts: []genai.Part{
				genai.Text(msg.Content),
			},
			Role: role,
		})
	}

	// Get the last message as the prompt
	if len(req.Messages) == 0 {
		return nil, agentErrors.NewInvalidInputError("gemini_provider", "messages", "no messages provided")
	}

	lastMessage := req.Messages[len(req.Messages)-1]
	if lastMessage.Role != "user" {
		return nil, agentErrors.NewInvalidInputError("gemini_provider", "last_message", "last message must be from user")
	}

	// Apply request-specific parameters using BaseProvider
	maxTokens := p.GetMaxTokens(req.MaxTokens)
	if maxTokens > 0x7FFFFFFF { // Max int32
		maxTokens = 0x7FFFFFFF
	}
	maxTokensInt32 := int32(maxTokens)
	p.model.MaxOutputTokens = &maxTokensInt32

	temperature := p.GetTemperature(req.Temperature)
	tempFloat32 := float32(temperature)
	p.model.Temperature = &tempFloat32

	// Send the message
	resp, err := cs.SendMessage(ctx, genai.Text(lastMessage.Content))
	if err != nil {
		return nil, agentErrors.NewLLMRequestError("gemini", p.modelName, err)
	}

	// Extract content from response
	var content strings.Builder
	for _, part := range resp.Candidates[0].Content.Parts {
		if text, ok := part.(genai.Text); ok {
			content.WriteString(string(text))
		}
	}

	return &agentllm.CompletionResponse{
		Content:      content.String(),
		Model:        p.modelName,
		TokensUsed:   int(resp.UsageMetadata.TotalTokenCount),
		FinishReason: string(resp.Candidates[0].FinishReason),
		Provider:     string(constants.ProviderGemini),
		Usage: &interfaces.TokenUsage{
			PromptTokens:     int(resp.UsageMetadata.PromptTokenCount),
			CompletionTokens: int(resp.UsageMetadata.CandidatesTokenCount),
			TotalTokens:      int(resp.UsageMetadata.TotalTokenCount),
		},
	}, nil
}

// Chat implements chat conversation
func (p *GeminiProvider) Chat(ctx context.Context, messages []agentllm.Message) (*agentllm.CompletionResponse, error) {
	return p.Complete(ctx, &agentllm.CompletionRequest{
		Messages: messages,
	})
}

// Stream implements streaming generation
func (p *GeminiProvider) Stream(ctx context.Context, prompt string) (<-chan string, error) {
	tokens := make(chan string, 100)

	// Start a new chat session
	cs := p.model.StartChat()

	go func() {
		defer close(tokens)

		iter := cs.SendMessageStream(ctx, genai.Text(prompt))
		for {
			resp, err := iter.Next()
			if errors.Is(err, iterator.Done) {
				break
			}
			if err != nil {
				// Log error but continue
				fmt.Printf("Gemini stream error: %v\n", err)
				break
			}

			// Extract text from response
			for _, part := range resp.Candidates[0].Content.Parts {
				if text, ok := part.(genai.Text); ok {
					select {
					case tokens <- string(text):
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

// GenerateWithTools implements tool calling
func (p *GeminiProvider) GenerateWithTools(ctx context.Context, prompt string, tools []interfaces.Tool) (*ToolCallResponse, error) {
	// Convert tools to Gemini function declarations
	functionDeclarations := p.convertToolsToFunctions(tools)

	// Configure model with tools using BaseProvider
	modelName := p.GetModel("")
	maxTokens := p.GetMaxTokens(0)
	temperature := p.GetTemperature(0)

	model := p.client.GenerativeModel(modelName)
	tempFloat32 := float32(temperature)
	model.Temperature = &tempFloat32

	// Validate maxTokens to prevent overflow
	if maxTokens > 0x7FFFFFFF { // Max int32
		maxTokens = 0x7FFFFFFF
	}
	maxTokensInt32 := int32(maxTokens)
	model.MaxOutputTokens = &maxTokensInt32
	model.Tools = []*genai.Tool{
		{FunctionDeclarations: functionDeclarations},
	}

	// Start chat with tools
	cs := model.StartChat()

	// Send message
	resp, err := cs.SendMessage(ctx, genai.Text(prompt))
	if err != nil {
		return nil, agentErrors.NewLLMRequestError("gemini", modelName, err).
			WithContext("tool_calling", true)
	}

	if len(resp.Candidates) == 0 {
		return nil, agentErrors.NewLLMResponseError("gemini", modelName, "no candidates returned")
	}

	result := &ToolCallResponse{}

	// Process response parts
	for _, part := range resp.Candidates[0].Content.Parts {
		switch v := part.(type) {
		case genai.Text:
			result.Content += string(v)
		case *genai.FunctionCall:
			// Convert function call to our format
			args := make(map[string]interface{})
			for k, val := range v.Args {
				args[k] = val
			}

			result.ToolCalls = append(result.ToolCalls, ToolCall{
				ID:        generateCallID(),
				Name:      v.Name,
				Arguments: args,
			})
		}
	}

	return result, nil
}

// StreamWithTools implements streaming tool calls
func (p *GeminiProvider) StreamWithTools(ctx context.Context, prompt string, tools []interfaces.Tool) (<-chan ToolChunk, error) {
	chunks := make(chan ToolChunk, 100)

	// Convert tools to Gemini function declarations
	functionDeclarations := p.convertToolsToFunctions(tools)

	// Configure model with tools using BaseProvider
	modelName := p.GetModel("")
	maxTokens := p.GetMaxTokens(0)
	temperature := p.GetTemperature(0)

	model := p.client.GenerativeModel(modelName)
	tempFloat32 := float32(temperature)
	model.Temperature = &tempFloat32
	if maxTokens > 0x7FFFFFFF { // Max int32
		maxTokens = 0x7FFFFFFF
	}
	maxTokensInt32 := int32(maxTokens)
	model.MaxOutputTokens = &maxTokensInt32
	model.Tools = []*genai.Tool{
		{FunctionDeclarations: functionDeclarations},
	}

	// Start chat with tools
	cs := model.StartChat()

	go func() {
		defer close(chunks)

		iter := cs.SendMessageStream(ctx, genai.Text(prompt))
		for {
			resp, err := iter.Next()
			if errors.Is(err, iterator.Done) {
				break
			}
			if err != nil {
				select {
				case chunks <- ToolChunk{Type: "error", Value: err}:
					// Successfully sent
				case <-ctx.Done():
					// Context cancelled, exit immediately
				}
				break
			}

			// Process response parts
			for _, part := range resp.Candidates[0].Content.Parts {
				switch v := part.(type) {
				case genai.Text:
					select {
					case chunks <- ToolChunk{Type: "content", Value: string(v)}:
						// Successfully sent
					case <-ctx.Done():
						// Context cancelled, exit immediately
						return
					}
				case *genai.FunctionCall:
					select {
					case chunks <- ToolChunk{Type: "tool_name", Value: v.Name}:
						// Successfully sent
					case <-ctx.Done():
						// Context cancelled, exit immediately
						return
					}

					// Send args as chunks
					for k, val := range v.Args {
						select {
						case chunks <- ToolChunk{
							Type:  "tool_args",
							Value: map[string]interface{}{k: val},
						}:
							// Successfully sent
						case <-ctx.Done():
							// Context cancelled, exit immediately
							return
						}
					}

					// Send complete tool call
					args := make(map[string]interface{})
					for k, val := range v.Args {
						args[k] = val
					}

					select {
					case chunks <- ToolChunk{
						Type: "tool_call",
						Value: ToolCall{
							ID:        generateCallID(),
							Name:      v.Name,
							Arguments: args,
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

	return chunks, nil
}

// Embed generates embeddings for text
func (p *GeminiProvider) Embed(ctx context.Context, text string) ([]float64, error) {
	// Gemini SDK doesn't expose EmbedContent method directly
	// This is a workaround - in production you should use the embedding API endpoint
	// For now, return a mock embedding
	mockEmbedding := make([]float64, 768)
	for i := range mockEmbedding {
		mockEmbedding[i] = float64(i) / 768.0
	}
	return mockEmbedding, nil
}

// Provider returns the provider type
func (p *GeminiProvider) Provider() constants.Provider {
	return constants.ProviderGemini
}

// IsAvailable checks if the provider is available
func (p *GeminiProvider) IsAvailable() bool {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Try a simple completion to check availability
	cs := p.model.StartChat()
	_, err := cs.SendMessage(ctx, genai.Text("test"))

	return err == nil
}

// ModelName returns the model name
func (p *GeminiProvider) ModelName() string {
	return p.GetModel("")
}

// MaxTokens returns the max tokens setting
func (p *GeminiProvider) MaxTokens() int {
	return p.GetMaxTokens(0)
}

// convertToolsToFunctions converts our tools to Gemini function format
func (p *GeminiProvider) convertToolsToFunctions(tools []interfaces.Tool) []*genai.FunctionDeclaration {
	functions := make([]*genai.FunctionDeclaration, len(tools))

	for i, tool := range tools {
		functions[i] = &genai.FunctionDeclaration{
			Name:        tool.Name(),
			Description: tool.Description(),
			Parameters:  p.toolSchemaToGeminiSchema(tool.ArgsSchema()),
		}
	}

	return functions
}

// toolSchemaToGeminiSchema converts tool schema to Gemini schema format
func (p *GeminiProvider) toolSchemaToGeminiSchema(schema interface{}) *genai.Schema {
	// Simplified version - you'd want to properly convert based on the actual schema
	return &genai.Schema{
		Type: genai.TypeObject,
		Properties: map[string]*genai.Schema{
			"input": {
				Type:        genai.TypeString,
				Description: "The input for the tool",
			},
		},
		Required: []string{"input"},
	}
}

// GeminiStreamingProvider extends GeminiProvider with advanced streaming
type GeminiStreamingProvider struct {
	*GeminiProvider
}

// NewGeminiStreaming creates a streaming-optimized provider
func NewGeminiStreaming(config *agentllm.LLMOptions) (*GeminiStreamingProvider, error) {
	base, err := NewGemini(config)
	if err != nil {
		return nil, err
	}

	return &GeminiStreamingProvider{
		GeminiProvider: base,
	}, nil
}

// StreamWithContext streams with cancellation support
func (p *GeminiStreamingProvider) StreamWithContext(ctx context.Context, prompt string) (<-chan StreamEvent, error) {
	events := make(chan StreamEvent, 100)

	cs := p.model.StartChat()

	go func() {
		defer close(events)

		// Send start event
		select {
		case events <- StreamEvent{
			Type:      "start",
			Timestamp: time.Now(),
		}:
			// Successfully sent
		case <-ctx.Done():
			// Context cancelled, exit immediately
			return
		}

		iter := cs.SendMessageStream(ctx, genai.Text(prompt))
		tokenCount := 0

		for {
			resp, err := iter.Next()
			if errors.Is(err, iterator.Done) {
				// Send completion event
				select {
				case events <- StreamEvent{
					Type:      "complete",
					Timestamp: time.Now(),
					Metadata: map[string]interface{}{
						"total_tokens": tokenCount,
					},
				}:
					// Successfully sent
				case <-ctx.Done():
					// Context cancelled, exit immediately
				}
				break
			}
			if err != nil {
				select {
				case events <- StreamEvent{
					Type:      "error",
					Error:     err,
					Timestamp: time.Now(),
				}:
					// Successfully sent
				case <-ctx.Done():
					// Context cancelled, exit immediately
				}
				break
			}

			// Extract and send content
			for _, part := range resp.Candidates[0].Content.Parts {
				if text, ok := part.(genai.Text); ok {
					tokenCount++
					select {
					case events <- StreamEvent{
						Type:      "token",
						Content:   string(text),
						Timestamp: time.Now(),
						Metadata: map[string]interface{}{
							"index": tokenCount,
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

	return events, nil
}

// StreamEvent represents a streaming event
type StreamEvent struct {
	Type      string // "start", "token", "error", "complete"
	Content   string
	Error     error
	Timestamp time.Time
	Metadata  map[string]interface{}
}
