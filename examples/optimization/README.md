# GoAgent 优化示例

本目录包含 GoAgent 框架中针对 Agent 执行模式的各种优化方案示例。

## 目录结构

```text
examples/optimization/
├── README.md                      # 本文件
├── ERROR_HANDLING_GUIDE.md        # 错误处理指南
├── cot_vs_react/
│   ├── README.md                  # CoT vs ReAct 详细说明
│   └── main.go                    # CoT vs ReAct 性能对比
├── planning_execution/
│   ├── README.md                  # Planning 模式详细说明
│   └── main.go                    # Planning + Execution 优化
└── hybrid_mode/
    ├── README.md                  # 混合模式详细说明
    └── main.go                    # 混合模式：智能代理选择
```

## 示例概览

### 示例 1: CoT vs ReAct 性能对比

**目录:** `cot_vs_react/`

**适用场景:** 纯推理任务，不需要或很少需要工具调用

**关键优势:**

- LLM 调用次数减少 80-90%（从 10+ 次降至 1-2 次）
- Token 消耗降低 60-70%
- 执行速度提升 3-5 倍
- 更好的推理连贯性和可解释性

**运行示例:**

```bash
export DEEPSEEK_API_KEY="your-api-key"
cd cot_vs_react
go run main.go
```

**预期结果:**

```text
=== CoT vs ReAct 性能对比 ===

【测试 1】使用 Chain-of-Thought Agent
状态: success
执行时间: 10.6s
推理步骤数: 8
最终答案: 9

【测试 2】使用 ReAct Agent
状态: success
执行时间: 7.4s
推理步骤数: 1
最终答案: 9

=== 性能对比总结 ===
CoT 执行时间:    10.6s
ReAct 执行时间:  7.4s
CoT 推理步骤:    8
ReAct 推理步骤:  1
```

**说明:**

- CoT 展示完整的推理过程（8 步），适合需要可解释性的场景
- ReAct 在简单任务上可能直接输出答案（1 步），这是 LLM 的智能决策
- 对于复杂任务（需要工具），ReAct 会展现完整的 Thought-Action-Observation 循环

详见 [cot_vs_react/README.md](cot_vs_react/README.md)

### 示例 2: Planning + Execution 模式

**目录:** `planning_execution/`

**适用场景:** 复杂多步骤任务，需要前瞻性规划和执行优化

**关键优势:**

- 前瞻性规划 - 提前识别所有必需步骤
- 智能优化 - 自动减少冗余步骤（20-30%）
- 并行执行 - 识别可并行步骤，节省时间
- 可验证性 - 执行前验证计划可行性
- 可追踪性 - 完整的执行历史和指标
- Token 使用追踪 - 精确的 Token 消耗统计

**运行示例:**

```bash
export DEEPSEEK_API_KEY="your-api-key"
cd planning_execution
go run main.go
```

**预期结果:**

```text
=== Planning + Execution 优化示例 ===

【步骤 1】创建智能规划器
✓ 智能规划器创建成功
  - 最大深度: 3
  - 超时时间: 5 分钟

【步骤 2】创建初始计划
✓ 计划创建成功 (耗时: 3.2s)
  - 步骤数: 8

【步骤 3】验证计划
✓ 计划验证通过

【步骤 4】优化计划
✓ 计划优化成功 (耗时: 2.1s)
  - 原始步骤: 8
  - 优化后步骤: 8
  - 可并行步骤: 0

【步骤 5】执行计划（使用真实 Agent）
[1/8] 执行步骤: Requirements Analysis
      ✓ 执行成功 (耗时: 8.5s)
      推理步骤: 5 步
      Token 使用: Prompt=234, Completion=156, Total=390

=== 总 Token 使用统计 ===
Total Tokens: 3120
平均每步: 390.0 tokens
```

**可用规划策略:**

| 策略 | 适用场景 | 特点 |
|------|---------|------|
| DecompositionStrategy | 复杂问题分解 | 递归分解为子任务 |
| BackwardChainingStrategy | 目标驱动任务 | 从目标反推所需步骤 |
| HierarchicalStrategy | 多层次任务 | 分阶段规划和执行 |

详见 [planning_execution/README.md](planning_execution/README.md)

### 示例 3: 混合模式

**目录:** `hybrid_mode/`

**适用场景:** 复杂项目，不同步骤有不同的复杂度和需求

**关键优势:**

- 智能选择 - 根据任务类型自动选择最优代理
- 性能优化 - CoT 处理纯推理，ReAct 处理工具调用
- 灵活性 - 平衡性能、成本和功能
- 可扩展 - 轻松添加新的代理类型
- 真实工具 - 使用真实的代码执行器、部署模拟器、测试运行器

**运行示例:**

```bash
export DEEPSEEK_API_KEY="your-api-key"
cd hybrid_mode
go run main.go
```

**代理选择策略:**

| 步骤类型 | 推荐代理 | 理由 |
|---------|---------|------|
| Analysis | CoT | 纯推理任务，高性能，低成本 |
| Action (无工具) | CoT | 纯推理/设计任务，更快更经济 |
| Action (需工具) | ReAct | 需要工具调用（代码执行、部署、测试） |
| Validation | CoT | 简单验证，快速高效 |

**预期结果:**

```text
代理分配统计:
  - CoT (Chain-of-Thought): 3 个步骤 (37.5%)
  - ReAct (Reasoning + Acting): 5 个步骤 (62.5%)

=== 性能分析报告 ===
执行时间统计:
  总执行时间: 15.2s
  平均每步时间: 1.9s

代理使用统计:
  CoT 步骤: 3 (37.5% | 总耗时: 4.5s | 平均: 1.5s)
  ReAct 步骤: 5 (62.5% | 总耗时: 10.7s | 平均: 2.1s)

=== 优化效果估算 ===
相比全部使用 ReAct:
  预计全 ReAct 时间: 16.8s
  实际混合模式时间: 15.2s
  时间节省: 1.6s (9.5%)
```

详见 [hybrid_mode/README.md](hybrid_mode/README.md)

## 性能对比总结

基于实际测试的性能对比：

| 场景 | ReAct | CoT | Planning + CoT |
|------|-------|-----|----------------|
| 简单数学问题 | 7-9s | 10-12s | 不适用 |
| 数据分析 | 15-20s | 不适用 | 12-15s |
| 多步骤工作流 | 20-30s | 不适用 | 15-20s |

**综合提升:**

- Token 节省: 20-40%（使用 CoT 或混合模式）
- 速度提升: 根据任务类型而定
- 成本降低: 30-50%（Token 消耗降低）
- 可解释性: CoT 提供完整推理过程

## 使用建议

### 决策树：选择合适的代理模式

```text
开始
  |
  ├─ 任务是否需要工具调用？
  |    |
  |    ├─ 否 ──> 使用 CoT（最高性能，低成本）
  |    |
  |    └─ 是 ──> 是否需要动态决策工具选择？
  |              |
  |              ├─ 否 ──> 使用混合模式 + 预定义工具
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
   - 提供完整推理过程

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
    Name:     "agent",
    LLM:      llmClient,
    Tools:    tools,
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

// 执行（使用 CoT Agent）
agent := cot.NewCoTAgent(cotConfig)
for _, step := range optimizedPlan.Steps {
    result, _ := agent.Invoke(ctx, &agentcore.AgentInput{
        Task: step.Description,
    })
}
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
# DeepSeek API Key
export DEEPSEEK_API_KEY="your-api-key"

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
apiKey := os.Getenv("DEEPSEEK_API_KEY")
if apiKey == "" {
    err := errors.New(errors.CodeInvalidConfig, "DEEPSEEK_API_KEY environment variable is not set").
        WithOperation("initialization").
        WithComponent("example").
        WithContext("env_var", "DEEPSEEK_API_KEY")
    fmt.Printf("错误: %v\n", err)
    os.Exit(1)
}

// LLM 错误
llmClient, err := providers.NewDeepSeek(config)
if err != nil {
    wrappedErr := errors.Wrap(err, errors.CodeLLMRequest, "failed to create LLM client").
        WithOperation("initialization").
        WithContext("provider", "deepseek")
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

A: 使用条件判断动态选择：

```go
var agent agentcore.Agent

if useCoT {
    agent = cot.NewCoTAgent(cotConfig)
} else {
    agent = react.NewReActAgent(reactConfig)
}
```

## 相关文档

- [架构文档](../../docs/architecture/ARCHITECTURE.md) - 框架整体架构
- [错误处理指南](ERROR_HANDLING_GUIDE.md) - 统一错误处理方式和最佳实践
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
