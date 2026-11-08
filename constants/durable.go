package constants

// 续跑种类与输入拒绝原因属于等待协议，不是业务错误码。
const (
	ContinuationKindInterrupt    = "interrupt"
	InterruptReasonInvalidJSON   = "invalid_json"
	InterruptReasonValidatePanic = "validate_panic"
	InterruptReasonRejected      = "rejected"
	// DefaultInterruptAttempts 允许 pending 与 waiting 之间崩溃后重新进入；中断本身没有副作用。
	DefaultInterruptAttempts = 3
	InterruptMaxReasonRunes  = 200
	// DurableInboxSeenLimit 仅覆盖投递方崩溃重投窗口，不随运行时长增长。
	DurableInboxSeenLimit = 256
)
