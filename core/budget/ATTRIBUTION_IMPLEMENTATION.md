# Optional attribution implementation validation

Date: 2026-11-01. Scope: `core/usage` and `core/budget`.

The usage package supplies owner/invocation values, private context lineage copies,
field masks, and monotonic evidence merging. The budget controller retains its
single ledger, with an explicit v2 snapshot version when AttributionVersion is
nonempty. Config additions use omitempty; v1 config JSON and old ledger records
retain their shape and semantics. Reopening a ledger with a different attribution
policy or ledger version fails rather than migrating it.

Attributed reservation registers the complete owner chain and attempt owner in
the same transaction. RegisterAgent preserves nodes that have no model requests.
RecordEvidence verifies the chain even during replay, merges only new known
fields, preserves the initial evidence source, and reports committed revision.
KnownTokens includes reported fields only; cached tokens are never added to total.
Attribution returns sorted, independent call and role summaries. BasicFields
availability determines partial entries and Complete; cache availability is optional
for Complete. Each summary independently counts UnknownPromptEntries,
UnknownCompletionEntries, UnknownTotalEntries, and UnknownCachedEntries, including
whole unknown requests. Missing cache evidence therefore remains explicit even
when BasicFields are complete. Evidence rejects nonzero tokens outside its mask.
An explicit source may accompany unknown evidence; saved delivery never fills an
absent original source, and an established source remains unchanged on replay.

Validation commands (Go 1.25.0, unset GOROOT):

- `go test -race ./core/usage ./core/budget` — passed.
- `go test ./core/budget -run '^$' -bench 'Benchmark(RecordUsageFixedLedger|AttributionEvidenceFixedLedger)' -benchtime=100ms -count=1` — passed.

Tests cover parent self=9/root=18/children=4+5, zero-request ancestors,
unknown/partial evidence upgrades and conflict rejection, source/revision replay,
field-level missing evidence (including cache), original-source handling,
Owner conflicts, concurrent admission, sorted copy isolation, v1/v2 isolation,
FileStore reopening, failed Save atomicity, aggregate token overflow, and real
subprocess exits immediately after reservation or evidence commit.

Apple M4 Pro, darwin/arm64, sample results:

| Path | Entries | ns/op | B/op | allocs/op |
| --- | ---: | ---: | ---: | ---: |
| v1 RecordUsage | 10 | 539 | 984 | 15 |
| v1 RecordUsage | 100 | 3198 | 6904 | 105 |
| v1 RecordUsage | 1000 | 36139 | 86816 | 1007 |
| v2 RecordEvidence | 10 | 1415 | 3928 | 26 |
| v2 RecordEvidence | 1000 | 96847 | 350005 | 1022 |
| v2 RecordEvidence | 10000 | 1138870 | 2856558 | 10106 |

The existing transaction clones and validates the ledger. v2 adds owner/evidence
maps and their validation to that same mechanism; it does not introduce a second
accounting store. Large ledgers therefore retain linear transaction cost and
additional memory overhead. No performance threshold is asserted from this single
machine sample.

The budget layer has no model result journal or observer delivery. The window
between saving a model result and recording evidence, and actual request resend
counts, must additionally be verified by builder integration tests. The subprocess
tests here demonstrate durable unknown reservation recovery and post-commit
query/replay behavior. Overflow is rejected when summing a report or Snapshot;
individual nonnegative evidence can exceed the representable aggregate.

KnownTokenDelta retains v1 legacy behavior. In v2 it includes an attempt only
when its evidence mask includes Total: a prompt-only result with a reservation
of 100 leaves the known delta unchanged, and later Total=25 establishes -75.
Both Snapshot and Attribution.Root are covered by the regression test.
