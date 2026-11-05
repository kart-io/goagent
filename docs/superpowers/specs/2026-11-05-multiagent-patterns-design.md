# 多 Agent 预置模式设计

日期：2026-11-05。基线 `f2e242b5`。

## 目标

在主模块新增顶层包 `multiagent`，提供 Sequential、Parallel、Loop 与两种 Supervisor 预置模式。成功标准：

- 宿主不必手写图拓扑即可编排多个 Agent；
- 前四种模式产出的图用 `CompileDurable` 编译后，检查点、审批、动态中断与根预算照常生效，重启后已完成的模型请求不重放；
- 不新增运行时、调度器或消息投递，所有执行仍走 `workflow`、`builder`、`tools`。

## 与定位决策的关系

[定位决策](../../research/2026-10-29-goagent-positioning.md) §4.2 删除了旧 `multiagent`（依赖 Core NATS、无持久消费），并规定预置模式不进核心。本设计是记录在案的例外：新包只组合已有基础件，不引入外部依赖；旧包的分布式投递语义不恢复。实现时同步修改定位文档 §4.2、§5 与 [Eino 对照](../../research/2026-11-04-eino-comparison.md)的“差距与采纳范围”。

## 方案选择

| 方案 | 结论 |
|---|---|
| A. 每种模式一个构造函数，返回未编译的 `*workflow.Graph[T]` | **采用**：宿主选 `Compile` 或 `CompileDurable`，持久语义直接继承 |
| B. 仿 Eino ADK 自建 Agent 接口、Runner 与事件迭代器 | 否决：与 `workflow`/`builder` 重复 |
| C. 单一 `New(Mode, opts...)` | 否决：各模式参数差异大，类型约束弱 |

## 包与依赖

- 路径 `github.com/kart-io/goagent/multiagent`，依赖 `core`、`builder`、`tools`、`workflow`、`interfaces`。
- `scripts/apicheck` 新增规则：`interfaces`、`errors`、`utils`、`cache`、`core`、`builder`、`tools`、`parsers`、`llm`、`workflow`、`memory`、`store`、`middleware`、`observability`、`devtools`、`harness`、`execution`、`plugin` 禁止导入 `multiagent`；同步 `main_test.go` 与 `docs/architecture/IMPORT_LAYERING.md`。
- 与 v0.17 删除的旧包同名不同义，CHANGELOG 注明。

## 底层 API：类型化状态

```go
type Step[T any] struct {
    Name string
    Run  core.Runnable[T, T]
}

// AgentStep 把 builder Agent 接成一步：in 从状态取任务输入，out 把输出写回状态。
func AgentStep[T any](name string, agent core.Runnable[any, *builder.AgentOutput],
    in func(T) any, out func(T, *builder.AgentOutput) (T, error)) Step[T]

func Sequential[T any](cfg workflow.Config, steps ...Step[T]) (*workflow.Graph[T], error)

type ParallelConfig[T any] struct {
    Name  string // 并行节点名
    Steps []Step[T]
    Clone func(T) T
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
    Name, Description string
    Factory           func(context.Context) (tools.OwnedRunnable[any, *builder.AgentOutput], error)
    Resumable         bool
    Timeout           time.Duration
}
func SupervisorTools(members ...Member) ([]interfaces.Tool, error)
```

### 图拓扑

| 模式 | 拓扑 | `MaxSteps` |
|---|---|---|
| Sequential | `START → s1 → … → sn → END` | `n` |
| Parallel | `START → p → END`，`p` 为 `AddParallel` 节点，分支即各 Step | `1` |
| Loop | `START → s1 → … → sn`，`sn` 上的分支：`Until` 为真到 `END`，否则回 `s1` | `MaxIterations × n` |
| RouterSupervisor | `START → sup`，`sup` 上的分支按 `Next` 到某成员或 `END`；每个成员回 `sup` | `2 × MaxRounds + 1` |

`MaxSteps` 由构造函数计算。宿主在 `cfg.MaxSteps` 设了非零值时构造报错，避免与轮数上限冲突；`cfg.MaxConcurrency` 照常传给并行节点。

### 轮数耗尽按失败处理

持久图的步数只存在检查点中，路由函数看不到，T 里也没有计数器，因此轮数上限用图的 `MaxSteps` 实现，重启后仍然准确。代价是 Loop 达到 `MaxIterations`、Router 用完 `MaxRounds` 时都返回 `workflow.ErrMaxSteps`，不能正常结束。需要“固定跑 N 轮”的宿主在 T 中记录轮数，由 `Until` 判断。

### 组合方式

返回的图不应再添加节点，否则步数上限失效。嵌套时先编译，再用 `Graph.AddRunnable` 或 `DurableGraph.AsChildRunnable` 放进外层图。

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

func TextStep(name string, agent core.Runnable[any, *builder.AgentOutput]) Step[Conversation]
func TextNext(members ...string) func(context.Context, Conversation) (string, error)
func MergeTurns(ctx context.Context, base Conversation, branches []Conversation) (Conversation, error)
```

- `TextStep` 的输入：有 Turns 时取最后一项的 Output，否则取 Input。输出取 `AgentOutput.Result`：字符串原样使用，其他类型按 JSON 编码；追加一轮 `Turn{Agent: name}`，空回答也记录。
- `MergeTurns` 按分支登记顺序追加各分支在 base 之后新增的 Turns，`Conversation` 为值类型，`Clone` 直接返回输入。
- `TextNext` 取 Supervisor 最后一轮输出的第一行，去掉首尾空白后必须恰好等于某个成员名或 `Finish`，否则返回错误，不做模糊匹配。

## 持久化与续跑

- 每个 Step 是一个图节点。`CompileDurable` 在节点边界落盘；节点内 ctx 带持久日志与节点执行键，`builder` 据此记录模型回合与工具调用（`builder/durable_turn.go`），重新执行节点时回放已完成的请求。
- 循环与 Router 的每次节点访问有不同执行键，各轮日志独立。
- 并行分支沿用 `durable_parallel`：分支级检查点，中断等待不取消兄弟分支。
- `DurableConfig.ApprovalNodes` 可填 Step 名；节点内可调用 `durable.AwaitInput`。
- T 须可 JSON 序列化，与持久图要求一致。
- `SupervisorTools` 经 `tools.NewRunnableToolFactory` 包装，固定 `ModelInvoking: true`，每次调用由 Factory 新建会话；`Resumable` 按成员声明，开启前提与 `RunnableToolConfig.Resumable` 相同。

## 校验规则

| 位置 | 规则 |
|---|---|
| 通用 | 至少一个 Step；名称合法且唯一（沿用 `workflow` 校验）；`Run` 非 nil；`AgentStep` 的 agent、in、out 非 nil |
| Parallel | `Name`、`Clone`、`Merge` 必填，Step 名不得与 `Name` 相同 |
| Loop | `MaxIterations > 0` |
| RouterSupervisor | `MaxRounds > 0`；`Next` 非 nil；至少一个成员；成员名不得为 `Finish` 或与 Supervisor 同名；`Next` 返回未声明的名字时运行失败 |
| SupervisorTools | `Name`、`Description`、`Factory` 必填，名称唯一 |
| 配置 | `cfg.MaxSteps` 必须为 0 |

运行中某步失败时，错误带节点名，图终止；并行分支失败取消其余分支（沿用 `workflow`）。

## 测试

- 单元测试用 `core.RunnableFunc` 作假 Agent：四种图模式的正常流程与上表每条规则；Loop 的 `Until` 提前退出与耗尽返回 `ErrMaxSteps`；Router 的 Finish、未知名字、轮数耗尽，`MaxSteps` 边界恰好允许 `MaxRounds` 次成员执行。
- 持久测试用 `durable.FileStore`：Sequential 第二步首次失败、重跑后第一步不再调用；Loop 各轮执行键不同；Router 在成员前审批后续跑；Parallel 分支 `AwaitInput` 等待不取消兄弟分支。
- 文本层：`TextStep` 输入取值、非字符串 Result 编码、`MergeTurns` 顺序、`TextNext` 去空白与拒绝模糊匹配。
- `SupervisorTools`：用离线模拟模型驱动 Supervisor Agent 调用成员工具，确认每次调用新建会话。
- 分层：`scripts/apicheck` 新规则的回归测试。
- 离线示例 `examples/multi-agent-patterns` 演示四种模式，不需要 API 密钥，纳入 `scripts/verify_all_examples.sh`。

## 文档

- 新增 `docs/guides/MULTI_AGENT_PATTERNS.md`：选择模式、状态设计、持久化、轮数耗尽语义、两种 Supervisor 的取舍。
- README 中英文文档索引各加一行；`docs/guides/AGENT_COMPOSITION.md` 的选择表链接到新指南。
- 修改定位文档与 Eino 对照（见上文）；CHANGELOG Added。

## 不做

- 跨进程消息投递、后台子 Agent、Agent 间共享对话历史的隐式传递；
- Agent 转交（transfer）语义与 Deep、PlanExecute 预置；
- 路由决策的模糊匹配或模型输出自动修复。
