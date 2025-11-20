package retrieval

import (
	"context"
	"math"
	"sort"

	coheregov2 "github.com/cohere-ai/cohere-go/v2"
	cohereclient "github.com/cohere-ai/cohere-go/v2/client"

	agentErrors "github.com/kart-io/goagent/errors"
)

// Reranker 重排序器接口
//
// 对检索到的文档进行重新排序，提高结果质量
type Reranker interface {
	// Rerank 重新排序文档
	Rerank(ctx context.Context, query string, docs []*Document) ([]*Document, error)
}

// BaseReranker 基础重排序器
type BaseReranker struct {
	Name string
}

// NewBaseReranker 创建基础重排序器
func NewBaseReranker(name string) *BaseReranker {
	return &BaseReranker{
		Name: name,
	}
}

// Rerank 默认实现（不改变顺序）
func (b *BaseReranker) Rerank(ctx context.Context, query string, docs []*Document) ([]*Document, error) {
	return docs, nil
}

// CrossEncoderReranker 交叉编码器重排序器
//
// 使用交叉编码器模型计算查询和文档的相关性分数
type CrossEncoderReranker struct {
	*BaseReranker

	// Model 模型名称
	Model string

	// TopN 返回前 N 个文档
	TopN int
}

// NewCrossEncoderReranker 创建交叉编码器重排序器
func NewCrossEncoderReranker(model string, topN int) *CrossEncoderReranker {
	return &CrossEncoderReranker{
		BaseReranker: NewBaseReranker("cross_encoder"),
		Model:        model,
		TopN:         topN,
	}
}

// Rerank 重新排序文档
func (c *CrossEncoderReranker) Rerank(ctx context.Context, query string, docs []*Document) ([]*Document, error) {
	if len(docs) == 0 {
		return docs, nil
	}

	// 模拟交叉编码器评分
	// 实际应该调用真实的模型 API
	scored := make([]*Document, len(docs))
	for i, doc := range docs {
		docCopy := doc.Clone()
		// 模拟评分：基于内容相似度
		docCopy.Score = c.calculateRelevanceScore(query, doc.PageContent)
		scored[i] = docCopy
	}

	// 排序
	collection := DocumentCollection(scored)
	collection.SortByScore()

	// 返回 top-N
	if c.TopN > 0 && len(collection) > c.TopN {
		collection = collection[:c.TopN]
	}

	return collection, nil
}

// calculateRelevanceScore 计算相关性分数（模拟）
func (c *CrossEncoderReranker) calculateRelevanceScore(query, content string) float64 {
	// 简单的基于关键词匹配的模拟
	queryWords := tokenize(query)
	contentWords := tokenize(content)

	if len(contentWords) == 0 {
		return 0.0
	}

	// 计算词汇重叠率
	matches := 0
	for _, qw := range queryWords {
		for _, cw := range contentWords {
			if qw == cw {
				matches++
				break
			}
		}
	}

	overlap := float64(matches) / float64(len(queryWords))

	// 结合原始分数
	return overlap * 0.7
}

// LLMReranker LLM 重排序器
//
// 使用 LLM 对文档进行相关性判断和排序
type LLMReranker struct {
	*BaseReranker

	// LLMClient LLM 客户端
	// LLMClient llm.LLMClient

	// TopN 返回前 N 个文档
	TopN int

	// Prompt 提示词模板
	Prompt string
}

// NewLLMReranker 创建 LLM 重排序器
func NewLLMReranker(topN int) *LLMReranker {
	return &LLMReranker{
		BaseReranker: NewBaseReranker("llm_reranker"),
		TopN:         topN,
		Prompt:       defaultRerankPrompt,
	}
}

const defaultRerankPrompt = `Given a query and a document, rate the relevance of the document to the query on a scale of 0-10.

Query: {{.Query}}
Document: {{.Document}}

Relevance score (0-10):`

// Rerank 重新排序文档
func (l *LLMReranker) Rerank(ctx context.Context, query string, docs []*Document) ([]*Document, error) {
	if len(docs) == 0 {
		return docs, nil
	}

	// 模拟 LLM 评分
	scored := make([]*Document, len(docs))
	for i, doc := range docs {
		docCopy := doc.Clone()
		// 模拟评分（实际应该调用 LLM）
		docCopy.Score = l.simulateLLMScore(query, doc.PageContent)
		scored[i] = docCopy
	}

	// 排序
	collection := DocumentCollection(scored)
	collection.SortByScore()

	// 返回 top-N
	if l.TopN > 0 && len(collection) > l.TopN {
		collection = collection[:l.TopN]
	}

	return collection, nil
}

// simulateLLMScore 模拟 LLM 评分
func (l *LLMReranker) simulateLLMScore(query, content string) float64 {
	// 简单的模拟逻辑
	queryWords := tokenize(query)
	contentWords := tokenize(content)

	if len(contentWords) == 0 {
		return 0.0
	}

	// 计算匹配度
	matches := 0
	for _, qw := range queryWords {
		for _, cw := range contentWords {
			if qw == cw {
				matches++
				break
			}
		}
	}

	return float64(matches) / float64(len(queryWords))
}

// MMRReranker 最大边际相关性重排序器
//
// 使用 MMR 算法平衡相关性和多样性
type MMRReranker struct {
	*BaseReranker

	// Lambda 相关性和多样性的平衡参数（0-1）
	// 0: 只考虑多样性，1: 只考虑相关性
	Lambda float64

	// TopN 返回前 N 个文档
	TopN int
}

// NewMMRReranker 创建 MMR 重排序器
func NewMMRReranker(lambda float64, topN int) *MMRReranker {
	return &MMRReranker{
		BaseReranker: NewBaseReranker("mmr"),
		Lambda:       lambda,
		TopN:         topN,
	}
}

// Rerank 使用 MMR 算法重新排序
//
// MMR = λ * Sim(D, Q) - (1-λ) * max(Sim(D, Di))
// 其中 Di 是已选择的文档
func (m *MMRReranker) Rerank(ctx context.Context, query string, docs []*Document) ([]*Document, error) {
	if len(docs) == 0 {
		return docs, nil
	}

	topN := m.TopN
	if topN <= 0 || topN > len(docs) {
		topN = len(docs)
	}

	// 初始化
	selected := make([]*Document, 0, topN)
	remaining := make([]*Document, len(docs))
	copy(remaining, docs)

	// 迭代选择
	for len(selected) < topN && len(remaining) > 0 {
		bestIdx := -1
		bestScore := -math.MaxFloat64

		for i, doc := range remaining {
			// 计算 MMR 分数
			relevanceScore := doc.Score
			diversityPenalty := 0.0

			// 计算与已选文档的最大相似度
			if len(selected) > 0 {
				maxSim := 0.0
				for _, selDoc := range selected {
					sim := m.documentSimilarity(doc, selDoc)
					if sim > maxSim {
						maxSim = sim
					}
				}
				diversityPenalty = maxSim
			}

			// MMR 公式
			mmrScore := m.Lambda*relevanceScore - (1-m.Lambda)*diversityPenalty

			if mmrScore > bestScore {
				bestScore = mmrScore
				bestIdx = i
			}
		}

		// 添加最佳文档
		if bestIdx >= 0 {
			selected = append(selected, remaining[bestIdx])
			// 从候选中移除
			remaining = append(remaining[:bestIdx], remaining[bestIdx+1:]...)
		}
	}

	return selected, nil
}

// documentSimilarity 计算文档相似度（简化版本）
func (m *MMRReranker) documentSimilarity(doc1, doc2 *Document) float64 {
	// 简单的基于词汇重叠的相似度
	words1 := tokenize(doc1.PageContent)
	words2 := tokenize(doc2.PageContent)

	if len(words1) == 0 || len(words2) == 0 {
		return 0.0
	}

	// 计算交集
	set1 := make(map[string]bool)
	for _, w := range words1 {
		set1[w] = true
	}

	intersection := 0
	for _, w := range words2 {
		if set1[w] {
			intersection++
		}
	}

	// Jaccard 相似度
	union := len(words1) + len(words2) - intersection
	if union == 0 {
		return 0.0
	}

	return float64(intersection) / float64(union)
}

// CohereReranker Cohere Rerank API 重排序器
type CohereReranker struct {
	*BaseReranker

	// APIKey Cohere API 密钥
	APIKey string

	// Model 模型名称
	Model string

	// TopN 返回前 N 个文档
	TopN int

	// client Cohere 客户端
	client *cohereclient.Client
}

// NewCohereReranker 创建 Cohere 重排序器
//
// 参数:
//   - apiKey: Cohere API 密钥
//   - model: 模型名称（可选，默认为 "rerank-english-v2.0"）
//   - topN: 返回前 N 个文档
//
// 返回:
//   - *CohereReranker: Cohere 重排序器实例
//   - error: 错误信息
func NewCohereReranker(apiKey, model string, topN int) (*CohereReranker, error) {
	if apiKey == "" {
		return nil, agentErrors.New(agentErrors.CodeInvalidConfig, "Cohere API key is required").
			WithComponent("cohere_reranker").
			WithOperation("create")
	}

	if model == "" {
		model = "rerank-english-v2.0"
	}

	client := cohereclient.NewClient(cohereclient.WithToken(apiKey))

	return &CohereReranker{
		BaseReranker: NewBaseReranker("cohere"),
		APIKey:       apiKey,
		Model:        model,
		TopN:         topN,
		client:       client,
	}, nil
}

// Rerank 使用 Cohere API 重新排序
//
// 参数:
//   - ctx: 上下文
//   - query: 查询字符串
//   - docs: 待重排序的文档列表
//
// 返回:
//   - []*Document: 重排序后的文档列表
//   - error: 错误信息
func (c *CohereReranker) Rerank(ctx context.Context, query string, docs []*Document) ([]*Document, error) {
	if len(docs) == 0 {
		return docs, nil
	}

	// 提取文档内容并转换为 RerankRequestDocumentsItem
	documentItems := make([]*coheregov2.RerankRequestDocumentsItem, len(docs))
	for i, doc := range docs {
		documentItems[i] = &coheregov2.RerankRequestDocumentsItem{
			String: doc.PageContent,
		}
	}

	// 调用 Cohere Rerank API
	topN := c.TopN
	if topN <= 0 {
		topN = len(docs)
	}

	response, err := c.client.Rerank(ctx, &coheregov2.RerankRequest{
		Query:     query,
		Documents: documentItems,
		Model:     &c.Model,
		TopN:      &topN,
	})

	if err != nil {
		return nil, agentErrors.Wrap(err, agentErrors.CodeInternal, "Cohere rerank API call failed").
			WithComponent("cohere_reranker").
			WithOperation("rerank").
			WithContext("query", query).
			WithContext("num_docs", len(docs))
	}

	// 转换结果
	if response == nil || response.Results == nil {
		return docs, nil
	}

	rerankedDocs := make([]*Document, 0, len(response.Results))
	for _, result := range response.Results {
		if result.Index < len(docs) {
			doc := docs[result.Index].Clone()
			doc.Score = result.RelevanceScore
			rerankedDocs = append(rerankedDocs, doc)
		}
	}

	return rerankedDocs, nil
}

// RerankingRetriever 带重排序的检索器
//
// 在基础检索器之上应用重排序
type RerankingRetriever struct {
	*BaseRetriever

	// BaseRetriever 基础检索器
	Retriever Retriever

	// Reranker 重排序器
	Reranker Reranker

	// FetchK 初始检索的文档数量
	FetchK int
}

// NewRerankingRetriever 创建带重排序的检索器
func NewRerankingRetriever(
	baseRetriever Retriever,
	reranker Reranker,
	fetchK int,
	config RetrieverConfig,
) *RerankingRetriever {
	retriever := &RerankingRetriever{
		BaseRetriever: NewBaseRetriever(),
		Retriever:     baseRetriever,
		Reranker:      reranker,
		FetchK:        fetchK,
	}

	retriever.TopK = config.TopK
	retriever.MinScore = config.MinScore
	retriever.Name = config.Name

	return retriever
}

// GetRelevantDocuments 检索并重排序文档
func (r *RerankingRetriever) GetRelevantDocuments(ctx context.Context, query string) ([]*Document, error) {
	// 1. 使用基础检索器获取候选文档
	docs, err := r.Retriever.GetRelevantDocuments(ctx, query)
	if err != nil {
		return nil, agentErrors.Wrap(err, agentErrors.CodeRetrievalSearch, "base retrieval failed").
			WithComponent("reranking_retriever").
			WithOperation("get_relevant_documents").
			WithContext("query", query)
	}

	if len(docs) == 0 {
		return docs, nil
	}

	// 2. 应用重排序
	reranked, err := r.Reranker.Rerank(ctx, query, docs)
	if err != nil {
		return nil, agentErrors.Wrap(err, agentErrors.CodeInternal, "reranking failed").
			WithComponent("reranking_retriever").
			WithOperation("get_relevant_documents").
			WithContext("query", query).
			WithContext("num_docs", len(docs))
	}

	// 3. 应用 top-k 限制
	limited := r.LimitTopK(reranked)

	return limited, nil
}

// CompareRankers 比较多个重排序器的性能
type CompareRankers struct {
	Rerankers []Reranker
}

// Compare 对比重排序结果
func (c *CompareRankers) Compare(ctx context.Context, query string, docs []*Document) (map[string][]*Document, error) {
	results := make(map[string][]*Document)

	for _, reranker := range c.Rerankers {
		if br, ok := reranker.(*BaseReranker); ok {
			reranked, err := reranker.Rerank(ctx, query, docs)
			if err != nil {
				return nil, err
			}
			results[br.Name] = reranked
		}
	}

	return results, nil
}

// RankFusion 排名融合
type RankFusion struct {
	// Method 融合方法
	Method string // "rrf", "borda", "comb_sum"

	// K RRF 参数
	K float64
}

// NewRankFusion 创建排名融合
func NewRankFusion(method string) *RankFusion {
	return &RankFusion{
		Method: method,
		K:      60.0,
	}
}

// Fuse 融合多个排名结果
func (rf *RankFusion) Fuse(rankings [][]*Document) []*Document {
	switch rf.Method {
	case "rrf":
		return rf.reciprocalRankFusion(rankings)
	case "borda":
		return rf.bordaCount(rankings)
	case "comb_sum":
		return rf.combSum(rankings)
	default:
		return rankings[0]
	}
}

// reciprocalRankFusion RRF 融合
func (rf *RankFusion) reciprocalRankFusion(rankings [][]*Document) []*Document {
	scores := make(map[string]float64)
	docMap := make(map[string]*Document)

	for _, ranking := range rankings {
		for rank, doc := range ranking {
			score := 1.0 / (rf.K + float64(rank+1))
			scores[doc.ID] += score
			if _, ok := docMap[doc.ID]; !ok {
				docMap[doc.ID] = doc.Clone()
			}
		}
	}

	// 创建结果列表
	result := make([]*Document, 0, len(docMap))
	for id, doc := range docMap {
		doc.Score = scores[id]
		result = append(result, doc)
	}

	// 排序
	sort.Slice(result, func(i, j int) bool {
		return result[i].Score > result[j].Score
	})

	return result
}

// bordaCount Borda 计数融合
func (rf *RankFusion) bordaCount(rankings [][]*Document) []*Document {
	scores := make(map[string]int)
	docMap := make(map[string]*Document)

	for _, ranking := range rankings {
		n := len(ranking)
		for rank, doc := range ranking {
			points := n - rank
			scores[doc.ID] += points
			if _, ok := docMap[doc.ID]; !ok {
				docMap[doc.ID] = doc.Clone()
			}
		}
	}

	// 创建结果列表
	result := make([]*Document, 0, len(docMap))
	for id, doc := range docMap {
		doc.Score = float64(scores[id])
		result = append(result, doc)
	}

	// 排序
	sort.Slice(result, func(i, j int) bool {
		return result[i].Score > result[j].Score
	})

	return result
}

// combSum 分数求和融合
func (rf *RankFusion) combSum(rankings [][]*Document) []*Document {
	scores := make(map[string]float64)
	docMap := make(map[string]*Document)

	for _, ranking := range rankings {
		for _, doc := range ranking {
			scores[doc.ID] += doc.Score
			if _, ok := docMap[doc.ID]; !ok {
				docMap[doc.ID] = doc.Clone()
			}
		}
	}

	// 创建结果列表
	result := make([]*Document, 0, len(docMap))
	for id, doc := range docMap {
		doc.Score = scores[id]
		result = append(result, doc)
	}

	// 排序
	sort.Slice(result, func(i, j int) bool {
		return result[i].Score > result[j].Score
	})

	return result
}
