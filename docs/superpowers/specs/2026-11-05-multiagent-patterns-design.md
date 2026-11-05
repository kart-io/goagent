# 多 Agent 预置模式设计

日期：2026-11-05。基线 `f2e242b5`。2026-11-05 按 Codex 审阅（14 项）修订。

## 目标

在主模块新增顶层包 `multiagent`，提供 Sequential、Parallel、Loop 与两种 Supervisor 预置模式，以及文本接力便捷层。成功标准：

- 宿主不必手写图拓扑即可编排多个 Agent；
- 满足“持久接入契约”的 Agent 放进前四种模式、用 `CompileDurable` 编译后，节点检查点、审批与根预算照常生效；重启后已提交为 completed 的模型与工具操作从日志回放，不重复执行；
- 不新增运行时、调度器或消息投递，所有执行仍走 `workflow`、`builder`、`tools`。

## 与既有决策的关系

[定位决策](../../research/2026-10-29-goagent-positioning.md) §4.2 删除的是依赖 Core NATS、无持久消费的旧 `multiagent`；“预置模式不进核心、需要时归 harness”的结论记在 [Eino 对照](../../research/2026-11-04-eino-comparison.md)的“差距与采纳范围”。本设计推翻后者：新包只组合已有基础件，不引入外部依赖，不恢复旧包的分布式投递语义。实现时同步修改 Eino 对照该行，并在定位文档 §5 记录这次能力补充。

## 方案选择

| 方案 | 结论 |
|---|---|
| A. 每种模式一个构造函数，返回未编译的 `*workflow.Graph[T]` | **采用**：宿主选 `Compile` 或 `CompileDurable`，持久语义直接继承 |
| B. 仿 Eino ADK 自建 Agent 接口、Runner 与事件迭代器 | 否决：与 `workflow`/`builder` 重复 |
| C. 单一 `New(Mode, opts...)` | 否决：各模式参数差异大，类型约束弱 |

## 包与依赖

- 路径 `github.com/kart-io/goagent/multiagent`，只依赖 `core`、`core/budget`（仅文档示例与测试）、`builder`、`tools`、`workflow`、`interfaces`。
- `scripts/apicheck` 新增反向依赖规则：`interfaces`、`errors`、`utils`、`cache`、`core`、`builder`、`tools`、`parsers`、`llm`、`workflow`、`memory`、`store`、`middleware`、`observability`、`devtools`、`harness`、`execution`、`plugin` 禁止导入 `multiagent`；同步 `main_test.go` 与 `docs/architecture/IMPORT_LAYERING.md`。检查器不约束新包自身的依赖，由代码审查把关。
- 包文档注释与 CHANGELOG 都写明：与 v0.17 删除的旧 `multiagent` 同名，但 API 与语义不兼容，旧代码不能直接迁移。

## 底层 API：类型化状态

```go
type Step[T any] struct {
    Name string
    Run  core.Runnable[T, T]
}

// AgentFactory 为每次节点执行新建一个 Agent 会话，避免循环与重复调用共享可变记忆。
type AgentFactory func(context.Context) (tools.OwnedRunnable[any, *builder.AgentOutput], error)

// AgentStep 把 Agent 接成一步：in 从状态取任务输入，out 把输出写回状态。
func AgentStep[T any](name string, agent AgentFactory,
    in func(T) any, out func(T, *builder.AgentOutput) (T, error)) (Step[T], error)

func Sequential[T any](cfg workflow.Config, steps ...Step[T]) (*workflow.Graph[T], error)

type ParallelConfig[T any] struct {
    Name  string // 并行节点名，也是审批时唯一可用的名字
    Steps []Step[T]
    Clone func(T) T // 必须深拷贝分支会修改的切片、映射与指针字段
    Merge func(context.Context, T, []T) (T, error)
}
func Parallel[T any](cfg workflow.Config, p ParallelConfig[T]) (*workflow.Graph[T], error)

type LoopConfig[T any] struct {
    Steps         []Step[T]
    Until         func(context.Context, T) (bool, error) // 每轮结束后判断；nil 表示不提前退出
    MaxIterations int                                    // 必填，> 0
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
    Timeout                    time.Duration
}
func SupervisorTools(members ...Member) ([]interfaces.Tool, error)
```

`AgentStep` 每次执行先调用 Factory，执行后调用 `Close`；Factory 返回错误、`Close` 为 nil 或 Runnable 为 nil 时该步失败。`Close` 的错误与执行错误合并返回。

### 图拓扑

| 模式 | 拓扑 | `MaxSteps` |
|---|---|---|
| Sequential | `START → s1 → … → sn → END` | `n` |
| Parallel | `START → p → END`，`p` 为 `AddParallel` 节点，分支即各 Step | `1` |
| Loop | `START → s1 → … → sn`，`sn` 上的分支目标为 `s1` 与 `END`：`Until` 为真到 `END`，否则回 `s1` | `MaxIterations × n` |
| RouterSupervisor | `START → sup`，`sup` 上的分支目标为全部成员与 `END`（`Next` 返回 `Finish` 时映射为 `END`）；每个成员回 `sup` | `2 × MaxRounds + 1` |

`Until` 为 nil 时分支仍声明 `END` 出口，以通过 `Compile` 的出口校验。`MaxSteps` 由构造函数计算，先检查乘法与加法溢出（超出 `int` 范围构造报错）；宿主在 `cfg.MaxSteps` 设了非零值时构造报错。`cfg.MaxConcurrency` 照常传给并行节点。

### 轮数上限语义

两种执行器（`Compile` 与 `CompileDurable`）只为节点执行计步，路由不计步，并行节点整体计一步；最后一个允许的节点执行后仍会执行路由，路由到 `END` 即成功。因此：

- Loop 第 `MaxIterations` 轮结束时 `Until` 为真，正常结束；为假（或 `Until` 为 nil）时尝试执行下一轮第一步，返回 `workflow.ErrMaxSteps`。
- Router 第 `MaxRounds` 次成员执行后，Supervisor 还会再执行一次：返回 `Finish` 时正常结束；选择任一成员时返回 `workflow.ErrMaxSteps`。
- 计数存放在检查点中，重启后仍准确。需要“固定跑 N 轮后正常结束”的宿主在 T 中记录轮数，由 `Until` 判断。

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
func CloneConversation(c Conversation) Conversation // 复制 Turns，供 ParallelConfig.Clone 使用
func MergeTurns(ctx context.Context, base Conversation, branches []Conversation) (Conversation, error)
func TextNext(members ...string) func(context.Context, Conversation) (string, error)
```

- `TextStep` 的输入：有 Turns 时取最后一项的 Output，否则取 Input。输出取 `AgentOutput.Result`：字符串原样使用，其他类型按 JSON 编码；追加一轮 `Turn{Agent: name}`，空回答也记录。
- `Conversation` 的 `Turns` 是切片，浅拷贝会让并行分支写同一底层数组，`ParallelConfig.Clone` 必须用 `CloneConversation`。
- `MergeTurns` 按分支登记顺序，追加各分支在 base 之后新增的 Turns；分支的 Turns 长度小于 base 或前缀不同时返回错误。
- `TextNext` 取最后一轮输出的第一行，去掉首尾空白后必须恰好等于某个成员名或 `Finish`，否则返回错误，不做模糊匹配。

## SupervisorTools 的工具协议

每个成员包装为 `tools.NewRunnableToolFactory[any, *builder.AgentOutput]`：

| 项 | 取值 |
|---|---|
| ArgsSchema | `{"type":"object","properties":{"task":{"type":"string"}},"required":["task"],"additionalProperties":false}` |
| Decode | 取 `task` 字符串作为 Agent 输入；缺失或非字符串时返回参数错误 |
| Encode | 返回 `AgentOutput.Result`：字符串原样，其他类型按 JSON 编码 |
| Version | `Member.Version`，必填 |
| ModelInvoking | 固定 `true` |
| Resumable | `Member.Resumable`；前提与 `RunnableToolConfig.Resumable` 相同：成员内所有效果都记在派生键下 |
| Factory | 每次调用新建会话，`Close` 必须非 nil |

宿主把返回的工具交给 Supervisor Agent 的 Dispatcher，再经 `Dispatcher.Tools()` 注册到 Agent。

## 持久接入契约

`multiagent` 不能检查抽象 Runnable 内部的配置，下列要求由宿主满足，指南与示例逐条演示。

**Agent 侧**（`builder/durable_turn.go:121-150`）：

- 每个 Agent 用 `WithDurableScope` 设置稳定的非空 scope。日志键为“节点执行键 → `agent/<scope>` → 模型或工具操作键”；每个 Step 是独立节点，同一 scope 在不同节点、不同轮次下互不冲突。
- 带工具时，模型客户端必须实现 `llm.ToolStreamClient`，工具必须经 `Dispatcher.Tools()` 包装（实现 `interfaces.DurableCallTool`）。
- 会话由 `AgentFactory` 每次新建。记忆与检索的读取结果参与模型请求的意图哈希：恢复时读到的内容变化，会使已完成请求的意图不一致而报错，不会静默重放。需要可靠恢复的 Agent 应避免在节点内读取会变化的外部记忆。

**预算与 Dispatcher**（`tools/call_scope.go:33-51`）：

- `SupervisorTools` 的成员是模型工具，调用时 ctx 必须带根预算（`budget.WithController`）；持久运行必须是 `budget.NewDurable` 创建的持久预算。
- Supervisor 与每个成员 Agent 使用各自独立的 Dispatcher，复用会触发 `ErrDispatcherReentry`。

**状态 T**：

- 持久图首次执行也会对状态做 JSON 往返，并行分支输出在 `Merge` 前同样经过 JSON 往返。T 必须语义稳定地往返：只用导出字段；不放 `any` 内的具体类型、依赖指针身份的数据，或数字类型会被改变的值。
- 测试对每种模式比较 `Compile` 与 `CompileDurable` 的结果一致。

**支持的组合**：

| 组合 | 是否支持 |
|---|---|
| 普通 `Compile` 的图，用 `AddRunnable` 嵌进另一个普通图 | 支持 |
| 持久图通过 `AsChildRunnable` 包装成 `Resumable` 工具，交给 Agent 或 `SupervisorTools` 成员 | 支持（已有路径） |
| 把已编译的图（普通或 `AsChildRunnable`）作为节点放进持久图 | **不支持**：节点 ctx 只有 `AllowInterrupt`，没有 `CanWait`，`AsChildRunnable` 会拒绝；普通图在持久节点内不派生访问键，同一 Agent 重入会复用操作键 |

**审批与中断**：

- `DurableConfig.ApprovalNodes` 可填 Sequential、Loop 的 Step 名与 Router 的 Supervisor、成员名；Parallel 只能填整体并行节点 `Name`，分支没有独立的审批点。
- 节点内直接调用 `durable.AwaitInput` 可以暂停。Agent 工具内的中断需要工具显式声明可中断（`WithInterruptible`）。Agent 调用的子图如果是纯审批等待，在外层持久图里按节点失败处理（`workflow/durable.go:348-369`），这一限制写进指南。
- 回放只覆盖已提交为 completed 的操作；外部请求已完成但日志未提交时为 pending，恢复按 `durable` 既有规则处理，可能需要宿主核对。

## 校验规则

| 位置 | 规则 |
|---|---|
| 通用 | 至少一个 Step；名称合法且唯一（沿用 `workflow` 校验）；`Run` 非 nil，typed nil 按 nil 处理（与 `tools` 的 `nilRunnable` 一致） |
| AgentStep / TextStep | agent、in、out 非 nil（含 typed nil），否则返回错误 |
| Parallel | `Name`、`Clone`、`Merge` 必填；Step 名不得与 `Name` 相同 |
| Loop | `MaxIterations > 0`；`MaxIterations × n` 不溢出 |
| RouterSupervisor | `MaxRounds > 0`；`2 × MaxRounds + 1` 不溢出；`Next` 非 nil；至少一个成员；成员名不得为 `Finish` 或与 Supervisor 同名；`Next` 返回未声明的名字时运行失败 |
| SupervisorTools | `Name`、`Description`、`Version`、`Factory` 必填，名称唯一 |
| 配置 | `cfg.MaxSteps` 必须为 0 |

运行中某步失败时，错误带节点名，图终止；并行分支失败取消其余分支（沿用 `workflow`）。

## 测试

**单元测试**（`core.RunnableFunc` 作假 Agent，普通与持久两种编译方式各跑一遍并比较结果）：

- 四种图模式的正常流程与校验表每条规则，包括 typed nil、溢出、`cfg.MaxSteps` 非零。
- 轮数边界：Loop 第 N 轮 `Until` 为真时正常结束、为假时返回 `ErrMaxSteps`；Router 第 R 次成员后 Finish 正常结束、再选成员返回 `ErrMaxSteps`；未知成员名失败。
- 文本层：`TextStep` 输入取值、非字符串 Result 编码、`CloneConversation` 后并行追加互不影响（`Turns` 预留容量，`-race` 运行）、`MergeTurns` 顺序与前缀校验、`TextNext` 去空白与拒绝模糊匹配。

**持久测试**（`durable.FileStore`，真实 Builder 与离线模拟模型，装配持久根预算与独立 Dispatcher）：

- 模型回合完成后节点输出保存失败，恢复后该节点内的模型请求从日志回放，模拟模型的调用次数不增加；
- 工具执行完成后进程中断，恢复后工具不再执行；
- Loop 不同轮次的操作键不同，日志互不覆盖；
- 恢复后根预算不重复扣减；
- Router 在成员节点前审批，续跑后按审批结果继续；
- Parallel 分支内 `AwaitInput` 等待时，兄弟分支照常完成；
- Agent 未设置 `WithDurableScope`、工具未经 `Dispatcher.Tools()` 包装时，在模型调用前失败。

**SupervisorTools**：离线模拟模型驱动 Supervisor 调用成员工具；确认每次调用新建会话并调用 Close；参数缺 `task` 时返回参数错误；没有根预算时失败。

**其他**：`scripts/apicheck` 新规则的回归测试；离线示例 `examples/multi-agent-patterns` 演示四种模式与两种 Supervisor，不需要 API 密钥，纳入 `scripts/verify_all_examples.sh`。

## 文档

- 新增 `docs/guides/MULTI_AGENT_PATTERNS.md`：选择模式、状态设计、持久接入契约、轮数上限语义、支持的组合、两种 Supervisor 的取舍。
- README 中英文文档索引各加一行；`docs/guides/AGENT_COMPOSITION.md` 的选择表链接到新指南。
- 修改 Eino 对照与定位文档 §5（见上文）；CHANGELOG Added。

## 不做

- 跨进程消息投递、后台子 Agent、Agent 间共享对话历史的隐式传递；
- Agent 转交（transfer）语义与 Deep、PlanExecute 预置；
- 在持久图节点内嵌套已编译图的适配；
- 路由决策的模糊匹配或模型输出自动修复。
