# Retrieval - 检索器系统

完整的 RAG（检索增强生成）检索器实现，借鉴 LangChain 的设计理念。

## 概述

检索器系统提供统一的文档检索接口，支持多种检索策略和重排序算法，用于构建高质量的 RAG 应用。

## 核心概念

### Document 文档

文档是检索系统的基本单元：

```go
type Document struct {
    PageContent string                 // 文档内容
    Metadata    map[string]interface{} // 元数据
    ID          string                 // 唯一标识符
    Score       float64                // 检索得分
}
```

### Retriever 检索器接口

统一的检索器接口，继承自 `Runnable[string, []*Document]`：

```go
type Retriever interface {
    core.Runnable[string, []*Document]
    GetRelevantDocuments(ctx context.Context, query string) ([]*Document, error)
}
```

支持的操作：

- `Invoke` - 单个查询检索
- `Stream` - 流式检索
- `Batch` - 批量检索
- `Pipe` - 管道连接
- `WithCallbacks` - 添加回调

## 检索器类型

### 1. VectorStoreRetriever 向量存储检索器

使用向量相似度进行检索：

```go
// 创建向量存储
vectorStore := retrieval.NewMockVectorStore()
vectorStore.AddDocuments(ctx, docs)

// 创建检索器
config := retrieval.DefaultRetrieverConfig()
config.TopK = 5

retriever := retrieval.NewVectorStoreRetriever(vectorStore, config)

// 执行检索
results, err := retriever.GetRelevantDocuments(ctx, "your query")
```

支持的搜索类型：

- `SearchTypeSimilarity` - 相似度搜索
- `SearchTypeSimilarityScoreThreshold` - 带阈值的相似度搜索
- `SearchTypeMMR` - 最大边际相关性搜索

### 2. KeywordRetriever 关键词检索器

使用 BM25 或 TF-IDF 算法进行检索：

```go
config := retrieval.DefaultRetrieverConfig()
config.TopK = 5

retriever := retrieval.NewKeywordRetriever(docs, config)
retriever.WithAlgorithm(retrieval.AlgorithmBM25)

results, err := retriever.GetRelevantDocuments(ctx, "your query")
```

支持的算法：

- `AlgorithmBM25` - BM25 算法（推荐）
- `AlgorithmTFIDF` - TF-IDF 算法

### 3. HybridRetriever 混合检索器

结合向量检索和关键词检索：

```go
// 创建向量检索器
vectorRetriever := retrieval.NewVectorStoreRetriever(vectorStore, config)

// 创建关键词检索器
keywordRetriever := retrieval.NewKeywordRetriever(docs, config)

// 创建混合检索器
hybrid := retrieval.NewHybridRetriever(
    vectorRetriever,
    keywordRetriever,
    0.6, // 向量权重
    0.4, // 关键词权重
    config,
)

// 设置融合策略
hybrid.WithFusionStrategy(retrieval.FusionStrategyRRF)

results, err := hybrid.GetRelevantDocuments(ctx, "your query")
```

支持的融合策略：

- `FusionStrategyWeightedSum` - 加权求和
- `FusionStrategyRRF` - 倒数排名融合（推荐）
- `FusionStrategyCombSum` - 组合求和

### 4. MultiQueryRetriever 多查询检索器

使用 LLM 生成多个查询变体：

```go
retriever := retrieval.NewMultiQueryRetriever(
    baseRetriever,
    llmClient,
    3, // 生成 3 个查询变体
    config,
)

results, err := retriever.GetRelevantDocuments(ctx, "your query")
```

### 5. EnsembleRetriever 集成检索器

组合多个检索器：

```go
ensemble := retrieval.NewEnsembleRetriever(
    []retrieval.Retriever{retriever1, retriever2, retriever3},
    []float64{0.5, 0.3, 0.2}, // 权重
    config,
)

results, err := ensemble.GetRelevantDocuments(ctx, "your query")
```

## 重排序系统

### Reranker 重排序器接口

```go
type Reranker interface {
    Rerank(ctx context.Context, query string, docs []*Document) ([]*Document, error)
}
```

### 重排序器类型

#### 1. CrossEncoderReranker 交叉编码器

使用交叉编码器模型重排序：

```go
reranker := retrieval.NewCrossEncoderReranker("model-name", 10)
reranked, err := reranker.Rerank(ctx, query, docs)
```

#### 2. MMRReranker 最大边际相关性

平衡相关性和多样性：

```go
reranker := retrieval.NewMMRReranker(
    0.7, // lambda: 0=多样性, 1=相关性
    10,  // top-N
)
reranked, err := reranker.Rerank(ctx, query, docs)
```

#### 3. LLMReranker LLM 重排序器

使用 LLM 进行重排序：

```go
reranker := retrieval.NewLLMReranker(10)
reranked, err := reranker.Rerank(ctx, query, docs)
```

#### 4. CohereReranker Cohere API

使用 Cohere Rerank API：

```go
reranker := retrieval.NewCohereReranker(apiKey, "model-name", 10)
reranked, err := reranker.Rerank(ctx, query, docs)
```

### RerankingRetriever 带重排序的检索器

在基础检索器上应用重排序：

```go
// 创建基础检索器
baseRetriever := retrieval.NewKeywordRetriever(docs, config)

// 创建重排序器
reranker := retrieval.NewCrossEncoderReranker("model", 10)

// 创建带重排序的检索器
config.TopK = 5
rerankingRetriever := retrieval.NewRerankingRetriever(
    baseRetriever,
    reranker,
    20, // 初始获取 20 个候选
    config,
)

results, err := rerankingRetriever.GetRelevantDocuments(ctx, query)
```

## 高级功能

### 1. 回调系统

为检索过程添加监控和日志：

```go
// 自定义回调
type MyCallback struct {
    core.BaseCallback
}

func (c *MyCallback) OnStart(ctx context.Context, input interface{}) error {
    fmt.Printf("Starting retrieval: %v\n", input)
    return nil
}

func (c *MyCallback) OnEnd(ctx context.Context, output interface{}) error {
    docs := output.([]*retrieval.Document)
    fmt.Printf("Retrieved %d documents\n", len(docs))
    return nil
}

// 使用回调
retriever := retriever.WithCallbacks(&MyCallback{})
```

### 2. 管道操作

将检索器与其他操作连接：

```go
// 创建处理函数
filterFunc := core.NewRunnableFunc(func(ctx context.Context, docs []*retrieval.Document) ([]*retrieval.Document, error) {
    // 过滤逻辑
    filtered := make([]*retrieval.Document, 0)
    for _, doc := range docs {
        if doc.Score > 0.5 {
            filtered = append(filtered, doc)
        }
    }
    return filtered, nil
})

// 构建管道
pipeline := retriever.Pipe(filterFunc)

// 执行
results, err := pipeline.Invoke(ctx, query)
```

### 3. 批量检索

并发处理多个查询：

```go
queries := []string{
    "query 1",
    "query 2",
    "query 3",
}

results, err := retriever.Batch(ctx, queries)
// results: [][]*Document
```

### 4. 流式检索

流式返回结果：

```go
stream, err := retriever.Stream(ctx, query)
if err != nil {
    return err
}

for chunk := range stream {
    if chunk.Error != nil {
        // 处理错误
        continue
    }
    // 处理结果
    docs := chunk.Data
}
```

## 配置选项

### RetrieverConfig

```go
config := retrieval.DefaultRetrieverConfig()

// 设置返回的最大文档数
config.TopK = 10

// 设置最小分数阈值
config.MinScore = 0.3

// 设置检索器名称
config.Name = "my_retriever"
```

### 文档集合操作

```go
collection := retrieval.DocumentCollection(docs)

// 按分数排序
collection.SortByScore()

// 获取前 N 个
top5 := collection.Top(5)

// 过滤
filtered := collection.Filter(func(d *retrieval.Document) bool {
    return d.Score > 0.5
})

// 去重
unique := collection.Deduplicate()

// 映射
mapped := collection.Map(func(d *retrieval.Document) *retrieval.Document {
    d.SetMetadata("processed", true)
    return d
})
```

## 完整示例

### 基础检索

```go
package main

import (
    "context"
    "fmt"

    "github.com/kart-io/goagent/retrieval"
)

func main() {
    // 创建文档
    docs := []*retrieval.Document{
        retrieval.NewDocument("Kubernetes is a container orchestration platform", nil),
        retrieval.NewDocument("Docker is a containerization technology", nil),
        retrieval.NewDocument("Python is a programming language", nil),
    }

    // 创建检索器
    config := retrieval.DefaultRetrieverConfig()
    config.TopK = 2

    retriever := retrieval.NewKeywordRetriever(docs, config)

    // 执行检索
    ctx := context.Background()
    results, err := retriever.GetRelevantDocuments(ctx, "container technology")
    if err != nil {
        panic(err)
    }

    // 显示结果
    for i, doc := range results {
        fmt.Printf("%d. Score: %.4f\n", i+1, doc.Score)
        fmt.Printf("   Content: %s\n", doc.PageContent)
    }
}
```

### 混合检索 + 重排序

```go
// 1. 创建向量检索器
vectorStore := retrieval.NewMockVectorStore()
vectorStore.AddDocuments(ctx, docs)
vectorRetriever := retrieval.NewVectorStoreRetriever(vectorStore, config)

// 2. 创建关键词检索器
keywordRetriever := retrieval.NewKeywordRetriever(docs, config)

// 3. 创建混合检索器
hybrid := retrieval.NewHybridRetriever(
    vectorRetriever,
    keywordRetriever,
    0.6, 0.4,
    config,
)
hybrid.WithFusionStrategy(retrieval.FusionStrategyRRF)

// 4. 创建重排序器
reranker := retrieval.NewMMRReranker(0.7, 10)

// 5. 创建最终检索器
finalRetriever := retrieval.NewRerankingRetriever(
    hybrid,
    reranker,
    20,
    config,
)

// 6. 执行检索
results, err := finalRetriever.GetRelevantDocuments(ctx, query)
```

## 最佳实践

### 1. 选择合适的检索策略

- **向量检索**: 适合语义相似度匹配
- **关键词检索**: 适合精确匹配和术语查询
- **混合检索**: 在大多数场景下提供最佳效果
- **多查询检索**: 适合复杂查询

### 2. 使用重排序提高质量

```go
// 推荐的流程
baseRetriever -> 获取候选 (top-k * 2-4)
    -> Reranker -> 精排序 (top-k)
```

### 3. 配置合理的参数

```go
config := retrieval.DefaultRetrieverConfig()
config.TopK = 5      // 通常 3-10 个结果足够
config.MinScore = 0.3 // 过滤低质量结果
```

### 4. 使用回调监控性能

```go
metricsCallback := &MetricsCallback{}
retriever := retriever.WithCallbacks(metricsCallback)
```

### 5. 批量处理优化性能

```go
// 批量检索比循环调用更高效
results, err := retriever.Batch(ctx, queries)
```

## 性能优化

### 1. 索引优化

- 使用倒排索引加速关键词检索
- 预计算文档向量
- 缓存常见查询结果

### 2. 并发控制

```go
config := core.RunnableConfig{
    MaxConcurrency: 10, // 限制并发数
}
retriever := retriever.WithConfig(config)
```

### 3. 结果缓存

```go
// 实现缓存层
type CachedRetriever struct {
    *retrieval.BaseRetriever
    cache map[string][]*retrieval.Document
}
```

## 扩展指南

### 自定义检索器

```go
type CustomRetriever struct {
    *retrieval.BaseRetriever
    // 自定义字段
}

func (c *CustomRetriever) GetRelevantDocuments(ctx context.Context, query string) ([]*retrieval.Document, error) {
    // 实现自定义检索逻辑
    return docs, nil
}
```

### 自定义重排序器

```go
type CustomReranker struct {
    *retrieval.BaseReranker
}

func (c *CustomReranker) Rerank(ctx context.Context, query string, docs []*retrieval.Document) ([]*retrieval.Document, error) {
    // 实现自定义重排序逻辑
    return docs, nil
}
```

## 测试

运行测试：

```bash
cd retrieval
go test -v
```

运行示例：

```bash
go run examples/basic_retrieval.go
go run examples/advanced_retrieval.go
```

## 参考

- [LangChain Retrievers](https://python.langchain.com/docs/modules/data_connection/retrievers/)
- [BM25 Algorithm](https://en.wikipedia.org/wiki/Okapi_BM25)
- [Maximum Marginal Relevance](https://www.cs.cmu.edu/~jgc/publication/The_Use_MMR_Diversity_Based_LTMIR_1998.pdf)
- [Reciprocal Rank Fusion](https://plg.uwaterloo.ca/~gvcormac/cormacksigir09-rrf.pdf)

## 相关模块

- `core` - 核心 Runnable 接口和回调系统
- `llm` - LLM 客户端（用于 MultiQueryRetriever）
- `chain` - 链式调用（用于构建 RAG 链）
