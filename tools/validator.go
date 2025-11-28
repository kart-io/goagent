package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	agentErrors "github.com/kart-io/goagent/errors"
	"github.com/kart-io/goagent/interfaces"
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
		// 如果 schema 解析失败，只记录警告，不阻止执行
		// 这样可以保持向后兼容性
		return nil
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
	Type        string      `json:"type"`
	Description string      `json:"description"`
	Enum        []interface{} `json:"enum"`
	Minimum     *float64    `json:"minimum"`
	Maximum     *float64    `json:"maximum"`
	MinLength   *int        `json:"minLength"`
	MaxLength   *int        `json:"maxLength"`
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
		return nil, fmt.Errorf("failed to parse schema: %w", err)
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
			return fmt.Errorf("required parameter '%s' is missing", required)
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
			return fmt.Errorf("parameter '%s' must be string, got %T", key, value)
		}
		// 验证字符串长度
		if s, ok := value.(string); ok {
			if prop.MinLength != nil && len(s) < *prop.MinLength {
				return fmt.Errorf("parameter '%s' length must be at least %d", key, *prop.MinLength)
			}
			if prop.MaxLength != nil && len(s) > *prop.MaxLength {
				return fmt.Errorf("parameter '%s' length must be at most %d", key, *prop.MaxLength)
			}
		}

	case "number", "integer":
		var num float64
		switch v := value.(type) {
		case float64:
			num = v
		case float32:
			num = float64(v)
		case int:
			num = float64(v)
		case int64:
			num = float64(v)
		case int32:
			num = float64(v)
		default:
			return fmt.Errorf("parameter '%s' must be number, got %T", key, value)
		}

		// 验证数值范围
		if prop.Minimum != nil && num < *prop.Minimum {
			return fmt.Errorf("parameter '%s' must be >= %v", key, *prop.Minimum)
		}
		if prop.Maximum != nil && num > *prop.Maximum {
			return fmt.Errorf("parameter '%s' must be <= %v", key, *prop.Maximum)
		}

		// 对于 integer 类型，验证是否为整数
		if prop.Type == "integer" && num != float64(int(num)) {
			return fmt.Errorf("parameter '%s' must be integer, got float", key)
		}

	case "boolean":
		if _, ok := value.(bool); !ok {
			return fmt.Errorf("parameter '%s' must be boolean, got %T", key, value)
		}

	case "array":
		switch value.(type) {
		case []interface{}, []string, []int, []float64, []bool:
			// 有效的数组类型
		default:
			return fmt.Errorf("parameter '%s' must be array, got %T", key, value)
		}

	case "object":
		if _, ok := value.(map[string]interface{}); !ok {
			return fmt.Errorf("parameter '%s' must be object, got %T", key, value)
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
			return fmt.Errorf("parameter '%s' must be one of %v, got %v", key, prop.Enum, value)
		}
	}

	return nil
}

// validateNoExtraArgs 验证是否有未定义的参数
func (v *InputValidator) validateNoExtraArgs(s *schema, args map[string]interface{}) error {
	for key := range args {
		if _, exists := s.Properties[key]; !exists {
			return fmt.Errorf("unexpected parameter '%s' (not defined in schema)", key)
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
