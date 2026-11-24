package constants

import "time"

// Durable interrupt protocol values. Continuation kinds and input rejection
// reasons are part of the wait protocol, not business error codes.
const (
	ContinuationKindInterrupt    = "interrupt"
	InterruptReasonInvalidJSON   = "invalid_json"
	InterruptReasonValidatePanic = "validate_panic"
	InterruptReasonRejected      = "rejected"
	// DefaultInterruptAttempts allows re-entry after a crash between pending and
	// waiting; an interrupt has no side effects.
	DefaultInterruptAttempts = 3
	InterruptMaxReasonRunes  = 200
	// DurableInboxSeenLimit covers only the redelivery window after a sender
	// crash and does not grow with run length.
	DurableInboxSeenLimit = 256
)

// DurableCompletionSaveTimeout bounds saving a run result after the caller
// cancels: the result already exists and must not be lost to cancellation.
const DurableCompletionSaveTimeout = 5 * time.Second
