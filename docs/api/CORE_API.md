<!-- generated-by: gsd-doc-writer -->
# GoAgent 核心 API 参考

新编排以 `core.Runnable[I, O]` 为统一入口。Builder Agent 通过 `AsRunnable()` 接入，工作流用 `workflow.Graph[T]` 组合普通代码、工具与 Agent。本文依据当前源码；旧 `interfaces.Agent/Runnable` 仅供存量兼容，不作为新开发入口。

## 统一执行接口

签名见 [core/runnable.go](../../core/runnable.go)：

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
    Event *StreamEvent
}
```

`Invoke` 返回最终输出；`Stream` 提供中间事件及终态；`Batch` 按输入索引返回结果，错误与部分结果均须检查。`MaxConcurrency` 控制 Batch 并发，不是进程全局执行额度。

`Pipe` 接口中的下游输出为 `any`；保留中间与最终类型时可用 `core.NewRunnablePipe[I, M, O](first, second)`。`core.NewRunnableSequence` 则组合 `Runnable[any, any]`。不同输入输出类型不会自动做字段映射。

`core.NewStreamRunnable[I,O](mode, source)` 从同一执行源提供 Invoke/Stream/Batch；源码见 [stream_runnable.go](../../core/stream_runnable.go)。可选的 `core.StreamCapability` 以 `StreamMode()` 声明 `StreamModeFinal`、`StreamModeSteps` 或 `StreamModeTokens`；有 Stream 方法不表示模型能逐 Token 输出。

### 流与取消契约

- 普通 StreamRunnable 的最终结果来自 `Done=true` 且 `Error=nil` 的块，中间 Data/Event 不能当作成功完成。
- 消费方检查启动错误、块错误与最终 `ctx.Err()`；提前停止读取时必须取消上下文。
- 组合器验证终态和通道关闭，缺失/重复终态、终态后的数据或错误都会失败，不进入下游。
- 忽略 context 的第三方调用不能被强制终止。最终块的业务含义仍由输出类型决定，持久工作流的审批等待见下文。

`RunnableConfig` 有 Callbacks、Tags、Metadata、MaxConcurrency、RetryPolicy 字段，但不能据 RetryPolicy 字段存在就认为所有 Runnable 自动重试。Builder 模型请求的恢复使用 `WithRequestError`，入口约束见[执行契约](../guides/EXECUTION_CONTRACT.md)。

## Agent 与 Builder 输出的区别

| 类型 | 输入/输出 | 用途 |
|---|---|---|
| `core.Agent` | 嵌入 `Runnable[*core.AgentInput,*core.AgentOutput]`，另有 Name/Description/Capabilities | 核心 Agent 接口 |
| `builder.ConfigurableAgent[C,S]` | AsRunnable 返回 `*core.StreamRunnable[any,*builder.AgentOutput]` | 可配置 Builder Agent |
| `interfaces.Agent/Runnable` | 旧 interfaces.Input/Output 消息协议 | 已弃用的兼容接口 |

`core.AgentInput` 含 Task、Instruction、Context、Options、SessionID、Timestamp。`core.AgentOutput` 含 Result、Status、步骤、工具调用与用量，见 [core/agent.go](../../core/agent.go)。

Builder 的输出是另一种类型，不能与 core.AgentOutput 混用：

```go
type AgentOutput struct { // 位于 builder 包
    Result     interface{}
    State      core.State
    Metadata   map[string]interface{}
    Duration   time.Duration
    Timestamp  time.Time
    TokenUsage *interfaces.TokenUsage
}
```

Builder 没有在此输出中定义 Status、Steps 或 ToolCalls 字段。通过 error、终态和业务结果判断完成；工具轨迹使用事件或观察器。源码见 [configurable_agent.go](../../builder/configurable_agent.go)、[runnable.go](../../builder/runnable.go)。

### 可独立运行的离线例子

保存为 main.go，在依赖当前 GoAgent 的模块中运行 `go run .`；使用模拟模型，无需密钥。

```go
package main

import (
    "context"
    "fmt"
    "time"

    "github.com/kart-io/goagent/builder"
    "github.com/kart-io/goagent/llm"
    "github.com/kart-io/goagent/workflow"
)

type State struct {
    Question string
    Answer   string
}

func main() {
    if err := run(); err != nil {
        panic(err)
    }
}

func run() error {
    ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
    defer cancel()
    agent, err := builder.NewSimpleBuilder(llm.NewMockStreamClient()).BuildContext(ctx)
    if err != nil {
        return err
    }
    node, err := workflow.Adapt(agent.AsRunnable(),
        func(s State) (any, error) { return s.Question, nil },
        func(s State, out *builder.AgentOutput) (State, error) {
            s.Answer = fmt.Sprint(out.Result)
            return s, nil
        },
    )
    if err != nil {
        return err
    }
    graph := workflow.NewGraph[State]()
    if err := graph.AddRunnable("answer", node); err != nil {
        return err
    }
    if err := graph.AddEdge(workflow.START, "answer"); err != nil {
        return err
    }
    if err := graph.AddEdge("answer", workflow.END); err != nil {
        return err
    }
    compiled, err := graph.Compile(ctx)
    if err != nil {
        return err
    }
    out, err := compiled.Invoke(ctx, State{Question: "演示 Agent 作为图节点"})
    if err != nil {
        return err
    }
    fmt.Println(out.Answer)
    return nil
}
```

## 类型化工作流

接口见 [graph.go](../../workflow/graph.go)、[adapter.go](../../workflow/adapter.go)、[parallel.go](../../workflow/parallel.go)。

| API | 作用与边界 |
|---|---|
| `workflow.NewGraph[T](configs ...workflow.Config)` | 创建可变构图器；T 可以是任意业务状态，不要求实现 core.State |
| `AddNode(name, Node[T])` | 普通节点签名为 `func(context.Context,T)(T,error)` |
| `AddRunnable(name, core.Runnable[T,T])` | 接入已有 Runnable |
| `workflow.Adapt[S,I,O](runnable, input, output)` | 显式映射 Runnable 输入与最终输出 |
| `AddEdge(from, to)` | 配置唯一普通后继 |
| `AddBranch(from, router, targets...)` | Router 从已完成状态选择一个预先声明的目标 |
| `AddParallel(name, ParallelConfig[T])` | 显式 fork/join，宿主提供 Clone 和 Merge |
| `Compile(ctx)` | 校验并生成 `*core.StreamRunnable[T,T]` |
| `CompileDurable(ctx, DurableConfig, opts...)` | 生成可恢复的 `*workflow.DurableGraph[T]` |

图允许有退出路径的循环，以 Config.MaxSteps 限制执行；不能证明路由一定选择退出。普通多入边不构成等待所有分支的 AND 汇合，须用 AddParallel。并行分支的可变状态由宿主深复制，合并顺序为注册顺序；没有自动字段 reducer 或任意动态 Send 调度协议。

持久图的 Router 须为纯函数；模型请求放在 Agent 节点，路由仅检查已保存决策。路由保存前崩溃可能再次调用 Router。完整约束见 [workflow 指南](../../workflow/README.md)。

## 持久执行与业务等待

`workflow.DurableConfig` 指定 `Store durable.Store`、`Version string`、`ApprovalNodes []string`。WithBeforeApprove 可以在运行锁内重新检查批准条件。

```go
type RunRequest[T any] struct {
    RunID       string
    Initial     T
    ApprovalKey string
}
```

DurableGraph.AsRunnable 的输入为 `workflow.RunRequest[T]`，输出为 `*workflow.GraphSnapshot[T]`。Snapshot 含 State、Node、Steps、Routing、Key、Waiting、Done、Preview。恢复时 Initial 被忽略；同一 RunID 不能当作新任务复用。

- AsRunnable 的一次成功 Invoke 或流 Done，不一定代表业务完成：Waiting=true、Done=false 表示保存了审批等待。
- `Run(ctx,id,initial,approvalKey)` 在审批等待时保留 `durable.ErrApprovalRequired` 错误契约。
- AsChildRunnable 用于可续跑持久子图，要求在带日志的 Resumable 操作内运行，等待通过 `*durable.WaitError` 传播。普通工具适配不会自动获得此能力。
- Snapshot 读取运行状态，Diagnose 只读诊断。节点检查点不替代外部业务幂等，未能确认的副作用不能盲目重做。

源码：[durable.go](../../workflow/durable.go)、[durable_runnable.go](../../workflow/durable_runnable.go)、[操作日志](../../core/durable/operation.go)。

## 状态、会话与存储边界

| 组件 | 负责什么 | 不等于什么 |
|---|---|---|
| core.State、core.AgentState | Builder 状态的 Get/Set/Update/Snapshot/Clone 等操作 | 不等于任意 Graph[T]，也不自动深复制所有调用方引用 |
| core/checkpoint.Checkpointer | 按 thread/session 保存及读取 core.State | 不等于工具调用日志或持久图检查点 |
| store.Store | 命名空间键值存储；Builder.WithStore 接收此接口 | 不等于 core/durable.Store |
| interfaces.MemoryManager | 对话与记忆读写 | 不等于业务运行恢复 |
| core/session.Store | 追加写会话事件，推导对话历史 | 不自动维护待澄清任务状态 |
| core/durable.Store | 运行快照、操作日志及锁 | 不自动提供任意外部效果的 exactly-once |

源码：[State](../../core/state/state.go)、[Checkpointer](../../core/checkpoint/checkpointer.go)、[命名空间 Store](../../store/store.go)、[会话 Store](../../core/session/store.go)。

## 宿主执行装配

`execution.NewScope(execution.Config{RunID, SessionID, Journal, Budget, Timeout, Observer, Tracer})` 校验宿主配置；scope.Bind(ctx) 返回派生 context、取消函数和 error；execution.ConfigureBuilder(b,scope,agentID) 设置会话和稳定 Agent scope。

Budget.RootKey 必须等于 RunID；同时提供 Journal 和 Budget 时预算必须持久化。Bind 拒绝预绑定 Journal、已绑定 Scope 与冲突的身份/控制器。Scope 不关闭宿主存储或观察资源。详见[宿主接入指南](../guides/HOST_INTEGRATION.md)、[execution/scope.go](../../execution/scope.go)。

Builder 方法、默认值和迁移见 [Builder API](../guides/BUILDER_API_REFERENCE.md)；工具调用见 [Tool API](TOOL_API.md)。
