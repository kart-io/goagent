package constants

import "time"

// 持久模型及收件箱策略：改变重发次数会使已有运行的操作策略校验失败。
const (
	DefaultModelResendAttempts = 3
	DefaultInboxWriteTimeout   = 5 * time.Second
	// MaxModelRetryAfter 限制持有回合锁等待服务端恢复的时长。
	MaxModelRetryAfter = time.Minute
	// 暂时性请求错误的默认退避，区别于提供商 SDK 的重试策略。
	DefaultTransientRetryBaseDelay = 500 * time.Millisecond
	DefaultTransientRetryMaxDelay  = 8 * time.Second
)
