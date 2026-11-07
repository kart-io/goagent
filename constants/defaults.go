package constants

import "time"

// 时间默认值区分整数秒与 Duration，避免旧接口发生隐式单位转换。
const (
	DefaultLLMTimeoutSeconds     = 60
	DefaultLLMTimeout            = DefaultLLMTimeoutSeconds * time.Second
	DefaultToolTimeout           = DefaultToolTimeoutSeconds * time.Second
	DefaultDBConnectionTimeout   = 5 * time.Second
	DefaultDBOperationTimeout    = 10 * time.Second
	DefaultHTTPTimeout           = 30 * time.Second
	DefaultPoolAcquireTimeout    = 10 * time.Second
	DefaultBatchTimeout          = 5 * time.Minute
	DefaultAgentExecutionTimeout = 5 * time.Minute
	DefaultSessionCleanupTimeout = 5 * time.Second
)

// 不同调用层的默认策略独立命名，不因当前数值相同而合并。
const (
	DefaultClientMaxTokens       = 2000
	DefaultAgentMaxTokens        = 2000
	DefaultAgentMaxIterations    = 10
	DefaultConversationHistory   = 20
	DefaultDispatcherConcurrency = 4
	DefaultDispatcherAttempts    = 3
	DefaultToolRetryDelay        = time.Second
	DefaultToolRetryMaxDelay     = 10 * time.Second
	DefaultToolRetryMultiplier   = 2.0
)
