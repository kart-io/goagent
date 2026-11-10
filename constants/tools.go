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
	DefaultApprovalTimeout       = 10 * time.Minute
	DefaultResumableToolAttempts = 3
	ToolReadSpill                = "read_spill"
	SpillPreviewRunes            = 1500
	DefaultSpillReadLimitLines   = 2000
	SpillMaxLineRunes            = 2000
	ToolReadContextResult        = "read_context_result"
)

// CalculatorMaxNesting 限制计算器表达式的括号与一元负号嵌套深度，防止超长输入耗尽栈。
const CalculatorMaxNesting = 64
