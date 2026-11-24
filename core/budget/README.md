# budget: Root Budget

[English](README.md) | [中文](README_CN.md)

`budget` bounds model request admission across one host-owned execution. A `Controller` is the root ledger for a logical run (`Config.RootKey`): it admits model requests and tool executions against optional limits and a deadline, records token usage evidence per attempt, and attributes usage to the agents that made the calls. Reservations are conservative: a crash never refunds an uncertain request. Reach for it when a run, including its child agents and tools, must stay within a request, token, tool-call, or time budget, or when you need per-agent token accounting.

## Usage

Bind a budget to a run; Builders and the tool Dispatcher that run under the context share it:

```go
controller, err := budget.NewDurable(budget.Config{
    RootKey: requestID, Version: "task-v1",
    MaxModelRequests:  20,
    MaxToolCalls:      30,
    DeadlineUnixMilli: time.Now().Add(5 * time.Minute).UnixMilli(),
}, journal) // budget.New(config) for an in-memory budget
if err != nil {
    return err
}
runCtx, cancel := budget.Bind(ctx, controller)
defer cancel()
```

Direct admission and accounting, plus a usage hook:

```go
c, err := budget.New(budget.Config{RootKey: "run", Version: "v1", MaxModelRequests: 2})
if err != nil {
    return err
}
if err := c.Reserve(ctx, "call-1"); errors.Is(err, agentErrors.ErrModelBudgetExceeded) {
    return err // over the limit: do not send the request
} else if err != nil {
    return err
}
// ... send the model request ...
u := &interfaces.TokenUsage{PromptTokens: 2, CompletionTokens: 3, TotalTokens: 5}
if err := c.RecordUsage(ctx, "call-1", u); err != nil { // nil records unknown usage
    return err
}
snap, err := c.Snapshot(ctx)

ctx = budget.WithUsageHook(ctx, func(ctx context.Context, e budget.UsageEvent) {
    _ = e.Model.Tokens // this attempt only
    if e.HasStatistics {
        _ = e.Statistics.Root.KnownTokens.TotalTokens // point-in-time total; never sum across events
    }
})
```

## Key API

| Identifier | Description |
|---|---|
| `Config` | Host-supplied policy: `RootKey`, `Version`, `MaxModelRequests`, `MaxReservedTokens`, `TokenPolicyVersion`, `MaxToolCalls`, `MaxAgentCalls`, `DeadlineUnixMilli` |
| `New(Config)` / `NewDurable(Config, durable.Store)` | In-memory controller / controller persisted to a stable root record |
| `Bind(ctx, c)` | Binds the root budget and its absolute deadline, propagating the deadline to graph nodes and subtasks |
| `WithController(ctx, c)` / `FromContext(ctx)` | Shares a controller with child model and tool invocations / reads it back |
| `Controller.Reserve`, `ReserveEstimated`, `ReserveAttributed` | Admit one model request before the network call; the estimated forms also reserve a host token estimate |
| `Controller.ReserveTool(ctx, key)` | Admits one actual tool execution |
| `Controller.RecordUsage` / `RecordEvidence` | Record basic token usage / usage evidence with a field mask for a reserved key |
| `Controller.Remaining(ctx)` | Model requests that can still be reserved (point-in-time) |
| `Controller.Snapshot(ctx)` / `Attribution(ctx)` | Root aggregation / per-call and per-agent `Self` and `Inclusive` usage |
| `WithUsageHook(ctx, hook)` / `UsageHook` / `UsageEvent` | Read-only per-attempt usage notification with an optional cumulative snapshot |
| `WithUsageHookTimeout(ctx, d)` | Bounds one statistics query plus hook call |

## Behavior Notes

- Every limit is caller-owned: 0 means unlimited. `New` rejects an empty `RootKey` or `Version`, negative limits, and `MaxReservedTokens > 0` without `TokenPolicyVersion`.
- The configuration is persisted with a durable ledger and must not change for the lifetime of `RootKey`.
- Over a request or tool-call limit, admission returns `errors.ErrModelBudgetExceeded`; reusing a key returns `errors.ErrReservationExists`. Past `DeadlineUnixMilli`, admission returns an error wrapping `context.DeadlineExceeded`.
- With `MaxReservedTokens` set, `Reserve` returns `errors.ErrTokenEstimateRequired`; use `ReserveEstimated`. Reservations are never refunded, whether actual usage is lower, the call is canceled, or the process crashes.
- `RecordUsage` requires a prior reservation for the key; `nil` records unknown usage. Recording the same evidence again is idempotent; changing an already known field is an error.
- `ReserveTool` should not be called on journal replay; a retry is a new execution with a new key. Under a durable journal it requires a durable controller.
- `Controller` is safe for concurrent use; durable controllers coordinate through Store locks. `Remaining` is only a snapshot: admission is decided by `Reserve`.
- Usage hooks run synchronously after accounting, outside ledger locks, on a context detached from execution cancellation and bounded by `WithUsageHookTimeout` (0 selects `constants.DefaultUsageHookTimeout`, negative is rejected). Hooks must support concurrent calls and return promptly; their failures and panics do not change the business outcome. Hooks are not durable delivery: reconcile from `Attribution`.

## See Also

- [Root Budget, Resource Governance, and Run Diagnostics](../../docs/guides/EXECUTION_GOVERNANCE.md#root-budget)
- [Composable Orchestration: Root Budget and Usage](../../docs/guides/COMPOSABLE_ORCHESTRATION.md#root-budget-and-usage)
- [Multi-Agent Hooks and Token Usage](../../docs/guides/MULTI_AGENT_USAGE.md) (Chinese only)
