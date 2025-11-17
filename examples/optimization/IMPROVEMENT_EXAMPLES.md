# 错误处理改进示例代码

本文档提供具体的代码改进示例，展示如何从当前的错误处理方式改进为使用 GoAgent errors 包。

## 1. 环境变量检查改进

### 当前代码（三个文件都有这种模式）

```go
// hybrid_mode/main.go:31
// planning_execution/main.go:26
// cot_vs_react/main.go:25

apiKey := os.Getenv("OPENAI_API_KEY")
if apiKey == "" {
    log.Fatal("请设置环境变量 OPENAI_API_KEY")
}
```

**问题**：
- 没有错误代码分类
- 没有上下文信息
- 难以进行自动化错误处理
- 不符合项目错误处理标准

### 改进方案 A（推荐 - 直接退出）

```go
import "github.com/kart-io/goagent/errors"

apiKey := os.Getenv("OPENAI_API_KEY")
if apiKey == "" {
    agentErr := errors.NewInvalidConfigError(
        "main",
        "OPENAI_API_KEY",
        "environment variable is not set",
    )
    log.Fatal(agentErr.Error())
}
```

**改进点**：
- ✓ 使用 `errors.NewInvalidConfigError()` 提供结构化错误
- ✓ 包含组件信息 ("main")
- ✓ 包含配置键信息 ("OPENAI_API_KEY")
- ✓ 提供清晰的错误信息
- ✓ 错误代码为 `CodeInvalidConfig`

### 改进方案 B（更优雅 - 返回错误）

```go
import "github.com/kart-io/goagent/errors"

func initializeConfig() (*Config, error) {
    apiKey := os.Getenv("OPENAI_API_KEY")
    if apiKey == "" {
        return nil, errors.NewInvalidConfigError(
            "config",
            "OPENAI_API_KEY",
            "environment variable is not set",
        )
    }
    // ... 其他初始化
    return cfg, nil
}

// 在 main() 中
func main() {
    cfg, err := initializeConfig()
    if err != nil {
        log.Fatalf("Failed to initialize config: %v", err)
    }
    // ... 继续
}
```

**改进点**：
- ✓ 错误处理职责分离
- ✓ 更容易测试
- ✓ 调用者可以决定如何处理错误
- ✓ 支持优雅降级

---

## 2. LLM 初始化错误改进

### 当前代码（三个文件都有这种模式）

```go
// hybrid_mode/main.go:42
// planning_execution/main.go:37
// cot_vs_react/main.go:36

llmClient, err := providers.NewOpenAI(&llm.Config{
    APIKey:      apiKey,
    Model:       "gpt-4",
    MaxTokens:   2000,
    Temperature: 0.7,
})
if err != nil {
    log.Fatalf("Failed to create LLM client: %v", err)
}
```

**问题**：
- 错误信息不包含提供商和模型信息
- 无法区分不同 LLM 提供商的错误
- 缺少错误链信息

### 改进方案

```go
import "github.com/kart-io/goagent/errors"

llmClient, err := providers.NewOpenAI(&llm.Config{
    APIKey:      apiKey,
    Model:       "gpt-4",
    MaxTokens:   2000,
    Temperature: 0.7,
})
if err != nil {
    agentErr := errors.NewLLMRequestError(
        "openai",           // 提供商
        "gpt-4",            // 模型
        err,                // 原始错误
    )
    log.Fatalf("Failed to initialize LLM: %s", agentErr.Error())
}
```

**改进点**：
- ✓ 使用 `errors.NewLLMRequestError()` 创建 LLM 错误
- ✓ 包含提供商信息 ("openai")
- ✓ 包含模型信息 ("gpt-4")
- ✓ 保留错误链（原始错误被包装）
- ✓ 错误代码为 `CodeLLMRequest`
- ✓ 便于后续的错误过滤和处理

**错误消息示例**：
```
[LLM_REQUEST] [llm] operation=request: LLM request failed (provider=openai, model=gpt-4): API rate limit exceeded
```

---

## 3. 计划创建错误改进

### 当前代码（两个文件）

```go
// hybrid_mode/main.go:110
// planning_execution/main.go:120

plan, err := planner.CreatePlan(ctx, task, planning.PlanConstraints{
    MaxSteps:    10,
    MaxDuration: 2 * time.Hour,
})
if err != nil {
    log.Fatalf("Failed to create plan: %v", err)
}
```

**问题**：
- 没有上下文信息（任务、约束等）
- 无法区分不同类型的计划创建错误
- 难以调试和追踪问题

### 改进方案

```go
import "github.com/kart-io/goagent/errors"

plan, err := planner.CreatePlan(ctx, task, planning.PlanConstraints{
    MaxSteps:    10,
    MaxDuration: 2 * time.Hour,
})
if err != nil {
    agentErr := errors.New(
        errors.CodeInternal,
        "failed to create plan",
    ).
    WithComponent("planning").
    WithOperation("create_plan").
    WithContext("max_steps", 10).
    WithContext("max_duration", "2h").
    WithContext("task_length", len(task))
    
    log.Fatalf("Failed to create plan: %s", agentErr.Error())
}
```

**改进点**：
- ✓ 使用 `errors.New()` 创建结构化错误
- ✓ 设置错误代码 `CodeInternal`
- ✓ 添加组件信息 ("planning")
- ✓ 添加操作信息 ("create_plan")
- ✓ 添加上下文信息（约束参数）
- ✓ 链式方法调用更简洁

**错误消息示例**：
```
[INTERNAL_ERROR] [planning] operation=create_plan: failed to create plan (max_steps=10, max_duration=2h, task_length=256)
```

---

## 4. 计划验证错误改进

### 当前代码

```go
// planning_execution/main.go:135

valid, issues, err := planner.ValidatePlan(ctx, plan)
if err != nil {
    log.Fatalf("Plan validation failed: %v", err)
}
```

**问题**：
- 发现的验证问题 (issues) 没有被记录
- 无法区分验证失败的具体原因
- 调试信息不完整

### 改进方案

```go
import "github.com/kart-io/goagent/errors"

valid, issues, err := planner.ValidatePlan(ctx, plan)
if err != nil {
    agentErr := errors.New(
        errors.CodeInternal,
        fmt.Sprintf("plan validation failed: %d issues found", len(issues)),
    ).
    WithComponent("planning").
    WithOperation("validate_plan").
    WithContext("plan_id", plan.ID).
    WithContext("issues_count", len(issues)).
    WithContext("issues", issues)  // 保存所有验证问题
    
    log.Fatalf("Failed to validate plan: %s", agentErr.Error())
}
```

**改进点**：
- ✓ 在错误信息中包含问题数量
- ✓ 在上下文中保存所有问题列表
- ✓ 保留计划 ID 用于追踪
- ✓ 便于分析和调试

**错误消息示例**：
```
[INTERNAL_ERROR] [planning] operation=validate_plan: plan validation failed: 3 issues found 
(plan_id=plan-123, issues_count=3, issues=[circular dependency, missing step, invalid duration])
```

---

## 5. 计划优化降级处理改进

### 当前代码（两个位置）

```go
// hybrid_mode/main.go:115-117
// planning_execution/main.go:164-167

optimizedPlan, err := planner.OptimizePlan(ctx, plan)
if err != nil {
    log.Printf("Plan optimization failed, using original plan: %v", err)
    return plan
}
```

**问题**：
- 降级处理本身是好的，但日志信息不结构化
- 无法自动化处理和追踪降级事件
- 缺少错误分类

### 改进方案 A（简单降级）

```go
import "github.com/kart-io/goagent/errors"

optimizedPlan, err := planner.OptimizePlan(ctx, plan)
if err != nil {
    agentErr := errors.Wrap(
        err,
        errors.CodeInternal,
        "plan optimization failed, using original plan",
    ).
    WithComponent("planning").
    WithOperation("optimize_plan").
    WithContext("plan_id", plan.ID).
    WithContext("action", "fallback")
    
    log.Printf("⚠ Warning: %s", agentErr.Error())
    return plan
}
```

**改进点**：
- ✓ 使用 `errors.Wrap()` 包装原始错误
- ✓ 保持错误链
- ✓ 添加降级标记 ("fallback")
- ✓ 结构化日志输出
- ✓ 便于监控降级事件

### 改进方案 B（增强监控）

```go
import "github.com/kart-io/goagent/errors"

optimizedPlan, err := planner.OptimizePlan(ctx, plan)
if err != nil {
    agentErr := errors.Wrap(
        err,
        errors.CodeInternal,
        "plan optimization failed",
    ).
    WithComponent("planning").
    WithOperation("optimize_plan").
    WithContext("plan_id", plan.ID).
    WithContext("action", "fallback").
    WithContext("fallback_reason", "optimization_error").
    WithContext("timestamp", time.Now().Unix())
    
    // 记录警告
    log.Printf("⚠ Warning: %s", agentErr.Error())
    
    // 可选：发送到监控系统
    // metrics.RecordFallback("plan_optimization", agentErr)
    
    return plan
}
```

**改进点**：
- ✓ 添加时间戳用于分析
- ✓ 清晰标记降级原因
- ✓ 便于与监控系统集成
- ✓ 完整的上下文信息

---

## 6. Agent 执行错误改进

### 当前代码（两个位置）

```go
// cot_vs_react/main.go:91-102, 120-131

output, err := agent.Invoke(ctx, &agentcore.AgentInput{
    Task:      task,
    Timestamp: startTime,
})
if err != nil {
    log.Printf("CoT execution failed: %v", err)
    return &agentcore.AgentOutput{
        Status:  "failed",
        Message: err.Error(),
        Latency: time.Since(startTime),
    }
}
```

**问题**：
- 错误信息没有分类
- 返回的 AgentOutput 缺少错误详情
- 无法追踪 Agent 执行错误的具体类型

### 改进方案

```go
import "github.com/kart-io/goagent/errors"

output, err := agent.Invoke(ctx, &agentcore.AgentInput{
    Task:      task,
    Timestamp: startTime,
})
if err != nil {
    agentErr := errors.NewAgentExecutionError(
        "cot_math_solver",      // Agent 名称
        "invoke",               // 操作
        err,                    // 原始错误
    )
    
    log.Printf("Agent execution failed: %s", agentErr.Error())
    
    return &agentcore.AgentOutput{
        Status:  "failed",
        Message: agentErr.Error(),
        Latency: time.Since(startTime),
        Metadata: map[string]interface{}{
            "error_code":      string(agentErr.Code),
            "error_component": agentErr.Component,
            "error_operation": agentErr.Operation,
            "error_context":   agentErr.Context,
        },
    }
}
```

**改进点**：
- ✓ 使用 `errors.NewAgentExecutionError()` 创建 Agent 错误
- ✓ 包含 Agent 名称用于识别
- ✓ 包含操作信息 ("invoke")
- ✓ 在 AgentOutput 元数据中保存完整错误信息
- ✓ 便于客户端了解执行失败的原因
- ✓ 支持自动化错误处理

**错误消息示例**：
```
Agent execution failed: [AGENT_EXECUTION] [agent] operation=invoke: agent execution failed 
(agent_name=cot_math_solver): context deadline exceeded
```

---

## 7. 完整的错误处理函数示例

### 创建统一的初始化函数

```go
import (
    "github.com/kart-io/goagent/llm"
    "github.com/kart-io/goagent/llm/providers"
    "github.com/kart-io/goagent/errors"
)

// InitializeLLMClient 创建并初始化 LLM 客户端，统一的错误处理
func InitializeLLMClient(apiKey string) (llm.Client, error) {
    if apiKey == "" {
        return nil, errors.NewInvalidConfigError(
            "llm",
            "OPENAI_API_KEY",
            "API key is empty",
        )
    }
    
    llmClient, err := providers.NewOpenAI(&llm.Config{
        APIKey:      apiKey,
        Model:       "gpt-4",
        MaxTokens:   2000,
        Temperature: 0.7,
    })
    
    if err != nil {
        return nil, errors.NewLLMRequestError(
            "openai",
            "gpt-4",
            err,
        )
    }
    
    return llmClient, nil
}

// 使用示例
func main() {
    apiKey := os.Getenv("OPENAI_API_KEY")
    
    llmClient, err := InitializeLLMClient(apiKey)
    if err != nil {
        log.Fatalf("Failed to initialize LLM: %v", err)
    }
    
    // 继续使用 llmClient
}
```

---

## 8. 错误包链式调用完整示例

```go
import "github.com/kart-io/goagent/errors"

// 复杂的错误处理示例，展示完整的链式调用
func complexOperation(ctx context.Context) error {
    result, err := someComplexOperation()
    
    if err != nil {
        return errors.New(
            errors.CodeInternal,
            "complex operation failed",
        ).
        WithComponent("business_logic").
        WithOperation("execute_complex_task").
        WithContext("attempt", 1).
        WithContext("timestamp", time.Now()).
        WithContext("user_id", getUserID(ctx)).
        WithContext("request_id", getRequestID(ctx)).
        WithContextMap(map[string]interface{}{
            "retry_eligible": true,
            "fallback_available": true,
            "estimated_retry_time": "30s",
        })
    }
    
    return nil
}
```

**输出示例**：
```
[INTERNAL_ERROR] [business_logic] operation=execute_complex_task: complex operation failed 
(attempt=1, timestamp=2024-11-17T10:30:45Z, user_id=user123, request_id=req456, 
retry_eligible=true, fallback_available=true, estimated_retry_time=30s)
```

---

## 9. 提取和检查错误信息

```go
import "github.com/kart-io/goagent/errors"

// 错误处理中提取信息的示例
func handleError(err error) {
    if err == nil {
        return
    }
    
    // 检查是否是 AgentError
    if errors.IsAgentError(err) {
        code := errors.GetCode(err)
        component := errors.GetComponent(err)
        operation := errors.GetOperation(err)
        context := errors.GetContext(err)
        
        // 根据错误代码处理
        switch code {
        case errors.CodeLLMRequest:
            // 处理 LLM 请求错误
            log.Printf("LLM error: %v", err)
            // 可能发送告警
            
        case errors.CodeAgentExecution:
            // 处理 Agent 执行错误
            agentName := context["agent_name"]
            log.Printf("Agent %v failed: %v", agentName, err)
            
        case errors.CodeInvalidConfig:
            // 处理配置错误
            log.Fatalf("Configuration error: %v", err)
            
        default:
            // 处理其他错误
            log.Printf("Error [%s] in %s.%s: %v", 
                code, component, operation, err)
        }
    }
}

// 获取完整的错误链
func printErrorChain(err error) {
    chain := errors.ErrorChain(err)
    for i, e := range chain {
        log.Printf("Error %d: %v", i, e)
    }
}

// 获取根本原因
func getRootCause(err error) error {
    return errors.RootCause(err)
}
```

---

## 总结

### 改进要点检查清单

使用以下清单确保错误处理改进的完整性：

- [ ] 使用 `errors` 包而不是 `log.Fatal/log.Fatalf`
- [ ] 为错误添加适当的错误代码 (ErrorCode)
- [ ] 包含组件信息 (`WithComponent()`)
- [ ] 包含操作信息 (`WithOperation()`)
- [ ] 添加上下文信息 (`WithContext()`, `WithContextMap()`)
- [ ] 保持错误链 (使用 `Wrap()` 而不是 `New()` 包装现有错误)
- [ ] 为调试添加足够的上下文
- [ ] 支持自动化错误处理和监控

### 建议的迁移顺序

1. **第一步**: 替换所有 `log.Fatal` 调用 (3 处)
2. **第二步**: 替换所有 `log.Fatalf` 调用 (7 处)
3. **第三步**: 改进 `log.Printf` 的错误处理 (3 处)
4. **第四步**: 考虑将 `fmt.Printf` 改为结构化日志

每一步都应该有单独的提交，便于审查和回滚。

