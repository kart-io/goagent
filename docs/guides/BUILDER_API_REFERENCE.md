# GoAgent Builder API 速查表

本文档提供 GoAgent Builder API 的快速参考指南，帮助开发者快速找到所需的配置方法。

---

## 📋 目录

- [API 分层概览](#api-分层概览)
- [Simple API（日常使用）](#simple-api日常使用)
- [Core API（标准功能）](#core-api标准功能)
- [Advanced API（高级功能）](#advanced-api高级功能)
- [快速构建函数](#快速构建函数)
- [推理 Agent 专用](#推理-agent-专用)
- [使用示例](#使用示例)

---

## API 分层概览

GoAgent Builder API 采用三层设计，帮助用户逐步学习：

| 层级 | 方法数 | 覆盖场景 | 目标用户 | 学习时间 |
|------|--------|----------|----------|----------|
| **[Simple]** | 5-8 个 | 80% | 快速原型、简单应用 | 5 分钟 |
| **[Core]** | 15-20 个 | 95% | 功能完整、生产应用 | 30 分钟 |
| **[Advanced]** | 30+ 个 | 100% | 企业级、特殊需求 | 2 小时 |

**设计原则**：
- 简单场景使用简单 API（3-5 个方法调用）
- 复杂场景逐步引入更多方法
- 完全向后兼容（所有方法始终可用）

---

## Simple API（日常使用）

### 核心方法（必需）

| 方法 | 参数 | 描述 | 默认值 | 使用频率 |
|------|------|------|--------|----------|
| **WithSystemPrompt** | `string` | 设置系统提示词（定义 Agent 角色） | "" | ⭐⭐⭐⭐⭐ |
| **WithTools** | `...Tool` | 添加工具到 Agent | 无工具 | ⭐⭐⭐⭐⭐ |
| **Build** | 无 | 构建 Agent 实例 | - | ⭐⭐⭐⭐⭐ |

### 常用配置

| 方法 | 参数 | 描述 | 默认值 | 推荐值 |
|------|------|------|--------|--------|
| **WithMaxIterations** | `int` | 最大推理步骤数 | 10 | 简单任务 5-10<br>复杂任务 15-30 |
| **WithTemperature** | `float64` | 控制 LLM 创造性 | 0.7 | 精确 0.0-0.3<br>平衡 0.4-0.7<br>创造 0.8-1.0 |

### 推理模式（可选，二选一）

| 方法 | 参数 | 描述 | 适用场景 |
|------|------|------|----------|
| **WithChainOfThought** | `cot.Config` | 链式思考推理 | 多步骤逻辑推理 |
| **WithReAct** | `react.Config` | 推理-行动循环 | 需要工具调用的任务 |

### Simple API 完整示例

```go
// 最简单的 Agent（3 行代码）
agent, err := builder.NewSimpleBuilder(llmClient).
    WithSystemPrompt("你是一个友好的助手").
    Build()

// 带工具的 Agent
agent, err := builder.NewSimpleBuilder(llmClient).
    WithSystemPrompt("你是一个数据分析助手").
    WithTools(calculator, searchTool).
    WithMaxIterations(15).
    WithTemperature(0.3).
    Build()

// 带推理的 Agent
agent, err := builder.NewSimpleBuilder(llmClient).
    WithSystemPrompt("你是一个推理专家").
    WithChainOfThought(cot.Config{ZeroShot: true}).
    Build()
```

---

## Core API（标准功能）

Core API 在 Simple API 基础上增加以下方法：

### 性能和超时控制

| 方法 | 参数 | 描述 | 默认值 | 使用场景 |
|------|------|------|--------|----------|
| **WithTimeout** | `time.Duration` | Agent 执行超时时间 | 5 分钟 | 防止长时间挂起 |
| **WithMaxTokens** | `int` | LLM 响应最大 token 数 | 2000 | 控制成本和响应长度 |

### 监控和调试

| 方法 | 参数 | 描述 | 默认值 | 使用场景 |
|------|------|------|--------|----------|
| **WithCallbacks** | `...Callback` | 添加回调函数 | 无 | 日志、指标、调试 |
| **WithVerbose** | `bool` | 详细日志 | false | 开发和调试 |
| **WithErrorHandler** | `func(error) error` | 自定义错误处理 | 默认处理 | 重试、降级逻辑 |

### 存储和持久化

| 方法 | 参数 | 描述 | 默认值 | 使用场景 |
|------|------|------|--------|----------|
| **WithStore** | `Store` | 长期存储 | 内存存储 | 持久化数据 |
| **WithCheckpointer** | `Checkpointer` | 检查点器 | 无 | 状态恢复 |

### 更多推理模式

| 方法 | 参数 | 描述 | 适用场景 |
|------|------|------|----------|
| **WithTreeOfThought** | `tot.Config` | 树形思考推理 | 多路径探索 |
| **WithGraphOfThought** | `got.Config` | 图形思考推理 | 复杂依赖关系 |
| **WithProgramOfThought** | `pot.Config` | 程序式思考 | 代码生成和执行 |

### Core API 完整示例

```go
// 生产级 Agent 配置
agent, err := builder.NewSimpleBuilder(llmClient).
    // Simple API
    WithSystemPrompt("你是一个客服助手").
    WithTools(searchKB, createTicket).
    WithMaxIterations(20).
    WithTemperature(0.5).

    // Core API
    WithTimeout(3 * time.Minute).
    WithMaxTokens(3000).
    WithCallbacks(
        core.NewStdoutCallback(true),
        core.NewMetricsCallback(),
    ).
    WithStore(redisStore).
    WithVerbose(false).

    Build()
```

---

## Advanced API（高级功能）

Advanced API 在 Core API 基础上增加以下方法：

### 高级状态管理

| 方法 | 参数 | 描述 | 使用场景 |
|------|------|------|----------|
| **WithState** | `S` | 自定义状态类型 | 需要泛型状态 |
| **WithContext** | `C` | 自定义上下文类型 | 需要泛型上下文 |
| **WithSessionID** | `string` | 会话标识符 | 会话管理 |
| **WithAutoSaveEnabled** | `bool` | 自动保存开关 | 状态持久化 |
| **WithSaveInterval** | `time.Duration` | 自动保存间隔 | 性能优化 |

### 中间件和扩展

| 方法 | 参数 | 描述 | 使用场景 |
|------|------|------|----------|
| **WithMiddleware** | `...Middleware` | 添加中间件 | 功能扩展、拦截 |
| **WithMetadata** | `string, interface{}` | 添加元数据 | 自定义键值对 |

### 高级推理

| 方法 | 参数 | 描述 | 适用场景 |
|------|------|------|----------|
| **WithSkeletonOfThought** | `sot.Config` | 骨架式思考 | 长文本并行生成 |
| **WithMetaCoT** | `metacot.Config` | 元认知链式思考 | 深度分析和自我反思 |

### 流式和性能

| 方法 | 参数 | 描述 | 使用场景 |
|------|------|------|----------|
| **WithStreamingEnabled** | `bool` | 流式响应 | 实时输出 |
| **WithTelemetry** | `interface{}` | OpenTelemetry 集成 | 分布式追踪 |
| **WithCommunicator** | `interface{}` | Agent 间通信 | 多 Agent 系统 |

### Advanced API 完整示例

```go
// 企业级 Agent 配置
type CustomState struct {
    *core.AgentState
    UserProfile map[string]interface{}
}

agent, err := builder.NewAgentBuilder[any, *CustomState](llmClient).
    // Simple + Core API（省略...）
    WithSystemPrompt("...").
    WithTools(...).
    WithStore(redisStore).
    WithCallbacks(...).

    // Advanced API
    WithState(&CustomState{
        AgentState: core.NewAgentState(),
        UserProfile: make(map[string]interface{}),
    }).
    WithMiddleware(
        middleware.NewLoggingMiddleware(nil),
        middleware.NewCachingMiddleware(cache.NewSimpleCache(5*time.Minute)),
        middleware.NewRateLimitMiddleware(10, 1),
    ).
    WithSessionID("user-123-session-456").
    WithStreamingEnabled(true).
    WithTelemetry(otelProvider).
    WithAutoSaveEnabled(true).
    WithSaveInterval(1 * time.Minute).

    Build()
```

---

## 快速构建函数

当你不需要精细控制时，使用快速函数一行创建 Agent：

### 通用 Agent

| 函数 | 参数 | 描述 | 使用场景 |
|------|------|------|----------|
| **QuickAgent** | `llm, prompt` | 最简单的 Agent | 快速原型 |
| **ChatAgent** | `llm, userName` | 聊天机器人 | 对话应用 |
| **RAGAgent** | `llm, retriever` | RAG 应用 | 知识问答 |
| **AnalysisAgent** | `llm, dataSource` | 数据分析 | 数据洞察 |
| **WorkflowAgent** | `llm, workflows` | 工作流编排 | 复杂流程 |
| **MonitoringAgent** | `llm, interval` | 监控告警 | 系统监控 |
| **ResearchAgent** | `llm, sources` | 研究助手 | 文献调研 |

### 推理 Agent

| 函数 | 参数 | 描述 | 适用场景 |
|------|------|------|----------|
| **QuickCoTAgent** | `llm` | CoT Agent | 逻辑推理 |
| **QuickReActAgent** | `llm, tools` | ReAct Agent | 工具调用 |
| **QuickToTAgent** | `llm` | ToT Agent | 多路径探索 |
| **QuickPoTAgent** | `llm` | PoT Agent | 代码生成 |
| **QuickSoTAgent** | `llm` | SoT Agent | 长文本生成 |
| **QuickGoTAgent** | `llm` | GoT Agent | 复杂DAG推理 |
| **QuickMetaCoTAgent** | `llm` | MetaCoT Agent | 自我反思 |

### 快速函数示例

```go
// 一行代码创建 Agent
agent, err := builder.QuickAgent(llmClient, "你是一个助手")

// 一行代码创建 RAG Agent
agent, err := builder.RAGAgent(llmClient, vectorStoreRetriever)

// 一行代码创建 ReAct Agent
agent, err := builder.QuickReActAgent(llmClient, []interfaces.Tool{tool1, tool2})
```

---

## 推理 Agent 专用

### Chain-of-Thought (CoT)

```go
// 零样本 CoT
agent, _ := builder.NewSimpleBuilder(llm).
    WithChainOfThought(cot.Config{ZeroShot: true}).
    Build()

// 少样本 CoT
agent, _ := builder.NewSimpleBuilder(llm).
    WithFewShotCoT([]cot.Example{
        {Question: "2+2=?", Reasoning: "2加2等于4", Answer: "4"},
    }).
    Build()
```

### Tree-of-Thought (ToT)

```go
// DFS 搜索
agent, _ := builder.NewSimpleBuilder(llm).
    WithDFSToT().
    Build()

// Beam Search
agent, _ := builder.NewSimpleBuilder(llm).
    WithBeamSearchToT(width, depth).
    Build()

// Monte Carlo Tree Search
agent, _ := builder.NewSimpleBuilder(llm).
    WithMCTSToT(iterations).
    Build()
```

### ReAct（推理-行动）

```go
// 标准 ReAct
agent, _ := builder.NewSimpleBuilder(llm).
    WithTools(tool1, tool2).
    WithReAct(react.Config{
        MaxIterations: 10,
        ReasoningPrompt: "...",
    }).
    Build()
```

---

## 使用示例

### 场景 1：快速原型（Simple API）

```go
// 3 行代码创建一个简单 Agent
agent, err := builder.NewSimpleBuilder(llmClient).
    WithSystemPrompt("你是一个翻译助手").
    Build()

if err != nil {
    log.Fatal(err)
}

result, _ := agent.Execute(context.Background(), "翻译: Hello World")
fmt.Println(result)
```

**使用的 API**：
- Simple: WithSystemPrompt, Build
- 方法数：2 个

---

### 场景 2：功能完整的生产 Agent（Core API）

```go
// 生产级配置（10 行代码）
agent, err := builder.NewSimpleBuilder(llmClient).
    // Simple API
    WithSystemPrompt("你是一个客服助手").
    WithTools(searchKB, createTicket, sendEmail).
    WithMaxIterations(20).
    WithTemperature(0.5).

    // Core API
    WithTimeout(3 * time.Minute).
    WithMaxTokens(3000).
    WithCallbacks(core.NewStdoutCallback(true)).
    WithStore(redisStore).
    WithVerbose(false).

    Build()

if err != nil {
    log.Fatal(err)
}

// 带 RAG 的 Agent
agent, err := builder.RAGAgent(llmClient, vectorStoreRetriever)
```

**使用的 API**：
- Simple: WithSystemPrompt, WithTools, WithMaxIterations, WithTemperature
- Core: WithTimeout, WithMaxTokens, WithCallbacks, WithStore, WithVerbose
- 方法数：9 个

---

### 场景 3：企业级自定义 Agent（Advanced API）

```go
// 自定义状态类型
type CustomState struct {
    *core.AgentState
    UserProfile map[string]interface{}
    BusinessContext map[string]string
}

// 企业级配置（20+ 行代码）
agent, err := builder.NewAgentBuilder[any, *CustomState](llmClient).
    // Simple + Core API
    WithSystemPrompt("你是一个企业级助手").
    WithTools(crmTool, erpTool, biTool).
    WithMaxIterations(30).
    WithStore(pgStore).
    WithCallbacks(metricsCallback, auditCallback).
    WithTimeout(10 * time.Minute).

    // Advanced API
    WithState(&CustomState{
        AgentState: core.NewAgentState(),
        UserProfile: loadUserProfile(),
        BusinessContext: loadBusinessContext(),
    }).
    WithMiddleware(
        middleware.NewAuthMiddleware(authProvider),
        middleware.NewCachingMiddleware(redisCache),
        middleware.NewRateLimitMiddleware(100, 60),
        middleware.NewAuditMiddleware(auditLog),
    ).
    WithSessionID(generateSessionID()).
    WithStreamingEnabled(true).
    WithTelemetry(otelProvider).
    WithMetadata("tenant_id", tenantID).
    WithMetadata("region", region).
    WithAutoSaveEnabled(true).
    WithSaveInterval(30 * time.Second).
    WithErrorHandler(func(err error) error {
        // 自定义错误处理：重试、降级、告警
        return customErrorHandler(err)
    }).

    Build()

if err != nil {
    log.Fatal(err)
}
```

**使用的 API**：
- Simple: 4 个
- Core: 5 个
- Advanced: 10 个
- 方法数：19 个

---

## 场景预设配置

快速应用场景优化配置：

```go
// RAG 场景预设
agent, _ := builder.NewSimpleBuilder(llm).
    WithSystemPrompt("...").
    WithTools(retriever).
    ConfigureForRAG().  // 自动配置 RAG 最佳参数
    Build()

// 聊天机器人场景
agent, _ := builder.NewSimpleBuilder(llm).
    WithSystemPrompt("...").
    ConfigureForChatbot().  // 自动配置聊天最佳参数
    Build()

// 数据分析场景
agent, _ := builder.NewSimpleBuilder(llm).
    WithSystemPrompt("...").
    WithTools(calculator, plotter).
    ConfigureForAnalysis().  // 自动配置分析最佳参数
    Build()
```

---

## 方法速查索引

### 按字母顺序

| 方法名 | 层级 | 参数类型 | 描述 |
|--------|------|----------|------|
| Build | Simple | 无 | 构建 Agent |
| ConfigureForAnalysis | Simple | 无 | 数据分析预设 |
| ConfigureForChatbot | Simple | 无 | 聊天机器人预设 |
| ConfigureForRAG | Simple | 无 | RAG 预设 |
| WithAutoSaveEnabled | Advanced | bool | 自动保存开关 |
| WithCallbacks | Core | ...Callback | 回调函数 |
| WithChainOfThought | Simple | cot.Config | CoT 推理 |
| WithCheckpointer | Core | Checkpointer | 检查点器 |
| WithCommunicator | Advanced | interface{} | Agent 通信 |
| WithContext | Advanced | C | 自定义上下文 |
| WithErrorHandler | Core | func(error)error | 错误处理 |
| WithFewShotCoT | Simple | []Example | 少样本 CoT |
| WithGraphOfThought | Core | got.Config | GoT 推理 |
| WithMaxIterations | Simple | int | 最大步骤数 |
| WithMaxTokens | Core | int | 最大 token 数 |
| WithMetadata | Advanced | string, interface{} | 元数据 |
| WithMetaCoT | Advanced | metacot.Config | MetaCoT 推理 |
| WithMiddleware | Advanced | ...Middleware | 中间件 |
| WithProgramOfThought | Core | pot.Config | PoT 推理 |
| WithReAct | Simple | react.Config | ReAct 推理 |
| WithSaveInterval | Advanced | time.Duration | 保存间隔 |
| WithSessionID | Advanced | string | 会话 ID |
| WithSkeletonOfThought | Advanced | sot.Config | SoT 推理 |
| WithState | Advanced | S | 自定义状态 |
| WithStore | Core | Store | 长期存储 |
| WithStreamingEnabled | Advanced | bool | 流式响应 |
| WithSystemPrompt | Simple | string | 系统提示词 |
| WithTelemetry | Advanced | interface{} | OpenTelemetry |
| WithTemperature | Simple | float64 | 温度参数 |
| WithTimeout | Core | time.Duration | 超时时间 |
| WithTools | Simple | ...Tool | 添加工具 |
| WithTreeOfThought | Core | tot.Config | ToT 推理 |
| WithVerbose | Core | bool | 详细日志 |
| WithZeroShotCoT | Simple | 无 | 零样本 CoT |

### 按使用频率

| 频率 | 方法名 | 使用率 |
|------|--------|--------|
| ⭐⭐⭐⭐⭐ | WithSystemPrompt | 99% |
| ⭐⭐⭐⭐⭐ | Build | 100% |
| ⭐⭐⭐⭐ | WithTools | 80% |
| ⭐⭐⭐⭐ | WithMaxIterations | 70% |
| ⭐⭐⭐ | WithTemperature | 60% |
| ⭐⭐⭐ | WithCallbacks | 50% |
| ⭐⭐ | WithTimeout | 40% |
| ⭐⭐ | WithMaxTokens | 40% |
| ⭐⭐ | WithStore | 30% |
| ⭐ | WithVerbose | 20% |

---

## 学习路径建议

### 第 1 天：Simple API（5 分钟）

1. 学习 `NewSimpleBuilder` 函数
2. 掌握 `WithSystemPrompt` 和 `Build`
3. 尝试添加 `WithTools`
4. 运行第一个 Agent

```go
agent, _ := builder.NewSimpleBuilder(llm).
    WithSystemPrompt("你是一个助手").
    Build()
```

### 第 2 天：Core API（30 分钟）

1. 学习配置参数：`WithMaxIterations`, `WithTemperature`
2. 添加监控：`WithCallbacks`, `WithVerbose`
3. 配置存储：`WithStore`
4. 尝试推理模式：`WithChainOfThought`

### 第 3 天：Advanced API（2 小时）

1. 理解泛型：`AgentBuilder[C, S]`
2. 自定义状态：`WithState`, `WithContext`
3. 添加中间件：`WithMiddleware`
4. 集成监控：`WithTelemetry`

---

## 常见问题 FAQ

### Q1: Simple/Core/Advanced 层级是强制的吗？

**A**: 不是！所有方法始终可用，层级仅作为学习指引。你可以在任何时候使用任何方法。

### Q2: 我应该从哪个层级开始？

**A**: 建议从 Simple API 开始（3-5 个方法），根据需要逐步添加更多配置。80% 的场景只需要 Simple API。

### Q3: 泛型 `AgentBuilder[C, S]` 是什么？

**A**: 这是高级特性，允许自定义上下文和状态类型。大多数情况下使用 `NewSimpleBuilder(llm)` 即可，它使用默认类型 `any` 和 `*core.AgentState`。

### Q4: 快速函数 vs Builder 模式，哪个更好？

**A**:
- **快速函数**：适合原型和简单场景（1 行代码）
- **Builder 模式**：适合需要精细控制的生产场景（可配置性强）

根据项目阶段选择：原型阶段用快速函数，生产阶段用 Builder。

### Q5: 如何知道我应该用哪个推理方法？

**A**: 推理方法选择指南：
- **CoT**：多步骤逻辑推理（数学、逻辑题）
- **ReAct**：需要工具调用的任务（搜索、计算）
- **ToT**：需要探索多个可能性（创意、规划）
- **GoT**：复杂依赖关系（流程图、因果分析）
- **PoT**：代码生成和执行（数据处理、自动化）
- **SoT**：长文本生成（文章、报告）

### Q6: 为什么有些方法标记为 [Advanced]？

**A**: [Advanced] 方法通常有以下特征：
- 需要深入理解 GoAgent 架构
- 需要额外的依赖或配置
- 仅在特殊场景下需要
- 可能增加系统复杂度

新手建议先掌握 [Simple] 和 [Core] 方法。

---

## 相关资源

- **完整文档**: [GoAgent 官方文档](../README.md)
- **示例代码**: [examples/](../../examples/)
  - `simple/` - Simple API 示例
  - `core/` - Core API 示例
  - `advanced/` - Advanced API 示例
- **详细指南**:
  - [快速开始](./QUICKSTART.md)
  - [工具中间件](./TOOL_MIDDLEWARE.md)
  - [缓存指南](./CACHING_GUIDE.md)
  - [架构文档](../architecture/CORE_ARCHITECTURE.md)

---

**最后更新时间**: 2025-12-04
**适用版本**: GoAgent v1.x
**维护者**: GoAgent Team
