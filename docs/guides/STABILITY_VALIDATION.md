# Stabilization Validation and Release Order

[English](STABILITY_VALIDATION.md) | [中文](STABILITY_VALIDATION_CN.md)

## Local Gates

Using the Go toolchain declared in go.mod, run on the current source the repo-wide `go test -race -count=1 ./...`, the coverage gate, `go build`, `go vet`, lint, import layering, and the standalone frameworkbench contract tests. Check skipped items separately, and do not substitute a green result after automatic retries for the first-run result.

## Offline Soak

```sh
go run ./scripts/stabilitysoak -duration 24h -workers 4 -interval 1s -sample 1m
```

Each cycle, through public interfaces, completes a governed tool loop, a file session log, a replay after rebuild with zero model requests/zero tool effects, durable graph approval and completion replay, and a zero-effect check for a canceled tool call. The model is a synthetic stream with no network; each cycle should make exactly two model requests, one tool effect, and one graph node effect.

This is a lifecycle load in which the same process repeatedly creates, executes, rebuilds, and cleans up. Temporary state is reclaimed every cycle to keep storage bounded; it does not validate continuous growth of long session history, real model quality, Postgres, remote services, or production business capacity. Long-session scale is validated separately with the existing session benchmarks, and production capacity must be tested separately with real business workloads and storage backends.

By default each cycle has a 5-second deadline and sampling happens every minute; heap memory 256MiB, 128 goroutines, and 16MiB of temporary storage are the sampling gates, adjustable with `-max-heap`, `-max-goroutines`, and `-max-disk`. These are sampled peaks, not continuous measurements or operating-system hard quotas. P95/P99 are histogram upper bounds of cumulative mixed-cycle durations, including cleanup, and the last bucket uses the actual maximum. Any execution error, cancellation, gate violation, or prolonged sampling interruption counts as a failure.

Temporary state is deleted automatically at runtime, and no report file is generated. When you need to check across sessions, first build the binary from clean, committed source, then start it:

```sh
go build -o /tmp/goagent-stabilitysoak ./scripts/stabilitysoak
/tmp/goagent-stabilitysoak -duration 24h -status-address 127.0.0.1:18763
curl --fail http://127.0.0.1:18763/status
```

The endpoint listens only on the loopback address, and results stay in process memory. Only `Phase=passed`, `Result.Elapsed >= Target`, a non-zero number of valid cycles, and a `Revision` that exactly matches the commit to be released together mean that the source completed the declared duration; `running`, `failed`, `unknown`, or `-dirty` cannot serve as evidence for a stable release. After checking, shut down the process with `curl -X POST http://127.0.0.1:18763/stop`; stopping early is treated as a cancellation failure. Keep the computer from sleeping during the run; suspended time does not count as sustained load.

## MiniMax Regression

Use the dedicated credentials and entry point from the [MiniMax Anthropic guide](MINIMAX_ANTHROPIC.md#bounded-periodic-regression). Each run makes at most three requests, with no retries on network errors; do not expand the small weekly real regression into 24 hours of continuous paid calls. The GitHub workflow runs only when the environment Secret is configured and it is explicitly enabled; without sufficient permissions, the host can schedule the same entry point locally, with credentials still read from the local machine.

## Version Alignment

Merging into the main branch is not the same as releasing. Create the core release tag only after the target commit passes the local gates and the soak; this round includes rejecting at build time configurations that were previously ignored silently, so it should be a minor bump in the 0.x stage rather than just a patch. Recheck the remote versions before releasing, and never overwrite an existing tag.

Contrib uses independent module tags, released in actual dependency order:

1. Once the core release tag is downloadable, update the core version in each migrated Contrib module and run per-module `GOWORK=off go test -mod=readonly -race ./...`.
2. Release a new version of retrieval.
3. Pin document, cohere, qdrant, and vertexai to the released retrieval version, run their tests independently, then release them; database and practical have no in-repo upstream and can be released independently.
4. Pin PDF to the released document and retrieval versions, verify it independently, then release it.
5. Use a brand-new temporary consumer module without any local replace to download and build the full set of released versions; delete the temporary module when done.

Do not commit dependencies that point to tags not yet released, and do not substitute workspace integration results for independent post-release consumer verification. If the soak is still running at release time, keep the unreleased state, and do not mark a short pre-check as a 24-hour pass.

The table below is the historical record of candidate versions for the v0.50.0 round (goagent `v0.50.0` has been released; the current version is determined by the remote tags) and does not represent the version currently pending release; downstream dependencies are changed only after the corresponding upstream tag is actually released; check again at release time whether the remote already has newer versions.

| Module | Candidate tag | In-repo dependencies |
|---|---|---|
| goagent | `v0.50.0` | None |
| retrieval | `retrieval/v0.4.1` | None |
| document | `document/v0.3.1` | retrieval `v0.4.1` |
| retrieval/cohere | `retrieval/cohere/v0.4.1` | retrieval `v0.4.1` |
| retrieval/qdrant | `retrieval/qdrant/v0.4.1` | retrieval `v0.4.1` |
| retrieval/vertexai | `retrieval/vertexai/v0.4.1` | retrieval `v0.4.1` |
| tools/database | `tools/database/v0.3.1` | None |
| tools/practical | `tools/practical/v0.3.1` | None |
| document/pdf | `document/pdf/v0.3.1` | document `v0.3.1`, retrieval `v0.4.1` |

The core dependency of the modules migrated in this round is aligned with the existing new core tag at release. ACP, HTTP Agent, and pgstore had no codec migration this time, so their minimum dependency is not raised just to keep version numbers aligned; consumers must still explicitly pin the core version they verified.
