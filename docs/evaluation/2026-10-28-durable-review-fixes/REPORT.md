# 持久化代码审核问题修复验证

日期：2026-10-28。基线提交：3bfeea7；本报告针对其后的本地修复。使用 Go 1.25。本轮没有调用付费模型，业务效果均为本地模拟；真实子进程退出/恢复测试仍在全量测试中执行。日志缩进及行尾空白已归一化。

## 三个问题的复现与结果

| 问题 | 修复前 | 修复后 |
|---|---|---|
| 同节点多 Agent 共用模型/工具日志 | A 请求 1 次、B 0 次，返回 A+A | 显式作用域 A/B，各请求 1 次，返回 A+B；重建后恢复均零请求、零工具执行 |
| 无工具/旧协议入口绕过日志 | 无工具恢复请求 2 次；旧协议工具执行 2 次 | 无工具恢复总请求 1 次；旧工具协议在持久上下文中提前拒绝，零模型/工具调用 |
| 持久图丢失类型化预览 | 普通图得到 7，持久图 Data=nil | 持久图 Data.State=7，Preview=true；最终或 Snapshot 状态 Preview=false |

对照输出见 probe-before.log、probe-after.log；独立复现程序保留为 .go.txt，避免加入项目包。可复制到临时 .go 文件后从项目根目录运行。四个初始回归测试在产品代码修改前失败，见 regression-before.log。

## 修复内容和接入变化

- 新增 `WithDurableScope`：持久化 Builder 必须设置宿主指定的稳定调用名称，同一节点内不同逻辑调用必须不同。模型与工具共享派生后的 Agent 子键；同一个 scope 表示恢复同一次调用，不表示启动新任务。非持久调用不要求 scope。
- 无工具 StreamClient 也执行模型操作日志；失败保留已知用量并停止重复请求。
- 持久模式下拒绝旧 GenerateWithTools 工具协议、同步 Execute、无流 ExecuteWithTools，以及裸 Tool。拒绝发生在模型、检索和中间件前。普通非持久执行保持兼容。
- 检测到旧节点上的无作用域 model/turn/0 日志会拒绝自动换键，避免重复执行已完成效果。宿主须核对业务效果、配置 scope 并提升工作流 Version。原生工具案例已由 v2 提升到 v3。日志检查的锁等待包含在 Agent 超时内。
- 持久流转发中间 Data 和 Event；包括零值、无 Event 的数据块。Preview 不驱动后继节点、不写入检查点。流取消时终止上游，恢复仍使用提交状态。

## 自动化覆盖

`builder/durable_entry_test.go` 验证：缺少作用域拒绝；无工具模型重放；旧协议零请求拒绝；两个 Agent 使用相同工具名和模型调用 ID 仍得到不同效果键；重建图和 Agent 后恢复不重跑；失败用量保留；同步入口拒绝；旧检查点拒绝迁移；日志锁等待遵守超时。

`workflow/durable_preview_test.go` 验证：带事件与无事件零值预览；成功、失败及取消时的持久状态；Preview 标记；取消后生产者退出。原有原生工具进程恢复测试随作用域配置及示例版本升级后继续通过。

## 检查结果

- `GO_TEST_MODE=true go test -race -count=1 -timeout 10m ./...`：65 个有测试包全部通过，无 race，见 race.log。
- `go build ./...`：通过，见 build.log。
- `golangci-lint run ./...`：0 issues，见 lint.log。
- `bash scripts/verify_imports.sh`：0 findings，见 imports.log。

本轮未重新验证真实 Provider 网络协议；2026-10-27 的三模型报告保留历史结论，不作为本次新增测试。FileStore 仍是单机持久化，外部业务效果仍需目标端原子幂等。完整接入约束见 [原生编排指南](../../guides/NATIVE_ORCHESTRATION.md#多-agent-作用域与入口约束)。
