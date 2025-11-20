package retrieval

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/qdrant/go-client/qdrant"

	agentErrors "github.com/kart-io/goagent/errors"
)

// QdrantVectorStore Qdrant 向量数据库存储
//
// 提供基于 Qdrant 的向量存储实现，支持高性能的语义搜索
type QdrantVectorStore struct {
	config QdrantConfig
	client *qdrant.Client
}

// QdrantConfig Qdrant 配置
type QdrantConfig struct {
	// URL Qdrant 服务地址
	URL string

	// APIKey API 密钥（如果需要）
	APIKey string

	// CollectionName 集合名称
	CollectionName string

	// VectorSize 向量维度
	VectorSize int

	// Distance 距离度量类型: cosine, euclidean, dot
	Distance string

	// Embedder 嵌入器（用于自动向量化）
	Embedder Embedder
}

// NewQdrantVectorStore 创建 Qdrant 向量存储
//
// 参数:
//   - config: Qdrant 配置
//
// 返回:
//   - *QdrantVectorStore: Qdrant 向量存储实例
//   - error: 错误信息
func NewQdrantVectorStore(ctx context.Context, config QdrantConfig) (*QdrantVectorStore, error) {
	if config.URL == "" {
		config.URL = "localhost:6334"
	}

	if config.CollectionName == "" {
		return nil, agentErrors.New(agentErrors.CodeInvalidConfig, "collection name is required").
			WithComponent("qdrant_store").
			WithOperation("create")
	}

	if config.VectorSize <= 0 {
		config.VectorSize = 100
	}

	if config.Distance == "" {
		config.Distance = "cosine"
	}

	if config.Embedder == nil {
		config.Embedder = NewSimpleEmbedder(config.VectorSize)
	}

	// 初始化 Qdrant 客户端
	clientConfig := &qdrant.Config{
		Host: config.URL,
	}
	if config.APIKey != "" {
		clientConfig.APIKey = config.APIKey
	}

	client, err := qdrant.NewClient(clientConfig)
	if err != nil {
		return nil, agentErrors.Wrap(err, agentErrors.CodeInternal, "failed to create Qdrant client").
			WithComponent("qdrant_store").
			WithOperation("create").
			WithContext("url", config.URL)
	}

	store := &QdrantVectorStore{
		config: config,
		client: client,
	}

	// 创建或验证集合
	if err := store.ensureCollection(ctx); err != nil {
		return nil, err
	}

	return store, nil
}

// ensureCollection 确保集合存在
func (q *QdrantVectorStore) ensureCollection(ctx context.Context) error {
	// 检查集合是否存在
	exists, err := q.client.CollectionExists(ctx, q.config.CollectionName)
	if err != nil {
		return agentErrors.Wrap(err, agentErrors.CodeInternal, "failed to check collection existence").
			WithComponent("qdrant_store").
			WithOperation("ensure_collection").
			WithContext("collection", q.config.CollectionName)
	}

	if exists {
		return nil
	}

	// 创建集合
	distance := qdrant.Distance_Cosine
	switch q.config.Distance {
	case "euclidean":
		distance = qdrant.Distance_Euclid
	case "dot":
		distance = qdrant.Distance_Dot
	case "cosine":
		distance = qdrant.Distance_Cosine
	}

	err = q.client.CreateCollection(ctx, &qdrant.CreateCollection{
		CollectionName: q.config.CollectionName,
		VectorsConfig: qdrant.NewVectorsConfig(&qdrant.VectorParams{
			Size:     uint64(q.config.VectorSize),
			Distance: distance,
		}),
	})

	if err != nil {
		return agentErrors.Wrap(err, agentErrors.CodeInternal, "failed to create collection").
			WithComponent("qdrant_store").
			WithOperation("ensure_collection").
			WithContext("collection", q.config.CollectionName)
	}

	return nil
}

// Add 添加文档和向量
func (q *QdrantVectorStore) Add(ctx context.Context, docs []*Document, vectors [][]float32) error {
	if len(docs) == 0 {
		return nil
	}

	if len(docs) != len(vectors) {
		return agentErrors.New(agentErrors.CodeInvalidInput, "number of documents and vectors must match").
			WithComponent("qdrant_store").
			WithOperation("add_documents").
			WithContext("num_docs", len(docs)).
			WithContext("num_vectors", len(vectors))
	}

	// 转换为 Qdrant points
	points := make([]*qdrant.PointStruct, len(docs))
	for i, doc := range docs {
		// 生成 ID 如果文档没有
		id := doc.ID
		if id == "" {
			id = uuid.New().String()
			doc.ID = id
		}

		// 转换 metadata 为 payload
		payload := make(map[string]*qdrant.Value)
		payload["page_content"] = qdrant.NewValueString(doc.PageContent)
		payload["id"] = qdrant.NewValueString(id)

		if doc.Metadata != nil {
			for k, v := range doc.Metadata {
				payload[k] = convertToQdrantValue(v)
			}
		}

		points[i] = &qdrant.PointStruct{
			Id:      qdrant.NewID(id),
			Vectors: qdrant.NewVectors(vectors[i]...),
			Payload: payload,
		}
	}

	// 批量上传点
	batchSize := 100
	for i := 0; i < len(points); i += batchSize {
		end := i + batchSize
		if end > len(points) {
			end = len(points)
		}

		batch := points[i:end]
		_, err := q.client.Upsert(ctx, &qdrant.UpsertPoints{
			CollectionName: q.config.CollectionName,
			Points:         batch,
		})

		if err != nil {
			return agentErrors.Wrap(err, agentErrors.CodeInternal, "failed to upsert points").
				WithComponent("qdrant_store").
				WithOperation("add_documents").
				WithContext("batch_start", i).
				WithContext("batch_end", end)
		}
	}

	return nil
}

// convertToQdrantValue 转换 Go 值为 Qdrant Value
func convertToQdrantValue(v interface{}) *qdrant.Value {
	switch val := v.(type) {
	case string:
		return qdrant.NewValueString(val)
	case int:
		return qdrant.NewValueInt(int64(val))
	case int64:
		return qdrant.NewValueInt(val)
	case float64:
		return qdrant.NewValueDouble(val)
	case float32:
		return qdrant.NewValueDouble(float64(val))
	case bool:
		return qdrant.NewValueBool(val)
	default:
		return qdrant.NewValueString(fmt.Sprintf("%v", v))
	}
}

// AddDocuments 添加文档（实现 VectorStore 接口）
func (q *QdrantVectorStore) AddDocuments(ctx context.Context, docs []*Document) error {
	// 自动生成向量
	if len(docs) == 0 {
		return nil
	}

	texts := make([]string, len(docs))
	for i, doc := range docs {
		texts[i] = doc.PageContent
	}

	vectors, err := q.config.Embedder.Embed(ctx, texts)
	if err != nil {
		return agentErrors.Wrap(err, agentErrors.CodeRetrievalEmbedding, "failed to generate vectors").
			WithComponent("qdrant_store").
			WithOperation("add_documents").
			WithContext("num_docs", len(docs))
	}

	return q.Add(ctx, docs, vectors)
}

// Search 相似度搜索
func (q *QdrantVectorStore) Search(ctx context.Context, query string, topK int) ([]*Document, error) {
	// 生成查询向量
	queryVector, err := q.config.Embedder.EmbedQuery(ctx, query)
	if err != nil {
		return nil, agentErrors.Wrap(err, agentErrors.CodeRetrievalEmbedding, "failed to embed query").
			WithComponent("qdrant_store").
			WithOperation("search").
			WithContext("query", query)
	}

	return q.SearchByVector(ctx, queryVector, topK)
}

// SearchByVector 通过向量搜索
func (q *QdrantVectorStore) SearchByVector(ctx context.Context, queryVector []float32, topK int) ([]*Document, error) {
	if topK <= 0 {
		topK = 4
	}

	// 执行搜索
	results, err := q.client.Query(ctx, &qdrant.QueryPoints{
		CollectionName: q.config.CollectionName,
		Query:          qdrant.NewQuery(queryVector...),
		Limit:          uintPtr(uint64(topK)),
		WithPayload:    qdrant.NewWithPayload(true),
	})

	if err != nil {
		return nil, agentErrors.Wrap(err, agentErrors.CodeRetrievalSearch, "failed to search vectors").
			WithComponent("qdrant_store").
			WithOperation("search_by_vector").
			WithContext("topK", topK)
	}

	// 转换结果为文档
	docs := make([]*Document, 0, len(results))
	for _, point := range results {
		doc := &Document{
			Metadata: make(map[string]interface{}),
		}

		// 提取 payload
		if point.Payload != nil {
			// 提取 page_content
			if content, ok := point.Payload["page_content"]; ok && content.GetStringValue() != "" {
				doc.PageContent = content.GetStringValue()
			}

			// 提取 ID
			if id, ok := point.Payload["id"]; ok && id.GetStringValue() != "" {
				doc.ID = id.GetStringValue()
			} else if point.Id != nil {
				doc.ID = point.Id.GetUuid()
			}

			// 提取其他 metadata
			for k, v := range point.Payload {
				if k != "page_content" && k != "id" {
					doc.Metadata[k] = convertFromQdrantValue(v)
				}
			}
		}

		// 设置分数
		doc.Score = float64(point.Score)

		docs = append(docs, doc)
	}

	return docs, nil
}

// convertFromQdrantValue 转换 Qdrant Value 为 Go 值
func convertFromQdrantValue(v *qdrant.Value) interface{} {
	if v == nil {
		return nil
	}

	if str := v.GetStringValue(); str != "" {
		return str
	}
	if v.GetIntegerValue() != 0 {
		return v.GetIntegerValue()
	}
	if v.GetDoubleValue() != 0 {
		return v.GetDoubleValue()
	}
	if v.GetBoolValue() {
		return true
	}

	return nil
}

// uintPtr 返回 uint64 指针
func uintPtr(v uint64) *uint64 {
	return &v
}

// SimilaritySearch 相似度搜索（实现 VectorStore 接口）
func (q *QdrantVectorStore) SimilaritySearch(ctx context.Context, query string, topK int) ([]*Document, error) {
	return q.Search(ctx, query, topK)
}

// SimilaritySearchWithScore 带分数的相似度搜索（实现 VectorStore 接口）
func (q *QdrantVectorStore) SimilaritySearchWithScore(ctx context.Context, query string, topK int) ([]*Document, error) {
	return q.Search(ctx, query, topK)
}

// Delete 删除文档
func (q *QdrantVectorStore) Delete(ctx context.Context, ids []string) error {
	if len(ids) == 0 {
		return nil
	}

	// 转换 ID 为 Qdrant PointId
	pointIDs := make([]*qdrant.PointId, len(ids))
	for i, id := range ids {
		pointIDs[i] = qdrant.NewID(id)
	}

	// 删除点
	_, err := q.client.Delete(ctx, &qdrant.DeletePoints{
		CollectionName: q.config.CollectionName,
		Points: &qdrant.PointsSelector{
			PointsSelectorOneOf: &qdrant.PointsSelector_Points{
				Points: &qdrant.PointsIdsList{
					Ids: pointIDs,
				},
			},
		},
	})

	if err != nil {
		return agentErrors.Wrap(err, agentErrors.CodeInternal, "failed to delete points").
			WithComponent("qdrant_store").
			WithOperation("delete_documents").
			WithContext("num_ids", len(ids))
	}

	return nil
}

// Update 更新文档
func (q *QdrantVectorStore) Update(ctx context.Context, docs []*Document) error {
	if len(docs) == 0 {
		return nil
	}

	// 生成向量
	texts := make([]string, len(docs))
	for i, doc := range docs {
		texts[i] = doc.PageContent
	}

	vectors, err := q.config.Embedder.Embed(ctx, texts)
	if err != nil {
		return agentErrors.Wrap(err, agentErrors.CodeRetrievalEmbedding, "failed to generate vectors for update").
			WithComponent("qdrant_store").
			WithOperation("update_documents").
			WithContext("num_docs", len(docs))
	}

	// Qdrant 的 Upsert 操作会自动处理更新
	return q.Add(ctx, docs, vectors)
}

// GetEmbedding 获取嵌入向量
func (q *QdrantVectorStore) GetEmbedding(ctx context.Context, text string) ([]float32, error) {
	return q.config.Embedder.EmbedQuery(ctx, text)
}

// Close 关闭连接
func (q *QdrantVectorStore) Close() error {
	if q.client != nil {
		return q.client.Close()
	}
	return nil
}

// QdrantVectorStoreOption Qdrant 选项函数
type QdrantVectorStoreOption func(*QdrantConfig)

// WithQdrantAPIKey 设置 API 密钥
func WithQdrantAPIKey(apiKey string) QdrantVectorStoreOption {
	return func(c *QdrantConfig) {
		c.APIKey = apiKey
	}
}

// WithQdrantDistance 设置距离度量
func WithQdrantDistance(distance string) QdrantVectorStoreOption {
	return func(c *QdrantConfig) {
		c.Distance = distance
	}
}

// WithQdrantEmbedder 设置嵌入器
func WithQdrantEmbedder(embedder Embedder) QdrantVectorStoreOption {
	return func(c *QdrantConfig) {
		c.Embedder = embedder
	}
}
