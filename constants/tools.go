package constants

import "time"

// Built-in tool names, descriptions and execution defaults.
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

// Tool runtime defaults.
const (
	// DefaultApprovalTimeout is a former default that tools no longer apply;
	// pass it to Pipeline.ApprovalTimeout to keep the old behavior.
	// DefaultResumableToolAttempts is written to the durable log and is still
	// the default when RunnableToolConfig.MaxAttempts is 0.
	DefaultApprovalTimeout       = 10 * time.Minute
	DefaultResumableToolAttempts = 3
	ToolReadSpill                = "read_spill"
	SpillPreviewRunes            = 1500
	DefaultSpillReadLimitLines   = 2000
	SpillMaxLineRunes            = 2000
	ToolReadContextResult        = "read_context_result"
	ToolRecallHistory            = "recall_history"
	// DefaultRecallHistoryLimit and MaxRecallHistoryLimit bound the messages
	// recall_history returns per call; MaxRecallHistoryRunes caps one message,
	// longer ones are truncated and marked.
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

// CalculatorMaxNesting caps parenthesis and unary minus nesting in calculator
// expressions so long input cannot exhaust the stack.
const CalculatorMaxNesting = 64
