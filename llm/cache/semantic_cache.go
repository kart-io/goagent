package cache

import (
	"container/list"
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

// 预编译正则表达式，避免每次调用都重新编译
var spaceRegex = regexp.MustCompile(`\s+`)

// lruEntry 内部 LRU 条目，包含 CacheEntry 和链表元素指针
type lruEntry struct {
	key     string        // 缓存键
	entry   *CacheEntry   // 实际缓存数据
	element *list.Element // 链表元素指针
}

// MemorySemanticCache implements SemanticCache with in-memory storage
type MemorySemanticCache struct {
	config   *SemanticCacheConfig
	provider EmbeddingProvider

	// entries stores cache entries by key (map 用于 O(1) 查找)
	entries map[string]*lruEntry

	// lruList tracks LRU order (list 用于 O(1) LRU 操作)
	lruList *list.List

	mu sync.RWMutex

	// Statistics
	hits            int64
	misses          int64
	tokensSaved     int64
	similaritySum   float64
	similarityCount int64

	// done channel for cleanup goroutine
	done chan struct{}

	// closeOnce ensures Close is called only once
	closeOnce sync.Once
}

// NewMemorySemanticCache creates a new in-memory semantic cache
func NewMemorySemanticCache(provider EmbeddingProvider, config *SemanticCacheConfig) *MemorySemanticCache {
	if config == nil {
		config = DefaultSemanticCacheConfig()
	}

	cache := &MemorySemanticCache{
		config:   config,
		provider: provider,
		entries:  make(map[string]*lruEntry),
		lruList:  list.New(),
		done:     make(chan struct{}),
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

	// 在读锁内完成查找和数据拷贝，避免 TOCTOU 竞态条件
	c.mu.RLock()
	entries := c.getEntriesForModel(model)

	if len(entries) == 0 {
		c.mu.RUnlock()
		atomic.AddInt64(&c.misses, 1)
		return nil, 0, nil
	}

	// Find most similar entry (在读锁内执行，确保数据一致性)
	bestEntry, similarity, _ := FindMostSimilar(embedding, entries)

	if bestEntry == nil || similarity < c.config.SimilarityThreshold {
		c.mu.RUnlock()
		atomic.AddInt64(&c.misses, 1)
		return nil, similarity, nil
	}

	// 在读锁内拷贝数据，避免返回悬空指针
	// 二次验证条目仍然存在（防止在 FindMostSimilar 期间被删除）
	lruEnt, ok := c.entries[bestEntry.Key]
	if !ok {
		// 条目已被删除，视为缓存未命中
		c.mu.RUnlock()
		atomic.AddInt64(&c.misses, 1)
		return nil, 0, nil
	}

	entry := lruEnt.entry

	// 深拷贝缓存条目数据，避免返回可能被修改的指针
	entryCopy := &CacheEntry{
		Key:        entry.Key,
		Prompt:     entry.Prompt,
		Embedding:  entry.Embedding, // 切片共享底层数组，但 embedding 是只读的
		Response:   entry.Response,
		Model:      entry.Model,
		TokensUsed: entry.TokensUsed,
		CreatedAt:  entry.CreatedAt,
		AccessedAt: entry.AccessedAt,
		HitCount:   entry.HitCount,
	}
	c.mu.RUnlock()

	// 使用独立的写锁更新访问统计，不阻塞其他读操作
	c.mu.Lock()
	// 再次检查条目是否存在（在释放读锁到获取写锁之间可能被删除）
	if lruEnt, ok := c.entries[bestEntry.Key]; ok {
		lruEnt.entry.AccessedAt = time.Now()
		lruEnt.entry.HitCount++
		// 使用 O(1) 的 MoveToFront 更新 LRU 顺序
		c.lruList.MoveToFront(lruEnt.element)
	}
	c.mu.Unlock()

	// Update statistics
	atomic.AddInt64(&c.hits, 1)
	atomic.AddInt64(&c.tokensSaved, int64(entryCopy.TokensUsed))

	c.mu.Lock()
	c.similaritySum += similarity
	c.similarityCount++
	c.mu.Unlock()

	return entryCopy, similarity, nil
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

	// Store entry with LRU tracking
	lruEnt := &lruEntry{
		key:   key,
		entry: entry,
	}
	// 将新条目添加到链表前端（最近使用）
	lruEnt.element = c.lruList.PushFront(lruEnt)
	c.entries[key] = lruEnt

	return nil
}

// Delete removes an entry from cache
func (c *MemorySemanticCache) Delete(ctx context.Context, key string) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if lruEnt, ok := c.entries[key]; ok {
		// 从链表中移除
		c.lruList.Remove(lruEnt.element)
		// 从 map 中删除
		delete(c.entries, key)
	}

	return nil
}

// Clear removes all entries from cache
func (c *MemorySemanticCache) Clear(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.entries = make(map[string]*lruEntry)
	c.lruList.Init() // 重新初始化链表

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
	for _, lruEnt := range c.entries {
		entry := lruEnt.entry
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
	c.closeOnce.Do(func() {
		close(c.done)
	})
	return nil
}

// getEntriesForModel returns entries for a specific model
func (c *MemorySemanticCache) getEntriesForModel(model string) []*CacheEntry {
	var entries []*CacheEntry

	for _, lruEnt := range c.entries {
		entry := lruEnt.entry
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
	if c.lruList.Len() == 0 {
		return
	}

	switch c.config.EvictionPolicy {
	case "lru":
		// Remove least recently used (链表尾部)
		oldest := c.lruList.Back()
		if oldest != nil {
			lruEnt := oldest.Value.(*lruEntry)
			c.lruList.Remove(oldest)
			delete(c.entries, lruEnt.key)
		}

	case "lfu":
		// Remove least frequently used
		var minKey string
		var minHits int64 = -1

		for key, lruEnt := range c.entries {
			if minHits == -1 || lruEnt.entry.HitCount < minHits {
				minHits = lruEnt.entry.HitCount
				minKey = key
			}
		}

		if minKey != "" {
			if lruEnt, ok := c.entries[minKey]; ok {
				c.lruList.Remove(lruEnt.element)
				delete(c.entries, minKey)
			}
		}

	case "fifo":
		// Remove oldest (按创建时间)
		var oldestKey string
		var oldestTime time.Time

		for key, lruEnt := range c.entries {
			if oldestKey == "" || lruEnt.entry.CreatedAt.Before(oldestTime) {
				oldestTime = lruEnt.entry.CreatedAt
				oldestKey = key
			}
		}

		if oldestKey != "" {
			if lruEnt, ok := c.entries[oldestKey]; ok {
				c.lruList.Remove(lruEnt.element)
				delete(c.entries, oldestKey)
			}
		}

	default:
		// Default to LRU
		oldest := c.lruList.Back()
		if oldest != nil {
			lruEnt := oldest.Value.(*lruEntry)
			c.lruList.Remove(oldest)
			delete(c.entries, lruEnt.key)
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
	for key, lruEnt := range c.entries {
		if now.Sub(lruEnt.entry.CreatedAt) > c.config.TTL {
			c.lruList.Remove(lruEnt.element)
			delete(c.entries, key)
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

	// Remove extra whitespace (使用预编译的正则表达式)
	normalized = spaceRegex.ReplaceAllString(normalized, " ")

	// Trim
	normalized = strings.TrimSpace(normalized)

	return normalized
}
