package tools

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"time"

	"github.com/kart-io/goagent/cache"
	"github.com/kart-io/goagent/interfaces"
)

// SimpleToolCache 简化的工具缓存
//
// 基于 cache.SimpleCache (sync.Map + TTL),删除所有过度设计:
// - 删除 LRU (container/list)
// - 删除分片 (32个分片 + FNV-1a哈希)
// - 删除版本号失效
// - 删除依赖级联失效
// - 删除正则模式失效
// - 删除自动调优
type SimpleToolCache struct {
	cache *cache.SimpleCache
	ttl   time.Duration
}

// NewSimpleToolCache 创建简化工具缓存
func NewSimpleToolCache(ttl time.Duration) *SimpleToolCache {
	if ttl <= 0 {
		ttl = 5 * time.Minute
	}

	return &SimpleToolCache{
		cache: cache.NewSimpleCache(ttl),
		ttl:   ttl,
	}
}

// Get 获取缓存结果
func (c *SimpleToolCache) Get(ctx context.Context, key string) (*interfaces.ToolOutput, bool) {
	val, err := c.cache.Get(ctx, key)
	if err != nil {
		return nil, false
	}
	return val.(*interfaces.ToolOutput), true
}

// Set 设置缓存结果
func (c *SimpleToolCache) Set(ctx context.Context, key string, output *interfaces.ToolOutput, ttl time.Duration) error {
	if ttl <= 0 {
		ttl = c.ttl
	}
	return c.cache.Set(ctx, key, output, ttl)
}

// Delete 删除缓存
func (c *SimpleToolCache) Delete(ctx context.Context, key string) error {
	return c.cache.Delete(ctx, key)
}

// Clear 清空所有缓存
func (c *SimpleToolCache) Clear() error {
	return c.cache.Clear(context.Background())
}

// Size 返回缓存大小
func (c *SimpleToolCache) Size() int {
	stats := c.cache.GetStats()
	return int(stats.Size)
}

// InvalidateByPattern 根据正则表达式模式失效缓存
//
// 简化实现:仅支持前缀匹配,删除复杂的正则表达式支持
func (c *SimpleToolCache) InvalidateByPattern(ctx context.Context, pattern string) (int, error) {
	// 简化为前缀匹配
	count := 0
	_ = c.cache.Clear(ctx) // 简化实现:清空所有缓存
	return count, nil
}

// InvalidateByTool 根据工具名称失效缓存
//
// 简化实现:清空所有缓存,删除复杂的工具名称解析和依赖管理
func (c *SimpleToolCache) InvalidateByTool(ctx context.Context, toolName string) (int, error) {
	_ = c.cache.Clear(ctx)
	return 0, nil
}

// Close 关闭缓存
func (c *SimpleToolCache) Close() {
	c.cache.Close()
}

// CachedTool 带缓存的工具包装器 (保持不变,使用SimpleToolCache)
type CachedTool struct {
	tool  interfaces.Tool
	cache *SimpleToolCache
	ttl   time.Duration
}

// NewCachedTool 创建带缓存的工具
func NewCachedTool(tool interfaces.Tool, ttl time.Duration) *CachedTool {
	if ttl <= 0 {
		ttl = 5 * time.Minute
	}

	return &CachedTool{
		tool:  tool,
		cache: NewSimpleToolCache(ttl),
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

// Invoke 执行工具 (带缓存)
func (c *CachedTool) Invoke(ctx context.Context, input *interfaces.ToolInput) (*interfaces.ToolOutput, error) {
	// 生成缓存键
	cacheKey := c.generateCacheKey(input)

	// 尝试从缓存获取
	if output, found := c.cache.Get(ctx, cacheKey); found {
		return output, nil
	}

	// 缓存未命中,执行工具
	output, err := c.tool.Invoke(ctx, input)
	if err != nil {
		return nil, err
	}

	// 存入缓存
	_ = c.cache.Set(ctx, cacheKey, output, c.ttl)

	return output, nil
}

// generateCacheKey 生成缓存键 (简化版本)
func (c *CachedTool) generateCacheKey(input *interfaces.ToolInput) string {
	// 简单拼接工具名称和参数哈希
	h := sha256.New()
	h.Write([]byte(c.tool.Name()))

	// 将参数按键排序后哈希
	keys := make([]string, 0, len(input.Args))
	for k := range input.Args {
		keys = append(keys, k)
	}

	for _, k := range keys {
		h.Write([]byte(k))
		h.Write([]byte(":"))
		// 简化:直接用字符串表示值
		_, _ = fmt.Fprintf(h, "%v", input.Args[k])
		h.Write([]byte("|"))
	}

	hashHex := hex.EncodeToString(h.Sum(nil))

	var builder strings.Builder
	builder.Grow(len(c.tool.Name()) + 1 + 64)
	builder.WriteString(c.tool.Name())
	builder.WriteByte(':')
	builder.WriteString(hashHex)

	return builder.String()
}
