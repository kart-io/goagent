# Execution Entry Points and Extension Contract

[English](EXTENSION_CONTRACT.md) | [中文](EXTENSION_CONTRACT_CN.md)

The recommended entry point is `agent.AsRunnable().Invoke/Stream`. `ExecuteStream` shares the same execution source; the Builder accepts any `llm.Client`, and configuring tools requires `llm.ToolStreamClient`.

## Public Contract and Change Verification

In the current 0.x versions, the following behaviors form the public contract; the version number does not promise that every interface stays unchanged forever. Changing a signature or recovery behavior requires migration notes in the CHANGELOG and verification of all related modules; do not add compatibility aliases whose only purpose is to hide a break.

| Boundary | Behavior that must be preserved | Regression location |
|---|---|---|
| `Runnable` / `StreamRunnable` | Invoke and Stream share the execution source; a success has exactly one terminal state; a missing/duplicate terminal state, a late error, or cancellation does not publish a successful result | `core/stream_runnable_test.go`, `core/composition_stream_test.go`, `builder/runnable_test.go` |
| `llm.Client` / `ToolStreamClient` | Complete tool calls come only from a successful terminal chunk; tool IDs, native continuation state, errors, and known usage are preserved, and unknown values are not faked as zero | `llm/providers/native_contract_test.go`, `structured_hardening_contract_test.go`, `builder/agent_usage_test.go` |
| `durable.Store` / durable operations | Lock, atomic Save/SaveAll, and read semantics follow the interface comments; completed operations are replayed; unknown effects are reconciled first or retried only when explicitly allowed | `core/durable/storetest`, `operation_wait_test.go`, `save_all_test.go` |
| Approval and interrupt recovery | Invalid input keeps the run waiting; a consumed resume key does not apply state changes again; host authorization cannot be replaced by an approval key | `workflow/resume_test.go`, `resume_process_test.go`, `core/durable/interrupt_test.go` |
| Root budget | Parent and child calls share the quota; recovering a durable run does not reset it; unknown usage, configuration drift, and concurrent requests are handled by ledger rules | `core/budget/unified_test.go`, `limits_test.go`, `builder/model_budget_test.go` |

Run `make test-contracts` to verify these boundaries, `make test-frameworkbench` to verify the standalone module, and `make compatibility` to verify the actual callers in local Harness, Contrib, and Examples. The cross-repo report records the current core dependency directory, source commit, full test logs, and skipped items. Changes to functions or state structures require proactively bumping the host workflow version; a successful Go compile alone does not prove that old checkpoints can be recovered. Parts involving databases, real models, or business authorization also need acceptance evidence from the corresponding environment.

## Extension Points

| Extension point | Changes execution? | Errors and timeouts | Recovery semantics |
|---|---|---|---|
| `core/observe.Observer` | Read-only observation, not a checkpoint | Synchronous sinks must return promptly; panics are isolated; can be explicitly wrapped with AsyncObserver | Events are not a durable ledger; replays carry the Replayed flag |
| `WithUsageHook` / `budget.WithUsageHook` | Read-only token usage notification; does not change execution | Synchronous and concurrency-safe; query/callback panics do not change business behavior; without a bound budget it is single-call evidence only | Inherited from parent to child, a non-nil child hook replaces it; the replay flag is preserved and cumulative values must not be added up; see the [usage guide](MULTI_AGENT_USAGE.md) |
| `core.Callback` | Lifecycle notification | OnStart/OnEnd errors on StreamRunnable fail the call; OnError's return value is ignored and panics are treated as errors; the OnEnd return value of the legacy RunnableFunc is still ignored | Parent and child callbacks are independent; hosts should avoid registering side effects twice |
| `core/middleware` | Can modify requests and results | Returning an error aborts; bound by the call context | Middleware side effects are not automatically idempotent; they need a host version and execution key |
| `WithOutputValidator` / `StreamRunnable.WithFinalValidator` | Read-only final acceptance; cannot add model requests | Errors/cancellation/panics block the successful terminal state; validators may be called multiple times | Replays re-run acceptance; saved history and external effects are not rolled back |
| `PreStep`, `TurnStopping` | Can modify history, block requests, or continue the run | Errors abort; continuation is limited by the request budget | Durable execution records results, which are replayed on recovery |
| `RequestHook` | Can adjust the model request or client | Errors abort | Must be deterministic in durable mode; changes require bumping the host version |
| `RequestErrorHook` (`WithRequestError`; use `builder.RetryTransient` for unchanged retries of transient failures) | Can compact, fall back, or retry | Native tool streams, with a bounded number of recoveries; streaming answers without tools only support unchanged retries | Unchanged retries stay within the same turn record, and each attempt consumes budget; replacing messages or the client in a durable run ends with an error |
| Pipeline Pre/approval/Guard | Can reject or declare a substitute call | Rejections the model can revise are Rejected, errors abort; approval has its own limit | Admission is outside the operation journal; replays of completed operations skip admission |
| `Authorize` | Host-level authorization | Errors abort; complements the Pipeline; may also be called when arguments are invalid | Do not infer, merely from the function's existence, that other business effects of a replay were authorized |
| Pipeline Around/Post | Wraps execution, changes the result, or appends context | Counts toward the single-call Timeout; abnormal results do not become successes | The frozen result is written to the operation journal |
| `OnResult` | Host result delivery/state rebuild | Errors fail the call; not subject to the tool's single-call Timeout; the host ctx is still valid | May fire on both the first run and replays; deduplicate by ExecutionKey |

Full signatures are defined by the Go documentation in the source; the final business result must come from a successful terminal state, and preview text does not prove the task is complete.

## Consistency Fixes for Pipe and Sequence

Combinators still wait for the previous stage's final output before running the next stage; there is no per-token transformation across nodes. The last stage's stream is validated through the shared execution source: it must have exactly one terminal state and close the channel after it. A missing/duplicate terminal state, events after the terminal state, or a late error all fail; an empty Sequence acts as the identity transformation.

Explicitly configured parent callbacks now apply per input for Invoke, Stream, and Batch, while child callbacks still run independently; legacy callers that registered the same side-effecting callback on both parent and child must deduplicate. Batch now invokes per input, respects the parent's MaxConcurrency, and keeps result indexes; after one input fails it still processes the other inputs, returns ordered partial results and the first observed error, and no longer calls the child's custom Batch. Error output is for diagnostics only and is not treated as a successful business result.

Consumers that stop reading early must cancel the ctx. The combinators' own sends and waits support cancellation, but they cannot forcibly stop third-party synchronous functions or producers that ignore the context; a third party that does not close the channel after delivering the terminal state is waited on until the ctx is canceled. Hosts that do not set a ctx deadline should manage the lifecycle themselves.

## Initialization and Memory

Hosts should build with `BuildContext(ctx)`; OnStart callbacks share that context, and cancellation is checked after the callbacks return. The legacy `Build()` uses Background. Cancellation does not roll back external effects and cannot forcibly stop implementations that ignore the ctx. For standard identity, budget, and observation assembly, see the [Host Integration Guide](HOST_INTEGRATION.md).

Multi-turn history comes only from the session log (`WithSessionLog`). If writing the log fails, the turn fails and the error keeps its errors.Is chain; streamed text may already have been emitted, and the failure does not roll back model calls or business side effects.

For governance and diagnostics integration, see the [Execution Governance Guide](EXECUTION_GOVERNANCE.md).

Loop hooks and prompt extensions that plain-text execution does not support now fail at BuildContext time; RequestError unchanged retries and OutputValidator are still available. See the [capability migration notes](EXECUTION_ACCEPTANCE.md#build-time-capability-checks-and-migration).
