# GoAgent 时序图

参与者均为当前源码中的类型；流程判定细节见 [流程图](FLOW_DIAGRAMS.md)。

## 1. 原生工具循环中的一次工具调用

```mermaid
sequenceDiagram
    participant Host as 宿主
    participant R as core.StreamRunnable
    participant A as builder.ConfigurableAgent
    participant M as llm.ToolStreamClient
    participant D as tools.Dispatcher
    participant T as interfaces.Tool

    Host->>R: Invoke(ctx, input)
    R->>A: ExecuteStream(ctx, input)
    A->>A: beginExecution / scopeDurableExecution
    A->>A: core/middleware 链 → runEvents → runToolEvents
    A->>M: CompleteToolsStream(ctx, req, tools)  [model/turn/0]
    M-->>A: StreamChunk（text 增量…，Done 块带 ToolCalls）
    A-->>R: AgentEvent step/tool_start
    A->>D: dispatchedTool.Invoke(ctx, ToolInput{CallID})
    D->>D: Schema 校验 → Authorize → Pipeline 准入
    D->>T: Invoke(ctx, input)  [durable.Do tool/turn/0/call/<id>]
    T-->>D: ToolOutput
    D->>D: OnResult
    D-->>A: ToolOutput
    A-->>R: AgentEvent step/tool_end
    A->>M: CompleteToolsStream(ctx, req+tool 消息, tools)  [model/turn/1]
    M-->>A: StreamChunk（最终文本，无工具调用）
    A->>A: TurnStopping？ → finishEvents（usage、写记忆、保存检查点）
    A-->>R: AgentEvent done(AgentOutput)
    R-->>Host: *AgentOutput
```

未绑定日志时 `durable.Do` 直接执行，工具调用的 `CallID` 为空。

## 2. 持久运行的崩溃恢复

宿主以相同执行键与日志重新执行；已完成的操作直接回放，不再调用模型或工具。

```mermaid
sequenceDiagram
    participant Host as 宿主
    participant A as ConfigurableAgent
    participant J as durable.Store（如 session.DurableStore）
    participant M as ToolStreamClient
    participant T as 工具

    Note over Host,T: 第一次执行
    Host->>A: Invoke(WithJournal(WithExecutionKey(ctx, key), store), input)
    A->>J: model/turn/0 pending → 调用模型 → completed
    A->>J: tool/turn/0/call/c1 pending
    A->>T: Invoke
    Note over A,T: 进程在工具返回前退出，记录停在 pending

    Note over Host,T: 恢复
    Host->>A: 以同一 key 与 store 重新 Invoke
    A->>J: model/turn/0：completed，回放（不调用模型）
    A->>J: tool/turn/0/call/c1：pending
    alt 工具实现 ReconcilableTool 且查到已生效
        A->>T: Reconcile → 补记 completed
    else 在 MaxAttempts 内
        A->>T: 再次 Invoke（目标端需按执行键幂等）
    end
    A->>J: model/turn/1 → 调用模型 → completed
    A-->>Host: AgentOutput
```

`WithSessionLog` 时会话日志续写同一轮，消息不重复；输入或尝试策略改变时以 `operation intent or attempt policy changed` 失败。

## 3. Pipeline 审批

```mermaid
sequenceDiagram
    participant D as Dispatcher
    participant P as tools.Pipeline
    participant Pre as PreHook
    participant Ap as Approver（宿主实现）
    participant G as Guard
    participant T as 工具

    D->>P: 准入（准入槽位，不计入 Timeout）
    P->>Pre: PreHook(ctx, call)
    Pre-->>P: PreDecision{Kind: PreAsk, Grant}
    alt 会话已有匹配授权
        P->>P: 直接放行
    else
        P->>Ap: Approve(ctx, ApprovalRequest)（受 ApprovalTimeout 约束）
        Ap-->>P: ApprovalResult
    end
    alt AllowedOnce / AllowedSession
        P->>G: Guard(ctx, call)
        G-->>P: 空字符串（放行）
        P-->>D: 允许
        D->>T: Around → Invoke → PostHook（执行槽位，计入 Timeout）
    else Rejected / Cancelled / Unavailable
        P-->>D: Rejected 结果，交还模型修订
    end
```

`PreDeny` 直接得到 Rejected 结果；`PreStop` 返回 `*tools.StopError` 终止运行；`PreSubstitute` 改为执行声明的替代工具。准入阶段不写日志，回放已完成调用时跳过。

## 4. 运行中插话（Handle）

```mermaid
sequenceDiagram
    participant Host as 宿主
    participant H as builder.Handle
    participant A as ConfigurableAgent
    participant M as ToolStreamClient

    Host->>A: Start(ctx, input)
    A-->>Host: *Handle
    A->>M: model/turn/0
    Host->>H: Steer("改查华东区")
    Note over H: 进入收件箱，下一步开始时领取（loop/inbox/N）
    M-->>A: 工具调用 → 执行工具
    A->>A: 领取收件箱，追加 user 消息
    A->>M: model/turn/1（含插话）
    Host->>H: Redirect("停止，改用另一种方式")
    H->>A: 取消正在进行的模型请求（非持久运行）
    A->>M: 保留已输出文本 + 纠正后重新请求
    M-->>A: 最终答案
    Host->>H: Wait(ctx)
    H-->>Host: *AgentOutput
```

`Start` 只支持原生工具流。`Inject` 随下一次模型请求发送，但不唤醒即将结束的轮次；`Followup` 在本轮结束后作为新一轮输入续接对话；`Interrupt(cause)` 取消运行。没有进行中的模型请求或处于持久运行时，`Redirect` 降级为 Steer。

## 5. 宿主装配

```mermaid
sequenceDiagram
    participant Host as 宿主
    participant S as execution.Scope
    participant B as builder.AgentBuilder
    participant A as ConfigurableAgent

    Host->>S: NewScope(Config{RunID, SessionID, Journal, Budget, Timeout, Observer})
    Host->>S: Bind(requestCtx)
    S-->>Host: runCtx（执行键、日志、预算、观察者、期限）, cancel
    Host->>S: ConfigureBuilder(b, scope, agentID)
    S->>B: 设置会话 ID 与持久作用域
    Host->>B: BuildContext(runCtx)
    B-->>Host: *ConfigurableAgent
    Host->>A: AsRunnable().Invoke(runCtx, input)
    A-->>Host: *AgentOutput
    Host->>S: cancel()
```

## 相关文档

- [流程图](FLOW_DIAGRAMS.md)
- [设计概述](DESIGN_OVERVIEW.md)
- [架构概述](../architecture/ARCHITECTURE.md)
