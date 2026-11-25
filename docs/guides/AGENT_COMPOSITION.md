# Agents, Workflows, and In-Flight Input

[English](AGENT_COMPOSITION.md) | [中文](AGENT_COMPOSITION_CN.md)

GoAgent provides a general execution foundation. The host decides task routing, and Runnable is the common carrier for Agents, tools, and flows; not every task has to pass through a main Agent first.

## Choosing an Entry Point

| Situation | Recommendation |
| --- | --- |
| Insufficient user information | The host returns a clarifying question, keeps the business state, and routes once the information is complete |
| Clear intent, fixed steps | Call the compiled workflow Runnable directly, with no additional model requests |
| Tool selection needs runtime exploration | Call an Agent and let the model choose among the tools allowed for this run |
| The main task needs a specialized sub-Agent | Adapt the sub-Agent's Runnable as a tool and expose it to the parent Agent through the Dispatcher |
| Multiple independent pieces of work can run in parallel | Use a bounded Graph.AddParallel; Clone/Merge explicitly and define branch failure rules clearly |
| Multiple Agents collaborate sequentially, in a loop, or by routing | Use `multiagent`'s Sequential, Loop, and RouterSupervisor; see [Multi-Agent Patterns](MULTI_AGENT_PATTERNS.md) |
| Human approval and recovery are needed | Use DurableGraph/AsChildRunnable, Resumable tools, and the journal and resume input provided by the host |

Routing rules and confidence thresholds belong to the business. Do not treat a routing result directly as tool authorization; actual execution still requires validation and admission. `workflow.Graph.AddBranch` can be used for typed routing; unknown intents should have an explicit clarification or rejection path.

## The Same Flow Can Run Directly or Be Exposed as a Tool

A compiled Graph implements Runnable. It can be invoked directly or passed to `tools.NewRunnableTool`; when providing it to an Agent, use `dispatcher.Tools()`. For a compilable, runnable offline example, see [ExampleNewRunnableTool_workflow](../../tools/runnable_tool_example_test.go):

```sh
go test ./tools -run '^ExampleNewRunnableTool_workflow$' -v
```

The example supplies the tool call arguments directly and sends no model requests. A real sub-Agent is adapted with `child.AsRunnable()`, explicitly declares `ModelInvoking: true`, and is given a shared root budget. Use `NewRunnableToolFactory` when each call needs its own session and resources; do not accidentally share mutable Agent/session state across concurrent requests.

Parent and child Dispatchers use separate instances to avoid reentrancy deadlocks while an ancestor holds a slot; the root budget and limits are passed down. For detailed configuration, see [Composable Orchestration](COMPOSABLE_ORCHESTRATION.md).

## A Resumable Subflow Is Not a Background Task Service

`DurableGraph.AsChildRunnable()` uses the Store of the parent's operation journal and a derived execution key; the outer tool must declare `Resumable: true`. An approval wait returns `durable.WaitError`, and the parent Builder pauses with `agentErrors.ErrAgentWaiting`; a wait must not be treated as a successful, model-visible tool result.

The host takes the operation execution key `operationKey` from the last element of the wait path, and the child graph checkpoint key `approvalKey` from `ChildContinuation.ApprovalKey`. After completing business authorization, it supplies a structured approval with `workflow.WithResume(ctx, operationKey, workflow.ResumeRequest{Key: approvalKey})`; when there is a human payload, write it to `ResumeRequest.Input`. The two keys are not interchangeable, and a bare approval key is no longer accepted as child graph resume input. The approval key is a resume credential, not proof that the user is entitled to approve. When resuming, keep the execution key, scope, version, and input intent stable. See the [actual nested approval regression](../../builder/nested_resume_test.go) and the [graph entry point](../../workflow/README.md).

Only a resumable flow whose effects are all recorded under the correct derived keys may declare Resumable. Ordinary external APIs, commands, or closures containing unrecorded model requests must not simply turn it on to get safe retries; a pending unknown result should first be reconciled with Reconcile or handed to the host.

These mechanisms do not provide background task leases, durable message delivery, or cross-host takeover. A background sub-Agent needs its own task identity, owner, cancellation policy, budget, status queries, result delivery, and reclamation; these are managed by the host's scheduling system.

## Handle Input and Completion Boundaries

| Method | When it is picked up | When unclaimed input may remain |
| --- | --- | --- |
| Steer | Next step; at the end check it can wake a loop that has remaining budget | Last round, arrival after the end check, failure, or cancellation |
| Inject | With the next step; does not wake the loop | The current answer ends and there is no next step |
| Redirect | Cancels and re-requests during a non-durable model request; otherwise becomes Steer | Failure or no further request is possible; after becoming Steer, its rules apply |
| Followup | After the current round succeeds, the queue is merged into the next round's input | The current round fails or is cancelled |
| Interrupt | Cooperative cancellation | Does not mean all external side effects have stopped |

A nil return from enqueueing does not mean processing is complete. Use the following pattern to handle input still left in the queue after the run settles:

```go
out, err := handle.Wait(waitCtx)
pending := handle.PendingInputs()
// err/out describe the result of this run; pending only describes input not yet picked up by the loop.
// The host decides whether to show it or schedule a new execution; it must not use this to automatically retry business effects that already happened.
```

PendingInputs is an independent read-only snapshot; calling it does not consume input. Order is preserved within a queue, and there is no global arrival order across queues. It can also be queried during a run, but will change afterwards. The context passed to Wait only controls waiting; a timeout does not cancel the Handle, and only the snapshot taken after the run has actually ended is stable. To abort, call Interrupt explicitly; the Handle waits for the current round's ExecuteStream to exit, including the tools and cleanup it runs synchronously. Tools and hooks must return cooperatively, and Wait can use a separate bounded context; a wait timeout must not be treated as cleanup being complete. Errors returned by the execution source and cancellation causes are all preserved in the error chain; after the producer side closes, the Handle reads a private terminal state and does not rely on public error events that cancellation may drop. Cancellation events from the public ExecuteStream remain best-effort.

The Handle inbox is an in-process object. When input must be received reliably across restarts, the host first persists the inbox and the receive IDs, and then manages delivery and acknowledgement. Input that has been picked up no longer appears in PendingInputs even if the request fails; this interface does not provide "all messages not yet successfully processed" or exactly-once.

The derived execution key of a Followup only identifies durable operations within a round; it does not persist round scheduling. If the 1st Followup is waiting at an approval, after a restart a bare `Start(ctxWithResume, original input)` replays round 0 and ends, without automatically entering that Followup. The host must rebuild the Followup chain using the original rounds and original merge batches, and enqueue them before the corresponding previous round ends; a different batch split may cause a resume intent mismatch. `WithResume` provides the approval credential; it does not rebuild scheduling. For full verification, see the [Handle lifecycle regression](../../builder/handle_lifecycle_test.go).

Replay may complete immediately, so do not assume that calling Followup right after Start returns will always be in time. The host needs an explicit synchronization boundary (the regression coordinates with a RequestHook) to guarantee batch reconstruction; fixed multi-round tasks are better orchestrated explicitly with a durable Graph, avoiding reliance on goroutine scheduling. A successful return from the Handle only describes the rounds actually rebuilt in this run; it does not prove that all work in the host's durable inbox is complete.

## Distinguishing Three Terminal States

- `AgentEvent.done`: one ExecuteStream call has completed, including the final Output after middleware processing. text/tool_end is neither the final answer nor the complete terminal state of the run.
- `Handle.Wait`: all Followup rounds have ended, or the run failed/paused; it returns the result of the last round. ErrWaiting still requires the host to continue the run.
- The stream Done of `DurableGraph.AsRunnable`: this call has ended; whether the business is complete is checked with GraphSnapshot.Done, and Waiting means it is still waiting.

When tools execute concurrently, GoAgent keeps results, tool_end, and OnResult processing in model call order, and does not copy Pi's completion-order event semantics. See the [Execution Contract](EXECUTION_CONTRACT.md) for more.

## Sessions and the External Environment Are Managed Separately

`WithSessionLog` records model history and history/rewrite; PreStep rewrites subsequent context, and Offload moves long results out and reads them by reference. UI details, credentials, and audit information should not all enter the model context; when redacting, check both the text and structured copies. Rewrite records in the session log may contain old content and are not equivalent to deleting historical secrets.

`session.Store.Fork` copies history events and does not undo tool side effects. The host maintains SessionID, RunID/ExecutionKey, sub-task identity, and SandboxID separately; when continuing from old history, explicitly choose the original environment or a new environment/artifacts. Do not change the execution key to bypass pending results or repeat business execution.

For positioning and follow-up admission, see the [Pi core adoption design](../design/PI_CORE_ADOPTION.md) and the capability comparison.
