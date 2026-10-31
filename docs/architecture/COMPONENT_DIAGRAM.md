# GoAgent 组件关系图

图中的包、类型与方法均取自当前源码；只列出与组件关系有关的成员，完整签名以 `go doc` 为准。包级依赖图见 [架构概述](ARCHITECTURE.md#包依赖)。

## Builder 与宿主装配

```mermaid
classDiagram
    class AgentBuilder~C,S~ {
        <<builder>>
        +WithSystemPrompt(string)
        +WithTools(...interfaces.Tool)
        +WithMaxIterations(int)
        +WithMiddleware(...middleware.Middleware)
        +WithMemory(interfaces.MemoryManager)
        +WithRetriever(DocumentRetriever, ...RetrievalConfig)
        +WithSessionID(string)
        +WithSessionLog(session.Store)
        +WithDurableScope(string)
        +WithPreStep / WithIdentifiedPreStep
        +WithRequestHook / WithRequestError / WithTurnStopping
        +WithOffload(*contextpolicy.Offload)
        +Build() (*ConfigurableAgent, error)
        +BuildContext(ctx) (*ConfigurableAgent, error)
    }
    class ConfigurableAgent~C,S~ {
        <<builder>>
        +AsRunnable() *core.StreamRunnable[any, *AgentOutput]
        +ExecuteStream(ctx, input) (<-chan AgentEvent, error)
        +Execute(ctx, input) (*AgentOutput, error)
        +ExecuteWithTools(ctx, input) (*AgentOutput, error)
        +Start(ctx, input string) *Handle
        +Initialize(ctx) / Shutdown(ctx)
    }
    class Handle {
        <<builder>>
        +Steer(string) error
        +Inject(string) error
        +Followup(string) error
        +Redirect(string) error
        +Interrupt(error)
        +Wait(ctx) (*AgentOutput, error)
    }
    class Scope {
        <<execution>>
        +Bind(ctx) (context.Context, CancelFunc, error)
        +RunID() string
        +SessionID() string
    }
    class Config {
        <<execution>>
        RunID, SessionID string
        Journal durable.Store
        Budget *budget.Controller
        Timeout time.Duration
        Observer observe.Observer
        Tracer trace.Tracer
    }
    AgentBuilder --> ConfigurableAgent : Build / BuildContext
    ConfigurableAgent --> Handle : Start
    Config --> Scope : NewScope
    Scope ..> AgentBuilder : ConfigureBuilder(b, scope, agentID)
```

`SimpleAgentBuilder` 与 `SimpleAgent` 是 `C=any, S=*core.AgentState` 的别名，`NewSimpleBuilder` 创建前者。

## Runnable 组合

```mermaid
classDiagram
    class Runnable~I,O~ {
        <<interface core>>
        +Invoke(ctx, I) (O, error)
        +Stream(ctx, I) (<-chan StreamChunk[O], error)
        +Batch(ctx, []I) ([]O, error)
        +Pipe(Runnable[O, any]) Runnable[I, any]
        +WithCallbacks(...Callback) Runnable[I, O]
        +WithConfig(RunnableConfig) Runnable[I, O]
    }
    class StreamRunnable~I,O~ {
        <<core>>
        NewStreamRunnable(mode, StreamSource)
    }
    class RunnableFunc~I,O~
    class RunnablePipe~I,M,O~
    class RunnableSequence
    class Graph~T~ {
        <<workflow>>
        +AddNode / AddRunnable / AddEdge / AddBranch / AddParallel
        +Compile(ctx) (*core.StreamRunnable[T, T], error)
        +CompileDurable(ctx, DurableConfig, ...) (*DurableGraph[T], error)
    }
    class DurableGraph~T~ {
        <<workflow>>
        +AsRunnable() *core.StreamRunnable[RunRequest[T], *GraphSnapshot[T]]
        +AsChildRunnable() *core.RunnableFunc[T, T]
        +Run(ctx, id, initial, approvalKey)
        +Snapshot(ctx, id) / Diagnose(ctx, id, ...)
    }
    Runnable <|.. StreamRunnable
    Runnable <|.. RunnableFunc
    Runnable <|.. RunnablePipe
    Runnable <|.. RunnableSequence
    Graph --> StreamRunnable : Compile
    Graph --> DurableGraph : CompileDurable
```

`workflow.Adapt` / `AdaptStream` 把任意 `core.Runnable[I, O]`（如 `agent.AsRunnable()`）适配为图节点；`tools.NewRunnableTool` 反向把 Runnable 包装为工具。

## 工具系统

```mermaid
classDiagram
    class Tool {
        <<interface interfaces>>
        +Name() string
        +Description() string
        +ArgsSchema() string
        +Invoke(ctx, *ToolInput) (*ToolOutput, error)
    }
    class ValidatableTool {
        <<interface interfaces>>
        +Validate(ctx, *ToolInput) error
    }
    class DurableCallTool {
        <<interface interfaces>>
        +SupportsDurableCalls() bool
    }
    class ConcurrentCallTool {
        <<interface interfaces>>
        +SupportsConcurrentCalls() bool
    }
    class Registry {
        <<tools>>
        +Register(Tool) error
        +Get(name) Tool
        +List() []Tool
    }
    class Dispatcher {
        <<tools>>
        +Invoke(ctx, ToolRequest) (*ToolOutput, error)
        +AsRunnable() *core.StreamRunnable[ToolRequest, *ToolOutput]
        +Tools() []Tool
    }
    class DispatchConfig {
        <<tools>>
        MaxAttempts, MaxConcurrency, MaxAdmissionConcurrency, MaxQueued int
        Timeout time.Duration
        Authorize func
        OnResult func
        ReturnArgumentErrors bool
        Pipeline *Pipeline
    }
    class Pipeline {
        <<tools>>
        Pre []PreHook
        Approver Approver
        Guards []Guard
        Around []Around
        Post []PostHook
    }
    Tool <|-- ValidatableTool
    Tool <|-- DurableCallTool
    Tool <|-- ConcurrentCallTool
    Registry o-- Tool
    Dispatcher --> Registry : NewDispatcher 快照
    Dispatcher --> DispatchConfig
    DispatchConfig --> Pipeline
    Dispatcher ..> DurableCallTool : Tools() 返回的工具实现
```

内置实现：`tools.FunctionTool`（`NewFunctionTool` / `NewTypedFunctionTool` / `NewFunctionToolBuilder`）、`tools/compute`、`tools/http`、`tools/search`、`tools/shell`、`tools/spill`、`contextpolicy.Offload`（读取工具）、`mcp/remote.Tools`（MCP 远程工具）。`tools/policy.Policy` 作为 `Pipeline` 的调用前钩子使用；`tools/middleware` 提供日志、缓存、限流工具中间件。

## LLM 客户端

```mermaid
classDiagram
    class Client {
        <<interface llm>>
        +Complete(ctx, *CompletionRequest) (*CompletionResponse, error)
        +Chat(ctx, []Message) (*CompletionResponse, error)
        +Provider() constants.Provider
        +IsAvailable() bool
    }
    class StreamClient {
        <<interface llm>>
        +CompleteStream(ctx, *CompletionRequest) (<-chan *StreamChunk, error)
        +ChatStream(ctx, []Message) (<-chan *StreamChunk, error)
    }
    class ToolStreamClient {
        <<interface llm>>
        +CompleteToolsStream(ctx, *CompletionRequest, []Tool) (<-chan *StreamChunk, error)
    }
    class ToolCallingClient {
        <<interface llm>>
        +GenerateWithTools(...)
    }
    class OpenAIProvider
    class DeepSeekProvider
    class GeminiProvider
    Client <|-- StreamClient
    StreamClient <|-- ToolStreamClient
    ToolStreamClient <|.. OpenAIProvider
    ToolStreamClient <|.. DeepSeekProvider
    ToolStreamClient <|.. GeminiProvider
```

能力探测用 `llm.HasStreaming`、`HasToolStreaming`、`HasToolCalling` 等函数。Builder 的原生工具循环要求 `ToolStreamClient`；只实现 `ToolCallingClient` 的客户端走旧的工具选择路径，不支持持久日志与根预算。

## 持久执行、会话与预算

```mermaid
classDiagram
    class DurableStore {
        <<interface core/durable.Store>>
        +Lock(ctx, id) (func() error, error)
        +Load(ctx, id) (*Snapshot, error)
        +Save(ctx, id, *Snapshot) error
    }
    class DurableFileStore {
        <<core/durable.FileStore>>
    }
    class Runner {
        <<core/durable>>
        Store, Version, Steps
        +Run(ctx, id, initial, approvalKey)
        +Diagnose(ctx, id, ...)
    }
    class SessionStore {
        <<interface core/session.Store>>
        +Append(ctx, id, expected, ...Event) (int64, error)
        +Read(ctx, id, after) ([]Event, error)
        +List(ctx) / Fork(...) / Lock(ctx, id, name)
    }
    class MemoryStore {
        <<core/session>>
    }
    class SessionFileStore {
        <<core/session.FileStore>>
    }
    class Controller {
        <<core/budget>>
        New(Config) / NewDurable(Config, durable.Store)
        +Snapshot(ctx)
    }
    DurableStore <|.. DurableFileStore
    SessionStore <|.. MemoryStore
    SessionStore <|.. SessionFileStore
    Runner --> DurableStore
    SessionStore ..> DurableStore : session.DurableStore(store, id)
    Controller ..> DurableStore : NewDurable
```

`durable.Do(ctx, id, input, policy, run)` 是 Agent 持久子操作（模型回合、工具调用、循环扩展点）的共同入口，ctx 上须有 `WithJournal` 与 `WithExecutionKey`；`workflow.DurableGraph` 用 `DurableConfig` 中的 `durable.Store` 保存图快照。`testing/replay` 读取会话日志做回放回归。

## 相关文档

- [架构概述](ARCHITECTURE.md)
- [导入层级说明](IMPORT_LAYERING.md)
- [设计概述](../design/DESIGN_OVERVIEW.md)
