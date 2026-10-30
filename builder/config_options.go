package builder

import (
	"fmt"
	"github.com/kart-io/goagent/builder/contextpolicy"
	"github.com/kart-io/goagent/interfaces"
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
	// DurableScope identifies one logical Agent invocation within a durable node.
	// It must be unique among siblings and stable across restarts.
	DurableScope string

	// ContextPolicy is opt-in governance for the native tool execution path.
	ContextPolicy     *ContextPolicy
	ContextPolicies   []contextpolicy.Factory                            `json:"-"`
	PolicyToolAdapter func([]interfaces.Tool) ([]interfaces.Tool, error) `json:"-"`

	// ReturnDirectTools 允许单个工具调用直接提供最终答案；仅作用于原生工具流。
	ReturnDirectTools []string

	// EmptyAnswerRetries 是模型正常结束却返回空回答时允许重问的次数，仅作用于原生工具流；0 表示不重问。
	EmptyAnswerRetries int

	// ModelRetries 是单次模型回合在暂时性失败后的重试次数，仅作用于原生流路径；0 表示不重试。
	ModelRetries int
	// ModelRetryBackoff 给出第 n 次重试前的等待时长；nil 时为 500ms 起指数增长、上限 8s。
	ModelRetryBackoff func(retry int) time.Duration `json:"-"`

	// ParallelToolCalls 是同一轮可并发执行的工具调用数上限；小于 2（默认 0）时按顺序执行。
	ParallelToolCalls int

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

// WithDurableScope sets a host-owned stable invocation name. Durable execution
// requires it; different agents/calls in the same node must use different scopes.
// Reusing the same scope means replaying the same logical invocation.
func (b *AgentBuilder[C, S]) WithDurableScope(scope string) *AgentBuilder[C, S] {
	b.config.DurableScope = scope
	return b
}

// WithReturnDirectTools 指定结果已可直接展示的终态工具，省去模型总结请求。
// 在 ExecuteStream/AsRunnable 及其 ExecuteWithTools 委派中生效，要求 ToolStreamClient。
// 旧 Execute 会报错而非忽略此配置。每次调用替换名单。
// 本轮多个工具调用时仍走完整模型循环；字符串原样输出，其他结果编码为 JSON。
// 启用者负责工具结果的格式、完整性与适用场景，模型输出格式指令不会再被执行。
func (b *AgentBuilder[C, S]) WithReturnDirectTools(names ...string) *AgentBuilder[C, S] {
	b.config.ReturnDirectTools = append([]string(nil), names...)
	return b
}

// WithEmptyAnswerRetries 设置空回答重问次数（默认 0，不重问）。
//
// [Advanced] 模型以 stop 结束、没有正文也没有工具调用时，不追加空 assistant 消息，
// 改为追加一条固定的 user 消息要求直接回答或调用工具，再请求下一轮。每次重问占用一次
// MaxIterations 额度并消耗一次预算；超过 n 次或已是最后一轮时按原方式报错。
// 持久模式下空回合按已完成记录，恢复时回放。负数视为 0。
func (b *AgentBuilder[C, S]) WithEmptyAnswerRetries(n int) *AgentBuilder[C, S] {
	b.config.EmptyAnswerRetries = max(n, 0)
	return b
}

// WithModelRetry 设置模型回合的重试次数（默认 0，不重试）与重试前的等待。
//
// [Advanced] 仅作用于原生流路径（AsRunnable、ExecuteStream），只重试 Provider 报告为暂时性的失败：
// 429、5xx 与连接错误，见 llm.ProviderError.Retryable。本回合已向消费者输出文本后不再重试，避免内容重复。
// 服务端给出 Retry-After 时按它等待，否则用 backoff（nil 时 500ms 起指数增长、上限 8s）；
// 等待会越过 ctx 截止时间，或 Retry-After 超过 1 分钟时直接失败。每次重试先通过根预算准入并计入 new_model_invocations，不计入 model_calls（逻辑回合数）与 MaxIterations。
// 持久模式下重试发生在同一条回合记录内，重试中途崩溃时恢复返回需宿主处理的错误。
// 配置不进入持久意图，修改后已有运行仍可恢复。子 Agent 不继承此设置。负数视为 0。
func (b *AgentBuilder[C, S]) WithModelRetry(retries int, backoff func(retry int) time.Duration) *AgentBuilder[C, S] {
	b.config.ModelRetries = max(retries, 0)
	b.config.ModelRetryBackoff = backoff
	return b
}

// WithParallelToolCalls 设置同一轮工具调用的并发上限（默认 0，按顺序执行）。
//
// [Advanced] 仅作用于原生工具流。n>=2 时，一轮有多个调用、且每个调用的工具都实现
// interfaces.ConcurrentCallTool（如 Dispatcher.Tools）时最多 n 个并发执行，实际峰值还受工具自身
// 限流（Dispatcher.MaxConcurrency）约束；不另设上限时传 math.MaxInt。n<2 按顺序执行，负数视为 0；
// 含上下文策略工具或任一工具未声明并发能力时整轮按顺序执行。
//
// 并发时执行先后不保证与模型给出的顺序一致，即使 Dispatcher.MaxConcurrency=1 也一样；工具内部不要
// 按执行顺序分配编号或依赖同轮调用的先后。结果仍按模型给出的顺序交还模型，tool_start 在派发前、
// tool_end 在全部结束后按该顺序发出，Dispatcher 的 OnResult 也在全部结束后按该顺序触发。
// 某个调用失败不取消其他调用，全部结束后返回顺序最靠前的错误。
func (b *AgentBuilder[C, S]) WithParallelToolCalls(n int) *AgentBuilder[C, S] {
	b.config.ParallelToolCalls = max(n, 0)
	return b
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
		SessionID:              fmt.Sprintf("session-%d", time.Now().UnixNano()),
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
