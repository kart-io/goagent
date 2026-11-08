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

	// 生命周期管理
	ctx         context.Context
	cancel      context.CancelFunc
	wg          sync.WaitGroup
	lazy        bool
	nextCleanup atomic.Int64
}

// cacheEntry 缓存条目
type cacheEntry struct {
	value     interface{}
	expiresAt time.Time
}

// SimpleCacheOption 配置缓存的清理生命周期。
type SimpleCacheOption func(*simpleCacheConfig)
type simpleCacheConfig struct{ lazy bool }

// WithoutBackgroundCleanup 以访问和写入驱动清理，不创建后台协程。
// 适用于不能把 Close 句柄交给调用方的函数式装配；闲置期间到期数据在下次访问或写入回收。
func WithoutBackgroundCleanup() SimpleCacheOption {
	return func(cfg *simpleCacheConfig) { cfg.lazy = true }
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
		ttl:    ttl,
		ctx:    ctx,
		cancel: cancel,
		lazy:   cfg.lazy,
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
		c.data.CompareAndDelete(key, v)
		c.misses.Add(1)
		return nil, agentErrors.ErrCacheMiss
	}

	c.hits.Add(1)
	return entry.value, nil
}

// Set 设置缓存值
func (c *SimpleCache) Set(ctx context.Context, key string, value interface{}, ttl time.Duration) error {
	if ttl <= 0 {
		ttl = c.ttl
	}

	c.data.Store(key, &cacheEntry{
		value:     value,
		expiresAt: time.Now().Add(ttl),
	})
	if c.lazy {
		now := time.Now()
		due := c.nextCleanup.Load()
		if now.UnixNano() >= due && c.nextCleanup.CompareAndSwap(due, now.Add(max(c.ttl/2, time.Nanosecond)).UnixNano()) {
			c.cleanupExpired()
		}
	}
	return nil
}

// Delete 删除缓存值
func (c *SimpleCache) Delete(ctx context.Context, key string) error {
	c.data.Delete(key)
	return nil
}

// Clear 清空所有缓存
func (c *SimpleCache) Clear(ctx context.Context) error {
	c.data.Range(func(key, value interface{}) bool {
		c.data.Delete(key)
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
		c.data.CompareAndDelete(key, v)
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
			c.data.CompareAndDelete(key, value)
		}
		return true
	})
}
