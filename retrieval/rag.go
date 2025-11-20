package retrieval

import (
	"context"
	"fmt"
	"strings"

	agentErrors "github.com/kart-io/goagent/errors"
	"github.com/kart-io/goagent/llm"
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
		return nil, agentErrors.New(agentErrors.CodeInvalidConfig, "vector store is required").
			WithComponent("rag_engine").
			WithOperation("create")
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
		return nil, agentErrors.Wrap(err, agentErrors.CodeRetrievalSearch, "failed to search documents").
			WithComponent("rag_engine").
			WithOperation("retrieve").
			WithContext("query", query).
			WithContext("topK", r.topK)
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

	return agentErrors.New(agentErrors.CodeNotImplemented, "clear operation not supported for this vector store type").
		WithComponent("rag_engine").
		WithOperation("clear")
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
	llmClient llm.Client
}

// NewRAGChain 创建 RAG 链
//
// 参数:
//   - retriever: RAG 检索器
//   - llmClient: LLM 客户端（可选，如果为 nil 则仅返回检索结果）
//
// 返回:
//   - *RAGChain: RAG 链实例
func NewRAGChain(retriever *RAGRetriever, llmClient llm.Client) *RAGChain {
	return &RAGChain{
		retriever: retriever,
		llmClient: llmClient,
	}
}

// Run 执行 RAG 链
//
// 执行完整的 RAG 流程：检索相关文档 -> 格式化上下文 -> 生成答案
//
// 参数:
//   - ctx: 上下文
//   - query: 用户查询
//
// 返回:
//   - string: 生成的答案或格式化的上下文
//   - error: 错误信息
func (c *RAGChain) Run(ctx context.Context, query string) (string, error) {
	// 1. 检索相关文档
	docs, err := c.retriever.Retrieve(ctx, query)
	if err != nil {
		return "", agentErrors.Wrap(err, agentErrors.CodeRetrievalSearch, "retrieval failed").
			WithComponent("rag_chain").
			WithOperation("run").
			WithContext("query", query)
	}

	if len(docs) == 0 {
		return "No relevant documents found.", nil
	}

	// 2. 格式化上下文
	contextPrompt, err := c.retriever.RetrieveWithContext(ctx, query)
	if err != nil {
		return "", agentErrors.Wrap(err, agentErrors.CodeInternal, "failed to format context").
			WithComponent("rag_chain").
			WithOperation("run").
			WithContext("query", query)
	}

	// 3. 如果没有 LLM 客户端，返回格式化的上下文
	if c.llmClient == nil {
		return contextPrompt, nil
	}

	// 4. 调用 LLM 生成回答
	response, err := c.llmClient.Complete(ctx, &llm.CompletionRequest{
		Messages: []llm.Message{
			llm.UserMessage(contextPrompt),
		},
	})

	if err != nil {
		return "", agentErrors.Wrap(err, agentErrors.CodeLLMRequest, "LLM generation failed").
			WithComponent("rag_chain").
			WithOperation("run").
			WithContext("query", query)
	}

	return response.Content, nil
}

// RAGMultiQueryRetriever RAG 多查询检索器
//
// 生成多个相关查询并合并结果，提高召回率
type RAGMultiQueryRetriever struct {
	BaseRetriever *RAGRetriever
	NumQueries    int
	LLMClient     llm.Client // LLM 客户端用于生成查询变体
}

// NewRAGMultiQueryRetriever 创建 RAG 多查询检索器
//
// 参数:
//   - baseRetriever: 基础 RAG 检索器
//   - numQueries: 生成的查询数量
//   - llmClient: LLM 客户端（可选，如果为 nil 则只使用原始查询）
//
// 返回:
//   - *RAGMultiQueryRetriever: 多查询检索器实例
func NewRAGMultiQueryRetriever(baseRetriever *RAGRetriever, numQueries int, llmClient llm.Client) *RAGMultiQueryRetriever {
	if numQueries <= 0 {
		numQueries = 3
	}

	return &RAGMultiQueryRetriever{
		BaseRetriever: baseRetriever,
		NumQueries:    numQueries,
		LLMClient:     llmClient,
	}
}

// Retrieve 检索相关文档
//
// 使用 LLM 生成查询变体，然后对每个查询进行检索并合并结果
//
// 参数:
//   - ctx: 上下文
//   - query: 原始查询
//
// 返回:
//   - []*Document: 合并后的文档列表
//   - error: 错误信息
func (m *RAGMultiQueryRetriever) Retrieve(ctx context.Context, query string) ([]*Document, error) {
	// 生成相关查询
	queries, err := m.generateQueries(ctx, query)
	if err != nil {
		// 如果生成查询失败，回退到使用原始查询
		queries = []string{query}
	}

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

// generateQueries 使用 LLM 生成查询变体
func (m *RAGMultiQueryRetriever) generateQueries(ctx context.Context, query string) ([]string, error) {
	// 如果没有 LLM 客户端，返回原始查询
	if m.LLMClient == nil {
		return []string{query}, nil
	}

	// 构建提示词
	prompt := fmt.Sprintf(`You are an AI assistant helping to generate alternative search queries.
Given the original query, generate %d different variations that could help retrieve relevant information.
The variations should rephrase the query in different ways while maintaining the same intent.

Original query: %s

Please provide only the alternative queries, one per line, without numbering or explanations.`, m.NumQueries, query)

	// 调用 LLM 生成查询
	response, err := m.LLMClient.Complete(ctx, &llm.CompletionRequest{
		Messages: []llm.Message{
			llm.UserMessage(prompt),
		},
		Temperature: 0.7, // 适度的创造性
	})

	if err != nil {
		return nil, agentErrors.Wrap(err, agentErrors.CodeLLMRequest, "failed to generate query variations").
			WithComponent("rag_multi_query_retriever").
			WithOperation("generate_queries").
			WithContext("query", query)
	}

	// 解析生成的查询
	queries := []string{query} // 始终包含原始查询
	lines := strings.Split(strings.TrimSpace(response.Content), "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		// 移除数字前缀（如 "1. ", "- ", 等）
		line = strings.TrimLeft(line, "0123456789.-) ")
		if line != "" && line != query {
			queries = append(queries, line)
			if len(queries) >= m.NumQueries+1 { // +1 因为包含原始查询
				break
			}
		}
	}

	return queries, nil
}
