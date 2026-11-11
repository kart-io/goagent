# interfaces 接口定义层

`interfaces` 是不依赖其他内部包的基础层，定义跨包共享的接口与数据类型。完整定义见 `go doc -all github.com/kart-io/goagent/interfaces`。

## 接口一览

| 接口 | 用途 | 仓库内的使用 / 实现 |
|---|---|---|
| `Tool` 及可选接口 | 工具执行 | `builder.WithTools`、`tools` 包的全部工具 |
| `DocumentRetriever` | 检索证据 | `builder.WithRetriever` |
| `VectorStore` | 向量检索 | 由 goagent-contrib/retrieval 的向量库适配实现 |

## Tool

```go
type Tool interface {
    Name() string
    Description() string
    Invoke(ctx context.Context, input *ToolInput) (*ToolOutput, error)
    ArgsSchema() string // JSON Schema
}
```

| 可选接口 | 方法 | 含义 |
|---|---|---|
| `ValidatableTool` | `Validate(ctx, *ToolInput) error` | 执行前自定义校验 |
| `ArgumentErrorTool` | `ReturnsArgumentErrors() bool` | 参数错误作为结果交还模型改正 |
| `ConcurrentCallTool` | `SupportsConcurrentCalls() bool` | 允许同一轮多个调用并发执行 |
| `DurableCallTool` | `SupportsDurableCalls() bool` | 按 `ToolInput.CallID` 记录并回放已完成结果 |

包装工具时必须显式转发这些可选接口，否则视为未声明。

```go
type ToolInput struct {
    CallID       string // 宿主分配的稳定子操作 ID，不从模型参数读取
    ExecutionKey string // 宿主执行键，副作用服务据此实现幂等
    Args         map[string]interface{}
    Context      context.Context
    CallerID     string
    TraceID      string
}

type ToolOutput struct {
    Result            interface{}
    Success           bool
    Error             string
    Metadata          map[string]interface{}
    AdditionalContext []string          // Builder 在本批工具结果后作为 user 消息注入
    Labels            map[string]string // 预留，目前不传播
}
```

另有 `ToolCall`（调用记录）。

## 检索

```go
type DocumentRetriever interface {
    GetRelevantDocuments(ctx context.Context, query string) ([]*Document, error)
}
```

- `Document`：`ID`、`PageContent`、`Metadata`、`Embedding`、`Score`，方法 `Clone`、`GetMetadata`、`SetMetadata`、`String`。
- `RetrievalSource`：`Reference`、`ID`、`Source`、`Score`，标识实际送入模型的资料，不代表已核验引用。

## 其他类型

- `TokenUsage`：`PromptTokens`、`CompletionTokens`、`TotalTokens`、`CachedTokens`，方法 `Add`、`IsEmpty`。`llm.CompletionResponse.Usage` 与 Builder 的 `AgentOutput.TokenUsage` 使用此类型。

## 常量

- 消息角色：`RoleSystem`、`RoleUser`、`RoleAssistant`、`RoleFunction`、`RoleTool`。
- `field_constants.go`、`status_constants.go`、`http_constants.go`、`constants.go` 定义字段名、状态值、HTTP 相关等字符串常量。

## 代码结构

```text
interfaces/
├── doc.go               # 包文档
├── tool.go              # Tool 及可选接口、ToolInput/ToolOutput/ToolCall
├── store.go             # VectorStore、Document
├── retrieval.go         # DocumentRetriever
├── retrieval_source.go  # RetrievalSource
├── token.go             # TokenUsage
└── *_constants.go       # 字符串常量
```

## 相关包

- [core](../core/) - 执行契约与状态
- [builder](../builder/) - Agent 构建器
- [memory](../memory/) - 记忆实现
- [tools](../tools/) - 工具系统
