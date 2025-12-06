# 01-basic-system 基础系统示例

本示例演示 MultiAgentSystem 的核心功能，包括创建多智能体系统、注册 Agent、执行协作任务和 Agent 间消息通信。

## 目录

- [架构设计](#架构设计)
- [核心组件](#核心组件)
- [执行流程](#执行流程)
- [使用方法](#使用方法)
- [代码结构](#代码结构)

## 架构设计

### 系统架构图

```mermaid
graph TB
    subgraph MultiAgentSystem["MultiAgentSystem 多智能体系统"]
        direction TB
        Registry["Agent 注册表"]
        TaskExecutor["任务执行器"]
        MessageBus["消息总线"]
    end

    subgraph Agents["协作 Agent 集合"]
        Leader["Leader Agent<br/>领导者"]
        Worker1["Worker Agent 1<br/>工作者"]
        Worker2["Worker Agent 2<br/>工作者"]
        Validator["Validator Agent<br/>验证者"]
    end

    subgraph Tasks["协作任务"]
        ParallelTask["并行任务"]
        SequentialTask["顺序任务"]
    end

    Leader --> Registry
    Worker1 --> Registry
    Worker2 --> Registry
    Validator --> Registry

    Tasks --> TaskExecutor
    TaskExecutor --> Agents
    Agents <--> MessageBus
```

### 组件关系图

```mermaid
classDiagram
    class MultiAgentSystem {
        -agents map~string~Agent
        -logger Logger
        -maxAgents int
        +RegisterAgent(id, agent)
        +UnregisterAgent(id)
        +ExecuteTask(ctx, task)
        +GetAgent(id) Agent
    }

    class CollaborativeAgent {
        <<interface>>
        +Name() string
        +GetRole() Role
        +SetRole(role)
        +Collaborate(ctx, task) Assignment
        +ReceiveMessage(ctx, msg) error
        +Vote(ctx, proposal) bool
    }

    class BaseCollaborativeAgent {
        -id string
        -description string
        -role Role
        -system MultiAgentSystem
        +Name() string
        +GetRole() Role
        +Collaborate(ctx, task) Assignment
    }

    class CollaborativeTask {
        +ID string
        +Name string
        +Type CollaborationType
        +Input interface
        +Assignments map~string~Assignment
    }

    MultiAgentSystem "1" --> "*" CollaborativeAgent : 管理
    BaseCollaborativeAgent ..|> CollaborativeAgent : 实现
    MultiAgentSystem --> CollaborativeTask : 执行
```

## 核心组件

### 1. MultiAgentSystem

多智能体系统的核心管理器，负责：

- Agent 注册与生命周期管理
- 任务分发与执行协调
- Agent 间消息路由

### 2. CollaborativeAgent

协作 Agent 接口，定义了 Agent 的核心能力：

| 方法 | 说明 |
|------|------|
| `Name()` | 返回 Agent 唯一标识 |
| `GetRole()` | 获取 Agent 角色 |
| `Collaborate()` | 执行协作任务 |
| `ReceiveMessage()` | 接收消息 |
| `Vote()` | 参与投票决策 |

### 3. Agent 角色

```mermaid
graph LR
    subgraph Roles["Agent 角色类型"]
        Leader["Leader<br/>领导者"]
        Worker["Worker<br/>工作者"]
        Coordinator["Coordinator<br/>协调者"]
        Specialist["Specialist<br/>专家"]
        Validator["Validator<br/>验证者"]
        Observer["Observer<br/>观察者"]
    end

    Leader --> |"分配任务"| Worker
    Leader --> |"协调"| Coordinator
    Coordinator --> |"调度"| Worker
    Worker --> |"请求验证"| Validator
    Specialist --> |"提供专业意见"| Leader
    Observer --> |"监控状态"| Leader
```

## 执行流程

### 任务执行流程图

```mermaid
sequenceDiagram
    participant Client as 客户端
    participant MAS as MultiAgentSystem
    participant Leader as Leader Agent
    participant Worker1 as Worker Agent 1
    participant Worker2 as Worker Agent 2
    participant Validator as Validator Agent

    Client->>MAS: ExecuteTask(task)
    MAS->>MAS: 识别任务类型

    alt 并行任务
        par 并行执行
            MAS->>Leader: Collaborate(task)
            Leader-->>MAS: Assignment
        and
            MAS->>Worker1: Collaborate(task)
            Worker1-->>MAS: Assignment
        and
            MAS->>Worker2: Collaborate(task)
            Worker2-->>MAS: Assignment
        and
            MAS->>Validator: Collaborate(task)
            Validator-->>MAS: Assignment
        end
    else 顺序任务
        MAS->>Leader: Collaborate(task)
        Leader-->>MAS: Assignment
        MAS->>Worker1: Collaborate(task)
        Worker1-->>MAS: Assignment
        MAS->>Worker2: Collaborate(task)
        Worker2-->>MAS: Assignment
        MAS->>Validator: Collaborate(task)
        Validator-->>MAS: Assignment
    end

    MAS->>MAS: 汇总结果
    MAS-->>Client: TaskResult
```

### Agent 消息通信流程

```mermaid
sequenceDiagram
    participant Leader as Leader Agent
    participant MAS as MultiAgentSystem
    participant Worker as Worker Agent

    Leader->>MAS: 创建消息(Command)
    Note over Leader,MAS: Message{From: leader, To: worker, Type: command}

    MAS->>Worker: ReceiveMessage(msg)
    Worker->>Worker: 处理命令
    Worker-->>MAS: 返回结果

    Note over Leader,Worker: 消息类型: Request, Response, Broadcast, Command, Report, Vote
```

### Agent 生命周期

```mermaid
stateDiagram-v2
    [*] --> Created: NewBaseCollaborativeAgent()
    Created --> Registered: system.RegisterAgent()
    Registered --> Idle: 等待任务
    Idle --> Executing: Collaborate() 被调用
    Executing --> Idle: 任务完成
    Idle --> Messaging: ReceiveMessage()
    Messaging --> Idle: 消息处理完成
    Registered --> Unregistered: system.UnregisterAgent()
    Unregistered --> [*]
```

## 使用方法

### 运行示例

```bash
cd examples/multiagent/01-basic-system
go run main.go
```

### 预期输出

```text
╔════════════════════════════════════════════════════════════════╗
║          MultiAgentSystem 基础示例                             ║
╚════════════════════════════════════════════════════════════════╝

【步骤 1】创建 MultiAgentSystem
✓ MultiAgentSystem 创建成功

【步骤 2】创建并注册协作 Agent
✓ 注册 Agent: leader-1 (角色: leader)
✓ 注册 Agent: worker-1 (角色: worker)
✓ 注册 Agent: worker-2 (角色: worker)
✓ 注册 Agent: validator-1 (角色: validator)

【步骤 3】执行并行协作任务
✓ 任务状态: completed
✓ 执行时长: 100ms

【步骤 4】执行顺序协作任务
✓ 任务状态: completed

【步骤 5】Agent 消息通信
✓ 消息发送成功

【步骤 6】注销 Agent
✓ Agent worker-2 已注销
```

## 代码结构

```text
01-basic-system/
├── main.go          # 示例入口
└── README.md        # 本文档
```

### 关键代码片段

#### 创建系统和注册 Agent

```go
// 创建多智能体系统
system := multiagent.NewMultiAgentSystem(
    logger,
    multiagent.WithMaxAgents(10),
)

// 创建并注册 Agent
agent := multiagent.NewBaseCollaborativeAgent(
    "worker-1",
    "工作者 Agent",
    multiagent.RoleWorker,
    system,
)
system.RegisterAgent("worker-1", agent)
```

#### 执行协作任务

```go
task := &multiagent.CollaborativeTask{
    ID:          "task-001",
    Name:        "数据处理",
    Type:        multiagent.CollaborationTypeParallel,
    Input:       map[string]interface{}{"data": "..."},
    Assignments: make(map[string]multiagent.Assignment),
}

result, err := system.ExecuteTask(ctx, task)
```

#### 发送消息

```go
message := multiagent.Message{
    ID:        "msg-001",
    From:      "leader-1",
    To:        "worker-1",
    Type:      multiagent.MessageTypeCommand,
    Content:   "开始处理数据",
    Timestamp: time.Now(),
}
agent.ReceiveMessage(ctx, message)
```

## 扩展阅读

- [02-collaboration-types](../02-collaboration-types/) - 协作类型示例
- [03-team-management](../03-team-management/) - 团队管理示例
- [multiagent 包文档](../../../multiagent/)
