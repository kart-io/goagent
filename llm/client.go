package llm

import (
	"context"
	"time"

	"github.com/kart-io/goagent/codec/json"

	"github.com/kart-io/goagent/core/usage"
	"github.com/kart-io/goagent/interfaces"
	"github.com/kart-io/goagent/llm/constants"
)

// Client 是 LLM 客户端的唯一必需接口：流式补全加提供商标识。
// 需要完整响应时用 Complete 收集流；调用方停止读取时必须取消 ctx。
type Client interface {
	CompleteStream(ctx context.Context, req *CompletionRequest) (<-chan *StreamChunk, error)
	Provider() constants.Provider
}

// Message 定义聊天消息
type Message struct {
	Role    string `json:"role"`           // "system", "user", "assistant"
	Content string `json:"content"`        // 消息内容
	Name    string `json:"name,omitempty"` // 可选的消息名称
	// ToolCalls 与 ToolCallID 用于保留 assistant/tool 的原生对话关系。
	ToolCalls        []ToolCall `json:"tool_calls,omitempty"`
	ReasoningContent string     `json:"reasoning_content,omitempty"`
	ToolCallID       string     `json:"tool_call_id,omitempty"`
	// ProviderState preserves opaque response parts (e.g. Gemini signatures).
	// Replay unchanged with the matching assistant content and tool calls.
	ProviderState *ProviderState `json:"provider_state,omitzero"`
}

type ProviderState struct {
	Provider constants.Provider `json:"provider"`
	Data     json.RawMessage    `json:"data"`
}

// CompletionRequest 定义补全请求
type CompletionRequest struct {
	Reasoning *usage.ReasoningConfig `json:"reasoning,omitzero"`
	// TemperatureSet 使 Temperature=0 显式覆盖客户端默认值；JSON 根据字段是否存在自动维护。
	TemperatureSet bool      `json:"-"`
	Messages       []Message `json:"messages"`             // 消息列表
	Temperature    float64   `json:"temperature,omitzero"` // 温度参数 (0.0-2.0)
	MaxTokens      int       `json:"max_tokens,omitzero"`  // 最大 token 数
	Model          string    `json:"model,omitempty"`      // 模型名称
	Stop           []string  `json:"stop,omitempty"`       // 停止序列
	TopP           float64   `json:"top_p,omitzero"`       // Top-p 采样
}

// CompletionResponse 定义补全响应
type CompletionResponse struct {
	ResponseModel string `json:"response_model,omitempty"`
	Content       string `json:"content"` // 生成的内容
	Model         string `json:"model"`   // 使用的模型

	TokensUsed   int                    `json:"tokens_used,omitzero"`    // 使用的 token 数
	FinishReason string                 `json:"finish_reason,omitempty"` // 结束原因
	Provider     string                 `json:"provider,omitempty"`      // 提供商
	Usage        *interfaces.TokenUsage `json:"usage,omitzero"`          // 详细的 Token 使用统计
}

// ResponseFormat 定义响应格式类型
type ResponseFormat struct {
	Type string `json:"type"` // "text" 或 "json_object"
}

// 预定义的响应格式常量
var (
	// ResponseFormatText 普通文本格式
	ResponseFormatText = &ResponseFormat{Type: "text"}
	// ResponseFormatJSON JSON 对象格式
	ResponseFormatJSON = &ResponseFormat{Type: "json_object"}
)

// Config 定义 LLM 配置
type LLMOptions struct {
	Reasoning *usage.ReasoningConfig `json:"reasoning,omitzero"`
	// TemperatureSet 区分显式零与零值配置；WithTemperature 和 JSON 解码自动维护。
	TemperatureSet bool `json:"-"`
	// 基础配置
	Provider       constants.Provider `json:"provider"`                  // 提供商
	APIKey         string             `json:"api_key"`                   // API 密钥
	BaseURL        string             `json:"base_url,omitempty"`        // 自定义 API 端点
	Model          string             `json:"model"`                     // 默认模型
	EmbeddingModel string             `json:"embedding_model,omitempty"` // Separate embedding model, used by OpenAI and Gemini.

	// 生成参数
	MaxTokens      int             `json:"max_tokens"`               // 默认最大 token 数
	Temperature    float64         `json:"temperature"`              // 默认温度 (0.0-2.0)
	TopP           float64         `json:"top_p,omitzero"`           // Top-P 采样参数 (0.0-1.0)
	ResponseFormat *ResponseFormat `json:"response_format,omitzero"` // 响应格式 (json_object 或 text)

	// 网络配置
	Timeout int `json:"timeout"` // 请求超时（秒）
	// StreamIdleTimeout 是流式响应两次收到数据之间的最长间隔，StreamTimeout 是一次流式请求的总时长上限；
	// 超过时中断并报告可重试的超时错误。0 表示不限制。目前作用于 OpenAI 兼容的工具流（含 DeepSeek）。
	StreamIdleTimeout time.Duration `json:"stream_idle_timeout,omitzero"`
	StreamTimeout     time.Duration `json:"stream_timeout,omitzero"`

	// 其他配置
	SystemPrompt  string            `json:"system_prompt,omitempty"`  // 默认系统提示
	CustomHeaders map[string]string `json:"custom_headers,omitempty"` // 自定义 HTTP 头
}

// DefaultConfig 返回默认配置
func DefaultLLMOptions() *LLMOptions {
	return &LLMOptions{
		Provider:    constants.ProviderOpenAI,
		MaxTokens:   2000,
		Temperature: 0.7,
		Timeout:     60,
		TopP:        1.0,
	}
}

// NewMessage 创建新消息
func NewMessage(role, content string) Message {
	return Message{
		Role:    role,
		Content: content,
	}
}

// SystemMessage 创建系统消息
func SystemMessage(content string) Message {
	return NewMessage("system", content)
}

// UserMessage 创建用户消息
func UserMessage(content string) Message {
	return NewMessage("user", content)
}

// AssistantMessage 创建助手消息
func AssistantMessage(content string) Message {
	return NewMessage("assistant", content)
}
