# Shared Constants

[English](README.md) | [中文](README_CN.md)

`constants/` is the single public home of shared protocol values, defaults and the `Provider` type, split into files by domain. Import it as `github.com/kart-io/goagent/constants`.

| File | Contents |
|---|---|
| `common.go` | Message roles |
| `fields.go` | Shared field names |
| `http.go` | HTTP methods, headers and content types |
| `llm.go` | Provider identifiers, environment variables, model parameters and protocol fields |
| `tools.go` | Tool names, descriptions and execution defaults |
| `parsers.go` | ReAct output markers |
| `defaults.go` | Shared timeouts, cleanup deadlines and per-layer defaults |
| `cache.go` | Middleware cache defaults |
| `execution.go` | Model resend, inbox write and transient retry policy |
| `budget.go` | Agent topology budget defaults and the architecture-sized unlimited remainder |
| `durable.go` | Interrupt and resume protocol, reasons, and re-entry/deduplication bounds |
| `session.go` | Session event types; payload types stay in `core/session` |
| `workflow.go` | Graph start and end markers, the multi-agent finish marker and resume rejection reasons |
| `memory.go` | Memory defaults and long-term memory tool names |
| `revisions.go` | Size and count limits of agent revision archives |
| `management.go` | Required management limit defaults and the adapter inbound request cap |
| `skills.go` | The `load_skill` tool name and skill resource limits |
| `doc.go` | Package documentation |

```go
import shared "github.com/kart-io/goagent/constants"

requestTimeout := shared.DefaultToolTimeout // time.Duration
role := shared.RoleAssistant
```

Error codes, categories, messages and error context fields live in [`errors/`](../errors/README.md). HTTP protocol values such as methods and headers stay here.

The former forwarding entries in `interfaces`, `llm/constants`, `llm/common`, `llm`, `tools`, `store`, `parsers` and `core` are gone, and so is the `llm/constants` package, with no compatibility aliases. Use this package directly and `errors` for errors.

`Provider` is a named string type; the built-in provider identifiers use it explicitly. Custom providers use `shared.Provider("minimax")`; string APIs need `string(provider)`. A client's `Provider()` method must return this type; see the [constants migration guide](../docs/guides/CONSTANTS_MIGRATION.md).

Rules:

- This is a stable package (see the [API stability policy](../docs/API_STABILITY.md)): removing a constant or changing its value goes through a deprecation period.
- Depend only on the standard library; never import framework implementations or third-party SDKs. `scripts/apicheck` enforces this.
- Shared protocol values, public extension conventions and default policies belong here. Module-specific enums, internal state machine markers, persisted format versions and test fixtures stay in their own packages.
- Equal numbers do not mean equal meaning. The legacy completion API `DefaultCompletionMaxTokens=2048` and the Anthropic `max_tokens` default `DefaultClientMaxTokens=2000` are managed separately; `DefaultProviderMaxTokens=1000` is kept only as the old value and providers no longer apply it. Limit constants such as `DefaultAgentMaxIterations`, `DefaultDispatcherConcurrency` and `DefaultMaxAgentCalls` record pre-v0.60 defaults; the framework no longer fills them in, so callers pass them explicitly when needed.
- Units are explicit: `DefaultToolTimeoutSeconds` is whole seconds, `DefaultToolTimeout` is a `time.Duration`, and existing API types are unchanged.
- Do not create a constant for every literal, and do not merge different domain concepts because their strings match.
- `BudgetUnlimited` stays `math.MaxInt` so its bound follows the target architecture; never hard-code a 64-bit literal. `DefaultApprovalTimeout`, `DefaultInboxWriteTimeout`, `MaxModelRetryAfter` and similar values remain `time.Duration`.
- `DefaultTransientRetryBaseDelay=500ms` and `DefaultTransientRetryMaxDelay=8s` belong to `builder.RetryTransient`; the provider-level `DefaultBaseDelay=1s` and `DefaultMaxDelay=30s` are a separate policy.
- Do not add constants without callers: a new constant arrives with a reference.
