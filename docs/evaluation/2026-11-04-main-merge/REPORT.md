# 主分支合并验证

日期：2026-11-04。按用户要求，将三个关联仓库的 `fix/goagent-compatibility` 合入各自最新主分支；GoAgent 原修复 `3d216cc` 已在 master，本次只追加合并验证资料。保留远端主分支期间新增能力，未强制覆盖任何远端提交。

| 仓库 | 目标主分支 | 合并提交 | 原修复提交 |
|---|---|---|---|
| goagent-contrib | master | `e0c7533` | `15b6e2c` |
| goagent-harness | main | `2368ac0` | `f08d851` |
| goagent-examples | main | `b900dfe` | `080b6cd` |

每个合并提交均包含最新远端基线及原修复提交，提交树与验证树一致。完整记录见 [merged-revisions.json](merged-revisions.json)、[verified-trees.json](verified-trees.json)。合并前 fetched 远端比原修复基线多出 contrib 17、Harness 63、examples 12 个提交，明细见 [upstream-baselines.json](upstream-baselines.json)。没有通过旧整文件覆盖新功能。

## 冲突处理

- contrib 保留新统一 Embedder、float32 与元数据过滤能力，依赖以最新主分支为底，核心统一 v0.47.0。检索和 HTTP 入口保留较强的角色、终态及每入口工具计数回归。
- PostgreSQL 保留 master 的 SaveAll 与 appendPlan 结构。实库回归发现空批次比较后等待其他会话行锁期间仍可被并发追加穿过；补上直到事务提交的行/唯一键保护，并为同会话快照租约稳定排序。缺失和已存在会话均先在旧逻辑复现，再验证修复。
- Harness 保留新版摘要预算/降级、Request 通道、wrap-up、校验、沙箱和环境能力。期限复核同时保留 Running 和新版 Waiting，避免审批等待任务不再到期；补齐 macOS runner 实际元数据，环境探测测试使用确定性可执行 fixture，不再假定系统 `sh --version` 必失败。
- examples 保留最新 CI 与正式 contrib/acp v0.3.0、httpagent v0.2.0，合入流终态/错误/用量回归及 Go1.26 工作区更新。
- 新增发现 Harness `integration/postgres` 独立模块，因此联合验证扩展为 **15 个模块**；其核心升级 v0.47.0、正式 pgstore v0.5.0 及原有相对 Harness replace 保留。

## 验证

| 范围 | 结果 |
|---|---|
| 合并后 15 模块当前源码 | 全部 `go test -race -count=1 -timeout=180s ./...` 退出0，未使用-short；临时modfile选择当前关联源码，见 [joint-results.json](joint-results.json) |
| GoAgent master | 完整race、vet、导入层级检查通过 |
| contrib | 正式test-all的11模块race/vet及lint-all通过；lint均0 issues |
| Harness根模块 | 完整race（11有测试包）、vet、lint通过；关键回归race重复40轮通过 |
| examples根模块及HTTP子模块 | race、vet、lint均通过 |
| pgstore真实数据库 | 79 PASS（含子测试）、0 FAIL、0 SKIP，44.430s，包含新空批次并发比较及原回滚/fencing/重启用例，见 [pgstore-race.txt](pgstore-race.txt) |
| Harness Postgres正式依赖 | 真实数据库race，3 PASS、0 FAIL、0 SKIP，4.894s，见 [harness-postgres-published.txt](harness-postgres-published.txt) |
| Harness Postgres三仓源码 | 另在独立数据库以当前core/Harness/pgstore源码联合race，3 PASS、0 FAIL、0 SKIP，3.726s |
| 独立合并审查 | 无阻断项；另定点复验流式合同、Waiting到期、摘要降级、环境fixture与模块选择 |

数据库均为新建临时隔离实例，测试后已清理，未接触已有或生产数据库。本轮没有重新调用付费模型；此前真实模型的24场景记录针对原修复工作树，不能冒充本次新增全部上游能力的真实模型验收。macOS本机验证不代替Linux Landlock实机或远端SSH连接测试，相关用例维持已有跳过条件。

环境：Go1.27.2 darwin/arm64；本机仅对子进程取消与PATH不匹配的GOROOT。没有修改既有主仓库两处未跟踪沙箱研究资料。
