# 多 Agent 预置模式设计

日期：2026-11-05。基线 `f2e242b5`。修订记录：Codex 首轮审阅（14 项）、独立架构复审（6 项重要、12 项次要）、Codex 复审（15 项，HEAD `6fdc234a`）。

## 目标

在主模块新增顶层包 `multiagent`，提供 Sequential、Loop、RouterSupervisor 三个图构造器、`ParallelStep` 组合件、工具型 Supervisor（`SupervisorTools`）与文本接力便捷层。成功标准：

- 宿主不必手写图拓扑即可编排多个 Agent；
- 满足“持久接入契约”的 Agent 放进图构造器、用 `CompileDurable` 编译后，节点检查点、审批与根预算照常生效；恢复时已提交为 completed 的模型与工具操作从日志回放，不重新请求模型、不重复执行工具；
- 不新增运行时、调度器或消息投递，所有执行仍走 `workflow`、`builder`、`tools`。

## 与既有决策的关系

[定位决策](../../research/2026-10-29-goagent-positioning.md) §4.2 删除的是依赖 Core NATS、无持久消费的旧 `multiagent`；“预置模式不进核心、需要时归 harness”的结论记在 [Eino 对照](../../research/2026-11-04-eino-comparison.md)的“差距与采纳范围”。本设计推翻后者：新包只组合已有基础件，不引入外部依赖，不恢复旧包的分布式投递语义。实现时同步修改 Eino 对照第 49 行（ADK 能力表）、第 73 行（差距表）与第 78 行（“没有需要进入核心的能力缺口”的结论），并在定位文档 §5 记录这次能力补充。

**准入依据**（定位文档 §5 要求业务证据）：内部客服服务计划接入 GoAgent。S1、S2 为业务方提出的流程，S3–S8 按业务方要求补充，覆盖客服服务中的其余典型流程：

| # | 场景 | 流程 | 使用的模式与能力 |
|---|---|---|---|
| S1 | 工单分流 | 分流 Agent 判断意图（退款、物流、技术、账单），交给对应的专业 Agent；专业 Agent 处理后回到分流 Agent，直到 FINISH | RouterSupervisor、`TextNext`、`MaxRounds` |
| S2 | 回复写作与评审 | 写作 Agent 起草回复，评审 Agent 检查语气、准确性与合规，不通过则带意见重写，直到通过 | Loop、`Until` 读评审结论、文本层记录 |
| S3 | 多源并行查询 | 回答前并行查询知识库、订单与物流信息，汇总后交给回答 Agent | Sequential 中嵌入 `ParallelStep`、`Clone`/`Merge` |
| S4 | 固定处理流水线 | 信息抽取（订单号、问题类型）→ 生成回答 → 合规检查，三步顺序执行 | Sequential、`AgentStep` 的类型化状态 |
| S5 | 高风险操作审批 | 分流到退款 Agent 前停下，由人工审批，批准后续跑 | RouterSupervisor + `ApprovalNodes`、持久续跑 |
| S6 | 向客户追问缺失信息 | 信息抽取发现缺订单号时暂停，等客户补充后从检查点继续 | 自定义 `Step.Run` 内调用 `durable.AwaitInput` |
| S7 | 复杂咨询由模型委派 | 一次咨询同时涉及账单与技术问题，主 Agent 自行调用多个专家工具并综合回答 | `SupervisorTools`、独立 Dispatcher、根预算 |
| S8 | 超限转人工 | 分流或写作评审在上限内没有结束，返回 `ErrMaxSteps`，宿主换运行 ID 与新根预算转人工处理 | `MaxRounds`/`MaxIterations` 硬上限、轮数上限语义 |

每种能力至少由一个场景使用：Sequential（S3、S4）、`ParallelStep`（S3）、Loop（S2、S8）、RouterSupervisor（S1、S5、S8）、`SupervisorTools`（S7）、文本接力层（S1、S2）。各场景的测试与示例见“场景覆盖”。

## 方案选择

| 方案 | 结论 |
|---|---|
| A. 每种模式一个构造函数，返回未编译的 `*workflow.Graph[T]` | **采用**：宿主选 `Compile` 或 `CompileDurable`，持久语义直接继承 |
| B. 仿 Eino ADK 自建 Agent 接口、Runner 与事件迭代器 | 否决：与 `workflow`/`builder` 重复 |
| C. 单一 `New(Mode, opts...)` | 否决：各模式参数差异大，类型约束弱 |

不提供独立的 `Parallel` 构造器：它等价于只含一个 `ParallelStep` 的 Sequential，单独提供只会多一套入口、校验与测试。

## 包与依赖

- 路径 `github.com/kart-io/goagent/multiagent`，属于机制层，约定只依赖 `core`、`builder`、`tools`、`workflow`、`interfaces`（测试另用 `core/budget`、`core/durable`、`testing/mocks`）。这是描述性约定，由代码审查把关；检查器只执行下面的禁止规则。
- `scripts/apicheck` 两处改动：
  - 新增反向依赖规则：`interfaces`、`errors`、`utils`、`cache`、`core`、`builder`、`tools`、`parsers`、`llm`、`workflow`、`memory`、`store`、`middleware`、`observability`、`devtools`、`harness` 禁止导入 `multiagent`；
  - 把 `multiagent` 加入现有 `execution` 规则与 `plugin` 规则的源目录列表，禁止它上行导入宿主装配层与能力装配层；
  - 同步 `main_test.go` 与 `docs/architecture/IMPORT_LAYERING.md` 规则表。
- 包文档注释与 CHANGELOG 都写明：与 v0.17 删除的旧 `multiagent` 同名，但 API 与语义不兼容，旧代码不能直接迁移。

## 底层 API：类型化状态

```go
type Step[T any] struct {
    Name string
    Run  core.Runnable[T, T]
    parallel *parallelSpec[T] // 仅 ParallelStep 设置；不导出
}

// AgentFactory 为每次节点执行新建一个 Agent 会话，避免循环与重复调用共享可变记忆。
// 并行分支与并发运行会同时调用它，实现必须并发安全。
type AgentFactory func(context.Context) (tools.OwnedRunnable[any, *builder.AgentOutput], error)

// AgentStep 把 Agent 接成一步：in 从状态取任务输入，out 把输出写回状态。
// out 返回 error 时该步失败，可用于把“合规检查不通过”变成节点错误。
func AgentStep[T any](name string, agent AgentFactory,
    in func(T) any, out func(T, *builder.AgentOutput) (T, error)) (Step[T], error)

type ParallelConfig[T any] struct {
    Steps []Step[T]
    Clone func(T) T // 必须深拷贝分支会修改的切片、映射与指针字段
    Merge func(context.Context, T, []T) (T, error)
}

// ParallelStep 把一组并行分支包装成名为 name 的一步。
func ParallelStep[T any](name string, p ParallelConfig[T]) (Step[T], error)

func Sequential[T any](cfg workflow.Config, steps ...Step[T]) (*workflow.Graph[T], error)

type LoopConfig[T any] struct {
    Steps         []Step[T]
    Until         func(context.Context, T) (bool, error) // 必填；每轮结束后判断是否结束
    MaxIterations int                                    // 必填，> 0；防失控的硬上限，达到后返回 ErrMaxSteps
}
func Loop[T any](cfg workflow.Config, l LoopConfig[T]) (*workflow.Graph[T], error)

const Finish = "FINISH"

type RouterConfig[T any] struct {
    Supervisor Step[T]
    Members    []Step[T]
    Next       func(context.Context, T) (string, error) // 返回成员名或 Finish
    MaxRounds  int                                      // 必填，> 0：成员最多执行的次数
}
func RouterSupervisor[T any](cfg workflow.Config, r RouterConfig[T]) (*workflow.Graph[T], error)

type Member struct {
    Name, Description, Version string // Version 随成员 Agent 的行为、提示词或工具集变化而提升
    Factory                    AgentFactory
    Resumable                  bool
    Timeout                    time.Duration // 0 继承 Dispatcher 默认；< 0 构造报错
}
func SupervisorTools(members ...Member) ([]interfaces.Tool, error)
```

### AgentStep 的执行

生命周期与 `tools/runnable_tool.go:173-191` 一致：每次执行先调用 Factory；只要返回的 `Close` 非 nil 就在结束时调用，即使 Factory 同时返回了错误；`Close` 使用 `context.WithoutCancel` 加 5s 超时。以下情况该步失败：Factory 返回错误；`Close` 为 nil；Runnable 为 nil（含 typed nil）；Agent 返回 `(nil, nil)`。`Close` 的错误与执行错误合并返回。

`in`、`out` 不接收 `context.Context`，不能在其中暂停。需要在 Agent 输出后向人要输入（S6）时，宿主写自定义 `Step.Run`：调用 Agent → 解析缺失字段 → `durable.AwaitInput` → 校验并更新状态。恢复时节点从头重入，Agent 的模型请求从日志回放。

### ParallelStep 的表示

- `ParallelStep` 返回的 `Step` 在私有字段 `parallel` 中保存复制后的 `Steps`、`Clone`、`Merge`；`Run` 设为一个直接调用即返回错误的 Runnable（“ParallelStep 只能交给 multiagent 构造器”），因此仍满足“`Run` 非 nil”。
- 三个图构造器遇到 `parallel != nil` 的 Step 时，以私有配置调用 `AddParallel` 注册，节点名为 `Step.Name`，忽略 `Run`；宿主修改返回值的 `Run` 不生效，修改 `Name` 即改变节点名。
- 分支名只需在本并行节点内唯一；分支执行键由节点键派生，与外层节点名不冲突。
- 分支不得是 `ParallelStep`（不支持嵌套并行），构造时报错。
- Router 成员为 `ParallelStep` 时，路由名与审批名都是外层 `Step.Name`；整组计 1 步。

### 图拓扑

| 模式 | 拓扑 | `MaxSteps` |
|---|---|---|
| Sequential | `START → s1 → … → sn → END` | `n` |
| Loop | `START → s1 → … → sn`，`sn` 上的分支目标为 `s1` 与 `END`：`Until` 为真到 `END`，否则回 `s1`；n=1 时为自环 | `MaxIterations × n` |
| RouterSupervisor | `START → sup`，`sup` 上的分支目标为全部成员与 `END`（`Next` 返回 `Finish` 时映射为 `END`）；每个成员回 `sup` | `2 × MaxRounds + 1` |

每个 `ParallelStep` 计为 1 步。`MaxSteps` 由构造函数计算，先检查乘法与加法溢出（超出 `int` 范围构造报错）；宿主在 `cfg.MaxSteps` 设了非零值时构造报错。`cfg.MaxConcurrency` 照常传给并行节点。

### 轮数上限语义

两种执行器（`Compile` 与 `CompileDurable`）只为节点执行计步，路由不计步，并行节点整体计一步；最后一个允许的节点执行后仍会执行路由，路由到 `END` 即成功。因此：

- Loop 第 `MaxIterations` 轮结束时 `Until` 为真，正常结束；为假时尝试执行下一轮第一步，返回 `workflow.ErrMaxSteps`。
- Router 第 `MaxRounds` 次成员执行后，Supervisor 还会再执行一次（会再发一次模型请求）：返回 `Finish` 时正常结束；选择任一成员时返回 `workflow.ErrMaxSteps`。
- 计数存放在检查点中，重启后仍准确。需要“固定跑 N 轮后正常结束”的宿主在 T 中记录轮数，由 `Until` 判断。
- 持久运行返回 `ErrMaxSteps` 后，检查点停在 `Next == MaxSteps`（`workflow/durable.go:274`），同一运行 ID 再 `Run` 仍然报错。宿主转人工或重新处理时使用新运行 ID，并为它创建新的根预算（新的 `budget.Config.RootKey`）；原根预算的用量不会因换运行 ID 而重置。

### 版本与迁移

持久图的计划摘要包含 `DurableConfig.Version` 与整个 `workflow.Config`（含 `MaxSteps`、`MaxConcurrency`），恢复时同时校验版本与摘要（`workflow/durable.go:121-131,409`）。因此修改 `MaxIterations`、`MaxRounds`、`MaxConcurrency`、步骤列表或审批节点后，旧运行不能交给新图恢复，提升 `Version` 也不行。宿主须为每个运行记录它使用的版本，保留旧版本的构造参数编译旧图，继续处理未完成的旧运行；新版本只接收新运行。指南给出按版本分流的示例代码。

### 返回的图只用于编译

构造器返回的 `*workflow.Graph[T]` 只应直接 `Compile` 或 `CompileDurable`。继续 `AddNode` 不会更新已算好的 `MaxSteps`；需要组合时用 `ParallelStep` 或在 Step 内完成。

### 路由与合并必须是纯函数

`Until`、`Next`、`Merge` 在崩溃恢复后可能重复执行，必须只依据输入状态决定结果：不得调用模型、读写外部计数或产生副作用。需要模型判断的逻辑放进 Step（节点）里，由 `Next` 读取节点写回状态的结果。

## 便捷层：文本接力

```go
type Turn struct {
    Agent  string
    Output string
}
type Conversation struct {
    Input string
    Turns []Turn
}

func TextStep(name string, agent AgentFactory) (Step[Conversation], error)
func TextStepWith(name string, agent AgentFactory, render func(Conversation) any) (Step[Conversation], error)
func LastTurns(k int) func(Conversation) any // 固定窗口：Input 加最近 k 轮
func CloneConversation(c Conversation) Conversation // 复制 Turns，供 ParallelConfig.Clone 使用
func MergeTurns(ctx context.Context, base Conversation, branches []Conversation) (Conversation, error)
func TextNext(members ...string) (func(context.Context, Conversation) (string, error), error)
```

- **默认输入**：`TextStep` 把 Input 加上每一轮的 `[Agent]: Output` 按顺序拼接，不截断；Router 的 Supervisor 因此能看到原始任务，Loop 的写作者能看到上一稿与评审意见。
- **增长与上限**：每轮输出长度近似固定时，单次请求随轮数线性增长，累计输入随轮数平方增长，检查点也反复序列化完整状态；固定轮数不能保证单次请求适配模型上下文。轮数较多的流程用 `TextStepWith(name, agent, LastTurns(k))` 或自定义 render；需要摘要时，把摘要做成单独的 Step，结果写回状态后由检查点保存，不新增摘要运行时。
- **render 的确定性契约**：render 必须是 `Conversation` 的纯函数，相同输入得到相同输出。模型请求意图包含完整请求，同一次节点访问恢复时 render 输出不同会使意图不一致而报错（`core/durable/operation.go:269-271`）。修改 render 等同于修改 Agent 行为，须按“版本与迁移”处理。`render` 为 nil 时用默认格式。
- **输出**：取 `AgentOutput.Result`，字符串原样使用，其他类型按 JSON 编码；追加一轮 `Turn{Agent: name}`，空回答也记录。
- **并行**：`Turns` 是切片，浅拷贝会让并行分支写同一底层数组，`ParallelConfig.Clone` 必须用 `CloneConversation`。`MergeTurns` 按分支登记顺序，追加各分支在 base 之后新增的 Turns；分支的 Turns 长度小于 base 或前缀不同时返回错误。
- **路由**：`TextNext` 构造时校验成员名：非空、单行（不含 `\n`、`\r`）、互不重复、不等于 `Finish`，否则返回错误。运行时取最后一轮输出的第一行并去掉首尾空白，必须恰好等于某个成员名或 `Finish`；记录为空、首行为空或不匹配时返回错误，不做模糊匹配。
- **单行名称**：用于文本路由的 Router 成员名必须是单行。`workflow` 只要求名称非空且首尾无空白（`workflow/graph.go:114`），内部换行合法，但这样的成员永远无法被 `TextNext` 选中。

## SupervisorTools 的工具协议

每个成员包装为 `tools.NewRunnableToolFactory[any, *builder.AgentOutput]`：

| 项 | 取值 |
|---|---|
| ArgsSchema | `{"type":"object","properties":{"task":{"type":"string"}},"required":["task"],"additionalProperties":false}` |
| Decode | Schema 已保证 `task` 存在且为字符串（`tools/runnable_tool.go:164-166` 在 Decode 前校验）；Decode 只做类型断言，失败时返回 `*tools.ArgumentError` |
| Encode | 返回 `AgentOutput.Result`：字符串原样，其他类型按 JSON 编码；输出为 nil 时返回错误 |
| Version | `Member.Version`，必填 |
| ModelInvoking | 固定 `true` |
| Timeout | `Member.Timeout` → `RunnableToolConfig.Timeout`。只在经 Dispatcher 调用时生效，直接 `Invoke` 不生效；0 继承 Dispatcher 默认超时；负值构造报错 |
| Resumable | `Member.Resumable`；前提与 `RunnableToolConfig.Resumable` 相同：成员内所有效果都记在派生键下。只能在带持久日志的调用中使用；pending 重入最多 3 次，等待续跑不计尝试 |
| Factory | 每次调用新建会话，`Close` 必须非 nil |

宿主把返回的工具交给 Supervisor Agent 的 Dispatcher，再经 `Dispatcher.Tools()` 注册到 Agent。参数不合法时，持久运行默认拒绝整个模型回合（`builder/durable_turn.go:107-113`）；要把参数错误交还模型改正，须在 Supervisor 的 Dispatcher 上开启 `ReturnArgumentErrors`，指南说明这一点。

## 持久接入契约

`multiagent` 不能检查抽象 Runnable 内部的配置，下列要求由宿主满足，指南与示例逐条演示。

**Agent 侧**（`builder/durable_turn.go:121-150`）：

- 每个 Agent 用 `WithDurableScope` 设置稳定的非空 scope。日志键为“节点执行键 → `agent/<scope>` → 模型或工具操作键”；每个 Step 是独立节点，同一 scope 在不同节点、不同轮次下互不冲突。
- 带工具时，模型客户端必须实现 `llm.ToolStreamClient`，工具必须经 `Dispatcher.Tools()` 包装（实现 `interfaces.DurableCallTool`）。
- 会话由 `AgentFactory` 每次新建。模型请求的全部内容参与意图哈希：记忆与检索结果、当前时间、随机示例、文本层 render 等任何非确定性内容在同一次节点访问恢复时变化，都会使已完成请求的意图不一致而报错（`core/durable/operation.go:269-271`），不会静默回放。

**预算与 Dispatcher**（`tools/call_scope.go:33-56`、`builder/model_call.go:196-201`）：

- `SupervisorTools` 的成员是模型工具，调用时 ctx 必须带根预算（`budget.WithController`）；持久运行必须是 `budget.NewDurable` 创建的持久预算。
- 持久模型请求的意图包含预算配置（`budget.Config`，含 `RootKey`、`Version`、各项上限与 `DeadlineUnixMilli`）与用量归属。恢复时必须复用原来的预算存储与完全相同的配置：不得在重启时重新计算截止时间、调整上限或改变归属，否则已完成请求无法回放。
- 成员 Agent 不得使用 Supervisor 或任何祖先调用的 Dispatcher，否则触发 `ErrDispatcherReentry`；成员之间互不嵌套时可以共用一个。

**状态 T**：

- 持久图首次执行也会对状态做 JSON 往返，并行分支输出在 `Merge` 前同样经过 JSON 往返（`workflow/durable.go:184,301`、`workflow/durable_parallel.go:52,91,98,225`）。T 必须语义稳定地往返：只用导出字段；不放 `any` 内的具体类型、依赖指针身份的数据，或数字类型会被改变的值。

**支持的组合**：

| 组合 | 是否支持 |
|---|---|
| `ParallelStep` 作为 Sequential、Loop、RouterSupervisor 的一步或 Router 成员 | 支持：同一张平面图，沿用分支检查点 |
| 普通 `Compile` 的图，用 `AddRunnable` 嵌进另一个普通图 | 支持 |
| 持久图通过 `AsChildRunnable` 包装成 `Resumable` 工具，交给 Agent 或 `SupervisorTools` 成员 | 支持（已有路径） |
| 把已编译的图（普通或 `AsChildRunnable`）作为节点放进持久图 | **不支持**：节点 ctx 只有 `AllowInterrupt`，没有 `CanWait`，`AsChildRunnable` 会拒绝；普通图在持久节点内不派生访问键，同一 Agent 重入会复用操作键 |
| `ParallelStep` 的分支再是 `ParallelStep` | **不支持**，构造报错 |

**审批与中断**：

- `DurableConfig.ApprovalNodes` 可填 Sequential、Loop 的 Step 名与 Router 的 Supervisor、成员名；`ParallelStep` 只能填它的 `Step.Name`，分支名不是图节点，填入会被 `CompileDurable` 拒绝。
- 循环中列入审批的节点每次访问都要审批：意图键包含步数 `Next`（`workflow/durable.go:278`），上一轮的审批不覆盖下一轮。
- 审批后续跑从被审批的节点开始，之前已完成的节点不会重新执行，也不会读取它们的模型日志。
- 节点内调用 `durable.AwaitInput` 可以暂停（S6 的写法见“AgentStep 的执行”）。Agent 工具内的中断需要工具声明可中断（`tools.InterruptibleTool` / `WithInterruptible`），或是声明 `Resumable` 的 Runnable 工具。Agent 调用的子图如果是纯审批等待，在外层持久图里按节点失败处理（`workflow/durable.go:348-369`），这一限制写进指南。
- 回放只覆盖已提交为 completed 的操作。pending 操作是否重入取决于策略：默认非 Resumable 的 Runnable 工具只允许一次尝试，pending 时恢复被拒绝，需要宿主核对。

## 校验规则

| 位置 | 规则 |
|---|---|
| 通用 | 至少一个 Step；名称合法且唯一（沿用 `workflow` 校验）；`Step.Run` 非 nil，typed nil 按 nil 处理（与 `tools` 的 `nilRunnable` 一致） |
| AgentStep | agent、in、out 非 nil |
| TextStep / TextStepWith | agent 非 nil；`render` 允许为 nil（使用默认格式） |
| LastTurns | `k > 0` |
| ParallelStep | 名称合法；`Clone`、`Merge` 非 nil；至少一个分支；分支名在本节点内唯一；分支不得是 `ParallelStep`；构造时复制 `Steps` |
| Loop | `Until` 非 nil；`MaxIterations > 0`；`MaxIterations × n` 不溢出 |
| RouterSupervisor | `MaxRounds > 0`；`2 × MaxRounds + 1` 不溢出；`Next` 非 nil；至少一个成员；成员名不得为 `Finish` 或与 Supervisor 同名；`Next` 返回未声明的名字时运行失败 |
| TextNext | 成员名非空、单行、互不重复、不等于 `Finish` |
| SupervisorTools | `Name`、`Description`、`Version`、`Factory` 必填；名称唯一；`Timeout >= 0` |
| 配置 | `cfg.MaxSteps` 必须为 0 |

运行中某步失败时，错误带节点名，图终止；并行分支失败取消其余分支（沿用 `workflow`）。

## 测试

### 测试替身

现有 `testing/mocks.MockLLMClient` 的 `CompleteToolsStream` 只转发文本（`testing/mocks/mock_llm.go:128-131`），不会产生结构化工具调用；响应是单一顺序队列，不能按请求内容回答，请求历史为浅拷贝。场景测试和离线示例共用一个新的替身，**必须先于所有场景测试与示例实现**：

- 位置：`testing/mocks/scripted_client.go`，导出 `ScriptedClient`，实现 `llm.ToolStreamClient`。
- 规则：按顺序匹配的“请求断言 → 回复”列表，回复可以是文本或带稳定 ID 的结构化 `ToolCalls`（放在成功终止块中，见 `llm/stream_client.go:25-26`）；也可按请求内容选择回复。
- 严格模式：收到没有匹配规则的额外请求时返回错误，测试据此发现意外的重放。
- 记录：请求历史深拷贝，`Calls()` 返回调用次数；读取在执行结束后进行。
- 每个角色（分流、写作、评审、各查询分支）使用独立实例，并行分支互不抢占回复；同一角色的实例在 Factory 之间共享，以保留脚本进度。

### 单元测试（`core.RunnableFunc` 作假 Agent）

- 三个图构造器与 `ParallelStep` 的正常流程，以及校验表每条规则，包括 typed nil、溢出、`cfg.MaxSteps` 非零、嵌套 `ParallelStep` 被拒绝、构造后修改调用方 `Steps` 切片不影响图。
- 普通与持久两种编译方式结果一致：每种图构造器各一例。
- 轮数边界：Loop 第 N 轮 `Until` 为真时正常结束、为假时返回 `ErrMaxSteps`；Router 第 R 次成员后 Finish 正常结束、再选成员返回 `ErrMaxSteps`；未知成员名失败。
- 组合：Sequential、Loop 中含 `ParallelStep`；Router 成员为 `ParallelStep`，以外层名审批，分支名填入 `ApprovalNodes` 被拒绝；`ParallelStep` 的 `Run` 被直接调用时返回错误。
- 并行失败路径：`Merge` 返回错误；`Clone` panic 时沿用既有 panic 边界（workflow 节点层记录后重新抛出，`core.StreamRunnable` 转为 `*errors.PanicError`），`Invoke` 返回该错误而不使进程崩溃；分支失败时取消兄弟分支且错误归属到正确分支；持久模式下分支输出已保存后重启，已完成的分支不重跑；分支输出 JSON 往返失败时报错。
- AgentStep：Factory 返回错误（且返回 Close 时仍调用 Close）、Runnable 为 typed nil、Agent 返回 `(nil, nil)`、执行错误与 Close 错误合并、`out` 返回错误时报错带节点名。
- 文本层：
  - 默认输入含 Input 与全部轮次；`TextStepWith` 自定义 render；`LastTurns(k)` 只含 Input 与最近 k 轮；
  - 非字符串 Result 的 JSON 编码；
  - `CloneConversation`：两个副本向同一预留容量位置追加不同的 Turn，断言互不影响（确定性断言，不依赖 `-race`）；
  - `MergeTurns` 顺序与长度、前缀校验；
  - `TextNext` 构造校验（多行、重复、`FINISH`）与运行时去空白、空记录、空首行、拒绝模糊匹配。

### 持久恢复测试

用 `durable.FileStore`、真实 Builder 与 `ScriptedClient`，装配持久根预算与独立 Dispatcher。“重启”指重建图、Agent、预算与存储对象，复用同一存储目录，不另起进程。故障窗口用包装 Store 精确定位：

- **模型回放**：模型操作 completed 后、节点输出保存前让图检查点保存失败；恢复后节点重新进入，`ScriptedClient.Calls()` 不增加。
- **工具 completed 回放**：工具操作 completed 后节点失败；恢复后工具不再执行。
- **工具 pending**：默认（非 Resumable）工具在 pending 状态下恢复被拒绝；Resumable 工具 pending 时重入，第 4 次尝试时报尝试耗尽。
- **跨轮隔离**：Loop 不同轮次的操作键不同，日志互不覆盖。
- **预算**：恢复后根预算不重复扣减；恢复时改变 `budget.Config`（例如重新计算 `DeadlineUnixMilli`）导致意图不一致而报错。
- **render**：同一次节点访问恢复时 render 输出相同则回放；替换 render 后恢复报意图不一致。
- **契约前提**：Agent 未设置 `WithDurableScope`、工具未经 `Dispatcher.Tools()` 包装时，在模型调用前失败。
- **版本**：修改 `MaxIterations` 后用新图恢复旧运行报检查点不匹配；用旧版本构造参数编译的图可以继续旧运行。

### SupervisorTools 测试

- `ScriptedClient` 驱动 Supervisor 调用成员工具：每次调用新建会话并调用 Close（Factory 出错但返回 Close 时也调用）。
- 直接调用工具 `Invoke`（ctx 带根预算，`Args` 为非 nil 的空对象）缺 `task` 时得到 `*tools.ArgumentError`。
- Supervisor 的 Dispatcher 开启 `ReturnArgumentErrors` 时参数错误交还模型；默认配置下整个回合失败。
- 没有根预算时失败；持久运行中使用非持久预算时失败。
- Timeout：0 继承 Dispatcher 默认；正值覆盖默认并在超时后取消成员且仍调用 Close；负值构造报错；直接 `Invoke` 不受 Timeout 约束。
- Resumable 成员：等待续跑不计尝试；无持久日志调用被拒绝。

### 场景覆盖

每个场景一个端到端测试，放在 `multiagent/scenario_test.go`，用真实 Builder 与 `ScriptedClient`；基础语义已由上面的定向测试覆盖，场景测试只验证业务链路。

| # | 测试 | 编译方式 | 断言 | 示例 |
|---|---|---|---|---|
| S1 | `TestScenarioTicketRouting` | 普通与持久 | 分流依次选中退款、物流两个专业 Agent 后 FINISH；每次 Supervisor 的请求包含原始工单；分流角色共享一个脚本实例 | 是 |
| S2 | `TestScenarioReplyReview` | 普通与持久 | `Until` 解析评审结论；第一轮不通过、第二轮通过即结束；写作者第二轮请求包含上一稿与评审意见 | 是 |
| S3 | `TestScenarioParallelLookup` | 普通与持久 | 三个查询分支各用独立脚本，结果按登记顺序合并，回答 Agent 收到合并结果；整组计 1 步 | 是 |
| S4 | `TestScenarioPipeline` | 普通与持久 | 抽取出的订单号写入状态，后两步读到；合规不通过由 `out` 返回错误，报错带节点名 | 否（S3 已演示 Sequential） |
| S5 | `TestScenarioRefundApproval` | 仅持久 | 到退款成员前返回 `ErrApprovalRequired`；重启后审批续跑，分流节点不重新执行（分流脚本 `Calls()` 不变），退款成员执行一次 | 是 |
| S6 | `TestScenarioAskCustomer` | 仅持久 | 自定义 Step 暂停并给出中断键；补充订单号后续跑，节点重入但抽取 Agent 不重新请求模型 | 否（与 `examples/human-in-the-loop-interrupt` 重复） |
| S7 | `TestScenarioDelegation` | 经 Agent 运行 | 主 Agent 发出账单、技术两个结构化工具调用，再给出汇总回答；每次调用新建会话并调用 Close；普通成员与 Resumable 成员分两个子测试 | 是 |
| S8 | `TestScenarioEscalation` | 普通：步数边界；持久：恢复断言 | 评审始终不通过时第 `MaxIterations` 轮后返回 `ErrMaxSteps`；持久模式下同一运行 ID 再 `Run` 仍报错，换运行 ID 与新根预算后可重新处理 | 否（指南中说明） |

### 离线示例与其他

- `examples/multi-agent-patterns` 演示 S1、S2、S3、S5、S7，用 `ScriptedClient`，不需要 API 密钥；各场景共用一个装配辅助函数。
- 示例验收：先 `go build` 出二进制，再以 `timeout 5s` 执行，要求退出码为 0。现有 `scripts/verify_all_examples.sh` 用 `go run` 计时且把超时记为通过、失败不返回非零（`scripts/verify_all_examples.sh:60,75,102`），不能作为这一验收；本次不修改该脚本。
- `scripts/apicheck` 新规则的回归测试。

## 文档

- 新增 `docs/guides/MULTI_AGENT_PATTERNS.md`：以 S1–S8 为线索讲选择模式、状态设计、持久接入契约（含预算配置不可变）、轮数上限与超限转人工、版本分流示例、文本层的窗口与 render 确定性、支持的组合、两种 Supervisor 的取舍。
- 指南链接 `MULTI_AGENT_USAGE.md`（身份与用量归属）与 `NATIVE_ORCHESTRATION.md` 的“多 Agent 作用域与入口约束”一节。
- README 中英文文档索引各加一行；`docs/guides/AGENT_COMPOSITION.md` 的选择表链接到新指南；`examples/README.md` 加一行。
- `docs/architecture/ARCHITECTURE.md` 的“包依赖”图与包表加入 `multiagent`。
- 现有 `examples/advanced/multi-agent-*` 四个手写示例保留，指南说明它们演示手写拓扑，新示例演示预置模式。
- 修改 Eino 对照（第 49、73、78 行）与定位文档 §5；CHANGELOG Added（`multiagent` 包与 `testing/mocks.ScriptedClient`）。

## 不做

- 独立的 `Parallel` 构造器（用只含一个 `ParallelStep` 的 Sequential）；
- 跨进程消息投递、后台子 Agent、Agent 间共享对话历史的隐式传递；
- Agent 转交（transfer）语义与 Deep、PlanExecute 预置；
- 在持久图节点内嵌套已编译图的适配、嵌套并行；
- 文本记录的模型摘要运行时（摘要做成普通 Step）；
- 路由决策的模糊匹配或模型输出自动修复；
- 修改 `scripts/verify_all_examples.sh`。
