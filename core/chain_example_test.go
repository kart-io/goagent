package core_test

import (
	"context"
	"fmt"
	"strings"

	"github.com/kart-io/goagent/core"
)

// simpleStep 实现 Step 接口的简单步骤
type simpleStep struct {
	name        string
	description string
	fn          func(context.Context, interface{}) (interface{}, error)
}

func (s *simpleStep) Name() string        { return s.name }
func (s *simpleStep) Description() string { return s.description }
func (s *simpleStep) Execute(ctx context.Context, input interface{}) (interface{}, error) {
	return s.fn(ctx, input)
}

// ExampleChain_Invoke 展示如何使用 Chain 的 Invoke 方法
func ExampleChain_Invoke() {
	// 创建处理步骤
	steps := []core.Step{
		&simpleStep{
			name:        "uppercase",
			description: "Convert to uppercase",
			fn: func(ctx context.Context, input interface{}) (interface{}, error) {
				if str, ok := input.(string); ok {
					return strings.ToUpper(str), nil
				}
				return input, nil
			},
		},
		&simpleStep{
			name:        "add-prefix",
			description: "Add prefix",
			fn: func(ctx context.Context, input interface{}) (interface{}, error) {
				if str, ok := input.(string); ok {
					return "PROCESSED: " + str, nil
				}
				return input, nil
			},
		},
	}

	// 创建 Chain
	chain := core.NewBaseChain("TextProcessing", steps)

	// 准备输入
	input := &core.ChainInput{
		Data:    "hello world",
		Options: core.DefaultChainOptions(),
	}

	// 执行 Chain
	output, err := chain.Invoke(context.Background(), input)
	if err != nil {
		fmt.Printf("Error: %v\n", err)
		return
	}

	fmt.Printf("Result: %s\n", output.Data)
	fmt.Printf("Steps: %d\n", len(output.StepsExecuted))
	fmt.Printf("Status: %s\n", output.Status)

	// Output:
	// Result: PROCESSED: HELLO WORLD
	// Steps: 2
	// Status: success
}

// ExampleChain_Stream 展示如何使用 Chain 的流式执行
func ExampleChain_Stream() {
	steps := []core.Step{
		&simpleStep{
			name:        "step1",
			description: "First step",
			fn: func(ctx context.Context, input interface{}) (interface{}, error) {
				return "step1-done", nil
			},
		},
		&simpleStep{
			name:        "step2",
			description: "Second step",
			fn: func(ctx context.Context, input interface{}) (interface{}, error) {
				return "step2-done", nil
			},
		},
	}

	chain := core.NewBaseChain("StreamChain", steps)
	input := &core.ChainInput{
		Data:    "initial",
		Options: core.DefaultChainOptions(),
	}

	// 流式执行
	stream, err := chain.Stream(context.Background(), input)
	if err != nil {
		fmt.Printf("Error: %v\n", err)
		return
	}

	// 消费流
	for chunk := range stream {
		if chunk.Error != nil {
			fmt.Printf("Stream error: %v\n", chunk.Error)
			break
		}

		if chunk.Done {
			fmt.Printf("Final result: %s\n", chunk.Data.Data)
		} else {
			fmt.Printf("Intermediate: steps completed = %d\n", len(chunk.Data.StepsExecuted))
		}
	}

	// Output:
	// Intermediate: steps completed = 1
	// Intermediate: steps completed = 2
	// Final result: step2-done
}

// ExampleChain_Batch 展示如何批量执行 Chain
func ExampleChain_Batch() {
	steps := []core.Step{
		&simpleStep{
			name:        "multiply",
			description: "Multiply by 2",
			fn: func(ctx context.Context, input interface{}) (interface{}, error) {
				if num, ok := input.(int); ok {
					return num * 2, nil
				}
				return input, nil
			},
		},
	}

	chain := core.NewBaseChain("BatchChain", steps)

	// 准备批量输入
	inputs := []*core.ChainInput{
		{Data: 1, Options: core.DefaultChainOptions()},
		{Data: 2, Options: core.DefaultChainOptions()},
		{Data: 3, Options: core.DefaultChainOptions()},
	}

	// 批量执行
	outputs, err := chain.Batch(context.Background(), inputs)
	if err != nil {
		fmt.Printf("Error: %v\n", err)
		return
	}

	// 打印结果
	for i, output := range outputs {
		fmt.Printf("Result %d: %v\n", i+1, output.Data)
	}

	// Output:
	// Result 1: 2
	// Result 2: 4
	// Result 3: 6
}

// ExampleChain_WithCallbacks 展示如何使用回调
func ExampleChain_WithCallbacks() {
	// 创建简单的回调
	callback := &loggingCallback{}

	steps := []core.Step{
		&simpleStep{
			name:        "process",
			description: "Process data",
			fn: func(ctx context.Context, input interface{}) (interface{}, error) {
				return "processed", nil
			},
		},
	}

	chain := core.NewBaseChain("CallbackChain", steps)

	// 添加回调
	chainWithCallback := chain.WithCallbacks(callback)

	input := &core.ChainInput{
		Data:    "input",
		Options: core.DefaultChainOptions(),
	}

	// 执行（会触发回调）
	output, err := chainWithCallback.Invoke(context.Background(), input)
	if err != nil {
		fmt.Printf("Error: %v\n", err)
		return
	}

	fmt.Printf("Result: %s\n", output.Data)

	// Output:
	// Chain started: CallbackChain
	// Chain ended: CallbackChain
	// Result: processed
}

// loggingCallback 简单的日志回调
type loggingCallback struct {
	*core.BaseCallback
}

func (l *loggingCallback) OnChainStart(ctx context.Context, chainName string, input interface{}) error {
	fmt.Printf("Chain started: %s\n", chainName)
	return nil
}

func (l *loggingCallback) OnChainEnd(ctx context.Context, chainName string, output interface{}) error {
	fmt.Printf("Chain ended: %s\n", chainName)
	return nil
}
