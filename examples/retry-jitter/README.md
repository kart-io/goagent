# ToolExecutor 重试抖动示例

## 概述

此示例演示了 ToolExecutor 的重试抖动（Jitter）功能。通过在重试延迟中添加随机抖动，可以有效避免高并发场景下的雷群效应（Thundering Herd）。

## 什么是雷群效应？

当大量客户端同时遇到失败并使用固定的指数退避策略重试时，它们会在相同的时刻再次发起请求，导致：

- 服务器瞬时压力骤增
- 重试风暴持续发生
- 系统恢复困难

## Jitter 如何解决这个问题？

通过在每次重试延迟中添加随机抖动，不同客户端的重试时间会分散开来：

```
固定延迟（无 Jitter）：
客户端 1: 1s -> 2s -> 4s
客户端 2: 1s -> 2s -> 4s
客户端 3: 1s -> 2s -> 4s
所有请求同时到达！❌

带 Jitter（25%）：
客户端 1: 0.9s -> 1.8s -> 3.6s
客户端 2: 1.1s -> 2.3s -> 4.2s
客户端 3: 0.8s -> 2.1s -> 3.9s
请求分散到达！✅
```

## 使用方法

### 默认 Jitter（25%）

```go
executor := tools.NewToolExecutor(
    tools.WithRetryPolicy(&tools.RetryPolicy{
        MaxRetries:   3,
        InitialDelay: time.Second,
        MaxDelay:     10 * time.Second,
        Multiplier:   2.0,
        // Jitter 未设置，自动使用默认值 0.25
    }),
)
```

### 自定义 Jitter

```go
executor := tools.NewToolExecutor(
    tools.WithRetryPolicy(&tools.RetryPolicy{
        MaxRetries:   3,
        InitialDelay: time.Second,
        MaxDelay:     10 * time.Second,
        Multiplier:   2.0,
        Jitter:       0.5, // 50% 抖动
    }),
)
```

## Jitter 参数说明

- **取值范围**：0.0 - 1.0
- **默认值**：0.25（25%）
- **计算方式**：实际延迟 = 基础延迟 × (1 ± Jitter)

### 推荐配置

| 场景 | 推荐 Jitter | 说明 |
|------|------------|------|
| 低并发（< 10 个客户端） | 0.1 - 0.25 | 较小抖动即可 |
| 中并发（10 - 100 个客户端） | 0.25 - 0.5 | 默认值适用 |
| 高并发（> 100 个客户端） | 0.5 - 1.0 | 需要更大的分散度 |

## 安全特性

### 1. 延迟下限保护

即使抖动产生负值，实际延迟也不会低于 `InitialDelay`：

```go
policy := &tools.RetryPolicy{
    InitialDelay: 10 * time.Millisecond,
    Jitter:       1.0, // 100% 抖动可能产生负值
}
// 实际延迟始终 >= 10ms
```

### 2. 延迟上限保护

抖动后的延迟不会超过 `MaxDelay`：

```go
policy := &tools.RetryPolicy{
    MaxDelay: 2 * time.Second,
    Jitter:   0.5,
}
// 实际延迟始终 <= 2s
```

### 3. 指数退避仍然生效

抖动只是在指数退避的基础上添加随机性，不会改变总体递增趋势：

```
第 1 次重试: ~1s  (范围: 0.75s - 1.25s)
第 2 次重试: ~2s  (范围: 1.5s - 2.5s)
第 3 次重试: ~4s  (范围: 3s - 5s)
```

## 运行示例

```bash
go run examples/retry-jitter/main.go
```

## 测试

完整的测试套件位于 `tools/executor_test.go`：

```bash
go test -v ./tools -run TestToolExecutor_RetryWithJitter
```

## 性能影响

- **计算开销**：每次重试增加一次随机数生成（纳秒级）
- **内存开销**：无额外内存分配
- **总体影响**：可忽略不计

## 参考资料

- [AWS Architecture Blog - Exponential Backoff And Jitter](https://aws.amazon.com/blogs/architecture/exponential-backoff-and-jitter/)
- [Google SRE Book - Handling Overload](https://sre.google/sre-book/handling-overload/)
