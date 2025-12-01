package tools

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/kart-io/goagent/interfaces"
	"github.com/stretchr/testify/assert"
)

// mockValidatableTool 实现 ValidatableTool 接口的 mock 工具
type mockValidatableTool struct {
	*BaseTool
	validateFunc func(ctx context.Context, input *interfaces.ToolInput) error
}

func (m *mockValidatableTool) Validate(ctx context.Context, input *interfaces.ToolInput) error {
	if m.validateFunc != nil {
		return m.validateFunc(ctx, input)
	}
	return nil
}

// TestInputValidator_ValidateRequired 测试必需参数验证
func TestInputValidator_ValidateRequired(t *testing.T) {
	validator := NewInputValidator()

	tool := NewBaseTool(
		"test_tool",
		"Test tool",
		`{
			"type": "object",
			"properties": {
				"name": {"type": "string"},
				"age": {"type": "number"}
			},
			"required": ["name"]
		}`,
		nil,
	)

	tests := []struct {
		name    string
		args    map[string]interface{}
		wantErr bool
		errMsg  string
	}{
		{
			name:    "all required present",
			args:    map[string]interface{}{"name": "John", "age": 30},
			wantErr: false,
		},
		{
			name:    "only required present",
			args:    map[string]interface{}{"name": "John"},
			wantErr: false,
		},
		{
			name:    "required missing",
			args:    map[string]interface{}{"age": 30},
			wantErr: true,
			errMsg:  "required parameter is missing",
		},
		{
			name:    "empty args",
			args:    map[string]interface{}{},
			wantErr: true,
			errMsg:  "required parameter is missing",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			input := &interfaces.ToolInput{Args: tt.args}
			err := validator.Validate(context.Background(), tool, input)

			if tt.wantErr {
				assert.Error(t, err)
				if tt.errMsg != "" {
					assert.Contains(t, err.Error(), tt.errMsg)
				}
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

// TestInputValidator_ValidateTypes 测试类型验证
func TestInputValidator_ValidateTypes(t *testing.T) {
	validator := NewInputValidator()

	tool := NewBaseTool(
		"test_tool",
		"Test tool",
		`{
			"type": "object",
			"properties": {
				"name": {"type": "string"},
				"age": {"type": "integer"},
				"score": {"type": "number"},
				"active": {"type": "boolean"},
				"tags": {"type": "array"},
				"metadata": {"type": "object"}
			}
		}`,
		nil,
	)

	tests := []struct {
		name    string
		args    map[string]interface{}
		wantErr bool
		errMsg  string
	}{
		{
			name: "all types correct",
			args: map[string]interface{}{
				"name":     "John",
				"age":      30,
				"score":    95.5,
				"active":   true,
				"tags":     []string{"tag1", "tag2"},
				"metadata": map[string]interface{}{"key": "value"},
			},
			wantErr: false,
		},
		{
			name:    "string type error",
			args:    map[string]interface{}{"name": 123},
			wantErr: true,
			errMsg:  "must be string",
		},
		{
			name:    "integer type error",
			args:    map[string]interface{}{"age": "30"},
			wantErr: true,
			errMsg:  "must be number",
		},
		{
			name:    "boolean type error",
			args:    map[string]interface{}{"active": "true"},
			wantErr: true,
			errMsg:  "must be boolean",
		},
		{
			name:    "array type error",
			args:    map[string]interface{}{"tags": "tag1,tag2"},
			wantErr: true,
			errMsg:  "must be array",
		},
		{
			name:    "object type error",
			args:    map[string]interface{}{"metadata": "data"},
			wantErr: true,
			errMsg:  "must be object",
		},
		{
			name:    "float as integer error",
			args:    map[string]interface{}{"age": 30.5},
			wantErr: true,
			errMsg:  "must be integer",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			input := &interfaces.ToolInput{Args: tt.args}
			err := validator.Validate(context.Background(), tool, input)

			if tt.wantErr {
				assert.Error(t, err)
				if tt.errMsg != "" {
					assert.Contains(t, err.Error(), tt.errMsg)
				}
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

// TestInputValidator_StrictMode 测试严格模式
func TestInputValidator_StrictMode(t *testing.T) {
	tool := NewBaseTool(
		"test_tool",
		"Test tool",
		`{
			"type": "object",
			"properties": {
				"name": {"type": "string"}
			}
		}`,
		nil,
	)

	tests := []struct {
		name       string
		strictMode bool
		args       map[string]interface{}
		wantErr    bool
		errMsg     string
	}{
		{
			name:       "non-strict allows extra args",
			strictMode: false,
			args:       map[string]interface{}{"name": "John", "extra": "data"},
			wantErr:    false,
		},
		{
			name:       "strict rejects extra args",
			strictMode: true,
			args:       map[string]interface{}{"name": "John", "extra": "data"},
			wantErr:    true,
			errMsg:     "unexpected parameter not defined in schema",
		},
		{
			name:       "strict allows defined args",
			strictMode: true,
			args:       map[string]interface{}{"name": "John"},
			wantErr:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			validator := &InputValidator{
				StrictMode:       tt.strictMode,
				ValidateTypes:    true,
				ValidateRequired: true,
			}

			input := &interfaces.ToolInput{Args: tt.args}
			err := validator.Validate(context.Background(), tool, input)

			if tt.wantErr {
				assert.Error(t, err)
				if tt.errMsg != "" {
					assert.Contains(t, err.Error(), tt.errMsg)
				}
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

// TestInputValidator_CustomValidation 测试自定义验证
func TestInputValidator_CustomValidation(t *testing.T) {
	validator := NewInputValidator()

	baseTool := NewBaseTool(
		"test_tool",
		"Test tool",
		`{"type": "object", "properties": {"amount": {"type": "number"}}}`,
		nil,
	)

	tests := []struct {
		name         string
		validateFunc func(ctx context.Context, input *interfaces.ToolInput) error
		args         map[string]interface{}
		wantErr      bool
		errMsg       string
	}{
		{
			name: "custom validation passes",
			validateFunc: func(ctx context.Context, input *interfaces.ToolInput) error {
				if amount, ok := input.Args["amount"].(float64); ok && amount < 0 {
					return fmt.Errorf("amount must be non-negative")
				}
				return nil
			},
			args:    map[string]interface{}{"amount": 100.0},
			wantErr: false,
		},
		{
			name: "custom validation fails",
			validateFunc: func(ctx context.Context, input *interfaces.ToolInput) error {
				if amount, ok := input.Args["amount"].(float64); ok && amount < 0 {
					return fmt.Errorf("amount must be non-negative")
				}
				return nil
			},
			args:    map[string]interface{}{"amount": -50.0},
			wantErr: true,
			errMsg:  "amount must be non-negative",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tool := &mockValidatableTool{
				BaseTool:     baseTool,
				validateFunc: tt.validateFunc,
			}

			input := &interfaces.ToolInput{Args: tt.args}
			err := validator.Validate(context.Background(), tool, input)

			if tt.wantErr {
				assert.Error(t, err)
				if tt.errMsg != "" {
					assert.Contains(t, err.Error(), tt.errMsg)
				}
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

// TestInputValidator_NumericConstraints 测试数值约束
func TestInputValidator_NumericConstraints(t *testing.T) {
	validator := NewInputValidator()

	tool := NewBaseTool(
		"test_tool",
		"Test tool",
		`{
			"type": "object",
			"properties": {
				"age": {
					"type": "integer",
					"minimum": 0,
					"maximum": 150
				},
				"score": {
					"type": "number",
					"minimum": 0.0,
					"maximum": 100.0
				}
			}
		}`,
		nil,
	)

	tests := []struct {
		name    string
		args    map[string]interface{}
		wantErr bool
		errMsg  string
	}{
		{
			name:    "values within range",
			args:    map[string]interface{}{"age": 30, "score": 95.5},
			wantErr: false,
		},
		{
			name:    "age below minimum",
			args:    map[string]interface{}{"age": -1},
			wantErr: true,
			errMsg:  "must be >= minimum",
		},
		{
			name:    "age above maximum",
			args:    map[string]interface{}{"age": 200},
			wantErr: true,
			errMsg:  "must be <= maximum",
		},
		{
			name:    "score below minimum",
			args:    map[string]interface{}{"score": -0.1},
			wantErr: true,
			errMsg:  "must be >= minimum",
		},
		{
			name:    "score above maximum",
			args:    map[string]interface{}{"score": 100.1},
			wantErr: true,
			errMsg:  "must be <= maximum",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			input := &interfaces.ToolInput{Args: tt.args}
			err := validator.Validate(context.Background(), tool, input)

			if tt.wantErr {
				assert.Error(t, err)
				if tt.errMsg != "" {
					assert.Contains(t, err.Error(), tt.errMsg)
				}
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

// TestInputValidator_StringConstraints 测试字符串约束
func TestInputValidator_StringConstraints(t *testing.T) {
	validator := NewInputValidator()

	tool := NewBaseTool(
		"test_tool",
		"Test tool",
		`{
			"type": "object",
			"properties": {
				"username": {
					"type": "string",
					"minLength": 3,
					"maxLength": 20
				}
			}
		}`,
		nil,
	)

	tests := []struct {
		name    string
		args    map[string]interface{}
		wantErr bool
		errMsg  string
	}{
		{
			name:    "valid length",
			args:    map[string]interface{}{"username": "john_doe"},
			wantErr: false,
		},
		{
			name:    "too short",
			args:    map[string]interface{}{"username": "jo"},
			wantErr: true,
			errMsg:  "length must be at least minimum",
		},
		{
			name:    "too long",
			args:    map[string]interface{}{"username": "this_username_is_way_too_long"},
			wantErr: true,
			errMsg:  "length must be at most maximum",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			input := &interfaces.ToolInput{Args: tt.args}
			err := validator.Validate(context.Background(), tool, input)

			if tt.wantErr {
				assert.Error(t, err)
				if tt.errMsg != "" {
					assert.Contains(t, err.Error(), tt.errMsg)
				}
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

// TestInputValidator_NilInputs 测试空输入处理
func TestInputValidator_NilInputs(t *testing.T) {
	validator := NewInputValidator()

	tool := NewBaseTool("test_tool", "Test tool", `{}`, nil)

	t.Run("nil tool", func(t *testing.T) {
		input := &interfaces.ToolInput{Args: map[string]interface{}{}}
		err := validator.Validate(context.Background(), nil, input)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "tool cannot be nil")
	})

	t.Run("nil input", func(t *testing.T) {
		err := validator.Validate(context.Background(), tool, nil)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "input cannot be nil")
	})

	t.Run("nil args map", func(t *testing.T) {
		input := &interfaces.ToolInput{Args: nil}
		err := validator.Validate(context.Background(), tool, input)
		// nil args map 应该被接受（相当于空 map）
		assert.NoError(t, err)
	})
}

// TestInputValidator_EmptySchema 测试空 schema
func TestInputValidator_EmptySchema(t *testing.T) {
	validator := NewInputValidator()

	tool := NewBaseTool("test_tool", "Test tool", "", nil)

	input := &interfaces.ToolInput{Args: map[string]interface{}{"any": "value"}}
	err := validator.Validate(context.Background(), tool, input)

	// 空 schema 应该接受任何输入
	assert.NoError(t, err)
}

// TestValidateAndInvoke 测试便捷方法
func TestValidateAndInvoke(t *testing.T) {
	invoked := false
	runFunc := func(ctx context.Context, input *interfaces.ToolInput) (*interfaces.ToolOutput, error) {
		invoked = true
		return &interfaces.ToolOutput{
			Result:  "success",
			Success: true,
		}, nil
	}

	tool := NewBaseTool(
		"test_tool",
		"Test tool",
		`{
			"type": "object",
			"properties": {"name": {"type": "string"}},
			"required": ["name"]
		}`,
		runFunc,
	)

	t.Run("valid input invokes tool", func(t *testing.T) {
		invoked = false
		input := &interfaces.ToolInput{Args: map[string]interface{}{"name": "test"}}
		output, err := ValidateAndInvoke(context.Background(), tool, input)

		assert.NoError(t, err)
		assert.True(t, output.Success)
		assert.True(t, invoked)
	})

	t.Run("invalid input does not invoke tool", func(t *testing.T) {
		invoked = false
		input := &interfaces.ToolInput{Args: map[string]interface{}{}} // missing required 'name'
		output, err := ValidateAndInvoke(context.Background(), tool, input)

		assert.Error(t, err)
		assert.False(t, output.Success)
		assert.False(t, invoked)
	})
}

// ===== P1-1 补充测试用例 =====

// TestInputValidator_NestedObjectValidation 测试深层嵌套对象验证
// 注意：当前验证器只验证顶层 required 字段，嵌套 required 不被验证
func TestInputValidator_NestedObjectValidation(t *testing.T) {
	validator := NewInputValidator()

	// 3 层嵌套对象的 JSON Schema
	schema := `{
		"type": "object",
		"properties": {
			"user": {
				"type": "object",
				"properties": {
					"profile": {
						"type": "object",
						"properties": {
							"name": {"type": "string"},
							"age": {"type": "integer"}
						},
						"required": ["name"]
					}
				},
				"required": ["profile"]
			}
		},
		"required": ["user"]
	}`

	tool := NewBaseTool("test", "Test nested validation", schema, nil)

	tests := []struct {
		name    string
		args    map[string]interface{}
		wantErr bool
	}{
		{
			name: "valid nested structure",
			args: map[string]interface{}{
				"user": map[string]interface{}{
					"profile": map[string]interface{}{
						"name": "John",
						"age":  30,
					},
				},
			},
			wantErr: false,
		},
		{
			name: "nested object with correct types",
			args: map[string]interface{}{
				"user": map[string]interface{}{
					"profile": map[string]interface{}{
						"age": 30,
					},
				},
			},
			// 注意：当前验证器不递归验证嵌套 required，只验证顶层
			wantErr: false,
		},
		{
			name: "nested object empty middle level",
			args: map[string]interface{}{
				"user": map[string]interface{}{},
			},
			// 注意：当前验证器不递归验证嵌套 required
			wantErr: false,
		},
		{
			name:    "missing top level required",
			args:    map[string]interface{}{},
			wantErr: true, // user is required - 顶层验证生效
		},
		{
			name: "wrong type for nested object",
			args: map[string]interface{}{
				"user": "not an object",
			},
			wantErr: true, // 类型验证生效
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			input := &interfaces.ToolInput{Args: tt.args}
			err := validator.Validate(context.Background(), tool, input)

			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

// TestInputValidator_LargeArrayPerformance 测试大型数组验证性能
func TestInputValidator_LargeArrayPerformance(t *testing.T) {
	validator := NewInputValidator()

	schema := `{
		"type": "object",
		"properties": {
			"items": {
				"type": "array",
				"items": {"type": "string"}
			}
		},
		"required": ["items"]
	}`

	tool := NewBaseTool("test", "Test array performance", schema, nil)

	// 生成 10,000 元素的数组
	largeArray := make([]interface{}, 10000)
	for i := 0; i < 10000; i++ {
		largeArray[i] = fmt.Sprintf("item_%d", i)
	}

	input := &interfaces.ToolInput{
		Args: map[string]interface{}{
			"items": largeArray,
		},
	}

	start := time.Now()
	err := validator.Validate(context.Background(), tool, input)
	duration := time.Since(start)

	assert.NoError(t, err)
	// 验证性能不差于 100ms
	assert.Less(t, duration, 100*time.Millisecond,
		"large array validation should complete within 100ms, took: %v", duration)

	t.Logf("Large array validation (10,000 items) took: %v", duration)
}

// TestInputValidator_Concurrent 测试并发验证的线程安全性
func TestInputValidator_Concurrent(t *testing.T) {
	validator := NewInputValidator()

	schema := `{
		"type": "object",
		"properties": {
			"value": {"type": "string"}
		},
		"required": ["value"]
	}`

	tool := NewBaseTool("test", "Test concurrent validation", schema, nil)

	numGoroutines := 100
	var wg sync.WaitGroup
	errors := make([]error, numGoroutines)

	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()

			input := &interfaces.ToolInput{
				Args: map[string]interface{}{
					"value": fmt.Sprintf("value_%d", idx),
				},
			}

			errors[idx] = validator.Validate(context.Background(), tool, input)
		}(i)
	}

	wg.Wait()

	// 所有验证应该成功
	for i, err := range errors {
		assert.NoError(t, err, "validation failed for goroutine %d", i)
	}
}

// TestInputValidator_ErrorRecovery 测试验证器错误恢复能力
func TestInputValidator_ErrorRecovery(t *testing.T) {
	validator := NewInputValidator()

	schema := `{
		"type": "object",
		"properties": {
			"value": {"type": "integer"}
		}
	}`

	tool := NewBaseTool("test", "Test error recovery", schema, nil)

	tests := []struct {
		name    string
		args    map[string]interface{}
		wantErr bool
	}{
		{
			name:    "first validation succeeds",
			args:    map[string]interface{}{"value": 10},
			wantErr: false,
		},
		{
			name:    "second validation fails",
			args:    map[string]interface{}{"value": "invalid"},
			wantErr: true,
		},
		{
			name:    "third validation succeeds after failure",
			args:    map[string]interface{}{"value": 20},
			wantErr: false,
		},
		{
			name:    "fourth validation fails again",
			args:    map[string]interface{}{"value": 3.14},
			wantErr: true, // float is not integer
		},
		{
			name:    "fifth validation succeeds",
			args:    map[string]interface{}{"value": 100},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			input := &interfaces.ToolInput{Args: tt.args}
			err := validator.Validate(context.Background(), tool, input)

			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}
