package core

import (
	"context"
	"errors"
	"github.com/kart-io/goagent/utils/json"
	"time"
)

var (
	// ErrNotImplemented is returned when an abstract method is called on a base type
	ErrNotImplemented = errors.New("method must be implemented by concrete tool")
)

// Tool MCP 工具接口
//
// Tool 代表一个可以被 AI Agent 调用的工具，符合 MCP (Model Context Protocol) 规范。
// 每个工具都有明确的输入输出定义，支持 JSON Schema 验证。
type Tool interface {
	// Name 返回工具名称（唯一标识符）
	Name() string

	// Description 返回工具描述（用于 AI 理解工具用途）
	Description() string

	// Category 返回工具分类（filesystem/network/database/system等）
	Category() string

	// Schema 返回工具的参数 JSON Schema
	Schema() *ToolSchema

	// Execute 执行工具
	Execute(ctx context.Context, input map[string]interface{}) (*ToolResult, error)

	// Validate 验证输入参数
	Validate(input map[string]interface{}) error

	// RequiresAuth 是否需要权限认证
	RequiresAuth() bool

	// IsDangerous 是否是危险操作
	IsDangerous() bool
}

// ToolSchema 工具的 JSON Schema 定义
type ToolSchema struct {
	// Type 必须是 "object"
	Type string `json:"type"`

	// Properties 参数定义
	Properties map[string]PropertySchema `json:"properties"`

	// Required 必需参数列表
	Required []string `json:"required,omitempty"`

	// AdditionalProperties 是否允许额外属性
	AdditionalProperties bool `json:"additionalProperties,omitempty"`
}

// PropertySchema 参数属性定义
type PropertySchema struct {
	// Type 参数类型 (string/number/boolean/object/array)
	Type string `json:"type"`

	// Description 参数描述
	Description string `json:"description,omitempty"`

	// Default 默认值
	Default interface{} `json:"default,omitempty"`

	// Enum 枚举值
	Enum []interface{} `json:"enum,omitempty"`

	// Format 格式约束 (email/uri/date-time等)
	Format string `json:"format,omitempty"`

	// Pattern 正则表达式模式
	Pattern string `json:"pattern,omitempty"`

	// Minimum 最小值（数字类型）
	Minimum *float64 `json:"minimum,omitempty"`

	// Maximum 最大值（数字类型）
	Maximum *float64 `json:"maximum,omitempty"`

	// MinLength 最小长度（字符串类型）
	MinLength *int `json:"minLength,omitempty"`

	// MaxLength 最大长度（字符串类型）
	MaxLength *int `json:"maxLength,omitempty"`

	// Items 数组项定义
	Items *PropertySchema `json:"items,omitempty"`
}

// ToolResult 工具执行结果
type ToolResult struct {
	// Success 是否成功
	Success bool `json:"success"`

	// Data 结果数据
	Data interface{} `json:"data,omitempty"`

	// Error 错误信息
	Error string `json:"error,omitempty"`

	// ErrorCode 错误代码
	ErrorCode string `json:"error_code,omitempty"`

	// Metadata 额外元数据
	Metadata map[string]interface{} `json:"metadata,omitempty"`

	// Duration 执行耗时
	Duration time.Duration `json:"duration"`

	// Timestamp 时间戳
	Timestamp time.Time `json:"timestamp"`

	// IsStreaming 是否是流式输出
	IsStreaming bool `json:"is_streaming,omitempty"`

	// StreamChannel 流式输出通道
	StreamChannel <-chan interface{} `json:"-"`
}

// ToolCall 工具调用请求
type ToolCall struct {
	// ID 调用 ID（用于追踪）
	ID string `json:"id"`

	// ToolName 工具名称
	ToolName string `json:"tool_name"`

	// Input 输入参数
	Input map[string]interface{} `json:"input"`

	// Context 上下文信息
	Context map[string]interface{} `json:"context,omitempty"`

	// Timestamp 调用时间
	Timestamp time.Time `json:"timestamp"`

	// SessionID 会话 ID
	SessionID string `json:"session_id,omitempty"`

	// UserID 用户 ID
	UserID string `json:"user_id,omitempty"`
}

// ToolCallResult 工具调用结果（包含调用信息）
type ToolCallResult struct {
	// Call 原始调用请求
	Call *ToolCall `json:"call"`

	// Result 执行结果
	Result *ToolResult `json:"result"`

	// ExecutedAt 执行时间
	ExecutedAt time.Time `json:"executed_at"`

	// CompletedAt 完成时间
	CompletedAt time.Time `json:"completed_at"`
}

// ToolMetadata 工具元数据
type ToolMetadata struct {
	// Name 工具名称
	Name string `json:"name"`

	// Description 工具描述
	Description string `json:"description"`

	// Category 工具分类
	Category string `json:"category"`

	// Version 工具版本
	Version string `json:"version"`

	// Author 工具作者
	Author string `json:"author,omitempty"`

	// Schema 参数 Schema
	Schema *ToolSchema `json:"schema"`

	// RequiresAuth 是否需要认证
	RequiresAuth bool `json:"requires_auth"`

	// IsDangerous 是否危险
	IsDangerous bool `json:"is_dangerous"`

	// Tags 标签
	Tags []string `json:"tags,omitempty"`

	// Examples 使用示例
	Examples []ToolExample `json:"examples,omitempty"`

	// RateLimit 速率限制（每分钟调用次数）
	RateLimit int `json:"rate_limit,omitempty"`

	// Timeout 超时时间
	Timeout time.Duration `json:"timeout,omitempty"`
}

// ToolExample 工具使用示例
type ToolExample struct {
	// Description 示例描述
	Description string `json:"description"`

	// Input 输入示例
	Input map[string]interface{} `json:"input"`

	// Output 输出示例
	Output interface{} `json:"output"`
}

// ToJSON 转换为 JSON 字符串
func (t *ToolMetadata) ToJSON() (string, error) {
	data, err := json.MarshalIndent(t, "", "  ")
	if err != nil {
		return "", err
	}
	return string(data), nil
}

// BaseTool 提供 Tool 的基础实现
type BaseTool struct {
	name         string
	description  string
	category     string
	schema       *ToolSchema
	requiresAuth bool
	isDangerous  bool
}

// NewBaseTool 创建基础工具
func NewBaseTool(name, description, category string, schema *ToolSchema) *BaseTool {
	return &BaseTool{
		name:         name,
		description:  description,
		category:     category,
		schema:       schema,
		requiresAuth: false,
		isDangerous:  false,
	}
}

// Name 返回工具名称
func (b *BaseTool) Name() string {
	return b.name
}

// Description 返回工具描述
func (b *BaseTool) Description() string {
	return b.description
}

// Category 返回工具分类
func (b *BaseTool) Category() string {
	return b.category
}

// Schema 返回参数 Schema
func (b *BaseTool) Schema() *ToolSchema {
	return b.schema
}

// RequiresAuth 返回是否需要认证
func (b *BaseTool) RequiresAuth() bool {
	return b.requiresAuth
}

// IsDangerous 返回是否危险
func (b *BaseTool) IsDangerous() bool {
	return b.isDangerous
}

// SetRequiresAuth 设置是否需要认证
func (b *BaseTool) SetRequiresAuth(requiresAuth bool) {
	b.requiresAuth = requiresAuth
}

// SetIsDangerous 设置是否危险
func (b *BaseTool) SetIsDangerous(isDangerous bool) {
	b.isDangerous = isDangerous
}

// Execute returns ErrNotImplemented.
//
// Concrete tool implementations must override this method.
// Using composition: embed BaseTool and implement Execute.
func (b *BaseTool) Execute(ctx context.Context, input map[string]interface{}) (*ToolResult, error) {
	return nil, ErrNotImplemented
}

// Validate returns ErrNotImplemented.
//
// Concrete tool implementations must override this method.
// Using composition: embed BaseTool and implement Validate.
func (b *BaseTool) Validate(input map[string]interface{}) error {
	return ErrNotImplemented
}
