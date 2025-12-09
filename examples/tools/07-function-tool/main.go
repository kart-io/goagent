// Package main 演示自定义函数工具的使用方法
// 本示例展示如何使用 FunctionTool 和 FunctionToolBuilder 创建自定义工具
package main

import (
	"context"
	"fmt"
	"math/rand"
	"strings"
	"time"

	"github.com/kart-io/goagent/interfaces"
	"github.com/kart-io/goagent/tools"
)

func main() {
	fmt.Println("╔════════════════════════════════════════════════════════════════╗")
	fmt.Println("║          自定义函数工具 (FunctionTool) 示例                    ║")
	fmt.Println("║   展示如何创建和使用自定义函数工具                             ║")
	fmt.Println("╚════════════════════════════════════════════════════════════════╝")
	fmt.Println()

	// 创建上下文
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// 1. 使用 NewFunctionTool 创建简单工具
	fmt.Println("【步骤 1】使用 NewFunctionTool 创建工具")
	fmt.Println("────────────────────────────────────────")

	// 创建一个随机数生成器工具
	randomTool := tools.NewFunctionTool(
		"random_number",
		"生成指定范围内的随机整数",
		`{
			"type": "object",
			"properties": {
				"min": {
					"type": "integer",
					"description": "最小值（包含）",
					"default": 0
				},
				"max": {
					"type": "integer",
					"description": "最大值（包含）",
					"default": 100
				}
			}
		}`,
		func(ctx context.Context, args map[string]interface{}) (interface{}, error) {
			min := 0
			max := 100

			if m, ok := args["min"].(float64); ok {
				min = int(m)
			}
			if m, ok := args["max"].(float64); ok {
				max = int(m)
			}

			if min > max {
				return nil, fmt.Errorf("min (%d) cannot be greater than max (%d)", min, max)
			}

			result := rand.Intn(max-min+1) + min
			return map[string]interface{}{
				"random_number": result,
				"range":         fmt.Sprintf("[%d, %d]", min, max),
			}, nil
		},
	)

	fmt.Printf("工具名称: %s\n", randomTool.Name())
	fmt.Printf("工具描述: %s\n", randomTool.Description())
	fmt.Println()

	// 测试随机数工具
	for i := 0; i < 3; i++ {
		output, err := randomTool.Invoke(ctx, &interfaces.ToolInput{
			Args: map[string]interface{}{
				"min": float64(1),
				"max": float64(100),
			},
			Context: ctx,
		})

		if err != nil {
			fmt.Printf("✗ 生成随机数失败: %v\n", err)
		} else if output.Success {
			fmt.Printf("✓ 随机数 #%d: %v\n", i+1, output.Result)
		}
	}
	fmt.Println()

	// 2. 使用 FunctionToolBuilder 创建工具
	fmt.Println("【步骤 2】使用 FunctionToolBuilder 创建工具")
	fmt.Println("────────────────────────────────────────")

	// 创建一个字符串处理工具
	stringTool := tools.NewFunctionToolBuilder("string_processor").
		WithDescription("处理字符串，支持多种操作").
		WithArgsSchema(`{
			"type": "object",
			"properties": {
				"text": {
					"type": "string",
					"description": "要处理的文本"
				},
				"operation": {
					"type": "string",
					"enum": ["uppercase", "lowercase", "reverse", "length", "word_count"],
					"description": "操作类型"
				}
			},
			"required": ["text", "operation"]
		}`).
		WithFunction(func(ctx context.Context, args map[string]interface{}) (interface{}, error) {
			text, _ := args["text"].(string)
			operation, _ := args["operation"].(string)

			var result interface{}
			switch operation {
			case "uppercase":
				result = strings.ToUpper(text)
			case "lowercase":
				result = strings.ToLower(text)
			case "reverse":
				runes := []rune(text)
				for i, j := 0, len(runes)-1; i < j; i, j = i+1, j-1 {
					runes[i], runes[j] = runes[j], runes[i]
				}
				result = string(runes)
			case "length":
				result = len(text)
			case "word_count":
				result = len(strings.Fields(text))
			default:
				return nil, fmt.Errorf("unknown operation: %s", operation)
			}

			return map[string]interface{}{
				"input":     text,
				"operation": operation,
				"result":    result,
			}, nil
		}).
		MustBuild()

	fmt.Printf("工具名称: %s\n", stringTool.Name())
	fmt.Println()

	// 测试字符串工具
	testCases := []struct {
		text      string
		operation string
	}{
		{"Hello World", "uppercase"},
		{"Hello World", "lowercase"},
		{"Hello World", "reverse"},
		{"Hello World", "length"},
		{"This is a test sentence with multiple words", "word_count"},
	}

	for _, tc := range testCases {
		output, err := stringTool.Invoke(ctx, &interfaces.ToolInput{
			Args: map[string]interface{}{
				"text":      tc.text,
				"operation": tc.operation,
			},
			Context: ctx,
		})

		if err != nil {
			fmt.Printf("✗ %s 失败: %v\n", tc.operation, err)
		} else if output.Success {
			if result, ok := output.Result.(map[string]interface{}); ok {
				fmt.Printf("✓ %s('%s') = %v\n", tc.operation, tc.text, result["result"])
			}
		}
	}
	fmt.Println()

	// 3. 创建带状态的工具（计数器）
	fmt.Println("【步骤 3】创建带状态的工具（计数器）")
	fmt.Println("────────────────────────────────────────")

	counter := 0
	counterTool := tools.NewFunctionToolBuilder("counter").
		WithDescription("计数器工具，支持增加、减少、重置和获取当前值").
		WithArgsSchema(`{
			"type": "object",
			"properties": {
				"action": {
					"type": "string",
					"enum": ["increment", "decrement", "reset", "get"],
					"description": "计数器操作"
				},
				"amount": {
					"type": "integer",
					"description": "增减量（默认为 1）",
					"default": 1
				}
			},
			"required": ["action"]
		}`).
		WithFunction(func(ctx context.Context, args map[string]interface{}) (interface{}, error) {
			action, _ := args["action"].(string)
			amount := 1
			if a, ok := args["amount"].(float64); ok {
				amount = int(a)
			}

			switch action {
			case "increment":
				counter += amount
			case "decrement":
				counter -= amount
			case "reset":
				counter = 0
			case "get":
				// 不做任何操作，只返回当前值
			}

			return map[string]interface{}{
				"action":  action,
				"counter": counter,
			}, nil
		}).
		MustBuild()

	// 测试计数器
	actions := []struct {
		action string
		amount float64
	}{
		{"get", 0},
		{"increment", 1},
		{"increment", 5},
		{"decrement", 2},
		{"get", 0},
		{"reset", 0},
		{"get", 0},
	}

	for _, a := range actions {
		args := map[string]interface{}{
			"action": a.action,
		}
		if a.amount > 0 {
			args["amount"] = a.amount
		}

		output, _ := counterTool.Invoke(ctx, &interfaces.ToolInput{
			Args:    args,
			Context: ctx,
		})

		if output.Success {
			if result, ok := output.Result.(map[string]interface{}); ok {
				fmt.Printf("✓ %s: 计数器 = %v\n", a.action, result["counter"])
			}
		}
	}
	fmt.Println()

	// 4. 创建天气查询工具（模拟）
	fmt.Println("【步骤 4】创建天气查询工具（模拟）")
	fmt.Println("────────────────────────────────────────")

	weatherTool := tools.NewFunctionToolBuilder("weather").
		WithDescription("查询指定城市的天气信息（模拟数据）").
		WithArgsSchema(`{
			"type": "object",
			"properties": {
				"city": {
					"type": "string",
					"description": "城市名称"
				},
				"unit": {
					"type": "string",
					"enum": ["celsius", "fahrenheit"],
					"default": "celsius",
					"description": "温度单位"
				}
			},
			"required": ["city"]
		}`).
		WithFunction(func(ctx context.Context, args map[string]interface{}) (interface{}, error) {
			city, _ := args["city"].(string)
			unit := "celsius"
			if u, ok := args["unit"].(string); ok {
				unit = u
			}

			// 模拟天气数据
			weatherData := map[string]map[string]interface{}{
				"北京": {"temp": 22, "condition": "晴", "humidity": 45},
				"上海": {"temp": 25, "condition": "多云", "humidity": 65},
				"广州": {"temp": 30, "condition": "阴", "humidity": 80},
				"深圳": {"temp": 28, "condition": "小雨", "humidity": 75},
			}

			data, ok := weatherData[city]
			if !ok {
				// 生成随机数据
				data = map[string]interface{}{
					"temp":      15 + rand.Intn(20),
					"condition": []string{"晴", "多云", "阴", "小雨"}[rand.Intn(4)],
					"humidity":  30 + rand.Intn(50),
				}
			}

			temp := data["temp"].(int)
			if unit == "fahrenheit" {
				temp = temp*9/5 + 32
			}

			return map[string]interface{}{
				"city":        city,
				"temperature": temp,
				"unit":        unit,
				"condition":   data["condition"],
				"humidity":    fmt.Sprintf("%d%%", data["humidity"]),
				"timestamp":   time.Now().Format(time.RFC3339),
			}, nil
		}).
		MustBuild()

	cities := []string{"北京", "上海", "广州", "东京"}
	for _, city := range cities {
		output, _ := weatherTool.Invoke(ctx, &interfaces.ToolInput{
			Args: map[string]interface{}{
				"city": city,
				"unit": "celsius",
			},
			Context: ctx,
		})

		if output.Success {
			if result, ok := output.Result.(map[string]interface{}); ok {
				fmt.Printf("✓ %s: %v°C, %v, 湿度 %v\n",
					city, result["temperature"], result["condition"], result["humidity"])
			}
		}
	}
	fmt.Println()

	// 5. 使用 BaseTool 创建工具
	fmt.Println("【步骤 5】使用 BaseTool 创建工具")
	fmt.Println("────────────────────────────────────────")

	timeTool := tools.NewBaseTool(
		"current_time",
		"获取当前时间，支持多种格式",
		`{
			"type": "object",
			"properties": {
				"format": {
					"type": "string",
					"enum": ["rfc3339", "date", "time", "unix", "human"],
					"default": "rfc3339",
					"description": "时间格式"
				},
				"timezone": {
					"type": "string",
					"description": "时区（如 Asia/Shanghai）"
				}
			}
		}`,
		func(ctx context.Context, input *interfaces.ToolInput) (*interfaces.ToolOutput, error) {
			format := "rfc3339"
			if f, ok := input.Args["format"].(string); ok {
				format = f
			}

			now := time.Now()

			// 处理时区
			if tz, ok := input.Args["timezone"].(string); ok {
				loc, err := time.LoadLocation(tz)
				if err == nil {
					now = now.In(loc)
				}
			}

			var result string
			switch format {
			case "rfc3339":
				result = now.Format(time.RFC3339)
			case "date":
				result = now.Format("2006-01-02")
			case "time":
				result = now.Format("15:04:05")
			case "unix":
				result = fmt.Sprintf("%d", now.Unix())
			case "human":
				result = now.Format("2006年01月02日 15:04:05")
			}

			return &interfaces.ToolOutput{
				Result: map[string]interface{}{
					"format":   format,
					"time":     result,
					"timezone": now.Location().String(),
				},
				Success: true,
			}, nil
		},
	)

	formats := []string{"rfc3339", "date", "time", "unix", "human"}
	for _, format := range formats {
		output, _ := timeTool.Invoke(ctx, &interfaces.ToolInput{
			Args: map[string]interface{}{
				"format": format,
			},
			Context: ctx,
		})

		if output.Success {
			if result, ok := output.Result.(map[string]interface{}); ok {
				fmt.Printf("✓ %s: %v\n", format, result["time"])
			}
		}
	}
	fmt.Println()

	// 总结
	fmt.Println("╔════════════════════════════════════════════════════════════════╗")
	fmt.Println("║                        示例完成                                ║")
	fmt.Println("╚════════════════════════════════════════════════════════════════╝")
	fmt.Println()
	fmt.Println("本示例演示了自定义函数工具的创建方法:")
	fmt.Println("  ✓ NewFunctionTool - 快速创建简单工具")
	fmt.Println("  ✓ FunctionToolBuilder - 链式构建复杂工具")
	fmt.Println("  ✓ BaseTool - 完全控制输入输出")
	fmt.Println("  ✓ 带状态的工具（计数器示例）")
	fmt.Println("  ✓ 模拟外部 API（天气查询示例）")
	fmt.Println()
	fmt.Println("💡 最佳实践:")
	fmt.Println("  - 使用清晰的工具名称和描述")
	fmt.Println("  - 定义完整的 JSON Schema 参数")
	fmt.Println("  - 处理所有可能的错误情况")
	fmt.Println("  - 返回结构化的结果")
	fmt.Println()
	fmt.Println("更多工具示例请参考其他目录")
}
