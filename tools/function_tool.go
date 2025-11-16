package tools

import (
	"context"
	"encoding/json"
	"fmt"
)

// FunctionTool 函数包装工具
//
// 将普通 Go 函数包装成工具
// 支持自动参数转换和结果包装
type FunctionTool struct {
	*BaseTool
	fn func(context.Context, map[string]interface{}) (interface{}, error)
}

// NewFunctionTool 创建函数工具
//
// Parameters:
//   - name: 工具名称
//   - description: 工具描述
//   - argsSchema: 参数 JSON Schema
//   - fn: 执行函数
func NewFunctionTool(
	name string,
	description string,
	argsSchema string,
	fn func(context.Context, map[string]interface{}) (interface{}, error),
) *FunctionTool {
	tool := &FunctionTool{
		fn: fn,
	}

	tool.BaseTool = NewBaseTool(name, description, argsSchema, tool.run)
	return tool
}

// run 执行函数
func (f *FunctionTool) run(ctx context.Context, input *ToolInput) (*ToolOutput, error) {
	result, err := f.fn(ctx, input.Args)
	if err != nil {
		return &ToolOutput{
			Success: false,
			Error:   err.Error(),
		}, NewToolError(f.Name(), "function execution failed", err)
	}

	return &ToolOutput{
		Result:  result,
		Success: true,
	}, nil
}

// FunctionToolBuilder 函数工具构建器
//
// 提供更灵活的函数工具创建方式
type FunctionToolBuilder struct {
	name        string
	description string
	argsSchema  string
	fn          func(context.Context, map[string]interface{}) (interface{}, error)
}

// NewFunctionToolBuilder 创建函数工具构建器
func NewFunctionToolBuilder(name string) *FunctionToolBuilder {
	return &FunctionToolBuilder{
		name: name,
	}
}

// WithDescription 设置描述
func (b *FunctionToolBuilder) WithDescription(description string) *FunctionToolBuilder {
	b.description = description
	return b
}

// WithArgsSchema 设置参数 Schema
func (b *FunctionToolBuilder) WithArgsSchema(schema string) *FunctionToolBuilder {
	b.argsSchema = schema
	return b
}

// WithArgsSchemaFromStruct 从结构体生成参数 Schema
func (b *FunctionToolBuilder) WithArgsSchemaFromStruct(v interface{}) *FunctionToolBuilder {
	// 简化实现：将结构体序列化为 JSON Schema
	// 实际项目中可以使用更专业的库如 jsonschema
	schema := generateJSONSchemaFromStruct(v)
	b.argsSchema = schema
	return b
}

// WithFunction 设置执行函数
func (b *FunctionToolBuilder) WithFunction(
	fn func(context.Context, map[string]interface{}) (interface{}, error),
) *FunctionToolBuilder {
	b.fn = fn
	return b
}

// Build 构建工具
func (b *FunctionToolBuilder) Build() *FunctionTool {
	if b.fn == nil {
		panic("function is required")
	}
	if b.argsSchema == "" {
		b.argsSchema = "{}"
	}
	return NewFunctionTool(b.name, b.description, b.argsSchema, b.fn)
}

// generateJSONSchemaFromStruct 从结构体生成 JSON Schema
// 简化实现，实际项目中应使用专业库
func generateJSONSchemaFromStruct(v interface{}) string {
	schema := map[string]interface{}{
		"type":       "object",
		"properties": map[string]interface{}{},
	}

	// 简化：直接返回基础 schema
	// 在实际项目中，应该使用反射解析结构体字段
	data, _ := json.Marshal(schema)
	return string(data)
}

// SimpleFunction 简单函数类型
// 用于快速创建不需要复杂参数的工具
type SimpleFunction func(context.Context) (interface{}, error)

// NewSimpleFunctionTool 创建简单函数工具
//
// 用于快速包装无参数或简单参数的函数
func NewSimpleFunctionTool(
	name string,
	description string,
	fn SimpleFunction,
) *FunctionTool {
	return NewFunctionTool(
		name,
		description,
		`{"type": "object", "properties": {}}`,
		func(ctx context.Context, args map[string]interface{}) (interface{}, error) {
			return fn(ctx)
		},
	)
}

// TypedFunction 类型安全的函数类型
// 使用泛型提供类型安全
type TypedFunction[I, O any] func(context.Context, I) (O, error)

// NewTypedFunctionTool 创建类型安全的函数工具
//
// 使用泛型提供编译时类型检查
func NewTypedFunctionTool[I, O any](
	name string,
	description string,
	argsSchema string,
	fn TypedFunction[I, O],
) *FunctionTool {
	return NewFunctionTool(
		name,
		description,
		argsSchema,
		func(ctx context.Context, args map[string]interface{}) (interface{}, error) {
			// 将 map 转换为类型化输入
			var input I
			data, err := json.Marshal(args)
			if err != nil {
				return nil, fmt.Errorf("failed to marshal args: %w", err)
			}
			if err := json.Unmarshal(data, &input); err != nil {
				return nil, fmt.Errorf("failed to unmarshal args: %w", err)
			}

			// 执行类型化函数
			output, err := fn(ctx, input)
			if err != nil {
				return nil, err
			}

			return output, nil
		},
	)
}

// Example: 使用示例
//
// // 1. 基础用法
// tool := NewFunctionTool(
//     "calculator",
//     "Performs basic arithmetic operations",
//     `{"type": "object", "properties": {"operation": {"type": "string"}, "a": {"type": "number"}, "b": {"type": "number"}}}`,
//     func(ctx context.Context, args map[string]interface{}) (interface{}, error) {
//         op := args["operation"].(string)
//         a := args["a"].(float64)
//         b := args["b"].(float64)
//         switch op {
//         case "add":
//             return a + b, nil
//         case "subtract":
//             return a - b, nil
//         default:
//             return nil, fmt.Errorf("unknown operation: %s", op)
//         }
//     },
// )
//
// // 2. 使用构建器
// tool := NewFunctionToolBuilder("calculator").
//     WithDescription("Performs basic arithmetic operations").
//     WithArgsSchema(`{...}`).
//     WithFunction(func(ctx context.Context, args map[string]interface{}) (interface{}, error) {
//         // ...
//     }).
//     Build()
//
// // 3. 简单函数工具
// tool := NewSimpleFunctionTool(
//     "get_time",
//     "Gets current time",
//     func(ctx context.Context) (interface{}, error) {
//         return time.Now().String(), nil
//     },
// )
//
// // 4. 类型安全工具
// type CalculatorInput struct {
//     Operation string  `json:"operation"`
//     A         float64 `json:"a"`
//     B         float64 `json:"b"`
// }
//
// tool := NewTypedFunctionTool[CalculatorInput, float64](
//     "calculator",
//     "Performs basic arithmetic operations",
//     `{...}`,
//     func(ctx context.Context, input CalculatorInput) (float64, error) {
//         switch input.Operation {
//         case "add":
//             return input.A + input.B, nil
//         default:
//             return 0, fmt.Errorf("unknown operation")
//         }
//     },
// )
