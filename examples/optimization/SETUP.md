# GoAgent 优化示例 - 安装和运行指南

## 目录说明

本目录包含三个 ReAct 优化方案的示例程序：

### ✅ 1. cot_vs_react/ - 已就绪
CoT 与 ReAct 性能对比示例，可直接运行。

### ✅ 2. planning_execution/ - 已就绪
Planning + Execution 优化示例，已完整实现内存管理器支持。

### ✅ 3. hybrid_mode/ - 已就绪
混合模式示例，已完整实现内存管理器支持。

## 快速开始

所有示例现已完全就绪，可直接运行！

### 运行 CoT vs ReAct 对比示例

```bash
# 1. 设置 API Key
export OPENAI_API_KEY="your-openai-api-key"

# 2. 运行示例
cd cot_vs_react
go run main.go
```

### 运行 Planning + Execution 示例

```bash
# 1. 设置 API Key
export OPENAI_API_KEY="your-openai-api-key"

# 2. 运行示例
cd planning_execution
go run main.go
```

### 运行混合模式示例

```bash
# 1. 设置 API Key
export OPENAI_API_KEY="your-openai-api-key"

# 2. 运行示例
cd hybrid_mode
go run main.go
```

### 预期输出

```text
=== CoT vs ReAct 性能对比 ===

【测试 1】使用 Chain-of-Thought Agent
状态: success
执行时间: 2.3s
推理步骤数: 4
最终答案: 9

【测试 2】使用 ReAct Agent
状态: success
执行时间: 8.7s
推理步骤数: 10
最终答案: 9

=== 性能对比总结 ===
CoT 执行时间:    2.3s
ReAct 执行时间:  8.7s
速度提升:        3.78x

CoT 推理步骤:    4
ReAct 推理步骤:  10
步骤减少:        60.0%
```

## 功能特性

所有示例都已完整实现以下功能：

### 内存管理支持

所有示例都已集成 `memory.Manager`，支持：

- 对话历史记录和上下文管理
- 基于案例的推理（Case-based Reasoning）
- 短期和长期记忆
- 内存配置和优化

```go
// 示例中使用的内存管理器
memoryManager := memory.NewInMemoryManager(memory.DefaultConfig())
```

### Planning 优化

`planning_execution/` 和 `hybrid_mode/` 示例展示：

- 前瞻性规划能力
- 计划验证和优化
- 步骤依赖分析
- 并行执行识别

### 智能代理选择

`hybrid_mode/` 示例展示：

- 根据任务类型自动选择最优代理
- CoT vs ReAct 的智能切换
- 性能和成本的平衡优化

### 统一错误处理

所有示例都已采用统一的错误处理方式：

- 使用 `github.com/kart-io/goagent/errors` 包
- 结构化错误，包含错误代码、操作、组件、上下文
- 支持错误链和堆栈跟踪
- 便于监控和日志分析

详见 [ERROR_HANDLING_GUIDE.md](ERROR_HANDLING_GUIDE.md)

## 核心价值

这些示例提供重要价值：

1. **展示最佳实践** - 演示如何正确使用 GoAgent 的优化功能
2. **架构指导** - 展示如何组织复杂的多代理系统
3. **性能基准** - 提供性能优化的参考数据
4. **API 设计** - 展示理想的 API 设计模式
5. **完整实现** - 所有示例都可以直接运行和测试

## 实际可用的优化方案

当前可以直接使用的优化功能：

### 1. 使用 CoT 代替 ReAct

```go
import "github.com/kart-io/goagent/agents/cot"

agent := cot.NewCoTAgent(cot.CoTConfig{
    Name:     "efficient_agent",
    LLM:      llmClient,
    ZeroShot: true,
})
```

### 2. 使用 Planning 创建计划

```go
import "github.com/kart-io/goagent/planning"

planner := planning.NewSmartPlanner(llmClient, nil)
plan, _ := planner.CreatePlan(ctx, "任务描述", planning.PlanConstraints{
    MaxSteps: 10,
})
```

### 3. 优化计划执行

```go
// 验证计划
valid, issues, _ := planner.ValidatePlan(ctx, plan)

// 优化计划
optimizedPlan, _ := planner.OptimizePlan(ctx, plan)
```

## 文档参考

- [ReAct 优化指南](../../docs/guides/REACT_OPTIMIZATION_GUIDE.md) - 详细的理论和策略
- [README.md](./README.md) - 示例概览和使用说明
- [CLAUDE.md](../../CLAUDE.md) - 项目架构和开发指南

## 贡献

如果您实现了这些概念示例中的缺失部分，欢迎提交 PR！我们特别需要：

- [ ] Memory Manager 的完整实现
- [ ] Tool Registry 系统
- [ ] Planning Executor 的完整实现
- [ ] 更多工具实现（Shell, HTTP, File 等）

## 许可证

与 GoAgent 项目相同的许可证。
