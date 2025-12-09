// Package main 演示计算器工具的使用方法
// 本示例展示 CalculatorTool 和 AdvancedCalculatorTool 的基本用法
package main

import (
	"context"
	"fmt"
	"time"

	"github.com/kart-io/goagent/interfaces"
	"github.com/kart-io/goagent/tools/compute"
)

func main() {
	fmt.Println("╔════════════════════════════════════════════════════════════════╗")
	fmt.Println("║              计算器工具 (Calculator Tool) 示例                 ║")
	fmt.Println("║   展示基础计算器和高级计算器的使用方法                          ║")
	fmt.Println("╚════════════════════════════════════════════════════════════════╝")
	fmt.Println()

	// 创建上下文
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// 1. 基础计算器示例
	fmt.Println("【步骤 1】使用基础计算器 (CalculatorTool)")
	fmt.Println("────────────────────────────────────────")

	calculator := compute.NewCalculatorTool()
	fmt.Printf("工具名称: %s\n", calculator.Name())
	fmt.Printf("工具描述: %s\n", calculator.Description())
	fmt.Println()

	// 基本运算示例
	expressions := []string{
		"2 + 3",
		"10 - 4",
		"6 * 7",
		"20 / 4",
		"2^8",
		"(10 + 5) * 2",
		"100 / (2 + 3)",
		"2^3 + 4 * 5",
	}

	fmt.Println("基本运算测试:")
	for _, expr := range expressions {
		output, err := calculator.Invoke(ctx, &interfaces.ToolInput{
			Args: map[string]interface{}{
				"expression": expr,
			},
			Context: ctx,
		})
		if err != nil {
			fmt.Printf("  ✗ %s = 错误: %v\n", expr, err)
			continue
		}

		if output.Success {
			fmt.Printf("  ✓ %s = %v\n", expr, output.Result)
		} else {
			fmt.Printf("  ✗ %s = 失败: %s\n", expr, output.Error)
		}
	}
	fmt.Println()

	// 2. 高级计算器示例
	fmt.Println("【步骤 2】使用高级计算器 (AdvancedCalculatorTool)")
	fmt.Println("────────────────────────────────────────")

	advancedCalc := compute.NewAdvancedCalculatorTool(compute.CalculatorOperations{
		Add:      true,
		Subtract: true,
		Multiply: true,
		Divide:   true,
		Power:    true,
		Sqrt:     true,
		Abs:      true,
	})

	fmt.Printf("工具名称: %s\n", advancedCalc.Name())
	fmt.Println()

	// 高级运算示例
	operations := []struct {
		op       string
		operands []interface{}
		desc     string
	}{
		{"add", []interface{}{1.0, 2.0, 3.0, 4.0, 5.0}, "加法: 1+2+3+4+5"},
		{"subtract", []interface{}{100.0, 30.0, 20.0}, "减法: 100-30-20"},
		{"multiply", []interface{}{2.0, 3.0, 4.0}, "乘法: 2*3*4"},
		{"divide", []interface{}{100.0, 2.0, 5.0}, "除法: 100/2/5"},
		{"power", []interface{}{2.0, 10.0}, "幂运算: 2^10"},
		{"sqrt", []interface{}{144.0}, "平方根: √144"},
		{"abs", []interface{}{-42.0}, "绝对值: |-42|"},
		{"sin", []interface{}{0.0}, "正弦: sin(0)"},
		{"cos", []interface{}{0.0}, "余弦: cos(0)"},
		{"log", []interface{}{100.0}, "对数: log10(100)"},
		{"ln", []interface{}{2.718281828}, "自然对数: ln(e)"},
	}

	fmt.Println("高级运算测试:")
	for _, op := range operations {
		output, err := advancedCalc.Invoke(ctx, &interfaces.ToolInput{
			Args: map[string]interface{}{
				"operation": op.op,
				"operands":  op.operands,
			},
			Context: ctx,
		})
		if err != nil {
			fmt.Printf("  ✗ %s = 错误: %v\n", op.desc, err)
			continue
		}

		if output.Success {
			fmt.Printf("  ✓ %s = %v\n", op.desc, output.Result)
		} else {
			fmt.Printf("  ✗ %s = 失败: %s\n", op.desc, output.Error)
		}
	}
	fmt.Println()

	// 3. 错误处理示例
	fmt.Println("【步骤 3】错误处理示例")
	fmt.Println("────────────────────────────────────────")

	errorCases := []struct {
		expr string
		desc string
	}{
		{"10 / 0", "除零错误"},
		{"(1 + 2", "括号不匹配"},
		{"abc", "无效表达式"},
	}

	fmt.Println("错误处理测试:")
	for _, tc := range errorCases {
		output, err := calculator.Invoke(ctx, &interfaces.ToolInput{
			Args: map[string]interface{}{
				"expression": tc.expr,
			},
			Context: ctx,
		})

		if err != nil {
			fmt.Printf("  ✓ %s: 正确捕获错误 - %v\n", tc.desc, err)
		} else if !output.Success {
			fmt.Printf("  ✓ %s: 正确返回失败 - %s\n", tc.desc, output.Error)
		} else {
			fmt.Printf("  ✗ %s: 预期应失败，但返回了: %v\n", tc.desc, output.Result)
		}
	}
	fmt.Println()

	// 总结
	fmt.Println("╔════════════════════════════════════════════════════════════════╗")
	fmt.Println("║                        示例完成                                ║")
	fmt.Println("╚════════════════════════════════════════════════════════════════╝")
	fmt.Println()
	fmt.Println("本示例演示了计算器工具的核心功能:")
	fmt.Println("  ✓ 基础计算器 - 表达式求值（支持 +、-、*、/、^、括号）")
	fmt.Println("  ✓ 高级计算器 - 数学函数（sqrt、abs、sin、cos、tan、log、ln）")
	fmt.Println("  ✓ 错误处理 - 除零、括号不匹配、无效输入")
	fmt.Println()
	fmt.Println("更多工具示例请参考其他目录")
}
