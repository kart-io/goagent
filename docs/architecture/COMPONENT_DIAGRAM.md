# GoAgent 组件关系

当前模块职责见 [架构概述](ARCHITECTURE.md)，API 见 [核心参考](../api/CORE_API.md)。图中仅包含当前仓库或明确独立的模块，不将旧 agents、multiagent、distributed、planning、retrieval 目录当成当前内置实现。

```mermaid
flowchart LR
    App[宿主] --> Builder[builder]
    App --> Scope[execution.Scope]
    App --> Graph[workflow.Graph]
    Builder --> Run[core.Runnable]
    Graph --> Run
    Builder --> LLM[llm Provider]
    Builder --> Dispatcher[tools.Dispatcher]
    Dispatcher --> Tool[interfaces.Tool]
    Tool --> Child[Runnable / 子Agent / 子图]
    Graph --> Durable[core/durable]
    Dispatcher --> Durable
    Builder --> Session[core/session]
    Builder --> Budget[core/budget]
    App --> Observe[core/observe]
    Harness[独立goagent-harness] --> Builder
    Harness --> Env[harness/env]
```

以上是集成关系，不表示所有箭头都为直接 Go import。精确导入约束见 [分层检查](IMPORT_LAYERING.md)。核心不依赖独立 Harness；业务示例位于 goagent-examples。

```mermaid
sequenceDiagram
    participant H as 宿主
    participant A as Builder Agent
    participant M as 原生工具流模型
    participant D as Dispatcher
    participant T as 业务工具
    H->>A: AsRunnable().Invoke(ctx, input)
    A->>M: 请求（预算准入）
    M-->>A: 工具调用或答案
    opt 选择工具
        A->>D: 工具名、参数、CallID
        D->>T: 校验并执行或回放
        T-->>D: ToolOutput
        D-->>A: 结构化结果
        A->>M: 携结果继续
        M-->>A: 答案
    end
    A-->>H: builder.AgentOutput 或错误/等待
```

模型循环不是每轮必定调用工具。真正的业务完成须结合最终状态判断，审批等待和预览不能作为完成证据。
