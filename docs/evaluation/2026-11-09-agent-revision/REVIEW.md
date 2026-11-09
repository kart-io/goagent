# Agent Revision 窄范围复核

日期：2026-11-09。状态：P1 已修复并通过最终专项 race 验证。首次只读复核确认的“预检成功后 Build 前置依赖失败会终结原等待任务”已由显式安全重试契约与事务恢复处理；具体闭环证据见下节。身份链与预检门禁未发现新的阻断问题。

## 修复闭环与最终验证

本次修复范围为 `management/build_retry.go`、`worker.go`、`task.go`、`adapters/archived.go`，专用测试为 `build_retry_test.go` 与 `adapters/archived_retry_test.go`。`regression_test.go` 仅同步三处内部 execute 调用参数，`adapters/archived_test.go` 同步失败 Binding 保留 Runtime 的断言；没有修改主审的 `management/revision_test.go`，没有提交或运行全量测试。

- 新增 `RetryableBuildError{Err: err}`：宿主明确保证本次 Build 未执行任务、无业务副作用且资源已清理；不代表此前 attempt 无副作用。ArchivedFactory 仅在尚未进入宿主 Build 的普通校验/物化失败、并且清理成功时自动标记。panic、普通宿主 Build 错误、空 runtime 不自动标记；宿主可显式作出该保证。
- worker 只在 Build 返回标记、Runtime 为空且 Cleanup 成功时阻塞原命令。清理失败继续上报且否决重试，Inspect/Run 返回同类标记也不会触发构建重试。adapter 失败返回时保留宿主 Runtime 是否存在的事实，防止 Runtime + marker 被清空后绕过 worker 门禁；错误返回后的 Runtime 仅供判断，不能执行。
- claim 前保存 TaskState/RecoveryNeeded。恢复事务读取最新 Task，恢复这两个执行字段，保留并发取消、修订号、等待点与版本身份；原 Command 置 blocked、Attempt 结束并记 blocked、非终态 Session 继续由原任务占用。原输入/审批决定和命令 ID 不变，恢复依赖后自动继续。
- 三处取消门禁统一为 `Queued/WaitingInput/WaitingApproval && !RecoveryNeeded`。满足条件的并发取消可在失败构建收尾事务直接完成；旧执行待核对或 Attention 不提前终结。Attention 不因本次未执行而凭空增加 RecoveryNeeded。没有已接纳恢复命令的未知结果取消明确返回 Conflict，保持 CancelRequested=false，让公开 Resume/人工核对入口仍可用。
- blocked 命令采用 1 秒退避；Attempt 编号按现有 `(CreatedAt, ID)` 游标分页取历史最大 Number + 1，避免持续故障时截断计数导致重复编号。没有增加数据库字段或更改 Store 接口。

最终执行并通过：

```text
go test -race ./management ./management/adapters -run 'Test(BuildRetry|Archived|ReconciliationEvidencePersistedAndApplied|ReviewUnknownEvidence|CompletedEvidenceWinsRecoveredCancel)' -count=1
ok  github.com/kart-io/goagent/management           5.466s
ok  github.com/kart-io/goagent/management/adapters  1.087s
```

关键回归证据：

| 测试 | 证明范围 |
| --- | --- |
| TestBuildRetryArchivedResumeKeepsTaskAndCommand / input、approval | 预检成功后实际归档 Load 失败；不调用宿主 Build/Inspect/Run，保留原等待、Task/Run/ExecutionKey/RevisionID、Session 占用、命令和输入/审批；恢复后同一命令完成，不产生 task.failed。 |
| TestBuildRetryGenericErrorAndUnsafeCleanupDoNotRetry | 普通 Build 错误、清理失败、返回 Runtime、Run 阶段 marker 均不自动重试；包含真实 ArchivedFactory → worker 的 Runtime + RetryableBuildError 场景。 |
| TestBuildRetryCancelRacePreservesPriorRecovery | 失败构建中的取消不覆盖最新任务字段；无旧执行责任时直接取消；有 RecoveryNeeded 时保留责任，第二次取消仍需核对，旧 completed 证据优先且不重新 Run。 |
| TestBuildRetryAttentionCancelStillInspectsCompletedEvidence | 真实 unknown evidence 产生无 RecoveryNeeded 的 Attention；无恢复命令的取消被拒绝且不封死 Resume；接纳核对命令后 Build 失败+取消及再次取消保留 Attention，最终 Inspect 的 completed 证据优先。 |
| TestBuildRetryAttemptNumbersPageAndPreserveGaps | 201 条跨页历史、相同时间戳与不连续编号；最大值 10001 后新增 10002。 |
| TestArchivedRetryValidationBoundary / MaterializationFailure / HostBuildBoundary | 自动标记仅限安全前置阶段，panic/普通宿主失败不自动标记，显式宿主保证保留，失败资源清理与并发重复清理正确。 |

已知边界：宿主未显式保证安全的 Build 错误仍沿用既有失败/停机策略，不能据此宣称所有 runtime 故障都能自动续跑。显式保证是受信任宿主契约。1 秒退避控制重试频率，不限制历史记录总量；分页编号不改变既有保留策略。完整全量 race/build/vet/lint 由主审统一验证，本报告不把修复落盘前的全量结果算作最终补丁证据。

## 范围与证据

只读检查当前工作区的 `management/types.go`、`agent.go`、`session.go`、`task.go`、`worker.go`、`revision.go`、`revision_test.go`。为确认恢复与 Run 展示路径，仅定点读取 `recovery.go` 的 `retryUnconfirmed` 和 `query.go` 的 `GetRun`，未扩展为既有 worker 历史或安全审计。

首次只读复核仅写报告，未修改代码或运行测试。主审当时报告真实 input/approval 等待、停止 worker、发布 v2、重开 archive/service、旧新会话与 Fork 测试 PASS；复核已阅读对应 `TestAgentRevisionSurvivesDeploymentAndBindsSessions`，但未独立重跑。后续修复与新增专项验证如上。以下问题路径和行号保留为首次复核的历史证据，不代表修复后行为。

## 原始问题（已关闭）：P1 — Build 前置依赖失败会不可恢复地终结等待任务

**位置：** `management/worker.go:638` 调用 Build，`:646` 返回错误，`:899` 默认完成分支，`:935` 消费命令。

**触发条件：** input/approval 等待任务已有 resume 命令；`start` 的 `validateFactory` 成功，但其后 Build 重新读归档失败，或者宿主 loader 构建固定程序所需的运行环境不可用。例子包括归档存储暂时不可读、归档句柄关闭、宿主 runtime 在 Check 后不可用、私有物化目录分配失败。这里必须区分：同一个 `ArchivedFactory` 的注册表是私有副本，不能正常热删其中的 loader；真实窗口主要是归档及外部运行环境可用性变化，不是注册表自然漂移。

修复前确定路径：

1. `worker.go:364` 预检通过。
2. `worker.go:482` 起的事务将任务设为 Running、清除 RecoveryNeeded、占用 Session、创建 Attempt，并将命令改为 applying。原等待点尚在，但任务的原状态未保存在用于回滚的参数中。
3. `worker.go:638` 的 Build 返回错误。`:646` 仅设置 `runErr`，`result.Outcome` 保持空值；尚未调用 Runtime.Inspect 或 Runtime.Run。
4. 在 worker 正常运行、没有 shutdown 的情况下，`finish` 的默认分支将任务改为 Failed，错误类型通常为 `execution`（`:899–909`）。
5. 终态处理释放 Session、结束任务、失效尚未决定的审批并产生 task.failed 投递；命令成为 applied，Attempt 结束（`:912–951`）。已批准/拒绝的审批不会被重新置为 pending，但其 resume 命令已被消费。

**可恢复程度：** 恢复同一归档/程序、补齐 loader 或重启 worker，都不能通过现有正常流程继续这个 Failed 任务。`ResumeTask` 在 `task.go:197` 拒绝终态；`DecideApproval` 在 `task.go:152` 拒绝终态；启动恢复只扫描 Running/Canceling 及 applying 命令（`worker.go:180,201`）；`retryUnconfirmed` 也不会重启终态任务。等待点可能仍留在 Task 记录里，但这不代表可继续。重新 SubmitTask 可以创建新任务，却会使用新的 TaskID/RunID/ExecutionKey，不能当作原任务续跑，更不能保证此前操作不会重做。人工直接修改存储不属于公开恢复能力。

**例外：** 若此时 worker 的 parent context 已取消，`finish` 在 `worker.go:900` 对空 outcome 走 Queued + RecoveryNeeded，命令恢复 pending。这解释了正常停机后重启可以成功，也说明停机测试 PASS 不能覆盖 worker 仍在运行时的依赖故障窗口。若进程在 finish 提交之前崩溃，启动恢复也可能保留任务；不能依赖这种时序作为产品恢复方式。

**建议：需要修复。** 归档修订承诺旧任务可恢复时，运行前的暂时依赖不可用不应产生业务终态。应保留同一 Task/Run/ExecutionKey、RevisionID、Wait/Epoch、输入或审批决定，以及原 resume 命令，使精确依赖恢复后能继续。实现可以将此类失败纳入现有 blocked/pending 恢复路径，但必须明确发生在 Runtime.Inspect/Run 前，并在事务中处理任务、命令、Attempt 与 Session 占用的一致性。

不要直接把所有 `Factory.Build` 错误都无限自动重试：宿主 Build 的资源和外部副作用语义目前没有统一的“可重试且未执行”保证。至少应能区分适配器加载/校验/物化阶段的依赖失败与已进入宿主构建的失败；后者若无法证明安全，应保留明确的非终态人工处理入口。也不宜只将 outcome 改为 attention：没有等待点的新任务可能没有可用 Resume 路径，已决定的审批命令也不会因 attention 自动恢复。修复需要明确原命令如何重新可执行。

建议新增一个确定性窄测试：建立真实等待任务并提交输入/审批决定，让预检成功，随后仅让本次 Build 的归档 Load 或宿主依赖检查失败一次，确认 Inspect/Run 未调用；恢复依赖，验证原 TaskID、ExecutionKey、RevisionID、等待点及决定保留，原命令能继续一次且不生成 task.failed。至少覆盖 input 与 approval；不需要全量测试。

## 已核对的身份与门禁行为

| 检查点 | 当前结果与依据 |
| --- | --- |
| Version 内容身份 | `agent.go:41` 保存 RevisionID；ConfigHash 在原 RunnerKind、ImplementationVersion、规范 Config 基础上再绑定 RevisionID。归档中的提示词、资源、runtime 变化会改变恢复身份。同名版本仍通过 insert 保持不可变。 |
| 保存与发布 | `agent.go:49,78` 均调用 validateFactory。发布只更新 Agent 的 PublishedVersion，不改已保存的 Session/Task。 |
| 普通 Factory | `revision.go:25` 仅对没有 RevisionID 的旧版本沿用 Validate；有 RevisionID 必须实现 RevisionFactory，否则拒绝。Save/Publish 的 wrapInvalid 会统一外层业务错误，这不影响拒绝结果。 |
| 不一致归档 | `revision.go:36` 对宿主返回快照再次 Verify；`:39` 检查规范 Config、快照 ID、Runtime.Version 与存储 Version。伪造或不一致的 ValidateRevision 返回值不能直接通过门禁。Version.Config 来自 SaveVersion 的 CanonicalJSON，所以这里只规范化快照 Config 的比较在正常写入路径下成立。 |
| 创建会话 | `session.go:31` 读取显式版本或当前已发布版本，复制该 Version.RevisionID。无需依赖随后再次发布的版本。创建会话本身不检查 runtime 当前是否可用；执行门禁负责阻塞，这是当前异步行为，不是身份错配。 |
| Fork | `session.go:104` 检查源 Session 与不可变 Version 的 RevisionID 一致，子会话复制源版本及 RevisionID，没有选择当前发布版本。 |
| Submit / Run 展示 | `task.go:40` 从 Session 复制 AgentID、Version、RevisionID；GetRun 直接返回存储 Task，所以 run.Task.RevisionID 保留原身份。 |
| 执行前身份 | `worker.go:413` 在改为 Running 前检查 Task/Session 的 AgentID、Version 和 Task/Session/Version 的 RevisionID 一致；`:419` 再拒绝普通 Factory。没有找到通过正常 API 把旧会话切换到新 revision 的路径。 |

宿主 RevisionFactory 仍是受信任实现：核心能核验 ValidateRevision 返回的内容，不能从通用 Binding 推导程序实际身份。精确 runtime 注册、归档克隆及物化由 ArchivedFactory 承担；本轮没有要求核心为任意恶意宿主 Build 提供证明。

## 预检失败是否保留等待任务

答案是保留。`worker.go:364` 的 validateFactory 失败发生在任务状态与 Attempt 写入前，转入 blockStart，只将 Command 改为 blocked。Task 继续保有 WaitingInput/WaitingApproval、原 Waits/WaitEpoch 与 RevisionID；已提交的输入留在命令中，审批决定留在 Approval/Command 中。`poll` 查询 pending 和 blocked（`:232`），恢复精确依赖后会重新尝试。loader 注册表不可热变时，需用包含旧 loader 的 factory 重建 service/worker；归档等外部依赖恢复则可由现有轮询继续。

预检目前把所有 validateFactory 错误映射为 Unsupported，并显示 `implementation_missing`。因此内容损坏、配置冲突和真实缺少实现都能保留任务，但无法仅凭 command.ErrorKind 区分原因；这属于诊断限制，不另列为本轮阻断问题。永久不一致不会被“修好”为其他 revision，也不会回退 latest，而是继续拒绝。

后续缺失 loader 阻塞恢复测试可证明这一条路径；它不能替代上面的“预检通过后 Build 才失败”测试。
