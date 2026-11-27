# 统一 Agent 运行管理

`github.com/kart-io/goagent/management` 是可嵌入宿主的管理服务，统一 Agent 注册与配置发布、Session 与 Fork、Task 接纳/取消/恢复、Approval 决定及运行观测。使用单个活动 worker 持续处理已持久接纳的命令；执行复用现有 Builder、Graph、durable、budget 和 execution.Scope。

| 包 | 职责 |
| --- | --- |
| `management` | 实体、Store/Runtime/Factory 合约、管理服务、Worker、Outbox、只读运行查询 |
| `management/adapters` | 原生 Agent、Graph 与不可变归档的执行适配 |
| `management/protocol` | A2A/MCP 公共任务身份、回复与状态观测 |
| `management/a2a` | 官方 A2A JSON-RPC/SSE、能力发现、取消与恢复 |
| `management/mcpserver` | Agent 工具、长任务、MCP 人工输入与审批续接 |
| `management/httpapi` | 可挂载的标准库 HTTP Handler 与 [OpenAPI](httpapi/openapi.yaml) |

宿主向 `management.New(Config)` 注入管理 Store、Session Store、HistoryReader、Authorizer 和固定版本 Factory；显式启动 `NewWorker(service).Run(ctx)`，通过 `httpapi.New` 生成 Handler。构造器不建表、不监听端口，也不自动启动后台工作。宿主负责认证、数据库迁移、资源关闭和确认旧进程停止后的 owner 回收。

存储由三个端口注入：`Store`（实体）、`HistoryReader`（业务历史）和 `ReconciliationStore`（核对记录，`Config.Reconciliations` 只需要其中的读接口）。端口的通用规则在本包：`FormatCursor` / `ParseCursor` 是分页游标的格式，`HistoryVisible` / `FilterHistory` 是业务历史的可见规则和分页形状，`NormalizeReconciliation` / `SameReconciliation` 是核对记录的存储形式和幂等判等。`memstore` 提供进程内实现，`SessionHistory` 在任意 `session.Store` 上实现 `HistoryReader`；自己实现端口时用 `management/storetest` 的三组一致性测试验证。

PostgreSQL 参考实现、真实数据库集成测试、退款业务案例位于 [contrib/management](https://github.com/kart-io/goagent-contrib/tree/master/management)。可选 Harness 适配保持独立；核心管理包不依赖 pgx、contrib 或 Harness。

## 请求与执行合约

- 创建和命令使用 Idempotency-Key；内容相同复用原响应，内容不同拒绝。修订更新采用 expected_revision。
- 业务配置版本为不可变记录。发布影响新 Session，已有 Session 和 Fork 继续绑定原版本；这不属于软件旧接口兼容。
- Agent 停用后拒绝新 Session（包括 Fork）和新 Task；重新启用后可重试先前被拒绝的请求。
- 任务取消在生产者实际退出后确认。未知外部效果保持 attention，须提供可信核对证据，不能直接重新执行。
- 每条终态路径（直接取消、认领时取消、取消 Attention、worker 收尾、构建阻塞时取消）都在同一事务内释放会话、作废待决审批，并写恰好一条 `task.<state>` 投递（payload `{"task_id","state"}`）。
- Attention 任务没有已接纳的恢复命令时可直接取消：转为 `canceled`，保留原 `ErrorKind` 与未决 `Operations`（不替宿主定案），审计记录取消人。已有恢复命令时仍由该命令先核对。模型请求中断引起的 attention 没有 `Operations`，无法核对，只能取消；`mcp/remote` 工具内层的远程调用（`mcp/call/N`，只执行一次）中断后同样产生没有 `Operations` 的 attention，只能取消。被取消的轮次在会话日志中被下一轮整轮遮蔽，后续任务的模型看不到它。

## 原生运行时的核对

`adapters.NativeFactory` 声明 `Capabilities.Reconcile`。工具调用经 Dispatcher 写入操作日志，崩溃或结果未知（工具返回 wrap 了 `errors.ErrEffectUnknown` 的错误）后：

- `Inspect` 对本轮中断的工具记录一律返回 `absent`，只统计本任务所在轮次；续跑中的 `durable.Do` 按工具策略处理。默认策略（`MaxAttempts` 3、`RetryInterrupted`）的工具自动重跑；声明 `{MaxAttempts: 1}` 的工具先调用 `tools.ReconcilableTool.Reconcile`，查到结果即定案，查不到才进入 attention，`Task.Operations` 只列出这些调用（`tool/` 前缀，带 `ParentKey`）。
- 宿主核对后以 `ResumeTask{ReconciliationRef}` 续跑。`Reconcile` 要求 `OperationRefs` 恰好覆盖本轮未决的工具调用，否则返回 `ErrManagementConflict`；通过后返回 `absent`，worker 带着结论调用 `Run`。结论对整组统一适用：`applied` 取工具 `Reconcile` 的成功结果，取不到时工具结果为 `operation was verified as applied; output unavailable`；`not_applied` 记为业务拒绝 `operation was not applied; call again if still needed`，不调用工具 `Reconcile`、不重跑。未列入的调用照常按策略执行。需要逐个给结论时，宿主应让写工具串行，使未决调用最多一项。
- 子智能体（RunnableTool、Supervisor 成员）内层工具未决时，外层调用记录保持 pending、不消耗尝试次数；续跑时无论外层是否 `Resumable` 都重新进入，只回放内层已完成的步骤，核对结论直接作用于内层记录。
- 运行报错的任务转为 `failed`：`ErrorKind` 为 `execution`；错误链含 `errors.ErrOperationIntentChanged`（版本、插件或宿主工具定义在本轮开始后变了，已完成的操作无法按原意图回放）时为 `intent_changed`，重试同一任务不会成功，只能重新提交。两种情况的原因都经 `Config.Report("run", err)` 交给宿主，任务记录只留 `ErrorKind`。
- `Task.Operations` 是历史记录：核对成功、任务之后成功或被取消都不清空，表示最近一次进入 attention 时的未决操作。
- 审批等待的 `Wait.Info` 带 `arguments`（工具调用参数），供审批人判断。
- 审批必须经 DecideApproval；决定和续跑命令原子持久化，普通 ResumeTask 不能绕过审批。
- JSON 使用 codec/json，精确保留数字并拒绝重复键；HTTP DTO 使用 Strict 规则，拒绝未知字段、非法 UTF-8 和不匹配大小写的字段名。
- 查询只读取已存在的日志/用量证据，未知消耗保留未知；运行汇总不把查询页大小当作总量限制。

## 删除会话

`Service.DeleteSession(ctx, actor, id)` 删除一个 Session、它的会话日志和会话命令，授权动作为 `session.delete`。删哪些会话由宿主决定，`Prune` 不删会话。以下情况返回 `ErrManagementConflict`：会话还有任务记录（已结束的任务由 `Prune` 在 `TaskRetention` 之后回收）、有待处理命令、或有尚未就绪的分叉；`Config.Sessions` 未实现 `session.Deleter` 时返回 `ErrManagementUnsupported`。

删除分三步：先把会话标为 `deleting`（此后不再接收任务与分叉），再删日志，最后删管理记录。中途失败时会话停在 `deleting`，再次调用即完成。任务的持久快照按执行键存放，不在本接口范围内。

## 限额配置

限额属于宿主策略，`Config.Limits` 的每个字段按类别独立解释，不再有“全零才套默认”：

- 限额类：`MaxPageSize`、`MaxPending`（按租户分页计数，到上限即停）、`ActorPending`（同一 Actor 的未完成任务数，超出时提交返回 `ErrManagementActorBusy`）、`MaxInputBytes`、`MaxOutputBytes`、`IdempotencyRetention`、`TaskRetention`、`AuditRetention`，0 表示不限制或永不清理。保留期在写入时算出 `ExpiresAt`，改配置只影响新行。`PageSize` 为 0 时取 `constants.DefaultManagementPageSize`（50），`DeliveryTimeout` 为 0 时只受 ctx 约束。
- 槽位分配：`KindWorkers`（`RunnerKind` → 槽数）在 `Workers` 之外为某类任务另留专用槽，该类任务先占专用槽，占满后再占共享槽；别的任务会提交并等待的那类任务（例如工具内部提交的子任务）应配专用槽，否则共享槽被等待方占满时双方都不前进。`ActorWorkers` 限制同一 Actor（租户 + Subject）同时占用的共享槽数，超出的任务留在队列，专用槽不计入；二者为 0 或 nil 表示不启用，负数报错。
- 必需值：`Workers`、`PollInterval`、`OutboxPollInterval`、`PersistTimeout`、`DeliveryBackoffMax`、`BuildRetryDelay`，0 取同名 `Default*` 常量，负数报错。
- 任务预算：`Config.DefaultTaskBudget` 与 `SubmitTaskRequest.Budget`（`MaxModelRequests`、`MaxToolCalls`、`MaxAgentCalls`、`Timeout`），0 表示不限，计入幂等哈希并随 Task 以 `management/v2` 持久化；`management/v1` 任务解码时把 `MaxAgentCalls` 补成 1000。远程调用方无法自带预算（httpapi、a2a、mcpserver 都丢弃客户端提交的预算），所以 `DefaultTaskBudget` 为零值时这三个入口构造报错；确需远程任务不限时设置 `Config.UnlimitedRemoteTaskBudget`。自建远程入口应在构造时调用 `Service.ValidateRemoteTaskBudget()`。
- 入站请求大小（`httpapi`、`a2a`、`mcpserver` 的 `MaxInputBytes`）是防滥用上限：0 取 `constants.DefaultManagementMaxRequestBytes`（1 MiB），`UnlimitedRequestBytes` 显式取消。

`DefaultLimits()` 返回旧版本在 `Limits` 全零时套用的取值，需要保持旧行为时显式传入。

## 已知限制

- 单 worker 槽：同一 Store 只允许一个活动 Worker，`Limits.Workers` 只是该进程内的并发数，不是多实例分片；没有心跳超时自动接管，回收须由 `Config.ConfirmWorkerStopped` 确认旧进程停止后调用 `RecoverWorkerSlot`。
- 全局 advisory lock：contrib 的 PostgreSQL Store 在每个管理写事务和迁移中取同一把 `pg_advisory_xact_lock`，所有管理写入串行。多实例部署的分片与锁粒度另行设计，本版本不处理。
- 持久（journal）模式下 Builder 的 `errorFeedback` 被禁用：工具返回 Go 错误或超时会让任务失败，而不是把错误交给模型。工具的业务失败应作为工具结果返回（如 `tools.Rejected`）。
- 投递重试只有退避上限（`DeliveryBackoffMax`），没有次数上限，也没有死信；接收端持续失败的投递会一直重投。结果保存失败经 `Config.Report`（kind `outbox_save`）上报后按退避整轮重试，接收端 panic 计为一次失败。

领域错误直接使用 `github.com/kart-io/goagent/errors` 中的 `ErrManagement*`，不在本包维护旧错误别名。迁移说明和完整对应关系见 [核心迁移指南](../docs/guides/MANAGEMENT_MIGRATION_CN.md)。

## 本地验证

在 GoAgent 根目录运行：

```bash
env -u GOROOT GOTOOLCHAIN=go1.27.4 GOWORK=off go test -race ./management/...
env -u GOROOT GOTOOLCHAIN=go1.27.4 GOWORK=off go build ./...
```

核心单测不需要数据库。持久恢复、进程故障和 HTTP 退款案例继续在 contrib 使用专用 Postgres 验证，不能用内存单测或数据库 SKIP 代替。

`make test-management` 执行默认离线回归；`make coverage` 额外要求 management ≥75%、adapters ≥75%、httpapi ≥80%，三个包分别检查，不能用其他包的覆盖率抵消。

## 纯聊天与恢复边界

`adapters.NativeFactory` 支持只有 `llm.Client` 的纯聊天 Builder，无需添加占位工具。适配器绑定的会话日志记录完整轮次，重建后可按原执行键回放，Fork 继续绑定原配置版本。循环钩子和 Handle 仍要求原生工具循环。

单个活动 Worker 可按 `Limits.Workers` 并发执行不同会话；这不提供多实例分片或心跳超时自动接管。异常退出后，宿主必须通过 `ConfirmWorkerStopped` 确认旧进程停止，再执行带 owner、generation 和原因的回收。`Shutdown` 超时不代表旧生产者已退出。

启动恢复每批读取200条 running/canceling 任务和 applying 命令，修改状态后继续读取，直到集合排空，不再截断于10000条；全部批次仍在同一事务内，失败或取消时回滚。数据库必须支持事务内读到自身写入和原子 CAS；超大积压的事务时长、锁等待及恢复目标仍由实际容量测试确定。

跨仓实库验收使用 `make compatibility-postgres`，须配置专用测试数据库和允许重启的容器。它要求数据库存储契约、崩溃接管、重启、同步提交、管理审批进程恢复、Fork 崩溃恢复，以及 Harness 跨连接恢复、旧持有者失效和 Inbox 去重具有实际通过事件；缺失或跳过即失败。

新增20组HTTP与4项数据库业务案例的功能映射、红绿修复和执行结果见 扩展案例验收。

DeepSeek与Gemini的真实模型、审批续跑、多轮会话、Fork及实际用量结果见 真实模型验收。

真实模型验收工具的失败退出、请求增量证据和配置前检修复见 可靠性修复。

纯聊天支持、恢复分页、覆盖门禁及跨仓数据库复验见 管理层稳定化验收。

A2A、HTTP 与 MCP 共用任务的装配及互操作约束见 [协议接入指南](../docs/guides/PROTOCOL_ADAPTERS_CN.md)。

不可变配置、提示词、Skills 与程序工件的实际归档和旧版本加载见 [AgentRevision 与会话绑定](../docs/guides/AGENT_REVISIONS_CN.md)。
