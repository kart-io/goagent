# InvokeFast 性能优化 - 快速开始

## 5分钟快速上手

### 什么是 InvokeFast？

**InvokeFast** 是 GoAgent 框架的热路径优化特性，通过绕过回调和中间件开销，将 Agent 执行性能提升 **4-6%**。

**关键优势**：
- ✅ **零代码修改** - 现有应用自动获得性能提升
- ✅ **自动优化** - Chain/Supervisor 自动使用快速路径
- ✅ **向后兼容** - 不支持的 Agent 自动回退
- ✅ **生产就绪** - 所有测试通过，稳定可靠

## 快速对比

### 标准调用（含回调）

```go
// 标准 Invoke - 触发所有回调和中间件
output, err := agent.Invoke(ctx, input)
// 延迟: ~1494ns, 内存分配: 24次
```

### 快速调用（无回调）

```go
// InvokeFast - 跳过回调，直接执行
output, err := agent.InvokeFast(ctx, input)
// 延迟: ~1399ns (-6.3%), 内存分配: 23次 (-4.2%)
```

## 使用场景

### ✅ 适合使用 InvokeFast

1. **Chain 内部调用** - Agent 链式执行
2. **Multi-Agent 系统** - Supervisor 调用 Worker
3. **高频循环** - ReAct 推理循环
4. **性能关键路径** - 实时响应场景

### ❌ 不适合使用 InvokeFast

1. **需要监控** - APM、日志、追踪
2. **外部 API 入口** - 用户直接调用的接口
3. **调试阶段** - 需要详细执行信息

## 三步启用优化

### 步骤 1：检查 Agent 是否支持

```go
import "github.com/kart-io/goagent/core"

if core.IsFastInvoker(agent) {
    fmt.Println("Agent 支持 InvokeFast 优化!")
}
```

**当前支持的 Agent**:
- ✅ ReActAgent
- ✅ ChainableAgent
- ✅ ExecutorAgent（自动优化内部调用）
- ✅ SupervisorAgent（自动优化子 Agent）

### 步骤 2：使用 TryInvokeFast（推荐）

```go
// TryInvokeFast 自动检测并使用最快路径
output, err := core.TryInvokeFast(ctx, agent, input)

// 等价于：
// if fastAgent, ok := agent.(core.FastInvoker); ok {
//     output, err = fastAgent.InvokeFast(ctx, input)
// } else {
//     output, err = agent.Invoke(ctx, input)
// }
```

### 步骤 3：验证性能提升

```go
// 创建基准测试
func BenchmarkAgent(b *testing.B) {
    agent := createYourAgent()
    ctx := context.Background()
    input := &core.AgentInput{Task: "test"}

    b.Run("Standard", func(b *testing.B) {
        for i := 0; i < b.N; i++ {
            agent.Invoke(ctx, input)
        }
    })

    b.Run("Optimized", func(b *testing.B) {
        for i := 0; i < b.N; i++ {
            core.TryInvokeFast(ctx, agent, input)
        }
    })
}

// 运行: go test -bench=BenchmarkAgent -benchmem
```

## 常见模式

### 模式 1：Chain 自动优化

```go
// ChainableAgent 自动使用 InvokeFast
chain := core.NewChainableAgent("my-chain", "description",
    agent1, agent2, agent3)

// 外层调用 - 保留监控能力
output, err := chain.Invoke(ctx, input)
// 内部自动对 agent1/2/3 使用 InvokeFast（如果支持）
```

### 模式 2：Supervisor 自动优化

```go
// SupervisorAgent 自动优化子 Agent 调用
supervisor := agents.NewSupervisorAgent(llmClient, config)
supervisor.AddAgent("worker1", worker1)
supervisor.AddAgent("worker2", worker2)

// 外层调用 - 触发完整监控
output, err := supervisor.Invoke(ctx, input)
// 内部自动对 workers 使用 TryInvokeFast
```

### 模式 3：手动优化内部调用

```go
func processMultipleAgents(ctx context.Context, agents []core.Agent, input *core.AgentInput) error {
    for _, agent := range agents {
        // 内部循环使用快速路径
        output, err := core.TryInvokeFast(ctx, agent, input)
        if err != nil {
            return err
        }
        // 处理输出...
    }
    return nil
}
```

## 性能收益示例

### ReActAgent 基准测试（Intel i7-14700KF）

```
标准 Invoke:     1494 ns/op    3103 B/op    24 allocs/op
InvokeFast:      1399 ns/op    3088 B/op    23 allocs/op
性能提升:        +6.3%         -0.5%        -4.2%

10x 链式调用:
标准:            15508 ns/op   32828 B/op   250 allocs/op
InvokeFast:      14825 ns/op   30878 B/op   230 allocs/op
性能提升:        +4.4%         -5.9%        -8.0%
```

### 复合优化效果

```
Supervisor → 3 Workers → ReAct → Tools
每层使用 InvokeFast，累积性能提升显著
```

## 最佳实践

### ✅ 推荐做法

```go
// 1. 外层使用 Invoke（保留监控）
func ExternalAPI(ctx context.Context, req Request) Response {
    // 触发完整回调链，便于监控和追踪
    return agent.Invoke(ctx, buildInput(req))
}

// 2. 内部使用 TryInvokeFast（优化性能）
func internalProcessing(ctx context.Context, agents []core.Agent) {
    for _, agent := range agents {
        // 内部调用使用快速路径
        core.TryInvokeFast(ctx, agent, input)
    }
}
```

### ❌ 避免做法

```go
// 不要在需要监控的入口点使用 InvokeFast
func UserFacingAPI(ctx context.Context, req Request) Response {
    // ❌ 错误：跳过了所有监控回调
    return agent.InvokeFast(ctx, buildInput(req))

    // ✅ 正确：保留监控能力
    return agent.Invoke(ctx, buildInput(req))
}
```

## 故障排查

### Q: 如何知道优化是否生效？

**A**: 使用基准测试验证：

```go
func BenchmarkOptimization(b *testing.B) {
    agent := createAgent()
    ctx := context.Background()
    input := &core.AgentInput{Task: "test"}

    b.ResetTimer()
    for i := 0; i < b.N; i++ {
        core.TryInvokeFast(ctx, agent, input)
    }
}

// 运行: go test -bench=. -benchmem -benchtime=3s
```

### Q: 为什么性能提升不明显？

**A**: 检查以下几点：

1. **LLM 调用时间占主导** - InvokeFast 优化的是框架开销，不影响 LLM 延迟
2. **Agent 不支持** - 使用 `core.IsFastInvoker(agent)` 检查
3. **单次调用测试** - 性能差异在高频调用时更明显

### Q: 生产环境建议？

**A**: 分层策略

```
┌─ 外层 API (Invoke) ────────────┐
│  触发监控和追踪                │
│  ├─ Supervisor (内部优化)     │
│  │  ├─ Worker1 (InvokeFast)   │
│  │  └─ Worker2 (InvokeFast)   │
│  └─ Chain (内部优化)          │
│     ├─ Agent A (InvokeFast)   │
│     └─ Agent B (InvokeFast)   │
└───────────────────────────────┘
```

## 下一步

- 📖 [完整文档](INVOKE_FAST_OPTIMIZATION.md) - 深入了解实现细节
- 🔬 [性能基准测试](../../agents/react/invoke_fast_benchmark_test.go) - 查看完整测试代码
- 🏗️ [实现自定义 Agent](INVOKE_FAST_OPTIMIZATION.md#实现-invokefast-的最佳实践) - 为你的 Agent 添加支持

## 总结

InvokeFast 是一个**零破坏性、自动传播、生产就绪**的性能优化特性：

- 🚀 **4-6% 性能提升** - 降低延迟，减少内存分配
- 🔄 **自动优化** - Chain/Supervisor 无需修改代码
- 🛡️ **向后兼容** - 现有代码保持工作
- 📊 **可观测性平衡** - 外层保留监控，内部追求性能

**立即开始**：在你的 Agent 内部调用中使用 `core.TryInvokeFast()`！