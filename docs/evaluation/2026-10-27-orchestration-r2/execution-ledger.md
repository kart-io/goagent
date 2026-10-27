# SDD ledger — plan: docs/superpowers/plans/2026-10-27-composable-orchestration.md

Execution branch: codex/composable-orchestration-r2. Workspace: /Users/costalong/.codex/worktrees/4bd1/goagent.
Baseline: 3bfeea7; documentation/benchmark inputs committed separately as b60e033. Root source checkout left unchanged.

Pre-flight: T1 produces Controller consumed by T2/T4; persist-mode checks and config identity required. T3/T4 agree on owned/fixed resource lifecycle. T5 OTel API lives in core/observe, bridge in observability, no exporter in execution packages. T6/T7 use immutable store and deep message validation. T8 wraps existing final-state contract. T9/T10 consume public APIs only.
Ruling: use the managed worktree and a local feature branch — preserves uncommitted source checkout documents — review must use this worktree's paths.
Ruling: root request controller also exposes read-only configuration for persistent intent identity — otherwise same operation could replay under a different root controller — risk is a small additive public API.

Task status is recorded below; T10 includes end-to-end child MemoryManager isolation and real process recovery.
Task 1: complete (commits b60e033..cf6aeba, tests: env -u GOROOT go test -race ./core/budget ./core/durable → ok  	github.com/kart-io/goagent/core/durable	(cached))
Task 2: complete (commits cf6aeba..d6837cb, tests: env -u GOROOT go test -race ./builder ./core/budget ./core/durable → ok  	github.com/kart-io/goagent/core/durable	(cached))
Task 3: complete (commits d6837cb..1414180, tests: env -u GOROOT go test -race ./tools ./workflow → ok  	github.com/kart-io/goagent/workflow	(cached))
Task 4: complete (commits 1414180..bc0d153, tests: env -u GOROOT go test -race ./tools ./builder ./workflow ./core/budget → ok  	github.com/kart-io/goagent/core/budget	(cached))
Task 5: complete (commits bc0d153..9b910b6, tests: env -u GOROOT go test -race ./core/observe ./observability ./workflow ./builder ./tools ./devtools → ok  	github.com/kart-io/goagent/devtools	(cached))

Ruling: implement the durable immutable store primitive in T6 together with memory-store contract tests; T7 owns Builder integration and process recovery — both stores must obey the same identity semantics.
Task 6: complete (commits 9b910b6..e951ec0, tests: env -u GOROOT go test -race ./builder/... → ok  	github.com/kart-io/goagent/builder/contextpolicy	(cached))
Task 7: complete (commits e951ec0..770c3b9, tests: env -u GOROOT go test -race ./builder/... ./core/durable ./core/budget ./examples/orchestration/context-recovery → ok  	github.com/kart-io/goagent/examples/orchestration/context-recovery	1.857s)

Ruling: Source/Map/Fold remains experimental. Barrier and late-failure tests validate incremental previews plus final-only commit. No four-paradigm API adopted; package benchmark recorded in t8-benchmark.log.
Task 8: complete (commits 770c3b9..f39e7c2, tests: env -u GOROOT go test -race ./core/flow ./core/... ./workflow → ok  	github.com/kart-io/goagent/workflow	(cached))

T9 visual verification: Chrome preview on 127.0.0.1:8876; topology loop/branch, yellow waiting, red error, green replay and drop summary render correctly. Escape tests green.
Task 9: complete (commits f39e7c2..c84de3f, tests: env -u GOROOT go test -race ./devtools ./workflow ./examples/orchestration/... → ?   	github.com/kart-io/goagent/examples/orchestration/stream-pipeline	[no test files])

T10: Go 1.25 race: 75 test packages + 132 packages without tests, all green. Build, golangci-lint 2.6.2 and imports exited 0. Graph panic, nonjournal stable host key, final-turn ordinary policies and disabled-observer allocations each reproduced RED then GREEN. Same-machine five-repeat benchmarks retained initial regression and optimized results.
Task 10: complete (commits c84de3f..72f67f0, tests: env -u GOROOT GOTOOLCHAIN=go1.25.0 GO_TEST_MODE=true go test -race -timeout 10m ./... → ok  	github.com/kart-io/goagent/workflow/refund	(cached))

Final review: independent fresh-context whole-branch reviewer, b60e033..72f67f0; 0 Critical, 4 Important, 0 Minor. Findings documented in docs/evaluation/2026-10-27-orchestration-r2/FINAL_REVIEW.md. One RED→GREEN fix pass; no second review dispatched.
Final: Ruling: hidden SDK requests/retries and arbitrary custom model calls remain host-controlled — budget covers only the unified entry, and bound legacy entries now fail closed — cost if wrong: hidden requests can escape accounting.
Final: Ruling: unique child sessions and matching store/root ownership remain host responsibilities — generic interfaces cannot prove physical backend identity, while example tests validate real session isolation — cost if wrong: memory or run scopes can be mixed.
Final: Ruling: cancellation remains cooperative for callbacks, sources, cleanup and writers — generic Go functions cannot be forcibly stopped safely — cost if wrong: uncooperative functions can block cleanup/Close.
Final: Ruling: nested recovery/approval and exactly-once effects remain deferred — opaque once-attempt wrappers refuse pending/failed reentry — cost if wrong: external effects could repeat if a host relaxes this boundary.
Final: Ruling: real-provider quality and production performance are not inferred from local evidence — microbenchmark costs are disclosed without an unsupported threshold — cost if wrong: extrapolating them can mislead production selection.
Final: fixed bound legacy budget bypass — TestLegacyEntriesRejectBoundRootBudgetBeforeRequest RED→GREEN, suite 75/75 test packages (132 additional packages without tests), Go 1.25 race/build/lint/imports green.
Final: fixed opaque provider byte mutation during cloning — TestIdentityPolicyPreservesOpaqueProviderBytes RED→GREEN, suite 75/75 test packages (132 additional packages without tests), Go 1.25 race/build/lint/imports green.
Final: fixed native-state-bound assistant content mutation — TestPolicyRejectsBoundAssistantContentBeforeAdmission RED→GREEN, suite 75/75 test packages (132 additional packages without tests), Go 1.25 race/build/lint/imports green.
Final: fixed durable observation identity collisions — TestDurableResumesHaveDistinctObservationIdentities RED→GREEN, suite 75/75 test packages (132 additional packages without tests), Go 1.25 race/build/lint/imports green.

Delivery: all T1–T10 complete; four independent-review Important findings fixed in 772925f by RED→GREEN plus full 75/75 race suite; 0 deferred minors. Final benchmark after fixes: graph +1.8%, loop -4.4%, tool +10.4%, stream +4.8%; same-machine five-repeat results disclosed. Feature branch retained in managed worktree for review; no integration action requested. Short-lived SDD workspace may now be removed; tracked execution-ledger and review reports preserve decisions/evidence.
