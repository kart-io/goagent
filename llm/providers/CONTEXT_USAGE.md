# Context Usage in LLM Providers

## Acceptable context.Background() Usage

The LLM provider implementations use `context.Background()` in specific scenarios where it is acceptable:

### 1. IsAvailable() Methods
**Pattern**: Health check operations
**Justification**: These are non-critical availability checks that should be independent of any specific request context. They have their own timeout and are used for monitoring/diagnostics.

**Example**:
```go
func (p *Provider) IsAvailable() bool {
    ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
    defer cancel()
    // Make minimal test request
}
```

### 2. ListModels() Methods
**Pattern**: Metadata fetching
**Justification**: Fetching available models is a setup/discovery operation independent of request processing.

**Example**:
```go
func (c *Client) ListModels() ([]string, error) {
    ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
    defer cancel()
    // Fetch model list
}
```

### 3. Close() Methods
**Pattern**: Cleanup operations
**Justification**: Resource cleanup during shutdown should complete independently of canceled request contexts.

**Example**:
```go
func (p *Provider) Close() error {
    ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
    defer cancel()
    // Close connections
}
```

## All Request Processing Uses Provided Context

All actual LLM request methods (Complete, Chat, Stream, GenerateWithTools, Embed) correctly use the context passed from the caller, allowing for proper:
- Request cancellation
- Timeout propagation
- Distributed tracing
- Request-scoped values

## Files Affected

The following provider files use `context.Background()` for the operations described above:

- anthropic.go
- cohere.go
- deepseek.go
- gemini.go
- huggingface.go
- kimi.go
- ollama.go
- openai.go
- siliconflow.go

All usage has been reviewed and deemed acceptable for the specific use cases.
