# Standard Host Integration

[English](HOST_INTEGRATION.md) | [中文](HOST_INTEGRATION_CN.md)

`execution.Scope` combines the host's existing execution identity, session, journal, budget, deadline, and observers. It does not create a new run engine; it only does the wiring.

## Build and Run

The complete runnable code is in the [offline example](../../examples/execution-scope/main.go); no model key is required:

```bash
go run ./examples/execution-scope
```

The typical order is `execution.NewScope(config)` → `scope.Bind(requestCtx)` → `execution.ConfigureBuilder(b, scope, agentID)` → `b.BuildContext(runCtx)` → `agent.AsRunnable().Invoke/Stream(runCtx, input)`. Check the error at every step and defer the cancel function returned by Bind. ConfigureBuilder returns the configured Builder; the type parameter is inferred from the Builder. The Builder is mutable and must not be configured concurrently.

`BuildContext(ctx)` rejects nil, canceled, and timed-out contexts, passes the same context to checkpoint initialization and OnStart callbacks, and checks for cancellation after store operations and after each callback returns. The old `Build()` still uses Background. The framework cannot forcibly interrupt synchronous implementations that ignore the context, and it does not roll back external effects already produced by initialization callbacks. Hosts should use stores and callbacks that honor the context.

## Identity and Recovery

- `Config.RunID` is required and must have no leading or trailing whitespace; the Budget's RootKey must equal the RunID. On recovery, keep the RunID, agentID, budget version, and configuration.
- An empty SessionID defaults to the RunID; an explicit SessionID is kept as the host session identity. Multiple runs may share a session, but the host must still coordinate concurrent writes to the same session from different Agent instances.
- `ConfigureBuilder` sets the session and durable scope; do not override these identities afterwards. A workflow's `RunRequest.RunID` uses `scope.RunID()`; node execution keys are still derived from the graph and must not all be replaced with the root RunID.
- `observe.RunIDFromContext(ctx)` reads the observation identity. Bind checks for conflicts with an existing execution key, session, observation identity, and budget.

Use a fresh host request context for each Bind; do not reuse a context that was already bound. Any pre-bound Journal is rejected, including a Journal without an execution key; this prevents a host request from accidentally inheriting another run's journal. A matching bare identity and the same budget controller are accepted.

## Resources and Deadlines

`Config` optionally takes a Journal, Budget, Timeout, Observer, and Tracer. When a Journal is bound and a budget is also used, the budget must be durable; using only a Journal does not force a budget to be created. Scope does not close the Store, Observer, or Tracer; the host owns sharing and closing these resources. If Observer/Tracer are not configured explicitly, they are inherited from the parent context. Scope only cancels the derived context; the effective deadline is the earliest of the parent context, the Scope Timeout, and the budget deadline.

Scope does not provide cross-process scheduling, distributed session mutual exclusion, or automatic side-effect compensation; sharing durable runs across instances requires a Store that supports leases (such as `durable/pgstore` in goagent-contrib). Durable approval and recovery are handled by the existing Runner, workflows, and Dispatcher; see [Execution Governance](EXECUTION_GOVERNANCE.md) for their behavior. The recovery integration test is [execution/integration_test.go](../../execution/integration_test.go); it covers approval pause, checkpoint failure after execution, rebuild-and-resume, and replay after completion, and verifies that side effects and budget are not duplicated.

## Division of Persistence

The Builder's persistence entry points each handle one concern and cannot substitute for one another:

| Concern | Entry point | Behavior |
|---|---|---|
| Recovery from a mid-run crash | Scope's Journal and `WithDurableScope` | Records model and tool calls; when resuming with the same execution key, completed operations are replayed |
| Conversation history | `WithSessionLog` | History is derived from the session log; a SessionLog can also serve as the durable journal (`session.DurableStore`) |
| Business state between turns | `WithState` | Kept in-process by default; `WithSessionState` restores state across processes through the session log; otherwise the host persists it, or it becomes the state of a `workflow.CompileDurable` graph |

The durable journal only replays operation results; it does not save `WithState` state.

## Overhead and Acceptance

The [measurement report](../evaluation/2026-10-31-execution-overhead/REPORT.md) contains the three budget modes, the 10/100/1000-operation matrix, fixed-ledger micro-benchmarks, and raw data. The probe can be run standalone:

```bash
go run ./evaluation/executionbench/cmd/run -operations=10,100,1000 -modes=none,memory,file -timeout=10m
```

This is a synthetic load on a single machine; file mode only covers budget persistence, with no real model, tool journal, or business service. After integrating a real service, you still need to verify latency, cancellation, recovery, budget, and resource lifecycle for typical tasks.

## Governed Tool Flow

When you need explicit authorization plus a bounded root budget, admission, and queuing, use `execution.NewGovernedBuilder` and run it under the same Scope.Bind; tools are supplied uniformly through WithDispatcher and must not be mixed with WithTools. It returns a regular Builder and a static governance report, without creating a new run engine or taking ownership of resources. See the [full guide](EXECUTION_ACCEPTANCE.md#标准治理装配) and the [offline example](../../examples/execution-governed/main.go).
