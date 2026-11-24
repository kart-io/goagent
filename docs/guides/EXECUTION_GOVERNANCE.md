# Root Budget, Resource Governance, and Run Diagnostics

[English](EXECUTION_GOVERNANCE.md) | [中文](EXECUTION_GOVERNANCE_CN.md)

## Root Budget

`core/budget.Config` adds the following optional fields on top of the original request count; a zero value disables each one:

| Field | Semantics |
|---|---|
| `MaxReservedTokens` | Cumulative admission limit on host estimates; not a hard cap on the actual bill |
| `TokenPolicyVersion` | Required when the token limit is enabled; identifies the host's estimation rules |
| `MaxToolCalls` | Total number of times the Dispatcher actually invokes a tool body; replays and admission rejections do not count, and a retry is a new execution |
| `DeadlineUnixMilli` | Absolute deadline of the root task (Unix milliseconds) |

```go
controller, err := budget.NewDurable(budget.Config{
    RootKey: requestID, Version: "task-v1", MaxModelRequests: 20,
    MaxReservedTokens: 100000, TokenPolicyVersion: "host-estimator-v1",
    MaxToolCalls: 30, DeadlineUnixMilli: time.Now().Add(5*time.Minute).UnixMilli(),
}, journal)
if err != nil { return err }
runCtx, cancel := budget.Bind(ctx, controller)
defer cancel()
agent, err := builder.NewSimpleBuilder(client).
    WithTokenEstimator(hostEstimate).WithTools(dispatcher.Tools()...).Build()
```

`hostEstimate` has type `builder.TokenEstimator`; its arguments are the read-only model request and the tool list. It must return a conservative estimate covering input, tool definitions, and maximum output. There is no default approximation: if the token limit is enabled but no estimator is set, or the estimator returns zero/negative or fails, the call is rejected before any network request. Each retry reserves independently; replays of completed operations are not re-estimated.

For direct admission, call `ReserveEstimated(ctx,key,tokens)`; the old `Reserve` keeps its original behavior when the token limit is off. Reservations are never refunded, whether actual usage is lower, the call is canceled, or the process crashes. `Snapshot.ReservedTokens` is the total estimate; `KnownTokenDelta` is the known actual tokens minus the corresponding estimates, counting only entries with usage evidence. Unknown usage continues to be counted in UnknownUsageEntries and must not be treated as zero. When actual usage exceeds the estimate, the real value is still recorded; evidence is not altered.

The root budget is shared across child calls. Independent per-subtask quotas and monetary pricing are allocated by the host based on business needs; this version introduces no price table and assumes no model rates. Tool counting is based on calls that actually cross the Dispatcher invocation boundary; nested tool boundaries each count separately, and functions that bypass the Dispatcher are not counted automatically.

`Bind` propagates the root deadline to the entire graph, callbacks, and subtasks. With only `WithController`, the Builder and Dispatcher still bind the deadline for their own execution, but for arbitrary host graph nodes and external callbacks to get the same deadline, use Bind. Third parties must still honor the context.

Durable runs must use a durable Controller, including tool-only tasks; an in-memory budget cannot retain its quota across restarts. Zero values of the new Config fields are omitted from JSON, so old ledgers and completed model intents remain recoverable. Enabling or changing a new limit or the estimator version rejects old runs; a new RootKey/version is required, and the original ledger cannot be silently modified.

## Two-Phase Admission in the Dispatcher

- `MaxConcurrency` (default 4) only constrains actual execution and is no longer occupied by approval waits.
- `MaxAdmissionConcurrency` (default 0, unlimited) independently constrains validation and Pipeline admission; hosts that need to control large numbers of pending-approval requests should configure it explicitly.
- `MaxQueued` (default 0, unlimited) limits the number of blocked waiters shared by both phases; immediately acquiring a free slot does not take a queue position. Exceeding it returns `agentErrors.ErrDispatcherBusy`, which can be matched with errors.Is.
- Both queues are bounded only by the host/root-budget ctx. Validation, Authorize, journaling, and execution cumulatively consume the single-call Timeout; Pipeline admission and waiting for an execution slot pause the clock. Cancellation is checked again after a slot is acquired, to avoid late execution.
- Replays and admission rejections need no execution slot; cancellation or queuing failure does not create a new pending operation. A third party that does not honor the context cannot be forcibly stopped by a timeout.

Approvals no longer occupying execution slots is an intentional semantic fix. If a host relied on the old MaxConcurrency to also limit the number of pending approvals, it should add MaxAdmissionConcurrency; neither guarantees fair scheduling.

## Asynchronous Observation and Grant Reclamation

`observe.NewAsyncObserver(sink,capacity,workers)` returns an adapter that implements Observer. The queue is bounded, enqueueing does not wait on the sink, and the number of workers is fixed; with workers>1 the sink must support concurrency. `Dropped()` counts events dropped due to queue overflow, submission after close, and queued events discarded on a canceled close; sink panics are isolated.

The host must call `Close(ctx)`: a normal close first stops accepting events, then drains. If ctx is canceled, the sink context is canceled and Close returns; Close can be called again afterwards to wait. A sink that ignores the context still occupies its worker; the framework does not create a replacement goroutine and cannot kill it. Asynchronous events keep context values and are detached from the original call's cancellation; they are not durable audit or metering evidence.

`Pipeline.GrantTTL` sets how long a new session grant remains valid from approval; <=0 keeps it non-expiring. An expired grant no longer takes effect even before it is cleaned up; `Grants` filters out expired entries; the host periodically calls `PruneGrants()` to reclaim expired grants and empty sessions, which returns the number of grants removed. `RevokeGrants(sessionID)` revokes explicitly; cleanup does not modify in-flight approvals.

## Read-Only Reports for Durable Runs

`Runner.Diagnose(ctx,runID,refs...)` and `DurableGraph.Diagnose` return a `durable.DiagnosticReport` that can be JSON-encoded directly. The report contains the position, status, obstacles, and suggested actions; it does not contain business state, tool results, approval keys, or raw store errors. Known operations can be added with explicit `OperationRef{ParentKey,ID}`; unknown subtasks are not traversed automatically.

| Report status | Conclusion you can draw |
|---|---|
| `not_found` | The run or reference was not found; check the identifier |
| `completed` | The saved resume position is complete |
| `approval_waiting` | The checkpoint is waiting for host review and approval |
| `outcome_unknown` | Not complete; it may be running, have crashed, or have failed, and cannot be safely re-run directly |
| `blocked` | Version, topology, or checkpoint structure mismatch |

Only explicit operation records provide pending/failed evidence: pending means the result is unknown, and failed also requires checking external effects and the retry policy. Suggested actions are not execution authorization; diagnosis does not modify state, approve, invoke nodes/routers/coordinators, or take execution locks. Multiple Loads are not a consistent transaction; coordinate in-flight executions before actual recovery. Graph reports do not call the host state deserialization function and do not verify business state type compatibility.

Version migration must be performed explicitly by the host; diagnosis itself does not migrate or repair stored data.

## Upgrade Compatibility Check

A durable graph can declare StateVersion/Components, export a Manifest, and call CheckCompatibility for specific runs. This check reuses the read-only Diagnose and does not execute, lock, write, or migrate tasks; Compatible does not prove that effects can be safely re-run. See [version manifest and drain procedure](EXECUTION_ACCEPTANCE.md#recovery-manifest-and-pre-upgrade-checks).
