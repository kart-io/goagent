# Option Pattern Best Practices

[English](OPTION_PATTERN_BEST_PRACTICES.md) | [中文](OPTION_PATTERN_BEST_PRACTICES_CN.md)

Components in `goagent` with many optional settings are configured with functional options. Agents are the exception: `builder.AgentBuilder` uses chained `With*` methods (see the [Builder API Reference](BUILDER_API_REFERENCE.md)).

## Existing Options

| Option type | Applies to | Constructors |
|---|---|---|
| `llm.ClientOption` | `*llm.LLMOptions` | `providers.NewOpenAIWithOptions`, `NewDeepSeekWithOptions`, `NewGeminiWithOptions`, etc. |
| `remote.Option` | MCP tool adaptation | `remote.Tools` (`mcp/remote`) |
| `LoggingOption`, `CachingOption`, `RateLimitOption` (`tools/middleware`) | Tool middleware | `NewLoggingMiddleware`, `NewCachingMiddleware`, `NewRateLimitMiddleware` |

```go
client, err := providers.NewOpenAIWithOptions(
    llm.WithModel("gpt-4o-mini"),
    llm.WithTemperature(0.2),
    llm.WithTimeout(30*time.Second),
)

cached := middleware.NewCachingMiddleware(
    middleware.WithTTL(10*time.Minute),
)
```

## Naming Conventions

- **Constructors**: use `New<Component>WithOptions(opts ...)` when all parameters are optional; otherwise use `New<Component>(required params..., opts ...<Component>Option)`.
- **Option functions**: start with `With` (`WithModel`, `WithMaxConcurrency`).
- **Option types**: `<Component>Option`, i.e. `func(*<Config>)`.
- **Config structs**: `<Component>Config` or `<Component>Options`.

## Implementation

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
    options := ComponentOptions{MaxRetries: 3, Timeout: 30 * time.Second} // Fill in defaults first
    for _, opt := range opts {
        opt(&options)
    }
    return &Component{retries: options.MaxRetries, timeout: options.Timeout}
}
```

- Set defaults first, then apply options; options passed later win.
- Validate the final configuration in the constructor and return an error on failure; do not panic.
- When converting a constructor to the option style, replace it directly; the project does not keep deprecated constructors for compatibility.

## Performance

Option closures are allocated only once, at construction time; do not put them on hot paths such as per-request execution.
