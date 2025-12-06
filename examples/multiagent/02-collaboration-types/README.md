# 02-collaboration-types 协作类型示例

本示例演示 MultiAgentSystem 支持的五种协作模式：并行（Parallel）、顺序（Sequential）、分层（Hierarchical）、共识（Consensus）和管道（Pipeline）。

## 目录

- [架构设计](#架构设计)
- [五种协作模式](#五种协作模式)
- [执行流程](#执行流程)
- [使用方法](#使用方法)
- [适用场景](#适用场景)

## 架构设计

### 协作类型总览

```mermaid
graph TB
    subgraph CollaborationTypes["协作类型"]
        direction LR
        Parallel["Parallel<br/>并行协作"]
        Sequential["Sequential<br/>顺序协作"]
        Hierarchical["Hierarchical<br/>分层协作"]
        Consensus["Consensus<br/>共识协作"]
        Pipeline["Pipeline<br/>管道协作"]
    end

    subgraph Characteristics["特点"]
        P_Char["独立执行<br/>结果合并"]
        S_Char["链式处理<br/>顺序依赖"]
        H_Char["层级分明<br/>职责清晰"]
        C_Char["投票决策<br/>达成共识"]
        Pi_Char["流式处理<br/>阶段划分"]
    end

    Parallel --> P_Char
    Sequential --> S_Char
    Hierarchical --> H_Char
    Consensus --> C_Char
    Pipeline --> Pi_Char
```

### 系统组件关系

```mermaid
classDiagram
    class CollaborativeTask {
        +ID string
        +Name string
        +Description string
        +Type CollaborationType
        +Input interface
        +Assignments map~string~Assignment
    }

    class CollaborationType {
        <<enumeration>>
        Parallel
        Sequential
        Hierarchical
        Consensus
        Pipeline
    }

    class Assignment {
        +AgentID string
        +Role Role
        +Subtask interface
        +Result interface
        +Status TaskStatus
        +StartTime Time
        +EndTime Time
    }

    CollaborativeTask --> CollaborationType
    CollaborativeTask "1" --> "*" Assignment
```

## 五种协作模式

### 1. Parallel 并行协作

多个 Agent 同时处理独立的子任务，最后合并结果。

```mermaid
graph LR
    subgraph Input["输入"]
        Task["协作任务"]
    end

    subgraph Parallel["并行执行"]
        A1["Agent 1"]
        A2["Agent 2"]
        A3["Agent 3"]
        A4["Agent 4"]
    end

    subgraph Output["输出"]
        Merge["结果合并"]
    end

    Task --> A1
    Task --> A2
    Task --> A3
    Task --> A4

    A1 --> Merge
    A2 --> Merge
    A3 --> Merge
    A4 --> Merge
```

**执行时序**：

```mermaid
sequenceDiagram
    participant MAS as MultiAgentSystem
    participant A1 as Agent 1
    participant A2 as Agent 2
    participant A3 as Agent 3

    MAS->>MAS: 分发任务

    par 并行执行
        MAS->>A1: Collaborate(task)
        A1-->>MAS: Assignment 1
    and
        MAS->>A2: Collaborate(task)
        A2-->>MAS: Assignment 2
    and
        MAS->>A3: Collaborate(task)
        A3-->>MAS: Assignment 3
    end

    MAS->>MAS: 合并所有结果
    Note over MAS: 返回 TaskResult
```

### 2. Sequential 顺序协作

Agent 按顺序依次处理，前一个的输出作为后一个的输入。

```mermaid
graph LR
    subgraph Sequential["顺序执行"]
        A1["Agent 1<br/>采集"] --> A2["Agent 2<br/>处理"]
        A2 --> A3["Agent 3<br/>分析"]
        A3 --> A4["Agent 4<br/>报告"]
    end

    Input["输入数据"] --> A1
    A4 --> Output["最终结果"]
```

**执行时序**：

```mermaid
sequenceDiagram
    participant MAS as MultiAgentSystem
    participant Collector as 数据采集
    participant Processor as 数据处理
    participant Analyzer as 数据分析
    participant Reporter as 报告生成

    MAS->>Collector: Collaborate(task)
    Collector-->>MAS: 采集结果

    MAS->>Processor: Collaborate(task + 采集结果)
    Processor-->>MAS: 处理结果

    MAS->>Analyzer: Collaborate(task + 处理结果)
    Analyzer-->>MAS: 分析结果

    MAS->>Reporter: Collaborate(task + 分析结果)
    Reporter-->>MAS: 最终报告
```

### 3. Hierarchical 分层协作

领导者分配任务，工作者执行，验证者检验结果。

```mermaid
graph TB
    subgraph Leadership["领导层"]
        Leader["Leader<br/>领导者"]
    end

    subgraph Coordination["协调层"]
        Coord["Coordinator<br/>协调者"]
    end

    subgraph Execution["执行层"]
        W1["Worker 1"]
        W2["Worker 2"]
        W3["Worker 3"]
    end

    subgraph Validation["验证层"]
        Val["Validator<br/>验证者"]
    end

    Leader --> |"制定策略"| Coord
    Coord --> |"分配任务"| W1
    Coord --> |"分配任务"| W2
    Coord --> |"分配任务"| W3
    W1 --> |"提交结果"| Val
    W2 --> |"提交结果"| Val
    W3 --> |"提交结果"| Val
    Val --> |"验证报告"| Leader
```

**执行时序**：

```mermaid
sequenceDiagram
    participant Leader as 领导者
    participant Coord as 协调者
    participant W1 as 工作者1
    participant W2 as 工作者2
    participant Val as 验证者

    Leader->>Leader: 制定执行策略
    Leader->>Coord: 下发任务计划

    Coord->>W1: 分配子任务1
    Coord->>W2: 分配子任务2

    par 并行执行
        W1-->>Coord: 完成子任务1
    and
        W2-->>Coord: 完成子任务2
    end

    Coord->>Val: 提交执行结果
    Val->>Val: 验证结果质量
    Val-->>Leader: 验证报告
```

### 4. Consensus 共识协作

多个 Agent 对某个决策进行投票，达成共识。

```mermaid
graph TB
    subgraph Proposal["提案"]
        P["方案提议"]
    end

    subgraph Voting["投票过程"]
        V1["Agent 1<br/>投票"]
        V2["Agent 2<br/>投票"]
        V3["Agent 3<br/>投票"]
        V4["Agent 4<br/>投票"]
        V5["Agent 5<br/>投票"]
    end

    subgraph Decision["决策"]
        Count["统计票数"]
        Result["最终决定"]
    end

    P --> V1
    P --> V2
    P --> V3
    P --> V4
    P --> V5

    V1 --> |"同意"| Count
    V2 --> |"同意"| Count
    V3 --> |"反对"| Count
    V4 --> |"同意"| Count
    V5 --> |"同意"| Count

    Count --> |"4/5 > 60%"| Result
```

**执行时序**：

```mermaid
sequenceDiagram
    participant MAS as MultiAgentSystem
    participant A1 as Agent 1
    participant A2 as Agent 2
    participant A3 as Agent 3

    MAS->>MAS: 发起投票提案
    Note over MAS: 提案: 采用微服务架构<br/>通过阈值: 60%

    par 并行投票
        MAS->>A1: Vote(proposal)
        A1-->>MAS: 同意
    and
        MAS->>A2: Vote(proposal)
        A2-->>MAS: 同意
    and
        MAS->>A3: Vote(proposal)
        A3-->>MAS: 反对
    end

    MAS->>MAS: 统计投票结果
    Note over MAS: 2/3 = 66.7% > 60%<br/>提案通过
```

### 5. Pipeline 管道协作

数据流经多个处理阶段，每个阶段由专门的 Agent 处理。

```mermaid
graph LR
    subgraph Pipeline["ETL 管道"]
        E["Extract<br/>数据抽取"]
        T["Transform<br/>数据转换"]
        L["Load<br/>数据加载"]
    end

    Source["数据源"] --> E
    E --> |"原始数据"| T
    T --> |"清洗数据"| L
    L --> Target["目标存储"]

    style E fill:#e1f5fe
    style T fill:#fff3e0
    style L fill:#e8f5e9
```

**执行时序**：

```mermaid
sequenceDiagram
    participant MAS as MultiAgentSystem
    participant Extract as Extract Agent
    participant Transform as Transform Agent
    participant Load as Load Agent

    Note over MAS: Pipeline 阶段配置

    MAS->>Extract: 执行阶段1 - Extract
    Extract->>Extract: SELECT * FROM users
    Extract-->>MAS: 原始数据

    MAS->>Transform: 执行阶段2 - Transform
    Transform->>Transform: clean, normalize, enrich
    Transform-->>MAS: 转换后数据

    MAS->>Load: 执行阶段3 - Load
    Load->>Load: 批量写入 data_warehouse
    Load-->>MAS: 加载完成

    Note over MAS: Pipeline 执行完成
```

## 执行流程

### 任务类型路由

```mermaid
flowchart TD
    Start["接收任务"] --> Check{"检查任务类型"}

    Check --> |"Parallel"| P["executeParallelTask()"]
    Check --> |"Sequential"| S["executeSequentialTask()"]
    Check --> |"Hierarchical"| H["executeHierarchicalTask()"]
    Check --> |"Consensus"| C["executeConsensusTask()"]
    Check --> |"Pipeline"| Pi["executePipelineTask()"]

    P --> Merge["合并结果"]
    S --> Chain["链式传递"]
    H --> Layer["层级汇报"]
    C --> Vote["投票统计"]
    Pi --> Stage["阶段推进"]

    Merge --> End["返回结果"]
    Chain --> End
    Layer --> End
    Vote --> End
    Stage --> End
```

## 使用方法

### 运行示例

```bash
cd examples/multiagent/02-collaboration-types
go run main.go
```

### 预期输出

```text
╔════════════════════════════════════════════════════════════════╗
║          多智能体协作类型示例                                  ║
║   展示五种协作模式：并行、顺序、分层、共识、管道               ║
╚════════════════════════════════════════════════════════════════╝

【准备工作】注册协作 Agent
✓ 注册 Agent: data-collector       角色: worker
✓ 注册 Agent: data-processor       角色: worker
...

╭────────────────────────────────────────╮
│  协作类型 1: Parallel（并行协作）      │
╰────────────────────────────────────────╯
✓ 执行状态: completed
✓ 参与 Agent 数: 8

╭────────────────────────────────────────╮
│  协作类型 2: Sequential（顺序协作）    │
╰────────────────────────────────────────╯
✓ 执行状态: completed

... (其他协作类型)
```

## 适用场景

| 协作类型 | 适用场景 | 示例 |
|---------|---------|------|
| **Parallel** | 独立任务并行处理 | 数据分片处理、批量文件处理 |
| **Sequential** | 有依赖的任务链 | 数据流水线、工作流引擎 |
| **Hierarchical** | 层级分明的项目 | 项目管理、组织协作 |
| **Consensus** | 需要投票决策 | 方案选择、审批流程 |
| **Pipeline** | 流式数据处理 | ETL、日志处理、消息处理 |

### 选择决策树

```mermaid
flowchart TD
    Start["选择协作类型"] --> Q1{"任务间有依赖吗?"}

    Q1 --> |"无依赖"| Q2{"需要投票决策吗?"}
    Q1 --> |"有依赖"| Q3{"是流式数据吗?"}

    Q2 --> |"是"| Consensus["Consensus<br/>共识协作"]
    Q2 --> |"否"| Parallel["Parallel<br/>并行协作"]

    Q3 --> |"是"| Pipeline["Pipeline<br/>管道协作"]
    Q3 --> |"否"| Q4{"需要层级管理吗?"}

    Q4 --> |"是"| Hierarchical["Hierarchical<br/>分层协作"]
    Q4 --> |"否"| Sequential["Sequential<br/>顺序协作"]
```

## 扩展阅读

- [01-basic-system](../01-basic-system/) - 基础系统示例
- [03-team-management](../03-team-management/) - 团队管理示例
- [05-llm-collaborative-agents](../05-llm-collaborative-agents/) - LLM 协作示例
