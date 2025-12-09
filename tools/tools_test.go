package tools

import (
	"context"
	"testing"

	agentErrors "github.com/kart-io/goagent/errors"
	"github.com/kart-io/goagent/interfaces"
	"github.com/kart-io/goagent/utils/json"
)

// TestBaseTool 测试基础工具
func TestBaseTool(t *testing.T) {
	tool := NewBaseTool(
		"test_tool",
		"A test tool",
		`{"type": "object"}`,
		func(ctx context.Context, input *interfaces.ToolInput) (*interfaces.ToolOutput, error) {
			return &interfaces.ToolOutput{
				Result:  "test result",
				Success: true,
			}, nil
		},
	)

	if tool.Name() != "test_tool" {
		t.Errorf("Expected name 'test_tool', got '%s'", tool.Name())
	}

	if tool.Description() != "A test tool" {
		t.Errorf("Expected description 'A test tool', got '%s'", tool.Description())
	}

	ctx := context.Background()
	input := &interfaces.ToolInput{
		Args:    map[string]interface{}{},
		Context: ctx,
	}

	output, err := tool.Invoke(ctx, input)
	if err != nil {
		t.Fatalf("Invoke failed: %v", err)
	}

	if !output.Success {
		t.Error("Expected success=true")
	}

	if output.Result != "test result" {
		t.Errorf("Expected result 'test result', got '%v'", output.Result)
	}
}

// TestFunctionTool 测试函数工具
func TestFunctionTool(t *testing.T) {
	tool := NewFunctionTool(
		"adder",
		"Adds two numbers",
		`{"type": "object", "properties": {"a": {"type": "number"}, "b": {"type": "number"}}}`,
		func(ctx context.Context, args map[string]interface{}) (interface{}, error) {
			a := args["a"].(float64)
			b := args["b"].(float64)
			return a + b, nil
		},
	)

	ctx := context.Background()
	input := &interfaces.ToolInput{
		Args: map[string]interface{}{
			"a": 5.0,
			"b": 3.0,
		},
		Context: ctx,
	}

	output, err := tool.Invoke(ctx, input)
	if err != nil {
		t.Fatalf("Invoke failed: %v", err)
	}

	if !output.Success {
		t.Error("Expected success=true")
	}

	if result, ok := output.Result.(float64); !ok || result != 8.0 {
		t.Errorf("Expected result 8.0, got %v", output.Result)
	}
}

// TestToolWithCallbacks 测试工具回调
// NOTE: This test is disabled because WithCallbacks is no longer part of the simplified interfaces.Tool interface
/*
func TestToolWithCallbacks(t *testing.T) {
	var callbackExecuted bool

	callback := &testCallback{
		onToolStart: func(ctx context.Context, toolName string, input interface{}) error {
			callbackExecuted = true
			if toolName != "test_tool" {
				t.Errorf("Expected toolName 'test_tool', got '%s'", toolName)
			}
			return nil
		},
	}

	tool := NewBaseTool(
		"test_tool",
		"A test tool",
		`{}`,
		func(ctx context.Context, input *interfaces.ToolInput) (*interfaces.ToolOutput, error) {
			return &interfaces.ToolOutput{Success: true}, nil
		},
	)

	toolWithCallback := tool.WithCallbacks(callback).(interfaces.Tool)

	ctx := context.Background()
	input := &interfaces.ToolInput{
		Args:    map[string]interface{}{},
		Context: ctx,
	}

	_, err := toolWithCallback.Invoke(ctx, input)
	if err != nil {
		t.Fatalf("Invoke failed: %v", err)
	}

	if !callbackExecuted {
		t.Error("Callback was not executed")
	}
}
*/

// TestBasicToolInvocation tests basic tool invocation without callbacks
func TestBasicToolInvocation(t *testing.T) {
	tool := NewBaseTool(
		"test_tool",
		"A test tool",
		`{}`,
		func(ctx context.Context, input *interfaces.ToolInput) (*interfaces.ToolOutput, error) {
			return &interfaces.ToolOutput{
				Success: true,
				Result:  "tool executed",
			}, nil
		},
	)

	ctx := context.Background()
	input := &interfaces.ToolInput{
		Args:    map[string]interface{}{},
		Context: ctx,
	}

	output, err := tool.Invoke(ctx, input)
	if err != nil {
		t.Fatalf("Invoke failed: %v", err)
	}

	if !output.Success {
		t.Error("Expected success=true")
	}

	if output.Result != "tool executed" {
		t.Errorf("Expected result 'tool executed', got '%v'", output.Result)
	}
}

// testCallback 测试回调实现
// NOTE: Commented out - no longer used after interface simplification
/*
type testCallback struct {
	agentcore.BaseCallback
	onToolStart func(ctx context.Context, toolName string, input interface{}) error
	onToolEnd   func(ctx context.Context, toolName string, output interface{}) error
	onToolError func(ctx context.Context, toolName string, err error) error
}

func (t *testCallback) OnToolStart(ctx context.Context, toolName string, input interface{}) error {
	if t.onToolStart != nil {
		return t.onToolStart(ctx, toolName, input)
	}
	return nil
}

func (t *testCallback) OnToolEnd(ctx context.Context, toolName string, output interface{}) error {
	if t.onToolEnd != nil {
		return t.onToolEnd(ctx, toolName, output)
	}
	return nil
}

func (t *testCallback) OnToolError(ctx context.Context, toolName string, err error) error {
	if t.onToolError != nil {
		return t.onToolError(ctx, toolName, err)
	}
	return nil
}
*/

// BenchmarkFunctionTool 性能测试
func BenchmarkFunctionTool(b *testing.B) {
	tool := NewFunctionTool(
		"adder",
		"Adds numbers",
		`{}`,
		func(ctx context.Context, args map[string]interface{}) (interface{}, error) {
			a := args["a"].(float64)
			b := args["b"].(float64)
			return a + b, nil
		},
	)

	ctx := context.Background()
	input := &interfaces.ToolInput{
		Args: map[string]interface{}{
			"a": 5.0,
			"b": 3.0,
		},
		Context: ctx,
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = tool.Invoke(ctx, input)
	}
}

// TestGenerateJSONSchemaFromStruct 测试从结构体生成 JSON Schema
func TestGenerateJSONSchemaFromStruct(t *testing.T) {
	// 测试用例 1: 基础结构体
	t.Run("basic struct", func(t *testing.T) {
		type TestStruct struct {
			Name  string `json:"name"`
			Age   int    `json:"age"`
			Email string `json:"email"`
		}

		schema := generateJSONSchemaFromStruct(TestStruct{})

		// 验证 schema 不为空
		if schema == "" {
			t.Error("Expected non-empty schema")
		}

		// 解析 schema 验证结构
		var schemaMap map[string]interface{}
		if err := json.Unmarshal([]byte(schema), &schemaMap); err != nil {
			t.Fatalf("Failed to parse schema: %v", err)
		}

		// 验证 type
		if schemaMap["type"] != "object" {
			t.Errorf("Expected type 'object', got %v", schemaMap["type"])
		}

		// 验证 properties
		properties, ok := schemaMap["properties"].(map[string]interface{})
		if !ok {
			t.Fatal("Expected properties to be a map")
		}

		if len(properties) != 3 {
			t.Errorf("Expected 3 properties, got %d", len(properties))
		}

		// 验证 name 字段
		nameProp, ok := properties["name"].(map[string]interface{})
		if !ok {
			t.Fatal("Expected name property to be a map")
		}
		if nameProp["type"] != "string" {
			t.Errorf("Expected name type 'string', got %v", nameProp["type"])
		}

		// 验证 age 字段
		ageProp, ok := properties["age"].(map[string]interface{})
		if !ok {
			t.Fatal("Expected age property to be a map")
		}
		if ageProp["type"] != "integer" {
			t.Errorf("Expected age type 'integer', got %v", ageProp["type"])
		}

		// 验证 required 字段
		required, ok := schemaMap["required"].([]interface{})
		if !ok {
			t.Fatal("Expected required to be an array")
		}
		if len(required) != 3 {
			t.Errorf("Expected 3 required fields, got %d", len(required))
		}
	})

	// 测试用例 2: 包含可选字段的结构体
	t.Run("struct with optional fields", func(t *testing.T) {
		type TestStruct struct {
			Name     string  `json:"name"`
			Age      int     `json:"age,omitempty"`
			Email    *string `json:"email"`
			Optional string  `json:"optional,omitempty"`
		}

		schema := generateJSONSchemaFromStruct(TestStruct{})

		var schemaMap map[string]interface{}
		if err := json.Unmarshal([]byte(schema), &schemaMap); err != nil {
			t.Fatalf("Failed to parse schema: %v", err)
		}

		// name 应该是必需的（非指针，没有 omitempty）
		// age 不应该是必需的（有 omitempty）
		// email 不应该是必需的（指针类型）
		// optional 不应该是必需的（有 omitempty）
		required, ok := schemaMap["required"].([]interface{})
		if !ok {
			t.Fatal("Expected required to be an array")
		}

		// 只有 name 应该是必需的
		if len(required) != 1 {
			t.Errorf("Expected 1 required field, got %d", len(required))
		}
		if required[0] != "name" {
			t.Errorf("Expected required field 'name', got %v", required[0])
		}
	})

	// 测试用例 3: 带描述的结构体
	t.Run("struct with descriptions", func(t *testing.T) {
		type TestStruct struct {
			Name  string `json:"name" description:"用户名称"`
			Age   int    `json:"age" description:"用户年龄"`
			Email string `json:"email" description:"电子邮件地址"`
		}

		schema := generateJSONSchemaFromStruct(TestStruct{})

		var schemaMap map[string]interface{}
		if err := json.Unmarshal([]byte(schema), &schemaMap); err != nil {
			t.Fatalf("Failed to parse schema: %v", err)
		}

		properties, ok := schemaMap["properties"].(map[string]interface{})
		if !ok {
			t.Fatal("Expected properties to be a map")
		}

		// 验证描述字段
		nameProp, ok := properties["name"].(map[string]interface{})
		if !ok {
			t.Fatal("Expected name property to be a map")
		}
		if nameProp["description"] != "用户名称" {
			t.Errorf("Expected description '用户名称', got %v", nameProp["description"])
		}
	})

	// 测试用例 4: 各种类型的字段
	t.Run("struct with various types", func(t *testing.T) {
		type TestStruct struct {
			StringField string            `json:"string_field"`
			IntField    int               `json:"int_field"`
			FloatField  float64           `json:"float_field"`
			BoolField   bool              `json:"bool_field"`
			ArrayField  []string          `json:"array_field"`
			ObjectField map[string]string `json:"object_field"`
		}

		schema := generateJSONSchemaFromStruct(TestStruct{})

		var schemaMap map[string]interface{}
		if err := json.Unmarshal([]byte(schema), &schemaMap); err != nil {
			t.Fatalf("Failed to parse schema: %v", err)
		}

		properties, ok := schemaMap["properties"].(map[string]interface{})
		if !ok {
			t.Fatal("Expected properties to be a map")
		}

		// 验证各种类型
		tests := []struct {
			fieldName    string
			expectedType string
		}{
			{"string_field", "string"},
			{"int_field", "integer"},
			{"float_field", "number"},
			{"bool_field", "boolean"},
			{"array_field", "array"},
			{"object_field", "object"},
		}

		for _, tt := range tests {
			prop, ok := properties[tt.fieldName].(map[string]interface{})
			if !ok {
				t.Fatalf("Expected %s property to be a map", tt.fieldName)
			}
			if prop["type"] != tt.expectedType {
				t.Errorf("Expected %s type '%s', got %v",
					tt.fieldName, tt.expectedType, prop["type"])
			}
		}
	})

	// 测试用例 5: nil 值
	t.Run("nil value", func(t *testing.T) {
		schema := generateJSONSchemaFromStruct(nil)

		var schemaMap map[string]interface{}
		if err := json.Unmarshal([]byte(schema), &schemaMap); err != nil {
			t.Fatalf("Failed to parse schema: %v", err)
		}

		if schemaMap["type"] != "object" {
			t.Errorf("Expected type 'object', got %v", schemaMap["type"])
		}

		properties, ok := schemaMap["properties"].(map[string]interface{})
		if !ok {
			t.Fatal("Expected properties to be a map")
		}
		if len(properties) != 0 {
			t.Errorf("Expected empty properties, got %d", len(properties))
		}
	})

	// 测试用例 6: 指针类型结构体
	t.Run("pointer to struct", func(t *testing.T) {
		type TestStruct struct {
			Name string `json:"name"`
		}

		schema := generateJSONSchemaFromStruct(&TestStruct{})

		var schemaMap map[string]interface{}
		if err := json.Unmarshal([]byte(schema), &schemaMap); err != nil {
			t.Fatalf("Failed to parse schema: %v", err)
		}

		properties, ok := schemaMap["properties"].(map[string]interface{})
		if !ok {
			t.Fatal("Expected properties to be a map")
		}
		if len(properties) != 1 {
			t.Errorf("Expected 1 property, got %d", len(properties))
		}
	})

	// 测试用例 7: 忽略字段
	t.Run("struct with ignored fields", func(t *testing.T) {
		type TestStruct struct {
			Name     string `json:"name"`
			Ignored  string `json:"-"`
			internal string //nolint:unused // 未导出字段，用于测试 schema 生成忽略私有字段
		}

		schema := generateJSONSchemaFromStruct(TestStruct{})

		var schemaMap map[string]interface{}
		if err := json.Unmarshal([]byte(schema), &schemaMap); err != nil {
			t.Fatalf("Failed to parse schema: %v", err)
		}

		properties, ok := schemaMap["properties"].(map[string]interface{})
		if !ok {
			t.Fatal("Expected properties to be a map")
		}

		// 只有 Name 字段应该被包含
		if len(properties) != 1 {
			t.Errorf("Expected 1 property, got %d", len(properties))
		}

		if _, exists := properties["name"]; !exists {
			t.Error("Expected 'name' property to exist")
		}

		if _, exists := properties["Ignored"]; exists {
			t.Error("Expected 'Ignored' property to be excluded")
		}

		if _, exists := properties["internal"]; exists {
			t.Error("Expected 'internal' property to be excluded")
		}
	})

	// 测试用例 8: 空结构体
	t.Run("empty struct", func(t *testing.T) {
		type EmptyStruct struct{}

		schema := generateJSONSchemaFromStruct(EmptyStruct{})

		var schemaMap map[string]interface{}
		if err := json.Unmarshal([]byte(schema), &schemaMap); err != nil {
			t.Fatalf("Failed to parse schema: %v", err)
		}

		properties, ok := schemaMap["properties"].(map[string]interface{})
		if !ok {
			t.Fatal("Expected properties to be a map")
		}
		if len(properties) != 0 {
			t.Errorf("Expected empty properties, got %d", len(properties))
		}
	})
}

// TestFunctionToolWithArgsSchemaFromStruct 测试 Builder 的 WithArgsSchemaFromStruct 方法
func TestFunctionToolWithArgsSchemaFromStruct(t *testing.T) {
	type CalculatorInput struct {
		Operation string  `json:"operation" description:"运算类型"`
		A         float64 `json:"a" description:"第一个数字"`
		B         float64 `json:"b" description:"第二个数字"`
	}

	tool := NewFunctionToolBuilder("calculator").
		WithDescription("计算器工具").
		WithArgsSchemaFromStruct(CalculatorInput{}).
		WithFunction(func(ctx context.Context, args map[string]interface{}) (interface{}, error) {
			op := args["operation"].(string)
			a := args["a"].(float64)
			b := args["b"].(float64)
			switch op {
			case "add":
				return a + b, nil
			case "subtract":
				return a - b, nil
			default:
				return nil, agentErrors.New(agentErrors.CodeInvalidInput, "unknown operation")
			}
		}).
		MustBuild()

	// 验证 schema 不为空
	schema := tool.ArgsSchema()
	if schema == "" {
		t.Error("Expected non-empty schema")
	}

	// 验证 schema 包含必需字段
	var schemaMap map[string]interface{}
	if err := json.Unmarshal([]byte(schema), &schemaMap); err != nil {
		t.Fatalf("Failed to parse schema: %v", err)
	}

	properties, ok := schemaMap["properties"].(map[string]interface{})
	if !ok {
		t.Fatal("Expected properties to be a map")
	}

	if len(properties) != 3 {
		t.Errorf("Expected 3 properties, got %d", len(properties))
	}

	// 测试工具调用
	ctx := context.Background()
	input := &interfaces.ToolInput{
		Args: map[string]interface{}{
			"operation": "add",
			"a":         5.0,
			"b":         3.0,
		},
		Context: ctx,
	}

	output, err := tool.Invoke(ctx, input)
	if err != nil {
		t.Fatalf("Invoke failed: %v", err)
	}

	if !output.Success {
		t.Error("Expected success=true")
	}

	if result, ok := output.Result.(float64); !ok || result != 8.0 {
		t.Errorf("Expected result 8.0, got %v", output.Result)
	}
}
