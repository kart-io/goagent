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

	// SessionID 用于检查点保存
	SessionID string

	// Verbose 启用详细日志
	Verbose bool
}

// DefaultAgentConfig 返回默认配置
func DefaultAgentConfig() *AgentConfig {
	return &AgentConfig{
		MaxIterations:   10,
		Timeout:         core.DefaultAgentExecutionTimeout,
		EnableStreaming: false,
		EnableAutoSave:  true,
		SaveInterval:    30 * time.Second,
		MaxTokens:       2000,
		Temperature:     0.7,
		SessionID:       fmt.Sprintf("session-%d", time.Now().Unix()),
		Verbose:         false,
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
