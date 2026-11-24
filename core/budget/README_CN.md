# budget 根预算

[English](README.md) | [中文](README_CN.md)

`budget` 在一次由宿主掌握的执行范围内约束模型请求的准入。`Controller` 是一次逻辑运行（`Config.RootKey`）的根账本：按可选限额和截止时间准入模型请求与工具执行，逐次尝试登记 Token 用量证据，并把用量归属到发起调用的 Agent。预留是保守的：崩溃从不退还不确定的请求。当一次运行（含子 Agent 与工具）必须控制在请求数、Token、工具调用次数或时间预算之内，或需要按 Agent 统计 Token 时使用它。

## 用法

把预算绑定到一次运行；在该 context 下运行的 Builder 与工具 Dispatcher 共享它：

```go
controller, err := budget.NewDurable(budget.Config{
    RootKey: requestID, Version: "task-v1",
    MaxModelRequests:  20,
    MaxToolCalls:      30,
    DeadlineUnixMilli: time.Now().Add(5 * time.Minute).UnixMilli(),
}, journal) // 内存预算使用 budget.New(config)
if err != nil {
    return err
}
runCtx, cancel := budget.Bind(ctx, controller)
defer cancel()
```

直接准入与记账，以及用量钩子：

```go
c, err := budget.New(budget.Config{RootKey: "run", Version: "v1", MaxModelRequests: 2})
if err != nil {
    return err
}
if err := c.Reserve(ctx, "call-1"); errors.Is(err, agentErrors.ErrModelBudgetExceeded) {
    return err // 超出限额：不要发送请求
} else if err != nil {
    return err
}
// ... 发送模型请求 ...
u := &interfaces.TokenUsage{PromptTokens: 2, CompletionTokens: 3, TotalTokens: 5}
if err := c.RecordUsage(ctx, "call-1", u); err != nil { // 传 nil 表示登记未知用量
    return err
}
snap, err := c.Snapshot(ctx)

ctx = budget.WithUsageHook(ctx, func(ctx context.Context, e budget.UsageEvent) {
    _ = e.Model.Tokens // 仅本次尝试
    if e.HasStatistics {
        _ = e.Statistics.Root.KnownTokens.TotalTokens // 时点累计；不要跨事件相加
    }
})
```

## 主要 API

| 标识符 | 说明 |
|---|---|
| `Config` | 宿主提供的策略：`RootKey`、`Version`、`MaxModelRequests`、`MaxReservedTokens`、`TokenPolicyVersion`、`MaxToolCalls`、`MaxAgentCalls`、`DeadlineUnixMilli` |
| `New(Config)` / `NewDurable(Config, durable.Store)` | 内存控制器 / 持久化到稳定根记录的控制器 |
| `Bind(ctx, c)` | 绑定根预算及其绝对截止时间，并把截止时间传播到图节点和子任务 |
| `WithController(ctx, c)` / `FromContext(ctx)` | 与子模型调用、子工具调用共享控制器 / 读回控制器 |
| `Controller.Reserve`、`ReserveEstimated`、`ReserveAttributed` | 在网络调用前准入一次模型请求；带估算的形式同时预留宿主的 Token 估算值 |
| `Controller.ReserveTool(ctx, key)` | 准入一次实际工具执行 |
| `Controller.RecordUsage` / `RecordEvidence` | 为已预留的键登记基本 Token 用量 / 带字段位图的用量证据 |
| `Controller.Remaining(ctx)` | 仍可预留的模型请求数（时点值） |
| `Controller.Snapshot(ctx)` / `Attribution(ctx)` | 根汇总 / 按调用和按 Agent 的 `Self` 与 `Inclusive` 用量 |
| `WithUsageHook(ctx, hook)` / `UsageHook` / `UsageEvent` | 只读的逐尝试用量通知，可附带累计快照 |
| `WithUsageHookTimeout(ctx, d)` | 限制一次统计查询加钩子调用的耗时 |

## 行为说明

- 所有限额由调用方决定：0 表示不限制。`New` 拒绝空的 `RootKey` 或 `Version`、负数限额，以及设置了 `MaxReservedTokens > 0` 却没有 `TokenPolicyVersion` 的配置。
- 配置随持久账本保存，在 `RootKey` 的整个生命周期内不得改变。
- 超出请求数或工具调用次数限额时准入返回 `errors.ErrModelBudgetExceeded`；重复使用键返回 `errors.ErrReservationExists`。超过 `DeadlineUnixMilli` 后准入返回包装了 `context.DeadlineExceeded` 的错误。
- 设置了 `MaxReservedTokens` 时，`Reserve` 返回 `errors.ErrTokenEstimateRequired`，应改用 `ReserveEstimated`。预留从不退还，无论实际用量更少、调用被取消还是进程崩溃。
- `RecordUsage` 要求该键已预留；传 `nil` 登记未知用量。重复登记相同证据是幂等的；改变已知字段会报错。
- journal 回放时不应调用 `ReserveTool`；重试是新的执行，应使用新键。在持久 journal 下它要求持久控制器。
- `Controller` 可并发使用；持久控制器通过 Store 锁协调。`Remaining` 只是快照，准入由 `Reserve` 决定。
- 用量钩子在记账之后、账本锁之外同步执行，所用 context 脱离执行取消信号并受 `WithUsageHookTimeout` 约束（0 取 `constants.DefaultUsageHookTimeout`，负值被拒绝）。钩子必须支持并发调用并及时返回；其失败和 panic 不改变业务结果。钩子不是持久投递：应以 `Attribution` 为准核对。

## 另见

- [根预算、资源治理与运行诊断](../../docs/guides/EXECUTION_GOVERNANCE_CN.md#根预算)
- [可组合编排：根预算和用量](../../docs/guides/COMPOSABLE_ORCHESTRATION_CN.md#根预算和用量)
- [多 Agent 钩子与 Token 统计](../../docs/guides/MULTI_AGENT_USAGE_CN.md)
