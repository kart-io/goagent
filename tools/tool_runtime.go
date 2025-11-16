package tools

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/kart-io/goagent/core"
	"github.com/kart-io/goagent/interfaces"
	"github.com/kart-io/goagent/store"
)

// Sentinel errors
var (
	ErrKeyNotFound       = errors.New("key not found")
	ErrValueNotFound     = errors.New("value not found")
	ErrStateAccessDenied = errors.New("state access is disabled")
	ErrStoreAccessDenied = errors.New("store access is disabled")
)

// ToolRuntime provides access to agent state and context from within tools
type ToolRuntime struct {
	// Core components
	State   core.State      // Agent's current state
	Context context.Context // Request context
	Store   store.Store     // Long-term memory store
	Config  *RuntimeConfig  // Runtime configuration

	// Execution context
	ToolCallID string // Current tool call ID
	AgentID    string // ID of the agent executing the tool
	SessionID  string // Session ID for tracking

	// Streaming support
	StreamWriter func(interface{}) error // Stream custom data

	// Additional context
	Metadata map[string]interface{} // Additional metadata
	mu       sync.RWMutex
}

// RuntimeConfig configures the tool runtime
type RuntimeConfig struct {
	// EnableStateAccess allows tools to read/write agent state
	EnableStateAccess bool

	// EnableStoreAccess allows tools to access long-term store
	EnableStoreAccess bool

	// EnableStreaming allows tools to stream data
	EnableStreaming bool

	// MaxExecutionTime limits tool execution time
	MaxExecutionTime int // seconds

	// AllowedNamespaces restricts store access to specific namespaces
	AllowedNamespaces []string
}

// DefaultRuntimeConfig returns default configuration
func DefaultRuntimeConfig() *RuntimeConfig {
	return &RuntimeConfig{
		EnableStateAccess: true,
		EnableStoreAccess: true,
		EnableStreaming:   true,
		MaxExecutionTime:  60,
		AllowedNamespaces: []string{}, // Empty means all allowed
	}
}

// NewToolRuntime creates a new tool runtime
func NewToolRuntime(ctx context.Context, state core.State, store store.Store) *ToolRuntime {
	return &ToolRuntime{
		State:    state,
		Context:  ctx,
		Store:    store,
		Config:   DefaultRuntimeConfig(),
		Metadata: make(map[string]interface{}),
	}
}

// WithConfig sets the runtime configuration
func (r *ToolRuntime) WithConfig(config *RuntimeConfig) *ToolRuntime {
	r.Config = config
	return r
}

// WithStreamWriter sets the stream writer
func (r *ToolRuntime) WithStreamWriter(writer func(interface{}) error) *ToolRuntime {
	r.StreamWriter = writer
	return r
}

// WithMetadata adds metadata to the runtime
func (r *ToolRuntime) WithMetadata(key string, value interface{}) *ToolRuntime {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.Metadata[key] = value
	return r
}

// GetState retrieves a value from agent state
func (r *ToolRuntime) GetState(key string) (interface{}, error) {
	if !r.Config.EnableStateAccess {
		return nil, ErrStateAccessDenied
	}

	val, ok := r.State.Get(key)
	if !ok {
		return nil, ErrKeyNotFound
	}
	return val, nil
}

// SetState updates a value in agent state
func (r *ToolRuntime) SetState(key string, value interface{}) error {
	if !r.Config.EnableStateAccess {
		return fmt.Errorf("state access is disabled")
	}

	r.State.Set(key, value)
	return nil
}

// GetFromStore retrieves data from long-term store
func (r *ToolRuntime) GetFromStore(namespace []string, key string) (interface{}, error) {
	if !r.Config.EnableStoreAccess {
		return nil, ErrStoreAccessDenied
	}

	// Check namespace restrictions
	if len(r.Config.AllowedNamespaces) > 0 {
		allowed := false
		for _, ns := range r.Config.AllowedNamespaces {
			if len(namespace) > 0 && namespace[0] == ns {
				allowed = true
				break
			}
		}
		if !allowed {
			return nil, fmt.Errorf("access to namespace %v is not allowed", namespace)
		}
	}

	val, err := r.Store.Get(r.Context, namespace, key)
	if err != nil {
		return nil, err
	}
	if val == nil {
		return nil, ErrValueNotFound
	}
	return val.Value, nil
}

// PutToStore saves data to long-term store
func (r *ToolRuntime) PutToStore(namespace []string, key string, value interface{}) error {
	if !r.Config.EnableStoreAccess {
		return fmt.Errorf("store access is disabled")
	}

	// Check namespace restrictions
	if len(r.Config.AllowedNamespaces) > 0 {
		allowed := false
		for _, ns := range r.Config.AllowedNamespaces {
			if len(namespace) > 0 && namespace[0] == ns {
				allowed = true
				break
			}
		}
		if !allowed {
			return fmt.Errorf("access to namespace %v is not allowed", namespace)
		}
	}

	return r.Store.Put(r.Context, namespace, key, value)
}

// Stream sends data to the stream writer
func (r *ToolRuntime) Stream(data interface{}) error {
	if !r.Config.EnableStreaming {
		return fmt.Errorf("streaming is disabled")
	}

	if r.StreamWriter == nil {
		return fmt.Errorf("no stream writer configured")
	}

	return r.StreamWriter(data)
}

// GetMetadata retrieves metadata value
func (r *ToolRuntime) GetMetadata(key string) (interface{}, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	value, exists := r.Metadata[key]
	return value, exists
}

// Clone creates a copy of the runtime
func (r *ToolRuntime) Clone() *ToolRuntime {
	r.mu.RLock()
	defer r.mu.RUnlock()

	// Copy metadata
	metadata := make(map[string]interface{})
	for k, v := range r.Metadata {
		metadata[k] = v
	}

	return &ToolRuntime{
		State:        r.State,
		Context:      r.Context,
		Store:        r.Store,
		Config:       r.Config,
		ToolCallID:   r.ToolCallID,
		AgentID:      r.AgentID,
		SessionID:    r.SessionID,
		StreamWriter: r.StreamWriter,
		Metadata:     metadata,
	}
}

// RuntimeTool interface for tools that use runtime
type RuntimeTool interface {
	Tool
	// ExecuteWithRuntime executes the tool with runtime context
	ExecuteWithRuntime(ctx context.Context, input *ToolInput, runtime *ToolRuntime) (*ToolOutput, error)
}

// RuntimeToolAdapter adapts a RuntimeTool to the standard Tool interface
type RuntimeToolAdapter struct {
	*BaseTool
	tool    RuntimeTool
	runtime *ToolRuntime
}

// NewRuntimeToolAdapter creates a new adapter
func NewRuntimeToolAdapter(tool RuntimeTool, runtime *ToolRuntime) *RuntimeToolAdapter {
	adapter := &RuntimeToolAdapter{
		tool:    tool,
		runtime: runtime,
	}

	// Create BaseTool with the adapted execute function
	adapter.BaseTool = NewBaseTool(
		tool.Name(),
		tool.Description(),
		tool.ArgsSchema(),
		func(ctx context.Context, input *ToolInput) (*ToolOutput, error) {
			return adapter.tool.ExecuteWithRuntime(ctx, input, adapter.runtime)
		},
	)

	return adapter
}

// Invoke implements the Tool interface through BaseTool
func (a *RuntimeToolAdapter) Invoke(ctx context.Context, input *interfaces.ToolInput) (*interfaces.ToolOutput, error) {
	return a.BaseTool.Invoke(ctx, input)
}

// BaseRuntimeTool provides a base implementation for runtime tools
type BaseRuntimeTool struct {
	*BaseTool
	runtime *ToolRuntime
}

// SetRuntime sets the runtime for the tool
func (t *BaseRuntimeTool) SetRuntime(runtime *ToolRuntime) {
	t.runtime = runtime
}

// GetRuntime returns the current runtime
func (t *BaseRuntimeTool) GetRuntime() *ToolRuntime {
	return t.runtime
}

// Example runtime tools

// UserInfoTool retrieves user information using runtime
type UserInfoTool struct {
	*BaseRuntimeTool
}

// NewUserInfoTool creates a new user info tool
func NewUserInfoTool() *UserInfoTool {
	tool := &UserInfoTool{
		BaseRuntimeTool: &BaseRuntimeTool{},
	}
	tool.BaseTool = NewBaseTool(
		"get_user_info",
		"Retrieve user information from store",
		`{"type": "object", "properties": {}}`,
		nil, // Will be overridden by ExecuteWithRuntime
	)
	return tool
}

// ExecuteWithRuntime retrieves user info using runtime
func (t *UserInfoTool) ExecuteWithRuntime(ctx context.Context, input *ToolInput, runtime *ToolRuntime) (*ToolOutput, error) {
	// Stream progress
	if err := runtime.Stream(map[string]interface{}{
		"status": "Looking up user information",
		"tool":   t.Name(),
	}); err != nil {
		return nil, fmt.Errorf("failed to stream progress: %w", err)
	}

	// Get user ID from state
	userID, err := runtime.GetState("user_id")
	if err != nil {
		return nil, fmt.Errorf("failed to get user ID: %w", err)
	}

	if userID == nil {
		return nil, fmt.Errorf("no user ID in state")
	}

	// Retrieve from store
	userInfo, err := runtime.GetFromStore([]string{"users"}, userID.(string))
	if err != nil {
		return nil, fmt.Errorf("failed to retrieve user info: %w", err)
	}

	// Stream completion
	if err := runtime.Stream(map[string]interface{}{
		"status":  "User information retrieved",
		"user_id": userID,
	}); err != nil {
		return nil, fmt.Errorf("failed to stream completion: %w", err)
	}

	return &ToolOutput{
		Result:  userInfo,
		Success: true,
	}, nil
}

// SavePreferenceTool saves user preferences using runtime
type SavePreferenceTool struct {
	*BaseRuntimeTool
}

// NewSavePreferenceTool creates a new save preference tool
func NewSavePreferenceTool() *SavePreferenceTool {
	tool := &SavePreferenceTool{
		BaseRuntimeTool: &BaseRuntimeTool{},
	}
	tool.BaseTool = NewBaseTool(
		"save_preference",
		"Save user preference to store",
		`{"type": "object", "properties": {"key": {"type": "string"}, "value": {}}}`,
		nil, // Will be overridden by ExecuteWithRuntime
	)
	return tool
}

// ExecuteWithRuntime saves a preference using runtime
func (t *SavePreferenceTool) ExecuteWithRuntime(ctx context.Context, input *ToolInput, runtime *ToolRuntime) (*ToolOutput, error) {
	// Parse input
	key, ok := input.Args["key"].(string)
	if !ok {
		return nil, fmt.Errorf("missing preference key")
	}

	value, ok := input.Args["value"]
	if !ok {
		return nil, fmt.Errorf("missing preference value")
	}

	// Get user ID from state
	userID, err := runtime.GetState("user_id")
	if err != nil {
		return nil, fmt.Errorf("failed to get user ID: %w", err)
	}

	if userID == nil {
		return nil, fmt.Errorf("no user ID in state")
	}

	// Get existing preferences
	existingPrefs, _ := runtime.GetFromStore([]string{"preferences"}, userID.(string))

	prefs, ok := existingPrefs.(map[string]interface{})
	if !ok || prefs == nil {
		prefs = make(map[string]interface{})
	}

	// Update preference
	prefs[key] = value

	// Save back to store
	err = runtime.PutToStore([]string{"preferences"}, userID.(string), prefs)
	if err != nil {
		return nil, fmt.Errorf("failed to save preference: %w", err)
	}

	// Update state with the new preference
	if err := runtime.SetState(fmt.Sprintf("pref_%s", key), value); err != nil {
		return nil, fmt.Errorf("failed to update state: %w", err)
	}

	return &ToolOutput{
		Result: map[string]interface{}{
			"status": "saved",
			"key":    key,
			"value":  value,
		},
		Success: true,
	}, nil
}

// UpdateStateTool modifies agent state directly
type UpdateStateTool struct {
	*BaseRuntimeTool
}

// NewUpdateStateTool creates a new update state tool
func NewUpdateStateTool() *UpdateStateTool {
	tool := &UpdateStateTool{
		BaseRuntimeTool: &BaseRuntimeTool{},
	}
	tool.BaseTool = NewBaseTool(
		"update_state",
		"Update agent state directly",
		`{"type": "object", "additionalProperties": true}`,
		nil, // Will be overridden by ExecuteWithRuntime
	)
	return tool
}

// ExecuteWithRuntime updates state using runtime
func (t *UpdateStateTool) ExecuteWithRuntime(ctx context.Context, input *ToolInput, runtime *ToolRuntime) (*ToolOutput, error) {
	// Apply updates
	for key, value := range input.Args {
		err := runtime.SetState(key, value)
		if err != nil {
			return nil, fmt.Errorf("failed to update state key %s: %w", key, err)
		}
	}

	// Stream the updates
	if err := runtime.Stream(map[string]interface{}{
		"status":  "State updated",
		"updates": input.Args,
	}); err != nil {
		return nil, fmt.Errorf("failed to stream updates: %w", err)
	}

	return &ToolOutput{
		Result: map[string]interface{}{
			"status":  "success",
			"updated": len(input.Args),
		},
		Success: true,
	}, nil
}

// ToolRuntimeManager manages runtime instances for tools
type ToolRuntimeManager struct {
	runtimes map[string]*ToolRuntime
	mu       sync.RWMutex
}

// NewToolRuntimeManager creates a new manager
func NewToolRuntimeManager() *ToolRuntimeManager {
	return &ToolRuntimeManager{
		runtimes: make(map[string]*ToolRuntime),
	}
}

// CreateRuntime creates a new runtime for a tool call
func (m *ToolRuntimeManager) CreateRuntime(callID string, state core.State, store store.Store) *ToolRuntime {
	m.mu.Lock()
	defer m.mu.Unlock()

	runtime := NewToolRuntime(context.Background(), state, store)
	runtime.ToolCallID = callID
	m.runtimes[callID] = runtime

	return runtime
}

// GetRuntime retrieves a runtime by call ID
func (m *ToolRuntimeManager) GetRuntime(callID string) (*ToolRuntime, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	runtime, exists := m.runtimes[callID]
	return runtime, exists
}

// RemoveRuntime removes a runtime
func (m *ToolRuntimeManager) RemoveRuntime(callID string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	delete(m.runtimes, callID)
}
