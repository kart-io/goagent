# Explicit Migration of Historical Operation Ledgers

[English](README.md) | [中文](README_CN.md)

Online `durable.Do/InspectOperation` accepts only `goagent-operation-v2`. This package is a one-off conversion tool that the host calls explicitly. The executor never calls it automatically; it runs no business callbacks, does not clear logs, and does not change JSON encoding rules.

Migration steps:

1. Stop and coordinate all running writers, and back up the original log. Keep the original Store's locking, atomic writes and durability guarantees; scoped runs must use the original Store/Scope view.
2. Build `[]migrate.OperationRef{{Key: durable.ChildKey(parentKey, operationID), ID: operationID}}` from the host's durable inventory of operation identities. Key is the full derived key, not an approval key or a run ID; this API does not guess or automatically enumerate targets.
3. Call `OperationsV1(ctx, store, refs, OperationOptions{})` to preview. Check the target identities in `Pending` and `Current`; the preview holds the target locks and loads and validates, but writes no snapshots.
4. After checking, pass `OperationOptions{Apply: true}` explicitly. All targets are locked in storage-identity order, and after all are validated they are committed atomically in a single `Store.SaveAll`, changing only the v1 version field. Current v2 records are validated the same way and are not rewritten.
5. Preview again; every target should appear in `Current`. Enable the new run version only after confirming the target inventory is complete; completed operations replay as usual without re-executing effects.

Run this package's `ExampleOperationsV1` to execute a preview-and-apply example against a temporary FileStore:

```sh
env -u GOROOT GOTOOLCHAIN=go1.27.4 go test ./core/durable/migrate -run '^ExampleOperationsV1$' -v
```

The conversion keeps all other Snapshot fields and passes the Data bytes loaded from the Store unchanged to SaveAll, without re-encoding OperationRecord. Identity, input fingerprint, status, attempt count, limit, result and error classification are validated uniformly by the current `durable.DecodeOperationRecord`; v1 records cannot carry wait or effect-reconciliation semantics. Invalid versions, corrupted identities/payloads, duplicate targets or cancellation are rejected before commit. There is no acceptable "delete the old log and rerun" fallback.

Batch writes, cancellation and unknown commit outcomes follow the Store contract: a failure before commit writes nothing; an unknown commit outcome may mean everything was already written, so preview again to decide, and never roll back or re-execute business logic based on the error. Re-migrating targets that are already v2 only validates and skips them. Callers must provide a backend that satisfies the atomic SaveAll contract; this package does not present a backend without transactions as storage that can be migrated safely.
