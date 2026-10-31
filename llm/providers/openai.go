package providers

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/sashabaranov/go-openai"

	agentErrors "github.com/kart-io/goagent/errors"
	"github.com/kart-io/goagent/interfaces"
	agentllm "github.com/kart-io/goagent/llm"
	"github.com/kart-io/goagent/llm/common"
	"github.com/kart-io/goagent/llm/constants"
	"github.com/kart-io/goagent/utils/json"
)

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
	clientConfig.HTTPClient = retryAfterDoer{next: responseHeaderTimeoutClient(base.GetTimeout())}

	provider := &OpenAIProvider{
		BaseProvider: base,
		ProviderCapabilities: common.NewProviderCapabilities(
			agentllm.CapabilityCompletion,
			agentllm.CapabilityChat,
			agentllm.CapabilityStreaming,
			agentllm.CapabilityStructuredStreaming,
			agentllm.CapabilityToolStreaming,
			agentllm.CapabilityToolCalling,
			agentllm.CapabilityEmbedding,
			agentllm.CapabilityVision,
			agentllm.CapabilityJSON,
		),
		client: openai.NewClientWithConfig(clientConfig),
	}

	return provider, nil
}

// responseHeaderTimeoutClient 把超时用作拿到响应头的时限：非流式请求在生成完毕后才返回响应头，因而整体受限；
// 流式请求的响应头随首块到达，之后的读取只受 ctx 控制，长回复不会被截断。
func responseHeaderTimeoutClient(timeout time.Duration) *http.Client {
	return &http.Client{Transport: headerTimeoutTransport{timeout: timeout}}
}

// headerTimeoutTransport 每次请求都使用当时的 http.DefaultTransport（宿主可能替换为 otelhttp、httpmock 等任意实现），
// 连接池也由它统一持有。计时从发起请求开始，覆盖建连与写请求。
type headerTimeoutTransport struct{ timeout time.Duration }

func (t headerTimeoutTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if t.timeout <= 0 {
		return http.DefaultTransport.RoundTrip(req)
	}
	ctx, cancel := context.WithCancel(req.Context())
	timer := time.AfterFunc(t.timeout, cancel)
	resp, err := http.DefaultTransport.RoundTrip(req.WithContext(ctx))
	// 定时器已触发时 ctx 已取消，即便响应恰好返回其 Body 也不可读，统一按超时处理。
	if !timer.Stop() {
		if err == nil {
			_ = resp.Body.Close()
		}
		return nil, errResponseHeaderTimeout
	}
	if err != nil {
		cancel()
		return nil, err
	}
	resp.Body = &cancelOnCloseBody{ReadCloser: resp.Body, cancel: cancel}
	return resp, nil
}

type cancelOnCloseBody struct {
	io.ReadCloser
	cancel context.CancelFunc
}

func (b *cancelOnCloseBody) Close() error {
	err := b.ReadCloser.Close()
	b.cancel()
	return err
}

// errResponseHeaderTimeout 实现 net.Error 且 Timeout() 为 true，与 http.Transport.ResponseHeaderTimeout 的错误可同样识别。
var errResponseHeaderTimeout error = responseHeaderTimeoutError{}

type responseHeaderTimeoutError struct{}

func (responseHeaderTimeoutError) Error() string   { return "等待响应头超时" }
func (responseHeaderTimeoutError) Timeout() bool   { return true }
func (responseHeaderTimeoutError) Temporary() bool { return true }

// Complete implements basic text completion
// 优化：使用 sync.Pool 复用消息切片以减少内存分配
func (p *OpenAIProvider) Complete(ctx context.Context, req *agentllm.CompletionRequest) (*agentllm.CompletionResponse, error) {
	// Convert messages
	var messages []openai.ChatCompletionMessage
	if p.Config.SystemPrompt != "" {
		messages = append(messages, openai.ChatCompletionMessage{
			Role:    openai.ChatMessageRoleSystem,
			Content: p.Config.SystemPrompt,
		})
	}

	for _, msg := range req.Messages {
		messages = append(messages, openai.ChatCompletionMessage{
			Role:    msg.Role,
			Content: msg.Content,
			Name:    msg.Name,
		})
	}

	// 使用 common.BaseProvider 的统一参数处理方法
	model := p.GetModel(req.Model)
	maxTokens := p.GetMaxTokens(req.MaxTokens)
	temperature := p.GetTemperature(req.Temperature)

	ctx, retryAfter := withRetryAfterSlot(ctx)
	resp, err := p.client.CreateChatCompletion(ctx, openai.ChatCompletionRequest{
		Model:       model,
		Messages:    messages,
		MaxTokens:   maxTokens,
		Temperature: float32(temperature),
		Stop:        req.Stop,
		TopP:        float32(req.TopP),
	})
	if err != nil {
		return nil, agentErrors.NewLLMRequestError(p.ProviderName(), model, openAIProviderError(err, *retryAfter))
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
// 超时只约束拿到响应头，流开始后不再计时；消费者不再读取时必须取消 ctx，否则 goroutine 与连接不会释放。
func (p *OpenAIProvider) Stream(ctx context.Context, prompt string) (<-chan string, error) {
	tokens := make(chan string, 100)

	model := p.GetModel("")
	maxTokens := p.GetMaxTokens(0)
	temperature := p.GetTemperature(0)

	messages := []openai.ChatCompletionMessage{}
	if p.Config.SystemPrompt != "" {
		messages = append(messages, openai.ChatCompletionMessage{
			Role:    openai.ChatMessageRoleSystem,
			Content: p.Config.SystemPrompt,
		})
	}
	messages = append(messages, openai.ChatCompletionMessage{
		Role:    openai.ChatMessageRoleUser,
		Content: prompt,
	})

	ctx, retryAfter := withRetryAfterSlot(ctx)
	stream, err := p.client.CreateChatCompletionStream(ctx, openai.ChatCompletionRequest{
		Model:       model,
		Messages:    messages,
		MaxTokens:   maxTokens,
		Temperature: float32(temperature),
		Stream:      true,
	})
	if err != nil {
		return nil, agentErrors.NewLLMRequestError(p.ProviderName(), model, openAIProviderError(err, *retryAfter)).
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
	// 使用当前工具协议；响应仍兼容旧 function_call。
	toolDefinitions := p.convertTools(tools)

	messages := []openai.ChatCompletionMessage{}
	if p.Config.SystemPrompt != "" {
		messages = append(messages, openai.ChatCompletionMessage{
			Role:    openai.ChatMessageRoleSystem,
			Content: p.Config.SystemPrompt,
		})
	}
	messages = append(messages, openai.ChatCompletionMessage{
		Role:    openai.ChatMessageRoleUser,
		Content: prompt,
	})

	model := p.GetModel("")
	maxTokens := p.GetMaxTokens(0)
	temperature := p.GetTemperature(0)

	ctx, retryAfter := withRetryAfterSlot(ctx)
	resp, err := p.client.CreateChatCompletion(ctx, openai.ChatCompletionRequest{
		Model:       model,
		Messages:    messages,
		MaxTokens:   maxTokens,
		Temperature: float32(temperature),
		Tools:       toolDefinitions,
	})
	if err != nil {
		return nil, agentErrors.NewLLMRequestError(p.ProviderName(), model, openAIProviderError(err, *retryAfter)).
			WithContext("tool_calling", true)
	}

	if len(resp.Choices) == 0 {
		return nil, agentErrors.NewLLMResponseError(p.ProviderName(), model, "no choices in response")
	}

	choice := resp.Choices[0]
	result := &agentllm.ToolCallResponse{Content: choice.Message.Content}
	// SDK 将缺失 usage 解码为零值；不把无法确认的零值声明为已报告用量。
	if resp.Usage.TotalTokens > 0 || resp.Usage.PromptTokens > 0 || resp.Usage.CompletionTokens > 0 {
		result.Usage = &interfaces.TokenUsage{
			PromptTokens: resp.Usage.PromptTokens, CompletionTokens: resp.Usage.CompletionTokens, TotalTokens: resp.Usage.TotalTokens,
		}
	}

	for _, call := range choice.Message.ToolCalls {
		result.ToolCalls = append(result.ToolCalls, toToolCall(call))
	}

	// Parse function calls - 转换为 agentllm.ToolCall 格式
	if len(result.ToolCalls) == 0 && choice.Message.FunctionCall != nil {
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
// 超时只约束拿到响应头，流开始后不再计时；消费者不再读取时必须取消 ctx，否则 goroutine 与连接不会释放。
func (p *OpenAIProvider) StreamWithTools(ctx context.Context, prompt string, tools []interfaces.Tool) (<-chan agentllm.ToolChunk, error) {
	messages := []openai.ChatCompletionMessage{}
	if p.Config.SystemPrompt != "" {
		messages = append(messages, openai.ChatCompletionMessage{Role: "system", Content: p.Config.SystemPrompt})
	}
	messages = append(messages, openai.ChatCompletionMessage{Role: "user", Content: prompt})
	ctx, retryAfter := withRetryAfterSlot(ctx)
	stream, err := p.client.CreateChatCompletionStream(ctx, openai.ChatCompletionRequest{
		Model: p.GetModel(""), Messages: messages, Tools: p.convertTools(tools),
		MaxTokens: p.GetMaxTokens(0), Temperature: float32(p.GetTemperature(0)), Stream: true,
	})
	if err != nil {
		return nil, openAIProviderError(err, *retryAfter)
	}
	chunks := make(chan agentllm.ToolChunk, 1)
	go func() {
		defer close(chunks)
		defer func() { _ = stream.Close() }()
		send := func(kind string, value any) bool {
			select {
			case chunks <- agentllm.ToolChunk{Type: kind, Value: value}:
				return true
			case <-ctx.Done():
				return false
			}
		}
		calls := map[int]*agentllm.ToolCall{}
		order := []int{}
		finished := false
		for {
			response, err := stream.Recv()
			if err != nil {
				if !errors.Is(err, io.EOF) {
					send("error", err)
					return
				}
				if !finished {
					send("error", io.ErrUnexpectedEOF)
					return
				}
				for _, index := range order {
					call := calls[index]
					if call.Function.Name == "" {
						send("error", fmt.Errorf("工具调用缺少名称"))
						return
					}
					if call.ID == "" {
						call.ID = common.GenerateCallID()
					}
					if !send("tool_call", call) {
						return
					}
				}
				return
			}
			if len(response.Choices) == 0 {
				continue
			}
			choice := response.Choices[0]
			if choice.FinishReason != "" {
				finished = true
			}
			if choice.Delta.Content != "" && !send("content", choice.Delta.Content) {
				return
			}
			deltas := choice.Delta.ToolCalls
			// 旧服务只返回 function_call 时按索引 0 合并，不重复计入现代协议。
			if len(deltas) == 0 && choice.Delta.FunctionCall != nil {
				index := 0
				deltas = []openai.ToolCall{{Index: &index, Type: openai.ToolTypeFunction, Function: *choice.Delta.FunctionCall}}
			}
			for _, delta := range deltas {
				index := 0
				if delta.Index != nil {
					index = *delta.Index
				}
				call := calls[index]
				if call == nil {
					call = &agentllm.ToolCall{ID: delta.ID, Type: "function"}
					calls[index] = call
					order = append(order, index)
				}
				if delta.ID != "" {
					call.ID = delta.ID
				}
				if delta.Function.Name != "" {
					call.Function.Name += delta.Function.Name
					if !send("tool_name", delta.Function.Name) {
						return
					}
				}
				if delta.Function.Arguments != "" {
					call.Function.Arguments += delta.Function.Arguments
					if !send("tool_args", delta.Function.Arguments) {
						return
					}
				}
			}
		}
	}()
	return chunks, nil
}

// convertTools 复用现有 JSON Schema 转换，并采用现代工具协议。
func (p *OpenAIProvider) convertTools(tools []interfaces.Tool) []openai.Tool {
	functions := p.convertToolsToFunctions(tools)
	result := make([]openai.Tool, len(functions))
	for i := range functions {
		result[i] = openai.Tool{Type: openai.ToolTypeFunction, Function: &functions[i]}
	}
	return result
}

func toToolCall(call openai.ToolCall) agentllm.ToolCall {
	result := agentllm.ToolCall{ID: call.ID, Type: string(call.Type)}
	result.Function.Name = call.Function.Name
	result.Function.Arguments = call.Function.Arguments
	return result
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
