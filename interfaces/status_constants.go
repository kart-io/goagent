// Package interfaces defines status and state constants used across the GoAgent framework.
// These constants provide standardized status values for execution states, task states,
// and operation outcomes.
package interfaces

// Execution Status defines the outcome of an execution or operation.
const (
	// StatusSuccess indicates successful completion
	StatusSuccess = "success"
	// StatusFailed indicates a failure occurred
	StatusFailed = "failed"
	// StatusError indicates an error occurred
	StatusError = "error"
	// StatusPending indicates the operation is pending
	StatusPending = "pending"
	// StatusInProgress indicates the operation is currently in progress
	StatusInProgress = "in_progress"
	// StatusCompleted indicates the operation has completed
	StatusCompleted = "completed"
	// StatusCanceled indicates the operation was canceled
	StatusCanceled = "canceled"
	// StatusTimeout indicates the operation timed out
	StatusTimeout = "timeout"
	// StatusRetrying indicates the operation is being retried
	StatusRetrying = "retrying"
	// StatusSkipped indicates the operation was skipped
	StatusSkipped = "skipped"
	// StatusPartial indicates partial completion
	StatusPartial = "partial"
)

// Task States define the lifecycle states of a task or workflow step.
const (
	// TaskStateQueued indicates the task is queued
	TaskStateQueued = "queued"
	// TaskStateRunning indicates the task is running
	TaskStateRunning = "running"
	// TaskStatePaused indicates the task is paused
	TaskStatePaused = "paused"
	// TaskStateWaiting indicates the task is waiting
	TaskStateWaiting = "waiting"
	// TaskStateBlocked indicates the task is blocked
	TaskStateBlocked = "blocked"
	// TaskStateFinished indicates the task is finished
	TaskStateFinished = "finished"
	// TaskStateAborted indicates the task was aborted
	TaskStateAborted = "aborted"
)

// Agent States define the operational states of an agent.
const (
	// AgentStateIdle indicates the agent is idle
	AgentStateIdle = "idle"
	// AgentStateActive indicates the agent is active
	AgentStateActive = "active"
	// AgentStateBusy indicates the agent is busy
	AgentStateBusy = "busy"
	// AgentStateProcessing indicates the agent is processing
	AgentStateProcessing = "processing"
	// AgentStateThinking indicates the agent is in reasoning/thinking phase
	AgentStateThinking = "thinking"
	// AgentStateExecuting indicates the agent is executing an action
	AgentStateExecuting = "executing"
	// AgentStateWaitingForInput indicates the agent is waiting for input
	AgentStateWaitingForInput = "waiting_for_input"
	// AgentStateStopped indicates the agent has stopped
	AgentStateStopped = "stopped"
	// AgentStateUnavailable indicates the agent is unavailable
	AgentStateUnavailable = "unavailable"
)

// Connection States define network or service connection states.
const (
	// ConnectionStateConnected indicates a successful connection
	ConnectionStateConnected = "connected"
	// ConnectionStateDisconnected indicates disconnection
	ConnectionStateDisconnected = "disconnected"
	// ConnectionStateConnecting indicates connection in progress
	ConnectionStateConnecting = "connecting"
	// ConnectionStateReconnecting indicates reconnection attempt
	ConnectionStateReconnecting = "reconnecting"
	// ConnectionStateClosing indicates connection is closing
	ConnectionStateClosing = "closing"
	// ConnectionStateClosed indicates connection is closed
	ConnectionStateClosed = "closed"
)

// Health States define health check status values.
const (
	// HealthStateHealthy indicates healthy state
	HealthStateHealthy = "healthy"
	// HealthStateUnhealthy indicates unhealthy state
	HealthStateUnhealthy = "unhealthy"
	// HealthStateDegraded indicates degraded performance
	HealthStateDegraded = "degraded"
	// HealthStateUnknown indicates unknown health state
	HealthStateUnknown = "unknown"
)

// Availability States define service availability status.
const (
	// AvailabilityStateAvailable indicates service is available
	AvailabilityStateAvailable = "available"
	// AvailabilityStateUnavailable indicates service is unavailable
	AvailabilityStateUnavailable = "unavailable"
	// AvailabilityStatePartiallyAvailable indicates partial availability
	AvailabilityStatePartiallyAvailable = "partially_available"
	// AvailabilityStateMaintenance indicates maintenance mode
	AvailabilityStateMaintenance = "maintenance"
)

// Priority Levels define priority classifications.
const (
	// PriorityLow indicates low priority
	PriorityLow = "low"
	// PriorityNormal indicates normal priority
	PriorityNormal = "normal"
	// PriorityMedium indicates medium priority
	PriorityMedium = "medium"
	// PriorityHigh indicates high priority
	PriorityHigh = "high"
	// PriorityCritical indicates critical priority
	PriorityCritical = "critical"
	// PriorityUrgent indicates urgent priority
	PriorityUrgent = "urgent"
)

// Severity Levels define issue severity classifications.
const (
	// SeverityInfo indicates informational severity
	SeverityInfo = "info"
	// SeverityDebug indicates debug severity
	SeverityDebug = "debug"
	// SeverityWarning indicates warning severity
	SeverityWarning = "warning"
	// SeverityError indicates error severity
	SeverityError = "error"
	// SeverityFatal indicates fatal severity
	SeverityFatal = "fatal"
)

// Operation Modes define how an operation should be executed.
const (
	// ModeSync indicates synchronous mode
	ModeSync = "sync"
	// ModeAsync indicates asynchronous mode
	ModeAsync = "async"
	// ModeStreaming indicates streaming mode
	ModeStreaming = "streaming"
	// ModeBatch indicates batch mode
	ModeBatch = "batch"
	// ModeParallel indicates parallel execution mode
	ModeParallel = "parallel"
	// ModeSequential indicates sequential execution mode
	ModeSequential = "sequential"
)

// Validation States define validation outcomes.
const (
	// ValidationStateValid indicates valid state
	ValidationStateValid = "valid"
	// ValidationStateInvalid indicates invalid state
	ValidationStateInvalid = "invalid"
	// ValidationStateUnvalidated indicates not yet validated
	ValidationStateUnvalidated = "unvalidated"
)

// Permission States define authorization states.
const (
	// PermissionStateAllowed indicates permission is granted
	PermissionStateAllowed = "allowed"
	// PermissionStateDenied indicates permission is denied
	PermissionStateDenied = "denied"
	// PermissionStateRestricted indicates restricted access
	PermissionStateRestricted = "restricted"
)
