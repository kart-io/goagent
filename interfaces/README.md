# interfaces 接口定义层

`interfaces` 是不依赖其他内部包的基础层，定义跨包共享的接口与数据类型。完整定义见 `go doc -all github.com/kart-io/goagent/interfaces`。

## 接口一览

| 接口 | 用途 | 仓库内的使用 / 实现 |
|---|---|---|
| `Tool` 及可选接口 | 工具执行 | `builder.WithTools`、`tools` 包的全部工具 |
| `MemoryManager` | 对话历史、案例、键值记忆 | `builder.WithMemory`；实现 `memory.InMemoryManager` |
| `DocumentRetriever` | 检索证据 | `builder.WithRetriever` |
| `Lifecycle` 及 `LifecycleAware`、`Reloadable`、`DependencyAware`、`LifecycleManager` | 组件生命周期 | `core.BaseLifecycle`、`core.DefaultLifecycleManager` |
| `ToolExecutor` | 按名称执行工具 | 仅定义，仓库内无实现 |
| `VectorStore`、`Store`、`ConversationMemory` | 向量检索、键值存储、对话存储 | 仅定义；`memory` 与 `store` 包使用各自的接口 |
| `Checkpointer` | 按检查点 ID 保存状态 | 仅定义；Builder 使用 `core/checkpoint.Checkpointer` |
| `ReasoningPattern` | 推理策略 | 仅定义 |
| `Agent`、`Runnable` | 旧的消息式执行协议 | 已 Deprecated，新代码使用 `core.Runnable[I, O]` 或 Builder 的 `AsRunnable` |

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

另有 `ToolCall`（调用记录）与 `ToolResult`（`ToolExecutor` 的结果）。

## 记忆与检索

```go
type MemoryManager interface {
    AddConversation(ctx context.Context, conv *Conversation) error
    GetConversationHistory(ctx context.Context, sessionID string, limit int) ([]*Conversation, error)
    ClearConversation(ctx context.Context, sessionID string) error
    AddCase(ctx context.Context, caseMemory *Case) error
    SearchSimilarCases(ctx context.Context, query string, limit int) ([]*Case, error)
    Store(ctx context.Context, key string, value interface{}) error
    Retrieve(ctx context.Context, key string) (interface{}, error)
    Delete(ctx context.Context, key string) error
    Clear(ctx context.Context) error
}

type DocumentRetriever interface {
    GetRelevantDocuments(ctx context.Context, query string) ([]*Document, error)
}
```

- `Conversation`：`ID`、`SessionID`、`Role`、`Content`、`Timestamp`、`Metadata`。
- `Case`：`ID`、`Title`、`Description`、`Problem`、`Solution`、`Category`、`Tags`、`Embedding`、`Similarity`、`CreatedAt`、`UpdatedAt`、`Metadata`。
- `Document`：`ID`、`PageContent`、`Metadata`、`Embedding`、`Score`，方法 `Clone`、`GetMetadata`、`SetMetadata`、`String`。
- `RetrievalSource`：`Reference`、`ID`、`Source`、`Score`，标识实际送入模型的资料，不代表已核验引用。

## 生命周期

```go
type Lifecycle interface {
    Init(ctx context.Context, config interface{}) error
    Start(ctx context.Context) error
    Stop(ctx context.Context) error
    HealthCheck(ctx context.Context) HealthStatus
}
```

- `LifecycleState`：`StateUninitialized`、`StateInitialized`、`StateStarting`、`StateRunning`、`StateStopping`、`StateStopped`、`StateFailed`。
- `HealthState`：`HealthHealthy`、`HealthDegraded`、`HealthUnhealthy`、`HealthUnknown`。
- `HealthStatus`：`State`、`Message`、`Details`、`LastChecked`、`ComponentName`；构造函数 `NewHealthStatus`、`NewHealthyStatus`、`NewDegradedStatus`、`NewUnhealthyStatus`，方法 `IsHealthy`、`IsOperational`。

## 其他类型

- `TokenUsage`：`PromptTokens`、`CompletionTokens`、`TotalTokens`、`CachedTokens`，方法 `Add`、`IsEmpty`。`llm.CompletionResponse.Usage` 与 Builder 的 `AgentOutput.TokenUsage` 使用此类型。
- `State`：`map[string]interface{}`，用于旧消息式协议；Builder 与 `core` 使用 `core/state.State` 接口。
- 旧协议类型：`Input`、`Output`、`Message`、`StreamChunk`、`Plan`、`Step`。
- 推理类型：`ReasoningInput`、`ReasoningOutput`、`ReasoningStep`、`ReasoningChunk`、`ReasoningToolCall`、`ThoughtNode`、`ProgramCode`、`SkeletonPoint`，策略常量 `StrategyDepthFirst`、`StrategyBreadthFirst`、`StrategyBeamSearch`、`StrategyMonteCarlo`、`StrategyGreedy`。
- 检查点类型：`Checkpoint`、`CheckpointMetadata`。

## 常量

- 消息角色：`RoleSystem`、`RoleUser`、`RoleAssistant`、`RoleFunction`、`RoleTool`。
- `field_constants.go`、`status_constants.go`、`http_constants.go`、`constants.go` 定义字段名、状态值、HTTP 相关等字符串常量。

## 代码结构

```text
interfaces/
├── doc.go               # 包文档
├── agent.go             # Agent、Runnable 与旧消息式类型
├── tool.go              # Tool 及可选接口、ToolInput/ToolOutput/ToolCall/ToolResult、ToolExecutor
├── memory.go            # MemoryManager、ConversationMemory、Conversation、Case
├── store.go             # VectorStore、Store、Document
├── retrieval.go         # DocumentRetriever
├── retrieval_source.go  # RetrievalSource
├── checkpoint.go        # Checkpointer、Checkpoint、CheckpointMetadata
├── lifecycle.go         # Lifecycle 与健康状态
├── reasoning.go         # ReasoningPattern 与推理类型
├── token.go             # TokenUsage
└── *_constants.go       # 字符串常量
```

## 相关包

- [core](../core/) - 执行契约与状态
- [builder](../builder/) - Agent 构建器
- [memory](../memory/) - 记忆实现
- [tools](../tools/) - 工具系统
