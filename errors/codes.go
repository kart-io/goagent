package errors

// ErrorCode identifies the error category of a framework operation.
type ErrorCode string

const (
	// Agent execution errors.
	CodeAgentExecution      ErrorCode = "AGENT_EXECUTION"
	CodeAgentValidation     ErrorCode = "AGENT_VALIDATION"
	CodeAgentNotFound       ErrorCode = "AGENT_NOT_FOUND"
	CodeAgentInitialization ErrorCode = "AGENT_INITIALIZATION"

	// Tool errors.
	CodeToolExecution      ErrorCode = "TOOL_EXECUTION"
	CodeToolNotFound       ErrorCode = "TOOL_NOT_FOUND"
	CodeToolValidation     ErrorCode = "TOOL_VALIDATION"
	CodeToolTimeout        ErrorCode = "TOOL_TIMEOUT"
	CodeToolRetryExhausted ErrorCode = "TOOL_RETRY_EXHAUSTED"

	// Middleware errors.
	CodeMiddlewareExecution  ErrorCode = "MIDDLEWARE_EXECUTION"
	CodeMiddlewareChain      ErrorCode = "MIDDLEWARE_CHAIN"
	CodeMiddlewareValidation ErrorCode = "MIDDLEWARE_VALIDATION"

	// State management errors.
	CodeStateLoad       ErrorCode = "STATE_LOAD"
	CodeStateSave       ErrorCode = "STATE_SAVE"
	CodeStateValidation ErrorCode = "STATE_VALIDATION"

	// Stream processing errors.
	CodeStreamRead    ErrorCode = "STREAM_READ"
	CodeStreamWrite   ErrorCode = "STREAM_WRITE"
	CodeStreamTimeout ErrorCode = "STREAM_TIMEOUT"
	CodeStreamClosed  ErrorCode = "STREAM_CLOSED"

	// Model call errors.
	CodeLLMRequest   ErrorCode = "LLM_REQUEST"
	CodeLLMResponse  ErrorCode = "LLM_RESPONSE"
	CodeLLMTimeout   ErrorCode = "LLM_TIMEOUT"
	CodeLLMRateLimit ErrorCode = "LLM_RATE_LIMIT"

	// Context errors.
	CodeContextCanceled ErrorCode = "CONTEXT_CANCELED"
	CodeContextTimeout  ErrorCode = "CONTEXT_TIMEOUT"

	// General errors.
	CodeInvalidInput   ErrorCode = "INVALID_INPUT"
	CodeInvalidConfig  ErrorCode = "INVALID_CONFIG"
	CodeNotImplemented ErrorCode = "NOT_IMPLEMENTED"
	CodeInternal       ErrorCode = "INTERNAL_ERROR"

	// Distributed and network errors.
	CodeDistributedConnection    ErrorCode = "DISTRIBUTED_CONNECTION"
	CodeDistributedSerialization ErrorCode = "DISTRIBUTED_SERIALIZATION"
	CodeDistributedCoordination  ErrorCode = "DISTRIBUTED_COORDINATION"
	CodeDistributedScheduling    ErrorCode = "DISTRIBUTED_SCHEDULING"
	CodeDistributedHeartbeat     ErrorCode = "DISTRIBUTED_HEARTBEAT"
	CodeDistributedRegistry      ErrorCode = "DISTRIBUTED_REGISTRY"

	// Retrieval and RAG errors.
	CodeRetrievalSearch    ErrorCode = "RETRIEVAL_SEARCH"
	CodeRetrievalEmbedding ErrorCode = "RETRIEVAL_EMBEDDING"
	CodeDocumentNotFound   ErrorCode = "DOCUMENT_NOT_FOUND"
	CodeVectorDimMismatch  ErrorCode = "VECTOR_DIM_MISMATCH"

	// Parsing errors.
	CodeParserFailed       ErrorCode = "PARSER_FAILED"
	CodeParserInvalidJSON  ErrorCode = "PARSER_INVALID_JSON"
	CodeParserMissingField ErrorCode = "PARSER_MISSING_FIELD"

	// Storage errors.
	CodeStoreConnection    ErrorCode = "STORE_CONNECTION"
	CodeStoreSerialization ErrorCode = "STORE_SERIALIZATION"
	CodeStoreNotFound      ErrorCode = "STORE_NOT_FOUND"

	// Routing errors.
	CodeRouterNoMatch  ErrorCode = "ROUTER_NO_MATCH"
	CodeRouterFailed   ErrorCode = "ROUTER_FAILED"
	CodeRouterOverload ErrorCode = "ROUTER_OVERLOAD"

	// Plugin and type system errors.
	CodeTypeMismatch  ErrorCode = "TYPE_MISMATCH"
	CodeAlreadyExists ErrorCode = "ALREADY_EXISTS"
	CodeNotFound      ErrorCode = "NOT_FOUND"
	CodeInvalidOutput ErrorCode = "INVALID_OUTPUT"
)

// Runtime categories for control signals; specific identities are distinguished
// by fixed sentinels.
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
