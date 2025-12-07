# GoAgent Middleware API 参考

本文档提供 GoAgent 中间件系统的完整 API 参考。

## 目录

- [Middleware 接口](#middleware-接口)
- [MiddlewareChain](#middlewarechain)
- [内置中间件](#内置中间件)
- [自定义中间件](#自定义中间件)

---

## Middleware 接口

### core/middleware.Middleware

```go
package middleware

// Middleware 中间件接口
type Middleware interface {
    // Name 返回中间件名称
    Name() string

    // OnBefore 请求前处理
    OnBefore(ctx context.Context, request *MiddlewareRequest) (*MiddlewareRequest, error)

    // OnAfter 响应后处理
    OnAfter(ctx context.Context, response *MiddlewareResponse) (*MiddlewareResponse, error)

    // OnError 错误处理
    OnError(ctx context.Context, err error) error
}
```

### MiddlewareRequest

```go
type MiddlewareRequest struct {
    // Input 输入数据
    Input interface{} `json:"input"`

    // State 状态
    State state.State `json:"state"`

    // Runtime 运行时引用
    Runtime interface{} `json:"-"`

    // Metadata 元数据
    Metadata map[string]interface{} `json:"metadata"`

    // Headers 请求头
    Headers map[string]string `json:"headers"`

    // Timestamp 时间戳
    Timestamp time.Time `json:"timestamp"`
}
```

### MiddlewareResponse

```go
type MiddlewareResponse struct {
    // Output 输出数据
    Output interface{} `json:"output"`

    // State 状态
    State state.State `json:"state"`

    // Metadata 元数据
    Metadata map[string]interface{} `json:"metadata"`

    // Headers 响应头
    Headers map[string]string `json:"headers"`

    // Duration 执行时长
    Duration time.Duration `json:"duration"`

    // TokenUsage Token 使用统计
    TokenUsage *interfaces.TokenUsage `json:"token_usage,omitempty"`

    // Error 错误
    Error error `json:"error,omitempty"`
}
```

### Handler

```go
// Handler 处理器函数类型
type Handler func(ctx context.Context, request *MiddlewareRequest) (*MiddlewareResponse, error)
```

---

## MiddlewareChain

### middleware.MiddlewareChain

```go
type MiddlewareChain struct {
    middlewares []Middleware
    handler     Handler
}

// NewMiddlewareChain 创建中间件链
func NewMiddlewareChain(handler Handler) *MiddlewareChain

// Use 添加中间件
func (c *MiddlewareChain) Use(middleware ...Middleware) *MiddlewareChain

// Execute 执行中间件链
func (c *MiddlewareChain) Execute(ctx context.Context, request *MiddlewareRequest) (*MiddlewareResponse, error)

// Size 返回中间件数量
func (c *MiddlewareChain) Size() int
```

### 执行流程

```
Request → MW1.OnBefore → MW2.OnBefore → ... → Handler → ... → MW2.OnAfter → MW1.OnAfter → Response
```

### 使用示例

```go
import "github.com/kart-io/goagent/core/middleware"

// 创建处理器
handler := func(ctx context.Context, req *middleware.MiddlewareRequest) (*middleware.MiddlewareResponse, error) {
    // 处理逻辑
    return &middleware.MiddlewareResponse{
        Output: "result",
    }, nil
}

// 创建中间件链
chain := middleware.NewMiddlewareChain(handler).
    Use(middleware.NewLoggingMiddleware(nil)).
    Use(middleware.NewTimingMiddleware()).
    Use(middleware.NewCacheMiddleware(5 * time.Minute))

// 执行
response, err := chain.Execute(ctx, &middleware.MiddlewareRequest{
    Input: "input data",
})
```

---

## 内置中间件

### BaseMiddleware

```go
type BaseMiddleware struct {
    name string
}

// NewBaseMiddleware 创建基础中间件
func NewBaseMiddleware(name string) *BaseMiddleware

// Name 返回名称
func (m *BaseMiddleware) Name() string

// OnBefore 默认实现（直接返回）
func (m *BaseMiddleware) OnBefore(ctx context.Context, req *MiddlewareRequest) (*MiddlewareRequest, error)

// OnAfter 默认实现（直接返回）
func (m *BaseMiddleware) OnAfter(ctx context.Context, resp *MiddlewareResponse) (*MiddlewareResponse, error)

// OnError 默认实现（直接返回错误）
func (m *BaseMiddleware) OnError(ctx context.Context, err error) error
```

### LoggingMiddleware

日志记录中间件，记录请求和响应信息。

```go
type LoggingMiddleware struct {
    *BaseMiddleware
    logger func(string)
}

// NewLoggingMiddleware 创建日志中间件
// logger 为 nil 时使用默认 log.Printf
func NewLoggingMiddleware(logger func(string)) *LoggingMiddleware
```

**功能：**
- 记录请求时间和输入
- 记录响应时间和输出
- 记录错误信息

**使用示例：**

```go
// 使用默认 logger
loggingMW := middleware.NewLoggingMiddleware(nil)

// 使用自定义 logger
customLoggingMW := middleware.NewLoggingMiddleware(func(msg string) {
    zap.L().Info(msg)
})
```

### TimingMiddleware

计时中间件，记录执行耗时。

```go
type TimingMiddleware struct {
    *BaseMiddleware
    timings map[string]time.Duration
    mu      sync.RWMutex
}

// NewTimingMiddleware 创建计时中间件
func NewTimingMiddleware() *TimingMiddleware

// GetTimings 获取所有计时记录
func (m *TimingMiddleware) GetTimings() map[string]time.Duration

// GetAverageLatency 获取平均延迟
func (m *TimingMiddleware) GetAverageLatency() time.Duration

// Reset 重置计时器
func (m *TimingMiddleware) Reset()
```

**使用示例：**

```go
timingMW := middleware.NewTimingMiddleware()

// 执行一些操作后...

// 获取平均延迟
avgLatency := timingMW.GetAverageLatency()
fmt.Printf("Average latency: %v\n", avgLatency)

// 获取所有计时
timings := timingMW.GetTimings()
for id, duration := range timings {
    fmt.Printf("%s: %v\n", id, duration)
}
```

### RetryMiddleware

重试中间件，在错误时自动重试。

```go
type RetryMiddleware struct {
    *BaseMiddleware
    maxRetries int
    backoff    time.Duration
    condition  func(error) bool
}

// NewRetryMiddleware 创建重试中间件
func NewRetryMiddleware(maxRetries int, backoff time.Duration) *RetryMiddleware

// WithRetryCondition 设置重试条件
func (m *RetryMiddleware) WithRetryCondition(condition func(error) bool) *RetryMiddleware
```

**使用示例：**

```go
retryMW := middleware.NewRetryMiddleware(3, 100*time.Millisecond).
    WithRetryCondition(func(err error) bool {
        // 只重试超时错误
        return errors.Is(err, context.DeadlineExceeded)
    })
```

### CacheMiddleware

缓存中间件，缓存请求结果。

```go
type CacheMiddleware struct {
    *BaseMiddleware
    cache     *ShardedCache
    ttl       time.Duration
    keyFunc   func(*MiddlewareRequest) string
}

// NewCacheMiddleware 创建缓存中间件
func NewCacheMiddleware(ttl time.Duration) *CacheMiddleware

// NewCacheMiddlewareWithShards 创建带分片的缓存中间件
func NewCacheMiddlewareWithShards(ttl time.Duration, numShards uint32) *CacheMiddleware

// WithKeyFunc 设置缓存键生成函数
func (m *CacheMiddleware) WithKeyFunc(fn func(*MiddlewareRequest) string) *CacheMiddleware

// Clear 清空缓存
func (m *CacheMiddleware) Clear()

// Size 返回缓存大小
func (m *CacheMiddleware) Size() int
```

**使用示例：**

```go
// 基础用法
cacheMW := middleware.NewCacheMiddleware(5 * time.Minute)

// 带分片（高并发场景）
shardedCacheMW := middleware.NewCacheMiddlewareWithShards(5*time.Minute, 32)

// 自定义缓存键
customCacheMW := middleware.NewCacheMiddleware(5*time.Minute).
    WithKeyFunc(func(req *MiddlewareRequest) string {
        return fmt.Sprintf("%s:%v", req.Input, req.Metadata["session_id"])
    })
```

### RateLimitMiddleware

速率限制中间件。

```go
type RateLimitMiddleware struct {
    *BaseMiddleware
    limiter *rate.Limiter
}

// NewRateLimitMiddleware 创建速率限制中间件
// rps: 每秒请求数
func NewRateLimitMiddleware(rps float64) *RateLimitMiddleware

// NewRateLimitMiddlewareWithBurst 创建带突发的速率限制中间件
func NewRateLimitMiddlewareWithBurst(rps float64, burst int) *RateLimitMiddleware
```

**使用示例：**

```go
// 每秒 10 个请求
rateLimitMW := middleware.NewRateLimitMiddleware(10)

// 每秒 10 个请求，允许突发 20 个
burstRateLimitMW := middleware.NewRateLimitMiddlewareWithBurst(10, 20)
```

### MiddlewareFunc

函数式中间件，快速创建简单中间件。

```go
type MiddlewareFunc struct {
    name    string
    before  func(context.Context, *MiddlewareRequest) (*MiddlewareRequest, error)
    after   func(context.Context, *MiddlewareResponse) (*MiddlewareResponse, error)
    onError func(context.Context, error) error
}

// NewMiddlewareFunc 创建函数式中间件
func NewMiddlewareFunc(
    name string,
    before func(context.Context, *MiddlewareRequest) (*MiddlewareRequest, error),
    after func(context.Context, *MiddlewareResponse) (*MiddlewareResponse, error),
    onError func(context.Context, error) error,
) *MiddlewareFunc
```

**使用示例：**

```go
// 创建简单的前置处理中间件
addHeaderMW := middleware.NewMiddlewareFunc(
    "add_header",
    func(ctx context.Context, req *MiddlewareRequest) (*MiddlewareRequest, error) {
        if req.Headers == nil {
            req.Headers = make(map[string]string)
        }
        req.Headers["X-Request-ID"] = uuid.New().String()
        return req, nil
    },
    nil, // 无后置处理
    nil, // 无错误处理
)
```

---

## 自定义中间件

### 实现步骤

1. 嵌入 `BaseMiddleware` 或实现完整接口
2. 实现所需的方法
3. 注册到中间件链

### 示例：认证中间件

```go
package mymiddleware

import (
    "context"
    "errors"
    "github.com/kart-io/goagent/core/middleware"
)

type AuthMiddleware struct {
    *middleware.BaseMiddleware
    validateToken func(string) bool
}

func NewAuthMiddleware(validator func(string) bool) *AuthMiddleware {
    return &AuthMiddleware{
        BaseMiddleware: middleware.NewBaseMiddleware("auth"),
        validateToken:  validator,
    }
}

func (m *AuthMiddleware) OnBefore(ctx context.Context, req *middleware.MiddlewareRequest) (*middleware.MiddlewareRequest, error) {
    token, ok := req.Headers["Authorization"]
    if !ok {
        return nil, errors.New("missing authorization header")
    }

    if !m.validateToken(token) {
        return nil, errors.New("invalid token")
    }

    // 添加用户信息到元数据
    req.Metadata["authenticated"] = true
    return req, nil
}

func (m *AuthMiddleware) OnError(ctx context.Context, err error) error {
    // 记录认证失败
    log.Printf("Auth error: %v", err)
    return err
}
```

### 示例：监控中间件

```go
package mymiddleware

import (
    "context"
    "time"
    "github.com/kart-io/goagent/core/middleware"
    "github.com/prometheus/client_golang/prometheus"
)

type MetricsMiddleware struct {
    *middleware.BaseMiddleware
    requestCounter  prometheus.Counter
    latencyHistogram prometheus.Histogram
}

func NewMetricsMiddleware() *MetricsMiddleware {
    return &MetricsMiddleware{
        BaseMiddleware: middleware.NewBaseMiddleware("metrics"),
        requestCounter: prometheus.NewCounter(prometheus.CounterOpts{
            Name: "goagent_requests_total",
        }),
        latencyHistogram: prometheus.NewHistogram(prometheus.HistogramOpts{
            Name:    "goagent_request_duration_seconds",
            Buckets: prometheus.DefBuckets,
        }),
    }
}

func (m *MetricsMiddleware) OnBefore(ctx context.Context, req *middleware.MiddlewareRequest) (*middleware.MiddlewareRequest, error) {
    m.requestCounter.Inc()
    req.Metadata["start_time"] = time.Now()
    return req, nil
}

func (m *MetricsMiddleware) OnAfter(ctx context.Context, resp *middleware.MiddlewareResponse) (*middleware.MiddlewareResponse, error) {
    if startTime, ok := resp.Metadata["start_time"].(time.Time); ok {
        duration := time.Since(startTime).Seconds()
        m.latencyHistogram.Observe(duration)
    }
    return resp, nil
}
```

### 示例：熔断中间件

```go
package mymiddleware

import (
    "context"
    "errors"
    "sync"
    "time"
    "github.com/kart-io/goagent/core/middleware"
)

type CircuitBreakerMiddleware struct {
    *middleware.BaseMiddleware
    mu              sync.RWMutex
    failures        int
    threshold       int
    resetTimeout    time.Duration
    lastFailureTime time.Time
    isOpen          bool
}

func NewCircuitBreakerMiddleware(threshold int, resetTimeout time.Duration) *CircuitBreakerMiddleware {
    return &CircuitBreakerMiddleware{
        BaseMiddleware: middleware.NewBaseMiddleware("circuit_breaker"),
        threshold:      threshold,
        resetTimeout:   resetTimeout,
    }
}

func (m *CircuitBreakerMiddleware) OnBefore(ctx context.Context, req *middleware.MiddlewareRequest) (*middleware.MiddlewareRequest, error) {
    m.mu.RLock()
    defer m.mu.RUnlock()

    if m.isOpen {
        if time.Since(m.lastFailureTime) > m.resetTimeout {
            // 半开状态，允许尝试
            return req, nil
        }
        return nil, errors.New("circuit breaker is open")
    }
    return req, nil
}

func (m *CircuitBreakerMiddleware) OnError(ctx context.Context, err error) error {
    m.mu.Lock()
    defer m.mu.Unlock()

    m.failures++
    m.lastFailureTime = time.Now()

    if m.failures >= m.threshold {
        m.isOpen = true
    }

    return err
}

func (m *CircuitBreakerMiddleware) OnAfter(ctx context.Context, resp *middleware.MiddlewareResponse) (*middleware.MiddlewareResponse, error) {
    m.mu.Lock()
    defer m.mu.Unlock()

    // 成功请求，重置计数
    m.failures = 0
    m.isOpen = false

    return resp, nil
}
```

---

## 对象池

为了减少 GC 压力，中间件系统提供对象池。

```go
// 获取请求对象
func GetMiddlewareRequest() *MiddlewareRequest

// 归还请求对象
func PutMiddlewareRequest(req *MiddlewareRequest)

// 获取响应对象
func GetMiddlewareResponse() *MiddlewareResponse

// 归还响应对象
func PutMiddlewareResponse(resp *MiddlewareResponse)
```

**使用示例：**

```go
// 从池中获取
req := middleware.GetMiddlewareRequest()
defer middleware.PutMiddlewareRequest(req)

// 设置字段
req.Input = input
req.Timestamp = time.Now()

// 执行
resp, err := chain.Execute(ctx, req)
```

---

## 中间件执行顺序

中间件按添加顺序执行：

1. **请求阶段（OnBefore）**：按添加顺序执行
2. **响应阶段（OnAfter）**：按添加顺序的逆序执行
3. **错误阶段（OnError）**：按添加顺序的逆序执行

```go
chain := middleware.NewMiddlewareChain(handler).
    Use(mw1).  // 第一个
    Use(mw2).  // 第二个
    Use(mw3)   // 第三个

// 执行顺序：
// Request  → mw1.OnBefore → mw2.OnBefore → mw3.OnBefore → Handler
// Response ← mw1.OnAfter  ← mw2.OnAfter  ← mw3.OnAfter  ← Handler
```

---

## 相关文档

- [核心 API 参考](CORE_API.md)
- [工具中间件指南](../guides/TOOL_MIDDLEWARE.md)
- [设计概述](../design/DESIGN_OVERVIEW.md)
