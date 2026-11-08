# 持久事实记忆验收记录

日期：2026-11-08
状态：实现进行中；已验证的行为可用，尚未达到计划的完整验收标准。

## 已执行案例

| 场景 | 自动化案例 | 结果 |
| --- | --- | --- |
| 外部 Go package 实现公开接口 | `TestFactExternalPackageCanImplementInterfaces` | PASS |
| 同请求幂等保存、冲突键拒绝、过期 tombstone、删除后重放不泄漏/复活 | `TestFactSaveIdempotentSameRequest`、`TestFactIdempotencyRejectsDifferentRequest`、`TestFactExpiredIdempotencyTombstoneSurvivesGCAndRestart`、`TestFactSaveDeleteRestartReplayDoesNotLeakOrResurrect` | PASS，内存契约 fixture |
| revision CAS 与替代关系 | `TestFactRevisionCASHasSingleWinner`、`TestFactSupersedeLeavesOneActiveFact` | PASS，内存契约 fixture；PostgreSQL CAS 也通过并发案例 |
| 召回阈值、常驻偏好预算/TTL、快照删除失效 | `TestFactRecallRequiresSimilarityThreshold`、`TestFactRecallAlwaysClassSurvivesUnrelatedQuery`、`TestFactRecallAlwaysStillHonorsBudgetAndTTL`、`TestFactSnapshotInvalidatesAfterDelete` | PASS |
| Snapshot 在事实更新或 embedding profile/generation 变化后失效 | `TestFactSnapshotInvalidatesAfterUpdate`、`TestFactSnapshotInvalidatesAfterProfileChange` | PASS |
| Builder 注入、缓存旁路、会话日志隔离 | `TestFactBuilder*`、`TestSessionLog*`（`builder` 包） | PASS |
| durable journal 重启后重放同一 SnapshotRef，事实 revision 变化时拒绝继续，并确认正文不写入 journal | `TestFactBuilderDurableReplayPinsSnapshotAndRejectsChangedFact` | PASS；FileStore 重开与变化生命周期通过持久 journal 案例验证 |
| Service 读写/召回与 Worker 索引更新的可观测事件、安全错误分类且不含正文 | `TestFactServiceEmitsBodyFreeOperationEvents`、`TestFactWorkerEmbedsAndAppliesPendingMutation` | PASS；memory race 检查通过 |
| PostgreSQL 跨进程重启、正文持久化、删除后不可读、稳定幂等回放 | `TestPostgresFactPersistsAcrossStoreRestart` | PASS；helper 在独立 Go test 子进程读取 |
| PostgreSQL provider 契约：幂等、隔离、CAS、删除、标签/来源过滤、替代、body-free 历史、TTL 和 scope 清空 | `TestPostgresProviderConformance` | PASS；9 个 Provider-neutral 子案例 |
| PostgreSQL 并发 CAS 与 body-free 变更链 | `TestPostgresConcurrentRevisionAndBodyFreeChanges` | PASS |
| scope 清空立即封锁写入，Worker 推进完成后可恢复 | `TestPostgresScopeClearBlocksWritesAndResumesWorker` | PASS |
| scope 清空提交后模拟 Store 进程退出，重开 Store 后仍封锁写入，Worker 续跑完成后恢复写入 | `TestPostgresScopeClearResumesAfterStoreRestart` | PASS；真实 PostgreSQL/pgvector Provider |
| pgvector 持久检索、scope 过滤、删除 tombstone 阻止旧向量复活 | `TestPostgresVectorIndexPersistsAndFiltersScope` | PASS |
| 在线重建扫描事实并切换到新 profile/generation | `TestPostgresOnlineRebuildSwitchesGeneration` | PASS；在真实 PostgreSQL/pgvector 上执行 |
| 真实向量索引在独立进程重启后完成语义召回 | `TestPostgresRecallAfterIndependentProcessRestart` | PASS；子进程重新打开 Store/Index 并召回正文 |
| 事实列表签名游标跨 Store 重启续页；绑定身份/scope/filter；标签与来源筛选生效 | `TestPostgresListCursorPersistsAndBindsFilters` | PASS；包含篡改和跨过滤条件复用拒绝 |
| 变更链分页及事实 ID 绑定 | `TestPostgresListCursorPersistsAndBindsFilters` | PASS |
| 维护任务分页、授权 scope SQL 过滤和状态筛选绑定 | `TestPostgresMaintenanceCursorBindsScopeAndState` | PASS |
| outbox 指数退避、10 次失败停止领取、显式授权重试及过期租约恢复 | `TestPostgresOutboxFailureBackoffStopsAndCanRetry` | PASS；固定诊断码不包含正文 |
| outbox 两个并发 Worker 领取结果无重叠 | `TestPostgresOutboxClaimsAreDisjointAcrossWorkers` | PASS；真实 PostgreSQL `SKIP LOCKED` 案例 |
| outbox 达到可配置容量时普通 Save 整笔回滚；Delete 使用保留容量，Worker 排空后容量恢复 | `TestPostgresOutboxBackpressureIsAtomicAndReservesDeleteCapacity` | PASS；真实 PostgreSQL 事务案例 |
| 同一层的签名游标拒绝篡改、跨查询绑定和过期 token | `TestSignedCursorRejectsTamperingBindingAndExpiry` | PASS |
| 重建期间写入事实，双写 outbox 追平后新代际包含 cutoff 前后事实 | `TestPostgresOnlineRebuildCatchesConcurrentWrite` | PASS；真实 PostgreSQL/pgvector 案例 |
| OwnerAgent 私有记忆绑定已认证用户，同 Agent 下不同用户互相不可见 | `TestFactAgentPrivateScopeBindsAuthenticatedUser`、`TestPostgresAgentPrivateScopeSeparatesUsersOfSameAgent` | PASS；内存服务和真实 Provider |
| 两个不同幂等键并发清空同一 scope 后，重试键仍指向同一清理任务 | `TestPostgresConcurrentClearKeysRemainBoundToSameOperation` | PASS；真实 PostgreSQL 事务案例 |
| 首屏查询后更新尚未翻页的事实，后续稳定游标仍能返回该事实 | `TestPostgresListCursorKeepsUnseenFactAfterUpdate` | PASS；游标按不可变创建序列分页 |
| 清理和替代后重放原始幂等键仍返回原始回执，不被新的 TTL 校验拒绝 | `TestFactExpiredInputReplayReturnsOriginalReceipt`、supersede 重放断言 | PASS |
| durable Run、模型请求 Hook 和恢复消息均不持久化召回事实正文 | `TestFactPreStepJournalDoesNotPersistFactBody`、`TestFactBuilderDurableReplayPinsSnapshotAndRejectsChangedFact` | PASS；FileStore journal 检查 |
| Handle Followup 每轮均重新召回当前授权的事实 | `TestFactBuilderRecallsForHandleFollowup` | PASS |
| always 事实超出普通 List 首页时仍能被召回 | `TestFactRecallAlwaysBeyondOrdinaryListPage` | PASS；内存 Provider fixture |
| Observer 回调 panic 不改变已提交操作结果，也不终止 Worker | `TestFactObserverPanicDoesNotChangeCommittedOperations`、`TestFactWorkerObserverPanicDoesNotStopWorker` | PASS |
| 幂等 key 仅存 SHA-256 摘要，过期回执字段清空为 tombstone | `TestPostgresIdempotencyExpiresToDigestOnlyTombstone` | PASS；真实 PostgreSQL |
| 过期 outbox worker 和 rebuild worker 无法覆盖新租约持有者的结果 | `TestPostgresExpiredWorkerCannotOverwriteNewerAcknowledgement`、`TestPostgresRebuildClaimFencesExpiredWorker` | PASS；真实 PostgreSQL race 案例 |
| 已启用事实记忆时，未声明 cache bypass 能力的响应缓存中间件会被拒绝 | `TestFactBuilderRejectsResponseCacheWithoutBypassSupport`、`TestPublicCacheBypassSignal` | PASS |

真实 Provider 命令（测试要求 DSN，未设置时失败，不会跳过）：

```sh
cd examples/fact-memory/postgres
GOAGENT_PGSTORE_DSN='postgres://postgres:localtest@localhost:55432/postgres?sslmode=disable' GOWORK=off go test ./... -count=1
```

本轮命令结果：PostgreSQL Provider `GOWORK=off go test -race ./... -count=1` PASS；根模块 `GOWORK=off go test ./... -count=1` PASS；`GOWORK=off go test -race ./memory/... ./builder/... -count=1` PASS；根模块 `go vet ./...` 与 `git diff --check` PASS。

## 尚未通过完整验收的要求

- conformance 已扩至 9 个 Provider-neutral 生命周期案例，但仍未映射覆盖完整 PM-01～PM-35。
- 游标已覆盖篡改、过期和绑定；签名密钥轮换策略尚未覆盖，密钥保存在 Provider schema 中，密钥泄露会允许伪造该 schema 的游标。
- outbox 已覆盖退避、租约恢复、10 次终止、显式重试和两个 Worker 的不重叠领取；高并发负载、长期 scope 清理进程中断恢复仍需要压力案例。
- 在线重建已覆盖全局扫描、并发写双写追平及切换；Embedder/Index 持续故障后的重试、删除与 TTL 边界及进程中断恢复的专项案例仍缺失。
- 尚无一个 acceptance 用例把真实 PostgreSQL Provider、Builder、durable Run、撤权、共享缓存和清空恢复全部串成完整业务流程；对应能力目前由独立组件案例验证。
- PM-21 已增加 privacy-safe `MemoryObserver` 事件接口；metrics exporter、Provider 连接池/队列深度等基础设施指标和仪表盘示例未包含。

因此当前结果是“核心逻辑与 PostgreSQL 参考 Provider 的代表案例通过”，不是完整计划验收，也不应作为全部 PM 项已满足的依据。

## 需求到案例映射（PM-01～PM-35）

状态说明：**覆盖**表示已有对应自动化行为断言；**部分**表示仅覆盖子行为或仅由内存契约 fixture 验证；**缺口**表示计划要求的验收证据尚未具备。即使状态为覆盖，也不代表已在真实 Provider 上覆盖所有跨进程边界。

| 需求 | 状态 | 现有案例 / 缺口 |
| --- | --- | --- |
| PM-01 租户/用户/Agent 隔离 | 部分 | `TestFactAgentPrivateScopeBindsAuthenticatedUser`、`TestPostgresAgentPrivateScopeSeparatesUsersOfSameAgent`、`TestPostgresProviderConformance`；租户/用户/Agent 组合矩阵仍需扩展 |
| PM-02 显式共享策略 | 部分 | `TestFactUserSharedScopeCanBeReadWithoutAgentBinding`；真实 Provider 对共享 allowlist 的授权矩阵未覆盖 |
| PM-03 正文与元数据持久化 | 覆盖 | `TestPostgresFactPersistsAcrossStoreRestart` |
| PM-04 持久索引/重建 | 部分 | `TestPostgresRecallAfterIndependentProcessRestart`、`TestPostgresOnlineRebuildSwitchesGeneration`、`TestPostgresRebuildClaimFencesExpiredWorker`；索引丢失恢复与重建故障重启未覆盖 |
| PM-05 并发修订安全 | 覆盖 | `TestFactRevisionCASHasSingleWinner`、`TestPostgresConcurrentRevisionAndBodyFreeChanges` |
| PM-05a 外部实现接口 | 覆盖 | `TestFactExternalPackageCanImplementInterfaces`、`TestFactPublicInterfacesHaveNoUnexportedMethods` |
| PM-05b Provider 契约套件 | 部分 | `TestPostgresProviderConformance`；当前 9 项，尚未覆盖全部持久化/维护契约 |
| PM-05c 能力与生命周期 | 部分 | `TestFactProviderMustDeclareLifecycleCapabilities`；实际关闭/并发能力声明覆盖不足 |
| PM-05d durable 召回快照 | 部分 | `TestFactBuilderDurableReplayPinsSnapshotAndRejectsChangedFact`、`TestFactPreStepJournalDoesNotPersistFactBody`；真实 PG+Run 跨进程组合未覆盖 |
| PM-05e 响应缓存隔离 | 覆盖 | `TestFactBuilderInjectsOnlyCurrentRequestAndBypassesCache`、`TestFactBuilderRejectsMissingPrincipalBeforeCache` |
| PM-06 来源信息 | 部分 | Provider conformance 和 body-free changes 检查来源引用；Session/文档/人工来源完整字段矩阵未覆盖 |
| PM-07 修改事实 | 部分 | `TestFactSnapshotInvalidatesAfterUpdate`、Provider CAS；PostgreSQL 更新来源关系的完整案例缺失 |
| PM-08 替代事实 | 覆盖 | `TestFactSupersedeLeavesOneActiveFact`、PostgreSQL conformance supersede 案例 |
| PM-09 失效时间 | 部分 | conformance 用短 TTL 验证 Get/List；真实 PG TTL 边界和异步清理尚未覆盖 |
| PM-10 删除事实 | 部分 | `TestFactDeleteImmediatelyBlocksHydration`、`TestPostgresFactPersistsAcrossStoreRestart`、向量 tombstone 案例；过期向量竞态矩阵未覆盖 |
| PM-11 按查询召回 | 部分 | 召回单测及 `TestPostgresRecallAfterIndependentProcessRestart`；类型/状态组合筛选矩阵需补充 |
| PM-12 查询筛选与限制 | 部分 | `TestPostgresListCursorPersistsAndBindsFilters`；空查询、非法数量、类别过滤组合未穷尽 |
| PM-13 模型调用前注入 | 覆盖 | `TestFactBuilderInjectsOnlyCurrentRequestAndBypassesCache` |
| PM-14 注入预算 | 覆盖 | `TestFactRecallAlwaysStillHonorsBudgetAndTTL` |
| PM-15 与策略分离 | 部分 | Builder 将事实写入独立低权限 user 数据消息；模型适配器 system 合并矩阵仍需验证 |
| PM-16 明确失败策略 | 部分 | `TestFactBuilderRejectsMissingPrincipalBeforeCache`、失败不调用模型案例；各后端策略组合未穷尽 |
| PM-17 单条查询与列表 | 部分 | `TestPostgresListCursorPersistsAndBindsFilters`、`TestPostgresListCursorKeepsUnseenFactAfterUpdate`；跨主体 ID 不可枚举矩阵未穷尽 |
| PM-18 纠正与替代 | 部分 | Service CAS/supersede 案例；持久 Provider 修改/来源链组合需补充 |
| PM-19 单条与范围删除 | 部分 | `TestPostgresScopeClearBlocksWritesAndResumesWorker`、`TestPostgresScopeClearResumesAfterStoreRestart`；全授权边界矩阵未穷尽 |
| PM-20 有界审计 | 部分 | `TestPostgresConcurrentRevisionAndBodyFreeChanges`、删除后正文清理断言；来源操作者/时间所有动作矩阵未穷尽 |
| PM-21 运行观测 | 部分 | `TestFactServiceEmitsBodyFreeOperationEvents`、Observer panic 隔离案例；无 exporter/队列指标/仪表盘 |
| PM-22 事实与索引一致 | 部分 | outbox retry、tombstone、独立进程召回案例；持续故障重试后重建恢复未覆盖 |
| PM-23 context 取消与超时 | 部分 | Builder retrieval cancellation 案例；PG Store/Embedder/Index 取消压力测试未覆盖 |
| PM-24 会话与模型上下文分离 | 覆盖 | `TestSessionLog*`、`TestFactBuilderDurableReplayPinsSnapshotAndRejectsChangedFact` |
| PM-25 Durable Run 稳定性 | 部分 | durable journal 重放固定 SnapshotRef 案例；真实 PG + durable Run + 进程恢复组合缺失 |
| PM-26 缓存安全 | 覆盖 | `TestFactBuilderInjectsOnlyCurrentRequestAndBypassesCache` 验证 no-read/no-store |
| PM-27 写入幂等 | 部分 | Service 幂等生命周期、`TestPostgresIdempotencyExpiresToDigestOnlyTombstone` 和过期输入重放案例；并发同键矩阵仍未穷尽 |
| PM-28 共享作用域 | 部分 | private/shared scope 单测；拒绝未知模式及 PG allowlist 矩阵需补充 |
| PM-29 Embedding Profile | 部分 | online rebuild 与 profile snapshot invalidation 案例；旧/新 Embedder 故障及恢复矩阵未覆盖 |
| PM-30 召回选择策略 | 覆盖 | 阈值、always 超普通分页、TTL 与预算召回案例 |
| PM-31 生命周期安全幂等回放 | 部分 | 删除、过期 tombstone、撤权/更新相关单测；PG 完整组合矩阵未覆盖 |
| PM-32 索引维护与恢复 | 部分 | 重试退避、授权 retry、全局 cutoff/并发写双写和切换、过期 worker fencing 案例；后端持续故障、重建 crash restart 缺失 |
| PM-33 作用域清空生命周期 | 部分 | 即时不可见、阻写、Store 重启恢复、不同幂等键并发合并案例；并发写全边界和 full-outbox 清理压力未覆盖 |
| PM-34 有界变更链 | 部分 | body-free changes、签名绑定游标案例；actor/source/supersedes 全字段矩阵需补充 |
| PM-35 输入边界与背压 | 部分 | `TestPostgresOutboxBackpressureIsAtomicAndReservesDeleteCapacity` 验证原子背压及删除保留容量；默认正文/元数据/worker 边界组合仍需补充 |

该表用于明确后续测试工作，不将“部分”包装为验收通过。最优先待补的组合验收是原子容量背压、真实持久 Provider 的取消/故障恢复，以及真实 PG + Builder + durable Run + 撤权/清空的一条端到端业务旅程。
