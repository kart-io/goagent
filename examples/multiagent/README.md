# MultiAgent 多智能体系统示例

本目录包含 GoAgent 多智能体系统 (MultiAgentSystem) 的完整使用示例，展示如何构建和管理协作式 AI Agent 系统。

## 目录结构

```text
multiagent/
├── 01-basic-system/              # 基础系统示例
├── 02-collaboration-types/       # 协作类型示例
├── 03-team-management/           # 团队管理示例
├── 04-specialized-agents/        # 专业化 Agent 示例
├── 05-llm-collaborative-agents/  # LLM 协作 Agent 示例 (NEW)
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
