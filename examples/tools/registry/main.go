// Package main 演示工具注册表和执行器的使用
//
// 本示例展示：
// 1. 工具注册表（Registry）的创建和管理
// 2. 自定义工具的创建和注册
// 3. 工具执行器（ToolExecutor）的使用
// 4. 工具的动态发现和调用
// 5. 工具组合和批量执行
package main

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/kart-io/goagent/interfaces"
	"github.com/kart-io/goagent/multiagent"
	"github.com/kart-io/goagent/tools"
	loggercore "github.com/kart-io/logger/core"
)

func main() {
	fmt.Println("╔════════════════════════════════════════════════════════════════╗")
	fmt.Println("║          工具注册与执行示例                                     ║")
	fmt.Println("║   展示工具注册表、自定义工具和执行器的使用                       ║")
	fmt.Println("╚════════════════════════════════════════════════════════════════╝")
	fmt.Println()

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	// 创建日志和系统
	logger := &simpleLogger{}
	system := multiagent.NewMultiAgentSystem(logger)
	defer func() { _ = system.Close() }()

	// 场景 1：工具注册表管理
	fmt.Println("【场景 1】工具注册表管理")
	fmt.Println("════════════════════════════════════════════════════════════════")
	demonstrateRegistry(ctx)

	// 场景 2：自定义工具创建
	fmt.Println("\n【场景 2】自定义工具创建")
	fmt.Println("════════════════════════════════════════════════════════════════")
	demonstrateCustomTools(ctx)

	// 场景 3：工具执行器
	fmt.Println("\n【场景 3】工具执行器")
	fmt.Println("════════════════════════════════════════════════════════════════")
	demonstrateToolExecutor(ctx)

	// 场景 4：工具动态发现
	fmt.Println("\n【场景 4】工具动态发现")
	fmt.Println("════════════════════════════════════════════════════════════════")
	demonstrateToolDiscovery(ctx)

	// 场景 5：工具组合和批量执行
	fmt.Println("\n【场景 5】工具组合和批量执行")
	fmt.Println("════════════════════════════════════════════════════════════════")
	demonstrateToolComposition(ctx, system)

	fmt.Println("\n╔════════════════════════════════════════════════════════════════╗")
	fmt.Println("║                        示例完成                                ║")
	fmt.Println("╚════════════════════════════════════════════════════════════════╝")
}

// ============================================================================
// 场景 1：工具注册表管理
// ============================================================================

func demonstrateRegistry(ctx context.Context) {
	fmt.Println("\n场景描述: 展示工具注册表的基本操作")
	fmt.Println()

	// 创建注册表
	registry := tools.NewRegistry()
	fmt.Println("✓ 创建工具注册表")

	// 注册工具
	fmt.Println("\n注册工具:")
	fmt.Println("────────────────────────────────────────")

	toolsToRegister := []interfaces.Tool{
		createCalculatorTool(),
		createTextProcessorTool(),
		createDateTimeTool(),
		createRandomGeneratorTool(),
	}

	for _, tool := range toolsToRegister {
		if err := registry.Register(tool); err != nil {
			fmt.Printf("  ✗ %s: 注册失败 - %v\n", tool.Name(), err)
		} else {
			fmt.Printf("  ✓ %s: %s\n", tool.Name(), tool.Description())
		}
	}

	// 查看注册表状态
	fmt.Println("\n注册表状态:")
	fmt.Println("────────────────────────────────────────")
	fmt.Printf("  已注册工具数: %d\n", registry.Size())
	fmt.Printf("  工具列表: %v\n", registry.Names())

	// 获取单个工具
	fmt.Println("\n获取工具:")
	fmt.Println("────────────────────────────────────────")

	if tool := registry.Get("calculator"); tool != nil {
		fmt.Printf("  ✓ 找到工具: %s\n", tool.Name())
		fmt.Printf("    描述: %s\n", tool.Description())
	}

	if tool := registry.Get("nonexistent"); tool == nil {
		fmt.Println("  ✓ 未找到工具: nonexistent (预期行为)")
	}

	// 尝试重复注册
	fmt.Println("\n重复注册测试:")
	fmt.Println("────────────────────────────────────────")
	if err := registry.Register(createCalculatorTool()); err != nil {
		fmt.Printf("  ✓ 重复注册被拒绝: %v\n", err)
	}

	// 清空注册表
	registry.Clear()
	fmt.Printf("\n✓ 清空注册表，当前工具数: %d\n", registry.Size())
}

// ============================================================================
// 场景 2：自定义工具创建
// ============================================================================

func demonstrateCustomTools(ctx context.Context) {
	fmt.Println("\n场景描述: 展示如何创建各种类型的自定义工具")
	fmt.Println()

	// 1. 使用 FunctionTool 创建简单工具
	fmt.Println("1. FunctionTool - 函数式工具")
	fmt.Println("────────────────────────────────────────")

	greetTool := tools.NewFunctionTool(
		"greet",
		"生成问候语",
		`{
			"type": "object",
			"properties": {
				"name": {"type": "string", "description": "姓名"},
				"language": {"type": "string", "enum": ["zh", "en"], "default": "zh"}
			},
			"required": ["name"]
		}`,
		func(ctx context.Context, args map[string]interface{}) (interface{}, error) {
			name := args["name"].(string)
			lang := "zh"
			if l, ok := args["language"].(string); ok {
				lang = l
			}

			if lang == "en" {
				return map[string]interface{}{
					"greeting": fmt.Sprintf("Hello, %s!", name),
					"language": "en",
				}, nil
			}
			return map[string]interface{}{
				"greeting": fmt.Sprintf("你好，%s！", name),
				"language": "zh",
			}, nil
		},
	)

	// 调用工具
	result, err := greetTool.Invoke(ctx, &interfaces.ToolInput{
		Args: map[string]interface{}{
			"name":     "张三",
			"language": "zh",
		},
	})
	if err != nil {
		fmt.Printf("  ✗ 调用失败: %v\n", err)
	} else {
		fmt.Printf("  ✓ 调用成功: %v\n", result.Result)
	}

	// 2. 使用 BaseTool 创建复杂工具
	fmt.Println("\n2. BaseTool - 基础工具类")
	fmt.Println("────────────────────────────────────────")

	transformTool := tools.NewBaseTool(
		"transform",
		"文本转换工具",
		`{
			"type": "object",
			"properties": {
				"text": {"type": "string"},
				"operation": {"type": "string", "enum": ["upper", "lower", "reverse", "length"]}
			},
			"required": ["text", "operation"]
		}`,
		func(ctx context.Context, input *interfaces.ToolInput) (*interfaces.ToolOutput, error) {
			text := input.Args["text"].(string)
			op := input.Args["operation"].(string)

			var result interface{}
			switch op {
			case "upper":
				result = strings.ToUpper(text)
			case "lower":
				result = strings.ToLower(text)
			case "reverse":
				runes := []rune(text)
				for i, j := 0, len(runes)-1; i < j; i, j = i+1, j-1 {
					runes[i], runes[j] = runes[j], runes[i]
				}
				result = string(runes)
			case "length":
				result = len([]rune(text))
			}

			return &interfaces.ToolOutput{
				Result:  map[string]interface{}{"operation": op, "result": result},
				Success: true,
			}, nil
		},
	)

	result2, _ := transformTool.Invoke(ctx, &interfaces.ToolInput{
		Args: map[string]interface{}{
			"text":      "Hello World",
			"operation": "reverse",
		},
	})
	fmt.Printf("  ✓ 文本反转: %v\n", result2.Result)

	// 3. 带验证的工具
	fmt.Println("\n3. 带验证的工具")
	fmt.Println("────────────────────────────────────────")

	divisionTool := tools.NewFunctionTool(
		"division",
		"执行除法运算",
		`{
			"type": "object",
			"properties": {
				"a": {"type": "number"},
				"b": {"type": "number"}
			},
			"required": ["a", "b"]
		}`,
		func(ctx context.Context, args map[string]interface{}) (interface{}, error) {
			a := args["a"].(float64)
			b := args["b"].(float64)

			// 输入验证
			if b == 0 {
				return nil, fmt.Errorf("除数不能为零")
			}

			return map[string]interface{}{
				"dividend": a,
				"divisor":  b,
				"quotient": a / b,
			}, nil
		},
	)

	// 正常调用
	result3, err := divisionTool.Invoke(ctx, &interfaces.ToolInput{
		Args: map[string]interface{}{"a": 10.0, "b": 3.0},
	})
	if err == nil {
		fmt.Printf("  ✓ 10 / 3 = %v\n", result3.Result)
	}

	// 错误调用
	_, err = divisionTool.Invoke(ctx, &interfaces.ToolInput{
		Args: map[string]interface{}{"a": 10.0, "b": 0.0},
	})
	if err != nil {
		fmt.Printf("  ✓ 除零错误被正确捕获: %v\n", err)
	}
}

// ============================================================================
// 场景 3：工具执行器
// ============================================================================

func demonstrateToolExecutor(ctx context.Context) {
	fmt.Println("\n场景描述: 展示工具执行器的使用")
	fmt.Println()

	// 创建执行器
	executor := NewSimpleToolExecutor()

	// 注册工具
	_ = executor.RegisterTool(createCalculatorTool())
	_ = executor.RegisterTool(createTextProcessorTool())
	_ = executor.RegisterTool(createDateTimeTool())

	fmt.Println("已注册工具:")
	fmt.Println("────────────────────────────────────────")
	for _, tool := range executor.ListTools() {
		fmt.Printf("  - %s: %s\n", tool.Name(), tool.Description())
	}

	// 执行工具
	fmt.Println("\n执行工具调用:")
	fmt.Println("────────────────────────────────────────")

	testCases := []struct {
		toolName string
		args     map[string]interface{}
	}{
		{"calculator", map[string]interface{}{"operation": "multiply", "a": 7.0, "b": 8.0}},
		{"text_processor", map[string]interface{}{"text": "hello world", "action": "uppercase"}},
		{"datetime", map[string]interface{}{"format": "date"}},
	}

	for _, tc := range testCases {
		result, err := executor.ExecuteTool(ctx, tc.toolName, tc.args)
		if err != nil {
			fmt.Printf("  ✗ %s: %v\n", tc.toolName, err)
		} else {
			fmt.Printf("  ✓ %s: %v (耗时: %v)\n", tc.toolName, result.Output.Result, result.Duration)
		}
	}
}

// ============================================================================
// 场景 4：工具动态发现
// ============================================================================

func demonstrateToolDiscovery(ctx context.Context) {
	fmt.Println("\n场景描述: 展示如何动态发现和匹配工具")
	fmt.Println()

	// 创建带标签的工具
	registry := tools.NewRegistry()

	// 注册带分类的工具
	mathTools := []interfaces.Tool{
		createCalculatorTool(),
		createRandomGeneratorTool(),
	}

	textTools := []interfaces.Tool{
		createTextProcessorTool(),
	}

	utilTools := []interfaces.Tool{
		createDateTimeTool(),
	}

	for _, t := range mathTools {
		_ = registry.Register(t)
	}
	for _, t := range textTools {
		_ = registry.Register(t)
	}
	for _, t := range utilTools {
		_ = registry.Register(t)
	}

	// 按名称模式查找
	fmt.Println("按名称模式查找:")
	fmt.Println("────────────────────────────────────────")

	patterns := []string{"calc", "text", "time", "rand"}
	for _, pattern := range patterns {
		matched := findToolsByPattern(registry, pattern)
		if len(matched) > 0 {
			fmt.Printf("  '%s' 匹配: %v\n", pattern, getToolNames(matched))
		} else {
			fmt.Printf("  '%s' 匹配: 无\n", pattern)
		}
	}

	// 按描述关键词查找
	fmt.Println("\n按描述关键词查找:")
	fmt.Println("────────────────────────────────────────")

	keywords := []string{"计算", "文本", "时间", "随机"}
	for _, keyword := range keywords {
		matched := findToolsByKeyword(registry, keyword)
		if len(matched) > 0 {
			fmt.Printf("  '%s' 匹配: %v\n", keyword, getToolNames(matched))
		} else {
			fmt.Printf("  '%s' 匹配: 无\n", keyword)
		}
	}

	// 列出所有工具的 Schema
	fmt.Println("\n工具参数 Schema:")
	fmt.Println("────────────────────────────────────────")
	for _, tool := range registry.List() {
		fmt.Printf("\n  [%s]\n", tool.Name())
		schema := tool.ArgsSchema()
		if len(schema) > 100 {
			schema = schema[:100] + "..."
		}
		fmt.Printf("    %s\n", schema)
	}
}

// ============================================================================
// 场景 5：工具组合和批量执行
// ============================================================================

func demonstrateToolComposition(ctx context.Context, system *multiagent.MultiAgentSystem) {
	fmt.Println("\n场景描述: 展示工具组合和批量执行")
	fmt.Println()

	// 创建工具链
	fmt.Println("1. 工具链执行 (Pipeline)")
	fmt.Println("────────────────────────────────────────")

	// 定义工具链：生成随机数 -> 计算 -> 格式化
	steps := []struct {
		name string
		tool interfaces.Tool
		args map[string]interface{}
	}{
		{"生成随机数", createRandomGeneratorTool(), map[string]interface{}{"min": 1.0, "max": 100.0}},
		{"乘以 2", createCalculatorTool(), nil}, // args 将从上一步获取
		{"格式化结果", createTextProcessorTool(), nil},
	}

	fmt.Println("  工具链: 随机数 → 计算 → 格式化")
	fmt.Println()

	var lastResult interface{}
	for i, step := range steps {
		args := step.args
		if args == nil {
			args = make(map[string]interface{})
		}

		// 使用上一步的结果
		if i == 1 && lastResult != nil {
			if r, ok := lastResult.(map[string]interface{}); ok {
				if num, ok := r["number"].(float64); ok {
					args["operation"] = "multiply"
					args["a"] = num
					args["b"] = 2.0
				}
			}
		}
		if i == 2 && lastResult != nil {
			if r, ok := lastResult.(map[string]interface{}); ok {
				args["text"] = fmt.Sprintf("计算结果: %v", r["result"])
				args["action"] = "uppercase"
			}
		}

		result, err := step.tool.Invoke(ctx, &interfaces.ToolInput{Args: args})
		if err != nil {
			fmt.Printf("  Step %d [%s]: ✗ %v\n", i+1, step.name, err)
			break
		}
		fmt.Printf("  Step %d [%s]: ✓ %v\n", i+1, step.name, result.Result)
		lastResult = result.Result
	}

	// 并行执行
	fmt.Println("\n2. 并行执行")
	fmt.Println("────────────────────────────────────────")

	parallelTools := []struct {
		tool interfaces.Tool
		args map[string]interface{}
	}{
		{createCalculatorTool(), map[string]interface{}{"operation": "add", "a": 100.0, "b": 200.0}},
		{createTextProcessorTool(), map[string]interface{}{"text": "parallel", "action": "uppercase"}},
		{createDateTimeTool(), map[string]interface{}{"format": "full"}},
		{createRandomGeneratorTool(), map[string]interface{}{"min": 1.0, "max": 1000.0}},
	}

	fmt.Printf("  并行执行 %d 个工具...\n", len(parallelTools))

	// 使用 channel 收集结果
	type toolResult struct {
		name   string
		result interface{}
		err    error
	}

	results := make(chan toolResult, len(parallelTools))

	for _, pt := range parallelTools {
		go func(t interfaces.Tool, a map[string]interface{}) {
			r, err := t.Invoke(ctx, &interfaces.ToolInput{Args: a})
			if err != nil {
				results <- toolResult{t.Name(), nil, err}
			} else {
				results <- toolResult{t.Name(), r.Result, nil}
			}
		}(pt.tool, pt.args)
	}

	// 收集结果
	fmt.Println("\n  并行执行结果:")
	for i := 0; i < len(parallelTools); i++ {
		r := <-results
		if r.err != nil {
			fmt.Printf("    ✗ %s: %v\n", r.name, r.err)
		} else {
			fmt.Printf("    ✓ %s: %v\n", r.name, r.result)
		}
	}

	// 条件执行
	fmt.Println("\n3. 条件执行")
	fmt.Println("────────────────────────────────────────")

	// 生成随机数，根据结果选择不同工具
	randTool := createRandomGeneratorTool()
	randResult, _ := randTool.Invoke(ctx, &interfaces.ToolInput{
		Args: map[string]interface{}{"min": 0.0, "max": 100.0},
	})

	if r, ok := randResult.Result.(map[string]interface{}); ok {
		num := r["number"].(float64)
		fmt.Printf("  生成随机数: %.2f\n", num)

		if num > 50 {
			fmt.Println("  条件: > 50，执行加法")
			calcResult, _ := createCalculatorTool().Invoke(ctx, &interfaces.ToolInput{
				Args: map[string]interface{}{"operation": "add", "a": num, "b": 100.0},
			})
			fmt.Printf("  结果: %v\n", calcResult.Result)
		} else {
			fmt.Println("  条件: <= 50，执行乘法")
			calcResult, _ := createCalculatorTool().Invoke(ctx, &interfaces.ToolInput{
				Args: map[string]interface{}{"operation": "multiply", "a": num, "b": 2.0},
			})
			fmt.Printf("  结果: %v\n", calcResult.Result)
		}
	}
}

// ============================================================================
// 工具定义
// ============================================================================

func createCalculatorTool() *tools.FunctionTool {
	return tools.NewFunctionTool(
		"calculator",
		"执行基本数学计算（加减乘除）",
		`{
			"type": "object",
			"properties": {
				"operation": {"type": "string", "enum": ["add", "subtract", "multiply", "divide"]},
				"a": {"type": "number"},
				"b": {"type": "number"}
			},
			"required": ["operation", "a", "b"]
		}`,
		func(ctx context.Context, args map[string]interface{}) (interface{}, error) {
			op := args["operation"].(string)
			a := args["a"].(float64)
			b := args["b"].(float64)

			var result float64
			switch op {
			case "add":
				result = a + b
			case "subtract":
				result = a - b
			case "multiply":
				result = a * b
			case "divide":
				if b == 0 {
					return nil, fmt.Errorf("除数不能为零")
				}
				result = a / b
			}
			return map[string]interface{}{"operation": op, "a": a, "b": b, "result": result}, nil
		},
	)
}

func createTextProcessorTool() *tools.FunctionTool {
	return tools.NewFunctionTool(
		"text_processor",
		"处理文本（大小写转换、长度计算等）",
		`{
			"type": "object",
			"properties": {
				"text": {"type": "string"},
				"action": {"type": "string", "enum": ["uppercase", "lowercase", "length", "reverse"]}
			},
			"required": ["text", "action"]
		}`,
		func(ctx context.Context, args map[string]interface{}) (interface{}, error) {
			text := args["text"].(string)
			action := args["action"].(string)

			var result interface{}
			switch action {
			case "uppercase":
				result = strings.ToUpper(text)
			case "lowercase":
				result = strings.ToLower(text)
			case "length":
				result = len(text)
			case "reverse":
				runes := []rune(text)
				for i, j := 0, len(runes)-1; i < j; i, j = i+1, j-1 {
					runes[i], runes[j] = runes[j], runes[i]
				}
				result = string(runes)
			}
			return map[string]interface{}{"action": action, "input": text, "output": result}, nil
		},
	)
}

func createDateTimeTool() *tools.FunctionTool {
	return tools.NewFunctionTool(
		"datetime",
		"获取当前日期和时间",
		`{
			"type": "object",
			"properties": {
				"format": {"type": "string", "enum": ["date", "time", "full", "unix"]}
			}
		}`,
		func(ctx context.Context, args map[string]interface{}) (interface{}, error) {
			format := "full"
			if f, ok := args["format"].(string); ok {
				format = f
			}

			now := time.Now()
			var result interface{}

			switch format {
			case "date":
				result = now.Format("2006-01-02")
			case "time":
				result = now.Format("15:04:05")
			case "full":
				result = now.Format("2006-01-02 15:04:05")
			case "unix":
				result = now.Unix()
			}
			return map[string]interface{}{"format": format, "value": result}, nil
		},
	)
}

func createRandomGeneratorTool() *tools.FunctionTool {
	return tools.NewFunctionTool(
		"random",
		"生成随机数",
		`{
			"type": "object",
			"properties": {
				"min": {"type": "number", "default": 0},
				"max": {"type": "number", "default": 100}
			}
		}`,
		func(ctx context.Context, args map[string]interface{}) (interface{}, error) {
			min := 0.0
			max := 100.0
			if m, ok := args["min"].(float64); ok {
				min = m
			}
			if m, ok := args["max"].(float64); ok {
				max = m
			}

			// 使用时间戳生成伪随机数
			seed := float64(time.Now().UnixNano() % 10000)
			number := min + (seed/10000)*(max-min)

			return map[string]interface{}{
				"min":    min,
				"max":    max,
				"number": number,
			}, nil
		},
	)
}

// ============================================================================
// 工具执行器实现
// ============================================================================

// SimpleToolExecutor 简单工具执行器
type SimpleToolExecutor struct {
	registry *tools.Registry
}

// NewSimpleToolExecutor 创建工具执行器
func NewSimpleToolExecutor() *SimpleToolExecutor {
	return &SimpleToolExecutor{
		registry: tools.NewRegistry(),
	}
}

// RegisterTool 注册工具
func (e *SimpleToolExecutor) RegisterTool(tool interfaces.Tool) error {
	return e.registry.Register(tool)
}

// ExecuteTool 执行工具
func (e *SimpleToolExecutor) ExecuteTool(ctx context.Context, toolName string, args map[string]interface{}) (*tools.ToolResult, error) {
	tool := e.registry.Get(toolName)
	if tool == nil {
		return nil, fmt.Errorf("工具未找到: %s", toolName)
	}

	startTime := time.Now()
	output, err := tool.Invoke(ctx, &interfaces.ToolInput{Args: args})
	executionTime := time.Since(startTime)

	if err != nil {
		return nil, err
	}

	return &tools.ToolResult{
		CallID:   toolName,
		Output:   output,
		Duration: executionTime,
	}, nil
}

// ListTools 列出所有工具
func (e *SimpleToolExecutor) ListTools() []interfaces.Tool {
	return e.registry.List()
}

// ============================================================================
// 辅助函数
// ============================================================================

func findToolsByPattern(registry *tools.Registry, pattern string) []interfaces.Tool {
	var matched []interfaces.Tool
	for _, tool := range registry.List() {
		if strings.Contains(strings.ToLower(tool.Name()), strings.ToLower(pattern)) {
			matched = append(matched, tool)
		}
	}
	return matched
}

func findToolsByKeyword(registry *tools.Registry, keyword string) []interfaces.Tool {
	var matched []interfaces.Tool
	for _, tool := range registry.List() {
		if strings.Contains(tool.Description(), keyword) {
			matched = append(matched, tool)
		}
	}
	return matched
}

func getToolNames(tools []interfaces.Tool) []string {
	names := make([]string, len(tools))
	for i, t := range tools {
		names[i] = t.Name()
	}
	return names
}

// ============================================================================
// 日志实现
// ============================================================================

type simpleLogger struct{}

func (l *simpleLogger) Debug(args ...interface{})                       {}
func (l *simpleLogger) Info(args ...interface{})                        {}
func (l *simpleLogger) Warn(args ...interface{})                        {}
func (l *simpleLogger) Error(args ...interface{})                       {}
func (l *simpleLogger) Fatal(args ...interface{})                       {}
func (l *simpleLogger) Debugf(template string, args ...interface{})     {}
func (l *simpleLogger) Infof(template string, args ...interface{})      {}
func (l *simpleLogger) Warnf(template string, args ...interface{})      {}
func (l *simpleLogger) Errorf(template string, args ...interface{})     {}
func (l *simpleLogger) Fatalf(template string, args ...interface{})     {}
func (l *simpleLogger) Debugw(msg string, keysAndValues ...interface{}) {}
func (l *simpleLogger) Infow(msg string, keysAndValues ...interface{})  {}
func (l *simpleLogger) Warnw(msg string, keysAndValues ...interface{})  {}
func (l *simpleLogger) Errorw(msg string, keysAndValues ...interface{}) {}
func (l *simpleLogger) Fatalw(msg string, keysAndValues ...interface{}) {}
func (l *simpleLogger) With(keyValues ...interface{}) loggercore.Logger { return l }
func (l *simpleLogger) WithCtx(_ context.Context, keyValues ...interface{}) loggercore.Logger {
	return l
}
func (l *simpleLogger) WithCallerSkip(skip int) loggercore.Logger { return l }
func (l *simpleLogger) SetLevel(level loggercore.Level)           {}
func (l *simpleLogger) Sync() error                               { return nil }
func (l *simpleLogger) Flush() error                              { return nil }

var _ loggercore.Logger = (*simpleLogger)(nil)
