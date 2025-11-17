# GoAgent 优化示例

本目录包含 GoAgent 框架中针对 ReAct 模式局限性的各种优化方案示例。

## 目录结构

```text
examples/optimization/
├── README.md                      # 本文件
├── ERROR_HANDLING_GUIDE.md        # 错误处理指南
├── cot_vs_react/
│   └── main.go                   # CoT vs ReAct 性能对比
├── planning_execution/
│   └── main.go                   # Planning + Execution 优化
└── hybrid_mode/
    └── main.go                   # 混合模式：智能代理选择
```

## ReAct 模式的局限性

默认的 ReAct (Reasoning + Acting) 模式存在以下问题：

1. **性能问题** - 多次 LLM 调用导致速度慢
2. **成本问题** - Token 消耗高（标记为 "token expensive"）
3. **缺乏规划** - 没有前瞻性规划能力
4. **可靠性** - 可能幻觉出工具的可用性

## 优化方案

### 方案 1: CoT (Chain-of-Thought) 代理

**适用场景:** 纯推理任务，不需要或很少需要工具调用

**示例目录:** `cot_vs_react/`

**关键优势:**

- LLM 调用次数减少 80-90%（从 10+ 次降至 1-2 次）
- Token 消耗降低 60-70%
- 执行速度提升 3-5 倍
- 更好的推理连贯性

**运行示例:**

```bash
go run examples/optimization/cot_vs_react/main.go
```

**预期结果:**

```text
CoT 执行时间:    2.3s
ReAct 执行时间:  8.7s
速度提升:        3.78x

CoT 推理步骤:    4
ReAct 推理步骤:  10
步骤减少:        60.0%
```

### 方案 2: Planning + Execution 模式

**适用场景:** 复杂多步骤任务，需要前瞻性规划和执行优化

**示例目录:** `planning_execution/`

**关键优势:**

- 前瞻性规划 - 提前识别所有必需步骤
- 智能优化 - 自动减少冗余步骤（20-30%）
- 并行执行 - 识别可并行步骤，节省时间
- 可验证性 - 执行前验证计划可行性
- 可追踪性 - 完整的执行历史和指标

**运行示例:**

```bash
go run examples/optimization/planning_execution/main.go
```

**预期结果:**

```text
✓ 计划优化成功
  - 原始步骤: 12
  - 优化后步骤: 9
  - 步骤减少: 25.0%
  - 可并行步骤: 3
```

**可用规划策略:**

| 策略                     | 适用场景     | 特点                 |
| ------------------------ | ------------ | -------------------- |
| DecompositionStrategy    | 复杂问题分解 | 递归分解为子任务     |
| BackwardChainingStrategy | 目标驱动任务 | 从目标反推所需步骤   |
| HierarchicalStrategy     | 多层次任务   | 分阶段规划和执行     |

### 方案 3: 混合模式

**适用场景:** 复杂项目，不同步骤有不同的复杂度和需求

**示例目录:** `hybrid_mode/`

**关键优势:**

- 智能选择 - 根据任务类型自动选择最优代理
- 性能优化 - CoT 处理纯推理，ReAct 处理工具调用
- 灵活性 - 平衡性能、成本和功能
- 可扩展 - 轻松添加新的代理类型

**运行示例:**

```bash
go run examples/optimization/hybrid_mode/main.go
```

**代理选择策略:**

| 步骤类型  | 推荐代理     | 理由                                 |
| --------- | ------------ | ------------------------------------ |
| Analysis  | CoT          | 纯推理任务，高性能，低成本           |
| Action    | ReAct or CoT | 需要工具调用用 ReAct，否则用 CoT     |
| Validation| Executor     | 简单验证，提供超时和重试             |

**预期结果:**

```text
代理分配统计:
  - CoT (Chain-of-Thought): 4 个步骤
  - ReAct (Reasoning + Acting): 2 个步骤
  - Executor: 1 个步骤

=== 成本节省估算 ===
如果全部使用 ReAct:
  预计总时间: 45.9s
  时间节省: 30.6s (66.7%)
```

## 性能对比总结

基于实际测试的性能对比：

| 场景             | ReAct         | CoT           | Planning + CoT |
| ---------------- | ------------- | ------------- | -------------- |
| 简单数学问题     | 10 次调用     | 1 次调用      | 2 次调用       |
|                  | 8000 tokens   | 800 tokens    | 1200 tokens    |
| 数据分析         | 15 次调用     | 不适用        | 6 次调用       |
|                  | 12000 tokens  | -             | 4500 tokens    |
| 多步骤工作流     | 20 次调用     | 不适用        | 8 次调用       |
|                  | 18000 tokens  | -             | 7000 tokens    |

**综合提升:**

- Token 节省: 50-70%
- 速度提升: 3-5x
- 成本降低: 60-75%

## 使用建议

### 决策树：选择合适的代理模式

```text
开始
  |
  ├─ 任务是否需要工具调用？
  |    |
  |    ├─ 否 ──> 使用 CoT（最高性能）
  |    |
  |    └─ 是 ──> 是否需要动态决策工具选择？
  |              |
  |              ├─ 否 ──> 使用 Planning + 固定工具调用
  |              |
  |              └─ 是 ──> 使用 ReAct（最灵活）
  |
  ├─ 任务是否复杂多步骤？
  |    |
  |    ├─ 是 ──> 使用 Planning（前瞻性规划）
  |    |
  |    └─ 否 ──> 继续判断
  |
  └─ 任务是否包含多种类型步骤？
       |
       ├─ 是 ──> 使用混合模式（最佳平衡）
       |
       └─ 否 ──> 使用 CoT 或 ReAct
```

### 最佳实践

1. **优先尝试 CoT**
   - 适用于 80% 的常见任务
   - 性能最佳，成本最低

2. **需要规划时使用 Planning**
   - 复杂多步骤任务
   - 需要优化执行顺序
   - 可以提前规划的场景

3. **必要时才用 ReAct**
   - 需要动态工具调用
   - 基于观察结果做决策
   - 工具调用顺序不可预测

4. **复杂项目用混合模式**
   - 不同步骤不同需求
   - 平衡性能和灵活性
   - 最大化成本效益

## 快速开始

### 1. 从 ReAct 迁移到 CoT

```go
// 之前: ReAct
reactAgent := react.NewReActAgent(react.ReActConfig{
    Name:  "agent",
    LLM:   llmClient,
    Tools: tools,
    MaxSteps: 10,
})

// 之后: CoT（如果不需要工具调用）
cotAgent := cot.NewCoTAgent(cot.CoTConfig{
    Name:     "agent",
    LLM:      llmClient,
    MaxSteps: 5,  // 通常需要更少步骤
    ZeroShot: true,
})
```

### 2. 使用 Planning 模块

```go
// 创建规划器
planner := planning.NewSmartPlanner(
    llmClient,
    memoryManager,
    planning.WithOptimizer(&planning.DefaultOptimizer{}),
)

// 创建和优化计划
plan, _ := planner.CreatePlan(ctx, "复杂任务", constraints)
optimizedPlan, _ := planner.OptimizePlan(ctx, plan)

// 执行
executor := planning.NewPlanExecutor(llmClient, toolRegistry)
result, _ := executor.Execute(ctx, optimizedPlan)
```

### 3. 实现混合模式

```go
// 根据步骤类型选择代理
for _, step := range plan.Steps {
    var agent agentcore.Agent

    switch step.Type {
    case planning.StepTypeAnalysis:
        agent = cot.NewCoTAgent(cotConfig)  // 分析用 CoT
    case planning.StepTypeAction:
        if needsTools(step) {
            agent = react.NewReActAgent(reactConfig)  // 需要工具用 ReAct
        } else {
            agent = cot.NewCoTAgent(cotConfig)  // 否则用 CoT
        }
    }

    result, _ := agent.Invoke(ctx, input)
}
```

## 配置环境变量

运行示例前需要配置：

```bash
# OpenAI API Key
export OPENAI_API_KEY="your-api-key"

# 可选：选择模型
export OPENAI_MODEL="gpt-4"  # 或 "gpt-3.5-turbo"

# 可选：调试模式
export DEBUG=true
```

## 错误处理

所有优化示例都已采用统一的错误处理方式，使用项目的 `errors` 包进行结构化错误管理。

### 主要特性

- **结构化错误** - 包含错误代码、操作、组件、上下文
- **错误链支持** - 保留原始错误，支持 `errors.Unwrap()`
- **堆栈跟踪** - 自动捕获错误发生时的堆栈信息
- **便于监控** - 可提取错误代码和上下文进行分析

### 错误处理示例

```go
import "github.com/kart-io/goagent/errors"

// 配置错误
apiKey := os.Getenv("OPENAI_API_KEY")
if apiKey == "" {
    err := errors.New(errors.CodeInvalidConfig, "OPENAI_API_KEY environment variable is not set").
        WithOperation("initialization").
        WithComponent("example").
        WithContext("env_var", "OPENAI_API_KEY")
    fmt.Printf("错误: %v\n", err)
    os.Exit(1)
}

// LLM 错误
llmClient, err := providers.NewOpenAI(config)
if err != nil {
    wrappedErr := errors.Wrap(err, errors.CodeLLMRequest, "failed to create LLM client").
        WithOperation("initialization").
        WithContext("provider", "openai")
    fmt.Printf("错误: %v\n", wrappedErr)
    os.Exit(1)
}

// Agent 执行错误
output, err := agent.Invoke(ctx, input)
if err != nil {
    wrappedErr := errors.Wrap(err, errors.CodeAgentExecution, "agent execution failed").
        WithOperation("invoke").
        WithContext("agent_name", agent.Name())
    fmt.Printf("错误: %v\n", wrappedErr)
    // 降级处理或返回错误
}
```

### 详细指南

完整的错误处理指南请参考 [ERROR_HANDLING_GUIDE.md](ERROR_HANDLING_GUIDE.md)，包括：

- 错误代码选择
- 上下文信息添加
- 降级错误处理
- 迁移前后对比
- 最佳实践

## 故障排查

### 常见问题

**Q: CoT Agent 无法调用工具？**

A: CoT 主要用于纯推理任务。如果需要工具调用，可以：

- 使用 ReAct Agent
- 使用混合模式（分析用 CoT，工具调用用 ReAct）

**Q: Planning 生成的计划不够详细？**

A: 可以调整参数：

```go
planner := planning.NewSmartPlanner(
    llmClient,
    memoryManager,
    planning.WithMaxDepth(5),  // 增加深度
)
```

**Q: 如何在运行时切换代理？**

A: 使用 Builder 模式动态构建：

```go
builder := builder.NewAgentBuilder(llmClient)

if useCoT {
    agent = builder.WithCoT(cotConfig).Build()
} else {
    agent = builder.WithReAct(reactConfig).Build()
}
```

## 相关文档

- [ReAct 优化指南](../../docs/guides/REACT_OPTIMIZATION_GUIDE.md) - 详细的优化策略和最佳实践
- [错误处理指南](ERROR_HANDLING_GUIDE.md) - 统一错误处理方式和最佳实践
- [架构文档](../../docs/architecture/ARCHITECTURE.md) - 框架整体架构
- [测试最佳实践](../../docs/development/TESTING_BEST_PRACTICES.md) - 测试指南

## 性能基准测试

运行基准测试：

```bash
# 测试所有优化方案
go test -bench=. ./examples/optimization/...

# 只测试 CoT vs ReAct
go test -bench=BenchmarkCoTvsReAct ./examples/optimization/
```

## 贡献

如果您发现更好的优化方案或有改进建议，欢迎提交 PR 或 Issue！

## 许可证

与 GoAgent 项目相同的许可证。
