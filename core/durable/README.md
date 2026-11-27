# durable: Persisted Steps and Operations

[English](README.md) | [中文](README_CN.md)

`durable` executes explicit, versioned steps with a persisted continuation, so a run can stop for approval or crash and then resume from the last saved position, in the same or a new process. It does not serialize Go closures or resume arbitrary Agent stack frames: you register named steps, and the package stores the next step index, the JSON data passed between steps, the approval state and a per-step idempotency key in a `Store`. Inside a step, `Do` journals named child operations so completed results replay instead of rerunning. Use it for multi-step tasks that must survive a process restart; for graphs use `workflow.Graph[T].CompileDurable`, which persists through the same `Store`.

## Usage

```go
import (
    "context"
    "errors"

    "github.com/kart-io/goagent/codec/json"
    "github.com/kart-io/goagent/core/durable"
    agentErrors "github.com/kart-io/goagent/errors"
)

store, err := durable.NewFileStore("/var/lib/myapp/durable")
if err != nil {
    return err
}
runner := &durable.Runner{
    Store:   store,
    Version: "refund-v1",
    Steps: []durable.Step{
        {Name: "prepare", Run: func(ctx context.Context, in durable.StepInput) (json.RawMessage, error) {
            return json.RawMessage(`{"amount":100}`), nil
        }},
        {Name: "pay", RequireApproval: true, Run: func(ctx context.Context, in durable.StepInput) (json.RawMessage, error) {
            // in.Key is stable across restarts: pass it to the payment API as the idempotency key.
            return json.RawMessage(`"paid"`), nil
        }},
    },
}

snap, err := runner.Run(ctx, "refund-42", json.RawMessage(`{"order":"o-1"}`), "")
if errors.Is(err, agentErrors.ErrApprovalRequired) {
    // Store snap.Key; after the host has authorized the approval, resume with it.
    snap, err = runner.Run(ctx, "refund-42", nil, snap.Key)
}
```

A journaled child operation inside a step (the Runner binds the store and execution key to the step's ctx):

```go
func charge(ctx context.Context, in durable.StepInput) (json.RawMessage, error) {
    rec, err := durable.Do(ctx, "charge", in.Data,
        durable.OperationPolicy{MaxAttempts: 3, RetryFailed: true},
        func(ctx context.Context) (json.RawMessage, error) {
            // durable.ExecutionKey(ctx) is the derived key for deduplicating the external call.
            return json.RawMessage(`{"charged":true}`), nil
        })
    if err != nil {
        return nil, err
    }
    return rec.Value, nil
}
```

## Key API

| Identifier | Description |
|---|---|
| `Runner` / `Runner.Run(ctx, id, initial, approvalKey)` | Starts or resumes run `id` over `Steps` under `Version`; `BeforeApprove` re-checks data before an approval takes effect |
| `Step` / `StepInput` | Named step with optional `RequireApproval`; input carries `Data` and the idempotency `Key` |
| `Snapshot` | Persisted continuation: `Next`, `Data`, `Waiting`, `Approved`, `Key` |
| `Store` | `Lock`, `Load`, `Save`, `SaveAll`; the contract is documented on the interface |
| `Scoped` | Optional Store partitioning: `Scope`, `RemoveScope`, `PruneRemoved` |
| `NewFileStore(dir)` | Local filesystem `Store` (also implements `Scoped`) |
| `Do(ctx, id, input, policy, run)` | Journals a named child operation; `OperationPolicy` sets `MaxAttempts`, `RetryFailed`, `RetryInterrupted`, `Resumable`, `Reconcile` |
| `InspectOperation` / `DiagnoseOperations` | Read a child record; read-only diagnosis of explicit `OperationRef`s |
| `AwaitInput` / `WaitError` / `WithResume` / `InterruptAnswer` | Pause an operation for host input and resume it |
| `OpenInbox` / `Inbox` | Persisted inputs received during a run, kept until `Ack` |
| `WithJournal`, `WithExecutionKey`, `ExecutionKey`, `ChildKey` | Bind a store and execution key to a ctx; derive child keys |
| `WithReplayOnly` | `Do` only replays completed records, never calls `run` or `Reconcile` |

## Behavior Notes

- Steps run at least once. The step position and key are saved before `Run` is called, and a crash after an external effect but before the next checkpoint repeats the step with the same `Key`; external writes must deduplicate by it.
- `Run` requires a non-empty `id`, `Version` and `Store`, and unique, non-empty step names. A checkpoint whose plan (version plus step names and approval flags) differs is rejected, so changing steps requires a new `Version`.
- `initial` is used only for a new run and must be valid JSON (empty means `null`); step outputs must be valid JSON.
- A step with `RequireApproval` saves `Waiting` and returns `ErrApprovalRequired`. Resuming requires the exact `Snapshot.Key`; it binds the approval to that run, plan and step and is an idempotency key, not an authentication secret. Authorization is the host's job.
- `Do` with no journal store or execution key in ctx just calls `run`. With a journal, completed records replay; `MaxAttempts` defaults to 1, and `Do` never retries within one call: `RetryFailed` only permits another attempt on a later invocation. Interrupted records rerun only after `Reconcile` confirms the effect did not happen or `RetryInterrupted` is set.
- Stored operation errors contain categories only, not third-party error text. Error text returned by an `AwaitInput` validator is persisted, so it must not contain secrets.
- `FileStore` uses OS file locks, fsync and same-directory atomic rename. It targets a single host's local filesystem and is not a distributed lease service; for runs shared by several instances, the Execution Contract points to goagent-contrib's `durable/pgstore`.
- `Store.Lock` waits until acquired or ctx ends, is not reentrant, and has no deadlock detection.

## Subpackages

- [migrate](migrate/README.md): explicit one-off conversion of old operation ledgers to `goagent-operation-v2`
- `storetest`: conformance tests for `Store` implementations (`storetest.Run`)
- `leasestore`: a `Store`, `Scoped` and `session.Store` shared by several instances, built on leases with an expiry and free of SQL and driver dependencies. The in-process gates, tokens, renewal and compensation live in this package; reads and writes go through a `leasestore.Backend` and `leasestore.SessionBackend` that the caller implements with any database, driver or ORM and its own tables. Verify an implementation with `leasestore/backendtest.Run`. The ready-made PostgreSQL backend is goagent-contrib's `durable/pgstore`

```go
store, err := leasestore.New(leasestore.Config{Backend: myBackend})
sessions, err := leasestore.NewSessionStore(store, myBackend)
```

## See Also

- [Execution Contract](../../docs/guides/EXECUTION_CONTRACT.md): what `core/durable` does and does not recover, Store choice
- [Native Orchestration](../../docs/guides/NATIVE_ORCHESTRATION.md): execution keys shared with `workflow` and tools
- [Extension Contract](../../docs/guides/EXTENSION_CONTRACT.md): obligations of `durable.Store` implementations
