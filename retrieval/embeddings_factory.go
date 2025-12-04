package retrieval

import (
	"context"
	"os"
	"sync"

	agentErrors "github.com/kart-io/goagent/errors"
)

// EmbedderProvider 嵌入服务提供商类型
type EmbedderProvider string

// 支持的嵌入服务提供商
const (
	// EmbedderProviderOpenAI OpenAI 嵌入服务
	EmbedderProviderOpenAI EmbedderProvider = "openai"
	// EmbedderProviderVertexAI Google Vertex AI 嵌入服务
	EmbedderProviderVertexAI EmbedderProvider = "vertexai"
	// EmbedderProviderCohere Cohere 嵌入服务
	EmbedderProviderCohere EmbedderProvider = "cohere"
	// EmbedderProviderHuggingFace Hugging Face 嵌入服务
	EmbedderProviderHuggingFace EmbedderProvider = "huggingface"
	// EmbedderProviderSimple 简单嵌入器（用于测试）
	EmbedderProviderSimple EmbedderProvider = "simple"
	// EmbedderProviderCustom 自定义嵌入器
	EmbedderProviderCustom EmbedderProvider = "custom"
)

// EmbedderFactory 自定义嵌入器工厂函数类型
//
// 用于注册自定义的嵌入器提供商
type EmbedderFactory func(ctx context.Context, options *EmbedderOptions) (Embedder, error)

// 自定义提供商注册表
var (
	customProviders   = make(map[EmbedderProvider]EmbedderFactory)
	customProvidersMu sync.RWMutex
)

// RegisterEmbedderProvider 注册自定义嵌入器提供商
//
// 允许用户注册自定义的嵌入器工厂函数，以支持内置提供商之外的服务
//
// 使用示例：
//
//	// 注册自定义提供商
//	retrieval.RegisterEmbedderProvider("my-provider", func(ctx context.Context, opts *retrieval.EmbedderOptions) (retrieval.Embedder, error) {
//	    return NewMyCustomEmbedder(opts.APIKey, opts.Model)
//	})
//
//	// 使用自定义提供商
//	embedder, err := retrieval.NewEmbedder(ctx,
//	    retrieval.WithProvider("my-provider"),
//	    retrieval.WithAPIKey("xxx"),
//	)
func RegisterEmbedderProvider(provider EmbedderProvider, factory EmbedderFactory) {
	customProvidersMu.Lock()
	defer customProvidersMu.Unlock()
	customProviders[provider] = factory
}

// UnregisterEmbedderProvider 注销自定义嵌入器提供商
func UnregisterEmbedderProvider(provider EmbedderProvider) {
	customProvidersMu.Lock()
	defer customProvidersMu.Unlock()
	delete(customProviders, provider)
}

// getCustomProvider 获取自定义提供商工厂函数
func getCustomProvider(provider EmbedderProvider) (EmbedderFactory, bool) {
	customProvidersMu.RLock()
	defer customProvidersMu.RUnlock()
	factory, ok := customProviders[provider]
	return factory, ok
}

// EmbedderOptions 嵌入器配置选项
type EmbedderOptions struct {
	// 通用配置
	Provider   EmbedderProvider
	APIKey     string
	BaseURL    string
	Model      string
	Dimensions int

	// Vertex AI 特定配置
	ProjectID string
	Location  string

	// Cohere 特定配置
	InputType string // search_document, search_query, classification, clustering

	// Hugging Face 特定配置
	HFEndpoint string // 自定义推理端点

	// 自定义嵌入器（直接注入）
	CustomEmbedder Embedder
}

// EmbedderOption 嵌入器配置函数类型
type EmbedderOption func(*EmbedderOptions)

// DefaultEmbedderOptions 返回默认配置
func DefaultEmbedderOptions() *EmbedderOptions {
	return &EmbedderOptions{
		Provider:   EmbedderProviderSimple,
		Dimensions: 768,
		Location:   "us-central1",
	}
}

// WithProvider 设置服务提供商
func WithProvider(provider EmbedderProvider) EmbedderOption {
	return func(o *EmbedderOptions) {
		o.Provider = provider
	}
}

// WithAPIKey 设置 API Key
func WithAPIKey(apiKey string) EmbedderOption {
	return func(o *EmbedderOptions) {
		o.APIKey = apiKey
	}
}

// WithBaseURL 设置 API 基础 URL
func WithBaseURL(baseURL string) EmbedderOption {
	return func(o *EmbedderOptions) {
		o.BaseURL = baseURL
	}
}

// WithModel 设置模型名称
func WithModel(model string) EmbedderOption {
	return func(o *EmbedderOptions) {
		o.Model = model
	}
}

// WithDimensions 设置向量维度
func WithDimensions(dimensions int) EmbedderOption {
	return func(o *EmbedderOptions) {
		o.Dimensions = dimensions
	}
}

// WithProjectID 设置 Google Cloud 项目 ID（Vertex AI）
func WithProjectID(projectID string) EmbedderOption {
	return func(o *EmbedderOptions) {
		o.ProjectID = projectID
	}
}

// WithLocation 设置区域（Vertex AI）
func WithLocation(location string) EmbedderOption {
	return func(o *EmbedderOptions) {
		o.Location = location
	}
}

// WithInputType 设置输入类型（Cohere）
func WithInputType(inputType string) EmbedderOption {
	return func(o *EmbedderOptions) {
		o.InputType = inputType
	}
}

// WithHFEndpoint 设置 Hugging Face 推理端点
func WithHFEndpoint(endpoint string) EmbedderOption {
	return func(o *EmbedderOptions) {
		o.HFEndpoint = endpoint
	}
}

// WithCustomEmbedder 直接注入自定义嵌入器
//
// 当使用此选项时，Provider 会自动设置为 EmbedderProviderCustom
//
// 使用示例：
//
//	myEmbedder := NewMyCustomEmbedder()
//	embedder, err := retrieval.NewEmbedder(ctx,
//	    retrieval.WithCustomEmbedder(myEmbedder),
//	)
func WithCustomEmbedder(embedder Embedder) EmbedderOption {
	return func(o *EmbedderOptions) {
		o.Provider = EmbedderProviderCustom
		o.CustomEmbedder = embedder
	}
}

// NewEmbedder 创建嵌入器的工厂函数
//
// 使用示例：
//
//	// 创建 OpenAI 嵌入器
//	embedder, err := retrieval.NewEmbedder(ctx,
//	    retrieval.WithProvider(retrieval.EmbedderProviderOpenAI),
//	    retrieval.WithAPIKey("sk-xxx"),
//	    retrieval.WithModel("text-embedding-3-small"),
//	)
//
//	// 创建 Vertex AI 嵌入器
//	embedder, err := retrieval.NewEmbedder(ctx,
//	    retrieval.WithProvider(retrieval.EmbedderProviderVertexAI),
//	    retrieval.WithProjectID("my-project"),
//	    retrieval.WithLocation("us-central1"),
//	)
//
//	// 创建简单嵌入器（用于测试）
//	embedder, err := retrieval.NewEmbedder(ctx,
//	    retrieval.WithProvider(retrieval.EmbedderProviderSimple),
//	    retrieval.WithDimensions(768),
//	)
//
//	// 使用自定义嵌入器
//	embedder, err := retrieval.NewEmbedder(ctx,
//	    retrieval.WithCustomEmbedder(myEmbedder),
//	)
//
//	// 使用注册的自定义提供商
//	retrieval.RegisterEmbedderProvider("my-provider", myFactory)
//	embedder, err := retrieval.NewEmbedder(ctx,
//	    retrieval.WithProvider("my-provider"),
//	)
func NewEmbedder(ctx context.Context, opts ...EmbedderOption) (Embedder, error) {
	options := DefaultEmbedderOptions()
	for _, opt := range opts {
		opt(options)
	}

	switch options.Provider {
	case EmbedderProviderOpenAI:
		return newOpenAIEmbedderFromOptions(options)
	case EmbedderProviderVertexAI:
		return newVertexAIEmbedderFromOptions(ctx, options)
	case EmbedderProviderCohere:
		return newCohereEmbedderFromOptions(options)
	case EmbedderProviderHuggingFace:
		return newHuggingFaceEmbedderFromOptions(options)
	case EmbedderProviderSimple:
		return NewSimpleEmbedder(options.Dimensions), nil
	case EmbedderProviderCustom:
		// 直接注入的自定义嵌入器
		if options.CustomEmbedder != nil {
			return options.CustomEmbedder, nil
		}
		return nil, agentErrors.New(agentErrors.CodeInvalidConfig, "custom embedder not provided").
			WithComponent("embedder_factory").
			WithOperation("create")
	default:
		// 检查是否为注册的自定义提供商
		if factory, ok := getCustomProvider(options.Provider); ok {
			return factory(ctx, options)
		}
		return nil, agentErrors.New(agentErrors.CodeInvalidConfig, "unsupported embedder provider").
			WithComponent("embedder_factory").
			WithOperation("create").
			WithContext("provider", string(options.Provider))
	}
}

// newOpenAIEmbedderFromOptions 从 Options 创建 OpenAI 嵌入器
func newOpenAIEmbedderFromOptions(options *EmbedderOptions) (*OpenAIEmbedder, error) {
	apiKey := options.APIKey
	if apiKey == "" {
		apiKey = os.Getenv("OPENAI_API_KEY")
	}

	config := OpenAIEmbedderConfig{
		APIKey:     apiKey,
		Model:      options.Model,
		BaseURL:    options.BaseURL,
		Dimensions: options.Dimensions,
	}

	return NewOpenAIEmbedder(config)
}

// newVertexAIEmbedderFromOptions 从 Options 创建 Vertex AI 嵌入器
func newVertexAIEmbedderFromOptions(ctx context.Context, options *EmbedderOptions) (*VertexAIEmbedder, error) {
	projectID := options.ProjectID
	if projectID == "" {
		projectID = os.Getenv("GOOGLE_CLOUD_PROJECT")
		if projectID == "" {
			projectID = os.Getenv("GCLOUD_PROJECT")
		}
	}

	config := VertexAIEmbedderConfig{
		ProjectID:  projectID,
		Location:   options.Location,
		ModelID:    options.Model,
		Dimensions: options.Dimensions,
	}

	return NewVertexAIEmbedder(ctx, config)
}

// newCohereEmbedderFromOptions 从 Options 创建 Cohere 嵌入器
func newCohereEmbedderFromOptions(options *EmbedderOptions) (Embedder, error) {
	apiKey := options.APIKey
	if apiKey == "" {
		apiKey = os.Getenv("COHERE_API_KEY")
	}

	config := CohereEmbedderConfig{
		APIKey:     apiKey,
		Model:      options.Model,
		InputType:  options.InputType,
		Dimensions: options.Dimensions,
	}

	return NewCohereEmbedder(config)
}

// newHuggingFaceEmbedderFromOptions 从 Options 创建 Hugging Face 嵌入器
func newHuggingFaceEmbedderFromOptions(options *EmbedderOptions) (Embedder, error) {
	apiKey := options.APIKey
	if apiKey == "" {
		apiKey = os.Getenv("HUGGINGFACE_API_KEY")
		if apiKey == "" {
			apiKey = os.Getenv("HF_API_KEY")
		}
	}

	config := HuggingFaceEmbedderConfig{
		APIKey:     apiKey,
		Model:      options.Model,
		Endpoint:   options.HFEndpoint,
		Dimensions: options.Dimensions,
	}

	return NewHuggingFaceEmbedder(config)
}

// MustNewEmbedder 创建嵌入器，失败时 panic
//
// 用于初始化时确定不会失败的场景，如测试代码
func MustNewEmbedder(ctx context.Context, opts ...EmbedderOption) Embedder {
	embedder, err := NewEmbedder(ctx, opts...)
	if err != nil {
		panic(err)
	}
	return embedder
}

// GetSupportedProviders 返回所有内置支持的提供商列表
//
// 不包含通过 RegisterEmbedderProvider 注册的自定义提供商
func GetSupportedProviders() []EmbedderProvider {
	return []EmbedderProvider{
		EmbedderProviderOpenAI,
		EmbedderProviderVertexAI,
		EmbedderProviderCohere,
		EmbedderProviderHuggingFace,
		EmbedderProviderSimple,
		EmbedderProviderCustom,
	}
}

// GetRegisteredProviders 返回所有已注册的自定义提供商列表
func GetRegisteredProviders() []EmbedderProvider {
	customProvidersMu.RLock()
	defer customProvidersMu.RUnlock()

	providers := make([]EmbedderProvider, 0, len(customProviders))
	for p := range customProviders {
		providers = append(providers, p)
	}
	return providers
}

// IsProviderSupported 检查提供商是否支持（内置或已注册）
func IsProviderSupported(provider EmbedderProvider) bool {
	// 检查内置提供商
	for _, p := range GetSupportedProviders() {
		if p == provider {
			return true
		}
	}
	// 检查已注册的自定义提供商
	_, ok := getCustomProvider(provider)
	return ok
}
