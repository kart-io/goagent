# agent-runtime 对照：能力差距与移植范围

日期：2026-11-19。GoAgent 基线 `28009d88`，agent-runtime 固定源码 `f86ca97cea150024b3fa51735030429ab399ce24`（v0.1.1 之后，约 100 次提交，2026-10-05 创建）。本文只回答“goagent 缺什么”，是合并方案的前置输入，不是合并方案本身。

源码链接前缀：`https://github.com/costa92/agent-runtime/blob/f86ca97cea150024b3fa51735030429ab399ce24/`，下文 AR 列只写相对路径。

## 先明确比较对象

agent-runtime 是一个以事件溯源为中心的纯标准库内核：`session` 定义 Session/Turn/Step 事件协议 v3 和 Reducer，事件是唯一事实来源；`agentloop` 只通过 `session.Store` 读写事实，工作流顺序完全交给宿主提供的 `product.Product`；`capability` 是外部效果的唯一入口；`profile` 用 digest 把会话钉在一份已发布的插件清单上。

GoAgent 的对应能力分布在 `core/session`（会话事件日志）、`core/durable`（具名操作回放）、`builder`（原生工具循环与钩子）、`tools`（Dispatcher 与审批管线）、`core/budget`（单次执行准入预算）和顶层 `plugin`（能力包 Host）。两者的根本差别：AR 用一条带严格状态机的事件流表达全部执行事实；GoAgent 把对话事实放在会话日志、把操作结果放在 durable 记录（经 `session.DurableStore` 写进同一 journal），由 intent 哈希保证恢复一致性。

结论分级：**等价**＝GoAgent 已有同等保证；**部分**＝有近似能力但保证更弱或形态不同；**缺失**＝未发现对应实现；**不适用**＝属于 AR 的宿主或自身约束。

## 一、持久化与会话

| AR 能力 | AR 证据 | GoAgent 证据 | 结论 | 处理建议 |
| --- | --- | --- | --- | --- |
| 追加式会话日志，按期望序号比较并交换 | `session/store.go` | [Store.Append(expected)](../../core/session/store.go) | 等价 | 不移植 |
| 单写者：租约 + 单调递增 fence | `session/store.go` Lease/Claim | [Store.Lock 租约语义](../../core/session/store.go)、[DurableLockName](../../core/session/durable.go)、`durable.ErrLeaseLost` | 等价 | 不移植 |
| 模型调用先落记录再发请求 | `agentloop/loop.go` 包注释 | [durable.Do 先写 pending/ambiguous 再运行](../../core/durable/operation.go) | 等价 | 不移植 |
| 历史前缀引用（HistoryThrough），避免请求历史按平方增长 | `session/types.go` ModelRequest | 历史由会话日志派生（[DeriveMessages](../../core/session/event.go)）；模型操作 intent 含完整请求，但记录只存 `InputHash`（[OperationRecord](../../core/durable/operation.go)） | 等价 | 不移植。每步对完整请求求哈希是 O(n) CPU，暂无证据成为瓶颈 |
| Reducer 增量回放 + 版本化二进制 checkpoint（校验和，损坏即从事件重建） | `session/checkpoint.go` | durable 每个操作单独快照；会话日志有 `boundary/snapshot` 事件，但没有“派生状态缓存” | 部分 | 低优先。有长会话恢复耗时的实测证据后再做 |
| Replay 对整条日志做状态机校验，非法转换一律 `ErrInvalidLog` | `session/replay.go`、`session/skipped_test.go` | [session.Validate](../../core/session/validate.go) 校验序号连续、已知类型可解码与角色相符、轮次交替、中止轮恰好撤销本轮、Surface 位置与范围，违例包装 `ErrSessionInvalidLog`；`management.ForkSession` 复制前校验；Builder 测试的公共读日志辅助函数对全部 Builder 写出的日志执行校验 | 等价（已实现） | 不移植。与 AR 的差异：未知事件类型放行（宿主有 `management/session` 等自定义类型）；Builder 不在每轮调用，避免对存量日志突然失败关闭 |
| 停放/恢复（等待审批或外部结果） | `session/types.go` SessionParked/Resumed、Store.Wake | [WaitError/AwaitInput/WithResume](../../core/durable/operation.go)、[WaitingError](../../builder/waiting.go) | 等价 | 不移植。AR 的持久 Wake 信号由 GoAgent 宿主用同一执行键重调代替 |
| 收件箱：幂等键提交，ack 与 Turn 开启事实同事务 | `agentloop/loop.go` Submit、`session/store.go` AppendCommand | [durable.Inbox](../../core/durable/inbox.go)（持久化，但 ack 与领取记录不同写，靠 Seq 去重）、[Handle.SteerOnce(key)](../../builder/handle.go) | 部分 | 低。现有“按 Seq 去重”契约已闭环，只差同事务保证 |
| Outbox 记录与事件同事务提交 | `session/types.go` OutboxRecord | [management Outbox](../../management/outbox.go)：任务状态变更与投递记录同事务写入（[worker.go](../../management/worker.go) `finish`），`OutboxDispatcher` 至少一次投递稳定 ID | 等价（管理层） | 不移植。核心会话日志不承担通知职责 |
| 导入既有对话（HistorySeed，只含模型可见消息） | `session/types.go` HistorySeed | 未发现 | 缺失 | **中**。迁移存量会话到 GoAgent 时需要；可作为会话日志的一种事件类型 |
| 会话列表摘要（标题＝首条用户消息截断） | `session/summary.go` | [Store.List](../../core/session/store.go)、[BuildTrajectory](../../core/session/export.go)，无标题 | 部分 | 不进核心，宿主自行派生 |

## 二、执行循环

| AR 能力 | AR 证据 | GoAgent 证据 | 结论 | 处理建议 |
| --- | --- | --- | --- | --- |
| 失败 attempt 也保留用量，重试归循环所有 | `model/model.go` Response | [model_call.go 重试、reserve、recordUsage](../../builder/model_call.go)，失败操作保留用量 | 等价 | 不移植 |
| 流式 Delta 不入事件，丢失只影响展示 | `agentloop/loop.go` Delta | [AgentEvent.Delta](../../builder/events.go) | 等价 | 不移植 |
| 未执行的工具声明显式关闭（ToolSkipped，与执行结果区分） | `session/types.go` ToolSkip、`agentloop/tools.go` failStep | [Rejected 结果](../../tools/rejected.go)回给模型 | 等价 | 不移植 |
| ContextSource：首次请求前快照宿主输入并持久化，恢复时不重读可变状态 | `agentloop/loop.go` ContextSource | [PromptContext](../../builder/loop_hooks.go) 每步重算、不入历史；PreStep 结果经 `journaled` 记入日志 | 部分 | 不移植。用带身份的 PreStep 即可得到同样的“快照一次、恢复回放”，在文档中补用法 |
| 停止当前轮、会话继续（StopTurn）；跨进程请求停止（RequestTurnStop）；挂起审批被视为拒绝；运行中工具冲突 | `agentloop/loop.go`、`session/stop.go` | 进程内：[Handle.Interrupt(cause)](../../builder/handle.go)。跨进程：[management CancelTask](../../management/task.go) 持久写入 `CancelRequested` 与 cancel 命令，Worker 处理后取消执行、确认生产者退出才置 Canceled，恢复不重跑、释放会话、挂起审批作废（`TestOfflineExecutionCancel` 覆盖 Queued/Running/WaitingInput/WaitingApproval）。会话日志在下一轮开启时把遗留轮补记为 `TurnEnd{Aborted:true}`（[session_log.go](../../builder/session_log.go)），会话可继续 | 等价（管理层） | 低。剩余差异：`Aborted` 不区分用户停止与崩溃；中止事实到下一轮才写入；不用 management 的宿主只有进程内 Interrupt |
| Product：确定性工作流规划，Plan 在 StepStart 中持久化，阶段分 Model/Compute/Tool，Settlement 质量门 | `product/product.go`、`agentloop/stages.go` | [workflow 类型化有界图](../../workflow/graph.go) + durable；builder 是模型自主循环，输出校验见 output validation | 部分（形态不同） | **核心设计分歧**，见文末。默认不移植，用 workflow 表达确定性流程 |
| ModelAuthorizer：每次 attempt（含恢复）前重新授权计划中的资源 | `product/product.go` | 未发现专用钩子；[RequestHook](../../builder/loop_hooks.go) 每步在请求前运行 | 部分 | 低。RequestHook 已可承载，补文档即可 |

## 三、外部效果治理

| AR 能力 | AR 证据 | GoAgent 证据 | 结论 | 处理建议 |
| --- | --- | --- | --- | --- |
| 单一治理入口：输入拦截 → 授权 → 策略 → 预算，再执行 | `capability/service.go` Prepare/Execute | [Dispatcher](../../tools/dispatcher.go)（Authorize、并发、超时）、[PreHook 管线](../../tools/pipeline.go)（Allow/Deny/Ask/Stop/Substitute）、[tools/policy](../../tools/policy/policy.go) | 等价（GoAgent 更丰富） | 不移植 |
| Recorder.Begin 原子完成：记录调用 + 唯一幂等键 + fence 校验 + 预算预留 | `capability/service.go` Recorder | durable.Do 先写 pending，再在工具体之前 [ReserveTool](../../tools/dispatcher.go)；预算按“实际执行次数”计（[EXECUTION_GOVERNANCE](../guides/EXECUTION_GOVERNANCE_CN.md)），任何崩溃窗口只会多计不会少计 | 等价 | 不移植。已由 `TestDispatcherToolBudgetBoundsExecutionsAcrossRestart` 验证中断加重启后执行次数不超过 MaxToolCalls |
| 结果不确定时对账 | `agentloop/tools.go` ReconcileTool（宿主核实后写入） | [ReconcilableTool](../../tools/rejected.go)（工具按执行键自查）、[OperationPolicy.Reconcile](../../core/durable/operation.go)；宿主人工核实：[management](../../management/task.go) Attention 任务凭 `ReconciliationRef` 续跑，校验 VersionHash、EvidenceHash、VerifiedBy 与操作集合 | 等价（管理层） | 不移植 |
| 审批绑定精确调用（Fingerprint 含输入，排除审批 ID）；输入改写只能发生在授权之前 | `capability/service.go` Invocation.Fingerprint、InputInterceptor | [Approver 回调与会话级 Grant](../../tools/pipeline.go)。恢复路径：审批不持久化，未完成调用重新询问，换参数因 intent 不符失败（`TestApprovalIsNotReusedAcrossResume`）。进程内：Dispatcher 在准入时快照参数，准入结束与执行前比对，审批人、Guard、Around、PreHook 改写 `Call.Args` 即报错且不执行，改参数只能经 PreSubstitute 重新准入（`TestApprovedArgumentsAreExecuted`） | 等价（已修复） | 不移植 |

## 四、插件与配置

| AR 能力 | AR 证据 | GoAgent 证据 | 结论 | 处理建议 |
| --- | --- | --- | --- | --- |
| 依赖序激活，任一失败逆序回滚；Close 受 deadline 约束 | `cordis/plugin.go` Use/Close | [plugin.Host](../../plugin/host.go) Start 按依赖序、失败逆序停止，回滚不受启动 ctx 取消影响 | 等价 | 不移植 |
| 层级作用域 DI，Require 只放行清单内服务 | `cordis/context.go`、`cordis/plugin.go` | [plugin.Resources + Lookup[T]](../../plugin/resources.go) 按命名空间查找，无层级作用域 | 部分 | 不移植。GoAgent 插件是静态 Go 组合，层级作用域没有使用场景 |
| 进程内字符串事件 Serial/Waterfall/Parallel | `cordis/event.go` | 类型化钩子（PreStep、RequestHook、TurnStopping、PreHook）、middleware；observe 是单向观测 | 等价（类型化替代） | 不移植。不能委托给 `core/observe`，语义不同 |
| Profile 解析 + digest 钉住“配置 + 实现版本”，会话创建时固定 | `profile/profile.go`、`agentloop/loop.go` Config | [management Version](../../management/types.go) 为不可变记录，`ConfigHash = digest(RunnerKind, ImplementationVersion, Config[, RevisionID])`（[agent.go](../../management/agent.go)），Session 与 Fork 创建时绑定版本；核心层另有插件 Version 进入 PreStep 身份、durable Runner.Version，intent 不符即失败 | 等价（管理层） | 低。仅直接使用 builder 而不经 management 的宿主没有创建时钉住 |
| 提供方中立的模型接口 | `model/model.go` | [llm.Client / ToolStreamClient](../../llm) | 等价 | 不移植 |

## 五、预算

| AR 能力 | AR 证据 | GoAgent 证据 | 结论 | 处理建议 |
| --- | --- | --- | --- | --- |
| 按主体（principal）的 UTC 固定窗口额度，在事件追加事务内扣减 | `session/budget.go` | [core/budget](../../core/budget/controller.go) 是单次执行的准入预算（请求数、token、工具数、截止时间）；`core/middleware` 限流是进程内内存 | 缺失（跨会话额度） | **中**。属于多租户宿主能力，若要做应放在 `execution` 层或 Store 扩展，不放进 `core/budget` 的单次执行语义 |

## 六、不适用

| AR 能力 | 原因 |
| --- | --- |
| 生产依赖只允许标准库（`architecture_test.go`） | AR 自身约束。GoAgent 有第三方依赖，分层守卫已由 [scripts/apicheck](../../scripts/apicheck/main.go) 承担 |
| 数据库适配器、产品实现 | AR README 写明放在宿主仓库 |

## 汇总

28 项中：等价 19，部分 7，缺失 2（含复现测试与 management 核查后的修正）。

真正值得评估的只有以下几项，均可在 GoAgent 现有抽象内增量实现，不需要引入 `core/plugin`、`core/loop`、`core/capability` 等新包：

| 优先级 | 项目 | 落点 |
| --- | --- | --- |
| 已修复 | 批准后的钩子改写参数会改变实际执行 | `tools/dispatcher.go`：准入时快照参数，准入结束与执行前比对，不符即报错，改参数须用 PreSubstitute |
| 已实现 | 会话日志结构校验器 | `core/session.Validate`，`management.ForkSession` 接入 |
| 中 | 导入既有对话（HistorySeed） | `core/session` 事件类型 |
| 中 | 跨会话主体窗口额度 | `execution` 或 Store 扩展，需宿主需求确认 |
| 低 | 派生状态 checkpoint、Inbox 同事务 ack、`Aborted` 区分用户停止与崩溃 | 有需求或性能证据再做 |

复现测试结果（2026-11-19）：工具预算的崩溃一致性已满足，改为“等价”；审批在恢复路径上不会复用；进程内批准与执行之间的参数改写缺口已修复，改为“等价”。

核查 `management` 后的修正（2026-11-19）：跨进程停止、配置版本钉住、Outbox、宿主人工对账均已由管理层实现，初版只看了核心包，误判为缺口。

## 核心设计分歧：Product 与模型自主循环

AR 的 `agentloop` 没有隐式对话流程，每一步由 Product 基于已提交事实决定；GoAgent 的 builder 是模型驱动的工具循环，确定性流程由 `workflow` 图表达。两种模式在 GoAgent 中已经通过 `workflow`（确定性编排）+ `builder`（模型自主）分别覆盖，AR 的 Product 不提供 GoAgent 缺失的能力，只提供另一种组织方式。把 `agentloop` 作为第二个循环内核引入，正是合并方案要避免的双轨制。

因此，合并的实际形态应是：按上表逐项在 GoAgent 内补能力，每项单独提交并满足 apicheck 与 API 兼容检查；agent-runtime 本身在 README 写明迁移对照后归档。
