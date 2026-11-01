# Pi 设计核心补齐验证

日期：2026-11-01。基线：GoAgent `fcff360`。分支：`codex/pi-core-contracts-20261101`。Go：1.25.0，darwin/arm64。

## 交付范围

- 新增 `builder.Handle.PendingInputs` 和 `PendingInput`，快照查询仍在队列的输入，不改变领取或执行策略。
- 修复第一轮失败时无条件清空尚未执行 Followup 的问题；在真正决定开始下一轮时才取出 Followup。
- 新增五项实际 Builder/Handle 回归：失败、结束检查边界、迭代上限、取消及等待超时、快照隔离与并发查询。
- 新增可运行 Example，展示同一 workflow 直接运行或适配为 Dispatcher 工具。
- 补齐 Pi 能力对照、GoAgent 采纳边界和组合指南；修正既有文档对嵌套可恢复工作流的过时描述。

## RED → GREEN

先用现有 Builder/Handle 执行链复现：在 TurnStopping 中阻塞，接收 Followup，再让本轮返回错误。原实现将未执行的输入清空，预期 `second`，实际 nil，见[失败日志](red-followup.log)（归档时仅展开制表符并去除行尾空白，原始日志 SHA256 另存）。随后为公开快照写测试，原实现缺少类型和方法，见[接口缺失日志](red-api.log)。

最小修改只移动 Followup 清空时机并增加锁内快照；[Handle 及停止钩子回归](handle.log)通过。所用脚本模型只替代外部供应商，工具循环、Handle、错误传播与状态转换均使用实际实现。

## 命令与结果

所有 Go 命令使用 `env -u GOROOT GOTOOLCHAIN=go1.25.0`，避免本机默认工具链与标准库版本不一致。

| 命令 | 结果 | 证据 |
| --- | --- | --- |
| `go test ./builder -run 'TestHandle\|TestTurnStopping' -count=1` | 通过 | [handle.log](handle.log) |
| `go test -race ./builder ./tools ./core/durable ./core/session ./workflow` | 通过 | [race.log](race.log) |
| `go test ./tools -run '^ExampleNewRunnableTool_workflow$' -v` | 通过，两种路径输出符合预期 | [example.log](example.log) |
| `go test ./...` | 根模块通过，包含已有嵌套审批、预算、会话及工具执行测试；部分未变更包使用 Go 缓存 | [suite.log](suite.log) |
| `git diff --check`、修改文档本地链接检查 | 通过 | [verification.log](verification.log) |

独立只读代码审查未发现实质问题。审查者尝试用默认 Go 运行测试时遇到 go1.27.1/1.27.2 的标准库/编译器版本错配；不把那次尝试计为通过。上表是主执行者显式指定 Go 1.25.0 后取得的结果。

## 定位与限制

没有新增依赖、默认并发、权限或重放策略；没有增加第二套 Agent 循环、后台服务或沙盒供应商。原有用户未跟踪的 Sandbox 调研文件保持原样。

PendingInputs 不是持久收件箱、业务成功确认或可安全自动重试的消息列表。已领取输入即使之后失败也不在快照中。Wait 超时仅退出等待；终态快照须在实际执行结束后取得。输入在结束检查之后到达时可保留在快照中，不为清空它绕过迭代限制。

本轮不验收真实供应商效果、独立持久后台任务、跨主机接管或远端 Sandbox。Pi 固定源码的选择性验证属于前轮 Harness 调研，不算作本轮 GoAgent 功能验收。功能边界见[设计](../../design/PI_CORE_ADOPTION.md)及[组合指南](../../guides/AGENT_COMPOSITION.md)。
