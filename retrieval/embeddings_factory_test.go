package retrieval

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEmbedderProvider_Constants(t *testing.T) {
	// 验证所有提供商常量已定义
	providers := GetSupportedProviders()
	assert.Contains(t, providers, EmbedderProviderOpenAI)
	assert.Contains(t, providers, EmbedderProviderVertexAI)
	assert.Contains(t, providers, EmbedderProviderCohere)
	assert.Contains(t, providers, EmbedderProviderHuggingFace)
	assert.Contains(t, providers, EmbedderProviderSimple)
	assert.Contains(t, providers, EmbedderProviderCustom)
}

func TestIsProviderSupported(t *testing.T) {
	tests := []struct {
		name     string
		provider EmbedderProvider
		expected bool
	}{
		{"OpenAI", EmbedderProviderOpenAI, true},
		{"VertexAI", EmbedderProviderVertexAI, true},
		{"Cohere", EmbedderProviderCohere, true},
		{"HuggingFace", EmbedderProviderHuggingFace, true},
		{"Simple", EmbedderProviderSimple, true},
		{"Custom", EmbedderProviderCustom, true},
		{"Unknown", EmbedderProvider("unknown"), false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := IsProviderSupported(tt.provider)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestDefaultEmbedderOptions(t *testing.T) {
	opts := DefaultEmbedderOptions()

	assert.Equal(t, EmbedderProviderSimple, opts.Provider)
	assert.Equal(t, 768, opts.Dimensions)
	assert.Equal(t, "us-central1", opts.Location)
}

func TestEmbedderOptions_WithFunctions(t *testing.T) {
	opts := DefaultEmbedderOptions()

	// 测试所有 With 函数
	WithProvider(EmbedderProviderOpenAI)(opts)
	assert.Equal(t, EmbedderProviderOpenAI, opts.Provider)

	WithAPIKey("test-api-key")(opts)
	assert.Equal(t, "test-api-key", opts.APIKey)

	WithBaseURL("https://custom.api.com")(opts)
	assert.Equal(t, "https://custom.api.com", opts.BaseURL)

	WithModel("text-embedding-3-small")(opts)
	assert.Equal(t, "text-embedding-3-small", opts.Model)

	WithDimensions(1536)(opts)
	assert.Equal(t, 1536, opts.Dimensions)

	WithProjectID("my-project")(opts)
	assert.Equal(t, "my-project", opts.ProjectID)

	WithLocation("europe-west1")(opts)
	assert.Equal(t, "europe-west1", opts.Location)

	WithInputType("search_query")(opts)
	assert.Equal(t, "search_query", opts.InputType)

	WithHFEndpoint("https://custom.hf.endpoint")(opts)
	assert.Equal(t, "https://custom.hf.endpoint", opts.HFEndpoint)
}

func TestNewEmbedder_SimpleProvider(t *testing.T) {
	ctx := context.Background()

	embedder, err := NewEmbedder(ctx,
		WithProvider(EmbedderProviderSimple),
		WithDimensions(512),
	)

	require.NoError(t, err)
	require.NotNil(t, embedder)
	assert.Equal(t, 512, embedder.Dimensions())
}

func TestNewEmbedder_UnsupportedProvider(t *testing.T) {
	ctx := context.Background()

	embedder, err := NewEmbedder(ctx,
		WithProvider(EmbedderProvider("unsupported")),
	)

	assert.Error(t, err)
	assert.Nil(t, embedder)
	assert.Contains(t, err.Error(), "unsupported embedder provider")
}

func TestNewEmbedder_OpenAI_NoAPIKey(t *testing.T) {
	ctx := context.Background()

	// 确保环境变量未设置
	t.Setenv("OPENAI_API_KEY", "")

	embedder, err := NewEmbedder(ctx,
		WithProvider(EmbedderProviderOpenAI),
	)

	assert.Error(t, err)
	assert.Nil(t, embedder)
	assert.Contains(t, err.Error(), "API key is required")
}

func TestNewEmbedder_OpenAI_WithAPIKey(t *testing.T) {
	ctx := context.Background()

	embedder, err := NewEmbedder(ctx,
		WithProvider(EmbedderProviderOpenAI),
		WithAPIKey("test-api-key"),
		WithModel("text-embedding-3-small"),
	)

	require.NoError(t, err)
	require.NotNil(t, embedder)
}

func TestNewEmbedder_Cohere_NoAPIKey(t *testing.T) {
	ctx := context.Background()

	// 确保环境变量未设置
	t.Setenv("COHERE_API_KEY", "")

	embedder, err := NewEmbedder(ctx,
		WithProvider(EmbedderProviderCohere),
	)

	assert.Error(t, err)
	assert.Nil(t, embedder)
	assert.Contains(t, err.Error(), "API key is required")
}

func TestNewEmbedder_HuggingFace_NoAPIKey(t *testing.T) {
	ctx := context.Background()

	// 确保环境变量未设置
	t.Setenv("HUGGINGFACE_API_KEY", "")
	t.Setenv("HF_API_KEY", "")

	embedder, err := NewEmbedder(ctx,
		WithProvider(EmbedderProviderHuggingFace),
	)

	assert.Error(t, err)
	assert.Nil(t, embedder)
	assert.Contains(t, err.Error(), "API key is required")
}

func TestMustNewEmbedder_Success(t *testing.T) {
	ctx := context.Background()

	// 不应该 panic
	embedder := MustNewEmbedder(ctx,
		WithProvider(EmbedderProviderSimple),
		WithDimensions(256),
	)

	require.NotNil(t, embedder)
	assert.Equal(t, 256, embedder.Dimensions())
}

func TestMustNewEmbedder_Panic(t *testing.T) {
	ctx := context.Background()

	// 确保环境变量未设置
	t.Setenv("OPENAI_API_KEY", "")

	assert.Panics(t, func() {
		MustNewEmbedder(ctx, WithProvider(EmbedderProviderOpenAI))
	})
}

func TestCohereEmbedder_Creation(t *testing.T) {
	config := CohereEmbedderConfig{
		APIKey: "test-api-key",
	}

	embedder, err := NewCohereEmbedder(config)
	require.NoError(t, err)
	require.NotNil(t, embedder)

	// 验证默认值
	assert.Equal(t, "embed-english-v3.0", embedder.model)
	assert.Equal(t, "search_document", embedder.inputType)
	assert.Equal(t, 1024, embedder.Dimensions())
}

func TestCohereEmbedder_Dimensions(t *testing.T) {
	tests := []struct {
		model      string
		dimensions int
	}{
		{"embed-english-v3.0", 1024},
		{"embed-multilingual-v3.0", 1024},
		{"embed-english-light-v3.0", 384},
		{"embed-multilingual-light-v3.0", 384},
		{"unknown-model", 1024},
	}

	for _, tt := range tests {
		t.Run(tt.model, func(t *testing.T) {
			config := CohereEmbedderConfig{
				APIKey: "test-api-key",
				Model:  tt.model,
			}
			embedder, err := NewCohereEmbedder(config)
			require.NoError(t, err)
			assert.Equal(t, tt.dimensions, embedder.Dimensions())
		})
	}
}

func TestCohereEmbedder_Embed_EmptyTexts(t *testing.T) {
	config := CohereEmbedderConfig{
		APIKey: "test-api-key",
	}
	embedder, _ := NewCohereEmbedder(config)

	result, err := embedder.Embed(context.Background(), []string{})
	require.NoError(t, err)
	assert.Empty(t, result)
}

func TestCohereEmbedder_Embed_MockServer(t *testing.T) {
	// 创建模拟服务器
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "POST", r.Method)
		assert.Equal(t, "application/json", r.Header.Get("Content-Type"))
		assert.Contains(t, r.Header.Get("Authorization"), "Bearer")

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{
			"id": "test-id",
			"texts": ["hello", "world"],
			"embeddings": [[0.1, 0.2, 0.3], [0.4, 0.5, 0.6]]
		}`))
	}))
	defer server.Close()

	config := CohereEmbedderConfig{
		APIKey:  "test-api-key",
		BaseURL: server.URL,
	}
	embedder, _ := NewCohereEmbedder(config)

	result, err := embedder.Embed(context.Background(), []string{"hello", "world"})
	require.NoError(t, err)
	assert.Len(t, result, 2)
	assert.Len(t, result[0], 3)
}

func TestCohereEmbedder_EmbedQuery_MockServer(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{
			"id": "test-id",
			"texts": ["query"],
			"embeddings": [[0.1, 0.2, 0.3]]
		}`))
	}))
	defer server.Close()

	config := CohereEmbedderConfig{
		APIKey:  "test-api-key",
		BaseURL: server.URL,
	}
	embedder, _ := NewCohereEmbedder(config)

	result, err := embedder.EmbedQuery(context.Background(), "query")
	require.NoError(t, err)
	assert.Len(t, result, 3)
}

func TestCohereEmbedder_Embed_ErrorResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"message": "invalid request"}`))
	}))
	defer server.Close()

	config := CohereEmbedderConfig{
		APIKey:  "test-api-key",
		BaseURL: server.URL,
	}
	embedder, _ := NewCohereEmbedder(config)

	result, err := embedder.Embed(context.Background(), []string{"hello"})
	assert.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "invalid request")
}

func TestHuggingFaceEmbedder_Creation(t *testing.T) {
	config := HuggingFaceEmbedderConfig{
		APIKey: "test-api-key",
	}

	embedder, err := NewHuggingFaceEmbedder(config)
	require.NoError(t, err)
	require.NotNil(t, embedder)

	// 验证默认值
	assert.Equal(t, "sentence-transformers/all-MiniLM-L6-v2", embedder.model)
	assert.Equal(t, 384, embedder.Dimensions())
}

func TestHuggingFaceEmbedder_Dimensions(t *testing.T) {
	tests := []struct {
		model      string
		dimensions int
	}{
		{"sentence-transformers/all-MiniLM-L6-v2", 384},
		{"sentence-transformers/all-mpnet-base-v2", 768},
		{"sentence-transformers/paraphrase-multilingual-MiniLM-L12-v2", 384},
		{"BAAI/bge-small-en-v1.5", 384},
		{"BAAI/bge-base-en-v1.5", 768},
		{"BAAI/bge-large-en-v1.5", 1024},
		{"unknown-model", 768},
	}

	for _, tt := range tests {
		t.Run(tt.model, func(t *testing.T) {
			config := HuggingFaceEmbedderConfig{
				APIKey: "test-api-key",
				Model:  tt.model,
			}
			embedder, err := NewHuggingFaceEmbedder(config)
			require.NoError(t, err)
			assert.Equal(t, tt.dimensions, embedder.Dimensions())
		})
	}
}

func TestHuggingFaceEmbedder_Embed_EmptyTexts(t *testing.T) {
	config := HuggingFaceEmbedderConfig{
		APIKey: "test-api-key",
	}
	embedder, _ := NewHuggingFaceEmbedder(config)

	result, err := embedder.Embed(context.Background(), []string{})
	require.NoError(t, err)
	assert.Empty(t, result)
}

func TestHuggingFaceEmbedder_Embed_MockServer(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "POST", r.Method)
		assert.Equal(t, "application/json", r.Header.Get("Content-Type"))
		assert.Contains(t, r.Header.Get("Authorization"), "Bearer")

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		// HuggingFace 返回 [][]float64
		_, _ = w.Write([]byte(`[[0.1, 0.2, 0.3], [0.4, 0.5, 0.6]]`))
	}))
	defer server.Close()

	config := HuggingFaceEmbedderConfig{
		APIKey:   "test-api-key",
		Endpoint: server.URL,
	}
	embedder, _ := NewHuggingFaceEmbedder(config)

	result, err := embedder.Embed(context.Background(), []string{"hello", "world"})
	require.NoError(t, err)
	assert.Len(t, result, 2)
	assert.Len(t, result[0], 3)
}

func TestHuggingFaceEmbedder_Embed_NestedResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		// 某些模型返回 [][][]float64（每个 token 的嵌入）
		_, _ = w.Write([]byte(`[[[0.1, 0.2, 0.3], [0.4, 0.5, 0.6]], [[0.7, 0.8, 0.9], [1.0, 1.1, 1.2]]]`))
	}))
	defer server.Close()

	config := HuggingFaceEmbedderConfig{
		APIKey:   "test-api-key",
		Endpoint: server.URL,
	}
	embedder, _ := NewHuggingFaceEmbedder(config)

	result, err := embedder.Embed(context.Background(), []string{"hello", "world"})
	require.NoError(t, err)
	assert.Len(t, result, 2)
	// 取第一个 token 的嵌入
	assert.Equal(t, float32(0.1), result[0][0])
	assert.Equal(t, float32(0.7), result[1][0])
}

func TestHuggingFaceEmbedder_EmbedQuery_MockServer(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`[[0.1, 0.2, 0.3]]`))
	}))
	defer server.Close()

	config := HuggingFaceEmbedderConfig{
		APIKey:   "test-api-key",
		Endpoint: server.URL,
	}
	embedder, _ := NewHuggingFaceEmbedder(config)

	result, err := embedder.EmbedQuery(context.Background(), "query")
	require.NoError(t, err)
	assert.Len(t, result, 3)
}

func TestHuggingFaceEmbedder_Embed_ErrorResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte(`{"error": "Model is loading", "estimated_time": 30.0}`))
	}))
	defer server.Close()

	config := HuggingFaceEmbedderConfig{
		APIKey:   "test-api-key",
		Endpoint: server.URL,
	}
	embedder, _ := NewHuggingFaceEmbedder(config)

	result, err := embedder.Embed(context.Background(), []string{"hello"})
	assert.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "Model is loading")
}

func TestNewEmbedder_ChainedOptions(t *testing.T) {
	ctx := context.Background()

	// 测试链式调用
	embedder, err := NewEmbedder(ctx,
		WithProvider(EmbedderProviderSimple),
		WithDimensions(256),
		WithModel("test-model"), // Simple 不使用 model，但不应报错
	)

	require.NoError(t, err)
	require.NotNil(t, embedder)
	assert.Equal(t, 256, embedder.Dimensions())
}

func TestNewEmbedder_VertexAI_NoProjectID(t *testing.T) {
	ctx := context.Background()

	// 确保环境变量未设置
	t.Setenv("GOOGLE_CLOUD_PROJECT", "")
	t.Setenv("GCLOUD_PROJECT", "")

	embedder, err := NewEmbedder(ctx,
		WithProvider(EmbedderProviderVertexAI),
	)

	assert.Error(t, err)
	assert.Nil(t, embedder)
	assert.Contains(t, err.Error(), "project ID is required")
}

// ==================== 自定义提供商测试 ====================

func TestRegisterEmbedderProvider(t *testing.T) {
	ctx := context.Background()
	providerName := EmbedderProvider("test-custom-provider")

	// 清理：确保测试后注销
	defer UnregisterEmbedderProvider(providerName)

	// 注册自定义提供商
	RegisterEmbedderProvider(providerName, func(ctx context.Context, opts *EmbedderOptions) (Embedder, error) {
		return NewSimpleEmbedder(opts.Dimensions), nil
	})

	// 使用自定义提供商创建嵌入器
	embedder, err := NewEmbedder(ctx,
		WithProvider(providerName),
		WithDimensions(512),
	)

	require.NoError(t, err)
	require.NotNil(t, embedder)
	assert.Equal(t, 512, embedder.Dimensions())
}

func TestUnregisterEmbedderProvider(t *testing.T) {
	ctx := context.Background()
	providerName := EmbedderProvider("test-unregister-provider")

	// 注册自定义提供商
	RegisterEmbedderProvider(providerName, func(ctx context.Context, opts *EmbedderOptions) (Embedder, error) {
		return NewSimpleEmbedder(256), nil
	})

	// 验证可以使用
	embedder, err := NewEmbedder(ctx, WithProvider(providerName))
	require.NoError(t, err)
	require.NotNil(t, embedder)

	// 注销
	UnregisterEmbedderProvider(providerName)

	// 验证无法再使用
	embedder, err = NewEmbedder(ctx, WithProvider(providerName))
	assert.Error(t, err)
	assert.Nil(t, embedder)
	assert.Contains(t, err.Error(), "unsupported embedder provider")
}

func TestWithCustomEmbedder(t *testing.T) {
	ctx := context.Background()

	// 创建自定义嵌入器
	customEmbedder := NewSimpleEmbedder(1024)

	// 使用 WithCustomEmbedder 注入
	embedder, err := NewEmbedder(ctx,
		WithCustomEmbedder(customEmbedder),
	)

	require.NoError(t, err)
	require.NotNil(t, embedder)
	assert.Equal(t, 1024, embedder.Dimensions())
	assert.Same(t, customEmbedder, embedder) // 应该是同一个实例
}

func TestNewEmbedder_CustomProvider_NotProvided(t *testing.T) {
	ctx := context.Background()

	// 使用 Custom 提供商但不提供嵌入器
	embedder, err := NewEmbedder(ctx,
		WithProvider(EmbedderProviderCustom),
	)

	assert.Error(t, err)
	assert.Nil(t, embedder)
	assert.Contains(t, err.Error(), "custom embedder not provided")
}

func TestGetRegisteredProviders(t *testing.T) {
	// 清理所有已注册的提供商
	for _, p := range GetRegisteredProviders() {
		UnregisterEmbedderProvider(p)
	}

	// 验证初始为空
	providers := GetRegisteredProviders()
	assert.Empty(t, providers)

	// 注册两个提供商
	RegisterEmbedderProvider("provider-a", func(ctx context.Context, opts *EmbedderOptions) (Embedder, error) {
		return NewSimpleEmbedder(256), nil
	})
	RegisterEmbedderProvider("provider-b", func(ctx context.Context, opts *EmbedderOptions) (Embedder, error) {
		return NewSimpleEmbedder(512), nil
	})

	// 清理
	defer UnregisterEmbedderProvider("provider-a")
	defer UnregisterEmbedderProvider("provider-b")

	// 验证已注册
	providers = GetRegisteredProviders()
	assert.Len(t, providers, 2)
	assert.Contains(t, providers, EmbedderProvider("provider-a"))
	assert.Contains(t, providers, EmbedderProvider("provider-b"))
}

func TestIsProviderSupported_Custom(t *testing.T) {
	providerName := EmbedderProvider("test-is-supported")

	// 注册前不支持
	assert.False(t, IsProviderSupported(providerName))

	// 注册后支持
	RegisterEmbedderProvider(providerName, func(ctx context.Context, opts *EmbedderOptions) (Embedder, error) {
		return NewSimpleEmbedder(256), nil
	})
	defer UnregisterEmbedderProvider(providerName)

	assert.True(t, IsProviderSupported(providerName))

	// 内置的 Custom 类型也应该支持
	assert.True(t, IsProviderSupported(EmbedderProviderCustom))
}

func TestCustomProvider_WithOptions(t *testing.T) {
	ctx := context.Background()
	providerName := EmbedderProvider("test-options-provider")

	// 清理
	defer UnregisterEmbedderProvider(providerName)

	// 注册一个使用 Options 的自定义提供商
	RegisterEmbedderProvider(providerName, func(ctx context.Context, opts *EmbedderOptions) (Embedder, error) {
		// 验证可以访问所有选项
		if opts.APIKey == "" {
			return nil, assert.AnError
		}
		if opts.Model == "" {
			return nil, assert.AnError
		}
		return NewSimpleEmbedder(opts.Dimensions), nil
	})

	// 测试传递选项
	embedder, err := NewEmbedder(ctx,
		WithProvider(providerName),
		WithAPIKey("test-key"),
		WithModel("test-model"),
		WithDimensions(768),
	)

	require.NoError(t, err)
	require.NotNil(t, embedder)
	assert.Equal(t, 768, embedder.Dimensions())
}

func TestCustomProvider_ConcurrentAccess(t *testing.T) {
	ctx := context.Background()
	providerName := EmbedderProvider("test-concurrent-provider")

	// 清理
	defer UnregisterEmbedderProvider(providerName)

	// 注册
	RegisterEmbedderProvider(providerName, func(ctx context.Context, opts *EmbedderOptions) (Embedder, error) {
		return NewSimpleEmbedder(256), nil
	})

	// 并发访问
	done := make(chan bool, 10)
	for i := 0; i < 10; i++ {
		go func() {
			embedder, err := NewEmbedder(ctx, WithProvider(providerName))
			assert.NoError(t, err)
			assert.NotNil(t, embedder)
			done <- true
		}()
	}

	// 等待所有 goroutine 完成
	for i := 0; i < 10; i++ {
		<-done
	}
}
