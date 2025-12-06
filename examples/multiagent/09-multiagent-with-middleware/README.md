# 09-multiagent-with-middleware 多智能体中间件示例

本示例演示多智能体系统中使用工具中间件进行增强和监控。

## 目录

- [架构设计](#架构设计)
- [核心组件](#核心组件)
- [执行流程](#执行流程)
- [使用方法](#使用方法)
- [代码结构](#代码结构)

## 架构设计

### 系统架构图

```mermaid
graph TB
    subgraph MultiAgentSystem[MultiAgentSystem]
        Registry[Agent Registry]
        MessageBus[Message Bus]
    end

    subgraph MiddlewareChain[Middleware Chain]
        LoggingMW[LoggingMiddleware]
        MetricsMW[MetricsMiddleware]
        TrackingMW[TrackingMiddleware]
        RestrictionMW[RestrictionMiddleware]
    end

    subgraph BaseTool[Base Tools]
        Calculator[Calculator]
        TextProcessor[TextProcessor]
    end

    subgraph Agents[MiddlewareAgent]
        Agent1[agent-1]
        Agent2[agent-2]
        AdminAgent[admin-agent]
        WorkerAgent[worker-agent]
    end

    Agent1 --> LoggingMW
    Agent2 --> LoggingMW
    LoggingMW --> Calculator
    LoggingMW --> TextProcessor

    AdminAgent --> TrackingMW
    WorkerAgent --> TrackingMW
    WorkerAgent --> RestrictionMW
    TrackingMW --> Calculator
    RestrictionMW --> Calculator
```

### 组件关系图

```mermaid
classDiagram
    class Tool {
        +Name() string
        +Description() string
        +Invoke(ctx, input) Output
    }

    class ToolMiddlewareFunc {
        +Apply(tool, next) ToolInvoker
    }

    class LogCollector {
        -logs string[]
        +Log(message)
        +GetLogs() string[]
    }

    class MetricsCollector {
        -totalCalls int64
        -successCalls int64
        -failedCalls int64
        +RecordCall(success, duration)
        +GetStats() MetricsStats
    }

    class AgentTracker {
        -traces string[]
        +Track(agentID, toolName, args, success)
        +GetTraces() string[]
    }

    class MiddlewareAgent {
        -tool Tool
        +ExecuteTool(ctx, args) interface
        +Collaborate(ctx, task) Assignment
    }

    class BaseCollaborativeAgent {
        -id string
        -role Role
    }

    MiddlewareAgent --|> BaseCollaborativeAgent
    MiddlewareAgent --> Tool
    ToolMiddlewareFunc --> LogCollector
    ToolMiddlewareFunc --> MetricsCollector
    ToolMiddlewareFunc --> AgentTracker
```

## 核心组件

### 1. 中间件类型

| 中间件 | 功能 | 应用场景 |
|-------|------|---------|
| LoggingMiddleware | 记录工具调用日志 | 调试和审计 |
| MetricsMiddleware | 收集调用指标 | 性能监控 |
| TrackingMiddleware | 追踪 Agent 调用 | 调用链分析 |
| RestrictionMiddleware | 限制特定操作 | 权限控制 |

### 2. MiddlewareAgent

使用带中间件工具的 Agent，负责：

- 通过中间件链调用工具
- 自动记录和追踪调用
- 支持权限控制

### 3. 收集器

- **LogCollector**: 收集和存储日志消息
- **MetricsCollector**: 使用原子操作收集调用指标
- **AgentTracker**: 追踪 Agent 级别的调用记录

## 执行流程

### 场景 1：带日志中间件的 Agent 协作

```mermaid
sequenceDiagram
    participant Agent1 as agent-1
    participant LogMW as LoggingMiddleware
    participant Collector as LogCollector
    participant Calc as Calculator

    Agent1->>LogMW: ExecuteTool(multiply, 12, 5)
    LogMW->>Collector: Log("[开始] calculator...")
    LogMW->>Calc: Invoke(multiply, 12, 5)
    Calc-->>LogMW: result: 60
    LogMW->>Collector: Log("[完成] calculator...")
    LogMW-->>Agent1: 60
```

### 场景 2：带指标中间件的分布式执行

```mermaid
sequenceDiagram
    participant Main
    participant W1 as Worker1
    participant W2 as Worker2
    participant W3 as Worker3
    participant W4 as Worker4
    participant MW as MetricsMiddleware
    participant Metrics as MetricsCollector

    Note over W1,W4: Parallel Execution

    W1->>MW: add 10, 20
    MW->>Metrics: RecordCall success, duration

    W2->>MW: multiply 5, 8
    MW->>Metrics: RecordCall success, duration

    W3->>MW: subtract 100, 35
    MW->>Metrics: RecordCall success, duration

    W4->>MW: divide 144, 12
    MW->>Metrics: RecordCall success, duration

    Main->>Metrics: GetStats
    Metrics-->>Main: TotalCalls 4, Success 4
```

### 场景 3：Agent 级别的自定义中间件

```mermaid
sequenceDiagram
    participant Admin as admin-agent
    participant Worker as worker-agent
    participant TrackMW as TrackingMiddleware
    participant RestrictMW as RestrictionMiddleware
    participant Tracker as AgentTracker
    participant Calc as Calculator

    Admin->>TrackMW: divide 100, 4
    TrackMW->>Calc: Invoke divide
    Calc-->>TrackMW: 25
    TrackMW->>Tracker: Track admin-agent, calculator, success
    TrackMW-->>Admin: 25

    Worker->>TrackMW: multiply 7, 8
    TrackMW->>RestrictMW: multiply 7, 8
    RestrictMW->>Calc: Invoke multiply
    Calc-->>RestrictMW: 56
    RestrictMW-->>TrackMW: 56
    TrackMW->>Tracker: Track worker-agent, calculator, success
    TrackMW-->>Worker: 56

    Worker->>TrackMW: divide 100, 4
    TrackMW->>RestrictMW: divide 100, 4
    RestrictMW-->>TrackMW: Error operation forbidden
    TrackMW->>Tracker: Track worker-agent, calculator, failed
    TrackMW-->>Worker: Error
```

## 使用方法

### 运行示例

```bash
cd examples/multiagent/09-multiagent-with-middleware
go run main.go
```

### 预期输出

```text
╔════════════════════════════════════════════════════════════════╗
║          多智能体中间件示例                                     ║
║   展示多 Agent 使用带中间件的工具进行协作                        ║
╚════════════════════════════════════════════════════════════════╝

【场景 1】带日志中间件的 Agent 协作
════════════════════════════════════════════════════════════════

场景描述: 多个 Agent 使用带日志中间件的工具，追踪调用链

Agent 配置:
  agent-1: 使用带日志的 calculator 工具
  agent-2: 使用带日志的 text_processor 工具

执行工具调用:
────────────────────────────────────────
  agent-1: 12 × 5 = map[result:60]
  agent-2: uppercase('hello middleware') = map[result:HELLO MIDDLEWARE]

日志记录:
────────────────────────────────────────
  [开始] calculator - 参数: map[a:12 b:5 operation:multiply]
  [完成] calculator - 耗时: 15.208µs, 结果: map[result:60]
  [开始] text_processor - 参数: map[action:uppercase text:hello middleware]
  [完成] text_processor - 耗时: 8.125µs, 结果: map[result:HELLO MIDDLEWARE]
```

## 代码结构

```text
09-multiagent-with-middleware/
├── main.go          # 示例入口
└── README.md        # 本文档
```

### 关键代码片段

#### 创建日志中间件

```go
// 创建日志中间件
func createLoggingMiddleware(collector *LogCollector) middleware.ToolMiddlewareFunc {
    return func(tool interfaces.Tool, next middleware.ToolInvoker) middleware.ToolInvoker {
        return func(ctx context.Context, input *interfaces.ToolInput) (*interfaces.ToolOutput, error) {
            start := time.Now()
            collector.Log(fmt.Sprintf("[开始] %s - 参数: %v", tool.Name(), input.Args))

            output, err := next(ctx, input)

            duration := time.Since(start)
            if err != nil {
                collector.Log(fmt.Sprintf("[失败] %s - 耗时: %v, 错误: %v", tool.Name(), duration, err))
            } else {
                collector.Log(fmt.Sprintf("[完成] %s - 耗时: %v, 结果: %v", tool.Name(), duration, output.Result))
            }

            return output, err
        }
    }
}
```

#### 创建指标中间件

```go
// 创建指标中间件
func createMetricsMiddleware(collector *MetricsCollector) middleware.ToolMiddlewareFunc {
    return func(tool interfaces.Tool, next middleware.ToolInvoker) middleware.ToolInvoker {
        return func(ctx context.Context, input *interfaces.ToolInput) (*interfaces.ToolOutput, error) {
            start := time.Now()
            output, err := next(ctx, input)
            duration := time.Since(start)

            collector.RecordCall(err == nil, duration)

            return output, err
        }
    }
}
```

#### 创建操作限制中间件

```go
// 创建操作限制中间件
func createOperationRestrictionMiddleware(forbiddenOps []string) middleware.ToolMiddlewareFunc {
    forbidden := make(map[string]bool)
    for _, op := range forbiddenOps {
        forbidden[op] = true
    }

    return func(tool interfaces.Tool, next middleware.ToolInvoker) middleware.ToolInvoker {
        return func(ctx context.Context, input *interfaces.ToolInput) (*interfaces.ToolOutput, error) {
            if op, ok := input.Args["operation"].(string); ok {
                if forbidden[op] {
                    return nil, fmt.Errorf("操作 '%s' 被禁止", op)
                }
            }
            return next(ctx, input)
        }
    }
}
```

#### 应用中间件到工具

```go
// 创建带中间件的工具
baseTool := createCalculatorTool()

// 单个中间件
calcTool := tools.WithMiddleware(baseTool, loggingMW)

// 多个中间件链
workerTool := tools.WithMiddleware(baseTool, trackingMW, restrictionMW)
```

#### 原子操作收集指标

```go
type MetricsCollector struct {
    totalCalls    int64
    successCalls  int64
    failedCalls   int64
    totalDuration int64 // 纳秒
}

func (c *MetricsCollector) RecordCall(success bool, duration time.Duration) {
    atomic.AddInt64(&c.totalCalls, 1)
    if success {
        atomic.AddInt64(&c.successCalls, 1)
    } else {
        atomic.AddInt64(&c.failedCalls, 1)
    }
    atomic.AddInt64(&c.totalDuration, int64(duration))
}
```

## 扩展阅读

- [06-llm-tool-calling](../06-llm-tool-calling/) - LLM 工具调用示例
- [08-multiagent-tool-registry](../08-multiagent-tool-registry/) - 工具注册表示例
- [tools/middleware 包文档](../../../tools/middleware/) - 中间件包文档
