# Multi-Agent Hooks and Token Accounting

[English](MULTI_AGENT_USAGE.md) | [中文](MULTI_AGENT_USAGE_CN.md)

GoAgent accounts for the parent agent, child agents, and summarization requests in a single root budget ledger, and Observer provides read-only notifications. All budgets use the same field-evidence and attribution model; no default storage is created, and business routing and permissions are not changed.

## Enabling Identity and Attribution

```go
controller, err := budget.New(budget.Config{
    RootKey: "business-run-123", Version: "policy-20261101",
    MaxModelRequests: 100,
    MaxAgentCalls: 50,
})
// Check err; use NewDurable(config, hostStore) when cross-process recovery is needed.
ctx := budget.WithController(hostCtx, controller)
agent, err := builder.NewSimpleBuilder(client).
    WithAgentID("coordinator").BuildContext(ctx)
```

Each agent automatically establishes a call identity; WithAgentID names the role explicitly, and when it is not set the role is anonymous. A child agent uses its own instance/session and the passed-in context, and inherits the root budget and Observer. Durable calls still need WithDurableScope; AgentID does not replace it. `execution.ConfigureBuilder` sets SessionID, DurableScope, and AgentID.

MaxAgentCalls limits the number of saved call nodes, including intermediate agents that make zero model calls; 0 means no limit and negative values are rejected; in-flight ledgers written with DefaultMaxAgentCalls=1000 must keep passing 1000 explicitly, otherwise the configuration change is rejected. Recovering the same logical call does not take an extra slot. A role with the same name can have several different call keys. The root RunID is determined by the Scope or Controller.RootKey; if an existing parent identity or observation root does not match, the call is rejected before the request is sent.

Hidden SDK requests that bypass the unified entry point are not metered automatically. ModelInvoking=true is a trusted host declaration, not a model-call detector. Full hook coverage includes ExecuteStream, AsRunnable, and Start, that is, all execution entry points of the Builder.

## Observation Hooks

To get tokens and cumulative statistics directly, use the dedicated hook; there is no need to implement an Observer or filter events:

```go
agent, err := builder.NewSimpleBuilder(client).
    WithUsageHook(func(ctx context.Context, e budget.UsageEvent) {
        // e.Model.Tokens: this attempt; KnownFields marks which fields were reported.
        // e.Model.AgentID / AgentCallKey: agent attribution.
        if e.HasStatistics {
            total := e.Statistics.Root.KnownTokens.TotalTokens
            _ = total // Point-in-time cumulative value; do not add up cumulative values from several callbacks.
            // Look up Self / Inclusive in e.Statistics.Calls by AgentCallKey.
        }
        // Replay flag e.Model.Replayed; the attempt key and Revision can be used to deduplicate projections.
    }).Build()
```

You can also bind parent and child calls together with `budget.WithUsageHook(ctx, hook)`. The parent hook is inherited by child agents by default; a non-nil hook set on a child Builder replaces the inherited one, and a nil configuration keeps the inherited one. Calling WithUsageHook(ctx,nil) directly clears it. An Observer can work at the same time; when you need several targets, dispatch from a single hook.

Without a budget, per-call usage is still notified, with HasStatistics=false; no ledger is created implicitly. When the query fails, StatisticsError is non-nil and the per-call Model evidence is still kept; when accounting fails, AccountingCommitted=false and the usage must not be treated as reliably recorded. Callback and query failures do not change the business result.

The hook runs synchronously, must be safe for concurrent use, and must return promptly; do not wait synchronously inside the hook or re-execute the current agent. The framework provides a bounded context detached from the execution's cancellation signal so that known usage can still be delivered after cancellation, but it cannot forcibly stop host code that ignores the context. Enabling the cumulative hook queries the ledger on every notification, which has a real cost; without it configured there is no such extra query. See the [design](../design/TOKEN_USAGE_HOOK.md) for the detailed contract.

General observation can still use Observer:

Implement `observe.Observer` and inject it through `observe.WithObserver(ctx, sink)` or execution.Scope. Descendants inherit the sink; do not register the same accumulator again at each level. Parent-level control hooks such as PreStep/RequestError remain independent of the child level. Inside these hooks or the tool Pipeline, usage.FromContext(ctx) reads the current call identity; identity labels do not replace authorization.

```go
func (s *Sink) Observe(ctx context.Context, e observe.Event) {
    if e.Type != "model_usage" || e.Usage == nil { return }
    u := *e.Usage // A copy; do not bill based on how many times it is delivered
    // u.AgentID / AgentCallKey / ParentAgentCallKey: attribution
    // u.AttemptKey / Attempt / Revision: attempt and evidence deduplication
    // u.Tokens / KnownFields: reported values and whether each field is known
    // u.Replayed / AccountingCommitted: replay and durable recording status
    // Project it into the host's display/metrics system; after dropped events, repair with a ledger query.
}
```

The new `model_attempt_start` is emitted after budget admission and before the client is called; it does not prove the request reached the provider, and it is not emitted when admission is rejected. `model_usage` covers success, failure, cancellation, and replay back-filling. The existing model_start/end still describe the logical model operation and must not be added on top of the per-attempt events.

`Event.Agent` contains the agent's Owner and the current VisitID/ParentVisitID; `Event.VisitID` keeps the visit identity of the observed operation. The two serve different purposes. Call recovery keeps the AgentCallKey and generates a new agent VisitID. Metadata does not include conversation content or raw provider errors.

A synchronous Observer must return promptly, and panics are isolated; AsyncObserver is bounded and may drop events. Failed notifications do not change business success, and exactly-once delivery is not provided. Reliable totals must query the budget ledger; do not use values accumulated from hooks as the final bill.

## Self, Inclusive, and Root

```go
reportCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
defer cancel()
report, err := controller.Attribution(reportCtx)
// report.Root: root total
// report.Calls: per logical agent call, Owner + Self + Inclusive
// report.Agents: Self totals per AgentID
// report.Unattributed: direct budget calls without agent context
```

You can also query after a failure or an approval pause. If Wait itself times out, execution may still be converging, and the query is only a point-in-time snapshot. A child agent finishing does not mean the outer OwnedRunnable.Close has completed; the final states of tools and Handles follow their own lifecycles.

With parent analysis 3, child A 4, child B 5, and parent summary 6, the parent Self=9, Inclusive=18, and the root is 18. Do not add up parent and child outputs, Inclusive values, or consecutive root snapshots. With parallel execution you also cannot derive a child agent's usage from the difference between two root snapshots.

The existing AgentOutput.TokenUsage keeps the logical-call scope of its own level; on replay it can still include historical usage, and it does not represent new consumption in this run. The root ledger deduplicates by unique model attempt key; a real retry takes a new attempt key.

## Unknown, Cached, and Partial Reports

`llm.Usage` adds CachedTokens and optional KnownFields; `Tokens()` reads the values and `Fields()` returns the field bitmap. A nil mask keeps the old three-field semantics; an explicit mask can mark reported zero values. Cached tokens are generally a subset of input and are not added to TotalTokens.

```go
fields := usage.Total | usage.Cached
u := &llm.Usage{TotalTokens: 19, CachedTokens: 4, KnownFields: &fields}
```

The unified ledger keeps field evidence and allows unknown fields to be filled in later; identical evidence is idempotent, and a change to a known field is an error. Across stream frames, only fields that become known later are updated; cumulative frames are not added up, and missing fields do not overwrite evidence from earlier frames. Replay notifications do not change the original source or increase the evidence Revision.

Reports provide UnknownUsageEntries, PartialUsageEntries, and, in each summary, UnknownPromptEntries/UnknownCompletionEntries/UnknownTotalEntries/UnknownCachedEntries. Complete only means the three basic fields are complete; it does not promise that cache usage is known, that the provider bill is complete, or that hidden external calls were all captured. When Total is unknown, KnownTokenDelta is not computed, and estimates are not treated as actual savings.

All budgets keep partial fields by default; there is no longer a mode that downgrades to the old unknown format. Successfully committed model_usage notifications set AccountingCommitted=true. Reserve/ReserveEstimated/RecordUsage are convenience entry points over the same evidence implementation; use RecordEvidence when you need a field mask. Direct calls without agent context are recorded explicitly as Unattributed, with no guessed attribution.

The current OpenAI/DeepSeek standard streams and Gemini pass through the cache and field information available from the SDK. When the SDK decodes both a missing value and an explicit zero as a scalar, the field is conservatively marked unknown; the part of DeepSeek's private cache fields not exposed by the SDK is not fabricated. Currency pricing, effective dates, and bill reconciliation are provided by the host.

## Recovery and Migration

There is only one ledger structure and one format marker, goagent-model-budget, with no format selection switch. Config.Version is only the host's budget policy fingerprint and does not select an implementation version. The old format is explicitly rejected on read and is not migrated automatically; existing durable runs must be handled explicitly by the original implementation or the host, and existing business must not be re-run under a different execution key.

If execution is interrupted after the model result is saved but before usage is recorded, recovery reads the saved result and back-fills the record without sending the request again. An interruption after the ledger commit but before notification may lose an event, but queries remain correct. The Owner/ancestor chain is validated on reservation, evidence recording, and replay; changing the AgentID or parent relationship is rejected. When the same tool is replayed as a whole, the child Builder is not re-entered and the historical attribution is used; changing the child configuration in a closure requires bumping the tool Version.

Ledger transactions keep the existing locking, cloning, and full validation; enabling attribution adds CPU/memory overhead, and querying and updating large ledgers still has linear cost. Hosts should limit the number of requests and call nodes according to business runs.

## Offline Runs and Evidence

All example code has moved to kart-io/goagent-examples, and the commands below are run from that repository's root (the current new API requires `make work` first to develop against a local GoAgent). For complete examples covering two business accounting scopes, see [Token accounting examples](https://github.com/kart-io/goagent-examples/tree/main/agent-usage):

```sh
# Full task: parent agent + two parallel child agents + a regular tool, total 18
go run ./agent-usage/task
# Single agent: writer total 10
go run ./agent-usage/agent -mode single
# Child agent: writer is 10, task total including parent analysis and summary is 17
go run ./agent-usage/agent -mode child
```

The examples also demonstrate the per-attempt hook, role Self, call Self/Inclusive, and the Root query after completion, without needing model credentials. Model usage values are preset; replace the model client for real integrations.

```sh
go run ./agent-usage
```

The example includes two parallel child agents, one failed retry, and a durable replay. Both outputs show Root=18, parent Self=9, parent Inclusive=18; there are 5 requests, one of which lacks usage, so unknown=1. The example creates and cleans up its own temporary directory and needs no model credentials.

The example receives all descendant notifications through the parent Builder.WithUsageHook and prints hook: live=5 replay=2; the number of notifications is not the same as new cost, and the root total after replay is still 18.

See the [design](../design/MULTI_AGENT_HOOKS_AND_USAGE.md) for the implementation basis and the delivery report for verification.

## Reasoning Breakdown, Actual Model, and Configuration

```go
agent, err := builder.NewSimpleBuilder(client).
    WithReasoning(usage.ReasoningConfig{Effort: "low"}). // OpenAI-compatible/DeepSeek; the model must support it
    WithUsageHook(func(ctx context.Context, e budget.UsageEvent) {
        fmt.Println(e.Model.Model, e.Model.ResponseModel) // Requested name and actual name from the response
        if e.Model.KnownFields&usage.Reasoning != 0 {
            fmt.Println("已知推理 Token", e.Model.Tokens.ReasoningTokens)
        }
        if e.Model.KnownFields&usage.ToolUsePrompt != 0 {
            fmt.Println("工具相关输入", e.Model.Tokens.ToolUsePromptTokens)
        }
        // ReasoningConfigured/Reasoning record the explicitly requested settings and do not infer upstream defaults.
    }).Build()
```

Gemini 2.5 can request a per-call thinking budget with `usage.ReasoningConfig{BudgetSet: true, BudgetTokens: 128}`; when the model supports a zero budget, an explicit 0 can still be distinguished from unset. Client defaults are set with `llm.WithReasoning(config)`. Providers without an implementation reject it explicitly; a custom client accepts the Builder's explicit setting only after implementing `llm.ReasoningRequestValidator`.

The reasoning and tool-input breakdowns must not be added to the provider's TotalTokens again. Fields that are not reported remain unknown and are not derived by subtracting from the total. UsageSummary provides `UnknownReasoningEntries` and `UnknownToolUsePromptEntries`; Complete for basic usage does not mean all optional breakdowns are known. When the actual response model is not provided, ResponseModel is empty.

The [design contract](../design/TOKEN_USAGE_EXPLAINABILITY.md) records the fields, configuration, and replay rules.
