# usage: Agent Identity and Usage Evidence

[English](README.md) | [中文](README_CN.md)

`usage` defines value-only agent identities and model usage evidence. It is the shared vocabulary between `core/budget` (which records and attributes evidence), `core/observe` (whose events carry it), and the Builder. A caller uses it to read the current agent invocation from a context (for example in a hook or tool pipeline), to interpret per-attempt usage in a `budget.UsageEvent` or `observe.Event`, or to set an explicit reasoning request with `usage.ReasoningConfig`.

## Usage

```go
ctx := usage.WithInvocation(context.Background(), usage.Invocation{
    Owner: usage.Owner{RunID: "r", AgentID: "root", AgentCallKey: "r"},
})
ctx = usage.WithInvocation(ctx, usage.Invocation{
    Owner: usage.Owner{RunID: "r", AgentID: "child", AgentCallKey: "c", ParentAgentCallKey: "r"},
})
in, _ := usage.FromContext(ctx) // the child invocation
chain := usage.Lineage(ctx)     // [root, child], a copy

// A stream frame first reports only the total, a later one adds prompt/completion.
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
// e.KnownFields == usage.BasicFields, e.Revision == 2
```

## Key API

| Identifier | Description |
|---|---|
| `Owner` | `RunID`, `AgentID`, `AgentCallKey`, `ParentAgentCallKey` of one logical agent call |
| `Invocation` | `Owner` plus `VisitID` and `ParentVisitID` of this visit |
| `WithInvocation(ctx, in)` / `FromContext(ctx)` | Appends an invocation to the context / reads the current one |
| `Lineage(ctx)` | Independent root-to-current `Owner` slice |
| `Fields` | Bit mask of reported fields: `Prompt`, `Completion`, `Total`, `Cached`, `Reasoning`, `ToolUsePrompt`; `BasicFields`, `AllFields` |
| `Evidence` | `Tokens`, `KnownFields`, `Source`, `Revision` |
| `Merge(old, next)` / `Evidence.Validate()` | Adds newly known fields / rejects invalid evidence |
| `ModelEvidence` | One model attempt: owner, operation and attempt keys, provider, model, evidence, `Replayed`, `AccountingCommitted` |
| `ReasoningConfig` | Explicit reasoning request setting: `Effort` or `BudgetSet` + `BudgetTokens` |

## Behavior Notes

- A field whose bit is not set in `KnownFields` is unknown, not zero. `Validate` rejects unknown mask bits, negative tokens, nonzero values in unreported fields, and a `Source` longer than 256 bytes.
- `Merge` adds only newly known fields and increments `Revision` when it does; delivering the same evidence again keeps source and revision. Changing an already known field returns an error. A `Source` of `"saved"` never replaces the original source.
- `Lineage` returns a copy; changing it does not affect the context.
- `ReasoningConfig.Validate` accepts `Effort` values `none`, `minimal`, `low`, `medium`, `high`, `xhigh`, `max` (or empty). `BudgetTokens` requires `BudgetSet`; `BudgetSet` with `BudgetTokens` 0 is an explicit zero budget and -1 requests a dynamic budget. `Effort` and a thinking budget are mutually exclusive.
- Identity labels are for attribution, not authorization.

## See Also

- [Multi-Agent Hooks and Token Usage](../../docs/guides/MULTI_AGENT_USAGE.md) (Chinese only)
- [Builder API Reference](../../docs/guides/BUILDER_API_REFERENCE.md) (`WithReasoning`)
- [budget](../budget/README.md), [observe](../observe/README.md)
