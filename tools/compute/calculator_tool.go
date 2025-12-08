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
		tools.ToolCalculator,
		tools.DescCalculator,
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
	resp := tools.NewToolErrorResponse(c.Name()).WithOperation("run")

	expression, ok := input.Args["expression"].(string)
	if !ok {
		return resp.ValidationError("expression is required and must be a string", "field", "expression")
	}

	// 清理表达式
	expression = strings.TrimSpace(expression)

	// 计算表达式
	result, err := evaluateExpression(expression)
	if err != nil {
		return resp.ExecutionError("calculation failed", nil, err)
	}

	return resp.Success(result, map[string]interface{}{
		"expression": expression,
		"result":     result,
	})
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
	resp := tools.NewToolErrorResponse(a.Name()).WithOperation("advanced_run")

	operation, ok := input.Args["operation"].(string)
	if !ok {
		return resp.ValidationError("operation is required", "field", "operation")
	}

	operands, ok := input.Args["operands"].([]interface{})
	if !ok {
		return resp.ValidationError("operands must be an array", "field", "operands")
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
				return resp.ValidationError(fmt.Sprintf("invalid operand: %v", v), "operand", v, "index", i)
			}
			nums[i] = f
		default:
			return resp.ValidationError(fmt.Sprintf("invalid operand type: %T", v), "type", fmt.Sprintf("%T", v), "index", i)
		}
	}

	// 执行操作
	var result float64

	switch operation {
	case "add":
		result = 0
		for _, n := range nums {
			result += n
		}
	case "subtract":
		if len(nums) < 2 {
			return resp.ValidationError("subtract requires at least 2 operands", "operation", operation, "operand_count", len(nums))
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
			return resp.ValidationError("divide requires at least 2 operands", "operation", operation, "operand_count", len(nums))
		}
		result = nums[0]
		for _, n := range nums[1:] {
			if n == 0 {
				return resp.ExecutionError("division by zero", nil, nil)
			}
			result /= n
		}
	case "power":
		if len(nums) != 2 {
			return resp.ValidationError("power requires exactly 2 operands", "operation", operation, "operand_count", len(nums))
		}
		result = math.Pow(nums[0], nums[1])
	case "sqrt":
		if len(nums) != 1 {
			return resp.ValidationError("sqrt requires exactly 1 operand", "operation", operation, "operand_count", len(nums))
		}
		result = math.Sqrt(nums[0])
	case "abs":
		if len(nums) != 1 {
			return resp.ValidationError("abs requires exactly 1 operand", "operation", operation, "operand_count", len(nums))
		}
		result = math.Abs(nums[0])
	case "sin":
		if len(nums) != 1 {
			return resp.ValidationError("sin requires exactly 1 operand", "operation", operation, "operand_count", len(nums))
		}
		result = math.Sin(nums[0])
	case "cos":
		if len(nums) != 1 {
			return resp.ValidationError("cos requires exactly 1 operand", "operation", operation, "operand_count", len(nums))
		}
		result = math.Cos(nums[0])
	case "tan":
		if len(nums) != 1 {
			return resp.ValidationError("tan requires exactly 1 operand", "operation", operation, "operand_count", len(nums))
		}
		result = math.Tan(nums[0])
	case "log":
		if len(nums) != 1 {
			return resp.ValidationError("log requires exactly 1 operand", "operation", operation, "operand_count", len(nums))
		}
		result = math.Log10(nums[0])
	case "ln":
		if len(nums) != 1 {
			return resp.ValidationError("ln requires exactly 1 operand", "operation", operation, "operand_count", len(nums))
		}
		result = math.Log(nums[0])
	default:
		return resp.ValidationError(fmt.Sprintf("unknown operation: %s", operation), "operation", operation)
	}

	return resp.Success(result, map[string]interface{}{
		"operation": operation,
		"operands":  nums,
		"result":    result,
	})
}
