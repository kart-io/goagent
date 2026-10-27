# 通用流程编排

`workflow.Graph[T]` 将串行节点、条件分支、显式并行汇合与有界循环编译为 `core.StreamRunnable[T,T]`。节点共享同一状态类型 T；不同输入/输出的 Agent 使用 Adapt 显式转换。

```go
graph := workflow.NewGraph[int](workflow.Config{MaxSteps: 20, MaxConcurrency: 4})
err := graph.AddNode("increment", func(ctx context.Context, value int) (int, error) {
    return value + 1, nil
})
if err != nil { return err }
if err := graph.AddEdge(workflow.START, "increment"); err != nil { return err }
if err := graph.AddBranch("increment", func(ctx context.Context, value int) (string, error) {
    if value >= 3 { return workflow.END, nil }
    return "increment", nil
}, "increment", workflow.END); err != nil { return err }
run, err := graph.Compile(ctx)
if err != nil { return err }
result, err := run.Invoke(ctx, 0) // 3
```

## 构图与编译

| API | 用途 |
|---|---|
| `AddNode(name, Node[T])` | 注册同步状态变换函数 |
| `AddRunnable(name, core.Runnable[T,T])` | 接入已有 Runnable、子图并保留事件 |
| `Adapt(runnable, input, output)` | 将 Runnable[I,O] 映射为 Runnable[T,T] |
| `AddEdge(from, to)` | 唯一普通后继 |
| `AddBranch(from, router, targets...)` | 从节点输出选择一个预先声明的后继 |
| `AddParallel(name, ParallelConfig[T])` | 注册并行分支及显式汇合 |
| `Compile(ctx)` | 验证并生成 Runnable |

START 只允许一条入口边；END 无后继。每个节点恰好配置一条普通边或一个条件分支。Compile 拒绝未知引用、无后继、不可达节点和没有结构性出口的循环；重复名称、重复路由及保留名称在构图时拒绝。

允许存在出口的循环，运行时路由仍可能一直不选出口。MaxSteps 默认 100，每次节点执行计一步，START/END 不计；超限返回 `workflow.ErrMaxSteps`，可通过 errors.Is 检查。一个并行节点计一步，分支和嵌套子图各有自己的执行边界。框架不自动重试节点或回滚已发生的副作用。

普通多入边表示不同路径可以到达同一个节点，只执行被选中的路径，不等待其他路径。AND 汇合必须使用 AddParallel，而非添加多条普通边。

Compile 复制图定义，已编译结果不受后续构图修改影响；函数闭包和 Runnable 资源仍属于调用方。Graph Builder 不支持并发修改。编译结果可以并发执行，但节点资源和输入状态必须支持这种用法。一个有会话状态的 Builder Agent 不应被当成多个独立会话共用。

## 并行与状态隔离

```go
err := graph.AddParallel("lookup", workflow.ParallelConfig[State]{
    Branches: []workflow.Branch[State]{
        {Name: "documents", Run: lookupDocuments},
        {Name: "orders", Run: lookupOrders},
    },
    Clone: cloneState,
    Merge: func(ctx context.Context, original State, results []State) (State, error) {
        // results[0] 始终是 documents，results[1] 始终是 orders。
        return combine(original, results), nil
    },
})
```

Clone 和 Merge 均为必填。框架在启动 worker 前串行调用 Clone 为每个分支准备输入。T 包含 map、slice、pointer 时，调用方需深复制分支可能修改的内容；标量或不可变状态可原样返回。Merge 获得原始输入和按注册顺序排列的成功结果；分支完成顺序不改变结果顺序。

MaxConcurrency 默认 4，限制每个并行节点的 worker 数量。Batch 并发与此限制相互独立；多个图执行或嵌套并行不会共享全局并发额度。

任意分支出错或 panic，取消同组其他任务，停止调度并等待已启动 worker 退出，Merge 不执行。业务函数必须遵守 context；不响应取消的函数会阻止该并行节点结束。Clone/路由/Merge 的 panic 也会转为整个图的错误。这里没有事务或副作用补偿。

## 接入 Builder Agent

```go
node, err := workflow.Adapt(agent.AsRunnable(),
    func(s State) (any, error) { return s.Question, nil },
    func(s State, answer *builder.AgentOutput) (State, error) {
        s.Answer = fmt.Sprint(answer.Result)
        return s, nil
    },
)
// 检查 err 后：graph.AddRunnable("answer", node)
```

输入/输出转换错误直接传播。Adapt 等待合法的节点终态和流结束后再转换输出，不丢弃最终结果。中间文本、来源、用量等事件透传；不同类型的中间 Data 不做隐式转换。并行分支需要调用 Agent 时，可在 Branch.Run 中调用已适配节点的 Invoke；这时并行层提供分支起止事件，不透传分支内部 Token 事件。

## 执行与事件

编译结果支持 Invoke、Stream、Batch、Pipe、WithConfig、WithCallbacks。同步和流式执行使用同一源。`StreamMode()` 为 steps；节点已有文本事件会透传，但边上的状态始终在节点成功完成后才交付给下游。

| Event.Type | Event.Step |
|---|---|
| `node_start` / `node_end` | 节点名 |
| `branch_start` / `branch_end` | 并行节点名/分支名 |
| 节点原有事件 | 节点名/原 Step，原 Step 为空时只有节点名 |

节点 Done 不向外冒充图完成。只有全部选中节点及图完成回调成功后，才发一个无错误 Done，Data 为最终 T。错误时以带 Error 的终态结束；取消以 ctx.Err() 为准，错误终态为尽力发送。提前停止消费必须取消 context。

自定义 Runnable 必须关闭流，并且恰好提供一个终态。缺失/重复终态、终态后的数据或错误、nil 流均失败，不运行下游。用户提供的 Runnable 或 Node 必须停止自己启动的后台工作。节点事件中的 usage 是该节点报告的累计快照，Step 标识作用域；图不将不同节点的用量自动累加，跨节点汇总由应用处理。

## 示例与验证

```sh
go run ./examples/workflow
go run ./examples/workflow -stream
go test -race ./workflow -count=1
```

示例无需模型或 API Key，演示验证输入、选择快速路径/检索路径、双分支检索汇合、草稿审查循环和最终结果。更详细的执行边界见[执行契约](../docs/guides/EXECUTION_CONTRACT.md)。

```mermaid
flowchart LR
    start([START]) --> validate[验证输入]
    validate -->|快速路径| fast[快速处理]
    validate -->|检索路径| retrieve[并行检索并汇合]
    retrieve --> draft[生成草稿]
    draft --> review[审查]
    review -->|未通过且仍有步骤预算| draft
    review -->|通过| finish([END])
    fast --> finish
```

当前不提供异构节点自动类型推断、字段映射 DSL、跨节点 Collect/Transform、持久化图中断恢复或分布式调度。已有 core/durable 仍用于显式可恢复顺序步骤。
