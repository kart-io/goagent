---
phase: 2026-11-09-management-hardening
reviewed: 2026-11-09T03:12:18Z
reviewer: Codex（独立深度审查）
depth: deep
status: issues_found
diff_base: 87696061ac858e64f6b3a717d11bf1f3749e3a57
source_root: /home/hellotalk/code/go/src/github.com/kart-io/.goagent-hardening-20261109-APraZJ/goagent-contrib
files_reviewed: 4
files_reviewed_list:
  - durable/pgstore/pgstore.go
  - durable/pgstore/pgstore_test.go
  - durable/pgstore/lease_cleanup_test.go
  - durable/pgstore/README.md
findings:
  critical: 0
  warning: 1
  info: 0
  total: 1
confirmed_findings:
  introduced: 0
  preexisting: 1
pending_hypotheses:
  total: 1
  introduced: 0
  preexisting: 1
live_postgres_validation: not_performed
---

# PostgreSQL 租约竞态修复独立审查

> 后续复核：本报告交付后，主代理已用独立 PostgreSQL 16 确认 H-01 在当前与基线均存在，并完成 WR-01 的5轮离线 race 复核。新的事实、证据与最终严重级别见 [汇总审查报告](REVIEW.md) 的“既有问题”和“实库后续复核”；下文保留本 agent 审查结束时的结论与验证边界。

## 结论

**未发现本次补丁引入的有据新增缺陷。** 获取与按 token 清理的事务 advisory 锁、显式 READ COMMITTED、获锁后独立 DELETE 的组合，能够针对原来的“清理先取快照、获取稍后提交”竞态建立正确顺序。

确认 **1 项既有 P2**：调用方在内部等待期间取消后，Lock 仍可返回成功。另有 **1 项既有 P1 候选**：默认 REPEATABLE READ 时 SaveAll 的墓碑检查使用旧快照；已提供具体实库验证时序，尚未得到实库结果。后者不计入上面的已确认 findings。两者均不是此次补丁新增的根因。

本报告只审查指定四个文件，并沿相关调用链核对上下文；没有扩大到全库审计。未连接任何数据库、未启动或重启服务、未改生产源码或既有测试、未提交。唯一仓库写入为本报告。

## 已确认发现

### WR-01 — P2：内部等待结束后未复核调用方取消，Lock 可以在取消后成功返回

- **性质：确定缺陷；既有。** 新 lease advisory 等待会经过这条既有缺陷路径，不能据此归为新增缺陷。
- **文件：** [/home/hellotalk/code/go/src/github.com/kart-io/.goagent-hardening-20261109-APraZJ/goagent-contrib/durable/pgstore/pgstore.go:223](/home/hellotalk/code/go/src/github.com/kart-io/.goagent-hardening-20261109-APraZJ/goagent-contrib/durable/pgstore/pgstore.go:223)
- **精确位置：** 223–237 行；内部 context 来源为 144–145、249–252 行；新增等待位置为 272–273、310–312 行。
- **契约：** 核心仓库 `core/durable/runner.go:42–46` 要求 ctx 结束前尚未获锁时，错误同时包装 ErrLockUnavailable 与调用方 ctx 错误。

**触发场景**

1. 某事务持有目标 lease advisory 锁，目标租约行尚不存在。
2. 调用 `Lock(ctx, "run")`，它通过预查询后在 advisory 锁上等待。
3. 调用方 ctx 先取消，随后阻塞事务结束；整个过程小于 OpTimeout。
4. `tryAcquire` 使用独立内部 context 继续 INSERT 和 COMMIT；`acquire` 的成功分支未检查调用方 ctx，登记 owners 并返回 unlock、nil。

**可验证影响**

调用方已经取消、数据库锁尚未获得时，该调用仍可能随后取得租约并返回成功，违背 Lock 的取消契约。等待及后续错误清理也按内部超时继续，调用方期限不能约束这段等待。返回成功的调用仍需要调用 unlock；这里没有宣称令牌保护被绕过，也未发现 Runner 因此必然执行取消后的步骤。

**证据**

仓库外完整源码副本中的 `TestReviewCancelledDuringAdvisoryWait`，以 pgx 实际客户端配合 `net.Pipe` 协议应答暂停 advisory 响应，先取消 ctx，再放行响应。连续 5 次启用 race 检测均输出：

```text
REPRODUCED: caller=context canceled; Lock err=nil; unlock returned=true
PASS
ok github.com/kart-io/goagent-contrib/durable/pgstore 1.021s
```

这证明 Go 控制流缺陷，不是 PostgreSQL 锁语义的实库验证。基线相应 `acquire` 成功分支和独立 context 实现完全相同。

**修复方向**

每轮尝试前检查 ctx；内部尝试结束、尚未将新持有者交给调用方前再次检查 ctx。若已取消，应先按本次 token 做有界清理，释放 gate，再返回包装 ErrLockUnavailable 与 ctx 错误的错误。不要为了响应取消直接省略提交不确定性的清理。若还要求及时中止等锁，需要把可取消等待阶段与提交、补偿阶段分开设计。

**回归验证**

用数据库实际 advisory 等待作为同步点，取消调用方后再解除阻塞；同时断言错误链、owners/gates 不残留、自身租约已清理。现有通用 `storetest.requireBlocked` 主要覆盖已提交 busy 租约或本地 gate 等待，新增测试又直接调用 `tryAcquire`，都未验证这个公开 Lock 路径。

## 待实库验证的具体候选

### H-01 — P1 候选：SaveAll 仍继承 REPEATABLE READ，等待 scope 锁后可能漏掉新墓碑

- **性质：高可信静态候选；既有；未实库确认，不计入确定发现。**
- **文件：** [/home/hellotalk/code/go/src/github.com/kart-io/.goagent-hardening-20261109-APraZJ/goagent-contrib/durable/pgstore/pgstore.go:401](/home/hellotalk/code/go/src/github.com/kart-io/.goagent-hardening-20261109-APraZJ/goagent-contrib/durable/pgstore/pgstore.go:401)
- **精确位置：** 401–406 行仍用 `pgx.BeginFunc`；403 行的 set_config SELECT 先于 scope 锁；296–300 行获锁后另语句查墓碑。
- **相关调用链：** `scope.go:33–39 → save/saveAll`；`scope.go:44–60` 将墓碑提交和数据清理分成两个阶段。
- **优先级依据：** 候选影响作用域删除后的写入拒绝契约；不是已证实的权限绕过或数据重新可读。

**触发条件与复现方向（已提前告知主代理）**

仅需隔离 schema、普通事务及两组连接，不需要改全局配置或重启：

1. 创建持有 `s:tenant/run` 合法 token 的 Store，禁用该测试持有者的后台续约，保留租约和快照。
2. 连接 A 开始事务，执行 `pg_advisory_xact_lock(hashtextextended('s:tenant', 0))`。
3. 写入连接池设置会话默认隔离级别 `repeatable read`，调用 `Scope("tenant").Save`；观察它在 scope 共享锁上等待。
4. A 插入该 scope 墓碑并提交；暂不删除租约和快照，模拟 RemoveScope 在墓碑提交后中断。
5. 检查 Save 的返回值与快照字节；候选缺陷表现为 Save 返回 nil 且内容被修改，而不是 ErrScopeRemoved。READ COMMITTED 对照应拒绝。

**判断依据**

本补丁只把获取和清理改成显式 READ COMMITTED。SaveAll 的第一次 SELECT 已在等 scope 锁前建立事务快照；REPEATABLE READ 下，后续独立 SELECT 不会刷新该快照。若没有其他事务改写租约，token UPDATE 不一定触发序列化失败，因此不能依赖它兜底。PostgreSQL 官方明确区分了两种隔离级别的快照时点：[事务隔离说明](https://www.postgresql.org/docs/current/transaction-iso.html)。

**待验证影响**

如果上述时序成立，墓碑已经生效时排队写入仍可成功，违反 `scope.go:42–43` 的写入拒绝语义。Load 自身仍查询墓碑；尚无证据表明此场景能令删除后的数据重新对调用者可见。

**修复方向**

凡依赖“获 advisory 锁后用新语句复核”的路径，应明确指定合适的 READ COMMITTED 事务，尤其 SaveAll；同步复核 PruneRemoved 的同类快照假设。若产品仅支持特定默认隔离级别，应显式校验并拒绝不支持的配置，而不是让获取端成功后写入端默默依赖不同隔离语义。

**现有测试盲区**

`lease_cleanup_test.go:43–58` 将默认隔离级别变体施加给直接调用 deleteLease 的 Store；它不调用 SaveAll。获取端两个测试（117–149、151–203 行）也没有主动设置 REPEATABLE READ。因此现有“默认隔离级别”覆盖不能推广到完整 Store 生命周期。

## 锁、令牌与事务调用链结论

| 路径 | 实际顺序与约束 | 审查判断 |
| --- | --- | --- |
| 获取 | Lock → 本地 gate → tryAcquire → 预查询 → 显式 RC 事务 → scope 共享锁 → 墓碑查询 → lease 排他锁 → UPSERT → COMMIT | 预查询只作快速判断；事务内复核阻止预查询后的 scope 删除竞态。 |
| 获取错误补偿 | tryAcquire 出错 → 自身 token 的 deleteLease；ErrScopeRemoved 除外 | 出现 INSERT/COMMIT 不确定性前已拿 lease 锁，补偿会等待原事务结束；在 scope 检查阶段失败尚未插入本次 token。 |
| 清理与解锁 | unlocker 移除 owners → deleteLease → 显式 RC → lease 锁 → 独立 DELETE → COMMIT → 放行 gate | 新快照能看到等锁期间提交的行；完整 ns/id/token 条件保留接管者。 |
| 续约 | owners 快照 → RenewPool 的 token 条件 UPDATE | 不取 advisory 锁；与接管/删除依赖行锁。DELETE 后 UPDATE 不会重新插入行；旧 token 不匹配新持有者。 |
| 写入 | SaveAll：scope 共享锁 → 排序后的租约行 UPDATE → 快照写入；会话追加同样校验 token | 单事务行锁保护写入与 token 更换；没有发现本补丁新增的 fencing 绕过。默认隔离级别例外见 H-01。 |
| 墓碑 | RemoveScope：scope 排他锁内提交墓碑，之后分批清理；PruneRemoved：scope 排他锁内回收 | deleteLease 不再请求 scope 锁；审查到的新增锁路径没有形成 lease → scope 的反向锁边。 |
| 池与超时 | 预查询和事务分别借 Pool；续约/过期清理借 RenewPool；补偿另建内部超时 | pgxpool 事务结束会释放连接；异常且仍处于事务状态的连接会被销毁，未发现事务锁作为空闲连接遗留的新路径。 |

依据 PostgreSQL 官方说明，事务 advisory 锁由事务结束释放，单 bigint 与双 int 键空间不重叠；本补丁的 scope/lease 分空间做法正确。[advisory 锁函数](https://www.postgresql.org/docs/current/functions-admin.html#FUNCTIONS-ADVISORY-LOCKS)

READ COMMITTED 的 DELETE 只在语句开始时搜寻可见目标行，遇到并发修改时对更新后的行重新检查条件。因此“先等 advisory、再独立 DELETE”与 token 条件均必要。[事务隔离说明](https://www.postgresql.org/docs/current/transaction-iso.html)

另核对了 `now()` 是事务开始时间，等锁会消耗有效租期；相关时间表达式在基线已经存在。未找到据此单独认定本补丁新 fencing 缺陷的证据，不另立发现。[日期时间函数](https://www.postgresql.org/docs/current/functions-datetime.html#FUNCTIONS-DATETIME-CURRENT)

## 新增测试的有效覆盖与缺口

有效覆盖包括：根/作用域命名空间、获取事务提交/回滚/不同 token、清理端默认 REPEATABLE READ、获取端确实请求 lease 锁，以及 scope → lease 的锁顺序。使用 `pg_locks/pg_blocking_pids` 确定等待，比仅依赖 sleep 更能约束竞态时序。

以下是具体补测建议，**不作为新增缺陷凑数**：

1. **获取端默认隔离级别。** 给 `TestLeaseCleanupDoesNotWaitForScopeLock` 增加 REPEATABLE READ 变体，等 scope 锁期间提交墓碑后断言 ErrScopeRemoved。现在只有清理端的隔离级别被变体覆盖。
2. **墓碑已提交后的清理。** 该测试在 188 行执行 deleteLease，197–199 行才插入并提交墓碑。它验证“不等待 scope 排他锁”，尚未独立验证“即使已存在已提交墓碑也删除自身 token”。可先提交墓碑并保留租约，再调用清理断言零行。
3. **公开 Lock 的取消。** 见 WR-01；必须通过 Lock 调用，直接调用 tryAcquire 无法覆盖 acquire 的取消后成功分支。
4. **超时与池恢复。** 保持 lease 锁超过 OpTimeout，断言 OnError 报告、操作有界结束，解除阻塞后同一池能再次取得租约。当前新测试只验证等待最终正常结束。
5. **提交不确定注入的证据边界。** `pgstore_test.go:253–260` 在 Write 后关闭连接，无法单凭该事件证明服务端已提交。修改后的注释准确；新手工事务测试补充了清理等待协议的确定时序，但本次未实库执行这些用例。

离线专门检查了当前 `dropAfterCommit`：真实 pgproto3 Query 编码下，`BEGIN ISOLATION LEVEL READ COMMITTED` 和包含 commit 文本的普通 SELECT 不触发，完整 COMMIT 恰好触发一次。pgx v5.10.0 的 `tx.go:185–190`、`conn.go:515–517` 也支持该无参数 COMMIT 使用 Simple Query 的假设。未发现此次精确匹配修改的确定回归。

## 验证记录与限制

| 检查 | 结果 |
| --- | --- |
| 对比指定基线和完整读取四个文件 | 完成；基线即当前 HEAD，两个新增文件按显式范围读取 |
| 跨文件追踪 | scope.go、schema.go、session.go、import.go；核心 Store/Scoped 契约、Runner、session durable 适配；pgx 事务/连接池实现 |
| 原模块离线编译 | 首次因未缓存 goagent v0.50.0 失败；未访问网络下载 |
| 本地核心源码替代后的编译 | 通过；`go test -mod=readonly -run '^$' -timeout 45s ./...`，没有执行集成测试 |
| 静态分析 | 同一临时 workspace 下 `go vet -mod=readonly ./...` 通过 |
| WR-01 内存协议复现 | 仓库外副本；`-race -count=5 -timeout 20s`，5/5 重现取消后成功 |
| COMMIT 注入器离线检查 | 仓库外副本；`-race -timeout 10s`，通过 |
| diff 格式与文件完整性 | `git diff --check` 通过；四个文件 SHA-256 前后相同 |
| PostgreSQL 集成验证 | 未执行；没有连接本机其他数据库或启动服务 |

离线命令均清除 `GOAGENT_PGSTORE_DSN`、`GOAGENT_PGSTORE_CONTAINER`、`GOAGENT_PGSTORE_CHILD`，并设置 `GOPROXY=off GOSUMDB=off GOTOOLCHAIN=local`。临时 workspace 在 `/tmp/pgstore-review-jKvsB4`，仅将目标 pgstore 模块和现有核心源码组合；未改仓库 go.mod/go.sum。临时复现使用源码副本和新增临时测试，原测试未改动。内存协议使用 net.Pipe，不监听端口、不连接数据库。

可复查的临时文件：

- `/tmp/pgstore-review-jKvsB4/go.work`：原模块编译与 vet。
- `/tmp/pgstore-review-jKvsB4/repro.work`：仓库外副本复现。
- `/tmp/pgstore-review-jKvsB4/pgstore/review_repro_test.go`：WR-01。
- `/tmp/pgstore-review-jKvsB4/pgstore/review_commit_test.go`：COMMIT 匹配。

临时文件依赖当前机器，不替代永久回归用例。H-01 已告知主代理触发条件和隔离验证需求；截至报告时间没有收到实库结果。工具环境没有独立 Read/Write、context7 或 desktop-commander，使用只读命令、官方 PostgreSQL 文档及现有文件 API 完成相应工作。

按核心仓库审查规范给出人工评估分：代码质量 90、测试覆盖 80、规范遵循 95；需求匹配 90、架构一致 90、风险评估 85；综合 88/100，建议“需讨论”。分数不是测试通过率；讨论点是既有缺陷是否随本次处理，不代表发现了新增阻断缺陷。

## 文件指纹

```text
2a72f85b40db1a3aee8cea4fb3210e18ca198d2f04690ec5ee6dcccfe0960f0c  durable/pgstore/pgstore.go
cad19dad8c6da7c8303bbf2d22000a30ad6ba2a823aa7b499de6be9587712565  durable/pgstore/pgstore_test.go
dc6c3c639bfe4b49af3a21cb89aee6393abc22a22f17e92d4200b4b4a8043695  durable/pgstore/lease_cleanup_test.go
1436f9999b3b019f7203edf691713209a50c26509ba468b9f47b008013bb582f  durable/pgstore/README.md
```
