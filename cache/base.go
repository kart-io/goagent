package cache

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"sync"
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
// 使用 sync.Map 提供线程安全的内存缓存
type InMemoryCache struct {
	entries         sync.Map      // 缓存条目 map[string]*CacheEntry
	stats           CacheStats    // 统计信息
	statsMu         sync.RWMutex  // 统计锁
	maxSize         int           // 最大条目数
	defaultTTL      time.Duration // 默认 TTL
	cleanupInterval time.Duration // 清理间隔
	stopCleanup     chan struct{}
	cleanupDone     sync.WaitGroup // Track cleanup goroutine
}

// NewInMemoryCache 创建内存缓存
func NewInMemoryCache(maxSize int, defaultTTL, cleanupInterval time.Duration) *InMemoryCache {
	cache := &InMemoryCache{
		maxSize:         maxSize,
		defaultTTL:      defaultTTL,
		cleanupInterval: cleanupInterval,
		stopCleanup:     make(chan struct{}),
	}

	cache.stats.MaxSize = int64(maxSize)

	// 启动定期清理
	if cleanupInterval > 0 {
		cache.cleanupDone.Add(1)
		go cache.cleanup()
	}

	return cache
}

// Get 获取缓存值
func (c *InMemoryCache) Get(ctx context.Context, key string) (interface{}, error) {
	value, ok := c.entries.Load(key)
	if !ok {
		c.incrementMiss()
		return nil, ErrCacheMiss
	}

	entry := value.(*CacheEntry)

	// 检查是否过期
	if entry.IsExpired() {
		c.entries.Delete(key)
		c.incrementMiss()
		c.incrementEviction()
		return nil, ErrCacheMiss
	}

	// 更新访问信息
	entry.AccessTime = time.Now()
	entry.AccessCount++

	c.incrementHit()
	return entry.Value, nil
}

// Set 设置缓存值
func (c *InMemoryCache) Set(ctx context.Context, key string, value interface{}, ttl time.Duration) error {
	// 检查大小限制
	if c.maxSize > 0 {
		size := c.size()
		if size >= int64(c.maxSize) {
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

	c.entries.Store(key, entry)
	c.incrementSet()

	return nil
}

// Delete 删除缓存值
func (c *InMemoryCache) Delete(ctx context.Context, key string) error {
	c.entries.Delete(key)
	c.incrementDelete()
	return nil
}

// Clear 清空所有缓存
func (c *InMemoryCache) Clear(ctx context.Context) error {
	c.entries.Range(func(key, value interface{}) bool {
		c.entries.Delete(key)
		return true
	})

	c.statsMu.Lock()
	c.stats = CacheStats{MaxSize: c.stats.MaxSize}
	c.statsMu.Unlock()

	return nil
}

// Has 检查键是否存在
func (c *InMemoryCache) Has(ctx context.Context, key string) (bool, error) {
	_, ok := c.entries.Load(key)
	return ok, nil
}

// GetStats 获取统计信息
func (c *InMemoryCache) GetStats() CacheStats {
	c.statsMu.RLock()
	defer c.statsMu.RUnlock()

	stats := c.stats
	stats.Size = c.size()

	// 计算命中率
	total := stats.Hits + stats.Misses
	if total > 0 {
		stats.HitRate = float64(stats.Hits) / float64(total)
	}

	return stats
}

// size 获取当前大小
func (c *InMemoryCache) size() int64 {
	var count int64
	c.entries.Range(func(key, value interface{}) bool {
		count++
		return true
	})
	return count
}

// evictOldest 驱逐最旧的条目
func (c *InMemoryCache) evictOldest() {
	var oldestKey string
	var oldestTime time.Time

	c.entries.Range(func(key, value interface{}) bool {
		entry := value.(*CacheEntry)
		if oldestTime.IsZero() || entry.CreateTime.Before(oldestTime) {
			oldestKey = key.(string)
			oldestTime = entry.CreateTime
		}
		return true
	})

	if oldestKey != "" {
		c.entries.Delete(oldestKey)
		c.incrementEviction()
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
func (c *InMemoryCache) cleanupExpired() {
	keysToDelete := []string{}

	c.entries.Range(func(key, value interface{}) bool {
		entry := value.(*CacheEntry)
		if entry.IsExpired() {
			keysToDelete = append(keysToDelete, key.(string))
		}
		return true
	})

	for _, key := range keysToDelete {
		c.entries.Delete(key)
		c.incrementEviction()
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

// 统计辅助方法
func (c *InMemoryCache) incrementHit() {
	c.statsMu.Lock()
	c.stats.Hits++
	c.statsMu.Unlock()
}

func (c *InMemoryCache) incrementMiss() {
	c.statsMu.Lock()
	c.stats.Misses++
	c.statsMu.Unlock()
}

func (c *InMemoryCache) incrementSet() {
	c.statsMu.Lock()
	c.stats.Sets++
	c.statsMu.Unlock()
}

func (c *InMemoryCache) incrementDelete() {
	c.statsMu.Lock()
	c.stats.Deletes++
	c.statsMu.Unlock()
}

func (c *InMemoryCache) incrementEviction() {
	c.statsMu.Lock()
	c.stats.Evictions++
	c.statsMu.Unlock()
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
	var lruKey string
	var lruTime time.Time

	c.entries.Range(func(key, value interface{}) bool {
		entry := value.(*CacheEntry)
		if lruTime.IsZero() || entry.AccessTime.Before(lruTime) {
			lruKey = key.(string)
			lruTime = entry.AccessTime
		}
		return true
	})

	if lruKey != "" {
		c.entries.Delete(lruKey)
		c.incrementEviction()
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
					fmt.Fprintf(os.Stderr, "cache tier %d backfill failed (key=%s): %v\n", j, key, err)
				}
			}
			return value, nil
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
			fmt.Fprintf(os.Stderr, "cache tier %d delete failed (key=%s): %v\n", i, key, err)
		}
	}
	return nil
}

// Clear 清空所有层级
func (c *MultiTierCache) Clear(ctx context.Context) error {
	for i, tier := range c.tiers {
		if err := tier.Clear(ctx); err != nil {
			// 缓存清空失败，记录但继续
			fmt.Fprintf(os.Stderr, "cache tier %d clear failed: %v\n", i, err)
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
func (g *CacheKeyGenerator) GenerateKeySimple(parts ...string) string {
	combined := ""
	for _, part := range parts {
		combined += part + "|"
	}

	hash := sha256.Sum256([]byte(combined))
	hashStr := hex.EncodeToString(hash[:])

	if g.prefix != "" {
		return fmt.Sprintf("%s:%s", g.prefix, hashStr)
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
