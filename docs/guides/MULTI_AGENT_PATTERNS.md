# Multi-Agent Prebuilt Patterns

[English](MULTI_AGENT_PATTERNS.md) | [中文](MULTI_AGENT_PATTERNS_CN.md)

The `multiagent` package provides, on top of `workflow`, three graph constructors (`Sequential`, `Loop`, `RouterSupervisor`), a parallel combinator `ParallelStep`, a tool-based Supervisor (`SupervisorTools`), and a text relay layer (`Conversation`, `TextStep`, `TextNext`). The constructors return an uncompiled `*workflow.Graph[T]`; the host chooses `Compile` or `CompileDurable` itself, and durable semantics are inherited directly from `workflow`. This package adds no runtime, scheduler, or message delivery.

Graph start/end and the route-finish marker are all imported from `github.com/kart-io/goagent/constants`, as `WorkflowStart`, `WorkflowEnd`, and `MultiagentFinish`; the old `workflow.START/END` and `multiagent.Finish` entry points have been removed.

This package has the same name as the old `multiagent` removed in v0.17, but its API and semantics are incompatible, and old code cannot be migrated directly. For the design rationale see [Multi-Agent Prebuilt Patterns Design](../superpowers/specs/2026-11-05-multiagent-patterns-design.md); for an offline example see [`examples/multi-agent-patterns`](../../examples/multi-agent-patterns/README.md). The four `examples/advanced/multi-agent-*` examples demonstrate hand-written topologies; this package demonstrates prebuilt patterns.

## Choosing a Pattern

| Scenario | Pattern | Key points |
|---|---|---|
| S1 Ticket triage | `RouterSupervisor` + `TextNext` | The first line of the triage Agent's output is a member name or `FINISH`; `MaxRounds` limits how many times members run |
| S2 Reply writing and review | `Loop` | `Until` reads the review verdict; `MaxIterations` is a hard cap |
| S3 Parallel multi-source lookup | `ParallelStep` inside `Sequential` | `Clone` uses `CloneConversation`, `Merge` uses `MergeTurns` |
| S4 Fixed pipeline | `Sequential` + `AgentStep` | Typed state; an error returned by `out` fails the node |
| S5 Approval of high-risk operations | `RouterSupervisor`, with the refund member as a custom `Step.Run` | Split into register → review → execute, each segment pinned by a durable operation |
| S6 Follow-up questions to the customer | `durable.AwaitInput` inside a custom `Step.Run` | Put format validation in validate |
| S7 Complex consultation delegated by the model | `SupervisorTools` | The main Agent decides which experts to call |
| S8 Hand off to a human when over the limit | `MaxRounds` / `MaxIterations` | Returns `agentErrors.ErrWorkflowMaxSteps`; switch to a new run ID and a new root budget |
| S9 Hand off to a human on an invalid decision | `RouterConfig.Fallback`, `LoopConfig.Fallback` | When routing is invalid, run the fallback Step and then end |

Trade-offs between the two Supervisors: every step of `RouterSupervisor` is a graph node, with node checkpoints, can be listed in `ApprovalNodes`, has a hard step cap, and routing is decided by host code. `SupervisorTools` turns members into model tools, and the model decides within one turn which members to call and how many times. It is flexible, but is bounded only by the Agent's `MaxIterations` and the root budget; member calls are not graph nodes and cannot be approved individually.

## State Design

A graph has exactly one state type T. A durable graph round-trips the state through JSON even on its first run, and the output of parallel branches also round-trips through JSON before `Merge`. So T uses only exported fields, and must not hold concrete types inside `any`, data that depends on pointer identity, or values whose numeric type would be changed.

The text relay layer uses `Conversation{Input, Turns}`; when you need typed fields such as an order ID, use `AgentStep` with a custom T for all steps. `AgentStep`'s `in` and `out`, as well as `Until`, `Next`, and `Merge`, must all be pure functions: they are re-executed after recovery, and the result of `in` also goes into the model request intent. Logic that needs a model's judgment belongs in a Step, and routing reads the result the Step writes back to the state.

## Step and Round Limits

| Pattern | `MaxSteps` |
|---|---|
| Sequential | number of steps n |
| Loop | `MaxIterations × n` |
| RouterSupervisor | `2 × MaxRounds + 1` |

Each `ParallelStep` counts as 1 step. `MaxSteps` is computed by the constructor, and `cfg.MaxSteps` must be 0; `cfg.MaxConcurrency` is passed to parallel nodes as usual. A graph returned by a constructor should only be compiled directly: calling `AddNode` on it further does not error, but the step limit is no longer accurate.

The executor counts steps only for nodes, not for routing. After the last allowed node runs, routing still happens, and routing to `END` is success:

- When the `MaxIterations`-th Loop iteration ends with `Until` true, the run ends normally; if false, it returns `agentErrors.ErrWorkflowMaxSteps`.
- After the `MaxRounds`-th member run in a Router, the Supervisor still runs once more (sending one more model request): returning `FINISH` ends normally, and choosing a member returns `agentErrors.ErrWorkflowMaxSteps`.
- Fallback does not add steps: the Router's Fallback takes one member round, and the Loop's Fallback takes the position of the first step of the next iteration. When the last decision is invalid, it returns `agentErrors.ErrWorkflowMaxSteps` if a Fallback is configured (handed off to a human the same way as an over-limit run), and `ErrInvalidRoute` if not.
- When you need "run exactly N rounds and then end normally", record the round count in T and let `Until` decide.

Hand off to a human when over the limit (S8): after a durable run returns `agentErrors.ErrWorkflowMaxSteps`, the checkpoint stays at `Next == MaxSteps`, and calling `Run` again with the same run ID still errors. When handing off to a human or reprocessing, use a new run ID and create a new root budget for it: the Builder requires the root budget's `RootKey` to match the durable graph's run ID (otherwise it reports `agent budget root conflict`), and the original root budget's usage is not reset by switching run IDs.

```go
runID := ticketID + "/human-1"
c, err := budget.NewDurable(budget.Config{RootKey: runID, Version: "v1", MaxModelRequests: 30}, budgetStore)
if err != nil {
    return err
}
snap, err := graph.Run(budget.WithController(ctx, c), runID, initial, "")
```

## Routing Failures and Fallback

After a durable graph commits a node's output, recovery re-runs routing with the same committed state. `Next` and `Until` are pure functions, so invalid input yields the same error on every recovery; fallback is therefore done by wrapping the route, not by retrying:

- `TextNext` takes the first line of the last turn's output, trims leading and trailing whitespace, and requires it to equal exactly a member name or `FINISH`. When there are no turns, the first line is empty, or it does not match, it returns an error wrapping `ErrInvalidRoute`; it does no fuzzy matching. Member names used for text routing must be single-line.
- A custom `Next` or `Until` should also wrap `agentErrors.ErrInvalidRoute` when it judges the input invalid.
- `GuardRoute`, in order: when `Next` returns an error wrapping `ErrInvalidRoute` and a fallback is configured, it goes to the fallback, and other errors are returned as is; `FINISH` maps to `END`; names not in the member set (the Supervisor name, the fallback name, `"__end__"`) are treated as invalid; otherwise it returns the member name.
- The fallback Step ends the run after it executes. The state it receives includes the invalid output, which it can use to hand off to a human. Without a fallback, `ErrInvalidRoute` is terminal for that run ID; reprocess with a new run ID and a new root budget.
- This package does not "ask once more": when needed, write that into the Supervisor Step itself.

## Text Relay Layer

The default input of `TextStep` is Input plus `[Agent]: Output` for each turn, joined line by line with no truncation, so the Router's Supervisor can see the original task and the Loop's writer can see the previous draft and the review comments. The output is taken from `AgentOutput.Result`: strings are used as is, other types are JSON-encoded, and the result is appended as one turn; empty answers are recorded too.

- **Growth**: a single request grows linearly with the number of turns, cumulative input grows quadratically with the number of turns, and checkpoints repeatedly serialize the full state. With many turns, use `WithLastTurns(k)` or `WithRender`; when you need a summary, make the summary a separate Step. `WithRender` and `WithLastTurns` are mutually exclusive.
- **Determinism of render**: render must be a pure function of `Conversation`. The model request intent includes the full request, so if render produces different output when the same node visit is recovered, it reports `operation intent or attempt policy changed`. Changing render or the window size is equivalent to changing the Agent's behavior; handle it as described in "Versioning and Migration".
- **Content isolation**: the default format concatenates customer input and member output directly, so external text can forge turns or steer triage. High-risk actions must have their concrete parameters confirmed by a human before execution (S5) and cannot rely on triage decisions alone; for flows sensitive to external input, use a structured render (for example, JSON-encoding each turn).
- **Parallelism**: `ParallelConfig.Clone` must use `CloneConversation`; `MergeTurns` appends the turns newly added by each branch in branch registration order, and errors when a branch's turns are not prefixed by the turns from before the merge.

## Durable Integration Contract

`multiagent` cannot inspect an Agent's internal configuration; the host must meet the following requirements.

**Agent side**:

- Each Agent sets a stable, non-empty scope with `WithDurableScope`; the journal key is "node execution key → `agent/<scope>` → model or tool operation key". Each Step is an independent node, and within a loop the execution key of each visit includes the step number, so they do not conflict. A second Agent call within the same node must use a different scope. For scope rules see [Native Orchestration: Multi-Agent Scopes and Entry Constraints](NATIVE_ORCHESTRATION.md#multi-agent-scopes-and-entry-constraints).
- With tools, the model client must implement `llm.ToolStreamClient`, and tools must be wrapped by `Dispatcher.Tools()`; otherwise it fails before the model call.
- The session is created anew by `AgentFactory` each time. If any nondeterministic content (memory, retrieval results, the current time, random examples, render, and so on) changes on recovery, the intent of completed requests becomes inconsistent and an error is reported; it is never silently replayed.

**Model request interruption**: by default a model request is allowed only one attempt. On recovery it is handled according to the journal record:

| What happened | Record | Default | With `WithInterruptedModelResend` enabled |
|---|---|---|---|
| Crash after the request was sent and before the terminal state was persisted (or saving the terminal state failed) | pending (`ambiguous`) | Rejected | Resent while attempts < 3 |
| The run ctx was canceled | failed (`canceled`) | Rejected | Resent while attempts < 3 |
| The error chain contains `context.DeadlineExceeded` (run timeout or root budget deadline) | failed (`deadline`) | Rejected | Resent while attempts < 3 |
| The model returned another error | failed (`execution`) | Rejected | Rejected |

"Rejected" means the node fails with `*durable.OperationError` (`errors.Is(err, agentErrors.ErrOperationAttention)`) and the same run ID cannot continue. The 3 counts durable attempts, not requests: within each attempt, `WithRequestError` may still retry as is, so one turn sends at most `3 × (1 + maxRecoveries)` requests (`TestModelResendWithRequestRetries` in `builder/model_resend_test.go`). When the model client wraps a timeout as a cancellation, the timeout is also recorded as canceled and may be resent. Resending may cause duplicate charges; the switch must stay the same for the entire lifetime of a run, otherwise recovery reports a policy change. For the deadline and execution classification see `core/durable/operation_test.go`.

**Budget**:

- `SupervisorTools` members are model tools, and the ctx must carry a root budget when they are called; durable runs must use a durable budget created by `budget.NewDurable`.
- The intent of a durable model request includes the budget configuration. On recovery you must reuse the original budget store and exactly the same configuration: reusing the store with a changed configuration makes `budget.NewDurable` report `budget configuration or record changed`; switching to a new store with a changed configuration makes completed requests report an intent mismatch; switching to a new store with the same configuration makes replay report `usage has no reservation`; and if the continued run has no requests to replay, nothing catches it at all and usage counts from zero. The budget store must be persisted together with the graph store and always reused on recovery.
- Member Agents must not use the Dispatcher of the Supervisor or of any ancestor call, otherwise `agentErrors.ErrDispatcherReentry` is triggered; members that do not nest within each other can share one.

## Human Waits

### Follow-up Questions to the Customer (S6)

`AgentStep`'s `in` and `out` do not receive a ctx and cannot pause. When you need input from a human after the Agent's output, write a custom `Step.Run`:

```go
step := multiagent.Step[Case]{Name: "extract", Run: core.NewRunnableFunc(func(ctx context.Context, c Case) (Case, error) {
    fields, err := extractOnce(ctx, c) // The Agent uses WithDurableScope("extract") and is replayed from the journal on recovery
    if err != nil || fields.OrderID != "" {
        c.OrderID = fields.OrderID
        return c, err
    }
    raw, err := durable.AwaitInput(ctx, "order-id", json.RawMessage(`{"question":"请提供订单号"}`), validateOrderID)
    if err != nil {
        return c, err // Waiting pauses the graph; Snapshot.Interrupts gives the interrupt key
    }
    return c, json.Unmarshal(raw, &c.OrderID)
})}
```

- Format validation of the order ID must go into validate. When validate rejects, the interrupt keeps waiting and `Resume` returns `*workflow.ResumeRejectedError`; the host submits a corrected value with the same interrupt key. Input accepted by validate is fixed from then on, and submitting a different value afterwards reports `agentErrors.ErrInterruptAnswered`.
- validate must be a pure function of the input. Its error text is persisted and exposed externally, and must not contain sensitive information.
- Checks that depend on external data (such as whether the order exists) must not be in the same node as `AwaitInput`, otherwise on re-entry a change in external data would take a different branch. Make the check the next node, write the result into the state, and route back to the question node when it fails; each visit to the question node is a new node execution, so interrupts do not conflict:

```go
g := workflow.NewGraph[Case](workflow.Config{MaxSteps: 1 + 2*maxAsks})
_ = multiagent.AddStep(g, askStep)   // Ask a follow-up question
_ = multiagent.AddStep(g, checkStep) // Check whether the order exists, write c.OrderFound
_ = g.AddEdge(constants.WorkflowStart, "ask")
_ = g.AddEdge("ask", "check")
_ = g.AddBranch("check", func(_ context.Context, c Case) (string, error) {
    if c.OrderFound {
        return constants.WorkflowEnd, nil
    }
    return "ask", nil
}, "ask", constants.WorkflowEnd)
```

### Review After Registration (S5)

Approval via `ApprovalNodes` happens before the node executes, and only suits coarse-grained gates such as "whether to enter a flow". To approve a concrete action decided by the Agent (the refund amount and order), split it inside the node into "propose → register → review → execute":

1. Call the refund Agent to get a structured proposal; the Agent's tools are read-only. The request ID should come from the ticket: the example uses `"refund-"+orderID` only for demonstration, and a second legitimate refund for the same order would be rejected by `Register` because the request ID is the same.
2. `durable.Do(ctx, "refund/register", intent, policy, run)` registers the request and computes the dispute decision. The intent contains only the request ID and the proposal, not rule thresholds or time; registration and decision are written in the same transaction (via the `refund.Service.OnRegister` hook: called only when a new request is inserted, within the same transaction, and any error rolls everything back). `Reconcile` records completion only when both the request and the decision exist, to avoid recording a zero-value "no dispute" and bypassing review. The decision is made by business rules; the Agent can only upgrade "no dispute" to "disputed". The decision is pinned with the operation record, so it does not flip even if rule thresholds or order facts change while waiting. Registration is idempotent by request ID (`refund.Service.Register` returns the existing request for the same intent and does not call the hook again), and the policy is set to `MaxAttempts: 3, RetryInterrupted: true`: it allows re-running when the record is stuck at pending and was not reconciled, or failed due to cancellation or timeout; if the re-run cannot read the decision, it errors instead of letting the zero value through.
3. When disputed, `durable.AwaitInput(ctx, "refund-review", info, validate)`. info contains the order ID, amount, decision reason, and the submitter needed to resolve the reviewer; the input is `{"approved": bool, "note": string, "approver": string, "reason": string}`, where `approver` is filled in by the host after authentication and must be non-empty on approval; `reason` is optional, and the timeout input is `{"approved": false, "reason": "timeout"}`. validate decodes strictly and rejects unknown fields. On rejection the refund is not executed, and "rejected by a human" is written back to the state.
4. The policy of `durable.Do(ctx, "refund/execute", ...)` is set to `MaxAttempts: 3, RetryFailed: true`, and a receipt-based `Reconcile` is provided. When the record was interrupted (stuck at pending, or failed due to cancellation or timeout), `Reconcile` checks first; if a receipt is found it records completion and does not run run again. When the refund was submitted but the call returned some other error, the record is failed; under the default policy failed is terminal and a continued run only gets an operation error, while `RetryFailed` lets the continued run re-run run within the attempt limit. run first looks up the receipt by request ID and returns directly if one exists; otherwise it approves and refunds. `refund.Service.Approve` checks the validity period and order facts; if a re-run first compares the order or calls `Approve`, an order change or approval expiry after the refund would misreport a completed refund as failed. `refund.Service.Refund` itself also returns an already-submitted receipt first, and then checks the cumulative limit in the same transaction, as a second line of defense. When order facts change during review, write "order changed, a new request is required" back to the state as a business result, not as a node error. The execute operation also provides a receipt-based `Reconcile`.

Do not put refunds in an interruptible tool: an interruptible tool allows only one attempt and has no reconciliation hook, so a crash where "the refund took effect but the record was not persisted" leaves pending and recovery is rejected; a continued run would also re-run the whole tool, and a request the reviewer already rejected might be re-judged as undisputed and refunded directly. The business implementation reuses `workflow/refund.Service`; reviewer resolution and the human handoff are defined by the [customer service orchestration case](https://github.com/kart-io/goagent-examples/blob/main/docs/superpowers/specs/2026-11-05-customer-service-design.md).

### Wait Limits and Budget Deadlines

Every model request reservation checks the root budget's `DeadlineUnixMilli`, and the budget configuration cannot change during the run's lifetime. For flows with human waits, the deadline must cover the entire wait window (the business default is 48 hours per wait, and one run may first ask a follow-up question and then go to review, so set it to the sum of all wait limits plus a margin), or set no deadline and instead control spending with `MaxModelRequests`, with the host implementing a run TTL: when the snapshot's `Waiting` is true and the limit has passed, give up and hand off to a human with a new run ID. If the deadline is set by the duration of a single request, a customer replying hours later guarantees that model requests in the continued run fail with `context.DeadlineExceeded`. On timeout, the host answers the interrupt with `{"approved": false, "reason": "timeout"}`, and the flow ends through the rejection branch.

Other interrupt rules: an interrupt inside an Agent tool requires the tool to be declared interruptible (`WithInterruptible`), or to be a Runnable tool declared `Resumable`; if a subgraph called by an Agent is a pure approval wait, it is treated as a node failure in the outer durable graph. For the general semantics of approvals and interrupts see [Human in the Loop](HUMAN_IN_THE_LOOP.md).

## SupervisorTools

Each member is wrapped as `tools.NewRunnableToolFactory`: the argument Schema is fixed to `{"task": string}`; the result is `AgentOutput.Result` (strings as is, other types JSON-encoded); `Version` is required and is bumped whenever the member's behavior, prompt, or tool set changes; `Timeout` takes effect only when called through a Dispatcher, and 0 inherits the Dispatcher default; the preconditions for `Resumable` are the same as for `RunnableToolConfig.Resumable`: all effects within the member are recorded under derived keys, and it can only be used in calls with a durable journal.

The host registers the returned tools with the Supervisor Agent's Dispatcher, then hands them to the Agent via `Dispatcher.Tools()`. With invalid arguments, a durable run by default rejects the whole model turn; to hand argument errors back to the model for correction, enable `ReturnArgumentErrors` on the Supervisor's Dispatcher. For usage attribution and member identity see [Multi-Agent Usage](MULTI_AGENT_USAGE.md).

## Events from Parallel Branches

Neither compilation mode forwards intermediate events from Agents inside parallel branches: plain `Compile` runs them directly through branch functions, and durable mode emits only `branch_start` and `branch_end`, with only the merged result. Branch names only need to be unique within their parallel node and are not graph nodes: `ApprovalNodes` can only contain the outer name of a `ParallelStep`, and branch names are rejected by `CompileDurable`.

## Supported Combinations

| Combination | Supported |
|---|---|
| `ParallelStep` as a step of Sequential, Loop, or RouterSupervisor, or as a Router member | Yes |
| A plain `Compile` graph embedded into another plain graph with `AddRunnable` | Yes |
| A durable graph wrapped via `AsChildRunnable` into a `Resumable` tool and given to an Agent | Yes |
| Nesting constructors (for example, a step of Sequential being a Loop) | Not provided; write the graph by hand |
| A compiled graph placed as a node in a durable graph | No |
| A branch of `ParallelStep` that is itself a `ParallelStep` | No; construction errors |

For combinations such as "extract → write⇄review loop → compliance", write the topology by hand with `workflow.NewGraph`, add this package's Steps with `AddStep`, wrap your own routing with `GuardRoute` to reuse fallback, and set `MaxSteps` yourself. When there are nodes after the loop, the total step count cannot bound the loop in the middle (after the N-th review fails, the step left for compliance would be used up by the (N+1)-th write), so the review exit route derives the round count from the state:

```go
const reviewRounds = 3 // MaxSteps = 1 + 2*reviewRounds + 1

reviewExit := func(ctx context.Context, c multiagent.Conversation) (string, error) {
    passed, err := parseVerdict(c) // Returns an error wrapping agentErrors.ErrInvalidRoute when it cannot parse
    if err != nil {
        return "", err
    }
    if passed {
        return "compliance", nil
    }
    rounds := 0
    for _, t := range c.Turns {
        if t.Agent == "review" {
            rounds++
        }
    }
    if rounds >= reviewRounds {
        return "", fmt.Errorf("%w: review loop limit %d", agentErrors.ErrWorkflowMaxSteps, reviewRounds)
    }
    return "write", nil
}
g := workflow.NewGraph[multiagent.Conversation](workflow.Config{MaxSteps: 1 + 2*reviewRounds + 1})
// After adding extract, write, review, compliance, and the fallback human with AddStep and wiring the edges:
route, targets, err := multiagent.GuardRoute(reviewExit, []string{"write", "compliance"}, "human")
if err != nil {
    return err
}
err = g.AddBranch("review", route, targets...)
```

`ErrInvalidRoute` goes to the fallback, and `agentErrors.ErrWorkflowMaxSteps` is returned as is; the durable run stops at the routing position of the review node (`Routing` is true), and calling `Run` again with the same run ID still errors. Because one step is left for the compliance node, the fallback can run when the N-th decision is invalid, which differs from the Loop constructor's "an invalid last round returns `agentErrors.ErrWorkflowMaxSteps`". The N inside the route closure does not go into the plan digest; only the `MaxSteps` computed from N does, so compute both from the same constant.

## Versioning and Migration

The plan digest of a durable graph includes `DurableConfig.Version` and the entire `workflow.Config` (including `MaxSteps` and `MaxConcurrency`), and both are checked on recovery. After changing `MaxIterations`, `MaxRounds`, `MaxConcurrency`, the step list, or approval nodes, old runs cannot be recovered by the new graph, and bumping `Version` does not help either. The host keeps the old version's construction parameters to compile the old graph and process unfinished old runs; the new version only accepts new runs. Checkpoints carry their own version, so route by version:

```go
graphs := map[string]*workflow.DurableGraph[multiagent.Conversation]{
    "v1": mustCompile(buildRouter(3), "v1"), // Old parameters: MaxRounds 3
    "v2": mustCompile(buildRouter(5), "v2"),
}
cp, err := store.Load(ctx, runID)
switch {
case errors.Is(err, os.ErrNotExist):
    return graphs["v2"].Run(ctx, runID, initial, "")
case err != nil:
    return nil, err
}
graph, ok := graphs[cp.Version]
if !ok {
    return nil, fmt.Errorf("运行 %s 的版本 %s 已下线", runID, cp.Version)
}
return graph.Run(ctx, runID, initial, approvalKey)
```

## Operational Monitoring

Collect from node events: the execution rate of Fallback nodes (such as `human`), and the occurrence rates of `agentErrors.ErrWorkflowMaxSteps` and `agentErrors.ErrInvalidRoute`. For alerts when usage exceeds a threshold use `budget.WithUsageHook`, whose events include cumulative statistics; `MaxModelRequests` must be ≥ 1 and should be set as a loose runaway guard, and when it is hit, hand off to a human just as with exceeding the round limit.
