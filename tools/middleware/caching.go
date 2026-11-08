package middleware

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"sync/atomic"
	"time"

	"github.com/kart-io/goagent/codec/json"

	loggercore "github.com/kart-io/logger/core"

	"github.com/kart-io/goagent/cache"
	agentErrors "github.com/kart-io/goagent/errors"
	"github.com/kart-io/goagent/interfaces"
	"github.com/kart-io/goagent/internal/snapshot"
)

// CachingConfig 缓存中间件配置
type CachingConfig struct {
	Cache   cache.Cache                                 `json:"-"`
	TTL     time.Duration                               `json:"ttl"`
	KeyFunc func(string, map[string]interface{}) string `json:"-"`
	Logger  loggercore.Logger                           `json:"-"`
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
		TTL:     5 * time.Minute,
		KeyFunc: defaultCacheKeyFunc,
		Logger:  nil, // 默认不使用日志（避免依赖）
	}

	for _, opt := range opts {
		opt(cfg)
	}

	if cfg.Cache == nil {
		cfg.Cache = cache.NewSimpleCache(cfg.TTL, cache.WithoutBackgroundCleanup())
	}
	if cfg.KeyFunc == nil {
		cfg.KeyFunc = defaultCacheKeyFunc
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
		if result, ok := m.cachedOutput(ctx, cacheKey); ok {
			// 缓存命中
			m.stats.Hits.Add(1)

			if m.config.Logger != nil {
				m.config.Logger.Debug("工具缓存命中",
					"tool", tool.Name(),
					"cacheKey", cacheKey,
					"hits", m.stats.Hits.Load(),
					"misses", m.stats.Misses.Load(),
				)
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

		if output == nil {
			return nil, agentErrors.New(agentErrors.CodeInvalidOutput, "工具返回空结果").WithComponent("tool_cache").WithOperation("invoke")
		}
		// 如果执行成功，缓存结果
		if output.Success {
			cachedOutput, copyErr := cloneToolOutput(output)
			if copyErr != nil {
				return output, nil
			} // 业务结果仍交付，不缓存无法隔离的资源对象。
			// 仅删除已隔离副本的临时键，不能再次插入原响应的可变引用。
			for k := range cachedOutput.Metadata {
				if strings.HasPrefix(k, "__") {
					delete(cachedOutput.Metadata, k)
				}
			}

			// 存储到缓存
			stored := false
			if err := m.config.Cache.Set(ctx, cacheKey, cachedOutput, m.config.TTL); err != nil {
				if m.config.Logger != nil {
					m.config.Logger.Warn("缓存设置失败",
						"tool", tool.Name(),
						"error", err,
					)
				}
			} else {
				stored = true
			}

			// 添加缓存存储标记
			if output.Metadata == nil {
				output.Metadata = make(map[string]interface{})
			}
			output.Metadata["cache_hit"] = false
			output.Metadata["cache_stored"] = stored
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
// 格式: tool:<tool_name>:<完整 sha256(args)>
//
// 示例格式: tool:calculator:<64个十六进制字符>
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

	// 保留完整摘要，避免短键碰撞复用另一组参数的结果。
	return "tool:" + toolName + ":" + hashStr
}

// cloneToolOutput 隔离所有业务字段，保留具体 Go 类型。
func cloneToolOutput(out *interfaces.ToolOutput) (*interfaces.ToolOutput, error) {
	cloned, err := snapshot.Clone(out)
	if err != nil {
		return nil, err
	}
	result := cloned.(*interfaces.ToolOutput)
	if result.Metadata == nil {
		result.Metadata = make(map[string]any)
	}
	return result, nil
}

// cachedOutput 将共享缓存中的非本组件数据视为未命中，异常缓存不阻断真实工具。
func (m *CachingMiddleware) cachedOutput(ctx context.Context, key string) (*interfaces.ToolOutput, bool) {
	value, err := m.config.Cache.Get(ctx, key)
	if err != nil {
		return nil, false
	}
	out, ok := value.(*interfaces.ToolOutput)
	if !ok || out == nil {
		return nil, false
	}
	copy, err := cloneToolOutput(out)
	return copy, err == nil
}
