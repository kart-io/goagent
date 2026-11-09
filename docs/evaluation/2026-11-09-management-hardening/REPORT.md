# 管理层稳定化与跨仓验收（2026-11-09）

本轮将项目分析中的管理层测试缺口、纯聊天接入限制和启动恢复截断问题落实为代码、回归测试和验收门禁。核心修改位于当前工作树，关联仓库修改在隔离工作树中，尚未发布。

后续三位专业 Agent 审查及主代理复核确认1项新增P1、2项新增P2及3项既有问题，详见[专业审查报告](review/REVIEW.md)。用户随后要求修复，最新修复、独立复审与集成结果见[修复汇总](fix/REVIEW-FIX.md)；Contrib 最终交付使用其中的新补丁。下文保留本轮原始验收事实与历史源码快照。

## 已完成的修复

| 问题 | 最终行为与证据 |
|---|---|
| NativeFactory 强制会话日志，普通聊天却要求配置工具 | Builder 的普通流式聊天复用现有轮次日志，保留 `model/answer/0` 操作键；测试覆盖普通 Client、无工具的 ToolStreamClient、多轮、存储重建、Fork、失败轮次及日志提交失败后的无重复请求恢复 |
| 启动只恢复前10000条任务/命令 | 每批200条，修改状态后继续从筛选集合头读取，全部批次仍在同一事务；10001条跨租户任务与命令、终态保留、重复恢复和失败回滚测试通过 |
| 管理层测试覆盖不足且无独立门禁 | 补服务、状态机、事务故障、权限、HTTP 和原生/Graph 适配器行为测试；management/adapters/httpapi 独立最低覆盖率为75/75/80% |
| pgstore 不确定提交后清理遗漏租约 | Contrib 获取与清理共用 `(ns,id)` 事务 advisory 锁，显式 READ COMMITTED，获锁后用新语句按 token 删除；确定性测试覆盖提交、回滚、其他 token、默认 REPEATABLE READ 和 scope 锁顺序 |
| Examples/Harness 仍引用已删除的常量 | 使用 `constants.WorkflowStart`、`WorkflowEnd`、`MultiagentFinish`、`ToolReadSpill`，移除无用旧导入 |
| 数据库测试 SKIP 可能被误读为通过 | `make compatibility-postgres` 要求专用数据库及容器，并核验关键场景的明确通过事件；缺失、跳过、失败及仅子测试通过均不能放行 |

pgstore 清理仍受操作期限约束，失败通过 OnError 报告、残留租约由 TTL 兜底。新协议要求并发获取者也使用同一锁，混用旧实现不具备串行化保证。SQL 快照依据见 [PostgreSQL 事务隔离](https://www.postgresql.org/docs/current/transaction-iso.html) 与 [事务 advisory 锁](https://www.postgresql.org/docs/current/explicit-locking.html#ADVISORY-LOCKS)。

## 核心验证

Go 1.27.3，Linux/amd64；全量 `go test -json -race -count=1 -timeout=120s -coverprofile=... ./...` 通过。测试事件包含子测试，不能视为独立业务样本。

68个包含测试的包执行了测试，5022个测试/子测试通过事件、9个测试跳过事件。跳过包括子进程辅助入口、未启用的 Docker executor 和既有缓存失效测试；完整名单保留在验证摘要，未将其记为通过。

| 覆盖率 | 分析时 | 本轮结果 | 门槛 |
|---|---:|---:|---:|
| 整体（脚本按语句加权） | 76.0% | 80.8% | 75% |
| management | 18.3% | 79.6% | 75% |
| management/adapters | 21.8% | 81.3% | 75% |
| management/httpapi | 9.9% | 97.3% | 80% |

Build、vet、golangci-lint（0 issues）、导入分层（0 findings）和独立 frameworkbench 的 readonly/race 验证均通过。管理层离线事务替身用于行为验证，不证明 PostgreSQL 的锁、隔离或持久性。

## 跨仓与负载结果

完整实库矩阵17/17模块通过，1924个测试/子测试通过事件，82个跳过事件：[最终跨仓摘要](compatibility.json)。跳过项来自 PDF 外部资料、Qdrant、真实模型、外部服务或辅助测试入口，完整名称逐项保留，未计为通过。

[18项关键场景事件](outcomes.json) 全部通过，包含 pgstore 存储契约、进程崩溃接管、数据库重启、同步提交、不确定提交租约清理、锁顺序，以及管理审批进程恢复、Fork 崩溃恢复、10个提交断点、Outbox 接受后退出、Harness 跨连接恢复/失效持有者/Inbox 去重和退款案例跨进程恢复。管理 integration 包实际耗时344.372秒。

最终报告通过新增的实库证据检查，首轮失败报告被同一检查拒绝。门禁回归还覆盖缺失、跳过、失败、仅子测试通过和先失败后通过的日志，避免把非通过记录转成绿色结果。

| 本地合成负载 | 结果 | 证据与限制 |
|---|---|---|
| 60秒、4工作者、100ms间隔、5秒采样 | 退出码0；末次采样2231周期、4462请求与4462效果；回放无额外请求/效果的周期断言通过 | [原始采样](soak.log)；P95/P99桶上界10ms，采样goroutine峰值11，采样堆小于3MiB；不是长稳或生产容量验收 |
| 10/100/1000操作 × none/memory/file预算 | 9/9组合通过，共3330次操作成功 | [原始矩阵](execution-matrix.json)；无模型请求，file临时目录位于本机tmpfs，不代表生产磁盘性能 |

规模矩阵显示文件预算账本仍有分配开销：1000次操作耗时3.817秒、P95为7.51ms、累计分配约2.58GiB，GC后活跃堆采样约1.52MiB。累计分配不是峰值或常驻内存；本轮没有前后基线对比，不能据此认定性能回退。后续应针对账本规模增长的分配与写入成本优化，再以实际存储和业务负载确定容量目标。

首轮17模块中13个通过、4个未通过：[首轮摘要](initial-compatibility.json)。pgstore 的 `TestAcquireUncertainCommitCleanup` 实际失败；management 达到原5分钟模块时限而被终止；examples 和 harness 因已删除的常量入口编译失败。对应处理是修复租约事务顺序、将实库时限改为15分钟、更新常量引用。未删除或跳过失败场景。

## 源码与交付

核心基线为 `f2e0b0b449fcfda416169356291689248e5f455b`，本轮变更尚未提交；[源码清单](source-manifest.json) 记录改动源码的 SHA-256。关联仓库取各自远端当前分支创建隔离工作树，原始工作树未被覆盖。

| 仓库 | 隔离基线 | 补丁 |
|---|---|---|
| goagent-contrib | `87696061` | [租约清理修复与回归](patches/goagent-contrib.patch) |
| goagent-examples | `454b2475` | [20个文件的常量迁移](patches/goagent-examples.patch) |
| goagent-harness | `367330b2` | [4个文件的常量迁移](patches/goagent-harness.patch) |

[补丁清单](patches/manifest.json) 包含完整基线、文件列表与 SHA-256；每份补丁均在对应基线新工作树中通过前向应用检查、实际应用、反向检查，并逐文件比对验证源码，确认包含新增测试及文档。合入时在对应基线检查补丁，不直接覆盖已有本地修改。跨仓验收针对这些源码组合，不能将结果套用于尚未包含补丁的已发布版本。

可直接审阅的隔离工作树保存在原仓库父目录的 `.goagent-hardening-20261109-APraZJ/` 下，分别为 `goagent-contrib`、`goagent-examples`、`goagent-harness`；均为上述基线加未提交补丁。原 examples 工作区的用户未提交修改未被覆盖。

回退代码时保留已有会话与操作日志；本轮未改变持久数据格式。pgstore 的新旧锁协议不宜混跑，部署或回退需协调所有获取租约的实例。实际业务部署、发布标签和远端提交均未执行。

## 运行边界

仍为一个活动 Worker，可并发执行不同会话；没有实现多实例分片或心跳自动接管。异常退出后须由宿主 `ConfirmWorkerStopped` 确认旧进程停止。分页恢复消除了数量截断，但大积压仍在一个事务内，生产锁等待和恢复时间须按实际容量验证。

本轮使用本机独立 PostgreSQL 16 容器与合成模型/效果数据，没有接入真实登录、订单或支付服务，没有新增真实模型调用。尚未收到业务测试地址、认证方式及数据范围，因此生产业务验收、长时间负载和业务恢复目标未验证。

专用测试数据库容器、匿名卷及临时凭据文件已清理。结果与执行命令见 [验证摘要](verification.json)，覆盖率门禁输出见 [coverage-gates.txt](coverage-gates.txt)。原始日志保存在上述持久目录的 `evidence/`，首次运行、定向修复过程与最终复验各自保留；摘要记录102份证据文件的 SHA-256，不把完整测试输出或数据库连接串写入仓库。历史日志中的临时工作树路径是执行时路径，最终交付源码已迁至持久目录并验证内容一致。

## 复现

```sh
make test-management
make coverage
make test-frameworkbench
# 本机独立测试数据库：设置 GOAGENT_PGSTORE_DSN 与 GOAGENT_PGSTORE_CONTAINER。
# 容器需绑定固定回环端口；测试会重启该容器并改变同步提交设置。
make compatibility-postgres COMPAT_ROOT=/path/to/isolated-repos COMPAT_OUT=/tmp/goagent-postgres-new
go run ./scripts/compatcheck -verify-postgres-report /tmp/goagent-postgres-new
go run ./scripts/stabilitysoak -duration 60s -workers 4 -interval 100ms -sample 5s
go run ./evaluation/executionbench/cmd/run -operations 10,100,1000 -modes none,memory,file -timeout 5m
```

实库命令每模块默认15分钟，可通过 `COMPAT_TIMEOUT` 调整；通用 compatibility 仍为5分钟。报告目录必须不存在。验证器通过临时 modfile 将全部本地模块及测试内子构建绑定到当前核心，不改写依赖文件。
