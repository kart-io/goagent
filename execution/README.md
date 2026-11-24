# execution: Host Execution Scope

[English](README.md) | [中文](README_CN.md)

`execution` combines the session, journal, budget, deadline, and observation configuration supplied by the host into one execution scope. A host reaches for it when it already owns a run identity and resources (a durable store, a budget controller, an observer or tracer) and wants every agent call of that run to see the same wiring. It does not create a run engine, default stores, or quotas; it binds what the host passes in onto a context and configures a Builder with matching identities. `NewGovernedBuilder` additionally assembles a Builder whose tool calls go through an explicitly authorized, bounded Dispatcher.

## Usage

### Scope, Bind, ConfigureBuilder

```go
const runID = "order-123"
controller, err := budget.New(budget.Config{RootKey: runID, Version: "v1", MaxModelRequests: 4, MaxAgentCalls: constants.DefaultMaxAgentCalls})
if err != nil {
    return err
}
scope, err := execution.NewScope(execution.Config{RunID: runID, SessionID: "user-42", Budget: controller, Timeout: time.Minute})
if err != nil {
    return err
}
runCtx, cancel, err := scope.Bind(ctx)
if err != nil {
    return err
}
defer cancel()

b, err := execution.ConfigureBuilder(builder.NewSimpleBuilder(client), scope, "assistant")
if err != nil {
    return err
}
agent, err := b.BuildContext(runCtx)
if err != nil {
    return err
}
out, err := agent.AsRunnable().Invoke(runCtx, "summarize the order")
```

A runnable offline version is in [examples/execution-scope](../examples/execution-scope/main.go).

### Governed tool flow

```go
b, report, err := execution.NewGovernedBuilder(client, scope, "assistant", registry, tools.DispatchConfig{
    MaxConcurrency:          4,
    Timeout:                 30 * time.Second,
    MaxAdmissionConcurrency: 2,
    MaxQueued:               8,
    Authorize: func(_ context.Context, name string, _ *interfaces.ToolInput) error {
        if name != "lookup" {
            return fmt.Errorf("tool %s is not authorized", name)
        }
        return nil
    },
})
if err != nil {
    return err
}
// Run it under scope.Bind, as above; report is a static snapshot.
fmt.Println(report.MaxToolCalls, report.Dispatch.ToolNames)
```

The full example, including rebuild and replay, is in [examples/execution-governed](../examples/execution-governed/main.go).

## Key API

| Identifier | Description |
|---|---|
| `Config` | Host-supplied `RunID`, `SessionID`, `Journal` (`durable.Store`), `Budget` (`*budget.Controller`), `Timeout`, `Observer`, `Tracer` |
| `NewScope(Config)` | Validates identities, budget, and optional components; does not access the store |
| `Scope.Bind(ctx)` | Returns a derived context carrying session, execution key, journal, run ID, observer, tracer, timeout, and budget, plus its cancel function |
| `Scope.RunID()` / `Scope.SessionID()` | Root run identity (for a graph `RunRequest`) and host session identity |
| `ConfigureBuilder(b, scope, agentID)` | Sets `WithSessionID`, `WithDurableScope(agentID)`, and `WithAgentID(agentID)` on an `AgentBuilder`; does not build |
| `NewGovernedBuilder(client, scope, agentID, registry, cfg)` | Returns a `*builder.SimpleBuilder` wired with a `tools.Dispatcher`, plus a `GovernanceReport` |
| `GovernanceReport` | Snapshot of run/session/agent IDs, journal and budget mode, model/tool limits, root timeout and deadline, and dispatch governance at creation time |

## Behavior Notes

- `RunID` must be non-empty with no leading or trailing whitespace; `Timeout` must not be negative. An empty `SessionID` defaults to `RunID`.
- `Journal`, `Observer`, and `Tracer` may be nil but not a typed nil. When `Budget` is set, its `RootKey` must equal `RunID`; when a `Journal` is also set, the budget must be durable.
- Scope does not close the journal, observer, or tracer. The cancel returned by `Bind` only cancels the derived context.
- `Bind` rejects a nil or already-done context, a context already bound to a Scope or to a journal, and a context whose execution key, session ID, observation run ID, or budget controller conflicts with the Scope. Use a fresh host context for each `Bind`.
- `Timeout > 0` adds a `context.WithTimeout`; the budget is bound with `budget.Bind`.
- `NewGovernedBuilder` requires a Scope with a budget, `MaxToolCalls >= 1`, a root deadline (Scope `Timeout` or budget `DeadlineUnixMilli`), a non-nil `Authorize`, `MaxAdmissionConcurrency >= 1`, `MaxQueued >= 1`, a client that implements `llm.ToolStreamClient`, and at least one registered tool. Model requests and tool authorization fail unless the context comes from `Bind` of the same Scope.
- All validation errors are `*errors.AgentError` with code `CodeInvalidConfig` or `CodeInvalidInput`.

## See also

- [Standard Host Integration](../docs/guides/HOST_INTEGRATION.md)
- [Execution Governance](../docs/guides/EXECUTION_GOVERNANCE.md)
- [Final Acceptance and Governed Assembly](../docs/guides/EXECUTION_ACCEPTANCE.md)
- [Execution Contract](../docs/guides/EXECUTION_CONTRACT.md)
