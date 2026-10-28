# 流式摘要与预算争用

## 流式摘要（core/flow 原型）

`agentSource` 同步消费 `Agent.ExecuteStream` 的文本增量，把它作为 `flow.Source` 接入 `flow.AsRunnable`：

- `Map` 逐项规整文本（去掉 `\r`），`Fold` 拼接，`Finish` 去掉首尾空白，并拒绝空输出。
- 每个增量立即作为 `preview` 事件下发，但这只是展示。只有 Source、Map、Fold 全部成功后才提交一次终态。
- 下游出错时取消 Agent 的 context，并把事件通道排空，保证不残留生产者。

DeepSeek 逐 token 输出，首段预览明显早于完成（实测首段约 1.3–1.5 秒，完成约 1.6–1.8 秒，共 68–76 段）。Gemini 虽然是流式，但只分 2 段，首段几乎和完成同时到达。MiniMax 一次性返回，只有一段预览。

## 预算争用

`Contend` 启动 N 个互相独立的子 Agent，它们共享一份额度为 M 的内存根预算。准入发生在真实请求之前，并且并发安全：最多 M 个请求能发出，其余在发出前收到 `budget.ErrBudgetExceeded`。

```sh
GOAGENT_LIVE_PROVIDER=deepseek go run ./examples/orchestration/live/stream-budget -children 3 -limit 2
```

测试分两类：

- 离线：`TestSummarizeOffline` 覆盖多段、单段、空输出和已取消四种情况；`TestContendOffline` 覆盖 5 组子 Agent 数与额度的组合，含 10 个子 Agent 争 3 次额度。
- Live：`TestLiveSummarize` 检查三行格式、关键事实、预览拼接等于终态，以及逐 token 模型的首段时延；`TestLiveContend` 检查恰好 2 成功、1 被拒、只发出 2 个请求。
