package checkpoint

import (
	"context"
	"fmt"
	"sync"
	"time"

	agentstate "github.com/kart-io/goagent/core/state"
)

// Checkpointer defines the interface for session state persistence.
//
// Inspired by LangChain's Checkpointer, it provides:
//   - Session state saving and loading
//   - Thread-based conversation continuity
//   - Checkpoint history management
//
// Use cases:
//   - Multi-turn conversations
//   - Resuming interrupted workflows
//   - A/B testing different conversation paths
//
// Note: The canonical Checkpointer interface is defined in interfaces.Checkpointer
// with a slightly different method signature. This interface is maintained for
// backward compatibility with existing code.
type Checkpointer interface {
	// Save persists the current state for a thread/session.
	Save(ctx context.Context, threadID string, state agentstate.State) error

	// Load retrieves the saved state for a thread/session.
	Load(ctx context.Context, threadID string) (agentstate.State, error)

	// List returns information about all saved checkpoints.
	List(ctx context.Context) ([]CheckpointInfo, error)

	// Delete removes the checkpoint for a thread/session.
	Delete(ctx context.Context, threadID string) error

	// Exists checks if a checkpoint exists for a thread/session.
	Exists(ctx context.Context, threadID string) (bool, error)
}

// CheckpointInfo contains metadata about a checkpoint.
//
// Note: This struct is aligned with interfaces.CheckpointMetadata to ensure consistency.
// Fields match exactly for seamless integration.
type CheckpointInfo struct {
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

// InMemorySaver is a thread-safe in-memory implementation of Checkpointer.
//
// Suitable for:
//   - Development and testing
//   - Single-instance deployments
//   - Short-lived sessions
type InMemorySaver struct {
	checkpoints map[string]*checkpoint
	mu          sync.RWMutex
}

// checkpoint is an internal structure to hold checkpoint data.
type checkpoint struct {
	state   agentstate.State
	info    CheckpointInfo
	history []agentstate.State // Keep history of state changes
}

// NewInMemorySaver creates a new InMemorySaver.
func NewInMemorySaver() *InMemorySaver {
	return &InMemorySaver{
		checkpoints: make(map[string]*checkpoint),
	}
}

// Save persists the current state for a thread/session.
func (s *InMemorySaver) Save(ctx context.Context, threadID string, state agentstate.State) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now()
	cp, exists := s.checkpoints[threadID]

	if exists {
		// Update existing checkpoint
		cp.info.UpdatedAt = now
		cp.info.Size = estimateStateSize(state)
		// Add previous state to history
		cp.history = append(cp.history, cp.state)
		cp.state = state
	} else {
		// Create new checkpoint
		cp = &checkpoint{
			state: state,
			info: CheckpointInfo{
				ID:        fmt.Sprintf("ckpt_%s_%d", threadID, now.UnixNano()),
				ThreadID:  threadID,
				CreatedAt: now,
				UpdatedAt: now,
				Metadata:  make(map[string]interface{}),
				Size:      estimateStateSize(state),
			},
			history: []agentstate.State{},
		}
		s.checkpoints[threadID] = cp
	}

	return nil
}

// Load retrieves the saved state for a thread/session.
func (s *InMemorySaver) Load(ctx context.Context, threadID string) (agentstate.State, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	cp, ok := s.checkpoints[threadID]
	if !ok {
		return nil, fmt.Errorf("checkpoint not found for thread: %s", threadID)
	}

	// Return a clone to prevent external modifications
	return cp.state.Clone(), nil
}

// List returns information about all saved checkpoints.
func (s *InMemorySaver) List(ctx context.Context) ([]CheckpointInfo, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	infos := make([]CheckpointInfo, 0, len(s.checkpoints))
	for _, cp := range s.checkpoints {
		infos = append(infos, cp.info)
	}

	return infos, nil
}

// Delete removes the checkpoint for a thread/session.
func (s *InMemorySaver) Delete(ctx context.Context, threadID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	delete(s.checkpoints, threadID)
	return nil
}

// Exists checks if a checkpoint exists for a thread/session.
func (s *InMemorySaver) Exists(ctx context.Context, threadID string) (bool, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	_, ok := s.checkpoints[threadID]
	return ok, nil
}

// Size returns the number of checkpoints.
func (s *InMemorySaver) Size() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.checkpoints)
}

// GetHistory returns the state history for a thread/session.
func (s *InMemorySaver) GetHistory(ctx context.Context, threadID string) ([]agentstate.State, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	cp, ok := s.checkpoints[threadID]
	if !ok {
		return nil, fmt.Errorf("checkpoint not found for thread: %s", threadID)
	}

	// Return clones to prevent external modifications
	history := make([]agentstate.State, len(cp.history))
	for i, state := range cp.history {
		history[i] = state.Clone()
	}
	return history, nil
}

// CleanupOld removes checkpoints older than the specified duration.
func (s *InMemorySaver) CleanupOld(maxAge time.Duration) int {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now()
	removed := 0

	for threadID, cp := range s.checkpoints {
		if now.Sub(cp.info.UpdatedAt) > maxAge {
			delete(s.checkpoints, threadID)
			removed++
		}
	}

	return removed
}

// estimateStateSize estimates the size of a state in bytes.
func estimateStateSize(state agentstate.State) int64 {
	// Simple estimation based on number of keys and values
	// In a real implementation, you might want to serialize and measure actual size
	snapshot := state.Snapshot()
	size := int64(0)

	for key, value := range snapshot {
		size += int64(len(key))
		// Rough estimation of value size
		size += int64(len(fmt.Sprintf("%v", value)))
	}

	return size
}

// CheckpointerConfig configures checkpointer behavior.
type CheckpointerConfig struct {
	// MaxHistorySize limits the number of historical states to keep
	MaxHistorySize int

	// MaxCheckpointAge is the maximum age before a checkpoint is considered stale
	MaxCheckpointAge time.Duration

	// EnableCompression enables state compression (for future implementation)
	EnableCompression bool

	// AutoCleanup enables automatic cleanup of old checkpoints
	AutoCleanup bool

	// CleanupInterval specifies how often to run cleanup
	CleanupInterval time.Duration
}

// DefaultCheckpointerConfig returns the default checkpointer configuration.
func DefaultCheckpointerConfig() *CheckpointerConfig {
	return &CheckpointerConfig{
		MaxHistorySize:    10,
		MaxCheckpointAge:  24 * time.Hour,
		EnableCompression: false,
		AutoCleanup:       true,
		CleanupInterval:   1 * time.Hour,
	}
}

// CheckpointerWithAutoCleanup wraps a Checkpointer with automatic cleanup.
type CheckpointerWithAutoCleanup struct {
	checkpointer Checkpointer
	config       *CheckpointerConfig
	stopChan     chan struct{}
	wg           sync.WaitGroup
}

// NewCheckpointerWithAutoCleanup creates a Checkpointer with automatic cleanup.
func NewCheckpointerWithAutoCleanup(cp Checkpointer, config *CheckpointerConfig) *CheckpointerWithAutoCleanup {
	if config == nil {
		config = DefaultCheckpointerConfig()
	}

	wrapper := &CheckpointerWithAutoCleanup{
		checkpointer: cp,
		config:       config,
		stopChan:     make(chan struct{}),
	}

	if config.AutoCleanup {
		wrapper.startAutoCleanup()
	}

	return wrapper
}

// startAutoCleanup starts the automatic cleanup goroutine.
func (c *CheckpointerWithAutoCleanup) startAutoCleanup() {
	c.wg.Add(1)
	go func() {
		defer c.wg.Done()
		ticker := time.NewTicker(c.config.CleanupInterval)
		defer ticker.Stop()

		for {
			select {
			case <-ticker.C:
				if saver, ok := c.checkpointer.(*InMemorySaver); ok {
					saver.CleanupOld(c.config.MaxCheckpointAge)
				}
			case <-c.stopChan:
				return
			}
		}
	}()
}

// Stop stops the automatic cleanup goroutine.
func (c *CheckpointerWithAutoCleanup) Stop() {
	close(c.stopChan)
	c.wg.Wait()
}

// Save persists the current state for a thread/session.
func (c *CheckpointerWithAutoCleanup) Save(ctx context.Context, threadID string, state agentstate.State) error {
	return c.checkpointer.Save(ctx, threadID, state)
}

// Load retrieves the saved state for a thread/session.
func (c *CheckpointerWithAutoCleanup) Load(ctx context.Context, threadID string) (agentstate.State, error) {
	return c.checkpointer.Load(ctx, threadID)
}

// List returns information about all saved checkpoints.
func (c *CheckpointerWithAutoCleanup) List(ctx context.Context) ([]CheckpointInfo, error) {
	return c.checkpointer.List(ctx)
}

// Delete removes the checkpoint for a thread/session.
func (c *CheckpointerWithAutoCleanup) Delete(ctx context.Context, threadID string) error {
	return c.checkpointer.Delete(ctx, threadID)
}

// Exists checks if a checkpoint exists for a thread/session.
func (c *CheckpointerWithAutoCleanup) Exists(ctx context.Context, threadID string) (bool, error) {
	return c.checkpointer.Exists(ctx, threadID)
}
