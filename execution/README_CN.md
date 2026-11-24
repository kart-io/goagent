# execution 宿主执行范围

[English](README.md) | [中文](README_CN.md)

`execution` 把宿主提供的会话、日志、预算、期限和观测配置组合为一个执行范围。宿主已经持有运行身份和资源（持久存储、预算控制器、观测器或 Tracer），并希望同一次运行中的每个 Agent 调用都看到同一套装配时使用它。它不创建运行引擎、默认存储或额度，只把宿主传入的内容绑定到 context，并用一致的身份配置 Builder。`NewGovernedBuilder` 进一步装配一个 Builder，其工具调用经过显式授权、有界的 Dispatcher。

## 用法

### Scope、Bind、ConfigureBuilder

```go
const runID = "order-123"
controller, err := budget.New(budget.Config{RootKey: runID, Version: "v1", MaxModelRequests: 4, MaxAgentCalls: constants.DefaultMaxAgentCalls})
if err != nil {
    return err
}
scope, err := execution.NewScope(execution.Config{RunID: runID, SessionID: "user-42", Budget: controller, Timeout: time.Minute})
if err != nil {
    return err
}
runCtx, cancel, err := scope.Bind(ctx)
if err != nil {
    return err
}
defer cancel()

b, err := execution.ConfigureBuilder(builder.NewSimpleBuilder(client), scope, "assistant")
if err != nil {
    return err
}
agent, err := b.BuildContext(runCtx)
if err != nil {
    return err
}
out, err := agent.AsRunnable().Invoke(runCtx, "summarize the order")
```

可离线运行的版本见 [examples/execution-scope](../examples/execution-scope/main.go)。

### 治理工具流

```go
b, report, err := execution.NewGovernedBuilder(client, scope, "assistant", registry, tools.DispatchConfig{
    MaxConcurrency:          4,
    Timeout:                 30 * time.Second,
    MaxAdmissionConcurrency: 2,
    MaxQueued:               8,
    Authorize: func(_ context.Context, name string, _ *interfaces.ToolInput) error {
        if name != "lookup" {
            return fmt.Errorf("tool %s is not authorized", name)
        }
        return nil
    },
})
if err != nil {
    return err
}
// 与上例一样在 scope.Bind 下运行；report 是静态快照。
fmt.Println(report.MaxToolCalls, report.Dispatch.ToolNames)
```

含重建与重放的完整示例见 [examples/execution-governed](../examples/execution-governed/main.go)。

## 主要 API

| 标识符 | 说明 |
|---|---|
| `Config` | 宿主提供的 `RunID`、`SessionID`、`Journal`（`durable.Store`）、`Budget`（`*budget.Controller`）、`Timeout`、`Observer`、`Tracer` |
| `NewScope(Config)` | 校验身份、预算和可选组件；不访问存储 |
| `Scope.Bind(ctx)` | 返回携带会话、执行键、日志、运行 ID、观测器、Tracer、超时和预算的派生 context 及其 cancel 函数 |
| `Scope.RunID()` / `Scope.SessionID()` | 根运行身份（供图的 `RunRequest` 使用）与宿主会话身份 |
| `ConfigureBuilder(b, scope, agentID)` | 在 `AgentBuilder` 上设置 `WithSessionID`、`WithDurableScope(agentID)`、`WithAgentID(agentID)`；不执行构建 |
| `NewGovernedBuilder(client, scope, agentID, registry, cfg)` | 返回接入 `tools.Dispatcher` 的 `*builder.SimpleBuilder` 和一份 `GovernanceReport` |
| `GovernanceReport` | 创建时的快照：运行/会话/Agent ID、日志与预算模式、模型/工具额度、根超时与期限、分发治理配置 |

## 行为说明

- `RunID` 必须非空且无首尾空白；`Timeout` 不能为负。`SessionID` 为空时取 `RunID`。
- `Journal`、`Observer`、`Tracer` 可以为 nil，但不能是带类型的 nil。设置 `Budget` 时其 `RootKey` 必须等于 `RunID`；同时设置 `Journal` 时预算必须是持久预算。
- Scope 不关闭日志、观测器或 Tracer。`Bind` 返回的 cancel 只取消派生 context。
- `Bind` 拒绝：nil 或已结束的 context；已绑定 Scope 或日志的 context；执行键、会话 ID、观测 RunID 或预算控制器与 Scope 冲突的 context。每次 `Bind` 使用新的宿主 context。
- `Timeout > 0` 时追加 `context.WithTimeout`；预算通过 `budget.Bind` 绑定。
- `NewGovernedBuilder` 要求：Scope 带预算、`MaxToolCalls >= 1`、有根期限（Scope 的 `Timeout` 或预算的 `DeadlineUnixMilli`）、`Authorize` 非 nil、`MaxAdmissionConcurrency >= 1`、`MaxQueued >= 1`、客户端实现 `llm.ToolStreamClient`、至少注册一个工具。context 不是来自同一 Scope 的 `Bind` 时，模型请求和工具授权都会失败。
- 所有校验错误都是 `*errors.AgentError`，错误码为 `CodeInvalidConfig` 或 `CodeInvalidInput`。

## 另见

- [标准宿主接入](../docs/guides/HOST_INTEGRATION_CN.md)
- [执行治理](../docs/guides/EXECUTION_GOVERNANCE_CN.md)
- [最终验收与治理装配](../docs/guides/EXECUTION_ACCEPTANCE_CN.md)
- [执行契约](../docs/guides/EXECUTION_CONTRACT_CN.md)
