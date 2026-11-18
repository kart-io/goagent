# interfaces: Interface Definition Layer

[English](README.md) | [中文](README_CN.md)

`interfaces` is a base layer that depends on no other internal package and defines the interfaces and data types shared across packages. For the full definitions see `go doc -all github.com/kart-io/goagent/interfaces`.

## Interfaces at a Glance

| Interface | Purpose | Use / implementation in this repository |
|---|---|---|
| `Tool` and optional interfaces | Tool execution | `builder.WithTools`, all tools in the `tools` package |
| `DocumentRetriever` | Evidence retrieval | `builder.WithRetriever` |
| `VectorStore` | Vector search | Implemented by the vector store adapters in goagent-contrib/retrieval |

## Tool

```go
type Tool interface {
    Name() string
    Description() string
    Invoke(ctx context.Context, input *ToolInput) (*ToolOutput, error)
    ArgsSchema() string // JSON Schema
}
```

| Optional interface | Method | Meaning |
|---|---|---|
| `ValidatableTool` | `Validate(ctx, *ToolInput) error` | Custom validation before execution |
| `ArgumentErrorTool` | `ReturnsArgumentErrors() bool` | Argument errors are returned to the model as results so it can correct them |
| `ConcurrentCallTool` | `SupportsConcurrentCalls() bool` | Allows multiple calls in the same turn to run concurrently |
| `DurableCallTool` | `SupportsDurableCalls() bool` | Records completed results by `ToolInput.CallID` and replays them |

A wrapping tool must forward these optional interfaces explicitly; otherwise they are treated as not declared.

```go
type ToolInput struct {
    CallID       string // stable child operation ID assigned by the host, never read from model arguments
    ExecutionKey string // host execution key; side-effecting services implement idempotency with it
    Args         map[string]interface{}
    CallerID     string
    TraceID      string
}

type ToolOutput struct {
    Result            interface{}
    Success           bool
    Error             string
    Metadata          map[string]interface{}
    AdditionalContext []string          // injected by Builder as a user message after this batch of tool results
    Labels            map[string]string // reserved, not propagated yet
}
```

There is also `ToolCall` (a call record).

## Retrieval

```go
type DocumentRetriever interface {
    GetRelevantDocuments(ctx context.Context, query string) ([]*Document, error)
}
```

- `Document`: `ID`, `PageContent`, `Metadata`, `Embedding`, `Score`; methods `Clone`, `GetMetadata`, `SetMetadata`, `String`.
- `RetrievalSource`: `Reference`, `ID`, `Source`, `Score`; identifies the material actually sent to the model, not a verified citation.

## Other Types

- `TokenUsage`: `PromptTokens`, `CompletionTokens`, `TotalTokens`, `CachedTokens`; methods `Add`, `IsEmpty`. Used by `llm.CompletionResponse.Usage` and the Builder's `AgentOutput.TokenUsage`.

## Constants

This package defines no string constants. The message roles `RoleSystem`, `RoleUser`, `RoleAssistant`, and the constants for field names, status values and HTTP, live in the [constants](../constants/README.md) package.

## Code Structure

```text
interfaces/
├── doc.go               # package documentation
├── tool.go              # Tool and optional interfaces, ToolInput/ToolOutput/ToolCall
├── store.go             # VectorStore, Document
├── retrieval.go         # DocumentRetriever
├── retrieval_source.go  # RetrievalSource
└── token.go             # TokenUsage
```

## Related Packages

- [core](../core/) - execution contract and state
- [builder](../builder/) - Agent builder
- [memory](../memory/) - memory implementations
- [tools](../tools/) - tool system
