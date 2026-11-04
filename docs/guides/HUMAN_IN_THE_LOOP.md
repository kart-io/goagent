# 人工介入：暂停、恢复与检查点

需要人工审批、补充信息或修改草稿后继续执行时，使用 `workflow.CompileDurable`。运行在指定节点前保存检查点并返回等待状态，释放运行锁；人可以稍后在新进程中回应。无需保持 HTTP 请求或 goroutine 等待。

| 需求 | 接入方式 |
|---|---|
| 在业务节点前暂停 | `DurableConfig.ApprovalNodes`；条件暂停可由图的分支路由到人工节点 |
| 查看持久状态 | `graph.Snapshot(ctx, runID)`，包含 State、Node、Steps、Key、Waiting、Done |
| 纯审批放行 | `graph.Run(ctx, runID, initial, waiting.Key)`，或 `graph.Resume` 的无载荷请求 |
| 人工输入、编辑状态后恢复 | `WithResumeHandler` + `graph.Resume(ctx, runID, ResumeRequest)` |
| 流式/批量入口 | `graph.AsRunnable()`，通过 `RunRequest.Resume` 提交相同请求 |
| Agent 工具中的子图 | `AsChildRunnable` + `Resumable` 工具；用 `workflow.WithResume` 给等待处提供输入 |
| 立即取消正在运行的 Agent | `Handle.Interrupt(cause)`；取消与持久人工等待是不同操作 |

## 最小接入

完整离线程序见 [human-in-the-loop 示例](../../examples/human-in-the-loop/README.md)，无需模型或 API 密钥，包含通过、拒绝、修改文本和独立进程恢复。

1. 建立带稳定业务 RunID 的持久图，将人工节点加入 `ApprovalNodes`，使用 `durable.FileStore` 保存本机检查点。
2. 配置 `WithResumeHandler`，把经过校验的人工输入转换成业务状态。宿主负责认证身份、限定允许修改的字段。
3. 调用 `Run`。收到 `durable.ErrApprovalRequired` 时向界面返回 `Snapshot.State`、`Node` 和 `Key`，等待人工决定。
4. 在相同存储、图定义与版本下调用 `Resume`。更新后的状态和放行标记原子落盘后，才执行暂停的节点。

```go
waiting, err := graph.Run(ctx, runID, initial, "")
if err != durable.ErrApprovalRequired {
    return err // 实际应用还需处理正常完成的结果
}

// ctx 中的身份必须由宿主认证；waiting.Key 只是定位本次暂停，不是授权凭证。
result, err := graph.Resume(ctx, runID, workflow.ResumeRequest{
    Key:   waiting.Key,
    Input: json.RawMessage(`{"decision":"approve","text":"人工修订的内容"}`),
})
if err != nil {
    return err // 也可能到达下一处人工节点，需再次处理等待
}
fmt.Println(result.Done, result.State)
```

处理器签名：

```go
workflow.WithResumeHandler(func(
    ctx context.Context,
    runID string,
    saved *workflow.GraphSnapshot[State],
    input json.RawMessage,
) (State, error) {
    // 解析输入，检查决定、允许修改的字段及业务约束。
    // saved 是锁内最新检查点的独立副本；只将返回的 State 写回。
    return applyValidatedHumanInput(ctx, saved.State, input)
})
```

处理器在运行锁内执行，必须无副作用，不能重入同一运行的 `Run`、`Resume` 或 `Snapshot`。写盘失败后可能再次调用它。配置处理器后，旧的纯审批入口也会调用它，输入为 nil；需要人工载荷时应拒绝 nil，不能只靠 UI 必填。JSON `null` 与没有载荷不同，都会交给处理器校验。

`WithBeforeApprove` 在处理器之后校验**拟保存的新状态**，可复核暂停期间变化的订单事实、权限和期限；它拿到的也是独立副本，修改无效。业务上的“拒绝”可作为有效人工决定写入状态并路由到拒绝分支；`ResumeRejectedError` 表示本次输入无效，`ApprovalRejectedError` 表示授权/复核失败，两者都保持原检查点继续等待。

## 检查点与故障语义

- 恢复使用已保存状态，忽略 `RunRequest.Initial`；不能用重新传 initial 覆盖已审查内容。
- `Resume` 必须提供当前等待节点的 Key；旧 Key、其他运行的 Key 和已完成节点的 Key 返回 `ErrApprovalMismatch`。循环每次访问节点有不同的 Key。
- 输入验证、权限复核或保存失败时，不执行暂停的节点，不提交人工修改。存储错误和 panic 不伪装成正常等待。
- 人工状态与放行标记在同一次 Save 中提交。后续节点失败或进程退出后，通过 `Run(ctx, runID, zero, "")` 从已接受的状态恢复，不再调用人工输入处理器。
- 响应丢失或提交结果未知时先读 `Snapshot`：如果仍等待原 Key，可以重交；如果已越过该等待，用无审批键的 `Run` 恢复或读取完成结果。重复 `Resume` 不会再次应用旧修改。
- `AsRunnable` 在正常暂停时返回 `Waiting=true, Done=false` 的成功快照；流的 Done 只代表本次调用结束，业务是否完成看 `Data.Done`。`RunRequest.Resume` 和 `ApprovalKey` 不能同时提供。
- 步骤及外部副作用仍为至少一次执行。取消不会回滚副作用，节点需要响应 context；外部写入使用 `workflow.DurableKey(ctx)` 或更强的业务幂等键去重。
- 检查点绑定图拓扑与 `DurableConfig.Version`；改变处理器、授权策略、节点行为或状态结构时提升版本。保存的是节点边界和业务状态，不是 Go 调用栈，不能恢复函数内部的任意指令位置。

本机文件存储适合单机；多实例共享存储使用 goagent-contrib 的 `durable/pgstore`。发现待处理运行、消息通知、审批人的登录认证与自动续跑调度由宿主负责。

真实模型验证见 [Gemini / DeepSeek HITL 报告](../evaluation/2026-11-04-hitl-live/REPORT.md)及[可复现脚本](../../evaluation/hitlbench/README.md)。两者的检查点恢复通过；Gemini 的最终通知含有未经执行的退款完成声明，不能将执行成功等同于模型内容正确。

## 节点或工具执行中途暂停

`durable.AwaitInput(ctx, id, info, validate)` 在工作流节点、并行分支或声明可中断的工具里暂停，等人提供输入：

```go
raw, err := durable.AwaitInput(ctx, "amount", json.RawMessage(`{"question":"退款金额"}`), validateAmount)
if err != nil {
    return state, err // 等待原样返回，由框架记为暂停
}
```

- 图暂停时 `Run` 返回 `durable.ErrApprovalRequired`，`Snapshot.Interrupts` 列出 `Key`、`ID`、`Info`。用 `graph.Resume(ctx, runID, workflow.ResumeRequest{Key: point.Key, Input: ...})` 回应；也可以用 `durable.WithResume(ctx, point.Key, input)` 加上不带键的 `Run`。
- 中断等待时 `Snapshot.Key` 与 `ChildContinuation.ApprovalKey` 不是回应键（前者是节点意图键，后者为空），回应须用 `Interrupts[i].Key`。把意图键连同输入经 `workflow.WithResume` 交给子图时，输入不被采纳，返回 `Rejected=true, Reason=approval_mismatch` 的等待。
- 不带键的 `Run` 恢复没有冲突或拒绝信号：已回应的中断再交不同输入会被忽略（回放已固定的输入），`validate` 拒绝只体现在 `Snapshot.Interrupts[i].Rejected`。需要 `ResumeRejectedError` 时用带键的 `Resume`。
- 恢复会从头重新执行节点或工具。`AwaitInput` 之前的外部写入必须经 `durable.Do` 记录，或者按 `workflow.DurableKey(ctx)` 自行幂等，否则会重复执行。
- 首次成功持久化的输入就此固定，之后的重放都返回它。用同一个键再交不同输入时返回 `ResumeRejectedError`（`workflow.ErrInterruptAnswered`），节点不执行；输入是否相同按 `json.Compact` 后的字节比较。
- 输入不是合法 JSON 或 `validate` 失败时保持等待，`Interrupts[i].Rejected` 和 `Reason` 说明原因，修正后可以重交。`null` 是合法的显式输入。`validate` panic 时原因为 `validate_panic`，panic 值与调用栈不持久化，只在本次返回的错误中：`Resume` 的 `ResumeRejectedError` 或 `WaitError.Cause` 里可用 `errors.As` 取出 `*errors.PanicError`；错误文本为空时为 `rejected`。`validate` 的错误文本会持久化并经 `Snapshot`、`ResumeRejectedError` 暴露，不得包含凭证等敏感信息。
- `id` 在同一节点或工具内必须唯一，循环中须带序号，否则会回放上一次的答案。`info` 必须是确定性的 JSON，不得包含凭证。
- 并行节点中一个分支暂停时，兄弟分支照常完成，中断逐个回应。嵌套子图的 `ChildContinuation.Interrupts` 给出最内层中断键。
- 只有中断等待（来自 `AwaitInput`，可经子图逐层上报）会让节点暂停；节点内的其他等待，例如子图的审批等待，仍按节点失败处理。
- 工具须用 `tools.NewFunctionToolBuilder(...).WithInterruptible(version)` 显式开启；自定义工具实现 `tools.InterruptibleTool`。开启或改变语义时提升版本。等使用该工具的进行中运行结束后再切换；自行实现 `DurablePolicyProvider` 的工具，只改 `Resumable` 不会被框架检测到。
- 动态中断的回应不经过 `WithResumeHandler`、`WithBeforeApprove`。中断键不是授权凭证：宿主必须先认证回应者，并确认其有权回应该中断。

| 机制 | 适用 |
|---|---|
| `ApprovalNodes` + `WithResumeHandler` | 节点执行前审批，人工修改业务状态 |
| `durable.AwaitInput` | 节点或工具执行中途要一个输入 |
| `core.InterruptManager` | 进程内阻塞等待，不能跨进程恢复 |
| `Handle.Interrupt` | 取消正在运行的 Agent，不是暂停 |

## 嵌套在 Agent 工具中

将持久图的 `AsChildRunnable()` 通过 `tools.NewRunnableToolFactory` 包装，并设置 `RunnableToolConfig.Resumable=true`。宿主收到 `builder.ErrWaiting` 后，解析 `durable.Waits(err)` 和其中的 `workflow.ChildContinuation`。

```go
resumeCtx, err := workflow.WithResume(ctx,
    wait.Path[len(wait.Path)-1].Key, // 等待操作的执行键
    workflow.ResumeRequest{
        Key:   continuation.ApprovalKey, // 子图检查点键
        Input: json.RawMessage(`{"decision":"approve"}`),
    },
)
if err != nil { return err }
out, err := agent.AsRunnable().Invoke(resumeCtx, originalInput)
```

这两个 Key 不同，不能互换。子图与父操作使用相同 Store；输入拒绝会保持父操作为 waiting，并通过 `ChildContinuation.Reason=resume_input` 返回。旧的 `durable.WithResume(ctx, operationKey, approvalKey)` 纯审批调用继续可用。由 `Start` 开始的 Agent 应同样以 `Start` 恢复，见 [Builder](../../builder/README.md)。

## 现有 Interrupt 与 Checkpointer 的边界

`Handle.Interrupt` 取消当前运行，`Wait` 等待工具与清理退出。若要恢复取消的运行，必须事先配置持久日志并保持执行身份；被中断模型请求的重发还受 `WithInterruptedModelResend` 策略控制。

`core.InterruptManager` / `InterruptableExecutor` 用于进程内阻塞等待。`RespondToInterrupt` 可提交审批与 Input，配置的 `core/checkpoint.Checkpointer` 保存业务状态；保存失败会直接返回错误，不发布中断也不执行后续业务。但等待条目和响应通道仍只在内存中，不能靠这个 Checkpointer 重建跨进程等待。

`core/checkpoint.InMemorySaver` 在重启后丢失；`Checkpointer` 保存状态本身，也不包含持久图的执行位置与工具日志。跨进程 HITL 应使用上面的持久图与 `durable.Store`。
