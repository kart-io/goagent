# Harness 独立项目迁移

GoAgent 专注执行机制：Runnable、Builder 原生循环、Graph、durable、budget、observe、Dispatcher/Pipeline、Handle 与循环钩子。
独立项目 GoAgent Harness 专注可配置的 Agent 装配：Preset、计划模式、压缩、技能、子 Agent 组件、Claude Code 命令钩子与编码工具。

Skills 的通用加载现由主仓顶层 `skills` 和可选 `tools/skill` 提供；Harness 负责技能配置、授权、
提示词与 Manifest 装配。普通业务无需引入 Harness 即可使用[技能目录与资源读取](SKILLS.md)。

## 路径对照

| 原 GoAgent 路径 | 新项目路径 |
|---|---|
| `github.com/kart-io/goagent/harness/preset` | `github.com/kart-io/goagent-harness/preset` |
| `github.com/kart-io/goagent/tools/coding` | `github.com/kart-io/goagent-harness/tools/coding` |
| `examples/tools/09-coding-agent` | `goagent-harness/examples/coding-agent` |
| `examples/tools/10-plan-agent` | `goagent-harness/examples/plan-agent` |

新项目是独立仓库 [kart-io/goagent-harness](https://github.com/kart-io/goagent-harness)，具有独立 Go module、许可证、文档、测试与提交历史；原始实现来源记录在其 NOTICE 中。
GoAgent 自 v0.27.0 起不再包含上表的包。迁移只改 import，原有配置文件路径、格式与装配 API 不变；不保留旧包的兼容转发。
仅使用 Builder 或工作流的业务无需引入新项目。

## 保留在核心

`harness/env` 保留环境能力接口、LocalFS 与 Executor 等实现；`tools/policy`、`tools/spill` 继续服务任何工具。
新项目直接消费这些公开接口，不复制模型循环、预算或日志。`scripts/apicheck` 禁止核心生产代码及示例反向导入新模块。

## 本地验证

Skills 公共包迁移尚未发布到 Harness 的版本依赖；当前应直接使用临时 workspace 验证两个 checkout。
从 GoAgent 根目录运行：

```bash
(
set -euo pipefail
core_dir="$PWD"
harness_dir="$(cd ../goagent-harness && pwd)"
workspace_dir="$(mktemp -d)"
trap 'rm -rf "$workspace_dir"' EXIT
(cd "$workspace_dir" && env -u GOROOT GOWORK=off go work init "$core_dir" "$harness_dir")
env -u GOROOT GOWORK="$workspace_dir/go.work" go -C "$harness_dir" test -race ./...
env -u GOROOT GOWORK="$workspace_dir/go.work" go -C "$harness_dir" build ./...
)
```

发布时更新 Harness 的 GoAgent 版本依赖后，再运行包含独立模块门禁的脚本：

```bash
# 在 goagent-harness 克隆目录运行，参数为本仓库路径（默认 ../goagent）。
./scripts/verify-goagent.sh ../goagent
```

脚本先以 `GOWORK=off` 验证已发布依赖，再临时创建 workspace 测试本地两个模块，不写入本地 replace 或修改现有 go.work。
新项目的 `integration` 测试通过公开 API 验证 Preset 装配、工具审批、持久图等待、重建宿主后的批准续跑和完成后回放；迁出的原有测试覆盖技能、子 Agent、计划模式、压缩与交互。

## 版本

GoAgent Harness 的版本依赖以其 `go.mod` 为准；本地 workspace 不会将新公共包的依赖传递给下游。
独立发布前必须选择包含 `skills`、`tools/skill` 的可获取 GoAgent 版本并通过 `GOWORK=off` 检查。

新项目中的历史设计文档保留来源；本仓库的 Harness 历史设计不再代表上层组件的维护位置。
