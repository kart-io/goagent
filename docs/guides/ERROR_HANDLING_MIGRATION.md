# Error Handling Migration

[English](ERROR_HANDLING_MIGRATION.md) | [中文](ERROR_HANDLING_MIGRATION_CN.md)

The single entry point for framework errors is `github.com/kart-io/goagent/errors`. This change removes the original domain sentinel declarations and the error constant forwarding, with no compatibility variables. The main repository and the local Examples, Contrib, and Harness have been updated accordingly; other users need to migrate their references before upgrading.

## Ordinary Business Errors

```go
import agentErrors "github.com/kart-io/goagent/errors"

return agentErrors.New(agentErrors.CodeInvalidInput, "会话 ID 不能为空").
    WithComponent("session").WithOperation("validate_id")
```

When you need to keep the underlying cause, use `Wrap` or an existing dedicated constructor; do not concatenate the underlying error and then drop the error chain. `AgentError.Error()` adds category and component prefixes, so callers should use `GetCode`/`errors.Is`/`errors.As` to check errors and avoid exact matching on business error strings. Context is read only through structured fields and is not mixed into the default error text.

## Fixed Control Signals

```go
import (
    "errors"
    agentErrors "github.com/kart-io/goagent/errors"
)

if errors.Is(err, agentErrors.ErrLeaseLost) {
    // The current holder has lost write access.
}
code := agentErrors.GetCode(err)
_ = code
```

Fixed signals keep their original text and distinct identity, and their type changes to `*errors.SentinelError` with immutable fields. A domain error only needs to implement `CodedError.ErrorCode()` to take part in unified classification. `GetCode` also keeps the business error projection of a custom `As`, and recognizes wrapping and joined chains; the same category does not mean the same identity. An outer `AgentError` still provides its category first, and for joined chains the first framework code is taken in the standard left-to-right traversal order. Standard context, fs, and io errors are still matched by their original identity.

| Old entry point | New entry point |
|---|---|
| `core.ErrAgentAlreadyExists/ErrChainAlreadyExists/ErrToolAlreadyExists/ErrNotImplemented` | Objects with the same names in `errors`; the remaining unused original core sentinels have been deleted |
| `durable.Err*` | `errors.Err*` |
| `session.ErrConflict/ErrNotFound/ErrExists` | `errors.ErrSessionConflict/ErrSessionNotFound/ErrSessionExists` |
| `builder.ErrWaiting/ErrHandleDone` | `errors.ErrAgentWaiting/ErrAgentHandleDone` |
| `contextpolicy.ErrReleased` | `errors.ErrResultScopeReleased` |
| `budget.ErrBudgetExceeded` | `errors.ErrModelBudgetExceeded` |
| `evaluation.ErrBudgetExceeded` | `errors.ErrEvaluationBudgetExceeded` |
| `workflow.ErrMaxSteps` | `errors.ErrWorkflowMaxSteps` |
| `refund.ErrNotFound` | `errors.ErrRefundNotFound` |
| `tools.ErrKeyNotFound/ErrValueNotFound` | `errors.ErrToolKeyNotFound/ErrToolValueNotFound` |
| Other public sentinels of `tools`, `workflow`, `multiagent`, `cache`, `parsers` | Objects with the same names in `errors` |

Unused sentinels such as the original `core.ErrToolNotFound`, and unused string hints such as the original `tools.ErrToolNotFound`, are deleted outright. For business errors that describe a tool not being found, use the existing `NewToolNotFoundError`, and callers classify by `CodeToolNotFound`. To get the text of a fixed signal that is actually used, call its `Error()`. Model hints use the `Message*` constants that are actually kept (such as `MessageAPIKeyMissing` and `MessageBadRequest`), and the configuration error field keeps the actually used `ErrorFieldAPIKey`.

Of the 68 original sentinels, the 46 actually used objects are kept; the 22 old objects with no business consumers are deleted outright, including `llm.ErrUnsupportedCapability` and `cache.ErrCacheInvalid`. New not-implemented capability errors can be expressed with the existing `NewNotImplementedError` and checked by `CodeNotImplemented`.

`ErrType*` category strings that are not actually used by business code are deleted; business errors reuse the existing structured `Code*` values according to the actual scenario. This change adds codes for only 13 kinds of existing runtime signals, rather than adding one code per message.

## Boundaries and Verification

The checker forbids framework production code from calling or aliasing the standard library `errors.New` directly; the standard library `Is/As/Join` continue to be used. The standalone codec, harness/env, examples/command-line tools, scripts, and test tools keep standard errors; for the actual boundaries, see [Import Layering](../architecture/IMPORT_LAYERING.md). The governance covers both standard `errors.New` and business `fmt.Errorf` without `%w`. Format business errors with `Newf(code, format, args...)` and add the component/operation; existing legitimate `%w` wrapping and SDK-typed errors are kept, so the new classification does not override the original signal. The join semantics of multiple `%w` verbs are also preserved, and `%%w` is just text.

```sh
go test ./errors ./scripts/apicheck
go run ./scripts/apicheck -mode imports -root .
go test -race -count=1 -timeout 5m ./...
```
