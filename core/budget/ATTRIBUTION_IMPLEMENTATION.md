# Unified budget evidence validation

Date: 2026-11-01. Scope: `core/budget`.

Budget now has one implementation and one storage marker,
`goagent-model-budget`. Config.Version remains the host policy fingerprint;
AttributionVersion, AttributionEnabled, and format selection were removed.
Previous `goagent-model-budget-v1` and `goagent-model-budget-v2` records are
rejected without rewriting or migrating them. MaxAgentCalls defaults to 1000
when zero, accepts positive host limits, and rejects negatives.

The single ledger stores Config, Entries, AgentCalls, and Tools. Each Entries
value contains Estimate, Evidence, and AgentCallKey. This removes independent
estimates, evidence, and owner maps and their consistency branches. Map values
contain only copied scalar values. Reservations and topology changes share the
same Store lock and Save operation.

Reserve and ReserveEstimated forward context lineage to ReserveAttributed;
RecordUsage forwards context lineage to RecordEvidence. Empty lineage is allowed
for reservation/evidence and produces explicit Unattributed summaries. It cannot
change ownership of an existing attempt. RegisterAgent requires a nonempty,
complete root-to-leaf chain. RecordUsage declares BasicFields, plus Cached when
positive; nil remains unknown. RecordEvidence always uses its explicit mask and
monotonic evidence revisions, including when called without an owner.

Snapshot and Attribution.Root share rootSnapshot. KnownTokenDelta includes an
attempt only when its Total field is reported. Prompt-only usage against a
100-token estimate leaves the known delta unchanged; later Total=25 contributes
-75. This behavior has one implementation for every caller.

Attribution returns sorted, independent call and role summaries. Each summary
retains field-level missing evidence counts. Complete requires attribution and
all BasicFields for every request; cache remains optional for Complete but its
unknown count is explicit. Cached tokens are never added to TotalTokens.

Validation (Go 1.25.0, unset GOROOT):

- `go test ./core/budget` — passed.
- `go test -race ./core/budget ./core/usage` — passed.
- Ledger benchmarks — passed.

Raw logs:

- `/tmp/goagent-budget-unified-red.log`: default configuration regression RED.
- `/tmp/goagent-budget-unified-context-red.log`: context-lineage forwarding RED.
- `/tmp/goagent-budget-unified-green.log`: complete budget tests GREEN.
- `/tmp/goagent-budget-unified-race.log`: related package race tests.
- `/tmp/goagent-budget-unified-benchmark.log`: fixed-ledger benchmark output.

Tests cover default limits and evidence, both attributed and unowned partial
usage, convenience API lineage, immutable attempt ownership, rejection of both old
storage markers, policy mismatch, root=18/parent self=9/children=4+5, zero-request
ancestors, field upgrades, query copy isolation, concurrent admission, FileStore
reopen, failed Save atomicity, token overflow, and subprocess exits after
reservation or evidence commit.

Apple M4 Pro, darwin/arm64, 100 ms sample:

| Path | Entries | ns/op | B/op | allocs/op |
| --- | ---: | ---: | ---: | ---: |
| RecordUsage | 10 | 639 | 1992 | 5 |
| RecordUsage | 100 | 3403 | 13768 | 5 |
| RecordUsage | 1000 | 50727 | 229616 | 7 |
| RecordEvidence | 10 | 879 | 2744 | 7 |
| RecordEvidence | 1000 | 51960 | 230371 | 9 |
| RecordEvidence | 10000 | 522253 | 1836583 | 37 |

The existing transaction still clones and validates the ledger, so transaction
cost remains linear in ledger size. The value-entry structure avoids per-entry
pointer copies and separate evidence/owner map copies. Benchmark samples are not
performance thresholds.

The budget package does not own model result journals or observer delivery.
Builder integration must additionally verify saved-result recovery without
request resends. Aggregate overflow is rejected by queries; individual valid
nonnegative evidence can exceed the representable aggregate.
