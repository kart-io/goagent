# 工单分诊：子 Agent 工具与共享根预算

父 Agent 处理退款工单，必须依次调用两个工具：

1. `order_lookup`：一个两节点的规则图（加载订单 → 计算 7 天窗口），用 `tools.NewRunnableTool` 包装成工具。
2. `policy_agent`：用 `tools.NewRunnableToolFactory` 包装。每次调用都新建一个只知道退款政策的子 Agent，使用唯一 `SessionID`，父对话不会传进去；调用结束后清理，清理时限 `CleanupTimeout` 为 10 秒。

父子模型请求共用一份持久根预算（`budget.NewDurable`，额度 8）；模型与工具日志写入 `-dir`。

| 工单 | 订单事实 | 应得结论 |
| --- | --- | --- |
| A1001 | 未拆封，签收第 3 天 | 批准 |
| A1002 | 未拆封，签收第 20 天 | 拒绝（超过 7 天） |
| A1003 | 定制刻字，签收第 2 天 | 拒绝（定制类不退） |

```sh
GOAGENT_LIVE_PROVIDER=deepseek go run ./examples/orchestration/live/ticket-triage -dir /tmp/triage-a1001 -ticket A1001
# 同一目录再运行一次：NewModelCalls=0、ChildCreated=0，答案和预算快照不变
GOAGENT_LIVE_PROVIDER=deepseek go run ./examples/orchestration/live/ticket-triage -dir /tmp/triage-a1001 -ticket A1001
```

输出中 `NewModelCalls` 是本次真实请求数，`Budget.ReservedRequests` 是根预算累计准入数。两者在首次运行时相等，说明父、子 Agent 的请求都经过了根预算。

测试：`TestOrderLookupTool` 离线检查规则图；`TestLiveTicketTriage` 用真实模型跑 3 张工单，检查结论、规则图只执行一次、子会话都被清理、请求数等于预算预留数，以及回放零请求。
