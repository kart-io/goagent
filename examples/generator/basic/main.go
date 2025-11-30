package main

import (
	"context"
	"fmt"
	"log"

	"github.com/kart-io/goagent/core"
)

// DemoAgent 演示用的 Agent 实现
type DemoAgent struct {
	*core.BaseAgent
	maxSteps int
}

// NewDemoAgent 创建演示 Agent
func NewDemoAgent(maxSteps int) *DemoAgent {
	return &DemoAgent{
		BaseAgent: core.NewBaseAgent("demo", "Demo Agent for Generator", []string{"demo"}),
		maxSteps:  maxSteps,
	}
}

// Invoke 实现基本执行
func (d *DemoAgent) Invoke(ctx context.Context, input *core.AgentInput) (*core.AgentOutput, error) {
	return &core.AgentOutput{
		Status:  "success",
		Message: fmt.Sprintf("Processed: %s", input.Task),
		Result:  "Final result",
	}, nil
}

// Stream 实现流式执行
func (d *DemoAgent) Stream(ctx context.Context, input *core.AgentInput) (<-chan core.StreamChunk[*core.AgentOutput], error) {
	ch := make(chan core.StreamChunk[*core.AgentOutput], d.maxSteps)

	go func() {
		defer close(ch)
		for step := 0; step < d.maxSteps; step++ {
			select {
			case <-ctx.Done():
				ch <- core.StreamChunk[*core.AgentOutput]{Error: ctx.Err()}
				return
			case ch <- core.StreamChunk[*core.AgentOutput]{
				Data: &core.AgentOutput{
					Status:  "success",
					Message: fmt.Sprintf("Step %d: Processing %s", step+1, input.Task),
					Result:  fmt.Sprintf("Result of step %d", step+1),
				},
				Error: nil,
			}:
			}
		}
	}()

	return ch, nil
}

// RunGenerator 实现流式生成器
func (d *DemoAgent) RunGenerator(ctx context.Context, input *core.AgentInput) core.Generator[*core.AgentOutput] {
	return func(yield func(*core.AgentOutput, error) bool) {
		for step := 0; step < d.maxSteps; step++ {
			// 检查上下文取消
			if ctx.Err() != nil {
				yield(nil, ctx.Err())
				return
			}

			// 产生步骤输出
			output := &core.AgentOutput{
				Status:  "success",
				Message: fmt.Sprintf("Step %d: Processing %s", step+1, input.Task),
				Result:  fmt.Sprintf("Result of step %d", step+1),
			}

			if !yield(output, nil) {
				// 消费者提前终止
				return
			}
		}
	}
}

func main() {
	fmt.Println("========================================")
	fmt.Println("Generator 模式示例")
	fmt.Println("========================================")
	fmt.Println()

	// 创建 Agent
	agent := NewDemoAgent(5)

	// 准备输入
	input := &core.AgentInput{
		Task: "示例任务：计算数据",
	}

	ctx := context.Background()

	// ========================================
	// 方式 1：使用 Generator（推荐 - 零分配）
	// ========================================
	fmt.Println("=== 方式 1：Generator 模式（推荐）===")
	for output, err := range agent.RunGenerator(ctx, input) {
		if err != nil {
			log.Fatal(err)
		}

		fmt.Printf("  %s\n", output.Message)
	}
	fmt.Println()

	// ========================================
	// 方式 2：使用 Channel
	// ========================================
	fmt.Println("=== 方式 2：Channel 模式 ===")
	ch, err := agent.Stream(ctx, input)
	if err != nil {
		log.Fatal(err)
	}

	for event := range ch {
		if event.Error != nil {
			log.Fatal(event.Error)
		}

		fmt.Printf("  %s\n", event.Data.Message)
	}
	fmt.Println()

	// ========================================
	// 方式 3：Generator 辅助函数 - Collect
	// ========================================
	fmt.Println("=== 方式 3：使用 Collect 辅助函数 ===")
	gen := agent.RunGenerator(ctx, input)
	results, err := core.Collect(gen)
	if err != nil {
		log.Fatal(err)
	}

	fmt.Printf("  共执行 %d 步\n", len(results))
	for i, output := range results {
		fmt.Printf("  Step %d: %s\n", i+1, output.Message)
	}
	fmt.Println()

	// ========================================
	// 方式 4：Generator 转 Channel（兼容性桥接）
	// ========================================
	fmt.Println("=== 方式 4：Generator 转 Channel ===")
	gen2 := agent.RunGenerator(ctx, input)
	ch2 := core.ToChannel(ctx, gen2, 10)

	for event := range ch2 {
		if event.Error != nil {
			log.Fatal(event.Error)
		}
		fmt.Printf("  %s\n", event.Data.Message)
	}
	fmt.Println()

	// ========================================
	// 方式 5：早期终止示例
	// ========================================
	fmt.Println("=== 方式 5：早期终止（仅处理前 3 步）===")
	gen3 := agent.RunGenerator(ctx, input)

	count := 0
	for output, err := range gen3 {
		if err != nil {
			log.Fatal(err)
		}

		fmt.Printf("  %s\n", output.Message)

		count++
		if count >= 3 {
			fmt.Println("  提前终止！")
			break
		}
	}
	fmt.Println()

	fmt.Println("========================================")
	fmt.Println("示例完成")
	fmt.Println("========================================")
}
