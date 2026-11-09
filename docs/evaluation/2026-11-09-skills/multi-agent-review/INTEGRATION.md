# Harness integration and backwards-compatibility audit

Date: 2026-11-09. **IN-01 (called INT-01 in the original review) is RESOLVED after current-source retest of all three reported path spellings. No lifecycle defect reproduced.** The updated Harness boundary-test issue is also CLOSED: the actual targeted Harness regression now passes. No open runtime integration defect remains in this specialist scope. The known unpublished-core release gate was not retested or counted as a new finding.

The original audit and failure evidence below are retained as historical evidence. The **Final current-source retest** section supersedes their open/failed status: all original temporary integration probes now pass, including embedded dot and repeated-separator paths. The subsequent closure retest also passes the corrected Harness assertion and joined-error classifier regressions. Full-suite and standalone release validation remain separately owned.

## Scope and evidence

Reviewed actual current source in:

- Core: `/home/hellotalk/code/go/src/github.com/kart-io/goagent`, HEAD `d010f039610aecbc57c0b0c261555fa2765c5f76`.
- Harness: `/home/hellotalk/code/go/src/github.com/kart-io/goagent-harness`, HEAD `912bca0a05c11c204b5cad695f0bd85fc0b10b5b`.

Compared Harness uncommitted changes with HEAD. Read the prior `../REPORT.md`; none of its nine closed findings is reopened here. Neither repository has `.claude/skills/` or `.agents/skills/` project skills. No phase SUMMARYs or requirement IDs were available; the requirements map below uses the requested audit areas instead of inventing milestone IDs.

Current-source tests used `/tmp/skills-integration-u10nq6yr/go.work`, containing both actual repository directories. `go list -m` confirmed both resolutions. No overlay, replace directive, module-manifest edit, repository test/source edit, git-state change, full suite, or real model was used. An exact `git archive HEAD` export of Harness under `/tmp/skills-integration-u10nq6yr/harness-head` served only as the historical comparison, with a separate `head.go.work` using the same current core checkout. Toolchain: `go1.27.3 linux/amd64`.

Only this assigned report was written inside either repository. Probe source and its initial output remain at `/tmp/skills-integration-u10nq6yr/integration_test.go` and `current-probes.log`.

## Original genuine defect — resolved

### IN-01 / INT-01 — P2, RESOLVED: Previously valid relative reference paths failed through the extracted tool

**Locations:** Harness `preset/skills.go:99`; core `tools/skill/tool.go:69`, `skills/catalog.go:166`, and `tools/skill/tool.go:89`.

**Concrete trigger:** An existing skill instructs the caller to read a Markdown-style relative link such as `./references/guide.md`. Its directory contains that regular file. Build succeeds, and the model passes the path from the instructions to `load_skill`.

**Observed:** Current Harness returns a model-facing rejection claiming the file does not exist. The canonical spelling `references/guide.md` succeeds. The same public Harness Build → assembled tool → Invoke flow succeeds at HEAD for both spellings. Also reproduced for `references/./guide.md` and `references//guide.md`.

| `file` argument | Harness HEAD | Current Harness |
|---|---|---|
| `references/guide.md` | Success, reference content | Success, reference content |
| `./references/guide.md` | Success, reference content | Rejected: `skill demo has no file ./references/guide.md` |
| `references/./guide.md` | Success, reference content | Rejected: `skill demo has no file references/./guide.md` |
| `references//guide.md` | Success, reference content | Rejected: `skill demo has no file references//guide.md` |

For `./references/guide.md`, actual current output was:

```json
{"result":{"error":"rejected","message":"skill demo has no file ./references/guide.md"},"success":false}
```

HEAD output was:

```json
{"result":{"content":"reference content","file":"./references/guide.md","name":"demo"},"success":true}
```

**Expected:** Existing harmless relative spellings accepted by the Harness tool should continue to load the same in-bound reference. The tool still describes `file` simply as a relative path. A caller following the unchanged skill instructions should not have to infer that a reported missing file actually needs path normalization.

**Cause:** HEAD's `preset/skills.go:246` passed `file` to the bounded `os.Root.ReadFile`. The extraction directly installs the shared tool, which forwards the same string to `Catalog.ReadReference`; `fs.ValidPath` rejects dot and empty path components. `loadError` then converts this invalid-path error into “has no file.” The shared Catalog's canonical-path restriction is explicitly documented and is not itself being reported as a security bug. The defect is the missing compatibility adaptation when replacing the existing Harness tool. Harness v3 migration documentation covers frontmatter and manifest changes, but does not address this runtime reference-argument break. This is distinct from the previously closed parser-compatibility finding.

**Minimal remedy:** At the Harness compatibility boundary, normalize benign `.` and repeated-separator components before calling the strict Catalog. Keep rejecting absolute paths, backslashes, NUL, and `..` components; continue using the Catalog's `os.Root` boundary. Preserve the original `file` spelling in successful tool results if maintaining the prior response contract. Add a focused assembled-tool regression for a skill whose body links to `./references/guide.md`.

**Reproducible evidence:** `TestCompatibilityRelativeReference` in the temporary probe fails on current source (exit 1, three failing subcases) and passes against the exact Harness HEAD export (exit 0). See the command inventory and standalone reproduction below.

## Optional improvements

### OPT-01 — P3, optional compatibility documentation: empty resource lists now serialize as `[]`

**Location:** core `skills/catalog.go:147`, returned by `tools/skill/tool.go:65`. Historical Harness location: `preset/skills.go:228` at HEAD.

**Trigger:** Load a skill containing only `SKILL.md`.

**Observed / expected:** Current result is `{"files":[],"instructions":"body","name":"demo"}`; HEAD returned `{"files":null,"instructions":"body","name":"demo"}`. Both are successful results. The normalized empty array is useful, and no failing downstream consumer was demonstrated; this is not counted as a product defect. Exact wire-format compatibility would require preserving the old null, but documenting the intentional normalization is preferable absent such a consumer requirement.

**Evidence:** `TestCompatibilityEmptyFilesShape` logs both JSON results under current and HEAD workspaces.

**Minimal remedy:** Mention the empty-list normalization in Harness migration notes and include it in a result-shape test. No production behavior change is necessary on the evidence collected.

## Original integration check (before fixes)

### Wiring summary

**Connected:** 7 key shared symbols/methods. **Orphaned:** 0 among these reviewed contracts. **Missing symbol connections:** 0. **Argument-contract mismatches:** 1 (INT-01).

| Provider | Consumer and verified use |
|---|---|
| `constants.ToolLoadSkill` | Harness `ToolLoadSkill` alias, prompt, and policy rule; shared tool registration |
| `skills.Discover` | Harness `buildSkills`, after configured sources are authorized and opened in order |
| `Catalog.List` | Harness prompt filtering and manifest digest list; adapter unknown-skill discrimination |
| `skill.New` | Harness `a.AddTools`; publication exposes it through Dispatcher/Pipeline |
| `Catalog.Load` | Tool body/files branch; actual assembled-tool invocation exercised |
| `Catalog.ReadReference` | Tool file/content branch; canonical references succeed; legacy spellings expose INT-01 |
| `Catalog.Close` | Registered with `a.OnClose` before empty-catalog return and publication |

Harness continues to own default sources, trust authorization, prompt visibility, policy, and manifest assembly. The shared package owns parsing/discovery, retained directory handles, snapshots, and constrained resource reads. No selected export was merely imported without being called.

### API coverage

HTTP API routes: not applicable to this extraction. The public tool is consumed through the real assembled `Assembly.Tools` dispatcher path, not just invoked as an isolated core adapter.

### Auth protection

Both external source forms tested—absolute and home-relative—require a project trust grant before Build, then load after granting the returned root/digest. Changing configured sources invalidates the grant. Trusted-source configuration bypasses the project grant as intended. Explicit `load_skill` deny policy overrides the component's built-in allow rule.

Relative default/custom sources remain workspace-bounded; the existing targeted boundary tests passed. Hidden skills remain name-callable and are omitted only from the system prompt, as documented. No new unprotected source or policy bypass was reproduced. Parser security is outside this specialist's scope.

### E2E flows

**Complete:** 7 checked flows. **Broken:** 1 compatibility flow.

| Flow | Result and evidence |
|---|---|
| Source configuration → discovery → selected body | PASS: default `.agents` before `.claude`; explicit empty dirs still use defaults; custom order reversed successfully; custom dirs replace defaults; missing sources skipped; duplicate source entries do not duplicate selected skills; malformed shadowed skill still fails Build |
| Untrusted external source → authorization → Build → tool | PASS: absolute and `~/` sources blocked, then load after grant; changed configuration requires reauthorization |
| Hidden metadata / permission rules → prompt and tool | PASS: hidden names omitted from prompt but callable by name; metadata does not synthesize missing tools; explicit deny remains effective |
| Empty/missing sources → empty assembly → Close | PASS: no skill prompt/tool, repeated Close succeeds |
| Tool arguments → dispatcher → result/error | PASS: body and reference result keys preserved; unknown skill, missing file and traversal produce model rejections; bad arguments become `invalid_arguments`; canceled and closed calls retain errors identifiable with `errors.Is` |
| Raw SKILL bytes → v3 manifest → durable pin/resume | PASS: unchanged Build stable; raw comment-only change alters manifest without changing prompt; reference-only edit does not change manifest and is read live; v2 version or changed raw document fails pin comparison |
| Build/Close/cancel/failure → resource cleanup | PASS: source-acquisition failure, later discovery failure, later component failure, duplicate-tool publication failure, normal/repeated Close, and mid-discovery cancel release handles; cleanup error remains joined to build error |
| Existing skill link `./references/guide.md` → assembled tool → content | FAIL: argument reaches stricter Catalog unchanged and becomes a false missing-file rejection (INT-01) |

### Lifecycle evidence

The Linux-only temporary probe counted `/proc/self/fd` links beneath its unique fixture root, avoiding unrelated process-descriptor counts. Successful Build retained exactly the two selected skill directory handles; closing released both. Twenty iterations each of normal Build/Close, a failing later component, a publish failure, and malformed-document discovery failure left zero fixture handles open. The later component's cleanup hook returned a sentinel error: Build preserved it through `errors.Is`, and the hook ran once per failed assembly.

For cancellation, a test Context wrapper canceled its underlying standard context when an `Err()` check observed that the first skill directory handle had actually been acquired. Build returned an error matching `context.Canceled` and left zero fixture handles. This verifies cleanup after partial discovery rather than only testing a context canceled before entry. It does not model interrupting a blocked kernel or network-filesystem call.

### Requirements integration map

No formal REQ-IDs were supplied. These rows map the user's requested areas to actual connections.

| Requested area | Integration path | Status | Issue |
|---|---|---|---|
| Default/custom source precedence | `SkillsConfig.Validate` → ordered source roots → `Discover` → prompt/loaded body | WIRED | — |
| Trust authorization | `SkillsConfig.Needs` → `TrustRequiredError` / grant digest → Build → discovery | WIRED | — |
| Hidden skills | parsed metadata → `Catalog.List` → prompt filter; `skill.New` → name lookup | WIRED | Visibility is deliberately not an access-control rule |
| Tool results/errors and backwards compatibility | assembled Dispatcher/Pipeline → adapter → `Load` / `ReadReference` → output | PARTIAL | INT-01; optional empty-list representation note OPT-01 |
| Manifest v3 effects | raw document digest → `SetInfo` / component version → `Manifest.Digest` → `PinManifest` | WIRED | Reference content excluded by declared design |
| Cleanup failure/cancel | borrowed roots → retained Catalog roots → `OnClose` → failed Build / Assembly.Close | WIRED | No leak or swallowed cleanup error reproduced |

**Requirements with no cross-phase wiring:** None of the six requested areas above. Parser internals and the unpublished dependency gate are separately owned or already declared, not additional uncovered integration requirements here.

## Declared limits

- The new core packages are not yet in Harness's published dependency. Main separately reproduced and archived the standalone release failure. This review does not claim standalone release readiness and did not repeat that check.
- Canonical slash-separated reference paths, byte/count/depth limits, and regular-file restrictions are explicit core contracts. INT-01 concerns adapting existing Harness inputs to that contract, not removing those restrictions.
- Raw `SKILL.md` is snapshotted and hashed. References are live reads and are not covered by the manifest digest; the probe confirms this declared behavior rather than treating it as a new defect.
- Manifest v3 deliberately invalidates prior pinned assemblies. Parser tightening and the nine closed prior findings were not relabeled as new findings.
- No actual `os.Root.Close` syscall failure was induced. The later-component sentinel verifies shared failed-build error aggregation and that skill cleanup still runs. No blocked remote filesystem, Windows/macOS runtime, Docker, Postgres submodule, real model, or full-suite claim is made.

## Original test commands run (before fixes)

All commands were targeted. Current workspace contents:

```go
go 1.27.0

toolchain go1.27.3

use (
 /home/hellotalk/code/go/src/github.com/kart-io/goagent
 /home/hellotalk/code/go/src/github.com/kart-io/goagent-harness
)
```

1. In Harness, **PASS**:

```sh
GOWORK=/tmp/skills-integration-u10nq6yr/go.work go test -race -count=1 -timeout=90s ./preset -run '^(TestSkills.*|TestBoundarySkill.*|TestTrustModel|TestBuildFailureClosesResources|TestPinManifest)$'
```

2. In core, **PASS** for both packages:

```sh
GOWORK=/tmp/skills-integration-u10nq6yr/go.work go test -race -count=1 -timeout=90s ./tools/skill ./skills -run '^(TestLoadAndReferenceContracts|TestUnknownSkillsAndInvalidReferencesAreRejected|TestInvalidArgumentsUseFrameworkValidation|TestCanceledAndClosedCatalogRemainErrors|TestNilCatalogFailsWithoutPanic|TestCatalogPrecedenceSnapshotsAndOwnership|TestCatalogContextAndConcurrentClose)$'
```

3. Temporary public-API probes against current source, **FAIL only in `TestCompatibilityRelativeReference`**; six integration tests and the empty-list observation passed. No race detected:

```sh
GOWORK=/tmp/skills-integration-u10nq6yr/go.work go test -race -count=1 -timeout=90s -v /tmp/skills-integration-u10nq6yr/integration_test.go
```

The initial command redirected output to `current-probes.log` and then displayed it; the shell's final status came from displaying the log. The log itself records the failed test. A direct confirmation below separately recorded exit 1.

4. Same compatibility probes against exact Harness HEAD export, **PASS**:

```sh
GOWORK=/tmp/skills-integration-u10nq6yr/head.go.work go test -race -count=1 -timeout=90s -v /tmp/skills-integration-u10nq6yr/integration_test.go -run '^(TestCompatibility.*|TestIntegrationDefaultCustomAndHidden|TestIntegrationTrustAndPolicy|TestIntegrationEmptySources)$'
```

`head.go.work` differs only by selecting `/tmp/skills-integration-u10nq6yr/harness-head` for the Harness module. The export was created with read-only `git archive HEAD`, not a worktree or checkout mutation.

5. Direct exit-code confirmation against current source, **FAIL / exit 1**, same three noncanonical reference spellings:

```sh
GOWORK=/tmp/skills-integration-u10nq6yr/go.work go test -count=1 -timeout=30s -v /tmp/skills-integration-u10nq6yr/integration_test.go -run '^TestCompatibilityRelativeReference$'
```

## Standalone reproduction for INT-01

Save the following as `/tmp/relative_reference_test.go`; run `GOWORK=<temporary workspace selecting core and Harness> go test -count=1 -v /tmp/relative_reference_test.go`. It fails with current Harness and passes with the HEAD export. It creates fixtures only beneath the test's temporary directory and invokes no model.

```go
package integration_test

import (
    "context"
    "os"
    "path/filepath"
    "testing"

    "github.com/kart-io/goagent-harness/preset"
    "github.com/kart-io/goagent/interfaces"
)

func TestRelativeReference(t *testing.T) {
    root := t.TempDir()
    dir := filepath.Join(root, ".agents", "skills", "demo")
    if err := os.MkdirAll(filepath.Join(dir, "references"), 0700); err != nil {
        t.Fatal(err)
    }
    files := map[string]string{
        "SKILL.md": "---\nname: demo\ndescription: demo\n---\nRead [guide](./references/guide.md).",
        "references/guide.md": "reference content",
    }
    for file, content := range files {
        if err := os.WriteFile(filepath.Join(dir, file), []byte(content), 0600); err != nil {
            t.Fatal(err)
        }
    }
    set, err := preset.Load(preset.Source{
        Layer: preset.LayerHost, Path: "probe", Trusted: true,
        Data: []byte(`presets: [{id: p, components: [{id: s, use: skills}]}]`),
    })
    if err != nil { t.Fatal(err) }
    p, ok := set.Get("p")
    if !ok { t.Fatal("missing preset") }
    asm, err := preset.Build(context.Background(), p, preset.Host{
        Root: root, ArtifactDir: t.TempDir(),
    })
    if err != nil { t.Fatal(err) }
    defer asm.Close()
    for _, tool := range asm.Tools {
        if tool.Name() != preset.ToolLoadSkill { continue }
        for _, file := range []string{"references/guide.md", "./references/guide.md"} {
            out, err := tool.Invoke(context.Background(), &interfaces.ToolInput{
                Args: map[string]any{"name": "demo", "file": file},
            })
            if err != nil { t.Fatal(err) }
            t.Logf("file=%q output=%+v", file, out)
            if out == nil || !out.Success { t.Errorf("existing reference rejected: %s", file) }
        }
        return
    }
    t.Fatal("load_skill missing")
}
```

## Original source fingerprints (before fixes)

SHA-256 at completion of source review:

```text
e178447af644ad655ca33e38e04abc1166a553d13a1c88eab6420beace126991  core/skills/catalog.go
c230a82ecf3b12ceaa715619dcc2799b09cd7fdfe29686c2352d1536a1cffff8  core/tools/skill/tool.go
b7265272b0c262b9f60fc30fbd1bca249d2919706dacc6ec69186184272a04e8  harness/preset/skills.go
e04255be05166567eb638f19628098d3808ae7a19ac6fb5575ff5a9749f9fb2d  harness/preset/build.go
```

## Historical handoff: reference error classification fix

After the report was completed, the parent reported Architecture's independent EMFILE reproduction and ownership of a positive expected-error allowlist / default `ExecutionError` fix. This specialist did not reproduce or double-count that finding. A subsequent source read still showed the baseline catch-all rejection at `tools/skill/tool.go:89`; the earlier passing error-contract probes did not exercise descriptor exhaustion.

Integration concerns for that fix:

- Defaulting unclassified read failures to `ExecutionError` with the original cause is appropriate. Keep `errors.Is` working for operational errors, cancellation, deadlines, and closed catalogs. Do not classify every `*fs.PathError` as a model rejection: operational failures use that wrapper too.
- Harness `preset/boundary_audit_test.go:65` currently groups `outside-link` with syntactically invalid paths and requires all three to return a rejection with nil error. An opaque `os.Root` escape error may legitimately become an execution error. Split that case from the typed invalid-path cases. Continue asserting no success and no returned outside content, while retaining the adjacent successful internal-symlink read as a positive control. Do not add brittle error-string matching solely to preserve the old rejection category.
- Keep known missing-file and invalid-path inputs model-recoverable, and preserve the existing unknown-skill distinction. The stricter canonical-path contract remains separate from INT-01's Harness adaptation requirement.
- As an implementation consideration, `skills/catalog.go` joins read and close errors. Matching one expected child with `errors.Is` should not silently discard an unexpected operational sibling in a joined error. No such simultaneous failure was injected in this audit.

No additional tests were run for this handoff; it is a source review of the proposed change's integration points. The parent-owned fix and its focused regression tests remain to be validated after implementation.


## Final current-source retest

**IN-01 / INT-01: RESOLVED.** The final `Catalog.ReadReference` rejects parent components, absolute paths, backslashes, NUL and trailing directory selectors before cleaning benign dot/repeated-separator spellings. All original `TestCompatibilityRelativeReference` subcases pass through actual Harness `Assembly.Tools`, not a source overlay or isolated replacement. The adapter preserves the original `file` argument in successful output.

| Original failing argument | Final actual Harness result |
|---|---|
| `./references/guide.md` | Success, `reference content`, original `file` spelling preserved |
| `references/./guide.md` | Success, `reference content`, original `file` spelling preserved |
| `references//guide.md` | Success, `reference content`, original `file` spelling preserved |

Canonical and repeated leading `././` spellings also succeed. The final temporary probe verifies denials for `..` before normalization (including an interior parent component), absolute paths, backslashes, NUL, trailing `/`, and trailing `/.`. Internal symlinks still read successfully; an external symlink returns an execution error with no result. The intermediate leading-prefix-only fix had left two original subcases failing; the expanded final fix supersedes that result.

All eight original temporary top-level tests plus the added post-fix boundary test pass under `-race`. This includes the six integration/lifecycle tests, the four reference-spelling subcases, and the empty-list representation observation. The reviewed runtime flows are now **8 complete / 0 broken**; the tool/backwards-compatibility requirement row is now **WIRED**. OPT-01 remains optional documentation only.

AR-01 remains Architecture-owned, not double-counted. Its new `TestUnclassifiedReadErrorsPreserveCause` and actual Linux descriptor-exhaustion subprocess test both pass. Unknown I/O failures preserve their cause; known missing files remain rejections, invalid/limited references receive specific explanations, and opaque root-escape errors propagate. The temporary assembled-Harness probe independently confirms that an opaque escape error produces `out == nil` and nonnil error through Dispatcher. Successful internal-symlink reads remain the positive control.

### Verification issue — P2, CLOSED: updated Harness assertion contradicted Dispatcher output contract

**Location:** Harness `preset/boundary_audit_test.go:68`; relevant unchanged implementation is core `tools/dispatcher.go:675`–`680`.

**Trigger:** Run `TestBoundarySkillReferencesStayConfined` after the reference error-classifier fix. Its `outside-link` invocation now correctly raises an execution error.

**Observed:** The test fails at `require.NotNil(t, out)`. `Dispatcher.runTool` deliberately returns `nil, e` when the underlying tool returns an execution error. The reference remains unreadable and no content is returned; this is a regression in the new test assertion, not a demonstrated production access-control defect.

**Expected:** The denial test accepts the assembled Dispatcher contract while asserting no successful result/content. The successful internal-link control remains in place.

**Evidence:** Actual Harness targeted command below exits 1 with `boundary_audit_test.go:68: Expected value not to be nil`. The independent temporary post-fix probe logs `output=<nil>` and `error=[TOOL_EXECUTION] [load_skill] operation=invoke: skill loading failed: openat outside-link: path escapes from parent` and passes its denial checks.

**Minimal remedy:** Keep `require.Error(t, err)` and use `require.Nil(t, out)` for this known Dispatcher contract, or conditionally require `!out.Success` and `out.Result == nil` when output is nonnil. No production error-classifier change is needed to satisfy this assertion. This specialist has not edited the Harness test.

### Retest commands and outcomes

Same actual-repository temporary workspace as the original review; no overlays, repository source/test/module changes by this specialist, full suites, or model calls.

1. Actual Harness, **FAIL solely at the new boundary-test nonnil-output assertion**:

```sh
GOWORK=/tmp/skills-integration-u10nq6yr/go.work go test -race -count=1 -timeout=90s ./preset -run '^(TestSkills.*|TestBoundarySkill.*|TestTrustModel|TestBuildFailureClosesResources|TestPinManifest)$'
```

This invocation ran after the error-classifier change and the first leading-prefix fix. The failing Harness assertion was re-read after the expanded path fix and remained unchanged; the final public-API probes below validate the expanded fix through the actual Harness. At that intermediate checkpoint the assertion had not yet passed; the subsequent closure retest below supersedes this intermediate failure.

2. First follow-up core selection, **PASS**:

```sh
GOWORK=/tmp/skills-integration-u10nq6yr/go.work go test -race -count=1 -timeout=90s ./skills ./tools/skill -run '^(TestCatalogPrecedenceSnapshotsAndOwnership|TestCatalogConfinesReferencesAndRetainsDirectory|TestCatalogContextAndConcurrentClose|TestCatalogFileAndTraversalLimits|TestLoadAndReferenceContracts|TestUnknownSkillsAndInvalidReferencesAreRejected|TestInvalidArgumentsUseFrameworkValidation|TestCanceledAndClosedCatalogRemainErrors|TestNilCatalogFailsWithoutPanic|TestReferenceContentAndLimitRejections)$'
```

3. Operational-error follow-up, **PASS**, including actual EMFILE in an isolated subprocess:

```sh
GOWORK=/tmp/skills-integration-u10nq6yr/go.work go test -race -count=1 -timeout=60s ./tools/skill -run '^(TestUnclassifiedReadErrorsPreserveCause|TestDescriptorExhaustionIsAnExecutionFailure)$'
```

4. Final expanded path fix, **PASS** for all original temporary probes plus the post-fix boundary test:

```sh
GOWORK=/tmp/skills-integration-u10nq6yr/go.work go test -race -count=1 -timeout=90s -v /tmp/skills-integration-u10nq6yr/integration_test.go /tmp/skills-integration-u10nq6yr/postfix_test.go
```

The original `integration_test.go` was not weakened or changed. Only the additional `/tmp/postfix_test.go` expectations were updated to test the expanded normalization contract. Before the final fix, rerunning the original file confirmed that the leading case passed while the two interior spellings still failed. A separate `-run '^TestPostfixLeadingDotAndBoundary$'` invocation passed for the intermediate leading-only implementation. Final combined run: `ok command-line-arguments 1.082s`.

5. Final expanded path fix, focused core regression selection, **PASS**:

```sh
GOWORK=/tmp/skills-integration-u10nq6yr/go.work go test -race -count=1 -timeout=90s ./skills ./tools/skill -run '^(TestCatalogPrecedenceSnapshotsAndOwnership|TestCatalogConfinesReferencesAndRetainsDirectory|TestLoadAndReferenceContracts|TestUnknownSkillsAndInvalidReferencesAreRejected|TestReferenceContentAndLimitRejections)$'
```

### Final retested source fingerprints

```text
0b86260e033822e915ebb31bef78eb1be77e594843130ed1340639ccbe3c8ed1  core/skills/catalog.go
5c862a659c3b2e717f5fe9ba2bfe8c06e65bf317ea6a3448421d4e64aa99e91b  core/tools/skill/tool.go
358fc37fe5dbe4cf60c8014c1afd543a666ca9297f121de5433173d7c1e742e2  core/skills/catalog_test.go
ba6f6270d00c594461b64fa183f97b15a66b7ed2eb192ddcbd63e9a98b4697d7  core/tools/skill/tool_test.go
28c12854d23507c3513c845c8fab968e2fcf144e8ac0ea60c178c950630eec56  core/tools/skill/errors_test.go
6d15c1a8a0985afd5606e3f3d7bc415906350fe6f5dbcd2cb8723a50f3d8ed29  core/tools/skill/resource_linux_test.go
4ac93caded2d43c9aa16a6bd024f98e0b9935d3e2cd839ea6b2ab6014a95bfaf  harness/preset/boundary_audit_test.go
```


## Closure retest: Harness boundary assertion and joined-error guard

**Status: CLOSED.** The corrected Harness boundary test passes against both actual current repositories using the same temporary `go.work`. It accepts a nil output on execution error, requires the preserved `*os.PathError` to identify `outside-link`, and checks unsuccessful/no-result output if one exists. The internal-link success control and other path-denial assertions remain intact. This resolves the test-issue note without changing the production Dispatcher contract.

The current `expectedReferenceError` guard recursively checks every joined or wrapped cause before permitting an expected rejection. The targeted regression confirms that request errors joined with EIO in either order, and an invalid-input wrapper around EIO, remain execution failures with `errors.Is(..., syscall.EIO)` preserved. Known missing/invalid-reference and size/content-limit cases continue to return their intended model-facing rejections. This closes the earlier joined-error implementation concern on the tested cases; it is not a separate new finding.

Commands run for this closure (both **PASS**, exit 0, `-race`):

```sh
# From actual Harness checkout:
GOWORK=/tmp/skills-integration-u10nq6yr/go.work go test -race -count=1 -timeout=90s ./preset -run '^(TestSkills.*|TestBoundarySkill.*|TestTrustModel|TestBuildFailureClosesResources|TestPinManifest)$'
# ok github.com/kart-io/goagent-harness/preset 1.037s

# From actual core checkout:
GOWORK=/tmp/skills-integration-u10nq6yr/go.work go test -race -count=1 -timeout=60s ./tools/skill -run '^(TestUnclassifiedReadErrorsPreserveCause|TestJoinedRequestAndIOErrorsRemainExecutionFailures|TestUnknownSkillsAndInvalidReferencesAreRejected|TestReferenceContentAndLimitRejections)$'
# ok github.com/kart-io/goagent/tools/skill 1.013s
```

IN-01 remains resolved for all three original spellings. The earlier successful final temporary probes remain applicable to the unchanged path normalization. No production source, repository test, module manifest, or git state was modified by this specialist; only this report was updated. No full suite or real model was run.

Closure-source SHA-256 values (supersede the corresponding earlier fingerprints):

```text
5376764841b9e48c5b7b1ac73dc0dadad48aa891ae282d2910bdeefa9b8e40da  core/tools/skill/tool.go
d29120807ba990a488f7800ce4763627f32f99fe3f39351a9da61e00da00137b  core/tools/skill/errors_test.go
dcda15e628ccfddda481e2b4e5dae11365654b4b1e510c5e086777621ff349f1  harness/preset/boundary_audit_test.go
```
