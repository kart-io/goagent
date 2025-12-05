package providers

import (
	"context"
	"errors"
	"fmt"
	"io"
	"sync"
	"time"

	"github.com/sashabaranov/go-openai"

	agentErrors "github.com/kart-io/goagent/errors"
	"github.com/kart-io/goagent/interfaces"
	agentllm "github.com/kart-io/goagent/llm"
	"github.com/kart-io/goagent/llm/common"
	"github.com/kart-io/goagent/llm/constants"
	"github.com/kart-io/goagent/utils/json"
)

// messageSlicePool is a sync.Pool for reusing message slices
// to reduce allocations in high-frequency LLM call paths
var messageSlicePool = sync.Pool{
	New: func() interface{} {
		// Pre-allocate slice with capacity for typical conversation length
		slice := make([]openai.ChatCompletionMessage, 0, 8)
		return &slice
	},
}

// OpenAIProvider implements LLM interface for OpenAI
type OpenAIProvider struct {
	*common.BaseProvider
	*common.ProviderCapabilities
	client *openai.Client
}

// NewOpenAIWithOptions creates a new OpenAI provider using options pattern
func NewOpenAIWithOptions(opts ...agentllm.ClientOption) (*OpenAIProvider, error) {
	// 创建 common.BaseProvider，统一处理 Options
	base := common.NewBaseProvider(opts...)

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
		ProviderCapabilities: common.NewProviderCapabilities(
			agentllm.CapabilityCompletion,
			agentllm.CapabilityChat,
			agentllm.CapabilityStreaming,
			agentllm.CapabilityToolCalling,
			agentllm.CapabilityEmbedding,
			agentllm.CapabilityVision,
			agentllm.CapabilityJSON,
		),
		client: openai.NewClientWithConfig(clientConfig),
	}

	return provider, nil
}

// Complete implements basic text completion
// 优化：使用 sync.Pool 复用消息切片以减少内存分配
func (p *OpenAIProvider) Complete(ctx context.Context, req *agentllm.CompletionRequest) (*agentllm.CompletionResponse, error) {
	// Get message slice from pool
	messagesPtr := messageSlicePool.Get().(*[]openai.ChatCompletionMessage)
	messages := *messagesPtr

	// Ensure capacity and reset length
	if cap(messages) < len(req.Messages) {
		messages = make([]openai.ChatCompletionMessage, len(req.Messages))
	} else {
		messages = messages[:len(req.Messages)]
	}

	// Convert messages
	for i, msg := range req.Messages {
		messages[i] = openai.ChatCompletionMessage{
			Role:    msg.Role,
			Content: msg.Content,
			Name:    msg.Name,
		}
	}

	// 使用 common.BaseProvider 的统一参数处理方法
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

	// Return slice to pool after use
	// Clear sensitive data before returning to pool
	for i := range messages {
		messages[i] = openai.ChatCompletionMessage{}
	}
	messages = messages[:0]
	*messagesPtr = messages
	messageSlicePool.Put(messagesPtr)

	if err != nil {
		return nil, agentErrors.NewLLMRequestError(p.ProviderName(), model, err)
	}

	if len(resp.Choices) == 0 {
		return nil, agentErrors.NewLLMResponseError(p.ProviderName(), model, "no choices in response")
	}

	return &agentllm.CompletionResponse{
		Content:      resp.Choices[0].Message.Content,
		Model:        resp.Model,
		TokensUsed:   resp.Usage.TotalTokens,
		FinishReason: string(resp.Choices[0].FinishReason),
		Provider:     p.ProviderName(),
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
		return nil, agentErrors.NewLLMRequestError(p.ProviderName(), model, err).
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
// 返回 *agentllm.ToolCallResponse 以符合 llm.ToolCallingClient 接口
func (p *OpenAIProvider) GenerateWithTools(ctx context.Context, prompt string, tools []interfaces.Tool) (*agentllm.ToolCallResponse, error) {
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
		return nil, agentErrors.NewLLMRequestError(p.ProviderName(), model, err).
			WithContext("tool_calling", true)
	}

	if len(resp.Choices) == 0 {
		return nil, agentErrors.NewLLMResponseError(p.ProviderName(), model, "no choices in response")
	}

	choice := resp.Choices[0]
	result := &agentllm.ToolCallResponse{
		Content: choice.Message.Content,
	}

	// Parse function calls - 转换为 agentllm.ToolCall 格式
	if choice.Message.FunctionCall != nil {
		result.ToolCalls = []agentllm.ToolCall{
			{
				ID:   common.GenerateCallID(),
				Type: "function",
				Function: struct {
					Name      string `json:"name"`
					Arguments string `json:"arguments"`
				}{
					Name:      choice.Message.FunctionCall.Name,
					Arguments: choice.Message.FunctionCall.Arguments, // 保持原始 JSON 字符串
				},
			},
		}
	}

	return result, nil
}

// StreamWithTools implements streaming tool calls
// 返回 <-chan agentllm.ToolChunk 以符合 llm.ToolCallingClient 接口
func (p *OpenAIProvider) StreamWithTools(ctx context.Context, prompt string, tools []interfaces.Tool) (<-chan agentllm.ToolChunk, error) {
	chunks := make(chan agentllm.ToolChunk, 100)
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
		return nil, agentErrors.NewLLMRequestError(p.ProviderName(), model, err).
			WithContext("stream", true).
			WithContext("tool_calling", true)
	}

	go func() {
		defer close(chunks)
		defer func() { _ = stream.Close() }()

		var currentCall *agentllm.ToolCall
		var argsBuffer string

		for {
			response, err := stream.Recv()
			if err != nil {
				if errors.Is(err, io.EOF) {
					// Finalize last tool call if exists
					if currentCall != nil && argsBuffer != "" {
						currentCall.Function.Arguments = argsBuffer
						select {
						case chunks <- agentllm.ToolChunk{Type: "tool_call", Value: currentCall}:
							// Successfully sent
						case <-ctx.Done():
							// Context cancelled, exit immediately
							return
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
				case chunks <- agentllm.ToolChunk{Type: "content", Value: choice.Delta.Content}:
					// Successfully sent
				case <-ctx.Done():
					// Context cancelled, exit immediately
					return
				}
			}

			// Handle function calls
			if choice.Delta.FunctionCall != nil {
				if choice.Delta.FunctionCall.Name != "" {
					// New function call - finalize previous call first
					if currentCall != nil && argsBuffer != "" {
						currentCall.Function.Arguments = argsBuffer
						select {
						case chunks <- agentllm.ToolChunk{Type: "tool_call", Value: currentCall}:
							// Successfully sent
						case <-ctx.Done():
							// Context cancelled, exit immediately
							return
						}
					}

					// Create new tool call with agentllm.ToolCall format
					currentCall = &agentllm.ToolCall{
						ID:   common.GenerateCallID(),
						Type: "function",
						Function: struct {
							Name      string `json:"name"`
							Arguments string `json:"arguments"`
						}{
							Name: choice.Delta.FunctionCall.Name,
						},
					}
					argsBuffer = ""
					select {
					case chunks <- agentllm.ToolChunk{Type: "tool_name", Value: choice.Delta.FunctionCall.Name}:
						// Successfully sent
					case <-ctx.Done():
						// Context cancelled, exit immediately
						return
					}
				}

				if choice.Delta.FunctionCall.Arguments != "" {
					argsBuffer += choice.Delta.FunctionCall.Arguments
					select {
					case chunks <- agentllm.ToolChunk{Type: "tool_args", Value: choice.Delta.FunctionCall.Arguments}:
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
		return nil, agentErrors.NewLLMResponseError(p.ProviderName(), string(openai.AdaEmbeddingV2), "no embeddings in response")
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

// TokenWithMetadata represents a streaming token with additional info
// Used by advanced streaming implementations
type TokenWithMetadata struct {
	Type     string // "token", "error", "finish"
	Content  string
	Error    error
	Metadata map[string]interface{}
}
