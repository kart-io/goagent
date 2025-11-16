package retrieval

import (
	"context"
	"fmt"
	"strings"
)

// RAGRetriever RAG (Retrieval-Augmented Generation) 检索器
//
// 结合向量检索和生成模型，提供增强的文档检索能力
type RAGRetriever struct {
	// VectorStore 向量存储
	vectorStore VectorStore

	// Embedder 嵌入器（可选，如果 VectorStore 不支持）
	embedder Embedder

	// TopK 返回的最大文档数
	topK int

	// ScoreThreshold 分数阈值，低于此分数的文档将被过滤
	scoreThreshold float32

	// IncludeMetadata 是否包含元数据
	includeMetadata bool

	// MaxContentLength 最大内容长度（超过会截断）
	maxContentLength int
}

// RAGRetrieverConfig RAG 检索器配置
type RAGRetrieverConfig struct {
	VectorStore      VectorStore
	Embedder         Embedder
	TopK             int
	ScoreThreshold   float32
	IncludeMetadata  bool
	MaxContentLength int
}

// NewRAGRetriever 创建 RAG 检索器
func NewRAGRetriever(config RAGRetrieverConfig) (*RAGRetriever, error) {
	if config.VectorStore == nil {
		return nil, fmt.Errorf("vector store is required")
	}

	if config.TopK <= 0 {
		config.TopK = 4
	}

	if config.ScoreThreshold < 0 {
		config.ScoreThreshold = 0
	}

	if config.MaxContentLength <= 0 {
		config.MaxContentLength = 1000
	}

	return &RAGRetriever{
		vectorStore:      config.VectorStore,
		embedder:         config.Embedder,
		topK:             config.TopK,
		scoreThreshold:   config.ScoreThreshold,
		includeMetadata:  config.IncludeMetadata,
		maxContentLength: config.MaxContentLength,
	}, nil
}

// Retrieve 检索相关文档
func (r *RAGRetriever) Retrieve(ctx context.Context, query string) ([]*Document, error) {
	// 从向量存储检索文档
	docs, err := r.vectorStore.SimilaritySearch(ctx, query, r.topK)
	if err != nil {
		return nil, fmt.Errorf("failed to search documents: %w", err)
	}

	// 过滤低分文档
	if r.scoreThreshold > 0 {
		filtered := make([]*Document, 0)
		for _, doc := range docs {
			if float32(doc.Score) >= r.scoreThreshold {
				filtered = append(filtered, doc)
			}
		}
		docs = filtered
	}

	// 截断内容
	if r.maxContentLength > 0 {
		for _, doc := range docs {
			if len(doc.PageContent) > r.maxContentLength {
				doc.PageContent = doc.PageContent[:r.maxContentLength] + "..."
			}
		}
	}

	return docs, nil
}

// RetrieveAndFormat 检索并格式化为 Prompt
//
// 使用指定的模板格式化检索到的文档
func (r *RAGRetriever) RetrieveAndFormat(ctx context.Context, query string, template string) (string, error) {
	docs, err := r.Retrieve(ctx, query)
	if err != nil {
		return "", err
	}

	if len(docs) == 0 {
		return "", nil
	}

	// 如果没有提供模板，使用默认格式
	if template == "" {
		template = r.defaultTemplate()
	}

	// 格式化文档
	formattedDocs := make([]string, len(docs))
	for i, doc := range docs {
		formattedDocs[i] = r.formatDocument(doc, i+1)
	}

	// 替换模板中的占位符
	result := strings.ReplaceAll(template, "{query}", query)
	result = strings.ReplaceAll(result, "{documents}", strings.Join(formattedDocs, "\n\n"))
	result = strings.ReplaceAll(result, "{num_docs}", fmt.Sprintf("%d", len(docs)))

	return result, nil
}

// RetrieveWithContext 检索并构建上下文
//
// 返回格式化的上下文字符串，可直接用于 LLM 提示
func (r *RAGRetriever) RetrieveWithContext(ctx context.Context, query string) (string, error) {
	template := `Based on the following context, please answer the question.

Context:
{documents}

Question: {query}

Answer:`

	return r.RetrieveAndFormat(ctx, query, template)
}

// AddDocuments 添加文档到向量存储
func (r *RAGRetriever) AddDocuments(ctx context.Context, docs []*Document) error {
	return r.vectorStore.AddDocuments(ctx, docs)
}

// Clear 清空向量存储（如果支持）
func (r *RAGRetriever) Clear() error {
	// 尝试类型断言到 MemoryVectorStore
	if memStore, ok := r.vectorStore.(*MemoryVectorStore); ok {
		memStore.Clear()
		return nil
	}

	return fmt.Errorf("clear operation not supported for this vector store type")
}

// SetTopK 设置 TopK
func (r *RAGRetriever) SetTopK(topK int) {
	if topK > 0 {
		r.topK = topK
	}
}

// SetScoreThreshold 设置分数阈值
func (r *RAGRetriever) SetScoreThreshold(threshold float32) {
	if threshold >= 0 {
		r.scoreThreshold = threshold
	}
}

// defaultTemplate 返回默认模板
func (r *RAGRetriever) defaultTemplate() string {
	return `Query: {query}

Retrieved Documents ({num_docs}):
{documents}`
}

// formatDocument 格式化单个文档
func (r *RAGRetriever) formatDocument(doc *Document, index int) string {
	var sb strings.Builder

	sb.WriteString(fmt.Sprintf("Document %d:\n", index))
	sb.WriteString(doc.PageContent)

	if r.includeMetadata && len(doc.Metadata) > 0 {
		sb.WriteString("\nMetadata:\n")
		for key, value := range doc.Metadata {
			sb.WriteString(fmt.Sprintf("  %s: %v\n", key, value))
		}
	}

	if doc.Score > 0 {
		sb.WriteString(fmt.Sprintf("Score: %.4f\n", doc.Score))
	}

	return sb.String()
}

// RAGChain RAG 链，组合检索和生成
type RAGChain struct {
	retriever *RAGRetriever
	// llmClient llm.Client // 可以添加 LLM 客户端进行生成
}

// NewRAGChain 创建 RAG 链
func NewRAGChain(retriever *RAGRetriever) *RAGChain {
	return &RAGChain{
		retriever: retriever,
	}
}

// Run 执行 RAG 链
func (c *RAGChain) Run(ctx context.Context, query string) (string, error) {
	// 1. 检索相关文档
	docs, err := c.retriever.Retrieve(ctx, query)
	if err != nil {
		return "", fmt.Errorf("retrieval failed: %w", err)
	}

	if len(docs) == 0 {
		return "No relevant documents found.", nil
	}

	// 2. 格式化上下文
	context, err := c.retriever.RetrieveWithContext(ctx, query)
	if err != nil {
		return "", fmt.Errorf("failed to format context: %w", err)
	}

	// 3. TODO: 调用 LLM 生成回答
	// response, err := c.llmClient.Complete(ctx, &llm.CompletionRequest{
	//     Messages: []llm.Message{
	//         llm.UserMessage(context),
	//     },
	// })
	// if err != nil {
	//     return "", fmt.Errorf("generation failed: %w", err)
	// }
	// return response.Content, nil

	// 临时返回格式化的上下文
	return context, nil
}

// RAGMultiQueryRetriever RAG 多查询检索器
//
// 生成多个相关查询并合并结果，提高召回率
type RAGMultiQueryRetriever struct {
	BaseRetriever *RAGRetriever
	NumQueries    int
}

// NewRAGMultiQueryRetriever 创建 RAG 多查询检索器
func NewRAGMultiQueryRetriever(baseRetriever *RAGRetriever, numQueries int) *RAGMultiQueryRetriever {
	if numQueries <= 0 {
		numQueries = 3
	}

	return &RAGMultiQueryRetriever{
		BaseRetriever: baseRetriever,
		NumQueries:    numQueries,
	}
}

// Retrieve 检索相关文档
func (m *RAGMultiQueryRetriever) Retrieve(ctx context.Context, query string) ([]*Document, error) {
	// TODO: 使用 LLM 生成相关查询
	// 当前简化版本：直接使用原查询
	queries := []string{query}

	// 去重集合
	docMap := make(map[string]*Document)

	// 对每个查询进行检索
	for _, q := range queries {
		docs, err := m.BaseRetriever.Retrieve(ctx, q)
		if err != nil {
			continue // 跳过失败的查询
		}

		for _, doc := range docs {
			if existingDoc, exists := docMap[doc.ID]; exists {
				// 如果文档已存在，取较高的分数
				if doc.Score > existingDoc.Score {
					docMap[doc.ID] = doc
				}
			} else {
				docMap[doc.ID] = doc
			}
		}
	}

	// 转换为切片并排序
	results := make([]*Document, 0, len(docMap))
	for _, doc := range docMap {
		results = append(results, doc)
	}

	// 按分数排序
	collection := DocumentCollection(results)
	collection.SortByScore()

	// 限制返回数量
	if m.BaseRetriever.topK > 0 && len(collection) > m.BaseRetriever.topK {
		collection = collection[:m.BaseRetriever.topK]
	}

	return collection, nil
}
