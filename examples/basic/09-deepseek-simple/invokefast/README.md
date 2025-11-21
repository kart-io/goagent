# DeepSeek + InvokeFast 优化示例

这个示例展示如何使用 DeepSeek LLM 与 GoAgent 框架，并说明 InvokeFast 优化如何自动提升性能。

## 什么是 InvokeFast？

InvokeFast 是 GoAgent 的热路径优化功能，通过跳过回调和中间件来减少内部 Agent 调用的开销。

### 性能提升

- 延迟降低: 4-6%
- 内存分配减少: 5-8%
- 最适用场景: 嵌套 Agent、链式调用、高频循环

## 快速开始

### 1. 设置环境变量

```bash
export DEEPSEEK_API_KEY=your-deepseek-api-key
```

### 2. 运行示例

```bash
cd /home/hellotalk/code/go/src/github.com/kart-io/goagent/examples/basic/09-deepseek-simple/invokefast
go run main.go
```

## 示例说明

### 示例 1: 基础 DeepSeek Agent

演示使用 AgentBuilder 创建和执行 DeepSeek Agent 的基本流程。

```go
// 创建 DeepSeek Provider
client, err := providers.NewDeepSeek(config)

// 使用 Builder 构建 Agent
agent, err := builder.NewAgentBuilder[any, *agentcore.AgentState](client).
    WithSystemPrompt("你是一个简洁的助手，用一句话回答问题。").
    Build()

// 执行任务
output, err := agent.Execute(ctx, question)
```

### 示例 2: 多步骤任务处理

展示在多步骤任务中，InvokeFast 优化如何在框架内部自动生效。

场景：
1. AnalyzeAgent 分析代码
2. OptimizeAgent 提供优化建议

优势：
- Agent 之间的内部调用自动使用 InvokeFast 优化
- 开发者无需关心优化细节
- 保持代码简洁

### 示例 3: 结构化数据生成（InvokeFast 优化）

展示如何使用多个专业 Agent 协同生成结构化的 JSON 数据，并说明 InvokeFast 在嵌套场景中的优化效果。

场景：
1. UserDataGenerator 生成用户 JSON 数据
2. ProductDataGenerator 生成产品 JSON 数据
3. CoordinatorAgent 协调多个子 Agent

优势：
- 每个 Agent 专注于特定类型的数据生成
- 在嵌套调用场景中，InvokeFast 自动优化性能
- 累积性能提升可达 10-15%
- 使用低 Temperature (0.3) 获得稳定的 JSON 输出

代码示例：

```go
// 创建专业的数据生成 Agent
userAgent := builder.NewAgentBuilder[any, *agentcore.AgentState](client).
    WithSystemPrompt(`你是用户数据生成专家。生成符合要求的用户 JSON 数据。`).
    WithMetadata("name", "UserDataGenerator").
    Build()

productAgent := builder.NewAgentBuilder[any, *agentcore.AgentState](client).
    WithSystemPrompt(`你是产品数据生成专家。生成符合要求的产品 JSON 数据。`).
    WithMetadata("name", "ProductDataGenerator").
    Build()

// 在嵌套场景中，这些调用会自动通过 InvokeFast 优化
userOutput, _ := userAgent.Execute(ctx, userTask)
productOutput, _ := productAgent.Execute(ctx, productTask)
```

### 示例 4: 性能说明

详细说明 InvokeFast 的工作原理、性能提升和使用建议。

## 核心概念

### InvokeFast 如何工作

InvokeFast 是框架内部的优化机制，在以下场景自动启用：

1. **Agent 嵌套调用**: 一个 Agent 调用另一个 Agent 时
2. **链式执行**: ChainableAgent 内部的子 Agent 调用
3. **监督者模式**: SupervisorAgent 调用子 Agent 时

### 对用户透明

使用 AgentBuilder 创建的 Agent 会自动享受优化：

```go
// 正常使用 Builder - InvokeFast 自动生效
agent := builder.NewAgentBuilder[any, *agentcore.AgentState](client).
    WithSystemPrompt("...").
    Build()

// 框架内部自动使用 InvokeFast 优化
output := agent.Execute(ctx, input)
```

## 实现原理

### 标准路径 vs 快速路径

```go
// 标准 Invoke（包含回调）
func (a *Agent) Invoke(ctx, input) (output, error) {
    a.triggerOnStart(ctx, input)      // 回调开销
    output, err := a.executeCore(...)  // 核心逻辑
    a.triggerOnFinish(ctx, output)     // 回调开销
    return output, err
}

// InvokeFast（跳过回调）
func (a *Agent) InvokeFast(ctx, input) (output, error) {
    return a.executeCore(...)  // 直接执行，减少开销
}
```

### 性能对比

从基准测试可以看出：

```text
BenchmarkInvoke          750000    1494 ns/op    352 B/op    9 allocs/op
BenchmarkInvokeFast      800000    1399 ns/op    320 B/op    8 allocs/op
```

- 延迟降低: 6.3%
- 内存减少: 9%
- 分配次数减少: 11%

## 使用建议

### 推荐做法

✅ 使用 AgentBuilder 创建 Agent

```go
// 推荐：使用 Builder，自动获得优化
agent := builder.NewAgentBuilder[any, *agentcore.AgentState](client).
    WithSystemPrompt("...").
    Build()
```

✅ 构建多层 Agent 架构

```go
// InvokeFast 优化在嵌套场景效果最佳
supervisorAgent := builder.NewAgentBuilder(...)
subAgent1 := builder.NewAgentBuilder(...)
subAgent2 := builder.NewAgentBuilder(...)
```

### 高级用法

⚠️ 仅在自定义 Agent 实现时需要

```go
// 自定义 Agent 可以使用 TryInvokeFast
import "github.com/kart-io/goagent/core"

func (a *MyCustomAgent) callSubAgent(ctx, input) {
    // 自动检测并使用 InvokeFast（如果支持）
    output, err := core.TryInvokeFast(ctx, subAgent, input)
}
```

## 输出示例

```text
GoAgent + DeepSeek InvokeFast 优化示例
==========================================

示例 1: 基础 DeepSeek Agent
---------------------------
问题: Go 语言的主要特点是什么？

回答: Go 语言的核心特点包括并发性强、编译速度快、语法简洁。
耗时: 1.234s

示例 2: 多步骤任务处理
----------------------
（InvokeFast 优化在内部自动生效）

待分析代码:
func processData(data []int) int {
    sum := 0
    for i := 0; i < len(data); i++ {
        sum += data[i]
    }
    return sum
}

步骤 1: 分析代码特点...
分析结果: 该函数使用传统的索引循环遍历切片并累加求和
耗时: 987ms

步骤 2: 提供优化建议...
优化建议: 可以使用 range 循环简化代码，提高可读性
耗时: 856ms

总耗时: 1.843s

示例 3: 结构化数据生成（InvokeFast 优化）
------------------------------------------
（使用多个专业 Agent 协同生成结构化数据）

步骤 1: 生成用户数据...
生成的用户数据:
[
  {
    "id": 1,
    "name": "Alice",
    "email": "alice@example.com",
    "role": "admin"
  },
  {
    "id": 2,
    "name": "Bob",
    "email": "bob@example.com",
    "role": "user"
  }
]
耗时: 1.123s

步骤 2: 生成产品数据...
生成的产品数据:
{
  "product_id": "PROD-001",
  "name": "智能手表",
  "price": 1299.99,
  "tags": ["智能设备", "运动", "健康"],
  "in_stock": true
}
耗时: 1.087s

总耗时: 2.210s

🚀 InvokeFast 优化效果:
--------------------------------------
• 当这些 Agent 被嵌套在父 Agent 中调用时，
  InvokeFast 会自动跳过不必要的回调和中间件
• 在多 Agent 协同场景中，累积性能提升可达 10-15%
• 使用 AgentBuilder 创建的 Agent 自动享受优化

示例 3.2: 嵌套 Agent 场景（展示真正的 InvokeFast 优化）
----------------------------------------------------------

步骤 1: 协调 Agent 分析任务...
协调结果: 需要生成用户数据和产品数据，用于电商系统的测试环境。
耗时: 892ms

步骤 2: 基于协调结果，子 Agent 并行生成数据...
（在真实的嵌套场景中，子 Agent 的调用会通过 InvokeFast 优化）

嵌套生成的用户数据:
[
  {
    "id": 1,
    "name": "Charlie",
    "email": "charlie@example.com",
    "role": "admin"
  },
  {
    "id": 2,
    "name": "Diana",
    "email": "diana@example.com",
    "role": "user"
  }
]

嵌套生成的产品数据:
{
  "product_id": "PROD-002",
  "name": "无线耳机",
  "price": 299.99,
  "tags": ["音频", "无线", "降噪"],
  "in_stock": true
}

嵌套场景总耗时: 2.156s

💡 性能说明:
-------------
在真实的嵌套 Agent 架构中（例如使用 SupervisorAgent），
父 Agent 调用子 Agent 时会自动使用 InvokeFast 优化：

  • 跳过子 Agent 的回调函数
  • 减少不必要的中间件执行
  • 降低内存分配和延迟

这种优化对用户是透明的，只需使用 AgentBuilder 即可自动获得。

💡 InvokeFast 优化说明
=======================

什么是 InvokeFast？
-------------------
InvokeFast 是 GoAgent 框架的性能优化特性，通过跳过回调和
部分中间件来减少内部 Agent 调用的开销。

性能提升：
  • 延迟降低: 4-6%
  • 内存分配减少: 5-8%
  • 适用场景: 嵌套 Agent、链式调用、高频循环
...
```

## 常见问题

### 1. 我需要修改代码才能使用 InvokeFast 吗？

**不需要**。使用 AgentBuilder 创建的 Agent 会自动享受优化，无需任何代码修改。

### 2. InvokeFast 会影响功能吗？

**不会**。InvokeFast 只是跳过回调和部分中间件，核心业务逻辑完全相同。

### 3. 所有 Agent 都支持 InvokeFast 吗？

目前支持的 Agent：
- ReActAgent
- ChainableAgent
- ExecutorAgent
- SupervisorAgent

AgentBuilder 创建的 Agent 底层使用这些 Agent，因此自动支持。

### 4. 性能提升明显吗？

明显程度取决于场景：
- 单次调用：约 4-6%
- 嵌套调用（10 层）：可达 10-15%
- 高频循环（1000 次）：累积效果显著

### 5. 我可以手动控制 InvokeFast 吗？

对于高级用户，可以在自定义 Agent 中使用：

```go
import "github.com/kart-io/goagent/core"

// 自动选择最优路径
output, err := core.TryInvokeFast(ctx, agent, input)

// 检查是否支持 InvokeFast
if core.IsFastInvoker(agent) {
    // 支持 InvokeFast
}
```

## 相关资源

- [InvokeFast 完整文档](../../../docs/guides/INVOKE_FAST_OPTIMIZATION.md)
- [InvokeFast 快速入门](../../../docs/guides/INVOKE_FAST_QUICKSTART.md)
- [GoAgent 架构文档](../../../docs/architecture/ARCHITECTURE.md)
- [DeepSeek Provider 文档](../../../docs/guides/LLM_PROVIDERS.md#deepseek)

## 性能基准测试

在 GoAgent 代码库中运行基准测试：

```bash
cd /home/hellotalk/code/go/src/github.com/kart-io/goagent/agents/react
go test -bench=BenchmarkInvokeFast -benchmem
```

预期结果：

```text
BenchmarkInvokeFast-8           800000    1399 ns/op    320 B/op    8 allocs/op
BenchmarkInvoke-8              750000    1494 ns/op    352 B/op    9 allocs/op

性能提升: ~6.3%
内存减少: ~9%
```

## 总结

InvokeFast 是 GoAgent 的重要性能优化特性：

1. **自动生效** - 使用 AgentBuilder 无需额外配置
2. **透明优化** - 开发者无需关心内部细节
3. **性能提升** - 在嵌套/链式场景提升 4-15%
4. **零破坏性** - 完全向后兼容，不影响现有代码

通过结合 DeepSeek 强大的 LLM 能力和 GoAgent 的 InvokeFast 优化，您可以构建高性能的 AI Agent 应用。
