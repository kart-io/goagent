# 工具中间件（Tool Middleware）

工具中间件提供了一种强大而灵活的方式来为工具调用添加横切关注点（cross-cutting concerns），如日志记录、缓存、限流等。

## 核心概念

### 中间件架构

GoAgent 的工具中间件采用**洋葱模型**（Onion Model）设计，允许多个中间件层层包装：

```
Logging (外层)
  └─> Caching (中层)
        └─> RateLimit (内层)
              └─> 实际工具调用
```

执行流程：
1. 请求从外向内穿过所有中间件
2. 到达实际工具执行
3. 响应从内向外返回，经过所有中间件

### 两种实现方式

#### 1. 函数式中间件（推荐）

```go
type ToolMiddlewareFunc func(interfaces.Tool, ToolInvoker) ToolInvoker
```

**优点**：
- 简洁，易于组合
- 可以直接在函数内短路执行（如缓存命中时）
- 更容易实现复杂的控制流

**示例**：
```go
cachingMW := middleware.Caching(
    middleware.WithTTL(5 * time.Minute),
)

rateLimitMW := middleware.RateLimit(
    middleware.WithQPS(10),
    middleware.WithBurst(20),
)
```

#### 2. 接口式中间件（旧接口）

```go
type ToolMiddleware interface {
    OnBeforeInvoke(ctx, tool, input) (*ToolInput, error)
    OnAfterInvoke(ctx, tool, output) (*ToolOutput, error)
    OnError(ctx, tool, err) error
}
```

**特点**：
- 分离前置/后置处理逻辑
- 适合简单的装饰器模式
- 已被函数式中间件取代，但仍然支持

## 内置中间件

### 1. Logging（日志记录）

记录工具调用的输入、输出和执行时间。

**功能**：
- 自动记录工具名称、参数、结果
- 可配置是否记录敏感数据
- 记录执行耗时
- 区分成功/失败日志级别

**使用示例**：
```go
loggingMW := middleware.NewLoggingMiddleware(
    middleware.WithLogger(myLogger),
    middleware.WithoutInputLogging(),  // 不记录输入参数
    middleware.WithMaxArgBytes(512),   // 限制参数日志大小
)
```

**输出示例**：
```json
{
  "level": "INFO",
  "message": "Tool invocation completed",
  "tool": "calculator",
  "success": true,
  "duration_ms": 15,
  "result": "{\"sum\": 42}"
}
```

### 2. Caching（缓存）

缓存成功的工具调用结果，避免重复计算。

**功能**：
- 基于工具名和参数生成缓存键
- 只缓存成功的结果
- 支持 TTL 过期
- 可自定义缓存键生成函数
- 缓存命中时**不调用实际工具**（性能优化）

**使用示例**：
```go
import (
    "github.com/kart-io/goagent/cache"
    "github.com/kart-io/goagent/tools/middleware"
)

// 创建缓存实例（推荐使用 SimpleCache）
cacheInstance := cache.NewSimpleCache(10 * time.Minute)

// 配置缓存中间件
cachingMW := middleware.Caching(
    middleware.WithCache(cacheInstance),
    middleware.WithTTL(10 * time.Minute),
    middleware.WithCacheKeyFunc(customKeyFunc),  // 可选：自定义键函数
)
```

> **💡 提示**：推荐使用 `cache.NewSimpleCache()` 作为默认缓存实现。
> 详细的缓存使用指南请参考 [CACHING_GUIDE.md](./CACHING_GUIDE.md)。

**元数据**：
```go
output.Metadata["cache_hit"]    // true/false
output.Metadata["cache_stored"] // true (仅首次调用)
```

**注意事项**：
- 缓存键默认为 `tool:<name>:<sha256(args)[:8]>`
- 内部元数据（`__` 前缀）不影响缓存键
- 完全并发安全（基于 sync.Map 或 sync.RWMutex）
- 仅缓存成功结果（错误不会被缓存）

### 3. RateLimit（限流）

限制工具调用的速率，防止过载。

**功能**：
- 基于令牌桶算法
- 支持全局限流或按工具限流
- 可配置 QPS 和突发容量
- 可选的等待超时

**使用示例**：
```go
rateLimitMW := middleware.RateLimit(
    middleware.WithQPS(10),           // 每秒 10 个请求
    middleware.WithBurst(20),         // 允许突发 20 个
    middleware.WithPerToolRateLimit(), // 每个工具独立限流
    middleware.WithWaitTimeout(1 * time.Second), // 等待令牌
)
```

**限流模式**：
- **全局限流**：所有工具共享配额
- **按工具限流**：每个工具独立配额

**错误处理**：
```go
// 限流拒绝时返回错误
err := errors.Wrap(
    fmt.Errorf("rate limit exceeded"),
    errors.CodeMiddlewareExecution,
    "rate limit exceeded",
)
```

## 使用方法

### 基本用法

```go
import (
    "github.com/kart-io/goagent/tools"
    "github.com/kart-io/goagent/tools/middleware"
)

// 创建原始工具
calculator := NewCalculatorTool()

// 应用中间件
wrappedTool := tools.WithMiddleware(calculator,
    middleware.NewLoggingMiddleware(),     // 最外层
    middleware.Caching(),                  // 中层
    middleware.RateLimit(
        middleware.WithQPS(10),
        middleware.WithBurst(5),
    ),
)

// 调用工具
output, err := wrappedTool.Invoke(ctx, &interfaces.ToolInput{
    Args: map[string]interface{}{"a": 1, "b": 2},
})
```

### 中间件执行顺序

**重要**：中间件按照传入顺序从外到内包装，但执行顺序遵循洋葱模型：

```go
tools.WithMiddleware(tool,
    logging,  // 1. 最外层，最先执行前置逻辑，最后执行后置逻辑
    caching,  // 2. 中层
    rateLimit, // 3. 最内层，最后执行前置逻辑，最先执行后置逻辑
)
```

**执行流程**：
```
Request:
  logging.OnBeforeInvoke
    → caching (检查缓存)
      → rateLimit (检查限流)
        → 实际工具调用

Response:
        ← rateLimit.OnAfterInvoke
      ← caching (存储结果)
    ← logging.OnAfterInvoke (记录耗时)
```

### 缓存 + 限流的最佳实践

**推荐顺序**：缓存在外，限流在内

```go
wrappedTool := tools.WithMiddleware(tool,
    middleware.Caching(),    // 外层：缓存命中时直接返回
    middleware.RateLimit(),  // 内层：只对缓存未命中的请求限流
)
```

**原理**：
- 缓存命中时，不会执行内层的限流中间件
- 节省限流配额，提高性能

### 错误处理

```go
output, err := wrappedTool.Invoke(ctx, input)
if err != nil {
    // 检查错误类型
    if errors.IsCode(err, errors.CodeMiddlewareExecution) {
        // 可能是限流错误
        log.Warn("Tool call rate limited", "error", err)
    } else {
        // 其他错误
        log.Error("Tool call failed", "error", err)
    }
}
```

## 自定义中间件

### 函数式中间件（推荐）

```go
func CustomMiddleware(config CustomConfig) middleware.ToolMiddlewareFunc {
    return func(tool interfaces.Tool, next middleware.ToolInvoker) middleware.ToolInvoker {
        return func(ctx context.Context, input *interfaces.ToolInput) (*interfaces.ToolOutput, error) {
            // 前置处理
            log.Info("Before tool call", "tool", tool.Name())

            // 调用下一层
            output, err := next(ctx, input)
            if err != nil {
                return nil, err
            }

            // 后置处理
            log.Info("After tool call", "result", output.Result)

            return output, nil
        }
    }
}
```

### 接口式中间件（旧接口）

```go
type CustomMiddleware struct {
    *middleware.BaseToolMiddleware
    config CustomConfig
}

func (m *CustomMiddleware) OnBeforeInvoke(
    ctx context.Context,
    tool interfaces.Tool,
    input *interfaces.ToolInput,
) (*interfaces.ToolInput, error) {
    // 前置处理
    log.Info("Before tool call", "tool", tool.Name())
    return input, nil
}

func (m *CustomMiddleware) OnAfterInvoke(
    ctx context.Context,
    tool interfaces.Tool,
    output *interfaces.ToolOutput,
) (*interfaces.ToolOutput, error) {
    // 后置处理
    log.Info("After tool call", "result", output.Result)
    return output, nil
}
```

## 性能考虑

### 缓存性能

- **缓存命中率**：通常 >90%
- **性能提升**：缓存命中时避免实际工具调用，提升 10-100 倍
- **内存占用**：LRU 缓存自动淘汰旧条目

### 限流性能

- **令牌桶算法**：O(1) 时间复杂度
- **并发安全**：使用 `golang.org/x/time/rate`，性能优异
- **开销**：<5% 额外开销

### 日志性能

- **序列化开销**：限制参数大小（默认 1KB）
- **异步日志**：使用支持异步的 logger 减少延迟

## 测试

### 单元测试

```go
func TestToolWithMiddleware(t *testing.T) {
    tool := NewMockTool()

    middleware := middleware.Caching()
    wrapped := tools.WithMiddleware(tool, middleware)

    // 第一次调用
    output1, _ := wrapped.Invoke(ctx, input)
    assert.Equal(t, 1, tool.CallCount)

    // 第二次调用（缓存命中）
    output2, _ := wrapped.Invoke(ctx, input)
    assert.Equal(t, 1, tool.CallCount) // 不应再次调用工具
    assert.True(t, output2.Metadata["cache_hit"])
}
```

### 集成测试

参考 `examples/basic/middleware/` 中的完整示例。

## 常见问题

### Q1: 缓存未生效？

**检查项**：
1. 参数是否完全相同（包括顺序）
2. 是否只缓存成功结果（`output.Success = true`）
3. TTL 是否过期

### Q2: 限流太严格？

**调整方法**：
- 增加 `Burst` 容量
- 降低 `QPS` 限制
- 使用 `WithWaitTimeout` 允许等待

### Q3: 中间件顺序重要吗？

**是的！** 缓存应该在限流外层，以避免浪费限流配额。

### Q4: 如何禁用中间件？

```go
// 不传入中间件即可
wrappedTool := tools.WithMiddleware(tool) // 返回原始工具
```

## 参考

- [Middleware 接口定义](https://github.com/kart-io/goagent/blob/master/tools/middleware/middleware.go)
- [Caching 实现](https://github.com/kart-io/goagent/blob/master/tools/middleware/caching.go)
- [RateLimit 实现](https://github.com/kart-io/goagent/blob/master/tools/middleware/rate_limit.go)
- [示例代码](https://github.com/kart-io/goagent/tree/master/examples/basic/middleware)