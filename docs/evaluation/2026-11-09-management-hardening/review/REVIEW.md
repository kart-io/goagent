---
status: changes_requested
review_date: 2026-11-09
depth: deep
independent_reviewers: 3
scope_files: 51
new_findings:
  P0: 0
  P1: 1
  P2: 2
existing_findings:
  P2: 3
production_code_modified: false
---

# 专业 Agent 审查与独立复核

> 后续状态：本报告保留修复前的审查结论与反例。六项问题的修复、独立复审及最终集成结果另见[修复汇总](../fix/REVIEW-FIX.md)，下文“尚未修复”描述的是本次原审查时点。

**结论：建议修复后再合入。** 本轮新增变更确认 **1项P1、2项P2**，另确认 **3项既有问题**。最优先处理普通子 Agent 写坏父会话日志的问题，再补齐实库证据门禁。没有发现 PostgreSQL 租约清理补丁引入的新缺陷。

三位 `gsd-code-reviewer` 分别审查核心执行、PostgreSQL 并发协议、验收门禁与测试。均未参与上一轮实现；使用当前会话继承的默认模型，不是人工审核或跨模型投票。主代理核对证据并独立复跑关键反例，期间没有修改生产源码、现有测试或提交代码。

## 新增问题：合入前处理

| 编号 | 严重级别与位置 | 触发及影响 | 复核结果／修复方向 |
|---|---|---|---|
| CORE-01 | **P1**：[builder/events.go:210](/home/hellotalk/code/go/src/github.com/kart-io/goagent/builder/events.go:210) | 有会话日志的父 Agent，在工具中用原 ctx 调用未配置日志的普通子 Agent。子 Agent 继承父 `sessionTurn`，提前追加父轮次结束事件；失败历史残留，同执行键恢复误入只读回放并失败。 | 三个反例在当前代码上全部失败，仅替换基线 events.go 后全部通过；主代理两组均启用 race。应在 Agent 执行边界屏蔽继承日志，只让当前 Agent 安装自己拥有的日志；覆盖公共与 Handle 入口。 |
| VR-01 | **P2**：[scripts/compatcheck/postgres.go:57](/home/hellotalk/code/go/src/github.com/kart-io/goagent/scripts/compatcheck/postgres.go:57) | 必需顶层测试通过，但关键子测试 `Skip`，父测试仍可通过。门禁仅查精确顶层名称，完整 `-require-postgres` 仍退出0。 | agent 用真实 Go 测试流及完整验证器复现，主代理从当前源码重新复核该报告仍错误通过。应同时检查契约后代的 skip/fail，允许的辅助跳过须明确列出。 |
| VR-02 | **P2**：[scripts/compatcheck/postgres.go:75](/home/hellotalk/code/go/src/github.com/kart-io/goagent/scripts/compatcheck/postgres.go:75) | 已有 summary 标记通过，而日志包含包级失败、JSON尾部截断，或缺少包结束事件。独立报告复核忽略这些信号，仍退出0。 | 主代理分别复现三种错误通过。应核验包生命周期、成功结束及失败事件，拒绝损坏/未完成的测试流，核对模块边界与 summary。正常实跑仍检查子进程退出码，此项不等于实跑过程忽略所有失败。 |

源码级分析见 [CORE-REVIEW.md](CORE-REVIEW.md) 和 [VALIDATION-REVIEW.md](VALIDATION-REVIEW.md)。主代理归档的最小复现、命令与输出见 [repro/README.md](repro/README.md)。

## 既有问题：独立跟踪

| 编号 | 严重级别与位置 | 已确认行为与边界 |
|---|---|---|
| PG-H01 | **P2**：[pgstore.go:401](/home/hellotalk/code/go/src/github.com/kart-io/.goagent-hardening-20261109-APraZJ/goagent-contrib/durable/pgstore/pgstore.go:401) | SaveAll 继承默认 `REPEATABLE READ`。等待 scope 锁前已建立快照，墓碑随后提交时仍可能用旧快照通过检查，返回成功并改变快照。主代理在独立 PostgreSQL 16 上证实；READ COMMITTED 对照拒绝。基线同样失败，故不是本补丁回归。 |
| PG-WR01 | **P2**：[pgstore.go:223](/home/hellotalk/code/go/src/github.com/kart-io/.goagent-hardening-20261109-APraZJ/goagent-contrib/durable/pgstore/pgstore.go:223) | 调用方在内部 advisory 等待期间取消，内部独立 context 仍可完成获取；成功分支没有再次检查取消，Lock 返回 unlock、nil。主代理使用真实 pgx 客户端与内存协议应答连续5轮 race 复现；这是控制流复现，不是实库等锁验证。 |
| VR-03 | **P2**：[scripts/compatcheck/main.go:199](/home/hellotalk/code/go/src/github.com/kart-io/goagent/scripts/compatcheck/main.go:199) | 模块发现及版本探测使用未设 deadline 的外层 context，配置的模块超时在这些步骤之后才建立。agent 用阻塞的假 Go 启动器证明短模块超时不能终止发现阶段；属于既有启动阶段有界性缺口。 |

PG-H01 在 PostgreSQL agent 报告中原为待验证P1候选。主代理实库确认后，汇总按P2列入：已证实的是特定隔离级别、墓碑提交后清理中断时的拒写契约失效；没有证据表明删除的数据因此重新可读或绕过权限。建议 SaveAll 显式采用所需隔离级别，并检查其他“获锁后刷新快照”路径。

三项既有问题不计入新增变更缺陷数，也不把已有缺陷归咎于租约补丁。PG-H01 应在使用 REPEATABLE READ 的环境接入前处理。相关独立分析见 [POSTGRES-REVIEW.md](POSTGRES-REVIEW.md)。

## 实库后续复核

主代理仅为 PG-H01 创建独立、随机密码、绑定回环地址的 PostgreSQL 16 容器，使用隔离 schema；没有连接其他数据库，也没有重启既有服务。

公开 `Scope.Lock/Save` 获取真实 token 和初始快照；测试禁用后台续约，通过真实 `pg_locks/pg_blocking_pids` 确认 Save 等待 scope 锁，再提交墓碑并解除阻塞。保留租约及旧快照模拟 RemoveScope 墓碑已提交、清理前中断。没有用 sleep 推测并发时序。

| 实现 | READ COMMITTED | REPEATABLE READ |
|---|---|---|
| 当前隔离补丁源码副本 | 返回 ErrScopeRemoved，快照仍为 before | 返回 nil，快照变为 after，测试失败 |
| 仅替换基线 pgstore.go | 返回 ErrScopeRemoved，快照仍为 before | 同样返回 nil、改写快照，测试失败 |

两次测试均启用 race：[当前输出](repro/postgres-tombstone-current.log)、[基线输出](repro/postgres-tombstone-baseline.log)、[反例源码](repro/review_tombstone_test.go.txt)。取消控制流的独立重复结果见 [postgres-cancel-reproduction.log](repro/postgres-cancel-reproduction.log)。临时数据库、匿名卷及凭据文件已清理。

## 没有认定为缺陷的内容

- 恢复分页的边界、两类记录回滚、取消、重复恢复，未发现有据新增问题；新增测试验证了200条附近边界和命令阶段故障。
- 新增管理测试调用真实 Service/Worker/HTTP/适配器路径；定向三轮 race 通过。事务替身仍不证明 PostgreSQL 隔离。
- 管理三个独立覆盖门禁按精确包匹配，不混入辅助子包。当前默认采集下辅助包为0/224，不存在抬高本轮整体覆盖率的证据。
- Examples/Harness 常量替换未发现协议值或副作用遗漏。
- 51个审查文件的快照、23个核心源码/构建文件的实际改动集合、3份补丁和102份历史证据摘要一致：[artifact-audit.json](artifact-audit.json)。
- 主代理额外核查原实库记录的846条关键模块事件，没有关键契约及后代跳过/失败、损坏JSON或相关包缺失成功终态：[original-evidence-check.json](original-evidence-check.json)。门禁缺陷不推翻这些已保存运行事实，但已有测试通过不能替代本次审查结论。

## 审查范围与交付

源码版本和文件指纹见 [scope.json](scope.json)，职责分工见 [reviewers.json](reviewers.json)。审查对象是当前未提交实现及隔离工作树补丁。未执行生产业务验收或再次运行全量跨仓矩阵；独立反例只确认所述触发条件及控制流。

本次按用户要求交付审查结论与复现证据。生产代码保持审查时状态，以上问题尚未修复，代码与报告均未提交。
