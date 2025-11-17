package parsers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"

	agentErrors "github.com/kart-io/goagent/errors"
)

var (
	ErrParseFailed    = errors.New("failed to parse output")
	ErrInvalidFormat  = errors.New("invalid output format")
	ErrMissingField   = errors.New("missing required field")
	ErrTypeConversion = errors.New("type conversion failed")
)

// OutputParser 定义输出解析器接口
//
// 借鉴 LangChain 的 OutputParser 设计，提供结构化的 LLM 输出解析
// 泛型参数 T 指定输出类型
type OutputParser[T any] interface {
	// Parse 解析文本输出为结构化数据
	Parse(ctx context.Context, text string) (T, error)

	// ParseWithPrompt 带提示信息的解析（用于错误恢复）
	ParseWithPrompt(ctx context.Context, text, prompt string) (T, error)

	// GetFormatInstructions 获取格式化指令
	// 这些指令会添加到 prompt 中，告诉 LLM 如何格式化输出
	GetFormatInstructions() string

	// GetType 获取输出类型描述
	GetType() string
}

// BaseOutputParser 提供 OutputParser 的基础实现
type BaseOutputParser[T any] struct {
	typeName string
}

// NewBaseOutputParser 创建基础输出解析器
func NewBaseOutputParser[T any]() *BaseOutputParser[T] {
	var zero T
	t := reflect.TypeOf(zero)
	typeName := "unknown"
	if t != nil {
		typeName = t.String()
	}

	return &BaseOutputParser[T]{
		typeName: typeName,
	}
}

// GetType 获取类型名称
func (p *BaseOutputParser[T]) GetType() string {
	return p.typeName
}

// ParseWithPrompt 默认实现：忽略 prompt，直接调用 Parse
func (p *BaseOutputParser[T]) ParseWithPrompt(ctx context.Context, text, prompt string) (T, error) {
	return p.Parse(ctx, text)
}

// Parse 需要由子类实现
func (p *BaseOutputParser[T]) Parse(ctx context.Context, text string) (T, error) {
	var zero T
	return zero, agentErrors.New(agentErrors.CodeNotImplemented, "Parse method must be implemented").
		WithComponent("base_output_parser").
		WithOperation("parse")
}

// GetFormatInstructions 需要由子类实现
func (p *BaseOutputParser[T]) GetFormatInstructions() string {
	return ""
}

// JSONOutputParser JSON 输出解析器
//
// 解析 LLM 输出中的 JSON 内容
type JSONOutputParser[T any] struct {
	*BaseOutputParser[T]
	strict bool // 是否严格模式（要求完整的 JSON 格式）
}

// NewJSONOutputParser 创建 JSON 输出解析器
func NewJSONOutputParser[T any](strict bool) *JSONOutputParser[T] {
	return &JSONOutputParser[T]{
		BaseOutputParser: NewBaseOutputParser[T](),
		strict:           strict,
	}
}

// Parse 解析 JSON 输出
func (p *JSONOutputParser[T]) Parse(ctx context.Context, text string) (T, error) {
	var result T

	// 提取 JSON（支持 markdown 代码块）
	jsonStr := p.extractJSON(text)
	if jsonStr == "" {
		return result, agentErrors.Wrap(ErrParseFailed, agentErrors.CodeParserInvalidJSON, "no JSON found in output").
			WithComponent("json_parser").
			WithOperation("parse").
			WithContext("text_length", len(text))
	}

	// 解析 JSON
	if err := json.Unmarshal([]byte(jsonStr), &result); err != nil {
		return result, agentErrors.Wrap(err, agentErrors.CodeParserInvalidJSON, "failed to unmarshal JSON").
			WithComponent("json_parser").
			WithOperation("parse").
			WithContext("json_snippet", jsonStr[:min(100, len(jsonStr))])
	}

	return result, nil
}

// extractJSON 从文本中提取 JSON
func (p *JSONOutputParser[T]) extractJSON(text string) string {
	// 尝试提取 markdown 代码块中的 JSON
	if strings.Contains(text, "```json") {
		start := strings.Index(text, "```json")
		if start != -1 {
			start += 7 // len("```json")
			end := strings.Index(text[start:], "```")
			if end != -1 {
				return strings.TrimSpace(text[start : start+end])
			}
		}
	}

	// 尝试提取纯 JSON（查找 { } 或 [ ]）
	text = strings.TrimSpace(text)

	// 找到第一个 { 或 [
	startIdx := -1
	startChar := ""
	for i, ch := range text {
		if ch == '{' || ch == '[' {
			startIdx = i
			startChar = string(ch)
			break
		}
	}

	if startIdx == -1 {
		return ""
	}

	// 找到对应的结束符
	endChar := "}"
	if startChar == "[" {
		endChar = "]"
	}

	depth := 0
	for i := startIdx; i < len(text); i++ {
		ch := string(text[i])
		switch ch {
		case startChar:
			depth++
		case endChar:
			depth--
			if depth == 0 {
				return text[startIdx : i+1]
			}
		}
	}

	// 如果不是严格模式，返回从起始位置到末尾
	if !p.strict {
		return text[startIdx:]
	}

	return ""
}

// GetFormatInstructions 获取格式化指令
func (p *JSONOutputParser[T]) GetFormatInstructions() string {
	var zero T
	t := reflect.TypeOf(zero)

	instructions := "You must format your response as a JSON object"

	// 如果是结构体，生成字段说明
	if t.Kind() == reflect.Struct {
		instructions += " with the following fields:\n\n"
		for i := 0; i < t.NumField(); i++ {
			field := t.Field(i)
			jsonTag := field.Tag.Get("json")
			if jsonTag == "" {
				jsonTag = field.Name
			} else {
				// 提取 json 标签的名称部分
				jsonTag = strings.Split(jsonTag, ",")[0]
			}

			instructions += fmt.Sprintf("- `%s`: (%s) %s\n",
				jsonTag,
				field.Type.String(),
				field.Tag.Get("description"),
			)
		}
	}

	instructions += "\nExample format:\n```json\n" + p.generateExample() + "\n```"

	return instructions
}

// generateExample 生成示例 JSON
func (p *JSONOutputParser[T]) generateExample() string {
	var zero T
	example, _ := json.MarshalIndent(zero, "", "  ")
	return string(example)
}

// StructuredOutputParser 结构化输出解析器
//
// 支持自定义字段的结构化解析
type StructuredOutputParser[T any] struct {
	*BaseOutputParser[T]
	schema map[string]FieldSchema // 字段模式
}

// FieldSchema 字段模式定义
type FieldSchema struct {
	Name        string // 字段名称
	Type        string // 字段类型
	Description string // 字段描述
	Required    bool   // 是否必需
}

// NewStructuredOutputParser 创建结构化输出解析器
func NewStructuredOutputParser[T any](schema map[string]FieldSchema) *StructuredOutputParser[T] {
	return &StructuredOutputParser[T]{
		BaseOutputParser: NewBaseOutputParser[T](),
		schema:           schema,
	}
}

// Parse 解析结构化输出
func (p *StructuredOutputParser[T]) Parse(ctx context.Context, text string) (T, error) {
	var result T

	// 提取所有字段
	fields := make(map[string]string)
	for fieldName := range p.schema {
		value := p.extractField(text, fieldName)
		if value != "" {
			fields[fieldName] = value
		}
	}

	// 检查必需字段
	for fieldName, schema := range p.schema {
		if schema.Required && fields[fieldName] == "" {
			return result, agentErrors.Wrap(ErrMissingField, agentErrors.CodeParserMissingField, "required field not found").
				WithComponent("structured_parser").
				WithOperation("parse").
				WithContext("field", fieldName).
				WithContext("field_type", schema.Type)
		}
	}

	// 构造 JSON 并解析
	jsonData, err := json.Marshal(fields)
	if err != nil {
		return result, agentErrors.Wrap(err, agentErrors.CodeParserFailed, "failed to marshal fields to JSON").
			WithComponent("structured_parser").
			WithOperation("parse").
			WithContext("fields_count", len(fields))
	}

	if err := json.Unmarshal(jsonData, &result); err != nil {
		return result, agentErrors.Wrap(err, agentErrors.CodeParserFailed, "failed to unmarshal to result type").
			WithComponent("structured_parser").
			WithOperation("parse").
			WithContext("fields_count", len(fields))
	}

	return result, nil
}

// extractField 从文本中提取字段值
func (p *StructuredOutputParser[T]) extractField(text, fieldName string) string {
	// 查找 "field_name: value" 模式
	patterns := []string{
		fmt.Sprintf("%s: ", fieldName),
		fmt.Sprintf("%s:", fieldName),
		fmt.Sprintf("**%s**: ", fieldName),
		fmt.Sprintf("**%s**:", fieldName),
	}

	for _, pattern := range patterns {
		idx := strings.Index(text, pattern)
		if idx != -1 {
			start := idx + len(pattern)
			end := strings.Index(text[start:], "\n")
			if end == -1 {
				return strings.TrimSpace(text[start:])
			}
			return strings.TrimSpace(text[start : start+end])
		}
	}

	return ""
}

// GetFormatInstructions 获取格式化指令
func (p *StructuredOutputParser[T]) GetFormatInstructions() string {
	instructions := "You must format your response with the following fields:\n\n"

	for fieldName, schema := range p.schema {
		required := ""
		if schema.Required {
			required = " (REQUIRED)"
		}
		instructions += fmt.Sprintf("**%s**%s: %s\n", fieldName, required, schema.Description)
	}

	instructions += "\nExample format:\n"
	for fieldName := range p.schema {
		instructions += fmt.Sprintf("%s: <value>\n", fieldName)
	}

	return instructions
}

// ListOutputParser 列表输出解析器
//
// 解析列表格式的输出（如逗号分隔、换行分隔等）
type ListOutputParser struct {
	*BaseOutputParser[[]string]
	separator string // 分隔符
}

// NewListOutputParser 创建列表输出解析器
func NewListOutputParser(separator string) *ListOutputParser {
	if separator == "" {
		separator = "\n"
	}

	return &ListOutputParser{
		BaseOutputParser: NewBaseOutputParser[[]string](),
		separator:        separator,
	}
}

// Parse 解析列表输出
func (p *ListOutputParser) Parse(ctx context.Context, text string) ([]string, error) {
	text = strings.TrimSpace(text)

	// 如果是编号列表，移除编号
	lines := strings.Split(text, "\n")
	result := make([]string, 0, len(lines))

	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		// 移除列表标记（1., -, *, 等）
		line = strings.TrimLeft(line, "0123456789.-*• ")
		if line != "" {
			result = append(result, line)
		}
	}

	// 如果使用自定义分隔符
	if p.separator != "\n" && len(result) == 1 {
		result = strings.Split(result[0], p.separator)
		for i, item := range result {
			result[i] = strings.TrimSpace(item)
		}
	}

	return result, nil
}

// GetFormatInstructions 获取格式化指令
func (p *ListOutputParser) GetFormatInstructions() string {
	if p.separator == "\n" {
		return "Format your response as a numbered or bulleted list, one item per line."
	}
	return fmt.Sprintf("Format your response as a list of items separated by '%s'.", p.separator)
}

// EnumOutputParser 枚举输出解析器
//
// 限制输出必须是预定义的枚举值之一
type EnumOutputParser struct {
	*BaseOutputParser[string]
	allowedValues []string
	caseSensitive bool
}

// NewEnumOutputParser 创建枚举输出解析器
func NewEnumOutputParser(allowedValues []string, caseSensitive bool) *EnumOutputParser {
	return &EnumOutputParser{
		BaseOutputParser: NewBaseOutputParser[string](),
		allowedValues:    allowedValues,
		caseSensitive:    caseSensitive,
	}
}

// Parse 解析枚举输出
func (p *EnumOutputParser) Parse(ctx context.Context, text string) (string, error) {
	text = strings.TrimSpace(text)

	for _, allowed := range p.allowedValues {
		if p.caseSensitive {
			if text == allowed {
				return text, nil
			}
		} else {
			if strings.EqualFold(text, allowed) {
				return allowed, nil // 返回标准格式
			}
		}
	}

	return "", agentErrors.Wrap(ErrInvalidFormat, agentErrors.CodeParserFailed, "value is not in allowed enum values").
		WithComponent("enum_parser").
		WithOperation("parse").
		WithContext("value", text).
		WithContext("allowed_values", p.allowedValues)
}

// GetFormatInstructions 获取格式化指令
func (p *EnumOutputParser) GetFormatInstructions() string {
	return fmt.Sprintf("Your response must be exactly one of the following values: %s",
		strings.Join(p.allowedValues, ", "))
}

// BooleanOutputParser 布尔输出解析器
//
// 解析是/否类型的输出
type BooleanOutputParser struct {
	*BaseOutputParser[bool]
	trueValues  []string
	falseValues []string
}

// NewBooleanOutputParser 创建布尔输出解析器
func NewBooleanOutputParser() *BooleanOutputParser {
	return &BooleanOutputParser{
		BaseOutputParser: NewBaseOutputParser[bool](),
		trueValues:       []string{"yes", "true", "y", "1", "是", "对", "correct"},
		falseValues:      []string{"no", "false", "n", "0", "否", "错", "incorrect"},
	}
}

// Parse 解析布尔输出
func (p *BooleanOutputParser) Parse(ctx context.Context, text string) (bool, error) {
	text = strings.ToLower(strings.TrimSpace(text))

	for _, trueVal := range p.trueValues {
		if text == trueVal || strings.Contains(text, trueVal) {
			return true, nil
		}
	}

	for _, falseVal := range p.falseValues {
		if text == falseVal || strings.Contains(text, falseVal) {
			return false, nil
		}
	}

	return false, agentErrors.Wrap(ErrParseFailed, agentErrors.CodeParserFailed, "cannot determine boolean value from text").
		WithComponent("boolean_parser").
		WithOperation("parse").
		WithContext("text", text).
		WithContext("true_values", p.trueValues).
		WithContext("false_values", p.falseValues)
}

// GetFormatInstructions 获取格式化指令
func (p *BooleanOutputParser) GetFormatInstructions() string {
	return "Answer with either 'yes' or 'no'."
}

// RegexOutputParser 正则表达式输出解析器
//
// 使用正则表达式提取输出
type RegexOutputParser struct {
	*BaseOutputParser[map[string]string]
	patterns map[string]string // field name -> regex pattern
}

// NewRegexOutputParser 创建正则表达式输出解析器
func NewRegexOutputParser(patterns map[string]string) *RegexOutputParser {
	return &RegexOutputParser{
		BaseOutputParser: NewBaseOutputParser[map[string]string](),
		patterns:         patterns,
	}
}

// Parse 解析输出
func (p *RegexOutputParser) Parse(ctx context.Context, text string) (map[string]string, error) {
	// 这里简化实现，实际应该使用 regexp 包
	result := make(map[string]string)

	// TODO: 实现正则匹配
	// 这是一个简化版本，实际需要使用 regexp.MustCompile 等

	return result, nil
}

// GetFormatInstructions 获取格式化指令
func (p *RegexOutputParser) GetFormatInstructions() string {
	return "Format your response according to the specified pattern."
}

// ChainOutputParser 链式输出解析器
//
// 尝试多个解析器，使用第一个成功的
type ChainOutputParser[T any] struct {
	parsers []OutputParser[T]
}

// NewChainOutputParser 创建链式输出解析器
func NewChainOutputParser[T any](parsers ...OutputParser[T]) *ChainOutputParser[T] {
	return &ChainOutputParser[T]{
		parsers: parsers,
	}
}

// Parse 尝试所有解析器
func (p *ChainOutputParser[T]) Parse(ctx context.Context, text string) (T, error) {
	var lastErr error
	for _, parser := range p.parsers {
		result, err := parser.Parse(ctx, text)
		if err == nil {
			return result, nil
		}
		lastErr = err
	}

	var zero T
	return zero, agentErrors.Wrap(lastErr, agentErrors.CodeParserFailed, "all parsers in chain failed").
		WithComponent("chain_parser").
		WithOperation("parse").
		WithContext("parsers_count", len(p.parsers))
}

// ParseWithPrompt 带提示的解析
func (p *ChainOutputParser[T]) ParseWithPrompt(ctx context.Context, text, prompt string) (T, error) {
	var lastErr error
	for _, parser := range p.parsers {
		result, err := parser.ParseWithPrompt(ctx, text, prompt)
		if err == nil {
			return result, nil
		}
		lastErr = err
	}

	var zero T
	return zero, agentErrors.Wrap(lastErr, agentErrors.CodeParserFailed, "all parsers in chain failed with prompt").
		WithComponent("chain_parser").
		WithOperation("parse_with_prompt").
		WithContext("parsers_count", len(p.parsers)).
		WithContext("prompt_length", len(prompt))
}

// GetFormatInstructions 获取格式化指令
func (p *ChainOutputParser[T]) GetFormatInstructions() string {
	if len(p.parsers) > 0 {
		return p.parsers[0].GetFormatInstructions()
	}
	return ""
}

// GetType 获取类型
func (p *ChainOutputParser[T]) GetType() string {
	if len(p.parsers) > 0 {
		return p.parsers[0].GetType()
	}
	return "unknown"
}

// min returns the minimum of two integers
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
