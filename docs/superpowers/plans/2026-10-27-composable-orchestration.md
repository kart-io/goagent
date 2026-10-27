# GoAgent 可组合编排 R2 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task. Use subagent-driven-development only when that execution approach is selected. Checkboxes track future implementation, not this document review.

**Goal:** 在保留现有执行和恢复契约的前提下，交付受控工具化、父子预算、固定内容引用和只读观察，并验证小型增量管道。

**Architecture:** 以现有 Runnable、Dispatcher、durable.Do 和 OTel 为底座。模型准入与计量在 Builder 统一请求入口执行；复合工具先采用不透明一次尝试语义。上下文先拆内部组件，流处理先原型，通用反射映射后置。

**Tech Stack:** Go 1.25.0、既有 JSON Schema/core/durable/OpenTelemetry、标准库 context/encoding/json/html/template；不新增外部编排依赖。

**Spec:** [R2 需求](../specs/2026-10-27-composable-orchestration-requirements.md)  
**Review:** [多框架依据与复审记录](../../research/2026-10-27-framework-orchestration-review.md)

状态：R2 首轮任务已实施（流 API 为实验性）；本文件替代初版 T1–T11 的交付顺序与候选 API。代码基线为 `3bfeea70fcce35c3e925a295cc42546c1ec80b1b`。

## Global Constraints

- Go 版本保持 1.25.0；不新增外部编排或前端运行依赖；不向 core.Runnable 添加必需方法。
- 旧 Pipe、Graph、Adapt/AdaptStream 的默认语义不变；未配置新功能的模型/工具意图哈希不变。
- 流数据和观察事件不提交业务状态；持久图仍等待节点合法终态及 EOF。
- 不透明复合工具的 pending/failed 不自动续跑；目标服务负责外部幂等，不承诺 exactly-once。
- 新功能显式配置；模型型子工具要求根预算；只有被统一模型入口覆盖的请求才受预算控制。
- 首轮不发布反射映射、完整四范式流运行时、通用 Debugger.Run、嵌套审批与 Handoff。
- 各任务先验证失败测试、再实现、再验证通过并独立提交；文档交付阶段不修改生产代码。

## Review Focus

1. 父子预算并发超额、重启重置与回放重复计量：T1/T2/T4。
2. 父包装未完成但子操作已完成时被错误宣称可恢复：T3/T4。
3. 子会话指针不同但 SessionID/记忆命名空间相同，以及 factory 资源泄漏：T4。
4. 稳定引用 key 被覆盖、配置未入指纹、保存顺序错位：T6/T7。
5. 观察身份重复 OTel、正文泄漏、流先展示后失败被提交：T5/T8/T9。

## 1. 文件职责与依赖

| 组件 | 拟新增/修改文件 | 职责 |
| --- | --- | --- |
| 根预算 | `core/budget/controller.go`、`durable.go`、`usage.go` | 原子准入、独立持久账本、操作键计量 |
| 模型入口 | `builder/model_call.go`、`durable_turn.go`、`tool_events.go`、`context_policy.go` | 统一真实调用前预算、局部预算、失败/回放用量 |
| 复合工具 | `tools/runnable_tool.go`、`call_scope.go`、`dispatcher.go` | 编解码、版本、一次尝试策略、深度/重入/资源归属 |
| 观察 | `core/observe/event.go`、`observability/execution.go`、`workflow/describe.go` | 窄事件出口、复用 OTel 的桥接、拓扑 |
| 上下文 | `builder/contextpolicy/policy.go`、`offload.go`、`store.go`、`durable_store.go` | 组件抽取、窄策略、固定内容引用 |
| 流原型 | `core/flow/source.go`、`fold.go`、`adapter.go` | 同步只读 Source/Map/Fold，无第二套 Reader/Frame |
| 调试 | `devtools/recorder.go`、`report.go` | 有界只读记录与后续离线 HTML |
| 业务示例 | `examples/orchestration/` | 图工具、子 Agent、类型化适配、引用恢复和流原型 |

core/budget 可以依赖 core/durable 和 interfaces，但 durable 不反向导入 budget。core/observe 的事件出口不依赖 workflow/tools/builder；OTel 桥接放 observability，既有业务入口通过窄接口接入。tools 只接受 core.Runnable，不导入 builder/workflow。Builder 用宿主提供的策略工具适配函数，避免把 Dispatcher 实现塞进上下文组件。

## 2. 接口与协议

### 2.1 根预算与计量

拟新增 core/budget：

```go
type Config struct {
    RootKey, Version string
    MaxModelRequests int // 必须 >0；无隐式默认
}
type Controller struct { /* 私有计数、请求键和持久配置 */ }
func New(config Config) (*Controller,error)
func NewDurable(config Config, store durable.Store) (*Controller,error)
func WithController(ctx context.Context, c *Controller) context.Context
func FromContext(ctx context.Context) (*Controller,bool)
func (c *Controller) IsDurable() bool
func (c *Controller) Reserve(ctx context.Context, operationKey string) error
func (c *Controller) RecordUsage(ctx context.Context, operationKey string, usage *interfaces.TokenUsage) error
func (c *Controller) Snapshot(ctx context.Context) (Snapshot,error)
type Snapshot struct {
    ReservedRequests, KnownUsageEntries, UnknownUsageEntries int
    KnownTokens interfaces.TokenUsage
}
var ErrBudgetExceeded error
var ErrReservationExists error
```

RootKey 是宿主拥有的运行标识，不是模型参数。预算存储 ID 从结构化元组 `[rootKey,"budget"]` 派生；版本与上限保存在记录中校验，同一 root key 改版本不能获得新额度。持久格式为 `goagent-model-budget-v1`。

Reserve 在进程内 mutex 或独立 Store.Lock 下检查额度并保存。operationKey 唯一，已有预留再次 Reserve 返回 ErrReservationExists；只有模型日志 completed 回放路径可以跳过 Reserve。预留不退款，因为预留后无法证明请求没有发送。预留额度是准入上界，不等同于已知计费次数。

锁顺序固定：图/工具/模型操作锁 → 短时预算账本锁；预算代码不能反向读取/锁图或 operation。预算锁在网络请求前释放，绝不复用当前节点的 Store 锁 ID。

RecordUsage 按操作键幂等，nil 表示未知；同一键相同已知记录重复登记不累加，不同已知值冲突。未知可在读取已保存模型操作证据后补为已知；不得通过重新请求模型补用量。累计只读请求级条目，不再将子 AgentOutput.TokenUsage 当作新用量加入根汇总。

Snapshot 将没有用量记录的已预留项也计为未知；它们可能尚未发送或未收到响应，不能据此推断实际计费。RecordUsage 只接受已预留的键，拒绝没有准入证据的登记。

新受控调用在 durable.HasJournal(ctx) 为 true 时检查 IsDurable，内存 Controller 在首次请求前拒绝。Store/root scope 的一致性由宿主在构造该运行时绑定和校验；通用 Store 接口无法证明两个实例指向同一物理后端，框架不做不可验证的保证。

模型日志与预算账本不是跨文件事务。若模型结果已保存但用量登记失败，保存失败必须可见，宿主可从现有操作记录重建已知用量，不自动重复模型调用；记录新增失败输出时仍保存已收到的用量。实际新增调用数按本次进入真实调用函数统计，与跨重启保留的 ReservedRequests 分列。

readJournaledTurn 的新顺序必须是：取得 durable.Do 返回的 record 与 opErr → 从非 nil record.Value 提取已知用量（含无效计划、截断等失败记录）→ 幂等 RecordUsage → errors.Join(opErr,登记错误) 决定返回。不能沿用当前 err 非 nil 时先丢弃 record 的分支。completed 回放同样补登记；failed/pending 使用 InspectOperation 或 Do 已返回证据，仅做计量修复，绝不再次发送。取消后的证据登记用最长 5 秒的独立 context，失败仍可见，不用重试模型补数据。

### 2.2 复合工具与资源归属

拟新增 tools：

```go
type RunnableToolConfig[I,O any] struct {
    Name, Description, ArgsSchema, Version string
    Decode func(map[string]any)(I,error)
    Encode func(O)(any,error)
    ModelInvoking bool // 受信宿主声明；true 时要求共享根预算
    MaxDepth int // 0=>8；负值拒绝
}
type OwnedRunnable[I,O any] struct {
    Runnable core.Runnable[I,O]
    Close func(context.Context) error
}
func NewRunnableTool[I,O any](run core.Runnable[I,O], cfg RunnableToolConfig[I,O]) (interfaces.Tool,error)
func NewRunnableToolFactory[I,O any](factory func(context.Context)(OwnedRunnable[I,O],error), cfg RunnableToolConfig[I,O]) (interfaces.Tool,error)
type DurableIntentVersioner interface { DurableIntentVersion() string }
type DurablePolicyProvider interface { DurableOperationPolicy() durable.OperationPolicy }
var ErrNestedExecutionLimit error
var ErrDispatcherReentry error
var ErrToolWorkflowWaiting error
```

固定 Runnable 不由适配器关闭；factory 产物要求提供 Close，即使不持有资源也显式传 no-op。factory 若返回部分产物与错误，非 nil Close 仍调用。Close 使用独立于父取消、最长 5 秒的清理 context；实现须合作式响应，库不声称能终止忽略 context 的 Close。调用结果与清理错误 errors.Join，清理失败不写 completed。

有状态 Agent 使用 factory。宿主在 factory 内分配独立 SessionID/记忆命名空间与 Dispatcher，并保持受信身份和根预算；框架不能仅从 core.Runnable 接口证明宿主没有复用内存。ModelInvoking 标记用于前置检查，不是自动发现/隔离任意隐藏模型调用的安全机制。

适配器声明 MaxAttempts=1、RetryFailed=false，Dispatcher 对该可选接口使用该策略；旧工具保持现有预算。相同新工具版本恢复 completed 才能复用。pending/failed 停止，不能把进入 factory 当成“读取子检查点”。此模式不暴露可配置重试选项。

同一执行链的 Dispatcher 实例栈在 slots 排队前检查，重入立即失败。独立子 Dispatcher 的总体请求量由根预算控制；深度与并发上限不等于请求预算。父 context 的 deadline 逐层继承；宿主自行为长工具设置合适 DispatchConfig.Timeout，不在适配器内默认延长 30 秒限制。

### 2.3 可观察性

core/observe 只定义业务关联事件及 sink：

```go
type Event struct {
    RunID, OperationKey, VisitID, EventID, Type string
    TraceID, SpanID, ParentSpanID string // 来自 OTel；可为空
    Path, ErrorKind string
    At time.Time
    Duration time.Duration
    Replayed bool
}
type Observer interface { Observe(context.Context, Event) }
func WithObserver(ctx context.Context, observer Observer) context.Context
func WithTracer(ctx context.Context, tracer trace.Tracer) context.Context
func TracerFromContext(ctx context.Context) trace.Tracer
```

OTel 桥接在 observability/execution.go，使用既有 Tracer/Provider 产生 span，并从 SpanContext 获取标识。不实现另一套 StartSpan/自造 TraceID，也不为了记录本地事件修改全局 Provider。恢复建立新 span，稳定 OperationKey 负责跨执行关联，EventID 是本次运行排序序号。

WithTracer 接收现有 OTel API 的 trace.Tracer；没有配置时返回 noop tracer。workflow/builder/tools 的生命周期入口各自调用该 tracer.Start，并把返回的子 context 传给节点/模型/工具，结束时调用 span.End；Observer 只观察，不能负责创建并传播 context。observability 通过 GetTracer 或宿主现有 tracer 注入，不反向导入执行包。core/observe 只新增 OTel API 依赖，不拉入 exporter/SDK 初始化。

workflow.Graph 增加可选拓扑描述接口与 Describe，编译时捕获排序快照；GraphDescription 格式 `goagent-graph-v1`，只含节点 ID/种类、边和分支。通过编译产物可选能力访问，不扩展 core.Runnable 必需方法。

内建 Recorder 默认队列 64、事件上限 10,000，队列满丢弃观察并累计 Dropped；Close(ctx) 返回写错误及丢弃摘要。JSONL 权限 0600、目录 0700。ErrorKind 存类别，不默认写 error.Error()。任意用户 Observer 可能阻塞，只有内建异步记录器承诺不等待磁盘。

### 2.4 上下文策略与引用

保留 WithContextPolicy，先把内部职责拆开。对外扩展采用受限接口，不开放可重复调用模型的 handler：

```go
type Descriptor struct { ID, Version, ConfigHash string; Durable bool }
type Turn struct { Round, RemainingCalls int; Messages []llm.Message }
type Policy interface {
    Descriptor() Descriptor
    BeforeTurn(context.Context, Turn)(Turn,error)
}
type Factory func() Policy
type ToolProvider interface { Tools() []interfaces.Tool }
type FinalTurnRestorer interface { BeforeFinalTurn(context.Context,Turn)(Turn,error) }
type StatsProvider interface { Stats() map[string]int64 }
type Ref struct { RunKey, Key, SHA256, Version string }
type ResultStore interface {
    PutIfAbsent(context.Context,Ref,[]byte) error
    Get(context.Context,Ref)([]byte,error)
}
```

新入口 WithContextPolicies(factories...) 与旧入口同时使用时拒绝配置；顺序固定并进入意图。描述符仅声明能力，必须以快照重放与故障测试验证，不能仅设 Durable=true。

ConfigHash 包含阈值、保留轮数、分页大小、排除工具、Store 命名空间及相关输出格式；对闭包/模型实现无法推导的业务语义仍要求宿主 Version。BeforeTurn 不得改变 Round/RemainingCalls、system、调用关系和原生续接字段；保留独立深复制的基线供比较。

Builder 提供 WithPolicyToolAdapter(func([]interfaces.Tool)([]interfaces.Tool,error))；宿主建立 Dispatcher 包装分页工具，持久模式缺工厂或返回裸工具则首次请求前拒绝。工具名、Schema 和版本意图保持一致，分页调用获得标准 CallID。首轮前注册工具并校验冲突；最后回合 BeforeFinalTurn 恢复原文并移除读取工具，再校验预算。

逻辑引用 ID 用结构化元组 `[parentKey,policyID,logicalResultID]` 经 JSON 编码后派生，logicalResultID 来自已持久回合和工具 ID。PutIfAbsent 在专有引用锁下 Load/摘要比较/Save，同键不同摘要拒绝。格式 `goagent-context-result-v1`，保存宿主范围与内容；Get 再验 SHA256。运行范围通过宿主 context 绑定，不能相信模型提交的 Ref.RunKey。

顺序：已记录工具结果 → 不可变引用落盘 → 构造消息 → 模型意图与请求。任一步失败都不让下游请求读悬空引用。持久摘要首轮仍拒绝；内存摘要走统一模型入口并计局部/根预算。旧字符分页与最后回合原文恢复行为保留。

### 2.5 流原型与类型化适配

首轮继续用现有 Adapt 的 Go 函数做字段提取，新增示例而不增加反射 API。流原型拟采用 core/flow：

```go
type Source[T any] func(context.Context, func(T) error) error
func Map[A,B any](source Source[A], fn func(context.Context,A)(B,error)) Source[B]
type Fold[D,O any] struct {
    Init func() O
    Add func(context.Context,O,D)(O,error)
    Finish func(context.Context,O)(O,error)
}
type Limits[D any] struct {
    MaxItems int // 0=>1024
    MaxBytes int64 // 0=>不声明字节上限
    Size func(D) int64 // MaxBytes>0 必填；返回负值拒绝
}
func AsRunnable[I,D,O any](open func(context.Context,I)(Source[D],error), fold Fold[D,O], preview func(D)(*core.StreamEvent,error), limits Limits[D]) (core.Runnable[I,O],error)
```

这不是完整 Collect/Transform API。source 通过同步 yield 传数据，返回错误或 nil 表示失败/成功。Map 不启动 goroutine；AsRunnable 用同一循环计数、检查字节、Add 和发送可选预览 Event，最终 Source 返回 nil 且 Finish 成功才提交唯一 Done.Data。数据只进入 Fold 一次，不创建额外 reducer 消费队列。Init 每次执行独立，避免并发共享可变 accumulator。

所有 Add/Finish/preview 错误必须保留，即使自定义 Source 忽略 yield 错误并返回 nil 也失败。preview 仅构造展示事件，不能带最终 Done 或直接推进图。模型接入 source 必须显式消费 Event.Delta 或 typed data；不从旧 StreamChunk.Data 零值猜测帧。

每一层 Map 也必须独立保存 fn/yield 的首个错误；source 忽略该错误继续 yield 时，Map 不再运行变换或透传数据，最终 errors.Join(sourceErr, firstErr)。只在最外层 AsRunnable 保存错误不足以发现被上游吞掉的 Map.fn 错误。Source/yield 首轮要求串行调用；并发生产者必须在自己的 source 内串行化，不让 Fold 隐式承担并发同步。

异步源需响应 yield 错误与 context，拥有其生产者的取消和等待责任。复用 core.Generator 前单独验证适配，不能因已有同名 Map/Collect 就认定终态和取消语义一致。包标记 experimental，通过 T8 原型评审后再决定稳定接口。

## 3. 实施任务

### Task 1: 根预算控制器与持久预留，FR-06 / M0

**Files:** 新建 core/budget/controller.go、durable.go、controller_test.go、durable_test.go。
**Interfaces:** 产出 2.1 Config/Controller/New/NewDurable/WithController/FromContext/Reserve/Snapshot。

- [x] 写 TestRootBudgetConcurrentAdmission：额度 2，三个屏障并发请求最多两个 Reserve 成功；重复操作键拒绝，未重置额度。
- [x] 写 TestBudgetSurvivesProcessExit：预留后真实子进程退出，重启额度仍占用；修改额度或版本拒绝；Save 失败不成功准入；与图锁使用不同 ID。
- [x] 运行 `go test ./core/budget -run 'TestRootBudget|TestBudget' -count=1`，确认新能力缺失失败。
- [x] 实现独立账本、锁顺序和预留语义，运行 `go test -race ./core/budget ./core/durable`，提交 `feat: reserve shared model request budgets`。

### Task 2: 模型入口与父子用量归属，FR-06 / M0

**Files:** 新建 core/budget/usage.go、usage_test.go、builder/model_call.go、model_call_test.go；修改 builder/durable_turn.go、tool_events.go、context_policy.go、events.go。
**Interfaces:** 消费 T1；产出 RecordUsage 与统一模型调用辅助入口。公开 Builder API 保持兼容。

- [x] 写 TestBudgetCoversParentChildAndSummary：父 1 次、两个子竞争剩余 1 次，fake transport 总计 2 次；摘要同样计数，局部 MaxIterations 仍限制。
- [x] 写 TestReplayUsageIsNotNewCost：保存模型回合重放无新 Reserve/调用；根账本按操作键只加一次，不再累加子 AgentOutput；nil 用量标未知。
- [x] 写 TestBudgetAndModelSaveWindows：预算保存失败零请求；预留后未发请求进程退出不退款；模型结果保存/用量登记任一步失败保留证据并停止，恢复不重复付费，可从已保存操作读回用量。
- [x] 写 TestFailedModelUsageReconciliation：无效模型计划仍附已知用量；登记失败后只读保存的 failed/completed 证据补登记；err 非 nil 的 OperationRecord.Value 不丢弃，根统计最终只加一次。
- [x] 先运行新增测试见失败，再将 Reserve 放在 durable.Do 的真实调用闭包内、模型请求前；无日志时用本次稳定请求 scope。不可在 replay 查找前扣费。
- [x] 运行 `go test -race ./builder ./core/budget ./core/durable`；保留无预算的旧意图 fixture，提交 `feat: account nested model calls at the root`。

### Task 3: 普通 Runnable 工具适配，FR-02 / M1

**Files:** 新建 tools/runnable_tool.go、runnable_tool_test.go；修改 tools/dispatcher.go；新增 examples/orchestration/runnable-tool/。
**Interfaces:** 产出 2.2 NewRunnableTool、DurableIntentVersioner、DurablePolicyProvider；消费 T1 context。

- [x] 写 TestRunnableToolCodecAndVersion：非法对象 Schema/空版本/缺 Codec 在构造时拒绝；Decode、Invoke、Encode 错误不成功；原普通工具哈希不变。
- [x] 写 TestOpaqueToolReplayAndPending：completed 同 CallID 只执行一次；failed/pending 不重入 Runnable，返回 ErrOperationAttention；版本变更拒绝。无 journal 调用不宣称跨进程去重。
- [x] 写 TestWaitingOutputIsNotSuccess：Encode 明确拒绝等待/未完成输出；审批型 DurableGraph 不作为本例输入。
- [x] 先运行 `go test ./tools -run 'TestRunnableTool|TestOpaqueTool|TestWaiting'` 见失败，实现后运行 `go test -race ./tools ./workflow`，提交 `feat: expose opaque runnable tools`。

### Task 4: owned 子会话、调用作用域与清理，FR-02/06 / M1

**Files:** 新建 tools/call_scope.go、call_scope_test.go；扩展 runnable_tool.go；新增 workflow/runnable_tool_integration_test.go、examples/orchestration/agent-tool/。
**Interfaces:** 产出 OwnedRunnable/NewRunnableToolFactory、深度/重入错误；消费 T1/T2 的共享 budget。

- [x] 写 TestOwnedAgentIsolation：两个并发 factory 分配独立 SessionID、MemoryManager 命名空间与 Dispatcher；不同指针但共享 SessionID 的负例在示例验证中失败。父历史不复制，身份/预算/deadline 显式继承。
- [x] 写 TestOwnedRunnableCleanup：成功、Decode 后执行错误、取消、factory 部分产物错误均关闭已取得资源；Close 失败合并主错误且不保存 completed；固定共享 Runnable 不关闭。
- [x] 写 TestNestedDispatcherScope：父子独立 MaxConcurrency=1 可完成；同实例重入排队前失败；深度 8 可进入，9 不调用子对象；父 deadline 不延长。
- [x] 写 TestModelToolRequiresRootBudget：缺预算在 factory 前拒绝；持久运行绑定内存 Controller 零模型请求，宿主构造测试校验 Store/root scope 归属；根预算的 transport 次数与 T2 一致。Opaque 父 pending 即使子日志 completed 也不续跑。
- [x] 先失败后实现，运行 `go test -race ./tools ./builder ./workflow ./core/budget`，提交 `feat: own and bound nested agent sessions`。

### Task 5: 复用 OTel 与最小只读事件，FR-04 / M0

**Files:** 新建 core/observe/event.go、observability/execution.go、workflow/describe.go、devtools/recorder.go 及测试；修改 workflow 的 graph/execute/parallel/durable 路径、builder/durable_turn.go、tools/dispatcher.go。
**Interfaces:** 产出 2.3 Event/Observer/WithObserver、可选 GraphDescriber；不新增自有 Span 生成器。

- [x] 写 TestExecutionUsesExistingOTelParent：使用内存 OTel exporter，节点/工具/模型继承同一 trace 的父子 span；恢复产生新 span，用稳定操作键关联。
- [x] 写 TestDescribeSnapshotAndVisit：拓扑排序固定，编译后修改原图不影响描述；循环访问、分支与重放区分，无闭包/正文。
- [x] 写 TestRecorderBoundedAndRedacted：队列满、10,000 事件截断、磁盘失败、异常字符串包含敏感值时主结果不变且正文不泄漏；Close 报错/丢弃可见；无 OTel provider 时 TraceID 可为空。
- [x] 先运行新测试见失败，再在既有生命周期接入；由宿主选择 OTel 桥接，不设置全局 provider。运行 `go test -race ./core/observe ./observability ./workflow ./builder ./tools ./devtools`，提交 `feat: correlate workflow observations with otel`。

### Task 6: 上下文内部组件与不可变存储，FR-05 / M2

**Files:** 新建 builder/contextpolicy/policy.go、offload.go、store.go、memory_store.go 及测试；修改 builder/context_policy.go。
**Interfaces:** 产出 2.4 Ref/ResultStore/Descriptor，先在内部适配，不立即开放任意消息重写。

- [x] 写 TestLegacyContextPolicyParity：分页 Unicode、排除工具、最后回合恢复、摘要预算与缺失用量保持旧行为。
- [x] 写 TestReferenceContentIdentity：相同 key/摘要幂等、不同内容拒绝、跨运行/摘要/格式不符拒绝；返回数据不共享可写数组；结构化 ID 中包含斜线不会碰撞。
- [x] 写 TestContextConfigFingerprint：改变阈值/分页/排除工具/Store 命名空间导致 ConfigHash 不同，顺序变化可见，默认无策略意图不变。
- [x] 先失败后抽取，运行 `go test -race ./builder/...`，提交 `refactor: isolate context policies and immutable results`。

### Task 7: 持久引用与窄策略扩展，FR-05 / M2

**Files:** 新建 builder/contextpolicy/durable_store.go、durable_store_test.go；修改 builder/config_options.go、tool_events.go、durable_turn.go；新增 examples/orchestration/context-recovery/ 及子进程测试。
**Interfaces:** 产出 2.4 Policy/Factory/ToolProvider/FinalTurnRestorer/StatsProvider、Builder.WithContextPolicies/WithPolicyToolAdapter，durable Store 适配。

- [x] 写 TestPolicyLifecycleAndProtocol：执行顺序固定，独立实例；新旧入口冲突；system/tool ID/原生字段/预算篡改含嵌套原地修改均拒绝；摘要不能绕过 T2。
- [x] 写 TestPolicyReadToolAuthorization：分页工具名称/Schema 冲突拒绝；持久缺 adapter/裸 Tool 拒绝；调用与回放授权，最后回合恢复原文移除读取工具。
- [x] 写 TestReferencesProcessRecovery：引用保存前失败零请求；引用已存、模型意图未存时重启得到相同引用；模型已发结果未落盘则 ErrOperationAttention；读取固定内容，ConfigHash 变化拒绝。
- [x] 持久摘要和不支持持久的策略先验拒绝。先验证失败，实施后运行 `go test -race ./builder/... ./core/durable ./core/budget ./examples/orchestration/context-recovery`，提交 `feat: recover immutable context references`。

### Task 8: 只读增量管道原型，FR-01 / M3

**Files:** 新建 experimental core/flow/source.go、fold.go、adapter.go 及测试；新增 examples/orchestration/stream-pipeline/、workflow/flow_integration_test.go。
**Interfaces:** 产出 2.5 Source/Map/Fold/Limits/AsRunnable；暂不稳定发布四范式接口。

- [x] 写 TestIncrementalBeforeSourceReturns：阻塞 Source 返回前下游收到第一项；0/false/空字符串均处理，不把事件或最终值重复 Fold。
- [x] 写 TestFoldLateErrorDoesNotCommit：Source 在若干项后返回错误，或忽略 Map.fn/Add/preview 返回错误，均无成功 Done；Map 在首错后不再变换/透传，图不调度下游。
- [x] 写 TestSourceCancellationAndLimits：取消使合作式模型源退出；1025 项超限；MaxBytes 开启而缺 Size/返回负值拒绝；并发 Invoke 不共享 Init 累积对象。
- [x] 先失败后实现无额外队列的同步原型，运行 `go test -race ./core/flow ./core/... ./workflow`。记录首帧屏障证据、基准、API 采用/推迟结论，提交 `feat: prototype incremental read-only pipelines`。

### Task 9: 类型化适配示例与离线报告，FR-03/04 / M1、M3

**Files:** 新增 examples/orchestration/field-adapter/、parallel-diagnostics/、devtools/report.go、assets/report.html 及测试。
**Interfaces:** 复用现有 Adapt/Clone/Merge、T5 拓扑/事件；不新增反射 Mapping 和 Debugger.Run。

- [x] 写 TestTypedAdapterErrorsStopNode：订单与并行结果显式转换，零值保留，nil/错误不调用下游，不修改来源状态；假工具无真实副作用。
- [x] 写 TestOfflineReportEscapesContent：拓扑/事件中脚本样字符串只作文本；无 Args/提示词；报告标明观察缺失和非恢复依据。
- [x] 先失败后实现示例及只读报告；浏览器人工检查循环/分支/等待/失败展示，运行 `go test -race ./devtools ./workflow ./examples/orchestration/...`，提交 `docs: demonstrate typed adapters and readonly reports`。

### Task 10: 订单闭环、兼容与交付证据，全部首轮需求

**Files:** 新增 examples/orchestration/composed-order/ 与测试；新增 docs/guides/COMPOSABLE_ORCHESTRATION.md；更新 EXECUTION_CONTRACT、NATIVE_ORCHESTRATION、CONTEXT_POLICY 和 docs 索引。
**Interfaces:** 仅消费已交付能力，不为闭环新增动态 DAG/隐藏重试。

- [x] 验证普通规则图工具、独立政策子 Agent、根额度争用、类型化 Adapt、长证据固定引用、重启与 OTel/JSONL 关联。分别断言实际请求、预算预留、累计用量和 replay 新增用量。
- [x] 使用旧检查点/工具意图 fixture 验证无配置兼容；新配置改变拒绝旧运行；pending 不删除、不退款、不强制重试。
- [x] 运行 `GO_TEST_MODE=true go test -race -timeout 10m ./...`、`go build ./...`、`golangci-lint run ./...`、`bash scripts/verify_imports.sh`，全部退出码 0 并记录工具版本、第三方警告。
- [x] 同机前后各跑相关 benchmark 5 次，比较 ns/op、B/op、allocs/op；解释显著回退，不用远端模型延迟代表框架开销。
- [x] 真实模型只显式启用，设置根预算、保留失败及已知/未知用量。更新能力状态，提交 `docs: verify composable execution boundaries`。

## 4. 顺序与阶段完成定义

依赖图：T1→T2→T4；T3→T4；T6→T7，T7 依赖 T2；T5 可先行，为其他阶段提供观察；T8 独立原型；T9 的类型化示例跟随 T3，报告跟随 T5；T10 汇总。

发布顺序：M0=T1/T2/T5 最小集；M1=T3/T4/T9 类型化示例；M2=T6/T7；M3=T8 原型与 T9 报告。纯规则图工具可以先开发测试，模型型子工具必须等待 M0 完成。每阶段可独立 PR，但不并行改同一 Builder 文件而不协调。

M0–M2 需交付实现、失败回归、离线示例与迁移说明。M3 流原型需形成采用/推迟评审记录，不因有原型就宣称完整四范式；反射映射、嵌套恢复等不计入本轮完成。

## 5. 追踪矩阵与后续准入

| 需求 | 任务 | 本轮结果 |
| --- | --- | --- |
| FR-01 | T8/T10 | 只读原型与采用结论 |
| FR-02 | T3/T4/T10 | 受控不透明工具化；非可恢复子工作流 |
| FR-03 | T9 | 两个类型化示例；反射映射推迟 |
| FR-04 | T5/T9/T10 | OTel 关联、JSONL、只读报告 |
| FR-05 | T6/T7/T10 | 内部组件、固定内容引用、窄策略 |
| FR-06 | T1/T2/T4/T10 | 根预算准入、持久预留与计量 |
| NFR-01–06 | 各任务/T10 | 兼容、race、故障、依赖、版本与基准 |

后续候选必须单独设计：反射映射需两个独立业务证据；四范式需原型收益与清理协议验证；可恢复子 Agent/审批需稳定 continuation、父子状态机、目标幂等及重放专项；分类重试需持久错误类型和预算语义；持久摘要需独立日志与请求后崩溃验证。不得仅修改 MaxAttempts 或 Durable 标记解除限制。

## 6. 发布与回退

无新功能配置时不创建根预算或引用记录，不追加意图字段。新模型型工具与新策略使用配置版本与摘要；同 root 预算改变不能悄悄生成新额度。新格式未知版本拒绝读取。

回退先停止新版本运行，保留对应版本 worker/二进制完成或人工处置存量。旧二进制不得读取新账本后假成功，不能删除 pending/failed 日志强制重做。预算、引用、检查点按运行统一保留及清理，不删除活跃引用。

T1–T10 首轮任务已执行；文件可按职责合并，真实子会话与闭环验证集中在 T10。交付命令、性能回退与实验范围见 [验证记录](../../evaluation/2026-10-27-orchestration-r2/REPORT.md)。这不构成生产可靠性或远端模型性能结论。
