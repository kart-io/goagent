package execution

import (
	"context"
	"time"

	"github.com/kart-io/goagent/core/checkpoint"
	"github.com/kart-io/goagent/core/state"
	"github.com/kart-io/goagent/store"
)

// Runtime provides the execution environment for tools and middleware.
//
// Inspired by LangChain's ToolRuntime, it provides access to:
//   - User-defined context (generic type C)
//   - Agent state (generic type S extending State)
//   - Long-term storage via Store
//   - Session persistence via Checkpointer
//   - Execution metadata (tool call ID, session ID, etc.)
//
// Generic type parameters:
//   - C: Custom context type for application-specific data
//   - S: State type (must implement State interface)
type Runtime[C any, S state.State] struct {
	// Context holds user-defined application context
	Context C

	// State holds the agent's current state
	State S

	// Store provides long-term persistent storage
	Store store.Store

	// Checkpointer handles session state persistence
	Checkpointer checkpoint.Checkpointer

	// ToolCallID is the unique identifier for the current tool call
	ToolCallID string

	// SessionID is the unique identifier for the current session/thread
	SessionID string

	// Timestamp is when this runtime was created
	Timestamp time.Time

	// Metadata holds additional runtime metadata
	Metadata map[string]interface{}
}

// NewRuntime creates a new Runtime with the given components.
func NewRuntime[C any, S state.State](
	ctx C,
	st S,
	store store.Store,
	checkpointer checkpoint.Checkpointer,
	sessionID string,
) *Runtime[C, S] {
	return &Runtime[C, S]{
		Context:      ctx,
		State:        st,
		Store:        store,
		Checkpointer: checkpointer,
		SessionID:    sessionID,
		Timestamp:    time.Now(),
		Metadata:     make(map[string]interface{}),
	}
}

// WithToolCallID returns a copy of the runtime with the specified tool call ID.
func (r *Runtime[C, S]) WithToolCallID(id string) *Runtime[C, S] {
	copy := *r
	copy.ToolCallID = id
	return &copy
}

// WithMetadata returns a copy of the runtime with additional metadata.
//
// Optimization: Uses copy-on-write pattern. The metadata map is only copied
// when modifications are made, reducing allocations for read-heavy workloads.
// Pre-allocates the new map with capacity for existing entries plus the new one.
func (r *Runtime[C, S]) WithMetadata(key string, value interface{}) *Runtime[C, S] {
	newRuntime := *r

	// Pre-allocate with exact capacity needed
	newMetadata := make(map[string]interface{}, len(r.Metadata)+1)

	// Copy existing metadata
	for k, v := range r.Metadata {
		newMetadata[k] = v
	}

	// Add new entry
	newMetadata[key] = value
	newRuntime.Metadata = newMetadata

	return &newRuntime
}

// SaveState persists the current state using the checkpointer if available.
func (r *Runtime[C, S]) SaveState(ctx context.Context) error {
	if r.Checkpointer == nil {
		return nil
	}
	return r.Checkpointer.Save(ctx, r.SessionID, r.State)
}

// LoadState loads the state from the checkpointer if available.
func (r *Runtime[C, S]) LoadState(ctx context.Context) (state.State, error) {
	if r.Checkpointer == nil {
		return r.State, nil
	}
	return r.Checkpointer.Load(ctx, r.SessionID)
}

// ToolFunc defines the signature for tool functions with runtime access.
//
// Generic type parameters:
//   - I: Input type for the tool
//   - O: Output type from the tool
//   - C: Custom context type
//   - S: State type (must implement State interface)
type ToolFunc[I, O, C any, S state.State] func(ctx context.Context, input I, runtime *Runtime[C, S]) (O, error)

// ToolWithRuntime wraps a ToolFunc into a Tool interface.
type ToolWithRuntime[I, O, C any, S state.State] struct {
	name        string
	description string
	fn          ToolFunc[I, O, C, S]
	runtime     *Runtime[C, S]
}

// NewToolWithRuntime creates a new tool that has access to runtime.
func NewToolWithRuntime[I, O, C any, S state.State](
	name, description string,
	fn ToolFunc[I, O, C, S],
	runtime *Runtime[C, S],
) *ToolWithRuntime[I, O, C, S] {
	return &ToolWithRuntime[I, O, C, S]{
		name:        name,
		description: description,
		fn:          fn,
		runtime:     runtime,
	}
}

// Name returns the tool name.
func (t *ToolWithRuntime[I, O, C, S]) Name() string {
	return t.name
}

// Description returns the tool description.
func (t *ToolWithRuntime[I, O, C, S]) Description() string {
	return t.description
}

// Execute runs the tool function with runtime access.
func (t *ToolWithRuntime[I, O, C, S]) Execute(ctx context.Context, input I) (O, error) {
	return t.fn(ctx, input, t.runtime)
}

// WithRuntime returns a new tool with updated runtime.
func (t *ToolWithRuntime[I, O, C, S]) WithRuntime(runtime *Runtime[C, S]) *ToolWithRuntime[I, O, C, S] {
	return &ToolWithRuntime[I, O, C, S]{
		name:        t.name,
		description: t.description,
		fn:          t.fn,
		runtime:     runtime,
	}
}

// RuntimeConfig configures runtime behavior.
type RuntimeConfig struct {
	// EnableAutoSave automatically saves state after tool execution
	EnableAutoSave bool

	// SaveInterval specifies how often to auto-save state (if enabled)
	SaveInterval time.Duration

	// MaxStateSize limits the maximum state size in bytes
	MaxStateSize int64

	// EnableMetrics enables runtime metrics collection
	EnableMetrics bool
}

// DefaultRuntimeConfig returns the default runtime configuration.
func DefaultRuntimeConfig() *RuntimeConfig {
	return &RuntimeConfig{
		EnableAutoSave: true,
		SaveInterval:   30 * time.Second,
		MaxStateSize:   10 * 1024 * 1024, // 10MB
		EnableMetrics:  false,
	}
}

// RuntimeMetrics tracks runtime execution metrics.
type RuntimeMetrics struct {
	// TotalToolCalls is the total number of tool calls executed
	TotalToolCalls int64

	// TotalStateUpdates is the total number of state updates
	TotalStateUpdates int64

	// TotalStorageOperations is the total number of storage operations
	TotalStorageOperations int64

	// TotalCheckpoints is the total number of checkpoints saved
	TotalCheckpoints int64

	// LastToolCall is the timestamp of the last tool call
	LastToolCall time.Time

	// LastStateUpdate is the timestamp of the last state update
	LastStateUpdate time.Time

	// AverageToolLatency is the average tool execution latency
	AverageToolLatency time.Duration
}

// NewRuntimeMetrics creates a new RuntimeMetrics instance.
func NewRuntimeMetrics() *RuntimeMetrics {
	return &RuntimeMetrics{}
}

// RuntimeManager manages multiple runtimes and their lifecycle.
type RuntimeManager[C any, S state.State] struct {
	// runtimes maps session ID to runtime
	runtimes map[string]*Runtime[C, S]

	// config is the runtime configuration
	config *RuntimeConfig

	// metrics tracks runtime metrics
	metrics *RuntimeMetrics
}

// NewRuntimeManager creates a new RuntimeManager.
func NewRuntimeManager[C any, S state.State](config *RuntimeConfig) *RuntimeManager[C, S] {
	if config == nil {
		config = DefaultRuntimeConfig()
	}
	return &RuntimeManager[C, S]{
		runtimes: make(map[string]*Runtime[C, S]),
		config:   config,
		metrics:  NewRuntimeMetrics(),
	}
}

// GetRuntime retrieves a runtime by session ID.
func (m *RuntimeManager[C, S]) GetRuntime(sessionID string) (*Runtime[C, S], bool) {
	runtime, ok := m.runtimes[sessionID]
	return runtime, ok
}

// SetRuntime stores a runtime for a session ID.
func (m *RuntimeManager[C, S]) SetRuntime(sessionID string, runtime *Runtime[C, S]) {
	m.runtimes[sessionID] = runtime
}

// RemoveRuntime removes a runtime by session ID.
func (m *RuntimeManager[C, S]) RemoveRuntime(sessionID string) {
	delete(m.runtimes, sessionID)
}

// GetOrCreateRuntime gets an existing runtime or creates a new one.
func (m *RuntimeManager[C, S]) GetOrCreateRuntime(
	sessionID string,
	ctx C,
	st S,
	store store.Store,
	checkpointer checkpoint.Checkpointer,
) *Runtime[C, S] {
	if runtime, ok := m.runtimes[sessionID]; ok {
		return runtime
	}

	runtime := NewRuntime(ctx, st, store, checkpointer, sessionID)
	m.runtimes[sessionID] = runtime
	return runtime
}

// CleanupExpired removes expired runtimes based on max age.
func (m *RuntimeManager[C, S]) CleanupExpired(maxAge time.Duration) int {
	now := time.Now()
	removed := 0

	for sessionID, runtime := range m.runtimes {
		if now.Sub(runtime.Timestamp) > maxAge {
			delete(m.runtimes, sessionID)
			removed++
		}
	}

	return removed
}

// Metrics returns the runtime metrics.
func (m *RuntimeManager[C, S]) Metrics() *RuntimeMetrics {
	return m.metrics
}

// Size returns the number of active runtimes.
func (m *RuntimeManager[C, S]) Size() int {
	return len(m.runtimes)
}
