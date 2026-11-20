package constants

import "time"

// 时间默认值区分整数秒与 Duration，避免旧接口发生隐式单位转换。
const (
	// DefaultLLMTimeoutSeconds 是 llm 客户端的旧默认超时，不再自动套用（0 = 不限）。
	DefaultLLMTimeoutSeconds = 60
	DefaultToolTimeout       = DefaultToolTimeoutSeconds * time.Second
	// DefaultAgentExecutionTimeout 与 DefaultAgentMaxIterations 不再由 builder 自动套用（0 = 不限），只供预设与需要旧取值的调用方显式传入。
	DefaultAgentExecutionTimeout = 5 * time.Minute
	DefaultSessionCleanupTimeout = 5 * time.Second
	// DefaultUsageHookTimeout bounds one budget usage statistics query plus
	// hook call when the caller does not choose a timeout.
	DefaultUsageHookTimeout = 5 * time.Second
)

// 不同调用层的默认策略独立命名，不因当前数值相同而合并。
const (
	// DefaultClientMaxTokens 不再作为客户端默认值发送（0 = 不发送 max_tokens），只作为 Anthropic 必填 max_tokens 的默认值。
	DefaultClientMaxTokens    = 2000
	DefaultAgentMaxIterations = 10
	// DefaultDispatcherConcurrency 不再由 Dispatcher 自动套用（0 = 不限），只供需要旧取值的调用方显式传入。
	DefaultDispatcherConcurrency = 4
	DefaultDispatcherAttempts    = 3
)
