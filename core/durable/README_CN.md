# durable 持久步骤与操作

[English](README.md) | [中文](README_CN.md)

`durable` 执行显式、带版本的步骤，并持久化续跑位置，使一次运行在等待审批或进程崩溃后，可在同一进程或新进程中从最后保存的位置继续。它不序列化 Go 闭包，也不恢复任意 Agent 调用栈：调用方注册具名步骤，本包把下一步序号、步骤间传递的 JSON 数据、审批状态和每步的幂等键保存到 `Store`。在步骤内部，`Do` 为具名子操作记账，已完成的结果回放而不重跑。需要在进程重启后续跑的多步任务使用本包；图编排使用 `workflow.Graph[T].CompileDurable`，它经同一个 `Store` 持久化。

## 用法

```go
import (
    "context"
    "errors"

    "github.com/kart-io/goagent/codec/json"
    "github.com/kart-io/goagent/core/durable"
    agentErrors "github.com/kart-io/goagent/errors"
)

store, err := durable.NewFileStore("/var/lib/myapp/durable")
if err != nil {
    return err
}
runner := &durable.Runner{
    Store:   store,
    Version: "refund-v1",
    Steps: []durable.Step{
        {Name: "prepare", Run: func(ctx context.Context, in durable.StepInput) (json.RawMessage, error) {
            return json.RawMessage(`{"amount":100}`), nil
        }},
        {Name: "pay", RequireApproval: true, Run: func(ctx context.Context, in durable.StepInput) (json.RawMessage, error) {
            // in.Key 在重启后保持不变：作为幂等键传给支付接口。
            return json.RawMessage(`"paid"`), nil
        }},
    },
}

snap, err := runner.Run(ctx, "refund-42", json.RawMessage(`{"order":"o-1"}`), "")
if errors.Is(err, agentErrors.ErrApprovalRequired) {
    // 保存 snap.Key；宿主完成授权后用它续跑。
    snap, err = runner.Run(ctx, "refund-42", nil, snap.Key)
}
```

在步骤内记账的子操作（Runner 已把 store 与执行键绑定到步骤的 ctx）：

```go
func charge(ctx context.Context, in durable.StepInput) (json.RawMessage, error) {
    rec, err := durable.Do(ctx, "charge", in.Data,
        durable.OperationPolicy{MaxAttempts: 3, RetryFailed: true},
        func(ctx context.Context) (json.RawMessage, error) {
            // durable.ExecutionKey(ctx) 是派生键，用于外部调用去重。
            return json.RawMessage(`{"charged":true}`), nil
        })
    if err != nil {
        return nil, err
    }
    return rec.Value, nil
}
```

## 主要 API

| 标识符 | 说明 |
|---|---|
| `Runner` / `Runner.Run(ctx, id, initial, approvalKey)` | 按 `Version` 启动或续跑运行 `id` 的 `Steps`；`BeforeApprove` 在审批生效前复核数据 |
| `Step` / `StepInput` | 具名步骤，可设 `RequireApproval`；输入含 `Data` 与幂等键 `Key` |
| `Snapshot` | 持久化的续跑点：`Next`、`Data`、`Waiting`、`Approved`、`Key` |
| `Store` | `Lock`、`Load`、`Save`、`SaveAll`；契约写在接口注释中 |
| `Scoped` | 可选的 Store 分区能力：`Scope`、`RemoveScope`、`PruneRemoved` |
| `NewFileStore(dir)` | 本地文件系统 `Store`（同时实现 `Scoped`） |
| `Do(ctx, id, input, policy, run)` | 为具名子操作记账；`OperationPolicy` 设置 `MaxAttempts`、`RetryFailed`、`RetryInterrupted`、`Resumable`、`Reconcile` |
| `InspectOperation` / `DiagnoseOperations` | 读取子操作记录；对显式指定的 `OperationRef` 做只读诊断 |
| `AwaitInput` / `WaitError` / `WithResume` / `InterruptAnswer` | 暂停操作等待宿主输入并续跑 |
| `OpenInbox` / `Inbox` | 运行期间收到的输入，持久保存直到 `Ack` |
| `WithJournal`、`WithExecutionKey`、`ExecutionKey`、`ChildKey` | 把 store 与执行键绑定到 ctx；派生子键 |
| `WithReplayOnly` | `Do` 只回放已完成记录，不调用 `run` 或 `Reconcile` |

## 行为说明

- 步骤至少执行一次。调用步骤前先保存位置与键；外部效果已发生、下一个检查点尚未保存时崩溃，重启后以相同 `Key` 重跑该步骤，外部写入必须按它去重。
- `Run` 要求非空的 `id`、`Version`、`Store`，步骤名非空且唯一。计划（版本加步骤名与审批标志）不一致的检查点会被拒绝，因此修改步骤需要新的 `Version`。
- `initial` 只用于新运行，必须是合法 JSON（为空即 `null`）；步骤输出也必须是合法 JSON。
- `RequireApproval` 的步骤保存 `Waiting` 并返回 `ErrApprovalRequired`。续跑需要精确的 `Snapshot.Key`，它把审批绑定到该运行、计划和步骤，是幂等键而非认证凭据；授权由宿主负责。
- ctx 中没有记账 store 或执行键时，`Do` 直接调用 `run`。有记账时已完成记录直接回放；`MaxAttempts` 默认为 1，`Do` 在单次调用内从不重试：`RetryFailed` 只允许之后的调用再试一次。中断的记录只有在 `Reconcile` 确认效果未发生，或设置了 `RetryInterrupted` 时才重跑。
- 持久化的操作错误只含类别，不含第三方错误文本。`AwaitInput` 校验函数返回的错误文本会被持久化，不得包含敏感信息。
- `FileStore` 使用操作系统文件锁、fsync 与同目录原子重命名，面向单机本地文件系统，不是分布式租约服务；多实例共享的运行，执行契约指向 goagent-contrib 的 `durable/pgstore`。
- `Store.Lock` 一直等到取得锁或 ctx 结束，不可重入，也没有死锁检测。

## 子包

- [migrate](migrate/README_CN.md)：把旧操作账本显式一次性转换为 `goagent-operation-v2`
- `storetest`：`Store` 实现的一致性测试（`storetest.Run`）

## 另见

- [执行契约](../../docs/guides/EXECUTION_CONTRACT_CN.md)：`core/durable` 能恢复与不能恢复的内容，Store 的选择
- [原生编排](../../docs/guides/NATIVE_ORCHESTRATION_CN.md)：与 `workflow`、工具共享的执行键
- [扩展契约](../../docs/guides/EXTENSION_CONTRACT_CN.md)：`durable.Store` 实现的义务
