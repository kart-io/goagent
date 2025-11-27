package middleware

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"golang.org/x/time/rate"

	loggercore "github.com/kart-io/logger/core"

	"github.com/kart-io/goagent/errors"
	"github.com/kart-io/goagent/interfaces"
)

// RateLimitConfig 限流中间件配置
type RateLimitConfig struct {
	// QPS 每秒请求数限制
	QPS float64
	// Burst 突发请求数（令牌桶容量）
	Burst int
	// PerTool 是否按工具名限流（false=全局限流）
	PerTool bool
	// Logger 日志记录器
	Logger loggercore.Logger
	// WaitTimeout 等待令牌的超时时间（0表示不等待，直接拒绝）
	WaitTimeout time.Duration
}

// RateLimitOption 限流中间件选项
type RateLimitOption func(*RateLimitConfig)

// WithQPS 设置 QPS 限制
func WithQPS(qps float64) RateLimitOption {
	return func(cfg *RateLimitConfig) {
		cfg.QPS = qps
	}
}

// WithBurst 设置突发容量
func WithBurst(burst int) RateLimitOption {
	return func(cfg *RateLimitConfig) {
		cfg.Burst = burst
	}
}

// WithPerToolRateLimit 启用按工具名限流
func WithPerToolRateLimit() RateLimitOption {
	return func(cfg *RateLimitConfig) {
		cfg.PerTool = true
	}
}

// WithRateLimitLogger 设置日志记录器
func WithRateLimitLogger(logger loggercore.Logger) RateLimitOption {
	return func(cfg *RateLimitConfig) {
		cfg.Logger = logger
	}
}

// WithWaitTimeout 设置等待令牌的超时时间
//
// 如果设置为 0，则不等待，直接拒绝超过限制的请求
// 如果设置为 > 0，则等待指定时间尝试获取令牌
func WithWaitTimeout(timeout time.Duration) RateLimitOption {
	return func(cfg *RateLimitConfig) {
		cfg.WaitTimeout = timeout
	}
}

// RateLimitMiddleware 提供工具调用的限流功能（使用函数式实现）。
//
// 它使用令牌桶算法限制工具调用的速率，支持：
//   - 全局限流：所有工具共享限流配额
//   - 按工具限流：每个工具独立的限流配额
//   - 可配置的 QPS 和突发容量
//   - 可选的等待超时（阻塞或立即拒绝）
//
// 使用示例:
//
//	rateLimitMW := RateLimit(
//	    WithQPS(10),           // 每秒 10 个请求
//	    WithBurst(20),         // 允许突发 20 个请求
//	    WithPerToolRateLimit(), // 每个工具独立限流
//	)
//	wrappedTool := tools.WithMiddleware(myTool, rateLimitMW)
type RateLimitMiddleware struct {
	config   *RateLimitConfig
	global   *rate.Limiter
	perTool  map[string]*rate.Limiter
	mu       sync.RWMutex
	rejected atomic.Int64
	allowed  atomic.Int64
}

// NewRateLimitMiddleware 创建一个新的限流中间件
//
// 参数:
//   - opts: 配置选项
//
// 返回:
//   - *RateLimitMiddleware: 限流中间件实例
func NewRateLimitMiddleware(opts ...RateLimitOption) *RateLimitMiddleware {
	cfg := &RateLimitConfig{
		QPS:         10,    // 默认 10 QPS
		Burst:       20,    // 默认突发 20
		PerTool:     false, // 默认全局限流
		Logger:      nil,   // 默认不使用日志
		WaitTimeout: 0,     // 默认不等待，直接拒绝
	}

	for _, opt := range opts {
		opt(cfg)
	}

	middleware := &RateLimitMiddleware{
		config:  cfg,
		perTool: make(map[string]*rate.Limiter),
	}

	// 如果是全局限流，创建全局限流器
	if !cfg.PerTool {
		middleware.global = rate.NewLimiter(rate.Limit(cfg.QPS), cfg.Burst)
	}

	return middleware
}

// getLimiter 获取限流器（全局或按工具）
func (m *RateLimitMiddleware) getLimiter(toolName string) *rate.Limiter {
	if !m.config.PerTool {
		return m.global
	}

	// 按工具限流，需要加锁
	m.mu.RLock()
	limiter, exists := m.perTool[toolName]
	m.mu.RUnlock()

	if exists {
		return limiter
	}

	// 创建新的限流器
	m.mu.Lock()
	defer m.mu.Unlock()

	// 双重检查
	if limiter, exists := m.perTool[toolName]; exists {
		return limiter
	}

	limiter = rate.NewLimiter(rate.Limit(m.config.QPS), m.config.Burst)
	m.perTool[toolName] = limiter
	return limiter
}

// Wrap 实现函数式中间件包装
//
// 这个方法返回一个新的 ToolInvoker，在超过限流时拒绝请求。
func (m *RateLimitMiddleware) Wrap(tool interfaces.Tool, next ToolInvoker) ToolInvoker {
	return func(ctx context.Context, input *interfaces.ToolInput) (*interfaces.ToolOutput, error) {
		limiter := m.getLimiter(tool.Name())

		// 尝试获取令牌
		var allowed bool
		if m.config.WaitTimeout > 0 {
			// 有等待超时，尝试等待获取令牌
			waitCtx, cancel := context.WithTimeout(ctx, m.config.WaitTimeout)
			defer cancel()

			err := limiter.Wait(waitCtx)
			allowed = (err == nil)
		} else {
			// 无等待超时，立即检查是否允许
			allowed = limiter.Allow()
		}

		if !allowed {
			// 限流拒绝
			m.rejected.Add(1)

			if m.config.Logger != nil {
				m.config.Logger.Warn("工具调用被限流拒绝",
					"tool", tool.Name(),
					"qps", m.config.QPS,
					"rejected", m.rejected.Load(),
					"allowed", m.allowed.Load(),
				)
			}

			return nil, errors.Wrap(
				fmt.Errorf("rate limit exceeded for tool: %s", tool.Name()),
				errors.CodeMiddlewareExecution,
				"rate limit exceeded",
			)
		}

		// 允许请求
		m.allowed.Add(1)

		// 调用下一层
		output, err := next(ctx, input)

		// 添加限流元数据
		if output != nil {
			if output.Metadata == nil {
				output.Metadata = make(map[string]interface{})
			}
			output.Metadata["rate_limited"] = false
		}

		return output, err
	}
}

// GetStats 获取限流统计信息（用于测试和监控）
func (m *RateLimitMiddleware) GetStats() (allowed, rejected int64) {
	return m.allowed.Load(), m.rejected.Load()
}

// RateLimit 返回限流中间件函数（简化接口）
//
// 参数:
//   - opts: 配置选项
//
// 返回:
//   - ToolMiddlewareFunc: 中间件函数
func RateLimit(opts ...RateLimitOption) ToolMiddlewareFunc {
	middleware := NewRateLimitMiddleware(opts...)
	return middleware.Wrap
}
