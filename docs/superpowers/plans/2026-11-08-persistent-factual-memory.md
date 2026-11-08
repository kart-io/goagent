# 持久事实记忆服务实施计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**目标：**在 GoAgent 核心交付公开、可扩展、可持久化的事实记忆服务，支持安全召回、用户管理、索引运维与宿主可验证的生命周期。

**架构：**在 `memory` 包提供高层 `MemoryService`、默认组合实现及可替换的 FactStore/MemoryIndex/Embedder；Builder 通过可信 scope resolver 在模型请求前召回。持久 outbox worker 与 `MemoryMaintenance` 提供索引重试、重建、状态查询及作用域清空恢复；宿主负责认证授权、注销触发和 HTTP/UI。

**技术栈：**Go 1.27、现有 GoAgent memory/Builder/session/durable/provider 抽象、Go 标准库和项目已有测试依赖。核心不绑定数据库、向量供应商或 HTTP 框架。

**规格：**[需求](../specs/2026-11-08-persistent-factual-memory-requirements.md)、[架构设计](../specs/2026-11-08-persistent-factual-memory-design.md)。

## 全局约束

- 公开接口不得包含未导出方法；外部 Go package 能独立实现并注入服务或底层 Provider。
- Tenant/User/Agent 身份来自可信宿主 context；默认私有隔离，跨 Agent 共享需要 resolver 明确授权。
- 事实正文以持久 FactStore 为真源；Index 可重建，正文、embedding、凭据和完整查询不进入日志或删除审计。
- 事实不得进入 system policy、SessionLog、Handle transcript 或 durable journal 正文；Builder 仅在当前模型请求添加低权限 user 数据消息。
- 启用事实记忆时缓存必须 no-read/no-store；不支持旁路的外部缓存不能组合使用。
- 初始默认建议：正文 64 KiB、规范化 Metadata 16 KiB、页大小默认 50/最大 200、Worker 并发默认 4/最大 32、tenant outbox 100,000；这些是可配置默认/Provider 能力，不是外部实现的统一硬上限。普通写入受背压，删除和清空始终保留逻辑生效及清理容量。
- 游标绑定 tenant、Principal、scope/filter 和排序版本；起始高水位限定成员而非正文快照，每页按当前权限 hydrate，过期/篡改/跨查询复用明确失败。
- `IndexGeneration`、per-Fact `MutationVersion`、全局 `StoreSequence`、`EmbeddingProfileID` 分别管理；任何一个都不作为缓存隔离版本。
- 默认 strict；授权、撤权、快照失效和一致性错误不可由 best-effort 吞掉。

## 审查重点

1. 常驻偏好不能绕过授权、TTL、状态或预算；普通语义结果低于阈值时为空，归 T4。
2. Save 成功后删除并重试幂等键，不能泄露旧正文、复活事实或重复创建，归 T1。
3. outbox 重试耗尽、进程重启、后端修复后可经公开维护入口恢复，归 T3。
4. Scope 清空期间的并发写、崩溃恢复与完成判定无竞态，归 T2/T3。
5. 变更分页不含历史正文、游标不能跨主体/过滤器使用，归 T2。
6. 容量上限在事实/outbox 同事务中生效，超限不会部分提交，归 T0/T1。
7. durable 恢复固定 SnapshotRef；删除、TTL 到期、撤权或 profile 变化时拒绝旧 Run，归 T4。
8. 记忆回答不能读写共享响应缓存，且不污染会话历史，归 T4。

## 文件职责

| 文件/目录 | 职责 |
| --- | --- |
| `memory/fact_types.go`、`fact_errors.go`、`fact_limits.go` | 公开事实、scope、来源、revision、游标、维护状态及稳定错误/限额 |
| `memory/fact_service.go`、`fact_store.go`、`fact_index.go`、`fact_embedder.go` | 高低层公开接口与默认 service 编排 |
| `memory/fact_outbox.go`、`fact_worker.go`、`fact_maintenance.go` | 持久 mutation/维护状态/重建/清空推进；另需实现者提供可重启 Provider fixture |
| `memory/fact_recall.go`、`fact_snapshot.go`、`fact_changes.go` | 语义和常驻召回、durable 快照、变更链查询 |
| `memory/fact_*_test.go` | 内存契约 fixture、生命周期、并发、容量和召回单测 |
| `memory/conformance/` | 外部 Provider 可调用的接口一致性测试套件 |
| `builder/fact_memory.go`、`builder/fact_memory_test.go` | Builder option、scope preflight、cache bypass、模型请求注入 |
| `builder/durable_fact_memory.go`、相关 durable 测试 | SnapshotRef 的 durable 固定、恢复与失效处理 |
| `examples/fact-memory/`、`memory/README.md`、`builder/README.md` | 外部 Provider 示例、运行方式和边界说明 |
| `docs/evaluation/persistent-factual-memory/` | 案例运行记录、持久 Provider 验收报告 |

最终文件拆分可依现有 package 习惯合并相邻小文件，但不得丢失职责和测试边界。

## 依赖顺序

`T0 → T1 → T2/T3 → T4 → T5 → T6`。T2 与 T3 可在 T1 接口冻结后独立实现；T4 依赖核心 service/store/index，T5 依赖全部公开接口与 T0 选定的真实 Provider；T6 依赖端到端验收。

### Task 0: T0：冻结公开契约、资源边界与错误语义

**文件：**新增 `memory/fact_types.go`、`fact_errors.go`、`fact_limits.go`、`fact_contract_test.go`、`fact_external_test.go`、`docs/feasibility/persistent-factual-memory-provider.md`。

**接口输出：**`MemoryService`、`FactStore`、`MemoryIndex`、`Embedder`、`EmbedderResolver`、`MemoryScopeResolver`、`MemoryMaintenance` 及请求/结果 DTO。冻结 `RecallClass`（`normal`/`always`）、操作收据、变更记录、清空操作、Worker 状态和游标字段。

- [x] 新增 `TestFactPublicInterfacesHaveNoUnexportedMethods`、`TestFactExternalPackageCanImplementInterfaces`、`TestFactDefaultsAndProviderCapacity`。
- [x] 定义公开方法的精确 Go 签名；`MemoryService` 增加 `ListChanges`，维护接口提供 `Run/Status/Retry/ClearStatus/StartRebuild/RebuildStatus`；FactStore 持久实现 `ListChanges`、维护状态/重试、`BeginScopeClear(ClearAccess,targetScope)/GetClearOperation/ClaimClearOperations/AckClearOperation`、重建进度和 ActiveGeneration 原子切换。
- [x] 定义稳定错误：授权/越权、RevisionConflict、IdempotencyConflict、IdempotencyExpired、Deleted/Gone、SnapshotInvalidated、CapacityExceeded、InvalidCursor、MaintenanceNotFound；索引待处理作为结果状态而非成功索引承诺；所有错误可通过 `errors.Is/As` 检查且不带事实正文。Save 重放对更新/替代/过期返回原不可变收据；删除返回 Gone；撤权返回授权错误；过期幂等键由永久最小 digest tombstone 拒绝，不保留正文。
- [x] 记录初始默认：页 50/最大 200、正文 64 KiB、Metadata 16 KiB、Worker 并发 4/最大 32、tenant outbox 100,000；明确它们是可配置建议值与 Provider 能力边界，不是统一硬上限。所有 byte 和计数单位写入 GoDoc。
- [x] 定义 Cursor 签名与绑定字段；首请求高水位仅限定候选成员，不是正文快照；每页按当前授权/生命周期 hydrate，删除/撤权项可消失，原位更新返回当前 revision，后续成员不重复。游标有效期用可注入时钟测试。
- [x] 查明现有仓库及用户可用的真实持久 Provider；在可行性记录中选定独立可选模块 `examples/fact-memory/postgres` 作为真实 Postgres Provider 的实现路径，复用项目已使用的 pgx v5.10.0 与 GoAgent 1.27 工具链；该模块不进入核心 go.mod，实际 Provider 由 T5 实现。未完成跨进程重启验收前不得宣称持久化验收完成。
- [x] 运行 `go test ./memory -run 'TestFact(PublicInterfaces|ExternalPackage|DefaultsAndProviderCapacity)'`，预期新增用例 PASS，外部 package fixture 独立编译。

### Task 1: T1：实现事实 Store、幂等与一致性 outbox

**文件：**新增 `memory/fact_service.go`、`fact_store.go`、`fact_outbox.go`、`fact_service_test.go`、`fact_store_contract_test.go`。

**接口：**消费 T0 的 DTO/接口；产出保存、读取、列表、更新、替代、删除、作用域清空的默认服务语义和 Store transaction contract。

- [x] 新增 `TestFactSaveIdempotentSameRequest`、`TestFactIdempotencyRejectsDifferentRequest`、`TestFactSaveDeleteRestartReplayDoesNotLeakOrResurrect`、`TestFactExpiredIdempotencyTombstoneSurvivesGCAndRestart`。
- [x] 新增 `TestFactRevisionCASHasSingleWinner`、`TestFactSupersedeLeavesOneActiveFact`、`TestFactDeleteImmediatelyBlocksHydration`。
- [ ] Fact、幂等操作引用/请求摘要、append-only ChangeRecord 和 IndexMutation outbox 原子提交；ApplyFact 返回不含正文的不可变 MutationReceipt 与当前 Lifecycle；幂等记录不得存正文，重放时重新授权和解析当前事实生命周期。更新/替代/过期回放只返回原不可变收据；删除返回 Gone；撤权返回授权错误。幂等原结果有配置保留期；期限后清除原结果正文，只保留最小 digest tombstone 并返回 IdempotencyExpired，不能复用键。
- [ ] 事实写入与 outbox 必须同事务容量检查；普通写入超出 tenant outbox 容量时整笔拒绝，不能只写 Fact 或只写 mutation；Delete/DeleteScope 走独立保留容量并立即逻辑失效。将 IndexState=Pending 作为持久结果；全局 StoreSequence 与 per-Fact MutationVersion 分别分配，旧 mutation 可 Obsolete 终结。
- [ ] Store contract 覆盖精确 scope 查询、ListChanges、revision CAS、租约领取、MutationVersion ack、Obsolete 终态、tombstone 水位、幂等摘要期限和跨重启恢复；外部适配器可在自己的持久实现满足同一行为。
- [ ] 运行 `go test -race ./memory -run 'TestFact(Save|Idempotency|Revision|Supersede|Delete|Outbox)'`，预期并发、重试和删除恢复案例 PASS。

### Task 2: T2：实现作用域管理、清空恢复与变更链查询

**文件：**新增 `memory/fact_clear.go`、`fact_changes.go`、`fact_clear_test.go`、`fact_changes_test.go`。

**接口：**使用 T1 的 Store transaction/outbox；产出 `DeleteScope` 清空操作与 `ListChanges` 变更查询。

- [ ] 新增 `TestFactClearScopeHasImmediateVisibilityBoundary`：清空事务写 epoch/cutoff 后，范围内既有事实立即不出现在 Get/List/Recall。
- [ ] 新增 `TestFactClearRejectsWritesUntilComplete`、`TestFactClearResumesAfterRestart`、`TestFactClearConcurrentSaveHasNoGap`、`TestFactClearRequiresClearCapabilityAfterWriteRevocation`、`TestFactDeleteAndClearSucceedAtFullOutbox`：清理期间拒绝新写，崩溃后继续清理，最终 Complete 后才恢复写入。
- [ ] 新增 `TestFactChangesTrackActorSourceAndSupersession`、`TestFactChangesNeverExposeHistoricalBody`、`TestFactChangesCursorBindsScopeAndFilter`。
- [ ] Change 包含 revision、action、actor、时间、来源引用和 supersedes 关系，不持有历史正文；主体只能查询其授权范围。Store 持久化清空任务的 claim/progress/ack 状态；Maintenance Worker 领取并推进，Service 负责通过独立 ClearAccess 发起 DeleteScope；宿主先封禁普通写权限再授予目标 scope 的清空权。
- [ ] 宿主注销的规范调用顺序写入 API 文档：先封禁该 scope 的新写入权限，再 DeleteScope，轮询 ClearOperation 到 Complete；Session 删除/Fork 不隐式删除或复制事实。
- [ ] 运行 `go test -race ./memory -run 'TestFact(Clear|Changes|DeleteAndClear)'`，预期并发边界、恢复、审计隐私和游标案例 PASS。

### Task 3: T3：实现索引 Worker、维护接口与容量背压

**文件：**新增 `memory/fact_worker.go`、`fact_maintenance.go`、`fact_maintenance_test.go`。

**接口：**消费 T0 `MemoryMaintenance` 和 T1 outbox；产出运行/状态/重试/重建能力供宿主与 T5 Provider 使用。

- [ ] 新增 `TestFactWorkerClaimsLeasesAndAcksMutationVersion`、`TestFactWorkerRetriesAndStopsAtLimit`、`TestFactMaintenanceStatusIsScopedAndPaged`。
- [ ] 新增 `TestFactMaintenanceRetryRestoresRecall`、`TestFactRebuildSwitchesGenerationAtomically`、`TestFactRebuildUsesGlobalStoreSequenceAcrossFacts`、`TestFactRebuildResolvesOldAndNewEmbedders`、`TestFactWorkerResumesPendingAfterRestart`。
- [ ] `Run(ctx)` 使用有租约、幂等 Apply、按 Provider 能力配置的有限并发；失败状态包含安全诊断码、尝试数与下次重试时间，不含正文/embedding。
- [ ] `Status` 按授权 scope 分页；`Retry` 仅允许对可见 Failed mutation 操作；`StartRebuild` 只支持全 MemoryService 全局代际重建，要求独立 MaintenanceAccess；持久化全局 StoreSequence cutoff/Building 状态，旧代际继续服务；EmbedderResolver 同时解析旧、新 Profile；扫描旧事实并双写 cutoff 后 mutation，以 StoreSequence 追平后原子切换 ActiveGeneration。Fact 不保存当前 generation。失败保留旧代际，重试可继续；Delete tombstone 同步应用到两代。
- [ ] 新增 `TestFactCapacityRejectsOversizedContentMetadataAndWorkerLimit`、`TestFactOutboxBackpressureIsAtomicAndRecovers`。
- [ ] 运行 `go test -race ./memory -run 'TestFact(Worker|Maintenance|Rebuild|Capacity|OutboxBackpressure)'`，预期重试耗尽、恢复、全局重建双写追平/切换、旧 mutation Obsolete、删除清理保留容量和背压案例 PASS。

### Task 4: T4：实现受控召回、Builder 注入和 durable 快照

**文件：**新增 `memory/fact_recall.go`、`fact_snapshot.go`、`fact_recall_test.go`、`builder/fact_memory.go`、`builder/fact_memory_test.go`、`builder/durable_fact_memory.go` 及相关 durable 测试。

**接口：**消费 T0–T3 的服务/Resolver；产出 Builder 每请求注入、缓存旁路和 durable SnapshotRef 恢复。

- [ ] 新增 `TestFactRecallRequiresSimilarityThreshold`、`TestFactRecallAlwaysClassSurvivesUnrelatedQuery`、`TestFactRecallAlwaysStillHonorsBudgetAndTTL`。
- [ ] `normal` 事实低于配置阈值不返回；将 Index score 统一规范到 [0,1] 且越大越相关，未能转换距离分值的 Provider 不得启用该阈值策略；`always` 常驻事实按 Priority 和固定顺序选取，但仍严格过滤 scope/status/TTL，合并后统一遵守条数和 token/字符预算。
- [ ] 新增 `TestFactBuilderInjectsOnlyCurrentRequest`、`TestFactBuilderRejectsMissingPrincipalBeforeCache`、`TestFactBuilderCacheBypassPreventsReadAndStore`。
- [ ] 记忆事实仅放入临时低权限 user 数据消息；检查 Gemini/Anthropic 等 system 合并路径不接收事实；session log、Handle transcript 与 durable 正文无注入文本。
- [ ] durable journal 固定 SnapshotRef 引用；恢复时依据当前授权解析相同 facts，已删除/过期/修改/撤权/profile generation 变化返回 `ErrMemorySnapshotInvalidated`。
- [ ] 运行 `go test -race ./memory ./builder -run 'Test(FactRecall|FactBuilder|FactDurable)'`，预期语义门槛、常驻偏好、缓存 no-read/no-store、会话隔离和恢复用例 PASS。

### Task 5: T5：提供外部实现者 conformance 套件与持久 Provider 示例

**文件：**新增 `memory/conformance/`、`memory/conformance_test.go`、`examples/fact-memory/`，以及 `examples/fact-memory/postgres/` 独立 Go module（依赖 pgx v5.10.0，不加入核心 go.mod），作为真实可重启 Provider。

**接口：**外部实现者只需提供公开 `MemoryService` 或 Store/Index/Embedder 测试 fixture，即可运行同一套行为验证。

- [ ] 新增可从 external package 调用的 `conformance.Suite`，不得导入私有测试 helper；覆盖 PM-01～PM-35 中 Provider 可观察的持久性、隔离、幂等、更新、删除、TTL、维护、清空、来源和容量语义。
- [ ] 示例实现者从独立 Go package 实现公开接口并注入 Builder；演示授权 scope、保存常驻偏好、列表/变更查询、清空状态和索引维护入口。
- [ ] 持久化验收使用 `examples/fact-memory/postgres` 的真实可重启 Provider；记录精确初始化、清理、跨进程重启命令和环境变量。环境缺失必须失败，不允许 SKIP；T0 的 Provider 选择记录列明 backend 及其非核心依赖；内存 fixture 只用于确定性单元/契约逻辑，不能宣称满足持久恢复。
- [ ] 运行 `go test -race ./memory/conformance/... ./examples/fact-memory/...`，并以模块外 fixture 编译验证无私有依赖。

### Task 6: T6：完成文档、端到端案例和验收报告

**文件：**更新 `memory/README.md`、`builder/README.md`、`docs/README.md`；新增 `memory/integration/acceptance_test.go`、`docs/evaluation/persistent-factual-memory/REPORT.md`、配置/运行说明。

**接口：**组合公开 Service/Builder、持久 Provider、可控 Embedder/模型和真实重启进程。

- [ ] 端到端案例覆盖跨重启偏好召回、用户/Agent 隔离、共享授权、修订冲突、幂等删除回放、作用域清空并发/恢复、来源链分页、outbox 故障恢复、容量背压、durable Run 失效及缓存 no-read/no-store。
- [ ] 提供至少一个不依赖线上模型密钥的完整可运行案例；需要真实 embedding/向量数据库的额外验收标明 Provider 环境变量和非 SKIP 结果。
- [ ] 更新 README 的三类数据边界、身份解析、接口注入、维护 Worker 生命周期、用户/Agent 注销清理顺序、上限默认值、故障恢复和外部扩展指南。
- [ ] 运行 `go test -race -count=1 ./memory/... ./builder/...`（包含 `memory/integration`）、`go test -race -count=1 ./examples/fact-memory/...`、`go vet ./...`；如果既有测试依赖外部服务，记录实际执行与 skip，不能把 skip 计作通过。
- [ ] 在 REPORT 中逐项映射 PM-01～PM-35 到测试名、执行命令和结果；所有持久化/删除/清空和重启案例必须由可重启 Provider 实际验证；T0 未选定 Provider 则项目验收未完成。

## 完成标准

- PM-01～PM-35 每一条都映射到实现任务和自动化验收案例。
- GoAgent 核心仅依赖公开抽象，不绑定某个持久化/向量供应商；外部 Provider 可编译、注入并运行契约套件。
- 服务关闭重建后事实可恢复；作用域、删除、清空、幂等重放、审计隐私和索引运维语义明确且经过持久案例验证。
- Builder 召回尊重相关性阈值与常驻偏好策略，且不泄露到会话记录、durable 正文或响应缓存。
- 文档与验收报告完整，所有限额和游标边界有自动化验证。
