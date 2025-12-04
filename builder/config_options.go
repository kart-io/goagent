package builder

import (
	"fmt"
	"time"

	"github.com/kart-io/goagent/core"
)

// Runtime 配置方法
// 本文件包含 AgentBuilder 的运行时配置方法
//
// API 分层说明：
// - [Simple]: 日常 API，覆盖 80% 使用场景（常用配置）
// - [Core]: 标准 API，覆盖 95% 使用场景（性能调优）
// - [Advanced]: 完整 API，覆盖所有场景（细粒度控制）

// OutputFormat 定义 LLM 输出格式类型
type OutputFormat string

const (
	// OutputFormatDefault 不指定格式，由 LLM 自行决定
	OutputFormatDefault OutputFormat = ""

	// OutputFormatPlainText 纯文本格式，不使用 Markdown 语法
	OutputFormatPlainText OutputFormat = "plain_text"

	// OutputFormatMarkdown Markdown 格式
	OutputFormatMarkdown OutputFormat = "markdown"

	// OutputFormatJSON JSON 格式
	OutputFormatJSON OutputFormat = "json"

	// OutputFormatCustom 自定义格式（需配合 CustomOutputPrompt 使用）
	OutputFormatCustom OutputFormat = "custom"
)

// outputFormatPrompts 存储各格式对应的提示词
var outputFormatPrompts = map[OutputFormat]string{
	OutputFormatPlainText: "请使用纯文本格式回复，不要使用 Markdown 语法。",
	OutputFormatMarkdown:  "请使用 Markdown 格式回复。",
	OutputFormatJSON:      "请使用 JSON 格式回复。",
}

// AgentConfig 保存 Agent 配置选项
type AgentConfig struct {
	// MaxIterations 限制推理步骤的最大次数
	MaxIterations int

	// Timeout 设置 Agent 执行超时时间
	Timeout time.Duration

	// EnableStreaming 启用流式响应
	EnableStreaming bool

	// EnableAutoSave 自动保存状态
	EnableAutoSave bool

	// SaveInterval 自动保存间隔
	SaveInterval time.Duration

	// MaxTokens 限制 LLM 响应的最大 token 数
	MaxTokens int

	// Temperature 控制 LLM 采样的随机性
	Temperature float64

	// SessionID 用于检查点保存和对话记忆
	SessionID string

	// Verbose 启用详细日志
	Verbose bool

	// MaxConversationHistory 限制加载的历史对话轮数
	// 用于控制上下文窗口大小，避免超出 LLM 的 token 限制
	// 0 或负数表示不限制
	MaxConversationHistory int

	// OutputFormat 指定 LLM 输出格式
	// 支持：OutputFormatDefault（默认）、OutputFormatPlainText、OutputFormatMarkdown、OutputFormatJSON、OutputFormatCustom
	OutputFormat OutputFormat

	// CustomOutputPrompt 自定义输出格式提示词
	// 仅当 OutputFormat 为 OutputFormatCustom 时生效
	CustomOutputPrompt string
}

// DefaultAgentConfig 返回默认配置
func DefaultAgentConfig() *AgentConfig {
	return &AgentConfig{
		MaxIterations:          10,
		Timeout:                core.DefaultAgentExecutionTimeout,
		EnableStreaming:        false,
		EnableAutoSave:         true,
		SaveInterval:           30 * time.Second,
		MaxTokens:              2000,
		Temperature:            0.7,
		SessionID:              fmt.Sprintf("session-%d", time.Now().Unix()),
		Verbose:                false,
		MaxConversationHistory: 20, // 默认保留最近 20 轮对话
	}
}

// WithMaxIterations 设置最大迭代次数
//
// [Simple] 常用配置，控制 Agent 推理的最大步骤数（默认 10）。
// 推荐根据任务复杂度调整：简单任务 5-10，复杂任务 15-30。
func (b *AgentBuilder[C, S]) WithMaxIterations(max int) *AgentBuilder[C, S] {
	if max > 0 {
		b.config.MaxIterations = max
	}
	return b
}

// WithTimeout 设置超时时间
//
// [Core] 标准配置，防止 Agent 执行时间过长（默认 5 分钟）。
func (b *AgentBuilder[C, S]) WithTimeout(timeout time.Duration) *AgentBuilder[C, S] {
	if timeout > 0 {
		b.config.Timeout = timeout
	}
	return b
}

// WithStreamingEnabled 设置是否启用流式响应
//
// [Advanced] 高级配置，用于实时流式输出 LLM 响应（需要 LLM 支持）。
func (b *AgentBuilder[C, S]) WithStreamingEnabled(enabled bool) *AgentBuilder[C, S] {
	b.config.EnableStreaming = enabled
	return b
}

// WithAutoSaveEnabled 设置是否启用自动保存
//
// [Advanced] 高级配置，控制是否自动保存 Agent 状态（默认 true）。
func (b *AgentBuilder[C, S]) WithAutoSaveEnabled(enabled bool) *AgentBuilder[C, S] {
	b.config.EnableAutoSave = enabled
	return b
}

// WithSaveInterval 设置自动保存间隔
//
// [Advanced] 高级配置，控制自动保存的时间间隔（默认 30 秒）。
func (b *AgentBuilder[C, S]) WithSaveInterval(interval time.Duration) *AgentBuilder[C, S] {
	if interval > 0 {
		b.config.SaveInterval = interval
	}
	return b
}

// WithMaxTokens 设置最大 token 数
//
// [Core] 标准配置，限制 LLM 响应的最大 token 数（默认 2000）。
// 用于控制成本和响应长度。
func (b *AgentBuilder[C, S]) WithMaxTokens(max int) *AgentBuilder[C, S] {
	if max > 0 {
		b.config.MaxTokens = max
	}
	return b
}

// WithTemperature 设置温度参数（控制随机性）
//
// [Simple] 常用配置，控制 LLM 输出的创造性（默认 0.7）。
// - 0.0-0.3: 精确、确定性（适合事实查询、代码生成）
// - 0.4-0.7: 平衡（适合通用对话）
// - 0.8-1.0: 创造性（适合写作、头脑风暴）
func (b *AgentBuilder[C, S]) WithTemperature(temp float64) *AgentBuilder[C, S] {
	if temp >= 0 && temp <= 2.0 {
		b.config.Temperature = temp
	}
	return b
}

// WithSessionID 设置会话 ID
//
// [Advanced] 高级配置，用于检查点保存和会话恢复（自动生成）。
func (b *AgentBuilder[C, S]) WithSessionID(sessionID string) *AgentBuilder[C, S] {
	if sessionID != "" {
		b.config.SessionID = sessionID
	}
	return b
}

// WithVerbose 设置是否启用详细日志
//
// [Core] 标准配置，用于调试和开发（默认 false）。
func (b *AgentBuilder[C, S]) WithVerbose(verbose bool) *AgentBuilder[C, S] {
	b.config.Verbose = verbose
	return b
}

// WithMaxConversationHistory 设置加载的最大历史对话轮数
//
// [Core] 标准配置，控制对话记忆的上下文窗口大小（默认 20）。
// 用于限制发送给 LLM 的历史对话数量，避免超出 token 限制。
// 设置为 0 或负数表示不限制（加载全部历史）。
//
// 注意：此设置仅在使用 WithMemory 配置了 MemoryManager 时生效。
func (b *AgentBuilder[C, S]) WithMaxConversationHistory(max int) *AgentBuilder[C, S] {
	b.config.MaxConversationHistory = max
	return b
}

// WithOutputFormat 设置 LLM 输出格式
//
// [Simple] 常用配置，控制 LLM 响应的输出格式。
// 支持以下格式：
//   - OutputFormatDefault: 不指定格式，由 LLM 自行决定
//   - OutputFormatPlainText: 纯文本格式，不使用 Markdown 语法（适合终端显示）
//   - OutputFormatMarkdown: Markdown 格式（适合富文本显示）
//   - OutputFormatJSON: JSON 格式（适合程序解析）
//
// 使用示例：
//
//	agent, err := builder.NewSimpleBuilder(llmClient).
//	    WithSystemPrompt("你是一个助手").
//	    WithOutputFormat(builder.OutputFormatPlainText).
//	    Build()
func (b *AgentBuilder[C, S]) WithOutputFormat(format OutputFormat) *AgentBuilder[C, S] {
	b.config.OutputFormat = format
	return b
}

// WithCustomOutputFormat 设置自定义输出格式提示词
//
// [Simple] 常用配置，允许用户指定任意格式提示词。
// 此方法会自动将 OutputFormat 设置为 OutputFormatCustom。
//
// 使用示例：
//
//	agent, err := builder.NewSimpleBuilder(llmClient).
//	    WithSystemPrompt("你是一个助手").
//	    WithCustomOutputFormat("请用表格格式回复，每行一个条目").
//	    Build()
func (b *AgentBuilder[C, S]) WithCustomOutputFormat(prompt string) *AgentBuilder[C, S] {
	if prompt != "" {
		b.config.OutputFormat = OutputFormatCustom
		b.config.CustomOutputPrompt = prompt
	}
	return b
}

// GetOutputFormatPrompt 获取输出格式对应的提示词
// 如果格式为 Default 或未定义，返回空字符串
func GetOutputFormatPrompt(format OutputFormat) string {
	if prompt, ok := outputFormatPrompts[format]; ok {
		return prompt
	}
	return ""
}
