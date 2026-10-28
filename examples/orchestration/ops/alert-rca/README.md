# 告警根因：证据逐字核对与有界修复

```text
告警 ──▶ 根因 Agent ──metrics_query / logs_query / change_query──▶ 合成数据（结果带 ref，如 E3fa29c）
                    └─submit_conclusion──▶ 宿主按账本核对证据 ──不通过──▶ 作为参数错误交还模型（最多 2 次）
                                                         └─通过──▶ 渲染“结论：…”直接作为最终回答
```

- 合成场景：`checkout-api` 5xx 升高；10:02 有一次配置变更，把连接池上限从 200 改为 50。另外安排了一个干扰项：`search-api` 在 09:30 发布过，但与告警无关。按数据应得的根因是 `config_change`。
- 数据工具的参数用 Schema 枚举约束。所有工具都经过开启 `ReturnArgumentErrors` 的 Dispatcher，参数错了会交还模型改正，不会中止调查。同一轮的多个查询并发执行；引用号由执行键派生，与完成顺序无关。
- `submit_conclusion` 要求每条证据写明结果的 `ref`，并给出从该结果中**原样复制**的片段。摘录必须是该结果中按原顺序连续的一段，可以跨字段，JSON 引号、括号、逗号可省略；截断数字（如把 `in_use=50` 写成 `in_use=5`）、截断单词、只摘字段名、改写内容或调换顺序都不通过。
  - 核对不通过时，问题作为 `tools.ArgumentError` 交还模型修复。
  - 修复第 3 次仍不通过就中止，不发布未经核验的结论。
  - 核对只证明摘录出自该次工具结果，不证明摘录支撑 `root_cause`。
- `submit_conclusion` 用 `WithReturnDirectTools` 注册：通过核验即返回“结论：<root_cause>”加 summary 作为最终回答并结束运行，省去一次模型总结请求，也不存在“最终回答与结论不一致”。模型不提交就直接作答则报错。`MaxIterations` 为 9（不再为总结预留一轮），并开启 `WithEmptyAnswerRetries(1)`。
- 账本、结论提交、Dispatcher 封装见 [opscase](../internal/opscase/opscase.go)。

```sh
GOAGENT_LIVE_PROVIDER=deepseek go run ./examples/orchestration/ops/alert-rca -dir /tmp/alert-rca-1
```

| 测试 | 断言 |
| --- | --- |
| `TestScriptedConclusionAccepted` | 一次提交即通过，最终回答由结论渲染；共 4 次请求，与预算预留数相等；同目录再次运行 0 次请求，也不执行工具，结论由回放恢复 |
| `TestScriptedRepair` | 指标名不在枚举内、摘录与原文不符，两类错误各交还模型一次；改正后通过，修复计数为 1，共 6 次请求 |
| `TestScriptedRepairLimit` | 连续提交编造的摘录：交还 2 次后第 3 次中止，没有被接受的结论 |
| `TestScriptedAnswerBoundToConclusion` | 不提交直接回答“结论：code_deploy”报错 |
| `TestLiveAlertRCA` | 真实模型得出 `config_change`，结论通过核验，修复不超过 2 次，请求数等于预算预留，回放 0 次请求 |

回放说明：持久回放时工具与 `submit_conclusion` 都不会重新执行。账本与已接受的结论都经 Dispatcher 的 `OnResult` 从日志重建：结论取自那次提交调用的参数，所以宿主不另行落盘；无论在调查中途还是 Agent 结束后、宿主处理前崩溃，同目录重跑都能恢复（见 opscase 的 `TestLedgerRebuiltAfterCrash`、`TestSubmitterRecoversAfterAgentEnd`）。
