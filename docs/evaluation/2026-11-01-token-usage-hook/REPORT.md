# Token 统计专用钩子交付

日期：2026-11-01。基线 `af0ce6a`。Go 1.25.0，darwin/arm64。

## 已实现

- `budget.UsageHook`、`UsageEvent`、`WithUsageHook`，以及 Builder.WithUsageHook 便捷入口。
- 回调包含逐次 Model 证据、Statistics 统一累计报表、HasStatistics、StatisticsError、ErrorKind。
- 父子继承、子钩子显式替换、Observer 独立共存、取消后已知消费通知、查询/回调 panic 隔离。
- 没有预算时仅通知单次消费，不创建隐式账本；累计与 Self/Inclusive/角色统计继续查询同一个预算实现。
- 示例改为直接使用父 Builder 钩子，真实继承到两个子 Agent，回放通知与新增消费分开。

## 验证

所有命令使用 `env -u GOROOT GOTOOLCHAIN=go1.25.0` 前缀。

| 命令 | 结果 | 证据 |
| --- | --- | --- |
| `go test ./...` | 根模块通过，未变更包可能使用缓存 | [suite.log](suite.log) |
| `go test -race ./builder ./core/budget ./core/observe ./execution` | 通过 | [race.log](race.log) |
| `go test -race ./builder ./core/budget -run '^TestUsageHook' -count=5` | 五轮通过 | [stress.log](stress.log) |
| `go run ./examples/agent-usage` | Root=18，两次回放前后不变；hook live=5、replay=2 | [example.log](example.log) |
| 格式、文档链接及 diff | 通过 | [verification.log](verification.log) |

回归覆盖无预算/无Observer、父子继承与覆盖、Self/Inclusive/Root、回放不新增请求、失败已知用量、非法证据、元数据副本、查询失败与panic、hook panic、取消通知及锁外只读重入查询。

## 审核与修复

接口缺失先编译 RED，见[初始日志](red-api.log)。查询 panic 原先中断通知，增加回归后改为保留单次证据并设置 StatisticsError，见[查询 RED](red-query.log)。

独立只读审核发现的两项 Important 均已 RED→GREEN：

1. 无预算/无AgentID、只有钩子时，旧工具选择请求会漏计。现在在付费模型请求前拒绝非原生工具选择路径。
2. RecordEvidence 在 Lock/Load 阶段失败曾用零值覆盖已知消费。现在失败时保留原始有效证据、AccountingCommitted=false，累计报表仍反映账本中的 unknown。

真实执行链回归见[审核 RED](red-review.log)，修复后五轮 race 与根模块通过。失败日志仅展开制表符并去行尾空白，原始 SHA256 另存。

## 契约与成本

查询与钩子在账本锁之外运行；不修改业务结果、不自动重试。启用时每次通知会查询累计账本，有额外成本；未启用不额外查询。钩子须支持并发并及时返回，5秒 context 是合作式约束，不能强制停止宿主函数。

Statistics 是时点累计，不能相加；并行时可能包含其他尝试。回放仍通知且带 Replayed，不是新增消费。没有持久事件投递或 exactly-once 承诺，可靠累计仍查询统一账本。

接入见[使用指南](../../guides/MULTI_AGENT_USAGE.md)，契约见[设计](../../design/TOKEN_USAGE_HOOK.md)。
