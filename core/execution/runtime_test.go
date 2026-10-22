package execution

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kart-io/goagent/core/checkpoint"
	"github.com/kart-io/goagent/core/state"
	"github.com/kart-io/goagent/store/memory"
)

// MockContext for testing
type MockContext struct {
	UserID   string
	UserName string
}

func TestNewRuntime(t *testing.T) {
	ctx := MockContext{UserID: "user123", UserName: "Alice"}
	st := state.NewAgentState()
	store := memory.New()
	checkpointer := checkpoint.NewInMemorySaver()
	sessionID := "session123"

	runtime := NewRuntime(ctx, st, store, checkpointer, sessionID)

	require.NotNil(t, runtime)
	assert.Equal(t, "user123", runtime.Context.UserID)
	assert.Equal(t, "Alice", runtime.Context.UserName)
	assert.NotNil(t, runtime.State)
	assert.NotNil(t, runtime.Store)
	assert.NotNil(t, runtime.Checkpointer)
	assert.Equal(t, "session123", runtime.SessionID)
	assert.NotZero(t, runtime.Timestamp)
	assert.NotNil(t, runtime.Metadata)
}

func TestRuntime_WithToolCallID(t *testing.T) {
	ctx := MockContext{UserID: "user123"}
	st := state.NewAgentState()
	runtime := NewRuntime(ctx, st, nil, nil, "session123")

	newRuntime := runtime.WithToolCallID("tool-call-456")

	assert.Equal(t, "tool-call-456", newRuntime.ToolCallID)
	assert.Empty(t, runtime.ToolCallID) // Original should be unchanged
}

func TestRuntime_WithMetadata(t *testing.T) {
	ctx := MockContext{UserID: "user123"}
	st := state.NewAgentState()
	runtime := NewRuntime(ctx, st, nil, nil, "session123")

	newRuntime := runtime.WithMetadata("key1", "value1")
	newRuntime2 := newRuntime.WithMetadata("key2", 42)

	// Check new runtime has metadata
	assert.Equal(t, "value1", newRuntime.Metadata["key1"])
	assert.Equal(t, "value1", newRuntime2.Metadata["key1"])
	assert.Equal(t, 42, newRuntime2.Metadata["key2"])

	// Original should be unchanged
	assert.Empty(t, runtime.Metadata)
}

func TestRuntime_SaveAndLoadState(t *testing.T) {
	ctx := MockContext{UserID: "user123"}
	st := state.NewAgentState()
	st.Set("key1", "value1")
	checkpointer := checkpoint.NewInMemorySaver()
	runtime := NewRuntime(ctx, st, nil, checkpointer, "session123")

	// Save state
	err := runtime.SaveState(context.Background())
	require.NoError(t, err)

	// Load state
	loadedState, err := runtime.LoadState(context.Background())
	require.NoError(t, err)
	require.NotNil(t, loadedState)

	val, ok := loadedState.Get("key1")
	assert.True(t, ok)
	assert.Equal(t, "value1", val)
}

func TestRuntime_SaveStateWithoutCheckpointer(t *testing.T) {
	ctx := MockContext{UserID: "user123"}
	st := state.NewAgentState()
	runtime := NewRuntime(ctx, st, nil, nil, "session123")

	// Should not error when checkpointer is nil
	err := runtime.SaveState(context.Background())
	assert.NoError(t, err)
}

func TestToolWithRuntime(t *testing.T) {
	ctx := MockContext{UserID: "user123", UserName: "Alice"}
	st := state.NewAgentState()
	store := memory.New()
	runtime := NewRuntime(ctx, st, store, nil, "session123")

	// Create a tool function that uses runtime
	toolFunc := func(ctx context.Context, input string, rt *Runtime[MockContext, *state.AgentState]) (string, error) {
		// Access runtime context
		userName := rt.Context.UserName

		// Update state
		rt.State.Set("last_input", input)

		return "Hello " + userName + ", you said: " + input, nil
	}

	// Create tool with runtime
	tool := NewToolWithRuntime("greet", "Greets the user", toolFunc, runtime)

	assert.Equal(t, "greet", tool.Name())
	assert.Equal(t, "Greets the user", tool.Description())

	// Execute tool
	result, err := tool.Execute(context.Background(), "test message")
	require.NoError(t, err)
	assert.Equal(t, "Hello Alice, you said: test message", result)

	// Verify state was updated
	val, ok := st.Get("last_input")
	assert.True(t, ok)
	assert.Equal(t, "test message", val)
}

func TestToolWithRuntime_WithRuntime(t *testing.T) {
	ctx1 := MockContext{UserID: "user1", UserName: "Alice"}
	ctx2 := MockContext{UserID: "user2", UserName: "Bob"}
	state1 := state.NewAgentState()
	state2 := state.NewAgentState()
	runtime1 := NewRuntime(ctx1, state1, nil, nil, "session1")
	runtime2 := NewRuntime(ctx2, state2, nil, nil, "session2")

	toolFunc := func(ctx context.Context, input string, rt *Runtime[MockContext, *state.AgentState]) (string, error) {
		return "Hello " + rt.Context.UserName, nil
	}

	// Create tool with runtime1
	tool := NewToolWithRuntime("greet", "Greets the user", toolFunc, runtime1)

	result1, err := tool.Execute(context.Background(), "")
	require.NoError(t, err)
	assert.Equal(t, "Hello Alice", result1)

	// Update tool with runtime2
	tool2 := tool.WithRuntime(runtime2)

	result2, err := tool2.Execute(context.Background(), "")
	require.NoError(t, err)
	assert.Equal(t, "Hello Bob", result2)
}

func TestDefaultRuntimeConfig(t *testing.T) {
	config := DefaultRuntimeConfig()

	require.NotNil(t, config)
	assert.True(t, config.EnableAutoSave)
	assert.Equal(t, 30*time.Second, config.SaveInterval)
	assert.Equal(t, int64(10*1024*1024), config.MaxStateSize)
	assert.False(t, config.EnableMetrics)
}

func TestNewRuntimeMetrics(t *testing.T) {
	metrics := NewRuntimeMetrics()

	require.NotNil(t, metrics)
	assert.Equal(t, int64(0), metrics.TotalToolCalls)
	assert.Equal(t, int64(0), metrics.TotalStateUpdates)
	assert.Equal(t, int64(0), metrics.TotalStorageOperations)
	assert.Equal(t, int64(0), metrics.TotalCheckpoints)
}

func TestNewRuntimeManager(t *testing.T) {
	config := DefaultRuntimeConfig()
	manager := NewRuntimeManager[MockContext, *state.AgentState](config)

	require.NotNil(t, manager)
	assert.Equal(t, 0, manager.Size())
	assert.NotNil(t, manager.Metrics())
}

func TestRuntimeManager_GetAndSetRuntime(t *testing.T) {
	manager := NewRuntimeManager[MockContext, *state.AgentState](nil)
	ctx := MockContext{UserID: "user123"}
	st := state.NewAgentState()
	runtime := NewRuntime(ctx, st, nil, nil, "session123")

	// Get non-existent runtime
	_, ok := manager.GetRuntime("session123")
	assert.False(t, ok)

	// Set runtime
	manager.SetRuntime("session123", runtime)
	assert.Equal(t, 1, manager.Size())

	// Get existing runtime
	retrievedRuntime, ok := manager.GetRuntime("session123")
	assert.True(t, ok)
	require.NotNil(t, retrievedRuntime)
	assert.Equal(t, "session123", retrievedRuntime.SessionID)
}

func TestRuntimeManager_RemoveRuntime(t *testing.T) {
	manager := NewRuntimeManager[MockContext, *state.AgentState](nil)
	ctx := MockContext{UserID: "user123"}
	st := state.NewAgentState()
	runtime := NewRuntime(ctx, st, nil, nil, "session123")

	manager.SetRuntime("session123", runtime)
	assert.Equal(t, 1, manager.Size())

	manager.RemoveRuntime("session123")
	assert.Equal(t, 0, manager.Size())

	_, ok := manager.GetRuntime("session123")
	assert.False(t, ok)
}

func TestRuntimeManager_GetOrCreateRuntime(t *testing.T) {
	manager := NewRuntimeManager[MockContext, *state.AgentState](nil)
	ctx := MockContext{UserID: "user123"}
	st := state.NewAgentState()
	store := memory.New()
	checkpointer := checkpoint.NewInMemorySaver()

	// Create new runtime
	runtime1 := manager.GetOrCreateRuntime("session123", ctx, st, store, checkpointer)
	require.NotNil(t, runtime1)
	assert.Equal(t, "session123", runtime1.SessionID)
	assert.Equal(t, 1, manager.Size())

	// Get existing runtime
	runtime2 := manager.GetOrCreateRuntime("session123", ctx, st, store, checkpointer)
	require.NotNil(t, runtime2)
	assert.Equal(t, runtime1, runtime2) // Should be same instance
	assert.Equal(t, 1, manager.Size())  // Size should not change
}

func TestRuntimeManager_CleanupExpired(t *testing.T) {
	manager := NewRuntimeManager[MockContext, *state.AgentState](nil)

	// Create old runtime
	ctx := MockContext{UserID: "user1"}
	state1 := state.NewAgentState()
	runtime1 := NewRuntime(ctx, state1, nil, nil, "session1")
	runtime1.Timestamp = time.Now().Add(-2 * time.Hour) // 2 hours ago
	manager.SetRuntime("session1", runtime1)

	// Create recent runtime
	state2 := state.NewAgentState()
	runtime2 := NewRuntime(ctx, state2, nil, nil, "session2")
	runtime2.Timestamp = time.Now().Add(-10 * time.Minute) // 10 minutes ago
	manager.SetRuntime("session2", runtime2)

	assert.Equal(t, 2, manager.Size())

	// Cleanup runtimes older than 1 hour
	removed := manager.CleanupExpired(1 * time.Hour)

	assert.Equal(t, 1, removed)
	assert.Equal(t, 1, manager.Size())

	// session1 should be removed
	_, ok := manager.GetRuntime("session1")
	assert.False(t, ok)

	// session2 should still exist
	_, ok = manager.GetRuntime("session2")
	assert.True(t, ok)
}

func TestRuntimeManager_Metrics(t *testing.T) {
	manager := NewRuntimeManager[MockContext, *state.AgentState](nil)

	metrics := manager.Metrics()
	require.NotNil(t, metrics)
	assert.Equal(t, int64(0), metrics.TotalToolCalls)
}

func BenchmarkRuntime_WithToolCallID(b *testing.B) {
	ctx := MockContext{UserID: "user123"}
	st := state.NewAgentState()
	runtime := NewRuntime(ctx, st, nil, nil, "session123")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		runtime.WithToolCallID("tool-call-123")
	}
}

func BenchmarkRuntime_WithMetadata(b *testing.B) {
	ctx := MockContext{UserID: "user123"}
	st := state.NewAgentState()
	runtime := NewRuntime(ctx, st, nil, nil, "session123")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		runtime.WithMetadata("key", "value")
	}
}

func BenchmarkToolWithRuntime_Execute(b *testing.B) {
	ctx := MockContext{UserID: "user123", UserName: "Alice"}
	st := state.NewAgentState()
	runtime := NewRuntime(ctx, st, nil, nil, "session123")

	toolFunc := func(ctx context.Context, input string, rt *Runtime[MockContext, *state.AgentState]) (string, error) {
		rt.State.Set("input", input)
		return "result", nil
	}

	tool := NewToolWithRuntime("test", "Test tool", toolFunc, runtime)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = tool.Execute(context.Background(), "test")
	}
}
