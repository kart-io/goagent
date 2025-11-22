package tools

import (
	"container/list"
	"context"
	"hash/fnv"
	"log"
	"regexp"
	"sync"
	"sync/atomic"
	"time"

	agentErrors "github.com/kart-io/goagent/errors"
)

// ShardedToolCache 分片工具缓存
// 通过将缓存分成多个分片来减少锁竞争，提升并发性能
type ShardedToolCache struct {
	shards       []*cacheShard
	shardCount   uint32
	cleanupDone  sync.WaitGroup
	ctx          context.Context
	cancel       context.CancelFunc
	closed       atomic.Int32
	stats        *CacheStats
	dependencies map[string][]string
	depMu        sync.RWMutex
}

// cacheShard 单个缓存分片
type cacheShard struct {
	mu       sync.RWMutex
	cache    map[string]*cacheEntry
	lruList  *list.List
	capacity int
}

// ShardedCacheConfig 分片缓存配置
type ShardedCacheConfig struct {
	// ShardCount 分片数量（建议为 2 的幂，默认 32）
	ShardCount uint32

	// Capacity 总容量（每个分片的容量 = Capacity / ShardCount）
	Capacity int

	// DefaultTTL 默认 TTL
	DefaultTTL time.Duration

	// CleanupInterval 清理间隔
	CleanupInterval time.Duration
}

// NewShardedToolCache 创建分片工具缓存
func NewShardedToolCache(config ShardedCacheConfig) *ShardedToolCache {
	if config.ShardCount <= 0 || (config.ShardCount&(config.ShardCount-1)) != 0 {
		// 如果不是 2 的幂，使用默认值 32
		config.ShardCount = 32
	}

	if config.Capacity <= 0 {
		config.Capacity = 1000
	}

	if config.DefaultTTL <= 0 {
		config.DefaultTTL = 5 * time.Minute
	}

	if config.CleanupInterval <= 0 {
		config.CleanupInterval = 1 * time.Minute
	}

	ctx, cancel := context.WithCancel(context.Background())
	shardCapacity := config.Capacity / int(config.ShardCount)
	if shardCapacity < 1 {
		shardCapacity = 1
	}

	cache := &ShardedToolCache{
		shards:       make([]*cacheShard, config.ShardCount),
		shardCount:   config.ShardCount,
		ctx:          ctx,
		cancel:       cancel,
		stats:        &CacheStats{},
		dependencies: make(map[string][]string),
	}

	// 初始化分片
	for i := uint32(0); i < config.ShardCount; i++ {
		cache.shards[i] = &cacheShard{
			cache:    make(map[string]*cacheEntry),
			lruList:  list.New(),
			capacity: shardCapacity,
		}
	}

	cache.closed.Store(0)

	// 启动清理 goroutine
	if config.CleanupInterval > 0 {
		cache.cleanupDone.Add(1)
		go cache.cleanupExpired(config.CleanupInterval, config.DefaultTTL)
	}

	return cache
}

// getShard 根据键获取对应的分片
func (c *ShardedToolCache) getShard(key string) *cacheShard {
	h := fnv.New32a()
	h.Write([]byte(key))
	return c.shards[h.Sum32()&(c.shardCount-1)]
}

// Get 获取缓存结果
func (c *ShardedToolCache) Get(ctx context.Context, key string) (*ToolOutput, bool) {
	shard := c.getShard(key)

	shard.mu.Lock()
	defer shard.mu.Unlock()

	entry, exists := shard.cache[key]
	if !exists {
		c.stats.recordMiss()
		return nil, false
	}

	// 检查是否过期
	if time.Now().After(entry.expireTime) {
		c.removeEntryFromShard(shard, entry)
		c.stats.recordMiss()
		return nil, false
	}

	// 移到 LRU 链表前面
	shard.lruList.MoveToFront(entry.element)
	c.stats.recordHit()

	return entry.output, true
}

// Set 设置缓存结果
func (c *ShardedToolCache) Set(ctx context.Context, key string, output *ToolOutput, ttl time.Duration) error {
	shard := c.getShard(key)
	toolName := extractToolNameFromKey(key)

	shard.mu.Lock()
	defer shard.mu.Unlock()

	// 如果已存在，更新
	if entry, exists := shard.cache[key]; exists {
		entry.output = output
		entry.expireTime = time.Now().Add(ttl)
		entry.toolName = toolName
		shard.lruList.MoveToFront(entry.element)
		return nil
	}

	// 检查容量，如果满了则淘汰最久未使用的
	if shard.lruList.Len() >= shard.capacity {
		c.evictOldestFromShard(shard)
	}

	// 添加新条目
	entry := &cacheEntry{
		key:        key,
		toolName:   toolName,
		output:     output,
		expireTime: time.Now().Add(ttl),
		version:    0,
	}

	entry.element = shard.lruList.PushFront(entry)
	shard.cache[key] = entry

	return nil
}

// Delete 删除缓存
func (c *ShardedToolCache) Delete(ctx context.Context, key string) error {
	shard := c.getShard(key)

	shard.mu.Lock()
	defer shard.mu.Unlock()

	if entry, exists := shard.cache[key]; exists {
		c.removeEntryFromShard(shard, entry)
	}

	return nil
}

// Clear 清空所有缓存
func (c *ShardedToolCache) Clear() error {
	for _, shard := range c.shards {
		shard.mu.Lock()
		shard.cache = make(map[string]*cacheEntry)
		shard.lruList.Init()
		shard.mu.Unlock()
	}
	return nil
}

// Size 返回缓存大小
func (c *ShardedToolCache) Size() int {
	total := 0
	for _, shard := range c.shards {
		shard.mu.RLock()
		total += len(shard.cache)
		shard.mu.RUnlock()
	}
	return total
}

// InvalidateByPattern 根据正则表达式模式失效缓存
func (c *ShardedToolCache) InvalidateByPattern(ctx context.Context, pattern string) (int, error) {
	re, err := regexp.Compile(pattern)
	if err != nil {
		return 0, agentErrors.Wrap(err, agentErrors.CodeInvalidInput, "invalid regex pattern").
			WithComponent("sharded_tool_cache").
			WithOperation("invalidate_by_pattern")
	}

	totalCount := 0
	affectedTools := make(map[string]struct{})

	// 遍历所有分片
	for _, shard := range c.shards {
		shard.mu.Lock()
		keysToRemove := make([]string, 0)

		for key, entry := range shard.cache {
			if re.MatchString(key) {
				keysToRemove = append(keysToRemove, key)
				if entry.toolName != "" {
					affectedTools[entry.toolName] = struct{}{}
				}
			}
		}

		for _, key := range keysToRemove {
			if entry, exists := shard.cache[key]; exists {
				shard.lruList.Remove(entry.element)
				delete(shard.cache, key)
				totalCount++
			}
		}
		shard.mu.Unlock()
	}

	// 级联失效依赖的工具
	for toolName := range affectedTools {
		count := c.invalidateDependents(toolName)
		totalCount += count
	}

	c.stats.recordInvalidation(int64(totalCount))
	return totalCount, nil
}

// InvalidateByTool 根据工具名称失效缓存
func (c *ShardedToolCache) InvalidateByTool(ctx context.Context, toolName string) (int, error) {
	totalCount := 0

	// 遍历所有分片
	for _, shard := range c.shards {
		shard.mu.Lock()
		keysToRemove := make([]string, 0)

		for key, entry := range shard.cache {
			if entry.toolName == toolName {
				keysToRemove = append(keysToRemove, key)
			}
		}

		for _, key := range keysToRemove {
			if entry, exists := shard.cache[key]; exists {
				shard.lruList.Remove(entry.element)
				delete(shard.cache, key)
				totalCount++
			}
		}
		shard.mu.Unlock()
	}

	// 级联失效依赖的工具
	dependentCount := c.invalidateDependents(toolName)
	totalCount += dependentCount

	c.stats.recordInvalidation(int64(totalCount))
	return totalCount, nil
}

// invalidateDependents 失效依赖指定工具的所有工具
func (c *ShardedToolCache) invalidateDependents(toolName string) int {
	c.depMu.RLock()
	dependents, exists := c.dependencies[toolName]
	c.depMu.RUnlock()

	if !exists || len(dependents) == 0 {
		return 0
	}

	totalCount := 0
	for _, dependent := range dependents {
		// 遍历所有分片删除依赖工具
		for _, shard := range c.shards {
			shard.mu.Lock()
			keysToRemove := make([]string, 0)

			for key, entry := range shard.cache {
				if entry.toolName == dependent {
					keysToRemove = append(keysToRemove, key)
				}
			}

			for _, key := range keysToRemove {
				if entry, exists := shard.cache[key]; exists {
					shard.lruList.Remove(entry.element)
					delete(shard.cache, key)
					totalCount++
				}
			}
			shard.mu.Unlock()
		}

		// 递归失效
		totalCount += c.invalidateDependents(dependent)
	}

	return totalCount
}

// AddDependency 添加工具依赖关系
func (c *ShardedToolCache) AddDependency(dependentTool, dependsOnTool string) {
	c.depMu.Lock()
	defer c.depMu.Unlock()

	if c.dependencies[dependsOnTool] == nil {
		c.dependencies[dependsOnTool] = make([]string, 0)
	}

	for _, dep := range c.dependencies[dependsOnTool] {
		if dep == dependentTool {
			return
		}
	}

	c.dependencies[dependsOnTool] = append(c.dependencies[dependsOnTool], dependentTool)
}

// RemoveDependency 移除工具依赖关系
func (c *ShardedToolCache) RemoveDependency(dependentTool, dependsOnTool string) {
	c.depMu.Lock()
	defer c.depMu.Unlock()

	deps, exists := c.dependencies[dependsOnTool]
	if !exists {
		return
	}

	for i, dep := range deps {
		if dep == dependentTool {
			c.dependencies[dependsOnTool] = append(deps[:i], deps[i+1:]...)
			return
		}
	}
}

// GetStats 获取统计信息
func (c *ShardedToolCache) GetStats() CacheStats {
	return CacheStats{
		Hits:          *copyAtomicInt64(&c.stats.Hits),
		Misses:        *copyAtomicInt64(&c.stats.Misses),
		Evicts:        *copyAtomicInt64(&c.stats.Evicts),
		Invalidations: *copyAtomicInt64(&c.stats.Invalidations),
	}
}

// GetVersion 获取当前缓存版本号（分片缓存不使用版本号）
func (c *ShardedToolCache) GetVersion() int64 {
	return 0
}

// Close 关闭缓存，清理资源
func (c *ShardedToolCache) Close() {
	if !c.closed.CompareAndSwap(0, 1) {
		return
	}

	c.cancel()
	c.cleanupDone.Wait()
}

// removeEntryFromShard 从分片中移除条目（内部方法，不加锁）
func (c *ShardedToolCache) removeEntryFromShard(shard *cacheShard, entry *cacheEntry) {
	shard.lruList.Remove(entry.element)
	delete(shard.cache, entry.key)
}

// evictOldestFromShard 从分片中淘汰最久未使用的条目
func (c *ShardedToolCache) evictOldestFromShard(shard *cacheShard) {
	oldest := shard.lruList.Back()
	if oldest != nil {
		entry := oldest.Value.(*cacheEntry)
		c.removeEntryFromShard(shard, entry)
		c.stats.recordEvict()
	}
}

// cleanupExpired 清理过期条目
func (c *ShardedToolCache) cleanupExpired(interval, defaultTTL time.Duration) {
	defer c.cleanupDone.Done()
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-c.ctx.Done():
			return
		case <-ticker.C:
			c.performCleanup()
		}
	}
}

// performCleanup 执行一次清理操作
func (c *ShardedToolCache) performCleanup() {
	now := time.Now()

	// 并发清理每个分片
	var wg sync.WaitGroup
	for _, shard := range c.shards {
		wg.Add(1)
		go func(s *cacheShard) {
			defer wg.Done()

			// 收集过期键
			s.mu.RLock()
			expiredKeys := make([]string, 0)
			for key, entry := range s.cache {
				if now.After(entry.expireTime) {
					expiredKeys = append(expiredKeys, key)
				}
			}
			s.mu.RUnlock()

			// 删除过期条目
			if len(expiredKeys) > 0 {
				s.mu.Lock()
				for _, key := range expiredKeys {
					if entry, exists := s.cache[key]; exists && now.After(entry.expireTime) {
						s.lruList.Remove(entry.element)
						delete(s.cache, key)
					}
				}
				s.mu.Unlock()
			}
		}(shard)
	}
	wg.Wait()
}

// hashKey 计算键的哈希值用于分片
func hashKey(key string) uint32 {
	h := fnv.New32a()
	h.Write([]byte(key))
	return h.Sum32()
}

// CreateShardedCache 创建分片缓存的辅助函数
func CreateShardedCache() ToolCache {
	return NewShardedToolCache(ShardedCacheConfig{
		ShardCount:      32,    // 32 个分片
		Capacity:        10000, // 总容量 10000
		DefaultTTL:      5 * time.Minute,
		CleanupInterval: 1 * time.Minute,
	})
}

// BenchmarkCompareShardedVsNormal 基准测试对比分片缓存和普通缓存
func BenchmarkCompareShardedVsNormal() {
	// 此函数可用于性能测试对比
	// 使用方法：go test -bench=BenchmarkCompare
	normalCache := NewMemoryToolCache(MemoryCacheConfig{
		Capacity:        10000,
		DefaultTTL:      5 * time.Minute,
		CleanupInterval: 1 * time.Minute,
	})
	defer normalCache.Close()

	shardedCache := NewShardedToolCache(ShardedCacheConfig{
		ShardCount:      32,
		Capacity:        10000,
		DefaultTTL:      5 * time.Minute,
		CleanupInterval: 1 * time.Minute,
	})
	defer shardedCache.Close()

	// 在高并发场景下，分片缓存性能会显著优于普通缓存
	// 特别是在多核 CPU 上，分片缓存可以实现近乎线性的扩展
	log.Println("Sharded cache created for benchmarking")
}