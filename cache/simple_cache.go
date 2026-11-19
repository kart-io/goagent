package cache

import (
	"context"
	"sync"
	"sync/atomic"
	"time"

	agentErrors "github.com/kart-io/goagent/errors"
)

// SimpleCache 简化的缓存实现
//
// 使用 sync.Map + TTL 提供线程安全的缓存,删除所有过度设计的特性:
// - 删除 LRU (实际场景不需要)
// - 删除多层缓存 (单进程应用不需要)
// - 删除分片 (sync.Map内部已优化)
// - 删除自动调优 (过度设计)
// - 删除依赖管理 (过度复杂)
type SimpleCache struct {
	data sync.Map
	ttl  time.Duration

	// 统计信息
	hits   atomic.Int64
	misses atomic.Int64
	sets   atomic.Int64

	// 生命周期管理
	ctx         context.Context
	cancel      context.CancelFunc
	wg          sync.WaitGroup
	lazy        bool
	nextCleanup atomic.Int64

	// maxEntries 为 0 表示不限条目数；size 是当前条目数，随每次成功插入与删除增减。
	maxEntries int64
	size       atomic.Int64

	// tagMu 保护 tags，串行化带标签的写入、删除与所有索引变更；不带标签的 Set 与读取不加锁，
	// 无锁 Set 换掉带标签条目是锁外的失效，之后再取锁摘除旧索引。
	// 不变量：缓存只能多删、不能漏删——锁外任一可读的带标签条目都登记在其每个标签下，DeleteTag 因此删尽它。
	tagMu sync.Mutex
	tags  map[string]map[string]struct{}
}

// cacheEntry 缓存条目
type cacheEntry struct {
	value     interface{}
	expiresAt time.Time
	tags      []string
}

// SimpleCacheOption 配置缓存的清理生命周期。
type (
	SimpleCacheOption func(*simpleCacheConfig)
	simpleCacheConfig struct {
		lazy       bool
		maxEntries int64
	}
)

// WithoutBackgroundCleanup 以访问和写入驱动清理，不创建后台协程。
// 适用于不能把 Close 句柄交给调用方的函数式装配；闲置期间到期数据在下次访问或写入回收。
func WithoutBackgroundCleanup() SimpleCacheOption {
	return func(cfg *simpleCacheConfig) { cfg.lazy = true }
}

// WithMaxEntries 限制条目数：超出时先回收过期条目，仍超出则淘汰其他条目。n<=0 表示不限。
// 键由外部输入决定时（如工具参数）应设置它，否则 TTL 窗口内内存随不同键持续增长。
func WithMaxEntries(n int) SimpleCacheOption {
	return func(cfg *simpleCacheConfig) { cfg.maxEntries = int64(max(n, 0)) }
}

// NewSimpleCache 创建简化缓存。默认启用后台清理，调用方须关闭它。
func NewSimpleCache(ttl time.Duration, opts ...SimpleCacheOption) *SimpleCache {
	if ttl <= 0 {
		ttl = 5 * time.Minute
	}

	var cfg simpleCacheConfig
	for _, opt := range opts {
		opt(&cfg)
	}
	ctx, cancel := context.WithCancel(context.Background())
	c := &SimpleCache{
		ttl:        ttl,
		ctx:        ctx,
		cancel:     cancel,
		lazy:       cfg.lazy,
		maxEntries: cfg.maxEntries,
		tags:       make(map[string]map[string]struct{}),
	}

	c.nextCleanup.Store(time.Now().Add(max(ttl/2, time.Nanosecond)).UnixNano())
	if !c.lazy {
		c.wg.Add(1)
		go c.cleanup()
	}

	return c
}

// Get 获取缓存值
func (c *SimpleCache) Get(ctx context.Context, key string) (interface{}, error) {
	v, ok := c.data.Load(key)
	if !ok {
		c.misses.Add(1)
		return nil, agentErrors.ErrCacheMiss
	}

	entry := v.(*cacheEntry)
	if time.Now().After(entry.expiresAt) {
		c.remove(key, v)
		c.misses.Add(1)
		return nil, agentErrors.ErrCacheMiss
	}

	c.hits.Add(1)
	return entry.value, nil
}

// GetBytes 按 Get 的命中、过期与统计路径读取；值不是 []byte 时返回 CodeInvalidInput。
func (c *SimpleCache) GetBytes(ctx context.Context, key string) ([]byte, error) {
	v, err := c.Get(ctx, key)
	if err != nil {
		return nil, err
	}
	b, ok := v.([]byte)
	if !ok {
		return nil, agentErrors.Newf(agentErrors.CodeInvalidInput, "缓存值类型为 %T，不是 []byte", v).WithComponent("cache").WithOperation("GetBytes")
	}
	return b, nil
}

// SetBytes 写入字节值。
func (c *SimpleCache) SetBytes(ctx context.Context, key string, value []byte, ttl time.Duration) error {
	return c.Set(ctx, key, value, ttl)
}

// Set 设置缓存值。不带标签的写入不取 tagMu：带标签条目只在锁内写入，无锁写入不会让带标签条目变得可读。
// 覆盖了带标签的旧条目时，再取锁摘除旧条目留下的索引；摘除之前 DeleteTag 可能多删这个新值，但不会漏删。
func (c *SimpleCache) Set(ctx context.Context, key string, value interface{}, ttl time.Duration) error {
	if ttl <= 0 {
		ttl = c.ttl
	}
	old, loaded := c.data.Swap(key, &cacheEntry{value: value, expiresAt: time.Now().Add(ttl)})
	if loaded && len(old.(*cacheEntry).tags) > 0 {
		c.tagMu.Lock()
		c.untagStale(key, old.(*cacheEntry).tags)
		c.tagMu.Unlock()
	}
	c.afterInsert(key, loaded)
	return nil
}

// set 在 tagMu 下写入带标签的条目，并以 tags 整体替换旧标签。
func (c *SimpleCache) set(key string, value interface{}, ttl time.Duration, tags []string) error {
	if ttl <= 0 {
		ttl = c.ttl
	}

	c.tagMu.Lock()
	old, loaded := c.data.Swap(key, &cacheEntry{
		value:     value,
		expiresAt: time.Now().Add(ttl),
		tags:      tags,
	})
	if loaded {
		c.untag(key, old.(*cacheEntry).tags)
	}
	c.tag(key, tags)
	c.tagMu.Unlock()

	c.afterInsert(key, loaded)
	return nil
}

// afterInsert 累计写入次数，在新增键时计数并按需淘汰，再按需执行惰性清理。
func (c *SimpleCache) afterInsert(key string, loaded bool) {
	c.sets.Add(1)
	if !loaded && c.size.Add(1) > c.maxEntries && c.maxEntries > 0 {
		c.evict(key)
	}
	if c.lazy {
		now := time.Now()
		due := c.nextCleanup.Load()
		if now.UnixNano() >= due && c.nextCleanup.CompareAndSwap(due, now.Add(max(c.ttl/2, time.Nanosecond)).UnixNano()) {
			c.cleanupExpired()
		}
	}
}

// Delete 删除缓存值
func (c *SimpleCache) Delete(ctx context.Context, key string) error {
	c.tagMu.Lock()
	defer c.tagMu.Unlock()

	if v, ok := c.data.LoadAndDelete(key); ok {
		c.size.Add(-1)
		c.untag(key, v.(*cacheEntry).tags)
	}
	return nil
}

// Clear 清空所有缓存
func (c *SimpleCache) Clear(ctx context.Context) error {
	c.data.Range(func(key, value interface{}) bool {
		c.remove(key, value)
		return true
	})
	return nil
}

// Has 检查键是否存在
func (c *SimpleCache) Has(ctx context.Context, key string) (bool, error) {
	v, ok := c.data.Load(key)
	if !ok {
		return false, nil
	}

	entry := v.(*cacheEntry)
	if time.Now().After(entry.expiresAt) {
		c.remove(key, v)
		return false, nil
	}

	return true, nil
}

// GetStats 获取统计信息
func (c *SimpleCache) GetStats() CacheStats {
	hits := c.hits.Load()
	misses := c.misses.Load()
	total := hits + misses

	var hitRate float64
	if total > 0 {
		hitRate = float64(hits) / float64(total)
	}

	// 计算大小
	var size int64
	c.data.Range(func(key, value interface{}) bool {
		size++
		return true
	})

	return CacheStats{
		Hits:    hits,
		Misses:  misses,
		Sets:    c.sets.Load(),
		Size:    size,
		HitRate: hitRate,
	}
}

// Close 关闭缓存
func (c *SimpleCache) Close() {
	c.cancel()
	c.wg.Wait()
}

// cleanup 定期清理过期条目
func (c *SimpleCache) cleanup() {
	defer c.wg.Done()

	ticker := time.NewTicker(max(c.ttl/2, time.Nanosecond))
	defer ticker.Stop()

	for {
		select {
		case <-c.ctx.Done():
			return
		case <-ticker.C:
			c.cleanupExpired()
		}
	}
}

// cleanupExpired 清理过期条目
func (c *SimpleCache) cleanupExpired() {
	now := time.Now()
	c.data.Range(func(key, value interface{}) bool {
		entry := value.(*cacheEntry)
		if now.After(entry.expiresAt) {
			c.remove(key, value)
		}
		return true
	})
}

// remove 在条目仍是 value 时删除它并同步计数。
func (c *SimpleCache) remove(key, value interface{}) {
	c.tagMu.Lock()
	defer c.tagMu.Unlock()

	if c.data.CompareAndDelete(key, value) {
		c.size.Add(-1)
		c.untag(key.(string), value.(*cacheEntry).tags)
	}
}

// evict 在条目数超过上限时先回收过期条目，仍超出则淘汰除 keep 外的条目。
func (c *SimpleCache) evict(keep string) {
	c.cleanupExpired()
	c.data.Range(func(key, value interface{}) bool {
		if c.size.Load() <= c.maxEntries {
			return false
		}
		if key != keep {
			c.remove(key, value)
		}
		return true
	})
}
