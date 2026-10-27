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
    WithTools(dispatcher.Tools()...).
    WithMaxIterations(3).
    Build()
```

Dispatcher 构造时冻结工具名、描述与 Schema，Registry 后续增删不影响已构造实例。工具实现仍需支持并发；不支持时设置 MaxConcurrency=1。Schema 根类型要求 object，按已固定版本的 JSON Schema 实现校验嵌套对象、额外字段、数值约束等；外部 `$ref` 无加载器，构造时拒绝。不会自动填充默认参数，也不重试。

超时包含排队；执行中的工具必须响应 context，执行器不会启动一个无法回收的后台 goroutine 来伪造超时。nil 输出、Success=false、Error 字段非空、panic、取消均返回错误。Authorize 可为空（无额外调度层授权）；涉及业务权限必须由宿主提供授权或由业务服务执行检查。CallerID 与审批身份不能来自模型参数。

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

一个键对应一个逻辑效果。如果在一个节点内自行执行多个写操作，必须派生不同键或使用各自的业务请求 ID；将完整 Builder 放入一个节点，并不意味着内部每轮工具调用已经获得独立检查点。

图中的状态和分支结果要求可 JSON 往返。串行 v1 检查点保持兼容；并行分支名称、顺序变化会改变计划指纹，拒绝旧检查点。函数、Merge、Clone 或状态结构发生语义变化时仍须提升 Version。FileStore 只提供单机可靠文件系统上的运行锁与原子保存，没有跨主机租约、接管或自动补偿。

## 可运行退款案例

```sh
# 使用一个新目录，第一次运行停在审批前，无模型调用。
go run ./examples/workflow/refund-recovery -engine graph -dir /tmp/refund-native
# 将 KEY 替换为上一步 snapshot.Key。
go run ./examples/workflow/refund-recovery -engine graph -dir /tmp/refund-native \
  -approve KEY -approver demo-reviewer -crash-after-refund
# 上一步模拟退出码 86。重启返回原回执，账本仅一条退款。
go run ./examples/workflow/refund-recovery -engine graph -dir /tmp/refund-native
```

图流程：恢复既有申请 → 并行查询订单/校验申请 → 注册审核快照 → 审批 → Dispatcher 退款工具 → 完成。工具调用真实的本地 SQLite 事务适配器，订单与审批身份仍是演示数据，不连接支付机构。`-engine steps` 是旧默认入口，两个执行器使用独立检查点子目录，共享业务账本去重。

```sh
go test -race ./workflow ./tools ./core/durable ./examples/workflow/refund-recovery
```

覆盖分支成功后跳过、失败/取消、汇合重试、分支保存失败的稳定键、真实进程崩溃、旧检查点兼容、工具 Schema/授权/结果失败、Builder 原生工具路径，以及退款图跨进程恢复。

## DevOps、文章审核、自动回复

[三个业务案例](../../examples/workflow/business-cases/README.md)提供完整 CLI、JSON 输入夹具和自动化测试：DevOps 并行诊断与审批后模拟重启；文章并行审核、拒绝与人工复核；自动回复基于本地 FAQ，有来源才回复，未知问题转人工。

```sh
go run ./examples/workflow/business-cases -case devops -dir /tmp/goagent-devops
go run ./examples/workflow/business-cases -case article-review -dir /tmp/goagent-article
go run ./examples/workflow/business-cases -case auto-reply -dir /tmp/goagent-reply
go test -race ./examples/workflow/business-cases
```

三个案例默认使用离线规则，可通过 `-live -provider gemini|deepseek|minimax -model MODEL` 接入真实模型；业务效果始终为本地模拟。模型判断持久化后，审批恢复和流程重放不再调用模型。测试覆盖审批前零效果、Schema 校验、失败/取消恢复、跨进程崩溃与业务 ID 去重。

真实模型测试使用 `bash scripts/business_cases_live.sh gemini NEW_REPORT_FILE`（或 `deepseek`、`minimax`），从环境读取相应 API Key 和模型名称；每个 Provider 最多 6 次请求，失败项保留、无自动重试。见[Gemini / DeepSeek 报告](../evaluation/2026-10-27-business-cases-live/REPORT.md)及[MiniMax 报告](../evaluation/2026-10-27-business-cases-minimax/REPORT.md)。

## 后续能力边界

当前保留同一图状态类型，异构数据用 Adapt 显式转换。跨节点 Collect/Transform、字段映射 DSL、可视化调试器和分布式调度尚未实现。原生编排和工具执行已经连通，不等于所有通用编排能力均已覆盖。
