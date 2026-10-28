# R2 分支整合与评测资料恢复验证

日期：2026-10-28。目标分支：`codex/composable-orchestration-r2`。远端基线：`92bf21b`；恢复提交：`b558e95`；本轮测试代码：`5fda667dffcb0af18daafafe40eda5ffcd9fbe66`。环境为 Linux、Go 1.25.0，使用 `GO_TEST_MODE=true`。本次没有调用真实或付费模型，没有重跑性能排名。

## 资料保留

撤销清理提交后，196 份日志与 6 个 Python 脚本全部恢复，逐文件与 `0b461d6` 的内容比较相同。见 [恢复清单及 SHA256](restored-manifest.json) 和 [归档检查](evidence.log)。同时恢复相关报告和复现说明；97 份已有评测 JSON 可以解析，变更文档的 106 个本地链接路径均存在。

[master 修复阶段报告](../2026-10-28-durable-review-fixes/REPORT.md) 和原始日志保持不变；其 65 个测试包的结果属于之前的基线，不作为本次 R2 全量结果。新增[保留约定](../RETENTION.md)，正式评测资料随代码保存，Python 缓存仍被忽略。

## 整合内容

- 将 master 上的三项持久化修复移植到 R2：Agent 调用作用域、入口日志保护、类型化预览。
- 保留 R2 的共享预算、模型用量登记、策略工厂和 OTel 图／节点／Agent／模型观测逻辑。无工具请求沿用 R2 的 `model/answer/0` 日志及统一预算入口。
- 旧的无作用域 `model/turn/0` 和 `model/answer/0` 均提前拒绝迁移，避免重复执行。已有宿主须配置 `WithDurableScope`，核对业务效果后迁移工作流版本。
- 更新组合订单父／子 Agent 和策略测试的稳定作用域。首次整合测试因这些接入点缺少作用域失败，保留 [首次失败日志](integration-first.log)，修正配置后通过。
- 补充交叉验证：两个 Agent 在同一节点使用相同工具调用 ID 时结果与效果键隔离，共享预算保持 2 次请求、10 Token，重建恢复不追加用量；无工具 Agent 使用独立 scope，共享 2 次请求、14 Token，第三次请求在模型前被预算拒绝，重建恢复零请求。

## 检查结果

| 检查 | 结果 | 证据 |
|---|---|---|
| `go test -race -count=1 -timeout 10m ./...`（主模块） | 75 个有测试包通过，无 race | [race.log](race.log) |
| `go build ./...` | 通过 | [build.log](build.log) |
| `golangci-lint run ./...` | 0 issues | [lint.log](lint.log) |
| `bash scripts/verify_imports.sh` | 通过 | [imports.log](imports.log) |
| 独立 `evaluation/frameworkbench` 模块 `go test -race -count=1 -timeout 5m ./...` | 通过 | [framework-contracts.log](framework-contracts.log) |
| `python3 -m unittest -v test_statistics` | 3 项通过 | [statistics.log](statistics.log) |
| 恢复的 6 个 Python 脚本语法解析 | 通过 | [evidence.log](evidence.log) |

针对 Builder、Workflow 和编排案例的额外 race 验证见 [focused-race.log](focused-race.log)。组合订单断言首次 4 次模型调用、8 Token、创建并关闭一个子会话；恢复新增调用为 0、预算不变；同样验证独立进程恢复与现有 OTel trace 关联。业务 DevOps、文章审核、自动回复及原生多工具进程恢复测试也包含在主模块全量测试中。

日志仅归一化行尾空白。Python 外部框架 SDK 的模型／流测试本轮未重跑；恢复脚本与历史文件的内容校验不能替代其依赖环境验证。历史真实模型记录与历史性能报告保留原日期、环境和适用范围，不作为本轮新测结果。
