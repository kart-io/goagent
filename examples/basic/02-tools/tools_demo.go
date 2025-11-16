package main

import (
	"context"
	"fmt"
	"log"
	"time"

	agentcore "github.com/kart-io/goagent/core"
	"github.com/kart-io/goagent/toolkits"
	"github.com/kart-io/goagent/tools"
	"github.com/kart-io/goagent/tools/compute"
	"github.com/kart-io/goagent/tools/http"
	"github.com/kart-io/goagent/tools/search"
	"github.com/kart-io/goagent/tools/shell"
)

func main() {
	fmt.Println("=== Tools System Examples ===")
	fmt.Println()

	// Example 1: Basic Tool Usage
	example1BasicToolUsage()

	// Example 2: Function Tool
	example2FunctionTool()

	// Example 3: Calculator Tool
	example3CalculatorTool()

	// Example 4: Search Tool
	example4SearchTool()

	// Example 5: Shell Tool
	example5ShellTool()

	// Example 6: API Tool
	example6APITool()

	// Example 7: Toolkit Usage
	example7ToolkitUsage()

	// Example 8: Tool Registry
	example8ToolRegistry()

	// Example 9: Tools with Callbacks
	// NOTE: Commented out - WithCallbacks is no longer part of the simplified Tool interface
	// example9ToolsWithCallbacks()

	// Example 10: Custom Tool
	example10CustomTool()

	// Example 11: Tool Executor
	example11ToolExecutor()
}

// Example 1: 基础工具使用
func example1BasicToolUsage() {
	fmt.Println("--- Example 1: Basic Tool Usage ---")

	// 创建一个简单的工具
	tool := tools.NewBaseTool(
		"hello",
		"Says hello to a name",
		`{"type": "object", "properties": {"name": {"type": "string"}}}`,
		func(ctx context.Context, input *tools.ToolInput) (*tools.ToolOutput, error) {
			name, _ := input.Args["name"].(string)
			return &tools.ToolOutput{
				Result:  fmt.Sprintf("Hello, %s!", name),
				Success: true,
			}, nil
		},
	)

	fmt.Printf("Tool Name: %s\n", tool.Name())
	fmt.Printf("Tool Description: %s\n", tool.Description())

	ctx := context.Background()
	input := &tools.ToolInput{
		Args: map[string]interface{}{
			"name": "World",
		},
		Context: ctx,
	}

	output, err := tool.Invoke(ctx, input)
	if err != nil {
		log.Printf("Error: %v\n", err)
		return
	}

	fmt.Printf("Result: %v\n", output.Result)
	fmt.Printf("Success: %v\n\n", output.Success)
}

// Example 2: 函数工具
func example2FunctionTool() {
	fmt.Println("--- Example 2: Function Tool ---")

	// 使用 FunctionToolBuilder
	tool := tools.NewFunctionToolBuilder("multiplier").
		WithDescription("Multiplies two numbers").
		WithArgsSchema(`{
			"type": "object",
			"properties": {
				"a": {"type": "number"},
				"b": {"type": "number"}
			}
		}`).
		WithFunction(func(ctx context.Context, args map[string]interface{}) (interface{}, error) {
			a := args["a"].(float64)
			b := args["b"].(float64)
			return a * b, nil
		}).
		Build()

	ctx := context.Background()
	input := &tools.ToolInput{
		Args: map[string]interface{}{
			"a": 6.0,
			"b": 7.0,
		},
		Context: ctx,
	}

	output, err := tool.Invoke(ctx, input)
	if err != nil {
		log.Printf("Error: %v\n", err)
		return
	}

	fmt.Printf("6 * 7 = %v\n\n", output.Result)
}

// Example 3: 计算器工具
func example3CalculatorTool() {
	fmt.Println("--- Example 3: Calculator Tool ---")

	tool := compute.NewCalculatorTool()
	ctx := context.Background()

	expressions := []string{
		"2 + 3",
		"10 - 5",
		"4 * 3",
		"15 / 3",
		"2 + 3 * 4",
		"(2 + 3) * 4",
		"2^8",
	}

	for _, expr := range expressions {
		input := &tools.ToolInput{
			Args: map[string]interface{}{
				"expression": expr,
			},
			Context: ctx,
		}

		output, err := tool.Invoke(ctx, input)
		if err != nil {
			log.Printf("Error evaluating '%s': %v\n", expr, err)
			continue
		}

		if output.Success {
			fmt.Printf("%s = %v\n", expr, output.Result)
		} else {
			fmt.Printf("%s failed: %s\n", expr, output.Error)
		}
	}
	fmt.Println()
}

// Example 4: 搜索工具
func example4SearchTool() {
	fmt.Println("--- Example 4: Search Tool ---")

	// 使用模拟搜索引擎
	engine := search.NewMockSearchEngine()
	tool := search.NewSearchTool(engine)

	ctx := context.Background()
	input := &tools.ToolInput{
		Args: map[string]interface{}{
			"query":       "Go programming language",
			"max_results": 3.0,
		},
		Context: ctx,
	}

	output, err := tool.Invoke(ctx, input)
	if err != nil {
		log.Printf("Error: %v\n", err)
		return
	}

	if output.Success {
		results := output.Result.([]search.SearchResult)
		fmt.Printf("Found %d results:\n", len(results))
		for i, result := range results {
			fmt.Printf("%d. %s\n", i+1, result.Title)
			fmt.Printf("   URL: %s\n", result.URL)
			fmt.Printf("   Snippet: %s\n", result.Snippet[:50]+"...")
		}
	}
	fmt.Println()
}

// Example 5: Shell 工具
func example5ShellTool() {
	fmt.Println("--- Example 5: Shell Tool ---")

	// 创建安全的 Shell 工具（只允许特定命令）
	tool := shell.NewShellToolBuilder().
		WithAllowedCommands("echo", "pwd", "ls", "date").
		WithTimeout(5 * time.Second).
		Build()

	ctx := context.Background()

	// 执行允许的命令
	input := &tools.ToolInput{
		Args: map[string]interface{}{
			"command": "echo",
			"args":    []interface{}{"Hello from Shell!"},
		},
		Context: ctx,
	}

	output, err := tool.Invoke(ctx, input)
	if err != nil {
		log.Printf("Error: %v\n", err)
	} else if output.Success {
		result := output.Result.(map[string]interface{})
		fmt.Printf("Command output: %s\n", result["output"])
	}

	// 尝试执行不允许的命令
	input2 := &tools.ToolInput{
		Args: map[string]interface{}{
			"command": "rm",
			"args":    []interface{}{"-rf", "/tmp/test"},
		},
		Context: ctx,
	}

	output2, err := tool.Invoke(ctx, input2)
	if err != nil {
		fmt.Printf("Disallowed command rejected: %v\n", err)
	} else if !output2.Success {
		fmt.Printf("Disallowed command rejected: %s\n", output2.Error)
	}
	fmt.Println()
}

// Example 6: API 工具
func example6APITool() {
	fmt.Println("--- Example 6: API Tool ---")

	// 创建 API 工具
	tool := http.NewAPIToolBuilder().
		WithBaseURL("https://jsonplaceholder.typicode.com").
		WithTimeout(10 * time.Second).
		Build()

	ctx := context.Background()

	// GET 请求
	input := &tools.ToolInput{
		Args: map[string]interface{}{
			"method": "GET",
			"url":    "/posts/1",
		},
		Context: ctx,
	}

	output, err := tool.Invoke(ctx, input)
	if err != nil {
		log.Printf("Error: %v\n", err)
		return
	}

	if output.Success {
		result := output.Result.(map[string]interface{})
		fmt.Printf("Status: %v\n", result["status"])
		body := result["body"].(map[string]interface{})
		fmt.Printf("Post Title: %v\n", body["title"])
	}
	fmt.Println()
}

// Example 7: 工具集使用
func example7ToolkitUsage() {
	fmt.Println("--- Example 7: Toolkit Usage ---")

	// 使用标准工具集
	toolkit := toolkits.NewStandardToolkit()

	fmt.Println("Available tools:")
	for _, name := range toolkit.GetToolNames() {
		fmt.Printf("- %s\n", name)
	}

	// 使用工具集中的工具
	calcTool, _ := toolkit.GetToolByName("calculator")
	ctx := context.Background()
	input := &tools.ToolInput{
		Args: map[string]interface{}{
			"expression": "10 + 20",
		},
		Context: ctx,
	}

	output, err := calcTool.Invoke(ctx, input)
	if err != nil {
		log.Printf("Error: %v\n", err)
		return
	}

	fmt.Printf("\nCalculator result: %v\n\n", output.Result)
}

// Example 8: 工具注册表
func example8ToolRegistry() {
	fmt.Println("--- Example 8: Tool Registry ---")

	registry := toolkits.NewToolRegistry()

	// 注册工具
	_ = registry.Register(compute.NewCalculatorTool())
	_ = registry.Register(search.NewSearchTool(search.NewMockSearchEngine()))

	fmt.Println("Registered tools:")
	for _, tool := range registry.List() {
		fmt.Printf("- %s: %s\n", tool.Name(), tool.Description())
	}

	// 从注册表创建工具集
	toolkit, _ := registry.CreateToolkit("calculator", "search")
	fmt.Printf("\nCreated toolkit with %d tools\n\n", len(toolkit.GetTools()))
}

// Example 9: 工具与回调
// NOTE: Commented out - WithCallbacks is no longer part of the simplified Tool interface
/*
func example9ToolsWithCallbacks() {
	fmt.Println("--- Example 9: Tools with Callbacks ---")

	// 创建回调
	callback := &loggingCallback{}

	// 创建带回调的工具
	tool := compute.NewCalculatorTool()
	toolWithCallback := tool.WithCallbacks(callback).(tools.Tool)

	ctx := context.Background()
	input := &tools.ToolInput{
		Args: map[string]interface{}{
			"expression": "5 + 5",
		},
		Context: ctx,
	}

	output, err := toolWithCallback.Invoke(ctx, input)
	if err != nil {
		log.Printf("Error: %v\n", err)
		return
	}

	fmt.Printf("Result: %v\n\n", output.Result)
}
*/

// Placeholder for example 9
func example9ToolsWithCallbacks() {
	fmt.Println("--- Example 9: Tools with Callbacks ---")
	fmt.Println("This example has been disabled due to interface changes.")
	fmt.Println("The simplified Tool interface no longer includes WithCallbacks method.")
	fmt.Println()
}

// Example 10: 自定义工具
func example10CustomTool() {
	fmt.Println("--- Example 10: Custom Tool ---")

	// 创建自定义工具
	tool := tools.NewBaseTool(
		"weather",
		"Gets weather information for a city",
		`{
			"type": "object",
			"properties": {
				"city": {"type": "string"},
				"units": {"type": "string", "enum": ["celsius", "fahrenheit"]}
			}
		}`,
		func(ctx context.Context, input *tools.ToolInput) (*tools.ToolOutput, error) {
			city, _ := input.Args["city"].(string)
			units, _ := input.Args["units"].(string)

			// 模拟天气数据
			weather := map[string]interface{}{
				"city":        city,
				"temperature": 22,
				"units":       units,
				"condition":   "Sunny",
				"humidity":    65,
			}

			return &tools.ToolOutput{
				Result:  weather,
				Success: true,
			}, nil
		},
	)

	ctx := context.Background()
	input := &tools.ToolInput{
		Args: map[string]interface{}{
			"city":  "Beijing",
			"units": "celsius",
		},
		Context: ctx,
	}

	output, err := tool.Invoke(ctx, input)
	if err != nil {
		log.Printf("Error: %v\n", err)
		return
	}

	if output.Success {
		weather := output.Result.(map[string]interface{})
		fmt.Printf("Weather in %s: %v°%s, %s\n",
			weather["city"],
			weather["temperature"],
			weather["units"],
			weather["condition"])
	}
	fmt.Println()
}

// Example 11: 工具执行器
func example11ToolExecutor() {
	fmt.Println("--- Example 11: Tool Executor ---")

	// 创建工具集
	toolkit := toolkits.NewToolkitBuilder().
		WithCalculator().
		WithSearch(nil).
		Build()

	// 创建执行器
	executor := toolkits.NewToolkitExecutor(toolkit)

	ctx := context.Background()

	// 执行单个工具
	output, err := executor.Execute(ctx, "calculator", &tools.ToolInput{
		Args: map[string]interface{}{
			"expression": "100 / 5",
		},
		Context: ctx,
	})

	if err != nil {
		log.Printf("Error: %v\n", err)
	} else {
		fmt.Printf("Calculator: %v\n", output.Result)
	}

	// 并行执行多个工具
	executor.WithParallel(true)

	requests := map[string]*tools.ToolInput{
		"calculator": {
			Args: map[string]interface{}{
				"expression": "50 + 50",
			},
			Context: ctx,
		},
		"search": {
			Args: map[string]interface{}{
				"query":       "Go tools",
				"max_results": 2.0,
			},
			Context: ctx,
		},
	}

	results, err := executor.ExecuteMultiple(ctx, requests)
	if err != nil {
		log.Printf("Error: %v\n", err)
	} else {
		fmt.Println("\nParallel execution results:")
		for toolName, output := range results {
			fmt.Printf("- %s: Success=%v\n", toolName, output.Success)
		}
	}
	fmt.Println()
}

// loggingCallback 日志回调
type loggingCallback struct {
	agentcore.BaseCallback
}

func (l *loggingCallback) OnToolStart(ctx context.Context, toolName string, input interface{}) error {
	fmt.Printf("[CALLBACK] Tool '%s' started\n", toolName)
	return nil
}

func (l *loggingCallback) OnToolEnd(ctx context.Context, toolName string, output interface{}) error {
	fmt.Printf("[CALLBACK] Tool '%s' completed\n", toolName)
	return nil
}

func (l *loggingCallback) OnToolError(ctx context.Context, toolName string, err error) error {
	fmt.Printf("[CALLBACK] Tool '%s' error: %v\n", toolName, err)
	return nil
}
