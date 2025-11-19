# SupervisorAgent 功能示例

## 概述

SupervisorAgent 是一个多 Agent 协作框架，能够将复杂任务分解并分配给不同的专业 SubAgent，然后聚合结果生成最终答案。

## 目录结构

```
supervisor_agent/
├── REQUIREMENTS.md          # 需求说明文档
├── SOLUTION.md             # 实现方案文档
├── README.md               # 本文件
├── main.go                 # 完整示例代码
└── scenarios/              # 不同场景示例
    ├── travel_planner.go   # 旅行规划场景
    ├── code_review.go      # 代码审查场景
    └── data_analysis.go    # 数据分析场景
```

## 核心概念

### 1. SupervisorAgent

协调多个 SubAgent 完成复杂任务的主控 Agent。

**主要功能**：
- 接收复杂任务
- 分配给合适的 SubAgent
- 聚合 SubAgent 的结果
- 返回最终答案

### 2. SubAgent

专门负责特定领域任务的 Agent。

**示例**：
- SearchAgent：负责搜索信息
- WeatherAgent：负责查询天气
- SummaryAgent：负责总结信息

### 3. 聚合策略

不同场景使用不同的结果聚合方式：

#### Parallel（并行聚合）
- **适用场景**：子任务独立，可并行执行
- **聚合方式**：简单合并所有结果
- **示例**：同时查询多个城市的天气

#### Hierarchy（层次聚合）
- **适用场景**：子任务有依赖，需串行执行
- **聚合方式**：使用 LLM 综合所有结果
- **示例**：搜索 → 分析 → 总结

#### Consensus（协商聚合）
- **适用场景**：多个专家意见需要综合
- **聚合方式**：LLM 分析各方意见，达成共识
- **示例**：代码审查（安全、性能、可读性）

## 快速开始

### 前置条件

1. **安装依赖**
```bash
cd /home/hellotalk/code/go/src/github.com/kart-io/goagent
go mod download
```

2. **设置环境变量**
```bash
export DEEPSEEK_API_KEY="your-deepseek-api-key"
# 或
export OPENAI_API_KEY="your-openai-api-key"
```

### 运行示例

#### 1. 基础示例
```bash
cd examples/advanced/supervisor_agent
go run main.go -scenario=basic
```

**输出示例**：
```
=== SupervisorAgent 基础示例 ===

任务: 研究法国首都，查询天气，生成旅行建议

[SubAgent:search] 执行中...
结果: 法国的首都是巴黎

[SubAgent:weather] 执行中...
结果: 巴黎今天晴天，温度 25°C

[SubAgent:summary] 执行中...
结果: 巴黎天气宜人，适合旅行。建议参观埃菲尔铁塔、卢浮宫等景点。

执行统计:
- 总任务数: 3
- 成功: 3
- 失败: 0
- 总耗时: 6.5s
- 总 Tokens: 870
```

#### 2. 旅行规划示例
```bash
go run main.go -scenario=travel
```

#### 3. 代码审查示例
```bash
go run main.go -scenario=review
```

#### 4. 所有示例
```bash
go run main.go -scenario=all
```

## 代码示例

### 1. 创建 SupervisorAgent

```go
package main

import (
    "context"
    "fmt"
    "os"

    "github.com/kart-io/goagent/agents"
    "github.com/kart-io/goagent/core"
    "github.com/kart-io/goagent/llm"
    "github.com/kart-io/goagent/llm/providers"
)

func main() {
    // 1. 创建 LLM 客户端
    llmClient, err := providers.NewDeepSeek(&llm.Config{
        APIKey: os.Getenv("DEEPSEEK_API_KEY"),
        Model:  "deepseek-chat",
    })
    if err != nil {
        panic(err)
    }

    // 2. 创建子 Agent
    searchAgent := createSearchAgent(llmClient)
    weatherAgent := createWeatherAgent(llmClient)
    summaryAgent := createSummaryAgent(llmClient)

    // 3. 创建 SupervisorAgent
    config := agents.DefaultSupervisorConfig()
    config.AggregationStrategy = agents.StrategyHierarchy

    supervisor := agents.NewSupervisorAgent(llmClient, config)
    supervisor.AddSubAgent("search", searchAgent)
    supervisor.AddSubAgent("weather", weatherAgent)
    supervisor.AddSubAgent("summary", summaryAgent)

    // 4. 执行任务
    result, err := supervisor.Invoke(context.Background(), &core.AgentInput{
        Task: "研究法国首都，查询天气，生成旅行建议",
    })

    if err != nil {
        panic(err)
    }

    // 5. 输出结果
    fmt.Printf("最终结果：%v\n", result.Result)
}

// 创建搜索 Agent
func createSearchAgent(llmClient llm.Client) core.Agent {
    // 实现略...
}
```

### 2. 自定义 SubAgent

```go
type CustomAgent struct {
    *core.BaseAgent
    llm llm.Client
}

func NewCustomAgent(llm llm.Client) *CustomAgent {
    return &CustomAgent{
        BaseAgent: core.NewBaseAgent("custom", "Custom agent description"),
        llm:       llm,
    }
}

func (a *CustomAgent) Invoke(ctx context.Context, input *core.AgentInput) (*core.AgentOutput, error) {
    // 使用 LLM 处理任务
    response, err := a.llm.Complete(ctx, &llm.CompletionRequest{
        Messages: []llm.Message{
            {Role: "user", Content: input.Task},
        },
    })

    if err != nil {
        return nil, err
    }

    return &core.AgentOutput{
        Result: response.Content,
        Status: "success",
        Usage:  response.Usage,
    }, nil
}
```

### 3. 使用不同的聚合策略

```go
// 并行聚合 - 适合独立任务
config := agents.DefaultSupervisorConfig()
config.AggregationStrategy = agents.StrategyParallel
supervisor := agents.NewSupervisorAgent(llmClient, config)

// 层次聚合 - 适合有依赖的任务
config.AggregationStrategy = agents.StrategyHierarchy

// 协商聚合 - 适合需要综合多方意见
config.AggregationStrategy = agents.StrategyConsensus
```

## 场景示例

### 场景 1：智能客服

**任务**："客户询问退款政策，并要求查询订单状态"

**SubAgents**：
- PolicyAgent：查询退款政策
- OrderAgent：查询订单状态
- ReplyAgent：生成客服回复

**聚合策略**：Hierarchy

**流程**：
```
1. PolicyAgent → 退款政策说明
2. OrderAgent → 订单状态（已发货）
3. ReplyAgent → 综合回复："根据退款政策，已发货订单..."
```

### 场景 2：技术文档生成

**任务**："为用户认证功能生成完整的技术文档"

**SubAgents**：
- RequirementAgent：分析需求
- DesignAgent：技术设计
- APIAgent：API 规范
- TestAgent：测试用例

**聚合策略**：Hierarchy

### 场景 3：数据分析

**任务**："分析销售数据，找出趋势并生成报告"

**SubAgents**：
- CleanAgent：数据清洗
- AnalysisAgent：统计分析
- VisualizationAgent：生成图表描述
- ReportAgent：撰写报告

**聚合策略**：Hierarchy

## 配置选项

### SupervisorConfig

```go
type SupervisorConfig struct {
    // 聚合策略
    AggregationStrategy string // "parallel" | "hierarchy" | "consensus"

    // 最大并发数（并行策略）
    MaxConcurrency int

    // 超时时间
    Timeout time.Duration

    // 是否启用容错
    EnableFallback bool

    // 最大重试次数
    MaxRetries int
}
```

### 默认配置

```go
config := agents.DefaultSupervisorConfig()
// 等同于：
config := &agents.SupervisorConfig{
    AggregationStrategy: agents.StrategyHierarchy,
    MaxConcurrency:      5,
    Timeout:             30 * time.Second,
    EnableFallback:      true,
    MaxRetries:          3,
}
```

## 性能优化建议

### 1. 选择合适的聚合策略

- **独立任务**：使用 `StrategyParallel` 提高并发
- **有依赖任务**：使用 `StrategyHierarchy` 保证顺序
- **需要共识**：使用 `StrategyConsensus` 综合意见

### 2. 控制并发数

```go
config.MaxConcurrency = 3 // 避免过多并发导致限流
```

### 3. 设置合理超时

```go
config.Timeout = 30 * time.Second // 防止长时间等待
```

### 4. 使用缓存

对于重复的查询，可以在 SubAgent 内部实现缓存。

## 错误处理

### 1. SubAgent 失败

SupervisorAgent 会捕获 SubAgent 的错误，并根据配置决定是否继续：

```go
config.EnableFallback = true  // 启用容错，部分失败仍返回结果
config.MaxRetries = 3         // 失败时重试 3 次
```

### 2. 超时处理

```go
ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
defer cancel()

result, err := supervisor.Invoke(ctx, input)
if err == context.DeadlineExceeded {
    // 处理超时
}
```

### 3. 降级方案

当关键 SubAgent 失败时，可以提供降级结果：

```go
result, err := supervisor.Invoke(ctx, input)
if err != nil {
    // 使用缓存或默认结果
    result = getFallbackResult()
}
```

## 监控与调试

### 1. 启用详细日志

```go
config.VerboseLogging = true
```

### 2. 查看执行统计

```go
stats := supervisor.GetStats()
fmt.Printf("总任务: %d\n", stats.TotalTasks)
fmt.Printf("成功: %d\n", stats.SuccessfulTasks)
fmt.Printf("失败: %d\n", stats.FailedTasks)
fmt.Printf("总耗时: %v\n", stats.TotalDuration)
fmt.Printf("总 Tokens: %d\n", stats.TotalTokens)
```

### 3. SubAgent 级别统计

```go
for agentName, agentStats := range stats.SubAgentStats {
    fmt.Printf("Agent %s:\n", agentName)
    fmt.Printf("  调用次数: %d\n", agentStats.Invocations)
    fmt.Printf("  成功率: %.2f%%\n", float64(agentStats.Successes)/float64(agentStats.Invocations)*100)
    fmt.Printf("  平均耗时: %v\n", agentStats.AvgDuration)
}
```

## 最佳实践

### 1. Agent 设计原则

- **单一职责**：每个 SubAgent 只负责一个明确的任务
- **清晰命名**：Agent 名称应该清楚表达其功能
- **错误处理**：SubAgent 应该优雅地处理错误

### 2. 任务分解

- **粒度适中**：子任务不要太细（增加开销）也不要太粗（失去并行机会）
- **依赖明确**：清楚标识任务之间的依赖关系
- **可并行化**：尽可能设计独立的子任务

### 3. 结果聚合

- **信息完整**：确保重要信息不丢失
- **格式统一**：SubAgent 输出格式应该一致
- **LLM 优化**：使用清晰的 Prompt 指导聚合过程

## 常见问题

### Q1: SupervisorAgent 和普通 Agent 有什么区别？

**A**: SupervisorAgent 负责协调多个 Agent，而不是直接处理任务。它专注于任务分解、调度和结果聚合。

### Q2: 何时使用 SupervisorAgent？

**A**: 当任务需要多个专业领域的知识，或者任务可以分解为多个独立子任务时，使用 SupervisorAgent 可以提高效率和质量。

### Q3: 如何选择聚合策略？

**A**:
- 独立任务 → Parallel
- 有依赖任务 → Hierarchy
- 需要综合意见 → Consensus

### Q4: SubAgent 失败会影响整体吗？

**A**: 如果 `EnableFallback = true`，部分 SubAgent 失败不会导致整体失败，而是返回部分结果。

### Q5: 如何优化性能？

**A**:
1. 使用并行策略提高并发
2. 设置合理的超时时间
3. 在 SubAgent 内部实现缓存
4. 优化 Prompt 减少 Token 消耗

## 进阶话题

### 1. 动态 Agent 选择

根据任务内容动态选择合适的 SubAgent：

```go
// TODO: 实现动态选择逻辑
supervisor.SelectAgentsByCapability(task)
```

### 2. Agent 链式调用

SubAgent 的输出作为下一个 SubAgent 的输入：

```go
// 使用 Hierarchy 策略自动实现
config.AggregationStrategy = agents.StrategyHierarchy
```

### 3. 分布式执行

SubAgent 可以部署在不同的服务器上：

```go
// TODO: 实现远程 Agent 调用
supervisor.AddRemoteAgent("remote-agent", "http://agent-service:8080")
```

## 相关资源

- [REQUIREMENTS.md](./REQUIREMENTS.md) - 详细需求说明
- [SOLUTION.md](./SOLUTION.md) - 实现方案文档
- [agents/supervisor_agent.go](../../../agents/supervisor_agent.go) - 源代码实现
- [GoAgent 文档](../../../README.md) - 项目主文档

## 贡献

如果你有新的场景示例或改进建议，欢迎提交 PR！

---

**版本**：v1.0
**更新时间**：2025-11-19
**维护者**：GoAgent Team
