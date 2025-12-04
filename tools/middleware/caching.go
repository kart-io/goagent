package middleware

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sync/atomic"
	"time"

	loggercore "github.com/kart-io/logger/core"

	"github.com/kart-io/goagent/cache"
	"github.com/kart-io/goagent/interfaces"
	"github.com/kart-io/goagent/utils/json"
)

// CachingConfig 缓存中间件配置
type CachingConfig struct {
	Cache   cache.Cache
	TTL     time.Duration
	KeyFunc func(string, map[string]interface{}) string
	Logger  loggercore.Logger
}

// CachingOption 缓存中间件选项
type CachingOption func(*CachingConfig)

// WithCache 设置缓存实现
func WithCache(c cache.Cache) CachingOption {
	return func(cfg *CachingConfig) {
		cfg.Cache = c
	}
}

// WithTTL 设置缓存过期时间
func WithTTL(ttl time.Duration) CachingOption {
	return func(cfg *CachingConfig) {
		cfg.TTL = ttl
	}
}

// WithCacheKeyFunc 设置缓存键生成函数
func WithCacheKeyFunc(f func(string, map[string]interface{}) string) CachingOption {
	return func(cfg *CachingConfig) {
		cfg.KeyFunc = f
	}
}

// WithCacheLogger 设置日志记录器
func WithCacheLogger(logger loggercore.Logger) CachingOption {
	return func(cfg *CachingConfig) {
		cfg.Logger = logger
	}
}

// cachingStats 统计信息
type cachingStats struct {
	Hits   atomic.Int64
	Misses atomic.Int64
}

// CachingMiddleware 提供工具调用结果的缓存功能（使用函数式实现）。
//
// 它通过缓存成功的工具执行结果来提高性能，避免重复计算。
// 使用可配置的 TTL（生存时间）和缓存键生成函数。
//
// 使用示例:
//
//	cachingMW := Caching(
//	    WithCache(myCache),
//	    WithTTL(10 * time.Minute),
//	)
//	wrappedTool := tools.WithMiddleware(myTool, cachingMW)
type CachingMiddleware struct {
	config *CachingConfig
	stats  *cachingStats
}

// NewCachingMiddleware 创建一个新的缓存中间件
//
// 参数:
//   - opts: 配置选项
//
// 返回:
//   - *CachingMiddleware: 缓存中间件实例
func NewCachingMiddleware(opts ...CachingOption) *CachingMiddleware {
	cfg := &CachingConfig{
		Cache:   cache.NewSimpleCache(5 * time.Minute), // 使用简化的 SimpleCache
		TTL:     5 * time.Minute,
		KeyFunc: defaultCacheKeyFunc,
		Logger:  nil, // 默认不使用日志（避免依赖）
	}

	for _, opt := range opts {
		opt(cfg)
	}

	return &CachingMiddleware{
		config: cfg,
		stats:  &cachingStats{},
	}
}

// Wrap 实现函数式中间件包装
//
// 这个方法返回一个新的 ToolInvoker，在缓存命中时直接返回结果，
// 不调用实际工具，从而避免不必要的计算。
func (m *CachingMiddleware) Wrap(tool interfaces.Tool, next ToolInvoker) ToolInvoker {
	return func(ctx context.Context, input *interfaces.ToolInput) (*interfaces.ToolOutput, error) {
		// 生成缓存键
		cacheKey := m.config.KeyFunc(tool.Name(), input.Args)

		// 尝试从缓存获取
		if cached, err := m.config.Cache.Get(ctx, cacheKey); err == nil {
			// 缓存命中
			m.stats.Hits.Add(1)
			cachedOutput := cached.(*interfaces.ToolOutput)

			if m.config.Logger != nil {
				m.config.Logger.Debug("工具缓存命中",
					"tool", tool.Name(),
					"cacheKey", cacheKey,
					"hits", m.stats.Hits.Load(),
					"misses", m.stats.Misses.Load(),
				)
			}

			// 创建输出副本，添加缓存元数据
			result := &interfaces.ToolOutput{
				Result:   cachedOutput.Result,
				Success:  cachedOutput.Success,
				Error:    cachedOutput.Error,
				Metadata: make(map[string]interface{}),
			}

			// 复制原始元数据
			for k, v := range cachedOutput.Metadata {
				result.Metadata[k] = v
			}

			// 添加缓存命中标记
			result.Metadata["cache_hit"] = true
			result.Metadata["cached"] = true

			return result, nil // 直接返回缓存结果，不调用 next
		}

		// 缓存未命中
		m.stats.Misses.Add(1)
		if m.config.Logger != nil {
			m.config.Logger.Debug("工具缓存未命中",
				"tool", tool.Name(),
				"cacheKey", cacheKey,
			)
		}

		// 调用实际工具
		output, err := next(ctx, input)
		if err != nil {
			return output, err
		}

		// 如果执行成功，缓存结果
		if output.Success {
			// 创建缓存副本（避免后续修改影响缓存）
			cachedOutput := &interfaces.ToolOutput{
				Result:   output.Result,
				Success:  output.Success,
				Error:    output.Error,
				Metadata: make(map[string]interface{}),
			}

			// 复制元数据（排除内部临时键）
			for k, v := range output.Metadata {
				if len(k) < 2 || k[:2] != "__" {
					cachedOutput.Metadata[k] = v
				}
			}

			// 存储到缓存
			if err := m.config.Cache.Set(ctx, cacheKey, cachedOutput, m.config.TTL); err != nil {
				if m.config.Logger != nil {
					m.config.Logger.Warn("缓存设置失败",
						"tool", tool.Name(),
						"error", err,
					)
				}
			}

			// 添加缓存存储标记
			if output.Metadata == nil {
				output.Metadata = make(map[string]interface{})
			}
			output.Metadata["cache_hit"] = false
			output.Metadata["cache_stored"] = true
		}

		return output, nil
	}
}

// GetStats 获取缓存统计信息（用于测试和监控）
func (m *CachingMiddleware) GetStats() (hits, misses int64) {
	return m.stats.Hits.Load(), m.stats.Misses.Load()
}

// Caching 返回缓存中间件函数（简化接口）
//
// 参数:
//   - opts: 配置选项
//
// 返回:
//   - ToolMiddlewareFunc: 中间件函数
func Caching(opts ...CachingOption) ToolMiddlewareFunc {
	middleware := NewCachingMiddleware(opts...)
	return middleware.Wrap
}

// defaultCacheKeyFunc 是默认的缓存键生成函数
//
// 格式: tool:<tool_name>:<sha256(args)[:8]>
//
// 示例: tool:calculator:a3b2c1d4
func defaultCacheKeyFunc(toolName string, args map[string]interface{}) string {
	// 过滤掉内部元数据
	filtered := make(map[string]interface{})
	for k, v := range args {
		if len(k) < 2 || k[:2] != "__" {
			filtered[k] = v
		}
	}

	// 序列化参数
	data, err := json.Marshal(filtered)
	if err != nil {
		// 序列化失败，使用时间戳作为键（确保不会缓存）
		return fmt.Sprintf("tool:%s:error:%d", toolName, time.Now().UnixNano())
	}

	// 计算 SHA256 哈希
	hash := sha256.Sum256(data)
	hashStr := hex.EncodeToString(hash[:])

	// 使用前 8 个字符作为短哈希
	return "tool:" + toolName + ":" + hashStr[:8]
}
