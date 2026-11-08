# 统一管理层迁入核心验收

2026-11-08，通用管理层、原生/Graph 适配、标准库管理 HTTP Handler 已迁入 GoAgent 主模块；Postgres 实现、数据库集成测试、退款案例和独立 Harness 适配保留在 contrib。没有旧根包、错误别名或兼容转发。

受检代码为核心 `db7fffed9a84`，扩展 `4cc802d`。以下是本地真实执行结果，不代表远端 CI 或发布已经完成。源码在隔离分支完成并独立审阅后，已按用户此前要求快进合入两个仓库的本地 master；核心原有未提交文档完整保留，索引三方合并且有恢复备份。本轮不推送或发布。

## 结构与依赖

| 位置 | 当前内容 |
| --- | --- |
| `goagent/management` | Agent、Session、Task、Approval、Worker、Outbox、运行观测及 Store/Runtime 合约 |
| `goagent/management/adapters` | Native 与 Graph 执行适配 |
| `goagent/management/httpapi` | Handler 与 OpenAPI，不自动监听端口 |
| `contrib/management/pgstore` | SQL 管理存储与历史查询 |
| `contrib/management/integration`、`examples/host` | 数据库、进程与 HTTP 验收宿主 |
| `contrib/management/harness` | 独立可选 Harness 对接 |

核心 go.mod/go.sum 没有新增依赖，独立消费者的依赖图不含 pgx、contrib 或 Harness。execution.Scope 继续只负责单次执行资源装配，新增导入规则禁止执行基础层反向依赖 management。

JSON 统一使用 codec/json，管理领域错误直接使用核心 errors.ErrManagement*。CanonicalJSON 复用核心严格解码与精确数字选项，删除重复解析实现；HTTP 采用 Strict 字段规则。对照迁移前后代码，原任务、审批、执行、存储与恢复流程不变。

## 实际验证

| 检查 | 结果 |
| --- | --- |
| 核心未修改基线 race | 4749 个通过事件，9 个既有跳过 |
| 核心迁移后全套 race | 4771 个通过事件，0 失败；跳过的9项与基线完全一致 |
| 核心覆盖率 | 76.0%，所有既有门禁通过，见 [覆盖率](coverage-gate.log) |
| 核心 vet、lint、build、离线 run | 通过；lint 0 issues |
| 独立 frameworkbench | 通过，GOWORK=off |
| 实际外部消费者 | 新入口先因不存在失败，迁入后编译运行通过 |
| 导入规则 | 新分层测试先失败后通过；全库导入检查 0 findings |
| contrib 13 模块真实数据库 race/vet | 972 个通过事件，0 失败，12 个既有 PDF/Qdrant 环境跳过 |
| contrib 13 模块 lint | 全部 0 issues |
| 独立扩展模块 | GOWORK=off，实际核心提交模块制品，Postgres 72 个事件通过、0跳过 |
| CLI 退款 | 退款1次、Token10、重复提交同Task，旧Session/Fork为v1、新Session为v2 |
| HTTP 黑盒业务 | 8组、126次请求通过，宿主退出0 |
| 独立完整审阅 | [审阅报告](review.md)：0 Critical、0 Important、0 Minor，Yes |

计数包含父测试和子用例。迁移前管理模块的93个事件完整拆为核心21个、扩展72个；新增1个分层规则事件，没有通过删除业务测试制造通过。管理相关测试均无跳过；contrib的数据库重启和同步提交也在本轮专用容器实际执行。

[CLI原始结果](builtin-case.json)、[HTTP原始结果](http-cases.json)、[全部统计与跳过清单](summary.json)均保留。HTTP覆盖认证、幂等、审批不可绕过、批准退款一次、拒绝/取消不退款、停用阻塞后续跑、版本与Fork隔离、只读日志和跨页汇总。该组6个Task产生4笔退款、55Tokens；重复查询前后日志30条且内容摘要不变。

## 实施取舍及交付顺序

| 决定 | 理由与影响 |
| --- | --- |
| 保留 contrib management 扩展 module，删除其根 package | 承载SQL与验收宿主，避免无价值拆分；未来若改变发布粒度需另行调整模块版本 |
| HTTP使用核心Strict规则 | 统一边界校验；依赖错误大小写或非法UTF-8的客户端必须修正请求 |
| contrib依赖及CI锁定真实核心提交 | 不将v0.50.0冒充为含新包的版本；外部机器和远端CI必须先能获取该核心提交 |
| 不将远端验证判断为成功 | 本轮未推送；先提供核心提交，再消费或发布扩展，不能用本地结果替代远端验证 |

锁定版本为 `v0.50.1-0.20261108090514-db7fffed9a84`，由 Go 官方模块库按真实提交和提交时间生成；本地模块 ZIP 由 `golang.org/x/mod/zip.CreateFromVCS` 从同一 Git 提交生成，而非伪造发布版本。关闭 workspace 的数据库验证使用该本地代理，相关版本信息见 [module-artifact.json](module-artifact.json)。格式与约束见 [Go模块规范](https://go.dev/ref/mod#pseudo-versions)。没有向仓库写入核心绝对 replace、代理地址或关闭校验的环境配置。

正式对外使用仍需让核心提交可从远端获取，并按原定顺序发布当前 pgstore、移除扩展中的同仓开发替换、验证外部安装。此次未改变该发布边界。

作者复核评分：代码质量92、测试覆盖90、规范遵循94；需求匹配95、架构一致95、风险控制90；综合93，建议通过本地实现验收。评分为作者判断，独立审阅另见上述报告。

## 复跑入口

核心根目录使用 Go1.27.3 执行 `go test -race ./...`、`go vet ./...`、`scripts/verify_imports.sh`、`make test-frameworkbench`，覆盖率按现有 scripts/check_coverage.sh。contrib 在专用 Postgres 上显式指定包含新包的 GOAGENT_CORE_ROOT，执行 scripts/test-all.sh 与 scripts/lint-all.sh；真实退款案例和完整迁移入口见 [迁移指南](../../guides/MANAGEMENT_MIGRATION.md)。

## 合入本地主线后的复测

核心主线再次完整执行竞态测试：4771个通过事件、0失败、9个既有跳过；contrib主线再次执行13模块vet及真实数据库竞态测试：972个通过事件、0失败、12个既有跳过。核心新管理包独立构建通过，主线Go源码与受检核心实现db7fffed9a84一致。核心原有文档改动继续保持未提交；contrib工作区干净。

原文档文件与补丁、文档索引三方合并结果及关键原始验证日志保留在核心主仓库 `.git/merge-backups/management-core-20261108`。本轮专用Postgres验证后删除，未操作其他业务容器。
