# DeepSeek 原生 Provider 与订单参数实测

2026-10-27 重新调用 DeepSeek 官方 API，以 `providers.NewDeepSeekWithOptions` 的原生工具流运行退款合成题。12 次运行全部完成：答案、调用契约、订单参数均为 12/12，用量完整 12/12，共 21 次模型请求、8,466 Token。没有删除失败或通过重跑筛选结果。

本轮是三道独立合成题，每个配置重复三次；退款状态分别执行普通循环和直返，形成 12 次运行。不是 12 道独立业务题，也不构成生产验收。与旧报告相比 Provider 和参数 schema 均有变化，不能将耗时或 Token 差异解释为单一框架优化收益。

## 配置与结果

源码基于提交 `91e6bbb923cf6269a76e98ad4b4c4a50cc3f33b1` 的未提交工作区（`goagent_dirty=true`），并非该提交的干净版本。Go 版本为 go1.27.1 linux/amd64。模型为 `deepseek-flash`，温度请求值 0.2，未显式配置 thinking；服务端实际模式由供应商决定。

每任务最多 2 次上游请求，每请求最多 2,048 输出 Token，观察用量阈值 8,000，单任务超时 90 秒，单并发。总计划上限 24 次请求；没有隐式失败重试。价格与缓存计费未采集，因此不换算账单金额。

| 场景 | 答案/参数/契约通过 | 模型请求 | 总 Token | P50 / P95（秒） |
|---|---:|---:|---:|---:|
| 退款期限提取 | 3/3 | 6 | 2,277 | 2.514 / 2.611 |
| 退款状态普通循环 | 3/3 | 6 | 2,301 | 2.502 / 2.789 |
| 退款状态直返 | 3/3 | 3 | 1,503 | 1.371 / 1.517 |
| 资料不足拒答 | 3/3 | 6 | 2,385 | 2.680 / 2.725 |
| 合计 | 12/12 | 21 | 8,466 | — |

输入 Token 7,827，输出 Token 639。每组只有三次运行，P95 是组内最大值，不用于推断长期尾延迟。

参数评分比较 `lookup.expected_arguments` 与实际调用的 JSON 对象。订单编号来自用户问题，参数标准答案只用于评分。错误、额外或缺失字段均不能通过；调用次数仍要求工具恰好一次。工具返回预置 evidence，不根据传入参数执行生产查询，因此该分数验证参数提取，不验证后端权限或查询隔离。

资料不足题正确输出 `UNKNOWN`；退款期限题返回 `7`；状态题原样返回指定文本。这些固定答案检查不构成自动逐句证据蕴含评估。

## 复跑

环境需设置 `DEEPSEEK_API_KEY`；输出目录必须不存在。去掉 `-live` 只生成计划。

```sh
go run ./evaluation/agentbench/cmd/stress -live \
  -provider deepseek -model deepseek-flash \
  -dataset evaluation/agentbench/testdata/refund-parameter-cases.json \
  -mode all -repeats 3 -workers 1 \
  -max-calls 2 -max-output-tokens 2048 -token-stop 8000 -timeout 90s \
  -out /tmp/goagent-native-refund-live
```

原始材料：[题集](dataset.json)、[配置及题集哈希](manifest.json)、[全部逐次结果](results.jsonl)、[汇总](summary.json)。第三步的独立离线验证见[退款审批与崩溃恢复案例](../../../examples/workflow/refund-recovery/README.md)：不调用模型、不执行真实退款。[命令行实跑记录](recovery-smoke.json)确认审批前零退款、提交后进程退出码 86、重启后完成且账本仅一笔、重复恢复返回同一结果。已完成的检查图没有重跑。

本轮使用 CI 对齐的 Go 1.25 验证：全仓短测试 62 个测试包通过；Builder、Provider、评测、durable、workflow 与退款恢复案例的相关竞态测试通过；`go vet ./...`、`go build ./...`、导入分层检查均通过；golangci-lint v2.6.2 报告 0 issues。SQLite 驱动编译时有上游 C 的 const 限定符警告，未导致编译或测试失败。
