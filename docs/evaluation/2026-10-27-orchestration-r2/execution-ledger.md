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
