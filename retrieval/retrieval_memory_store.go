package retrieval

import (
	"context"
	"sort"
	"sync"

	agentErrors "github.com/kart-io/goagent/errors"
)

// MemoryVectorStore 内存向量存储实现
//
// 线程安全的内存向量存储，支持：
// - 余弦相似度搜索
// - 向量和文档的增删改查
// - 自动向量化
type MemoryVectorStore struct {
	// 嵌入器（用于自动向量化）
	embedder Embedder

	// 距离度量类型
	distanceMetric DistanceMetric

	// 文档和向量存储
	documents map[string]*DocumentWithVector
	vectors   map[string][]float32

	// 读写锁
	mu sync.RWMutex
}

// DocumentWithVector 包含向量的文档
type DocumentWithVector struct {
	Document *Document
	Vector   []float32
}

// DistanceMetric 距离度量类型
type DistanceMetric string

const (
	// DistanceMetricCosine 余弦相似度
	DistanceMetricCosine DistanceMetric = "cosine"

	// DistanceMetricEuclidean 欧氏距离
	DistanceMetricEuclidean DistanceMetric = "euclidean"

	// DistanceMetricDot 点积
	DistanceMetricDot DistanceMetric = "dot"
)

// MemoryVectorStoreConfig 内存向量存储配置
type MemoryVectorStoreConfig struct {
	Embedder       Embedder
	DistanceMetric DistanceMetric
}

// NewMemoryVectorStore 创建内存向量存储
func NewMemoryVectorStore(config MemoryVectorStoreConfig) *MemoryVectorStore {
	if config.Embedder == nil {
		// 默认使用简单嵌入器
		config.Embedder = NewSimpleEmbedder(100)
	}

	if config.DistanceMetric == "" {
		config.DistanceMetric = DistanceMetricCosine
	}

	return &MemoryVectorStore{
		embedder:       config.Embedder,
		distanceMetric: config.DistanceMetric,
		documents:      make(map[string]*DocumentWithVector),
		vectors:        make(map[string][]float32),
	}
}

// Add 添加文档和向量
func (m *MemoryVectorStore) Add(ctx context.Context, docs []*Document, vectors [][]float32) error {
	if len(docs) == 0 {
		return nil
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	// 如果没有提供向量，自动生成
	if len(vectors) == 0 {
		texts := make([]string, len(docs))
		for i, doc := range docs {
			texts[i] = doc.PageContent
		}

		generatedVectors, err := m.embedder.Embed(ctx, texts)
		if err != nil {
			return agentErrors.Wrap(err, agentErrors.CodeRetrievalEmbedding, "failed to generate vectors").
				WithComponent("memory_store").
				WithOperation("add_documents").
				WithContext("num_docs", len(docs))
		}
		vectors = generatedVectors
	}

	if len(docs) != len(vectors) {
		return agentErrors.New(agentErrors.CodeVectorDimMismatch, "documents and vectors count mismatch").
			WithComponent("memory_store").
			WithOperation("add_documents").
			WithContext("num_docs", len(docs)).
			WithContext("num_vectors", len(vectors))
	}

	// 存储文档和向量
	for i, doc := range docs {
		if doc.ID == "" {
			doc.ID = generateID()
		}

		m.documents[doc.ID] = &DocumentWithVector{
			Document: doc,
			Vector:   vectors[i],
		}
		m.vectors[doc.ID] = vectors[i]
	}

	return nil
}

// AddDocuments 添加文档（实现 VectorStore 接口）
func (m *MemoryVectorStore) AddDocuments(ctx context.Context, docs []*Document) error {
	return m.Add(ctx, docs, nil)
}

// Search 相似度搜索
func (m *MemoryVectorStore) Search(ctx context.Context, query string, topK int) ([]*Document, error) {
	// 生成查询向量
	queryVector, err := m.embedder.EmbedQuery(ctx, query)
	if err != nil {
		return nil, agentErrors.Wrap(err, agentErrors.CodeRetrievalEmbedding, "failed to embed query").
			WithComponent("memory_store").
			WithOperation("search").
			WithContext("query", query)
	}

	return m.SearchByVector(ctx, queryVector, topK)
}

// SearchByVector 通过向量搜索
func (m *MemoryVectorStore) SearchByVector(ctx context.Context, queryVector []float32, topK int) ([]*Document, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if len(m.documents) == 0 {
		return []*Document{}, nil
	}

	// 计算所有文档的相似度
	type docScore struct {
		doc   *Document
		score float32
	}

	scores := make([]docScore, 0, len(m.documents))

	for _, docWithVec := range m.documents {
		score, err := m.calculateSimilarity(queryVector, docWithVec.Vector)
		if err != nil {
			continue // 跳过错误的向量
		}

		doc := docWithVec.Document.Clone()
		doc.Score = float64(score)

		scores = append(scores, docScore{
			doc:   doc,
			score: score,
		})
	}

	// 根据距离度量排序
	sort.Slice(scores, func(i, j int) bool {
		// 余弦相似度和点积：越大越相似
		// 欧氏距离：越小越相似
		if m.distanceMetric == DistanceMetricEuclidean {
			return scores[i].score < scores[j].score
		}
		return scores[i].score > scores[j].score
	})

	// 返回 top-k
	if topK > 0 && len(scores) > topK {
		scores = scores[:topK]
	}

	results := make([]*Document, len(scores))
	for i, ds := range scores {
		results[i] = ds.doc
	}

	return results, nil
}

// SimilaritySearch 相似度搜索（实现 VectorStore 接口）
func (m *MemoryVectorStore) SimilaritySearch(ctx context.Context, query string, topK int) ([]*Document, error) {
	return m.Search(ctx, query, topK)
}

// SimilaritySearchWithScore 带分数的相似度搜索（实现 VectorStore 接口）
func (m *MemoryVectorStore) SimilaritySearchWithScore(ctx context.Context, query string, topK int) ([]*Document, error) {
	return m.Search(ctx, query, topK)
}

// Delete 删除文档
func (m *MemoryVectorStore) Delete(ctx context.Context, ids []string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	for _, id := range ids {
		delete(m.documents, id)
		delete(m.vectors, id)
	}

	return nil
}

// Update 更新文档
func (m *MemoryVectorStore) Update(ctx context.Context, docs []*Document) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	for _, doc := range docs {
		if doc.ID == "" {
			return agentErrors.New(agentErrors.CodeInvalidInput, "document ID is required for update").
				WithComponent("memory_store").
				WithOperation("update_documents")
		}

		// 检查文档是否存在
		if _, exists := m.documents[doc.ID]; !exists {
			return agentErrors.New(agentErrors.CodeDocumentNotFound, "document not found").
				WithComponent("memory_store").
				WithOperation("update_documents").
				WithContext("document_id", doc.ID)
		}

		// 重新生成向量
		vector, err := m.embedder.EmbedQuery(ctx, doc.PageContent)
		if err != nil {
			return agentErrors.Wrap(err, agentErrors.CodeRetrievalEmbedding, "failed to generate vector for document").
				WithComponent("memory_store").
				WithOperation("update_documents").
				WithContext("document_id", doc.ID)
		}

		// 更新文档和向量
		m.documents[doc.ID] = &DocumentWithVector{
			Document: doc,
			Vector:   vector,
		}
		m.vectors[doc.ID] = vector
	}

	return nil
}

// Get 获取文档
func (m *MemoryVectorStore) Get(ctx context.Context, id string) (*Document, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	docWithVec, exists := m.documents[id]
	if !exists {
		return nil, agentErrors.New(agentErrors.CodeDocumentNotFound, "document not found").
			WithComponent("memory_store").
			WithOperation("get_document").
			WithContext("document_id", id)
	}

	return docWithVec.Document.Clone(), nil
}

// GetVector 获取文档向量
func (m *MemoryVectorStore) GetVector(ctx context.Context, id string) ([]float32, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	vector, exists := m.vectors[id]
	if !exists {
		return nil, agentErrors.New(agentErrors.CodeDocumentNotFound, "vector for document not found").
			WithComponent("memory_store").
			WithOperation("get_vector").
			WithContext("document_id", id)
	}

	// 返回副本
	result := make([]float32, len(vector))
	copy(result, vector)
	return result, nil
}

// Count 返回文档数量
func (m *MemoryVectorStore) Count() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.documents)
}

// Clear 清空所有文档
func (m *MemoryVectorStore) Clear() {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.documents = make(map[string]*DocumentWithVector)
	m.vectors = make(map[string][]float32)
}

// calculateSimilarity 计算相似度
func (m *MemoryVectorStore) calculateSimilarity(vec1, vec2 []float32) (float32, error) {
	switch m.distanceMetric {
	case DistanceMetricCosine:
		return cosineSimilarity(vec1, vec2)
	case DistanceMetricEuclidean:
		return euclideanDistance(vec1, vec2)
	case DistanceMetricDot:
		return dotProduct(vec1, vec2)
	default:
		return cosineSimilarity(vec1, vec2)
	}
}

// GetEmbedding 获取嵌入向量（实现扩展接口）
func (m *MemoryVectorStore) GetEmbedding(ctx context.Context, text string) ([]float32, error) {
	return m.embedder.EmbedQuery(ctx, text)
}
