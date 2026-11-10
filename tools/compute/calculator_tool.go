package compute

import (
	"context"
	"fmt"
	"math"
	"strconv"
	"strings"

	"github.com/kart-io/goagent/codec/json"
	shared "github.com/kart-io/goagent/constants"
	agentErrors "github.com/kart-io/goagent/errors"
	"github.com/kart-io/goagent/interfaces"
	"github.com/kart-io/goagent/tools"
	"github.com/kart-io/goagent/tools/toolargs"
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
		shared.ToolCalculator,
		shared.DescCalculator,
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

// evaluateExpression 计算数学表达式：递归下降一次扫描，耗时与表达式长度成正比。
// 优先级：括号 > 幂（右结合）> 一元负号 > 乘除 > 加减。
func evaluateExpression(expr string) (float64, error) {
	p := &exprParser{src: strings.ReplaceAll(expr, " ", "")}
	v, err := p.sum()
	if err != nil {
		return 0, err
	}
	if p.pos < len(p.src) {
		if p.src[p.pos] == ')' {
			return 0, calcError("mismatched parentheses", p.src)
		}
		return 0, calcError("invalid number", p.src[p.pos:])
	}
	return v, nil
}

type exprParser struct {
	src   string
	pos   int
	depth int
}

func calcError(msg, expr string) error {
	return agentErrors.New(agentErrors.CodeToolExecution, msg).
		WithComponent("calculator_tool").
		WithOperation("evaluate_expression").
		WithContext("expression", expr)
}

func (p *exprParser) accept(c byte) bool {
	if p.pos < len(p.src) && p.src[p.pos] == c {
		p.pos++
		return true
	}
	return false
}

func (p *exprParser) sum() (float64, error) {
	v, err := p.product()
	for err == nil && p.pos < len(p.src) {
		var r float64
		switch {
		case p.accept('+'):
			if r, err = p.product(); err == nil {
				v += r
			}
		case p.accept('-'):
			if r, err = p.product(); err == nil {
				v -= r
			}
		default:
			return v, nil
		}
	}
	return v, err
}

func (p *exprParser) product() (float64, error) {
	v, err := p.unary()
	for err == nil && p.pos < len(p.src) {
		var r float64
		switch {
		case p.accept('*'):
			if r, err = p.unary(); err == nil {
				v *= r
			}
		case p.accept('/'):
			if r, err = p.unary(); err == nil {
				if r == 0 {
					return 0, calcError("division by zero", p.src)
				}
				v /= r
			}
		default:
			return v, nil
		}
	}
	return v, err
}

func (p *exprParser) unary() (float64, error) {
	if !p.accept('-') {
		return p.power()
	}
	if err := p.enter(); err != nil {
		return 0, err
	}
	defer p.leave()
	v, err := p.unary()
	return -v, err
}

func (p *exprParser) power() (float64, error) {
	base, err := p.primary()
	if err != nil || !p.accept('^') {
		return base, err
	}
	if err := p.enter(); err != nil {
		return 0, err
	}
	defer p.leave()
	exp, err := p.unary()
	return math.Pow(base, exp), err
}

func (p *exprParser) primary() (float64, error) {
	if p.accept('(') {
		if err := p.enter(); err != nil {
			return 0, err
		}
		defer p.leave()
		v, err := p.sum()
		if err != nil {
			return 0, err
		}
		if !p.accept(')') {
			return 0, calcError("mismatched parentheses", p.src)
		}
		return v, nil
	}
	start := p.pos
	for p.pos < len(p.src) {
		c := p.src[p.pos]
		exponentSign := (c == '+' || c == '-') && p.pos > start && (p.src[p.pos-1] == 'e' || p.src[p.pos-1] == 'E')
		numeric := c >= '0' && c <= '9' || c == '.' || c == 'e' || c == 'E' || exponentSign
		if !numeric {
			break
		}
		p.pos++
	}
	num, err := strconv.ParseFloat(p.src[start:p.pos], 64)
	if err != nil {
		return 0, agentErrors.Wrap(err, agentErrors.CodeToolExecution, "invalid number").
			WithComponent("calculator_tool").
			WithOperation("evaluate_expression").
			WithContext("expression", p.src[start:])
	}
	return num, nil
}

func (p *exprParser) enter() error {
	if p.depth++; p.depth > shared.CalculatorMaxNesting {
		return calcError("expression nesting too deep", p.src)
	}
	return nil
}

func (p *exprParser) leave() { p.depth-- }

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
		case json.Number:
			f, ok := toolargs.Float64(v)
			if !ok {
				return resp.ValidationError(fmt.Sprintf("invalid operand: %v", v), "operand", v, "index", i)
			}
			nums[i] = f
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
