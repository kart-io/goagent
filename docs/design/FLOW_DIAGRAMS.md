# GoAgent 流程图

流程按当前源码整理，节点名称对应实际函数；细节以源码为准。

## 1. 执行入口：ExecuteStream

`AsRunnable().Invoke/Stream` 都经过此入口（`builder/events.go`）。

```mermaid
flowchart TB
    IN([ExecuteStream ctx, input]) --> BUDGET{validateModelBudget}
    BUDGET -->|预算已耗尽| ERR([返回错误])
    BUDGET --> SC{客户端实现 llm.StreamClient?}
    SC -->|否| ERR
    SC -->|是| TC{配置了工具?}
    TC -->|是，且既不是 ToolStreamClient<br/>也不支持 GenerateWithTools| ERR
    TC --> GATE[beginExecution：WithTimeout、budget.Bind、<br/>等待本实例执行权]
    TC -->|否| GATE
    GATE --> DUR[scopeDurableExecution：<br/>有日志时检查工具支持持久调用、DurableScope 非空，<br/>执行键派生为 key/agent/scope]
    DUR --> GO[启动 goroutine，返回事件通道]
    GO --> CHAIN[execute：core/middleware 链 → runEvents]
    CHAIN --> RET{Retriever?}
    RET -->|是| SRC[检索并发出 sources 事件]
    RET -->|否| PATH
    SRC --> PATH{ToolStreamClient 且有工具?}
    PATH -->|是| LOOP[原生工具循环 runToolEvents]
    PATH -->|否，有 GenerateWithTools| LEGACY[旧工具选择循环 + model/answer/0]
    PATH -->|否，无工具| PLAIN[readPlainModelTurn model/answer/0]
    LOOP --> FIN[finishEvents：usage 事件、写记忆、<br/>自动保存检查点]
    LEGACY --> FIN
    PLAIN --> FIN
    FIN --> DONE([done 事件])
    CHAIN -->|错误| ERRE([error 事件，带出错工具名])
```

会话日志（`WithSessionLog`）只支持原生工具循环；根预算与持久日志也不支持旧工具选择路径。

## 2. 原生工具循环

`runToolEvents`（`builder/tool_events.go`）每轮一次模型请求，`WithMaxIterations` 包含最终答案那一次。

```mermaid
flowchart TB
    S([开始一轮 round]) --> INBOX[领取 Handle 收件箱输入<br/>loop/inbox/N]
    INBOX --> FINAL{最后一轮?}
    FINAL -->|是| ONLYDIRECT[只提供 ReturnDirectTools]
    FINAL -->|否| ALL[提供全部工具]
    ONLYDIRECT --> PRE
    ALL --> PRE[PreStep 钩子 loop/pre/N<br/>+ Offload 引用重建]
    PRE -->|Halt| ANSWER
    PRE --> SYNC[会话日志同步]
    SYNC --> REQ[requestTurn：PromptContexts、RequestHook、<br/>readJournaledTurn model/turn/N]
    REQ -->|失败| RE{RequestError 钩子}
    RE -->|重试/替换| REQ
    RE -->|失败| ERR([错误])
    REQ -->|Handle.Redirect| S
    REQ --> CALLS{有工具调用?}
    CALLS -->|否，空回答且可重问| S
    CALLS -->|否| STOP{Steer 唤醒或<br/>TurnStopping 续跑?}
    STOP -->|是| S
    STOP -->|否| ANSWER([finish：结束本轮])
    CALLS -->|是| CHECK[校验调用 ID、工具名已提供、参数为 JSON 对象]
    CHECK --> EXEC{WithParallelToolCalls 且<br/>工具支持并发?}
    EXEC -->|是| PAR[invokeConcurrently]
    EXEC -->|否| SEQ[逐个 invokeToolCall]
    PAR --> RES[按模型顺序追加 tool 消息，发出 tool_end]
    SEQ --> RES
    RES -->|WaitError| WAIT([WaitingError：等待宿主继续])
    RES -->|单个直接返回工具成功| ANSWER
    RES --> ADD[AdditionalContext 追加为 user 消息]
    ADD --> S
```

额度用尽时返回“模型调用达到 MaxIterations 上限”的错误，最近一次工具失败时附上其信息。

## 3. Dispatcher 工具调用

`Dispatcher.invoke`（`tools/dispatcher.go`）。Builder 经 `Dispatcher.Tools()` 进入，`workflow` 节点可经 `Dispatcher.Invoke` 或 `AsRunnable()` 进入。

```mermaid
flowchart TB
    IN([ToolRequest]) --> LOOK{已注册?}
    LOOK -->|否| ERR([错误])
    LOOK --> KEY[派生执行键 key/tool/CallID，开始 tool 观察]
    KEY --> AS[获取准入槽位]
    AS --> VAL{Schema 与 ValidatableTool 校验}
    VAL -->|不合法且 ReturnArgumentErrors| ARG[参数错误结果，交还模型]
    VAL -->|不合法| ERR
    VAL --> AUTH{Authorize}
    ARG --> AUTH
    AUTH -->|错误| ERR
    AUTH --> DONEQ{日志中已完成?}
    DONEQ -->|是| DO
    DONEQ -->|否| ADMIT[Pipeline 准入：PreHook → 审批 → Guard]
    ADMIT -->|拒绝| REJ[Rejected 结果]
    ADMIT -->|允许| SLOT[获取执行槽位]
    REJ --> DO
    SLOT --> DO{有 CallID?}
    DO -->|否| RUN[直接执行]
    DO -->|是| JD[durable.Do tool/CallID]
    JD --> RUN2[Around → 工具 Invoke → PostHook，计入 Timeout]
    RUN --> RUN2
    RUN2 --> RESULT[OnResult 回调<br/>同轮并发时推迟到整轮结束按序触发]
    RESULT --> OUT([ToolOutput])
```

## 4. 持久操作 durable.Do

`core/durable/operation.go`。ctx 上没有日志或执行键时直接执行，不留记录。

```mermaid
flowchart TB
    IN([Do ctx, id, intent, policy, run]) --> J{有 Journal 与执行键?}
    J -->|否| RUN0[执行 run，不记录]
    J -->|是| LOCK[Store.Lock 操作键]
    LOCK --> LOAD{已有记录?}
    LOAD -->|否| NEW[新记录]
    LOAD -->|是| SAME{输入摘要与 MaxAttempts 一致?}
    SAME -->|否| CHG([错误：intent or attempt policy changed])
    SAME -->|是| ST{状态}
    ST -->|completed| REPLAY([返回记录 Replayed=true])
    ST -->|pending 且有 Reconcile| REC{Reconcile 找到效果?}
    REC -->|是| FILL([补记 completed 并返回])
    REC -->|否| RETRY
    ST -->|pending / failed / waiting| RETRY{waiting：须 Resumable；<br/>pending：Resumable 或 RetryFailed，且尝试未满；<br/>failed：RetryFailed 且尝试未满}
    RETRY -->|否| OPERR([OperationError])
    RETRY -->|是| PEND
    NEW --> PEND[记为 pending，保存]
    PEND --> RUN[执行 run]
    RUN --> SAVE([保存 completed / failed / waiting])
```

## 5. 宿主装配与恢复

```mermaid
flowchart LR
    CFG[execution.Config<br/>RunID、SessionID、Journal、Budget、Timeout、Observer] --> NS[execution.NewScope]
    NS --> BIND[scope.Bind requestCtx]
    BIND --> CB[execution.ConfigureBuilder b, scope, agentID]
    CB --> BC[b.BuildContext runCtx]
    BC --> INV["agent.AsRunnable().Invoke/Stream"]
    INV -->|进程崩溃或 WaitingError| BIND
```

恢复时用相同 RunID、agentID、预算版本与配置重新执行这一链路，已完成的模型回合与工具调用被回放。见 [宿主接入](../guides/HOST_INTEGRATION.md)。

## 相关文档

- [时序图](SEQUENCE_DIAGRAMS.md)
- [设计概述](DESIGN_OVERVIEW.md)
- [架构概述](../architecture/ARCHITECTURE.md)
