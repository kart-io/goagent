# 04-specialized-agents 专业化 Agent 示例

本示例演示专业化 Agent 的使用，包括 SpecializedAgent（领域专家）、NegotiatingAgent（谈判 Agent）以及投票决策机制。

## 目录

- [架构设计](#架构设计)
- [Agent 类型](#agent-类型)
- [执行流程](#执行流程)
- [使用方法](#使用方法)
- [应用场景](#应用场景)

## 架构设计

### Agent 类型继承关系

```mermaid
classDiagram
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
        -system *MultiAgentSystem
        +Name() string
        +GetRole() Role
        +Collaborate(ctx, task) Assignment
        +Vote(ctx, proposal) bool
    }

    class SpecializedAgent {
        -specialization string
        +GetSpecialization() string
        +Collaborate(ctx, task) Assignment
    }

    class NegotiatingAgent {
        -negotiationHistory list~Proposal~
        +Propose(ctx, proposal) Response
        +CounterPropose(ctx, counter) Response
        +Accept(ctx) bool
        +Reject(ctx, reason) bool
    }

    CollaborativeAgent <|.. BaseCollaborativeAgent : 实现
    BaseCollaborativeAgent <|-- SpecializedAgent : 继承
    BaseCollaborativeAgent <|-- NegotiatingAgent : 继承
```

### 专业化 Agent 架构

```mermaid
graph TB
    subgraph Specialists["专业化 Agent 集合"]
        Security["安全专家<br/>security-expert"]
        Performance["性能专家<br/>performance-expert"]
        Architecture["架构专家<br/>architecture-expert"]
        ML["机器学习专家<br/>ml-expert"]
        Database["数据库专家<br/>database-expert"]
    end

    subgraph Task["评审任务"]
        Review["系统架构评审"]
    end

    subgraph Results["评审结果"]
        R1["安全性评估"]
        R2["性能瓶颈分析"]
        R3["架构合理性"]
        R4["AI/ML 集成建议"]
        R5["数据存储方案"]
    end

    Review --> Security --> R1
    Review --> Performance --> R2
    Review --> Architecture --> R3
    Review --> ML --> R4
    Review --> Database --> R5

    style Security fill:#ffcdd2
    style Performance fill:#c8e6c9
    style Architecture fill:#bbdefb
    style ML fill:#fff9c4
    style Database fill:#d1c4e9
```

## Agent 类型

### 1. SpecializedAgent 领域专家

领域专家 Agent 具有特定专业领域知识，能够提供专业化分析和建议。

```mermaid
graph LR
    subgraph SpecializedAgent["SpecializedAgent"]
        Base["BaseCollaborativeAgent"]
        Spec["specialization<br/>专业领域"]
        Analyze["专业分析能力"]
    end

    Input["任务输入"] --> SpecializedAgent
    SpecializedAgent --> Output["专家意见<br/>置信度评分<br/>建议清单"]
```

**核心能力**：

| 能力 | 说明 |
|------|------|
| 领域分析 | 基于专业领域提供深度分析 |
| 置信度评估 | 对分析结果给出置信度评分 |
| 专业建议 | 提供领域相关的优化建议 |

### 2. NegotiatingAgent 谈判 Agent

谈判 Agent 支持多轮协商，能够提出提案、反馈和修改直至达成共识。

```mermaid
sequenceDiagram
    participant A as Negotiator A
    participant B as Negotiator B
    participant C as Negotiator C

    Note over A,C: 资源分配谈判

    A->>B: 提案 P1: A=40%, B=35%, C=25%
    B->>A: 反对，提出 P2: A=33%, B=33%, C=34%
    C->>A: 支持 P2

    A->>B: 接受 P2 方案
    B->>C: 确认达成共识

    Note over A,C: 共识达成: 均分方案
```

**谈判流程状态图**：

```mermaid
stateDiagram-v2
    [*] --> Proposing: 发起提案

    Proposing --> Reviewing: 等待审议
    Reviewing --> Accepted: 多数同意
    Reviewing --> CounterProposed: 提出反提案
    Reviewing --> Rejected: 多数反对

    CounterProposed --> Reviewing: 重新审议
    Accepted --> [*]: 达成共识
    Rejected --> Proposing: 重新提案
    Rejected --> [*]: 谈判失败
```

### 3. 投票机制

多个 Agent 通过投票进行民主决策。

```mermaid
graph TB
    subgraph Proposal["技术提案"]
        Title["采用微服务架构"]
        Pros["优点:<br/>可扩展性强<br/>独立部署<br/>技术栈灵活"]
        Cons["缺点:<br/>运维复杂<br/>网络开销<br/>一致性挑战"]
    end

    subgraph Voters["投票者"]
        V1["安全专家"]
        V2["性能专家"]
        V3["架构专家"]
    end

    subgraph Voting["投票过程"]
        direction TB
        Vote1["安全专家: 同意"]
        Vote2["性能专家: 反对"]
        Vote3["架构专家: 同意"]
    end

    subgraph Result["投票结果"]
        Count["2/3 同意 = 66.7%"]
        Decision["提案通过 (>60%)"]
    end

    Proposal --> Voters
    V1 --> Vote1
    V2 --> Vote2
    V3 --> Vote3
    Voting --> Count --> Decision

    style Vote1 fill:#c8e6c9
    style Vote2 fill:#ffcdd2
    style Vote3 fill:#c8e6c9
    style Decision fill:#c8e6c9
```

## 执行流程

### 专家评审流程

```mermaid
sequenceDiagram
    participant Client as 客户端
    participant MAS as MultiAgentSystem
    participant SE as 安全专家
    participant PE as 性能专家
    participant AE as 架构专家

    Client->>MAS: 提交评审任务

    par 并行评审
        MAS->>SE: Collaborate(task)
        SE->>SE: 安全性分析
        SE-->>MAS: 安全评审报告
    and
        MAS->>PE: Collaborate(task)
        PE->>PE: 性能瓶颈分析
        PE-->>MAS: 性能评审报告
    and
        MAS->>AE: Collaborate(task)
        AE->>AE: 架构合理性分析
        AE-->>MAS: 架构评审报告
    end

    MAS->>MAS: 汇总专家意见
    MAS-->>Client: 综合评审结果
```

### 谈判协商流程

```mermaid
flowchart TD
    Start["开始谈判"] --> Init["初始化谈判参与者"]
    Init --> Propose["发起资源分配提案"]

    Propose --> Review{"审议提案"}
    Review --> |"多数同意"| Accept["接受提案"]
    Review --> |"多数反对"| Counter["提出反提案"]
    Review --> |"僵局"| Mediate["协调者介入"]

    Counter --> Review
    Mediate --> NewPropose["生成折中方案"]
    NewPropose --> Review

    Accept --> Consensus["达成共识"]
    Consensus --> End["结束谈判"]

    style Accept fill:#c8e6c9
    style Consensus fill:#c8e6c9
```

### 投票决策流程

```mermaid
flowchart TD
    Start["发起投票"] --> Collect["收集所有投票"]

    Collect --> Count["统计投票结果"]
    Count --> Check{"检查通过阈值"}

    Check --> |"≥60%"| Pass["提案通过"]
    Check --> |"<60%"| Fail["提案未通过"]

    Pass --> Execute["执行决议"]
    Fail --> Revise["修改提案"]
    Revise --> Start

    style Pass fill:#c8e6c9
    style Fail fill:#ffcdd2
```

## 使用方法

### 运行示例

```bash
cd examples/multiagent/04-specialized-agents
go run main.go
```

### 预期输出

```text
╔════════════════════════════════════════════════════════════════╗
║          专业化 Agent 示例                                     ║
║   展示 SpecializedAgent 和 NegotiatingAgent 的高级用法         ║
╚════════════════════════════════════════════════════════════════╝

【演示 1】专业化 Agent (SpecializedAgent)
✓ 创建专家: security-expert      专业: 安全分析
✓ 创建专家: performance-expert   专业: 性能优化
✓ 创建专家: architecture-expert  专业: 架构设计
✓ 创建专家: ml-expert            专业: 机器学习
✓ 创建专家: database-expert      专业: 数据库优化

专家评审结果:
【security-expert】
  专业领域: 安全分析
  置信度: 85%

【演示 2】谈判 Agent (NegotiatingAgent)
✓ 创建谈判 Agent: negotiator-A
✓ 创建谈判 Agent: negotiator-B
✓ 创建谈判 Agent: negotiator-C

提出的分配方案:
方案 P1: A=40%, B=35%, C=25%
方案 P2: A=33%, B=33%, C=34%
方案 P3: A=50%, B=30%, C=20%

✓ 谈判完成，状态: completed

【演示 3】专业化 Agent 混合协作
✓ 混合协作任务完成

【演示 4】Agent 投票决策
提案: 采用微服务架构
投票结果: 2/3 同意 (66.7%)
✓ 提案通过！
```

### 关键代码

#### 创建 SpecializedAgent

```go
agent := multiagent.NewSpecializedAgent(
    "security-expert",
    "安全分析",
    system,
)
system.RegisterAgent("security-expert", agent)
```

#### 创建 NegotiatingAgent

```go
agent := multiagent.NewNegotiatingAgent("negotiator-A", system)
system.RegisterAgent("negotiator-A", agent)
```

#### 发起投票

```go
proposal := map[string]interface{}{
    "title":       "采用微服务架构",
    "description": "将单体应用拆分为微服务架构",
    "pros":        []string{"可扩展性强", "独立部署"},
    "cons":        []string{"运维复杂度高", "网络开销大"},
}

vote, err := agent.Vote(ctx, proposal)
```

## 应用场景

### SpecializedAgent 应用场景

```mermaid
mindmap
    root((领域专家))
        安全
            漏洞扫描
            合规检查
            风险评估
        性能
            瓶颈分析
            优化建议
            负载测试
        架构
            设计评审
            技术选型
            可扩展性
        数据
            数据建模
            查询优化
            存储方案
```

### NegotiatingAgent 应用场景

| 场景 | 说明 | 示例 |
|------|------|------|
| 资源分配 | 多方协商资源分配方案 | CPU、内存、带宽分配 |
| 价格谈判 | 买卖双方价格协商 | 服务定价、合同谈判 |
| 任务分配 | 团队间任务协商 | 项目工作量分配 |
| 冲突解决 | 多方利益冲突调解 | 优先级争议、资源竞争 |

### 投票机制应用场景

| 场景 | 说明 | 通过阈值 |
|------|------|---------|
| 技术方案选择 | 多位专家投票选择最佳方案 | 60% |
| 代码审核 | 多位审核者投票决定是否合并 | 100% |
| 功能上线 | 多方投票决定是否发布 | 75% |
| 紧急变更 | 快速投票决定紧急修复 | 50% |

## 扩展阅读

- [01-basic-system](../01-basic-system/) - 基础系统示例
- [02-collaboration-types](../02-collaboration-types/) - 协作类型示例
- [05-llm-collaborative-agents](../05-llm-collaborative-agents/) - LLM 协作示例
