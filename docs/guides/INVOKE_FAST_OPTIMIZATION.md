# InvokeFast 核心执行路径优化指南

## 概述

`InvokeFast` 是 GoAgent 框架中的热路径优化方法，通过绕过回调和中间件开销，为内部调用提供更高的性能。

**当前实现状态：**
- ✅ **ReActAgent**: 完整实现（executeCore 重构 + InvokeFast）
- ✅ **ChainableAgent**: 完整实现（自动使用 FastInvoker）
- ✅ **ExecutorAgent**: 使用 TryInvokeFast 优化内部调用
- ✅ **SupervisorAgent**: 使用 TryInvokeFast 优化子 Agent 调用
- ✅ **FastInvoker 接口**: 统一的快速调用接口（`core/fast_invoker.go`）
- ✅ **辅助函数**: `TryInvokeFast` 和 `IsFastInvoker`

## 性能收益

基于基准测试结果（Intel i7-14700KF，Go 1.25）：

### 单次调用性能对比

| 方法 | 延迟 (ns/op) | 内存分配次数 | 内存使用 (B/op) | 提升幅度 |
|------|-------------|-------------|----------------|---------|
| Invoke (无回调) | 1494 | 24 | 3103 | 基准 |
| Invoke (1个回调) | 1496 | 24 | 3103 | -0.1% |
| Invoke (5个回调) | 1513 | 24 | 3102 | -1.3% |
| **InvokeFast** | **1399** | **23** | **3088** | **+6.3%** |

### 链式调用性能对比（10次调用）

| 方法 | 延迟 (ns/op) | 内存分配次数 | 内存使用 (B/op) | 提升幅度 |
|------|-------------|-------------|----------------|---------|
| Invoke (10x) | 15508 | 250 | 32828 | 基准 |
| **InvokeFast (10x)** | **14825** | **230** | **30878** | **+4.4%** |

### 关键收益

- **延迟降低**: 单次调用快 6.3%，链式调用快 4.4%
- **内存优化**: 减少 8% 的内存分配次数（250 → 230）
- **内存使用**: 减少 5.9% 的内存使用（32828 → 30878 字节）
- **可预测性**: 无回调干扰，执行路径更稳定

## 实现原理

### 标准 Invoke 调用链

```
Invoke()
  ├── triggerOnStart() (遍历所有回调)
  ├── executeCore()
  │   ├── triggerOnLLMStart()
  │   ├── LLM.Chat()
  │   ├── triggerOnLLMEnd()
  │   ├── triggerOnToolStart()
  │   ├── Tool.Invoke()
  │   └── triggerOnToolEnd()
  └── triggerOnFinish() (遍历所有回调)
```

### InvokeFast 优化路径

```
InvokeFast()
  └── executeCore(withCallbacks=false)
      ├── LLM.Chat() (直接调用)
      └── Tool.Invoke() (直接调用)
```

### 优化点

1. **跳过回调遍历**: 不触发任何回调（OnStart/OnFinish/OnLLM*/OnTool*）
2. **减少虚拟方法调用**: 避免接口方法分派开销
3. **减少内存分配**: 不创建回调相关的中间对象
4. **减少上下文切换**: 简化调用栈深度

## 使用场景

### 适合使用 InvokeFast

1. **Chain 内部调用**
   - Agent Chain 中 Agent 之间的调用
   - Sequential 顺序执行
   - Pipeline 管道处理

2. **嵌套 Agent 调用**
   - Multi-Agent 系统中子 Agent 的调用
   - Supervisor Agent 调用 Worker Agent
   - Hierarchical Agent 层级结构

3. **高频循环场景**
   - ReAct Agent 的推理循环
   - Retry 重试逻辑
   - Batch 批处理内部循环

4. **性能关键路径**
   - 实时响应要求高的场景
   - 大量并发调用
   - 资源受限环境

### 不适合使用 InvokeFast

1. **需要监控和追踪**
   - 生产环境中需要 APM 监控
   - 调试和问题排查阶段
   - 需要详细的执行日志

2. **外部 API 调用**
   - 用户直接调用的入口点
   - 需要计费和审计的场景
   - 需要限流和熔断保护

3. **复杂的中间件逻辑**
   - 需要缓存中间件
   - 需要权限验证
   - 需要自定义处理逻辑

## 使用示例

### 示例 1: Chain 内部优化

```go
// 错误示例：Chain 内部调用使用 Invoke（会触发所有回调）
type AgentChain struct {
    agents []core.Agent
}

func (c *AgentChain) Execute(ctx context.Context, input *core.AgentInput) (*core.AgentOutput, error) {
    current := input
    for _, agent := range c.agents {
        // ❌ 每次调用都触发回调，开销大
        output, err := agent.Invoke(ctx, current)
        if err != nil {
            return nil, err
        }
        // 准备下一个 Agent 的输入
        current = &core.AgentInput{
            Task:      output.Message,
            Context:   output.Metadata,
            Timestamp: time.Now(),
        }
    }
    return current, nil
}

// 优化示例：使用 InvokeFast
func (c *AgentChain) Execute(ctx context.Context, input *core.AgentInput) (*core.AgentOutput, error) {
    current := input
    for _, agent := range c.agents {
        // ✅ 内部调用使用 InvokeFast，减少开销
        var output *core.AgentOutput
        var err error

        // 尝试使用 InvokeFast（如果 Agent 实现了该方法）
        if fastAgent, ok := agent.(interface {
            InvokeFast(context.Context, *core.AgentInput) (*core.AgentOutput, error)
        }); ok {
            output, err = fastAgent.InvokeFast(ctx, current)
        } else {
            output, err = agent.Invoke(ctx, current)
        }

        if err != nil {
            return nil, err
        }

        current = &core.AgentInput{
            Task:      output.Message,
            Context:   output.Metadata,
            Timestamp: time.Now(),
        }
    }
    return current, nil
}
```

### 示例 2: Multi-Agent 系统优化

```go
// SupervisorAgent 调用多个 Worker Agent
type SupervisorAgent struct {
    *core.BaseAgent
    workers []core.Agent
}

func (s *SupervisorAgent) Invoke(ctx context.Context, input *core.AgentInput) (*core.AgentOutput, error) {
    startTime := time.Now()

    // ✅ 外部调用使用 Invoke（触发监控回调）
    s.triggerOnStart(ctx, input)

    results := make([]*core.AgentOutput, 0, len(s.workers))

    // ✅ 内部调用使用 InvokeFast（高性能）
    for _, worker := range s.workers {
        if fastWorker, ok := worker.(interface {
            InvokeFast(context.Context, *core.AgentInput) (*core.AgentOutput, error)
        }); ok {
            result, err := fastWorker.InvokeFast(ctx, input)
            if err != nil {
                continue // 跳过失败的 Worker
            }
            results = append(results, result)
        }
    }

    // 聚合结果
    output := s.aggregateResults(results)
    output.Latency = time.Since(startTime)

    s.triggerOnFinish(ctx, output)
    return output, nil
}
```

### 示例 3: 批处理优化

```go
// 批量处理多个任务
func BatchProcess(ctx context.Context, agent core.Agent, tasks []string) ([]*core.AgentOutput, error) {
    outputs := make([]*core.AgentOutput, 0, len(tasks))

    for _, task := range tasks {
        input := &core.AgentInput{
            Task:      task,
            Timestamp: time.Now(),
        }

        // ✅ 批处理内部使用 InvokeFast
        var output *core.AgentOutput
        var err error

        if fastAgent, ok := agent.(interface {
            InvokeFast(context.Context, *core.AgentInput) (*core.AgentOutput, error)
        }); ok {
            output, err = fastAgent.InvokeFast(ctx, input)
        } else {
            output, err = agent.Invoke(ctx, input)
        }

        if err != nil {
            return nil, err
        }

        outputs = append(outputs, output)
    }

    return outputs, nil
}
```

## 实现 InvokeFast 的最佳实践

如果你正在实现自己的 Agent，以下是添加 InvokeFast 支持的最佳实践：

### 1. 提取核心逻辑

```go
type MyAgent struct {
    *core.BaseAgent
    // ... 其他字段
}

// executeCore 包含核心业务逻辑
func (a *MyAgent) executeCore(ctx context.Context, input *core.AgentInput, startTime time.Time, withCallbacks bool) (*core.AgentOutput, error) {
    // 核心执行逻辑
    output := &core.AgentOutput{}

    // 条件性触发回调
    if withCallbacks {
        if err := a.triggerOnLLMStart(ctx, prompts); err != nil {
            return nil, err
        }
    }

    // LLM 调用
    resp, err := a.llm.Chat(ctx, messages)
    if err != nil {
        return nil, err
    }

    if withCallbacks {
        if err := a.triggerOnLLMEnd(ctx, resp.Content, resp.TokensUsed); err != nil {
            return nil, err
        }
    }

    // 构建输出
    output.Result = resp.Content
    output.Latency = time.Since(startTime)
    return output, nil
}
```

### 2. 实现 Invoke 和 InvokeFast

```go
// Invoke 完整执行（含回调）
func (a *MyAgent) Invoke(ctx context.Context, input *core.AgentInput) (*core.AgentOutput, error) {
    startTime := time.Now()

    // 触发开始回调
    if err := a.triggerOnStart(ctx, input); err != nil {
        return nil, err
    }

    // 执行核心逻辑（withCallbacks=true）
    output, err := a.executeCore(ctx, input, startTime, true)

    // 触发完成回调
    if err == nil {
        if cbErr := a.triggerOnFinish(ctx, output); cbErr != nil {
            return nil, cbErr
        }
    }

    return output, err
}

// InvokeFast 快速执行（无回调）
//
//go:inline
func (a *MyAgent) InvokeFast(ctx context.Context, input *core.AgentInput) (*core.AgentOutput, error) {
    return a.executeCore(ctx, input, time.Now(), false)
}
```

### 3. 添加工具调用优化

```go
// executeTool 标准工具调用（含回调）
func (a *MyAgent) executeTool(ctx context.Context, toolName string, input map[string]interface{}) (interface{}, error) {
    tool := a.toolsByName[toolName]

    if err := a.triggerOnToolStart(ctx, toolName, input); err != nil {
        return nil, err
    }

    result, err := tool.Invoke(ctx, &interfaces.ToolInput{Args: input})

    if err != nil {
        a.triggerOnToolError(ctx, toolName, err)
        return nil, err
    }

    a.triggerOnToolEnd(ctx, toolName, result)
    return result.Result, nil
}

// executeToolFast 快速工具调用（无回调）
//
//go:inline
func (a *MyAgent) executeToolFast(ctx context.Context, toolName string, input map[string]interface{}) (interface{}, error) {
    tool := a.toolsByName[toolName]
    result, err := tool.Invoke(ctx, &interfaces.ToolInput{Args: input})
    if err != nil {
        return nil, err
    }
    return result.Result, nil
}
```

## 注意事项

### 1. 类型断言检查

使用 InvokeFast 前，始终检查 Agent 是否实现了该方法：

```go
// ✅ 正确：先检查再使用
if fastAgent, ok := agent.(interface {
    InvokeFast(context.Context, *core.AgentInput) (*core.AgentOutput, error)
}); ok {
    return fastAgent.InvokeFast(ctx, input)
}
return agent.Invoke(ctx, input)

// ❌ 错误：直接断言会 panic
return agent.(interface {
    InvokeFast(context.Context, *core.AgentInput) (*core.AgentOutput, error)
}).InvokeFast(ctx, input)
```

### 2. 调试时的选择

开发和调试阶段，建议使用标准 Invoke 方法以获得完整的追踪信息。性能优化应该在功能稳定后进行。

```go
// 开发环境：使用 Invoke（便于调试）
if debug {
    return agent.Invoke(ctx, input)
}

// 生产环境：使用 InvokeFast（高性能）
if fastAgent, ok := agent.(FastInvoker); ok {
    return fastAgent.InvokeFast(ctx, input)
}
return agent.Invoke(ctx, input)
```

### 3. 回调的权衡

回调系统提供了强大的监控和扩展能力，但会带来性能开销。选择使用 InvokeFast 意味着：

- **放弃**: 监控、追踪、日志、指标收集
- **获得**: 更低延迟、更少内存分配、更高吞吐量

在关键路径上使用 InvokeFast，在外层保留 Invoke 的监控能力，是一种平衡的策略。

## 性能测试建议

在你的项目中验证 InvokeFast 的性能收益：

```go
func BenchmarkYourAgent(b *testing.B) {
    agent := createYourAgent()
    ctx := context.Background()
    input := &core.AgentInput{Task: "test"}

    b.Run("Invoke", func(b *testing.B) {
        for i := 0; i < b.N; i++ {
            _, _ = agent.Invoke(ctx, input)
        }
    })

    b.Run("InvokeFast", func(b *testing.B) {
        for i := 0; i < b.N; i++ {
            _, _ = agent.InvokeFast(ctx, input)
        }
    })
}
```

运行基准测试：

```bash
cd your-agent-dir
go test -bench=BenchmarkYourAgent -benchmem -benchtime=3s
```

## 系统级实现

GoAgent 框架已在多个核心组件中实现了 InvokeFast 优化，形成了完整的性能优化生态。

### FastInvoker 接口

定义在 `core/fast_invoker.go`：

```go
// FastInvoker 定义快速调用接口
type FastInvoker interface {
    InvokeFast(ctx context.Context, input *AgentInput) (*AgentOutput, error)
}

// TryInvokeFast 尝试使用快速调用，如果不支持则回退到标准 Invoke
func TryInvokeFast(ctx context.Context, agent Agent, input *AgentInput) (*AgentOutput, error)

// IsFastInvoker 检查 Agent 是否支持快速调用
func IsFastInvoker(agent Agent) bool
```

### 已实现的 Agent

#### 1. ReActAgent（完整实现）

**位置**: `agents/react/react.go`

**实现方式**:
- 重构 `Invoke` 方法，提取核心逻辑到 `executeCore`
- 实现 `InvokeFast` 方法，直接调用 `executeCore(withCallbacks=false)`
- 实现 `executeToolFast` 方法，跳过工具回调
- 实现 `handleErrorFast` 方法，跳过错误回调

**性能提升**: 单次调用快 6.3%，链式调用快 4.4%

**使用示例**:
```go
reactAgent := react.NewReActAgent(config)

// 外层调用使用 Invoke（保留监控）
output, err := reactAgent.Invoke(ctx, input)

// 内部循环使用 InvokeFast（追求性能）
output, err := reactAgent.InvokeFast(ctx, input)
```

#### 2. ChainableAgent（完整实现）

**位置**: `core/agent.go`

**实现方式**:
- 重构 `Invoke` 方法，内部自动使用 `TryInvokeFast` 调用子 Agent
- 实现 `InvokeFast` 方法，传递 `useFastPath=true` 给 `executeChain`
- 支持嵌套链的全路径优化

**优化收益**: 链内部调用自动使用快速路径，无需手动优化

**使用示例**:
```go
chain := core.NewChainableAgent("my-chain", "description", agent1, agent2, agent3)

// Invoke 内部自动对 agent1/2/3 使用 InvokeFast（如果支持）
output, err := chain.Invoke(ctx, input)

// 嵌套链场景使用 InvokeFast
output, err := chain.InvokeFast(ctx, input)
```

#### 3. ExecutorAgent（使用优化）

**位置**: `agents/executor/executor_agent.go`

**实现方式**:
- 在 `Execute` 方法中使用 `TryInvokeFast` 调用被包装的 Agent
- 保留记忆加载、超时控制、迭代限制等外层逻辑
- 仅优化核心 Agent 调用路径

**优化收益**: 减少 Executor 对被包装 Agent 的调用开销

**使用示例**:
```go
executor := executor.NewAgentExecutor(executor.ExecutorConfig{
    Agent:            reactAgent,
    MaxIterations:    10,
    MaxExecutionTime: 60 * time.Second,
})

// Executor 内部自动使用 TryInvokeFast 调用 reactAgent
output, err := executor.Execute(ctx, input)
```

#### 4. SupervisorAgent（使用优化）

**位置**: `agents/supervisor.go`

**实现方式**:
- 在 `executeTask` 方法中使用 `TryInvokeFast` 调用子 Agent
- 保留任务分解、路由选择、结果聚合等外层逻辑
- 优化并发执行场景下的子 Agent 调用

**优化收益**: 显著降低 Multi-Agent 系统的调度开销

**使用示例**:
```go
supervisor := agents.NewSupervisorAgent(llmClient, config)
supervisor.AddAgent("worker1", worker1Agent)
supervisor.AddAgent("worker2", worker2Agent)

// Supervisor 内部自动使用 TryInvokeFast 调用 worker agents
output, err := supervisor.Invoke(ctx, input)
```

### 优化传播路径

当你使用嵌套的 Agent 结构时，InvokeFast 优化会自动传播：

```
Supervisor (Invoke)
  ├─> Worker1 (InvokeFast via TryInvokeFast)
  │   └─> ReAct (InvokeFast)
  │       ├─> LLM Call (无回调)
  │       └─> Tool Call (executeToolFast)
  │
  └─> Worker2 (InvokeFast via TryInvokeFast)
      └─> Chain (InvokeFast)
          ├─> Agent A (InvokeFast via TryInvokeFast)
          ├─> Agent B (InvokeFast via TryInvokeFast)
          └─> Agent C (InvokeFast via TryInvokeFast)
```

在这个例子中：
- **顶层 Supervisor.Invoke**: 触发完整回调（监控入口点）
- **所有内部调用**: 自动使用 InvokeFast（高性能执行）
- **性能收益**: 复合优化，多层嵌套时效果更显著

### 向后兼容

所有优化都是**向后兼容**的：
- 不实现 `InvokeFast` 的 Agent 自动回退到标准 `Invoke`
- 使用 `TryInvokeFast` 自动处理兼容性
- 现有代码无需修改即可获得性能提升（如果底层 Agent 支持）

### 扩展指南

要为你的自定义 Agent 添加 InvokeFast 支持：

1. **实现 FastInvoker 接口**:
   ```go
   func (a *MyAgent) InvokeFast(ctx context.Context, input *core.AgentInput) (*core.AgentOutput, error) {
       // 跳过回调的快速执行路径
       return a.executeCore(ctx, input, time.Now(), false)
   }
   ```

2. **内部调用使用 TryInvokeFast**:
   ```go
   // 如果调用其他 Agent
   output, err := core.TryInvokeFast(ctx, subAgent, input)
   ```

3. **保持接口兼容性**:
   - `Invoke` 方法仍然存在并触发完整回调
   - `InvokeFast` 是可选的优化方法
   - 使用 `core.IsFastInvoker(agent)` 检测支持情况

## 总结

`InvokeFast` 是 GoAgent 框架中的重要性能优化工具：

- **使用场景**: 内部调用、链式调用、高频循环
- **性能收益**: 延迟降低 4-6%，内存优化 5-8%
- **实现要求**: 提取核心逻辑，条件性触发回调
- **最佳实践**: 外层使用 Invoke 监控，内部使用 InvokeFast 优化

通过合理使用 InvokeFast，你可以在保持系统可观测性的同时，显著提升关键路径的性能。
