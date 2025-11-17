package toolbox

import (
	"fmt"
	"reflect"
	"regexp"
	"strings"

	agentErrors "github.com/kart-io/goagent/errors"
	"github.com/kart-io/goagent/mcp/core"
)

// JSONSchemaValidator JSON Schema 验证器
type JSONSchemaValidator struct{}

// NewJSONSchemaValidator 创建 JSON Schema 验证器
func NewJSONSchemaValidator() *JSONSchemaValidator {
	return &JSONSchemaValidator{}
}

// ValidateSchema 验证 Schema
func (v *JSONSchemaValidator) ValidateSchema(schema *core.ToolSchema) error {
	if schema == nil {
		return agentErrors.New(agentErrors.CodeToolValidation, "schema cannot be nil").
			WithComponent("json_schema_validator").
			WithOperation("validate_schema")
	}

	if schema.Type != "object" {
		return agentErrors.New(agentErrors.CodeToolValidation, "schema type must be 'object'").
			WithComponent("json_schema_validator").
			WithOperation("validate_schema").
			WithContext("got_type", schema.Type)
	}

	// 验证所有 required 字段在 properties 中定义
	for _, required := range schema.Required {
		if _, exists := schema.Properties[required]; !exists {
			return agentErrors.New(agentErrors.CodeToolValidation, "required field not defined in properties").
				WithComponent("json_schema_validator").
				WithOperation("validate_schema").
				WithContext("field", required)
		}
	}

	// 验证每个属性定义
	for name := range schema.Properties {
		prop := schema.Properties[name] // Create copy to avoid implicit memory aliasing
		if err := v.validatePropertySchema(&prop); err != nil {
			return agentErrors.Wrap(err, agentErrors.CodeToolValidation, "invalid property").
				WithComponent("json_schema_validator").
				WithOperation("validate_schema").
				WithContext("property", name)
		}
	}

	return nil
}

// validatePropertySchema 验证属性 Schema
func (v *JSONSchemaValidator) validatePropertySchema(prop *core.PropertySchema) error {
	validTypes := map[string]bool{
		"string": true, "number": true, "integer": true,
		"boolean": true, "object": true, "array": true,
	}

	if !validTypes[prop.Type] {
		return agentErrors.New(agentErrors.CodeToolValidation, "invalid property type").
			WithComponent("json_schema_validator").
			WithOperation("validate_property_schema").
			WithContext("type", prop.Type)
	}

	// 验证数组类型
	if prop.Type == "array" && prop.Items == nil {
		return agentErrors.New(agentErrors.CodeToolValidation, "array type must define items").
			WithComponent("json_schema_validator").
			WithOperation("validate_property_schema")
	}

	// 验证数字范围
	if prop.Minimum != nil && prop.Maximum != nil {
		if *prop.Minimum > *prop.Maximum {
			return agentErrors.New(agentErrors.CodeToolValidation, "minimum cannot be greater than maximum").
				WithComponent("json_schema_validator").
				WithOperation("validate_property_schema").
				WithContext("minimum", *prop.Minimum).
				WithContext("maximum", *prop.Maximum)
		}
	}

	// 验证字符串长度
	if prop.MinLength != nil && prop.MaxLength != nil {
		if *prop.MinLength > *prop.MaxLength {
			return agentErrors.New(agentErrors.CodeToolValidation, "minLength cannot be greater than maxLength").
				WithComponent("json_schema_validator").
				WithOperation("validate_property_schema").
				WithContext("min_length", *prop.MinLength).
				WithContext("max_length", *prop.MaxLength)
		}
	}

	return nil
}

// ValidateInput 验证输入参数
func (v *JSONSchemaValidator) ValidateInput(schema *core.ToolSchema, input map[string]interface{}) error {
	// 检查必需字段
	for _, required := range schema.Required {
		if _, exists := input[required]; !exists {
			return &core.ErrInvalidInput{
				Field:   required,
				Message: "required field is missing",
			}
		}
	}

	// 验证每个输入字段
	for key, value := range input {
		propSchema, exists := schema.Properties[key]
		if !exists {
			if !schema.AdditionalProperties {
				return &core.ErrInvalidInput{
					Field:   key,
					Message: "field not defined in schema",
				}
			}
			continue
		}

		if err := v.validateValue(key, value, &propSchema); err != nil {
			return err
		}
	}

	return nil
}

// validateValue 验证值
func (v *JSONSchemaValidator) validateValue(fieldName string, value interface{}, schema *core.PropertySchema) error {
	if value == nil {
		return nil // nil 值总是有效的
	}

	switch schema.Type {
	case "string":
		return v.validateString(fieldName, value, schema)
	case "number", "integer":
		return v.validateNumber(fieldName, value, schema)
	case "boolean":
		return v.validateBoolean(fieldName, value)
	case "array":
		return v.validateArray(fieldName, value, schema)
	case "object":
		return v.validateObject(fieldName, value)
	default:
		return &core.ErrInvalidInput{
			Field:   fieldName,
			Message: fmt.Sprintf("unsupported type: %s", schema.Type),
		}
	}
}

// validateString 验证字符串
func (v *JSONSchemaValidator) validateString(fieldName string, value interface{}, schema *core.PropertySchema) error {
	str, ok := value.(string)
	if !ok {
		return &core.ErrInvalidInput{
			Field:   fieldName,
			Message: fmt.Sprintf("expected string, got %T", value),
		}
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
			return &core.ErrInvalidInput{
				Field:   fieldName,
				Message: fmt.Sprintf("value must be one of: %v", schema.Enum),
			}
		}
	}

	// 检查长度
	if schema.MinLength != nil && len(str) < *schema.MinLength {
		return &core.ErrInvalidInput{
			Field:   fieldName,
			Message: fmt.Sprintf("length must be at least %d", *schema.MinLength),
		}
	}
	if schema.MaxLength != nil && len(str) > *schema.MaxLength {
		return &core.ErrInvalidInput{
			Field:   fieldName,
			Message: fmt.Sprintf("length must not exceed %d", *schema.MaxLength),
		}
	}

	// 检查正则表达式
	if schema.Pattern != "" {
		matched, err := regexp.MatchString(schema.Pattern, str)
		if err != nil {
			return &core.ErrInvalidInput{
				Field:   fieldName,
				Message: fmt.Sprintf("invalid pattern: %v", err),
			}
		}
		if !matched {
			return &core.ErrInvalidInput{
				Field:   fieldName,
				Message: fmt.Sprintf("does not match pattern: %s", schema.Pattern),
			}
		}
	}

	// 检查格式
	if schema.Format != "" {
		if err := v.validateFormat(str, schema.Format); err != nil {
			return &core.ErrInvalidInput{
				Field:   fieldName,
				Message: err.Error(),
			}
		}
	}

	return nil
}

// validateNumber 验证数字
func (v *JSONSchemaValidator) validateNumber(fieldName string, value interface{}, schema *core.PropertySchema) error {
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
		return &core.ErrInvalidInput{
			Field:   fieldName,
			Message: fmt.Sprintf("expected number, got %T", value),
		}
	}

	// 检查整数类型
	if schema.Type == "integer" && num != float64(int64(num)) {
		return &core.ErrInvalidInput{
			Field:   fieldName,
			Message: "expected integer value",
		}
	}

	// 检查范围
	if schema.Minimum != nil && num < *schema.Minimum {
		return &core.ErrInvalidInput{
			Field:   fieldName,
			Message: fmt.Sprintf("must be at least %f", *schema.Minimum),
		}
	}
	if schema.Maximum != nil && num > *schema.Maximum {
		return &core.ErrInvalidInput{
			Field:   fieldName,
			Message: fmt.Sprintf("must not exceed %f", *schema.Maximum),
		}
	}

	return nil
}

// validateBoolean 验证布尔值
func (v *JSONSchemaValidator) validateBoolean(fieldName string, value interface{}) error {
	if _, ok := value.(bool); !ok {
		return &core.ErrInvalidInput{
			Field:   fieldName,
			Message: fmt.Sprintf("expected boolean, got %T", value),
		}
	}
	return nil
}

// validateArray 验证数组
func (v *JSONSchemaValidator) validateArray(fieldName string, value interface{}, schema *core.PropertySchema) error {
	val := reflect.ValueOf(value)
	if val.Kind() != reflect.Slice && val.Kind() != reflect.Array {
		return &core.ErrInvalidInput{
			Field:   fieldName,
			Message: fmt.Sprintf("expected array, got %T", value),
		}
	}

	// 验证每个元素
	if schema.Items != nil {
		for i := 0; i < val.Len(); i++ {
			elem := val.Index(i).Interface()
			elemName := fmt.Sprintf("%s[%d]", fieldName, i)
			if err := v.validateValue(elemName, elem, schema.Items); err != nil {
				return err
			}
		}
	}

	return nil
}

// validateObject 验证对象
func (v *JSONSchemaValidator) validateObject(fieldName string, value interface{}) error {
	if _, ok := value.(map[string]interface{}); !ok {
		return &core.ErrInvalidInput{
			Field:   fieldName,
			Message: fmt.Sprintf("expected object, got %T", value),
		}
	}
	return nil
}

// validateFormat 验证格式
func (v *JSONSchemaValidator) validateFormat(value, format string) error {
	switch format {
	case "email":
		if !strings.Contains(value, "@") {
			return agentErrors.New(agentErrors.CodeToolValidation, "invalid email format").
				WithComponent("json_schema_validator").
				WithOperation("validate_format").
				WithContext("value", value)
		}
	case "uri", "url":
		if !strings.HasPrefix(value, "http://") && !strings.HasPrefix(value, "https://") {
			return agentErrors.New(agentErrors.CodeToolValidation, "invalid URL format").
				WithComponent("json_schema_validator").
				WithOperation("validate_format").
				WithContext("value", value)
		}
	case "uuid":
		// 简单的 UUID 格式验证
		if matched, _ := regexp.MatchString(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`, value); !matched {
			return agentErrors.New(agentErrors.CodeToolValidation, "invalid UUID format").
				WithComponent("json_schema_validator").
				WithOperation("validate_format").
				WithContext("value", value)
		}
	}
	return nil
}

// ValidateOutput 验证输出结果
func (v *JSONSchemaValidator) ValidateOutput(schema *core.ToolSchema, output interface{}) error {
	// 输出验证可以根据需要实现
	return nil
}
