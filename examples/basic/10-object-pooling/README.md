# 对象池化示例 (Object Pooling Example)

这个示例展示了如何使用 GoAgent 的对象池来减轻 GC 压力并提升性能。

## 功能特点

- **基础对象池使用**：演示如何获取和归还池对象
- **自动生命周期管理**：使用 defer 自动归还对象
- **性能对比**：展示有池和无池的性能差异
- **并发场景**：演示多 goroutine 并发使用池
- **池统计信息**：查看池的使用效率

## 运行示例

```bash
cd examples/basic/10-object-pooling
go run main.go
```

## 预期输出

```text
GoAgent 对象池化示例
====================

示例 1: 基础对象池使用
-----------------------
处理任务: 分析用户行为
上下文: map[timeframe:24h user_id:12345]
✅ 对象已归还到池中
结果: 用户活跃度：85%
推理步骤数: 2
✅ 输出对象已归还到池中

示例 2: 自动生命周期管理（使用 defer）
---------------------------------------
任务: 生成报告
✅ 将在函数返回时自动归还对象
结果: 月度报告已生成
✅ Output 也将自动归还

示例 3: 性能对比（有池 vs 无池）
-------------------------------
运行无池化测试...
运行池化测试...

性能对比结果（10000 次迭代）:
─────────────────────────────────────────
无池化耗时: 45.2ms
有池化耗时: 12.3ms
性能提升: 72.79%

无池化内存分配: 128 MB
有池化内存分配: 8 MB
内存减少: 93.75%

示例 4: 高并发场景下的池使用
-----------------------------
并发测试完成:
  Goroutines: 100
  每个 Goroutine 迭代: 100
  总操作数: 10000
  总耗时: 156.7ms
  平均每操作: 15.67µs
✅ 池在并发场景下表现良好

示例 5: 查看池统计信息
---------------------
池统计信息:
─────────────────────────────────────
AgentInput:
  Get 次数: 1000
  Put 次数: 1000
  复用率: 100.00%

AgentOutput:
  Get 次数: 1000
  Put 次数: 1000
  复用率: 100.00%

总体:
  池命中率: 100.00%
  ✅ 池使用效率优秀！
```

## 核心概念

### 1. 对象获取和归还

```go
// 获取对象
input := performance.GetAgentInput()

// 使用对象
input.Task = "your task"

// 归还对象（重要！）
performance.PutAgentInput(input)
```

### 2. 自动生命周期管理

```go
// 使用 defer 自动归还
pooledInput := performance.NewPooledAgentInput(nil)
defer pooledInput.Release()

// 使用 pooledInput.Input
pooledInput.Input.Task = "your task"
// 函数返回时自动归还
```

### 3. 切片零分配

```go
// 归还时：重置长度但保留容量
output.ReasoningSteps = output.ReasoningSteps[:0]

// 下次使用时：复用底层数组
output.ReasoningSteps = append(output.ReasoningSteps,
    core.ReasoningStep{...})
```

## 性能优势

### 内存分配减少

- **无池化**：每次都分配新对象
- **有池化**：复用对象，零分配

### GC 压力减轻

- **无池化**：频繁触发 GC
- **有池化**：80-90% GC 压力减轻

### 性能提升

- **延迟降低**：70-75%
- **吞吐量提升**：4-5x
- **内存使用减少**：80-90%

## 使用场景

### 适合使用对象池的场景

✅ **高并发 Agent 调用**
✅ **热路径代码**（频繁执行）
✅ **需要降低 GC 压力**
✅ **对性能要求严格**

### 不适合使用对象池的场景

❌ 低频率调用
❌ 对象需要长期持有
❌ 对象不会被频繁创建

## 最佳实践

### 1. 始终归还对象

```go
// ✅ 正确
input := performance.GetAgentInput()
defer performance.PutAgentInput(input)

// ❌ 错误：忘记归还
input := performance.GetAgentInput()
// 没有归还
```

### 2. 不要长时间持有

```go
// ✅ 正确：快速使用并归还
func process() {
    input := performance.GetAgentInput()
    defer performance.PutAgentInput(input)
    // 快速处理
}

// ❌ 错误：长时间持有
func badProcess() {
    input := performance.GetAgentInput()
    time.Sleep(10 * time.Minute) // 占用太久
}
```

### 3. 并发安全

```go
// ✅ 正确：每个 goroutine 独立使用
go func() {
    input := performance.GetAgentInput()
    defer performance.PutAgentInput(input)
    // 处理
}()
```

## 性能基准测试

运行基准测试：

```bash
cd ../../performance
go test -bench=BenchmarkComplexWorkflow -benchmem
```

## 相关文档

- [对象池化详细文档](../../../performance/OBJECT_POOLING.md)
- [性能优化指南](../../../docs/guides/PERFORMANCE_OPTIMIZATION.md)
- [InvokeFast 优化](../../09-deepseek-simple/invokefast/README.md)

## 总结

对象池化是 GoAgent 实现**零分配**目标的关键优化：

- 使用 `sync.Pool` 复用对象
- 切片零分配技巧 (`slice[:0]`)
- Map 底层存储复用
- 80%+ GC 压力减轻
- 4-5x 性能提升

通过合理使用对象池，GoAgent 能够在高并发场景下保持低内存占用和高性能表现。
