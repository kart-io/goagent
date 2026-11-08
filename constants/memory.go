package constants

// 记忆配置词汇和默认策略；标识存在不表示框架已实现对应后端。
// Chroma、Pinecone、Weaviate、Local 和 Mock 当前仅保留配置词汇。
const (
	VectorStoreTypeMemory              = "memory"
	VectorStoreTypeChroma              = "chroma"
	VectorStoreTypePinecone            = "pinecone"
	VectorStoreTypeWeaviate            = "weaviate"
	EmbedderProviderOpenAI             = "openai"
	EmbedderProviderLocal              = "local"
	EmbedderProviderMock               = "mock"
	DefaultMemoryMaxConversationLength = 10
	DefaultMemorySearchLimit           = 5
	DefaultMemorySimilarityThreshold   = 0.7
	DefaultMemoryEmbeddingDimension    = 1536
	DefaultMemoryEmbeddingModel        = "text-embedding-ada-002"
)
