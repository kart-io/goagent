package middleware

import (
	"context"
	"sync"
	"sync/atomic"
	"time"

	loggercore "github.com/kart-io/logger/core"
	"golang.org/x/time/rate"

	"github.com/kart-io/goagent/core/logging"
	"github.com/kart-io/goagent/errors"
	"github.com/kart-io/goagent/interfaces"
)

// RateLimitConfig is the rate limiting middleware configuration.
type RateLimitConfig struct {
	// QPS is the requests-per-second limit.
	QPS float64 `json:"qps"`
	// Burst is the number of burst requests (token bucket capacity).
	Burst int `json:"burst"`
	// PerTool controls whether limits apply per tool name (false means a
	// global limit).
	PerTool bool `json:"per_tool"`
	// Logger is a kart-io/logger logger.
	//
	// Deprecated: use ContextLogger; Logger is used only when ContextLogger is nil.
	Logger loggercore.Logger `json:"-"`
	// ContextLogger receives rejected calls; nil disables logging.
	ContextLogger logging.Logger `json:"-"`
	// WaitTimeout is the timeout for waiting for a token (0 means do not wait;
	// reject immediately).
	WaitTimeout time.Duration `json:"wait_timeout"`
}

// RateLimitOption is a rate limiting middleware option.
type RateLimitOption func(*RateLimitConfig)

// WithQPS sets the QPS limit.
func WithQPS(qps float64) RateLimitOption {
	return func(cfg *RateLimitConfig) {
		cfg.QPS = qps
	}
}

// WithBurst sets the burst capacity.
func WithBurst(burst int) RateLimitOption {
	return func(cfg *RateLimitConfig) {
		cfg.Burst = burst
	}
}

// WithPerToolRateLimit enables per-tool-name rate limiting.
func WithPerToolRateLimit() RateLimitOption {
	return func(cfg *RateLimitConfig) {
		cfg.PerTool = true
	}
}

// WithRateLimitContextLogger sets the logger, typically a *slog.Logger or a
// *slogx.Logger.
func WithRateLimitContextLogger(logger logging.Logger) RateLimitOption {
	return func(cfg *RateLimitConfig) {
		cfg.ContextLogger = logger
	}
}

// WithRateLimitLogger sets a kart-io/logger logger.
//
// Deprecated: use WithRateLimitContextLogger.
func WithRateLimitLogger(logger loggercore.Logger) RateLimitOption {
	return func(cfg *RateLimitConfig) {
		cfg.Logger = logger
	}
}

// WithWaitTimeout sets the timeout for waiting for a token.
//
// If set to 0, requests over the limit are rejected immediately without
// waiting. If set to > 0, it waits up to the given time to obtain a token.
func WithWaitTimeout(timeout time.Duration) RateLimitOption {
	return func(cfg *RateLimitConfig) {
		cfg.WaitTimeout = timeout
	}
}

// RateLimitMiddleware rate-limits tool calls (functional implementation).
//
// It uses a token bucket algorithm to limit the tool call rate and supports:
//   - global limiting: all tools share one quota
//   - per-tool limiting: each tool has its own quota
//   - configurable QPS and burst capacity
//   - an optional wait timeout (block or reject immediately)
//
// Example:
//
//	rateLimitMW := RateLimit(
//	    WithQPS(10),           // 10 requests per second
//	    WithBurst(20),         // allow bursts of 20 requests
//	    WithPerToolRateLimit(), // limit each tool independently
//	)
//	wrappedTool := middleware.Wrap(myTool, rateLimitMW)
type RateLimitMiddleware struct {
	config   *RateLimitConfig
	global   *rate.Limiter
	perTool  map[string]*rate.Limiter
	mu       sync.RWMutex
	rejected atomic.Int64
	allowed  atomic.Int64
}

// NewRateLimitMiddleware creates a new rate limiting middleware.
//
// Parameters:
//   - opts: configuration options
//
// Returns:
//   - *RateLimitMiddleware: the rate limiting middleware instance
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

// Wrap implements functional middleware wrapping.
//
// It returns a new ToolInvoker that rejects requests when the rate limit is
// exceeded.
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

			if l := m.log(); l != nil {
				l.WarnContext(
					ctx,
					"工具调用被限流拒绝",
					"tool", tool.Name(),
					"qps", m.config.QPS,
					"rejected", m.rejected.Load(),
					"allowed", m.allowed.Load(),
				)
			}

			return nil, errors.Wrap(
				errors.Newf(errors.CodeMiddlewareExecution, "rate limit exceeded for tool: %s", tool.Name()).
					WithComponent("rate_limit").
					WithOperation("execute").
					WithContext("tool_name", tool.Name()),
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

// GetStats returns rate limiting statistics (for testing and monitoring).
func (m *RateLimitMiddleware) GetStats() (allowed, rejected int64) {
	return m.allowed.Load(), m.rejected.Load()
}

// RateLimit returns the rate limiting middleware function (simplified
// interface).
//
// Parameters:
//   - opts: configuration options
//
// Returns:
//   - ToolMiddlewareFunc: the middleware function
func RateLimit(opts ...RateLimitOption) ToolMiddlewareFunc {
	middleware := NewRateLimitMiddleware(opts...)
	return middleware.Wrap
}
