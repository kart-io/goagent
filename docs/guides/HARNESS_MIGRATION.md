# Harness 独立项目迁移

GoAgent 专注执行机制：Runnable、Builder 原生循环、Graph、durable、budget、observe、Dispatcher/Pipeline、Handle 与循环钩子。
独立项目 GoAgent Harness 专注可配置的 Agent 装配：Preset、计划模式、压缩、技能、子 Agent 组件、Claude Code 命令钩子与编码工具。

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

```bash
# 在 goagent-harness 克隆目录运行，参数为本仓库路径（默认 ../goagent）。
./scripts/verify-goagent.sh ../goagent
```

脚本先以 `GOWORK=off` 验证已发布依赖，再临时创建 workspace 测试本地两个模块，不写入本地 replace 或修改现有 go.work。
新项目的 `integration` 测试通过公开 API 验证 Preset 装配、工具审批、持久图等待、重建宿主后的批准续跑和完成后回放；迁出的原有测试覆盖技能、子 Agent、计划模式、压缩与交互。

## 版本

GoAgent Harness 目前只有 `main` 分支，尚未发布版本标签，其 go.mod 声明的 GoAgent 依赖为 v0.23.0。

新项目中的历史设计文档保留来源；本仓库的 Harness 历史设计不再代表上层组件的维护位置。
