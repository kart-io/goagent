# 中间件与可观测性示例

本示例演示工具中间件的使用，包括日志、缓存、限流中间件，以及自定义中间件和可观测性指标收集。

## 目录

- [架构设计](#架构设计)
- [核心组件](#核心组件)
- [执行流程](#执行流程)
- [使用方法](#使用方法)
- [API 参考](#api-参考)

## 架构设计

### 中间件架构

```mermaid
graph TB
    subgraph Request["请求流程"]
        Req[请求] --> MW1[日志中间件]
        MW1 --> MW2[缓存中间件]
        MW2 --> MW3[限流中间件]
        MW3 --> Tool[工具执行]
    end

    subgraph Response["响应流程"]
        Tool --> R3[限流后处理]
        R3 --> R2[缓存后处理]
        R2 --> R1[日志后处理]
        R1 --> Resp[响应]
    end

    subgraph Middleware["中间件层"]
        Logging[LoggingMiddleware]
        Caching[CachingMiddleware]
        RateLimit[RateLimitMiddleware]
        Custom[自定义中间件]
    end

    style Req fill:#e3f2fd
    style Resp fill:#c8e6c9
    style Tool fill:#fff9c4
```

### 中间件接口体系

```mermaid
classDiagram
    class ToolMiddleware {
        <<interface>>
        +OnBeforeInvoke(ctx, tool, input) tuple~ctx, error~
        +OnAfterInvoke(ctx, tool, input, output) error
        +OnError(ctx, tool, input, err) error
    }

    class BaseToolMiddleware {
        +OnBeforeInvoke() tuple~ctx, nil~
        +OnAfterInvoke() nil
        +OnError() err
    }

    class LoggingMiddleware {
        -logger Logger
        -level string
        +WithLogger(logger) Option
        +WithLogLevel(level) Option
    }

    class CachingMiddleware {
        -cache Cache
        -ttl Duration
        +WithCacheTTL(ttl) Option
    }

    class RateLimitMiddleware {
        -rate float64
        -burst int
        +WithRate(rate) Option
        +WithBurst(burst) Option
    }

    class TimingMiddleware {
        -lastDuration Duration
        +GetLastDuration() Duration
    }

    class RetryMiddleware {
        -maxRetries int
        -delay Duration
    }

    class MetricsMiddleware {
        -totalCalls int64
        -successCalls int64
        -failedCalls int64
        +GetStats() MetricsStats
    }

    ToolMiddleware <|.. BaseToolMiddleware : 实现
    BaseToolMiddleware <|-- LoggingMiddleware : 继承
    BaseToolMiddleware <|-- CachingMiddleware : 继承
    BaseToolMiddleware <|-- RateLimitMiddleware : 继承
    BaseToolMiddleware <|-- TimingMiddleware : 继承
    BaseToolMiddleware <|-- RetryMiddleware : 继承
    BaseToolMiddleware <|-- MetricsMiddleware : 继承
```

### 洋葱模型执行流程

```mermaid
sequenceDiagram
    participant Req as 请求
    participant MW1 as 日志中间件
    participant MW2 as 缓存中间件
    participant MW3 as 限流中间件
    participant Tool as 工具

    Req->>MW1: OnBeforeInvoke
    MW1->>MW2: OnBeforeInvoke
    MW2->>MW3: OnBeforeInvoke
    MW3->>Tool: Invoke()

    Tool-->>MW3: Output
    MW3-->>MW3: OnAfterInvoke
    MW3-->>MW2: Output
    MW2-->>MW2: OnAfterInvoke
    MW2-->>MW1: Output
    MW1-->>MW1: OnAfterInvoke
    MW1-->>Req: 响应
```

## 核心组件

### 1. 日志中间件 (LoggingMiddleware)

```mermaid
graph LR
    subgraph LoggingMW["日志中间件"]
        Before[OnBeforeInvoke]
        After[OnAfterInvoke]
        Error[OnError]
    end

    subgraph Actions["日志动作"]
        LogInput[记录输入参数]
        LogOutput[记录输出结果]
        LogError[记录错误信息]
        LogTiming[记录耗时]
    end

    Before --> LogInput
    After --> LogOutput
    After --> LogTiming
    Error --> LogError

    style Before fill:#e3f2fd
    style After fill:#c8e6c9
    style Error fill:#ffcdd2
```

### 2. 缓存中间件 (CachingMiddleware)

```mermaid
flowchart TD
    Start[接收请求] --> GenKey[生成缓存键]
    GenKey --> CheckCache{缓存命中?}

    CheckCache --> |是| ReturnCache[返回缓存结果]
    CheckCache --> |否| Execute[执行工具]

    Execute --> Success{执行成功?}
    Success --> |是| SetCache[存入缓存]
    Success --> |否| ReturnError[返回错误]

    SetCache --> Return[返回结果]
    ReturnCache --> Return

    style ReturnCache fill:#c8e6c9
    style SetCache fill:#fff9c4
```

### 3. 限流中间件 (RateLimitMiddleware)

```mermaid
graph TB
    subgraph TokenBucket["令牌桶算法"]
        Bucket[令牌桶]
        Rate[填充速率]
        Burst[桶容量]
    end

    subgraph Flow["请求流程"]
        Request[请求到达]
        Check{有可用令牌?}
        Take[获取令牌]
        Reject[拒绝请求]
        Execute[执行请求]
    end

    Rate --> Bucket
    Burst --> Bucket
    Request --> Check
    Check --> |是| Take
    Check --> |否| Reject
    Take --> Execute

    style Execute fill:#c8e6c9
    style Reject fill:#ffcdd2
```

### 4. 中间件链 (Chain)

```mermaid
graph LR
    subgraph Chain["middleware.Chain()"]
        MW1[中间件 1]
        MW2[中间件 2]
        MW3[中间件 3]
    end

    subgraph Result["组合结果"]
        Combined[组合中间件]
    end

    MW1 --> Combined
    MW2 --> Combined
    MW3 --> Combined

    style Combined fill:#e3f2fd
```

## 执行流程

### 场景 1: 日志中间件执行流程

```mermaid
sequenceDiagram
    participant App as 应用
    participant LM as LoggingMiddleware
    participant Tool as 工具
    participant Logger as 日志器

    App->>LM: Invoke(input)
    LM->>Logger: 记录: 开始调用 [tool_name]
    LM->>Logger: 记录: 输入参数 {...}

    LM->>Tool: Invoke(input)
    Tool-->>LM: output

    LM->>Logger: 记录: 调用完成，耗时 Xms
    LM->>Logger: 记录: 输出结果 {...}

    LM-->>App: output
```

### 场景 2: 缓存中间件执行流程

```mermaid
sequenceDiagram
    participant App as 应用
    participant CM as CachingMiddleware
    participant Cache as 缓存
    participant Tool as 工具

    App->>CM: Invoke(input)
    CM->>CM: 生成缓存键

    CM->>Cache: Get(key)

    alt 缓存命中
        Cache-->>CM: cached_result
        CM-->>App: cached_result (快速返回)
    else 缓存未命中
        Cache-->>CM: nil
        CM->>Tool: Invoke(input)
        Tool-->>CM: output
        CM->>Cache: Set(key, output, TTL)
        CM-->>App: output
    end
```

### 场景 3: 限流中间件执行流程

```mermaid
flowchart TD
    Start[请求到达] --> CheckToken{检查令牌}

    CheckToken --> |有令牌| Acquire[获取令牌]
    CheckToken --> |无令牌| Wait{等待策略}

    Wait --> |超时| Reject[拒绝请求]
    Wait --> |获得令牌| Acquire

    Acquire --> Execute[执行工具]
    Execute --> Return[返回结果]

    style Return fill:#c8e6c9
    style Reject fill:#ffcdd2
```

### 场景 4: 中间件链组合

```mermaid
flowchart LR
    subgraph Before["OnBeforeInvoke 顺序"]
        B1[日志] --> B2[认证]
        B2 --> B3[缓存]
        B3 --> B4[限流]
    end

    B4 --> Tool[工具执行]

    subgraph After["OnAfterInvoke 顺序"]
        A4[限流] --> A3[缓存]
        A3 --> A2[认证]
        A2 --> A1[日志]
    end

    Tool --> A4

    style Tool fill:#fff9c4
```

## 使用方法

### 运行示例

```bash
cd examples/tools/middleware
go run main.go
```

### 预期输出

```text
╔════════════════════════════════════════════════════════════════╗
║          中间件与可观测性示例                                   ║
║   展示日志、缓存、限流中间件和可观测性指标收集                   ║
╚════════════════════════════════════════════════════════════════╝

【场景 1】日志中间件
════════════════════════════════════════════════════════════════

场景描述: 展示日志中间件记录工具调用的输入输出

1. 创建带日志中间件的工具
────────────────────────────────────────
  原始工具: calculator
  包装后工具: calculator

2. 执行工具调用
────────────────────────────────────────
  ✓ 调用成功: map[result:60]

3. 日志记录
────────────────────────────────────────
  [INFO] 工具调用开始: calculator
  [INFO] 工具调用完成: calculator, 耗时: 0ms

【场景 2】缓存中间件
════════════════════════════════════════════════════════════════

1. 首次调用（缓存未命中）
  结果: map[input:10 result:100]
  耗时: 100.5ms
  实际执行次数: 1

2. 第二次调用（相同参数，缓存命中）
  结果: map[input:10 result:100]
  耗时: 50µs
  实际执行次数: 1 (未增加，使用缓存)

3. 缓存效果统计
  缓存命中率: 33.3%

【场景 3】限流中间件
════════════════════════════════════════════════════════════════

1. 限流配置
  速率限制: 2 请求/秒
  突发容量: 3 请求

2. 快速连续调用测试
  请求 1: ✓ 成功
  请求 2: ✓ 成功
  请求 3: ✓ 成功
  请求 4: ✗ 被限流
  请求 5: ✗ 被限流
  请求 6: ✗ 被限流

【场景 4】中间件链式组合
════════════════════════════════════════════════════════════════

执行顺序记录:
  1. 日志中间件: OnBeforeInvoke
  2. 认证中间件: OnBeforeInvoke
  3. 缓存中间件: OnBeforeInvoke
  4. 工具执行
  5. 缓存中间件: OnAfterInvoke
  6. 认证中间件: OnAfterInvoke
  7. 日志中间件: OnAfterInvoke

【场景 5】自定义中间件
════════════════════════════════════════════════════════════════

1. 计时中间件
  [计时] slow_operation 执行耗时: 150ms

2. 重试中间件
  [重试] 第 1 次重试 (原因: 模拟失败)
  [重试] 第 2 次重试成功
  ✓ 最终成功: 成功 (经过 3 次尝试)

3. 指标收集中间件
  总调用次数: 5
  成功次数: 5
  失败次数: 0
  平均耗时: 50µs

【场景 6】可观测性指标收集
════════════════════════════════════════════════════════════════

3. 可观测性报告
  ┌─────────────────┬────────┬────────┬────────┬──────────┬──────────┐
  │ 工具名称        │ 总调用 │ 成功   │ 失败   │ 平均耗时 │ 最大耗时 │
  ├─────────────────┼────────┼────────┼────────┼──────────┼──────────┤
  │ calculator      │     10 │     10 │      0 │     50µs │    100µs │
  │ text_processor  │      5 │      5 │      0 │     30µs │     60µs │
  │ datetime        │      3 │      3 │      0 │     20µs │     40µs │
  └─────────────────┴────────┴────────┴────────┴──────────┴──────────┘

4. 告警检查
  ✓ 无告警
```

## API 参考

### LoggingMiddleware（接口式中间件）

| 选项 | 说明 | 默认值 |
|------|------|--------|
| `WithLogger(logger)` | 设置日志记录器 | nil |

```go
loggingMW := middleware.NewLoggingMiddleware(
    middleware.WithLogger(logger),
)

// 应用到工具
wrappedTool := tools.WithMiddleware(tool, loggingMW)
```

### CachingMiddleware（函数式中间件）

| 选项 | 说明 | 默认值 |
|------|------|--------|
| `WithTTL(ttl)` | 设置缓存过期时间 | 5 分钟 |
| `WithCache(cache)` | 设置自定义缓存实现 | 内置缓存 |

```go
// 使用默认缓存
cachingMW := middleware.Caching(
    middleware.WithTTL(10*time.Minute),
)

// 使用自定义缓存
cachingMW := middleware.Caching(
    middleware.WithCache(customCache),
    middleware.WithTTL(5*time.Minute),
)

// 应用到工具
wrappedTool := tools.WithMiddleware(tool, cachingMW)
```

### RateLimitMiddleware（函数式中间件）

| 选项 | 说明 | 默认值 |
|------|------|--------|
| `WithQPS(qps)` | 设置每秒请求数 | 10 |
| `WithBurst(burst)` | 设置突发容量 | 20 |

```go
rateLimitMW := middleware.RateLimit(
    middleware.WithQPS(5),
    middleware.WithBurst(10),
)

// 应用到工具
wrappedTool := tools.WithMiddleware(tool, rateLimitMW)
```

### 中间件组合

```go
// 使用 WithMiddleware 组合多个中间件（推荐）
// 执行顺序：mw1 → mw2 → mw3 → 工具 → mw3 → mw2 → mw1
wrappedTool := tools.WithMiddleware(tool, mw1, mw2, mw3)

// 也可以逐层包装
tool1 := tools.WithMiddleware(tool, mw3)
tool2 := tools.WithMiddleware(tool1, mw2)
wrappedTool := tools.WithMiddleware(tool2, mw1)
```

### 自定义中间件（函数式实现，推荐）

```go
// 使用 ToolMiddlewareFunc 类型创建自定义中间件
func createTimingMiddleware() middleware.ToolMiddlewareFunc {
    return func(tool interfaces.Tool, next middleware.ToolInvoker) middleware.ToolInvoker {
        return func(ctx context.Context, input *interfaces.ToolInput) (*interfaces.ToolOutput, error) {
            // 前置处理
            start := time.Now()

            // 调用下一个中间件或工具
            output, err := next(ctx, input)

            // 后置处理
            duration := time.Since(start)
            fmt.Printf("[计时] %s 执行耗时: %v\n", tool.Name(), duration)

            return output, err
        }
    }
}

// 使用自定义中间件
timingMW := createTimingMiddleware()
wrappedTool := tools.WithMiddleware(tool, timingMW)
```

### 自定义中间件（接口式实现）

```go
// 实现 ToolMiddleware 接口
type CustomMiddleware struct{}

func (m *CustomMiddleware) Wrap(tool interfaces.Tool, next middleware.ToolInvoker) middleware.ToolInvoker {
    return func(ctx context.Context, input *interfaces.ToolInput) (*interfaces.ToolOutput, error) {
        // 前置处理
        fmt.Println("Before invoke")

        // 调用下一个中间件或工具
        output, err := next(ctx, input)

        // 后置处理（包含错误处理）
        if err != nil {
            fmt.Printf("Error: %v\n", err)
        } else {
            fmt.Println("After invoke: success")
        }

        return output, err
    }
}

// 使用自定义中间件
customMW := &CustomMiddleware{}
wrappedTool := tools.WithMiddleware(tool, customMW)
```

## 扩展阅读

- [LLM 工具调用示例](../../multiagent/06-llm-tool-calling/)
- [LLM 高级用法示例](../../llm/advanced/)
- [工具注册与执行示例](../registry/)
- [tools/middleware 包文档](../../../tools/middleware/) - 中间件 API 参考
