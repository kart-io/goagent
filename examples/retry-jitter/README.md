# ToolExecutor 重试抖动示例

演示 `tools.ToolExecutor` 的重试抖动（Jitter）：在指数退避延迟上叠加随机偏移，让大量客户端的重试时间错开，避免雷群效应。示例使用一个总是失败的本地工具，不需要 API Key。

## 运行

```bash
go run ./examples/retry-jitter
```

示例依次运行三段，共约 10 秒：

1. 默认抖动：`MaxRetries: 3`、`InitialDelay: 1s`、`Multiplier: 2`，未设置 `Jitter`。
2. 自定义抖动：`MaxRetries: 2`、`InitialDelay: 500ms`、`Jitter: 0.5`。
3. 同一策略（`MaxRetries: 1`、`InitialDelay: 100ms`、`Jitter: 0.25`）执行 10 次，打印每次耗时以观察随机性。

## 配置

```go
executor := tools.NewToolExecutor(
    tools.WithRetryPolicy(&tools.RetryPolicy{
        MaxRetries:   3,
        InitialDelay: time.Second,
        MaxDelay:     10 * time.Second,
        Multiplier:   2.0,
        Jitter:       0.5, // 不设置或 <= 0 时使用 0.25
    }),
)
results, _ := executor.ExecuteParallel(ctx, []*tools.ToolCall{call})
```

第 n 次重试的延迟由 `calculateRetryDelay`（`tools/executor_tool.go`）计算：

- 基础延迟 = `InitialDelay × Multiplier^n`，再乘以 `1 ± Jitter` 的随机系数；
- `Jitter` 大于 1 时按 1 处理；
- 结果被限制在 `[InitialDelay, MaxDelay]` 内，因此第一次重试的延迟不会低于 `InitialDelay`（示例 3 中实际落在 100ms 到 125ms 之间）。

## 测试

```bash
go test ./tools -run TestToolExecutor_RetryWithJitter -v
```

## 参考

- [AWS Architecture Blog - Exponential Backoff And Jitter](https://aws.amazon.com/blogs/architecture/exponential-backoff-and-jitter/)
