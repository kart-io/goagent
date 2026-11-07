package errors

// 框架控制信号只创建一次；调用方通过 errors.Is 判断身份、GetCode 判断类别。
// 同码信号仍保持不同对象，禁止修改共享上下文或把类别相等当作身份相等。
var (
	// builder/contextpolicy。
	ErrResultScopeReleased = newSentinel(CodeScopeRemoved, "结果作用域已释放，拒绝读写")
	// builder。
	ErrAgentHandleDone = newSentinel(CodeInvalidInput, "agent handle is done")
	ErrModelRedirected = newSentinel(CodeLLMRequest, "model request redirected")
	ErrAgentWaiting    = newSentinel(CodeWaiting, "agent is waiting for host continuation")
	// cache。
	ErrCacheMiss     = newSentinel(CodeNotFound, "cache miss")
	ErrCacheDisabled = newSentinel(CodeInvalidConfig, "cache is disabled")
	// core/budget。
	ErrModelBudgetExceeded   = newSentinel(CodeBudgetExceeded, "root model request budget exceeded")
	ErrReservationExists     = newSentinel(CodeAlreadyExists, "model request reservation already exists")
	ErrTokenEstimateRequired = newSentinel(CodeInvalidInput, "token limit requires a host estimate")
	// core/durable。
	ErrInterruptNotDurable = newSentinel(CodeInvalidConfig, "暂停需要持久日志")
	ErrReplayOnly          = newSentinel(CodeReplayOnly, "只读重放需要已完成的持久记录")
	ErrOperationAttention  = newSentinel(CodeAttentionRequired, "durable operation needs host attention")
	ErrWaitNotResumable    = newSentinel(CodeInvalidConfig, "等待只能由声明 Resumable 的持久操作承载")
	ErrApprovalRequired    = newSentinel(CodeApprovalRequired, "durable step requires approval")
	ErrLockUnavailable     = newSentinel(CodeLockUnavailable, "持久运行锁不可用")
	ErrLeaseLost           = newSentinel(CodeLeaseLost, "持久运行锁已失效，拒绝写入")
	ErrScopeRemoved        = newSentinel(CodeScopeRemoved, "持久作用域已删除，拒绝读写")
	// core。
	ErrAgentAlreadyExists = newSentinel(CodeAlreadyExists, "agent already exists")
	ErrNotImplemented     = newSentinel(CodeNotImplemented, "method not implemented")
	ErrChainAlreadyExists = newSentinel(CodeAlreadyExists, "chain already exists")
	ErrToolAlreadyExists  = newSentinel(CodeAlreadyExists, "tool already exists")
	// core/middleware。
	ErrCircuitOpen = newSentinel(CodeCircuitOpen, "circuit breaker is open")
	// core/session。
	ErrSessionConflict = newSentinel(CodeConflict, "会话日志已被并发写入")
	ErrSessionNotFound = newSentinel(CodeNotFound, "会话不存在")
	ErrSessionExists   = newSentinel(CodeAlreadyExists, "会话已存在")
	// evaluation。
	ErrEvaluationBudgetExceeded = newSentinel(CodeBudgetExceeded, "evaluation budget exhausted")
	// llm。
	// multiagent。
	ErrParallelRegistration = newSentinel(CodeInvalidConfig, "ParallelStep 只能经 AddStep 或 multiagent 构造器注册，不能直接执行")
	ErrInvalidRoute         = newSentinel(CodeRouterNoMatch, "路由决策无效")
	// parsers。
	ErrParseFailed    = newSentinel(CodeParserFailed, "failed to parse output")
	ErrInvalidFormat  = newSentinel(CodeParserFailed, "invalid output format")
	ErrMissingField   = newSentinel(CodeParserMissingField, "missing required field")
	ErrTypeConversion = newSentinel(CodeParserFailed, "type conversion failed")
	ErrNoParsers      = newSentinel(CodeInvalidConfig, "no parsers configured")
	// tools。
	ErrNestedExecutionLimit = newSentinel(CodeExecutionLimit, "nested runnable depth exceeded")
	ErrDispatcherReentry    = newSentinel(CodeInvalidConfig, "dispatcher reentry requires an independent child dispatcher")
	ErrDispatcherBusy       = newSentinel(CodeOverloaded, "Dispatcher 等待队列已满")
	ErrSubstituteUnknown    = newSentinel(CodeToolNotFound, "substitute tool is not registered")
	ErrToolWorkflowWaiting  = newSentinel(CodeWaiting, "tool workflow requires host continuation")
	ErrToolKeyNotFound      = newSentinel(CodeNotFound, "key not found")
	ErrToolValueNotFound    = newSentinel(CodeNotFound, "value not found")
	ErrStateAccessDenied    = newSentinel(CodeAccessDenied, "state access is disabled")
	ErrStoreAccessDenied    = newSentinel(CodeAccessDenied, "store access is disabled")
	// workflow。
	ErrApprovalMismatch  = newSentinel(CodeConflict, "approval does not match waiting node")
	ErrWorkflowMaxSteps  = newSentinel(CodeExecutionLimit, "workflow maximum steps exceeded")
	ErrInterruptAnswered = newSentinel(CodeConflict, "中断已回应")
	// workflow/refund。
	ErrRefundNotFound = newSentinel(CodeNotFound, "refund record not found")
)
