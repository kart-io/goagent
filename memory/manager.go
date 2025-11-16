package memory

import (
	"context"

	"github.com/kart-io/goagent/interfaces"
)

// Manager 定义记忆管理器接口
//
// Deprecated: Use interfaces.MemoryManager instead.
// This type alias provides backward compatibility. It will be removed in v1.0.0.
//
// Migration: import "github.com/kart-io/goagent/interfaces"
// See: pkg/agent/docs/refactoring/migration-guide.md
type Manager = interfaces.MemoryManager

// Conversation 对话记录
//
// Deprecated: Use interfaces.Conversation instead.
// This type alias provides backward compatibility. It will be removed in v1.0.0.
//
// Migration: import "github.com/kart-io/goagent/interfaces"
// See: pkg/agent/docs/refactoring/migration-guide.md
type Conversation = interfaces.Conversation

// Case 案例记忆
//
// Deprecated: Use interfaces.Case instead.
// This type alias provides backward compatibility. It will be removed in v1.0.0.
//
// Migration: import "github.com/kart-io/goagent/interfaces"
// See: pkg/agent/docs/refactoring/migration-guide.md
type Case = interfaces.Case

// ConversationStore 对话存储接口
type ConversationStore interface {
	// Add 添加对话
	Add(ctx context.Context, conv *Conversation) error

	// Get 获取对话历史
	Get(ctx context.Context, sessionID string, limit int) ([]*Conversation, error)

	// Clear 清空会话
	Clear(ctx context.Context, sessionID string) error

	// Count 获取会话对话数量
	Count(ctx context.Context, sessionID string) (int, error)
}

// VectorStore 向量存储接口
type VectorStore interface {
	// Add 添加向量
	Add(ctx context.Context, id string, embedding []float64, metadata map[string]interface{}) error

	// Search 搜索相似向量
	Search(ctx context.Context, embedding []float64, limit int) ([]*SearchResult, error)

	// Delete 删除向量
	Delete(ctx context.Context, id string) error

	// Clear 清空存储
	Clear(ctx context.Context) error
}

// SearchResult 搜索结果
type SearchResult struct {
	ID        string                 `json:"id"`        // ID
	Score     float64                `json:"score"`     // 相似度分数
	Embedding []float64              `json:"embedding"` // 向量
	Metadata  map[string]interface{} `json:"metadata"`  // 元数据
}

// Embedder 嵌入生成器接口
type Embedder interface {
	// Embed 生成文本嵌入
	Embed(ctx context.Context, text string) ([]float64, error)

	// EmbedBatch 批量生成嵌入
	EmbedBatch(ctx context.Context, texts []string) ([][]float64, error)
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

// 向量存储类型
const (
	VectorStoreTypeMemory   = "memory"   // 内存存储
	VectorStoreTypeChroma   = "chroma"   // Chroma 向量数据库
	VectorStoreTypePinecone = "pinecone" // Pinecone
	VectorStoreTypeWeaviate = "weaviate" // Weaviate
)

// 嵌入提供商
const (
	EmbedderProviderOpenAI = "openai" // OpenAI
	EmbedderProviderLocal  = "local"  // 本地模型
	EmbedderProviderMock   = "mock"   // Mock（测试用）
)

// 默认配置值
const (
	DefaultMaxConversationLength = 10   // 默认最大对话长度
	DefaultSearchLimit           = 5    // 默认搜索结果数量
	DefaultSimilarityThreshold   = 0.7  // 默认相似度阈值
	DefaultEmbeddingDimension    = 1536 // OpenAI text-embedding-ada-002 维度
)

// DefaultConfig 返回默认配置
func DefaultConfig() *Config {
	return &Config{
		EnableConversation:    true,
		MaxConversationLength: DefaultMaxConversationLength,
		EnableVectorStore:     false,
		VectorStoreType:       VectorStoreTypeMemory,
		EmbeddingDimension:    DefaultEmbeddingDimension,
		EmbeddingModel:        "text-embedding-ada-002",
		EmbeddingProvider:     EmbedderProviderOpenAI,
		DefaultSearchLimit:    DefaultSearchLimit,
		SimilarityThreshold:   DefaultSimilarityThreshold,
	}
}
