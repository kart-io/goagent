# 共享常量与 Provider 迁移

共享协议值和默认参数统一通过 `github.com/kart-io/goagent/constants` 使用。原来位于 `interfaces`、`llm/constants`、`llm/common`、`llm`、`tools`、`store`、`parsers` 和 `core` 的460个公开转发常量已删除，`llm/constants` 包也已删除；没有兼容别名。内部清理期限也直接引用中央值。

`interfaces.Tool` 等领域接口、`llm.ToolCall` 及其方法、`core.TimeoutConfig` 及其配置函数仍在原包。只迁移共享常量和 Provider 类型，不改变这些领域对象的使用方式。

## 导入与名称

按旧符号所属包逐项迁移，不能只按名字替换。大部分常量在中央保持原名称，以下默认策略需要特别区分：

| 旧入口 | 新入口 |
|---|---|
| `llm/constants.DefaultMaxTokens` | `constants.DefaultProviderMaxTokens`（1000） |
| `llm.DefaultMaxTokens`、`llm/common.DefaultMaxTokens` | `constants.DefaultCompletionMaxTokens`（2048） |
| `tools.DefaultToolTimeout` | `constants.DefaultToolTimeoutSeconds`（整数秒） |
| `tools.MaxConcurrentToolCalls` | `constants.DefaultToolConcurrency` |
| `tools.MaxRetries` | `constants.DefaultToolRetries` |
| `core.DefaultAgentExecutionTimeout` 等8个默认超时 | `constants` 中同名常量 |

Client/Agent 的2000 token默认策略仍独立管理。`constants.DefaultToolTimeout` 是 `time.Duration`，与整数秒的 `DefaultToolTimeoutSeconds` 不可互换。不能因为字面值相同而合并不同协议字段、状态或默认策略。

```go
import shared "github.com/kart-io/goagent/constants"

method := shared.MethodGet
contentType := shared.ContentTypeJSON
providerTokens := shared.DefaultProviderMaxTokens
timeout := shared.DefaultToolTimeout
```

## Provider 的具名类型

`Provider` 的声明位置从 `llm/constants.Provider` 改为 `constants.Provider`，底层仍为 string。这会改变 Go 的具名类型身份，所有 `llm.Client` 实现、配置字段、`WithProvider` 参数及模拟客户端方法都须同步使用新类型。

```go
import shared "github.com/kart-io/goagent/constants"

func (c *CustomClient) Provider() shared.Provider {
    return shared.Provider("minimax")
}

provider := shared.ProviderOpenAI // 推断为 shared.Provider
label := string(provider)         // 传给 string 参数或赋给 string 变量
custom := shared.Provider("offline-demo")
```

8个内置枚举 `ProviderOpenAI`、`ProviderGemini`、`ProviderDeepSeek`、`ProviderOllama`、`ProviderSiliconFlow`、`ProviderKimi`、`ProviderCustom`、`ProviderAnthropic` 均显式声明为 `Provider`，字符串值不变。Provider 取值空间保持开放，自定义提供商不受枚举集合限制。

原中央枚举是 untyped string；现在直接使用中央枚举时，传给 string 参数、赋给 string 变量或与 string 变量比较，需要显式 `string(...)` 转换。原来使用旧包 typed 枚举的代码保持同样的类型约束，但必须更新导入路径和方法签名。

## 错误与协议字段

错误码、分类、错误提示、错误上下文字段和哨兵使用 `errors` 包，详见[错误处理迁移指南](ERROR_HANDLING_MIGRATION.md)。`FieldError`、`StreamEventError`、`StatusError` 和 HTTP 状态码是协议值，继续在 `constants` 中。
