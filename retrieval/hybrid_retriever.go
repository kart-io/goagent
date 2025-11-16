package retrieval

import (
	"context"
	"fmt"
)

// HybridRetriever 混合检索器
//
// 结合向量检索和关键词检索，使用加权融合策略
type HybridRetriever struct {
	*BaseRetriever

	// VectorRetriever 向量检索器
	VectorRetriever Retriever

	// KeywordRetriever 关键词检索器
	KeywordRetriever Retriever

	// VectorWeight 向量检索的权重（0-1）
	VectorWeight float64

	// KeywordWeight 关键词检索的权重（0-1）
	KeywordWeight float64

	// FusionStrategy 融合策略
	FusionStrategy FusionStrategy
}

// FusionStrategy 融合策略
type FusionStrategy string

const (
	// FusionStrategyWeightedSum 加权求和
	FusionStrategyWeightedSum FusionStrategy = "weighted_sum"

	// FusionStrategyRRF 倒数排名融合 (Reciprocal Rank Fusion)
	FusionStrategyRRF FusionStrategy = "rrf"

	// FusionStrategyCombSum 组合求和
	FusionStrategyCombSum FusionStrategy = "comb_sum"
)

// NewHybridRetriever 创建混合检索器
func NewHybridRetriever(
	vectorRetriever, keywordRetriever Retriever,
	vectorWeight, keywordWeight float64,
	config RetrieverConfig,
) *HybridRetriever {
	retriever := &HybridRetriever{
		BaseRetriever:    NewBaseRetriever(),
		VectorRetriever:  vectorRetriever,
		KeywordRetriever: keywordRetriever,
		VectorWeight:     vectorWeight,
		KeywordWeight:    keywordWeight,
		FusionStrategy:   FusionStrategyWeightedSum,
	}

	retriever.TopK = config.TopK
	retriever.MinScore = config.MinScore
	retriever.Name = config.Name

	return retriever
}

// GetRelevantDocuments 检索相关文档
func (h *HybridRetriever) GetRelevantDocuments(ctx context.Context, query string) ([]*Document, error) {
	// 并发执行两种检索
	type result struct {
		docs []*Document
		err  error
	}

	vectorChan := make(chan result, 1)
	keywordChan := make(chan result, 1)

	// 向量检索
	go func() {
		docs, err := h.VectorRetriever.GetRelevantDocuments(ctx, query)
		vectorChan <- result{docs: docs, err: err}
	}()

	// 关键词检索
	go func() {
		docs, err := h.KeywordRetriever.GetRelevantDocuments(ctx, query)
		keywordChan <- result{docs: docs, err: err}
	}()

	// 等待结果
	vectorResult := <-vectorChan
	keywordResult := <-keywordChan

	if vectorResult.err != nil {
		return nil, fmt.Errorf("vector retrieval failed: %w", vectorResult.err)
	}
	if keywordResult.err != nil {
		return nil, fmt.Errorf("keyword retrieval failed: %w", keywordResult.err)
	}

	// 融合结果
	var fused []*Document
	switch h.FusionStrategy {
	case FusionStrategyWeightedSum:
		fused = h.weightedSumFusion(vectorResult.docs, keywordResult.docs)
	case FusionStrategyRRF:
		fused = h.rrfFusion(vectorResult.docs, keywordResult.docs)
	case FusionStrategyCombSum:
		fused = h.combSumFusion(vectorResult.docs, keywordResult.docs)
	default:
		return nil, fmt.Errorf("unknown fusion strategy: %s", h.FusionStrategy)
	}

	// 排序
	collection := DocumentCollection(fused)
	collection.SortByScore()

	// 应用过滤和限制
	filtered := h.FilterByScore(collection)
	limited := h.LimitTopK(filtered)

	return limited, nil
}

// weightedSumFusion 加权求和融合
func (h *HybridRetriever) weightedSumFusion(vectorDocs, keywordDocs []*Document) []*Document {
	// 归一化分数
	vectorNormalized := normalizeScores(vectorDocs)
	keywordNormalized := normalizeScores(keywordDocs)

	// 创建文档映射
	docMap := make(map[string]*Document)

	// 添加向量检索结果
	for i, doc := range vectorNormalized {
		docCopy := doc.Clone()
		docCopy.Score = vectorDocs[i].Score * h.VectorWeight
		docMap[doc.ID] = docCopy
	}

	// 融合关键词检索结果
	for i, doc := range keywordNormalized {
		if existing, ok := docMap[doc.ID]; ok {
			// 文档已存在，累加分数
			existing.Score += keywordDocs[i].Score * h.KeywordWeight
		} else {
			// 新文档
			docCopy := doc.Clone()
			docCopy.Score = keywordDocs[i].Score * h.KeywordWeight
			docMap[doc.ID] = docCopy
		}
	}

	// 转换为列表
	results := make([]*Document, 0, len(docMap))
	for _, doc := range docMap {
		results = append(results, doc)
	}

	return results
}

// rrfFusion 倒数排名融合
//
// RRF = sum(1/(k + rank))
// k 通常设为 60
func (h *HybridRetriever) rrfFusion(vectorDocs, keywordDocs []*Document) []*Document {
	const k = 60.0

	// 创建文档映射
	docMap := make(map[string]*Document)
	docScores := make(map[string]float64)

	// 处理向量检索结果
	for rank, doc := range vectorDocs {
		rrfScore := 1.0 / (k + float64(rank+1))
		docScores[doc.ID] = rrfScore * h.VectorWeight
		docMap[doc.ID] = doc.Clone()
	}

	// 处理关键词检索结果
	for rank, doc := range keywordDocs {
		rrfScore := 1.0 / (k + float64(rank+1))
		if _, ok := docScores[doc.ID]; ok {
			docScores[doc.ID] += rrfScore * h.KeywordWeight
		} else {
			docScores[doc.ID] = rrfScore * h.KeywordWeight
			docMap[doc.ID] = doc.Clone()
		}
	}

	// 设置最终分数
	results := make([]*Document, 0, len(docMap))
	for id, doc := range docMap {
		doc.Score = docScores[id]
		results = append(results, doc)
	}

	return results
}

// combSumFusion 组合求和融合
//
// 直接累加所有检索器的原始分数
func (h *HybridRetriever) combSumFusion(vectorDocs, keywordDocs []*Document) []*Document {
	// 创建文档映射
	docMap := make(map[string]*Document)

	// 添加向量检索结果
	for _, doc := range vectorDocs {
		docCopy := doc.Clone()
		docCopy.Score = doc.Score
		docMap[doc.ID] = docCopy
	}

	// 融合关键词检索结果
	for _, doc := range keywordDocs {
		if existing, ok := docMap[doc.ID]; ok {
			existing.Score += doc.Score
		} else {
			docCopy := doc.Clone()
			docMap[doc.ID] = docCopy
		}
	}

	// 转换为列表
	results := make([]*Document, 0, len(docMap))
	for _, doc := range docMap {
		results = append(results, doc)
	}

	return results
}

// WithFusionStrategy 设置融合策略
func (h *HybridRetriever) WithFusionStrategy(strategy FusionStrategy) *HybridRetriever {
	h.FusionStrategy = strategy
	return h
}

// WithWeights 设置权重
func (h *HybridRetriever) WithWeights(vectorWeight, keywordWeight float64) *HybridRetriever {
	h.VectorWeight = vectorWeight
	h.KeywordWeight = keywordWeight
	return h
}

// normalizeScores 归一化分数到 0-1 范围
func normalizeScores(docs []*Document) []*Document {
	if len(docs) == 0 {
		return docs
	}

	// 找到最大和最小分数
	minScore := docs[0].Score
	maxScore := docs[0].Score

	for _, doc := range docs {
		if doc.Score < minScore {
			minScore = doc.Score
		}
		if doc.Score > maxScore {
			maxScore = doc.Score
		}
	}

	// 归一化
	scoreRange := maxScore - minScore
	if scoreRange == 0 {
		// 所有分数相同
		return docs
	}

	normalized := make([]*Document, len(docs))
	for i, doc := range docs {
		docCopy := doc.Clone()
		docCopy.Score = (doc.Score - minScore) / scoreRange
		normalized[i] = docCopy
	}

	return normalized
}
