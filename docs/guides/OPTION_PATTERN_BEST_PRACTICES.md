# 选项模式最佳实践

`goagent` 中可选配置较多的组件用函数式选项配置。Agent 例外：`builder.AgentBuilder` 使用链式 `With*` 方法（见 [Builder API 参考](BUILDER_API_REFERENCE.md)）。

## 现有选项

| 选项类型 | 作用对象 | 构造函数 |
|---|---|---|
| `llm.ClientOption` | `*llm.LLMOptions` | `providers.NewOpenAIWithOptions`、`NewDeepSeekWithOptions`、`NewGeminiWithOptions` 等 |
| `tools.ExecutorOption` | `*tools.ToolExecutor` | `tools.NewToolExecutor` |
| `tools.ShardedCacheOption` | `*tools.ShardedCacheConfig` | `tools.NewShardedToolCacheWithOptions` |
| `memory.MemoryOption` | `*memory.HierarchicalMemory` | `memory.NewHierarchicalMemoryWithContext` |
| `remote.Option` | MCP 工具适配 | `remote.Tools`（`mcp/remote`） |
| `LoggingOption`、`CachingOption`、`RateLimitOption`（`tools/middleware`） | 工具中间件 | `NewLoggingMiddleware`、`NewCachingMiddleware`、`NewRateLimitMiddleware` |

```go
client, err := providers.NewOpenAIWithOptions(
    llm.WithModel("gpt-4o-mini"),
    llm.WithTemperature(0.2),
    llm.WithTimeout(30*time.Second),
)

executor := tools.NewToolExecutor(
    tools.WithMaxConcurrency(10),
    tools.WithTimeout(30*time.Second),
)
```

## 命名约定

- **构造函数**：参数全部可选时用 `New<Component>WithOptions(opts ...)`，否则用 `New<Component>(必填参数..., opts ...<Component>Option)`。
- **选项函数**：以 `With` 开头（`WithModel`、`WithMaxConcurrency`）。
- **选项类型**：`<Component>Option`，即 `func(*<Config>)`。
- **配置结构体**：`<Component>Config` 或 `<Component>Options`。

## 实现

```go
type ComponentOptions struct {
    MaxRetries int
    Timeout    time.Duration
}

type ComponentOption func(*ComponentOptions)

func WithMaxRetries(n int) ComponentOption {
    return func(o *ComponentOptions) { o.MaxRetries = n }
}

func WithTimeout(d time.Duration) ComponentOption {
    return func(o *ComponentOptions) { o.Timeout = d }
}

func NewComponent(opts ...ComponentOption) *Component {
    options := ComponentOptions{MaxRetries: 3, Timeout: 30 * time.Second} // 先填默认值
    for _, opt := range opts {
        opt(&options)
    }
    return &Component{retries: options.MaxRetries, timeout: options.Timeout}
}
```

- 先设默认值再应用选项，后传的选项生效。
- 在构造函数中校验最终配置，出错时返回 error，不要 panic。
- 把构造函数改为选项式时直接替换，项目不为兼容保留废弃的构造函数。

## 性能

选项闭包只在构造时分配一次；不要放在每次请求执行这样的热路径上。
