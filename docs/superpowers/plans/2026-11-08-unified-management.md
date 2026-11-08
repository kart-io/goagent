# 统一管理服务实施计划

> 后续决策：用户已取消旧版兼容要求。原计划命令留作归档；当前统一 GoAgent v0.50.0 与同仓 pgstore，复跑入口见[实施结果](../../evaluation/2026-11-08-unified-management/REPORT.md)。

> 执行更新（2026-11-08）：用户已授权执行，首批实现与案例验收已完成，见[实施结果](../../evaluation/2026-11-08-unified-management/REPORT.md)。原规划与调研文字保留作为基线，实际接口调整、合并测试和未覆盖边界以实施报告为准。
> **状态：**本计划已执行。以下步骤保留为实施记录；结果和后续验收见[实施结果](../../evaluation/2026-11-08-unified-management/REPORT.md)及其列出的后续报告。

**目标：**提供可嵌入宿主的 Agent、Session、Task、Approval 与运行查询服务，包含持久接纳、审批续跑和中断恢复的完整路径。

**架构：**独立 `goagent-contrib/management` 模块持有资源、管理事务与顶层 Task；GoAgent 继续执行模型、工具和持久图。多个 API 共用 Postgres，一个活动 worker 应用命令；Harness 仅通过可选子模块接入子工作与外部 effects。

**技术栈：**Go 1.27.0、Postgres 17、pgx/v5 v5.10.0、GoAgent Scope/session/durable/workflow/budget、标准 net/http、testify。核心工具链遵循各模块 go.mod。

**规格：**[需求](../specs/2026-11-08-unified-management-requirements.md)、[技术设计](../specs/2026-11-08-unified-management-design.md)、[可行性调研](../../research/2026-11-08-unified-management-feasibility.md)。

## 全局约束

- 所有新增生产文件位于相邻 `goagent-contrib/management`，不把管理数据库或 Harness 依赖加进 GoAgent 核心。先读取目标仓库开发规范。
- 本计划以下文件路径以 `goagent-contrib/management/` 为根；GoAgent 原文件作为只读参考，除非 T0 给出需要修订核心的实际证据。
- 默认分页 50、最大 200；请求体 1 MiB、结果 4 MiB；幂等默认 30 天、Task/结果 90 天、审计 180 天。未知用量保留未知。
- Session 固定版本；RunID=TaskID；恢复沿用 ExecutionKey、输入与预算。首批单次批准，Fork 限静止会话的正常轮次结束边界。
- 内存 Store 仅供测试；事务、隔离和恢复必须用真实本地 Postgres。`GOAGENT_PGSTORE_DSN` 未设置造成的 SKIP 不算通过数据库验收。
- 单活动 worker，无心跳超时自动接管；停服不是业务取消；Runtime/producer 真实退出后才释放资源。
- 生产依赖必须可下载，不提交机器 replace；本地组合使用临时 workspace。源码、文档、错误与提交说明遵循目标仓库中文规范。

## 审查重点

1. 相同幂等键配不同 JSON 数值、重复键或字段顺序：规范化不能舍入，也不能漏检冲突，归 T2/T8。
2. 两个并行工具待审、只批准一个、恢复后另一个仍待审：不串用批准、不重复创建待办，归 T0/T6。
3. Runtime 已成功、完成信封或 SQL 索引写失败：不得再次请求模型或执行工具，归 T0/T5/T10。
4. 取消、停用、停服与成功竞争：无伪终态、无资源提前关闭，归 T3/T5/T10。
5. 数据库或连接池暂不可用、worker 异常退出：事务必须结束、接纳未知可幂等重查、旧 owner 不可自动重入，归 T2/T5/T10。

## 文件职责

| 文件/目录 | 职责 |
| --- | --- |
| `go.mod`、`doc.go`、`README.md` | 独立模块、接入边界与使用说明 |
| `types.go`、`errors.go`、`auth.go`、`limits.go` | 实体/DTO、稳定错误、可信身份与查询权限、构造校验 |
| `store.go`、`pgstore/{store,tx,schema,query,history,owner}.go` | 管理事务接口与 Postgres 实现，schema 独立于既有 durable/pgstore |
| `agent.go`、`version.go` | 注册、不可变配置、发布与启停 |
| `session.go`、`fork.go` | 会话元数据与可恢复日志操作 |
| `task.go`、`command.go`、`worker.go`、`recovery.go` | 接纳、命令应用、会话准入、执行所有权与补偿 |
| `runtime.go`、`adapters/{native,graph,outcome,approval}.go` | Factory/Runtime 契约、原生/图运行、完成信封与持久审批 |
| `approval.go` | 待办投影、决定与可靠续跑命令 |
| `query.go`、`usage.go` | 按管理身份查询与用量去重汇总 |
| `httpapi/{handler,decode,routes,errors}.go`、`httpapi/openapi.yaml` | 标准 HTTP 路由、严格 DTO 与接口契约 |
| `outbox.go`、`harness/` | SQL 通知投递与可选 Harness 独立模块 |
| `internal/testkit/`、`integration/`、`examples/host/` | 脚本模型、子进程断点、真实数据库组合验收与离线例子 |

本表锁定职责和引用名称，不要求为很短的相邻类型机械拆文件；调整文件粒度时保留接口与验证归属。

## 依赖顺序

`T0 → T1 → T2 → T3/T4 → T5 → T6/T7 → T8/T9 → T10`。斜线表示逻辑上相互独立，不自动授权多代理工作。T0 任一可行性实验失败，先修订对应设计和计划，不能用占位实现绕过。

### T0：验证运行与审批的组合契约

**文件：**新增 `go.mod`、`adapters/feasibility_test.go`、`internal/testkit/script_model.go`、`internal/testkit/process.go`、`docs/feasibility.md`。参考 GoAgent `builder/waiting.go`、`tools/pipeline.go`、`core/durable/interrupt.go` 与 `workflow` 恢复用例。

**输入：**调研 E1–E4，现有 `durable.AwaitInput/InterruptAnswer`、`Builder.ExecuteStream`、`workflow.Resume/Snapshot`。

**输出：**实际组合证据，以及 T1 的 Runtime/Factory 最终协议；不产出可被误用为生产版本的临时 API。

- [ ] 新增 `TestNativeApprovalAfterRestart`：脚本模型提出工具调用，Approver 返回持久等待；杀掉子进程，新进程批准只执行一次、拒绝执行零次；必须看到正常 WaitError，不能当工具错误吞掉。
- [ ] 新增 `TestParallelApprovalsStayIsolated`：两个工具分别形成稳定待办，批准 A 不放行 B；B 的拒绝经工具结果交给模型。重入不会创建新的 B 待办。
- [ ] 新增 `TestApprovalAnswerConsumedAcrossCrash`：分别在输入固定前和固定后退出进程；InterruptAnswer 与原意图一致，已消费答复不再应用到下一次循环等待。
- [ ] 新增 `TestGraphDecisionAfterLostAck`、`TestCompletedRunRebuildsOutcomeReadOnly`：图批准/拒绝、丢确认后继续；完成结果的回放不能再调用模型/工具，调用计数严格不增加。
- [ ] 先运行单个新测试，确认错误源于尚无适配实现；实现仅实验所需的真实适配；再运行 `GOWORK=off go test -race -count=1 -run 'Test(NativeApprovalAfterRestart|ParallelApprovalsStayIsolated|ApprovalAnswerConsumedAcrossCrash|GraphDecisionAfterLostAck|CompletedRunRebuildsOutcomeReadOnly)' ./adapters`。
- [ ] 将每个实验的核心/依赖提交、执行命令、非 SKIP 结果和结论写入 `docs/feasibility.md`。任何等待传播或完成重建失败都必须修订设计后再进入 T1。
- [ ] 评审并提交该组实验与证据，提交说明：`验证管理服务的持久审批与恢复契约`。

### T1：冻结公开类型、权限和状态契约

**文件：**新增 `types.go`、`errors.go`、`auth.go`、`limits.go`、`runtime.go`、`store.go`、`types_test.go`、`auth_test.go`；完善 `go.mod`、`doc.go`。

**输入：**设计第 2/3/6/7 节和 T0 已验证协议。

**输出：**Service/Worker 共用的实体/请求、Factory/Runtime、Repository/Store/Tx 接口，随后各任务不得自行改名或暗加字段。

- [ ] 写 `TestTaskTransitionRejectsTerminalResume`、`TestCrossTenantReferencesRejected`、`TestDefaultsAndLimits`：终态不可恢复，租户交叉失败；默认/最大分页为 50/200，输入上限 1 MiB，幂等保留 30 天。
- [ ] 明确定义设计中全部 `*Request`、Actor、Action、ResourceRef、Query、Page[T]、Capabilities、Resources、RunSpec/RunResult/RunEvidence 和稳定错误；state、身份与执行键只能由服务端写入。
- [ ] 将设计中每个 Service 方法、Store.WithinTx、Runtime.Run/Inspect 与 Factory.Validate/Build 写为编译可用签名。Tx 使用 `Get/Insert/CAS` 加资源类型、`Get/PutIdempotency`、`AppendAudit/EnqueueCommand/EnqueueOutbox`；只读查询包含带 LIMIT 的历史方法。
- [ ] 定义设计中的 `Authorizer.Authorize` 与 `Authorizer.Scope(ctx,actor,action,resourceType) (Visibility,error)`；Visibility 的 All/ResourceIDs/AgentIDs/SessionIDs 只能编译为带租户约束的参数化 SQL，PolicyVersion 加入游标。列表默认拒绝，不支持 Scope 的实现构造失败。定义 ReconciliationReader 与不可变核对记录，未配置时 attention 恢复明确不支持。给常量与构造器补中文契约说明。
- [ ] 先跑新测试观察失败，完成类型校验和状态迁移规则后运行 `GOWORK=off go test -race -count=1 ./...`，预期全部已有测试通过，不允许空方法返回成功。
- [ ] 提交：`定义统一管理资源与运行适配契约`。

### T2：完成持久管理事务、幂等和查询基础

**文件：**新增 `pgstore/{store,tx,schema,query,history,owner}.go`、`pgstore/store_test.go`、`pgstore/idempotency_test.go`、`internal/testkit/postgres.go`。

**输入：**T1 Store/Tx/Repository、既有 contrib durable/pgstore 的存储与独立事务事实。

**输出：**`pgstore.New(ctx,pool) (management.Store,error)`、schema 版本校验与显式 `pgstore.Migrate(ctx,pool) error`；构造器不得隐式改表。

- [ ] 写 `TestAdmissionRollsBackAllRecords`：在实体、审计和命令写入后注入错误，全部回滚；`TestCommitUnknownRetriesSameKey`：确认丢失后重试得到同一资源。
- [ ] 写 `TestIdempotencyHashPreservesNumbers`：`9007199254740992` 与 `9007199254740993` 冲突、字段顺序不同不冲突、重复对象键拒绝；`TestRetentionDoesNotDropLiveKeys`：保留期内不能因结果裁剪失去去重。
- [ ] 实现全部设计管理表、带租户外键、唯一约束、revision CAS、事务结束和必要索引。管理事务只包 SQL，外部日志调用留在事务外。Schema 迁移有序且重复执行安全。
- [ ] 写 `TestHistoryIsBoundedAndHidesSnapshots`、`TestPaginationAppliesVisibilityBeforeLimit`、`TestCanceledTxReleasesConnection`、`TestWorkerOwnerCannotExpireByHeartbeat`，同时验证两个连接并发 CAS 仅一个成功。
- [ ] 在真实本地 Postgres 设置 `GOAGENT_PGSTORE_DSN` 后运行 `GOWORK=off go test -race -count=1 -v ./pgstore`；预期所有数据库用例 PASS，不能 SKIP。数据库不可用时本任务未完成。
- [ ] 运行 `GOWORK=off go vet ./pgstore`，提交：`实现管理事务与持久资源索引`。

### T3：实现 Agent 注册、固定版本和启停

**文件：**新增 `service.go`、`agent.go`、`version.go`、`agent_test.go`、`version_test.go`。

**输入：**T1 Service/Authorizer/Factory；T2 原子 Store。

**输出：**RegisterAgent、SaveVersion、PublishVersion、SetAgentEnabled；提供 T4/T5 所用的不可变 Version 和版本能力查询。

- [ ] 写 `TestVersionCannotBeOverwritten`、`TestPublishValidatesRuntimeCapabilities`、`TestRollbackPublishesExistingVersion`：禁止改快照，缺失审批/拒绝能力不能发布，回滚发布既有版本留审计。
- [ ] 实现严格配置校验、配置摘要、SecretRef、实现版本定位、发布 CAS 与可信操作者记录。发布只改变新会话默认值，不修改旧会话或运行配置。
- [ ] 写 `TestDisabledAgentBlocksAdmissionAndResume`、`TestDisableDoesNotCancelRunningTask`、`TestOldImplementationMissingDoesNotUseLatest`，验证停用与启动之间在 worker 再次校验，而不是只在 API 校验。
- [ ] 先观察上述测试失败，再完成服务逻辑，运行 `GOWORK=off go test -race -count=1 -run 'Test(Version|Publish|Rollback|Disabled|Disable|OldImplementation)' .`；预期 PASS。
- [ ] 提交：`实现 Agent 版本发布与启停管理`。

### T4：实现会话创建、历史和 Fork 补偿

**文件：**新增 `session.go`、`fork.go`、`session_test.go`、`fork_test.go`、`integration/session_process_test.go`。

**输入：**T2 管理事务与有界历史，T3 Version；现有 session.Store 的 Append/Read/Fork/Lock。

**输出：**CreateSession、ForkSession、History，以及 `SessionOperations.Apply(ctx,Command) error`、`SessionOperations.Reconcile(ctx,Command) error`，供 T5 派发 create_session/fork 命令。

- [ ] 写 `TestCreateSessionPreparingToReady`、`TestSessionKeepsPublishedVersion`：202 后 preparing，日志来源核对成功才 ready；v2 发布不改变 v1 会话。
- [ ] 实现源日志 ID、创建来源标识、接纳事务和异步 apply/reconcile；日志已存在时必须核对来源，不覆盖数据。无日志 Store 时拒绝构造。
- [ ] 写 `TestForkOnlyAtCompletedTurn`、`TestForkBlocksWhenTaskUnfinished`、`TestForkDoesNotReplayParentTask`、`TestForkSourceAndTargetIsolated`；纯图不支持日志 Fork 时返回能力错误。
- [ ] 写子进程 `TestForkCrashAfterLogCopy`，在日志复制完成、SQL ready 前退出；新进程比对前缀摘要并补 ready，目标内容不同进入 attention；创建也覆盖同样断点。
- [ ] 先失败后实现，运行 `GOWORK=off go test -race -count=1 -run 'Test(CreateSession|SessionKeeps|Fork)' . ./integration`，数据库测试全部执行。
- [ ] 提交：`实现固定版本会话与可恢复分叉`。

### T5：实现 Task、命令与单宿主执行闭环

**文件：**新增 `task.go`、`command.go`、`worker.go`、`recovery.go`、`adapters/{native,graph,outcome}.go`、`task_test.go`、`worker_test.go`、`integration/recovery_process_test.go`。

**输入：**T0 已验证运行路径、T1 Runtime/Factory、T2 Store、T3 Version、T4 SessionOperations。

**输出：**SubmitTask、CancelTask、Worker.Run/BeginShutdown/Shutdown、RecoverWorkerSlot；可运行的原生 Builder/持久图适配器与完成信封。命令执行器复用 T4 操作，不再维护第二份会话恢复逻辑。

- [ ] 写 `TestSubmitTaskReturnsAfterDurableAcceptance`、`TestDuplicateSubmitReusesTask`、`TestQueuedCancelNeverBuildsRuntime`、`TestSameSessionNeverOverlaps`：接纳后调用数为零，同会话运行计数最大为 1。
- [ ] 实现短事务命令领取、会话 active_task_id、FIFO 准入、实际调用前 Attempt、同一 Run/ExecutionKey 的 Scope＋持久 Budget。一个运行占一槽，等待后释放槽并保留会话逻辑占用。
- [ ] 实现 Factory 部分失败 Cleanup、Run 必须等待生产者结束、完成信封先保存后索引；Inspect 只读重建最终结果或等待，不能请求模型/工具。输出编码与 4 MiB 限制失败不能重新执行业务。
- [ ] 写 `TestCancelRequestWaitsForRealExit`、`TestSuccessWinsLateCancel`、`TestShutdownIsNotBusinessCancellation`、`TestCleanupFailureDoesNotReplaceOutcome`。取消走独立控制通道，不被同 Task 的长期 Run 阻塞。
- [ ] 写 `TestOutcomeSavedBeforeIndexCrash`、`TestCompletedOutputRebuiltWithoutModel`、`TestUnknownExternalEffectNeedsAttention`、`TestSecondWorkerRejected`、`TestRecoverOwnerRequiresExpectedGeneration`；异常 owner 不按心跳自动回收。
- [ ] 先失败后实现，运行 `GOWORK=off go test -race -count=1 . ./adapters ./integration`，检查子进程重启测试非 SKIP；再运行 `GOWORK=off go vet ./...`。
- [ ] 提交：`实现持久任务接纳取消与宿主恢复`。

### T6：实现持久审批与可靠续跑

**文件：**新增 `approval.go`、`adapters/approval.go`、`approval_test.go`、`integration/approval_process_test.go`。

**输入：**T0 输入消费证据；T5 等待集合、命令和运行适配。

**输出：**DecideApproval、ResumeTask、待办投影；`ApprovalProjector.Sync(ctx,Task,[]WaitPoint) error` 和 `ResumeResolver.Resolve(ctx,Command) (RunSpec,error)`，供 worker 使用。

- [ ] 写 `TestDecisionAuditAndCommandCommitTogether`、`TestConcurrentDecisionsHaveOneWinner`：两个审批人并发，只有一个 revision 更新和一个有效命令；决定已提交但 worker 未应用时仍可查询。
- [ ] 实现安全摘要、可信 Actor、稳定 WaitRef/意图哈希、等待 epoch 和目录补偿。相同未处理等待重新投影必须保留原 ApprovalID 和 epoch。
- [ ] 实现工具 approve/reject 答复、图批准/拒绝分支和当前策略复核；仅允许单次授权。无有效等待或不支持拒绝的适配器明确失败，不能伪造已批准。
- [ ] 写 `TestApprovalSurvivesCrashBeforeResume`、`TestConsumedDecisionDoesNotApplyToNextWait`、`TestPartialParallelDecisionPreservesOtherApproval`、`TestRevokedPermissionBlocksEffect`、`TestDisabledAgentKeepsDecisionPending`。
- [ ] 写 `TestTaskResumeCannotBypassApproval`：只拥有 task.resume 的主体提交审批等待点必须返回 approval_decision_required；伪造内部答复没有持久决定时 Runtime 拒绝消费。并行待办 B 不因 A 已消费而改变实例 ID/epoch 或丢失决定。
- [ ] 写 `TestInvalidResumeInputKeepsWaiting`：输入未被核心接受时保留可更正的 waiting_input，不把错误输入标记为已成功消费；不得形成无界自动重试。
- [ ] 先失败后实现，运行 `GOWORK=off go test -race -count=1 -run 'Test(Decision|ConcurrentDecisions|Approval|ConsumedDecision|PartialParallel|Revoked|DisabledAgentKeeps|InvalidResume|TaskResume)' . ./integration`。
- [ ] 提交：`实现审批审计与持久续跑命令`。

### T7：实现资源查询、运行与用量汇总

**文件：**新增 `query.go`、`usage.go`、`query_test.go`、`usage_test.go`。

**输入：**T1 Query/Page、T2 Repository/HistoryReader、T5 Task/Attempt/结果引用；现有 budget.Attribution、usage.Evidence。

**输出：**Get/List 五类资源、History 与 Run 视图；`UsageProjection.Refresh(ctx,TaskID) error` 和只读结果，不在查询中执行模型或恢复业务。

- [ ] 写 `TestQueriesFilterByAgentSessionTaskAndStatus`、`TestCursorRejectsChangedFilter`、`TestTenantCannotEnumerateOtherResources`，校验创建时间＋ID 翻页、待办与积压筛选和权限下推。
- [ ] 实现稳定 RunID 关联多 Attempt；持久计量按模型执行键/证据 revision 去重，不能累加每次查询到的累计账本。
- [ ] 写 `TestReplayDoesNotDoubleCountUsage`、`TestAgentTotalsUseSelfNotInclusive`、`TestUnknownUsageStaysUnknown`、`TestDroppedObservationCannotChangeTaskState`、`TestUsageUnavailableIsNotZero`。
- [ ] 先失败后实现，运行 `GOWORK=off go test -race -count=1 -run 'Test(Queries|Cursor|Tenant|ReplayDoesNot|AgentTotals|UnknownUsage|DroppedObservation|UsageUnavailable)' .`。
- [ ] 提交：`实现管理查询与持久用量投影`。

### T8：实现管理 HTTP 与可核对接口说明

**文件：**新增 `httpapi/{handler,decode,routes,errors}.go`、`httpapi/handler_test.go`、`httpapi/openapi.yaml`。

**输入：**T3–T7 已完成的 Service 方法；可信 `ActorFromRequest func(*http.Request)(Actor,error)`。

**输出：**`httpapi.New(service *management.Service, config Config) (http.Handler,error)`，设计第 7 节全部路由、OpenAPI 3.1 和 DTO。

- [ ] 写表驱动 `TestAllManagementRoutes`、`TestHTTPRejectsUnknownIdentityAndStateFields`、`TestBodyLimitAndDuplicateKeys`；验证 1 MiB、UseNumber、未知字段/重复键、多 JSON 文档、缺失 Idempotency-Key。
- [ ] 实现严格解码、请求 ID、Location、revision 与统一错误码；表中每条路由调用同一 Service。Handler 不持有 Task goroutine、不自动监听端口、不调用 httpagent.Run 重执行业务。
- [ ] 写 `TestHTTPStatusAndAcceptance`、`TestHTTPInvisibleResourceReturns404`、`TestDecisionRejectsClientActorOrWaitKey`、`TestClientDisconnectDoesNotCancelAcceptedTask`，覆盖 200/201/202/400/401/403/404/409/413/422/429/503。
- [ ] 为每条路由在 OpenAPI 声明输入、输出、必填头、权限与幂等语义；用 `TestOpenAPIRoutesMatchHandler` 从文档路由清单与注册表比较，不靠手工承诺完整。
- [ ] 先失败后实现，运行 `GOWORK=off go test -race -count=1 ./httpapi` 和 `GOWORK=off go vet ./httpapi`；真实 httptest.Server 证明 HTTP 断开后接纳仍存在。
- [ ] 提交：`提供统一管理 HTTP API 与接口契约`。

### T9：接入外部通知与可选 Harness 适配

**文件：**新增 `outbox.go`、`outbox_test.go`、`harness/go.mod`、`harness/{background,effects}.go`、`harness/integration_test.go`。

**输入：**T2 SQL outbox、T5/T6 已持久化的业务结果/决定；既有 background/effects 公开接口。

**输出：**`DeliverySink.Accept(ctx,Delivery) error`、`OutboxDispatcher.Run(ctx) error`；可选 Harness 适配将管理资源关联到已有子工作和效果，不接管顶层 Task。

- [ ] 写 `TestNotifyFailureDoesNotRerunTask`、`TestOutboxRedeliveryKeepsDeliveryID`：结果事务与 outbox 一起提交，通知失败重投同一 ID，不改变业务终态。
- [ ] Delivery 含稳定 ID、租户、事件类型、资源引用与安全摘要。确认丢失后允许至少一次，目标按 ID 去重；SQL outbox 与 effects 入队不是跨存储原子事务，桥接重试必须保留此边界。
- [ ] 写 `TestEffectUnknownNeedsReconcile`、`TestChildProjectionDoesNotStartTopLevelTask`、`TestBackgroundRetentionDoesNotDropManagementIdempotency`。禁止把 effects 的待办覆盖合并行为当成永久去重。
- [ ] 主模块运行 `GOWORK=off go test -race -count=1 .`；在 `harness/` 运行 `GOWORK=off go test -race -count=1 ./...`，并用临时 workspace 验证本地核心、Harness、pgstore 与管理模块。
- [ ] 主模块单独 `go list -deps ./...` 确认不引用 Harness；提交：`接入可靠通知与可选 Harness 管理适配`。

### T10：完成跨进程验收与宿主交付

**文件：**新增 `integration/{host,shutdown,acceptance}_test.go`、`examples/host/main.go`、`docs/acceptance.md`、`docs/operations.md`，完善 `README.md`。更新 contrib 既有模块验证入口，沿用其格式，不私自建立另一套构建脚本。

**输入：**T0–T9 全部接口与实现。

**输出：**无模型密钥的真实 HTTP＋Postgres＋脚本模型完整闭环例子，可重复恢复/审批/取消测试与可审计验证报告。

- [ ] 写完整 `TestHostRegisterPublishSessionTaskApprovalResume`：v1 注册发布、Session、工具审批、单次执行、查询用量；发布 v2 后旧 Session 仍 v1，新 Session 用 v2。
- [ ] 写 `TestRestartAtEveryCommitBoundary`：接纳后、日志创建/Fork 后、答复消费前后、结果信封后、SQL 结果前、outbox 确认前分别杀进程；重启不丢命令、不重做已确认效果、不复用旧批准。
- [ ] 写 `TestCancelDisableShutdownAndSuccessRace`、`TestUncooperativeRunnerKeepsOwnership`、`TestDatabaseUnavailableLeavesNoFalseAcceptance`、`TestOwnerRecoveryDoesNotOverlapOldProcess`。旧进程未退出时不能正常回收 worker slot。
- [ ] 运行主模块与可选子模块各自 `GOWORK=off go test -race -count=1 -timeout 180s ./...`、`GOWORK=off go vet ./...`，使用真实本地 DSN；输出需显示所有要求数据库/子进程测试均执行而非 SKIP。
- [ ] 在 GoAgent 仓库运行 `make compatibility COMPAT_ROOT=..`，检查新嵌套模块被发现。若枚举规则未覆盖，修订 contrib 的既有验证入口；不得把未参与检查的模块写成已兼容。
- [ ] 文档明确启动与 shutdown 顺序、owner 异常回收前置条件、Postgres/pgstore schema、保留/清理、attention 核对、版本保留、幂等有效期和上线前容量实测。示例不含模型密钥或机器路径 replace。
- [ ] 按下面需求矩阵逐项回填实际命令、PASS/FAIL/SKIP、证据文件及限制；所有首批需求满足后提交：`完成统一管理服务宿主验收与运维说明`。

## 需求到任务与验收映射

| 需求编号 | 责任任务 | 主要验证 |
| --- | --- | --- |
| AG-01 | T2、T3、T8 | 注册唯一、共享查询、HTTP 路由 |
| AG-02 | T3、T10 | 不可变版本、发布校验、发布审计 |
| AG-03 | T3、T5、T6 | 停用准入、既有运行、待应用恢复 |
| AG-04 | T3、T4、T10 | 旧版本重建与实现缺失拒绝 |
| SE-01 | T4、T10 | preparing/ready、创建断点恢复 |
| SE-02 | T2、T4、T7 | LIMIT、事件顺序、快照隐藏 |
| SE-03 | T4、T10 | 合法 Fork、源分支隔离、复制后中断 |
| SE-04 | T5、T6 | 同会话不重叠、等待保留占用 |
| TA-01 | T2、T5、T8 | 幂等提交、事务回滚、确认丢失 |
| TA-02 | T7、T8 | queued/等待/attention 筛选 |
| TA-03 | T5、T10 | queued 取消不构建、真实退出、成功竞争 |
| TA-04 | T0、T5、T6 | 固定身份恢复、非法输入保持等待 |
| TA-05 | T0、T5、T10 | 独立进程恢复与结果补偿 |
| TA-06 | T5、T9 | 未知外部效果不盲目重试 |
| TA-07 | T5、T10 | 生产者/Cleanup 退出与关闭顺序 |
| AP-01 | T6、T7、T8 | 安全待办目录与权限列表 |
| AP-02 | T2、T6、T8 | 决定 CAS、审计、可信操作者 |
| AP-03 | T6、T10 | 决定与命令原子提交、崩溃后应用 |
| AP-04 | T0、T3、T6 | 工具拒绝、图拒绝分支、能力校验 |
| AP-05 | T0、T6 | 并行等待隔离、循环旧决定失效 |
| OB-01 | T7、T8 | Run/Attempt 关联与错误查询 |
| OB-02 | T7、T10 | 持久证据去重、恢复不重复累加 |
| OB-03 | T7 | unknown、self/inclusive、未归属 |
| CO-01 | T1、T2、T7、T8 | 跨租户拒绝、身份不可写、权限下推 |
| CO-02 | T2、T4、T5、T6、T9 | 原子接纳与各跨存储核对点 |
| CO-03 | T1、T2、T7、T8 | 50/200 分页、LIMIT 与 1 MiB |
| CO-04 | T2、T9、T10 | 保留期内幂等独立于子工作裁剪 |
| CO-05 | T5、T8、T9、T10 | 断开/流结束/通知失败不产生伪终态 |

## 开始实施与本轮验证边界

开发入口是 T0。先验证组合契约，然后按依赖顺序推进；T3/T4 等划分不代表跳过前置约束。每个任务的测试失败必须先定位原因，禁止把“测试可跳过”当作完成标准。

本轮只检查文档可追溯性、需求覆盖、路径和契约一致性；上述拟新增文件、接口、测试与命令是未来执行项，不是已存在或已通过的产品能力。本计划不要求本轮安装数据库、修改相邻仓库或提交代码。
