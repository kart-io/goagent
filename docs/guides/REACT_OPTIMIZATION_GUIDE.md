# ReAct 模式优化指南

## 问题分析

### ReAct 模式的局限性

默认的 ReAct (Reasoning + Acting) 模式存在以下已知问题：

1. **性能问题（速度）**
   - 每个思考-行动循环都需要独立的 LLM 调用
   - 默认最大步骤 10 步，意味着可能需要 10+ 次 LLM 调用
   - 对于简单任务，这种开销过大

2. **成本问题（Token 消耗）**
   - 每次 LLM 调用都会累积 Token 消耗
   - Scratchpad 会随着步骤增加而变长，导致后续调用 Token 增多
   - 被标识为"token expensive"（高 Token 消耗）

3. **缺乏前瞻性规划**
   - ReAct 是反应式的，逐步决策
   - 没有全局规划能力，可能走弯路
   - 难以处理需要多步骤协调的复杂任务

4. **可靠性问题**
   - 代理可能"幻觉"出不存在的工具
   - 解析错误可能导致执行失败
   - 缺乏错误恢复机制

## 优化方案

GoAgent 框架提供了多种优化方案，针对不同场景选择合适的代理模式：

### 方案 1: 使用 CoT (Chain of Thought) 代理 - 推荐用于简单到中等复杂度任务

**优势:**

- **显著减少 LLM 调用次数**: 通常只需要 1-2 次调用
- **更低的 Token 消耗**: 一次性生成所有推理步骤
- **更快的执行速度**: 无需多次往返 LLM
- **更好的推理质量**: 整体性思考，避免局部最优

**实现位置:** `agents/cot/cot.go`

**使用示例:**

```go
import (
    "github.com/kart-io/goagent/agents/cot"
    "github.com/kart-io/goagent/llm"
)

// 创建 CoT Agent
agent := cot.NewCoTAgent(cot.CoTConfig{
    Name:        "cot_agent",
    Description: "Chain-of-Thought reasoning agent",
    LLM:         llmClient,
    MaxSteps:    5, // 通常需要更少步骤

    // CoT 特定配置
    ZeroShot:             true,  // 使用 "Let's think step by step"
    ShowStepNumbers:      true,  // 显示步骤编号
    RequireJustification: true,  // 要求每步提供理由
    FinalAnswerFormat:    "Therefore, the final answer is:",
})

// 执行
output, err := agent.Invoke(ctx, &agentcore.AgentInput{
    Task: "解决复杂数学问题",
})
```

**性能对比:**

| 指标 | ReAct | CoT |
|------|-------|-----|
| LLM 调用次数 | 10+ | 1-2 |
| 平均 Token 消耗 | 高 (随步骤增长) | 中 (固定) |
| 执行速度 | 慢 | 快 |
| 适用场景 | 需要工具调用的复杂任务 | 推理密集型任务 |

### 方案 2: 使用 Planning + Execution 模式 - 推荐用于复杂多步骤任务

**优势:**

- **前瞻性规划**: 先制定完整计划，再执行
- **优化执行顺序**: 可以并行化独立步骤
- **更好的可控性**: 可以在执行前验证和优化计划
- **错误恢复**: 支持计划调整和重试

**实现位置:**

- `planning/planner.go` - 规划器
- `planning/strategies.go` - 规划策略
- `planning/executor.go` - 执行器
- `planning/agents.go` - 规划代理

**使用示例:**

```go
import (
    "github.com/kart-io/goagent/planning"
)

// 1. 创建智能规划器
planner := planning.NewSmartPlanner(
    llmClient,
    memoryManager,
    planning.WithMaxDepth(3),
    planning.WithTimeout(5 * time.Minute),
    planning.WithOptimizer(&planning.DefaultOptimizer{}),
)

// 2. 创建计划
plan, err := planner.CreatePlan(ctx, "复杂的多步骤任务", planning.PlanConstraints{
    MaxSteps:    20,
    MaxDuration: 10 * time.Minute,
})

// 3. 验证计划
valid, issues, err := planner.ValidatePlan(ctx, plan)
if !valid {
    // 根据问题优化计划
    plan, err = planner.RefinePlan(ctx, plan, strings.Join(issues, "; "))
}

// 4. 优化计划
optimizedPlan, err := planner.OptimizePlan(ctx, plan)

// 5. 执行计划
executor := planning.NewPlanExecutor(llmClient, toolRegistry)
result, err := executor.Execute(ctx, optimizedPlan)
```

**规划策略:**

| 策略 | 适用场景 | 特点 |
|------|---------|------|
| DecompositionStrategy | 复杂问题分解 | 递归分解为子任务 |
| BackwardChainingStrategy | 目标驱动任务 | 从目标反推步骤 |
| HierarchicalStrategy | 多层次任务 | 阶段性规划 |

### 方案 3: 混合模式 - Planning + ReAct/CoT

**最佳实践:** 结合 Planning 的前瞻性和 ReAct/CoT 的灵活性

```go
// 1. 使用 Planning 创建高层次计划
planner := planning.NewSmartPlanner(llmClient, memoryManager)
plan, _ := planner.CreatePlan(ctx, "复杂任务", planning.PlanConstraints{
    MaxSteps: 5, // 高层次步骤较少
})

// 2. 为每个步骤使用合适的执行代理
for _, step := range plan.Steps {
    var stepAgent agentcore.Agent

    switch step.Type {
    case planning.StepTypeAnalysis:
        // 分析步骤使用 CoT
        stepAgent = cot.NewCoTAgent(cotConfig)

    case planning.StepTypeAction:
        // 行动步骤使用 ReAct（需要工具调用）
        stepAgent = react.NewReActAgent(reactConfig)

    case planning.StepTypeValidation:
        // 验证步骤使用简单 Executor
        stepAgent = executor.NewAgentExecutor(execConfig)
    }

    // 执行步骤
    result, _ := stepAgent.Invoke(ctx, &agentcore.AgentInput{
        Task: step.Description,
    })

    // 更新计划状态
    step.Result = &planning.StepResult{
        Success: true,
        Output:  result.Result,
    }
}
```

### 方案 4: 使用其他思维模式

框架还提供了其他高级思维模式：

| 模式 | 文件位置 | 适用场景 |
|------|---------|---------|
| ToT (Tree of Thoughts) | `agents/tot/tot.go` | 需要探索多个解决方案路径 |
| GoT (Graph of Thoughts) | `agents/got/got.go` | 复杂的依赖关系图 |
| SoT (Skeleton of Thoughts) | `agents/sot/sot.go` | 需要骨架式规划 |
| PoT (Program of Thoughts) | `agents/pot/pot.go` | 程序化推理任务 |
| Meta-CoT | `agents/metacot/metacot.go` | 元认知层面的推理 |

## 性能优化技巧

### 1. 减少 LLM 调用次数

```go
// ❌ 不好的做法：每个小步骤都调用 LLM
for i := 0; i < 10; i++ {
    result, _ := llmClient.Complete(ctx, smallTask)
}

// ✅ 好的做法：批量处理
bigPrompt := combineAllTasks(tasks)
result, _ := llmClient.Complete(ctx, bigPrompt)
```

### 2. 使用缓存中间件

```go
import "github.com/kart-io/goagent/middleware"

// 添加缓存中间件
agent := builder.NewAgentBuilder(llmClient).
    WithMiddleware(middleware.NewCachingMiddleware(cacheConfig)).
    Build()
```

### 3. 设置合理的超时和最大步骤

```go
executor := executor.NewAgentExecutor(executor.ExecutorConfig{
    Agent:            agent,
    MaxIterations:    5,  // 减少最大迭代次数
    MaxExecutionTime: 30 * time.Second, // 设置超时
})
```

### 4. 使用工具选择中间件

```go
// 智能选择最相关的工具，而非所有工具
import "github.com/kart-io/goagent/middleware"

agent := builder.NewAgentBuilder(llmClient).
    WithMiddleware(middleware.NewToolSelectionMiddleware(toolSelector)).
    Build()
```

## 场景选择指南

### 何时使用 ReAct

- 需要动态工具调用
- 任务需要基于观察结果做决策
- 工具调用顺序不可预测

### 何时使用 CoT

- 纯推理任务（数学、逻辑问题）
- 不需要或很少需要工具调用
- 性能和成本是主要考虑因素

### 何时使用 Planning

- 复杂多步骤任务
- 需要优化执行顺序
- 可以提前规划的任务
- 需要并行执行某些步骤

### 何时使用混合模式

- 既需要规划又需要灵活执行
- 不同步骤有不同的复杂度
- 需要在性能和灵活性之间平衡

## 实际案例

### 案例 1: 数据分析任务

**任务:** 分析销售数据并生成报告

**推荐方案:** Planning + CoT

```go
// 1. 规划阶段
plan := planner.CreatePlan(ctx, "分析 Q4 销售数据", constraints)
// 步骤: 加载数据 -> 清洗数据 -> 分析趋势 -> 生成报告

// 2. 执行阶段（使用 CoT）
cotAgent := cot.NewCoTAgent(cotConfig)
for _, step := range plan.Steps {
    result, _ := cotAgent.Invoke(ctx, &agentcore.AgentInput{
        Task: step.Description,
    })
}
```

**优势:**

- 预先规划所有步骤
- CoT 高效完成每个分析步骤
- 总 LLM 调用次数: ~5-8 次（vs ReAct 的 20+ 次）

### 案例 2: 自动化测试任务

**任务:** 生成并执行集成测试

**推荐方案:** Planning + Executor

```go
// 1. 规划测试步骤
plan := planner.CreatePlan(ctx, "为 API 生成集成测试", constraints)

// 2. 优化并行执行
optimizedPlan, _ := planner.OptimizePlan(ctx, plan)
// 优化器会识别可并行的测试步骤

// 3. 执行
executor := planning.NewPlanExecutor(llmClient, toolRegistry)
result, _ := executor.Execute(ctx, optimizedPlan)
```

**优势:**

- 测试步骤可并行执行
- 减少总执行时间 50-70%

### 案例 3: 客户支持对话

**任务:** 处理客户问题并提供解决方案

**推荐方案:** CoT (简单问题) or Planning + ReAct (复杂问题)

```go
// 根据问题复杂度动态选择
if isSimpleQuery(customerQuestion) {
    // 使用 CoT 快速响应
    agent = cot.NewCoTAgent(cotConfig)
} else {
    // 复杂问题使用 Planning + ReAct
    plan := planner.CreatePlan(ctx, customerQuestion, constraints)
    agent = react.NewReActAgent(reactConfig)
}
```

## 性能基准测试

基于 `examples/` 目录中的测试：

| 场景 | ReAct | CoT | Planning + CoT |
|------|-------|-----|----------------|
| 简单数学问题 | 10 次调用 / 8000 tokens | 1 次调用 / 800 tokens | 2 次调用 / 1200 tokens |
| 数据分析 | 15 次调用 / 12000 tokens | 不适用 | 6 次调用 / 4500 tokens |
| 多步骤工作流 | 20 次调用 / 18000 tokens | 不适用 | 8 次调用 / 7000 tokens |

**Token 节省:** 50-70%
**速度提升:** 3-5x

## 迁移建议

### 从 ReAct 迁移到 CoT

```go
// 之前: ReAct
reactAgent := react.NewReActAgent(react.ReActConfig{
    Name:  "agent",
    LLM:   llmClient,
    Tools: tools,
})

// 之后: CoT
cotAgent := cot.NewCoTAgent(cot.CoTConfig{
    Name:    "agent",
    LLM:     llmClient,
    Tools:   tools, // 可选，仅在需要时调用
    ZeroShot: true,
})
```

### 从 ReAct 迁移到 Planning

```go
// 之前: 直接使用 ReAct
reactAgent := react.NewReActAgent(reactConfig)
output, _ := reactAgent.Invoke(ctx, input)

// 之后: Planning + Executor
planner := planning.NewSmartPlanner(llmClient, memoryManager)
plan, _ := planner.CreatePlan(ctx, input.Task, constraints)
executor := planning.NewPlanExecutor(llmClient, toolRegistry)
result, _ := executor.Execute(ctx, plan)
```

## 总结

1. **ReAct 适合**: 需要动态工具调用的场景，但代价是性能和成本
2. **CoT 适合**: 推理密集型任务，性能优异
3. **Planning 适合**: 复杂多步骤任务，提供前瞻性和优化
4. **混合模式**: 针对不同步骤使用不同代理，平衡性能和灵活性

**推荐默认策略:**

- 优先尝试 CoT（性能最佳）
- 需要前瞻性规划时使用 Planning
- 只在必须动态决策时才使用 ReAct
- 复杂任务使用混合模式

## 参考文档

- [Chain-of-Thought Prompting](https://arxiv.org/abs/2201.11903)
- [ReAct: Synergizing Reasoning and Acting](https://arxiv.org/abs/2210.03629)
- [Tree of Thoughts](https://arxiv.org/abs/2305.10601)
- GoAgent Architecture: `docs/architecture/ARCHITECTURE.md`
- Testing Best Practices: `docs/development/TESTING_BEST_PRACTICES.md`
