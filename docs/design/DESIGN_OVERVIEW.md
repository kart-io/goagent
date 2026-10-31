# GoAgent 设计概述

本文概括当前执行内核的设计取舍。包结构见 [架构概述](../architecture/ARCHITECTURE.md)，演进过程见 [Harness 核心化设计记录](HARNESS_CORE_DESIGN.md) 第 16–21 节。

## 设计原则

1. **单一执行源**：`ConfigurableAgent.AsRunnable()` 的 `Invoke`/`Stream` 与 `ExecuteStream` 共享同一执行源；`core.StreamRunnable` 只在执行源和完成回调都成功后发出最终块，错误之后不会再出现成功终态。
2. **循环只定义扩展点**：原生工具循环本身固定，新行为挂到 `WithPreStep`、`WithRequestHook`、`WithRequestError`、`WithTurnStopping`、`WithPromptSections`/`WithPromptContexts` 以及工具管线 `tools.Pipeline` 上。上下文改写只剩 PreStep（`WithOffload` 也装为 PreStep），模型请求失败的处置只剩 RequestError（重试用 `builder.RetryTransient`）。
3. **显式持久**：`core/durable` 不序列化 Go 闭包、不恢复任意栈帧，只记录有名字的子操作（模型回合、工具调用、循环扩展点）的结果与输入摘要。恢复即以相同执行键重新执行并回放已完成的操作；意图或尝试策略改变时以错误结束而不是静默沿用旧结果。
4. **模型可见即已记录**：配置 `WithSessionLog` 后，每次模型请求都能由 `system + session.DeriveMessages + 运行上下文` 重建；对历史的任何改写记为 `history/rewrite`。
5. **观察与拦截分离**：`core/observe.Observer` 只读，事件不是检查点或账本；中间件、循环钩子与 Pipeline 可改变行为，错误按各自契约中止或交还模型。
6. **宿主拥有资源**：`execution.Scope`、`budget.Controller`、`durable.Store`、`session.Store` 都由宿主创建；框架不创建默认存储，也不关闭宿主资源。
7. **处处有界**：`WithMaxIterations` 限制模型请求次数；根预算限制请求数、预留 Token、工具次数与期限；`DispatchConfig` 限制执行/准入并发、排队数与单次超时；`workflow` 图有最大步数（`ErrMaxSteps`）。

## 分层

```text
宿主（服务 / CLI / 评测）
  │  execution.Scope：RunID、会话、日志、预算、期限、观察者
  ▼
builder.ConfigurableAgent  ←  Handle：Steer / Inject / Followup / Redirect / Interrupt
  每轮：[PreStep] → 组装请求（PromptSections/Contexts）→ [RequestHook]
        → ToolStreamClient.CompleteToolsStream → 失败时 [RequestError]
        → 工具批次 ─► tools.Dispatcher：校验 → Authorize → Pipeline 准入 → durable.Do → OnResult
        → 无工具调用时 [TurnStopping] 决定结束或续跑
  │
  ├─ core/durable：模型回合、工具调用、循环扩展点的持久记录
  ├─ core/session：会话事件日志（DurableStore 让 durable 快照写入同一份日志）
  ├─ core/budget：根预算
  └─ core/observe：只读事件
workflow.Graph / DurableGraph：把 Agent、Dispatcher 或普通函数作为节点编排
harness/env：工具使用的文件系统与命令执行能力接口
```

## 关键机制

| 机制 | 实现位置 | 说明 |
|------|----------|------|
| 原生工具循环 | `builder/tool_events.go` `runToolEvents` | 每轮一次模型请求；最后一轮只提供 `WithReturnDirectTools` 声明的工具；模型请求未提供的工具即报错 |
| 旧工具选择路径 | `builder/events.go` `runEvents` | 客户端只实现 `ToolCallingClient` 时使用；不支持会话日志、持久日志与根预算 |
| 持久回合 | `builder/durable_turn.go`、`builder/model_call.go` | 操作 ID `model/turn/N`；RequestError 的原样重试留在同一条回合记录内 |
| 工具执行 | `tools/dispatcher.go`、`tools/pipeline.go` | 准入阶段（Pre、审批、Guard）在操作日志之外；Around、工具与 Post 在超时之内 |
| 结果卸载 | `builder/contextpolicy` `Offload` | 超长工具结果换成引用，读取工具 `read_context_result` 经 Dispatcher 注册 |
| 运行中输入 | `builder/handle.go` | `Start` 返回 `Handle`；插话按步领取，持久运行中也记入日志（`loop/inbox/N`） |

## 并发

- 同一个 `ConfigurableAgent` 同一时间只执行一次，后到的调用排队，排队时间计入 `WithTimeout`。不同实例互不影响。
- Builder 是可变对象，不要并发配置。
- `WithParallelToolCalls(n)`：同一轮多个调用且每个工具实现 `interfaces.ConcurrentCallTool`（如 `Dispatcher.Tools()`）时并发执行；结果、`tool_end` 事件与 `OnResult` 仍按模型给出的顺序。
- `Dispatcher` 的工具须支持并发调用，否则设 `MaxConcurrency=1`；审批等待不占执行槽位。
- 编译后的 `workflow` 图可并发运行，前提是节点与调用方状态支持并发。

## 状态与检查点

`core/state.AgentState` 是默认状态类型；`WithCheckpointer` 配置的 `core/checkpoint.Checkpointer`（如 `InMemorySaver`）以 SessionID 为键，在 `Initialize` 时恢复状态，`WithAutoSaveEnabled`（默认开启）时每次成功结束后保存。它与 `core/durable` 无关：检查点保存状态，持久日志保存操作结果。

## 扩展点

各扩展点是否改变执行、错误与超时语义、恢复语义见 [扩展契约](../guides/EXTENSION_CONTRACT.md)；工具管线见 [工具中间件](../guides/TOOL_MIDDLEWARE.md) 与 [执行治理](../guides/EXECUTION_GOVERNANCE.md)；上下文治理见 [上下文策略](../guides/CONTEXT_POLICY.md)。

## 相关文档

- [流程图](FLOW_DIAGRAMS.md)
- [时序图](SEQUENCE_DIAGRAMS.md)
- [组件关系图](../architecture/COMPONENT_DIAGRAM.md)
- [执行契约](../guides/EXECUTION_CONTRACT.md)
