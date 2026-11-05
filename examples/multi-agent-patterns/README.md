# multi-agent-patterns

离线演示 `multiagent` 预置模式，模型由 `testing/mocks.ScriptedClient` 按脚本回答，不需要 API 密钥：

| 场景 | 模式 |
| --- | --- |
| S1 工单分流 | `RouterSupervisor` + `TextNext` |
| S2 回复写作与评审 | `Loop` + `Until` |
| S3 多源并行查询 | `Sequential` + `ParallelStep` |
| S5 高风险操作审批 | 持久 `RouterSupervisor`，退款成员内 `durable.Do` 与 `durable.AwaitInput` |
| S7 复杂咨询由模型委派 | `SupervisorTools` |
| S9 判断无效转人工 | `RouterConfig.Fallback` |
| 组合流程 | 手写图 + `AddStep` + `GuardRoute` |

```bash
go run ./examples/multi-agent-patterns
go test ./examples/multi-agent-patterns
```

S5 的账本只为演示；真实业务使用 `workflow/refund.Service` 与带事务的存储。示例以 `"refund-"+订单号` 作申请 ID，真实业务的申请 ID 应取自工单，否则同一订单的第二笔合法退款会被 `Register` 拒绝。各模式的取舍与持久接入契约见 [多 Agent 预置模式指南](../../docs/guides/MULTI_AGENT_PATTERNS.md)。
