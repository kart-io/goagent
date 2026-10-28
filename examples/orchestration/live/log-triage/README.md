# 长日志排障：上下文卸载与跨进程恢复

`fetch_logs` 返回 120 行、10809 字的支付服务日志，其中第 84 行是唯一的 ERROR。`contextpolicy.Offload` 把超过 400 字的工具结果存进持久 `ResultStore`，模型看到的只是一个引用：

```json
{"context_result": "r1", "runes": 10809, "read_tool": "read_context_result"}
```

模型必须调用 `read_context_result` 按 `offset` 分页读取（每页 3000 字）。错误行在第二页，所以至少要翻页一次。模型请求、工具调用、引用和根预算（额度 10）都持久化在 `-dir`。

```sh
GOAGENT_LIVE_PROVIDER=minimax go run ./examples/orchestration/live/log-triage -dir /tmp/pay-incident
```

`TestLiveLogTriageCrashRecovery` 用子进程模拟崩溃，分三个进程运行：

1. **crash**：第一次分页读取开始时 `os.Exit(86)`，此时已完成 fetch 回合和发起读取的模型回合。退出发生在读取工具校验参数之前，模型传入的 key 要到恢复进程执行读取时才被检查。
2. **resume**：同一目录重新运行。已完成的模型回合和 `fetch_logs` 直接回放，未完成的读取按 Dispatcher 策略重试，随后得出答案。断言“崩溃前请求数 + 本次新增请求数 = 预算预留数”，即没有请求被重发。
3. **replay**：第三次运行零请求、零工具执行，答案和预算不变。

读取是幂等的，所以恢复时可以重试。有外部副作用的工具需要目标端按 `ExecutionKey` 去重，见[原生编排指南](../../../../docs/guides/NATIVE_ORCHESTRATION.md)。
