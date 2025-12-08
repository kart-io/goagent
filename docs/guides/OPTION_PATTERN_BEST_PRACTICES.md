# Option Pattern Best Practices

This guide outlines the best practices for using the Option pattern within the `goagent` framework. The Option pattern is the standard way to configure Agents, Tools, and other components in `goagent`.

## Why Option Pattern?

The Option pattern provides several benefits:
1.  **Encapsulation**: It keeps configuration internal to the package while allowing external modification.
2.  **Extensibility**: New options can be added without breaking existing APIs.
3.  **Readability**: Configuration is explicit and self-documenting.
4.  **Default Values**: It allows for sensible defaults while enabling customization.

## Naming Conventions

*   **Constructors**: Use `New<Component>WithOptions(...)` or `New<Component>(..., options ...<Component>Option)`.
*   **Option Functions**: Prefix option functions with `With` (e.g., `WithModel`, `WithTemperature`).
*   **Option Type**: Name the function type `<Component>Option` (e.g., `AgentOption`, `ToolOption`).
*   **Config Struct**: Name the configuration struct `<Component>Config` or `<Component>Options`.

## Implementation Guide

### 1. Define the Options Struct

```go
type MyComponentOptions struct {
    MaxRetries int
    Timeout    time.Duration
    Logger     logger.Logger
}

func DefaultMyComponentOptions() MyComponentOptions {
    return MyComponentOptions{
        MaxRetries: 3,
        Timeout:    30 * time.Second,
        Logger:     logger.NoopLogger,
    }
}
```

### 2. Define the Option Function Type

```go
type MyComponentOption func(*MyComponentOptions)
```

### 3. Implement Option Functions

```go
func WithMaxRetries(retries int) MyComponentOption {
    return func(o *MyComponentOptions) {
        o.MaxRetries = retries
    }
}

func WithTimeout(timeout time.Duration) MyComponentOption {
    return func(o *MyComponentOptions) {
        o.Timeout = timeout
    }
}
```

### 4. Implement the Constructor

```go
func NewMyComponent(opts ...MyComponentOption) *MyComponent {
    // 1. Start with defaults
    options := DefaultMyComponentOptions()

    // 2. Apply options
    for _, opt := range opts {
        opt(&options)
    }

    // 3. Use options to build component
    return &MyComponent{
        retries: options.MaxRetries,
        timeout: options.Timeout,
    }
}
```

## Migration Guide

If you are migrating from struct-based configuration or multiple constructor arguments, follow these steps:
1.  Identify the parameters that are optional.
2.  Create the Options struct and Option function type.
3.  Rewrite the constructor to accept a variadic list of options.
4.  Mark the old constructor as deprecated if you wish to keep it for backward compatibility.

## Performance Considerations

*   **Inlining**: Small option functions should be inlineable.
*   **Allocation**: The Option pattern typically involves a small allocation for the closure, but this is negligible for initialization logic.
