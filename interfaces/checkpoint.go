package interfaces

import (
	"context"
	"time"
)

// Checkpointer is the canonical interface for saving/loading agent state.
//
// This interface provides session state persistence for multi-turn conversations,
// resuming interrupted workflows, and A/B testing different conversation paths.
//
// Implementations:
//   - checkpoint.MemoryCheckpointer (core/checkpoint/memory.go)
//   - checkpoint.RedisCheckpointer (core/checkpoint/redis.go)
//   - checkpoint.DistributedCheckpointer (core/checkpoint/distributed.go)
//
// Inspired by LangChain's Checkpointer pattern, it enables:
//   - Thread-based conversation continuity
//   - Checkpoint history management
//   - State persistence and recovery
//
// Example usage:
//
//	checkpointer := checkpoint.NewMemoryCheckpointer()
//
//	// Save agent state
//	cp := &interfaces.Checkpoint{
//	    ID:       "ckpt-001",
//	    ThreadID: "thread-123",
//	    State:    agentState,
//	}
//	err := checkpointer.SaveCheckpoint(ctx, cp)
//
//	// Load agent state
//	loaded, err := checkpointer.LoadCheckpoint(ctx, "ckpt-001")
//
//	// List checkpoints for a thread
//	metadata, err := checkpointer.ListCheckpoints(ctx, "thread-123", 10)
type Checkpointer interface {
	// SaveCheckpoint persists a checkpoint.
	//
	// The checkpoint contains the complete agent state at a point in time,
	// allowing resumption of execution from that state.
	//
	// Parameters:
	//   - ctx: Context for cancellation and deadlines
	//   - checkpoint: The checkpoint to save (must have ID and ThreadID)
	//
	// Returns:
	//   - error: SaveError if persistence fails
	SaveCheckpoint(ctx context.Context, checkpoint *Checkpoint) error

	// LoadCheckpoint retrieves a checkpoint by ID.
	//
	// Returns the complete checkpoint including state and metadata.
	//
	// Parameters:
	//   - ctx: Context for cancellation and deadlines
	//   - checkpointID: The unique checkpoint identifier
	//
	// Returns:
	//   - *Checkpoint: The loaded checkpoint
	//   - error: NotFoundError if checkpoint doesn't exist, LoadError for other failures
	LoadCheckpoint(ctx context.Context, checkpointID string) (*Checkpoint, error)

	// ListCheckpoints lists checkpoints for a thread.
	//
	// Returns metadata for the most recent checkpoints, useful for:
	//   - Displaying checkpoint history to users
	//   - Finding the latest checkpoint to resume from
	//   - Analyzing checkpoint patterns
	//
	// Parameters:
	//   - ctx: Context for cancellation and deadlines
	//   - threadID: The thread/session identifier
	//   - limit: Maximum number of checkpoints to return (0 = all)
	//
	// Returns:
	//   - []*CheckpointMetadata: List of checkpoint metadata, ordered by creation time (newest first)
	//   - error: QueryError if listing fails
	ListCheckpoints(ctx context.Context, threadID string, limit int) ([]*CheckpointMetadata, error)

	// DeleteCheckpoint removes a checkpoint.
	//
	// Permanently deletes a checkpoint and its associated state.
	// This operation cannot be undone.
	//
	// Parameters:
	//   - ctx: Context for cancellation and deadlines
	//   - checkpointID: The unique checkpoint identifier
	//
	// Returns:
	//   - error: DeleteError if removal fails (not an error if checkpoint doesn't exist)
	DeleteCheckpoint(ctx context.Context, checkpointID string) error
}

// Checkpoint represents a saved state snapshot.
//
// A checkpoint captures the complete state of an agent at a point in time,
// including conversation history, tool outputs, and intermediate results.
//
// Thread-based organization allows multiple independent conversation threads
// with separate state management.
type Checkpoint struct {
	// ID is the unique checkpoint identifier.
	// Format: "ckpt-{uuid}" or implementation-specific format
	ID string `json:"id"`

	// ThreadID is the thread/session identifier.
	// All checkpoints with the same ThreadID belong to the same conversation.
	ThreadID string `json:"thread_id"`

	// State is the agent state at checkpoint time.
	// Contains all data needed to resume execution from this point.
	State State `json:"state"`

	// Metadata contains additional checkpoint information.
	// Common metadata keys:
	//   - "agent_name": Name of the agent that created the checkpoint
	//   - "checkpoint_reason": Why this checkpoint was created (e.g., "user_request", "step_complete")
	//   - "parent_checkpoint_id": ID of the previous checkpoint (for history traversal)
	//   - "tags": Array of tags for categorization
	Metadata map[string]interface{} `json:"metadata,omitempty"`

	// CreatedAt is when the checkpoint was created.
	CreatedAt time.Time `json:"created_at"`
}

// CheckpointMetadata contains checkpoint summary information.
//
// Provides lightweight checkpoint information without loading the full state,
// useful for listing and browsing checkpoints.
type CheckpointMetadata struct {
	// ID is the unique checkpoint identifier.
	ID string `json:"id"`

	// ThreadID is the thread/session identifier.
	ThreadID string `json:"thread_id"`

	// CreatedAt is when the checkpoint was created.
	CreatedAt time.Time `json:"created_at"`

	// UpdatedAt is when the checkpoint was last updated.
	UpdatedAt time.Time `json:"updated_at"`

	// Metadata holds additional checkpoint information.
	Metadata map[string]interface{} `json:"metadata,omitempty"`

	// Size is the approximate size in bytes of the checkpoint data.
	// Useful for storage management and cleanup decisions.
	Size int64 `json:"size"`
}
