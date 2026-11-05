# 多 Agent 预置模式

`multiagent` 包在 `workflow` 之上提供三个图构造器（`Sequential`、`Loop`、`RouterSupervisor`）、并行组合件 `ParallelStep`、工具型 Supervisor（`SupervisorTools`）与文本接力层（`Conversation`、`TextStep`、`TextNext`）。构造器返回未编译的 `*workflow.Graph[T]`，宿主自己选择 `Compile` 或 `CompileDurable`，持久语义直接继承 `workflow`。本包不新增运行时、调度器或消息投递。

本包与 v0.17 删除的旧 `multiagent` 同名，但 API 与语义不兼容，旧代码不能直接迁移。设计依据见[多 Agent 预置模式设计](../superpowers/specs/2026-11-05-multiagent-patterns-design.md)，离线示例见 [`examples/multi-agent-patterns`](../../examples/multi-agent-patterns/README.md)。`examples/advanced/multi-agent-*` 四个示例演示手写拓扑，本包演示预置模式。

## 选择模式

| 场景 | 模式 | 要点 |
|---|---|---|
| S1 工单分流 | `RouterSupervisor` + `TextNext` | 分流 Agent 输出首行为成员名或 `FINISH`；`MaxRounds` 限制成员执行次数 |
| S2 回复写作与评审 | `Loop` | `Until` 读取评审结论；`MaxIterations` 是硬上限 |
| S3 多源并行查询 | `Sequential` 中放 `ParallelStep` | `Clone` 用 `CloneConversation`，`Merge` 用 `MergeTurns` |
| S4 固定流水线 | `Sequential` + `AgentStep` | 类型化状态；`out` 返回错误即节点失败 |
| S5 高风险操作审批 | `RouterSupervisor`，退款成员为自定义 `Step.Run` | 登记 → 审核 → 执行分段，每段由持久操作固定 |
| S6 向客户追问 | 自定义 `Step.Run` 内 `durable.AwaitInput` | 格式校验放进 validate |
| S7 复杂咨询由模型委派 | `SupervisorTools` | 主 Agent 自行决定调用哪些专家 |
| S8 超限转人工 | `MaxRounds` / `MaxIterations` | 返回 `workflow.ErrMaxSteps`，换运行 ID 与新根预算 |
| S9 判断无效转人工 | `RouterConfig.Fallback`、`LoopConfig.Fallback` | 路由无效时执行兜底 Step 后结束 |

两种 Supervisor 的取舍：`RouterSupervisor` 的每一步都是图节点，有节点检查点、可列入 `ApprovalNodes`、步数有硬上限，路由由宿主代码决定；`SupervisorTools` 把成员做成模型工具，由模型在一个回合内决定调用哪些成员、调用几次，灵活但只有 Agent 的 `MaxIterations` 与根预算约束，成员调用不是图节点，不能单独审批。

## 状态设计

一张图只有一个状态类型 T。持久图首次执行也会对状态做 JSON 往返，并行分支的输出在 `Merge` 前同样经过 JSON 往返，因此 T 只用导出字段，不放 `any` 内的具体类型、依赖指针身份的数据，或数字类型会被改变的值。

文本接力层用 `Conversation{Input, Turns}`；需要订单号这类类型化字段时，全部步骤用 `AgentStep` 加自定义 T。`AgentStep` 的 `in`、`out`，以及 `Until`、`Next`、`Merge` 都必须是纯函数：它们会在恢复后重复执行，`in` 的结果还进入模型请求意图。需要模型判断的逻辑放进 Step，由路由读取 Step 写回状态的结果。

## 步数与轮数上限

| 模式 | `MaxSteps` |
|---|---|
| Sequential | 步数 n |
| Loop | `MaxIterations × n` |
| RouterSupervisor | `2 × MaxRounds + 1` |

每个 `ParallelStep` 计 1 步。`MaxSteps` 由构造器计算，`cfg.MaxSteps` 必须为 0；`cfg.MaxConcurrency` 照常传给并行节点。构造器返回的图只应直接编译：继续 `AddNode` 不会报错，但步数上限不再准确。

执行器只为节点计步，路由不计步，最后一个允许的节点执行后仍会路由，路由到 `END` 即成功：

- Loop 第 `MaxIterations` 轮结束时 `Until` 为真，正常结束；为假时返回 `workflow.ErrMaxSteps`。
- Router 第 `MaxRounds` 次成员执行后 Supervisor 还会再执行一次（再发一次模型请求）：返回 `FINISH` 正常结束，选择成员返回 `ErrMaxSteps`。
- Fallback 不增加步数：Router 的 Fallback 占用一次成员轮数，Loop 的 Fallback 占用下一轮第一步的位置。最后一次判断无效时，配置了 Fallback 返回 `ErrMaxSteps`（与超限同样转人工），没有配置返回 `ErrInvalidRoute`。
- 需要“固定跑 N 轮后正常结束”时，在 T 中记录轮数，由 `Until` 判断。

超限转人工（S8）：持久运行返回 `ErrMaxSteps` 后，检查点停在 `Next == MaxSteps`，同一运行 ID 再 `Run` 仍报错。转人工或重新处理时使用新运行 ID，并为它创建新的根预算：Builder 要求根预算的 `RootKey` 与持久图的运行 ID 一致（否则报 `agent budget root conflict`），而且原根预算的用量不会因换运行 ID 而重置。

```go
runID := ticketID + "/human-1"
c, err := budget.NewDurable(budget.Config{RootKey: runID, Version: "v1", MaxModelRequests: 30}, budgetStore)
if err != nil {
    return err
}
snap, err := graph.Run(budget.WithController(ctx, c), runID, initial, "")
```

## 路由失败与兜底

持久图提交节点输出后，恢复时用同一份已提交状态重新执行路由。`Next`、`Until` 是纯函数，无效输入每次恢复都得到同样的错误，所以兜底由包装路由完成，而不是靠重试：

- `TextNext` 取最后一轮输出的第一行、去掉首尾空白，必须恰好等于某个成员名或 `FINISH`；记录为空、首行为空或不匹配时返回包装 `ErrInvalidRoute` 的错误，不做模糊匹配。用于文本路由的成员名必须是单行。
- 自定义 `Next`、`Until` 判定输入无效时也应包装 `multiagent.ErrInvalidRoute`。
- `GuardRoute` 依次：`Next` 返回包装 `ErrInvalidRoute` 的错误且配置了兜底时转兜底，其他错误原样返回；`FINISH` 映射为 `END`；不在成员集合中的名字（Supervisor 名、兜底名、`"__end__"`）按无效处理；否则返回成员名。
- 兜底 Step 执行后结束，它收到的状态里有无效的那次输出，可据此转人工。没有配置兜底时，`ErrInvalidRoute` 对该运行 ID 是终态，换运行 ID 与新根预算重新处理。
- 本包不做“重问一次”：需要时写进 Supervisor Step 自身。

## 文本接力层

`TextStep` 的默认输入是 Input 加每一轮的 `[Agent]: Output`，逐行拼接、不截断，Router 的 Supervisor 因此能看到原始任务，Loop 的写作者能看到上一稿与评审意见。输出取 `AgentOutput.Result`，字符串原样使用，其他类型按 JSON 编码，追加为一轮，空回答也记录。

- **增长**：单次请求随轮数线性增长，累计输入随轮数平方增长，检查点也反复序列化完整状态。轮数较多时用 `WithLastTurns(k)` 或 `WithRender`；需要摘要时把摘要做成单独的 Step。`WithRender` 与 `WithLastTurns` 互斥。
- **render 的确定性**：render 必须是 `Conversation` 的纯函数。模型请求意图包含完整请求，同一次节点访问恢复时 render 输出不同会报 `operation intent or attempt policy changed`。修改 render 或窗口大小等同于修改 Agent 行为，按“版本与迁移”处理。
- **内容隔离**：默认格式把客户输入与成员输出直接拼接，外部文本可以伪造轮次或诱导分流。高风险动作必须在执行前由人确认具体参数（S5），不能只靠分流判断；对外部输入敏感的流程用结构化 render（如 JSON 编码各轮）。
- **并行**：`ParallelConfig.Clone` 必须用 `CloneConversation`；`MergeTurns` 按分支登记顺序追加各分支新增的轮次，分支记录不以合并前的记录为前缀时报错。

## 持久接入契约

`multiagent` 不能检查 Agent 内部的配置，以下要求由宿主满足。

**Agent 侧**：

- 每个 Agent 用 `WithDurableScope` 设置稳定的非空 scope，日志键为“节点执行键 → `agent/<scope>` → 模型或工具操作键”。每个 Step 是独立节点，循环中每次访问的执行键包含步数，互不冲突。同一节点内第二次调用 Agent 必须换 scope。作用域规则见[原生编排：多 Agent 作用域与入口约束](NATIVE_ORCHESTRATION.md#多-agent-作用域与入口约束)。
- 带工具时，模型客户端必须实现 `llm.ToolStreamClient`，工具必须经 `Dispatcher.Tools()` 包装；否则在模型调用前失败。
- 会话由 `AgentFactory` 每次新建。记忆、检索结果、当前时间、随机示例、render 等任何非确定性内容在恢复时变化，都会使已完成请求的意图不一致而报错，不会静默回放。

**模型请求中断**：模型请求默认只允许一次尝试。恢复时按日志记录处理：

| 发生了什么 | 记录 | 默认 | 开启 `WithInterruptedModelResend` |
|---|---|---|---|
| 请求发出后、终态落盘前崩溃（或终态保存失败） | pending（`ambiguous`） | 拒绝 | 尝试次数 < 3 时重发 |
| 运行 ctx 被取消 | failed（`canceled`） | 拒绝 | 尝试次数 < 3 时重发 |
| 错误链含 `context.DeadlineExceeded` | failed（`deadline`） | 拒绝 | 拒绝 |
| 模型返回其他错误 | failed（`execution`） | 拒绝 | 拒绝 |

“拒绝”指节点以 `*durable.OperationError` 失败（`errors.Is(err, durable.ErrOperationAttention)`），同一运行 ID 无法继续。3 次是持久尝试次数，不是请求次数：每次尝试内 `WithRequestError` 还可原样重试，同一回合最多发出 `3 × (1 + maxRecoveries)` 个请求（`builder/model_resend_test.go` 的 `TestModelResendWithRequestRetries`）。模型客户端把超时包装成取消时，超时也会记为 canceled 并可能被重发。重发可能重复计费；开关在运行的整个生命周期内必须一致，否则恢复时报策略变更。deadline 与 execution 的分类见 `core/durable/operation_test.go`。

**预算**：

- `SupervisorTools` 的成员是模型工具，调用时 ctx 必须带根预算；持久运行必须是 `budget.NewDurable` 创建的持久预算。
- 持久模型请求的意图包含预算配置。恢复时必须复用原来的预算存储与完全相同的配置：复用存储但配置变化时 `budget.NewDurable` 报 `budget configuration or record changed`；换用新存储且配置也变时已完成的请求报意图不一致；换用新存储、配置相同时回放报 `usage has no reservation`；如果续跑时没有需要回放的请求，则完全拦不住，用量从零计数。预算存储必须与图存储一起持久化，恢复时总是复用。
- 成员 Agent 不得使用 Supervisor 或任何祖先调用的 Dispatcher，否则触发 `tools.ErrDispatcherReentry`；成员之间互不嵌套时可以共用一个。

## 人工等待

### 向客户追问（S6）

`AgentStep` 的 `in`、`out` 不接收 ctx，不能暂停。需要在 Agent 输出后向人要输入时写自定义 `Step.Run`：

```go
step := multiagent.Step[Case]{Name: "extract", Run: core.NewRunnableFunc(func(ctx context.Context, c Case) (Case, error) {
    fields, err := extractOnce(ctx, c) // Agent 使用 WithDurableScope("extract")，恢复时从日志回放
    if err != nil || fields.OrderID != "" {
        c.OrderID = fields.OrderID
        return c, err
    }
    raw, err := durable.AwaitInput(ctx, "order-id", json.RawMessage(`{"question":"请提供订单号"}`), validateOrderID)
    if err != nil {
        return c, err // 等待会让图暂停，Snapshot.Interrupts 给出中断键
    }
    return c, json.Unmarshal(raw, &c.OrderID)
})}
```

- 订单号的格式校验必须放进 validate。validate 拒绝时中断保持等待，`Resume` 返回 `*workflow.ResumeRejectedError`，宿主用同一中断键提交修正值；validate 接受的输入就此固定，之后提交不同的值报 `workflow.ErrInterruptAnswered`。
- validate 必须是输入的纯函数，它的错误文本会持久化并对外暴露，不得包含敏感信息。
- 依赖外部数据的核对（如订单是否存在）不能与 `AwaitInput` 放在同一节点，否则重入时外部数据变化会走另一条分支。核对做成下一个节点，结果写进状态，不通过时路由回追问节点；每次访问追问节点都是新的节点执行，中断互不冲突：

```go
g := workflow.NewGraph[Case](workflow.Config{MaxSteps: 1 + 2*maxAsks})
_ = multiagent.AddStep(g, askStep)   // 追问
_ = multiagent.AddStep(g, checkStep) // 核对订单是否存在，写入 c.OrderFound
_ = g.AddEdge(workflow.START, "ask")
_ = g.AddEdge("ask", "check")
_ = g.AddBranch("check", func(_ context.Context, c Case) (string, error) {
    if c.OrderFound {
        return workflow.END, nil
    }
    return "ask", nil
}, "ask", workflow.END)
```

### 登记后审核（S5）

`ApprovalNodes` 的审批发生在节点执行之前，只适合“是否进入某流程”这类粗粒度门禁。要审批 Agent 决定的具体动作（退款金额与订单），在节点内按“提议 → 登记 → 审核 → 执行”分段：

1. 调用退款 Agent 得到结构化提议，Agent 的工具只读。申请 ID 应取自工单：示例用 `"refund-"+订单号` 只为演示，同一订单的第二笔合法退款会因申请 ID 相同而被 `Register` 拒绝。
2. `durable.Do(ctx, "refund/register", intent, policy, run)` 登记申请并计算争议判定。意图只含申请 ID 与提议，不含规则阈值或时间；登记与判定在同一事务内写入（经 `refund.Service.OnRegister` 钩子：仅在新插入申请时、同一事务内调用，出错整体回滚）；`Reconcile` 只有在申请与判定都存在时才补记完成，避免以零值“无争议”补记而绕过审核。判定由业务规则决定，Agent 只能把“无争议”升级为“有争议”。判定随操作记录固定，等待期间规则阈值或订单事实变化也不会翻转。登记按申请 ID 幂等（`refund.Service.Register` 遇到同一意图返回已有申请、不再调用钩子），策略设 `MaxAttempts: 3, RetryInterrupted: true`：记录停在 pending 且未核对到、或因取消而失败时允许重跑；重跑读不到判定时报错，不以零值放行。
3. 有争议时 `durable.AwaitInput(ctx, "refund-review", info, validate)`。info 含订单号、金额、判定理由与解析审核人所需的提交人；输入为 `{"approved": bool, "note": string, "approver": string, "reason": string}`，`approver` 由宿主认证后填入，批准时必须非空；`reason` 可选，超时输入为 `{"approved": false, "reason": "timeout"}`，validate 严格解码、拒绝未知字段。拒绝时不执行退款，把“人工拒绝”写回状态。
4. `durable.Do(ctx, "refund/execute", ...)` 的策略设 `MaxAttempts: 3, RetryFailed: true`，并提供按回执核对的 `Reconcile`。`Reconcile` 只在记录停在 pending 时调用；退款已提交而调用返回错误时记录为 failed，默认策略下 failed 是终态，续跑只会得到操作错误，`RetryFailed` 才让续跑在尝试次数内重跑 run。run 先按申请 ID 查回执，已有回执直接返回；否则批准并退款。`refund.Service.Approve` 校验有效期与订单事实，重跑时若先比对订单或 `Approve`，退款后订单变化或审批过期会把已完成的退款误报为失败。`refund.Service.Refund` 自身也先返回已提交回执，再在同一事务内检查累计额度，是第二道防线。审核期间订单事实变化时，把“订单已变化，需重新申请”作为业务结果写回状态，而不是节点错误。执行操作也提供按回执核对的 `Reconcile`。

不要把退款放进可中断工具：可中断工具只允许一次尝试且没有对账钩子，“退款已生效、记录未落盘”时崩溃会留下 pending，恢复被拒；续跑还会整体重跑工具，审核人已拒绝的申请可能被重新判为无争议直接退款。业务实现复用 `workflow/refund.Service`，审核人解析与转人工交接由[客服编排案例](https://github.com/kart-io/goagent-examples/blob/main/docs/superpowers/specs/2026-11-05-customer-service-design.md)定义。

### 等待时限与预算截止时间

每次预留模型请求都会检查根预算的 `DeadlineUnixMilli`，而预算配置在运行生命周期内不能改。有人工等待的流程，截止时间必须覆盖全部等待窗口（业务默认每次等待 48 小时，一个运行可能先追问再审核，设为全部等待时限之和加余量），或者不设截止时间，改用 `MaxModelRequests` 控制花费、由宿主实现运行 TTL：快照 `Waiting` 为真且超过时限即放弃，换运行 ID 转人工。按单次请求时长设截止时间时，客户隔几小时回复，续跑中的模型请求必然以 `context.DeadlineExceeded` 失败。超时由宿主以 `{"approved": false, "reason": "timeout"}` 回应中断，流程按拒绝分支结束。

其他中断规则：Agent 工具内的中断需要工具声明可中断（`WithInterruptible`），或是声明 `Resumable` 的 Runnable 工具；Agent 调用的子图如果是纯审批等待，在外层持久图里按节点失败处理。审批与中断的通用语义见[人工介入](HUMAN_IN_THE_LOOP.md)。

## SupervisorTools

每个成员包装为 `tools.NewRunnableToolFactory`：参数 Schema 固定为 `{"task": string}`；结果为 `AgentOutput.Result`（字符串原样，其他类型按 JSON 编码）；`Version` 必填，随成员的行为、提示词或工具集变化而提升；`Timeout` 只在经 Dispatcher 调用时生效，0 继承 Dispatcher 默认；`Resumable` 的前提与 `RunnableToolConfig.Resumable` 相同，成员内所有效果都记在派生键下，只能在带持久日志的调用中使用。

宿主把返回的工具注册到 Supervisor Agent 的 Dispatcher，再经 `Dispatcher.Tools()` 交给 Agent。参数不合法时持久运行默认拒绝整个模型回合；要把参数错误交还模型改正，在 Supervisor 的 Dispatcher 上开启 `ReturnArgumentErrors`。用量归属与成员身份见[多 Agent 用量](MULTI_AGENT_USAGE.md)。

## 并行分支的事件

两种编译方式都不转发并行分支内 Agent 的中间事件：普通 `Compile` 经分支函数直接执行，持久模式只发出 `branch_start`、`branch_end`，只有合并后的结果。分支名只需在本并行节点内唯一，不是图节点：`ApprovalNodes` 只能填 `ParallelStep` 的外层名，填分支名会被 `CompileDurable` 拒绝。

## 支持的组合

| 组合 | 是否支持 |
|---|---|
| `ParallelStep` 作为 Sequential、Loop、RouterSupervisor 的一步或 Router 成员 | 支持 |
| 普通 `Compile` 的图用 `AddRunnable` 嵌进另一个普通图 | 支持 |
| 持久图经 `AsChildRunnable` 包装成 `Resumable` 工具交给 Agent | 支持 |
| 构造器之间嵌套（如 Sequential 的一步是 Loop） | 不提供，手写图 |
| 已编译的图作为节点放进持久图 | 不支持 |
| `ParallelStep` 的分支再是 `ParallelStep` | 不支持，构造报错 |

“抽取 → 写作⇄评审循环 → 合规”这类组合用 `workflow.NewGraph` 手写拓扑，用 `AddStep` 加入本包的 Step，用 `GuardRoute` 包装自己的路由以复用兜底，并自行设置 `MaxSteps`。循环后面还有节点时，总步数不能约束中间的循环（第 N 轮评审不通过后，留给合规的那一步会被第 N+1 次写作用掉），所以评审出口路由从状态推出轮数：

```go
const reviewRounds = 3 // MaxSteps = 1 + 2*reviewRounds + 1

reviewExit := func(ctx context.Context, c multiagent.Conversation) (string, error) {
    passed, err := parseVerdict(c) // 无法解析时返回包装 multiagent.ErrInvalidRoute 的错误
    if err != nil {
        return "", err
    }
    if passed {
        return "compliance", nil
    }
    rounds := 0
    for _, t := range c.Turns {
        if t.Agent == "review" {
            rounds++
        }
    }
    if rounds >= reviewRounds {
        return "", fmt.Errorf("%w: review loop limit %d", workflow.ErrMaxSteps, reviewRounds)
    }
    return "write", nil
}
g := workflow.NewGraph[multiagent.Conversation](workflow.Config{MaxSteps: 1 + 2*reviewRounds + 1})
// AddStep 加入 extract、write、review、compliance 与兜底 human，连边后：
route, targets, err := multiagent.GuardRoute(reviewExit, []string{"write", "compliance"}, "human")
if err != nil {
    return err
}
err = g.AddBranch("review", route, targets...)
```

`ErrInvalidRoute` 转兜底，`ErrMaxSteps` 原样返回；持久运行停在评审节点的路由位置（`Routing` 为真），同一运行 ID 再 `Run` 仍报错。因为合规节点留有一步，第 N 轮判断无效时兜底能执行，这与 Loop 构造器“末轮无效返回 `ErrMaxSteps`”不同。路由闭包里的 N 不进计划摘要，只有由 N 算出的 `MaxSteps` 进入，所以用同一个常量计算两者。

## 版本与迁移

持久图的计划摘要包含 `DurableConfig.Version` 与整个 `workflow.Config`（含 `MaxSteps`、`MaxConcurrency`），恢复时同时校验。修改 `MaxIterations`、`MaxRounds`、`MaxConcurrency`、步骤列表或审批节点后，旧运行不能交给新图恢复，提升 `Version` 也不行。宿主保留旧版本的构造参数编译旧图，处理未完成的旧运行；新版本只接收新运行。检查点自带版本，按版本分流：

```go
graphs := map[string]*workflow.DurableGraph[multiagent.Conversation]{
    "v1": mustCompile(buildRouter(3), "v1"), // 旧参数：MaxRounds 3
    "v2": mustCompile(buildRouter(5), "v2"),
}
cp, err := store.Load(ctx, runID)
switch {
case errors.Is(err, os.ErrNotExist):
    return graphs["v2"].Run(ctx, runID, initial, "")
case err != nil:
    return nil, err
}
graph, ok := graphs[cp.Version]
if !ok {
    return nil, fmt.Errorf("运行 %s 的版本 %s 已下线", runID, cp.Version)
}
return graph.Run(ctx, runID, initial, approvalKey)
```

## 运营监控

从节点事件统计：Fallback 节点（如 `human`）的执行率、`workflow.ErrMaxSteps` 与 `multiagent.ErrInvalidRoute` 的发生率。用量超过阈值的提示用 `budget.WithUsageHook`，事件含累计统计；`MaxModelRequests` 必须 ≥ 1，设为宽松的防失控上限，触达时与轮数超限一样转人工。
