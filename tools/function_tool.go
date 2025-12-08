package tools

import (
	"context"
	"reflect"
	"strings"

	agentErrors "github.com/kart-io/goagent/errors"
	"github.com/kart-io/goagent/interfaces"
	"github.com/kart-io/goagent/utils/json"
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
func (f *FunctionTool) run(ctx context.Context, input *interfaces.ToolInput) (*interfaces.ToolOutput, error) {
	resp := NewToolErrorResponse(f.Name()).WithOperation("run")

	result, err := f.fn(ctx, input.Args)
	if err != nil {
		return resp.ExecutionError("function execution failed", nil, err)
	}

	return resp.Success(result, nil)
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
//
// Returns an error if the function is not set.
// This replaces the previous panic behavior with proper error handling.
func (b *FunctionToolBuilder) Build() (*FunctionTool, error) {
	if b.fn == nil {
		return nil, agentErrors.New(agentErrors.CodeInvalidConfig, "function is required").
			WithComponent("function_tool_builder").
			WithOperation("build")
	}
	if b.argsSchema == "" {
		b.argsSchema = "{}"
	}
	return NewFunctionTool(b.name, b.description, b.argsSchema, b.fn), nil
}

// MustBuild 构建工具，如果失败则 panic
//
// 用于初始化时确定配置正确的场景。
// 对于运行时构建，应使用 Build() 方法并处理错误。
func (b *FunctionToolBuilder) MustBuild() *FunctionTool {
	tool, err := b.Build()
	if err != nil {
		panic(err)
	}
	return tool
}

// generateJSONSchemaFromStruct 从结构体生成 JSON Schema
//
// 使用反射解析结构体字段，生成符合 JSON Schema 规范的字符串。
// 支持以下功能：
//   - 解析 json tag 确定字段名
//   - 自动推断字段类型
//   - 解析 description tag 添加字段描述
//   - 非指针字段自动标记为 required
func generateJSONSchemaFromStruct(v interface{}) string {
	// 处理 nil 值
	if v == nil {
		return `{"type":"object","properties":{}}`
	}

	t := reflect.TypeOf(v)

	// 处理指针类型
	if t.Kind() == reflect.Ptr {
		t = t.Elem()
	}

	// 如果不是结构体，返回基础 schema
	if t.Kind() != reflect.Struct {
		return `{"type":"object","properties":{}}`
	}

	// 构建 schema
	properties := make(map[string]interface{})
	var required []string

	// 遍历结构体字段
	for i := 0; i < t.NumField(); i++ {
		field := t.Field(i)

		// 跳过未导出的字段
		if !field.IsExported() {
			continue
		}

		// 获取 JSON 字段名
		jsonName := getJSONFieldName(field)
		if jsonName == "-" {
			// json:"-" 表示忽略该字段
			continue
		}

		// 构建字段属性
		prop := make(map[string]interface{})
		prop["type"] = getTypeString(field.Type)

		// 添加描述（如果有）
		if desc := field.Tag.Get("description"); desc != "" {
			prop["description"] = desc
		}

		properties[jsonName] = prop

		// 判断是否为必需字段
		// 1. 显式标记 required:"true"
		// 2. 非指针类型且没有 omitempty 标记
		if isRequired := field.Tag.Get("required"); isRequired == "true" {
			required = append(required, jsonName)
		} else if field.Type.Kind() != reflect.Ptr && !hasOmitEmpty(field) {
			required = append(required, jsonName)
		}
	}

	// 构建完整 schema
	schema := map[string]interface{}{
		"type":       "object",
		"properties": properties,
	}

	if len(required) > 0 {
		schema["required"] = required
	}

	// 序列化为 JSON
	data, _ := json.Marshal(schema)
	return string(data)
}

// getJSONFieldName 从字段中提取 JSON 字段名
func getJSONFieldName(field reflect.StructField) string {
	jsonTag := field.Tag.Get("json")
	if jsonTag == "" {
		// 没有 json tag，使用字段名的小写形式
		return strings.ToLower(field.Name[:1]) + field.Name[1:]
	}

	// 解析 json tag，格式可能是 "name" 或 "name,omitempty"
	parts := strings.Split(jsonTag, ",")
	return parts[0]
}

// hasOmitEmpty 检查字段是否有 omitempty 标记
func hasOmitEmpty(field reflect.StructField) bool {
	jsonTag := field.Tag.Get("json")
	return strings.Contains(jsonTag, "omitempty")
}

// getTypeString 将 Go 类型映射为 JSON Schema 类型
func getTypeString(t reflect.Type) string {
	// 处理指针类型
	if t.Kind() == reflect.Ptr {
		t = t.Elem()
	}

	switch t.Kind() {
	case reflect.String:
		return "string"
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return "integer"
	case reflect.Float32, reflect.Float64:
		return "number"
	case reflect.Bool:
		return "boolean"
	case reflect.Slice, reflect.Array:
		return "array"
	case reflect.Struct, reflect.Map:
		return "object"
	default:
		return "string" // 默认类型
	}
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
				return nil, agentErrors.Wrap(err, agentErrors.CodeToolExecution, "failed to marshal args").
					WithComponent("function_tool").
					WithOperation("typed_function_run")
			}
			if err := json.Unmarshal(data, &input); err != nil {
				return nil, agentErrors.Wrap(err, agentErrors.CodeToolExecution, "failed to unmarshal args").
					WithComponent("function_tool").
					WithOperation("typed_function_run")
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
// tool, err := NewFunctionToolBuilder("calculator").
//     WithDescription("Performs basic arithmetic operations").
//     WithArgsSchema(`{...}`).
//     WithFunction(func(ctx context.Context, args map[string]interface{}) (interface{}, error) {
//         // ...
//     }).
//     Build()
// if err != nil {
//     return err
// }
//
// // 2b. 使用 MustBuild (用于初始化时确定配置正确的场景)
// tool := NewFunctionToolBuilder("calculator").
//     WithDescription("Performs basic arithmetic operations").
//     WithArgsSchema(`{...}`).
//     WithFunction(func(ctx context.Context, args map[string]interface{}) (interface{}, error) {
//         // ...
//     }).
//     MustBuild()
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
