# 工具上下文与历史摘要

新应用使用 `agent.AsRunnable().Invoke/Stream`。`WithContextPolicy` 显式启用上下文治理，要求配置工具并使用 `llm.ToolStreamClient`；OpenAI 与 DeepSeek 专用 Provider 均支持该接口。ExecuteWithTools 在流式 Provider 上委派同一管线，也应用此策略；旧 Execute 会明确报错并提示迁移，不再静默忽略。

```go
agent, err := builder.NewSimpleBuilder(client).
    WithTools(searchTool).
    WithMaxIterations(12).
    WithContextPolicy(builder.ContextPolicy{
        MaxToolResultRunes:   8000,
        KeepRecentToolRounds: 2,
        ReadPageRunes:        1024,
        ExcludeTools:        []string{"payment_receipt"},
    }).Build()
// 检查 err 后：
output, err := agent.AsRunnable().Invoke(ctx, question)
```

## 大结果卸载

每次模型请求前，保留最近指定数量的完整工具回合；一轮多个工具视为同一回合。更早且超过阈值的工具正文改为带 `context_result` 引用的 JSON，原始全文保存在本次执行的内存中。assistant 的工具调用、reasoning_content、结果角色和 tool_call_id 保持不变。

自动提供 `read_context_result` 工具，以 `key` 和可选 `offset` 读取原文；offset 按 Unicode 字符计数，每页默认 1024 字符，最大可配置 16384。返回 `text`、`next_offset` 和 `done`。引用只在当前执行有效；不能读取其他执行的结果、任意路径或历史会话内容。工具名称冲突在 Build 时拒绝。调用读取工具也消耗任务回合，不追加隐藏模型请求。

排除名单和读取工具本身的输出不卸载。`KeepRecentToolRounds=0` 表示不保留完整回合；`MaxToolResultRunes=0` 关闭卸载。选择足够大的正文阈值：引用封装也占字符，极小阈值可能增加输入。这里只减少模型输入，不限制进程内存；完整证据保留至执行结束，不持久化，也不提供跨进程恢复。

最后一个模型请求仍用于最终回答或显式直返工具。由于无法再调用读取工具后总结，这一回合恢复已卸载的原始证据，也不进行新的卸载；因此不承诺最终请求的上下文大小下降。需要在最终回合前通过工具提取业务字段，或增加任务预算。该机制不保证整个输入小于模型窗口。

`WithReturnDirectTools` 仍返回完整工具原文，不应用截断或摘要。最终展示读取 done.Output，暂态文本不代表最终结果。

## 显式历史摘要

```go
policy := builder.ContextPolicy{
    MaxToolResultRunes: 8000,
    KeepRecentToolRounds: 2,
    HistorySummary: &builder.HistorySummaryConfig{
        Client: summaryClient, // 显式选择的 llm.StreamClient，可与主模型相同。
        TriggerRunes: 24000,
        KeepRecentMessages: 6,
        MaxTokens: 1000,
    },
}
// builder.WithMemory(...) 启用的历史由原 MaxConversationHistory 先行限制。
// builder.WithContextPolicy(policy)
```

每次执行开始，加载到的普通对话历史超过 TriggerRunes 且存在可压缩前缀时，进行一次摘要。保留 system 消息、最近指定数量的历史消息，以及当前问题和检索证据。摘要放在标为不可信数据的 user 消息中，不提升为 system 指令。持久化记忆中的原始历史不被修改；后续执行可能再次摘要。

触发阈值按历史 Unicode 字符总量计算，不包含 system 和当前请求。KeepRecentMessages 至少为 1。摘要正文及标记必须比替换的历史更短；空摘要、工具调用、截断、上游错误和取消直接失败，不回退成未经声明的额外请求。

摘要计入 MaxIterations 和累计 Token 用量，至少预留一个任务模型请求；需要查工具再生成答案时，通常至少配置 3 次预算。摘要输出上限由自己的 MaxTokens 控制；摘要输入不另行截断，调用方需根据摘要模型窗口选择历史上限和触发阈值。MaxIterations 约束本层请求次数，Provider 内部重试或自定义 Client 的额外请求应使用外部计量代理限制。

## 用量与验证

- `model_calls`：摘要与任务模型请求的合计。
- `summary_calls`：发生摘要时为 1。
- `usage_reports`：真实报告用量的请求数；缺失摘要用量时 `usage_complete=false`，不伪造 Token。
- `context_offloaded_results`：本次执行累计卸载的结果数，最终回合即使恢复原文也保留此计数。

字符减少不等于 Token 或费用必然减少；摘要会增加请求，分页可能增加工具回合。当前回归覆盖执行协议、分页、预算和隔离；效果验收仍需长会话业务题集，同时比较正确率、完整请求用量和延迟。
