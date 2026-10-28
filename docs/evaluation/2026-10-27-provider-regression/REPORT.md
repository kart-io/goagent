# 2026-10-27 持续回归与业务保护改进

> 清理说明：本目录的测试日志及 Python 脚本已删除；历史测试结论和现存 JSON/Markdown 汇总保留。

基线提交 `9de4929`，以下真实测试在本轮变更提交前运行。Provider 实现未变更；新增脚本调用原真实验证入口。每个套件只运行一次，无失败重试或挑选结果。源码哈希与脏工作区说明保存在 [run-context.json](run-context.json)。

| 真实套件 | 成绩 | 实际上游请求 | 已知总 Token |
|---|---:|---:|---:|
| GeminiProvider 原生，gemini-2.5-flash / gemini-embedding-001 | 6/6 | 10：8 生成 + 2 向量 | 1915，仅生成 |
| DeepSeek 专用 Provider，deepseek-flash，租户订单 | 6/6 | 8 生成 | 3584 |

Gemini 覆盖 Invoke、Stream、工具循环、工具直返、连续两次工具查询和真实向量；向量请求没有用量字段。DeepSeek 检查答案、工具参数和调用契约，含跨租户隔离、缺失订单、证据不足。合成只读案例不证明生产业务质量或与其他框架的性能差异。

原始证据：[Gemini](gemini-native.json)、[DeepSeek 逐题结果](deepseek-results.jsonl)、[汇总](deepseek-summary.json)、[运行清单](deepseek-manifest.json)、[题集](deepseek-dataset.json)。

## 本轮实现

1. `workflow/refund.Service`：独立事务存储、授权与策略接口；业务 ID 去重、审批期限、版本/事实复核、累计退款限额。SQLite 案例成为适配器。新增授权撤销、事务提交失败、过期后的回执重放测试，保留原跨进程与并发超额测试。
2. Provider 离线契约 CI 和有界真实定时工作流。缺少配置明确失败，付费工作流默认需仓库显式开启；本轮未修改远端环境、密钥或开启定时调用。
3. `Graph.CompileDurable`：复用 Store，实现串行节点、条件分支与有界循环的节点边界恢复；节点输出和路由分别保存，审批绑定节点访问，稳定副作用键支持重放去重。包含真实子进程崩溃与再启动测试。
4. AST 迁移候选清单与真实导入检查。修复旧导入脚本扫描目录和包路径错误，精确记录两个旧推理工厂的依赖例外。推荐案例的迁移检查加入 CI。

## 验证

- Go 1.25 全库 `go test -short -timeout 5m ./...`：64 个测试包通过，日志（原文件已清理）。
- Provider、Builder、agentbench：race + short + count=1，6 个测试包、820 个测试及子测试通过，0 失败，[摘要](contract-summary.json)。
- 图恢复、退款组件、SQLite 案例、durable、AST 检查器：竞态测试通过，日志（原文件已清理）。AST 精确例外的新增断言另行复测通过。
- golangci-lint 2.6.2：修正一处静态表达式建议后 0 issues，日志（原文件已清理）。
- 导入扫描 0 findings；三个推荐案例目录迁移候选 0；Shell 语法、两个工作流 YAML 解析、diff 空白检查通过。
- 检查新增结果中不存在当前配置的 API Key。SQLite 依赖仍有已知 C const 编译警告，测试未失败。

## 明确边界

持久图首版不接受 AddParallel，不恢复节点内部 Agent 栈、不重放流事件、不提供跨主机接管；并行分支恢复需独立实现。Store 需可靠本地锁和原子保存，外部效果仍至少一次执行。闭包和业务策略变化必须提升显式版本。

退款接口已就绪，真实身份、订单事务服务和支付网关未提供；当前适配器仍是合成演示。迁移检查只给出语法候选，不做类型推导或自动替换。保持现有主模块及导入兼容，本轮未进行破坏性依赖拆分。

## 提交前全量验证

用户要求测试并提交后，使用 Go 1.25 执行 `GO_TEST_MODE=true go test -race -timeout 10m ./...`（未使用 `-short`）：64 个测试包全部通过，退出码 0。完整日志（原文件已清理）。`go build ./...`、golangci-lint 2.6.2（0 issues）、严格导入检查均通过，见[验证清单](precommit-verification.json)。源码哈希与前次真实 Gemini / DeepSeek 测试记录一致，本次提交验证未重复付费调用。
