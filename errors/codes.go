package errors

// ErrorCode 标识框架操作的错误分类。
type ErrorCode string

const (
	// Agent 执行错误
	CodeAgentExecution      ErrorCode = "AGENT_EXECUTION"
	CodeAgentValidation     ErrorCode = "AGENT_VALIDATION"
	CodeAgentNotFound       ErrorCode = "AGENT_NOT_FOUND"
	CodeAgentInitialization ErrorCode = "AGENT_INITIALIZATION"

	// 工具错误
	CodeToolExecution      ErrorCode = "TOOL_EXECUTION"
	CodeToolNotFound       ErrorCode = "TOOL_NOT_FOUND"
	CodeToolValidation     ErrorCode = "TOOL_VALIDATION"
	CodeToolTimeout        ErrorCode = "TOOL_TIMEOUT"
	CodeToolRetryExhausted ErrorCode = "TOOL_RETRY_EXHAUSTED"

	// 中间件错误
	CodeMiddlewareExecution  ErrorCode = "MIDDLEWARE_EXECUTION"
	CodeMiddlewareChain      ErrorCode = "MIDDLEWARE_CHAIN"
	CodeMiddlewareValidation ErrorCode = "MIDDLEWARE_VALIDATION"

	// 状态管理错误
	CodeStateLoad       ErrorCode = "STATE_LOAD"
	CodeStateSave       ErrorCode = "STATE_SAVE"
	CodeStateValidation ErrorCode = "STATE_VALIDATION"
	CodeStateCheckpoint ErrorCode = "STATE_CHECKPOINT"

	// 流处理错误
	CodeStreamRead    ErrorCode = "STREAM_READ"
	CodeStreamWrite   ErrorCode = "STREAM_WRITE"
	CodeStreamTimeout ErrorCode = "STREAM_TIMEOUT"
	CodeStreamClosed  ErrorCode = "STREAM_CLOSED"

	// 模型调用错误
	CodeLLMRequest   ErrorCode = "LLM_REQUEST"
	CodeLLMResponse  ErrorCode = "LLM_RESPONSE"
	CodeLLMTimeout   ErrorCode = "LLM_TIMEOUT"
	CodeLLMRateLimit ErrorCode = "LLM_RATE_LIMIT"

	// 上下文错误
	CodeContextCanceled ErrorCode = "CONTEXT_CANCELED"
	CodeContextTimeout  ErrorCode = "CONTEXT_TIMEOUT"

	// 通用错误
	CodeInvalidInput   ErrorCode = "INVALID_INPUT"
	CodeInvalidConfig  ErrorCode = "INVALID_CONFIG"
	CodeNotImplemented ErrorCode = "NOT_IMPLEMENTED"
	CodeInternal       ErrorCode = "INTERNAL_ERROR"

	// 分布式与网络错误
	CodeDistributedConnection    ErrorCode = "DISTRIBUTED_CONNECTION"
	CodeDistributedSerialization ErrorCode = "DISTRIBUTED_SERIALIZATION"
	CodeDistributedCoordination  ErrorCode = "DISTRIBUTED_COORDINATION"
	CodeDistributedScheduling    ErrorCode = "DISTRIBUTED_SCHEDULING"
	CodeDistributedHeartbeat     ErrorCode = "DISTRIBUTED_HEARTBEAT"
	CodeDistributedRegistry      ErrorCode = "DISTRIBUTED_REGISTRY"

	// 检索与 RAG 错误
	CodeRetrievalSearch    ErrorCode = "RETRIEVAL_SEARCH"
	CodeRetrievalEmbedding ErrorCode = "RETRIEVAL_EMBEDDING"
	CodeDocumentNotFound   ErrorCode = "DOCUMENT_NOT_FOUND"
	CodeVectorDimMismatch  ErrorCode = "VECTOR_DIM_MISMATCH"

	// 解析错误
	CodeParserFailed       ErrorCode = "PARSER_FAILED"
	CodeParserInvalidJSON  ErrorCode = "PARSER_INVALID_JSON"
	CodeParserMissingField ErrorCode = "PARSER_MISSING_FIELD"

	// 存储错误
	CodeStoreConnection    ErrorCode = "STORE_CONNECTION"
	CodeStoreSerialization ErrorCode = "STORE_SERIALIZATION"
	CodeStoreNotFound      ErrorCode = "STORE_NOT_FOUND"

	// 路由错误
	CodeRouterNoMatch  ErrorCode = "ROUTER_NO_MATCH"
	CodeRouterFailed   ErrorCode = "ROUTER_FAILED"
	CodeRouterOverload ErrorCode = "ROUTER_OVERLOAD"

	// 插件与类型系统错误
	CodeTypeMismatch  ErrorCode = "TYPE_MISMATCH"
	CodeAlreadyExists ErrorCode = "ALREADY_EXISTS"
	CodeNotFound      ErrorCode = "NOT_FOUND"
	CodeInvalidOutput ErrorCode = "INVALID_OUTPUT"
)

// 控制信号对应的实际运行类别，具体身份由固定哨兵区分。
const (
	CodeWaiting           ErrorCode = "WAITING"
	CodeBudgetExceeded    ErrorCode = "BUDGET_EXCEEDED"
	CodeReplayOnly        ErrorCode = "REPLAY_ONLY"
	CodeAttentionRequired ErrorCode = "ATTENTION_REQUIRED"
	CodeApprovalRequired  ErrorCode = "APPROVAL_REQUIRED"
	CodeLockUnavailable   ErrorCode = "LOCK_UNAVAILABLE"
	CodeLeaseLost         ErrorCode = "LEASE_LOST"
	CodeScopeRemoved      ErrorCode = "SCOPE_REMOVED"
	CodeCircuitOpen       ErrorCode = "CIRCUIT_OPEN"
	CodeConflict          ErrorCode = "CONFLICT"
	CodeExecutionLimit    ErrorCode = "EXECUTION_LIMIT"
	CodeOverloaded        ErrorCode = "OVERLOADED"
	CodeAccessDenied      ErrorCode = "ACCESS_DENIED"
)
