package execution

import (
	"context"
	"sync"
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

// RuntimeManager 并发安全地管理运行时注册表；返回的 Runtime 业务字段仍由宿主同步。
type RuntimeManager[C any, S state.State] struct {
	mu sync.RWMutex
	// runtimes maps session ID to runtime
	runtimes map[string]*Runtime[C, S]
}

// NewRuntimeManager 创建仅管理会话注册表的运行时管理器。
func NewRuntimeManager[C any, S state.State]() *RuntimeManager[C, S] {
	return &RuntimeManager[C, S]{runtimes: make(map[string]*Runtime[C, S])}
}

// GetRuntime retrieves a runtime by session ID.
func (m *RuntimeManager[C, S]) GetRuntime(sessionID string) (*Runtime[C, S], bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	runtime, ok := m.runtimes[sessionID]
	return runtime, ok
}

// SetRuntime stores a runtime for a session ID.
func (m *RuntimeManager[C, S]) SetRuntime(sessionID string, runtime *Runtime[C, S]) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.runtimes[sessionID] = runtime
}

// RemoveRuntime removes a runtime by session ID.
func (m *RuntimeManager[C, S]) RemoveRuntime(sessionID string) {
	m.mu.Lock()
	defer m.mu.Unlock()
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
	m.mu.Lock()
	defer m.mu.Unlock()
	if runtime, ok := m.runtimes[sessionID]; ok {
		return runtime
	}

	runtime := NewRuntime(ctx, st, store, checkpointer, sessionID)
	m.runtimes[sessionID] = runtime
	return runtime
}

// CleanupExpired removes expired runtimes based on max age.
func (m *RuntimeManager[C, S]) CleanupExpired(maxAge time.Duration) int {
	m.mu.Lock()
	defer m.mu.Unlock()
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

// Size returns the number of active runtimes.
func (m *RuntimeManager[C, S]) Size() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.runtimes)
}
