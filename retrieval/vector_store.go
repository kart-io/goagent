package retrieval

import (
	"context"
	cryptorand "crypto/rand"
	"math"
	"math/big"
	"sync"

	agentErrors "github.com/kart-io/goagent/errors"
	"github.com/kart-io/goagent/interfaces"
)

// VectorStore 向量存储接口
//
// Deprecated: Use interfaces.VectorStore instead.
// This type alias provides backward compatibility. It will be removed in v1.0.0.
//
// Migration: import "github.com/kart-io/goagent/interfaces"
// See: pkg/agent/docs/refactoring/migration-guide.md
type VectorStore = interfaces.VectorStore

// VectorStoreRetriever 向量存储检索器
//
// 使用向量相似度进行文档检索
type VectorStoreRetriever struct {
	*BaseRetriever

	// VectorStore 向量存储实例
	VectorStore VectorStore

	// SearchType 搜索类型
	SearchType SearchType

	// SearchKwargs 搜索参数
	SearchKwargs map[string]interface{}
}

// SearchType 搜索类型
type SearchType string

const (
	// SearchTypeSimilarity 相似度搜索
	SearchTypeSimilarity SearchType = "similarity"

	// SearchTypeSimilarityScoreThreshold 基于相似度阈值的搜索
	SearchTypeSimilarityScoreThreshold SearchType = "similarity_score_threshold"

	// SearchTypeMMR 最大边际相关性搜索
	SearchTypeMMR SearchType = "mmr"
)

// NewVectorStoreRetriever 创建向量存储检索器
func NewVectorStoreRetriever(vectorStore VectorStore, config RetrieverConfig) *VectorStoreRetriever {
	retriever := &VectorStoreRetriever{
		BaseRetriever: NewBaseRetriever(),
		VectorStore:   vectorStore,
		SearchType:    SearchTypeSimilarity,
		SearchKwargs:  make(map[string]interface{}),
	}

	retriever.TopK = config.TopK
	retriever.MinScore = config.MinScore
	retriever.Name = config.Name

	return retriever
}

// GetRelevantDocuments 检索相关文档
func (v *VectorStoreRetriever) GetRelevantDocuments(ctx context.Context, query string) ([]*Document, error) {
	var docs []*Document
	var err error

	switch v.SearchType {
	case SearchTypeSimilarity:
		docs, err = v.VectorStore.SimilaritySearch(ctx, query, v.TopK)
	case SearchTypeSimilarityScoreThreshold:
		docs, err = v.VectorStore.SimilaritySearchWithScore(ctx, query, v.TopK)
		if err == nil {
			docs = v.FilterByScore(docs)
		}
	case SearchTypeMMR:
		// MMR 需要额外参数，这里简化为相似度搜索
		docs, err = v.VectorStore.SimilaritySearch(ctx, query, v.TopK)
	default:
		return nil, agentErrors.New(agentErrors.CodeInvalidInput, "unknown search type").
			WithComponent("vector_store_retriever").
			WithOperation("get_relevant_documents").
			WithContext("search_type", string(v.SearchType))
	}

	if err != nil {
		return nil, agentErrors.Wrap(err, agentErrors.CodeRetrievalSearch, "vector store search failed").
			WithComponent("vector_store_retriever").
			WithOperation("get_relevant_documents").
			WithContext("query", query).
			WithContext("topK", v.TopK)
	}

	return docs, nil
}

// WithSearchType 设置搜索类型
func (v *VectorStoreRetriever) WithSearchType(searchType SearchType) *VectorStoreRetriever {
	v.SearchType = searchType
	return v
}

// WithSearchKwargs 设置搜索参数
func (v *VectorStoreRetriever) WithSearchKwargs(kwargs map[string]interface{}) *VectorStoreRetriever {
	v.SearchKwargs = kwargs
	return v
}

// MockVectorStore 模拟向量存储（用于测试和示例）
type MockVectorStore struct {
	documents []*Document
	mu        sync.RWMutex
}

// NewMockVectorStore 创建模拟向量存储
func NewMockVectorStore() *MockVectorStore {
	return &MockVectorStore{
		documents: make([]*Document, 0),
	}
}

// SimilaritySearch 相似度搜索（模拟实现）
func (m *MockVectorStore) SimilaritySearch(ctx context.Context, query string, topK int) ([]*Document, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	// 简单的模拟：基于文本包含关系
	results := make([]*Document, 0)
	for _, doc := range m.documents {
		// 模拟相似度计算
		score := m.calculateSimilarity(query, doc.PageContent)
		if score > 0 {
			docCopy := doc.Clone()
			docCopy.Score = score
			results = append(results, docCopy)
		}
	}

	// 排序并返回 top-k
	collection := DocumentCollection(results)
	collection.SortByScore()

	if topK > 0 && len(collection) > topK {
		collection = collection[:topK]
	}

	return collection, nil
}

// SimilaritySearchWithScore 带分数的相似度搜索
func (m *MockVectorStore) SimilaritySearchWithScore(ctx context.Context, query string, topK int) ([]*Document, error) {
	return m.SimilaritySearch(ctx, query, topK)
}

// AddDocuments 添加文档
func (m *MockVectorStore) AddDocuments(ctx context.Context, docs []*Document) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.documents = append(m.documents, docs...)
	return nil
}

// Delete 删除文档
func (m *MockVectorStore) Delete(ctx context.Context, ids []string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	idSet := make(map[string]bool)
	for _, id := range ids {
		idSet[id] = true
	}

	filtered := make([]*Document, 0)
	for _, doc := range m.documents {
		if !idSet[doc.ID] {
			filtered = append(filtered, doc)
		}
	}

	m.documents = filtered
	return nil
}

// calculateSimilarity 计算相似度（简化版本）
//
// 实际应该使用向量余弦相似度
func (m *MockVectorStore) calculateSimilarity(query, content string) float64 {
	// 简单的基于包含和随机性的模拟
	score := 0.0

	// 基础分数
	if len(content) > 0 {
		score = 0.3
	}

	// 如果包含查询词，增加分数
	queryWords := splitWords(query)
	for _, word := range queryWords {
		if contains(content, word) {
			score += 0.2
		}
	}

	// 添加一些随机性 - using crypto/rand for security
	n, err := cryptorand.Int(cryptorand.Reader, big.NewInt(1000))
	if err == nil {
		score += float64(n.Int64()) / 1000.0 * 0.3
	}

	// 确保在 0-1 范围内
	return math.Min(score, 1.0)
}

// LoadDocuments 加载文档到向量存储
func (m *MockVectorStore) LoadDocuments(docs []*Document) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.documents = append(m.documents, docs...)
}

// GetAllDocuments 获取所有文档
func (m *MockVectorStore) GetAllDocuments() []*Document {
	m.mu.RLock()
	defer m.mu.RUnlock()

	result := make([]*Document, len(m.documents))
	copy(result, m.documents)
	return result
}

// Clear 清空所有文档
func (m *MockVectorStore) Clear() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.documents = make([]*Document, 0)
}
