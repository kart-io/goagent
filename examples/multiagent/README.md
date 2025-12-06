# MultiAgent 多智能体系统示例

本目录包含 GoAgent 多智能体系统 (MultiAgentSystem) 的完整使用示例，展示如何构建和管理协作式 AI Agent 系统。

## 目录结构

```text
multiagent/
├── 01-basic-system/              # 基础系统示例
├── 02-collaboration-types/       # 协作类型示例
├── 03-team-management/           # 团队管理示例
├── 04-specialized-agents/        # 专业化 Agent 示例
├── 05-llm-collaborative-agents/  # LLM 协作 Agent 示例
├── 06-llm-tool-calling/          # LLM 工具调用示例
├── 07-multiagent-llm-stream/     # 多智能体 LLM 流式示例
├── 08-multiagent-tool-registry/  # 多智能体工具注册表示例
├── 09-multiagent-with-middleware/# 多智能体中间件示例
├── 10-multiagent-integrated/     # 多智能体综合示例（LLM+工具+中间件）
└── README.md                     # 本文档
```

## 快速开始

### 运行示例

```bash
# 运行基础系统示例
cd examples/multiagent/01-basic-system
go run main.go

# 运行协作类型示例
cd examples/multiagent/02-collaboration-types
go run main.go

# 运行团队管理示例
cd examples/multiagent/03-team-management
go run main.go

# 运行专业化 Agent 示例
cd examples/multiagent/04-specialized-agents
go run main.go

# 运行 LLM 协作示例（需要 API Key 或本地 Ollama）
cd examples/multiagent/05-llm-collaborative-agents
export DEEPSEEK_API_KEY="your-api-key"
go run main.go

# 运行 LLM 工具调用示例
cd examples/multiagent/06-llm-tool-calling
export DEEPSEEK_API_KEY="your-api-key"
go run main.go

# 运行多智能体 LLM 流式示例
cd examples/multiagent/07-multiagent-llm-stream
export DEEPSEEK_API_KEY="your-api-key"
go run main.go

# 运行多智能体工具注册表示例
cd examples/multiagent/08-multiagent-tool-registry
go run main.go

# 运行多智能体中间件示例
cd examples/multiagent/09-multiagent-with-middleware
go run main.go

# 运行多智能体综合示例（LLM+工具+中间件）
cd examples/multiagent/10-multiagent-integrated
export DEEPSEEK_API_KEY="your-api-key"  # 可选，未配置时使用模拟模式
go run main.go
```

## 示例说明

### 01-basic-system - 基础系统示例

演示 MultiAgentSystem 的核心功能：

- 创建多智能体系统
- 注册不同角色的协作 Agent
- 执行并行和顺序协作任务
- Agent 间消息通信
- 注销 Agent

**适用场景**: 初学者入门，理解 MultiAgentSystem 基本概念

### 02-collaboration-types - 协作类型示例

演示五种协作模式：

| 协作类型 | 说明 | 适用场景 |
|---------|------|---------|
| **Parallel** | 并行协作 | 独立任务并行处理，如数据分片处理 |
| **Sequential** | 顺序协作 | 有依赖的任务链式处理，如数据流水线 |
| **Hierarchical** | 分层协作 | 层级分明的项目管理，如领导-执行-验证模式 |
| **Consensus** | 共识协作 | 需要多方投票决策，如方案选择、审批流程 |
| **Pipeline** | 管道协作 | 流式数据处理，如 ETL、日志处理 |

**适用场景**: 理解不同协作模式的特点和使用方法

### 03-team-management - 团队管理示例

演示团队管理功能：

- 创建团队 (CreateTeam)
- 设置团队负责人和成员
- 定义团队能力和技术栈
- 跨团队协作项目
- 角色动态调整

**适用场景**: 需要组织多个 Agent 进行复杂项目协作

### 04-specialized-agents - 专业化 Agent 示例

演示高级 Agent 类型：

- **SpecializedAgent**: 领域专家 Agent，提供专业化分析
- **NegotiatingAgent**: 谈判 Agent，支持多轮协商
- **投票机制**: Agent 民主决策

**适用场景**: 需要专业领域知识或复杂决策场景

### 05-llm-collaborative-agents - LLM 协作 Agent 示例

演示使用 LLM 进行智能协作：

- **LLMCollaborativeAgent**: 具有 LLM 推理能力的协作 Agent
- **多专家代码审查**: 安全、性能、质量专家并行审查
- **协作研究分析**: 技术、市场研究员协作分析
- **流水线处理**: 大纲→撰写→编辑的顺序处理

**支持的 LLM 提供商**:

- DeepSeek (`DEEPSEEK_API_KEY`)
- OpenAI (`OPENAI_API_KEY`)
- Ollama (本地部署)

**适用场景**: 需要 LLM 推理能力的复杂任务处理

### 06-llm-tool-calling - LLM 工具调用示例

演示 LLM 工具调用功能：

- **单 Agent 工具调用**: 一个 Agent 配备多工具，自动选择
- **多 Agent 专业工具协作**: 每个 Agent 专注特定工具集
- **Pipeline 工具链**: 工具输出作为下一个工具的输入

**支持的工具类型**:

- `calculator`: 基础数学计算
- `weather`: 天气查询
- `search`: 信息搜索
- `current_time`: 时间查询
- `advanced_math`: 高级数学（三角函数、对数等）

**适用场景**: 需要 LLM 自动调用工具完成复杂任务

### 07-multiagent-llm-stream - 多智能体 LLM 流式示例

演示多 Agent 使用 LLM 流式响应协作：

- **流式响应协作**: 多个专家 Agent 并行使用 LLM 流式响应分析问题
- **流式响应聚合**: 多个 Agent 流式输出实时聚合到协调者
- **多轮流式对话**: Agent 维护对话历史，进行多轮流式交互

**核心组件**:

- `StreamAgent`: 支持流式响应的 Agent
- `AggregatorAgent`: 聚合多个 Agent 响应的协调者
- `ConversationAgent`: 支持多轮对话的 Agent

**适用场景**: 需要实时响应和多 Agent 流式协作的场景

### 08-multiagent-tool-registry - 多智能体工具注册表示例

演示多 Agent 共享工具注册表：

- **共享工具注册表**: 多个 Agent 从同一注册表获取工具
- **分布式工具执行**: 多个 Agent 并行执行不同工具，汇总结果
- **工具结果传递**: Agent 链式处理，前一个工具的输出作为后一个的输入

**核心组件**:

- `tools.Registry`: 工具注册表
- `RegistryAgent`: 使用注册表的 Agent，支持工具权限控制

**适用场景**: 需要统一管理工具、控制工具访问权限的多 Agent 系统

### 09-multiagent-with-middleware - 多智能体中间件示例

演示多 Agent 使用带中间件的工具：

- **日志中间件**: 追踪跨 Agent 的工具调用链
- **指标中间件**: 收集分布式执行的调用指标
- **自定义中间件**: 实现 Agent 级别的访问控制和增强

**核心组件**:

- `middleware.ToolMiddlewareFunc`: 函数式中间件
- `tools.WithMiddleware()`: 为工具应用中间件
- `LogCollector`: 日志收集器
- `MetricsCollector`: 指标收集器
- `AgentTracker`: Agent 追踪器

**适用场景**: 需要可观测性、访问控制和工具增强的多 Agent 系统

### 10-multiagent-integrated - 多智能体综合示例

**同时展示 LLM、工具注册表、中间件和记忆的综合使用**：

- **智能数据分析流水线**: 完整的多 Agent 协作场景
  - 协调者 Agent：使用 LLM 理解任务并分配工作
  - 数据 Agent：使用带中间件的工具获取和处理数据
  - 分析 Agent：使用 LLM 和工具进行智能分析
  - 记忆管理器：存储对话历史和分析案例

**综合特性**:

| 组件 | 功能 | 示例中的应用 |
|------|------|------------|
| **LLM** | 智能决策 | 任务理解、数据分析、报告生成 |
| **工具注册表** | 统一工具管理 | data_fetch, data_process, calculator, formatter |
| **中间件** | 可观测性 | 日志记录、指标收集 |
| **记忆** | 知识存储 | 对话历史、案例存储、键值存储 |

**核心组件**:

- `IntegratedAgent`: 综合 Agent，同时支持 LLM 调用、工具执行和记忆访问
- `tools.Registry`: 共享工具注册表
- `memory.InMemoryManager`: 记忆管理器（对话历史 + 案例存储 + 键值存储）
- `LogCollector` + `MetricsCollector`: 可观测性组件

**流水线流程**:

```
用户任务 → 协调者(LLM理解) → 数据Agent(工具获取) → 分析Agent(LLM+工具分析) → 报告输出
                              ↓                              ↓
                        日志/指标中间件记录              记忆管理器存储
                                                    (对话历史/案例/键值)
```

**适用场景**: 需要完整 AI 能力（LLM推理 + 工具调用 + 可观测性 + 记忆）的复杂多 Agent 系统

## 核心概念

### Agent 角色

```go
const (
    RoleLeader      Role = "leader"      // 领导者
    RoleWorker      Role = "worker"      // 工作者
    RoleCoordinator Role = "coordinator" // 协调者
    RoleSpecialist  Role = "specialist"  // 专家
    RoleValidator   Role = "validator"   // 验证者
    RoleObserver    Role = "observer"    // 观察者
)
```

### 协作类型

```go
const (
    CollaborationTypeParallel     CollaborationType = "parallel"     // 并行
    CollaborationTypeSequential   CollaborationType = "sequential"   // 顺序
    CollaborationTypeHierarchical CollaborationType = "hierarchical" // 分层
    CollaborationTypeConsensus    CollaborationType = "consensus"    // 共识
    CollaborationTypePipeline     CollaborationType = "pipeline"     // 管道
)
```

### 消息类型

```go
const (
    MessageTypeRequest      MessageType = "request"      // 请求
    MessageTypeResponse     MessageType = "response"     // 响应
    MessageTypeBroadcast    MessageType = "broadcast"    // 广播
    MessageTypeNotification MessageType = "notification" // 通知
    MessageTypeCommand      MessageType = "command"      // 命令
    MessageTypeReport       MessageType = "report"       // 报告
    MessageTypeVote         MessageType = "vote"         // 投票
)
```

## 使用示例

### 创建 MultiAgentSystem

```go
import "github.com/kart-io/goagent/multiagent"

// 创建系统
system := multiagent.NewMultiAgentSystem(
    logger,
    multiagent.WithMaxAgents(100),
    multiagent.WithTimeout(30*time.Second),
)
```

### 创建和注册 Agent

```go
// 创建基础协作 Agent
agent := multiagent.NewBaseCollaborativeAgent(
    "agent-id",
    "Agent 描述",
    multiagent.RoleWorker,
    system,
)

// 注册到系统
if err := system.RegisterAgent("agent-id", agent); err != nil {
    log.Fatal(err)
}
```

### 创建团队

```go
team := &multiagent.Team{
    ID:           "team-dev",
    Name:         "研发团队",
    Leader:       "dev-lead",
    Members:      []string{"dev-lead", "dev-1", "dev-2"},
    Purpose:      "负责产品开发",
    Capabilities: []string{"前端开发", "后端开发"},
}

if err := system.CreateTeam(team); err != nil {
    log.Fatal(err)
}
```

### 执行协作任务

```go
task := &multiagent.CollaborativeTask{
    ID:          "task-001",
    Name:        "数据处理任务",
    Description: "多 Agent 协作处理数据",
    Type:        multiagent.CollaborationTypeParallel,
    Input: map[string]interface{}{
        "data_source": "sensor_data",
    },
    Assignments: make(map[string]multiagent.Assignment),
}

result, err := system.ExecuteTask(ctx, task)
if err != nil {
    log.Fatal(err)
}
```

### Agent 间消息通信

```go
// 创建消息
message := multiagent.Message{
    ID:        "msg-001",
    From:      "agent-a",
    To:        "agent-b",
    Type:      multiagent.MessageTypeRequest,
    Content:   "处理数据",
    Priority:  1,
    Timestamp: time.Now(),
}

// 发送消息
if err := agent.ReceiveMessage(ctx, message); err != nil {
    log.Fatal(err)
}
```

### 创建 LLM 协作 Agent

```go
import (
    "github.com/kart-io/goagent/llm"
    "github.com/kart-io/goagent/llm/providers"
)

// 创建 LLM 客户端
llmClient, _ := providers.NewDeepSeekWithOptions(
    llm.WithAPIKey(os.Getenv("DEEPSEEK_API_KEY")),
    llm.WithModel("deepseek-chat"),
)

// 自定义 LLM 协作 Agent
type LLMCollaborativeAgent struct {
    *multiagent.BaseCollaborativeAgent
    llmClient    llm.Client
    systemPrompt string
}

// 使用 LLM 执行协作任务
func (a *LLMCollaborativeAgent) Collaborate(ctx context.Context, task *multiagent.CollaborativeTask) (*multiagent.Assignment, error) {
    response, err := a.llmClient.Complete(ctx, &llm.CompletionRequest{
        Messages: []llm.Message{
            {Role: "system", Content: a.systemPrompt},
            {Role: "user", Content: fmt.Sprintf("%v", task.Input)},
        },
    })
    if err != nil {
        return nil, err
    }

    return &multiagent.Assignment{
        AgentID: a.Name(),
        Result:  response.Content,
        Status:  multiagent.TaskStatusCompleted,
    }, nil
}
```

## 相关文档

- [架构文档](../../docs/architecture/ARCHITECTURE.md)
- [API 参考](../../docs/api/README.md)
- [高级示例 - SupervisorAgent](../advanced/supervisor_agent/)
- [集成示例 - NATS 通信](../integration/multiagent-nats/)
- [LLM 高级用法示例](../llm/advanced/) - 流式响应、能力检查、Token 统计
- [工具注册与执行示例](../tools/registry/) - Registry、FunctionTool、工具组合
- [中间件与可观测性示例](../tools/middleware/) - 日志、缓存、限流中间件

## 常见问题

### Q: 如何选择协作类型？

根据任务特点选择：

- **独立任务可并行** → Parallel
- **任务有依赖关系** → Sequential
- **需要层级管理** → Hierarchical
- **需要投票决策** → Consensus
- **流式数据处理** → Pipeline

### Q: Agent 数量有限制吗？

默认最大 100 个，可通过 `WithMaxAgents()` 配置：

```go
system := multiagent.NewMultiAgentSystem(
    logger,
    multiagent.WithMaxAgents(500),
)
```

### Q: 如何处理 Agent 执行失败？

- 任务会返回失败状态
- 可通过检查 `result.Status` 和 `assignment.Status` 判断
- 建议实现重试机制或降级策略

## 贡献指南

欢迎提交 Issue 和 Pull Request 改进示例代码。
