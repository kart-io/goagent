package compute

import (
	"context"
	"math"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kart-io/goagent/interfaces"
)

// TestNewCalculatorTool tests calculator tool creation
func TestNewCalculatorTool(t *testing.T) {
	calc := NewCalculatorTool()

	assert.NotNil(t, calc)
	assert.NotNil(t, calc.BaseTool)
	assert.Equal(t, "calculator", calc.Name())
	assert.Contains(t, calc.Description(), "mathematical calculations")
	assert.NotNil(t, calc.ArgsSchema())
}

// TestCalculatorTool_BasicArithmetic tests basic arithmetic operations
func TestCalculatorTool_BasicArithmetic(t *testing.T) {
	calc := NewCalculatorTool()
	ctx := context.Background()

	tests := []struct {
		name       string
		expression string
		expected   float64
	}{
		{"Addition", "2 + 3", 5.0},
		{"Subtraction", "10 - 4", 6.0},
		{"Multiplication", "3 * 4", 12.0},
		{"Division", "20 / 5", 4.0},
		{"Complex", "2 + 3 * 4", 14.0},
		{"Order of operations", "10 - 2 * 3", 4.0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			output, err := calc.Invoke(ctx, &interfaces.ToolInput{
				Args: map[string]interface{}{
					"expression": tt.expression,
				},
				Context: ctx,
			})

			require.NoError(t, err)
			assert.True(t, output.Success)
			assert.InDelta(t, tt.expected, output.Result.(float64), 0.0001)
		})
	}
}

// TestCalculatorTool_Parentheses tests parentheses handling
func TestCalculatorTool_Parentheses(t *testing.T) {
	calc := NewCalculatorTool()
	ctx := context.Background()

	tests := []struct {
		name       string
		expression string
		expected   float64
	}{
		{"Simple parentheses", "(2 + 3) * 4", 20.0},
		{"Nested parentheses", "((2 + 3) * 4) - 10", 10.0},
		{"Multiple groups", "(10 - 5) / (2 + 3)", 1.0},
		{"Complex", "2 * (3 + 4) - (5 - 1)", 10.0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			output, err := calc.Invoke(ctx, &interfaces.ToolInput{
				Args: map[string]interface{}{
					"expression": tt.expression,
				},
				Context: ctx,
			})

			require.NoError(t, err)
			assert.True(t, output.Success)
			assert.InDelta(t, tt.expected, output.Result.(float64), 0.0001)
		})
	}
}

// TestCalculatorTool_PowerOperations tests power operations
func TestCalculatorTool_PowerOperations(t *testing.T) {
	calc := NewCalculatorTool()
	ctx := context.Background()

	tests := []struct {
		name       string
		expression string
		expected   float64
	}{
		{"Simple power", "2^3", 8.0},
		{"Power with addition", "2 + 2^3", 10.0},
		{"Power with multiplication", "2 * 2^3", 16.0},
		{"Nested power", "2^2^2", 16.0}, // Right associative: 2^(2^2)
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			output, err := calc.Invoke(ctx, &interfaces.ToolInput{
				Args: map[string]interface{}{
					"expression": tt.expression,
				},
				Context: ctx,
			})

			require.NoError(t, err)
			assert.True(t, output.Success)
			assert.InDelta(t, tt.expected, output.Result.(float64), 0.0001)
		})
	}
}

// TestCalculatorTool_ErrorCases tests error handling
func TestCalculatorTool_ErrorCases(t *testing.T) {
	calc := NewCalculatorTool()
	ctx := context.Background()

	tests := []struct {
		name           string
		args           map[string]interface{}
		expectedErrMsg string
	}{
		{
			name:           "Missing expression",
			args:           map[string]interface{}{},
			expectedErrMsg: "expression is required",
		},
		{
			name: "Invalid expression type",
			args: map[string]interface{}{
				"expression": 123,
			},
			expectedErrMsg: "expression is required",
		},
		{
			name: "Division by zero",
			args: map[string]interface{}{
				"expression": "10 / 0",
			},
			expectedErrMsg: "division by zero",
		},
		{
			name: "Mismatched parentheses",
			args: map[string]interface{}{
				"expression": "(2 + 3",
			},
			expectedErrMsg: "mismatched parentheses",
		},
		{
			name: "Invalid number",
			args: map[string]interface{}{
				"expression": "abc",
			},
			expectedErrMsg: "invalid number",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			output, err := calc.Invoke(ctx, &interfaces.ToolInput{
				Args:    tt.args,
				Context: ctx,
			})

			require.Error(t, err)
			assert.False(t, output.Success)
			assert.Contains(t, output.Error, tt.expectedErrMsg)
		})
	}
}

// TestCalculatorTool_WhitespaceHandling tests whitespace handling
func TestCalculatorTool_WhitespaceHandling(t *testing.T) {
	calc := NewCalculatorTool()
	ctx := context.Background()

	tests := []struct {
		expression string
		expected   float64
	}{
		{"  2+3  ", 5.0},
		{"2  +  3", 5.0},
		{" (  2 + 3  ) * 4 ", 20.0},
	}

	for _, tt := range tests {
		t.Run(tt.expression, func(t *testing.T) {
			output, err := calc.Invoke(ctx, &interfaces.ToolInput{
				Args: map[string]interface{}{
					"expression": tt.expression,
				},
				Context: ctx,
			})

			require.NoError(t, err)
			assert.True(t, output.Success)
			assert.InDelta(t, tt.expected, output.Result.(float64), 0.0001)
		})
	}
}

// TestAdvancedCalculatorTool tests advanced calculator tool
func TestAdvancedCalculatorTool_Creation(t *testing.T) {
	ops := CalculatorOperations{
		Add:      true,
		Subtract: true,
		Multiply: true,
		Divide:   true,
		Power:    true,
		Sqrt:     true,
		Abs:      true,
	}

	calc := NewAdvancedCalculatorTool(ops)

	assert.NotNil(t, calc)
	assert.NotNil(t, calc.BaseTool)
	assert.Equal(t, "advanced_calculator", calc.Name())
	assert.Contains(t, calc.Description(), "advanced mathematical")
}

// TestAdvancedCalculatorTool_BasicOperations tests basic operations
func TestAdvancedCalculatorTool_BasicOperations(t *testing.T) {
	ops := CalculatorOperations{Add: true, Subtract: true, Multiply: true, Divide: true}
	calc := NewAdvancedCalculatorTool(ops)
	ctx := context.Background()

	tests := []struct {
		name      string
		operation string
		operands  []interface{}
		expected  float64
	}{
		{"Add two numbers", "add", []interface{}{2.0, 3.0}, 5.0},
		{"Add multiple numbers", "add", []interface{}{1.0, 2.0, 3.0, 4.0}, 10.0},
		{"Subtract", "subtract", []interface{}{10.0, 3.0}, 7.0},
		{"Multiply", "multiply", []interface{}{2.0, 3.0, 4.0}, 24.0},
		{"Divide", "divide", []interface{}{20.0, 2.0, 2.0}, 5.0},
		{"Power", "power", []interface{}{2.0, 8.0}, 256.0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			output, err := calc.Invoke(ctx, &interfaces.ToolInput{
				Args: map[string]interface{}{
					"operation": tt.operation,
					"operands":  tt.operands,
				},
				Context: ctx,
			})

			require.NoError(t, err)
			assert.True(t, output.Success)
			assert.InDelta(t, tt.expected, output.Result.(float64), 0.0001)
		})
	}
}

// TestAdvancedCalculatorTool_MathFunctions tests mathematical functions
func TestAdvancedCalculatorTool_MathFunctions(t *testing.T) {
	ops := CalculatorOperations{Sqrt: true, Abs: true}
	calc := NewAdvancedCalculatorTool(ops)
	ctx := context.Background()

	tests := []struct {
		name      string
		operation string
		operands  []interface{}
		expected  float64
	}{
		{"Square root", "sqrt", []interface{}{16.0}, 4.0},
		{"Absolute value positive", "abs", []interface{}{5.0}, 5.0},
		{"Absolute value negative", "abs", []interface{}{-5.0}, 5.0},
		{"Sin", "sin", []interface{}{0.0}, 0.0},
		{"Cos", "cos", []interface{}{0.0}, 1.0},
		{"Log10", "log", []interface{}{100.0}, 2.0},
		{"Natural log", "ln", []interface{}{math.E}, 1.0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			output, err := calc.Invoke(ctx, &interfaces.ToolInput{
				Args: map[string]interface{}{
					"operation": tt.operation,
					"operands":  tt.operands,
				},
				Context: ctx,
			})

			require.NoError(t, err)
			assert.True(t, output.Success)
			assert.InDelta(t, tt.expected, output.Result.(float64), 0.0001)
		})
	}
}

// TestAdvancedCalculatorTool_OperandConversion tests operand type conversion
func TestAdvancedCalculatorTool_OperandConversion(t *testing.T) {
	ops := CalculatorOperations{Add: true}
	calc := NewAdvancedCalculatorTool(ops)
	ctx := context.Background()

	tests := []struct {
		name     string
		operands []interface{}
		expected float64
	}{
		{"Float64 operands", []interface{}{2.5, 3.5}, 6.0},
		{"Int operands", []interface{}{2, 3}, 5.0},
		{"String operands", []interface{}{"2.5", "3.5"}, 6.0},
		{"Mixed operands", []interface{}{2, 3.5, "1.5"}, 7.0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			output, err := calc.Invoke(ctx, &interfaces.ToolInput{
				Args: map[string]interface{}{
					"operation": "add",
					"operands":  tt.operands,
				},
				Context: ctx,
			})

			require.NoError(t, err)
			assert.True(t, output.Success)
			assert.InDelta(t, tt.expected, output.Result.(float64), 0.0001)
		})
	}
}

// TestAdvancedCalculatorTool_ErrorCases tests error handling
func TestAdvancedCalculatorTool_ErrorCases(t *testing.T) {
	ops := CalculatorOperations{Add: true, Divide: true, Sqrt: true}
	calc := NewAdvancedCalculatorTool(ops)
	ctx := context.Background()

	tests := []struct {
		name           string
		args           map[string]interface{}
		expectedErrMsg string
	}{
		{
			name:           "Missing operation",
			args:           map[string]interface{}{"operands": []interface{}{1.0, 2.0}},
			expectedErrMsg: "operation is required",
		},
		{
			name:           "Missing operands",
			args:           map[string]interface{}{"operation": "add"},
			expectedErrMsg: "operands must be an array",
		},
		{
			name: "Unknown operation",
			args: map[string]interface{}{
				"operation": "unknown",
				"operands":  []interface{}{1.0},
			},
			expectedErrMsg: "unknown operation",
		},
		{
			name: "Division by zero",
			args: map[string]interface{}{
				"operation": "divide",
				"operands":  []interface{}{10.0, 0.0},
			},
			expectedErrMsg: "division by zero",
		},
		{
			name: "Wrong operand count for sqrt",
			args: map[string]interface{}{
				"operation": "sqrt",
				"operands":  []interface{}{1.0, 2.0},
			},
			expectedErrMsg: "sqrt requires exactly 1 operand",
		},
		{
			name: "Wrong operand count for power",
			args: map[string]interface{}{
				"operation": "power",
				"operands":  []interface{}{2.0},
			},
			expectedErrMsg: "power requires exactly 2 operands",
		},
		{
			name: "Invalid operand type",
			args: map[string]interface{}{
				"operation": "add",
				"operands":  []interface{}{true},
			},
			expectedErrMsg: "invalid operand type",
		},
		{
			name: "Invalid string operand",
			args: map[string]interface{}{
				"operation": "add",
				"operands":  []interface{}{"abc"},
			},
			expectedErrMsg: "invalid operand",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			output, err := calc.Invoke(ctx, &interfaces.ToolInput{
				Args:    tt.args,
				Context: ctx,
			})

			if err == nil {
				// Some errors are returned in output, not as error
				assert.False(t, output.Success)
				assert.Contains(t, output.Error, tt.expectedErrMsg)
			} else {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.expectedErrMsg)
			}
		})
	}
}

// TestAdvancedCalculatorTool_Metadata tests metadata in output
func TestAdvancedCalculatorTool_Metadata(t *testing.T) {
	ops := CalculatorOperations{Add: true}
	calc := NewAdvancedCalculatorTool(ops)
	ctx := context.Background()

	output, err := calc.Invoke(ctx, &interfaces.ToolInput{
		Args: map[string]interface{}{
			"operation": "add",
			"operands":  []interface{}{2.0, 3.0},
		},
		Context: ctx,
	})

	require.NoError(t, err)
	assert.True(t, output.Success)

	// Check metadata
	assert.NotNil(t, output.Metadata)
	assert.Equal(t, "add", output.Metadata["operation"])
	assert.NotNil(t, output.Metadata["operands"])
	assert.Equal(t, 5.0, output.Metadata["result"])
}

// BenchmarkCalculatorTool benchmarks calculator performance
func BenchmarkCalculatorTool(b *testing.B) {
	calc := NewCalculatorTool()
	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = calc.Invoke(ctx, &interfaces.ToolInput{
			Args: map[string]interface{}{
				"expression": "(2 + 3) * 4 - 5 / 2",
			},
			Context: ctx,
		})
	}
}

// BenchmarkAdvancedCalculatorTool benchmarks advanced calculator
func BenchmarkAdvancedCalculatorTool(b *testing.B) {
	ops := CalculatorOperations{Add: true, Multiply: true}
	calc := NewAdvancedCalculatorTool(ops)
	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = calc.Invoke(ctx, &interfaces.ToolInput{
			Args: map[string]interface{}{
				"operation": "add",
				"operands":  []interface{}{1.0, 2.0, 3.0, 4.0, 5.0},
			},
			Context: ctx,
		})
	}
}
