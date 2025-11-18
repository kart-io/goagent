# Planning + Execution 优化示例

## 概述

本示例演示如何使用 Planning 模块对复杂多步骤任务进行前瞻性规划和优化，然后使用 CoT Agent 执行计划中的每个步骤。

## 核心功能

### 1. 智能规划器

创建高层次的执行计划，包含：

- **任务分解** - 将复杂任务分解为可执行的步骤
- **依赖分析** - 识别步骤之间的依赖关系
- **并行识别** - 发现可以并行执行的步骤
- **优先级排序** - 根据重要性和依赖关系排序

### 2. 计划验证

执行前验证计划的可行性：

- 检查循环依赖
- 验证步骤完整性
- 确保资源可用性
- 评估时间预算

### 3. 计划优化

自动优化执行计划：

- 减少冗余步骤（20-30%）
- 识别可并行执行的步骤
- 调整执行顺序
- 优化资源分配

### 4. 真实执行

使用 CoT Agent 执行每个步骤：

- 完整的推理过程
- Token 使用统计
- 执行时间追踪
- 成功率监控

## 运行方式

```bash
export DEEPSEEK_API_KEY=your_api_key
cd planning_execution
go run main.go
```

## 实现细节

### Planning 策略

本示例使用 `SmartPlanner`，支持多种规划策略：

1. **分解策略** (DecompositionStrategy)
   - 递归分解复杂任务
   - 适用于可分解的问题

2. **后向链接** (BackwardChainingStrategy)
   - 从目标反推所需步骤
   - 适用于目标明确的任务

3. **分层策略** (HierarchicalStrategy)
   - 分阶段规划和执行
   - 适用于多层次任务

### 示例任务

示例使用一个数据分析任务：

```
分析 2024 年 Q4 的销售数据，并生成综合报告
```

Planning 会将其分解为：

1. 加载并清洗销售数据
2. 分析销售趋势（按产品、地区、时间）
3. 识别最好和最差产品
4. 分析客户行为模式
5. 生成可视化图表
6. 撰写执行摘要
7. 提供改进建议

### 执行流程

```text
┌─────────────────────────┐
│ 1. 创建智能规划器        │
│    - 配置最大深度        │
│    - 设置超时时间        │
│    - 启用优化器          │
└───────────┬─────────────┘
            │
            v
┌─────────────────────────┐
│ 2. 创建初始计划          │
│    - 分析任务需求        │
│    - 生成步骤列表        │
│    - 设置约束条件        │
└───────────┬─────────────┘
            │
            v
┌─────────────────────────┐
│ 3. 验证计划              │
│    - 检查循环依赖        │
│    - 验证步骤完整性      │
│    - 评估可行性          │
└───────────┬─────────────┘
            │
            v
┌─────────────────────────┐
│ 4. 优化计划              │
│    - 减少冗余步骤        │
│    - 识别并行机会        │
│    - 优化执行顺序        │
└───────────┬─────────────┘
            │
            v
┌─────────────────────────┐
│ 5. 执行计划              │
│    - 逐步执行 CoT        │
│    - 追踪 Token 使用     │
│    - 监控成功率          │
└─────────────────────────┘
```

## 预期输出

### 规划阶段

```text
=== Planning + Execution 优化示例 ===

【步骤 1】创建智能规划器
✓ 智能规划器创建成功
  - 最大深度: 3
  - 超时时间: 5 分钟
  - 内存支持: 已启用
  - 已注册策略: decomposition, backward_chaining, hierarchical

【步骤 2】创建初始计划
✓ 计划创建成功 (耗时: 3.2s)
  - 计划 ID: plan_1700123456
  - 策略: Hierarchical decomposition
  - 步骤数: 8

步骤列表:
  1. [analysis] 加载并清洗销售数据
  2. [analysis] 分析销售趋势
  3. [analysis] 识别最佳和最差产品
  4. [analysis] 分析客户行为模式
  5. [action] 生成可视化图表
  6. [action] 撰写执行摘要
  7. [action] 提供改进建议
  8. [validation] 审核报告质量

【步骤 3】验证计划
✓ 计划验证通过

【步骤 4】优化计划
✓ 计划优化成功 (耗时: 2.1s)
  - 原始步骤: 8
  - 优化后步骤: 8
  - 可并行步骤: 0
```

### 执行阶段

```text
【步骤 5】执行计划（使用真实 Agent）

[1/8] 执行步骤: 加载并清洗销售数据
      类型: analysis
      描述: Load Q4 2024 sales data from database, clean missing values...
      ✓ 执行成功 (耗时: 8.5s)
      推理步骤: 5 步
      Token 使用: Prompt=234, Completion=156, Total=390
      结果: 数据加载完成，共 15,234 条记录，清洗后保留 15,100 条有效记录...

[2/8] 执行步骤: 分析销售趋势
      类型: analysis
      描述: Analyze sales trends by product category, region, and time...
      ✓ 执行成功 (耗时: 9.2s)
      推理步骤: 6 步
      Token 使用: Prompt=267, Completion=189, Total=456
      结果: 趋势分析完成：Q4 整体增长 15%，电子产品类别表现最佳（+25%）...

[3/8] 执行步骤: 识别最佳和最差产品
      ✓ 执行成功 (耗时: 7.8s)
      推理步骤: 4 步
      Token 使用: Prompt=198, Completion=134, Total=332

...

✓ 计划执行完成
```

### 总结报告

```text
=== 执行总结 ===
计划 ID: plan_1700123456
总步骤: 8
已完成: 8
失败: 0
成功率: 100.0%
总耗时: 68.4s

=== Token 使用总结 ===
Prompt Tokens: 1872
Completion Tokens: 1248
Total Tokens: 3120
平均每步: 390.0 tokens

=== Planning 模式优势 ===
1. ✓ 前瞻性规划：提前识别所有必需步骤
2. ✓ 智能优化：自动减少冗余步骤
3. ✓ 并行执行：识别可并行步骤，节省时间
4. ✓ 可验证性：执行前验证计划可行性
5. ✓ 可追踪性：完整的执行历史和指标
6. ✓ Token 追踪：精确的 Token 使用统计
```

## 关键优势

### 1. 前瞻性规划

与 ReAct 的逐步推理不同，Planning 在执行前就完成完整规划：

**ReAct 模式：**
- 步骤 1 → 思考 → 行动 → 观察
- 步骤 2 → 思考 → 行动 → 观察
- ...

**Planning 模式：**
- 一次性规划所有步骤
- 验证整体可行性
- 优化执行顺序
- 然后执行

### 2. 减少冗余

Planning 可以识别并消除冗余步骤：

```text
原始计划（10 步）:
1. 加载数据
2. 清洗数据
3. 验证数据质量
4. 再次清洗数据（冗余）
5. 分析趋势
6. 生成图表
7. 验证图表（冗余）
8. 撰写报告
9. 审核报告
10. 最终确认（可合并到步骤 9）

优化后（8 步）:
1. 加载并清洗数据（合并 1, 2）
2. 验证数据质量
3. 分析趋势
4. 生成可视化图表
5. 撰写报告
6. 审核报告质量（合并 9, 10）
```

### 3. 并行执行机会

Planning 可以识别可并行的步骤：

```text
串行执行：
步骤 A → 步骤 B → 步骤 C → 步骤 D
总时间：10s + 8s + 12s + 6s = 36s

并行执行：
步骤 A → 步骤 B ┐
                ├→ 步骤 D
步骤 C ─────────┘
总时间：max(10s+8s, 12s) + 6s = 24s
节省：33%
```

### 4. Token 使用追踪

精确追踪每个步骤的 Token 消耗：

- Prompt Tokens - 输入消耗
- Completion Tokens - 输出消耗
- Total Tokens - 总消耗
- 平均每步消耗
- 成本估算

## 最佳实践

### 1. 任务描述要清晰

```text
✅ 好的描述:
分析 2024 年 Q4 的销售数据，生成包含趋势分析、产品对比、客户行为和改进建议的综合报告

❌ 不好的描述:
做一个销售报告
```

### 2. 设置合理的约束

```go
constraints := planning.PlanConstraints{
    MaxSteps:    20,               // 最大步骤数
    MaxDuration: 30 * time.Minute, // 最大执行时间
}
```

### 3. 选择合适的规划器配置

```go
planner := planning.NewSmartPlanner(
    llmClient,
    memoryManager,
    planning.WithMaxDepth(3),            // 递归深度
    planning.WithTimeout(5*time.Minute), // 规划超时
    planning.WithOptimizer(&planning.DefaultOptimizer{}), // 启用优化
)
```

### 4. 处理执行失败

```go
for _, step := range plan.Steps {
    output, err := agent.Invoke(ctx, input)
    if err != nil {
        // 记录错误但继续执行其他步骤
        step.Status = planning.StepStatusFailed
        step.Result = &planning.StepResult{
            Success: false,
            Output:  err.Error(),
        }
        continue
    }
    // 处理成功情况
}
```

## 与其他模式的对比

### vs. ReAct

| 特性 | Planning + CoT | ReAct |
|------|---------------|-------|
| 规划方式 | 前瞻性规划 | 逐步推理 |
| 步骤优化 | 自动减少冗余 | 可能重复 |
| 并行执行 | 支持识别 | 不支持 |
| Token 消耗 | 较低（预先规划） | 较高（多次调用） |
| 适用场景 | 复杂多步骤任务 | 需要动态决策的任务 |

### vs. 纯 CoT

| 特性 | Planning + CoT | 纯 CoT |
|------|---------------|--------|
| 任务分解 | 自动分解 | 需要手动 |
| 步骤追踪 | 详细追踪 | 有限 |
| 执行优化 | 自动优化 | 无 |
| 复杂度上限 | 高（多步骤） | 中（有限步骤） |

## 使用场景

### 适合使用 Planning 的场景

1. **数据分析项目**
   - 多个分析阶段
   - 需要生成报告
   - 可预先规划步骤

2. **系统部署**
   - 多个部署阶段
   - 有依赖关系
   - 需要验证每步

3. **内容生成**
   - 研究 → 大纲 → 撰写 → 审核
   - 步骤明确
   - 可并行处理

4. **复杂工作流**
   - 多个独立任务
   - 需要协调
   - 可优化顺序

### 不适合使用 Planning 的场景

1. **需要动态决策**
   - 下一步取决于当前结果
   - 无法预先规划

2. **简单任务**
   - 1-3 步就能完成
   - 规划开销大于收益

3. **实时响应**
   - 需要立即反馈
   - 不能等待规划完成

## 扩展方向

### 1. 自定义规划策略

```go
type CustomStrategy struct{}

func (s *CustomStrategy) GenerateSteps(ctx context.Context, task string) ([]*planning.Step, error) {
    // 自定义步骤生成逻辑
    return steps, nil
}
```

### 2. 计划持久化

```go
// 保存计划
planJSON, _ := json.Marshal(plan)
ioutil.WriteFile("plan.json", planJSON, 0644)

// 恢复计划
planJSON, _ := ioutil.ReadFile("plan.json")
var plan planning.Plan
json.Unmarshal(planJSON, &plan)
```

### 3. 并行执行实现

```go
// 使用 goroutine 并行执行可并行的步骤
var wg sync.WaitGroup
for _, step := range parallelSteps {
    wg.Add(1)
    go func(s *planning.Step) {
        defer wg.Done()
        agent.Invoke(ctx, &agentcore.AgentInput{
            Task: s.Description,
        })
    }(step)
}
wg.Wait()
```

### 4. 动态调整计划

```go
// 在执行过程中根据结果调整计划
if step.Result.Success {
    // 继续原计划
} else {
    // 添加修复步骤
    fixStep := &planning.Step{
        ID:          "fix_" + step.ID,
        Name:        "Fix " + step.Name,
        Description: "Fix the failed step",
    }
    plan.Steps = append(plan.Steps, fixStep)
}
```

## 参考文档

- [Planning 模块文档](../../../planning/)
- [CoT Agent 文档](../../../agents/cot/)
- [SmartPlanner API](../../../planning/smart_planner.go)
- [混合模式示例](../hybrid_mode/)

## 总结

Planning + Execution 模式最适合：

- ✅ 复杂多步骤任务
- ✅ 可预先规划的工作流
- ✅ 需要优化执行效率的场景
- ✅ 需要详细追踪和 Token 统计的项目

通过前瞻性规划、自动优化和精确追踪，Planning 模式为复杂任务执行提供了强大的支持。
