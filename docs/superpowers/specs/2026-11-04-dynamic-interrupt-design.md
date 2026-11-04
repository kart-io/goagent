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

在 `durable.Do` 上实现原语。人工输入作为操作的 completed 值写入日志：首次成功持久化的输入就此固定，之后的重放都返回同一个值。这只保证输入不变，不保证业务副作用只执行一次。

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
- `run` 的逻辑。原语先自行检查 JSON 合法性，再调用 `validate`；任何拒绝都返回带 `Rejected` 的 `WaitError`，绝不返回普通错误。普通错误会把记录写成 failed，Resumable 不会重入 failed 记录；不合法的 JSON 输出也会被 `Do` 记为 failed（`operation.go:312`）。

  | `ResumeInput` 的结果 | 处理 | 记录 |
  |---|---|---|
  | 没有输入（`ok=false`） | 返回 `&WaitError{Continuation: {ID, Info}}` | waiting |
  | 空字符串 | 拒绝，`Reason="invalid_json"` | waiting |
  | 不合法的 JSON | 拒绝，`Reason="invalid_json"` | waiting |
  | 合法 JSON（包括 `null`），`validate == nil` | 接受，原样返回 | completed |
  | 合法 JSON（包括 `null`），`validate` 通过 | 接受，原样返回 | completed |
  | 合法 JSON，`validate` 失败 | 拒绝，`Reason` 取校验结果 | waiting |

  `null` 是合法的显式输入，与“没有输入”不同。需要非空输入的调用方要在 `validate` 中拒绝 `null`。
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
### 检查点状态规则

“审批等待”和“中断等待”是两种互斥的等待，用 `Interrupts` 是否为空来区分：

| 状态 | 条件 | 含义 |
|---|---|---|
| 审批等待 | `Waiting && !Approved && len(Interrupts)==0`，节点在 `ApprovalNodes` 中 | 节点执行前等审批（现有语义） |
| 中断等待 | `Waiting && len(Interrupts)>0` | 节点执行中途等输入 |
| 已审批节点内的中断等待 | `Waiting && Approved && len(Interrupts)>0` | 审批已通过，节点执行中又暂停 |

`validateContinuation` 按上表修改：

- `Waiting && Approved` 只在 `Interrupts` 非空时合法；`Interrupts` 为空时仍判为损坏。
- 中断等待不要求节点在 `ApprovalNodes` 中，但 `Key` 必须等于当前节点 intent 的 `graphDigest`。
- 并行检查点（`state.Parallel != nil`）允许中断等待，但不允许审批等待。
- `Routing`、`END` 位置上不允许出现 `Interrupts`。

恢复时保留 `Approved=true`：已审批的节点因中断重新执行时，不再走审批，也不再调用 `WithResumeHandler` 和 `WithBeforeApprove`。

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
- `RunRequest.Resume`（AsRunnable）接受同样的中断键。
- 处于中断等待时调用不带键的 `Run(ctx, runID, zero, "")`：只要满足以下**任一**条件，就重新执行节点：
  - ctx 里有某个待回应中断键对应的 `durable.ResumeInput`；
  - 某个待回应中断在日志中已经 completed。

  恢复期间检查点保持中断等待，直到节点完成才更新，不在重新执行前清除。

  重新执行后，已回应的中断回放已固定的输入，其余中断再次暂停。图检查点的 `Interrupts` 更新为剩余的中断。两个条件都不满足时，不执行节点，原样返回等待。

### 输入入日志后、图检查点更新前崩溃

`AwaitInput` 的记录和图检查点是两条独立的记录。如果崩溃发生在输入已写入日志（中断记录 completed）、图检查点仍为中断等待之后：

- 恢复（`Resume` 或不带键的 `Run`）时，按中断键查日志，发现已经 completed，就视为已回应：重新执行节点，`AwaitInput` 回放已固定的输入，不会再次询问。
- 并行分支部分回应：例如 A 的输入已写入日志、B 仍在等待时崩溃。新进程恢复时按上一节的“任一条件”规则重新执行节点：A 回放已固定的输入，B 再次暂停，检查点的 `Interrupts` 只剩 B。
- 用同一个键再次提交**相同**输入：与正常恢复相同，幂等。“相同”的判定：两份输入各自经 `json.Compact` 压缩后逐字节相等。空白差异不算不同；键顺序或数字写法不同，算不同。
- 用同一个键再次提交**不同**输入：直接拒绝，返回 `ResumeRejectedError`，原因为 `already_answered`。本次**不执行**节点，检查点不变，与 `ResumeRejectedError` 现有的“拒绝且检查点不变”契约一致。嵌套子图中同样返回拒绝等待（`ChildContinuation.Rejected`），父操作不会因此失败。宿主之后用不带输入的 `Run` 恢复，节点使用已固定的输入继续。
- 判断是否已回应需要按键读取中断记录。`core/durable` 导出一个只读查询，具体形式在实现计划中确定，不暴露 `OperationRecord` 的写入能力。

### 嵌套子图的键传递

日志里的输入按执行键查找（`durable.ResumeInput` 取的是 `ExecutionKey(ctx)`），ctx 里的恢复输入会一路传到最内层，因此规定两种传入方式：

1. **直接传最内层的中断键**（推荐，任意层级都可用）：宿主调用 `durable.WithResume(ctx, 中断键, input)`，中断键取 `WaitError.Path` 的最后一项。中间每一层子图在重新执行时，都按“中断等待 + ctx 中有该键的输入”的规则重新执行等待的节点，最内层的 `AwaitInput` 读到输入。
2. **经 `workflow.WithResume` 封装**：宿主调用 `workflow.WithResume(ctx, 父操作执行键, ResumeRequest{Key: 中断键, Input})`。`AsChildRunnable` 从父操作执行键读到封装的请求后：
   - `Key` 是本子图的审批键 → 沿用现有审批流程；
   - `Key` 属于本子图的 `Interrupts`，或者属于更深层的等待 → 转换为 `durable.WithResume(ctx, 中断键, Input)`，再执行本子图。

`ChildContinuation` 新增 `Interrupts []InterruptPoint`，包含所有层级的待回应中断，每一项的 `Key` 都是最内层的中断键，宿主不需要自己拼路径。

### 并行分支

`runDurableParallel` 里一个分支出错会立即取消兄弟分支（`durable_parallel.go:146`）。分支返回等待时不触发取消：等其余分支跑完，再用 `errors.Join` 汇总所有等待返回；只要有真正的失败，就仍按失败处理。

### 嵌套子图

`AsChildRunnable` 在子图因节点内中断而暂停时，返回的 `ChildContinuation` 带上中断列表。外层 Agent 收到 `builder.ErrWaiting` 后，按上一节的两种方式之一回应。

## 授权边界

- 动态中断的回应不经过 `WithResumeHandler`、`WithBeforeApprove`，框架不做任何授权判断。
- 中断键只用于定位这一次暂停，不是授权凭证，任何拿到键的人都能构造回应。宿主在调用 `Resume`、`durable.WithResume`、`workflow.WithResume` 之前，必须先认证回应者，并确认其有权回应这个中断（例如按 `InterruptPoint.ID` 和业务归属判断）。
- `info` 会通过 `Snapshot` 和 `ChildContinuation` 暴露给宿主，不得放入凭证或宿主不该展示的数据。
- 输入的业务合法性由 `validate` 负责，调用方不能只依赖界面上的必填校验。

## 工具

- `FunctionToolBuilder.WithInterruptible(version)`：经可选接口 `tools.InterruptibleTool` 声明，工具实现 `DurablePolicyProvider`，策略为 `{Resumable: true, MaxAttempts: 1}`。开启必须带版本，版本进入 intent 哈希。
  - `MaxAttempts: 1` 只允许从等待中重入（续跑不计尝试次数），崩溃后的 pending 记录仍交给宿主处理。
- 默认不开启。理由：续跑会从头重新执行工具，`AwaitInput` 之前没有用 `durable.Do` 记录的副作用会重复执行，必须由工具作者声明可以接受。
- 等待经 Dispatcher 与 `builder.ErrWaiting` 上报（`builder/tool_events.go:302`、`:327`），这条链路已经存在。宿主用 `durable.WithResume(ctx, 中断键, input)` 加原输入调用 `Invoke` 恢复；由 `Start` 开始的运行用 `Start` 恢复。

## 破坏性变更与迁移

- 已有 FunctionTool 改为 `WithInterruptible(version)` 后，版本变化会改变 intent 哈希，旧记录报 intent 变更，须等旧运行结束或换新运行。“只改 `Resumable` 不报错”只适用于自行实现 `DurablePolicyProvider` 的工具：持久策略从 `{MaxAttempts: DispatchConfig.MaxAttempts, RetryFailed: true}` 变为 `{Resumable: true, MaxAttempts: 1}`，框架**不能可靠检测**这一变化，`Do` 只比较输入哈希和 `MaxAttempts`，不比较 `Resumable`、`RetryFailed`（`core/durable/operation.go:266`）。`DispatchConfig.MaxAttempts` 恰好为 1 时不会报错，旧记录会按新策略处理。迁移要求：
  - 提升工具的 intent 版本，使输入哈希变化，旧记录不再被复用；
  - 或者等使用该工具的进行中运行（包括 waiting 和 pending）全部结束后再切换。
  - 文档中写明这一要求，并用测试固定“只改 Resumable 不报错”的现状，避免有人误以为框架会检测。
- 节点返回 `*WaitError` 以前按失败处理，现在按暂停处理。依赖旧行为的宿主需要调整，CHANGELOG 中写明。
- 检查点新增的 `Interrupts` 是可选字段，旧检查点可以正常读取。

## 文档

- `docs/guides/HUMAN_IN_THE_LOOP.md` 新增“节点或工具执行中途暂停”一节，写明：
  - 重新执行语义，以及副作用必须记日志或自行幂等；
  - id 必须唯一；
  - info 必须是确定性的。
- 在同一文档中写明 `AwaitInput`、`ApprovalNodes`、`core.InterruptManager`、`Handle.Interrupt` 四者的适用边界。
- 新增离线示例 `examples/human-in-the-loop-interrupt`，演示工作流节点中途中断与跨进程恢复；工具中断由 builder 测试覆盖。

## 验收（本地执行）

**原语**

- 首次调用进入等待，Continuation 带有 ID 与 Info。
- 有输入时返回输入；之后再进入只回放，不再询问。
- 校验失败时保持等待，并标记 `Rejected`；修正输入后可以通过。
- 按输入表逐项覆盖：没有输入、空字符串、不合法的 JSON、`null`（`validate` 为 nil 和不为 nil 两种情况）、`validate` 失败。拒绝的情况下记录都保持 waiting，并且之后能用合法输入续跑。
- 没有日志时返回 `ErrInterruptNotDurable`；不可等待的 ctx 返回 `ErrWaitNotResumable`，且不写记录。
- 在写 pending 与写 waiting 之间模拟崩溃，之后能重入。
- id 重复会回放上一次的输入（测试固定这一行为）。
- info 变化时报 intent changed。

**workflow**

- 节点中途暂停，进程重启后恢复，中断前的 `durable.Do` 步骤只执行一次。
- 审批节点通过后在节点内中断：检查点为 `Waiting && Approved`，恢复时不重复审批，不再调用 `WithResumeHandler`、`WithBeforeApprove`。
- 损坏检测：`Waiting && Approved` 且 `Interrupts` 为空、`Routing` 或 `END` 位置带 `Interrupts`、并行检查点处于审批等待，都判为损坏。
- 两个并行分支各自暂停，兄弟分支不被取消，可以逐个回应恢复。
- 嵌套子图节点内暂停，外层 Agent 以 `ErrWaiting` 暂停；分别用“直接传最内层中断键”和“`workflow.WithResume` 封装”两种方式恢复，覆盖两层嵌套。
- 输入已写入日志、图检查点仍为中断等待时模拟崩溃：恢复后不再询问，用的是已固定的输入。
- 再次提交相同输入（含空白差异）时幂等。提交不同输入时返回 `ResumeRejectedError(already_answered)`，节点不执行，检查点不变；之后用不带输入的 `Run` 能恢复。嵌套子图中同样如此，父操作保持等待，不失败。
- 并行分支部分回应后崩溃：A 已写入日志、B 仍在等待；不带输入的 `Run` 恢复后，A 继续执行，`Interrupts` 只剩 B，回应 B 后节点完成。
- 同一图中既有审批等待也有中断等待。
- 错误的键返回 `ErrApprovalMismatch`。
- AsRunnable 入口：暂停时返回 `Waiting=true`，并发出 `interrupt_required` 事件。
- 旧检查点（没有 Interrupts 字段）可以继续执行。

**工具**

- `WithInterruptible()` 的函数工具在 Agent 原生工具循环中暂停，并以 `ErrWaiting` 上报；用 Invoke 与 Start 两种入口都能恢复。
- 没有开启时报 `ErrWaitNotResumable`。
- `DispatchConfig.MaxAttempts=1` 时，只把工具从默认策略改为 `WithInterruptible()` 不会报错（固定现状）；提升 intent 版本后，旧记录不会被复用。
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
- 放开 `validateContinuation` 会削弱现有的检查点一致性校验。新的约束（见“检查点状态规则”）要有对应的损坏检测测试。
- 工具策略变化不能被框架检测，只能靠版本迁移要求约束。
- 实现依赖尚未合入的人工审批改动，合入前不能开始编码。

## 回滚

- 原语与 `WithInterruptible` 是新增 API，回退相关提交即可。
- 不调用 `AwaitInput` 时，节点循环的新分支不会触发；但节点返回 `*WaitError` 的语义已经从失败变为暂停，回滚时要同步回退 CHANGELOG 中的迁移说明。
