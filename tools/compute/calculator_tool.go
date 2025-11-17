package compute

import (
	"context"
	"fmt"
	"math"
	"strconv"
	"strings"

	agentErrors "github.com/kart-io/goagent/errors"
	"github.com/kart-io/goagent/interfaces"
	"github.com/kart-io/goagent/tools"
)

// CalculatorTool 计算器工具
//
// 支持基本的数学运算
type CalculatorTool struct {
	*tools.BaseTool
}

// NewCalculatorTool 创建计算器工具
func NewCalculatorTool() *CalculatorTool {
	tool := &CalculatorTool{}
	tool.BaseTool = tools.NewBaseTool(
		"calculator",
		"Performs mathematical calculations. Supports basic arithmetic (+, -, *, /), power (^), and parentheses.",
		`{
			"type": "object",
			"properties": {
				"expression": {
					"type": "string",
					"description": "Mathematical expression to evaluate (e.g., '2 + 3 * 4', '(10 - 5) / 2', '2^8')"
				}
			},
			"required": ["expression"]
		}`,
		tool.run,
	)
	return tool
}

// run 执行计算
func (c *CalculatorTool) run(ctx context.Context, input *interfaces.ToolInput) (*interfaces.ToolOutput, error) {
	expression, ok := input.Args["expression"].(string)
	if !ok {
		return &interfaces.ToolOutput{
				Success: false,
				Error:   "expression is required and must be a string",
			}, tools.NewToolError(c.Name(), "invalid input", agentErrors.New(agentErrors.CodeInvalidInput, "expression is required").
				WithComponent("calculator_tool").
				WithOperation("run"))
	}

	// 清理表达式
	expression = strings.TrimSpace(expression)

	// 计算表达式
	result, err := evaluateExpression(expression)
	if err != nil {
		return &interfaces.ToolOutput{
			Success: false,
			Error:   err.Error(),
		}, tools.NewToolError(c.Name(), "calculation failed", err)
	}

	return &interfaces.ToolOutput{
		Result:  result,
		Success: true,
		Metadata: map[string]interface{}{
			"expression": expression,
			"result":     result,
		},
	}, nil
}

// evaluateExpression 计算数学表达式
// 简化实现：支持基本运算符和括号
// 运算符优先级：括号 > 幂 > 乘除 > 加减
func evaluateExpression(expr string) (float64, error) {
	// 移除空格
	expr = strings.ReplaceAll(expr, " ", "")

	// 处理括号
	for strings.Contains(expr, "(") {
		start := strings.LastIndex(expr, "(")
		end := strings.Index(expr[start:], ")")
		if end == -1 {
			return 0, agentErrors.New(agentErrors.CodeToolExecution, "mismatched parentheses").
				WithComponent("calculator_tool").
				WithOperation("evaluate_expression").
				WithContext("expression", expr)
		}
		end += start

		subExpr := expr[start+1 : end]
		subResult, err := evaluateExpression(subExpr)
		if err != nil {
			return 0, err
		}

		expr = expr[:start] + fmt.Sprintf("%v", subResult) + expr[end+1:]
	}

	// 处理加减（最低优先级，从右到左）
	for i := len(expr) - 1; i >= 0; i-- {
		if expr[i] == '+' || (expr[i] == '-' && i > 0 && expr[i-1] != '^' && expr[i-1] != '*' && expr[i-1] != '/') {
			op := string(expr[i])
			left, err := evaluateExpression(expr[:i])
			if err != nil {
				return 0, err
			}
			right, err := evaluateExpression(expr[i+1:])
			if err != nil {
				return 0, err
			}

			if op == "+" {
				return left + right, nil
			}
			return left - right, nil
		}
	}

	// 处理乘除（中等优先级，从右到左）
	for i := len(expr) - 1; i >= 0; i-- {
		if expr[i] == '*' || expr[i] == '/' {
			op := string(expr[i])
			left, err := evaluateExpression(expr[:i])
			if err != nil {
				return 0, err
			}
			right, err := evaluateExpression(expr[i+1:])
			if err != nil {
				return 0, err
			}

			if op == "*" {
				return left * right, nil
			}
			if right == 0 {
				return 0, agentErrors.New(agentErrors.CodeToolExecution, "division by zero").
					WithComponent("calculator_tool").
					WithOperation("evaluate_expression")
			}
			return left / right, nil
		}
	}

	// 处理幂运算（高优先级，从右到左）
	if idx := strings.LastIndex(expr, "^"); idx != -1 {
		base, err := evaluateExpression(expr[:idx])
		if err != nil {
			return 0, err
		}
		exp, err := evaluateExpression(expr[idx+1:])
		if err != nil {
			return 0, err
		}
		return math.Pow(base, exp), nil
	}

	// 解析数字
	num, err := strconv.ParseFloat(expr, 64)
	if err != nil {
		return 0, agentErrors.Wrap(err, agentErrors.CodeToolExecution, "invalid number").
			WithComponent("calculator_tool").
			WithOperation("evaluate_expression").
			WithContext("expression", expr)
	}

	return num, nil
}

// CalculatorOperations 支持的计算操作类型
type CalculatorOperations struct {
	Add      bool // 加法
	Subtract bool // 减法
	Multiply bool // 乘法
	Divide   bool // 除法
	Power    bool // 幂运算
	Sqrt     bool // 平方根
	Abs      bool // 绝对值
}

// AdvancedCalculatorTool 高级计算器工具
//
// 支持更多数学函数
type AdvancedCalculatorTool struct {
	*tools.BaseTool
	operations CalculatorOperations
}

// NewAdvancedCalculatorTool 创建高级计算器工具
func NewAdvancedCalculatorTool(operations CalculatorOperations) *AdvancedCalculatorTool {
	tool := &AdvancedCalculatorTool{
		operations: operations,
	}

	tool.BaseTool = tools.NewBaseTool(
		"advanced_calculator",
		"Performs advanced mathematical calculations including sqrt, abs, sin, cos, tan, log, etc.",
		`{
			"type": "object",
			"properties": {
				"operation": {
					"type": "string",
					"enum": ["add", "subtract", "multiply", "divide", "power", "sqrt", "abs", "sin", "cos", "tan", "log", "ln"],
					"description": "Mathematical operation to perform"
				},
				"operands": {
					"type": "array",
					"items": {"type": "number"},
					"description": "Operands for the operation"
				}
			},
			"required": ["operation", "operands"]
		}`,
		tool.run,
	)
	return tool
}

// run 执行高级计算
//
//nolint:gocyclo // High complexity is inherent due to multiple math operations
func (a *AdvancedCalculatorTool) run(ctx context.Context, input *interfaces.ToolInput) (*interfaces.ToolOutput, error) {
	operation, ok := input.Args["operation"].(string)
	if !ok {
		return &interfaces.ToolOutput{
				Success: false,
				Error:   "operation is required",
			}, tools.NewToolError(a.Name(), "invalid input", agentErrors.New(agentErrors.CodeInvalidInput, "operation is required").
				WithComponent("calculator_tool").
				WithOperation("advanced_run"))
	}

	operands, ok := input.Args["operands"].([]interface{})
	if !ok {
		return &interfaces.ToolOutput{
				Success: false,
				Error:   "operands must be an array",
			}, tools.NewToolError(a.Name(), "invalid input", agentErrors.New(agentErrors.CodeInvalidInput, "operands must be an array").
				WithComponent("calculator_tool").
				WithOperation("advanced_run"))
	}

	// 转换为 float64
	nums := make([]float64, len(operands))
	for i, op := range operands {
		switch v := op.(type) {
		case float64:
			nums[i] = v
		case int:
			nums[i] = float64(v)
		case string:
			f, err := strconv.ParseFloat(v, 64)
			if err != nil {
				return &interfaces.ToolOutput{
						Success: false,
						Error:   fmt.Sprintf("invalid operand: %v", v),
					}, tools.NewToolError(a.Name(), "invalid operand", agentErrors.Wrap(err, agentErrors.CodeInvalidInput, "invalid operand format").
						WithComponent("calculator_tool").
						WithOperation("advanced_run").
						WithContext("operand", v))
			}
			nums[i] = f
		default:
			return &interfaces.ToolOutput{
					Success: false,
					Error:   fmt.Sprintf("invalid operand type: %T", v),
				}, tools.NewToolError(a.Name(), "invalid operand type", agentErrors.New(agentErrors.CodeInvalidInput, "invalid operand type").
					WithComponent("calculator_tool").
					WithOperation("advanced_run").
					WithContext("type", fmt.Sprintf("%T", v)))
		}
	}

	// 执行操作
	var result float64
	var err error

	switch operation {
	case "add":
		result = 0
		for _, n := range nums {
			result += n
		}
	case "subtract":
		if len(nums) < 2 {
			return &interfaces.ToolOutput{Success: false, Error: "subtract requires at least 2 operands"}, nil
		}
		result = nums[0]
		for _, n := range nums[1:] {
			result -= n
		}
	case "multiply":
		result = 1
		for _, n := range nums {
			result *= n
		}
	case "divide":
		if len(nums) < 2 {
			return &interfaces.ToolOutput{Success: false, Error: "divide requires at least 2 operands"}, nil
		}
		result = nums[0]
		for _, n := range nums[1:] {
			if n == 0 {
				return &interfaces.ToolOutput{Success: false, Error: "division by zero"}, nil
			}
			result /= n
		}
	case "power":
		if len(nums) != 2 {
			return &interfaces.ToolOutput{Success: false, Error: "power requires exactly 2 operands"}, nil
		}
		result = math.Pow(nums[0], nums[1])
	case "sqrt":
		if len(nums) != 1 {
			return &interfaces.ToolOutput{Success: false, Error: "sqrt requires exactly 1 operand"}, nil
		}
		result = math.Sqrt(nums[0])
	case "abs":
		if len(nums) != 1 {
			return &interfaces.ToolOutput{Success: false, Error: "abs requires exactly 1 operand"}, nil
		}
		result = math.Abs(nums[0])
	case "sin":
		if len(nums) != 1 {
			return &interfaces.ToolOutput{Success: false, Error: "sin requires exactly 1 operand"}, nil
		}
		result = math.Sin(nums[0])
	case "cos":
		if len(nums) != 1 {
			return &interfaces.ToolOutput{Success: false, Error: "cos requires exactly 1 operand"}, nil
		}
		result = math.Cos(nums[0])
	case "tan":
		if len(nums) != 1 {
			return &interfaces.ToolOutput{Success: false, Error: "tan requires exactly 1 operand"}, nil
		}
		result = math.Tan(nums[0])
	case "log":
		if len(nums) != 1 {
			return &interfaces.ToolOutput{Success: false, Error: "log requires exactly 1 operand"}, nil
		}
		result = math.Log10(nums[0])
	case "ln":
		if len(nums) != 1 {
			return &interfaces.ToolOutput{Success: false, Error: "ln requires exactly 1 operand"}, nil
		}
		result = math.Log(nums[0])
	default:
		return &interfaces.ToolOutput{
				Success: false,
				Error:   fmt.Sprintf("unknown operation: %s", operation),
			}, tools.NewToolError(a.Name(), "unknown operation", agentErrors.New(agentErrors.CodeInvalidInput, "unknown operation").
				WithComponent("calculator_tool").
				WithOperation("advanced_run").
				WithContext("operation", operation))
	}

	return &interfaces.ToolOutput{
		Result:  result,
		Success: true,
		Metadata: map[string]interface{}{
			"operation": operation,
			"operands":  nums,
			"result":    result,
		},
	}, err
}
