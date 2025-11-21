package cache

import (
	"context"
	"time"
)

// CacheEntry represents a cached LLM response
type CacheEntry struct {
	// Key is the unique identifier for this entry
	Key string `json:"key"`

	// Prompt is the original prompt text
	Prompt string `json:"prompt"`

	// Embedding is the vector representation of the prompt
	Embedding []float32 `json:"embedding"`

	// Response is the cached LLM response
	Response string `json:"response"`

	// Model is the LLM model used
	Model string `json:"model"`

	// TokensUsed is the number of tokens consumed
	TokensUsed int `json:"tokens_used"`

	// CreatedAt is when this entry was created
	CreatedAt time.Time `json:"created_at"`

	// AccessedAt is when this entry was last accessed
	AccessedAt time.Time `json:"accessed_at"`

	// HitCount is the number of times this entry was accessed
	HitCount int64 `json:"hit_count"`

	// Metadata contains additional information
	Metadata map[string]interface{} `json:"metadata,omitempty"`
}

// CacheStats contains cache performance statistics
type CacheStats struct {
	// TotalEntries is the number of entries in cache
	TotalEntries int64 `json:"total_entries"`

	// TotalHits is the total number of cache hits
	TotalHits int64 `json:"total_hits"`

	// TotalMisses is the total number of cache misses
	TotalMisses int64 `json:"total_misses"`

	// HitRate is the cache hit rate (0.0 - 1.0)
	HitRate float64 `json:"hit_rate"`

	// AverageSimilarity is the average similarity score of hits
	AverageSimilarity float64 `json:"average_similarity"`

	// TokensSaved is the estimated tokens saved by caching
	TokensSaved int64 `json:"tokens_saved"`

	// LatencySaved is the estimated latency saved (in ms)
	LatencySaved int64 `json:"latency_saved_ms"`

	// MemoryUsed is the memory used by cache (in bytes)
	MemoryUsed int64 `json:"memory_used_bytes"`
}

// SemanticCacheConfig configures the semantic cache
type SemanticCacheConfig struct {
	// SimilarityThreshold is the minimum similarity score for a cache hit (0.0 - 1.0)
	// Default: 0.95
	SimilarityThreshold float64 `json:"similarity_threshold"`

	// MaxEntries is the maximum number of entries in cache
	// Default: 10000
	MaxEntries int `json:"max_entries"`

	// TTL is the time-to-live for cache entries
	// Default: 24 hours
	TTL time.Duration `json:"ttl"`

	// EnableStats enables statistics collection
	// Default: true
	EnableStats bool `json:"enable_stats"`

	// EvictionPolicy determines how entries are evicted when cache is full
	// Options: "lru", "lfu", "fifo"
	// Default: "lru"
	EvictionPolicy string `json:"eviction_policy"`

	// ModelSpecific if true, caches are model-specific
	// Default: true
	ModelSpecific bool `json:"model_specific"`

	// NormalizePrompts if true, normalizes prompts before caching
	// Default: true
	NormalizePrompts bool `json:"normalize_prompts"`
}

// DefaultSemanticCacheConfig returns default configuration
func DefaultSemanticCacheConfig() *SemanticCacheConfig {
	return &SemanticCacheConfig{
		SimilarityThreshold: 0.95,
		MaxEntries:          10000,
		TTL:                 24 * time.Hour,
		EnableStats:         true,
		EvictionPolicy:      "lru",
		ModelSpecific:       true,
		NormalizePrompts:    true,
	}
}

// SemanticCache defines the interface for semantic caching
type SemanticCache interface {
	// Get retrieves a cached response if similarity >= threshold
	// Returns the entry and similarity score if found
	Get(ctx context.Context, prompt string, model string) (*CacheEntry, float64, error)

	// Set stores a response in the cache
	Set(ctx context.Context, prompt string, response string, model string, tokensUsed int) error

	// Delete removes an entry from cache
	Delete(ctx context.Context, key string) error

	// Clear removes all entries from cache
	Clear(ctx context.Context) error

	// Stats returns cache statistics
	Stats() *CacheStats

	// Close closes the cache and releases resources
	Close() error
}

// EmbeddingProvider generates vector embeddings for text
type EmbeddingProvider interface {
	// Embed generates an embedding vector for the given text
	Embed(ctx context.Context, text string) ([]float32, error)

	// EmbedBatch generates embeddings for multiple texts
	EmbedBatch(ctx context.Context, texts []string) ([][]float32, error)

	// Dimension returns the embedding dimension
	Dimension() int
}

// CacheStorage defines the storage backend for cache entries
type CacheStorage interface {
	// Store saves a cache entry
	Store(ctx context.Context, entry *CacheEntry) error

	// Load retrieves a cache entry by key
	Load(ctx context.Context, key string) (*CacheEntry, error)

	// Delete removes a cache entry
	Delete(ctx context.Context, key string) error

	// List returns all cache entries
	List(ctx context.Context) ([]*CacheEntry, error)

	// Clear removes all entries
	Clear(ctx context.Context) error

	// Count returns the number of entries
	Count(ctx context.Context) (int64, error)

	// Close closes the storage
	Close() error
}
