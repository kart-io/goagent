package providers

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"

	"github.com/sashabaranov/go-openai"

	"github.com/kart-io/goagent/codec/json"
	shared "github.com/kart-io/goagent/constants"
	agentErrors "github.com/kart-io/goagent/errors"
	"github.com/kart-io/goagent/interfaces"
	agentllm "github.com/kart-io/goagent/llm"
	"github.com/kart-io/goagent/llm/common"
)

// OpenAIProvider implements LLM interface for OpenAI
type OpenAIProvider struct {
	*common.BaseProvider
	*common.ProviderCapabilities
	client *openai.Client
	// reasoning 表示服务接受 reasoning_effort；不接受时显式推理配置在请求前报错。
	reasoning bool
	// thinkingSwitch 为 true 时，effort=none 不发送 reasoning_effort。
	thinkingSwitch bool
	// imageCompletionTokens 为 true 时，带图片的请求把输出上限发为 max_completion_tokens。
	imageCompletionTokens bool
	// thinkingNoneOnly 用于不接受 reasoning_effort 的服务（DeepSeek）：只接受 effort=none 并改发 thinking 禁用，
	// 其余推理配置仍在请求前报错。
	thinkingNoneOnly bool
}

// NewOpenAIWithOptions creates a new OpenAI provider using options pattern
func NewOpenAIWithOptions(opts ...agentllm.ClientOption) (*OpenAIProvider, error) {
	return newOpenAICompatible(openAICompatibleSpec{
		provider:       shared.ProviderOpenAI,
		defaultBaseURL: "https://api.openai.com/v1",
		defaultModel:   openai.GPT4TurboPreview,
		envAPIKey:      shared.EnvOpenAIAPIKey,
		envBaseURL:     shared.EnvOpenAIBaseURL,
		envModel:       shared.EnvOpenAIModel,
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
	provider                        shared.Provider
	defaultBaseURL, defaultModel    string
	envAPIKey, envBaseURL, envModel string
	// apiKeyOptional 用于本地服务：未提供密钥时发送占位值，SDK 要求非空。
	apiKeyOptional bool
	reasoning      bool
	// thinkingSwitch 表示服务用 thinking 开关控制推理：effort=none 时按模型改写为 thinking 禁用。
	thinkingSwitch bool
	// imageCompletionTokens 表示服务的视觉接口只读 max_completion_tokens、忽略 max_tokens。
	imageCompletionTokens bool
	capabilities          []agentllm.Capability
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
	if err := validateEmbeddingDimensions(spec.provider, base.Config.EmbeddingDimensions); err != nil {
		return nil, err
	}

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

	timeout := base.GetTimeout()

	clientConfig := openai.DefaultConfig(base.Config.APIKey)
	if base.Config.BaseURL != "" {
		clientConfig.BaseURL = base.Config.BaseURL
	}
	var doer openai.HTTPDoer = retryAfterDoer{next: nonSSEDoer{next: responseHeaderTimeoutClient(timeout, base.Config.CustomHeaders, base.Config.HTTPClient)}}
	if spec.thinkingSwitch {
		doer = thinkingDisabledDoer{next: doer}
	}
	clientConfig.HTTPClient = zeroTemperatureDoer{next: doer}

	return &OpenAIProvider{
		BaseProvider:          base,
		ProviderCapabilities:  common.NewProviderCapabilities(spec.capabilities...),
		client:                openai.NewClientWithConfig(clientConfig),
		reasoning:             spec.reasoning,
		thinkingSwitch:        spec.thinkingSwitch,
		imageCompletionTokens: spec.imageCompletionTokens,
	}, nil
}

// responseHeaderTimeoutClient 把超时用作拿到响应头的时限：非流式请求在生成完毕后才返回响应头，因而整体受限；
// 流式请求的响应头随首块到达，之后的读取只受 ctx 控制，长回复不会被截断。
// headers 来自 WithCustomHeaders，覆盖 SDK 设置的同名头。
// injected 来自 WithHTTPClient：其 Transport 取代 http.DefaultTransport，CheckRedirect 原样沿用。
func responseHeaderTimeoutClient(timeout time.Duration, headers map[string]string, injected *http.Client) *http.Client {
	t := headerTimeoutTransport{timeout: timeout, headers: headers}
	client := &http.Client{}
	if injected != nil {
		t.base = injected.Transport
		client.CheckRedirect = injected.CheckRedirect
	}
	client.Transport = t
	return client
}

// headerTimeoutTransport 默认每次请求都使用当时的 http.DefaultTransport（宿主可能替换为 otelhttp、httpmock 等任意实现），
// 连接池也由它统一持有；base 非空时改用 base。计时从发起请求开始，覆盖建连与写请求。
type headerTimeoutTransport struct {
	timeout time.Duration
	headers map[string]string
	base    http.RoundTripper
}

func (t headerTimeoutTransport) roundTripper() http.RoundTripper {
	if t.base != nil {
		return t.base
	}
	return http.DefaultTransport
}

func (t headerTimeoutTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if len(t.headers) > 0 {
		req = req.Clone(req.Context())
		for key, value := range t.headers {
			req.Header.Set(key, value)
		}
	}
	if t.timeout <= 0 {
		return t.roundTripper().RoundTrip(req)
	}
	ctx, cancel := context.WithCancel(req.Context())
	timer := time.AfterFunc(t.timeout, cancel)
	resp, err := t.roundTripper().RoundTrip(req.WithContext(ctx))
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

// Embed embeds one text through EmbedBatch.
func (p *OpenAIProvider) Embed(ctx context.Context, text string) ([]float32, error) {
	vecs, err := p.EmbedBatch(ctx, []string{text})
	if err != nil {
		return nil, err
	}
	return vecs[0], nil
}

// EmbedBatch sends all texts in one request and returns vectors in input order.
// It uses the model set by llm.WithEmbeddingModel, defaulting to text-embedding-ada-002.
func (p *OpenAIProvider) EmbedBatch(ctx context.Context, texts []string) ([][]float32, error) {
	if len(texts) == 0 {
		return nil, agentErrors.Newf(agentErrors.CodeInvalidInput, "%s: embedding input is required", p.ProviderName()).
			WithComponent(p.ProviderName()).WithOperation("generate_embedding")
	}
	textPreview := texts[0]
	if len(textPreview) > 100 {
		textPreview = textPreview[:100] + "..."
	}
	model := openai.EmbeddingModel(p.Config.EmbeddingModel)
	if model == "" {
		model = openai.AdaEmbeddingV2
	}

	ctx, retryAfter := withRetryAfterSlot(ctx)
	resp, err := p.client.CreateEmbeddings(ctx, openai.EmbeddingRequest{
		Input:      texts,
		Model:      model,
		Dimensions: p.Config.EmbeddingDimensions,
	})
	if err != nil {
		return nil, wrapProviderError(p.ProviderName(), string(model), openAIProviderError(err, *retryAfter)).
			WithContext("text_preview", textPreview).WithOperation("generate_embedding")
	}

	if len(resp.Data) != len(texts) {
		return nil, agentErrors.NewLLMResponseError(p.ProviderName(), string(model),
			fmt.Sprintf("expected %d embeddings, got %d", len(texts), len(resp.Data)))
	}
	out := make([][]float32, len(texts))
	for _, d := range resp.Data {
		if d.Index < 0 || d.Index >= len(texts) || out[d.Index] != nil {
			return nil, agentErrors.NewLLMResponseError(p.ProviderName(), string(model),
				fmt.Sprintf("invalid or duplicate embedding index %d", d.Index))
		}
		out[d.Index] = d.Embedding
	}
	return out, nil
}

func validateEmbeddingDimensions(provider shared.Provider, n int) error {
	if n < 0 {
		return agentErrors.NewInvalidConfigError(string(provider), "embedding_dimensions", "must not be negative")
	}
	return nil
}

// Provider returns the provider type
func (p *OpenAIProvider) Provider() shared.Provider {
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
