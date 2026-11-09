---
status: fixed
finding: CORE-01
production_file: builder/events.go
test_file: builder/session_log_nested_test.go
---

# 父子 Agent 会话日志隔离修复

在公共流式调用与 Handle 共用的 `executeStream` 入口屏蔽继承的 `sessionLogKey`，本次 `runEvents` 只有显式配置 SessionLog 才能创建自己的轮次。保留 Handle 自有 inbox/history、共享执行预算和工具上下文。

新增回归覆盖普通 Client、无工具原生 Client、工具子 Agent、Handle 子 Agent、显式独立日志与 Handle 独立日志。断言子调用前后父日志完全相同、父轮次保持打开、父系统提示和工具定义不变；另外验证失败父历史被中止，以及子调用完成后父轮次中断、重建后可继续持久执行。

修复前这些断言复现父日志被追加、同键恢复进入只读失败及失败历史残留。最初测试夹具只实现工具流导致普通路径 EOF，已改用同时支持普通/工具流的既有 ScriptedClient；夹具初次输出另存 `core-fixture-initial.log`，有效红证据为 `core-red.log`。

修复后命令均通过：

```sh
env -u GOROOT GO_TEST_MODE=true go test -race -count=3 -timeout=120s ./builder \
  -run '^(TestNested(AgentSessionLog|PlainAgent)|TestPlainSession|TestSessionLog|TestHandle)'
env -u GOROOT GO_TEST_MODE=true go test -race -count=1 -timeout=60s \
  -overlay=docs/evaluation/2026-11-09-management-hardening/review/repro/current-overlay.json \
  ./builder -run '^TestCoreReviewPlainChild'
```

原专业审查的三个独立反例也全部转绿。定向日志位于 `/tmp/goagent-review-fix-8cIyLB/core-green.log` 与 `core-review-counterexamples.log`；最终全量与跨仓证据由汇总报告记录。
