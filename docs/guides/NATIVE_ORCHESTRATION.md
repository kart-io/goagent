# GoAgent 原生编排与工具体系

图编排、工具执行、审批与恢复均由 GoAgent 实现，主模块没有引入其他编排框架。Builder 的模型工具循环继续使用现有 Provider；确定性业务流程由 `workflow.Graph` 调度，两者通过 Runnable 和显式状态映射组合。

| 组件 | 职责 |
|---|---|
| `workflow.Graph[T]` | 条件路由、并行分支、确定顺序的汇合、有界循环 |
| `Compile` | 内存执行，Invoke/Stream/Batch 与节点事件 |
| `CompileDurable` | 节点及并行分支检查点、审批、重启恢复 |
| `DurableGraph.AsRunnable` | 持久流程的 Invoke/Stream/Batch，统一事件入口 |
| `tools.Registry` | 工具注册与查找 |
| `tools.Dispatcher` | Schema/自定义校验、授权钩子、超时、并发限制、失败传播 |
| `workflow/refund.Service` | 业务幂等、审批时效、订单事实与累计金额保护 |

## 工具接入图与 Builder

```go
registry := tools.NewRegistry()
if err := registry.Register(lookupTool); err != nil { return err }
dispatcher, err := tools.NewDispatcher(registry, tools.DispatchConfig{
    MaxConcurrency: 4,
    Timeout: 30 * time.Second,
    Authorize: authorizeToolFromHostContext,
})
if err != nil { return err }

node, err := workflow.Adapt(dispatcher.AsRunnable(),
    func(s State) (tools.ToolRequest, error) {
        return tools.ToolRequest{
            Name: "lookup_order",
            Input: &interfaces.ToolInput{Args: map[string]any{"order_id": s.OrderID}},
        }, nil
    },
    func(s State, out *interfaces.ToolOutput) (State, error) {
        s.Evidence = fmt.Sprint(out.Result)
        return s, nil
    },
)
if err != nil { return err }
if err := graph.AddRunnable("lookup", node); err != nil { return err }

// 也可以供模型选择调用；使用相同的校验、授权和并发门。
agent, err := builder.NewSimpleBuilder(client).
    WithDurableScope("assistant").
    WithTools(dispatcher.Tools()...).
    WithMaxIterations(3).
    Build()
```

Dispatcher 构造时冻结工具名、描述与 Schema，Registry 后续增删不影响已构造实例。工具实现仍需支持并发；不支持时设置 MaxConcurrency=1。Schema 根类型要求 object，按已固定版本的 JSON Schema 实现校验嵌套对象、额外字段、数值约束等；外部 `$ref` 无加载器，构造时拒绝。不会自动填充默认参数，也不重试。

`DispatchConfig.Timeout` 是单次调用的默认超时（默认 30 秒）。工具实现 `tools.DispatchTimeoutProvider`（`DispatchTimeout() time.Duration`）时，以声明的正值替代默认值，可长可短；返回 0 沿用默认值，负值在构造 Dispatcher 时拒绝。子 Agent 通常需要远超 30 秒，应在组合工具上设置 `RunnableToolConfig.Timeout`，而不是调大整个 Dispatcher 的默认值；它只在经 Dispatcher 调用时生效。计时从取得并发槽位开始，不含排队，排队只受调用方 ctx 约束；需要限制总耗时的宿主应在 ctx 上设置截止时间。执行中的工具必须响应 context，执行器不会启动一个无法回收的后台 goroutine 来伪造超时。nil 输出、Success=false、Error 字段非空、panic、取消均返回错误。Authorize 可为空（无额外调度层授权）；涉及业务权限必须由宿主提供授权或由业务服务执行检查。CallerID 与审批身份不能来自模型参数。

### 参数错误交还模型

默认任何工具错误都会中止 Agent。设置 `DispatchConfig{ReturnArgumentErrors: true}` 后，经 `Dispatcher.Tools()` 发起的模型调用遇到以下错误时，改为返回 `Success=false`、`Error` 为空、`Result={"error":"invalid_arguments","message":...}` 的结果，由模型在下一轮改正：

- Schema 校验失败；
- 自定义 `Validate` 或工具 `Invoke` 返回的 `*tools.ArgumentError`（可被 `%w` 包装；`errors.Join` 只有每个分支都是参数错误才算），包括内置工具 `ValidationError` 的返回值与经 `MarkModelArgument()` 标记的 `*errors.AgentError`。`Message` 原样展示给模型，`Err` 不展示；
- `NewRunnableTool`/`NewRunnableToolFactory` 的 Schema 错误，以及 `Decode` 返回的 `*tools.ArgumentError`，此时不会创建子会话。`Decode` 的其他错误仍然中止；
- 直接注册进本 Dispatcher、且自身开启该选项的另一个 Dispatcher 工具交还的参数错误结果，原样透传。其他工具返回同形状的结果仍按失败中止。

`Dispatcher.Invoke` 与 `AsRunnable`（workflow 节点等宿主路径）不受开关影响，仍以 error 报告参数错误。授权拒绝、超时与取消、嵌套深度超限、Dispatcher 重入、等待宿主继续、未知工具、参数不是 JSON 对象仍然中止；参数不合法时也会先执行 `Authorize`，拒绝即中止，因此 `Authorize` 可能收到不满足 Schema 的参数。子会话或内层 Dispatcher 报告的任何错误（包括内层的参数错误）都属于本工具执行失败，外层不会当作自己模型的参数错误。内层 Dispatcher、组合工具和 Builder Agent 的执行入口会自动登记；自己调用其他内层组件的包装器须透传收到的 ctx，并用 `tools.Settle(ctx, err)` 登记返回的错误；丢弃 ctx 时屏障、重入与深度保护都会失效。工具把内层错误重新包进新的参数错误，视为本层重新定性；这一判定按错误值身份在本次调用内登记完成，不包装错误，经 Dispatcher 与组合工具返回的 error 仍是工具原始返回的同一个值。同一包级哨兵若在同一次调用中既由内层返回、又由工具自己返回，会保守地按内层错误中止。同一轮的其他合法调用照常执行。

参数错误结果与正常执行使用同一调用意图和日志记录，按已完成写入，恢复时回放原结果而不执行工具；开关不进入意图，切换后合法调用照常回放，但参数错误记录在未开启恢复的路径上回放会中止。开关关闭时已写入的失败记录，打开开关后只有允许重试失败的操作（默认工具策略）会重新判定；组合工具（`MaxAttempts=1`）与被拒绝的模型回合仍需宿主处理。Builder 持久模式下，实现 `interfaces.ArgumentErrorTool` 且返回 true 的工具，参数不合法时模型回合照常提交；包装器须显式转发该接口。直接返回工具遇到参数错误时，错误按普通工具结果交还模型改正并继续循环，不会把错误当作答案；最后一轮仍传错时按额度耗尽报错并附带该参数错误，直接返回工具的其他失败仍中止。反复传错由 `WithMaxIterations` 兜底，耗尽时报错附带最后一次工具失败信息（之后的调用成功则不附带，超过 500 字符截断）。

旧 `ToolExecutor` 保留已有行为，包括原有重试配置。需要本文契约的调用方显式采用 Dispatcher；本轮未静默改变所有历史调用方。

## 持久图的统一入口

```go
run, err := graph.CompileDurable(ctx, workflow.DurableConfig{
    Store: store,
    Version: "order-flow-v1",
    ApprovalNodes: []string{"refund"},
})
if err != nil { return err }
snapshot, err := run.AsRunnable().Invoke(ctx, workflow.RunRequest[State]{
    RunID: runID, Initial: initial,
})
if err != nil { return err }
if snapshot.Waiting {
    // 宿主完成身份与业务授权后，使用同一 RunID 和 snapshot.Key 发起恢复。
}
```

`AsRunnable` 的审批暂停是一次正常结束的调用：`snapshot.Waiting=true`、`snapshot.Done=false`。Stream 的最终 Done 只代表本次调用结束，业务流程是否完成要检查 Data.Done。兼容方法 `Run` 仍返回 `durable.ErrApprovalRequired`。`Snapshot(ctx, runID)` 只读加载并验证持久状态，需要获取运行锁；活跃执行期间可能等待，调用方应设置超时。

事件包括 node_start/node_end、branch_start/branch_end、工具及节点原有事件、approval_required。分支和子事件带节点作用域。它们是本次执行的实时观察，不是可恢复的事件日志；消费者断开不会撤销已经提交的业务效果。提前停止消费 Stream 必须取消 context。

## 并行恢复与效果去重

1. 开始并行节点前，Clone 的各分支输入先完成 JSON 持久化，再调度工具或其他效果。
2. 每个成功分支单独保存结果。一个分支失败会取消同组未完成工作，并等待已启动函数退出；已保存的成功分支在恢复时跳过。
3. 汇合始终按分支注册顺序执行。Merge 必须无副作用；其失败或输出检查点保存失败时，恢复仅重做 Merge，保留分支结果。
4. 节点和每个分支都有独立稳定执行键，循环的不同访问使用不同键。效果已经提交但结果未保存时仍可能重跑，由业务服务做幂等。

`workflow.DurableKey(ctx)` 与 `durable.ExecutionKey(ctx)` 返回同一执行键。Dispatcher 将该键注入 ToolInput.ExecutionKey；`core/durable.Runner` 同样向步骤 context 注入键。模型的 Args 无权覆盖此元数据。

一个键对应一个逻辑效果。手写多工具节点为每个操作设置稳定 CallID，Dispatcher 自动派生子键；Builder 原生工具循环通过已持久化的模型回合和调用 ID 生成 CallID。裸 Tool 不承诺调用级恢复，持久 Builder 会在模型请求前拒绝它。详见下文。

图中的状态和分支结果要求可 JSON 往返。串行 v1 检查点保持兼容；并行分支名称、顺序变化会改变计划指纹，拒绝旧检查点。函数、Merge、Clone 或状态结构发生语义变化时仍须提升 Version。FileStore 只提供单机可靠文件系统上的运行锁与原子保存，没有跨主机租约、接管或自动补偿。

## 可运行退款案例

案例位于 [goagent-examples](https://github.com/kart-io/goagent-examples/blob/main/workflow/refund-recovery/README.md)，本节命令在该仓库根目录运行。

```sh
# 使用一个新目录，第一次运行停在审批前，无模型调用。
go run ./workflow/refund-recovery -engine graph -dir /tmp/refund-native
# 将 KEY 替换为上一步 snapshot.Key。
go run ./workflow/refund-recovery -engine graph -dir /tmp/refund-native \
  -approve KEY -approver demo-reviewer -crash-after-refund
# 上一步模拟退出码 86。重启返回原回执，账本仅一条退款。
go run ./workflow/refund-recovery -engine graph -dir /tmp/refund-native
```

图流程：恢复既有申请 → 并行查询订单/校验申请 → 注册审核快照 → 审批 → Dispatcher 退款工具 → 完成。工具调用真实的本地 SQLite 事务适配器，订单与审批身份仍是演示数据，不连接支付机构。`-engine steps` 是旧默认入口，两个执行器使用独立检查点子目录，共享业务账本去重。

```sh
# goagent 仓库
go test -race ./workflow ./tools ./core/durable
# goagent-examples 仓库
go test -race ./workflow/refund-recovery
```

覆盖分支成功后跳过、失败/取消、汇合重试、分支保存失败的稳定键、真实进程崩溃、旧检查点兼容、工具 Schema/授权/结果失败、Builder 原生工具路径，以及退款图跨进程恢复。

## DevOps、文章审核、自动回复

[三个业务案例](https://github.com/kart-io/goagent-examples/blob/main/workflow/business-cases/README.md)位于 goagent-examples 仓库，本节命令在该仓库根目录运行。案例提供完整 CLI、JSON 输入夹具和自动化测试：DevOps 并行诊断与审批后模拟重启；文章并行审核、拒绝与人工复核；自动回复基于本地 FAQ，有来源才回复，未知问题转人工。

```sh
go run ./workflow/business-cases -case devops -dir /tmp/goagent-devops
go run ./workflow/business-cases -case article-review -dir /tmp/goagent-article
go run ./workflow/business-cases -case auto-reply -dir /tmp/goagent-reply
go test -race ./workflow/business-cases
```

三个案例默认使用离线规则，可通过 `-live -provider gemini|deepseek|minimax -model MODEL` 接入真实模型；业务效果始终为本地模拟。模型判断持久化后，审批恢复和流程重放不再调用模型。测试覆盖审批前零效果、Schema 校验、失败/取消恢复、跨进程崩溃与业务 ID 去重。

真实模型测试使用 `bash scripts/business_cases_live.sh gemini NEW_REPORT_FILE`（或 `deepseek`、`minimax`），从环境读取相应 API Key 和模型名称；每个 Provider 最多 6 次请求，失败项保留、无自动重试。见[Gemini / DeepSeek 报告](../evaluation/2026-10-27-business-cases-live/REPORT.md)及[MiniMax 报告](../evaluation/2026-10-27-business-cases-minimax/REPORT.md)。

## 后续能力边界

当前保留同一图状态类型，异构数据用 Adapt 显式转换。跨节点 Collect/Transform、字段映射 DSL、可视化调试器和分布式调度尚未实现。原生编排和工具执行已经连通，不等于所有通用编排能力均已覆盖。


## 工具调用级日志与恢复

```go
// ctx 来自持久图节点，包含 store 和该次节点访问的父执行键。
out, err := dispatcher.Invoke(ctx, tools.ToolRequest{
    Name: "reserve_stock",
    Input: &interfaces.ToolInput{
        CallID: "reserve", // 宿主定义的稳定逻辑 ID；第二个效果必须使用不同 ID
        Args: map[string]any{"sku": "demo", "quantity": 1},
    },
})
```

每次操作依次执行：获取子操作锁 → 校验意图摘要 → 保存 pending 与尝试次数 → 执行 → 保存 completed/failed。重复的完成调用返回保存结果；同一 ID 的参数、schema、调用方或尝试预算改变时拒绝恢复。调用输出跨 JSON 边界，fresh/replay 都按 JSON 解码：对象为 map、数值为 json.Number，自定义 Go 类型须在应用边界显式转换。旧空 CallID 不启用此日志，不改变原工具输出类型。

`DispatchConfig.MaxAttempts` 默认 3，包含首次执行；只有下一次显式调用才会尝试失败或未确定的操作。恢复每次仍先做 schema、自定义校验及授权。授权函数不能有副作用；首次执行和回放都可能调用。日志不提供外部效果 exactly-once：崩溃窗口里的工具必须让目标系统将去重键和业务写入放在同一事务中。保存失败不会被当成成功。

回放直接返回日志中的结果，不再经过工具本身或包在 Dispatcher 内部的包装器。宿主若按执行顺序维护状态（例如证据账本按到达顺序分配引用号），应放进 `DispatchConfig.OnResult`：它在得到结果后、`tool_end` 之前同步调用，首次执行、日志回放和交还模型的参数错误结果都会触发，参数为 `tools.DispatchResult{Name, CallID, ExecutionKey, Args, Output, Replayed}`。持久调用的 `Output` 总从日志解码，首次与回放值相同；`ExecutionKey` 由父键与 CallID 派生，重启后不变，可作宿主去重键。崩溃恢复时已完成的调用回放并再次触发回调。顺序执行的调用按调用顺序触发；Builder 同轮并发执行的调用经 `tools.DeferResults` 推迟到整轮结束后按模型给出的顺序触发，首次执行与恢复回放顺序一致，按到达顺序编号的账本因此能原样重建。宿主自行并发调用 `Invoke` 时触发顺序不确定且会并发触发，需自行加锁，编号应从 `ExecutionKey` 派生而不是按到达顺序分配。回调返回错误或 panic 时本次调用失败，已写入的日志不变。回调不进入意图摘要，不受 `Timeout` 约束，不得修改 `Args` 与 `Output`。

Builder 要求显式配置 `WithDurableScope("assistant")`，先从节点键派生 Agent 调用键，再在其下用 `model/turn/N` 保存完整模型回合、用 `tool/turn/N/call/ID` 保存每个工具。因此 Gemini 的 ProviderState 与 DeepSeek/MiniMax 的 reasoning_content 可在重启后续接，已执行的调用 ID 不依赖重新生成。原生续接数据仅在本地检查点中使用，公开测试报告不包含这些内容。Store 可能包含业务数据和模型内容，宿主负责访问控制与保留期限。

模型回合先验证所有工具名、ID、JSON 参数及 Schema；任一调用无效则整轮不执行工具。网络错误、截断、无效计划也保存已取得的用量。模型操作最多请求一次：请求后崩溃但结果未落盘会保留 pending/ambiguous，恢复返回 `ErrOperationAttention`，不会再次付费；此时尚未得到的用量无法重建。在对应 Agent 作用域 context 上调用 `InspectOperation(ctx, "model/turn/0")` 可读取详细状态；不能直接使用节点的父 context 查询子作用域。处理不确定效果后，由业务宿主决定补偿或启动新工作流；不直接删除日志强制重试。

`durable.Do` 可供其他节点保存具名子操作，`WithJournal` 与 `WithExecutionKey` 可供宿主显式接入。其输入保存摘要，输出保存 JSON，错误仅保存类别。默认一次尝试；`RetryFailed: true` 配合 MaxAttempts 允许显式恢复。未绑定日志时直接执行，不提供持久性。

模型、提示词、工具实现、策略或输入结构改变时，必须提升 `DurableConfig.Version`。模型请求中的 provider、消息与工具定义另有意图校验，但框架不能推断闭包配置的全部变化。恢复要求固定输入和上下文；当前不支持持久工具循环使用 ContextPolicy（在模型请求前拒绝）。外部记忆、检索、中间件、完成回调也不是任意调用栈检查点，需由宿主保证稳定或自行持久化。

## 类型化流适配与能力声明

`workflow.AdaptStream` 增加显式 intermediate chunk 映射，保留类型化预览和业务事件；最终映射仍在合法终态及 EOF 后运行。预览不提交图状态、不调度下游，可在恢复时重复。映射错误、取消和上游协议错误会终止执行。

Builder 会读取模型可选的 `core.StreamCapability`。MiniMax 案例适配器当前支持真实原生工具调用，但 HTTP 响应完整后才交付：模型为 final，Agent 为 steps，不宣称逐 token 流。跨节点 Collect/Transform 仍未实现，也未引入外部编排框架依赖。

## 多工具真实进程恢复验证

以下命令在 goagent-examples 仓库根目录运行。

```sh
# 默认离线：模拟原生协议，验证真实子进程退出与续接。
go test -race ./workflow/business-cases -run TestNativeToolLoopProcessRecovery
# 显式真实模型：选择一个 provider，每次最多 4 个请求。
DEEPSEEK_MODEL=deepseek-flash bash scripts/business_tool_loop_live.sh deepseek /tmp/deepseek-loop.json
```

同一个 Agent 节点执行 A、B 两个工具，在 B 效果落盘后 `os.Exit(86)`；新进程应跳过 A、恢复 B 并仅请求最终回答；第三次进程启动应零模型/工具调用。效果按子执行键去重，断言只有两条记录、两个不同键及最终答案包含两条回执。见[三模型实测及首次失败记录](../evaluation/2026-10-27-durable-tool-loop/REPORT.md)。

## 可组合编排 R2

新增根预算、受控 Runnable 工具、OTel 观察及不可变上下文结果引用；见 [可组合编排指南](COMPOSABLE_ORCHESTRATION.md)。上下文结果卸载使用 `WithOffload`，持久运行中读取工具与业务工具一起经宿主 Dispatcher。流预览仍不代表业务终态，core/flow 仅为实验原型。


## 多 Agent 作用域与入口约束

同一节点中的每个逻辑 Agent 调用必须有不同且跨重启稳定的作用域：

```go
author, err := builder.NewSimpleBuilder(client).
    WithDurableScope("author").WithTools(dispatcher.Tools()...).Build()
// 检查 err 后构造另一组件：
reviewer, err := builder.NewSimpleBuilder(client).
    WithDurableScope("reviewer").WithTools(dispatcher.Tools()...).Build()
```

调用键派生为 `durable.ChildKey(nodeKey, "agent/" + scope)`，模型和工具日志均在此键下。重用同一个 scope 表示恢复**同一次逻辑调用**，不是启动新工作；同一 Agent 在一个节点内处理多个业务项时，宿主应使用稳定业务 ID 派生不同父键，或使用不同作用域/图节点。不要使用随机 ID、进程指针或并发完成顺序。没有作用域、空白作用域或旧的无作用域日志都会在模型请求前报错。

无工具的 `StreamClient` 也通过相同的模型操作日志执行，成功结果可重放，失败保留已知用量并停止重复请求。有工具的持久化 Agent 必须使用 `ToolStreamClient` 和 `Dispatcher.Tools()`（或等价 DurableCallTool）；旧 GenerateWithTools 协议在持久化模式下会提前拒绝，普通非持久执行保持兼容。同步 `Execute` 和不支持流的 `ExecuteWithTools` 在持久上下文中也会拒绝，避免绕过日志；使用 `AsRunnable().Invoke` / `ExecuteStream`。这些检查在中间件、检索及模型请求前完成，日志检查的锁等待计入 Agent 超时。

同一轮的多个工具调用默认按顺序执行。`WithParallelToolCalls(n)` 设为 n>=2（不另设上限传 `math.MaxInt`）且每个工具都实现 `interfaces.ConcurrentCallTool`（`Dispatcher.Tools()` 已实现，包装器须显式转发）时并发执行，峰值为 `min(n, MaxConcurrency)`；含上下文策略工具时始终顺序执行。并发时执行先后不保证与模型给出的顺序一致，即使 `MaxConcurrency=1` 也一样。工具消息与 `tool_start`/`tool_end` 事件按模型给出的顺序，与完成顺序无关；`OnResult` 推迟到整轮结束后按同一顺序触发。某个调用失败不取消其他调用，全部结束后返回顺序最靠前的错误，避免兄弟调用被记为取消失败而在恢复时需要宿主处理。工具内部不要按执行顺序分配编号或依赖同轮调用的先后。直接返回工具只在本轮只有它一个调用时生效，因此不参与并发。

`WithEmptyAnswerRetries(n)` 默认关闭。开启后，模型以 stop 结束且既无正文也无工具调用的回合按已完成写入日志，不追加空 assistant 消息，而是追加一条固定 user 消息重问；重问计入 MaxIterations 并占用一次预算。超过 n 次或已是最后一轮时按原方式报错，持久回合记为失败。

这是持久化接入契约的变更。原有宿主需要补充作用域并提升 `DurableConfig.Version`；先核对旧运行已发生的业务效果，再决定迁移或发起新的逻辑工作，不直接删除检查点重试。检测到节点下旧 `model/turn/0` 或 `model/answer/0` 日志时，不会悄悄换键执行。原生多工具案例已提升为 native-loop-v3，避免把历史 v2 运行当成新键继续执行。

## 持久流中的类型化预览

持久图 `AsRunnable().Stream` 保留节点中间 Data：通过 `chunk.Data.State` 获取，且 `chunk.Data.Preview=true`、`chunk.Done=false`。零值数据和没有 Event 的数据块也会转发；事件仍保留节点作用域。纯事件块的 State 可能是 T 的零值，不应据此推断业务状态。

Preview 是本次执行的临时观察，不是检查点；不驱动下游、不提交持久状态，也不代表审批或完成。消费方不要把它用作恢复状态。`Snapshot`/`Run` 返回的持久状态以及最终成功块均为 Preview=false。预览之后出错或取消，恢复仍从上次已提交状态开始；重新执行可能再次产生预览。
