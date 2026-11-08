# 统一管理服务可行性调研

> 执行更新（2026-11-08）：用户已授权执行，首批实现与案例验收已完成，见[实施结果](../evaluation/2026-11-08-unified-management/REPORT.md)。原规划与调研文字保留作为基线，实际接口调整、合并测试和未覆盖边界以实施报告为准。
日期：2026-11-08。结论状态：代码与官方文档调研完成；集成可行性实验、产品实现与数据库验收尚未运行。

需求见[需求文档](../superpowers/specs/2026-11-08-unified-management-requirements.md)，方案见[技术设计](../superpowers/specs/2026-11-08-unified-management-design.md)，行动项见[实施计划](../superpowers/plans/2026-11-08-unified-management.md)。

## 1. 结论

现有组件足以支撑统一管理服务，但缺少业务资源目录、配置版本、持久命令接纳、权限审计和面向运营的查询。核心工作是补管理控制流程并装配现有执行机制。

建议新增独立 `goagent-contrib/management` 模块，GoAgent 核心不反向依赖它。顶层 Task 由管理 worker 统一持有；Harness background 继续管理 Agent 内部子工作，effects 用于外部通知与动作。不能把 background 的子任务状态直接当成全局 Task 状态。

单活动 worker、共享 Postgres 接纳/查询是首批可落实的部署建议。多 worker 自动接管与管理界面需要独立需求和验收，不在这组文档里宣称已具备。

## 2. 调研基线

以下是本地检索时的提交，不代表这些模块已经发布或已经通过组合测试。

| 仓库 | 提交 | 检索范围 |
| --- | --- | --- |
| goagent | `14f8c328ef07058156cf768bb3a2b918fea5d445` | Scope、Session、durable、Workflow、Builder、Dispatcher、budget、usage、Plugin |
| goagent-harness | `367330b2194713b2d94b6c567c75634c4f866cb5` | background、effects 及恢复测试 |
| goagent-contrib | `634b5d8c242859e6842fa0e7d5aa4f32e2d35170` | httpagent、durable/pgstore 及数据库测试 |

核心声明 Go 1.27.0、toolchain go1.27.3；相邻模块声明 Go 1.27.0，pgstore 声明 pgx v5.10.0。相邻模块当前引用的 GoAgent 伪版本不等于本地 HEAD。实施时须分别验证已发布依赖与临时 workspace 的本地组合，不能把机器绝对路径 replace 提交到仓库。

## 3. 已有能力、缺口与复用方式

| 能力 | 代码依据 | 已有保证 | 管理层仍要补充 |
| --- | --- | --- | --- |
| 执行资源装配 | [Scope](../../execution/scope.go) | 稳定 Run/Session、Journal、Budget、Tracer/Observer，拒绝上下文身份冲突 | Agent 资源目录、版本、Task/Attempt、资源所有权 |
| 会话日志 | [Store](../../core/session/store.go)、[日志适配](../../core/session/durable.go) | CAS、List、Fork、Lock，日志与快照同源 | 业务元数据、租户、版本、分页、全会话执行准入 |
| 恢复与外部效果 | [Operation](../../core/durable/operation.go) | 持久边界、回放、结果未知核对、WaitError | 全局发现、任务状态、显式恢复授权 |
| 动态人工输入 | [Interrupt](../../core/durable/interrupt.go) | AwaitInput 的首次合法输入固定，InterruptAnswer 查询是否已消费 | 待办目录、可信处理人、决定审计、续跑命令 |
| 图审批 | [人工介入契约](../guides/HUMAN_IN_THE_LOOP.md) | Resume 写入状态与放行标记；旧等待键拒绝；授权再复核 | 业务 API 与 worker、拒绝分支配置 |
| 工具审批 | [Pipeline](../../tools/pipeline.go)、[Dispatcher](../../tools/dispatcher.go) | Approver 回调，单次/会话允许、拒绝，执行前 Guard | 把等待变成持久待办；普通阻塞回调不能直接作为跨重启审批 |
| 子工作 | [background 源码](https://github.com/kart-io/goagent-harness/blob/367330b2194713b2d94b6c567c75634c4f866cb5/background/background.go) | 稳定身份、等待/恢复、结果/通知提交、槽位 | 顶层 queued、全局目录、业务取消、长期结果保留 |
| 外部待办 | [effects 源码](https://github.com/kart-io/goagent-harness/blob/367330b2194713b2d94b6c567c75634c4f866cb5/effects/effects.go) | Commit 原子写入同一 durable.Store 的决定与待办，未知效果核对 | 与管理 SQL 事务之间的投递桥接、通知审计 |
| 共享持久存储 | [pgstore](https://github.com/kart-io/goagent-contrib/blob/634b5d8c242859e6842fa0e7d5aa4f32e2d35170/durable/pgstore/pgstore.go) | 租约续期，写事务校验令牌，旧持有者写入返回 ErrLeaseLost | 管理表事务、运行索引；外部系统仍需自己的幂等和隔离 |
| 网络入口 | [httpagent](https://github.com/kart-io/goagent-contrib/tree/634b5d8c242859e6842fa0e7d5aa4f32e2d35170/httpagent) | 统一运行绑定、SSE/WS、显式 queued Acceptor | 管理路由、Task 幂等、持久命令，不重复启动业务 |
| 用量归属 | [budget.Attribution](../../core/budget/attribution.go)、[usage.Evidence](../../core/usage/evidence.go) | self/inclusive、已知字段、未知条目、持久预算 | 按管理资源建立索引、去重汇总与完整性标记 |
| 观察与诊断 | [observe](../../core/observe/event.go)、[Diagnose](../../core/durable/diagnostics.go) | 只读观察、有限错误分类、显式操作定位 | 不能用丢失的观察事件推断成功，也不能自动枚举未知操作 |
| 插件注册 | [PluginRegistry](../../core/plugin_bridge.go)、[Plugin Host](../../plugin/host.go) | 进程内 Runnable/插件注册、依赖启停 | 持久 Agent 资源、配置发布、版本保留；不是现成运营目录 |

## 4. 影响实施的关键发现

### 4.1 background 不能直接当顶层调度平台

同一 Store/Name 只有一个 `Manager.Run`。`Spawn` 与 `Resume` 检查本进程的 Manager 状态；记录的 running 包含等待槽位的时间，没有独立 queued。`RetainFinished` 默认 1000，裁剪后同 ID 可以再执行；默认保存结果最多 256 KiB，不能担当业务的永久幂等或结果存储。

`Resume` 的输入不持久化，续跑中崩溃可再次等待。`Kill` 的先落盘结果策略也不能直接转换成管理 API 中“Runner 已退出”的取消确认。依据：[background 使用契约](https://github.com/kart-io/goagent-harness/blob/367330b2194713b2d94b6c567c75634c4f866cb5/background/README.md)。

**采用方式：**顶层 Task 与 Command 放管理表；background 作为可选子工作后端，投影其 ChildID/Owner/Generation。不要为了接入管理服务复制 background，也不要另套一个顶层 Manager 造成两个生命周期所有者。

### 4.2 持久审批和阻塞审批须区分

`core.InterruptManager.ListPendingInterrupts` 来自实例内待办集合，不能直接作为跨实例持久待办目录。`tools.Approver` 是回调协议，不自动保证持久等待。`durable.AwaitInput` 和图的 Resume 才提供明确持久输入边界。

**采用方式：**管理层存审批目录与审计；图适配器用图 Resume，动态输入用 AwaitInput/InterruptAnswer。工具适配器拟通过持久 AwaitInput 返回等待，并把批准/拒绝映射为 ApprovalResult。该路径需要先验证 Builder→Dispatcher→Approver 的等待传播、并行工具调用键和恢复消费；对应实施任务 T0。若无法保持 WaitError 与稳定调用身份，必须先修订适配方案，不能退回一直挂住 goroutine 的实现并继续宣称持久审批。

### 4.3 一个数据库不代表一个事务

pgstore 与 SessionStore 在自己的方法内部创建事务。管理模块即便使用相同 pgxpool，也不能把这些独立事务自动并入管理实体事务；effects.Commit 的 SaveAll 原子性同样只覆盖对应 Store 的范围。

**采用方式：**SQL 事务保存决定和命令；worker 在事务外调用运行存储；通过稳定键与只读核对恢复缺失的确认。只有明确支持共享事务的新增接口才可改变这一边界，首批不为此修改核心存储接口。

### 4.4 租约隔离不等于外部动作不会重复

pgstore 能阻止过期持有者写回检查点，不能替第三方系统撤销已经发出的动作。首批只运行一个活动执行宿主，第二个 worker 直接拒绝启动，不自动超时抢占。worker 异常重启仍须确认旧进程退出；不能依据心跳缺失自动启动另一个可能重叠的执行者。

后续自动接管需同时验证会话日志普通消息写入、Task 状态写入和外部效果的隔离，不能只运行 durable.Store 的通用一致性测试就声明多机安全。

### 4.5 Fork 不是跨版本恢复

底层 Fork 复制事件和检查点，源会话与目标隔离，但不会自动建立新的业务元数据或迁移图/Agent 定义。首批固定版本、选完整轮次边界、创建新的 Task 执行键，并限制源会话无未结束任务。执行中分叉与跨版本历史导入放到后续设计。

### 4.6 计量需要证据与查询关联

预算的根 RunID 与 AgentCallKey 不等于管理层 AttemptID，Task 恢复后也不应按每次观察投递再次计费。Token 的 KnownFields、UnknownUsageEntries 和 self/inclusive 都应保留。CPU、内存、金额并无统一现成证据来源，不应承诺首批自动采集全部资源。

## 5. 官方方案对照

外部资料仅用于核对机制，不作为引入新运行时的理由。以下架构选择是结合本地代码作出的推断。

| 一手资料 | 资料确认的机制 | 对本方案的影响 |
| --- | --- | --- |
| [LangChain Agent Server](https://docs.langchain.com/langsmith/agent-server) | assistants/threads/runs 分离；API 和 worker 可分离；一个 thread 至多一个 run 执行 | 对应 Agent/Session/Task；会话串行必须由服务保证 |
| [Temporal Go 取消](https://docs.temporal.io/develop/go/workflows/cancellation) | 取消请求由工作流处理，最终状态取决于实际退出行为 | 管理层记录 canceling，不能接纳取消就宣称已停止 |
| [Postgres 17 SELECT](https://www.postgresql.org/docs/17/sql-select.html) | SKIP LOCKED 可用于队列消费者，但不是一般一致性查询 | 将来用于短事务命令领取；不能替代会话锁与长期执行所有权 |
| [pgxpool BeginTx](https://pkg.go.dev/github.com/jackc/pgx/v5/pgxpool#Pool.BeginTx) | Begin 的 context 取消不自动回滚；必须结束事务 | 管理事务使用明确 Commit/Rollback 或 BeginFunc，禁止长期事务包住 Agent 执行 |

pgx 文档首先经 Context7 查询 `/jackc/pgx/v5.10.0`，再对照官方 API。所有外部页面核对日期为 2026-11-08；未引用二手教程或将其他框架的保证视作 GoAgent 的能力。

## 6. 方案比较

| 方案 | 优点 | 代价与结论 |
| --- | --- | --- |
| 加进 GoAgent 核心 | 一个模块接入 | 引入数据库与业务权限，违反现有核心/Harness 边界；不采用 |
| 独立管理模块＋现有执行内核 | 宿主可嵌入；复用日志、预算、审批和图 | 需实现管理事务与命令应用；建议采用 |
| 直接包装 background | 接入初期代码少 | queued、取消、输入消费、保留期和全局查询不匹配；不作顶层方案 |
| 接入 Temporal 或完整 Agent Server | 现成调度和运营能力更多 | 引入第二套部署/执行语义，需要明确平台替换目标；本需求下暂不采用 |

## 7. 实施前必须验证的实验

| 实验 | 最小证据 | 失败后的处理 |
| --- | --- | --- |
| E1 原生工具持久审批 | 脚本模型触发受限工具，等待后退出进程；新进程批准/拒绝；工具零次/一次；并行工具待办隔离 | 修订 Approver 适配与稳定键；不继续上层 API 实现 |
| E2 输入消费与恢复 | AwaitInput 接受前/后各崩溃一次；InterruptAnswer 能识别已消费；新轮次拒绝旧决定 | 修订命令核对，不能无限重投旧批准 |
| E3 图审批 | 有批准/拒绝分支的持久图，Resume 确认丢失后 Snapshot＋Run 恢复，无重复人工修改 | 收紧图适配契约或报告不支持 |
| E4 完成记录与索引补偿 | 输出已持久化而管理 SQL 提交前崩溃，新进程只补索引 | 增加稳定完成信封，不能重调模型求一次新结果 |
| E5 接纳/Fork 中断 | 管理 preparing 与会话日志写入之间各中断，核对来源与摘要后恢复 | 定义确定的 attention，不静默覆盖目标会话 |

本轮没有运行这些实验，也没有宣称它们通过；每项均有实施任务和验收归属。已有核心单测仅证明组件契约，不能替代新组合的证据。
