# Agent 实现文档

GoAgent 框架提供多种高级推理模式的 Agent 实现，每种都针对不同类型的问题优化。

## 📚 目录

- [推理模式概览](#推理模式概览)
- [ReAct Agent](#react-agent) - 推理与行动循环
- [Chain-of-Thought (CoT)](#chain-of-thought-cot) - 思维链推理
- [Tree-of-Thought (ToT)](#tree-of-thought-tot) - 树状思维探索
- [Graph-of-Thought (GoT)](#graph-of-thought-got) - 图状思维推理
- [Program-of-Thought (PoT)](#program-of-thought-pot) - 程序化思维
- [Skeleton-of-Thought (SoT)](#skeleton-of-thought-sot) - 骨架式思维
- [Meta-CoT / Self-Ask](#meta-cot--self-ask) - 元推理与自我提问

---

## 推理模式概览

### 快速对比

| 推理模式 | 适用场景 | 优势 | 主要特性 |
|---------|---------|------|---------|
| **ReAct** | 需要工具调用的任务 | 灵活、可控 | 思考-行动-观察循环 |
| **CoT** | 需要逐步推理的问题 | 可解释性强 | 线性推理链 |
| **ToT** | 需要探索多个解决路径 | 全面性 | 树状搜索（DFS/BFS/Beam/MCTS） |
| **GoT** | 复杂的多依赖推理 | 并行处理 | DAG 结构、并行执行 |
| **PoT** | 数学、逻辑计算任务 | 精确计算 | 代码生成与执行 |
| **SoT** | 长篇内容生成 | 速度快 | 并行骨架展开 |
| **Meta-CoT** | 需要深度分析的复杂问题 | 自我完善 | 自我提问、自我批判 |

### 选择指南

```
需要工具调用？
├─ 是 → ReAct
└─ 否
   ├─ 需要数学计算？
   │  └─ 是 → PoT
   └─ 否
      ├─ 需要探索多个方案？
      │  └─ 是 → ToT 或 GoT
      └─ 否
         ├─ 需要长篇输出？
         │  └─ 是 → SoT
         └─ 否
            ├─ 需要深度分析？
            │  └─ 是 → Meta-CoT
            └─ 否 → CoT
```

---

## ReAct Agent

**Reasoning + Acting** - 最经典的推理与行动循环模式

### 概述

ReAct Agent 通过 **思考-行动-观察** 循环来解决问题：

1. **Thought**: 分析当前情况，决定下一步做什么
2. **Action**: 选择并执行一个工具
3. **Observation**: 观察工具执行结果
4. 重复上述过程直到得出最终答案

### 架构

```
┌─────────────────────────────────────────┐
│          ReAct Agent                     │
├─────────────────────────────────────────┤
│                                          │
│  ┌──────────┐       ┌──────────┐       │
│  │   LLM    │◄─────►│  Parser  │       │
│  └──────────┘       └──────────┘       │
│       ▲                   ▲             │
│       │                   │             │
│       ▼                   ▼             │
│  ┌──────────────────────────────┐      │
│  │      Reasoning Loop           │      │
│  │  1. Thought                    │      │
│  │  2. Action (Tool Selection)    │      │
│  │  3. Observation (Tool Result)  │      │
│  └──────────────────────────────┘      │
│       │                                 │
│       ▼                                 │
│  ┌──────────┐                          │
│  │  Tools   │                          │
│  └──────────┘                          │
│                                          │
└─────────────────────────────────────────┘
```

### 快速开始

```go
package main

import (
    "context"
    "github.com/kart-io/goagent/agents/react"
    "github.com/kart-io/goagent/core"
    "github.com/kart-io/goagent/interfaces"
)

func main() {
    // 创建 ReAct Agent
    agent := react.NewReActAgent(react.ReActConfig{
        Name:        "MyAgent",
        Description: "A helpful assistant",
        LLM:         llmClient,
        Tools:       []interfaces.Tool{calculatorTool, searchTool},
        MaxSteps:    10,
    })

    // 执行任务
    ctx := context.Background()
    output, err := agent.Invoke(ctx, &core.AgentInput{
        Task: "What is 15 * 7 + 23?",
    })
}
```

### 配置选项

| 字段         | 类型                | 说明         | 默认值            |
| ------------ | ------------------- | ------------ | ----------------- |
| Name         | string              | Agent 名称   | 必需              |
| Description  | string              | Agent 描述   | 必需              |
| LLM          | llm.Client          | LLM 客户端   | 必需              |
| Tools        | []interfaces.Tool   | 可用工具列表 | 必需              |
| MaxSteps     | int                 | 最大步数     | 10                |
| StopPattern  | []string            | 停止模式     | ["Final Answer:"] |

详细文档见 [agents/react/](react/)

---

## Chain-of-Thought (CoT)

**思维链推理** - 通过逐步推理解决问题

### 概述

CoT 通过明确的步骤链条进行推理，每一步都有清晰的思考过程和理由。

### 特性

- ✅ Zero-shot CoT（零样本）
- ✅ Few-shot CoT（少样本示例）
- ✅ 每步推理都有明确理由
- ✅ 可配置推理深度
- ✅ 支持自我验证

### 使用场景

- 数学问题求解
- 逻辑推理
- 常识推理
- 需要解释的决策

### 快速开始

```go
import "github.com/kart-io/goagent/agents/cot"

// 创建 CoT Agent
agent := cot.NewCoTAgent(cot.CoTConfig{
    Name:        "reasoning-agent",
    Description: "Step-by-step reasoning",
    LLM:         llmClient,
    ZeroShot:    true,  // 使用零样本 CoT
    RequireJustification: true,  // 要求每步提供理由
})

output, err := agent.Invoke(ctx, &core.AgentInput{
    Task: "If John has 3 apples and gives 2 to Mary, who then gives 1 back, how many apples does John have?",
})
```

### 配置选项

| 字段                  | 类型       | 说明             | 默认值 |
| --------------------- | ---------- | ---------------- | ------ |
| ZeroShot              | bool       | 零样本模式       | true   |
| Examples              | []Example  | 示例集合         | nil    |
| RequireJustification  | bool       | 要求提供理由     | false  |
| MaxReasoningSteps     | int        | 最大推理步数     | 10     |
| SelfVerify            | bool       | 启用自我验证     | false  |

### 输出示例

```
Step 1: John starts with 3 apples
Justification: This is the initial state

Step 2: After giving 2 to Mary, John has 1 apple
Justification: 3 - 2 = 1

Step 3: Mary gives 1 back, so John has 2 apples
Justification: 1 + 1 = 2

Final Answer: John has 2 apples
```

---

## Tree-of-Thought (ToT)

**树状思维** - 探索多个推理路径

### 概述

ToT 将推理过程组织为树结构，探索多个可能的解决路径，并选择最优方案。

### 特性

- ✅ 多种搜索策略：DFS、BFS、Beam Search、MCTS
- ✅ 可配置分支因子和深度
- ✅ 思维评估和剪枝
- ✅ 并行路径探索
- ✅ 回溯机制

### 使用场景

- 创意写作
- 战略规划
- 游戏 AI
- 需要探索多种方案的问题

### 快速开始

```go
import "github.com/kart-io/goagent/agents/tot"

// 使用 BFS 搜索
agent := tot.NewToTAgent(tot.ToTConfig{
    Name:           "explorer",
    LLM:            llmClient,
    SearchStrategy: "bfs",
    BranchFactor:   3,
    MaxDepth:       4,
})

// 使用 Beam Search
agent := tot.NewToTAgent(tot.ToTConfig{
    Name:           "beam-agent",
    LLM:            llmClient,
    SearchStrategy: "beam",
    BeamWidth:      5,
    MaxDepth:       3,
})
```

### 搜索策略

#### 1. DFS (深度优先)
- 快速找到一个解决方案
- 内存占用小
- 可能错过更优解

#### 2. BFS (广度优先)
- 保证找到最短路径
- 内存占用大
- 适合层数少的问题

#### 3. Beam Search (束搜索)
- 在速度和质量间平衡
- 保留 top-k 个最优路径
- 推荐用于大多数场景

#### 4. MCTS (蒙特卡洛树搜索)
- 探索-利用平衡
- 适合游戏和策略问题
- 需要更多计算

### 配置选项

| 字段              | 类型   | 说明           | 默认值 |
| ----------------- | ------ | -------------- | ------ |
| SearchStrategy    | string | 搜索策略       | "bfs"  |
| BranchFactor      | int    | 分支因子       | 3      |
| MaxDepth          | int    | 最大深度       | 4      |
| BeamWidth         | int    | Beam 宽度      | 3      |
| MCTSIterations    | int    | MCTS 迭代次数  | 100    |
| PruneThreshold    | float64| 剪枝阈值       | 0.3    |
| ParallelExecution | bool   | 并行执行       | false  |

---

## Graph-of-Thought (GoT)

**图状思维** - 复杂依赖关系的推理

### 概述

GoT 将推理组织为有向无环图（DAG），支持复杂的依赖关系和并行推理。

### 特性

- ✅ DAG 结构推理
- ✅ 并行节点执行
- ✅ 拓扑排序
- ✅ 循环检测
- ✅ 多种合并策略（投票、加权、LLM）

### 使用场景

- 复杂系统分析
- 多因素决策
- 项目规划
- 因果推理

### 快速开始

```go
import "github.com/kart-io/goagent/agents/got"

agent := got.NewGoTAgent(got.GoTConfig{
    Name:              "graph-reasoner",
    LLM:               llmClient,
    MaxNodes:          50,
    MaxEdgesPerNode:   5,
    ParallelExecution: true,
    MergeStrategy:     "weighted",  // vote, weighted, llm
    CycleDetection:    true,
    PruneThreshold:    0.3,
})

output, err := agent.Invoke(ctx, &core.AgentInput{
    Task: "Analyze the benefits and drawbacks of renewable energy from multiple perspectives",
})
```

### 合并策略

#### 1. Vote (投票)
- 简单多数投票
- 适合二元选择
- 快速高效

#### 2. Weighted (加权)
- 根据节点得分加权
- 平衡不同观点
- 推荐用于大多数场景

#### 3. LLM (语言模型)
- LLM 综合所有观点
- 最灵活
- 成本较高

### 配置选项

| 字段              | 类型    | 说明           | 默认值     |
| ----------------- | ------- | -------------- | ---------- |
| MaxNodes          | int     | 最大节点数     | 50         |
| MaxEdgesPerNode   | int     | 每节点最大边数 | 5          |
| ParallelExecution | bool    | 并行执行       | false      |
| MergeStrategy     | string  | 合并策略       | "weighted" |
| CycleDetection    | bool    | 循环检测       | true       |
| PruneThreshold    | float64 | 剪枝阈值       | 0.3        |

---

## Program-of-Thought (PoT)

**程序化思维** - 通过代码生成解决问题

### 概述

PoT 将问题转换为可执行代码，特别适合数学计算和逻辑推理。

### 特性

- ✅ 多语言支持：Python、JavaScript、Go
- ✅ 安全模式与沙箱
- ✅ 代码验证
- ✅ 执行结果捕获
- ✅ 错误处理与重试

### 使用场景

- 数学计算
- 数据分析
- 算法实现
- 逻辑验证

### 快速开始

```go
import "github.com/kart-io/goagent/agents/pot"

// Python 模式（默认）
agent := pot.NewPoTAgent(pot.PoTConfig{
    Name:     "calculator",
    LLM:      llmClient,
    Language: "python",
    SafeMode: true,
    AllowImports: []string{"math", "statistics"},
})

// JavaScript 模式
agent := pot.NewPoTAgent(pot.PoTConfig{
    Name:     "js-executor",
    LLM:      llmClient,
    Language: "javascript",
    SafeMode: true,
})

output, err := agent.Invoke(ctx, &core.AgentInput{
    Task: "Calculate the factorial of 10 and find its prime factors",
})
```

### 安全模式

启用 SafeMode 后，会限制：

**Python:**
- 禁止: `os`, `subprocess`, `sys`, `eval`, `exec`
- 允许: `math`, `statistics`, `datetime`, `json`

**JavaScript:**
- 禁止: `eval`, `Function`, `child_process`, `fs`
- 允许: `Math`, `Date`, `JSON`

**Go:**
- 要求: `package main` 和 `func main()`
- 验证: 语法检查

### 配置选项

| 字段         | 类型     | 说明               | 默认值     |
| ------------ | -------- | ------------------ | ---------- |
| Language     | string   | 编程语言           | "python"   |
| SafeMode     | bool     | 安全模式           | true       |
| AllowImports | []string | 允许的导入         | []         |
| Timeout      | Duration | 执行超时           | 30s        |
| PythonPath   | string   | Python 解释器路径  | "python3"  |
| NodePath     | string   | Node.js 路径       | "node"     |
| GoPath       | string   | Go 编译器路径      | "go"       |

### 输出示例

```go
// 任务: 计算斐波那契数列第10项

// 生成的代码:
def fibonacci(n):
    if n <= 1:
        return n
    a, b = 0, 1
    for _ in range(2, n + 1):
        a, b = b, a + b
    return b

result = fibonacci(10)
print(result)

// 执行结果: 55
```

---

## Skeleton-of-Thought (SoT)

**骨架式思维** - 快速生成长篇内容

### 概述

SoT 先生成内容骨架，然后并行展开各部分，适合长文本生成。

### 特性

- ✅ 骨架生成
- ✅ 并行详述
- ✅ 依赖感知调度
- ✅ 多种聚合策略
- ✅ 超时控制

### 使用场景

- 文章写作
- 报告生成
- 文档创建
- 结构化内容

### 快速开始

```go
import "github.com/kart-io/goagent/agents/sot"

agent := sot.NewSoTAgent(sot.SoTConfig{
    Name:                "writer",
    LLM:                 llmClient,
    MaxSkeletonPoints:   10,
    MinSkeletonPoints:   3,
    MaxConcurrency:      5,
    AggregationStrategy: "sequential",  // sequential, hierarchical, weighted
    DependencyAware:     true,
})

output, err := agent.Invoke(ctx, &core.AgentInput{
    Task: "Write a comprehensive guide on machine learning for beginners",
})
```

### 工作流程

```
1. 骨架生成
   → [Introduction, Basics, Algorithms, Applications, Conclusion]

2. 并行详述（考虑依赖）
   Level 0: [Introduction] ──────────────┐
   Level 1: [Basics, Algorithms] ────────┼→ 并行执行
   Level 2: [Applications, Conclusion] ──┘

3. 聚合结果
   → 完整文档
```

### 聚合策略

#### 1. Sequential（顺序）
- 按顺序拼接各部分
- 保持简单结构
- 最快

#### 2. Hierarchical（层次）
- 识别章节层次
- 生成目录
- 适合长文档

#### 3. Weighted（加权）
- 根据重要性调整篇幅
- 平衡各部分
- 质量最高

### 配置选项

| 字段                | 类型     | 说明           | 默认值       |
| ------------------- | -------- | -------------- | ------------ |
| MaxSkeletonPoints   | int      | 最大骨架点数   | 10           |
| MinSkeletonPoints   | int      | 最小骨架点数   | 3            |
| AutoDecompose       | bool     | 自动分解       | false        |
| MaxConcurrency      | int      | 最大并发数     | 5            |
| ElaborationTimeout  | Duration | 详述超时       | 30s          |
| BatchSize           | int      | 批次大小       | 3            |
| AggregationStrategy | string   | 聚合策略       | "sequential" |
| DependencyAware     | bool     | 依赖感知       | false        |

---

## Meta-CoT / Self-Ask

**元推理与自我提问** - 深度分析与自我完善

### 概述

Meta-CoT 通过自我提问和自我批判来深化推理，不断完善答案。

### 特性

- ✅ 自动问题分解
- ✅ 递归子问题求解
- ✅ 自我批判与完善
- ✅ 置信度评估
- ✅ 证据收集
- ✅ 答案验证

### 使用场景

- 复杂问题分析
- 研究型任务
- 需要深度思考的问题
- 多步推理

### 快速开始

```go
import "github.com/kart-io/goagent/agents/metacot"

agent := metacot.NewMetaCoTAgent(metacot.MetaCoTConfig{
    Name:                "deep-thinker",
    LLM:                 llmClient,
    MaxQuestions:        5,
    MaxDepth:            3,
    AutoDecompose:       true,
    RequireEvidence:     true,
    SelfCritique:        true,
    QuestionStrategy:    "focused",  // focused, broad, critical
    VerifyAnswers:       true,
    ConfidenceThreshold: 0.7,
})

output, err := agent.Invoke(ctx, &core.AgentInput{
    Task: "What are the long-term implications of artificial intelligence on society?",
})
```

### 工作流程

```
1. 主问题
   "What are the implications of AI on society?"

2. 分解子问题
   Q1: "How will AI affect employment?"
   Q2: "What are the ethical concerns?"
   Q3: "How will education change?"

3. 递归求解
   Q1 → [Sub-Q1.1, Sub-Q1.2] → Answers
   Q2 → [Sub-Q2.1] → Answers
   Q3 → Direct Answer

4. 自我批判
   Critique: "Need more evidence on Q2"
   → 补充研究 → 完善答案

5. 综合答案
   Final Answer with confidence: 0.85
```

### 问题策略

#### 1. Focused（聚焦）
- 针对性强的子问题
- 快速收敛
- 适合明确目标

#### 2. Broad（广泛）
- 全面探索
- 多角度分析
- 适合开放问题

#### 3. Critical（批判）
- 质疑假设
- 寻找反例
- 适合验证结论

### 配置选项

| 字段                 | 类型    | 说明           | 默认值     |
| -------------------- | ------- | -------------- | ---------- |
| MaxQuestions         | int     | 最大问题数     | 5          |
| MaxDepth             | int     | 最大递归深度   | 3          |
| AutoDecompose        | bool    | 自动分解       | true       |
| RequireEvidence      | bool    | 要求证据       | false      |
| SelfCritique         | bool    | 自我批判       | false      |
| QuestionStrategy     | string  | 问题策略       | "focused"  |
| VerifyAnswers        | bool    | 验证答案       | false      |
| ConfidenceThreshold  | float64 | 置信度阈值     | 0.7        |

---

## Builder API

所有推理模式都可以通过 Builder API 轻松创建：

```go
import "github.com/kart-io/goagent/builder"

// CoT
agent := builder.NewAgentBuilder(llmClient).
    WithZeroShotCoT().
    Build()

// ToT with Beam Search
agent := builder.NewAgentBuilder(llmClient).
    WithBeamSearchToT(5, 3).  // beamWidth, maxDepth
    Build()

// GoT with parallel execution
agent := builder.NewAgentBuilder(llmClient).
    WithGraphOfThought(got.GoTConfig{
        ParallelExecution: true,
        MergeStrategy:     "weighted",
    }).
    Build()

// PoT for Python
agent := builder.NewAgentBuilder(llmClient).
    WithProgramOfThought(pot.PoTConfig{
        Language: "python",
        SafeMode: true,
    }).
    Build()

// SoT for writing
agent := builder.NewAgentBuilder(llmClient).
    WithSkeletonOfThought(sot.SoTConfig{
        MaxConcurrency:      5,
        AggregationStrategy: "hierarchical",
    }).
    Build()

// Meta-CoT with critique
agent := builder.NewAgentBuilder(llmClient).
    WithMetaCoT(metacot.MetaCoTConfig{
        SelfCritique:  true,
        RequireEvidence: true,
    }).
    Build()
```

---

## 性能对比

基于测试结果的性能参考：

| 推理模式 | 平均延迟 | Token 消耗 | 并行能力 | 适用问题复杂度 |
|---------|---------|-----------|---------|--------------|
| ReAct   | ~1-2s   | 中        | 否      | 中           |
| CoT     | ~500ms  | 低        | 否      | 低-中        |
| ToT     | ~3-10s  | 高        | 部分    | 中-高        |
| GoT     | ~2-5s   | 高        | 是      | 高           |
| PoT     | ~1-3s   | 中        | 否      | 中           |
| SoT     | ~2-4s   | 中-高     | 是      | 中-高        |
| Meta-CoT| ~5-15s  | 高        | 否      | 高           |

---

## 最佳实践

### 1. 选择合适的推理模式

- **简单问题**: CoT
- **需要工具**: ReAct
- **需要计算**: PoT
- **需要探索**: ToT 或 GoT
- **需要速度**: SoT
- **需要深度**: Meta-CoT

### 2. 性能优化

```go
// 设置合理的限制
agent := tot.NewToTAgent(tot.ToTConfig{
    MaxDepth:     3,  // 避免过深
    BranchFactor: 3,  // 控制分支
    ParallelExecution: true,  // 启用并行
})

// 使用超时
ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
defer cancel()
```

### 3. 错误处理

```go
output, err := agent.Invoke(ctx, input)
if err != nil {
    // 检查是否超时
    if ctx.Err() == context.DeadlineExceeded {
        log.Println("Task timeout")
    }

    // 检查部分结果
    if output.Status == "partial" {
        log.Printf("Partial result: %v", output.Result)
    }
}
```

### 4. 监控与调试

```go
// 使用回调监控
type MonitorCallback struct {
    core.BaseCallback
}

func (m *MonitorCallback) OnLLMEnd(ctx context.Context, output string, tokens int) error {
    log.Printf("LLM used %d tokens", tokens)
    return nil
}

agent.WithCallbacks(&MonitorCallback{})
```

---

## 测试

运行所有 Agent 测试：

```bash
# 测试所有推理模式
go test ./agents/... -v

# 测试特定模式
go test ./agents/cot -v
go test ./agents/tot -v
go test ./agents/got -v
go test ./agents/pot -v
go test ./agents/sot -v
go test ./agents/metacot -v

# 基准测试
go test ./agents/... -bench=. -benchmem
```

---

## 示例

完整示例见 `examples/` 目录：

- `examples/cot_example/` - CoT 示例
- `examples/tot_example/` - ToT 示例
- `examples/got_example/` - GoT 示例
- `examples/pot_example/` - PoT 示例
- `examples/sot_example/` - SoT 示例
- `examples/metacot_example/` - Meta-CoT 示例

---

## 参考文献

- **ReAct**: [Synergizing Reasoning and Acting in Language Models](https://arxiv.org/abs/2210.03629)
- **CoT**: [Chain-of-Thought Prompting Elicits Reasoning](https://arxiv.org/abs/2201.11903)
- **ToT**: [Tree of Thoughts: Deliberate Problem Solving](https://arxiv.org/abs/2305.10601)
- **GoT**: [Graph of Thoughts: Solving Problems with LLMs](https://arxiv.org/abs/2308.09687)
- **PoT**: [Program of Thoughts Prompting](https://arxiv.org/abs/2211.12588)
- **SoT**: [Skeleton-of-Thought: Faster Generation](https://arxiv.org/abs/2307.15337)

---

## License

MIT License
