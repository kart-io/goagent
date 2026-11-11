package constants

import "time"

// 时间默认值区分整数秒与 Duration，避免旧接口发生隐式单位转换。
const (
	DefaultLLMTimeoutSeconds     = 60
	DefaultToolTimeout           = DefaultToolTimeoutSeconds * time.Second
	DefaultAgentExecutionTimeout = 5 * time.Minute
	DefaultSessionCleanupTimeout = 5 * time.Second
)

// 不同调用层的默认策略独立命名，不因当前数值相同而合并。
const (
	DefaultClientMaxTokens       = 2000
	DefaultAgentMaxIterations    = 10
	DefaultDispatcherConcurrency = 4
	DefaultDispatcherAttempts    = 3
)
