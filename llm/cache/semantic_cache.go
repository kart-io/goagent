package cache

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// MemorySemanticCache implements SemanticCache with in-memory storage
type MemorySemanticCache struct {
	config   *SemanticCacheConfig
	provider EmbeddingProvider

	// entries stores cache entries by key
	entries map[string]*CacheEntry

	// accessOrder tracks LRU order
	accessOrder []string

	mu sync.RWMutex

	// Statistics
	hits            int64
	misses          int64
	tokensSaved     int64
	similaritySum   float64
	similarityCount int64

	// done channel for cleanup goroutine
	done chan struct{}
}

// NewMemorySemanticCache creates a new in-memory semantic cache
func NewMemorySemanticCache(provider EmbeddingProvider, config *SemanticCacheConfig) *MemorySemanticCache {
	if config == nil {
		config = DefaultSemanticCacheConfig()
	}

	cache := &MemorySemanticCache{
		config:      config,
		provider:    provider,
		entries:     make(map[string]*CacheEntry),
		accessOrder: make([]string, 0),
		done:        make(chan struct{}),
	}

	// Start cleanup goroutine
	go cache.cleanupLoop()

	return cache
}

// Get retrieves a cached response if similarity >= threshold
func (c *MemorySemanticCache) Get(ctx context.Context, prompt string, model string) (*CacheEntry, float64, error) {
	// Normalize prompt if configured
	normalizedPrompt := prompt
	if c.config.NormalizePrompts {
		normalizedPrompt = normalizePrompt(prompt)
	}

	// Generate embedding for the prompt
	embedding, err := c.provider.Embed(ctx, normalizedPrompt)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to generate embedding: %w", err)
	}

	c.mu.RLock()
	entries := c.getEntriesForModel(model)
	c.mu.RUnlock()

	if len(entries) == 0 {
		atomic.AddInt64(&c.misses, 1)
		return nil, 0, nil
	}

	// Find most similar entry
	bestEntry, similarity, _ := FindMostSimilar(embedding, entries)

	if bestEntry == nil || similarity < c.config.SimilarityThreshold {
		atomic.AddInt64(&c.misses, 1)
		return nil, similarity, nil
	}

	// Update access time and hit count
	c.mu.Lock()
	if entry, ok := c.entries[bestEntry.Key]; ok {
		entry.AccessedAt = time.Now()
		entry.HitCount++
		c.updateAccessOrder(bestEntry.Key)
	}
	c.mu.Unlock()

	// Update statistics
	atomic.AddInt64(&c.hits, 1)
	atomic.AddInt64(&c.tokensSaved, int64(bestEntry.TokensUsed))

	c.mu.Lock()
	c.similaritySum += similarity
	c.similarityCount++
	c.mu.Unlock()

	return bestEntry, similarity, nil
}

// Set stores a response in the cache
func (c *MemorySemanticCache) Set(ctx context.Context, prompt string, response string, model string, tokensUsed int) error {
	// Normalize prompt if configured
	normalizedPrompt := prompt
	if c.config.NormalizePrompts {
		normalizedPrompt = normalizePrompt(prompt)
	}

	// Generate embedding
	embedding, err := c.provider.Embed(ctx, normalizedPrompt)
	if err != nil {
		return fmt.Errorf("failed to generate embedding: %w", err)
	}

	// Generate key
	key := generateCacheKey(normalizedPrompt, model)

	entry := &CacheEntry{
		Key:        key,
		Prompt:     prompt,
		Embedding:  embedding,
		Response:   response,
		Model:      model,
		TokensUsed: tokensUsed,
		CreatedAt:  time.Now(),
		AccessedAt: time.Now(),
		HitCount:   0,
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	// Check if we need to evict
	if len(c.entries) >= c.config.MaxEntries {
		c.evict()
	}

	// Store entry
	c.entries[key] = entry
	c.accessOrder = append(c.accessOrder, key)

	return nil
}

// Delete removes an entry from cache
func (c *MemorySemanticCache) Delete(ctx context.Context, key string) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	delete(c.entries, key)
	c.removeFromAccessOrder(key)

	return nil
}

// Clear removes all entries from cache
func (c *MemorySemanticCache) Clear(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.entries = make(map[string]*CacheEntry)
	c.accessOrder = make([]string, 0)

	return nil
}

// Stats returns cache statistics
func (c *MemorySemanticCache) Stats() *CacheStats {
	c.mu.RLock()
	defer c.mu.RUnlock()

	hits := atomic.LoadInt64(&c.hits)
	misses := atomic.LoadInt64(&c.misses)
	total := hits + misses

	var hitRate float64
	if total > 0 {
		hitRate = float64(hits) / float64(total)
	}

	var avgSimilarity float64
	if c.similarityCount > 0 {
		avgSimilarity = c.similaritySum / float64(c.similarityCount)
	}

	// Estimate memory usage (rough approximation)
	var memoryUsed int64
	for _, entry := range c.entries {
		memoryUsed += int64(len(entry.Prompt) + len(entry.Response))
		memoryUsed += int64(len(entry.Embedding) * 4) // float32 = 4 bytes
		memoryUsed += 200                             // overhead for other fields
	}

	return &CacheStats{
		TotalEntries:      int64(len(c.entries)),
		TotalHits:         hits,
		TotalMisses:       misses,
		HitRate:           hitRate,
		AverageSimilarity: avgSimilarity,
		TokensSaved:       atomic.LoadInt64(&c.tokensSaved),
		LatencySaved:      hits * 2000, // Assume 2s saved per hit
		MemoryUsed:        memoryUsed,
	}
}

// Close closes the cache and releases resources
func (c *MemorySemanticCache) Close() error {
	close(c.done)
	return nil
}

// getEntriesForModel returns entries for a specific model
func (c *MemorySemanticCache) getEntriesForModel(model string) []*CacheEntry {
	var entries []*CacheEntry

	for _, entry := range c.entries {
		// Filter by model if model-specific caching is enabled
		if c.config.ModelSpecific && entry.Model != model {
			continue
		}
		// Skip expired entries
		if time.Since(entry.CreatedAt) > c.config.TTL {
			continue
		}
		entries = append(entries, entry)
	}

	return entries
}

// evict removes entries based on eviction policy
func (c *MemorySemanticCache) evict() {
	if len(c.accessOrder) == 0 {
		return
	}

	switch c.config.EvictionPolicy {
	case "lru":
		// Remove least recently used
		key := c.accessOrder[0]
		delete(c.entries, key)
		c.accessOrder = c.accessOrder[1:]

	case "lfu":
		// Remove least frequently used
		var minKey string
		var minHits int64 = -1

		for key, entry := range c.entries {
			if minHits == -1 || entry.HitCount < minHits {
				minHits = entry.HitCount
				minKey = key
			}
		}

		if minKey != "" {
			delete(c.entries, minKey)
			c.removeFromAccessOrder(minKey)
		}

	case "fifo":
		// Remove oldest
		var oldestKey string
		var oldestTime time.Time

		for key, entry := range c.entries {
			if oldestKey == "" || entry.CreatedAt.Before(oldestTime) {
				oldestTime = entry.CreatedAt
				oldestKey = key
			}
		}

		if oldestKey != "" {
			delete(c.entries, oldestKey)
			c.removeFromAccessOrder(oldestKey)
		}

	default:
		// Default to LRU
		if len(c.accessOrder) > 0 {
			key := c.accessOrder[0]
			delete(c.entries, key)
			c.accessOrder = c.accessOrder[1:]
		}
	}
}

// updateAccessOrder moves a key to the end (most recently used)
func (c *MemorySemanticCache) updateAccessOrder(key string) {
	c.removeFromAccessOrder(key)
	c.accessOrder = append(c.accessOrder, key)
}

// removeFromAccessOrder removes a key from the access order list
func (c *MemorySemanticCache) removeFromAccessOrder(key string) {
	for i, k := range c.accessOrder {
		if k == key {
			c.accessOrder = append(c.accessOrder[:i], c.accessOrder[i+1:]...)
			return
		}
	}
}

// cleanupLoop periodically removes expired entries
func (c *MemorySemanticCache) cleanupLoop() {
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			c.cleanup()
		case <-c.done:
			return
		}
	}
}

// cleanup removes expired entries
func (c *MemorySemanticCache) cleanup() {
	c.mu.Lock()
	defer c.mu.Unlock()

	now := time.Now()
	for key, entry := range c.entries {
		if now.Sub(entry.CreatedAt) > c.config.TTL {
			delete(c.entries, key)
			c.removeFromAccessOrder(key)
		}
	}
}

// generateCacheKey generates a unique key for a prompt and model
func generateCacheKey(prompt string, model string) string {
	data := prompt + "|" + model
	hash := sha256.Sum256([]byte(data))
	return hex.EncodeToString(hash[:])
}

// normalizePrompt normalizes a prompt for better cache matching
func normalizePrompt(prompt string) string {
	// Convert to lowercase
	normalized := strings.ToLower(prompt)

	// Remove extra whitespace
	spaceRegex := regexp.MustCompile(`\s+`)
	normalized = spaceRegex.ReplaceAllString(normalized, " ")

	// Trim
	normalized = strings.TrimSpace(normalized)

	return normalized
}
