# 多 Agent 预置模式设计

日期：2026-11-05。基线 `f2e242b5`。修订记录：Codex 首轮审阅（14 项）、独立架构复审（6 项重要、12 项次要）、Codex 复审（15 项，HEAD `6fdc234a`）、第四轮架构复审（7 项重要、12 项次要，HEAD `d1a8f79c`）、Codex 第五轮（7 项，HEAD `0c7be5c6`，解决方案经独立架构分析）、Codex 第六轮（5 项，HEAD `52b64d95`）、第七轮架构复审（4 项重要、10 项次要，HEAD `1cd75eb6`）。

## 目标

在主模块新增顶层包 `multiagent`，提供 Sequential、Loop、RouterSupervisor 三个图构造器、`ParallelStep` 组合件、工具型 Supervisor（`SupervisorTools`）与文本接力便捷层。成功标准：

- 宿主不必手写图拓扑即可编排多个 Agent；
- 满足“持久接入契约”的 Agent 放进图构造器、用 `CompileDurable` 编译后，节点检查点、审批与根预算照常生效；恢复时已提交为 completed 的模型与工具操作从日志回放，不重新请求模型、不重复执行工具（pending 与 failed 操作的处理见“持久接入契约”）；
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
| S5 | 高风险操作审批 | 退款 Agent 拟定退款金额与订单后，按业务规则判定是否有争议：无争议直接退款；有争议时退款工具在执行前停下，把具体参数交审核人确认，批准后才执行，超时按拒绝处理 | RouterSupervisor、可中断工具内的 `durable.AwaitInput`、持久续跑 |
| S6 | 向客户追问缺失信息 | 信息抽取发现缺订单号时暂停，等客户补充后从检查点继续 | 自定义 `Step.Run` 内调用 `durable.AwaitInput` |
| S7 | 复杂咨询由模型委派 | 一次咨询同时涉及账单与技术问题，主 Agent 自行调用多个专家工具并综合回答 | `SupervisorTools`、独立 Dispatcher、根预算 |
| S8 | 超限转人工 | 分流或写作评审在上限内没有结束，返回 `ErrMaxSteps`，宿主换运行 ID 与新根预算转人工处理 | `MaxRounds`/`MaxIterations` 硬上限、轮数上限语义 |
| S9 | 判断无效转人工 | 分流模型给出无法识别的选择，或评审结论无法解析时，路由到兜底 Step（转人工）后结束，而不是让持久运行卡在同一处 | `RouterConfig.Fallback`、`LoopConfig.Fallback`、`ErrInvalidRoute` |

**业务方确认（2026-11-05）**：以下决定来自业务方，约束本设计的场景与指南；其中的业务能力不进入 `multiagent`，由单独的[客服编排设计](2026-11-05-customer-service-orchestration-design.md)承接（待写）。

| 事项 | 决定 | 本设计中的落点 |
|---|---|---|
| 退款审批 | 分两种：无争议直接退；有争议由人工审核决定是否退。争议由业务配置的确定性规则判定，Agent 只能把“无争议”升级为“有争议”，不能降级 | S5：规则与升级逻辑在退款工具内，只有有争议时才调用 `AwaitInput` |
| 审核人 | 可配置，按组织架构解析（如提交人的上级、财务人员）；参考飞书通讯录接口，但必须是抽象接口，不能写死 | 不进入本包：由客服编排定义解析接口与飞书实现；本包只要求等待的 Info 携带解析所需的提交人与金额 |
| 等待时限 | 由业务配置，默认 48 小时；超时即结束，按拒绝处理并告知客户，追问超时则按现有信息结束 | 宿主按时限以 `{"approved": false, "reason": "timeout"}` 回应中断，流程按拒绝分支结束；根预算截止时间设为“等待时限 + 余量”，见“人工等待与截止时间” |
| 临近超时提醒 | 等待临近超时时提醒审核人 | 宿主调度，不进入本包 |
| 转人工 | 渠道取决于 IM 系统；交接上下文必须包含客户意图与解决方案（可以多个） | 交接接口由客服编排定义；本包的 Fallback 与超限分支负责产出这份上下文 |
| 成本 | 不设业务硬上限，但必须有处理；用量超过阈值提示运营，轮数超限提示客户并转人工 | 用量提示用现有 `budget.WithUsageHook`（事件含累计统计）；`MaxModelRequests` 必须 ≥ 1（`core/budget/controller.go:75`），设为宽松的防失控上限，触达时与轮数超限一样转人工 |

每种能力至少由一个场景使用：Sequential（S3、S4）、`ParallelStep`（S3）、Loop（S2、S8、S9）、RouterSupervisor（S1、S5、S8、S9）、`SupervisorTools`（S7）、文本接力层（S1、S2、S9）、兜底（S9）。各场景的测试与示例见“场景覆盖”。

模式之间的嵌套组合（如 S4 的流水线中间放 S2 的写作评审循环，或 S1 的退款成员本身是循环）不由构造器提供，宿主用 `workflow.Graph` 手写拓扑，并用 `AddStep` 与 `GuardRoute` 复用本包的 Step（含 `ParallelStep`）与兜底路由，见“组合流程”。

## 方案选择

| 方案 | 结论 |
|---|---|
| A. 每种模式一个构造函数，返回未编译的 `*workflow.Graph[T]` | **采用**：宿主选 `Compile` 或 `CompileDurable`，持久语义直接继承 |
| B. 仿 Eino ADK 自建 Agent 接口、Runner 与事件迭代器 | 否决：与 `workflow`/`builder` 重复 |
| C. 单一 `New(Mode, opts...)` | 否决：各模式参数差异大，类型约束弱 |

不提供独立的 `Parallel` 构造器：它等价于只含一个 `ParallelStep` 的 Sequential，单独提供只会多一套入口、校验与测试。

## 包与依赖

- 路径 `github.com/kart-io/goagent/multiagent`，属于机制层，约定只依赖 `core`、`builder`、`tools`、`workflow`、`interfaces`（测试另用 `core/budget`、`core/durable`、`testing/mocks`）。这是描述性约定，由代码审查把关；检查器只执行下面的禁止规则。
- `scripts/apicheck` 三处改动：
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
// in 返回 error 时在发出模型请求前失败，可用于拒绝不完整的状态；
// out 返回 error 时该步失败，可用于把“合规检查不通过”变成节点错误。
func AgentStep[T any](name string, agent AgentFactory,
    in func(T) (any, error), out func(T, *builder.AgentOutput) (T, error)) (Step[T], error)

type ParallelConfig[T any] struct {
    Steps []Step[T]
    Clone func(T) T // 必须深拷贝分支会修改的切片、映射与指针字段
    Merge func(context.Context, T, []T) (T, error)
}

// ParallelStep 把一组并行分支包装成名为 name 的一步。
func ParallelStep[T any](name string, p ParallelConfig[T]) (Step[T], error)

func Sequential[T any](cfg workflow.Config, steps ...Step[T]) (*workflow.Graph[T], error)

// ErrInvalidRoute 表示路由或结束判断的输入无效（如模型输出无法识别）。
// 配置了 Fallback 时转入兜底 Step，否则运行失败。
var ErrInvalidRoute = errors.New("multiagent: invalid route decision")

type LoopConfig[T any] struct {
    Steps         []Step[T]
    Until         func(context.Context, T) (bool, error) // 必填；每轮结束后判断是否结束
    MaxIterations int                                    // 必填，> 0；防失控的硬上限，达到后返回 ErrMaxSteps
    Fallback      *Step[T]                               // 可选；Until 返回包装 ErrInvalidRoute 的错误时执行，之后结束
}
func Loop[T any](cfg workflow.Config, l LoopConfig[T]) (*workflow.Graph[T], error)

const Finish = "FINISH"

// AddStep 把 Step 加入宿主手写的图：ParallelStep 以 AddParallel 注册，其余以 AddRunnable 注册。
// 三个构造器内部也用它注册节点。
func AddStep[T any](g *workflow.Graph[T], s Step[T]) error

// GuardRoute 把返回成员名或 Finish 的 next 包装成带兜底的 workflow.Router，处理顺序见“路由失败与兜底”。
// fallback 为空表示不兜底。targets 是应交给 AddBranch 的全部目标（成员、END 与 fallback）。
// RouterSupervisor 与 Loop 内部也用它注册路由。
func GuardRoute[T any](next func(context.Context, T) (string, error), members []string, fallback string) (route workflow.Router[T], targets []string, err error)

type RouterConfig[T any] struct {
    Supervisor Step[T]
    Members    []Step[T]
    Next       func(context.Context, T) (string, error) // 返回成员名或 Finish
    MaxRounds  int                                      // 必填，> 0：成员最多执行的次数
    Fallback   *Step[T]                                 // 可选；路由无效时执行，之后结束
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

输入输出映射与中间事件转发复用 `workflow.Adapt`（`workflow/adapter.go:13`）。`Adapt` 接收固定的 `core.Runnable[I, O]`，而 Factory 每次返回新的 `tools.OwnedRunnable`，所以 AgentStep 在构造时把 Factory 包装成一个固定的会话 Runnable：每次调用新建会话，把流原样委托给 `owned.Runnable.Stream`，流关闭后调用 `Close`，Done 块的输出为 nil 时报错。然后只调用一次 `workflow.Adapt(session, in, out)`，其构造错误由 `AgentStep` 返回；`out` 不会收到 nil 输出。取消后排空、缺少 Done、重复 Done 等边界交给外层 `Adapt` 使用的 `consumeNode`（`workflow/execute.go:86-134`）处理，会话 Runnable 不另写一份排空逻辑。

`in`、`out` 不接收 `context.Context`，不能在其中暂停。需要在 Agent 输出后向人要输入（S6）时，宿主写自定义 `Step.Run`：调用抽取 Agent → 解析缺失字段 → `durable.AwaitInput(ctx, "order-id", info, validate)` → 用返回值更新状态。

- 订单号的格式校验必须放进 validate。validate 拒绝时中断保持等待，`Resume` 返回 `*workflow.ResumeRejectedError`，快照中该中断 `Rejected` 为真，宿主用同一中断键提交修正值。validate 接受的输入会固定下来（`core/durable/interrupt.go:58-59`），之后再提交不同的值会因 `ErrInterruptAnswered` 被拒（`workflow/interrupt.go:73-80`），所以不能在 `AwaitInput` 返回后才校验格式，否则运行会卡在无效值上。
- validate 必须是输入的纯函数（重入时可能再次执行）；它的错误文本会持久化并对外暴露，不得包含敏感信息。
- 依赖外部数据、结果可能变化的核对（如订单是否存在）不能与 `AwaitInput` 放在同一节点：节点重入时会重新核对，外部数据若已变化，就会走另一条分支，可能跳过后续追问、丢掉客户的新输入。核对做成独立的下一个节点，结果写进状态，由节点检查点固定；核对不通过时由路由回到追问节点。每次访问追问节点都是一次新的节点执行（执行键包含步数），中断互不冲突。这需要手写图（追问 → 核对 → 回到追问或继续），S6 场景测试只演示格式校验，指南给出该回路的写法。
- 恢复时节点从头重入，Agent 的模型请求从日志回放。补充信息后如需在同一节点再调用一次 Agent，第二次调用必须用不同的 `WithDurableScope`（`builder/config_options.go:128-130`），否则与第一次调用的意图冲突。

### ParallelStep 的表示

- `ParallelStep` 返回的 `Step` 在私有字段 `parallel` 中保存复制后的 `Steps`、`Clone`、`Merge`；`Run` 设为一个直接调用即返回错误的 Runnable（“ParallelStep 只能交给 multiagent 构造器”），因此仍满足“`Run` 非 nil”。
- 三个图构造器遇到 `parallel != nil` 的 Step 时，以私有配置调用 `AddParallel` 注册，节点名为 `Step.Name`，忽略 `Run`；宿主修改返回值的 `Run` 不生效，修改 `Name` 即改变节点名。
- 分支名只需在本并行节点内唯一；分支执行键由节点键派生，与外层节点名不冲突。
- 分支不得是 `ParallelStep`（不支持嵌套并行），构造时报错。
- Router 成员、Supervisor 或 Fallback 为 `ParallelStep` 时，路由名与审批名都是外层 `Step.Name`；整组计 1 步。
- 两种编译方式都不转发并行分支内 Agent 的中间事件：普通 `Compile` 经 `Branch.Run` 执行分支（`workflow/parallel.go:96-101`），持久模式只发出 `branch_start`、`branch_end`（`workflow/durable_parallel.go:140,172`），只有合并后的结果；指南说明这一点。

### 图拓扑

| 模式 | 拓扑 | `MaxSteps` |
|---|---|---|
| Sequential | `START → s1 → … → sn → END` | `n` |
| Loop | `START → s1 → … → sn`，`sn` 上的分支目标为 `s1` 与 `END`：`Until` 为真到 `END`，否则回 `s1`；n=1 时为自环。有 Fallback 时分支目标再加 `fb`，`fb → END` | `MaxIterations × n` |
| RouterSupervisor | `START → sup`，`sup` 上的分支目标为全部成员与 `END`（`Next` 返回 `Finish` 时映射为 `END`）；每个成员回 `sup`。有 Fallback 时分支目标再加 `fb`，`fb → END` | `2 × MaxRounds + 1` |

每个 `ParallelStep` 计为 1 步。Fallback 不增加 `MaxSteps`：Router 的 Fallback 占用一次成员轮数，Loop 的 Fallback 占用下一轮第一步的位置，因此配置了 Fallback 时，最后一次判断无效返回 `ErrMaxSteps`，与超限同样处理（S8），不会让成员多执行一次；没有配置 Fallback 时路由先报错，返回 `ErrInvalidRoute`。`MaxSteps` 由构造函数计算，先检查乘法与加法溢出（超出 `int` 范围构造报错）；宿主在 `cfg.MaxSteps` 设了非零值时构造报错。`cfg.MaxConcurrency` 照常传给并行节点。

### 轮数上限语义

两种执行器（`Compile` 与 `CompileDurable`）只为节点执行计步，路由不计步，并行节点整体计一步；最后一个允许的节点执行后仍会执行路由，路由到 `END` 即成功。因此：

- Loop 第 `MaxIterations` 轮结束时 `Until` 为真，正常结束；为假时尝试执行下一轮第一步，返回 `workflow.ErrMaxSteps`。
- Router 第 `MaxRounds` 次成员执行后，Supervisor 还会再执行一次（会再发一次模型请求）：返回 `Finish` 时正常结束；选择任一成员时返回 `workflow.ErrMaxSteps`。
- 计数存放在检查点中，重启后仍准确。需要“固定跑 N 轮后正常结束”的宿主在 T 中记录轮数，由 `Until` 判断。
- 三个构造器的 `ErrMaxSteps` 由执行器的步数检查产生，持久运行返回后检查点停在 `Next == MaxSteps`（`workflow/durable.go:274`），同一运行 ID 再 `Run` 仍然报错。宿主路由主动返回的 `ErrMaxSteps`（如“组合流程”）不满足这一条件：检查点停在该节点的路由位置（`Routing` 为真，`Next` 等于已执行的节点数，与 `MaxSteps` 没有固定关系；“组合流程”的示例中恰为 `MaxSteps - 1`），再 `Run` 时重新路由并得到同样的错误。宿主转人工或重新处理时使用新运行 ID，并为它创建新的根预算（新的 `budget.Config.RootKey`）；原根预算的用量不会因换运行 ID 而重置。

### 版本与迁移

持久图的计划摘要包含 `DurableConfig.Version` 与整个 `workflow.Config`（含 `MaxSteps`、`MaxConcurrency`），恢复时同时校验版本与摘要（`workflow/durable.go:121-131,409`）。因此修改 `MaxIterations`、`MaxRounds`、`MaxConcurrency`、步骤列表或审批节点后，旧运行不能交给新图恢复，提升 `Version` 也不行。宿主保留旧版本的构造参数编译旧图，继续处理未完成的旧运行；新版本只接收新运行。运行使用的版本不必另记：检查点本身带 `Version`（`workflow/durable.go:188`），宿主直接 `store.Load(runID)` 读出后选择对应版本的图。指南给出按版本分流的示例代码。

### 返回的图只用于编译

构造器返回的 `*workflow.Graph[T]` 只应直接 `Compile` 或 `CompileDurable`。继续 `AddNode` 不会报错，但已算好的 `MaxSteps` 不再准确；包文档注释写明这一警告。需要组合时用 `ParallelStep`，或按“组合流程”手写图。

### 路由失败与兜底

持久图提交节点输出后，恢复时用同一份已提交状态重新执行路由（`workflow/durable.go:257-265`）。`Next`、`Until` 是纯函数，无效输入每次恢复都得到同样的错误，运行只能换 ID 重来。因此：

- `TextNext` 在记录为空、首行为空或不匹配时返回包装 `ErrInvalidRoute` 的错误；自定义 `Next`、`Until` 判定输入无效时也应包装它。
- workflow 把路由错误直接作为运行错误，不会转成目标（`workflow/execute.go:68-71`、`workflow/durable.go:512-515`），所以兜底由包装路由完成。包装逻辑只有一份，即导出的 `GuardRoute`，构造器与宿主手写图共用。持久恢复调用的是同一个包装函数（`workflow/durable.go:262`），恢复时结果相同。
- `GuardRoute` 的构造校验：成员非空、互不重复，不得为 `Finish` 或 `workflow.END`；fallback 非空时不得与成员同名。RouterSupervisor 以成员名与 Fallback 名调用它，注册在 Supervisor 上。运行时按以下顺序处理：
  1. 调用 `Next`。返回错误时，错误满足 `errors.Is(err, ErrInvalidRoute)` 且配置了 Fallback 则返回 Fallback 名，否则原样返回错误，忽略同时返回的名字。
  2. 名字等于 `Finish` 时返回 `workflow.END`。
  3. 名字不在**成员集合**中时按无效处理：有 Fallback 返回 Fallback 名，否则返回包装 `ErrInvalidRoute` 的错误。成员集合只含成员，不含 Supervisor 名、Fallback 名和 `workflow.END` 字面值 `"__end__"`，不能用分支目标列表代替：Fallback 名与 END 都是合法分支目标（`workflow/graph.go:161-166`），workflow 会放行；Supervisor 名不是分支目标，workflow 报的错也不是 `ErrInvalidRoute`。
  4. 否则返回该成员名。
- Loop 把 `Until` 改写成 next（为真返回 `Finish`，为假返回第一步名，错误原样返回），以第一步名为唯一成员、Loop 的 Fallback 为兜底调用 `GuardRoute`，注册在最后一步上。
- 兜底 Step 执行后结束；它收到的状态里有无效的那次输出，可据此转人工。
- 没有配置 `Fallback` 时，`ErrInvalidRoute` 使运行失败，并且对该运行 ID 是终态，宿主换运行 ID 与新根预算重新处理。
- 不做“重问一次”的自动修复：重问需要再加一个节点、再请求模型，并改变步数公式，宿主需要时可以把它写进 Supervisor Step 自身。

### 路由、合并与映射必须是纯函数

`Until`、`Next`、`Merge` 在崩溃恢复后可能重复执行；`in` 的结果进入模型请求意图，`out` 在节点输出保存失败后随节点重入再次执行。它们必须只依据输入决定结果：不得调用模型、读写外部计数或产生副作用。需要模型判断的逻辑放进 Step（节点）里，由 `Next` 读取节点写回状态的结果。

### 组合流程

构造器之间不能嵌套：持久节点内不能运行已编译的图（见“支持的组合”）。需要“抽取 → 写作⇄评审循环 → 合规”这类组合时，宿主用 `workflow.NewGraph` 手写拓扑，用 `AddStep` 加入 `AgentStep`、`TextStep`、`ParallelStep` 返回的 Step，用 `GuardRoute` 包装自己的路由以复用兜底，并自行设置 `MaxSteps`。第一版不提供可摊平的 `LoopStep` 或轮数路由辅助函数：路由是纯函数，拿不到运行步数，它们同样只能从状态读轮数，只是多一层封装；等出现第二个需要组合的业务流程再评估。

- **状态类型**：一张图只有一个 T。复用 `TextStep`（`Step[Conversation]`）时整张图都用 `Conversation`，抽取与合规也写成 `AgentStep[Conversation]`，抽取出的订单号放在 Turn 里；需要类型化字段时，全部用 `AgentStep` 加自定义 T。
- **循环上限由路由负责**：总步数只约束整张图，不约束中间的循环。普通执行器每轮先执行节点再路由（`workflow/execute.go:24,66-81`），持久执行器先路由再检查步数（`workflow/durable.go:257-276`）；循环后面还有节点时，第 N 轮评审不通过后，留给合规的那一步会被第 N+1 次写作用掉。因此评审后的出口路由从状态推出轮数：以 `Conversation` 为状态时数评审成员的 Turn 个数，自定义 T 时由评审 Step 的 `out` 给计数字段加 1。判断顺序：先看解析错误（包装 `ErrInvalidRoute`），再看是否通过（通过到合规），最后看轮数；第 N 轮仍不通过时返回包装 `workflow.ErrMaxSteps` 的错误，与 Loop 构造器的超限语义相同（S8），持久运行停在该路由位置，同一运行 ID 再 `Run` 仍报错。
- **步数兜底与版本**：`MaxSteps` 设为 `1 + 2N + 1`，作为防失控的兜底，也正好够用。路由闭包里的 N 不进计划摘要，只有由 N 算出的 `MaxSteps` 进入（`workflow/durable.go:121-131`），所以示例用同一个常量计算两者，修改 N 后旧运行才会被拒绝恢复，按“版本与迁移”处理。
- 出口路由经 `GuardRoute(reviewExit, []string{"write", "compliance"}, fallback)` 包装后交给 `AddBranch`：`ErrInvalidRoute` 转兜底，`ErrMaxSteps` 属于其他错误，原样返回。因为合规节点留有一步，第 N 轮判断无效时 Fallback 能执行，这与 Loop 构造器“末轮无效返回 `ErrMaxSteps`”不同，指南说明这一点。

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
```

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

type TextOption func(*textConfig) // textConfig 不导出

func TextStep(name string, agent AgentFactory, opts ...TextOption) (Step[Conversation], error)
func WithRender(render func(Conversation) any) TextOption // 自定义输入格式
func WithLastTurns(k int) TextOption                       // 固定窗口：Input 加最近 k 轮
func CloneConversation(c Conversation) Conversation        // 复制 Turns，供 ParallelConfig.Clone 使用
func MergeTurns(ctx context.Context, base Conversation, branches []Conversation) (Conversation, error)
// TextNext 从成员 Step 推导名称，避免与 RouterConfig.Members 重复声明。
func TextNext(members ...Step[Conversation]) (func(context.Context, Conversation) (string, error), error)
```

`WithRender` 与 `WithLastTurns` 互斥，同时给出、`WithLastTurns` 的 `k <= 0` 或 `WithRender(nil)` 时，`TextStep` 返回错误。

- **默认输入**：`TextStep` 把 Input 加上每一轮的 `[Agent]: Output` 按顺序拼接，不截断；Router 的 Supervisor 因此能看到原始任务，Loop 的写作者能看到上一稿与评审意见。
- **增长与上限**：每轮输出长度近似固定时，单次请求随轮数线性增长，累计输入随轮数平方增长，检查点也反复序列化完整状态；固定轮数不能保证单次请求适配模型上下文。轮数较多的流程用 `WithLastTurns(k)` 或 `WithRender`；需要摘要时，把摘要做成单独的 Step，结果写回状态后由检查点保存，不新增摘要运行时。
- **render 的确定性契约**：render 必须是 `Conversation` 的纯函数，相同输入得到相同输出。模型请求意图包含完整请求，同一次节点访问恢复时 render 输出不同会使意图不一致而报错（`core/durable/operation.go:269-271`）。修改 render 或窗口大小等同于修改 Agent 行为，须按“版本与迁移”处理。不给选项时用默认格式。
- **内容隔离**：默认格式把客户输入与成员输出直接拼成 `[Agent]: Output`，外部文本可以伪造轮次或诱导分流。指南说明：高风险动作（如退款）必须在执行前由人确认具体参数（S5 的工具内确认），不能只靠分流判断；对外部输入敏感的流程用结构化 render（如 JSON 编码各轮）。
- **输出**：取 `AgentOutput.Result`，字符串原样使用，其他类型按 JSON 编码；追加一轮 `Turn{Agent: name}`，空回答也记录。
- **并行**：`Turns` 是切片，浅拷贝会让并行分支写同一底层数组，`ParallelConfig.Clone` 必须用 `CloneConversation`。`MergeTurns` 按分支登记顺序，追加各分支在 base 之后新增的 Turns；分支的 Turns 长度小于 base 或前缀不同时返回错误。
- **路由**：`TextNext` 取各成员 Step 的 `Name`，与传给 `RouterConfig.Members` 的是同一组 Step，名字不会拼写不一致。构造时校验成员名：非空、单行（不含 `\n`、`\r`）、互不重复、不等于 `Finish`，否则返回错误。运行时取最后一轮输出的第一行并去掉首尾空白，必须恰好等于某个成员名或 `Finish`；记录为空、首行为空或不匹配时返回包装 `ErrInvalidRoute` 的错误（见“路由失败与兜底”），不做模糊匹配。
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
- 模型请求默认只允许一次尝试（`builder/model_call.go:209-212`）。每次尝试先写 pending，执行后用 `context.WithoutCancel` 保存终态；恢复时按日志记录的状态处理（`core/durable/operation.go:286-296,338-351`）：

  | 发生了什么 | 记录 | 默认 | 开启 `WithInterruptedModelResend` |
  |---|---|---|---|
  | 请求发出后、终态落盘前进程崩溃（或终态保存本身失败） | pending（`ambiguous`） | 拒绝 | 尝试次数 < 3 时重发 |
  | 运行 ctx 被取消（宿主停止运行） | failed（`canceled`） | 拒绝 | 尝试次数 < 3 时重发 |
  | 错误链含 `context.DeadlineExceeded`（超时） | failed（`deadline`） | 拒绝 | 拒绝 |
  | 模型返回其他错误 | failed（`execution`） | 拒绝 | 拒绝 |

  “拒绝”指节点以 `*durable.OperationError` 失败（`errors.Is(err, durable.ErrOperationAttention)`，`Kind` 为上表括号中的值），同一运行 ID 无法继续，宿主换运行 ID 与新根预算重新处理。分类按优先级（`core/durable/operation.go:338-347`）：错误链含 `context.DeadlineExceeded` 时为 deadline；否则错误链含 `context.Canceled` 或父 ctx 已取消时为 canceled；其余为 execution。模型客户端把超时包装成取消时（如 `llm/common/base.go` 的重试在退避中遇到 ctx 结束，返回包装 `context.Canceled` 的错误，`errors/helpers.go:222-225`），超时也会记为 canceled 并可能被重发。
- 重发的 3 次是持久操作的尝试次数，不是请求次数。每次尝试内，`WithRequestError` 的钩子还可原样重试最多 `maxRecoveries` 次，计数在每次尝试时重新开始（`builder/loop_hooks.go:195-203,442,456`），所以同一回合最多发出 `3 × (1 + maxRecoveries)` 个请求。每个请求发送前在根预算预留一次（`builder/model_call.go:151,184`），真实请求总量由 `budget.Config.MaxModelRequests` 限制；模型客户端自身的重试（如 `llm/providers/deepseek.go:139-146`）不经过预算，接入持久运行时应关闭或计入估算。重发可能重复计费；开关在运行的整个生命周期内必须一致，否则恢复时报策略变更。

**预算与 Dispatcher**（`tools/call_scope.go:33-56`、`builder/model_call.go:196-201`）：

- `SupervisorTools` 的成员是模型工具，调用时 ctx 必须带根预算（`budget.WithController`）；持久运行必须是 `budget.NewDurable` 创建的持久预算。
- 持久模型请求的意图包含预算配置（`budget.Config`，含 `RootKey`、`Version`、各项上限与 `DeadlineUnixMilli`）与用量归属。恢复时必须复用原来的预算存储与完全相同的配置：不得在重启时重新计算截止时间、调整上限或改变归属。能拦截的违规与拦不住的情况：
  - 复用预算存储但配置变化：`budget.NewDurable` 在构造时就报配置变化（`core/budget/controller.go:88-99,160-161`）。
  - 换用新的预算存储且配置也变：已完成的模型请求因意图不一致无法回放（意图含 `c.Configuration()`，`builder/model_call.go:198-203`）。
  - 换用新的预算存储、配置相同：意图一致，回放已完成请求时因找不到预留报“usage has no reservation”（`core/budget/attribution.go:119-121`）；续跑时没有需要回放的请求则**完全拦不住**，用量从零重新计数，`MaxModelRequests` 可能被突破。这一条只能由宿主保证：预算存储与图存储一起持久化，恢复时总是复用。
- **人工等待与截止时间**：每次预留都会检查截止时间（`core/budget/controller.go:300-306`），而配置在运行生命周期内不能改。有人工等待（S5 的退款确认、S6 的追问）的流程，`DeadlineUnixMilli` 必须覆盖最长等待窗口（业务默认等待 48 小时，截止时间设为等待时限加余量），或者不设，改用 `MaxModelRequests` 控制花费、由宿主按等待时长实现运行 TTL（快照 `Waiting` 为真且超过时限即放弃，换运行 ID 转人工）。按单次请求时长设截止时间时，客户隔几小时回复，续跑中的模型请求必然以 `context.DeadlineExceeded` 失败，而且无法挽回。
- 成员 Agent 不得使用 Supervisor 或任何祖先调用的 Dispatcher，否则触发 `ErrDispatcherReentry`；成员之间互不嵌套时可以共用一个。

**状态 T**：

- 持久图首次执行也会对状态做 JSON 往返，并行分支输出在 `Merge` 前同样经过 JSON 往返（`workflow/durable.go:184,301`、`workflow/durable_parallel.go:52,91,98,225`）。T 必须语义稳定地往返：只用导出字段；不放 `any` 内的具体类型、依赖指针身份的数据，或数字类型会被改变的值。

**支持的组合**：

| 组合 | 是否支持 |
|---|---|
| `ParallelStep` 作为 Sequential、Loop、RouterSupervisor 的一步或 Router 成员 | 支持：同一张平面图，沿用分支检查点 |
| 普通 `Compile` 的图，用 `AddRunnable` 嵌进另一个普通图 | 支持 |
| 持久图通过 `AsChildRunnable` 包装成 `Resumable` 工具，交给 Agent（包括 `SupervisorTools` 的成员 Agent）作为工具 | 支持（已有路径） |
| 构造器之间嵌套（如 Sequential 的一步是 Loop） | **不提供**：宿主手写图，用 `AddStep`（含 `ParallelStep`）与 `GuardRoute` 复用，见“组合流程” |
| 把已编译的图（普通或 `AsChildRunnable`）作为节点放进持久图 | **不支持**：节点 ctx 只有 `AllowInterrupt`，没有 `CanWait`，`AsChildRunnable` 会拒绝；普通图在持久节点内不派生访问键，同一 Agent 重入会复用操作键 |
| `ParallelStep` 的分支再是 `ParallelStep` | **不支持**，构造报错 |

**审批与中断**：

- `DurableConfig.ApprovalNodes` 可填 Sequential、Loop 的 Step 名，Router 的 Supervisor、成员名，以及 Fallback 名；`ParallelStep` 只能填它的 `Step.Name`，分支名不是图节点，填入会被 `CompileDurable` 拒绝。
- 循环中列入审批的节点每次访问都要审批：意图键包含步数 `Next`（`workflow/durable.go:278`），上一轮的审批不覆盖下一轮。
- 审批后续跑从被审批的节点开始，之前已完成的节点不会重新执行，也不会读取它们的模型日志。
- `ApprovalNodes` 的审批发生在节点执行之前（`workflow/durable.go:279-293`），审批人只能看到进入节点前的状态，适合“是否进入某流程”这类粗粒度门禁；要审批 Agent 决定的具体动作（S5 的退款金额与订单），在执行该动作的工具里确认参数：
  - 退款工具用 `FunctionToolBuilder.WithInterruptible(version)` 声明可中断（`tools/function_tool.go:110-113`），经 Dispatcher 注册给退款 Agent。
  - 工具在产生副作用之前调用 `durable.AwaitInput(ctx, "refund-confirm", info, validate)`，`info` 含订单号与金额，宿主据此展示给审批人；输入为 `{"approved": bool, "note": string}`，validate 校验格式。
  - 续跑时工具整体重新执行，`AwaitInput` 直接返回已确认的输入，此后才执行退款，所以退款恰好执行一次；拒绝时工具不执行退款，把“人工拒绝”作为工具结果交还 Agent。
- 节点内调用 `durable.AwaitInput` 可以暂停（S6 的写法见“AgentStep 的执行”）。Agent 工具内的中断需要工具声明可中断（`tools.InterruptibleTool` / `WithInterruptible`），或是声明 `Resumable` 的 Runnable 工具。Agent 调用的子图如果是纯审批等待，在外层持久图里按节点失败处理（`workflow/durable.go:348-369`），这一限制写进指南。
- 回放只覆盖已提交为 completed 的操作。pending 操作是否重入取决于策略：默认非 Resumable 的 Runnable 工具与默认模型请求都只允许一次尝试，pending 与 failed 时恢复被拒绝，需要宿主核对（模型请求开启重发时 pending 与 canceled 除外，见“Agent 侧”）。

## 校验规则

| 位置 | 规则 |
|---|---|
| 通用 | 至少一个 Step；名称合法且唯一（沿用 `workflow` 校验）；`Step.Run` 非 nil，typed nil 按 nil 处理（与 `tools` 的 `nilRunnable` 一致） |
| AgentStep | agent、in、out 非 nil |
| TextStep | agent 非 nil；`WithRender` 与 `WithLastTurns` 互斥；`WithRender(nil)` 与 `WithLastTurns(k <= 0)` 报错 |
| AddStep | 图与 Step 非 nil，`Step.Run` 非 nil；名称冲突沿用 `workflow` 校验 |
| GuardRoute | next 非 nil；成员非空、互不重复、不为 `Finish` 或 `workflow.END`；fallback 不与成员同名 |
| ParallelStep | 名称合法；`Clone`、`Merge` 非 nil；至少一个分支；分支名在本节点内唯一；分支不得是 `ParallelStep`；构造时复制 `Steps` |
| Loop | `Until` 非 nil；`MaxIterations > 0`；`MaxIterations × n` 不溢出；Fallback 非 nil 时按 Step 通用规则校验，名称与各步不同 |
| RouterSupervisor | `MaxRounds > 0`；`2 × MaxRounds + 1` 不溢出；`Next` 非 nil；至少一个成员；成员名与 Fallback 名不得为 `Finish`、不得与 Supervisor 或彼此同名；Supervisor 与 Fallback 允许是 `ParallelStep`；`Next` 返回未声明的名字或 `workflow.END` 时按 `ErrInvalidRoute` 处理 |
| TextNext | 至少一个成员 Step；成员名单行、互不重复、不等于 `Finish` |
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
- 按规则序号断言：可以查询每条规则被匹配的次数，用来证明某次请求没有被重放。
- 每个角色（分流、写作、评审、各查询分支）使用独立实例，并行分支互不抢占回复；同一角色的实例在 Factory 之间共享，以保留脚本进度。

### 单元测试（`core.RunnableFunc` 作假 Agent）

- 三个图构造器与 `ParallelStep` 的正常流程，以及校验表每条规则，包括 typed nil、溢出、`cfg.MaxSteps` 非零、嵌套 `ParallelStep` 被拒绝、构造后修改调用方 `Steps` 切片不影响图。
- 普通与持久两种编译方式结果一致：每种图构造器各一例。
- 轮数边界：Loop 第 N 轮 `Until` 为真时正常结束、为假时返回 `ErrMaxSteps`；Router 第 R 次成员后 Finish 正常结束、再选成员返回 `ErrMaxSteps`。
- 兜底：Router 的 `Next` 返回 `ErrInvalidRoute`、未知成员名、`"__end__"` 时，有 Fallback 则执行兜底后结束，无 Fallback 则运行失败且错误满足 `errors.Is(err, ErrInvalidRoute)`；`Next` 返回其他错误时即使有 Fallback 也失败；`Next` 返回 Supervisor 名或 Fallback 名时按无效路由处理；Loop 的 `Until` 同理；最后一轮判断无效时，配置了 Fallback 返回 `ErrMaxSteps`（成员不会多执行一次），未配置返回 `ErrInvalidRoute`，两种各测一例；Fallback 可列入 `ApprovalNodes`。
- AgentStep 的 `in` 返回错误时不发出模型请求，`ScriptedClient.Calls()` 为 0。
- 组合：Sequential、Loop 中含 `ParallelStep`；Router 成员为 `ParallelStep`，以外层名审批，分支名填入 `ApprovalNodes` 被拒绝；`ParallelStep` 的 `Run` 被直接调用时返回错误。
- 并行失败路径：`Merge` 返回错误；`Clone` panic 时沿用既有 panic 边界（workflow 节点层记录后重新抛出，`core.StreamRunnable` 转为 `*errors.PanicError`），`Invoke` 返回该错误而不使进程崩溃；分支失败时取消兄弟分支且错误归属到正确分支；持久模式下分支输出已保存后重启，已完成的分支不重跑；分支输出 JSON 往返失败时报错。
- AgentStep：Factory 返回错误（且返回 Close 时仍调用 Close）、Runnable 为 typed nil、Agent 返回 `(nil, nil)`、执行错误与 Close 错误合并、`out` 返回错误时报错带节点名。
- 文本层：
  - 默认输入含 Input 与全部轮次；`WithRender` 自定义输入；`WithLastTurns(k)` 只含 Input 与最近 k 轮；两个选项同时给出、`k <= 0`、`WithRender(nil)` 时构造报错；
  - 非字符串 Result 的 JSON 编码；
  - `CloneConversation`：两个副本向同一预留容量位置追加不同的 Turn，断言互不影响（确定性断言，不依赖 `-race`）；
  - `MergeTurns` 顺序与长度、前缀校验；
  - `TextNext` 构造校验（多行、重复、`FINISH`）与运行时去空白；空记录、空首行、模糊匹配均返回包装 `ErrInvalidRoute` 的错误。
- `AddStep`：普通 Step 与 `ParallelStep` 分别注册，加入手写图后两种编译方式都能运行；名称冲突报错。
- `GuardRoute`：构造校验表各规则；运行时四步顺序（含 `Next` 返回 Supervisor 名、Fallback 名、`"__end__"`）；返回的 targets 恰为成员、END 与 fallback；`ErrMaxSteps` 等其他错误原样返回。

### 持久恢复测试

用 `durable.FileStore`、真实 Builder 与 `ScriptedClient`，装配持久根预算与独立 Dispatcher。“重启”指重建图、Agent、预算与存储对象，复用同一存储目录，不另起进程；`ScriptedClient` 实例跨重启保留、不重建，否则“`Calls()` 不增加”的断言恒为真。故障窗口一律用包装 Store 精确定位，不靠让模型请求报错来注入（那样会留下 failed 记录，使恢复被拒）：

- **模型回放**：模型操作 completed 后、节点输出保存前让图检查点保存失败；恢复后节点重新进入，`ScriptedClient.Calls()` 不增加。
- **工具 completed 回放**：工具操作 completed 后，按记录内容匹配下一个模型回合（`ID == "model/turn/1"` 且 `Status == "pending"`），只让这一次保存失败，并断言恰好命中一次；恢复后工具不再执行。
- **工具 pending**：默认（非 Resumable）工具在 pending 状态下恢复被拒绝；Resumable 工具 pending 时重入，第 4 次尝试时报尝试耗尽。
- **模型请求中断**：multiagent 只证明“图节点 → Agent → 模型操作”这条链路正确传递日志与取消，测两种状态；deadline 与 execution 两种由下层测试覆盖（`core/durable/operation_test.go:144-180`、`builder/model_resend_test.go:151`），重发与 RequestError 重试的叠加由 `builder/model_resend_test.go:164-186` 覆盖，均在指南中引用。
  - 前提：Agent 必须配置一个经 `Dispatcher.Tools()` 包装的工具，才会走工具回合路径，操作 ID 为 `model/turn/0`（`builder/durable_turn.go:39,54`）；没有工具时走 `model/answer/0`（`builder/events.go:191,214`），只拦截 `model/turn/0` 会打不中故障点。注入器记录命中次数，断言恰好命中一次。
  - 开关不能中途切换：重发开关改变 `MaxAttempts`，同一运行先用默认配置、再开启重发恢复会报策略变更（`core/durable/operation.go:269`、`builder/model_call.go:209`）。因此拆成四个独立子测试：pending、canceled 各自在“默认”和“开启重发”下，从首次执行起固定开关。
  - pending：包装 Store 只让 `model/turn/0` 的终态保存失败一次，pending 照常写入，恢复前解除故障。此时 `Do` 返回的内存记录已是 completed 并同时返回保存错误（`core/durable/operation.go:320,351`），所以必须重新读取存储断言落盘记录为 pending/`ambiguous`、`Attempts == 1`，并断言 `Calls() == 1`。重建后：默认配置报 `OperationError{Kind: "ambiguous"}` 且 `Calls()` 不变；开启重发的子测试恢复后完成，`Calls() == 2`，记录 `Attempts == 2`。
  - canceled：`ScriptedClient` 的一条规则阻塞到 `ctx.Done()` 并在进入时通知测试，测试随即取消运行；断言 `errors.Is(err, context.Canceled)`。取消时 Agent 的流立即返回（`builder/runnable.go:28-31`），内部 goroutine 可能还没保存终态，所以读取落盘记录必须走带操作锁的 `durable.InspectOperation`（先 `Lock` 再读，`core/durable/operation.go:190-201`，会等旧的 `Do` 释放），在与执行相同的键路径下读得 failed/`canceled`；pending 子测试也用同一方式读取。重建后：默认配置报 `Kind == "canceled"`；开启重发的子测试恢复后完成，`Calls()` 加 1。
- **跨轮隔离**：Loop 不同轮次的操作键不同，日志互不覆盖。
- **预算**：恢复后根预算不重复扣减。另分两个子测试：(a) 复用预算存储并改变 `budget.Config`（例如重新计算 `DeadlineUnixMilli`），`budget.NewDurable` 报配置变化；(b) 换用新的空预算存储（`RootKey` 相同、配置不同），恢复时已完成的模型请求报意图不一致；(c) 换用新的空预算存储、配置相同，回放已完成请求时报 “usage has no reservation”。“配置相同且没有需要回放的请求”拦不住，不写测试，指南写明由宿主保证。
- **兜底**：Router 分流首行无效时，持久运行转入 Fallback 并完成；无 Fallback 时同一运行 ID 再 `Run` 仍报 `ErrInvalidRoute`。
- **兜底的重新路由窗口**：包装 Store 只让“路由到 Fallback”的那次检查点保存失败一次（判断条件：非操作记录、`Key` 为空、`Next > 0`、状态 `Routing` 为假且 `Node` 为 Fallback 名；节点意图保存的 Key 非空，首次保存 `Next == 0`，节点输出提交时 `Routing` 为真，只有路由结果保存满足该条件，见 `workflow/durable.go:188,266-295,376`）。首次 `Run` 返回保存错误，快照停在 Supervisor 且 `Routing` 为真；重建后同一运行 ID 完成，分流 `Calls()` 不增加，Fallback 执行一次且收到无效输出。Loop 的 `Until` 无效同样一例。
- **render**：同一次节点访问恢复时 render 输出相同则回放；替换 render 后恢复报意图不一致。
- **契约前提**：Agent 未设置 `WithDurableScope`、工具未经 `Dispatcher.Tools()` 包装时，在模型调用前失败。
- **版本**：修改 `MaxIterations` 后用新图恢复旧运行报检查点不匹配；用旧版本构造参数编译的图可以继续旧运行。

### SupervisorTools 测试

- `ScriptedClient` 驱动 Supervisor 调用成员工具：每次调用新建会话并调用 Close（Factory 出错但返回 Close 时也调用）。
- 直接调用工具 `Invoke`（ctx 带根预算，`Args` 为非 nil 的空对象）缺 `task` 时得到 `*tools.ArgumentError`。
- Supervisor 的 Dispatcher 开启 `ReturnArgumentErrors` 时参数错误交还模型；默认配置下整个回合失败。
- 没有根预算时失败；持久运行中使用非持久预算时失败。
- Timeout：0 继承 Dispatcher 默认；正值覆盖默认并在超时后取消成员且仍调用 Close；负值构造报错；直接 `Invoke` 不受 Timeout 约束。
- Resumable 成员：放进 `CompileDurable` 的单节点图，或显式用 `durable.WithJournal` 加 `WithExecutionKey` 提供持久日志。成员 Agent 带一个声明了 `WithInterruptible` 的工具来产生等待，续跑后断言 `durable.OperationAttempt` 没有增加；无持久日志的调用被拒绝。

### 场景覆盖

每个场景一个端到端测试，放在 `multiagent/scenario_test.go`，用真实 Builder 与 `ScriptedClient`；基础语义已由上面的定向测试覆盖，场景测试只验证业务链路。

| # | 测试 | 编译方式 | 断言 | 示例 |
|---|---|---|---|---|
| S1 | `TestScenarioTicketRouting` | 普通与持久 | 分流依次选中退款、物流两个专业 Agent 后 FINISH；每次 Supervisor 的请求包含原始工单；分流角色共享一个脚本实例 | 是 |
| S2 | `TestScenarioReplyReview` | 普通与持久 | `Until` 解析评审结论；第一轮不通过、第二轮通过即结束；写作者第二轮请求包含上一稿与评审意见 | 是 |
| S3 | `TestScenarioParallelLookup` | 普通与持久 | 三个查询分支各用独立脚本，结果按登记顺序合并，回答 Agent 收到合并结果；整组计 1 步 | 是 |
| S4 | `TestScenarioPipeline` | 普通与持久 | 抽取出的订单号写入状态，后两步读到；合规不通过由 `out` 返回错误，报错带节点名 | 否（S3 已演示 Sequential） |
| S5 | `TestScenarioRefundApproval` | 仅持久 | 退款 Agent 发出带订单号与金额的退款工具调用，工具在执行前以中断等待，`Run` 返回 `ErrApprovalRequired`，中断的 Info 含该订单号与金额，退款尚未执行；重启后以批准输入续跑：退款恰好执行一次，审批前那次分流请求的规则没有被再次匹配，分流脚本 `Calls()` 恰好加 1（退款后 Supervisor 再请求一次得到 FINISH）；拒绝子测试中退款不执行，Agent 收到“人工拒绝”的工具结果；无争议子测试：规则判为无争议时不等待、直接退款一次；升级子测试：规则判为无争议但 Agent 标记有争议时进入等待；超时子测试：宿主以超时输入回应，按拒绝分支结束且退款不执行 | 是 |
| S6 | `TestScenarioAskCustomer` | 仅持久 | 自定义 Step 暂停并给出中断键；先提交格式无效的订单号，`Resume` 返回 `*workflow.ResumeRejectedError`，快照仍等待且该中断 `Rejected` 为真；再用同一中断键提交有效值后续跑完成；两次续跑中抽取 Agent 都不重新请求模型；补充后再次抽取使用另一个 scope；子测试“截止时间已过”：根预算截止时间早于续跑时间，续跑以 `errors.Is(err, context.DeadlineExceeded)` 失败，另一子测试截止时间覆盖等待窗口时正常完成 | 否（与 `examples/human-in-the-loop-interrupt` 重复） |
| S7 | `TestScenarioDelegation` | 普通成员经 Agent 运行；Resumable 成员放进 `CompileDurable` 单节点图 | 主 Agent 发出账单、技术两个结构化工具调用，再给出汇总回答；每次调用新建会话并调用 Close；Resumable 子测试中成员经可中断工具等待，续跑后完成 | 是 |
| S8 | `TestScenarioEscalation` | 普通：步数边界；持久：恢复断言 | 评审始终不通过时第 `MaxIterations` 轮后返回 `ErrMaxSteps`；Router 子测试：分流始终选成员，第 `MaxRounds` 次成员后返回 `ErrMaxSteps`；持久模式下同一运行 ID 再 `Run` 仍报错，换运行 ID 与新根预算后可重新处理 | 否（指南中说明） |
| S9 | `TestScenarioInvalidDecision` | 普通与持久 | 分流首行为“退款（原因…）”时转入转人工 Fallback 后结束，Fallback 收到的状态含该输出；评审结论无法解析时 Loop 同样转入 Fallback | 是 |
| 组合 | `TestScenarioComposedPipeline` | 普通与持久 | 手写图（状态为 `Conversation`）：抽取 → 写作⇄评审循环 → 合规，节点经 `AddStep` 加入，抽取后含一个 `ParallelStep`（并行查询），评审出口路由经 `GuardRoute` 包装。第 N 轮通过时 `MaxSteps` 恰好够用；子测试“始终不通过”：返回 `ErrMaxSteps`，写作与评审 `Calls()` 都等于 N，合规为 0；持久模式下快照停在评审节点、`Routing` 为真、步数 `1+2N`，同一运行 ID 再 `Run` 仍报 `ErrMaxSteps` 且各脚本 `Calls()` 不增加；子测试“评审结论无法解析”：转入 Fallback 后结束 | 是 |

### 离线示例与其他

- `examples/multi-agent-patterns` 演示 S1、S2、S3、S5、S7、S9 与组合流程，用 `ScriptedClient`，不需要 API 密钥；各场景共用一个装配辅助函数。
- 示例验收固化为测试：示例逻辑放在 `run(ctx context.Context, w io.Writer) error`，`main` 以 `os.Stdout` 调用；每个场景结束时向 w 写一行完成标记（如 `S1 完成`）。同目录 `main_test.go` 在 goroutine 中调用 `run`，用独立的 30s 计时器等待（不只依赖 ctx 超时：图会等节点关闭流，不响应取消的节点会让同步调用挂住，见 `workflow/execute.go:96-102`；`-race` 下 5s 偏紧），超时即 `t.Fatalf`；返回后断言错误为 nil，并逐一断言 S1、S2、S3、S5、S7、S9 与组合流程的完成标记都出现。w 用带锁的 Writer，测试读取输出时也持同一把锁，避免超时分支读取输出时触发 `-race`。随 `go test ./...` 执行，先例为 `examples/integration/human-in-loop/hitl_demo_test.go:28-38`。现有 `scripts/verify_all_examples.sh` 用 `go run` 计时且把超时记为通过、失败不返回非零（`scripts/verify_all_examples.sh:60,75,102`），不能作为这一验收；本次不修改该脚本。
- `scripts/apicheck` 新规则的回归测试。

## 文档

- 新增 `docs/guides/MULTI_AGENT_PATTERNS.md`：以 S1–S9 为线索讲选择模式、状态设计、持久接入契约（含预算配置不可变、模型 pending 与 `WithInterruptedModelResend` 的取舍）、轮数上限与超限转人工、路由失败与兜底、版本分流示例、文本层的窗口与 render 确定性、内容隔离与工具内确认（S5，含规则优先的争议判定与超时按拒绝处理）、人工等待与预算截止时间及运行 TTL、并行分支的事件、支持的组合与手写组合流程（`AddStep`、`GuardRoute`）、两种 Supervisor 的取舍、运营监控（Fallback 执行率、`ErrMaxSteps` 与 `ErrInvalidRoute` 发生率，可从节点事件统计）。
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
- 构造器之间的嵌套组合与可摊平的 `LoopStep`（组合流程手写图，用 `AddStep` 与 `GuardRoute` 复用）；
- 文本记录的模型摘要运行时（摘要做成普通 Step）；
- 路由决策的模糊匹配、模型输出自动修复或自动重问（无效时走 Fallback）；
- 修改 `scripts/verify_all_examples.sh`。

## 相关问题（不在本设计范围，另开小改动）

- `builder/config_options.go:136-138` 的 `WithInterruptedModelResend` 注释写“同一回合最多发出 3 次”“模型明确失败的请求不受影响”，与“Agent 侧”的契约表不符：3 次是持久尝试次数，取消也记为 failed 且会被重发。
- 模型客户端的退避重试在 ctx 结束时返回包装 `context.Canceled` 的错误（`errors/helpers.go:222-225`），超时因此可能被记为 canceled 并被重发。
- provider 内部重试（如 `llm/providers/deepseek.go:139-146`）不经过根预算的预留。
