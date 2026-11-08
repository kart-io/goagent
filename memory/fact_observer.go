package memory

import (
	"errors"
	"time"
)

// MemoryEvent is a privacy-safe summary of one factual-memory operation.
// It intentionally contains no tenant ID, fact ID, query, body, embedding, or metadata.
type MemoryEvent struct {
	Operation string
	Duration  time.Duration
	Count     int
	ErrorCode string
}

// MemoryObserver receives synchronous operation summaries and should return quickly.
// Calls can be concurrent; implementations must be concurrency-safe. Implementations
// may aggregate durations/counts or export them to a metrics system.
type MemoryObserver interface {
	ObserveMemory(MemoryEvent)
}

// observeMemory isolates observer panics from committed service operations and
// background workers. Monitoring callbacks must not change business outcomes.
func observeMemory(observer MemoryObserver, event MemoryEvent) {
	if observer == nil {
		return
	}
	defer func() { _ = recover() }()
	observer.ObserveMemory(event)
}

func memoryErrorCode(err error) string {
	if err == nil {
		return ""
	}
	for _, pair := range []struct {
		err  error
		code string
	}{
		{ErrAuthorization, "authorization"}, {ErrRevisionConflict, "revision_conflict"}, {ErrIdempotencyConflict, "idempotency_conflict"},
		{ErrIdempotencyExpired, "idempotency_expired"}, {ErrGone, "gone"}, {ErrSnapshotInvalidated, "snapshot_invalidated"},
		{ErrCapacityExceeded, "capacity_exceeded"}, {ErrInvalidCursor, "invalid_cursor"}, {ErrMaintenanceNotFound, "maintenance_not_found"},
		{ErrProviderCapability, "provider_capability"}, {ErrInvalidFact, "invalid_fact"},
	} {
		if errors.Is(err, pair.err) {
			return pair.code
		}
	}
	return "internal"
}
