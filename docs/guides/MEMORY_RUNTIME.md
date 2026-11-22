# Memory runtime and recovery

[简体中文](MEMORY_RUNTIME_CN.md) · [Memory API](../../memory/README.md)

Long-term memory supplies revocable context. Business inputs, accepted artifacts, approvals, effect receipts and execution budgets belong to the host's durable execution state. Forgetting memory does not erase those records or authorize a write to run again. Hosts that retain personal data in business artifacts must apply a separate retention and deletion policy there.

## Context selection

`memory.Config.ContextLayout == nil` preserves the previous selection and `Identity()` value. To opt into a pinned-record quota, pass `&memory.ContextLayout{CoreRunes: n}`. `CoreRunes` counts full pinned lines and separators inside the existing `RecallRunes` total. Zero excludes pinned records. Pinned records that do not fit cannot return through ordinary ranking; unused pinned space is available to other recalled records. This is a character quota, not a token or billing estimate.

`Explain(ctx, query)` uses the same selection and reports references and allocation without record bodies. `Snapshot` persists references; `Hydrate` filters those references against the current namespace, scope and content revisions. Hydration cannot select replacement memories. Bind namespaces from trusted tenant/user identities.

Builder now supports automatic recall for agents with no tools. New turns commit `plain-memory/1` or `plain-none/1`. Old turns without `RecallMode` keep their previous model intent and do not gain new memory operations during replay. Completed requests replay without contacting the provider. Before a new physical request, including a recoverable same-turn retry, the builder hydrates the same references again so forgotten content is omitted. An already submitted provider request cannot be recalled.

New plain automatic recall cannot be combined with `RequestHook`. For native tool agents, the host must not copy or modify the automatic recall block in a hook: such changes can break durable intent validation. A new protocol turn with existing model/use records but missing recall records fails recovery instead of silently changing the request.

## Maintenance admission

`Config.Background == nil` retains previous maintenance behavior. Production hosts that need bounded paid maintenance should configure `BackgroundConfig` with:

- A durable `budget.Controller` using a stable window `RootKey`, positive `MaxModelRequests` and `MaxReservedTokens`, and the intended window deadline.
- Positive `MaxInputRunes`, `MaxOutputRunes`, `MaxOutputTokens` and `MaxDerivationsPerPass`.
- Conservative `EstimateChat` and `EstimateEmbedding` functions. `EstimatorVersion` must equal the budget's `TokenPolicyVersion`.
- `SingleRequestClients: true`, a host assertion that the underlying model and embedding clients neither retry nor split one call into several physical requests. Disable such behavior in provider wrappers before making this assertion.

Every physical maintenance request, including malformed-JSON repair and embedding, needs its own admission and evidence. The persisted namespace/scope request sequence prevents key reuse after restart. Unknown usage remains unknown. Maintenance strips foreground journal, lineage and budget bindings while preserving cancellation and deadlines. Online `Recall`/`Remember` embeddings need their own host admission; they cannot borrow this background window.

Community and reflection work records persist dirty/done progress, technical attempts, retry time, pause state and the blocked budget window. Each pass starts at most `MaxDerivationsPerPass` derivations per namespace/category. Technical failures use bounded backoff and pause after five attempts. Oversized input pauses before a request. A new budget window releases budget blocking only: it does not reset technical failures. Reusing the same window after restart retains its spent quota.

After repairing a technical problem, a host may explicitly call `MarkDerivationsDirty` in the affected namespace. This registers both community and reflection work and clears their technical pause/attempt state. It preserves the budget blocking key and physical-request sequence. Ordinary input changes do not silently reset a paused derivation. Deletion and index cleanup remain eligible when model quota is exhausted; failed index updates remain queued for repair.

## Storage upgrade and rollback

The memory state writer now uses `/4`. It reads real `/2` and `/3` state and preserves derivation progress through cloning, incremental commits, compaction and scope operations. Old readers reject `/4`; mixed old/new writers are unsupported.

1. Stop agents and maintenance writers sharing the store. Back up the complete durable store and external index, and retain the deletion log for the rollback window.
2. Deploy the new reader/writer to every participating process. Resume existing execution with the same graph, member manifests, approval keys and budget roots.
3. Exercise recall and deletion, then run maintenance to reconcile derived indexes. Do not clear pending work or attention records to make an upgrade appear successful.
4. To roll back data, restore the compatible backup and replay deletions since that backup. Replacing only the binary cannot downgrade `/4`; restoring a backup without deletion replay can resurrect forgotten content.

Changing layout changes new memory identity. Do not change it inside an in-flight durable run. Background controller configuration is runtime admission policy and does not enter memory identity. Namespace/scope validation still applies to every restored snapshot.

## Verification and evaluation

The Harness planning example demonstrates local source processing and durable waits. Integration coverage includes actual nonempty recall, forgetting between requests, separate online/background budgets, unknown write effects and real process restarts. These checks establish execution and recovery contracts. Model answer quality, parallel latency and layout savings require separate live trials; offline results do not establish those benefits.

For a layout comparison, copy one already formed memory snapshot into isolated stores for nil-layout and quota-layout runs. Use the same context cap and raw history for the no-memory baseline. Charge formation, foreground answering and an independent judge separately, count every retry at the physical boundary, and retain failed and unstarted trials in the preregistered denominator. References support traceability; their existence alone does not prove a conclusion.
