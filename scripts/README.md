# 验证与迁移脚本

推荐入口：

- `verify_imports.sh`：AST 导入层级检查，扫描当前仓库真实模块路径。
- `provider_regression.sh [REPORT.jsonl]`：离线 Provider/Builder 契约与竞态回归，无自动重试。
- `provider_live.sh gemini|deepseek|openai NEW_DIRECTORY`：有请求上限的真实合成测试，需显式配置相应模型和密钥，见 [持续回归](../docs/guides/PROVIDER_REGRESSION.md)。

其他脚本：

- `check_coverage.sh`：读取已有的 `coverage.out`（先运行 `go test -coverprofile=coverage.out ./...`），检查不含示例的整体覆盖率（门槛 45%）及关键文件覆盖率。
- `create_release.sh <version> [alpha|beta|rc]`：创建并推送发布 tag。
- `verify_all_examples.sh`：以 5 秒超时逐个运行 `examples/` 下的 `package main` 程序并写出结果日志。
