# 业务案例验证记录：2026-10-27

基于当前工作区的 GoAgent 原生编排与工具实现，新增 DevOps、文章审核、自动回复三个离线案例。[运行说明](../../../examples/workflow/business-cases/README.md)，源码内容摘要见 `source-sha256.json`。本次没有提交或推送。

## 验证环境与结果

- Go 1.25.0；golangci-lint 2.6.2。
- `go test -race -count=1 -v ./examples/workflow/business-cases`：通过，见 `cases-race.log`。
- `GO_TEST_MODE=true go test -race -timeout 10m ./...`：65 个测试包通过；未变化的包包含 Go 缓存结果，见 `all-race.log`。
- `go build ./...`：通过；已有 SQLite C 依赖出现 const qualifier 编译警告。
- `golangci-lint run ./...`：0 issues，见 `lint.log`。
- `bash scripts/verify_imports.sh -strict`：0 findings。

## 案例实跑

使用构建后的 CLI 在新临时目录中执行默认输入及全部 6 个 JSON 夹具，并对每个已完成运行再次执行，结果均保持一致。详见 `smoke.json`。

| 场景 | 输入 | 最终结果 | 新增模拟效果 |
| --- | --- | --- | --- |
| DevOps | 默认故障，审批通过 | recovered | 1 |
| DevOps | 健康服务 | healthy | 0 |
| DevOps | 健康信号与错误率冲突 | escalated | 0 |
| 文章审核 | 默认合格文章 | published | 1 |
| 文章审核 | 拒绝标记 | rejected | 0 |
| 文章审核 | 人工复核标记，审批通过 | published | 1 |
| 自动回复 | 营业时间 | replied | 1 |
| 自动回复 | 未知问题 | human_handoff | 0 |
| 自动回复 | 密码重置 | replied | 1 |

另外，三个场景分别启动真实子进程，在效果持久化后以 86 退出，再启动新进程恢复。每个场景最终只有一条效果记录；相同 run 重放和不同 run 接收同一业务请求都复用原收据。审批场景额外检查未审批、错误身份时零效果。

测试还覆盖 Schema 拒绝非法参数/目标、业务 ID 内容冲突、并发重复消息、工具失败与取消后恢复、Unicode 长度边界、拒绝优先、未知问题不生成答案及非法 CLI 输入。

## 验证边界

模型调用 0 次，外部业务 API 调用 0 次。本次验证的是编排、参数约束、持久化、审批与业务去重；不证明真实运维操作有效、文章语义审核准确或生成式回复质量。真实业务仍需认证身份、领域策略和目标服务幂等适配器。
