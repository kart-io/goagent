package memory

import (
	"context"

	"github.com/kart-io/goagent/constants"
)

// VectorStore 向量存储接口
type VectorStore interface {
	// Add 添加向量
	Add(ctx context.Context, id string, embedding []float32, metadata map[string]interface{}) error

	// Search 搜索相似向量
	Search(ctx context.Context, embedding []float32, limit int) ([]*SearchResult, error)

	// Delete 删除向量
	Delete(ctx context.Context, id string) error

	// Clear 清空存储
	Clear(ctx context.Context) error
}

// SearchResult 搜索结果
type SearchResult struct {
	ID        string                 `json:"id"`        // ID
	Score     float64                `json:"score"`     // 相似度分数
	Embedding []float32              `json:"embedding"` // 向量
	Metadata  map[string]interface{} `json:"metadata"`  // 元数据
}

// Config 记忆管理器配置
type Config struct {
	// 对话记忆配置
	EnableConversation    bool `json:"enable_conversation"`     // 是否启用对话记忆
	MaxConversationLength int  `json:"max_conversation_length"` // 最大对话长度

	// 向量存储配置
	EnableVectorStore  bool   `json:"enable_vector_store"` // 是否启用向量存储
	VectorStoreType    string `json:"vector_store_type"`   // 向量存储类型
	VectorStorePath    string `json:"vector_store_path"`   // 向量存储路径
	EmbeddingDimension int    `json:"embedding_dimension"` // 嵌入维度

	// 嵌入器配置
	EmbeddingModel    string `json:"embedding_model"`    // 嵌入模型
	EmbeddingProvider string `json:"embedding_provider"` // 嵌入提供商

	// 搜索配置
	DefaultSearchLimit  int     `json:"default_search_limit"` // 默认搜索结果数量
	SimilarityThreshold float64 `json:"similarity_threshold"` // 相似度阈值
}

// DefaultConfig 返回默认配置
func DefaultConfig() *Config {
	return &Config{
		EnableConversation:    true,
		MaxConversationLength: constants.DefaultMemoryMaxConversationLength,
		EnableVectorStore:     false,
		VectorStoreType:       constants.VectorStoreTypeMemory,
		EmbeddingDimension:    constants.DefaultMemoryEmbeddingDimension,
		EmbeddingModel:        constants.DefaultMemoryEmbeddingModel,
		EmbeddingProvider:     constants.EmbedderProviderOpenAI,
		DefaultSearchLimit:    constants.DefaultMemorySearchLimit,
		SimilarityThreshold:   constants.DefaultMemorySimilarityThreshold,
	}
}
