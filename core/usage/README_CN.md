# usage Agent 身份与用量证据

[English](README.md) | [中文](README_CN.md)

`usage` 定义纯值的 Agent 身份与模型用量证据。它是 `core/budget`（登记并归属证据）、`core/observe`（事件中携带证据）与 Builder 之间的共用词汇。调用方用它从 context 读取当前 Agent 调用（例如在钩子或工具 Pipeline 中），解读 `budget.UsageEvent` 或 `observe.Event` 中的逐尝试用量，或通过 `usage.ReasoningConfig` 显式设置推理请求。

## 用法

```go
ctx := usage.WithInvocation(context.Background(), usage.Invocation{
    Owner: usage.Owner{RunID: "r", AgentID: "root", AgentCallKey: "r"},
})
ctx = usage.WithInvocation(ctx, usage.Invocation{
    Owner: usage.Owner{RunID: "r", AgentID: "child", AgentCallKey: "c", ParentAgentCallKey: "r"},
})
in, _ := usage.FromContext(ctx) // 子调用
chain := usage.Lineage(ctx)     // [root, child]，副本

// 流的前一帧只报告总量，后一帧补充 prompt/completion。
e, err := usage.Merge(usage.Evidence{}, usage.Evidence{
    KnownFields: usage.Total, Tokens: interfaces.TokenUsage{TotalTokens: 5}, Source: "provider",
})
if err != nil {
    return err
}
e, err = usage.Merge(e, usage.Evidence{
    KnownFields: usage.Prompt | usage.Completion,
    Tokens:      interfaces.TokenUsage{PromptTokens: 3, CompletionTokens: 2},
})
// e.KnownFields == usage.BasicFields，e.Revision == 2
```

## 主要 API

| 标识符 | 说明 |
|---|---|
| `Owner` | 一次逻辑 Agent 调用的 `RunID`、`AgentID`、`AgentCallKey`、`ParentAgentCallKey` |
| `Invocation` | `Owner` 加本次访问的 `VisitID` 与 `ParentVisitID` |
| `WithInvocation(ctx, in)` / `FromContext(ctx)` | 向 context 追加一次调用 / 读取当前调用 |
| `Lineage(ctx)` | 从根到当前的独立 `Owner` 切片 |
| `Fields` | 已报告字段的位图：`Prompt`、`Completion`、`Total`、`Cached`、`Reasoning`、`ToolUsePrompt`；`BasicFields`、`AllFields` |
| `Evidence` | `Tokens`、`KnownFields`、`Source`、`Revision` |
| `Merge(old, next)` / `Evidence.Validate()` | 补充新知字段 / 拒绝非法证据 |
| `ModelEvidence` | 一次模型尝试：归属、操作键与尝试键、Provider、模型、证据、`Replayed`、`AccountingCommitted` |
| `ReasoningConfig` | 显式推理请求设置：`Effort`，或 `BudgetSet` + `BudgetTokens` |

## 行为说明

- `KnownFields` 中未置位的字段表示未知，而不是零。`Validate` 拒绝未定义的位、负数 Token、未报告字段中的非零值，以及超过 256 字节的 `Source`。
- `Merge` 只补充新知字段，补充时 `Revision` 加一；重复投递相同证据保持来源和修订号不变。改变已知字段返回错误。`Source` 为 `"saved"` 时不会替换原始来源。
- `Lineage` 返回副本，修改它不影响 context。
- `ReasoningConfig.Validate` 接受的 `Effort` 为 `none`、`minimal`、`low`、`medium`、`high`、`xhigh`、`max`（或空）。`BudgetTokens` 需要 `BudgetSet`；`BudgetSet` 且 `BudgetTokens` 为 0 表示显式零预算，-1 表示请求动态预算。`Effort` 与思考预算互斥。
- 身份标签用于归属，不替代授权。

## 另见

- [多 Agent 钩子与 Token 统计](../../docs/guides/MULTI_AGENT_USAGE_CN.md)
- [Builder API 参考](../../docs/guides/BUILDER_API_REFERENCE_CN.md)（`WithReasoning`）
- [budget](../budget/README_CN.md)、[observe](../observe/README_CN.md)
