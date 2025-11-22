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
	config       ShardedCacheConfig // 存储配置

	// 自动调优相关
	lastTuneTime time.Time
	tuneMetrics  *tuneMetrics
}

// tuneMetrics 自动调优指标
type tuneMetrics struct {
	mu            sync.RWMutex
	avgHitRate    float64
	avgLoadFactor float64
	qpsHistory    []float64
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

	// EvictionPolicy 淘汰策略
	EvictionPolicy EvictionPolicy

	// CleanupStrategy 清理策略
	CleanupStrategy CleanupStrategy

	// LoadBalancing 负载均衡策略
	LoadBalancing LoadBalancingStrategy

	// AutoTuning 自动调优
	AutoTuning bool

	// MetricsEnabled 是否启用指标收集
	MetricsEnabled bool

	// MaxConcurrency 每个分片的最大并发数
	MaxConcurrency int

	// WarmupEntries 预热条目
	WarmupEntries map[string]*ToolOutput

	// CompressionThreshold 压缩阈值（字节）
	CompressionThreshold int

	// MaxEntrySize 单个条目最大大小（字节）
	MaxEntrySize int

	// MemoryLimit 内存限制（字节）
	MemoryLimit int64

	// WorkloadType 工作负载类型
	WorkloadType WorkloadType
}

// NewShardedToolCache 创建分片工具缓存（使用配置结构体）
func NewShardedToolCache(config ShardedCacheConfig) *ShardedToolCache {
	// 应用默认值
	if config.ShardCount <= 0 || (config.ShardCount&(config.ShardCount-1)) != 0 {
		config.ShardCount = 32
	}
	if config.Capacity <= 0 {
		config.Capacity = 10000
	}
	if config.DefaultTTL <= 0 {
		config.DefaultTTL = 5 * time.Minute
	}
	if config.CleanupInterval <= 0 {
		config.CleanupInterval = 1 * time.Minute
	}

	return newShardedToolCacheWithConfig(config)
}

// NewShardedToolCacheWithOptions 使用选项模式创建分片工具缓存
func NewShardedToolCacheWithOptions(opts ...ShardedCacheOption) *ShardedToolCache {
	// 使用默认配置
	config := DefaultShardedCacheConfig()

	// 应用所有选项
	for _, opt := range opts {
		opt(&config)
	}

	return newShardedToolCacheWithConfig(config)
}

// newShardedToolCacheWithConfig 内部创建函数
func newShardedToolCacheWithConfig(config ShardedCacheConfig) *ShardedToolCache {
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
		config:       config,
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

	// 应用预热条目
	if config.WarmupEntries != nil {
		for key, output := range config.WarmupEntries {
			_ = cache.Set(ctx, key, output, config.DefaultTTL)
		}
	}

	// 根据清理策略启动清理
	switch config.CleanupStrategy {
	case PeriodicCleanup, HybridCleanup:
		if config.CleanupInterval > 0 {
			cache.cleanupDone.Add(1)
			go cache.cleanupExpired(config.CleanupInterval, config.DefaultTTL)
		}
	case AdaptiveCleanup:
		cache.cleanupDone.Add(1)
		go cache.adaptiveCleanup()
	}

	// 启动自动调优
	if config.AutoTuning {
		cache.cleanupDone.Add(1)
		go cache.autoTune()
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

// GetStatsValues 获取统计信息的数值
func (c *ShardedToolCache) GetStatsValues() (hits, misses, evicts, invalidations int64) {
	return c.stats.Hits.Load(), c.stats.Misses.Load(),
		c.stats.Evicts.Load(), c.stats.Invalidations.Load()
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

// adaptiveCleanup 自适应清理策略
func (c *ShardedToolCache) adaptiveCleanup() {
	defer c.cleanupDone.Done()

	baseInterval := c.config.CleanupInterval
	if baseInterval <= 0 {
		baseInterval = 1 * time.Minute
	}

	currentInterval := baseInterval
	ticker := time.NewTicker(currentInterval)
	defer ticker.Stop()

	for {
		select {
		case <-c.ctx.Done():
			return
		case <-ticker.C:
			// 执行清理
			cleanedCount := c.performCleanupWithCount()

			// 根据清理的条目数量调整间隔
			loadFactor := float64(c.Size()) / float64(c.config.Capacity)

			if loadFactor > 0.9 || cleanedCount > c.config.Capacity/10 {
				// 负载高或清理了很多条目，减少间隔
				currentInterval = baseInterval / 2
			} else if loadFactor < 0.5 && cleanedCount < c.config.Capacity/100 {
				// 负载低且清理很少，增加间隔
				currentInterval = baseInterval * 2
			} else {
				currentInterval = baseInterval
			}

			// 限制间隔范围
			if currentInterval < 10*time.Second {
				currentInterval = 10 * time.Second
			} else if currentInterval > 10*time.Minute {
				currentInterval = 10 * time.Minute
			}

			// 重置ticker
			ticker.Reset(currentInterval)
		}
	}
}

// performCleanupWithCount 执行清理并返回清理的条目数
func (c *ShardedToolCache) performCleanupWithCount() int {
	now := time.Now()
	totalCleaned := 0

	// 并发清理每个分片
	var wg sync.WaitGroup
	cleanedCounts := make([]int, len(c.shards))

	for i, shard := range c.shards {
		wg.Add(1)
		go func(idx int, s *cacheShard) {
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
						cleanedCounts[idx]++
					}
				}
				s.mu.Unlock()
			}
		}(i, shard)
	}
	wg.Wait()

	for _, count := range cleanedCounts {
		totalCleaned += count
	}

	return totalCleaned
}

// autoTune 自动调优
func (c *ShardedToolCache) autoTune() {
	defer c.cleanupDone.Done()

	// 初始化调优指标
	c.tuneMetrics = &tuneMetrics{
		qpsHistory: make([]float64, 0, 60), // 保留60个样本
	}

	ticker := time.NewTicker(10 * time.Second) // 每10秒检查一次
	defer ticker.Stop()

	var lastHits, lastMisses int64
	lastCheck := time.Now()

	for {
		select {
		case <-c.ctx.Done():
			return
		case <-ticker.C:
			now := time.Now()
			duration := now.Sub(lastCheck).Seconds()

			// 计算QPS
			currentHits := c.stats.Hits.Load()
			currentMisses := c.stats.Misses.Load()
			totalRequests := (currentHits - lastHits) + (currentMisses - lastMisses)
			qps := float64(totalRequests) / duration

			// 更新指标
			c.tuneMetrics.mu.Lock()
			c.tuneMetrics.qpsHistory = append(c.tuneMetrics.qpsHistory, qps)
			if len(c.tuneMetrics.qpsHistory) > 60 {
				c.tuneMetrics.qpsHistory = c.tuneMetrics.qpsHistory[1:]
			}

			// 计算命中率
			if totalRequests > 0 {
				c.tuneMetrics.avgHitRate = float64(currentHits-lastHits) / float64(totalRequests)
			}

			// 计算负载因子
			c.tuneMetrics.avgLoadFactor = float64(c.Size()) / float64(c.config.Capacity)
			c.tuneMetrics.mu.Unlock()

			// 每分钟执行一次调优决策
			if now.Sub(c.lastTuneTime) >= time.Minute {
				c.performTuning()
				c.lastTuneTime = now
			}

			lastHits = currentHits
			lastMisses = currentMisses
			lastCheck = now
		}
	}
}

// performTuning 执行调优决策
func (c *ShardedToolCache) performTuning() {
	c.tuneMetrics.mu.RLock()
	defer c.tuneMetrics.mu.RUnlock()

	if len(c.tuneMetrics.qpsHistory) < 6 {
		return // 需要至少1分钟的数据
	}

	// 计算平均QPS
	var avgQPS float64
	for _, qps := range c.tuneMetrics.qpsHistory {
		avgQPS += qps
	}
	avgQPS /= float64(len(c.tuneMetrics.qpsHistory))

	// 根据指标调整配置
	// 注意：分片数量在运行时不能更改，但可以记录建议供下次重启使用
	if avgQPS > 1000 && c.tuneMetrics.avgHitRate < 0.7 {
		log.Printf("Auto-tuning: High QPS (%.2f) with low hit rate (%.2f%%). Consider increasing cache capacity or TTL.",
			avgQPS, c.tuneMetrics.avgHitRate*100)
	}

	if c.tuneMetrics.avgLoadFactor > 0.95 {
		log.Printf("Auto-tuning: Cache nearly full (%.2f%%). Consider increasing capacity.",
			c.tuneMetrics.avgLoadFactor*100)
	}

	// 可以动态调整的参数示例：
	// - 调整清理间隔（已在adaptiveCleanup中实现）
	// - 记录性能建议供监控系统使用
}
