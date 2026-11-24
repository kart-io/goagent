# Runtime Capabilities and Follow-up Acceptance

[English](RUNTIME_READINESS.md) | [中文](RUNTIME_READINESS_CN.md)

Checked on: 2026-11-14. Implementation baseline: `54c308f` on master (v0.53.0). This document records the current scope of support; the latest results of the acceptance commands are kept separately in the execution report, and historical evaluations do not substitute for this verification.

## Current Scope of Support

| Capability | Current implementation | What the host is responsible for |
|---|---|---|
| Execution and recovery | Builder native tool loop, Graph, operation journal, session log, approval, and root budget | Stable execution keys, versions, budget storage, tool authorization, and resource lifecycle |
| External side effects | Completed operations are replayed; interrupted operations can be checked with `Reconcile` | Non-idempotent tools provide business deduplication or a reconciliation policy; the Dispatcher allows rerunning interrupted tools by default, so the journal alone cannot guarantee the external system does not apply an effect twice |
| Long-term memory | Records, temporal graph, community summaries, deletion with derivation cascade, rollback, asynchronous extraction | Trusted namespaces, session log, durable.Store, and maintenance tasks |
| Automatic recall | Only in the native tool flow; recalled text does not enter session history or the durable journal | Plain chat with no registered tools does not recall automatically |
| Management service | Agent/versions, Session/Fork, Task, Approval, Worker, Outbox, and queries | Inject storage, authentication and authorization, and the execution factory; start the Worker explicitly |
| Management Worker | A single active Worker that can run different sessions concurrently | No multi-instance sharding or automatic takeover on heartbeat timeout; confirm the old process has exited before reclaiming the owner |
| Memory Worker | Mutual exclusion via the `memory/worker` lock on the same storage | Extraction parallelism cannot be increased by adding Worker instances on the same storage; monitor backlog and processing time |
| Memory management extensions | Paginated listing and shared namespaces are still designs | Do not currently call the designed `Memory.List` or `WithSharedNamespaces`; see [MEMORY_ADMIN](../design/MEMORY_ADMIN.md) |

The execution gate of `ConfigurableAgent` protects the whole session round, and a single instance executes serially. The host creates instances per session; cross-session concurrency must be verified together with shared resource limits, and throughput cannot be inferred from the existence of `Batch` or the multi-Agent APIs.

The `Durable` of long-term memory must also implement `durable.Scoped`. Deployments that do not run the extraction Worker (`Run`/`ProcessPending`) still need to run `RunMaintenance` or call `Maintain` periodically to handle forgetting, Purge, and cleanup of late vectors. The current default state limit per namespace is 8 MiB; deletion operations are not blocked by this limit.

## Memory Upgrade and Rollback

G0/G1/G2 have been merged. The design documents keep each stage's original plan and measurements; the current public interfaces and the migration entries in the [CHANGELOG](../../CHANGELOG.md) take precedence.

Migrating from the old `store.Store` record store to G1/G2:

1. Stop Agent traffic and Workers, finish in-progress durable runs (including runs waiting for approval), and back up the old records and indexes.
2. Switch to `Config.Durable`; a custom `Index` implements `IDs` and `Clear`. If data rollback is needed, set `RollbackWindow` in advance and keep the deletion log.
3. Use `MigrateFromStore` and check the report for each namespace. Old relations, entities, communities, and tasks are not migrated; record sources change from the old single field to `Sources`, and old-format sources are not filled in automatically.
4. After cleanup is complete, restore traffic and start the Worker or maintenance task. For the full steps and conflict recovery, see the [G1 storage design](../design/MEMORY_G1_STORAGE.md).

Upgrading from G1 to G2:

1. Stop the Worker and back up state; after upgrading, run `Reindex(true)`, then start the Worker.
2. The new program can read G1's `/2` and writes `memory-state-blob/3` and `memory-head/3`. The old program refuses to read `/3`, so a code rollback must be paired with restoring the pre-upgrade data backup.
3. G1 relations lack derivation evidence and entities lack name sources, so they are no longer used directly; the graph is rebuilt from new conversations. Community summaries are rewritten on the first sync, which makes model calls.
4. After a data rollback, replay Forget/Purge within the rollback window from the deletion log, then rebuild the index; without the deletion log enabled, a data rollback may bring back content that was later forgotten.

Recovery must still bind to the namespace of the original round; a change returns `ErrMemoryNamespaceChanged`. Request hooks must not rewrite or move recall messages; changing the request intent during recovery causes a conflict. For more constraints, see [Deletion governance](../design/MEMORY_GOVERNANCE.md).

## Next Round of Memory Effectiveness Acceptance

The existing [G2 LOCOMO evaluation](../evaluation/2026-11-13-memory-g2/README.md) had graph accuracy of 50.5% and 51.0% across two runs, not significantly different from G1. The increase in evidence session recall is affected by the change to the source-union measurement, and cannot on its own be taken as evidence of improved effectiveness. The existing [G1 temporal question diagnosis](../evaluation/2026-11-13-memory-g1/README.md) found extraction omissions, relative time handling, and ranking problems; follow-up work should verify each stage first.

The next round of experiments proceeds in the following order, with the result of each step deciding the change in the next:

1. Fix question IDs, model and prompts, extraction input, embedding model, clock, RecallK, and context budget, and keep per-question output and the code version. Use an independent held-out question set to avoid tuning only against known wrong answers.
2. Classify errors as extraction, candidate retrieval, ranking, context truncation, answering, or judging; for temporal questions, check the original event time against the update time of the merged record, and for multi-hop questions, check each required piece of evidence.
3. Under the same context budget, compare the current approach, time-aware ranking, and RecallK adjustment separately, changing one variable at a time; perform paired statistics on the same questions, and report per-category accuracy, evidence precision/recall, context length, and abstentions.
4. Report request counts, tokens, latency p50/p95, failure rate, and actual billing for extraction and answering; monetary cost must use actual bills or the prices at the time, not character counts.
5. Accept functional effectiveness and runtime performance separately: re-measure cold/warm recall, per-namespace writes, cache eviction, and Worker backlog on the real backend. The existing 5k-record measurement is limited by hardware, data, and submission rate; cold recall is reported separately and cannot be extrapolated directly to production.

Business accuracy, latency, and cost limits must be defined by the specific scenario; currently these do not trigger automatic algorithm switching or paid evaluations.

## Preconditions for Multi-Instance Capability

First measure the target load of a single active Worker: concurrent sessions, rounds per session, tool duration, memory extraction volume, task backlog, recovery time, and storage lock wait. Design multi-instance scheduling only when a single instance reaches a resource bottleneck or the business explicitly requires automatic failover.

Deliverables for that phase need to be defined together: shard ownership, owner/generation isolation, lease expiry, proof that the old executor has stopped, reconciliation of external side effects, routing of approvals and cancellation, recovery targets, and the upgrade path for old-version data. Verify with process termination, network isolation, and late writes from old holders; simply adding Workers is not enough.

The current cross-repository database acceptance entry point is `make compatibility-postgres`; it requires a dedicated database and containers that may be restarted, and requires the key scenarios to actually pass; skips must not count as acceptance.

## Local Verification Entry Points

```bash
make test
make coverage
make test-frameworkbench
go build ./...
go vet ./...
bash scripts/verify_imports.sh
```

`make test` and `make coverage` each run the full main-module tests; for full acceptance, run them one after the other or choose the set with coverage, and avoid running them at the same time. By default, tests run package by package, without caching, with race detection, and with a 10-minute package timeout; for variables and scope, see the [testing guide](../development/TESTING_BEST_PRACTICES.md). The standalone frameworkbench is not included in the root module's `go test ./...` and must be run separately.
