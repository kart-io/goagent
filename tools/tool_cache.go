package tools

import (
	"container/list"
	"context"
	"crypto/sha256"
	"fmt"
	"sync"
	"time"

	agentErrors "github.com/kart-io/goagent/errors"
	"github.com/kart-io/goagent/utils/json"
)

// ToolCache 工具缓存接口
type ToolCache interface {
	// Get 获取缓存结果
	Get(ctx context.Context, key string) (*ToolOutput, bool)

	// Set 设置缓存结果
	Set(ctx context.Context, key string, output *ToolOutput, ttl time.Duration) error

	// Delete 删除缓存
	Delete(ctx context.Context, key string) error

	// Clear 清空所有缓存
	Clear() error

	// Size 返回缓存大小
	Size() int
}

// MemoryToolCache 内存工具缓存
//
// 线程安全的 LRU 缓存实现
type MemoryToolCache struct {
	// capacity 最大容量
	capacity int

	// cache 缓存映射
	cache map[string]*cacheEntry

	// lruList LRU 链表
	lruList *list.List

	// mu 读写锁
	mu sync.RWMutex

	// stats 统计信息
	stats *CacheStats

	// Lifecycle management
	stopCleanup chan struct{}
	cleanupDone sync.WaitGroup
}

// cacheEntry 缓存条目
type cacheEntry struct {
	key        string
	output     *ToolOutput
	expireTime time.Time
	element    *list.Element
}

// CacheStats 缓存统计信息
type CacheStats struct {
	Hits   int64
	Misses int64
	Evicts int64
	mu     sync.RWMutex
}

// MemoryCacheConfig 内存缓存配置
type MemoryCacheConfig struct {
	// Capacity 最大容量（条目数）
	Capacity int

	// DefaultTTL 默认 TTL
	DefaultTTL time.Duration

	// CleanupInterval 清理间隔
	CleanupInterval time.Duration
}

// NewMemoryToolCache 创建内存工具缓存
func NewMemoryToolCache(config MemoryCacheConfig) *MemoryToolCache {
	if config.Capacity <= 0 {
		config.Capacity = 1000
	}

	if config.DefaultTTL <= 0 {
		config.DefaultTTL = 5 * time.Minute
	}

	if config.CleanupInterval <= 0 {
		config.CleanupInterval = 1 * time.Minute
	}

	cache := &MemoryToolCache{
		capacity:    config.Capacity,
		cache:       make(map[string]*cacheEntry),
		lruList:     list.New(),
		stats:       &CacheStats{},
		stopCleanup: make(chan struct{}),
	}

	// 启动清理 goroutine with proper lifecycle management
	if config.CleanupInterval > 0 {
		cache.cleanupDone.Add(1)
		go cache.cleanupExpired(config.CleanupInterval)
	}

	return cache
}

// Get 获取缓存结果
func (c *MemoryToolCache) Get(ctx context.Context, key string) (*ToolOutput, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()

	entry, exists := c.cache[key]
	if !exists {
		c.stats.recordMiss()
		return nil, false
	}

	// 检查是否过期
	if time.Now().After(entry.expireTime) {
		c.removeEntry(entry)
		c.stats.recordMiss()
		return nil, false
	}

	// 移到 LRU 链表前面
	c.lruList.MoveToFront(entry.element)
	c.stats.recordHit()

	return entry.output, true
}

// Set 设置缓存结果
func (c *MemoryToolCache) Set(ctx context.Context, key string, output *ToolOutput, ttl time.Duration) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	// 如果已存在，更新
	if entry, exists := c.cache[key]; exists {
		entry.output = output
		entry.expireTime = time.Now().Add(ttl)
		c.lruList.MoveToFront(entry.element)
		return nil
	}

	// 检查容量，如果满了则淘汰最久未使用的
	if c.lruList.Len() >= c.capacity {
		c.evictOldest()
	}

	// 添加新条目
	entry := &cacheEntry{
		key:        key,
		output:     output,
		expireTime: time.Now().Add(ttl),
	}

	entry.element = c.lruList.PushFront(entry)
	c.cache[key] = entry

	return nil
}

// Delete 删除缓存
func (c *MemoryToolCache) Delete(ctx context.Context, key string) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if entry, exists := c.cache[key]; exists {
		c.removeEntry(entry)
	}

	return nil
}

// Clear 清空所有缓存
func (c *MemoryToolCache) Clear() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.cache = make(map[string]*cacheEntry)
	c.lruList.Init()

	return nil
}

// Size 返回缓存大小
func (c *MemoryToolCache) Size() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return len(c.cache)
}

// GetStats 获取统计信息
func (c *MemoryToolCache) GetStats() CacheStats {
	c.stats.mu.RLock()
	defer c.stats.mu.RUnlock()

	return CacheStats{
		Hits:   c.stats.Hits,
		Misses: c.stats.Misses,
		Evicts: c.stats.Evicts,
	}
}

// Close 关闭缓存，清理资源
func (c *MemoryToolCache) Close() {
	// Signal cleanup goroutine to stop
	close(c.stopCleanup)
	// Wait for cleanup goroutine to finish
	c.cleanupDone.Wait()
}

// removeEntry 移除条目（内部方法，不加锁）
func (c *MemoryToolCache) removeEntry(entry *cacheEntry) {
	c.lruList.Remove(entry.element)
	delete(c.cache, entry.key)
}

// evictOldest 淘汰最久未使用的条目
func (c *MemoryToolCache) evictOldest() {
	oldest := c.lruList.Back()
	if oldest != nil {
		entry := oldest.Value.(*cacheEntry)
		c.removeEntry(entry)
		c.stats.recordEvict()
	}
}

// cleanupExpired 清理过期条目
func (c *MemoryToolCache) cleanupExpired(interval time.Duration) {
	defer c.cleanupDone.Done()
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-c.stopCleanup:
			return // Clean shutdown
		case <-ticker.C:
			c.mu.Lock()
			now := time.Now()

			// 收集过期的 key
			expiredKeys := make([]string, 0)
			for key, entry := range c.cache {
				if now.After(entry.expireTime) {
					expiredKeys = append(expiredKeys, key)
				}
			}

			// 删除过期条目
			for _, key := range expiredKeys {
				if entry, exists := c.cache[key]; exists {
					c.removeEntry(entry)
				}
			}

			c.mu.Unlock()
		}
	}
}

// recordHit 记录命中
func (s *CacheStats) recordHit() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.Hits++
}

// recordMiss 记录未命中
func (s *CacheStats) recordMiss() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.Misses++
}

// recordEvict 记录淘汰
func (s *CacheStats) recordEvict() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.Evicts++
}

// HitRate 计算命中率
func (s *CacheStats) HitRate() float64 {
	s.mu.RLock()
	defer s.mu.RUnlock()

	total := s.Hits + s.Misses
	if total == 0 {
		return 0
	}

	return float64(s.Hits) / float64(total)
}

// CachedTool 带缓存的工具包装器
type CachedTool struct {
	tool  Tool
	cache ToolCache
	ttl   time.Duration
}

// NewCachedTool 创建带缓存的工具
func NewCachedTool(tool Tool, cache ToolCache, ttl time.Duration) *CachedTool {
	if ttl <= 0 {
		ttl = 5 * time.Minute
	}

	return &CachedTool{
		tool:  tool,
		cache: cache,
		ttl:   ttl,
	}
}

// Name 返回工具名称
func (c *CachedTool) Name() string {
	return c.tool.Name()
}

// Description 返回工具描述
func (c *CachedTool) Description() string {
	return c.tool.Description()
}

// ArgsSchema 返回参数 Schema
func (c *CachedTool) ArgsSchema() string {
	return c.tool.ArgsSchema()
}

// Invoke 执行工具（带缓存）
func (c *CachedTool) Invoke(ctx context.Context, input *ToolInput) (*ToolOutput, error) {
	// 生成缓存键
	cacheKey, err := c.generateCacheKey(input)
	if err != nil {
		// 如果生成缓存键失败，直接执行工具
		return c.tool.Invoke(ctx, input)
	}

	// 尝试从缓存获取
	if output, found := c.cache.Get(ctx, cacheKey); found {
		return output, nil
	}

	// 缓存未命中，执行工具
	output, err := c.tool.Invoke(ctx, input)
	if err != nil {
		return nil, err
	}

	// 存入缓存
	_ = c.cache.Set(ctx, cacheKey, output, c.ttl)

	return output, nil
}

// generateCacheKey 生成缓存键
func (c *CachedTool) generateCacheKey(input *ToolInput) (string, error) {
	// 序列化输入参数
	data, err := json.Marshal(input.Args)
	if err != nil {
		return "", err
	}

	// 使用 SHA256 生成哈希
	hash := sha256.Sum256(data)
	key := fmt.Sprintf("%s:%x", c.tool.Name(), hash)

	return key, nil
}

// InvalidateCacheByPrefix 根据前缀失效缓存
func (c *CachedTool) InvalidateCacheByPrefix(ctx context.Context, prefix string) error {
	// 注意：这需要缓存实现支持前缀查询
	// 当前简化实现不支持
	return agentErrors.New(agentErrors.CodeNotImplemented, "prefix invalidation not implemented").
		WithComponent("cached_tool").
		WithOperation("invalidate_cache_by_prefix")
}
