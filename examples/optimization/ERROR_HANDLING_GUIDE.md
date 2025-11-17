# 错误处理指南

本文档说明 GoAgent 优化示例中的统一错误处理方式。

## 目录

- [为什么统一错误处理](#为什么统一错误处理)
- [错误处理包概述](#错误处理包概述)
- [使用方法](#使用方法)
- [迁移对比](#迁移对比)
- [最佳实践](#最佳实践)

## 为什么统一错误处理

### 之前的问题

在重构之前，项目中使用了多种错误处理方式：

- `log.Fatal()` - 直接退出程序，无错误上下文
- `log.Fatalf()` - 格式化输出后退出，无错误分类
- `log.Printf()` - 仅打印日志，无结构化信息
- `fmt.Printf()` - 混乱的错误输出

**主要缺陷：**

1. **无错误分类** - 无法区分配置错误、LLM 错误、内部错误等
2. **缺少上下文** - 错误信息不包含操作、组件等关键信息
3. **难以追踪** - 没有堆栈跟踪和错误链
4. **不利监控** - 无法进行结构化日志分析和告警
5. **不统一** - 各处理方式不一致，维护困难

### 重构后的优势

使用统一的 `github.com/kart-io/goagent/errors` 包：

1. **结构化错误** - 包含错误代码、操作、组件、上下文
2. **错误链支持** - 保留原始错误，支持 `errors.Unwrap()`
3. **堆栈跟踪** - 自动捕获错误发生时的堆栈信息
4. **便于监控** - 可提取错误代码和上下文进行分析
5. **统一接口** - 所有错误使用相同的创建和处理方式

## 错误处理包概述

### 预定义错误代码

`github.com/kart-io/goagent/errors` 包提供了丰富的错误代码：

```go
const (
    // 配置错误
    CodeInvalidConfig  ErrorCode = "INVALID_CONFIG"

    // LLM 错误
    CodeLLMRequest     ErrorCode = "LLM_REQUEST"
    CodeLLMResponse    ErrorCode = "LLM_RESPONSE"
    CodeLLMTimeout     ErrorCode = "LLM_TIMEOUT"
    CodeLLMRateLimit   ErrorCode = "LLM_RATE_LIMIT"

    // Agent 错误
    CodeAgentExecution      ErrorCode = "AGENT_EXECUTION"
    CodeAgentInitialization ErrorCode = "AGENT_INITIALIZATION"

    // 通用错误
    CodeInternal       ErrorCode = "INTERNAL_ERROR"
)
```

### 核心方法

#### 创建新错误

```go
// New - 创建新错误
err := errors.New(errors.CodeInvalidConfig, "API key is missing")

// Newf - 格式化创建
err := errors.Newf(errors.CodeLLMRequest, "failed to call model %s", modelName)
```

#### 包装现有错误

```go
// Wrap - 包装错误并添加上下文
err := errors.Wrap(originalErr, errors.CodeLLMRequest, "failed to create LLM client")

// Wrapf - 格式化包装
err := errors.Wrapf(originalErr, errors.CodeAgentExecution, "agent %s failed", agentName)
```

#### 链式添加上下文

```go
err := errors.New(errors.CodeInvalidConfig, "API key missing").
    WithOperation("initialization").           // 设置操作名称
    WithComponent("cot_vs_react_example").    // 设置组件名称
    WithContext("env_var", "OPENAI_API_KEY") // 添加上下文键值对
```

## 使用方法

### 1. 导入错误包

```go
import (
    "github.com/kart-io/goagent/errors"
)
```

### 2. 配置错误处理

用于环境变量缺失、配置文件错误等场景：

```go
apiKey := os.Getenv("OPENAI_API_KEY")
if apiKey == "" {
    err := errors.New(errors.CodeInvalidConfig, "OPENAI_API_KEY environment variable is not set").
        WithOperation("initialization").
        WithComponent("cot_vs_react_example").
        WithContext("env_var", "OPENAI_API_KEY")
    fmt.Printf("错误: %v\n", err)
    fmt.Println("请设置环境变量 OPENAI_API_KEY")
    os.Exit(1)
}
```

**错误输出示例：**

```text
错误: [INVALID_CONFIG] [cot_vs_react_example] operation=initialization: OPENAI_API_KEY environment variable is not set (env_var=OPENAI_API_KEY)
请设置环境变量 OPENAI_API_KEY
```

### 3. LLM 错误处理

用于 LLM 客户端初始化、请求失败等场景：

```go
llmClient, err := providers.NewOpenAI(&llm.Config{
    APIKey:      apiKey,
    Model:       "gpt-4",
    MaxTokens:   2000,
    Temperature: 0.7,
})
if err != nil {
    wrappedErr := errors.Wrap(err, errors.CodeLLMRequest, "failed to create LLM client").
        WithOperation("initialization").
        WithComponent("cot_vs_react_example").
        WithContext("provider", "openai").
        WithContext("model", "gpt-4")
    fmt.Printf("错误: %v\n", wrappedErr)
    os.Exit(1)
}
```

**错误输出示例：**

```text
错误: [LLM_REQUEST] [cot_vs_react_example] operation=initialization: failed to create LLM client (provider=openai, model=gpt-4): <原始错误信息>
```

### 4. Agent 执行错误

用于 Agent 调用失败等场景：

```go
output, err := agent.Invoke(ctx, &agentcore.AgentInput{
    Task:      task,
    Timestamp: startTime,
})
if err != nil {
    wrappedErr := errors.Wrap(err, errors.CodeAgentExecution, "CoT agent execution failed").
        WithOperation("invoke").
        WithComponent("cot_agent").
        WithContext("agent_name", "cot_math_solver")
    fmt.Printf("CoT 执行失败: %v\n", wrappedErr)
    return &agentcore.AgentOutput{
        Status:  "failed",
        Message: wrappedErr.Error(),
        Latency: time.Since(startTime),
    }
}
```

### 5. 降级错误处理

用于非致命错误，允许继续执行：

```go
optimizedPlan, err := planner.OptimizePlan(ctx, plan)
if err != nil {
    wrappedErr := errors.Wrap(err, errors.CodeInternal, "plan optimization failed, using original plan").
        WithOperation("optimize_plan").
        WithComponent("smart_planner").
        WithContext("plan_id", plan.ID)
    fmt.Printf("警告: %v\n", wrappedErr)
    return plan  // 返回原始计划，不退出
}
```

**错误输出示例：**

```text
警告: [INTERNAL_ERROR] [smart_planner] operation=optimize_plan: plan optimization failed, using original plan (plan_id=abc123): <原始错误>
```

## 迁移对比

### 示例 1: API Key 检查

#### 迁移前

```go
apiKey := os.Getenv("OPENAI_API_KEY")
if apiKey == "" {
    log.Fatal("请设置环境变量 OPENAI_API_KEY")
}
```

**问题：**

- 无错误代码，无法分类
- 无上下文信息
- 无堆栈跟踪

#### 迁移后

```go
apiKey := os.Getenv("OPENAI_API_KEY")
if apiKey == "" {
    err := errors.New(errors.CodeInvalidConfig, "OPENAI_API_KEY environment variable is not set").
        WithOperation("initialization").
        WithComponent("cot_vs_react_example").
        WithContext("env_var", "OPENAI_API_KEY")
    fmt.Printf("错误: %v\n", err)
    fmt.Println("请设置环境变量 OPENAI_API_KEY")
    os.Exit(1)
}
```

**改进：**

- ✅ 错误代码：`INVALID_CONFIG`
- ✅ 操作上下文：`initialization`
- ✅ 组件信息：`cot_vs_react_example`
- ✅ 附加上下文：`env_var=OPENAI_API_KEY`
- ✅ 结构化输出，便于日志分析

### 示例 2: LLM 初始化

#### 迁移前

```go
llmClient, err := providers.NewOpenAI(&llm.Config{...})
if err != nil {
    log.Fatalf("Failed to create LLM client: %v", err)
}
```

**问题：**

- 错误信息简单
- 无法区分不同的 LLM 错误类型
- 丢失原始错误上下文

#### 迁移后

```go
llmClient, err := providers.NewOpenAI(&llm.Config{...})
if err != nil {
    wrappedErr := errors.Wrap(err, errors.CodeLLMRequest, "failed to create LLM client").
        WithOperation("initialization").
        WithComponent("cot_vs_react_example").
        WithContext("provider", "openai").
        WithContext("model", "gpt-4")
    fmt.Printf("错误: %v\n", wrappedErr)
    os.Exit(1)
}
```

**改进：**

- ✅ 错误代码：`LLM_REQUEST`
- ✅ 保留原始错误（错误链）
- ✅ 添加 provider 和 model 上下文
- ✅ 便于监控 LLM 相关错误

### 示例 3: Agent 执行失败

#### 迁移前

```go
output, err := agent.Invoke(ctx, input)
if err != nil {
    log.Printf("CoT execution failed: %v", err)
    return &agentcore.AgentOutput{
        Status:  "failed",
        Message: err.Error(),
        Latency: time.Since(startTime),
    }
}
```

**问题：**

- 使用 `log.Printf` 仅记录，不中断执行
- 错误信息不包含 Agent 名称等上下文

#### 迁移后

```go
output, err := agent.Invoke(ctx, input)
if err != nil {
    wrappedErr := errors.Wrap(err, errors.CodeAgentExecution, "CoT agent execution failed").
        WithOperation("invoke").
        WithComponent("cot_agent").
        WithContext("agent_name", "cot_math_solver")
    fmt.Printf("CoT 执行失败: %v\n", wrappedErr)
    return &agentcore.AgentOutput{
        Status:  "failed",
        Message: wrappedErr.Error(),
        Latency: time.Since(startTime),
    }
}
```

**改进：**

- ✅ 错误代码：`AGENT_EXECUTION`
- ✅ Agent 名称上下文
- ✅ 返回的 AgentOutput 包含结构化错误信息

## 最佳实践

### 1. 选择合适的错误代码

根据错误类型选择最合适的错误代码：

| 场景 | 错误代码 |
|------|---------|
| 缺少环境变量、配置文件错误 | `CodeInvalidConfig` |
| LLM 客户端初始化失败 | `CodeLLMRequest` |
| LLM 调用超时 | `CodeLLMTimeout` |
| Agent 执行失败 | `CodeAgentExecution` |
| Agent 初始化失败 | `CodeAgentInitialization` |
| 内部逻辑错误 | `CodeInternal` |

### 2. 始终添加上下文

使用链式方法添加丰富的上下文信息：

```go
err := errors.New(code, message).
    WithOperation("operation_name").     // 操作名称
    WithComponent("component_name").     // 组件名称
    WithContext("key1", value1).         // 自定义上下文
    WithContext("key2", value2)
```

**推荐的上下文信息：**

- **operation** - 正在执行的操作（如 `initialization`, `invoke`, `create_plan`）
- **component** - 发生错误的组件（如 `cot_agent`, `smart_planner`）
- **自定义键值对** - 相关参数（如 `model`, `agent_name`, `plan_id`）

### 3. 包装而非丢弃原始错误

使用 `errors.Wrap()` 保留原始错误信息：

```go
// ✅ 正确 - 保留原始错误
wrappedErr := errors.Wrap(err, errors.CodeLLMRequest, "failed to create client")

// ❌ 错误 - 丢失原始错误
newErr := errors.New(errors.CodeLLMRequest, "failed to create client")
```

### 4. 区分致命错误和降级错误

- **致命错误** - 使用 `os.Exit(1)` 退出
- **降级错误** - 使用 `fmt.Printf("警告:...")` 并继续执行

```go
// 致命错误 - 退出
if err != nil {
    wrappedErr := errors.Wrap(err, code, message)
    fmt.Printf("错误: %v\n", wrappedErr)
    os.Exit(1)
}

// 降级错误 - 继续
if err != nil {
    wrappedErr := errors.Wrap(err, code, "operation failed, using fallback")
    fmt.Printf("警告: %v\n", wrappedErr)
    return fallbackValue  // 使用降级方案
}
```

### 5. 提供用户友好的错误消息

在输出结构化错误后，提供额外的用户友好提示：

```go
if apiKey == "" {
    err := errors.New(errors.CodeInvalidConfig, "OPENAI_API_KEY environment variable is not set").
        WithOperation("initialization").
        WithComponent("example")
    fmt.Printf("错误: %v\n", err)
    fmt.Println("请设置环境变量 OPENAI_API_KEY")  // 用户友好提示
    os.Exit(1)
}
```

### 6. 在日志和监控中使用错误代码

错误代码可用于：

- **日志分析** - 统计各类错误发生频率
- **告警规则** - 针对特定错误代码设置告警
- **错误追踪** - 按错误代码分类和查询

```go
// 示例：提取错误代码用于监控
if err != nil {
    wrappedErr := errors.Wrap(err, errors.CodeLLMRequest, "request failed")

    // 可以提取错误代码
    code := errors.GetCode(wrappedErr)  // 返回 "LLM_REQUEST"

    // 用于监控系统
    metrics.IncrementErrorCount(string(code))

    fmt.Printf("错误: %v\n", wrappedErr)
}
```

## 错误处理统计

经过重构，所有优化示例的错误处理已完全统一：

| 文件 | 重构前 | 重构后 |
|------|--------|--------|
| `cot_vs_react/main.go` | 3 处混乱错误处理 | 5 处统一错误处理 |
| `planning_execution/main.go` | 6 处混乱错误处理 | 6 处统一错误处理 |
| `hybrid_mode/main.go` | 4 处混乱错误处理 | 4 处统一错误处理 |
| **总计** | **13 处** | **15 处（新增 2 处降级错误）** |

**改进覆盖率：100%**

## 参考资料

- [GoAgent errors 包源码](/home/hellotalk/code/go/src/github.com/kart-io/goagent/errors/errors.go)
- [错误处理分析报告](ERROR_HANDLING_ANALYSIS.md)
- [错误处理改进示例](IMPROVEMENT_EXAMPLES.md)

## 总结

通过统一使用 `github.com/kart-io/goagent/errors` 包，我们实现了：

1. ✅ **结构化错误处理** - 所有错误包含代码、操作、组件、上下文
2. ✅ **错误链支持** - 保留原始错误，便于调试
3. ✅ **便于监控和分析** - 可按错误代码分类和统计
4. ✅ **统一接口** - 所有示例使用相同的错误处理模式
5. ✅ **更好的可维护性** - 清晰的错误分类和上下文信息

所有优化示例现已采用统一的错误处理方式，为后续的监控、日志分析和错误追踪打下了坚实基础。
