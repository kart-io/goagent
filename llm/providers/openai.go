package providers

import (
	"context"
	"io"
	"net/http"
	"os"
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
	// reasoning 表示服务接受 reasoning_effort；不接受时显式推理配置在请求前报错。
	reasoning bool
}

// NewOpenAIWithOptions creates a new OpenAI provider using options pattern
func NewOpenAIWithOptions(opts ...agentllm.ClientOption) (*OpenAIProvider, error) {
	return newOpenAICompatible(openAICompatibleSpec{
		provider:       constants.ProviderOpenAI,
		defaultBaseURL: "https://api.openai.com/v1",
		defaultModel:   openai.GPT4TurboPreview,
		envAPIKey:      constants.EnvOpenAIAPIKey,
		envBaseURL:     constants.EnvOpenAIBaseURL,
		envModel:       constants.EnvOpenAIModel,
		reasoning:      true,
		capabilities: []agentllm.Capability{
			agentllm.CapabilityCompletion,
			agentllm.CapabilityChat,
			agentllm.CapabilityStreaming,
			agentllm.CapabilityStructuredStreaming,
			agentllm.CapabilityToolStreaming,
			agentllm.CapabilityToolCalling,
			agentllm.CapabilityEmbedding,
			agentllm.CapabilityVision,
			agentllm.CapabilityJSON,
		},
	}, opts...)
}

// openAICompatibleSpec 描述走 OpenAI Chat Completions 协议的服务：身份、默认值与声明的能力。
type openAICompatibleSpec struct {
	provider                        constants.Provider
	defaultBaseURL, defaultModel    string
	envAPIKey, envBaseURL, envModel string
	// apiKeyOptional 用于本地服务：未提供密钥时发送占位值，SDK 要求非空。
	apiKeyOptional bool
	// defaultTimeout 仅在调用方未改动默认超时时生效。
	defaultTimeout time.Duration
	reasoning      bool
	capabilities   []agentllm.Capability
}

// openAICompatibleCapabilities 是兼容服务的保守能力集：Embed 固定使用 OpenAI 的向量模型，
// 视觉与 JSON 模式依赖具体模型，因此不声明。
var openAICompatibleCapabilities = []agentllm.Capability{
	agentllm.CapabilityCompletion,
	agentllm.CapabilityChat,
	agentllm.CapabilityStreaming,
	agentllm.CapabilityStructuredStreaming,
	agentllm.CapabilityToolStreaming,
	agentllm.CapabilityToolCalling,
}

func newOpenAICompatible(spec openAICompatibleSpec, opts ...agentllm.ClientOption) (*OpenAIProvider, error) {
	base := common.NewBaseProvider(opts...)
	base.ApplyProviderDefaults(spec.provider, spec.defaultBaseURL, spec.defaultModel, spec.envBaseURL, spec.envModel)

	if spec.apiKeyOptional {
		if err := base.RejectReasoning(nil); err != nil {
			return nil, err
		}
		if base.Config.APIKey == "" && spec.envAPIKey != "" {
			base.Config.APIKey = os.Getenv(spec.envAPIKey)
		}
		if base.Config.APIKey == "" {
			base.Config.APIKey = string(spec.provider)
		}
	} else if err := base.EnsureAPIKey(spec.envAPIKey, spec.provider); err != nil {
		return nil, err
	}

	timeout := compatibleTimeout(base.GetTimeout(), spec.defaultTimeout)

	clientConfig := openai.DefaultConfig(base.Config.APIKey)
	if base.Config.BaseURL != "" {
		clientConfig.BaseURL = base.Config.BaseURL
	}
	clientConfig.HTTPClient = zeroTemperatureDoer{next: retryAfterDoer{next: responseHeaderTimeoutClient(timeout)}}

	return &OpenAIProvider{
		BaseProvider:         base,
		ProviderCapabilities: common.NewProviderCapabilities(spec.capabilities...),
		client:               openai.NewClientWithConfig(clientConfig),
		reasoning:            spec.reasoning,
	}, nil
}

// compatibleTimeout 在调用方保留默认超时时改用服务自身的默认值，显式设置的超时不变。
func compatibleTimeout(configured, serviceDefault time.Duration) time.Duration {
	if serviceDefault > 0 && configured == time.Duration(agentllm.DefaultLLMOptions().Timeout)*time.Second {
		return serviceDefault
	}
	return configured
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

// convertTools 复用现有 JSON Schema 转换，并采用现代工具协议。
func (p *OpenAIProvider) convertTools(tools []interfaces.Tool) []openai.Tool {
	functions := p.convertToolsToFunctions(tools)
	result := make([]openai.Tool, len(functions))
	for i := range functions {
		result[i] = openai.Tool{Type: openai.ToolTypeFunction, Function: &functions[i]}
	}
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
	return p.Config.Provider
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
