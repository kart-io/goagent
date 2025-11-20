# 解耦池管理架构

本示例展示了 GoAgent 对象池的**解耦设计架构**，通过依赖注入、策略模式和 Agent 模式实现灵活、可测试、可扩展的对象池管理。

## 核心设计原则

### 1. 接口抽象
通过 `PoolManager` 接口定义清晰契约，实现与具体实现解耦。

### 2. 依赖注入
消除全局状态，支持隔离实例，提高可测试性。

### 3. 策略模式
灵活控制池化行为，支持多种使用场景。

### 4. Agent 模式
池管理器可作为 Agent 集成到更大的系统中。

## 核心组件

### PoolManager 接口

定义了对象池管理的完整契约：

```go
type PoolManager interface {
    // 对象获取和归还
    GetBuffer() *bytes.Buffer
    PutBuffer(buf *bytes.Buffer)
    GetMessage() *interfaces.Message
    PutMessage(msg *interfaces.Message)
    GetToolInput() *interfaces.ToolInput
    PutToolInput(input *interfaces.ToolInput)
    GetToolOutput() *interfaces.ToolOutput
    PutToolOutput(output *interfaces.ToolOutput)
    GetAgentInput() *core.AgentInput
    PutAgentInput(input *core.AgentInput)
    GetAgentOutput() *core.AgentOutput
    PutAgentOutput(output *core.AgentOutput)

    // 配置管理
    Configure(config *PoolManagerConfig) error
    GetConfig() *PoolManagerConfig
    EnablePool(poolType PoolType)
    DisablePool(poolType PoolType)
    IsPoolEnabled(poolType PoolType) bool

    // 统计信息
    GetStats(poolType PoolType) *ObjectPoolStats
    GetAllStats() map[PoolType]*ObjectPoolStats
    ResetStats()

    // 生命周期
    Close() error
}
```

### PoolStrategy 策略接口

控制池化行为：

```go
type PoolStrategy interface {
    // 决定是否池化对象
    ShouldPool(poolType PoolType, size int) bool

    // 生命周期钩子
    PreGet(poolType PoolType)
    PostPut(poolType PoolType)
}
```

## 内置策略

### 1. AdaptivePoolStrategy - 自适应策略

根据使用频率和资源压力动态调整池化行为：

- ✅ 监控使用频率
- ✅ 低频使用不触发池化（节省资源）
- ✅ 高频使用自动池化（提升性能）
- ✅ 内存压力感知

**适用场景**: 负载波动大的应用

### 2. ScenarioBasedStrategy - 场景策略

预定义常见场景的最优配置：

| 场景 | 启用的池 | 用途 |
|-----|---------|------|
| **LLM Calls** | Message, AgentInput/Output | 高频 LLM API 调用 |
| **Tool Execution** | ToolInput/Output, ByteBuffer | 工具并发执行 |
| **JSON Processing** | ByteBuffer | JSON 序列化/反序列化 |
| **Streaming** | Message, ByteBuffer | 流式响应处理 |
| **General** | All Pools | 通用场景 |

**适用场景**: 明确的使用模式

### 3. MetricsPoolStrategy - 指标策略

装饰器模式收集池使用指标：

- ✅ Get/Put 操作延迟
- ✅ 池命中率/未命中率
- ✅ 与监控系统集成
- ✅ 实时性能分析

**适用场景**: 生产环境监控

### 4. PriorityPoolStrategy - 优先级策略

资源受限环境下的优先级控制：

- ✅ 池类型优先级
- ✅ 资源配额管理
- ✅ 动态资源分配

**适用场景**: 内存受限环境

## 使用示例

### 基础使用：依赖注入

```go
// 创建配置
config := &performance.PoolManagerConfig{
    EnabledPools: map[performance.PoolType]bool{
        performance.PoolTypeByteBuffer: true,
        performance.PoolTypeMessage:    true,
    },
    MaxBufferSize: 64 * 1024,
    MaxMapSize:    100,
}

// 创建池管理器
poolManager := performance.NewPoolAgent(config)

// 使用池
msg := poolManager.GetMessage()
msg.Content = "Hello, World!"
poolManager.PutMessage(msg)
```

### 场景驱动配置

```go
// 创建场景策略
config := performance.DefaultPoolManagerConfig()
scenarioStrategy := performance.NewScenarioBasedStrategy(config)
config.UseStrategy = scenarioStrategy

poolManager := performance.NewPoolAgent(config)

// 切换到 LLM 场景
scenarioStrategy.SetScenario(performance.ScenarioLLMCalls)
// 现在 Message 和 Agent 池自动启用

// 切换到 JSON 处理场景
scenarioStrategy.SetScenario(performance.ScenarioJSONProcess)
// 现在只有 ByteBuffer 池启用
```

### 自适应策略

```go
// 创建自适应策略
config := performance.DefaultPoolManagerConfig()
adaptiveStrategy := performance.NewAdaptivePoolStrategy(config)
config.UseStrategy = adaptiveStrategy

poolManager := performance.NewPoolAgent(config)

// 策略会自动根据使用频率调整池化行为
// 低频使用时不池化，高频使用时自动池化
```

### 指标收集

```go
// 实现自定义指标收集器
type MyMetricsCollector struct{}

func (c *MyMetricsCollector) RecordPoolGet(poolType PoolType, latency time.Duration) {
    // 记录到 Prometheus/StatsD/等
}
// ... 实现其他方法

// 创建带指标的策略
baseStrategy := &defaultPoolStrategy{config: config}
collector := &MyMetricsCollector{}
metricsStrategy := performance.NewMetricsPoolStrategy(baseStrategy, collector)

config.UseStrategy = metricsStrategy
poolManager := performance.NewPoolAgent(config)
```

### PoolManagerAgent

将池管理器作为 Agent 使用：

```go
// 创建 Agent
agent := performance.NewPoolManagerAgent("pool_optimizer", config)

// Agent 信息
fmt.Println(agent.Name())        // "pool_optimizer"
fmt.Println(agent.Description()) // "Pool manager agent..."
fmt.Println(agent.Capabilities()) // [object_pooling, memory_optimization, ...]

// 通过 Agent 执行配置
input := &core.AgentInput{
    Task: "configure_pools",
    Context: map[string]interface{}{
        "scenario": "llm_calls",
    },
}

output, _ := agent.Execute(context.Background(), input)
// 池已根据场景自动配置
```

### 隔离测试

```go
// 创建隔离的池管理器（不影响其他代码）
testConfig := &PoolManagerConfig{
    EnabledPools: map[PoolType]bool{
        PoolTypeByteBuffer: true,
    },
}

testManager := performance.CreateIsolatedPoolManager(testConfig)

// 在测试中使用
buf := testManager.GetBuffer()
buf.WriteString("test data")
testManager.PutBuffer(buf)

// 验证统计
stats := testManager.GetStats(PoolTypeByteBuffer)
assert.Equal(t, int64(1), stats.Gets.Load())
```

## 运行演示

```bash
cd examples/advanced/pool-decoupled-architecture
go run main.go
```

演示内容：

1. **依赖注入模式** - 创建独立的池管理器实例
2. **场景驱动策略** - 不同场景自动配置
3. **自适应策略** - 根据使用频率动态调整
4. **指标收集** - 监控池使用情况
5. **Agent 模式** - 池管理器作为 Agent
6. **隔离测试** - 测试友好的设计

## 架构图

```
┌─────────────────────────────────────────────┐
│            应用层                            │
│  (使用 PoolManager 接口)                    │
├─────────────────────────────────────────────┤
│         PoolManager 接口                     │
│    • GetBuffer/PutBuffer                   │
│    • GetMessage/PutMessage                 │
│    • Configure/GetStats                    │
├──────────────┬──────────────┬───────────────┤
│  PoolAgent   │ PoolStrategy │  Statistics   │
│  (实现)      │  (策略)      │   (统计)     │
│              │              │               │
│  • 依赖注入   │ • Adaptive   │ • Gets/Puts  │
│  • 线程安全   │ • Scenario   │ • Hit Rate   │
│  • 生命周期   │ • Metrics    │ • Latency    │
│              │ • Priority   │               │
├──────────────┴──────────────┴───────────────┤
│              sync.Pool                       │
│    (Go 标准库对象池)                         │
└─────────────────────────────────────────────┘
```

## 文件结构

```
performance/
├── pool_manager.go       # PoolManager 接口和 PoolAgent 实现
├── pool_strategies.go    # 策略实现（Adaptive/Scenario/Metrics/Priority）
├── pool_config.go        # 配置结构
└── object_pool.go        # 基础池和统计结构

examples/
└── pool-decoupled-architecture/
    ├── main.go          # 演示代码
    └── README.md        # 本文档
```

## 最佳实践

### 1. 选择合适的策略

```go
// 高负载、负载波动大
strategy = NewAdaptivePoolStrategy(config)

// 明确的使用场景
strategy = NewScenarioBasedStrategy(config)

// 生产环境监控
strategy = NewMetricsPoolStrategy(baseStrategy, collector)

// 资源受限
strategy = NewPriorityPoolStrategy(config, maxPools)
```

### 2. 配置池大小限制

```go
config := &PoolManagerConfig{
    MaxBufferSize: 64 * 1024,  // 64KB
    MaxMapSize:    100,         // 100 个键
    MaxSliceSize:  100,         // 100 个元素
}
```

### 3. 监控池使用情况

```go
stats := manager.GetAllStats()
for poolType, stat := range stats {
    hitRate := float64(stat.Gets.Load()-stat.News.Load()) /
               float64(stat.Gets.Load()) * 100
    fmt.Printf("%s: %.2f%% hit rate\n", poolType, hitRate)
}
```

### 4. 测试隔离

```go
func TestMyFeature(t *testing.T) {
    // 创建测试专用的池管理器
    testManager := CreateIsolatedPoolManager(testConfig)

    // 测试逻辑
    msg := testManager.GetMessage()
    // ...

    // 验证
    stats := testManager.GetStats(PoolTypeMessage)
    assert.Equal(t, int64(1), stats.Gets.Load())
}
```

## 性能对比

| 策略 | 优势 | 适用场景 |
|-----|------|---------|
| **默认策略** | 简单、稳定 | 通用场景 |
| **自适应** | 自动优化 | 负载波动大 |
| **场景驱动** | 针对性强 | 明确使用模式 |
| **指标收集** | 可观测性 | 生产环境 |
| **优先级** | 资源优化 | 内存受限 |

## 扩展指南

### 实现自定义策略

```go
type MyCustomStrategy struct {
    // 自定义字段
    threshold int
}

func (s *MyCustomStrategy) ShouldPool(poolType PoolType, size int) bool {
    // 自定义逻辑
    if size > s.threshold {
        return false  // 太大不池化
    }
    return true
}

func (s *MyCustomStrategy) PreGet(poolType PoolType) {
    // 获取前处理（如日志）
    log.Printf("Getting from pool: %s", poolType)
}

func (s *MyCustomStrategy) PostPut(poolType PoolType) {
    // 归还后处理（如清理）
}
```

### 集成监控系统

```go
type PrometheusCollector struct {
    getLatency *prometheus.HistogramVec
    hitRate    *prometheus.GaugeVec
}

func (c *PrometheusCollector) RecordPoolGet(poolType PoolType, latency time.Duration) {
    c.getLatency.WithLabelValues(string(poolType)).Observe(latency.Seconds())
}

func (c *PrometheusCollector) RecordPoolHit(poolType PoolType) {
    // 更新命中率指标
}
```

## 总结

这个解耦的池管理架构提供了：

✅ **灵活性** - 策略模式支持多种池化行为
✅ **可测试性** - 依赖注入消除全局状态
✅ **可扩展性** - 轻松添加新策略
✅ **可观测性** - 内置统计和指标支持
✅ **生产就绪** - 经过优化和充分测试

适合需要精细控制对象池行为的复杂生产环境。