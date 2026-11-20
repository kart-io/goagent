// 工具名称、描述与执行默认值。
package constants

import "time"

const (
	ToolCalculator            = "calculator"
	ToolAPI                   = "api"
	ToolSearch                = "search"
	ToolShell                 = "shell"
	DescCalculator            = "计算器工具，支持基本数学运算（加减乘除、幂运算、括号）"
	DescAPI                   = "HTTP API 调用工具，支持 GET、POST、PUT、DELETE、PATCH 等方法"
	DescSearch                = "搜索工具，提供网络搜索功能"
	DescShell                 = "Shell 命令执行工具，安全地执行白名单中的系统命令"
	DefaultToolTimeoutSeconds = 30
	DefaultToolConcurrency    = 10
	DefaultToolRetries        = 3
)

// 共享默认策略与协议值；领域实现直接引用本包。
const (
	// DefaultApprovalTimeout 是旧默认值，tools 不再自动套用，宿主要保留旧行为时显式传入 Pipeline.ApprovalTimeout。
	// DefaultResumableToolAttempts 写入 durable 日志，仍是 RunnableToolConfig.MaxAttempts 为 0 时的默认值。
	DefaultApprovalTimeout       = 10 * time.Minute
	DefaultResumableToolAttempts = 3
	ToolReadSpill                = "read_spill"
	SpillPreviewRunes            = 1500
	DefaultSpillReadLimitLines   = 2000
	SpillMaxLineRunes            = 2000
	ToolReadContextResult        = "read_context_result"
	ToolRecallHistory            = "recall_history"
	// DefaultRecallHistoryLimit 与 MaxRecallHistoryLimit 是 recall_history 每次返回的消息条数；
	// MaxRecallHistoryRunes 是单条消息的上限，更长的截断并标注。
	DefaultRecallHistoryLimit = 20
	MaxRecallHistoryLimit     = 100
	MaxRecallHistoryRunes     = 4000
)

// Anti-abuse caps for tools whose input comes from the model: 0 in the
// matching option selects these values instead of meaning "unlimited".
const (
	// DefaultRunnableToolMaxDepth is the runnable tool nesting cap used when
	// RunnableToolConfig.MaxDepth is 0.
	DefaultRunnableToolMaxDepth = 8
	// DefaultAPIToolMaxResponseBytes is the tools/http response body cap; a
	// larger body fails the request.
	DefaultAPIToolMaxResponseBytes = 10 << 20
	// DefaultAPIToolMaxRedirects matches the net/http default redirect cap.
	DefaultAPIToolMaxRedirects = 10
	// DefaultShellMaxOutputBytes is how much combined tools/shell output is
	// kept; the rest is consumed and dropped.
	DefaultShellMaxOutputBytes = 1 << 20
)

// CalculatorMaxNesting 限制计算器表达式的括号与一元负号嵌套深度，防止超长输入耗尽栈。
const CalculatorMaxNesting = 64
