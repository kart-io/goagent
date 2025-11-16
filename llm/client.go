package llm

import (
	"context"
)

// Provider 定义 LLM 提供商类型
type Provider string

const (
	ProviderOpenAI      Provider = "openai"
	ProviderGemini      Provider = "gemini"
	ProviderDeepSeek    Provider = "deepseek"
	ProviderOllama      Provider = "ollama"
	ProviderSiliconFlow Provider = "siliconflow"
	ProviderKimi        Provider = "kimi"
	ProviderCustom      Provider = "custom"
)

// Client 定义 LLM 客户端接口
type Client interface {
	// Complete 生成文本补全
	Complete(ctx context.Context, req *CompletionRequest) (*CompletionResponse, error)

	// Chat 进行对话
	Chat(ctx context.Context, messages []Message) (*CompletionResponse, error)

	// Provider 返回提供商类型
	Provider() Provider

	// IsAvailable 检查 LLM 是否可用
	IsAvailable() bool
}

// Message 定义聊天消息
type Message struct {
	Role    string `json:"role"`           // "system", "user", "assistant"
	Content string `json:"content"`        // 消息内容
	Name    string `json:"name,omitempty"` // 可选的消息名称
}

// CompletionRequest 定义补全请求
type CompletionRequest struct {
	Messages    []Message `json:"messages"`              // 消息列表
	Temperature float64   `json:"temperature,omitempty"` // 温度参数 (0.0-2.0)
	MaxTokens   int       `json:"max_tokens,omitempty"`  // 最大 token 数
	Model       string    `json:"model,omitempty"`       // 模型名称
	Stop        []string  `json:"stop,omitempty"`        // 停止序列
	TopP        float64   `json:"top_p,omitempty"`       // Top-p 采样
}

// CompletionResponse 定义补全响应
type CompletionResponse struct {
	Content      string `json:"content"`                 // 生成的内容
	Model        string `json:"model"`                   // 使用的模型
	TokensUsed   int    `json:"tokens_used,omitempty"`   // 使用的 token 数
	FinishReason string `json:"finish_reason,omitempty"` // 结束原因
	Provider     string `json:"provider,omitempty"`      // 提供商
}

// Config 定义 LLM 配置
type Config struct {
	Provider    Provider `json:"provider"`           // 提供商
	APIKey      string   `json:"api_key"`            // API 密钥
	BaseURL     string   `json:"base_url,omitempty"` // 自定义 API 端点
	Model       string   `json:"model"`              // 默认模型
	MaxTokens   int      `json:"max_tokens"`         // 默认最大 token 数
	Temperature float64  `json:"temperature"`        // 默认温度
	Timeout     int      `json:"timeout"`            // 请求超时（秒）
}

// DefaultConfig 返回默认配置
func DefaultConfig() *Config {
	return &Config{
		Provider:    ProviderOpenAI,
		MaxTokens:   2000,
		Temperature: 0.7,
		Timeout:     60,
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
