# Pi 核心补齐：继续审核与修订

日期：2026-11-01。审核基线：`4b2d643`。分支：`codex/pi-core-contracts-20261101`。环境：Go 1.25.0，darwin/arm64。

## 审核发现与处理

| 问题 | 影响 | 修复 |
| --- | --- | --- |
| Handle 通过 AsRunnable 取消消费时先于 ExecuteStream 生产者返回 | Wait 可能已返回，OwnedRunnable.Close 仍在运行，终态及清理判断过早 | Handle 持续消费同一个执行源直到关闭；关闭前生产者释放执行权 |
| h.run 用取消原因覆盖已有错误 | 已知执行/清理失败从错误链消失 | 合并取消原因，保留原始错误类型和 errors.Is/As |
| 调用成功与下一轮领取之间已经发生取消 | 可能清空 Followup、启动无效下一轮并返回成功 | 在终态锁内检查取消原因；已取消则结束且保留尚未领取 Followup |
| 公开 error 事件在取消后允许丢弃 | 即使消费到关闭，仍可能漏掉真实 Close 错误 | 共用私有 executeStream，生产者在关闭事件通道前写权威结果；Handle 在关闭后读取，不依赖公开 error 事件 |
| Followup 恢复条件不够明确 | 重建 Start/WithResume 不会自动调度先前等待的子轮，可能只返回第 0 轮结果 | 说明由宿主保存并同步重建原轮次/合并批次；固定多轮优先持久 Graph |

前四项属于取消与终态正确性问题，均已修复。第五项修正文档并用实际持久审批场景固定能力边界，没有新增后台调度器。

## 反例与回归

新增 [handle_lifecycle_test.go](../../../builder/handle_lifecycle_test.go) 的四项测试：

1. 调用返回成功前已取消，未领取 Followup 保留且返回取消错误。
2. 取消原因与已有执行/清理错误均可通过 errors.Is 识别。
3. 真实 Start 调用 OwnedRunnable，暂停 Close：Wait 可先超时但不能提前终结 Handle；释放 Close 后，确认清理退出、执行权释放、Followup 保留，并同时返回取消与清理错误。
4. Followup 子轮审批等待后重建运行：不重投批次只回放第 0 轮；同步重建原批次后恢复审批，不重复原有模型调用或业务效果。

前三项先在旧代码上失败，见[初始 RED](red-lifecycle.log)。独立审查进一步发现公开终态事件可能被取消丢弃；让真实 Close 返回独立错误后，以 20 次重复运行复现，见[清理错误 RED](red-cleanup.log)。修复后执行同组 Handle、既有持久回放和嵌套审批测试的 20 轮 race 验证。

归档失败日志仅展开制表符、去除行尾空白，原始日志 SHA256 记录在 checksums.json。执行路径没有用模型桩替换 Handle/Dispatcher/Graph；受控模型只替代外部供应商。

## 验证结果

Go 命令统一前缀：`env -u GOROOT GOTOOLCHAIN=go1.25.0`。

| 命令 | 结果 | 证据 |
| --- | --- | --- |
| `go test -race ./builder -run '^TestHandle\|TestLoopHooksDurableReplay\|TestNestedApproval' -count=20` | 通过 | [stress.log](stress.log) |
| `go test -race ./builder ./tools ./core/durable ./core/session ./workflow` | 通过 | [race.log](race.log) |
| `go test ./...` | 根模块通过，未变更包可能使用缓存 | [suite.log](suite.log) |
| 文档本地链接、源码格式与 diff 检查 | 通过 | [verification.log](verification.log) |

独立只读审查覆盖持久输入回放、Followup 恢复、预算/回调与新消费路径；其发现的清理错误传递缺陷已按真实生产链路 RED→GREEN 修复。未留下未处理的实质审核项。

## 定位与限制

公开 ExecuteStream 及一般 AsRunnable 保留原取消消费约定；Handle 内部可靠取得同一执行源的终态，不新增公开 API、依赖、执行循环或默认调度服务。普通流取消后的 error 事件仍是 best-effort。

工具、钩子和清理函数必须合作返回；Wait 的独立 context 可以限制等待，但超时不表示清理结束。等待执行源退出也不证明外部进程、数据库或任意脱离 context 的后台工作已经停止。取消不撤销副作用。

Handle 仍是进程内输入队列。Followup 调度不持久化，PendingInputs 不含已领取的消息；仅重建 Start/WithResume 不能证明宿主持久收件箱中的所有任务已完成。没有引入持久后台调度、跨主机接管或 Sandbox 产品能力。
