# Human in the Loop: Pause, Resume and Checkpoints

[English](HUMAN_IN_THE_LOOP.md) | [中文](HUMAN_IN_THE_LOOP_CN.md)

When execution must continue after human approval, additional information or an edited draft, use `workflow.Graph[T].CompileDurable`. The run saves a checkpoint before the designated node, returns a waiting status and releases the run lock; a person can respond later, even from a new process. There is no need to keep an HTTP request or a goroutine waiting.

| Need | How to wire it |
|---|---|
| Pause before a business node | `DurableConfig.ApprovalNodes`; conditional pauses can route to a human node through a graph branch |
| Inspect persisted state | `graph.Snapshot(ctx, runID)`, containing State, Node, Steps, Key, Waiting, Done |
| Plain approval | `graph.Run(ctx, runID, initial, waiting.Key)`, or a payload-less request to `graph.Resume` |
| Resume after human input or state edits | `WithResumeHandler` + `graph.Resume(ctx, runID, ResumeRequest)` |
| Streaming/batch entry | `graph.AsRunnable()`, submitting the same request through `RunRequest.Resume` |
| Subgraph inside an Agent tool | `AsChildRunnable` + a `Resumable` tool; use `workflow.WithResume` to supply input at the wait |
| Cancel a running Agent immediately | `Handle.Interrupt(cause)`; cancellation and a durable human wait are different operations |

## Minimal Integration

The complete offline program is the [human-in-the-loop example](../../examples/human-in-the-loop/README.md); it needs no model or API key and covers approve, reject, edited text and recovery in a separate process.

1. Build a durable graph with a stable business RunID, add the human node to `ApprovalNodes`, and use `durable.FileStore` to save local checkpoints.
2. Configure `WithResumeHandler` to turn validated human input into business state. The host is responsible for authenticating identity and restricting which fields may be changed.
3. Call `Run`. On `agentErrors.ErrApprovalRequired`, return `Snapshot.State`, `Node` and `Key` to the UI and wait for the human decision.
4. Call `Resume` with the same store, graph definition and version. The paused node runs only after the updated state and the release marker are atomically persisted.

```go
waiting, err := graph.Run(ctx, runID, initial, "")
if err != agentErrors.ErrApprovalRequired {
    return err // a real application must also handle a normally completed result
}

// The identity in ctx must be authenticated by the host; waiting.Key only locates this pause and is not an authorization credential.
result, err := graph.Resume(ctx, runID, workflow.ResumeRequest{
    Key:   waiting.Key,
    Input: json.RawMessage(`{"decision":"approve","text":"human-revised content"}`),
})
if err != nil {
    return err // the run may also reach the next human node and need another wait to be handled
}
fmt.Println(result.Done, result.State)
```

Handler signature:

```go
workflow.WithResumeHandler(func(
    ctx context.Context,
    runID string,
    saved *workflow.GraphSnapshot[State],
    input json.RawMessage,
) (State, error) {
    // Parse the input; check the decision, the fields allowed to change and business constraints.
    // saved is an independent copy of the latest checkpoint under the lock; only the returned State is written back.
    return applyValidatedHumanInput(ctx, saved.State, input)
})
```

The handler runs inside the run lock, must be side-effect free, and must not re-enter `Run`, `Resume` or `Snapshot` for the same run. It may be called again after a write failure. Once a handler is configured, approval requests without a human payload also call it, with nil input; when a human payload is required, reject nil rather than relying on the UI making the field mandatory. JSON `null` differs from no payload; both are passed to the handler for validation.

`WithBeforeApprove` validates the **proposed new state** after the handler and can re-check order facts, permissions and deadlines that changed during the pause; it also receives an independent copy, so modifying it has no effect. A business "reject" can be written into state as a valid human decision and routed to a rejection branch; `ResumeRejectedError` means this input is invalid, `ApprovalRejectedError` means authorization/re-check failed, and both keep the original checkpoint waiting.

## Checkpoint and Failure Semantics

- Recovery uses the saved state and ignores `RunRequest.Initial`; passing initial again cannot overwrite reviewed content.
- `Resume` must provide the Key of the currently waiting node; an old Key, another run's Key or a completed node's Key returns `ErrApprovalMismatch`. Each visit to a node in a loop has a different Key.
- When input validation, permission re-check or saving fails, the paused node does not run and the human edits are not committed. Storage errors and panics are never disguised as a normal wait.
- The human state and the release marker are committed in the same Save. If a later node fails or the process exits, recover from the accepted state with `Run(ctx, runID, zero, "")`; the human input handler is not called again.
- When the response is lost or the commit outcome is unknown, read `Snapshot` first: if it is still waiting on the original Key, you can resubmit; if it has moved past that wait, use `Run` without an approval key to recover or read the completed result. A repeated `Resume` never applies the old edit again.
- On a normal pause `AsRunnable` returns a successful snapshot with `Waiting=true, Done=false`; the stream's Done only means this call has ended, and whether the business is complete is given by `Data.Done`. `RunRequest.Resume` and `ApprovalKey` cannot both be provided.
- Steps and external side effects are still executed at least once. Cancellation does not roll back side effects, and nodes need to respond to context; deduplicate external writes with `workflow.DurableKey(ctx)` or a stronger business idempotency key.
- Checkpoints are bound to the graph topology and `DurableConfig.Version`; bump the version when the handler, authorization policy, node behavior or state structure changes. What is saved is node boundaries and business state, not the Go call stack, so an arbitrary instruction position inside a function cannot be restored.

The local file store suits a single host; for storage shared by multiple instances use goagent-contrib's `durable/pgstore`. Discovering pending runs, sending notifications, authenticating approvers and scheduling automatic resumes are the host's responsibility.

For real-model validation see the Gemini / DeepSeek HITL report and the [reproducible script](../../evaluation/hitlbench/README.md). Checkpoint recovery passed for both; Gemini's final notification contained a refund-completed claim for an action that was never executed, so successful execution must not be equated with correct model content.

## Pausing Mid-Node or Mid-Tool

`durable.AwaitInput(ctx, id, info, validate)` pauses inside a workflow node, a parallel branch or a tool declared interruptible, waiting for a person to provide input:

```go
raw, err := durable.AwaitInput(ctx, "amount", json.RawMessage(`{"question":"refund amount"}`), validateAmount)
if err != nil {
    return state, err // return the wait unchanged; the framework records it as a pause
}
```

- When the graph pauses, `Run` returns `agentErrors.ErrApprovalRequired`, and `Snapshot.Interrupts` lists `Key`, `ID` and `Info`. Respond with `graph.Resume(ctx, runID, workflow.ResumeRequest{Key: point.Key, Input: ...})`; alternatively use `durable.WithResume(ctx, point.Key, input)` plus a keyless `Run`.
- During an interrupt wait, `Snapshot.Key` and `ChildContinuation.ApprovalKey` are not response keys (the former is the node intent key, the latter is empty); respond with `Interrupts[i].Key`. If the intent key is passed to a subgraph together with input via `workflow.WithResume`, the input is not accepted and a wait with `Rejected=true, Reason=approval_mismatch` is returned.
- Recovery via a keyless `Run` gives no conflict or rejection signal: submitting different input to an already answered interrupt is ignored (the fixed input is replayed), and a `validate` rejection only shows up in `Snapshot.Interrupts[i].Rejected`. Use a keyed `Resume` when you need `ResumeRejectedError`.
- Recovery re-runs the node or tool from the beginning. External writes before `AwaitInput` must be recorded through `durable.Do` or made idempotent by `workflow.DurableKey(ctx)`; otherwise they run again.
- The first successfully persisted input is fixed, and every later replay returns it. Submitting different input under the same key returns `ResumeRejectedError` (`agentErrors.ErrInterruptAnswered`) and the node does not run; whether inputs are the same is decided by comparing bytes after `json.Compact`.
- If the input is not valid JSON or `validate` fails, the wait remains; `Interrupts[i].Rejected` and `Reason` explain why, and it can be resubmitted after correction. `null` is a valid explicit input. If `validate` panics the reason is `validate_panic`; the panic value and stack are not persisted and appear only in the error returned this time: extract `*errors.PanicError` with `errors.As` from `Resume`'s `ResumeRejectedError` or from `WaitError.Cause`; if the error text is empty the reason is `rejected`. The error text of `validate` is persisted and exposed through `Snapshot` and `ResumeRejectedError`, so it must not contain credentials or other sensitive information.
- `id` must be unique within the same node or tool; inside loops include a sequence number, or the previous answer will be replayed. `info` must be deterministic JSON and must not contain credentials.
- When one branch of a parallel node pauses, sibling branches complete normally and interrupts are answered one by one. For nested subgraphs, `ChildContinuation.Interrupts` gives the innermost interrupt keys.
- Only interrupt waits (from `AwaitInput`, possibly reported up through subgraphs layer by layer) pause a node; other waits inside a node, such as a subgraph's approval wait, are still treated as node failures.
- Tools must opt in explicitly with `tools.NewFunctionToolBuilder(...).WithInterruptible(version)`; custom tools implement `tools.InterruptibleTool`. Bump the version when enabling it or changing its semantics. Switch only after in-flight runs using the tool have finished; for tools that implement `DurablePolicyProvider` themselves, changing only `Resumable` is not detected by the framework.
- Responses to dynamic interrupts do not go through `WithResumeHandler` or `WithBeforeApprove`. An interrupt key is not an authorization credential: the host must first authenticate the responder and confirm they are allowed to answer that interrupt.

| Mechanism | Use for |
|---|---|
| `ApprovalNodes` + `WithResumeHandler` | Approval before a node runs; humans edit business state |
| `durable.AwaitInput` | A node or tool needs one input mid-execution |
| `Handle.Interrupt` | Cancel a running Agent; not a pause |

## Nesting Inside an Agent Tool

Wrap the durable graph's `AsChildRunnable()` with `tools.NewRunnableToolFactory` and set `RunnableToolConfig.Resumable=true`. When the host receives `agentErrors.ErrAgentWaiting`, it parses `durable.Waits(err)` and the `workflow.ChildContinuation` inside.

```go
resumeCtx, err := workflow.WithResume(ctx,
    wait.Path[len(wait.Path)-1].Key, // execution key of the waiting operation
    workflow.ResumeRequest{
        Key:   continuation.ApprovalKey, // subgraph checkpoint key
        Input: json.RawMessage(`{"decision":"approve"}`),
    },
)
if err != nil { return err }
out, err := agent.AsRunnable().Invoke(resumeCtx, originalInput)
```

These two Keys differ and are not interchangeable. The subgraph and the parent operation use the same Store; an input rejection keeps the parent operation waiting and is returned through `ChildContinuation.Reason=resume_input`. Subgraph approval only accepts a structured `ResumeRequest` encoded by `workflow.WithResume` and no longer accepts the bare-key call `durable.WithResume(ctx, operationKey, approvalKey)`; `durable.WithResume` can still pass raw input to leaf interrupts. An Agent started with `Start` should likewise be resumed with `Start`; see [Builder](../../builder/README.md).

## Boundaries of Cancellation and Recovery

`Handle.Interrupt` cancels the current run, and `Wait` waits for tools and cleanup to exit. To recover a cancelled run, a durable journal must have been configured beforehand and the execution identity kept; resending an interrupted model request is also governed by the `WithInterruptedModelResends` policy.

The old `core.InterruptManager`, `InterruptableExecutor` and `core/checkpoint` have been removed; human waits always use the durable graph, `durable.AwaitInput` and `durable.Store` above. A complete `durable.AwaitInput` example is in [human-in-the-loop-interrupt](../../examples/human-in-the-loop-interrupt/README.md).
