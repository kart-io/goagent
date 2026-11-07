# 错误处理迁移

框架错误的唯一入口为 `github.com/kart-io/goagent/errors`。此次删除原领域哨兵声明及错误常量转发，不提供兼容变量。主仓与本地 Examples、Contrib、Harness 已同步更新；其他使用方需迁移引用后升级。

## 普通业务错误

```go
import agentErrors "github.com/kart-io/goagent/errors"

return agentErrors.New(agentErrors.CodeInvalidInput, "会话 ID 不能为空").
    WithComponent("session").WithOperation("validate_id")
```

需要保留底层原因时使用 `Wrap` 或已有专用构造器，不拼接底层错误后丢弃错误链。`AgentError.Error()` 会增加分类和组件前缀，调用方应使用 `GetCode`/`errors.Is`/`errors.As` 判断，避免完全匹配业务错误字符串。上下文仅通过结构化字段读取，不混入默认错误文本。

## 固定控制信号

```go
import (
    "errors"
    agentErrors "github.com/kart-io/goagent/errors"
)

if errors.Is(err, agentErrors.ErrLeaseLost) {
    // 当前持有者已经失去写入权。
}
code := agentErrors.GetCode(err)
_ = code
```

固定信号保持原有文本和独立身份，类型改为字段不可变的 `*errors.SentinelError`。领域错误实现 `CodedError.ErrorCode()` 即可参与统一分类，`GetCode` 同时保留自定义 `As` 的业务错误投影，并识别包装与合并链；分类相同不代表身份相同。外层 `AgentError` 仍优先提供其分类，合并链按从左到右的标准遍历顺序取首个框架码。标准上下文、fs、io 错误仍按原身份匹配。

| 原入口 | 新入口 |
|---|---|
| `core.ErrAgentAlreadyExists/ErrChainAlreadyExists/ErrToolAlreadyExists/ErrNotImplemented` | `errors` 中同名对象；其余未使用的原 core 哨兵已删除 |
| `durable.Err*` | `errors.Err*` |
| `session.ErrConflict/ErrNotFound/ErrExists` | `errors.ErrSessionConflict/ErrSessionNotFound/ErrSessionExists` |
| `builder.ErrWaiting/ErrHandleDone` | `errors.ErrAgentWaiting/ErrAgentHandleDone` |
| `contextpolicy.ErrReleased` | `errors.ErrResultScopeReleased` |
| `budget.ErrBudgetExceeded` | `errors.ErrModelBudgetExceeded` |
| `evaluation.ErrBudgetExceeded` | `errors.ErrEvaluationBudgetExceeded` |
| `workflow.ErrMaxSteps` | `errors.ErrWorkflowMaxSteps` |
| `refund.ErrNotFound` | `errors.ErrRefundNotFound` |
| `tools.ErrKeyNotFound/ErrValueNotFound` | `errors.ErrToolKeyNotFound/ErrToolValueNotFound` |
| 其余 `tools`、`workflow`、`multiagent`、`cache`、`parsers` 的公开哨兵 | `errors` 中同名对象 |

原 `core.ErrToolNotFound` 等未使用哨兵，以及原 `tools.ErrToolNotFound` 等未使用字符串提示，均直接删除。需要描述工具未找到的业务错误，使用已有 `NewToolNotFoundError`，调用方按 `CodeToolNotFound` 分类。实际使用的固定信号取文本可使用其 `Error()`。模型提示使用实际保留的 `Message*`（如 `MessageAPIKeyMissing` 和 `MessageBadRequest`），配置错误字段保留实际使用的 `ErrorFieldAPIKey`。

68个原哨兵中保留46个实际使用的对象；22个没有业务消费者的旧对象直接删除，其中包括 `llm.ErrUnsupportedCapability`、`cache.ErrCacheInvalid`。新的未实现能力错误可由已有 `NewNotImplementedError` 表达，按 `CodeNotImplemented` 判断。

未被实际业务使用的 `ErrType*` 分类字符串删除；业务错误按实际场景复用已有结构化 `Code*`。本次仅为已有运行信号增加13类代码，不按每条文案新增一个码。

## 边界与验证

检查器禁止框架生产代码直接调用或别名引用标准库 `errors.New`，标准库 `Is/As/Join` 继续使用。独立的 codec、harness/env、示例/命令行、脚本与测试工具保留标准错误；实际边界见 [导入层级](../architecture/IMPORT_LAYERING.md)。治理同时覆盖标准 `errors.New` 和无 `%w` 的业务 `fmt.Errorf`。格式化业务错误用 `Newf(code, format, args...)` 并补组件/操作；保留已有合法 `%w` 包装及SDK类型错误，不让新的分类覆盖原信号。多个 `%w` 的合并语义也保持，`%%w` 仅为文本。

```sh
go test ./errors ./scripts/apicheck
go run ./scripts/apicheck -mode imports -root .
GO_TEST_MODE=true go test -race -count=1 -timeout 5m ./...
```
