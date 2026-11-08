package cache

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"time"

	"github.com/kart-io/goagent/codec/json"
	agentErrors "github.com/kart-io/goagent/errors"
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
	return nil, agentErrors.ErrCacheDisabled
}

func (c *NoOpCache) Set(ctx context.Context, key string, value interface{}, ttl time.Duration) error {
	return agentErrors.ErrCacheDisabled
}

func (c *NoOpCache) Delete(ctx context.Context, key string) error {
	return agentErrors.ErrCacheDisabled
}

func (c *NoOpCache) Clear(ctx context.Context) error {
	return agentErrors.ErrCacheDisabled
}

func (c *NoOpCache) Has(ctx context.Context, key string) (bool, error) {
	return false, agentErrors.ErrCacheDisabled
}

func (c *NoOpCache) GetStats() CacheStats {
	return CacheStats{}
}

// CacheConfig 缓存配置
type CacheConfig struct {
	Enabled    bool          `json:"enabled"`     // 是否启用缓存
	DefaultTTL time.Duration `json:"default_ttl"` // 默认 TTL
}

// DefaultCacheConfig 返回默认配置。
func DefaultCacheConfig() CacheConfig {
	return CacheConfig{Enabled: true, DefaultTTL: 5 * time.Minute}
}

// NewCacheFromConfig 按启用状态与默认 TTL 创建缓存。
// 启用时返回 *SimpleCache，调用方须调用其 Close 释放后台清理资源。
func NewCacheFromConfig(config CacheConfig) Cache {
	if !config.Enabled {
		return NewNoOpCache()
	}

	return NewSimpleCache(config.DefaultTTL)
}
