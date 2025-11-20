package main

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/kart-io/goagent/core"
	"github.com/kart-io/goagent/performance"
)

// CustomMetricsCollector 自定义指标收集器
type CustomMetricsCollector struct {
	mu      sync.Mutex
	metrics map[performance.PoolType][]time.Duration
}

func NewCustomMetricsCollector() *CustomMetricsCollector {
	return &CustomMetricsCollector{
		metrics: make(map[performance.PoolType][]time.Duration),
	}
}

func (c *CustomMetricsCollector) RecordPoolGet(poolType performance.PoolType, latency time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.metrics[poolType] = append(c.metrics[poolType], latency)
}

func (c *CustomMetricsCollector) RecordPoolPut(poolType performance.PoolType, latency time.Duration) {
	// 实现记录逻辑
}

func (c *CustomMetricsCollector) RecordPoolHit(poolType performance.PoolType) {
	// 实现记录逻辑
}

func (c *CustomMetricsCollector) RecordPoolMiss(poolType performance.PoolType) {
	// 实现记录逻辑
}

func (c *CustomMetricsCollector) PrintMetrics() {
	c.mu.Lock()
	defer c.mu.Unlock()
	fmt.Println("\n=== Custom Metrics ===")
	for poolType, latencies := range c.metrics {
		if len(latencies) > 0 {
			fmt.Printf("%s: %d operations recorded\n", poolType, len(latencies))
		}
	}
}

// 演示1: 使用依赖注入的池管理器
func demonstrateDependencyInjection() {
	fmt.Println("\n=== 演示 1: 依赖注入模式 ===")

	// 创建独立的池管理器（不使用全局状态）
	config := &performance.PoolManagerConfig{
		EnabledPools: map[performance.PoolType]bool{
			performance.PoolTypeByteBuffer:  true,
			performance.PoolTypeMessage:     true,
			performance.PoolTypeToolInput:   false,
			performance.PoolTypeToolOutput:  false,
			performance.PoolTypeAgentInput:  true,
			performance.PoolTypeAgentOutput: true,
		},
		MaxBufferSize: 64 * 1024,
		MaxMapSize:    100,
		MaxSliceSize:  100,
	}

	poolManager := performance.NewPoolAgent(config)

	// 使用池管理器进行操作
	fmt.Println("使用依赖注入的池管理器:")

	// 获取和使用 Message
	msg := poolManager.GetMessage()
	msg.Role = "user"
	msg.Content = "Hello from injected pool manager"
	fmt.Printf("Message created: %s\n", msg.Content)
	poolManager.PutMessage(msg)

	// 获取和使用 ByteBuffer
	buf := poolManager.GetBuffer()
	buf.WriteString("Data from injected pool")
	fmt.Printf("Buffer content: %s\n", buf.String())
	poolManager.PutBuffer(buf)

	// 打印统计
	stats := poolManager.GetAllStats()
	fmt.Println("\n池统计信息:")
	for poolType, stat := range stats {
		if stat.Gets.Load() > 0 {
			fmt.Printf("  %s: Gets=%d, Puts=%d, News=%d\n",
				poolType, stat.Gets.Load(), stat.Puts.Load(), stat.News.Load())
		}
	}
}

// 演示2: 场景驱动的策略模式
func demonstrateScenarioBasedStrategy() {
	fmt.Println("\n=== 演示 2: 场景驱动策略 ===")

	// 创建场景策略
	config := performance.DefaultPoolManagerConfig()
	scenarioStrategy := performance.NewScenarioBasedStrategy(config)

	// 使用场景策略创建池管理器
	config.UseStrategy = scenarioStrategy
	poolManager := performance.NewPoolAgent(config)

	// 场景1: LLM 调用场景
	fmt.Println("\n切换到 LLM 调用场景:")
	scenarioStrategy.SetScenario(performance.ScenarioLLMCalls)

	// 在 LLM 场景下，Message 和 Agent 池应该启用
	for i := 0; i < 100; i++ {
		msg := poolManager.GetMessage()
		msg.Role = "assistant"
		msg.Content = fmt.Sprintf("LLM response %d", i)

		input := poolManager.GetAgentInput()
		input.Task = "Process LLM response"

		poolManager.PutMessage(msg)
		poolManager.PutAgentInput(input)
	}

	stats1 := poolManager.GetAllStats()

	// 场景2: JSON 处理场景
	fmt.Println("\n切换到 JSON 处理场景:")
	scenarioStrategy.SetScenario(performance.ScenarioJSONProcess)
	poolManager.ResetStats()

	// 在 JSON 场景下，只有 ByteBuffer 池启用
	for i := 0; i < 100; i++ {
		buf := poolManager.GetBuffer()
		buf.WriteString(`{"id":`)
		fmt.Fprintf(buf, "%d", i)
		buf.WriteString(`}`)
		_ = buf.String()
		poolManager.PutBuffer(buf)
	}

	stats2 := poolManager.GetAllStats()

	// 打印对比
	fmt.Println("\n场景对比:")
	fmt.Println("LLM 场景池使用:")
	printPoolUsage(stats1)
	fmt.Println("\nJSON 场景池使用:")
	printPoolUsage(stats2)
}

// 演示3: 自适应策略
func demonstrateAdaptiveStrategy() {
	fmt.Println("\n=== 演示 3: 自适应策略 ===")

	config := performance.DefaultPoolManagerConfig()
	adaptiveStrategy := performance.NewAdaptivePoolStrategy(config)
	config.UseStrategy = adaptiveStrategy

	poolManager := performance.NewPoolAgent(config)

	fmt.Println("模拟不同使用频率的场景:")

	// 低频使用（不会触发池化）
	fmt.Println("\n阶段1: 低频使用")
	for i := 0; i < 50; i++ { // 少于 minUsageForPool (100)
		msg := poolManager.GetMessage()
		msg.Content = fmt.Sprintf("Low frequency message %d", i)
		poolManager.PutMessage(msg)
	}

	stats1 := poolManager.GetStats(performance.PoolTypeMessage)
	fmt.Printf("低频使用统计 - Gets: %d, Puts: %d, News: %d\n",
		stats1.Gets.Load(), stats1.Puts.Load(), stats1.News.Load())

	// 高频使用（会触发池化）
	fmt.Println("\n阶段2: 高频使用")
	for i := 0; i < 100; i++ { // 达到 minUsageForPool
		msg := poolManager.GetMessage()
		msg.Content = fmt.Sprintf("High frequency message %d", i)
		poolManager.PutMessage(msg)
	}

	stats2 := poolManager.GetStats(performance.PoolTypeMessage)
	fmt.Printf("高频使用统计 - Gets: %d, Puts: %d, News: %d\n",
		stats2.Gets.Load(), stats2.Puts.Load(), stats2.News.Load())
	fmt.Printf("池命中率: %.2f%%\n",
		float64(stats2.Gets.Load()-stats2.News.Load())/float64(stats2.Gets.Load())*100)
}

// 演示4: 带指标收集的策略
func demonstrateMetricsStrategy() {
	fmt.Println("\n=== 演示 4: 指标收集策略 ===")

	// 创建基础策略和指标收集器
	baseConfig := performance.DefaultPoolManagerConfig()
	collector := NewCustomMetricsCollector()

	// 创建带指标的策略（使用内置的 defaultPoolStrategy）
	// 注意：这里我们使用一个简单的策略作为基础
	baseStrategy := &simpleStrategy{config: baseConfig}
	metricsStrategy := performance.NewMetricsPoolStrategy(baseStrategy, collector)
	baseConfig.UseStrategy = metricsStrategy

	poolManager := performance.NewPoolAgent(baseConfig)

	// 执行一些操作
	fmt.Println("执行池操作并收集指标...")
	for i := 0; i < 200; i++ {
		// ByteBuffer 操作
		buf := poolManager.GetBuffer()
		fmt.Fprintf(buf, "Data %d", i)
		poolManager.PutBuffer(buf)

		// Message 操作
		msg := poolManager.GetMessage()
		msg.Content = fmt.Sprintf("Message %d", i)
		poolManager.PutMessage(msg)
	}

	// 打印自定义指标
	collector.PrintMetrics()

	// 打印池统计
	allStats := poolManager.GetAllStats()
	fmt.Println("\n池统计信息:")
	for poolType, stats := range allStats {
		if stats.Gets.Load() > 0 {
			fmt.Printf("  %s: Gets=%d, Puts=%d, Hit Rate=%.2f%%\n",
				poolType, stats.Gets.Load(), stats.Puts.Load(),
				float64(stats.Gets.Load()-stats.News.Load())/float64(stats.Gets.Load())*100)
		}
	}
}

// 演示5: 使用 PoolManagerAgent
func demonstratePoolManagerAgent() {
	fmt.Println("\n=== 演示 5: PoolManagerAgent 模式 ===")

	// 创建 PoolManagerAgent
	agent := performance.NewPoolManagerAgent("pool_optimizer", performance.DefaultPoolManagerConfig())

	fmt.Println("Agent 信息:")
	fmt.Printf("  名称: %s\n", agent.Name())
	fmt.Printf("  描述: %s\n", agent.Description())
	fmt.Printf("  能力: %v\n", agent.Capabilities())

	// 执行不同场景的配置
	scenarios := []performance.ScenarioType{
		performance.ScenarioLLMCalls,
		performance.ScenarioToolExec,
		performance.ScenarioJSONProcess,
		performance.ScenarioStreaming,
	}

	for _, scenario := range scenarios {
		fmt.Printf("\n配置场景: %s\n", scenario)

		// 创建输入
		input := &core.AgentInput{
			Task:    "configure_pools",
			Context: map[string]interface{}{"scenario": string(scenario)},
		}

		// 执行 Agent
		output, err := agent.Execute(context.Background(), input)
		if err != nil {
			fmt.Printf("执行错误: %v\n", err)
			continue
		}

		fmt.Printf("状态: %s\n", output.Status)
		fmt.Printf("消息: %s\n", output.Message)

		// 显示配置
		if config, ok := output.Metadata["configuration"].(*performance.PoolManagerConfig); ok {
			fmt.Println("池配置:")
			for poolType, enabled := range config.EnabledPools {
				if enabled {
					fmt.Printf("  ✓ %s\n", poolType)
				}
			}
		}
	}
}

// 演示6: 隔离的池管理器（用于测试）
func demonstrateIsolatedPoolManager() {
	fmt.Println("\n=== 演示 6: 隔离的池管理器 ===")

	// 创建隔离的池管理器（不影响全局状态）
	config1 := &performance.PoolManagerConfig{
		EnabledPools: map[performance.PoolType]bool{
			performance.PoolTypeByteBuffer: true,
			performance.PoolTypeMessage:    false,
		},
		MaxBufferSize: 32 * 1024,
	}

	config2 := &performance.PoolManagerConfig{
		EnabledPools: map[performance.PoolType]bool{
			performance.PoolTypeByteBuffer: false,
			performance.PoolTypeMessage:    true,
		},
	}

	manager1 := performance.CreateIsolatedPoolManager(config1)
	manager2 := performance.CreateIsolatedPoolManager(config2)

	fmt.Println("管理器1（只启用 ByteBuffer）:")
	testManager(manager1)

	fmt.Println("\n管理器2（只启用 Message）:")
	testManager(manager2)
}

// 辅助函数
func printPoolUsage(stats map[performance.PoolType]*performance.ObjectPoolStats) {
	for poolType, stat := range stats {
		if stat.Gets.Load() > 0 {
			fmt.Printf("  %s: Gets=%d, Puts=%d, Pool Hit Rate=%.2f%%\n",
				poolType, stat.Gets.Load(), stat.Puts.Load(),
				float64(stat.Gets.Load()-stat.News.Load())/float64(stat.Gets.Load())*100)
		}
	}
}

func testManager(manager performance.PoolManager) {
	// 测试 ByteBuffer
	buf := manager.GetBuffer()
	buf.WriteString("Test data")
	manager.PutBuffer(buf)

	// 测试 Message
	msg := manager.GetMessage()
	msg.Content = "Test message"
	manager.PutMessage(msg)

	// 打印统计
	stats := manager.GetAllStats()
	for poolType, stat := range stats {
		if stat.Gets.Load() > 0 {
			fmt.Printf("  %s: Gets=%d, News=%d (池%s)\n",
				poolType, stat.Gets.Load(), stat.News.Load(),
				ternary(stat.News.Load() == 0, "已启用", "已禁用"))
		}
	}
}

func ternary(condition bool, ifTrue, ifFalse string) string {
	if condition {
		return ifTrue
	}
	return ifFalse
}

// simpleStrategy 简单策略实现
type simpleStrategy struct {
	config *performance.PoolManagerConfig
}

func (s *simpleStrategy) ShouldPool(poolType performance.PoolType, size int) bool {
	if !s.config.EnabledPools[poolType] {
		return false
	}

	switch poolType {
	case performance.PoolTypeByteBuffer:
		return size <= s.config.MaxBufferSize
	default:
		return size <= s.config.MaxMapSize
	}
}

func (s *simpleStrategy) PreGet(poolType performance.PoolType)  {}
func (s *simpleStrategy) PostPut(poolType performance.PoolType) {}

func main() {
	fmt.Println("GoAgent 解耦池管理架构演示")
	fmt.Println("=====================================")

	// 运行各种演示
	demonstrateDependencyInjection()
	demonstrateScenarioBasedStrategy()
	demonstrateAdaptiveStrategy()
	demonstrateMetricsStrategy()
	demonstratePoolManagerAgent()
	demonstrateIsolatedPoolManager()

	fmt.Println("\n✅ 所有演示完成！")
	fmt.Println("\n架构优势:")
	fmt.Println("1. ✓ 解耦设计 - 接口抽象和依赖注入")
	fmt.Println("2. ✓ 策略模式 - 灵活的池行为控制")
	fmt.Println("3. ✓ Agent 模式 - 池管理器作为 Agent")
	fmt.Println("4. ✓ 可测试性 - 隔离的池管理器")
	fmt.Println("5. ✓ 可扩展性 - 插件式策略系统")
}
