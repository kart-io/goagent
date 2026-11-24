# contextpolicy 上下文治理

[English](README.md) | [中文](README_CN.md)

`contextpolicy` 让较长的 Agent 对话保持在模型上下文之内。`Offload` 把超长工具结果替换为引用，由模型分页读回；`Compaction` 把较早的对话历史替换为摘要；`RecallTool` 让模型取回被改写掉的原始消息。`ResultStore` 保存被卸载结果背后的不可变证据。Builder 通过 `WithOffload` 和 `WithCompaction` 把 `Offload` 与 `Compaction` 作为带身份的 PreStep 安装。

## 用法

```go
offload, err := contextpolicy.NewOffload(contextpolicy.OffloadConfig{
    Store:                contextpolicy.NewMemoryStore(), // 持久运行使用 contextpolicy.NewDurableResultStore(store)
    MaxToolResultRunes:   8000,
    KeepRecentToolRounds: 2,
    ReadPageRunes:        1024,
})
if err != nil {
    return err
}
sessions := session.NewMemoryStore()
compaction, err := contextpolicy.NewCompaction(contextpolicy.CompactionConfig{
    MaxTokens:  100000,
    KeepTokens: 25000,
    Model:      summaryClient, // 为 nil 时不做摘要，只省略旧消息并附说明
})
if err != nil {
    return err
}
recall, err := contextpolicy.NewRecallTool(sessions)
if err != nil {
    return err
}
agent, err := builder.NewSimpleBuilder(client).
    WithTools(searchTool, offload, recall). // offload 本身也是 read_context_result 工具
    WithOffload(offload).
    WithSessionLog(sessions).
    WithCompaction(compaction).
    Build()
```

宿主掌握运行身份时，由宿主提供结果作用域并负责释放：

```go
ctx = contextpolicy.WithResultScope(ctx, runKey)
// ... 用 ctx 运行 Agent，可多次恢复 ...
// 确定该运行不再恢复后释放其证据：
return offload.Release(ctx)
```

## 主要 API

| 标识符 | 说明 |
|---|---|
| `NewOffload(OffloadConfig)` | 校验配置并返回 `Offload`；它既是 PreStep 组件，也是 `read_context_result` 工具 |
| `Offload.Release(ctx)` | 删除当前结果作用域的全部证据和引用表 |
| `Offload.Stats()` | 该实例累计替换为引用的结果数 |
| `NewCompaction(CompactionConfig)` | 校验配置、填充默认值并返回 `Compaction` |
| `NewRecallTool(store)` / `NewRecallToolConfig(store, RecallConfig)` | 读取会话日志的 `recall_history` 工具；必须与 `WithSessionLog` 使用同一个 store |
| `ResultStore` | 证据存储接口：`PutIfAbsent`、`Get`、`Release`、`PruneReleased` |
| `NewMemoryStore()` | 并发安全的内存 `ResultStore` |
| `NewDurableResultStore(durable.Store)` | 基于持久存储的 `ResultStore` |
| `WithResultScope(ctx, runKey)` / `ResultScope(ctx)` | 提供 / 读取证据的可信运行归属 |
| `EstimateTokens(msg)` | 默认估算：UTF-8 字节数的三分之一 |
| `IsSummary`、`SummaryBody`、`KeepUserMessages` | 处理摘要消息和编写自定义 `Keep` 函数的辅助函数 |

## 行为说明

- 限额由调用方决定。`CompactionConfig` 与 `RecallConfig` 中 0 表示不限制，负值被拒绝。`CompactionConfig.MaxTokens` 为 0 时从不主动压缩；`KeepTokens` 为 0 时取 `MaxTokens/4`，因此 `MaxTokens` 为 0 时必须设置它。
- `OffloadConfig.MaxToolResultRunes` 必须为正数。`ReadPageRunes` 为 0 时一页返回剩余全部内容。`ID`、`Version`、`Namespace` 为空时分别默认为 `offload`、`v3`、`results`；切换存储后端时须更换 `Namespace`。
- `Offload.Reduce` 的结果只取决于对话和配置，同一对话重复调用得到逐字节相同的结果。最后一次请求（`final` 为 true）时引用会还原为原文。
- 请求因超出上下文而失败时，Builder 强制压缩一次（`Recover`，只保留最近一次交互）后重试。
- 两个组件的 `Identity()` 都涵盖 ID、版本和各项限额，并写入持久 journal；配置变更后恢复旧运行会被拒绝。`Keep` 或 `Annotate` 的行为变化时须修改 `CompactionConfig.Version`。
- 一个 `Offload` 由多个执行并发共享，引用表按结果作用域隔离。非持久执行的临时作用域在执行结束时释放；通过 `WithResultScope` 提供的作用域（包括持久运行）由宿主释放，释放后不得复用该运行键。
- `ResultStore` 从不以不同证据覆盖同一逻辑键。`Release` 之后，该运行键迟到的读写返回 `errors.ErrResultScopeReleased`，直到 `PruneReleased` 回收墓碑；其 `olderThan` 应大于最长的重试或恢复窗口，并定期调用。
- 只有底层存储实现了 `durable.Scoped`（如 `durable.FileStore`）时，`NewDurableResultStore` 才能按运行整体释放；否则 `Release` 与 `PruneReleased` 返回错误。
- `RecallTool` 从请求上下文取得会话 ID（`tools.WithSessionID`，Builder 默认填充），因此只能读取当前会话。

## 另见

- [工具上下文：结果卸载与历史压缩](../../docs/guides/CONTEXT_POLICY_CN.md)
- [可组合编排：上下文结果卸载](../../docs/guides/COMPOSABLE_ORCHESTRATION_CN.md#上下文结果卸载)
- [Builder API 参考](../../docs/guides/BUILDER_API_REFERENCE_CN.md)
