# API Stability Policy

[English](API_STABILITY.md) | [中文](API_STABILITY_CN.md)

GoAgent is heading to v1.0. This page states which packages carry a compatibility promise, how breaking changes are handled before and after v1.0, and how the promise is enforced.

## Stable packages

The packages listed in [`scripts/stable-packages.txt`](../scripts/stable-packages.txt) are **stable**. That file is the single source of truth; in short it covers the execution path:

- `builder`, `builder/contextpolicy`
- `core` and its execution subpackages (`durable`, `budget`, `session`, `observe`, `middleware`, `state`, `usage`)
- `workflow`
- `llm`, `llm/common`, `llm/providers`
- `tools` and its governance subpackages (`policy`, `spill`, `toolargs`, `middleware`)
- `interfaces`, `errors`, `execution`, `mcp/remote`

The list matches import paths exactly; a subpackage is stable only if it is listed itself.

## Experimental packages

Every other public package is **experimental**, including `memory`, `management`, `multiagent`, `a2a`, `harness`, `cache`, `codec`, `parsers`, `store`, `skills`, `plugin`, `revision`, `observability`, `middleware`, `utils`, `testing`, `core/flow`, the built-in tool implementations under `tools/` (`compute`, `http`, `search`, `shell`, `skill`) and `workflow/refund`.

Experimental packages may change in any minor release. Breaking changes are still listed with migration steps in [CHANGELOG.md](../CHANGELOG.md).

## Rules for stable packages

**Before v1.0**

1. Exported API is not removed or changed incompatibly in a single step. Mark it `// Deprecated: use X instead.` and keep it for **at least one minor release** before removing it.
2. If a breaking change cannot go through deprecation (for example, a security fix), the PR carries the `breaking-change` label and the CHANGELOG entry under "Breaking changes" includes migration steps.

**From v1.0**

1. No incompatible change to the exported API of stable packages within v1. Removals happen only in a new major version (`/v2`).
2. Deprecated API stays until the next major version.

**Always**

- Adding methods to an exported interface is a breaking change for implementers and follows the same rules.
- Raising the minimum Go version in `go.mod` happens only in a minor release and is noted in the CHANGELOG.
- Unexported code, `internal/` packages, examples and tests are not covered.

## Enforcement

`scripts/check_api_compat.sh` compares the exported API of the working tree with the latest release tag using the Go team's [`apidiff`](https://pkg.go.dev/golang.org/x/exp/cmd/apidiff), and fails when a stable package has an incompatible change. It runs on every pull request; the `breaking-change` label turns the failure into a report.

```bash
bash scripts/check_api_compat.sh            # compare with the latest tag
bash scripts/check_api_compat.sh v0.55.0    # compare with a given version
```

## Road to v1.0

v1.0 is tagged when all of the following hold:

- [ ] Two consecutive minor releases ship with no incompatible change to stable packages.
- [ ] Package docs and exported identifiers of stable packages have English godoc.
- [ ] User-facing guides have English versions.
- [ ] Stable package coverage stays at or above the thresholds enforced by `scripts/check_coverage.sh`.
