// 运行状态、优先级与权限常量。
package constants

// 执行结果状态。
const (
	StatusSuccess    = "success"
	StatusFailed     = "failed"
	StatusError      = "error"
	StatusPending    = "pending"
	StatusInProgress = "in_progress"
	StatusCompleted  = "completed"
	StatusCanceled   = "canceled"
	StatusTimeout    = "timeout"
	StatusRetrying   = "retrying"
	StatusSkipped    = "skipped"
	StatusPartial    = "partial"
)

// 任务生命周期状态。
const (
	TaskStateQueued   = "queued"
	TaskStateRunning  = "running"
	TaskStatePaused   = "paused"
	TaskStateWaiting  = "waiting"
	TaskStateBlocked  = "blocked"
	TaskStateFinished = "finished"
	TaskStateAborted  = "aborted"
)

// 智能体运行状态。
const (
	AgentStateIdle            = "idle"
	AgentStateActive          = "active"
	AgentStateBusy            = "busy"
	AgentStateProcessing      = "processing"
	AgentStateThinking        = "thinking"
	AgentStateExecuting       = "executing"
	AgentStateWaitingForInput = "waiting_for_input"
	AgentStateStopped         = "stopped"
	AgentStateUnavailable     = "unavailable"
)

// 连接状态。
const (
	ConnectionStateConnected    = "connected"
	ConnectionStateDisconnected = "disconnected"
	ConnectionStateConnecting   = "connecting"
	ConnectionStateReconnecting = "reconnecting"
	ConnectionStateClosing      = "closing"
	ConnectionStateClosed       = "closed"
)

// 健康状态。
const (
	HealthStateHealthy   = "healthy"
	HealthStateUnhealthy = "unhealthy"
	HealthStateDegraded  = "degraded"
	HealthStateUnknown   = "unknown"
)

// 服务可用状态。
const (
	AvailabilityStateAvailable          = "available"
	AvailabilityStateUnavailable        = "unavailable"
	AvailabilityStatePartiallyAvailable = "partially_available"
	AvailabilityStateMaintenance        = "maintenance"
)

// 优先级。
const (
	PriorityLow      = "low"
	PriorityNormal   = "normal"
	PriorityMedium   = "medium"
	PriorityHigh     = "high"
	PriorityCritical = "critical"
	PriorityUrgent   = "urgent"
)

// 严重级别。
const (
	SeverityInfo    = "info"
	SeverityDebug   = "debug"
	SeverityWarning = "warning"
	SeverityError   = "error"
	SeverityFatal   = "fatal"
)

// 执行模式。
const (
	ModeSync       = "sync"
	ModeAsync      = "async"
	ModeStreaming  = "streaming"
	ModeBatch      = "batch"
	ModeParallel   = "parallel"
	ModeSequential = "sequential"
)

// 验证状态。
const (
	ValidationStateValid       = "valid"
	ValidationStateInvalid     = "invalid"
	ValidationStateUnvalidated = "unvalidated"
)

// 权限状态。
const (
	PermissionStateAllowed    = "allowed"
	PermissionStateDenied     = "denied"
	PermissionStateRestricted = "restricted"
)
