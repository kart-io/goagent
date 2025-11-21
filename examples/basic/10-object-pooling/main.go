// Package main demonstrates the usage of GoAgent's object pooling for GC pressure reduction
//
// This example shows:
// - Using DataPools to reduce GC pressure
// - Zero allocation patterns with slice reuse
// - Performance comparison with and without pooling
// - Best practices for pool usage
package main

import (
	"fmt"
	"runtime"
	"time"

	"github.com/kart-io/goagent/core"
	"github.com/kart-io/goagent/performance"
)

func main() {
	fmt.Println("GoAgent 对象池化示例")
	fmt.Println("====================")
	fmt.Println()

	// 示例 1: 基础使用
	fmt.Println("示例 1: 基础对象池使用")
	fmt.Println("-----------------------")
	basicPoolUsage()
	fmt.Println()

	// 示例 2: 自动生命周期管理
	fmt.Println("示例 2: 自动生命周期管理（使用 defer）")
	fmt.Println("---------------------------------------")
	autoLifecycleManagement()
	fmt.Println()

	// 示例 3: 性能对比
	fmt.Println("示例 3: 性能对比（有池 vs 无池）")
	fmt.Println("-------------------------------")
	performanceComparison()
	fmt.Println()

	// 示例 4: 并发场景
	fmt.Println("示例 4: 高并发场景下的池使用")
	fmt.Println("-----------------------------")
	concurrentPoolUsage()
	fmt.Println()

	// 示例 5: 池统计信息
	fmt.Println("示例 5: 查看池统计信息")
	fmt.Println("---------------------")
	poolStatistics()
}

// basicPoolUsage 演示基础的对象池使用
func basicPoolUsage() {
	// 获取 AgentInput
	input := performance.GetAgentInput()

	// 设置数据
	input.Task = "分析用户行为"
	input.Instruction = "统计过去24小时的用户活跃度"
	input.Context["user_id"] = "12345"
	input.Context["timeframe"] = "24h"
	input.Timestamp = time.Now()

	// 模拟使用
	fmt.Printf("处理任务: %s\n", input.Task)
	fmt.Printf("上下文: %v\n", input.Context)

	// 归还到池中（重要！）
	performance.PutAgentInput(input)
	fmt.Println("✅ 对象已归还到池中")

	// 获取 AgentOutput
	output := performance.GetAgentOutput()

	// 设置结果
	output.Result = "用户活跃度：85%"
	output.Status = "success"
	output.Message = "分析完成"

	// 使用切片零分配技巧
	output.ReasoningSteps = append(output.ReasoningSteps,
		core.ReasoningStep{
			Step:        1,
			Action:      "数据采集",
			Description: "收集用户行为数据",
			Result:      "已采集1000条记录",
			Duration:    100 * time.Millisecond,
			Success:     true,
		},
		core.ReasoningStep{
			Step:        2,
			Action:      "数据分析",
			Description: "计算活跃度指标",
			Result:      "活跃度：85%",
			Duration:    50 * time.Millisecond,
			Success:     true,
		},
	)

	fmt.Printf("结果: %v\n", output.Result)
	fmt.Printf("推理步骤数: %d\n", len(output.ReasoningSteps))

	// 归还到池中
	performance.PutAgentOutput(output)
	fmt.Println("✅ 输出对象已归还到池中")
}

// autoLifecycleManagement 演示使用 defer 自动管理生命周期
func autoLifecycleManagement() {
	// 使用 PooledAgentInput 自动管理
	pooledInput := performance.NewPooledAgentInput(nil)
	defer pooledInput.Release() // 自动归还

	// 使用 pooledInput.Input
	pooledInput.Input.Task = "生成报告"
	pooledInput.Input.Context["report_type"] = "monthly"

	fmt.Printf("任务: %s\n", pooledInput.Input.Task)
	fmt.Println("✅ 将在函数返回时自动归还对象")

	// 同样适用于 Output
	pooledOutput := performance.NewPooledAgentOutput(nil)
	defer pooledOutput.Release() // 自动归还

	pooledOutput.Output.Result = "月度报告已生成"
	pooledOutput.Output.Status = "success"

	fmt.Printf("结果: %v\n", pooledOutput.Output.Result)
	fmt.Println("✅ Output 也将自动归还")
}

// performanceComparison 对比有池和无池的性能
func performanceComparison() {
	const iterations = 10000

	// 无池化：每次都分配新对象
	fmt.Println("运行无池化测试...")
	var m1 runtime.MemStats
	runtime.ReadMemStats(&m1)
	startWithout := time.Now()

	for i := 0; i < iterations; i++ {
		// 创建新对象
		input := &core.AgentInput{
			Task:      "task",
			Context:   make(map[string]interface{}, 8),
			Timestamp: time.Now(),
		}
		input.Context["key"] = "value"

		output := &core.AgentOutput{
			Result:         "result",
			Status:         "success",
			ReasoningSteps: make([]core.ReasoningStep, 0, 10),
			ToolCalls:      make([]core.ToolCall, 0, 5),
			Metadata:       make(map[string]interface{}, 8),
		}
		output.ReasoningSteps = append(output.ReasoningSteps,
			core.ReasoningStep{Step: 1, Action: "test"},
		)

		// 模拟使用
		_ = input.Task
		_ = output.Result

		// 等待 GC 回收
	}

	durationWithout := time.Since(startWithout)
	var m2 runtime.MemStats
	runtime.ReadMemStats(&m2)

	// 有池化：复用对象
	fmt.Println("运行池化测试...")
	var m3 runtime.MemStats
	runtime.ReadMemStats(&m3)
	startWith := time.Now()

	pool := performance.NewDataPools()
	for i := 0; i < iterations; i++ {
		// 从池中获取
		input := pool.GetAgentInput()
		input.Task = "task"
		input.Timestamp = time.Now()
		input.Context["key"] = "value"

		output := pool.GetAgentOutput()
		output.Result = "result"
		output.Status = "success"
		output.ReasoningSteps = append(output.ReasoningSteps,
			core.ReasoningStep{Step: 1, Action: "test"},
		)

		// 模拟使用
		_ = input.Task
		_ = output.Result

		// 归还到池中
		pool.PutAgentInput(input)
		pool.PutAgentOutput(output)
	}

	durationWith := time.Since(startWith)
	var m4 runtime.MemStats
	runtime.ReadMemStats(&m4)

	// 显示结果
	fmt.Printf("\n性能对比结果（%d 次迭代）:\n", iterations)
	fmt.Println("─────────────────────────────────────────")
	fmt.Printf("无池化耗时: %v\n", durationWithout)
	fmt.Printf("有池化耗时: %v\n", durationWith)
	fmt.Printf("性能提升: %.2f%%\n\n", (1-float64(durationWith)/float64(durationWithout))*100)

	fmt.Printf("无池化内存分配: %d MB\n", (m2.TotalAlloc-m1.TotalAlloc)/1024/1024)
	fmt.Printf("有池化内存分配: %d MB\n", (m4.TotalAlloc-m3.TotalAlloc)/1024/1024)
	fmt.Printf("内存减少: %.2f%%\n", (1-float64(m4.TotalAlloc-m3.TotalAlloc)/float64(m2.TotalAlloc-m1.TotalAlloc))*100)
}

// concurrentPoolUsage 演示并发场景下的池使用
func concurrentPoolUsage() {
	const goroutines = 100
	const iterationsPerGoroutine = 100

	pool := performance.NewDataPools()
	done := make(chan bool, goroutines)

	start := time.Now()

	// 启动多个 goroutine
	for i := 0; i < goroutines; i++ {
		go func(id int) {
			defer func() { done <- true }()

			for j := 0; j < iterationsPerGoroutine; j++ {
				// 每个 goroutine 独立使用池
				input := pool.GetAgentInput()
				input.Task = fmt.Sprintf("task-%d-%d", id, j)

				output := pool.GetAgentOutput()
				output.Result = fmt.Sprintf("result-%d-%d", id, j)

				// 模拟处理
				time.Sleep(time.Microsecond)

				// 归还
				pool.PutAgentInput(input)
				pool.PutAgentOutput(output)
			}
		}(i)
	}

	// 等待所有 goroutine 完成
	for i := 0; i < goroutines; i++ {
		<-done
	}

	duration := time.Since(start)

	fmt.Printf("并发测试完成:\n")
	fmt.Printf("  Goroutines: %d\n", goroutines)
	fmt.Printf("  每个 Goroutine 迭代: %d\n", iterationsPerGoroutine)
	fmt.Printf("  总操作数: %d\n", goroutines*iterationsPerGoroutine)
	fmt.Printf("  总耗时: %v\n", duration)
	fmt.Printf("  平均每操作: %v\n", duration/time.Duration(goroutines*iterationsPerGoroutine))
	fmt.Println("✅ 池在并发场景下表现良好")
}

// poolStatistics 显示池统计信息
func poolStatistics() {
	pool := performance.NewDataPools()

	// 模拟一些操作
	for i := 0; i < 1000; i++ {
		input := pool.GetAgentInput()
		input.Task = "test"
		pool.PutAgentInput(input)

		output := pool.GetAgentOutput()
		output.Result = "result"
		pool.PutAgentOutput(output)
	}

	// 获取统计信息
	stats := pool.GetStats()

	fmt.Println("池统计信息:")
	fmt.Println("─────────────────────────────────────")
	fmt.Printf("AgentInput:\n")
	fmt.Printf("  Get 次数: %d\n", stats.InputGetCount)
	fmt.Printf("  Put 次数: %d\n", stats.InputPutCount)
	fmt.Printf("  复用率: %.2f%%\n\n", float64(stats.InputPutCount)/float64(stats.InputGetCount)*100)

	fmt.Printf("AgentOutput:\n")
	fmt.Printf("  Get 次数: %d\n", stats.OutputGetCount)
	fmt.Printf("  Put 次数: %d\n", stats.OutputPutCount)
	fmt.Printf("  复用率: %.2f%%\n\n", float64(stats.OutputPutCount)/float64(stats.OutputGetCount)*100)

	fmt.Printf("总体:\n")
	fmt.Printf("  池命中率: %.2f%%\n", stats.PoolHitRate)

	if stats.PoolHitRate >= 90 {
		fmt.Println("  ✅ 池使用效率优秀！")
	} else if stats.PoolHitRate >= 70 {
		fmt.Println("  ⚠️  池使用效率良好，但有改进空间")
	} else {
		fmt.Println("  ❌ 池使用效率较低，请检查是否正确归还对象")
	}
}
