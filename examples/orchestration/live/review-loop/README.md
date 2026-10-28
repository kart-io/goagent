# 文稿审校循环：持久图中的条件分支循环

`write → review →（write | END）` 是一个持久图（`CompileDurable`）。审校节点后的 `AddBranch` 决定是回到写作还是结束，最多 `MaxRounds=3` 轮。每次节点访问都有独立检查点，执行键里带步骤号，所以循环的每一轮都能单独回放。根预算按"每轮写 + 审各 1 次"设为 6。

任务书只说"写退款政策公告，不超过 150 字"。审校清单另外要求三项具体事实：3 个工作日原路退回、定制刻字商品不适用、咨询邮箱 `refund@demo.example`。写作 Agent 第一轮无从得知这些条款，尤其是邮箱，所以必然被打回，循环一定会发生。

```sh
GOAGENT_LIVE_PROVIDER=deepseek go run ./examples/orchestration/live/review-loop -dir /tmp/review-1
```

`TestLiveReviewLoopRecovery` 分三个进程运行：

1. 第 2 轮写作开始前退出。此时第 1 轮的写、审已经提交，用了 2 次请求。
2. 新进程恢复。第 1 轮不再执行，从第 2 轮写作继续，直到通过。宿主用 `MissingFacts` 独立检查终稿，不依赖审校 Agent 的判断。断言"崩溃前请求数 + 恢复后新增请求数 = 预算预留数 = 2 × 轮次"。
3. 第三次运行完全回放：0 次请求，终稿和预算都不变。

设计中踩过的坑，都保留在评测日志里：

- 字数要求最初只写在审校清单里，Gemini 三轮都写超长稿，循环按上限结束、没有通过。改为写进任务书。
- 模型数不准字数，会把合格稿件判为"修改"。字数检查已从审校清单中移除。
- `deepseek-flash` 是推理模型，偶尔推理过长，触发 `finish_reason=length` 并被框架判为输出不完整。正常一次约 300 Token，节点输出上限放宽到 8192。
