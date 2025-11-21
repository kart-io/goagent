package cache

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/kart-io/goagent/utils/json"
)

var (
	ErrCacheMiss     = errors.New("cache miss")
	ErrCacheInvalid  = errors.New("invalid cache entry")
	ErrCacheDisabled = errors.New("cache is disabled")
)

// Cache 定义缓存接口
//
// 借鉴 LangChain 的缓存设计，用于缓存 LLM 调用结果
// 减少 API 调用次数，降低成本和延迟
type Cache interface {
	// Get 获取缓存值
	Get(ctx context.Context, key string) (interface{}, error)

	// Set 设置缓存值
	Set(ctx context.Context, key string, value interface{}, ttl time.Duration) error

	// Delete 删除缓存值
	Delete(ctx context.Context, key string) error

	// Clear 清空所有缓存
	Clear(ctx context.Context) error

	// Has 检查键是否存在
	Has(ctx context.Context, key string) (bool, error)

	// GetStats 获取缓存统计信息
	GetStats() CacheStats
}

// CacheStats 缓存统计信息
type CacheStats struct {
	Hits      int64   // 命中次数
	Misses    int64   // 未命中次数
	Sets      int64   // 设置次数
	Deletes   int64   // 删除次数
	Evictions int64   // 驱逐次数
	Size      int64   // 当前大小
	MaxSize   int64   // 最大大小
	HitRate   float64 // 命中率
}

// CacheEntry 缓存条目
type CacheEntry struct {
	Key         string      // 键
	Value       interface{} // 值
	CreateTime  time.Time   // 创建时间
	ExpireTime  time.Time   // 过期时间
	AccessTime  time.Time   // 最后访问时间
	AccessCount int64       // 访问次数
}

// IsExpired 检查是否过期
func (e *CacheEntry) IsExpired() bool {
	return !e.ExpireTime.IsZero() && time.Now().After(e.ExpireTime)
}

// InMemoryCache 内存缓存实现
//
// 使用 sync.RWMutex + map 提供线程安全的内存缓存
// 相比 sync.Map，在读多写少且需要遍历的场景下性能更好
type InMemoryCache struct {
	entries         map[string]*CacheEntry // 缓存条目
	entriesMu       sync.RWMutex           // 条目读写锁
	hits            atomic.Int64           // 命中次数
	misses          atomic.Int64           // 未命中次数
	sets            atomic.Int64           // 设置次数
	deletes         atomic.Int64           // 删除次数
	evictions       atomic.Int64           // 驱逐次数
	maxSize         int                    // 最大条目数
	defaultTTL      time.Duration          // 默认 TTL
	cleanupInterval time.Duration          // 清理间隔
	stopCleanup     chan struct{}
	cleanupDone     sync.WaitGroup // Track cleanup goroutine
}

// NewInMemoryCache 创建内存缓存
func NewInMemoryCache(maxSize int, defaultTTL, cleanupInterval time.Duration) *InMemoryCache {
	cache := &InMemoryCache{
		entries:         make(map[string]*CacheEntry),
		maxSize:         maxSize,
		defaultTTL:      defaultTTL,
		cleanupInterval: cleanupInterval,
		stopCleanup:     make(chan struct{}),
	}

	// 启动定期清理
	if cleanupInterval > 0 {
		cache.cleanupDone.Add(1)
		go cache.cleanup()
	}

	return cache
}

// Get 获取缓存值
func (c *InMemoryCache) Get(ctx context.Context, key string) (interface{}, error) {
	c.entriesMu.RLock()
	entry, ok := c.entries[key]
	c.entriesMu.RUnlock()

	if !ok {
		c.misses.Add(1)
		return nil, ErrCacheMiss
	}

	// 检查是否过期
	if entry.IsExpired() {
		c.entriesMu.Lock()
		delete(c.entries, key)
		c.entriesMu.Unlock()
		c.misses.Add(1)
		c.evictions.Add(1)
		return nil, ErrCacheMiss
	}

	// 更新访问信息 (Note: this is a race, but acceptable for cache stats)
	entry.AccessTime = time.Now()
	entry.AccessCount++

	c.hits.Add(1)
	return entry.Value, nil
}

// Set 设置缓存值
func (c *InMemoryCache) Set(ctx context.Context, key string, value interface{}, ttl time.Duration) error {
	// 检查大小限制
	if c.maxSize > 0 {
		c.entriesMu.RLock()
		size := len(c.entries)
		c.entriesMu.RUnlock()

		if size >= c.maxSize {
			// 驱逐最旧的条目
			c.evictOldest()
		}
	}

	// 使用默认 TTL
	if ttl == 0 {
		ttl = c.defaultTTL
	}

	now := time.Now()
	entry := &CacheEntry{
		Key:         key,
		Value:       value,
		CreateTime:  now,
		AccessTime:  now,
		AccessCount: 0,
	}

	if ttl > 0 {
		entry.ExpireTime = now.Add(ttl)
	}

	c.entriesMu.Lock()
	c.entries[key] = entry
	c.entriesMu.Unlock()
	c.sets.Add(1)

	return nil
}

// Delete 删除缓存值
func (c *InMemoryCache) Delete(ctx context.Context, key string) error {
	c.entriesMu.Lock()
	delete(c.entries, key)
	c.entriesMu.Unlock()
	c.deletes.Add(1)
	return nil
}

// Clear 清空所有缓存
func (c *InMemoryCache) Clear(ctx context.Context) error {
	c.entriesMu.Lock()
	c.entries = make(map[string]*CacheEntry)
	c.entriesMu.Unlock()

	// Reset stats
	c.hits.Store(0)
	c.misses.Store(0)
	c.sets.Store(0)
	c.deletes.Store(0)
	c.evictions.Store(0)

	return nil
}

// Has 检查键是否存在
func (c *InMemoryCache) Has(ctx context.Context, key string) (bool, error) {
	c.entriesMu.RLock()
	_, ok := c.entries[key]
	c.entriesMu.RUnlock()
	return ok, nil
}

// GetStats 获取统计信息
func (c *InMemoryCache) GetStats() CacheStats {
	c.entriesMu.RLock()
	size := int64(len(c.entries))
	c.entriesMu.RUnlock()

	hits := c.hits.Load()
	misses := c.misses.Load()

	stats := CacheStats{
		Hits:      hits,
		Misses:    misses,
		Sets:      c.sets.Load(),
		Deletes:   c.deletes.Load(),
		Evictions: c.evictions.Load(),
		Size:      size,
		MaxSize:   int64(c.maxSize),
	}

	// 计算命中率
	total := hits + misses
	if total > 0 {
		stats.HitRate = float64(hits) / float64(total)
	}

	return stats
}

// evictOldest 驱逐最旧的条目
func (c *InMemoryCache) evictOldest() {
	c.entriesMu.Lock()
	defer c.entriesMu.Unlock()

	var oldestKey string
	var oldestTime time.Time

	for key, entry := range c.entries {
		if oldestTime.IsZero() || entry.CreateTime.Before(oldestTime) {
			oldestKey = key
			oldestTime = entry.CreateTime
		}
	}

	if oldestKey != "" {
		delete(c.entries, oldestKey)
		c.evictions.Add(1)
	}
}

// cleanup 定期清理过期条目
func (c *InMemoryCache) cleanup() {
	defer c.cleanupDone.Done()
	ticker := time.NewTicker(c.cleanupInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			c.cleanupExpired()
		case <-c.stopCleanup:
			return
		}
	}
}

// cleanupExpired 清理过期条目
//
// Optimized single-pass cleanup that deletes expired entries directly
// during iteration, avoiding intermediate slice allocation.
func (c *InMemoryCache) cleanupExpired() {
	c.entriesMu.Lock()
	defer c.entriesMu.Unlock()

	var evictionCount int64
	for key, entry := range c.entries {
		if entry.IsExpired() {
			delete(c.entries, key)
			evictionCount++
		}
	}

	// Batch update eviction stats
	if evictionCount > 0 {
		c.evictions.Add(evictionCount)
	}
}

// Close 关闭缓存
func (c *InMemoryCache) Close() {
	// Signal cleanup to stop
	if c.cleanupInterval > 0 {
		close(c.stopCleanup)
		// Wait for cleanup goroutine to finish
		c.cleanupDone.Wait()
	}
}

// LRUCache LRU (Least Recently Used) 缓存
//
// 当缓存满时，驱逐最近最少使用的条目
type LRUCache struct {
	*InMemoryCache
}

// NewLRUCache 创建 LRU 缓存
func NewLRUCache(maxSize int, defaultTTL, cleanupInterval time.Duration) *LRUCache {
	return &LRUCache{
		InMemoryCache: NewInMemoryCache(maxSize, defaultTTL, cleanupInterval),
	}
}

// evictOldest 驱逐最近最少使用的条目
//
//nolint:unused // Reserved for future LRU eviction strategy
func (c *LRUCache) evictOldest() {
	c.entriesMu.Lock()
	defer c.entriesMu.Unlock()

	var lruKey string
	var lruTime time.Time

	for key, entry := range c.entries {
		if lruTime.IsZero() || entry.AccessTime.Before(lruTime) {
			lruKey = key
			lruTime = entry.AccessTime
		}
	}

	if lruKey != "" {
		delete(c.entries, lruKey)
		c.evictions.Add(1)
	}
}

// MultiTierCache 多级缓存
//
// 支持多个缓存层，如 L1 内存 + L2 Redis
type MultiTierCache struct {
	tiers []Cache
}

// NewMultiTierCache 创建多级缓存
func NewMultiTierCache(tiers ...Cache) *MultiTierCache {
	return &MultiTierCache{
		tiers: tiers,
	}
}

// Get 从各级缓存获取
func (c *MultiTierCache) Get(ctx context.Context, key string) (interface{}, error) {
	for i, tier := range c.tiers {
		value, err := tier.Get(ctx, key)
		if err == nil {
			// 回填到更高层级
			for j := 0; j < i; j++ {
				if err := c.tiers[j].Set(ctx, key, value, 0); err != nil {
					// 缓存回填失败不影响业务，但记录日志便于调试
					fmt.Fprintf(os.Stderr, "[WARN] cache tier %d backfill failed (key=%s): %v\n", j, key, err)
				}
			}
			return value, nil
		}
		// Log tier failures at WARN level for debugging cascading failures
		// Skip logging for simple cache misses as they are expected
		if !errors.Is(err, ErrCacheMiss) && !errors.Is(err, ErrCacheDisabled) {
			fmt.Fprintf(os.Stderr, "[WARN] cache tier %d get failed (key=%s): %v\n", i, key, err)
		}
	}

	return nil, ErrCacheMiss
}

// Set 设置到所有层级
func (c *MultiTierCache) Set(ctx context.Context, key string, value interface{}, ttl time.Duration) error {
	var lastErr error
	for _, tier := range c.tiers {
		if err := tier.Set(ctx, key, value, ttl); err != nil {
			lastErr = err
		}
	}
	return lastErr
}

// Delete 从所有层级删除
func (c *MultiTierCache) Delete(ctx context.Context, key string) error {
	for i, tier := range c.tiers {
		if err := tier.Delete(ctx, key); err != nil {
			// 缓存删除失败，记录但继续
			fmt.Fprintf(os.Stderr, "[WARN] cache tier %d delete failed (key=%s): %v\n", i, key, err)
		}
	}
	return nil
}

// Clear 清空所有层级
func (c *MultiTierCache) Clear(ctx context.Context) error {
	for i, tier := range c.tiers {
		if err := tier.Clear(ctx); err != nil {
			// 缓存清空失败，记录但继续
			fmt.Fprintf(os.Stderr, "[WARN] cache tier %d clear failed: %v\n", i, err)
		}
	}
	return nil
}

// Has 检查键是否存在于任何层级
func (c *MultiTierCache) Has(ctx context.Context, key string) (bool, error) {
	for _, tier := range c.tiers {
		if has, _ := tier.Has(ctx, key); has {
			return true, nil
		}
	}
	return false, nil
}

// GetStats 获取第一层的统计信息
func (c *MultiTierCache) GetStats() CacheStats {
	if len(c.tiers) > 0 {
		return c.tiers[0].GetStats()
	}
	return CacheStats{}
}

// CacheKeyGenerator 缓存键生成器
type CacheKeyGenerator struct {
	prefix string
}

// NewCacheKeyGenerator 创建键生成器
func NewCacheKeyGenerator(prefix string) *CacheKeyGenerator {
	return &CacheKeyGenerator{
		prefix: prefix,
	}
}

// GenerateKey 生成缓存键
//
// 根据提示和参数生成唯一的缓存键
func (g *CacheKeyGenerator) GenerateKey(prompt string, params map[string]interface{}) string {
	// 将参数序列化为 JSON
	paramsJSON, _ := json.Marshal(params)

	// 组合提示和参数
	combined := fmt.Sprintf("%s|%s", prompt, paramsJSON)

	// 使用 SHA256 生成哈希
	hash := sha256.Sum256([]byte(combined))
	hashStr := hex.EncodeToString(hash[:])

	if g.prefix != "" {
		return fmt.Sprintf("%s:%s", g.prefix, hashStr)
	}

	return hashStr
}

// GenerateKeySimple 生成简单的缓存键
//
// Optimized to use strings.Builder for efficient string concatenation
// and avoid multiple allocations in the loop.
func (g *CacheKeyGenerator) GenerateKeySimple(parts ...string) string {
	// Pre-calculate total length for efficient allocation
	totalLen := 0
	for _, part := range parts {
		totalLen += len(part) + 1 // +1 for separator
	}

	var builder strings.Builder
	builder.Grow(totalLen)

	for _, part := range parts {
		builder.WriteString(part)
		builder.WriteByte('|')
	}

	hash := sha256.Sum256([]byte(builder.String()))
	hashStr := hex.EncodeToString(hash[:])

	if g.prefix != "" {
		// Use strings.Builder for final concatenation
		var result strings.Builder
		result.Grow(len(g.prefix) + 1 + len(hashStr))
		result.WriteString(g.prefix)
		result.WriteByte(':')
		result.WriteString(hashStr)
		return result.String()
	}

	return hashStr
}

// NoOpCache 无操作缓存
//
// 用于禁用缓存的场景
type NoOpCache struct{}

// NewNoOpCache 创建无操作缓存
func NewNoOpCache() *NoOpCache {
	return &NoOpCache{}
}

func (c *NoOpCache) Get(ctx context.Context, key string) (interface{}, error) {
	return nil, ErrCacheDisabled
}

func (c *NoOpCache) Set(ctx context.Context, key string, value interface{}, ttl time.Duration) error {
	return ErrCacheDisabled
}

func (c *NoOpCache) Delete(ctx context.Context, key string) error {
	return ErrCacheDisabled
}

func (c *NoOpCache) Clear(ctx context.Context) error {
	return ErrCacheDisabled
}

func (c *NoOpCache) Has(ctx context.Context, key string) (bool, error) {
	return false, ErrCacheDisabled
}

func (c *NoOpCache) GetStats() CacheStats {
	return CacheStats{}
}

// CacheConfig 缓存配置
type CacheConfig struct {
	Enabled         bool          // 是否启用缓存
	Type            string        // 缓存类型: "memory", "redis", "multi-tier"
	MaxSize         int           // 最大条目数
	DefaultTTL      time.Duration // 默认 TTL
	CleanupInterval time.Duration // 清理间隔
}

// DefaultCacheConfig 返回默认配置
func DefaultCacheConfig() CacheConfig {
	return CacheConfig{
		Enabled:         true,
		Type:            "memory",
		MaxSize:         1000,
		DefaultTTL:      5 * time.Minute,
		CleanupInterval: 1 * time.Minute,
	}
}

// NewCacheFromConfig 根据配置创建缓存
func NewCacheFromConfig(config CacheConfig) Cache {
	if !config.Enabled {
		return NewNoOpCache()
	}

	switch config.Type {
	case "lru":
		return NewLRUCache(config.MaxSize, config.DefaultTTL, config.CleanupInterval)
	case "memory":
		return NewInMemoryCache(config.MaxSize, config.DefaultTTL, config.CleanupInterval)
	default:
		return NewInMemoryCache(config.MaxSize, config.DefaultTTL, config.CleanupInterval)
	}
}
