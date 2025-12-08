package main

import (
	"context"
	"fmt"
	"time"

	"github.com/kart-io/goagent/interfaces"
	"github.com/kart-io/goagent/tools"
)

func main() {
	fmt.Println("=== ToolExecutor 重试抖动示例 ===")

	// 创建一个会失败的模拟工具
	failingTool := tools.NewBaseTool(
		"failing-service",
		"模拟一个会失败的服务调用",
		`{"type": "object"}`,
		func(ctx context.Context, input *interfaces.ToolInput) (*interfaces.ToolOutput, error) {
			return nil, fmt.Errorf("服务暂时不可用")
		},
	)

	// 示例 1: 使用默认抖动（25%）
	fmt.Println("示例 1: 默认抖动策略 (25%)")
	fmt.Println("----------------------------------------")
	executor1 := tools.NewToolExecutor(
		tools.WithRetryPolicy(&tools.RetryPolicy{
			MaxRetries:   3,
			InitialDelay: time.Second,
			MaxDelay:     10 * time.Second,
			Multiplier:   2.0,
			// Jitter 未设置，将使用默认值 0.25
		}),
	)

	call := &tools.ToolCall{
		Tool:  failingTool,
		Input: &interfaces.ToolInput{Args: map[string]interface{}{}},
		ID:    "call1",
	}

	start := time.Now()
	results, _ := executor1.ExecuteParallel(context.Background(), []*tools.ToolCall{call})
	duration := time.Since(start)

	fmt.Printf("执行结果: %v\n", results[0].Error)
	fmt.Printf("总耗时: %v\n", duration)
	fmt.Printf("预期延迟: 1s + 2s + 4s = 7s (±25%% 抖动)\n\n")

	// 示例 2: 自定义抖动（50%）
	fmt.Println("示例 2: 自定义抖动策略 (50%)")
	fmt.Println("----------------------------------------")
	executor2 := tools.NewToolExecutor(
		tools.WithRetryPolicy(&tools.RetryPolicy{
			MaxRetries:   2,
			InitialDelay: 500 * time.Millisecond,
			MaxDelay:     5 * time.Second,
			Multiplier:   2.0,
			Jitter:       0.5, // 50% 抖动
		}),
	)

	start = time.Now()
	results, _ = executor2.ExecuteParallel(context.Background(), []*tools.ToolCall{call})
	duration = time.Since(start)

	fmt.Printf("执行结果: %v\n", results[0].Error)
	fmt.Printf("总耗时: %v\n", duration)
	fmt.Printf("预期延迟: 500ms + 1s = 1.5s (±50%% 抖动)\n\n")

	// 示例 3: 演示抖动的随机性
	fmt.Println("示例 3: 抖动随机性演示")
	fmt.Println("----------------------------------------")
	executor3 := tools.NewToolExecutor(
		tools.WithRetryPolicy(&tools.RetryPolicy{
			MaxRetries:   1,
			InitialDelay: 100 * time.Millisecond,
			MaxDelay:     1 * time.Second,
			Multiplier:   2.0,
			Jitter:       0.25,
		}),
	)

	fmt.Println("执行 10 次，观察延迟差异:")
	for i := 0; i < 10; i++ {
		start = time.Now()
		_, _ = executor3.ExecuteParallel(context.Background(), []*tools.ToolCall{call})
		duration = time.Since(start)
		fmt.Printf("  第 %2d 次: %v\n", i+1, duration)
	}

	fmt.Println("\n说明:")
	fmt.Println("- 由于 25% 抖动，每次重试的实际延迟在 [75ms, 125ms] 范围内随机")
	fmt.Println("- 这种随机性有助于避免高并发场景下的雷群效应")
	fmt.Println("- 雷群效应: 大量客户端同时重试导致服务压力骤增")
}
