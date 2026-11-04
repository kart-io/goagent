# 动态中断设计

日期：2026-11-04
范围：`goagent` 主模块（`core/durable`、`workflow`、`tools`）
前置条件：人工审批改动（`workflow.ResumeRequest`、`WithResumeHandler`、`DurableGraph.Resume`）已合入

## 目标

工作流节点或工具可以在执行**中途**暂停，等人提供输入后从检查点恢复。嵌套子图与并行分支中的暂停也能恢复。

现状：只能在预先声明的 `DurableConfig.ApprovalNodes` 节点执行前暂停；工具只有 `RunnableToolConfig.Resumable` 的子图工具能承载等待。

## 非目标

- 在 Agent 中间件、模型钩子（PreStep、TurnStopping）里暂停：当前没有承载等待的持久操作。
- 非持久运行的暂停：直接返回 `ErrInterruptNotDurable`。
- 可插拔的状态序列化器。
- 下线 `core.InterruptManager`：本期只在文档里划清边界。

## 选型

在 `durable.Do` 上实现原语。人工输入作为操作的 completed 值写入日志，之后再进入只回放，同一份输入只消费一次。

| 方案 | 结论 |
|---|---|
| 基于 `durable.Do` 的原语 | 采用。复用 Do、`Waits`、`WithResume`、`builder.ErrWaiting` |
| 节点或工具返回类型化错误，由框架注入输入 | 不采用。另造一套机制，输入不入日志，崩溃后有重复应用风险 |
| 扩展 ApprovalNodes | 不采用。只能在节点开始前暂停，达不到需求 |

## 原语：`core/durable/interrupt.go`

```go
// AwaitInput 在当前持久操作中暂停，等待宿主回应；续跑时返回人工输入，之后再进入只回放该输入。
// id 在同一父执行键下必须唯一（循环中带序号）。info 必须是确定性的 JSON，供界面展示。
func AwaitInput(ctx context.Context, id string, info json.RawMessage,
    validate func(json.RawMessage) error) (json.RawMessage, error)

// InterruptContinuation 是 WaitError.Continuation 的内容。
type InterruptContinuation struct {
    ID       string          `json:"id"`
    Info     json.RawMessage `json:"info"`
    Rejected bool            `json:"rejected,omitempty"`
    Reason   string          `json:"reason,omitempty"`
}

var ErrInterruptNotDurable = errors.New("暂停需要持久日志")
```

实现要点：

- 内部调用 `Do(ctx, "interrupt/"+id, info, OperationPolicy{Resumable: true, MaxAttempts: interruptAttempts}, run)`。`interruptAttempts` 是不小于 2 的常量：在写 pending 与写 waiting 之间崩溃时可以重入，中断本身没有副作用。
- `run` 的逻辑：
  - `ResumeInput` 有输入且 `validate` 通过 → 返回输入，记录变为 completed；
  - 没有输入 → 返回 `&WaitError{Continuation: {ID, Info}}`，记录变为 waiting；
  - `validate` 失败 → 返回 `&WaitError{Continuation: {ID, Info, Rejected: true, Reason}}`，记录保持 waiting。不能返回普通错误：普通错误会把记录写成 failed，Resumable 不会重入 failed 记录。
- 没有日志时（`HasJournal(ctx)` 为假），返回 `ErrInterruptNotDurable`。
- ctx 不允许等待时（不在 Resumable 操作内，也不在 workflow 节点内），在写记录前返回 `ErrWaitNotResumable`。为此导出一个窄接口，供 workflow 把节点 ctx 标记为可等待；具体形式在实现计划中确定。
- `Reason` 只写校验错误的类别或宿主提供的简短文本，不写入第三方错误原文，与 Do 保存错误时的约定一致。

中断键：`WaitError.Path` 的最后一项，即 `ChildKey(ExecutionKey(ctx), "interrupt/"+id)`。

## workflow

### 节点内暂停

- 节点 ctx 标记为可等待。
- 节点返回的错误满足 `durable.Waits(e) != nil` 时，视为暂停，不视为失败：
  - 检查点停在当前节点的 intent，`Next` 不加一；
  - 设置 `Waiting=true`，并把待回应的中断写入 `graphState.Interrupts`；
  - 发出 `interrupt_required` 事件（对应现有的 `approval_required`）；
  - 返回 `durable.ErrApprovalRequired`，宿主现有的分支不用改。
- `validateContinuation` 放开校验：`Waiting` 可以出现在非审批节点上，前提是 `Interrupts` 非空。审批等待与中断等待互斥。

### 快照

```go
type InterruptPoint struct {
    Key      string          // 中断键，用于 Resume
    ID       string
    Info     json.RawMessage
    Rejected bool
    Reason   string
}
// GraphSnapshot[T] 新增 Interrupts []InterruptPoint
```

### 恢复

`Resume(ctx, runID, ResumeRequest{Key, Input})`：

- `Key` 等于审批键 → 沿用现有审批流程（`WithResumeHandler`、`WithBeforeApprove`）。
- `Key` 属于 `Interrupts` → 内部调用 `durable.WithResume(ctx, key, input)`，从头重新执行该节点。节点里已完成的 `durable.Do` 步骤会回放，`AwaitInput` 返回输入。宿主不需要分别处理两种键。
- 其他键 → 返回 `ErrApprovalMismatch`。
- 一次回应一个中断。节点重新执行后，如果仍有中断没回应，就再次暂停并返回剩余的中断。
- `RunRequest.Resume`（AsRunnable）与 `workflow.WithResume`（嵌套子图）接受同样的中断键。

### 并行分支

`runDurableParallel` 里一个分支出错会立即取消兄弟分支（`durable_parallel.go:146`）。分支返回等待时不触发取消：等其余分支跑完，再用 `errors.Join` 汇总所有等待返回；只要有真正的失败，就仍按失败处理。

### 嵌套子图

`AsChildRunnable` 在子图因节点内中断而暂停时，返回的 `ChildContinuation` 也要带上中断列表。外层 Agent 收到 `builder.ErrWaiting` 后，用最内层的中断键回应。

## 工具

- `FunctionToolBuilder.WithInterruptible()`：工具实现 `DurablePolicyProvider`，策略为 `{Resumable: true, MaxAttempts: 1}`。
  - `MaxAttempts: 1` 只允许从等待中重入（续跑不计尝试次数），崩溃后的 pending 记录仍交给宿主处理。
- 默认不开启。理由：续跑会从头重新执行工具，`AwaitInput` 之前没有用 `durable.Do` 记录的副作用会重复执行，必须由工具作者声明可以接受。
- 等待经 Dispatcher 与 `builder.ErrWaiting` 上报（`builder/tool_events.go:302`、`:327`），这条链路已经存在。宿主用 `durable.WithResume(ctx, 中断键, input)` 加原输入调用 `Invoke` 恢复；由 `Start` 开始的运行用 `Start` 恢复。

## 破坏性变更与迁移

- 已有工具改为 `WithInterruptible()` 后，持久策略随之改变，旧日志记录会报 "attempt policy changed"。宿主需要提升 `DurableConfig.Version` 或工具版本，等进行中的运行结束后再切换。
- 节点返回 `*WaitError` 以前按失败处理，现在按暂停处理。依赖旧行为的宿主需要调整，CHANGELOG 中写明。
- 检查点新增的 `Interrupts` 是可选字段，旧检查点可以正常读取。

## 文档

- `docs/guides/HUMAN_IN_THE_LOOP.md` 新增“节点或工具执行中途暂停”一节，写明：
  - 重新执行语义，以及副作用必须记日志或自行幂等；
  - id 必须唯一；
  - info 必须是确定性的。
- 在同一文档中写明 `AwaitInput`、`ApprovalNodes`、`core.InterruptManager`、`Handle.Interrupt` 四者的适用边界。
- `examples/human-in-the-loop` 新增“工具执行中途询问用户”的离线场景。

## 验收（本地执行）

**原语**

- 首次调用进入等待，Continuation 带有 ID 与 Info。
- 有输入时返回输入；之后再进入只回放，不再询问。
- 校验失败时保持等待，并标记 `Rejected`；修正输入后可以通过。
- 没有日志时返回 `ErrInterruptNotDurable`；不可等待的 ctx 返回 `ErrWaitNotResumable`，且不写记录。
- 在写 pending 与写 waiting 之间模拟崩溃，之后能重入。
- id 重复会回放上一次的输入（测试固定这一行为）。
- info 变化时报 intent changed。

**workflow**

- 节点中途暂停，进程重启后恢复，中断前的 `durable.Do` 步骤只执行一次。
- 两个并行分支各自暂停，兄弟分支不被取消，可以逐个回应恢复。
- 嵌套子图节点内暂停，外层 Agent 以 `ErrWaiting` 暂停，用中断键恢复。
- 同一图中既有审批等待也有中断等待。
- 错误的键返回 `ErrApprovalMismatch`。
- AsRunnable 入口：暂停时返回 `Waiting=true`，并发出 `interrupt_required` 事件。
- 旧检查点（没有 Interrupts 字段）可以继续执行。

**工具**

- `WithInterruptible()` 的函数工具在 Agent 原生工具循环中暂停，并以 `ErrWaiting` 上报；用 Invoke 与 Start 两种入口都能恢复。
- 没有开启时报 `ErrWaitNotResumable`。
- 并发工具调用中一个暂停，其余调用正常完成。

**全量验证**

- `go test -race ./...`
- `golangci-lint run`
- `bash scripts/verify_imports.sh`
- `bash scripts/check_coverage.sh`
- `go run ./examples/human-in-the-loop`

## 风险

- 重新执行节点或工具时，没有记日志的副作用会重复执行。这一点通过文档和默认关闭来约束，框架无法检测。
- id 冲突会悄悄回放旧输入，只能靠文档和测试约束。
- 放开 `validateContinuation` 会削弱现有的检查点一致性校验。新的约束（Interrupts 非空，且与审批等待互斥）要有对应的损坏检测测试。
- 实现依赖尚未合入的人工审批改动，合入前不能开始编码。

## 回滚

- 原语与 `WithInterruptible` 是新增 API，回退相关提交即可。
- 不调用 `AwaitInput` 时，节点循环的新分支不会触发；但节点返回 `*WaitError` 的语义已经从失败变为暂停，回滚时要同步回退 CHANGELOG 中的迁移说明。
