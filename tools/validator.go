package tools

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	agentErrors "github.com/kart-io/goagent/errors"
	"github.com/kart-io/goagent/interfaces"
	"github.com/kart-io/goagent/mcp/core"
	"github.com/kart-io/goagent/utils/json"
)

// InputValidator 提供工具输入验证功能
type InputValidator struct {
	// StrictMode 严格模式，不允许额外的未定义参数
	StrictMode bool

	// ValidateTypes 是否验证参数类型
	ValidateTypes bool

	// ValidateRequired 是否验证必需参数
	ValidateRequired bool
}

// NewInputValidator 创建默认配置的验证器
func NewInputValidator() *InputValidator {
	return &InputValidator{
		StrictMode:       false,
		ValidateTypes:    true,
		ValidateRequired: true,
	}
}

// NewStrictInputValidator 创建严格模式的验证器
func NewStrictInputValidator() *InputValidator {
	return &InputValidator{
		StrictMode:       true,
		ValidateTypes:    true,
		ValidateRequired: true,
	}
}

// Validate 验证工具输入
//
// 验证步骤:
// 1. 如果工具实现了 ValidatableTool 接口，调用其 Validate 方法
// 2. 解析工具的 JSON Schema
// 3. 验证必需参数
// 4. 验证参数类型
// 5. 在严格模式下验证是否有未定义的参数
func (v *InputValidator) Validate(ctx context.Context, tool interfaces.Tool, input *interfaces.ToolInput) error {
	if tool == nil {
		return agentErrors.New(agentErrors.CodeInvalidInput, "tool cannot be nil").
			WithComponent("input_validator").
			WithOperation("validate")
	}

	if input == nil {
		return agentErrors.New(agentErrors.CodeInvalidInput, "input cannot be nil").
			WithComponent("input_validator").
			WithOperation("validate").
			WithContext("tool_name", tool.Name())
	}

	// 1. 如果工具实现了 ValidatableTool，调用自定义验证
	if validatable, ok := tool.(interfaces.ValidatableTool); ok {
		if err := validatable.Validate(ctx, input); err != nil {
			return agentErrors.New(agentErrors.CodeToolValidation, "tool custom validation failed").
				WithComponent("input_validator").
				WithOperation("validate").
				WithContext("tool_name", tool.Name()).
				WithContext("validation_error", err.Error())
		}
	}

	// 2. 解析 JSON Schema
	schema, err := v.parseSchema(tool.ArgsSchema())
	if err != nil {
		return agentErrors.New(agentErrors.CodeInvalidInput, "schema parsing failed").
			WithComponent("input_validator").
			WithOperation("parse_schema").
			WithContext("tool_name", tool.Name()).
			WithContext("error", err.Error())
	}

	// 3. 验证必需参数
	if v.ValidateRequired {
		if err := v.validateRequired(schema, input.Args); err != nil {
			return agentErrors.New(agentErrors.CodeToolValidation, "required parameter validation failed").
				WithComponent("input_validator").
				WithOperation("validate_required").
				WithContext("tool_name", tool.Name()).
				WithContext("validation_error", err.Error())
		}
	}

	// 4. 验证参数类型
	if v.ValidateTypes {
		if err := v.validateTypes(schema, input.Args); err != nil {
			return agentErrors.New(agentErrors.CodeToolValidation, "parameter type validation failed").
				WithComponent("input_validator").
				WithOperation("validate_types").
				WithContext("tool_name", tool.Name()).
				WithContext("validation_error", err.Error())
		}
	}

	// 5. 严格模式：验证是否有未定义的参数
	if v.StrictMode {
		if err := v.validateNoExtraArgs(schema, input.Args); err != nil {
			return agentErrors.New(agentErrors.CodeToolValidation, "extra parameters not allowed").
				WithComponent("input_validator").
				WithOperation("validate_strict").
				WithContext("tool_name", tool.Name()).
				WithContext("validation_error", err.Error())
		}
	}

	return nil
}

// schema 表示简化的 JSON Schema
type schema struct {
	Type       string                 `json:"type"`
	Properties map[string]property    `json:"properties"`
	Required   []string               `json:"required"`
	Additional map[string]interface{} `json:"-"` // 其他未解析的字段
}

// property 表示属性定义
type property struct {
	Type        string        `json:"type"`
	Description string        `json:"description"`
	Enum        []interface{} `json:"enum"`
	Minimum     *float64      `json:"minimum"`
	Maximum     *float64      `json:"maximum"`
	MinLength   *int          `json:"minLength"`
	MaxLength   *int          `json:"maxLength"`
}

// parseSchema 解析 JSON Schema
func (v *InputValidator) parseSchema(schemaStr string) (*schema, error) {
	if strings.TrimSpace(schemaStr) == "" {
		// 空 schema 视为不需要参数
		return &schema{
			Type:       "object",
			Properties: make(map[string]property),
			Required:   []string{},
		}, nil
	}

	var s schema
	if err := json.Unmarshal([]byte(schemaStr), &s); err != nil {
		return nil, agentErrors.Wrap(err, agentErrors.CodeInvalidInput, "failed to parse schema").
			WithComponent("input_validator").
			WithOperation("parse_schema")
	}

	if s.Properties == nil {
		s.Properties = make(map[string]property)
	}

	if s.Required == nil {
		s.Required = []string{}
	}

	return &s, nil
}

// validateRequired 验证必需参数
func (v *InputValidator) validateRequired(s *schema, args map[string]interface{}) error {
	for _, required := range s.Required {
		if _, exists := args[required]; !exists {
			return agentErrors.New(agentErrors.CodeToolValidation, "required parameter is missing").
				WithComponent("input_validator").
				WithOperation("validate_required").
				WithContext("parameter", required)
		}
	}
	return nil
}

// validateTypes 验证参数类型
func (v *InputValidator) validateTypes(s *schema, args map[string]interface{}) error {
	for key, value := range args {
		prop, exists := s.Properties[key]
		if !exists {
			// 如果不是严格模式，未定义的参数跳过类型检查
			continue
		}

		if err := v.validateType(key, value, prop); err != nil {
			return err
		}
	}
	return nil
}

// validateType 验证单个值的类型
func (v *InputValidator) validateType(key string, value interface{}, prop property) error {
	if value == nil {
		return nil // nil 值跳过类型检查
	}

	switch prop.Type {
	case "string":
		if _, ok := value.(string); !ok {
			return agentErrors.New(agentErrors.CodeToolValidation, "parameter must be string").
				WithComponent("input_validator").
				WithOperation("validate_type").
				WithContext("parameter", key).
				WithContext("got_type", fmt.Sprintf("%T", value))
		}
		// 验证字符串长度
		if s, ok := value.(string); ok {
			if prop.MinLength != nil && len(s) < *prop.MinLength {
				return agentErrors.New(agentErrors.CodeToolValidation, "parameter length must be at least minimum").
					WithComponent("input_validator").
					WithOperation("validate_type").
					WithContext("parameter", key).
					WithContext("min_length", *prop.MinLength).
					WithContext("got_length", len(s))
			}
			if prop.MaxLength != nil && len(s) > *prop.MaxLength {
				return agentErrors.New(agentErrors.CodeToolValidation, "parameter length must be at most maximum").
					WithComponent("input_validator").
					WithOperation("validate_type").
					WithContext("parameter", key).
					WithContext("max_length", *prop.MaxLength).
					WithContext("got_length", len(s))
			}
		}

	case "number", "integer":
		var num float64
		switch val := value.(type) {
		case float64:
			num = val
		case float32:
			num = float64(val)
		case int:
			num = float64(val)
		case int64:
			num = float64(val)
		case int32:
			num = float64(val)
		default:
			return agentErrors.New(agentErrors.CodeToolValidation, "parameter must be number").
				WithComponent("input_validator").
				WithOperation("validate_type").
				WithContext("parameter", key).
				WithContext("got_type", fmt.Sprintf("%T", value))
		}

		// 验证数值范围
		if prop.Minimum != nil && num < *prop.Minimum {
			return agentErrors.New(agentErrors.CodeToolValidation, "parameter must be >= minimum").
				WithComponent("input_validator").
				WithOperation("validate_type").
				WithContext("parameter", key).
				WithContext("minimum", *prop.Minimum).
				WithContext("got_value", num)
		}
		if prop.Maximum != nil && num > *prop.Maximum {
			return agentErrors.New(agentErrors.CodeToolValidation, "parameter must be <= maximum").
				WithComponent("input_validator").
				WithOperation("validate_type").
				WithContext("parameter", key).
				WithContext("maximum", *prop.Maximum).
				WithContext("got_value", num)
		}

		// 对于 integer 类型，验证是否为整数
		if prop.Type == "integer" && num != float64(int(num)) {
			return agentErrors.New(agentErrors.CodeToolValidation, "parameter must be integer").
				WithComponent("input_validator").
				WithOperation("validate_type").
				WithContext("parameter", key).
				WithContext("got_value", num)
		}

	case "boolean":
		if _, ok := value.(bool); !ok {
			return agentErrors.New(agentErrors.CodeToolValidation, "parameter must be boolean").
				WithComponent("input_validator").
				WithOperation("validate_type").
				WithContext("parameter", key).
				WithContext("got_type", fmt.Sprintf("%T", value))
		}

	case "array":
		switch value.(type) {
		case []interface{}, []string, []int, []float64, []bool:
			// 有效的数组类型
		default:
			return agentErrors.New(agentErrors.CodeToolValidation, "parameter must be array").
				WithComponent("input_validator").
				WithOperation("validate_type").
				WithContext("parameter", key).
				WithContext("got_type", fmt.Sprintf("%T", value))
		}

	case "object":
		if _, ok := value.(map[string]interface{}); !ok {
			return agentErrors.New(agentErrors.CodeToolValidation, "parameter must be object").
				WithComponent("input_validator").
				WithOperation("validate_type").
				WithContext("parameter", key).
				WithContext("got_type", fmt.Sprintf("%T", value))
		}
	}

	// 验证枚举值
	if len(prop.Enum) > 0 {
		found := false
		for _, enumVal := range prop.Enum {
			if value == enumVal {
				found = true
				break
			}
		}
		if !found {
			return agentErrors.New(agentErrors.CodeToolValidation, "parameter must be one of enum values").
				WithComponent("input_validator").
				WithOperation("validate_type").
				WithContext("parameter", key).
				WithContext("enum_values", fmt.Sprintf("%v", prop.Enum)).
				WithContext("got_value", value)
		}
	}

	return nil
}

// validateNoExtraArgs 验证是否有未定义的参数
func (v *InputValidator) validateNoExtraArgs(s *schema, args map[string]interface{}) error {
	for key := range args {
		if _, exists := s.Properties[key]; !exists {
			return agentErrors.New(agentErrors.CodeToolValidation, "unexpected parameter not defined in schema").
				WithComponent("input_validator").
				WithOperation("validate_no_extra_args").
				WithContext("parameter", key)
		}
	}
	return nil
}

// ValidateAndInvoke 验证后执行工具（便捷方法）
func ValidateAndInvoke(ctx context.Context, tool interfaces.Tool, input *interfaces.ToolInput) (*interfaces.ToolOutput, error) {
	validator := NewInputValidator()
	if err := validator.Validate(ctx, tool, input); err != nil {
		return &interfaces.ToolOutput{
			Success: false,
			Error:   err.Error(),
		}, err
	}
	return tool.Invoke(ctx, input)
}

// ============================================================================
// MCP ToolSchema 验证支持
// ============================================================================

// ValidateToolSchema 验证 MCP ToolSchema 定义本身
//
// 验证内容：
// 1. Type 必须是 "object"
// 2. Required 字段必须在 Properties 中定义
// 3. 每个属性的类型必须有效
// 4. 数组类型必须定义 Items
// 5. 数值/字符串约束必须合理（最小值 <= 最大值）
func ValidateToolSchema(schema *core.ToolSchema) error {
	if schema == nil {
		return agentErrors.New(agentErrors.CodeInvalidInput, "schema cannot be nil").
			WithComponent("validator").
			WithOperation("validate_tool_schema")
	}

	if schema.Type != "object" {
		return agentErrors.New(agentErrors.CodeToolValidation, "schema type must be 'object'").
			WithComponent("validator").
			WithOperation("validate_tool_schema").
			WithContext("got_type", schema.Type)
	}

	// 验证所有 required 字段在 properties 中定义
	for _, required := range schema.Required {
		if _, exists := schema.Properties[required]; !exists {
			return agentErrors.New(agentErrors.CodeToolValidation, "required field not defined in properties").
				WithComponent("validator").
				WithOperation("validate_tool_schema").
				WithContext("field", required)
		}
	}

	// 验证每个属性定义
	for name := range schema.Properties {
		prop := schema.Properties[name]
		if err := validatePropertySchema(&prop); err != nil {
			return agentErrors.Wrap(err, agentErrors.CodeToolValidation, "invalid property").
				WithComponent("validator").
				WithOperation("validate_tool_schema").
				WithContext("property", name)
		}
	}

	return nil
}

// validatePropertySchema 验证属性 Schema
func validatePropertySchema(prop *core.PropertySchema) error {
	validTypes := map[string]bool{
		"string": true, "number": true, "integer": true,
		"boolean": true, "object": true, "array": true,
	}

	if !validTypes[prop.Type] {
		return agentErrors.New(agentErrors.CodeToolValidation, "invalid property type").
			WithComponent("validator").
			WithOperation("validate_property_schema").
			WithContext("type", prop.Type)
	}

	// 验证数组类型
	if prop.Type == "array" && prop.Items == nil {
		return agentErrors.New(agentErrors.CodeToolValidation, "array type must define items").
			WithComponent("validator").
			WithOperation("validate_property_schema")
	}

	// 验证数字范围
	if prop.Minimum != nil && prop.Maximum != nil {
		if *prop.Minimum > *prop.Maximum {
			return agentErrors.New(agentErrors.CodeToolValidation, "minimum cannot be greater than maximum").
				WithComponent("validator").
				WithOperation("validate_property_schema").
				WithContext("minimum", *prop.Minimum).
				WithContext("maximum", *prop.Maximum)
		}
	}

	// 验证字符串长度
	if prop.MinLength != nil && prop.MaxLength != nil {
		if *prop.MinLength > *prop.MaxLength {
			return agentErrors.New(agentErrors.CodeToolValidation, "minLength cannot be greater than maxLength").
				WithComponent("validator").
				WithOperation("validate_property_schema").
				WithContext("min_length", *prop.MinLength).
				WithContext("max_length", *prop.MaxLength)
		}
	}

	return nil
}

// ValidateInputWithSchema 使用 MCP ToolSchema 验证输入参数
//
// 验证步骤：
// 1. 检查必需字段
// 2. 验证每个输入字段的类型和约束
// 3. 在非 AdditionalProperties 模式下验证未定义字段
func ValidateInputWithSchema(schema *core.ToolSchema, input map[string]interface{}, strict bool) error {
	if schema == nil {
		return agentErrors.New(agentErrors.CodeInvalidInput, "schema cannot be nil").
			WithComponent("validator").
			WithOperation("validate_input_with_schema")
	}

	// 检查必需字段
	for _, required := range schema.Required {
		if _, exists := input[required]; !exists {
			return agentErrors.New(agentErrors.CodeToolValidation, "required field is missing").
				WithComponent("validator").
				WithOperation("validate_input_with_schema").
				WithContext("field", required)
		}
	}

	// 验证每个输入字段
	for key, value := range input {
		propSchema, exists := schema.Properties[key]
		if !exists {
			if !schema.AdditionalProperties && strict {
				return agentErrors.New(agentErrors.CodeToolValidation, "field not defined in schema").
					WithComponent("validator").
					WithOperation("validate_input_with_schema").
					WithContext("field", key)
			}
			continue
		}

		if err := validateValueWithPropertySchema(key, value, &propSchema); err != nil {
			return err
		}
	}

	return nil
}

// validateValueWithPropertySchema 验证值符合属性 Schema
func validateValueWithPropertySchema(fieldName string, value interface{}, schema *core.PropertySchema) error {
	if value == nil {
		return nil // nil 值总是有效的
	}

	switch schema.Type {
	case "string":
		return validateStringWithSchema(fieldName, value, schema)
	case "number", "integer":
		return validateNumberWithSchema(fieldName, value, schema)
	case "boolean":
		return validateBooleanWithSchema(fieldName, value)
	case "array":
		return validateArrayWithSchema(fieldName, value, schema)
	case "object":
		return validateObjectWithSchema(fieldName, value)
	default:
		return agentErrors.New(agentErrors.CodeToolValidation, "unsupported type").
			WithComponent("validator").
			WithOperation("validate_value").
			WithContext("field", fieldName).
			WithContext("type", schema.Type)
	}
}

// validateStringWithSchema 验证字符串值
func validateStringWithSchema(fieldName string, value interface{}, schema *core.PropertySchema) error {
	str, ok := value.(string)
	if !ok {
		return agentErrors.New(agentErrors.CodeToolValidation, "expected string").
			WithComponent("validator").
			WithOperation("validate_string").
			WithContext("field", fieldName).
			WithContext("got_type", fmt.Sprintf("%T", value))
	}

	// 检查枚举值
	if len(schema.Enum) > 0 {
		found := false
		for _, enum := range schema.Enum {
			if str == enum {
				found = true
				break
			}
		}
		if !found {
			return agentErrors.New(agentErrors.CodeToolValidation, "value must be one of enum").
				WithComponent("validator").
				WithOperation("validate_string").
				WithContext("field", fieldName).
				WithContext("enum_values", fmt.Sprintf("%v", schema.Enum)).
				WithContext("got_value", str)
		}
	}

	// 检查长度
	if schema.MinLength != nil && len(str) < *schema.MinLength {
		return agentErrors.New(agentErrors.CodeToolValidation, "length must be at least minimum").
			WithComponent("validator").
			WithOperation("validate_string").
			WithContext("field", fieldName).
			WithContext("min_length", *schema.MinLength).
			WithContext("got_length", len(str))
	}
	if schema.MaxLength != nil && len(str) > *schema.MaxLength {
		return agentErrors.New(agentErrors.CodeToolValidation, "length must not exceed maximum").
			WithComponent("validator").
			WithOperation("validate_string").
			WithContext("field", fieldName).
			WithContext("max_length", *schema.MaxLength).
			WithContext("got_length", len(str))
	}

	// 检查正则表达式
	if schema.Pattern != "" {
		matched, err := regexp.MatchString(schema.Pattern, str)
		if err != nil {
			return agentErrors.New(agentErrors.CodeToolValidation, "invalid pattern").
				WithComponent("validator").
				WithOperation("validate_string").
				WithContext("field", fieldName).
				WithContext("pattern", schema.Pattern).
				WithContext("error", err.Error())
		}
		if !matched {
			return agentErrors.New(agentErrors.CodeToolValidation, "does not match pattern").
				WithComponent("validator").
				WithOperation("validate_string").
				WithContext("field", fieldName).
				WithContext("pattern", schema.Pattern).
				WithContext("value", str)
		}
	}

	// 检查格式
	if schema.Format != "" {
		if err := validateFormat(str, schema.Format); err != nil {
			return agentErrors.Wrap(err, agentErrors.CodeToolValidation, "format validation failed").
				WithComponent("validator").
				WithOperation("validate_string").
				WithContext("field", fieldName).
				WithContext("format", schema.Format)
		}
	}

	return nil
}

// validateNumberWithSchema 验证数字值
func validateNumberWithSchema(fieldName string, value interface{}, schema *core.PropertySchema) error {
	var num float64

	switch val := value.(type) {
	case float64:
		num = val
	case float32:
		num = float64(val)
	case int:
		num = float64(val)
	case int64:
		num = float64(val)
	case int32:
		num = float64(val)
	default:
		return agentErrors.New(agentErrors.CodeToolValidation, "expected number").
			WithComponent("validator").
			WithOperation("validate_number").
			WithContext("field", fieldName).
			WithContext("got_type", fmt.Sprintf("%T", value))
	}

	// 检查整数类型
	if schema.Type == "integer" && num != float64(int64(num)) {
		return agentErrors.New(agentErrors.CodeToolValidation, "expected integer value").
			WithComponent("validator").
			WithOperation("validate_number").
			WithContext("field", fieldName).
			WithContext("got_value", num)
	}

	// 检查范围
	if schema.Minimum != nil && num < *schema.Minimum {
		return agentErrors.New(agentErrors.CodeToolValidation, "must be at least minimum").
			WithComponent("validator").
			WithOperation("validate_number").
			WithContext("field", fieldName).
			WithContext("minimum", *schema.Minimum).
			WithContext("got_value", num)
	}
	if schema.Maximum != nil && num > *schema.Maximum {
		return agentErrors.New(agentErrors.CodeToolValidation, "must not exceed maximum").
			WithComponent("validator").
			WithOperation("validate_number").
			WithContext("field", fieldName).
			WithContext("maximum", *schema.Maximum).
			WithContext("got_value", num)
	}

	return nil
}

// validateBooleanWithSchema 验证布尔值
func validateBooleanWithSchema(fieldName string, value interface{}) error {
	if _, ok := value.(bool); !ok {
		return agentErrors.New(agentErrors.CodeToolValidation, "expected boolean").
			WithComponent("validator").
			WithOperation("validate_boolean").
			WithContext("field", fieldName).
			WithContext("got_type", fmt.Sprintf("%T", value))
	}
	return nil
}

// validateArrayWithSchema 验证数组值
func validateArrayWithSchema(fieldName string, value interface{}, schema *core.PropertySchema) error {
	// 使用类型断言检查常见的数组类型
	switch arr := value.(type) {
	case []interface{}:
		if schema.Items != nil {
			for i, elem := range arr {
				elemName := fmt.Sprintf("%s[%d]", fieldName, i)
				if err := validateValueWithPropertySchema(elemName, elem, schema.Items); err != nil {
					return err
				}
			}
		}
		return nil
	case []string, []int, []float64, []bool:
		// 这些类型的数组也是有效的，但不递归验证元素
		return nil
	default:
		return agentErrors.New(agentErrors.CodeToolValidation, "expected array").
			WithComponent("validator").
			WithOperation("validate_array").
			WithContext("field", fieldName).
			WithContext("got_type", fmt.Sprintf("%T", value))
	}
}

// validateObjectWithSchema 验证对象值
func validateObjectWithSchema(fieldName string, value interface{}) error {
	if _, ok := value.(map[string]interface{}); !ok {
		return agentErrors.New(agentErrors.CodeToolValidation, "expected object").
			WithComponent("validator").
			WithOperation("validate_object").
			WithContext("field", fieldName).
			WithContext("got_type", fmt.Sprintf("%T", value))
	}
	return nil
}

// validateFormat 验证格式
func validateFormat(value, format string) error {
	switch format {
	case "email":
		if !strings.Contains(value, "@") {
			return agentErrors.New(agentErrors.CodeToolValidation, "invalid email format").
				WithComponent("validator").
				WithOperation("validate_format").
				WithContext("value", value)
		}
	case "uri", "url":
		if !strings.HasPrefix(value, "http://") && !strings.HasPrefix(value, "https://") {
			return agentErrors.New(agentErrors.CodeToolValidation, "invalid URL format").
				WithComponent("validator").
				WithOperation("validate_format").
				WithContext("value", value)
		}
	case "uuid":
		// 简单的 UUID 格式验证
		if matched, _ := regexp.MatchString(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`, value); !matched {
			return agentErrors.New(agentErrors.CodeToolValidation, "invalid UUID format").
				WithComponent("validator").
				WithOperation("validate_format").
				WithContext("value", value)
		}
	}
	return nil
}
