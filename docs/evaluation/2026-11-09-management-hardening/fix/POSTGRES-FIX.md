---
phase: 2026-11-09-management-hardening
fixed_at: 2026-11-09T03:32:34Z
review_path: docs/evaluation/2026-11-09-management-hardening/review/REVIEW.md
iteration: 1
findings_in_scope: 2
fixed: 2
skipped: 0
status: all_fixed
committed: false
full_matrix: delegated_to_main_agent
database_testing: stopped
---

# PostgreSQL 审查修复

PG-WR01 与 PG-H01 已修复。修改位于真实隔离工作树 `/home/hellotalk/code/go/src/github.com/kart-io/.goagent-hardening-20261109-APraZJ/goagent-contrib`，未提交。保留了原 lease 清理未提交补丁，没有回退他人修改，也没有改动 go.mod/go.sum。

**2026-11-09 03:32:34 UTC 已停止全部 DB 测试。** 全矩阵由主代理随后串行执行。仅使用 `/tmp/goagent-review-fix-8cIyLB/validation.env` 指定的专用数据库，每个测试创建并清理独立 schema；没有重启容器、修改全局配置或使用其他数据库/密钥。

## PG-WR01：取消后的租约交付

**状态：** fixed: requires human verification（逻辑变更标记；下述自动化实库回归已通过）。

**源码：** `durable/pgstore/pgstore.go`。

- 每轮获取前检查调用方 ctx；`tryAcquire` 接收调用方 ctx，并叠加 `OpTimeout`，连接池及 scope/lease advisory 等待可以取消。
- 以 `writeAttempted` 区分仅等锁失败与已经尝试 INSERT 的失败。前者没有本次 token 的写入，无需再等待补偿锁；后者保留按 token 的独立期限补偿，包括 INSERT/COMMIT 结果不确定。
- 补偿放在 `tryAcquire` 的事务调用返回后，继续使用上轮 lease advisory 锁、显式 RC 和独立 DELETE 协议；不使用已取消的调用方 ctx。
- COMMIT 成功后，在登记 owners 前再次检查取消。取消则先删除自身 token，再释放 gate；不向调用方交付 unlock。
- 取消错误同时包装 `ErrLockUnavailable` 与调用方的 `context.Canceled` / `context.DeadlineExceeded`，释放 gate 和 holders 计数。

**新增回归：** `durable/pgstore/lock_cancel_test.go`。

1. `TestLockAlreadyCancelled`：没有 Pool 的离线检查，已取消或已过期的 ctx 不启动数据库访问，gate/owners 为空。
2. `TestLockCancellationDuringAdvisoryWait`：公开 Scope.Lock，真实 scope/lease advisory 等待分别覆盖 cancel/deadline。以 `pg_locks` / `pg_blocking_pids` 确认等待后取消；要求在阻塞事务尚未释放时返回，检查错误链、owners/gates/holders/lease 无残留，并立即重试。
3. `TestLockCancelledAfterCommit`：pgx 收到提交结果后通过 QueryTracer 取消，分别覆盖提交成功与 COMMIT 发出后断连，检查错误链和无残留，证明补偿不被调用方取消中断。

原 `TestAcquireUncertainCommitCleanup` 与三个 lease cleanup/锁协议顶层测试继续通过。`lease_cleanup_test.go` 仅为两个内部 tryAcquire 调用增加 ctx 参数，没有删除原覆盖。

## PG-H01：获锁后读取已提交状态

**状态：** fixed: requires human verification（逻辑变更标记；下述自动化实库回归已通过）。

**源码：** `durable/pgstore/pgstore.go`、`scope.go`、`session.go`。

Save/SaveAll 的整个写事务显式使用 READ COMMITTED。scope 共享锁与墓碑查询继续分成独立语句，等待期间提交的墓碑现在可见；墓碑存在时整批拒写，旧快照保持不变。

按要求检查并处理同根路径，均有修复前后的实库证据：

| 路径 | 修复前默认 RR 的实测行为 | 修复后 |
| --- | --- | --- |
| Save、SaveAll | 墓碑已提交后返回 nil | 返回 ErrScopeRemoved，两个快照仍为 before |
| RemoveScope | 另一删除事务在等锁期间提交墓碑，返回 SQLSTATE 40001 | 获锁后幂等处理，保留原墓碑时间 |
| PruneRemoved | 另一回收事务先删除墓碑，等待者返回 SQLSTATE 40001 | 获锁后重新读取，无操作成功 |
| Append、AppendAll、仅比较批次 | 等行锁期间序号变更，返回 SQLSTATE 40001 | 返回 ErrSessionConflict，已提交日志不变 |
| Fork | 等行锁期间来源会话更新，返回 SQLSTATE 40001 | 复制获锁后的完整已提交前缀 |

这些事务统一显式 RC，不改连接/数据库默认隔离级别。RemoveScope 的后续分批删除和 PruneRemoved 的候选列表读取本来就是独立语句/事务，不共享获锁前的长期快照，保持原逻辑。会话路径没有 scope 墓碑检查；此次证实的是同一隔离依赖导致的错误语义问题，没有将其描述为绕过 fencing 或已发生数据丢失。

**新增回归：** `durable/pgstore/isolation_test.go` 中三个顶层测试，每条路径均有默认 RC/RR 对照；不靠 sleep 推断等待。会话前序写入使用真实 appendPlan 和真实事务，公开 Append/AppendAll/Fork 作为被测入口。

## 验证证据

所有 Go 命令均用 `env -u GOROOT`，通过已有 `scripts/with-local-modules.sh` 和 `GOAGENT_CORE_ROOT=/home/hellotalk/code/go/src/github.com/kart-io/goagent` 绑定当前核心。脚本本身无执行位，使用 `bash` 调用；未修改权限。载入专用环境后对子命令移除 `GOAGENT_PGSTORE_CONTAINER`，并使用精确测试名单，防止运行需要重启/全局设置的辅助测试。

| 日志（均在 `/tmp/goagent-review-fix-8cIyLB/`） | 结果 |
| --- | --- |
| `pg-offline-build.log` | 新用例编译通过；未配置 DSN 时实库用例跳过，仅作离线编译证据 |
| `pg-baseline-repro.log` | 修改源码前的 race 复现；五个新顶层用例按预期失败，隔离测试 RC 对照通过、RR 失败 |
| `pg-fixed-race.log` | 九个定向顶层测试，`-race -count=3` 全通过，117.888s，无跳过或 race 报告 |
| `pg-existing-write-regression.log` | 八个原写入/批量回滚/fencing 顶层测试，`-race -count=1` 全通过，10.550s |
| `pg-offline-cancel.log` | TestLockAlreadyCancelled，离线 `-race -count=20` 全通过 |
| `pg-vet.log` | `go vet ./...` 成功，空输出 |

修改后均重读相关片段，`gofmt` 检查与 `git diff --check` 通过。当前工具没有 Read/Edit/Write、desktop-commander、sequential-thinking、context7；使用本地源码读取、apply_patch、现有 pgx 模块源码与本地测试完成等价检查，未增加仓库外部依赖或额外工作流文件。

实库定向验证的可复用命令（在 contrib 工作树根目录执行；凭据不输出）：

```sh
set -a
source /tmp/goagent-review-fix-8cIyLB/validation.env
set +a
env -u GOROOT -u GOAGENT_PGSTORE_CONTAINER \
  GOAGENT_CORE_ROOT=/home/hellotalk/code/go/src/github.com/kart-io/goagent \
  bash scripts/with-local-modules.sh go test -C durable/pgstore -race \
  -run '^Test(LockCancellationDuringAdvisoryWait|LockCancelledAfterCommit|SaveRejectsTombstoneAfterScopeWait|ScopeMaintenanceAfterConcurrentTransaction|SessionWritesAfterRowWait|AcquireUncertainCommitCleanup|DeleteLeaseWaitsForAcquireTransaction|TryAcquireUsesLeaseTransactionLock|LeaseCleanupDoesNotWaitForScopeLock)$' \
  -count=3 -v
```

原写入回归名单：`TestSaveAllDatabaseRollback`、`TestScopedSaveAllFencingAndIsolation`、`TestAppendAllDatabaseRollback`、`TestAppendAllCompareOnlyAndEncoding`、`TestAppendAllConcurrentReverseOrder`、`TestAppendAllSnapshotLeaseFencing`、`TestAppendAllCompareOnlyHoldsUntilCommit`、`TestSessionSnapshotRequiresLock`。

## 交付与剩余边界

本轮新增 `lock_cancel_test.go`、`isolation_test.go`；修改 `pgstore.go`、`scope.go`、`session.go`、原未提交的 `lease_cleanup_test.go` 和 `README.md`。原 `pgstore_test.go` 的 COMMIT 匹配补丁保持不变。此报告是核心仓库唯一写入文件。

补偿有独立 OpTimeout，因此发生写入不确定性时返回可晚于调用方期限。数据库不可用或清理超时时仍由 OnError 报告、TTL 兜底，不承诺无条件清理成功。成功交付租约之后的取消仍须调用 unlock。租约事务协议仍要求并发实例采用相同 advisory 锁方案，不能对混用旧实现作保证。

本轮证明上述时序下的行为及 race 检查通过，没有执行全模块矩阵、主备切换或故障恢复验收。DB 测试已经结束，后续全矩阵由主代理负责；未提交、未推送。
