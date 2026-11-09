# 验证与迁移脚本

推荐入口：

- `make test-management`：无需数据库的管理状态机、HTTP 和适配器回归。
- `make compatibility-postgres`：在完整跨仓验证后检查关键实库恢复场景，要求 `GOAGENT_PGSTORE_DSN` 与本次专用测试容器 `GOAGENT_PGSTORE_CONTAINER`。测试会重启该容器并修改测试数据库配置，不能指向业务数据库。每模块默认15分钟，可用 `COMPAT_TIMEOUT` 覆盖；通用 `make compatibility` 仍默认5分钟。只读复核已有报告可运行 `go run ./scripts/compatcheck -verify-postgres-report REPORT_DIRECTORY`。

实库证据要求关键契约及其全部后代通过，相关模块的包和测试生命周期完整，日志统计与摘要一致。拒绝失败、损坏 JSON、跨模块测试事件及缺失终态；允许普通 Go 构建诊断、依赖构建输出、无测试文件的包级跳过，以及关键契约之外的辅助测试跳过。

- `verify_imports.sh`：AST 导入层级检查，扫描当前仓库真实模块路径。
- `provider_regression.sh [REPORT.jsonl]`：离线 Provider/Builder 契约与竞态回归，无自动重试。
- `go run ./scripts/compatcheck -root .. -out NEW_DIRECTORY`（或 `make compatibility`）：发现本地 goagent-harness、goagent-examples、goagent-contrib 的全部模块（含嵌套模块），通过临时 modfile 将本地模块绑定到当前源码，关闭外部 go.work，逐模块执行完整 race 测试。缺少仓库、定位错误或测试失败即失败；继续收集其他模块结果，不改下游依赖文件。输出原始 JSONL、核心依赖定位与 `summary.json`；其中跳过的测试必须另行验收。`-timeout` 默认 5 分钟，发现与版本探测共享一个启动时限，后续每个模块仍各有独立的总时限。启动阶段失败会直接报错退出，可能尚未生成摘要。

跨仓测试通过受控 `GOFLAGS` 把临时 modfile 传给测试内的 `go build/run`，包括含空格的路径；测试若主动清除环境或显式指定另一 modfile，则需单独核对其源码定位。Unix 上 Go 命令在独立进程组中运行，SIGINT、SIGTERM 与超时会清理同组后代并保留摘要；主动脱离进程组的程序不在此范围。非 Unix 平台沿用 Go `CommandContext` 的父进程取消，不声明进程树清理保证。
- `provider_live.sh minimax`：国内 Anthropic 有界回归，读取专用 `MINIMAX_CN_API_KEY`，最多三个请求，不生成报告。Gemini、DeepSeek、OpenAI 仍使用 `provider_live.sh gemini|deepseek|openai NEW_DIRECTORY`，见 [持续回归](../docs/guides/PROVIDER_REGRESSION.md)。
- `go run ./scripts/stabilitysoak -duration 24h`：持续离线混合负载，临时存储自动清理，结果仅输出到终端。配置、状态查询和发布顺序见[稳定化验证](../docs/guides/STABILITY_VALIDATION.md)。
- `hitl_live.py --live --out NEW_DIRECTORY`：Gemini / DeepSeek 的人工暂停、修改、跨进程恢复与拒绝验证，默认最多 6 次模型请求；`--review DIRECTORY` 仅离线复核保存结果。见 [HITL 评测](../evaluation/hitlbench/README.md)。

其他脚本：

- `check_coverage.sh [PROFILE]`：默认读取 `coverage/coverage.out`；CI 或自定义文件可显式传入路径。由 `scripts/coveragecheck` 按语句数加权检查：整体 75%，`core/durable` 70%，`core/budget`、`workflow`、`builder` 各 75%，`multiagent` 85%，`management`、`management/adapters` 各 75%，`management/httpapi` 80%（精确包路径，不混入子包），以及既有 Provider 单文件阈值；缺少关键数据即失败。`make coverage` 生成报告并执行门禁，不依赖 Bash 关联数组或 bc。
- `create_release.sh <version> [alpha|beta|rc]`：创建并推送发布 tag。
- `verify_all_examples.sh`：以 5 秒超时逐个运行 `examples/` 下的 `package main` 程序并写出结果日志。
