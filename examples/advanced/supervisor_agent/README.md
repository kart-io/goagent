# SupervisorAgent 功能示例

## 概述

SupervisorAgent 是一个多 Agent 协作框架，能够将复杂任务分解并分配给不同的专业 SubAgent，然后聚合结果生成最终答案。本示例展示了 SupervisorAgent 的完整功能，包括基础协作、高级聚合策略和企业级特性。

## 目录结构

```
supervisor_agent/
├── main.go                      # 主程序（包含 basic, travel, review 场景）
├── features/                    # 高级功能包
│   └── advanced.go             # 高级功能实现（缓存、工具调用、fallback等）
├── test_direct.go              # 直接测试程序
├── Makefile                    # 构建和运行命令
├── REQUIREMENTS.md             # 详细需求说明
├── SOLUTION.md                 # 实现方案文档
├── QUICK_START.md              # 快速开始指南
├── ADVANCED_FEATURES.md        # 高级功能使用指南
└── README.md                   # 本文件
```

## 核心概念

### 1. SupervisorAgent

协调多个 SubAgent 完成复杂任务的主控 Agent。

**主要功能**：
- 接收复杂任务
- 智能任务分解
- 动态 Agent 选择
- 结果聚合
- 错误处理和重试

### 2. SubAgent

专门负责特定领域任务的 Agent。

**示例 SubAgent**：
- **SearchAgent**：负责搜索信息
- **WeatherAgent**：负责查询天气
- **SummaryAgent**：负责总结信息
- **CityInfoAgent**：提供城市信息
- **SecurityReviewAgent**：代码安全审查
- **PerformanceReviewAgent**：代码性能分析

### 3. 聚合策略（Aggregation Strategies）

不同场景使用不同的结果聚合方式：

#### StrategyMerge（合并聚合）
- **适用场景**：多个独立专家意见需要合并
- **聚合方式**：简单合并所有结果，保留每个 Agent 的输出
- **示例**：代码审查（安全、性能、可读性）
- **代码**：
  ```go
  config.AggregationStrategy = agents.StrategyMerge
  ```

#### StrategyHierarchy（层次聚合）
- **适用场景**：子任务有依赖，需串行执行
- **聚合方式**：使用 LLM 综合所有结果，生成统一答案
- **示例**：旅行规划（城市信息 → 天气 → 景点 → 行程）
- **代码**：
  ```go
  config.AggregationStrategy = agents.StrategyHierarchy
  ```

#### StrategyBest（最佳选择聚合）
- **适用场景**：从多个结果中选择最佳
- **聚合方式**：LLM 评估并选择质量最高的结果
- **示例**：多个翻译结果选择最佳

#### StrategyConsensus（协商聚合）
- **适用场景**：需要多方达成一致意见
- **聚合方式**：LLM 分析各方意见，寻求共识
- **示例**：技术方案评审

### 4. 路由策略（Routing Strategies）

决定如何将任务分配给 SubAgent：

- **StrategyLLMBased**：基于 LLM 智能路由（默认）
- **StrategyRuleBased**：基于预定义规则路由
- **StrategyRoundRobin**：轮询分配
- **StrategyCapability**：基于能力匹配

## 快速开始

### 前置条件

**1. 安装依赖**
```bash
cd /home/costalong/code/go/src/github.com/kart-io/goagent
go mod download
```

**2. 设置环境变量**
```bash
# 使用 DeepSeek（推荐）
export DEEPSEEK_API_KEY="sk-your-api-key-here"

# 或使用 OpenAI
export OPENAI_API_KEY="sk-your-api-key-here"
```

### 运行示例

#### 方式 1：使用 Makefile（推荐）

```bash
cd examples/advanced/supervisor_agent

# 基础示例 - 多 Agent 协作
make run-basic

# 旅行规划 - 层次聚合策略
make run-travel

# 代码审查 - 合并聚合策略
make run-review

# 高级功能演示 - 缓存/工具调用/Fallback/批处理
make run-advanced

# 运行所有场景
make run-all
```

#### 方式 2：直接运行

```bash
# 优化后的简洁运行方式
go run main.go -scenario=basic      # 基础示例
go run main.go -scenario=travel     # 旅行规划
go run main.go -scenario=review     # 代码审查
go run main.go -scenario=advanced   # 高级功能
go run main.go -scenario=all        # 所有场景

# 使用 OpenAI 提供商
go run main.go -scenario=basic -provider=openai
```

#### 方式 3：编译后运行

```bash
# 编译
make build

# 运行
./supervisor_example -scenario=basic
./supervisor_example -scenario=advanced
```

## 场景详解

### 场景 1: 基础示例（Basic）

**演示内容**：多 Agent 协作的基本流程

**任务**：研究法国的首都，查询当地天气，并生成一份简短的旅行建议

**SubAgent 配置**：
- **search**：负责搜索信息
- **weather**：负责查询天气
- **summary**：负责生成总结

**聚合策略**：StrategyHierarchy（层次聚合）

**预期输出**：
```
=== SupervisorAgent 功能示例 ===

📋 场景 1: 基础示例 - 多 Agent 协作
────────────────────────────────────────────────────────────────────────────────

🎯 任务: 研究法国的首都，查询当地天气，并生成一份简短的旅行建议

✅ 执行成功！

📊 最终结果:
────────────────────────────────────────────────────────────────────────────────
法国的首都是巴黎。当前巴黎天气晴朗，温度25°C，非常适合旅行。
推荐您参观埃菲尔铁塔、卢浮宫、巴黎圣母院、香榭丽舍大街等著名景点...
────────────────────────────────────────────────────────────────────────────────

⏱️  执行时间: 17s
🎫 Token 使用: 870 (提示: 450, 完成: 420)
```

### 场景 2: 旅行规划（Travel）

**演示内容**：复杂任务的层次聚合

**任务**：我计划去东京旅行3天，帮我了解城市信息、天气情况、推荐景点，并生成一份3天的行程安排

**SubAgent 配置**：
- **city_info**：城市信息专家
- **weather**：天气预报专家
- **attractions**：景点推荐专家
- **itinerary**：行程规划专家

**聚合策略**：StrategyHierarchy（层次聚合）

**配置**：
```go
config := agents.DefaultSupervisorConfig()
config.AggregationStrategy = agents.StrategyHierarchy
config.SubAgentTimeout = 60 * time.Second  // 复杂任务需要更长超时
```

**特点**：
- 子任务有依赖关系
- 需要综合多个信息源
- LLM 最终生成统一的行程规划

### 场景 3: 代码审查（Review）

**演示内容**：多专家意见的合并聚合

**任务**：从安全性、性能、可读性三个维度审查 Go 代码

**SubAgent 配置**：
- **security**：安全审查专家
- **performance**：性能分析专家
- **readability**：可读性评估专家

**聚合策略**：StrategyMerge（合并聚合）

**配置**：
```go
config := agents.DefaultSupervisorConfig()
config.AggregationStrategy = agents.StrategyMerge
config.SubAgentTimeout = 60 * time.Second
```

**审查代码示例**：
```go
func ProcessUserData(data string) error {
    // 直接使用用户输入构建 SQL（安全风险）
    query := "SELECT * FROM users WHERE name = '" + data + "'"

    // 在循环中重复执行查询（性能问题）
    for i := 0; i < 1000000; i++ {
        result := db.Query(query)
        // 处理结果...
    }

    return nil
}
```

**预期输出**：
```
📊 审查结果:
────────────────────────────────────────────────────────────────────────────────

【security】
  安全评分: 2/10分
  发现的安全问题:
    1. 存在严重的 SQL 注入漏洞
    2. 未对用户输入进行验证和过滤
  改进建议:
    1. 使用参数化查询（Prepared Statement）
    2. 添加输入验证和转义

【performance】
  性能评分: 1/10分
  发现的性能问题:
    1. 在循环中执行数据库查询（百万次）
    2. 没有使用连接池
    3. 时间复杂度 O(n*m)，n=100万
  改进建议:
    1. 将查询移到循环外
    2. 使用批量查询
    3. 添加缓存机制

【readability】
  可读性评分: 4/10分
  发现的可读性问题:
    1. 函数名不够描述性
    2. 缺少注释
    3. 魔术数字 1000000
  改进建议:
    1. 重命名为 ProcessAndQueryUserData
    2. 添加函数注释说明功能
    3. 使用常量替代魔术数字
────────────────────────────────────────────────────────────────────────────────
```

### 场景 4: 高级功能（Advanced）

**演示内容**：企业级高级特性

**实现的功能**：

#### 1. 响应缓存（Response Cache）
- **技术**：内存缓存 + TTL 过期机制
- **性能提升**：22,436,388x（超过 2200 万倍加速）
- **实现**：
  ```go
  type ResponseCache struct {
      data  map[string]*CacheEntry
      mutex sync.RWMutex
      ttl   time.Duration
  }
  ```
- **效果**：
  ```
  第一次请求耗时: 40.228s
  第二次请求耗时: 1.793μs (缓存命中)
  加速比: 22436388.86x ⚡
  ```

#### 2. Tool Calling 支持
- **格式**：兼容 OpenAI/Anthropic Function Calling
- **工具定义**：
  ```go
  type ToolDefinition struct {
      Type     string             `json:"type"`
      Function FunctionDefinition `json:"function"`
  }
  ```
- **内置工具**：
  - `search_web`：搜索网络获取最新信息
  - `analyze_code`：分析代码并提供改进建议

#### 3. 自动 Fallback 机制
- **多级重试**：主提供商失败后自动切换
- **配置示例**：
  ```go
  features := &AdvancedFeatures{
      EnableAutoFallback: true,
      FallbackProviders:  []string{"openai", "deepseek"},
      MaxRetries:         3,
  }
  ```
- **工作流程**：
  ```
  DeepSeek (主) → 失败
    ↓ 重试 1
  DeepSeek → 失败
    ↓ 重试 2
  DeepSeek → 失败
    ↓ Fallback
  OpenAI → 成功 ✓
  ```

#### 4. 批处理 API（Batch Processing）
- **批量请求**：减少网络开销
- **配置**：
  ```go
  BatchSize:    10,
  BatchTimeout: 5 * time.Second,
  ```

#### 5. 多模态支持
- **支持模式**：`[text, code, json]`
- **智能识别**：自动检测和处理不同类型内容

**运行效果**：
```
🚀 高级功能演示
================================================================================

📦 1. 响应缓存演示
--------------------------------------------------------------------------------
第一次请求耗时: 40.228445218s
响应: 当然，很乐意为您详细解释 Go 语言...
✅ 缓存命中
第二次请求耗时: 1.793µs (缓存命中)
加速比: 22436388.86x

🔧 2. Tool Calling 演示
--------------------------------------------------------------------------------
已注册工具:
  1. search_web: 搜索网络获取最新信息
  2. analyze_code: 分析代码并提供改进建议

🎨 3. 多模态支持演示
--------------------------------------------------------------------------------
支持的模式: [text code json]

⚡ 4. 自动 Fallback 演示
--------------------------------------------------------------------------------
配置:
  - 主提供商: DeepSeek
  - Fallback 提供商: [openai deepseek]
  - 最大重试次数: 3

✅ 高级功能演示完成
```

## 代码示例

### 1. 创建基础 SupervisorAgent

```go
package main

import (
    "context"
    "fmt"
    "os"
    "time"

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
    searchAgent := createSimpleAgent(llmClient, "search", "负责搜索信息")
    weatherAgent := createSimpleAgent(llmClient, "weather", "负责查询天气")
    summaryAgent := createSimpleAgent(llmClient, "summary", "负责生成总结")

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
    fmt.Printf("Token 使用：%d\n", result.TokenUsage.TotalTokens)
}
```

### 2. 创建自定义 SubAgent

使用 MockAgent 快速创建：

```go
import (
    "context"
    "fmt"

    "github.com/kart-io/goagent/core"
    "github.com/kart-io/goagent/examples/testhelpers"
    "github.com/kart-io/goagent/llm"
)

func createSimpleAgent(llmClient llm.Client, name, description string) core.Agent {
    agent := testhelpers.NewMockAgent(name)
    agent.SetInvokeFn(func(ctx context.Context, input *core.AgentInput) (*core.AgentOutput, error) {
        // 使用 LLM 处理任务
        response, err := llmClient.Complete(ctx, &llm.CompletionRequest{
            Messages: []llm.Message{
                {Role: "system", Content: fmt.Sprintf("你是一个%s", description)},
                {Role: "user", Content: input.Task},
            },
        })

        if err != nil {
            return nil, err
        }

        return &core.AgentOutput{
            Result:     response.Content,
            Status:     "success",
            TokenUsage: response.Usage,
        }, nil
    })

    return agent
}
```

### 3. 使用高级功能

```go
import (
    "github.com/kart-io/goagent/examples/advanced/supervisor_agent/features"
)

func main() {
    // 创建 LLM 客户端
    llmClient, _ := providers.NewDeepSeek(&llm.Config{
        APIKey: os.Getenv("DEEPSEEK_API_KEY"),
        Model:  "deepseek-chat",
    })

    // 配置高级功能
    advancedFeatures := &features.AdvancedFeatures{
        EnableResponseCache: true,
        CacheTTL:            10 * time.Minute,
        EnableToolCalling:   true,
        Tools: []features.ToolDefinition{
            {
                Type: "function",
                Function: features.FunctionDefinition{
                    Name:        "search_web",
                    Description: "搜索网络",
                    Parameters: map[string]interface{}{
                        "type": "object",
                        "properties": map[string]interface{}{
                            "query": map[string]interface{}{
                                "type": "string",
                            },
                        },
                    },
                },
            },
        },
        EnableAutoFallback: true,
        FallbackProviders:  []string{"openai"},
        MaxRetries:         3,
    }

    // 创建增强客户端
    enhanced := features.NewEnhancedLLMClient(llmClient, advancedFeatures)

    // 使用增强客户端
    response, err := enhanced.Complete(ctx, &llm.CompletionRequest{
        Messages: []llm.Message{
            {Role: "user", Content: "什么是 Go 语言？"},
        },
    })
}
```

### 4. 配置不同的聚合策略

```go
// 合并聚合 - 适合多个专家独立意见
config := agents.DefaultSupervisorConfig()
config.AggregationStrategy = agents.StrategyMerge
supervisor := agents.NewSupervisorAgent(llmClient, config)

// 层次聚合 - 适合有依赖的任务链
config.AggregationStrategy = agents.StrategyHierarchy

// 最佳选择聚合 - 适合从多个结果中选最佳
config.AggregationStrategy = agents.StrategyBest

// 协商聚合 - 适合需要达成共识
config.AggregationStrategy = agents.StrategyConsensus
```

## 配置选项

### SupervisorConfig 完整配置

```go
type SupervisorConfig struct {
    // 聚合策略
    AggregationStrategy AggregationStrategy // "merge" | "hierarchy" | "best" | "consensus"

    // 路由策略
    RoutingStrategy RoutingStrategy // "llm" | "rule" | "round_robin" | "capability"

    // 并发控制
    MaxConcurrency int // 最大并发 SubAgent 数量

    // 超时控制
    SubAgentTimeout time.Duration // 单个 SubAgent 超时时间
    GlobalTimeout   time.Duration // 整体任务超时时间

    // 错误处理
    EnableFallback bool // 是否启用容错
    MaxRetries     int  // 最大重试次数

    // 性能优化
    EnableCaching  bool          // 是否启用结果缓存
    CacheTTL       time.Duration // 缓存过期时间
}
```

### 默认配置

```go
config := agents.DefaultSupervisorConfig()
// 等同于：
config := &agents.SupervisorConfig{
    AggregationStrategy: agents.StrategyHierarchy,
    RoutingStrategy:     agents.StrategyLLMBased,
    MaxConcurrency:      5,
    SubAgentTimeout:     30 * time.Second,
    GlobalTimeout:       120 * time.Second,
    EnableFallback:      true,
    MaxRetries:          3,
    EnableCaching:       false,
    CacheTTL:            5 * time.Minute,
}
```

### 推荐配置

**复杂任务（旅行规划、代码审查）**：
```go
config := agents.DefaultSupervisorConfig()
config.SubAgentTimeout = 60 * time.Second  // 增加超时
config.GlobalTimeout = 180 * time.Second
config.AggregationStrategy = agents.StrategyHierarchy
```

**高并发场景**：
```go
config.MaxConcurrency = 10
config.AggregationStrategy = agents.StrategyMerge
config.EnableCaching = true
```

**生产环境**：
```go
config.EnableFallback = true
config.MaxRetries = 3
config.EnableCaching = true
config.CacheTTL = 10 * time.Minute
```

## 性能优化建议

### 1. 选择合适的聚合策略

| 场景 | 推荐策略 | 原因 |
|------|---------|------|
| 独立子任务 | StrategyMerge | 可并行执行，简单合并 |
| 有依赖任务链 | StrategyHierarchy | 保证执行顺序，智能综合 |
| 多个候选方案 | StrategyBest | 自动选择最优结果 |
| 需要共识 | StrategyConsensus | 综合多方意见 |

### 2. 控制并发数

```go
// 根据 LLM API 限流调整
config.MaxConcurrency = 3  // 避免触发 rate limit
```

### 3. 设置合理超时

```go
// 简单任务
config.SubAgentTimeout = 30 * time.Second

// 复杂任务（代码分析、数据处理）
config.SubAgentTimeout = 60 * time.Second
```

### 4. 启用缓存

```go
// 重复查询场景
features.EnableResponseCache = true
features.CacheTTL = 10 * time.Minute

// 结果：22,436,388x 加速
```

### 5. 使用批处理

```go
// 大量相似请求
features.EnableBatchAPI = true
features.BatchSize = 10
features.BatchTimeout = 5 * time.Second
```

## 错误处理

### 1. SubAgent 失败处理

```go
config.EnableFallback = true  // 启用容错，部分失败仍返回结果
config.MaxRetries = 3         // 失败时重试 3 次
```

### 2. 超时处理

```go
ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
defer cancel()

result, err := supervisor.Invoke(ctx, input)
if err == context.DeadlineExceeded {
    fmt.Println("任务超时，请增加 GlobalTimeout")
}
```

### 3. 自动 Fallback

```go
// 主 LLM 失败时自动切换到备用 LLM
enhanced := features.NewEnhancedLLMClient(llmClient, features)
enhanced.AddFallbackClient(backupLLMClient)
```

## 监控与调试

### 查看执行统计

```go
result, err := supervisor.Invoke(ctx, input)

fmt.Printf("执行时间: %v\n", result.Metadata["duration"])
fmt.Printf("Token 使用: %d\n", result.TokenUsage.TotalTokens)
fmt.Printf("提示 Tokens: %d\n", result.TokenUsage.PromptTokens)
fmt.Printf("完成 Tokens: %d\n", result.TokenUsage.CompletionTokens)
```

## 最佳实践

### 1. Agent 设计原则

- **单一职责**：每个 SubAgent 只负责一个明确的任务
- **清晰命名**：Agent 名称应清楚表达其功能
  - ✅ `SecurityReviewAgent`
  - ❌ `Agent1`
- **错误处理**：SubAgent 应该优雅地处理错误并返回有意义的信息

### 2. Prompt 设计

**代码审查 Prompt 示例**：
```go
prompt := fmt.Sprintf(`你是一个代码安全审查专家。

%s

请从**安全角度**审查上述代码，重点关注：
1. SQL 注入漏洞
2. XSS 攻击风险
3. 数据验证缺失
4. 敏感信息泄露

**请按以下格式输出：**
- 安全评分：X/10分
- 发现的安全问题（列出具体问题）
- 改进建议（给出具体的修复方案）`, input.Task)
```

**关键点**：
- 将任务内容（如代码）直接放在 Prompt 中
- 明确角色和职责
- 清晰的输出格式要求
- 具体的评审维度

### 3. 任务分解

- **粒度适中**：子任务不要太细（增加开销）也不要太粗（失去并行机会）
- **依赖明确**：清楚标识任务之间的依赖关系
- **可并行化**：尽可能设计独立的子任务

### 4. 结果聚合

- **信息完整**：确保重要信息不丢失
- **格式统一**：SubAgent 输出格式应该一致
- **LLM 优化**：使用清晰的 Prompt 指导聚合过程

## 常见问题

### Q1: SupervisorAgent 和普通 Agent 有什么区别？

**A**: SupervisorAgent 负责协调多个 Agent，而不是直接处理任务。它专注于任务分解、调度和结果聚合。

### Q2: 何时使用 SupervisorAgent？

**A**: 以下场景适合使用 SupervisorAgent：
- 任务需要多个专业领域的知识（如代码审查需要安全、性能、可读性专家）
- 任务可以分解为多个独立子任务（如旅行规划）
- 需要综合多个 Agent 的意见（如技术方案评审）

### Q3: 如何选择聚合策略？

**A**:
- **独立子任务** → StrategyMerge（保留每个 Agent 的完整输出）
- **有依赖任务** → StrategyHierarchy（LLM 综合生成统一结果）
- **需要选最佳** → StrategyBest（从多个结果中选择质量最高的）
- **需要共识** → StrategyConsensus（综合多方意见达成一致）

### Q4: SubAgent 失败会影响整体吗？

**A**: 如果 `EnableFallback = true`，部分 SubAgent 失败不会导致整体失败，而是返回部分结果。建议生产环境启用。

### Q5: 如何优化性能？

**A**:
1. **启用缓存**：重复查询场景可获得 2000 万倍以上加速
2. **使用合适的聚合策略**：StrategyMerge 可并行执行
3. **控制并发数**：避免触发 API 限流
4. **设置合理超时**：避免长时间等待

### Q6: 高级功能的缓存持久化吗？

**A**: 当前缓存存储在内存中，程序退出后自动清空。如需持久化缓存，可扩展为 Redis 等外部存储。

### Q7: 为什么要用 `go run main.go` 而不是 `go run *.go`？

**A**:
- `go run *.go` 会包含 `test_direct.go`，导致 "main redeclared" 错误
- 使用模块化包结构后，只需运行 `main.go`，Go 编译器会自动发现并编译 `features/` 包
- 这符合 Go 最佳实践

## 业务场景示例

### 场景 1：智能客服系统

```go
// 任务：客户询问退款政策，并要求查询订单状态
policyAgent := createPolicyAgent(llm)    // 查询退款政策
orderAgent := createOrderAgent(llm)      // 查询订单状态
replyAgent := createReplyAgent(llm)      // 生成客服回复

config := agents.DefaultSupervisorConfig()
config.AggregationStrategy = agents.StrategyHierarchy

supervisor := agents.NewSupervisorAgent(llm, config)
supervisor.AddSubAgent("policy", policyAgent)
supervisor.AddSubAgent("order", orderAgent)
supervisor.AddSubAgent("reply", replyAgent)
```

**流程**：
1. PolicyAgent → 退款政策说明
2. OrderAgent → 订单状态（已发货）
3. ReplyAgent → 综合回复："根据退款政策，已发货订单..."

### 场景 2：技术文档生成

```go
// 任务：为用户认证功能生成完整的技术文档
requirementAgent := createRequirementAgent(llm)  // 分析需求
designAgent := createDesignAgent(llm)            // 技术设计
apiAgent := createAPIAgent(llm)                  // API 规范
testAgent := createTestAgent(llm)                // 测试用例

config.AggregationStrategy = agents.StrategyHierarchy
```

### 场景 3：数据分析报告

```go
// 任务：分析销售数据，找出趋势并生成报告
cleanAgent := createDataCleanAgent(llm)          // 数据清洗
analysisAgent := createAnalysisAgent(llm)        // 统计分析
vizAgent := createVisualizationAgent(llm)        // 生成图表描述
reportAgent := createReportAgent(llm)            // 撰写报告

config.AggregationStrategy = agents.StrategyHierarchy
```

## 相关资源

### 文档

- **[QUICK_START.md](./QUICK_START.md)** - 快速开始指南，包含详细的运行说明
- **[REQUIREMENTS.md](./REQUIREMENTS.md)** - 详细需求说明，8 个业务场景
- **[SOLUTION.md](./SOLUTION.md)** - 实现方案文档，技术架构设计
- **[ADVANCED_FEATURES.md](./ADVANCED_FEATURES.md)** - 高级功能使用指南
- **[OPTIMIZATION_COMPLETE.md](./OPTIMIZATION_COMPLETE.md)** - 包结构优化说明

### 源代码

- **[agents/supervisor.go](../../../agents/supervisor.go)** - SupervisorAgent 核心实现
- **[main.go](./main.go)** - 完整示例代码（basic, travel, review）
- **[features/advanced.go](./features/advanced.go)** - 高级功能实现

### GoAgent 项目

- **[GoAgent 文档](../../../README.md)** - 项目主文档
- **[架构文档](../../../docs/architecture/ARCHITECTURE.md)** - 整体架构设计
- **[测试最佳实践](../../../docs/development/TESTING_BEST_PRACTICES.md)** - 测试规范

## 贡献

如果你有新的场景示例或改进建议，欢迎提交 PR！

**贡献方向**：
- 新的业务场景示例
- 性能优化方案
- 错误处理最佳实践
- 文档改进

## 更新日志

### v2.0 (2025-11-19)

- ✅ 优化包结构：将高级功能提取到 `features/` 包
- ✅ 简化运行命令：从 `go run main.go advanced_features.go` 简化为 `go run main.go`
- ✅ 实现高级功能：缓存（2200万倍加速）、工具调用、自动 Fallback、批处理、多模态
- ✅ 修复 SupervisorAgent 任务路由问题：保留完整的原始输入
- ✅ 增加超时配置：复杂任务支持 60 秒超时
- ✅ 完善文档：新增 QUICK_START、ADVANCED_FEATURES 等文档

### v1.0 (2025-11-18)

- ✅ 初始版本发布
- ✅ 实现 3 个基础场景：basic, travel, review
- ✅ 支持 4 种聚合策略：Merge, Hierarchy, Best, Consensus
- ✅ 完整的需求和方案文档

---

**版本**：v2.0
**更新时间**：2025-11-19
**维护者**：GoAgent Team
