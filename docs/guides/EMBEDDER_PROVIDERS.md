# Embedder 提供商指南

## 概述

GoAgent 提供统一的文本嵌入（Embedding）接口，支持多种嵌入服务提供商。本指南详细说明各提供商的配置、使用方法以及如何实现自定义提供商。

## 支持的提供商

| 提供商 | 常量 | 特点 |
|-------|------|------|
| OpenAI | `EmbedderProviderOpenAI` | text-embedding-3 系列 |
| Vertex AI | `EmbedderProviderVertexAI` | Google Cloud 嵌入服务 |
| Cohere | `EmbedderProviderCohere` | 多语言支持 |
| HuggingFace | `EmbedderProviderHuggingFace` | 开源模型 |
| Simple | `EmbedderProviderSimple` | 测试用简单嵌入器 |
| Custom | `EmbedderProviderCustom` | 自定义嵌入器 |

## 统一接口

所有嵌入器都实现 `Embedder` 接口：

```go
type Embedder interface {
    // Embed 批量嵌入文本
    // 输入多个文本，返回对应的向量数组
    Embed(ctx context.Context, texts []string) ([][]float32, error)

    // EmbedQuery 嵌入单个查询文本
    // 用于查询时的文本嵌入
    EmbedQuery(ctx context.Context, query string) ([]float32, error)

    // Dimensions 返回向量维度
    Dimensions() int
}
```

## 统一工厂函数

使用 `NewEmbedder` 统一创建各种嵌入器：

```go
import "github.com/kart-io/goagent/retrieval"

// 创建嵌入器
embedder, err := retrieval.NewEmbedder(ctx,
    retrieval.WithProvider(retrieval.EmbedderProviderOpenAI),
    retrieval.WithAPIKey("sk-xxx"),
    retrieval.WithModel("text-embedding-3-small"),
)
```

## 提供商配置

### OpenAI

```go
import "github.com/kart-io/goagent/retrieval"

embedder, err := retrieval.NewEmbedder(ctx,
    retrieval.WithProvider(retrieval.EmbedderProviderOpenAI),
    retrieval.WithAPIKey(os.Getenv("OPENAI_API_KEY")),
    retrieval.WithModel("text-embedding-3-small"),
    retrieval.WithDimensions(1536),  // 可选，指定维度
)
```

**支持的模型：**

| 模型 | 维度 | 特点 |
|------|------|------|
| `text-embedding-3-small` | 1536 | 性价比高 |
| `text-embedding-3-large` | 3072 | 高精度 |
| `text-embedding-ada-002` | 1536 | 旧版本 |

**环境变量：** `OPENAI_API_KEY`

### Vertex AI (Google)

```go
import "github.com/kart-io/goagent/retrieval"

embedder, err := retrieval.NewEmbedder(ctx,
    retrieval.WithProvider(retrieval.EmbedderProviderVertexAI),
    retrieval.WithProjectID("my-gcp-project"),
    retrieval.WithLocation("us-central1"),
    retrieval.WithModel("text-embedding-005"),
    retrieval.WithDimensions(768),
)
```

**支持的模型：**

| 模型 | 维度 | 特点 |
|------|------|------|
| `text-embedding-005` | 768 | 最新版本 |
| `text-embedding-004` | 768 | 稳定版本 |
| `textembedding-gecko` | 768 | 旧版本 |

**环境变量：** `GOOGLE_CLOUD_PROJECT` 或 `GCLOUD_PROJECT`

**前置条件：**
- 已配置 Google Cloud 认证（`gcloud auth application-default login`）
- 已启用 Vertex AI API

### Cohere

```go
import "github.com/kart-io/goagent/retrieval"

embedder, err := retrieval.NewEmbedder(ctx,
    retrieval.WithProvider(retrieval.EmbedderProviderCohere),
    retrieval.WithAPIKey(os.Getenv("COHERE_API_KEY")),
    retrieval.WithModel("embed-english-v3.0"),
    retrieval.WithInputType("search_document"),  // 文档嵌入
)
```

**支持的模型：**

| 模型 | 维度 | 特点 |
|------|------|------|
| `embed-english-v3.0` | 1024 | 英文优化 |
| `embed-multilingual-v3.0` | 1024 | 多语言支持 |
| `embed-english-light-v3.0` | 384 | 轻量版 |
| `embed-multilingual-light-v3.0` | 384 | 轻量多语言 |

**输入类型 (InputType)：**

| 类型 | 说明 |
|------|------|
| `search_document` | 文档嵌入（索引时使用） |
| `search_query` | 查询嵌入（检索时使用） |
| `classification` | 分类任务 |
| `clustering` | 聚类任务 |

**环境变量：** `COHERE_API_KEY`

### HuggingFace

```go
import "github.com/kart-io/goagent/retrieval"

embedder, err := retrieval.NewEmbedder(ctx,
    retrieval.WithProvider(retrieval.EmbedderProviderHuggingFace),
    retrieval.WithAPIKey(os.Getenv("HUGGINGFACE_API_KEY")),
    retrieval.WithModel("sentence-transformers/all-MiniLM-L6-v2"),
)

// 使用自定义推理端点
embedder, err := retrieval.NewEmbedder(ctx,
    retrieval.WithProvider(retrieval.EmbedderProviderHuggingFace),
    retrieval.WithAPIKey(os.Getenv("HUGGINGFACE_API_KEY")),
    retrieval.WithHFEndpoint("https://your-endpoint.endpoints.huggingface.cloud"),
)
```

**常用模型：**

| 模型 | 维度 | 特点 |
|------|------|------|
| `sentence-transformers/all-MiniLM-L6-v2` | 384 | 轻量通用 |
| `sentence-transformers/all-mpnet-base-v2` | 768 | 高质量 |
| `BAAI/bge-small-en-v1.5` | 384 | BGE 小型 |
| `BAAI/bge-base-en-v1.5` | 768 | BGE 基础 |
| `BAAI/bge-large-en-v1.5` | 1024 | BGE 大型 |

**环境变量：** `HUGGINGFACE_API_KEY` 或 `HF_API_KEY`

### Simple（测试用）

```go
import "github.com/kart-io/goagent/retrieval"

// 创建简单嵌入器（用于测试）
embedder, err := retrieval.NewEmbedder(ctx,
    retrieval.WithProvider(retrieval.EmbedderProviderSimple),
    retrieval.WithDimensions(768),
)
```

Simple 嵌入器使用哈希算法生成确定性向量，仅用于测试和开发。

## 自定义提供商

GoAgent 支持两种方式添加自定义嵌入器：

### 方式一：直接注入嵌入器实例

```go
// 创建自定义嵌入器实例
myEmbedder := NewMyCustomEmbedder(config)

// 直接注入
embedder, err := retrieval.NewEmbedder(ctx,
    retrieval.WithCustomEmbedder(myEmbedder),
)
```

### 方式二：注册自定义提供商工厂

```go
// 注册自定义提供商
retrieval.RegisterEmbedderProvider("my-provider", func(ctx context.Context, opts *retrieval.EmbedderOptions) (retrieval.Embedder, error) {
    return NewMyCustomEmbedder(opts.APIKey, opts.Model, opts.Dimensions), nil
})

// 使用自定义提供商
embedder, err := retrieval.NewEmbedder(ctx,
    retrieval.WithProvider("my-provider"),
    retrieval.WithAPIKey("xxx"),
    retrieval.WithModel("my-model"),
    retrieval.WithDimensions(768),
)

// 注销（如需要）
retrieval.UnregisterEmbedderProvider("my-provider")
```

### 实现自定义 Embedder

自定义嵌入器需要实现 `Embedder` 接口：

```go
package myembedder

import (
    "context"
)

// MyEmbedder 自定义嵌入器
type MyEmbedder struct {
    apiKey     string
    model      string
    dimensions int
    // 其他配置...
}

// NewMyEmbedder 创建自定义嵌入器
func NewMyEmbedder(apiKey, model string, dimensions int) *MyEmbedder {
    return &MyEmbedder{
        apiKey:     apiKey,
        model:      model,
        dimensions: dimensions,
    }
}

// Embed 批量嵌入文本
func (e *MyEmbedder) Embed(ctx context.Context, texts []string) ([][]float32, error) {
    if len(texts) == 0 {
        return [][]float32{}, nil
    }

    results := make([][]float32, len(texts))
    for i, text := range texts {
        // 调用你的嵌入服务 API
        vector, err := e.callEmbeddingAPI(ctx, text)
        if err != nil {
            return nil, err
        }
        results[i] = vector
    }
    return results, nil
}

// EmbedQuery 嵌入单个查询
func (e *MyEmbedder) EmbedQuery(ctx context.Context, query string) ([]float32, error) {
    vectors, err := e.Embed(ctx, []string{query})
    if err != nil {
        return nil, err
    }
    if len(vectors) == 0 {
        return nil, fmt.Errorf("no embedding returned")
    }
    return vectors[0], nil
}

// Dimensions 返回向量维度
func (e *MyEmbedder) Dimensions() int {
    return e.dimensions
}

// callEmbeddingAPI 调用嵌入 API（示例）
func (e *MyEmbedder) callEmbeddingAPI(ctx context.Context, text string) ([]float32, error) {
    // 实现你的 API 调用逻辑
    // ...
    return vector, nil
}
```

### 注册自定义提供商的完整示例

```go
package main

import (
    "context"
    "github.com/kart-io/goagent/retrieval"
)

func init() {
    // 在应用启动时注册自定义提供商
    retrieval.RegisterEmbedderProvider("my-service", createMyEmbedder)
}

func createMyEmbedder(ctx context.Context, opts *retrieval.EmbedderOptions) (retrieval.Embedder, error) {
    // 从 Options 获取配置
    apiKey := opts.APIKey
    if apiKey == "" {
        apiKey = os.Getenv("MY_SERVICE_API_KEY")
    }

    model := opts.Model
    if model == "" {
        model = "default-model"
    }

    dimensions := opts.Dimensions
    if dimensions <= 0 {
        dimensions = 768
    }

    return NewMyEmbedder(apiKey, model, dimensions), nil
}

func main() {
    ctx := context.Background()

    // 使用自定义提供商
    embedder, err := retrieval.NewEmbedder(ctx,
        retrieval.WithProvider("my-service"),
        retrieval.WithAPIKey("xxx"),
    )
    if err != nil {
        log.Fatal(err)
    }

    // 使用嵌入器
    vectors, err := embedder.Embed(ctx, []string{"Hello", "World"})
    // ...
}
```

## 辅助函数

### 检查提供商支持

```go
// 检查提供商是否支持（包括内置和已注册的自定义提供商）
if retrieval.IsProviderSupported("my-provider") {
    // 使用该提供商
}

// 获取所有内置提供商
providers := retrieval.GetSupportedProviders()

// 获取所有已注册的自定义提供商
customProviders := retrieval.GetRegisteredProviders()
```

### MustNewEmbedder

```go
// 创建嵌入器，失败时 panic（适用于初始化）
embedder := retrieval.MustNewEmbedder(ctx,
    retrieval.WithProvider(retrieval.EmbedderProviderSimple),
    retrieval.WithDimensions(768),
)
```

## 使用示例

### 基本使用

```go
ctx := context.Background()

// 创建嵌入器
embedder, err := retrieval.NewEmbedder(ctx,
    retrieval.WithProvider(retrieval.EmbedderProviderOpenAI),
    retrieval.WithAPIKey(os.Getenv("OPENAI_API_KEY")),
    retrieval.WithModel("text-embedding-3-small"),
)
if err != nil {
    log.Fatal(err)
}

// 批量嵌入文档
docs := []string{
    "GoAgent 是一个 AI Agent 框架",
    "支持多种 LLM 提供商",
    "提供统一的接口抽象",
}
vectors, err := embedder.Embed(ctx, docs)
if err != nil {
    log.Fatal(err)
}

fmt.Printf("嵌入了 %d 个文档，维度: %d\n", len(vectors), embedder.Dimensions())

// 嵌入查询
queryVector, err := embedder.EmbedQuery(ctx, "什么是 GoAgent？")
if err != nil {
    log.Fatal(err)
}
```

### 与向量存储配合使用

```go
// 创建嵌入器
embedder, _ := retrieval.NewEmbedder(ctx,
    retrieval.WithProvider(retrieval.EmbedderProviderOpenAI),
    retrieval.WithAPIKey(os.Getenv("OPENAI_API_KEY")),
)

// 创建向量存储
store := retrieval.NewMemoryVectorStore(retrieval.MemoryVectorStoreConfig{
    Embedder:       embedder,
    DistanceMetric: retrieval.DistanceMetricCosine,
})

// 添加文档
docs := []*interfaces.Document{
    {PageContent: "文档内容1"},
    {PageContent: "文档内容2"},
}
err := store.AddDocuments(ctx, docs)

// 搜索相似文档
results, err := store.SimilaritySearch(ctx, "查询内容", 5)
```

### 切换提供商

```go
func createEmbedder(provider string) (retrieval.Embedder, error) {
    ctx := context.Background()

    switch provider {
    case "openai":
        return retrieval.NewEmbedder(ctx,
            retrieval.WithProvider(retrieval.EmbedderProviderOpenAI),
            retrieval.WithAPIKey(os.Getenv("OPENAI_API_KEY")),
        )
    case "cohere":
        return retrieval.NewEmbedder(ctx,
            retrieval.WithProvider(retrieval.EmbedderProviderCohere),
            retrieval.WithAPIKey(os.Getenv("COHERE_API_KEY")),
        )
    case "vertexai":
        return retrieval.NewEmbedder(ctx,
            retrieval.WithProvider(retrieval.EmbedderProviderVertexAI),
            retrieval.WithProjectID(os.Getenv("GOOGLE_CLOUD_PROJECT")),
        )
    default:
        return retrieval.NewEmbedder(ctx,
            retrieval.WithProvider(retrieval.EmbedderProviderSimple),
        )
    }
}
```

## 最佳实践

### 1. 使用环境变量管理密钥

```bash
export OPENAI_API_KEY="sk-..."
export COHERE_API_KEY="..."
export HUGGINGFACE_API_KEY="hf_..."
export GOOGLE_CLOUD_PROJECT="my-project"
```

### 2. 选择合适的模型

根据场景选择合适的嵌入模型：

| 场景 | 推荐模型 |
|------|---------|
| 通用语义搜索 | OpenAI text-embedding-3-small |
| 高精度需求 | OpenAI text-embedding-3-large |
| 多语言支持 | Cohere embed-multilingual-v3.0 |
| 成本敏感 | HuggingFace all-MiniLM-L6-v2 |
| 本地部署 | HuggingFace + 自托管端点 |

### 3. 批量处理

```go
// 批量嵌入更高效
texts := []string{"text1", "text2", "text3", ...}
vectors, err := embedder.Embed(ctx, texts)

// 避免逐个嵌入
for _, text := range texts {
    vector, err := embedder.EmbedQuery(ctx, text) // 效率低
}
```

### 4. 错误处理

```go
embedder, err := retrieval.NewEmbedder(ctx,
    retrieval.WithProvider(retrieval.EmbedderProviderOpenAI),
    retrieval.WithAPIKey(apiKey),
)
if err != nil {
    // 检查错误类型
    if strings.Contains(err.Error(), "API key is required") {
        log.Fatal("请设置 OPENAI_API_KEY 环境变量")
    }
    log.Fatalf("创建嵌入器失败: %v", err)
}
```

### 5. 维度一致性

确保在同一个向量存储中使用相同维度的嵌入器：

```go
// 文档嵌入和查询嵌入应使用相同的模型和维度
embedder, _ := retrieval.NewEmbedder(ctx,
    retrieval.WithProvider(retrieval.EmbedderProviderOpenAI),
    retrieval.WithModel("text-embedding-3-small"),
    retrieval.WithDimensions(1536),
)

// 索引和查询使用同一个嵌入器
store := retrieval.NewMemoryVectorStore(retrieval.MemoryVectorStoreConfig{
    Embedder: embedder,
})
```

## 相关文档

- [快速入门](QUICKSTART.md)
- [LLM 提供商指南](LLM_PROVIDERS.md)
- [架构概述](../architecture/ARCHITECTURE.md)
