package tools

import (
	"context"
	"errors"
	"fmt"
	"net/mail"
	"regexp"
	"strings"

	agentErrors "github.com/kart-io/goagent/errors"
	"github.com/kart-io/goagent/interfaces"
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
			return agentErrors.New(agentErrors.CodeToolValidation, "tool custom validation failed: "+validationReason(err)).
				WithComponent("input_validator").
				WithOperation("validate").
				WithContext("tool_name", tool.Name()).
				WithContext("validation_error", err.Error())
		}
	}

	// 2. 解析 JSON Schema
	schema, err := v.parseSchema(tool.ArgsSchema())
	if err != nil {
		return agentErrors.New(agentErrors.CodeInvalidInput, "schema parsing failed: "+validationReason(err)).
			WithComponent("input_validator").
			WithOperation("parse_schema").
			WithContext("tool_name", tool.Name()).
			WithContext("error", err.Error())
	}

	// 3. 验证必需参数
	if v.ValidateRequired {
		if err := v.validateRequired(schema, input.Args); err != nil {
			return agentErrors.New(agentErrors.CodeToolValidation, "required parameter validation failed: "+validationReason(err)).
				WithComponent("input_validator").
				WithOperation("validate_required").
				WithContext("tool_name", tool.Name()).
				WithContext("validation_error", err.Error())
		}
	}

	// 4. 验证参数类型
	if v.ValidateTypes {
		if err := v.validateTypes(schema, input.Args); err != nil {
			return agentErrors.New(agentErrors.CodeToolValidation, "parameter type validation failed: "+validationReason(err)).
				WithComponent("input_validator").
				WithOperation("validate_types").
				WithContext("tool_name", tool.Name()).
				WithContext("validation_error", err.Error())
		}
	}

	// 5. 严格模式：验证是否有未定义的参数
	if v.StrictMode {
		if err := v.validateNoExtraArgs(schema, input.Args); err != nil {
			return agentErrors.New(agentErrors.CodeToolValidation, "extra parameters not allowed: "+validationReason(err)).
				WithComponent("input_validator").
				WithOperation("validate_strict").
				WithContext("tool_name", tool.Name()).
				WithContext("validation_error", err.Error())
		}
	}

	return nil
}

// validationReason 返回校验失败的具体原因：AgentError 取固定描述的 Message 与参数名，不含参数值，
// 因为 Error() 会交还模型。非 AgentError（如自定义校验的 fmt.Errorf）原文可能含参数值，只返回固定文本，
// 原文由调用方放入 Context。
func validationReason(err error) string {
	var agentErr *agentErrors.AgentError
	if !errors.As(err, &agentErr) {
		return "校验未通过"
	}
	for _, key := range []string{"parameter", "field", "property"} {
		if name, ok := agentErr.Context[key].(string); ok && name != "" {
			return agentErr.Message + ": " + name
		}
	}
	return agentErr.Message
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
	Format      string        `json:"format"`
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

	// 验证 schema 结构
	// 1. 验证 Type 字段（应为空或 "object"）
	if s.Type != "" && s.Type != "object" {
		return nil, agentErrors.New(agentErrors.CodeInvalidInput, "tool schema type must be 'object'").
			WithComponent("input_validator").
			WithOperation("parse_schema").
			WithContext("got_type", s.Type)
	}

	// 2. 验证属性类型
	validTypes := map[string]bool{
		"string": true, "number": true, "integer": true,
		"boolean": true, "object": true, "array": true, "": true,
	}
	for propName, prop := range s.Properties {
		if !validTypes[prop.Type] {
			return nil, agentErrors.New(agentErrors.CodeInvalidInput, "invalid property type").
				WithComponent("input_validator").
				WithOperation("parse_schema").
				WithContext("property", propName).
				WithContext("type", prop.Type)
		}
	}

	// 3. 验证 required 字段存在于 properties 中
	for _, required := range s.Required {
		if _, exists := s.Properties[required]; !exists {
			return nil, agentErrors.New(agentErrors.CodeInvalidInput, "required field not found in properties").
				WithComponent("input_validator").
				WithOperation("parse_schema").
				WithContext("field", required)
		}
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
			// 验证格式
			if prop.Format != "" {
				if err := validateFormat(s, prop.Format); err != nil {
					return err
				}
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

// validateFormat 验证格式
func validateFormat(value, format string) error {
	switch format {
	case "email":
		// 使用标准库进行 email 验证
		_, err := mail.ParseAddress(value)
		if err != nil {
			return agentErrors.New(agentErrors.CodeToolValidation, "invalid email format").
				WithComponent("validator").
				WithOperation("validate_format").
				WithContext("value", value).
				WithContext("error", err.Error())
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
