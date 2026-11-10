package builder

import (
	"fmt"
	"time"

	"github.com/kart-io/goagent/builder/contextpolicy"
	shared "github.com/kart-io/goagent/constants"
	"github.com/kart-io/goagent/core/session"

	"github.com/kart-io/goagent/core/budget"
	"github.com/kart-io/goagent/core/usage"
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
	OutputValidators []OutputValidator      `json:"-"`
	Reasoning        *usage.ReasoningConfig `json:"reasoning,omitzero"`
	UsageHook        budget.UsageHook       `json:"-"`
	AgentID          string                 `json:",omitempty"`
	// TokenEstimator 是宿主的保守 Token 估计函数，不写入持久配置。
	TokenEstimator TokenEstimator `json:"-"`
	// MemoryFailurePolicy 控制对话记忆读写失败时是否中断执行。
	MemoryFailurePolicy MemoryFailurePolicy `json:",omitempty"`

	// DurableScope identifies one logical Agent invocation within a durable node.
	// It must be unique among siblings and stable across restarts.
	DurableScope string

	// Offloads 是 WithOffload 装入的结果卸载；仅作用于原生工具流。
	Offloads []*contextpolicy.Offload `json:"-"`

	// ReturnDirectTools 允许单个工具调用直接提供最终答案；仅作用于原生工具流。
	ReturnDirectTools []string

	// EmptyAnswerRetries 是模型正常结束却返回空回答时允许重问的次数，仅作用于原生工具流；0 表示不重问。
	EmptyAnswerRetries int

	// ParallelToolCalls 是同一轮可并发执行的工具调用数上限；小于 2（默认 0）时按顺序执行。
	ParallelToolCalls int

	// ToolErrorFeedback 把可恢复的工具失败作为工具结果交给模型，仅作用于原生工具流；见 WithToolErrorFeedback。
	ToolErrorFeedback bool
	// ReplayablePartialOutput 声明调用方能丢弃失败请求已输出的增量；见 WithReplayablePartialOutput。
	ReplayablePartialOutput bool

	// 循环扩展点与提示词组装，见 loop_hooks.go；仅作用于原生工具流。
	PreStep              []preStep          `json:"-"`
	RequestHooks         []RequestHook      `json:"-"`
	RequestErrorHooks    []RequestErrorHook `json:"-"`
	MaxRequestRecoveries int
	TurnStopping         []TurnStoppingHook `json:"-"`
	PromptSections       []PromptSection    `json:"-"`
	PromptContexts       []PromptContext    `json:"-"`
	SessionLog           session.Store      `json:"-"`
	// SessionState 见 WithSessionState。
	SessionState bool

	// ResendInterruptedModel 见 WithInterruptedModelResend。
	ResendInterruptedModel bool

	// MaxIterations 限制推理步骤的最大次数
	MaxIterations int

	// Timeout 设置 Agent 执行超时时间
	Timeout time.Duration

	// MaxTokens 限制 LLM 响应的最大 token 数；0 表示不下发，由 LLM 客户端配置决定
	MaxTokens int

	// Temperature 控制 LLM 采样的随机性，仅在 TemperatureSet 为真时下发
	Temperature float64

	// TemperatureSet 表示 Temperature 被显式设置；为假时由 LLM 客户端配置决定
	TemperatureSet bool

	// SessionID 标识会话，用于对话记忆与会话日志
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

// WithDurableScope sets a host-owned stable invocation name. Durable execution
// requires it; different agents/calls in the same node must use different scopes.
// Reusing the same scope means replaying the same logical invocation.
func (b *AgentBuilder[S]) WithDurableScope(scope string) *AgentBuilder[S] {
	b.config.DurableScope = scope
	return b
}

// WithInterruptedModelResend 让持久运行恢复时重发被进程停止或崩溃打断的模型请求，同一回合最多发出 3 次。
// 默认不重发，因为被打断的请求可能已经计费：开启后可能重复计费，每次重发各占一份预算，额度不足时不发送。
// 已提交的回合、模型明确失败的请求和工具调用都不受影响。开关须在运行的整个生命周期内保持一致，否则恢复时报策略变更。
func (b *AgentBuilder[S]) WithInterruptedModelResend() *AgentBuilder[S] {
	b.config.ResendInterruptedModel = true
	return b
}

// WithReturnDirectTools 指定结果已可直接展示的终态工具，省去模型总结请求。
// 在 ExecuteStream/AsRunnable 中生效，要求 ToolStreamClient。每次调用替换名单。
// 本轮多个工具调用时仍走完整模型循环；字符串原样输出，其他结果编码为 JSON。
// 启用者负责工具结果的格式、完整性与适用场景，模型输出格式指令不会再被执行。
func (b *AgentBuilder[S]) WithReturnDirectTools(names ...string) *AgentBuilder[S] {
	b.config.ReturnDirectTools = append([]string(nil), names...)
	return b
}

// WithEmptyAnswerRetries 设置空回答重问次数（默认 0，不重问）。
//
// [Advanced] 模型以 stop 结束、没有正文也没有工具调用时，不追加空 assistant 消息，
// 改为追加一条固定的 user 消息要求直接回答或调用工具，再请求下一轮。每次重问占用一次
// MaxIterations 额度并消耗一次预算；超过 n 次或已是最后一轮时按原方式报错。
// 持久模式下空回合按已完成记录，恢复时回放。负数视为 0。
func (b *AgentBuilder[S]) WithEmptyAnswerRetries(n int) *AgentBuilder[S] {
	b.config.EmptyAnswerRetries = max(n, 0)
	return b
}

// WithParallelToolCalls 设置同一轮工具调用的并发上限（默认 0，按顺序执行）。
//
// [Advanced] 仅作用于原生工具流。n>=2 时，一轮有多个调用、且每个调用的工具都实现
// interfaces.ConcurrentCallTool（如 Dispatcher.Tools）时最多 n 个并发执行，实际峰值还受工具自身
// 限流（Dispatcher.MaxConcurrency）约束；不另设上限时传 math.MaxInt。n<2 按顺序执行，负数视为 0；
// 任一工具未声明并发能力时整轮按顺序执行。
//
// 并发时执行先后不保证与模型给出的顺序一致，即使 Dispatcher.MaxConcurrency=1 也一样；工具内部不要
// 按执行顺序分配编号或依赖同轮调用的先后。结果仍按模型给出的顺序交还模型，tool_start 在派发前、
// tool_end 在全部结束后按该顺序发出，Dispatcher 的 OnResult 也在全部结束后按该顺序触发。
// 某个调用失败不取消其他调用，全部结束后返回顺序最靠前的错误。
func (b *AgentBuilder[S]) WithParallelToolCalls(n int) *AgentBuilder[S] {
	b.config.ParallelToolCalls = max(n, 0)
	return b
}

// WithToolErrorFeedback 让模型看到可恢复的工具失败并自行纠正，而不是中止运行（默认关闭）。
//
// [Advanced] 仅作用于原生工具流。开启后以下情况按业务拒绝（tools.Rejected）交给模型，占用正常的迭代额度：
//   - 调用了本轮未提供的工具、参数不是 JSON 对象：由模型输出决定，持久运行中同样生效；
//   - 工具返回 error：仅限非持久运行。持久运行无法记录失败结果，恢复时会重新执行，仍按原方式中止。
//
// 运行 ctx 已结束、预算耗尽、panic 与持久等待始终中止运行。
func (b *AgentBuilder[S]) WithToolErrorFeedback() *AgentBuilder[S] {
	b.config.ToolErrorFeedback = true
	return b
}

// WithReplayablePartialOutput 声明调用方只使用最终结果，或能丢弃失败请求已输出的增量（默认关闭）。
// 开启后模型请求中途失败时，RequestError 钩子看到的 Emitted 恒为 false，RetryTransient 会照常重发。
// 适合 AsRunnable().Invoke 等只取最终答复的调用：服务端过载时流常先输出几个字节再停住，默认规则下这次失败不会重试。
// 通过 ExecuteStream 或 Stream 逐段展示输出的调用方开启后，重试会让已展示的片段再出现一次。
func (b *AgentBuilder[S]) WithReplayablePartialOutput() *AgentBuilder[S] {
	b.config.ReplayablePartialOutput = true
	return b
}

// DefaultAgentConfig 返回默认配置
func DefaultAgentConfig() *AgentConfig {
	return &AgentConfig{
		MaxIterations:          shared.DefaultAgentMaxIterations,
		Timeout:                shared.DefaultAgentExecutionTimeout,
		SessionID:              fmt.Sprintf("session-%d", time.Now().UnixNano()),
		Verbose:                false,
		MaxConversationHistory: shared.DefaultConversationHistory,
	}
}

// WithMaxIterations 设置最大迭代次数
//
// [Simple] 常用配置，控制 Agent 推理的最大步骤数（默认 10）。
// 推荐根据任务复杂度调整：简单任务 5-10，复杂任务 15-30。
func (b *AgentBuilder[S]) WithMaxIterations(max int) *AgentBuilder[S] {
	if max > 0 {
		b.config.MaxIterations = max
	}
	return b
}

// WithTimeout 设置超时时间
//
// [Core] 标准配置，防止 Agent 执行时间过长（默认 5 分钟）。
func (b *AgentBuilder[S]) WithTimeout(timeout time.Duration) *AgentBuilder[S] {
	if timeout > 0 {
		b.config.Timeout = timeout
	}
	return b
}

// WithMaxTokens 设置最大 token 数
//
// [Core] 标准配置，限制 LLM 响应的最大 token 数（默认不设置，沿用 LLM 客户端配置）。
// 用于控制成本和响应长度。
func (b *AgentBuilder[S]) WithMaxTokens(max int) *AgentBuilder[S] {
	if max > 0 {
		b.config.MaxTokens = max
	}
	return b
}

// WithTemperature 设置温度参数（控制随机性）
//
// [Simple] 常用配置，控制 LLM 输出的创造性（默认不设置，沿用 LLM 客户端配置）。
// - 0.0-0.3: 精确、确定性（适合事实查询、代码生成）
// - 0.4-0.7: 平衡（适合通用对话）
// - 0.8-1.0: 创造性（适合写作、头脑风暴）
func (b *AgentBuilder[S]) WithTemperature(temp float64) *AgentBuilder[S] {
	if temp >= 0 && temp <= 2.0 {
		b.config.Temperature, b.config.TemperatureSet = temp, true
	}
	return b
}

// WithSessionID 设置会话 ID
//
// [Advanced] 高级配置，用于检查点保存和会话恢复（自动生成）。
func (b *AgentBuilder[S]) WithSessionID(sessionID string) *AgentBuilder[S] {
	if sessionID != "" {
		b.config.SessionID = sessionID
	}
	return b
}

// WithVerbose 设置是否启用详细日志
//
// [Core] 标准配置，用于调试和开发（默认 false）。
func (b *AgentBuilder[S]) WithVerbose(verbose bool) *AgentBuilder[S] {
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
func (b *AgentBuilder[S]) WithMaxConversationHistory(max int) *AgentBuilder[S] {
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
func (b *AgentBuilder[S]) WithOutputFormat(format OutputFormat) *AgentBuilder[S] {
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
func (b *AgentBuilder[S]) WithCustomOutputFormat(prompt string) *AgentBuilder[S] {
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
