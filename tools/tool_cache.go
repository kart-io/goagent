package tools

import (
	"container/list"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"hash"
	"os"
	"regexp"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	agentErrors "github.com/kart-io/goagent/errors"
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

	// InvalidateByPattern 根据正则表达式模式失效缓存
	InvalidateByPattern(ctx context.Context, pattern string) (int, error)

	// InvalidateByTool 根据工具名称失效缓存
	InvalidateByTool(ctx context.Context, toolName string) (int, error)
}

// MemoryToolCache 内存工具缓存
//
// 线程安全的 LRU 缓存实现，使用 context 进行生命周期管理
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

	// Lifecycle management using context for graceful shutdown
	ctx         context.Context
	cancel      context.CancelFunc
	cleanupDone sync.WaitGroup

	// closed tracks whether Close() has been called (atomic)
	closed atomic.Int32

	// version 版本号，每次失效时递增
	version atomic.Int64

	// dependencies 工具依赖关系，key 是工具名称，value 是依赖它的工具列表
	dependencies map[string][]string
	depMu        sync.RWMutex
}

// cacheEntry 缓存条目
type cacheEntry struct {
	key        string
	toolName   string // 工具名称，用于按工具失效
	output     *ToolOutput
	expireTime time.Time
	element    *list.Element
	version    int64 // 缓存版本，用于检测失效
}

// CacheStats 缓存统计信息
type CacheStats struct {
	Hits          atomic.Int64
	Misses        atomic.Int64
	Evicts        atomic.Int64
	Invalidations atomic.Int64 // 失效次数
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
//
// 使用 context 进行生命周期管理，确保清理 goroutine 可以优雅关闭
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

	// Create context for lifecycle management
	ctx, cancel := context.WithCancel(context.Background())

	cache := &MemoryToolCache{
		capacity:     config.Capacity,
		cache:        make(map[string]*cacheEntry),
		lruList:      list.New(),
		stats:        &CacheStats{},
		ctx:          ctx,
		cancel:       cancel,
		dependencies: make(map[string][]string),
	}
	cache.closed.Store(0) // Explicitly initialize to not closed
	cache.version.Store(0) // Initialize version

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

	// 检查版本是否失效
	currentVersion := c.version.Load()
	if entry.version < currentVersion {
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

	// 从缓存键中提取工具名称（格式为 "toolName:hash"）
	toolName := extractToolNameFromKey(key)
	currentVersion := c.version.Load()

	// 如果已存在，更新
	if entry, exists := c.cache[key]; exists {
		entry.output = output
		entry.expireTime = time.Now().Add(ttl)
		entry.version = currentVersion
		entry.toolName = toolName
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
		toolName:   toolName,
		output:     output,
		expireTime: time.Now().Add(ttl),
		version:    currentVersion,
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
	return CacheStats{
		Hits:          *copyAtomicInt64(&c.stats.Hits),
		Misses:        *copyAtomicInt64(&c.stats.Misses),
		Evicts:        *copyAtomicInt64(&c.stats.Evicts),
		Invalidations: *copyAtomicInt64(&c.stats.Invalidations),
	}
}

// copyAtomicInt64 creates a copy of an atomic.Int64 with the same value
func copyAtomicInt64(src *atomic.Int64) *atomic.Int64 {
	dst := &atomic.Int64{}
	dst.Store(src.Load())
	return dst
}

// InvalidateByPattern 根据正则表达式模式失效缓存
//
// 支持正则表达式模式匹配缓存键。返回失效的条目数量。
// 失效时会递增全局版本号，并且会级联失效依赖的工具。
func (c *MemoryToolCache) InvalidateByPattern(ctx context.Context, pattern string) (int, error) {
	// 编译正则表达式
	re, err := regexp.Compile(pattern)
	if err != nil {
		return 0, agentErrors.Wrap(err, agentErrors.CodeInvalidInput, "invalid regex pattern").
			WithComponent("memory_tool_cache").
			WithOperation("invalidate_by_pattern")
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	// 收集匹配的键和受影响的工具
	keysToRemove := make([]string, 0)
	affectedTools := make(map[string]struct{})

	for key, entry := range c.cache {
		if re.MatchString(key) {
			keysToRemove = append(keysToRemove, key)
			if entry.toolName != "" {
				affectedTools[entry.toolName] = struct{}{}
			}
		}
	}

	// 递增版本号
	c.version.Add(1)

	// 移除匹配的条目
	for _, key := range keysToRemove {
		if entry, exists := c.cache[key]; exists {
			c.lruList.Remove(entry.element)
			delete(c.cache, key)
		}
	}

	// 级联失效依赖的工具
	dependentCount := 0
	for toolName := range affectedTools {
		count := c.invalidateDependents(toolName)
		dependentCount += count
	}

	// 记录失效统计
	totalInvalidated := len(keysToRemove) + dependentCount
	c.stats.recordInvalidation(int64(totalInvalidated))

	return totalInvalidated, nil
}

// InvalidateByTool 根据工具名称失效缓存
//
// 失效指定工具的所有缓存条目，并级联失效依赖该工具的其他工具。
// 返回失效的条目数量。
func (c *MemoryToolCache) InvalidateByTool(ctx context.Context, toolName string) (int, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	// 收集该工具的所有缓存键
	keysToRemove := make([]string, 0)
	for key, entry := range c.cache {
		if entry.toolName == toolName {
			keysToRemove = append(keysToRemove, key)
		}
	}

	// 递增版本号
	c.version.Add(1)

	// 移除该工具的所有条目
	for _, key := range keysToRemove {
		if entry, exists := c.cache[key]; exists {
			c.lruList.Remove(entry.element)
			delete(c.cache, key)
		}
	}

	// 级联失效依赖的工具
	dependentCount := c.invalidateDependents(toolName)

	// 记录失效统计
	totalInvalidated := len(keysToRemove) + dependentCount
	c.stats.recordInvalidation(int64(totalInvalidated))

	return totalInvalidated, nil
}

// invalidateDependents 失效依赖指定工具的所有工具（内部方法，不加锁）
//
// 递归失效所有直接和间接依赖的工具。
// 注意：调用者必须持有 mu 锁。
func (c *MemoryToolCache) invalidateDependents(toolName string) int {
	c.depMu.RLock()
	dependents, exists := c.dependencies[toolName]
	c.depMu.RUnlock()

	if !exists || len(dependents) == 0 {
		return 0
	}

	totalCount := 0

	// 失效每个依赖工具
	for _, dependent := range dependents {
		keysToRemove := make([]string, 0)
		for key, entry := range c.cache {
			if entry.toolName == dependent {
				keysToRemove = append(keysToRemove, key)
			}
		}

		// 移除依赖工具的条目
		for _, key := range keysToRemove {
			if entry, exists := c.cache[key]; exists {
				c.lruList.Remove(entry.element)
				delete(c.cache, key)
				totalCount++
			}
		}

		// 递归失效依赖的依赖
		totalCount += c.invalidateDependents(dependent)
	}

	return totalCount
}

// AddDependency 添加工具依赖关系
//
// 声明 dependentTool 依赖 dependsOnTool。
// 当 dependsOnTool 的缓存失效时，dependentTool 的缓存也会自动失效。
func (c *MemoryToolCache) AddDependency(dependentTool, dependsOnTool string) {
	c.depMu.Lock()
	defer c.depMu.Unlock()

	// 初始化依赖列表
	if c.dependencies[dependsOnTool] == nil {
		c.dependencies[dependsOnTool] = make([]string, 0)
	}

	// 检查是否已存在
	for _, dep := range c.dependencies[dependsOnTool] {
		if dep == dependentTool {
			return // 已存在，不重复添加
		}
	}

	// 添加依赖关系
	c.dependencies[dependsOnTool] = append(c.dependencies[dependsOnTool], dependentTool)
}

// RemoveDependency 移除工具依赖关系
func (c *MemoryToolCache) RemoveDependency(dependentTool, dependsOnTool string) {
	c.depMu.Lock()
	defer c.depMu.Unlock()

	deps, exists := c.dependencies[dependsOnTool]
	if !exists {
		return
	}

	// 查找并移除
	for i, dep := range deps {
		if dep == dependentTool {
			c.dependencies[dependsOnTool] = append(deps[:i], deps[i+1:]...)
			return
		}
	}
}

// GetVersion 获取当前缓存版本号
func (c *MemoryToolCache) GetVersion() int64 {
	return c.version.Load()
}

// extractToolNameFromKey 从缓存键中提取工具名称
//
// 缓存键格式为 "toolName:hash"
func extractToolNameFromKey(key string) string {
	idx := strings.IndexByte(key, ':')
	if idx == -1 {
		return ""
	}
	return key[:idx]
}

// Close 关闭缓存，清理资源
//
// 使用 context cancellation 优雅关闭清理 goroutine。
// 使用 atomic 操作确保 Close 的幂等性（多次调用是安全的）。
func (c *MemoryToolCache) Close() {
	// Use atomic CAS to ensure Close is idempotent
	if !c.closed.CompareAndSwap(0, 1) {
		// Already closed, return immediately
		return
	}

	// Cancel the context to signal cleanup goroutine to stop
	c.cancel()

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
//
// 使用 context 进行优雅关闭。在清理过程中使用细粒度锁定
// 以避免阻塞其他操作过长时间。
func (c *MemoryToolCache) cleanupExpired(interval time.Duration) {
	defer c.cleanupDone.Done()
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-c.ctx.Done():
			// Context cancelled - graceful shutdown
			return
		case <-ticker.C:
			c.performCleanup()
		}
	}
}

// performCleanup 执行一次清理操作
//
// 分两步进行：
// 1. 快速扫描找出过期的键（持有读锁）
// 2. 批量删除过期条目（持有写锁）
// 这样可以最小化写锁持有时间，减少对并发访问的影响。
func (c *MemoryToolCache) performCleanup() {
	now := time.Now()

	// Phase 1: Identify expired keys with read lock
	// This allows concurrent reads during scanning
	c.mu.RLock()
	expiredKeys := make([]string, 0, len(c.cache)/10) // Pre-allocate for ~10% expiry rate
	for key, entry := range c.cache {
		if now.After(entry.expireTime) {
			expiredKeys = append(expiredKeys, key)
		}
	}
	c.mu.RUnlock()

	// Phase 2: Remove expired entries with write lock
	// Only lock if there's something to delete
	if len(expiredKeys) > 0 {
		c.mu.Lock()
		for _, key := range expiredKeys {
			// Double-check entry still exists and is still expired
			// (it might have been updated/deleted between phases)
			if entry, exists := c.cache[key]; exists && now.After(entry.expireTime) {
				c.lruList.Remove(entry.element)
				delete(c.cache, key)
			}
		}
		c.mu.Unlock()
	}
}

// recordHit 记录命中
func (s *CacheStats) recordHit() {
	s.Hits.Add(1)
}

// recordMiss 记录未命中
func (s *CacheStats) recordMiss() {
	s.Misses.Add(1)
}

// recordEvict 记录淘汰
func (s *CacheStats) recordEvict() {
	s.Evicts.Add(1)
}

// recordInvalidation 记录失效
func (s *CacheStats) recordInvalidation(count int64) {
	s.Invalidations.Add(count)
}

// HitRate 计算命中率
func (s *CacheStats) HitRate() float64 {
	hits := s.Hits.Load()
	misses := s.Misses.Load()
	total := hits + misses
	if total == 0 {
		return 0
	}

	return float64(hits) / float64(total)
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
	// Log error at WARN level for debugging; cache failures should not affect tool execution
	if setErr := c.cache.Set(ctx, cacheKey, output, c.ttl); setErr != nil {
		fmt.Fprintf(os.Stderr, "[WARN] cache set failed (tool=%s, key=%s): %v\n", c.tool.Name(), cacheKey, setErr)
	}

	return output, nil
}

// generateCacheKey 生成缓存键
//
// This function uses direct hash computation instead of JSON marshaling
// for better performance. It hashes the map keys and values directly
// to avoid intermediate allocations.
//
// Optimization: Uses strings.Builder with hex.EncodeToString instead of
// fmt.Sprintf to reduce allocations.
func (c *CachedTool) generateCacheKey(input *ToolInput) (string, error) {
	h := sha256.New()

	// Hash the tool name first to namespace the cache key
	h.Write([]byte(c.tool.Name()))
	h.Write([]byte{0}) // separator

	// Hash the args map deterministically (sorted keys)
	if err := hashMap(h, input.Args); err != nil {
		return "", err
	}

	// Build key using strings.Builder for efficiency
	toolName := c.tool.Name()
	hashHex := hex.EncodeToString(h.Sum(nil))

	// Pre-allocate: toolName + ":" + hex hash (64 chars for SHA256)
	var builder strings.Builder
	builder.Grow(len(toolName) + 1 + 64)
	builder.WriteString(toolName)
	builder.WriteByte(':')
	builder.WriteString(hashHex)

	return builder.String(), nil
}

// hashMap writes a deterministic hash of a map to the hasher.
// Keys are sorted to ensure consistent ordering.
func hashMap(h hash.Hash, m map[string]interface{}) error {
	if m == nil {
		h.Write([]byte{0}) // nil marker
		return nil
	}

	// Sort keys for deterministic ordering
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	// Write length prefix
	var buf [8]byte
	binary.LittleEndian.PutUint64(buf[:], uint64(len(m)))
	h.Write(buf[:])

	for _, k := range keys {
		// Write key
		h.Write([]byte(k))
		h.Write([]byte{0}) // separator

		// Write value
		if err := hashValue(h, m[k]); err != nil {
			return err
		}
	}

	return nil
}

// hashValue writes a deterministic hash of a value to the hasher.
func hashValue(h hash.Hash, v interface{}) error {
	var buf [8]byte

	switch val := v.(type) {
	case nil:
		h.Write([]byte{0}) // type marker for nil
	case bool:
		h.Write([]byte{1}) // type marker
		if val {
			h.Write([]byte{1})
		} else {
			h.Write([]byte{0})
		}
	case int:
		h.Write([]byte{2}) // type marker
		binary.LittleEndian.PutUint64(buf[:], uint64(val))
		h.Write(buf[:])
	case int64:
		h.Write([]byte{3}) // type marker
		binary.LittleEndian.PutUint64(buf[:], uint64(val))
		h.Write(buf[:])
	case float64:
		h.Write([]byte{4}) // type marker
		binary.LittleEndian.PutUint64(buf[:], uint64(val))
		h.Write(buf[:])
	case string:
		h.Write([]byte{5}) // type marker
		binary.LittleEndian.PutUint64(buf[:], uint64(len(val)))
		h.Write(buf[:])
		h.Write([]byte(val))
	case []interface{}:
		h.Write([]byte{6}) // type marker
		binary.LittleEndian.PutUint64(buf[:], uint64(len(val)))
		h.Write(buf[:])
		for _, item := range val {
			if err := hashValue(h, item); err != nil {
				return err
			}
		}
	case map[string]interface{}:
		h.Write([]byte{7}) // type marker
		if err := hashMap(h, val); err != nil {
			return err
		}
	default:
		// For unknown types, use fmt.Sprintf as fallback
		// This is less efficient but ensures correctness
		h.Write([]byte{8}) // type marker for unknown
		str := fmt.Sprintf("%v", val)
		binary.LittleEndian.PutUint64(buf[:], uint64(len(str)))
		h.Write(buf[:])
		h.Write([]byte(str))
	}

	return nil
}

// InvalidateCacheByPrefix 根据前缀失效缓存
func (c *CachedTool) InvalidateCacheByPrefix(ctx context.Context, prefix string) error {
	// 注意：这需要缓存实现支持前缀查询
	// 当前简化实现不支持
	return agentErrors.New(agentErrors.CodeNotImplemented, "prefix invalidation not implemented").
		WithComponent("cached_tool").
		WithOperation("invalidate_cache_by_prefix")
}
