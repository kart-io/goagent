# 03-team-management 团队管理示例

本示例演示多智能体系统的团队管理功能，包括创建团队、分配角色、管理团队成员和跨团队协作项目。

## 目录

- [架构设计](#架构设计)
- [团队结构](#团队结构)
- [执行流程](#执行流程)
- [使用方法](#使用方法)
- [核心功能](#核心功能)

## 架构设计

### 整体架构图

```mermaid
graph TB
    subgraph MAS["MultiAgentSystem"]
        Registry["Agent 注册表"]
        TeamMgr["团队管理器"]
        TaskExec["任务执行器"]
    end

    subgraph Teams["团队组织"]
        DevTeam["研发团队<br/>team-dev"]
        DataTeam["数据团队<br/>team-data"]
        OpsTeam["运维团队<br/>team-ops"]
    end

    subgraph Project["跨团队项目"]
        Phase1["阶段1: 数据准备"]
        Phase2["阶段2: 模型开发"]
        Phase3["阶段3: 后端集成"]
        Phase4["阶段4: 部署上线"]
    end

    Registry --> Teams
    TeamMgr --> Teams
    TaskExec --> Project

    DataTeam --> Phase1
    DataTeam --> Phase2
    DevTeam --> Phase3
    OpsTeam --> Phase4
```

### 团队与 Agent 关系

```mermaid
classDiagram
    class Team {
        +ID string
        +Name string
        +Leader string
        +Members list~string~
        +Purpose string
        +Capabilities list~string~
        +Metadata map~string~interface~
    }

    class CollaborativeAgent {
        <<interface>>
        +Name() string
        +GetRole() Role
        +SetRole(role)
        +Collaborate(ctx, task) Assignment
    }

    class MultiAgentSystem {
        +CreateTeam(team) error
        +GetTeam(id) Team
        +RegisterAgent(id, agent)
        +ExecuteTask(ctx, task) Result
    }

    MultiAgentSystem "1" --> "*" Team : 管理
    Team "1" --> "*" CollaborativeAgent : 包含
    MultiAgentSystem "1" --> "*" CollaborativeAgent : 注册
```

## 团队结构

### 研发团队 (team-dev)

```mermaid
graph TB
    subgraph DevTeam["研发团队"]
        DevLead["dev-lead<br/>★ 负责人<br/>Leader"]

        subgraph Frontend["前端组"]
            FE1["frontend-dev-1<br/>Worker"]
            FE2["frontend-dev-2<br/>Worker"]
        end

        subgraph Backend["后端组"]
            BE1["backend-dev-1<br/>Worker"]
            BE2["backend-dev-2<br/>Worker"]
        end

        QA["qa-engineer<br/>Validator"]
    end

    DevLead --> Frontend
    DevLead --> Backend
    DevLead --> QA

    style DevLead fill:#ffeb3b
```

**团队能力**：

- 前端开发
- 后端开发
- API 设计
- 单元测试
- 集成测试

### 数据分析团队 (team-data)

```mermaid
graph TB
    subgraph DataTeam["数据分析团队"]
        DataLead["data-lead<br/>★ 负责人<br/>Leader"]

        subgraph Engineering["数据工程"]
            DE1["data-engineer-1<br/>Worker"]
            DE2["data-engineer-2<br/>Worker"]
        end

        subgraph Analysis["数据分析"]
            DA["data-analyst<br/>Specialist"]
            ML["ml-engineer<br/>Specialist"]
        end
    end

    DataLead --> Engineering
    DataLead --> Analysis

    style DataLead fill:#ffeb3b
```

**团队能力**：

- 数据采集
- 数据清洗
- 数据分析
- 机器学习
- 报表生成

### 运维团队 (team-ops)

```mermaid
graph TB
    subgraph OpsTeam["运维团队"]
        OpsLead["ops-lead<br/>★ 负责人<br/>Leader"]

        subgraph DevOps["DevOps"]
            DO1["devops-1<br/>Worker"]
            DO2["devops-2<br/>Worker"]
        end

        SRE["sre-1<br/>Specialist"]
        Monitor["monitor<br/>Observer"]
    end

    OpsLead --> DevOps
    OpsLead --> SRE
    OpsLead --> Monitor

    style OpsLead fill:#ffeb3b
```

**团队能力**：

- CI/CD
- 容器化部署
- 系统监控
- 故障排查
- 性能优化

## 执行流程

### 跨团队项目执行流程

```mermaid
sequenceDiagram
    participant Coord as 项目协调者
    participant Data as 数据团队
    participant Dev as 研发团队
    participant Ops as 运维团队

    rect rgb(230, 245, 255)
        Note over Data: 阶段1: 数据准备
        Coord->>Data: 启动数据准备
        Data->>Data: 数据采集、清洗、特征工程
        Data-->>Coord: 阶段1完成
    end

    rect rgb(230, 245, 255)
        Note over Data: 阶段2: 模型开发
        Coord->>Data: 启动模型开发
        Data->>Data: 多模型并行训练和评估
        Data-->>Coord: 阶段2完成
    end

    rect rgb(255, 245, 230)
        Note over Dev: 阶段3: 后端集成
        Coord->>Dev: 启动后端集成
        Dev->>Dev: API 开发、模型集成、测试
        Dev-->>Coord: 阶段3完成
    end

    rect rgb(230, 255, 230)
        Note over Ops: 阶段4: 部署上线
        Coord->>Ops: 启动部署上线
        Ops->>Ops: 容器化、部署、监控配置
        Ops-->>Coord: 阶段4完成
    end

    Note over Coord,Ops: 项目完成
```

### 项目阶段与协作类型

```mermaid
flowchart LR
    subgraph Phase1["阶段1: 数据准备"]
        direction TB
        P1Type["Sequential<br/>顺序协作"]
        P1Task["采集 → 清洗 → 特征工程"]
    end

    subgraph Phase2["阶段2: 模型开发"]
        direction TB
        P2Type["Parallel<br/>并行协作"]
        P2Task["多模型并行训练"]
    end

    subgraph Phase3["阶段3: 后端集成"]
        direction TB
        P3Type["Sequential<br/>顺序协作"]
        P3Task["API → 集成 → 测试"]
    end

    subgraph Phase4["阶段4: 部署上线"]
        direction TB
        P4Type["Pipeline<br/>管道协作"]
        P4Task["容器化 → 部署 → 监控"]
    end

    Phase1 --> Phase2 --> Phase3 --> Phase4

    style P1Type fill:#e3f2fd
    style P2Type fill:#e8f5e9
    style P3Type fill:#e3f2fd
    style P4Type fill:#fff3e0
```

### 角色动态调整流程

```mermaid
stateDiagram-v2
    [*] --> Worker: 初始角色

    Worker --> Coordinator: 项目紧急<br/>临时提升
    Coordinator --> Worker: 任务完成<br/>恢复原角色

    Worker --> Specialist: 专业任务<br/>角色转换
    Specialist --> Worker: 任务完成<br/>恢复原角色

    Worker --> Leader: 晋升
    Leader --> [*]: 退出系统
```

## 使用方法

### 运行示例

```bash
cd examples/multiagent/03-team-management
go run main.go
```

### 预期输出

```text
╔════════════════════════════════════════════════════════════════╗
║          多智能体团队管理示例                                  ║
║   展示如何创建团队、分配角色、管理团队成员                     ║
╚════════════════════════════════════════════════════════════════╝

【步骤 1】创建 Agent 池
✓ 共创建 17 个 Agent

【步骤 2】创建研发团队
┌─────────────────────────────────────────┐
│ 团队: 研发团队                          │
│ 成员数: 6                               │
└─────────────────────────────────────────┘

【步骤 3】创建数据分析团队
【步骤 4】创建运维团队

【步骤 5】跨团队协作项目
项目名称: 智能推荐系统 v2.0

【阶段 1】数据准备阶段
✓ 阶段完成 - 状态: completed

【阶段 2】模型开发阶段
✓ 阶段完成 - 状态: completed

【阶段 3】后端集成阶段
✓ 阶段完成 - 状态: completed

【阶段 4】部署上线阶段
✓ 阶段完成 - 状态: completed

✓ 项目所有阶段执行完成!

【步骤 6】角色动态调整
调整前: frontend-dev-1 角色为 worker
调整后: frontend-dev-1 角色为 coordinator
恢复后: frontend-dev-1 角色为 worker
```

## 核心功能

### 1. 团队创建

```go
team := &multiagent.Team{
    ID:      "team-dev",
    Name:    "研发团队",
    Leader:  "dev-lead",
    Members: []string{"dev-lead", "frontend-dev-1", "backend-dev-1"},
    Purpose: "负责产品功能开发和质量保证",
    Capabilities: []string{"前端开发", "后端开发", "API 设计"},
    Metadata: map[string]interface{}{
        "tech_stack": []string{"React", "Go", "PostgreSQL"},
    },
}

system.CreateTeam(team)
```

### 2. Agent 池管理

```mermaid
graph LR
    subgraph AgentPool["Agent 池"]
        A1["Agent 1"]
        A2["Agent 2"]
        A3["Agent 3"]
        A4["Agent 4"]
        A5["Agent 5"]
    end

    subgraph Team1["团队 A"]
        T1A1["Agent 1"]
        T1A2["Agent 2"]
    end

    subgraph Team2["团队 B"]
        T2A3["Agent 3"]
        T2A4["Agent 4"]
    end

    A1 -.-> T1A1
    A2 -.-> T1A2
    A3 -.-> T2A3
    A4 -.-> T2A4
    A5 -.-> |"未分配"| A5
```

### 3. 角色动态调整

```go
// 获取 Agent
agent := agents["frontend-dev-1"]

// 临时提升为协调者
agent.SetRole(multiagent.RoleCoordinator)

// 执行协调任务...

// 恢复原角色
agent.SetRole(multiagent.RoleWorker)
```

### 功能总结

| 功能 | 说明 | API |
|------|------|-----|
| 团队创建 | 创建具有特定能力的团队 | `CreateTeam()` |
| 成员管理 | 添加/移除团队成员 | `Team.Members` |
| 角色分配 | 为 Agent 分配角色 | `SetRole()` |
| 能力定义 | 定义团队核心能力 | `Team.Capabilities` |
| 跨团队协作 | 多团队协同完成项目 | `ExecuteTask()` |
| 角色动态调整 | 根据需要调整角色 | `SetRole()` |

## 扩展阅读

- [01-basic-system](../01-basic-system/) - 基础系统示例
- [02-collaboration-types](../02-collaboration-types/) - 协作类型示例
- [04-specialized-agents](../04-specialized-agents/) - 专业化 Agent 示例
