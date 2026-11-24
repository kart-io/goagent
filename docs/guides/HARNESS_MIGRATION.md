# Migrating to the Standalone Harness Project

[English](HARNESS_MIGRATION.md) | [中文](HARNESS_MIGRATION_CN.md)

GoAgent focuses on execution mechanisms: Runnable, the Builder's native loop, Graph, durable, budget, observe, Dispatcher/Pipeline, Handle, and loop hooks.
The standalone GoAgent Harness project focuses on configurable Agent assembly: Presets, plan mode, compaction, skills, sub-Agent components, Claude Code command hooks, and coding tools.

Generic loading of Skills is now provided by the top-level `skills` package and the optional `tools/skill` in the main repository; Harness is responsible for skill configuration, authorization,
prompts, and Manifest assembly. Ordinary applications can use the [skill catalog and resource reading](SKILLS.md) without bringing in Harness.

## Path Mapping

| Original GoAgent path | New project path |
|---|---|
| `github.com/kart-io/goagent/harness/preset` | `github.com/kart-io/goagent-harness/preset` |
| `github.com/kart-io/goagent/tools/coding` | `github.com/kart-io/goagent-harness/tools/coding` |
| `examples/tools/09-coding-agent` | `goagent-harness/examples/coding-agent` |
| `examples/tools/10-plan-agent` | `goagent-harness/examples/plan-agent` |

The new project is a standalone repository, [kart-io/goagent-harness](https://github.com/kart-io/goagent-harness), with its own Go module, license, documentation, tests, and commit history; the origin of the original implementation is recorded in its NOTICE.
Since v0.27.0, GoAgent no longer contains the packages in the table above. Migration only changes imports; the original config file paths, formats, and assembly APIs are unchanged; no compatibility forwarding is kept for the old packages.
Applications that only use the Builder or workflows do not need to bring in the new project.

## Kept in the Core

`harness/env` keeps the environment capability interfaces and implementations such as LocalFS and Executor; `tools/policy` and `tools/spill` continue to serve any tool.
The new project consumes these public interfaces directly and does not copy the model loop, budget, or logs. `scripts/apicheck` forbids core production code and examples from importing the new module in reverse.

## Local Verification

The Skills public packages were released in GoAgent v0.52.0; when integrating unreleased core changes, verify both checkouts with a temporary workspace.
Run from the GoAgent root directory:

```bash
(
set -euo pipefail
core_dir="$PWD"
harness_dir="$(cd ../goagent-harness && pwd)"
workspace_dir="$(mktemp -d)"
trap 'rm -rf "$workspace_dir"' EXIT
(cd "$workspace_dir" && env -u GOROOT GOWORK=off go work init "$core_dir" "$harness_dir")
env -u GOROOT GOWORK="$workspace_dir/go.work" go -C "$harness_dir" test -race ./...
env -u GOROOT GOWORK="$workspace_dir/go.work" go -C "$harness_dir" build ./...
)
```

When releasing, after updating Harness's GoAgent version dependency, run the script that includes the standalone module gates:

```bash
# Run in the goagent-harness clone; the argument is the path to this repository (default ../goagent).
./scripts/verify-goagent.sh ../goagent
```

The script first verifies the released dependency with `GOWORK=off`, then temporarily creates a workspace to test the two local modules, without writing a local replace or modifying an existing go.work.
The new project's `integration` tests verify, through public APIs, Preset assembly, tool approval, durable graph waiting, approval resume after rebuilding the host, and replay after completion; the migrated original tests cover skills, sub-Agents, plan mode, compaction, and interaction.

## Versions

GoAgent Harness's version dependency is defined by its `go.mod`; a local workspace does not pass new public package dependencies on to downstream.
Before an independent release, you must select a fetchable GoAgent version that contains `skills` and `tools/skill`, and pass the `GOWORK=off` check.

Historical design documents in the new project keep their origin; the Harness historical designs in this repository no longer represent where the upper-layer components are maintained.
