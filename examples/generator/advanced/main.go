package main

import (
	"context"
	"fmt"
	"log"

	"github.com/kart-io/goagent/core"
)

// AdvancedAgent 高级示例 Agent
type AdvancedAgent struct {
	*core.BaseAgent
	steps int
}

// NewAdvancedAgent 创建高级 Agent
func NewAdvancedAgent(steps int) *AdvancedAgent {
	return &AdvancedAgent{
		BaseAgent: core.NewBaseAgent("advanced", "Advanced Generator Agent", []string{"advanced"}),
		steps:     steps,
	}
}

// Invoke 实现基本执行
func (a *AdvancedAgent) Invoke(ctx context.Context, input *core.AgentInput) (*core.AgentOutput, error) {
	return &core.AgentOutput{
		Status:  "success",
		Message: fmt.Sprintf("Processed: %s", input.Task),
	}, nil
}

// Stream 实现流式执行
func (a *AdvancedAgent) Stream(ctx context.Context, input *core.AgentInput) (<-chan core.StreamChunk[*core.AgentOutput], error) {
	ch := make(chan core.StreamChunk[*core.AgentOutput], a.steps)

	go func() {
		defer close(ch)
		for i := 0; i < a.steps; i++ {
			if ctx.Err() != nil {
				ch <- core.StreamChunk[*core.AgentOutput]{Error: ctx.Err()}
				return
			}

			var status, message string
			switch i % 3 {
			case 0:
				status = "success"
				message = fmt.Sprintf("Step %d: 成功处理数据", i+1)
			case 1:
				status = "processing"
				message = fmt.Sprintf("Step %d: 正在处理...", i+1)
			default:
				status = "pending"
				message = fmt.Sprintf("Step %d: 等待中...", i+1)
			}

			ch <- core.StreamChunk[*core.AgentOutput]{
				Data: &core.AgentOutput{
					Status:  status,
					Message: message,
					Result:  i + 1,
				},
			}
		}
	}()

	return ch, nil
}

// RunGenerator 实现带状态的流式生成器
func (a *AdvancedAgent) RunGenerator(ctx context.Context, input *core.AgentInput) core.Generator[*core.AgentOutput] {
	return func(yield func(*core.AgentOutput, error) bool) {
		for i := 0; i < a.steps; i++ {
			if ctx.Err() != nil {
				yield(nil, ctx.Err())
				return
			}

			// 模拟不同状态的输出
			var status, message string
			switch i % 3 {
			case 0:
				status = "success"
				message = fmt.Sprintf("Step %d: 成功处理数据", i+1)
			case 1:
				status = "processing"
				message = fmt.Sprintf("Step %d: 正在处理...", i+1)
			default:
				status = "pending"
				message = fmt.Sprintf("Step %d: 等待中...", i+1)
			}

			output := &core.AgentOutput{
				Status:  status,
				Message: message,
				Result:  i + 1,
			}

			if !yield(output, nil) {
				return
			}
		}
	}
}

func main() {
	fmt.Println("========================================")
	fmt.Println("Generator 高级功能示例")
	fmt.Println("========================================")
	fmt.Println()

	agent := NewAdvancedAgent(10)
	input := &core.AgentInput{
		Task: "处理数据流",
	}
	ctx := context.Background()

	// ========================================
	// 示例 1：Take - 仅取前 3 个输出
	// ========================================
	fmt.Println("=== 示例 1：Take - 仅取前 3 个输出 ===")
	gen := agent.RunGenerator(ctx, input)
	first3 := core.Take(gen, 3)

	for output, err := range first3 {
		if err != nil {
			log.Fatal(err)
		}
		fmt.Printf("  %s\n", output.Message)
	}
	fmt.Println()

	// ========================================
	// 示例 2：Filter - 过滤成功的输出
	// ========================================
	fmt.Println("=== 示例 2：Filter - 仅显示成功状态 ===")
	gen2 := agent.RunGenerator(ctx, input)
	filtered := core.Filter(gen2, func(output *core.AgentOutput) bool {
		return output.Status == "success"
	})

	for output, err := range filtered {
		if err != nil {
			log.Fatal(err)
		}
		fmt.Printf("  %s (Status: %s)\n", output.Message, output.Status)
	}
	fmt.Println()

	// ========================================
	// 示例 3：Map - 转换输出格式
	// ========================================
	fmt.Println("=== 示例 3：Map - 转换输出为简单字符串 ===")
	gen3 := agent.RunGenerator(ctx, input)
	mapped := core.Map(gen3, func(output *core.AgentOutput) string {
		return fmt.Sprintf("[%s] %s", output.Status, output.Message)
	})

	for text, err := range mapped {
		if err != nil {
			log.Fatal(err)
		}
		fmt.Printf("  %s\n", text)
	}
	fmt.Println()

	// ========================================
	// 示例 4：链式操作 - 组合多个操作
	// ========================================
	fmt.Println("=== 示例 4：链式操作 - Filter + Map + Take ===")
	gen4 := agent.RunGenerator(ctx, input)

	// 链式操作：过滤 -> 映射 -> 取前5个
	result := core.Take(
		core.Map(
			core.Filter(gen4, func(output *core.AgentOutput) bool {
				return output.Status == "success" || output.Status == "processing"
			}),
			func(output *core.AgentOutput) string {
				return fmt.Sprintf("✓ %s", output.Message)
			},
		),
		5,
	)

	for text, err := range result {
		if err != nil {
			log.Fatal(err)
		}
		fmt.Printf("  %s\n", text)
	}
	fmt.Println()

	// ========================================
	// 示例 5：早期终止 - 条件控制
	// ========================================
	fmt.Println("=== 示例 5：早期终止 - 遇到特定条件停止 ===")
	gen5 := agent.RunGenerator(ctx, input)

	count := 0
	for output, err := range gen5 {
		if err != nil {
			fmt.Println("  遇到错误，停止执行:", err)
			break
		}

		fmt.Printf("  %s\n", output.Message)

		// 自定义终止条件：处理 5 步或遇到 pending 状态
		count++
		if count >= 5 || output.Status == "pending" {
			fmt.Printf("  达到终止条件（已处理 %d 步）\n", count)
			break
		}
	}
	fmt.Println()

	// ========================================
	// 示例 6：复杂数据流处理
	// ========================================
	fmt.Println("=== 示例 6：复杂数据流处理 ===")
	gen6 := agent.RunGenerator(ctx, input)

	// 分类统计
	stats := map[string]int{
		"success":    0,
		"processing": 0,
		"pending":    0,
	}

	for output, err := range gen6 {
		if err != nil {
			log.Fatal(err)
		}

		stats[output.Status]++
		fmt.Printf("  处理: %s (当前状态统计: success=%d, processing=%d, pending=%d)\n",
			output.Message, stats["success"], stats["processing"], stats["pending"])
	}

	fmt.Printf("\n  最终统计:\n")
	fmt.Printf("    成功: %d\n", stats["success"])
	fmt.Printf("    处理中: %d\n", stats["processing"])
	fmt.Printf("    等待中: %d\n", stats["pending"])
	fmt.Println()

	// ========================================
	// 示例 7：与 Channel 互转
	// ========================================
	fmt.Println("=== 示例 7：Generator 和 Channel 互转 ===")

	// Generator -> Channel
	gen7 := agent.RunGenerator(ctx, input)
	ch := core.ToChannel(ctx, gen7, 5)

	fmt.Println("  转换为 Channel 后读取前 3 个:")
	count = 0
	for event := range ch {
		if event.Error != nil {
			log.Fatal(event.Error)
		}
		fmt.Printf("    %s\n", event.Data.Message)
		count++
		if count >= 3 {
			break
		}
	}
	fmt.Println()

	fmt.Println("========================================")
	fmt.Println("高级示例完成")
	fmt.Println("========================================")
}
