# Final Acceptance, Governance Assembly, and Upgrade Checks

[English](EXECUTION_ACCEPTANCE.md) | [中文](EXECUTION_ACCEPTANCE_CN.md)

This guide covers the execution contract improvements. Complete offline example: `go run ./examples/execution-governed`, which uses a mock model and needs no credentials; it verifies assembly, the model's final receipt, and zero new requests/effects after a rebuild. It does not represent real business authorization or production load validation.

## Final Output Acceptance

`builder.WithOutputValidator(validators ...builder.OutputValidator)` accepts `func(context.Context, *builder.AgentOutput) error`. It checks the final output after middleware processing, covering plain text, native tool loops, direct tool returns, early termination by PreStep, and durable replay. It runs both before and after the Builder's completion callbacks, and `AsRunnable` additionally checks after the outer completion callbacks via `core.StreamRunnable.WithFinalValidator`; cloning the configuration and callbacks keeps this terminal gate.

```go
b.WithOutputValidator(func(ctx context.Context, out *builder.AgentOutput) error {
    if err := ctx.Err(); err != nil { return err }
    if out.Result != expectedReceipt { return fmt.Errorf("最终回执不匹配") }
    return nil
})
```

Validators must be read-only and safe to call repeatedly; they should not bill, write to a database, or modify the result. On an error or cancellation, no successful terminal state is published, and the Builder's error handler cannot clear the validation error; a panic is converted into a recognizable error. A streaming error chunk may also carry `Done=true`; success requires `Done && Error == nil`.

`TurnStopping` handles optional correction, while `OutputValidator` handles mandatory acceptance. Insufficient iteration quota skips the former but cannot skip the latter. Validators do not trigger retries and do not roll back model calls, tool effects, session logs, checkpoints, or callback effects that have already happened. Intermediate text may already have been emitted. Other results actively transformed by a downstream `Pipe` should be accepted separately by that stage.

A plain generic `StreamRunnable` can use `WithFinalValidator(func(context.Context, O) error)` to check the result after its own completion callbacks; multiple validators run in order. `WithCallbacks` and `WithConfig` keep the validators; when no validator is configured, no model requests are added.

## Standard Governance Assembly

`execution.NewGovernedBuilder(client, scope, agentID, registry, dispatchConfig)` returns an ordinary `SimpleAgentBuilder`, a `GovernanceReport`, and an error, reusing Scope, Dispatcher, and Builder.

Creation requirements: the Scope has an explicit root request budget and a positive tool quota; a Scope timeout or an absolute root budget deadline is configured; Authorize, a positive `MaxAdmissionConcurrency`, and `MaxQueued` are provided explicitly; at least one tool is registered, and the client implements `ToolStreamClient`. Other Dispatcher parameters keep their existing defaults, and the host still owns and closes the resources.

The host must call `scope.Bind` from creation and pass the derived context to `BuildContext` and to execution. The assembled request hook checks Scope, budget, and session identity before each model call, and checks them again before tool authorization; if Bind is forgotten, no model request is sent. Side effects of other host callbacks and custom hooks must still be controlled by the host.

`builder.WithDispatcher(d)` specifies the complete tool set and cannot be mixed with `WithTools`, regardless of configuration order; a nil dispatcher fails at Build. To add tools, register them in the same Registry first and then create the Dispatcher, so that tools cannot bypass governance. Repeated WithDispatcher calls use the last configuration, and repeated Build calls do not write the dispatched tools back into the Builder.

The report only describes the assembly at creation time: root deadline, root quota, durable budget, logs, tool list, and dispatch defaults. It contains no references to functions or mutable resources, and does not prove that any particular business operation was authorized. `Dispatch.Timeout` is the default tool timeout and can be overridden by tool declarations. Later Builder configuration does not modify an existing report; hosts must not treat the snapshot as a dynamic security certification.

## Build-Time Capability Checks and Migration

The following configurations require a `ToolStreamClient` and a non-empty tool set; otherwise BuildContext fails before initialization callbacks and storage access: PreStep, RequestHook, TurnStopping, PromptSections, PromptContexts. Some of these were previously ignored; they are now uniformly rejected up front. `SessionLog` supports ordinary streaming chat and native tool loops and does not require tools; plain chat keeps using the existing `model/answer/0` durable operation key.

Plain-text workloads that only check the final format should use OutputValidator; when loop intervention is needed, configure a real tool stream. RequestError unchanged retries still support the tool-less path. Existing WithTools usage keeps working; when unified dispatch is needed, switch to WithDispatcher and remove WithTools.

## Recovery Manifest and Pre-Upgrade Checks

`workflow.DurableConfig` adds optional `StateVersion` and `Components map[string]string` for declaring the state structure and the versions of host components such as Agents, tools, prompts, and policies. Non-empty declarations are included in the plan digest; after a change, old runs no longer match. An empty string together with a nil or empty component map leaves the legacy plan digest unchanged.

```go
d, err := graph.CompileDurable(ctx, workflow.DurableConfig{
    Store: journal, Version: "refund-v2", StateVersion: "refund-state-v1",
    Components: map[string]string{"agent": "v3", "payment-tool": "v2"},
})
if err != nil { return err }
manifest := d.Manifest() // Independent copy; can be recorded in release artifacts.
report, err := d.CheckCompatibility(ctx, "run-1", "run-2")
```

Manifest exports the versions, plan digest, and a copy of the declarations, without business data. CheckCompatibility reuses Diagnose and reads only the specified runs; it takes no run locks, writes nothing, approves nothing, and does not call nodes or decode business state. A run that does not exist is incompatible; empty or duplicate IDs return an error. Both the component input map and the exported map are copied, so external modifications cannot change the compiled identity.

Compatible only means the declarations and checkpoint structure match: `outcome_unknown` may also match, and external effects and active executors must still be reconciled. Reading multiple runs does not form a consistent snapshot; tasks are not automatically listed, migrated, resumed, or rolled back. Go closure behavior cannot be detected automatically, so the host must bump versions correctly.

Upgrade order: archive the old Manifest and the IDs of pending runs; run the read-only check with the new graph; for incompatible in-flight tasks, first finish them on the old version, drain them, or perform an explicit migration; finally switch new traffic. Do not overwrite the old plan digest just to pass the check.

## Performance Benchmarks

History reads, no-change sync, append sync, and rewrite sync are measured separately, with in-memory and file stores each covering 100, 1000, and 10000 messages. Mutation scenarios rebuild a fixed history outside the timer and use a fixed iteration count to control fixture construction cost:

```sh
GO_TEST_MODE=true go test -run '^$' -bench '^(BenchmarkSessionHistory|BenchmarkLargeToolResult)$' -benchmem -benchtime=5x -count=3 ./core/session ./builder/contextpolicy
GO_TEST_MODE=true go test -run '^$' -bench '^BenchmarkAgentSessions$' -benchmem -benchtime=300ms -count=3 -cpu=1,4 ./builder
```

The session benchmarks distinguish serial execution on the same Agent from concurrent independent sessions; the mock model has no network latency, so the results must not be taken as real model latency. When verifying related repositories with a worktree, explicitly run `go run ./scripts/compatcheck -root /path/to/repos -core-dir /path/to/current/goagent -out /new/report`; the Makefile's COMPAT_CORE defaults to the current directory.
