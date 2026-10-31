# GoAgent 核心 API 参考

本文概述 `builder` 与 `core` 的主要 API。完整签名以 Go 文档为准：

```bash
go doc -all github.com/kart-io/goagent/builder
go doc -all github.com/kart-io/goagent/core
```

执行入口与扩展点的行为契约见 [执行入口与扩展契约](../guides/EXTENSION_CONTRACT.md)，宿主装配见 [宿主接入指南](../guides/HOST_INTEGRATION.md)。

## Builder

```go
func NewSimpleBuilder(llmClient llm.Client) *SimpleAgentBuilder          // = AgentBuilder[any, *core.AgentState]
func NewAgentBuilder[C any, S core.State](llmClient llm.Client) *AgentBuilder[C, S]

func (b *AgentBuilder[C, S]) BuildContext(ctx context.Context) (*ConfigurableAgent[C, S], error) // 宿主推荐
func (b *AgentBuilder[C, S]) Build() (*ConfigurableAgent[C, S], error)                          // 使用 Background
```

Builder 是可变对象，不要并发配置。所有 `With*` 方法返回同一个 Builder，可链式调用。

### 配置方法

| 类别 | 方法 |
|---|---|
| 提示与输出 | `WithSystemPrompt`、`WithOutputFormat(OutputFormat)`、`WithCustomOutputFormat(prompt)`、`WithPromptSections`、`WithPromptContexts` |
| 生成参数 | `WithMaxTokens`、`WithTemperature`、`WithMaxIterations`、`WithTimeout` |
| 工具 | `WithTools(...interfaces.Tool)`、`WithReturnDirectTools(names...)`、`WithParallelToolCalls(n)`、`WithEmptyAnswerRetries(n)` |
| 循环钩子 | `WithPreStep`、`WithIdentifiedPreStep(identity, hook)`、`WithRequestHook`、`WithRequestError(maxRecoveries, hooks...)`、`WithTurnStopping`、`WithOffload(*contextpolicy.Offload)`、`WithTokenEstimator` |
| 会话与持久化 | `WithSessionID`、`WithSessionLog(session.Store)`、`WithDurableScope`、`WithCheckpointer(checkpoint.Checkpointer)`、`WithStore(store.Store)`、`WithState(S)`、`WithAutoSaveEnabled` |
| 记忆与检索 | `WithMemory(interfaces.MemoryManager)`、`WithMemoryFailurePolicy`、`WithMaxConversationHistory`、`WithRetriever(interfaces.DocumentRetriever, ...RetrievalConfig)` |
| 横切 | `WithMiddleware(...middleware.Middleware)`、`WithCallbacks(...core.Callback)`、`WithErrorHandler`、`WithMetadata`、`WithInputSerializer`、`WithVerbose`、`WithContext(C)` |
| 预设 | `ConfigureForChatbot`、`ConfigureForRAG`、`ConfigureForAnalysis` |

循环钩子、`WithReturnDirectTools`、`WithEmptyAnswerRetries`、`WithOffload` 只作用于原生工具流，要求客户端实现 `llm.ToolStreamClient`。`WithParallelToolCalls(n>=2)` 只对声明 `interfaces.ConcurrentCallTool` 的工具并发执行。`WithMiddleware` 的中间件见 [Middleware API](MIDDLEWARE_API.md)。

`OutputFormat` 取值：`OutputFormatDefault`（空字符串）、`OutputFormatPlainText`、`OutputFormatMarkdown`、`OutputFormatJSON`、`OutputFormatCustom`。

`AgentConfig` 保存上述配置，`DefaultAgentConfig()` 返回默认值；字段说明见 `go doc ./builder AgentConfig`。

快捷构造函数返回 `*SimpleAgent`：`QuickAgent(client, systemPrompt)`、`ChatAgent`、`RAGAgent`、`AnalysisAgent`、`ResearchAgent`、`MonitoringAgent`、`WorkflowAgent`。

### 模型请求恢复

```go
type RequestErrorHook func(ctx context.Context, failure *RequestFailure) (RequestErrorDecision, error)
func RetryTransient(retries int, backoff func(retry int) time.Duration) RequestErrorHook
```

`RequestFailure` 携带 `Step`、`Attempt`、`Err`、`Class`（`llm.ClassifiedError`）、`Messages` 和 `Emitted`。钩子返回 `RequestErrorDecision{Action: RequestRetry, ...}` 时可替换 `Messages`（压缩）、`Client`（回退）或设置 `Delay`；`RequestFail` 交给下一个钩子。`RetryTransient` 只原样重试 `ClassifyError` 判为可重试且尚未输出内容的失败，优先遵循 `Retry-After`，`backoff` 为 nil 时从 500ms 指数增长、上限 8s。

```go
agent, err := builder.NewSimpleBuilder(client).
    WithRequestError(3, builder.RetryTransient(3, nil)).
    BuildContext(ctx)
```

其他钩子类型：`PreStepHook func(ctx, *Step) (PreStepResult, error)`、`RequestHook func(ctx, *ModelRequest) error`、`TurnStoppingHook func(ctx, *Stopping) (continueWith string, err error)`。

## ConfigurableAgent

| 方法 | 说明 |
|---|---|
| `AsRunnable() *core.StreamRunnable[any, *AgentOutput]` | 推荐入口；`Invoke` / `Stream` / `Batch` 共享执行源，要求 `llm.StreamClient` |
| `ExecuteStream(ctx, input) (<-chan AgentEvent, error)` | 事件流；只有 `done` 事件的 `Output` 是最终结果 |
| `Execute(ctx, input)` | 同步执行，不隐式调用工具 |
| `ExecuteWithTools(ctx, input)` | 客户端支持结构化流时走统一路径，否则使用旧工具循环 |
| `Start(ctx, input string) *Handle` | 后台运行；`Handle` 提供 `Steer`、`Inject`、`Redirect`、`Followup`、`Interrupt`、`Wait`，只支持原生工具流 |
| `Initialize`、`Shutdown`、`GetState`、`GetMetrics` | 生命周期与诊断 |

`AgentOutput` 字段：`Result`、`State`、`Metadata`、`Duration`、`Timestamp`、`TokenUsage`。`AgentEvent.Type` 为 `sources`、`step`、`text`、`usage`、`error` 或 `done`；持久运行暂停时返回 `*WaitingError`（`errors.Is(err, ErrWaiting)`）。

### 示例

```go
client, err := providers.NewOpenAIWithOptions(
    llm.WithAPIKey(os.Getenv("OPENAI_API_KEY")),
    llm.WithModel("gpt-4o"),
)
if err != nil {
    return err
}

agent, err := builder.NewSimpleBuilder(client).
    WithSystemPrompt("你是一个有帮助的助手").
    WithTools(calculatorTool).
    WithMaxIterations(5).
    WithRequestError(3, builder.RetryTransient(3, nil)).
    BuildContext(ctx)
if err != nil {
    return err
}

out, err := agent.AsRunnable().Invoke(ctx, "计算 (3+4)*5")
if err != nil {
    return err
}
fmt.Println(out.Result)

// 流式：增量在 chunk.Event 中，Done 且无错误时 chunk.Data 是最终结果
chunks, err := agent.AsRunnable().Stream(ctx, "再算一次")
if err != nil {
    return err
}
for chunk := range chunks {
    if chunk.Error != nil {
        return chunk.Error
    }
    if chunk.Event != nil && chunk.Event.Type == "text" {
        fmt.Print(chunk.Event.Delta)
    }
    if chunk.Done {
        fmt.Println("\n最终结果：", chunk.Data.Result)
    }
}
```

## core 执行契约

```go
type Runnable[I, O any] interface {
    Invoke(ctx context.Context, input I) (O, error)
    Stream(ctx context.Context, input I) (<-chan StreamChunk[O], error)
    Batch(ctx context.Context, inputs []I) ([]O, error)
    Pipe(next Runnable[O, any]) Runnable[I, any]
    WithCallbacks(callbacks ...Callback) Runnable[I, O]
    WithConfig(config RunnableConfig) Runnable[I, O]
}

type StreamChunk[T any] struct {
    Data  T
    Error error
    Done  bool
    Event *StreamEvent // 可选增量事件：Type、Delta、Step、Tool、Sources、Usage
}
```

- `StreamRunnable[I, O]`（`NewStreamRunnable(mode, source)`）从同一执行源提供 `Invoke`、`Stream`、`Batch`、`Pipe`；`StreamMode()` 说明实际输出能力。
- `RunnableFunc`（`NewRunnableFunc`）、`RunnablePipe`（`NewRunnablePipe`）、`RunnableSequence` 用于组合；组合语义见 [扩展契约](../guides/EXTENSION_CONTRACT.md#pipe-与-sequence-的一致性修复)。
- `core.Agent` 是 `Runnable[*AgentInput, *AgentOutput]` 加 `Name`、`Description`、`Capabilities`；`BaseAgent`（`NewBaseAgent`）是基础实现。
- `Callback` / `BaseCallback`、`CallbackManager` 以及 `LoggingCallback`、`MetricsCallback`、`CostTrackingCallback` 提供生命周期通知。

`interfaces.Agent` 与 `interfaces.Runnable` 是旧的消息式协议，已标记 Deprecated；新代码使用 `core.Runnable[I, O]` 或 Builder 的 `AsRunnable`。

## State

`core.State` 是 `core/state.State` 的别名：

```go
type State interface {
    Get(key string) (interface{}, bool)
    Set(key string, value interface{})
    Update(updates map[string]interface{})
    Snapshot() map[string]interface{}
    Clone() State
    Delete(key string)
    Clear()
    Keys() []string
    Size() int
}
```

`core.NewAgentState()` 返回线程安全的 `*AgentState`，另有 `GetString`、`GetInt`、`GetBool`、`GetFloat64`、`GetMap`、`GetSlice` 等类型化读取方法。

## Checkpoint

Builder 的 `WithCheckpointer` 接收 `core/checkpoint.Checkpointer`，按线程（会话）ID 保存整份状态：

```go
type Checkpointer interface {
    Save(ctx context.Context, threadID string, state state.State) error
    Load(ctx context.Context, threadID string) (state.State, error)
    List(ctx context.Context) ([]CheckpointInfo, error)
    Delete(ctx context.Context, threadID string) error
    Exists(ctx context.Context, threadID string) (bool, error)
}
```

实现：`NewInMemorySaver()`、`NewDistributedCheckpointer(*DistributedCheckpointerConfig)`；`NewCheckpointerWithAutoCleanup` 为任意实现增加定期清理。

```go
cp := checkpoint.NewInMemorySaver()
st := core.NewAgentState()
st.Set("step", 5)
if err := cp.Save(ctx, "thread-123", st); err != nil {
    return err
}
loaded, err := cp.Load(ctx, "thread-123")
```

`interfaces.Checkpointer`（`SaveCheckpoint` / `LoadCheckpoint` / `ListCheckpoints` / `DeleteCheckpoint`）是另一套按检查点 ID 存取的接口，仓库内没有实现，Builder 也不使用它。持久执行（`core/durable`、`core/session`）的接入见 [宿主接入指南](../guides/HOST_INTEGRATION.md)。

## 相关文档

- [Tool API 参考](TOOL_API.md)
- [LLM API 参考](LLM_API.md)
- [Middleware API 参考](MIDDLEWARE_API.md)
- [快速入门](../guides/QUICKSTART.md)
