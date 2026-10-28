# 并行诊断与观测报告

`diagnose` 是一个 `AddParallel` 节点，CPU、数据库、网络三个分支各由一个 Agent 判断（`WithDurableScope` 各不相同），然后交给 `summarize` 节点的汇总 Agent 决定“升级”或“观察”。各分支的判定规则写在提示里，合成指标按规则应得：CPU 异常、数据库正常、网络异常，汇总结论为升级。

观测相关：

- `devtools.Recorder` 把事件异步写入 `events.jsonl`。默认只记录类型、路径、耗时、错误类别和 trace 关联，不记录提示词、输入或模型正文。
- `Graph.Describe` 导出拓扑，`devtools.WriteReport` 生成转义、无脚本的 `report.html`。
- 宿主通过 `observe.WithTracer` 传入自己的 OTel tracer 后，graph、node、branch、agent、model 的 span 都挂在宿主 trace 下。

```sh
GOAGENT_LIVE_PROVIDER=deepseek go run ./examples/orchestration/live/parallel-diagnostics -dir /tmp/diag-report
# 用浏览器打开 /tmp/diag-report/report.html
```

`TestLiveParallelDiagnostics` 检查以下几点：

- 各分支结论和汇总结论正确。
- 4 次请求全部计入预算。
- 每种事件的 start 与 end 成对出现，且没有丢弃事件。
- 事件文件里不出现提示词、指标原文和“结论”字样。
- 报告中没有 `<script`。
- 所有 span 属于同一条 trace，且父 span 都能找到。
- 三个分支的模型请求在时间上有重叠，证明它们是并发执行的。
