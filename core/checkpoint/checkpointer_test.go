package checkpoint

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	agentstate "github.com/kart-io/goagent/core/state"
)

func TestNewInMemorySaver(t *testing.T) {
	saver := NewInMemorySaver()
	require.NotNil(t, saver)
	assert.Equal(t, 0, saver.Size())
}

func TestInMemorySaver_SaveAndLoad(t *testing.T) {
	saver := NewInMemorySaver()
	ctx := context.Background()

	state := agentstate.NewAgentState()
	state.Set("key1", "value1")
	state.Set("key2", 42)

	threadID := "thread123"

	// Save state
	err := saver.Save(ctx, threadID, state)
	require.NoError(t, err)
	assert.Equal(t, 1, saver.Size())

	// Load state
	loadedState, err := saver.Load(ctx, threadID)
	require.NoError(t, err)
	require.NotNil(t, loadedState)

	// Verify loaded state
	val1, ok := loadedState.Get("key1")
	assert.True(t, ok)
	assert.Equal(t, "value1", val1)

	val2, ok := loadedState.Get("key2")
	assert.True(t, ok)
	assert.Equal(t, 42, val2)
}

func TestInMemorySaver_UpdateCheckpoint(t *testing.T) {
	saver := NewInMemorySaver()
	ctx := context.Background()
	threadID := "thread123"

	// Initial save
	state1 := agentstate.NewAgentState()
	state1.Set("key1", "value1")
	err := saver.Save(ctx, threadID, state1)
	require.NoError(t, err)

	// Wait a bit to ensure different timestamp
	time.Sleep(10 * time.Millisecond)

	// Update save
	state2 := agentstate.NewAgentState()
	state2.Set("key1", "value2")
	state2.Set("key2", "new")
	err = saver.Save(ctx, threadID, state2)
	require.NoError(t, err)

	// Still one checkpoint
	assert.Equal(t, 1, saver.Size())

	// Load should return latest state
	loadedState, err := saver.Load(ctx, threadID)
	require.NoError(t, err)

	val1, ok := loadedState.Get("key1")
	assert.True(t, ok)
	assert.Equal(t, "value2", val1)

	val2, ok := loadedState.Get("key2")
	assert.True(t, ok)
	assert.Equal(t, "new", val2)
}

func TestInMemorySaver_LoadNonExistent(t *testing.T) {
	saver := NewInMemorySaver()
	ctx := context.Background()

	_, err := saver.Load(ctx, "nonexistent")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "checkpoint not found")
}

func TestInMemorySaver_List(t *testing.T) {
	saver := NewInMemorySaver()
	ctx := context.Background()

	// Save multiple checkpoints
	for i := 0; i < 3; i++ {
		state := agentstate.NewAgentState()
		state.Set("count", i)
		err := saver.Save(ctx, fmt.Sprintf("thread%d", i), state)
		require.NoError(t, err)
	}

	// List checkpoints
	infos, err := saver.List(ctx)
	require.NoError(t, err)
	assert.Equal(t, 3, len(infos))

	// Verify each checkpoint has required fields
	for _, info := range infos {
		assert.NotEmpty(t, info.ThreadID)
		assert.NotZero(t, info.CreatedAt)
		assert.NotZero(t, info.UpdatedAt)
		assert.NotNil(t, info.Metadata)
	}
}

func TestInMemorySaver_Delete(t *testing.T) {
	saver := NewInMemorySaver()
	ctx := context.Background()
	threadID := "thread123"

	// Save checkpoint
	state := agentstate.NewAgentState()
	state.Set("key1", "value1")
	err := saver.Save(ctx, threadID, state)
	require.NoError(t, err)
	assert.Equal(t, 1, saver.Size())

	// Delete checkpoint
	err = saver.Delete(ctx, threadID)
	require.NoError(t, err)
	assert.Equal(t, 0, saver.Size())

	// Verify deleted
	_, err = saver.Load(ctx, threadID)
	assert.Error(t, err)

	// Delete non-existent should not error
	err = saver.Delete(ctx, "nonexistent")
	assert.NoError(t, err)
}

func TestInMemorySaver_Exists(t *testing.T) {
	saver := NewInMemorySaver()
	ctx := context.Background()
	threadID := "thread123"

	// Check non-existent
	exists, err := saver.Exists(ctx, threadID)
	require.NoError(t, err)
	assert.False(t, exists)

	// Save checkpoint
	state := agentstate.NewAgentState()
	err = saver.Save(ctx, threadID, state)
	require.NoError(t, err)

	// Check exists
	exists, err = saver.Exists(ctx, threadID)
	require.NoError(t, err)
	assert.True(t, exists)

	// Delete and check again
	err = saver.Delete(ctx, threadID)
	require.NoError(t, err)

	exists, err = saver.Exists(ctx, threadID)
	require.NoError(t, err)
	assert.False(t, exists)
}

func TestInMemorySaver_GetHistory(t *testing.T) {
	saver := NewInMemorySaver()
	ctx := context.Background()
	threadID := "thread123"

	// Save initial state
	state1 := agentstate.NewAgentState()
	state1.Set("version", 1)
	err := saver.Save(ctx, threadID, state1)
	require.NoError(t, err)

	// Update multiple times
	state2 := agentstate.NewAgentState()
	state2.Set("version", 2)
	err = saver.Save(ctx, threadID, state2)
	require.NoError(t, err)

	state3 := agentstate.NewAgentState()
	state3.Set("version", 3)
	err = saver.Save(ctx, threadID, state3)
	require.NoError(t, err)

	// Get history
	history, err := saver.GetHistory(ctx, threadID)
	require.NoError(t, err)
	assert.Equal(t, 2, len(history)) // 2 previous versions

	// Verify history order (oldest to newest)
	val1, ok := history[0].Get("version")
	assert.True(t, ok)
	assert.Equal(t, 1, val1)

	val2, ok := history[1].Get("version")
	assert.True(t, ok)
	assert.Equal(t, 2, val2)

	// Current state should be version 3
	current, err := saver.Load(ctx, threadID)
	require.NoError(t, err)
	val3, ok := current.Get("version")
	assert.True(t, ok)
	assert.Equal(t, 3, val3)
}

func TestInMemorySaver_GetHistoryNonExistent(t *testing.T) {
	saver := NewInMemorySaver()
	ctx := context.Background()

	_, err := saver.GetHistory(ctx, "nonexistent")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "checkpoint not found")
}

func TestInMemorySaver_CleanupOld(t *testing.T) {
	saver := NewInMemorySaver()
	ctx := context.Background()

	// Save checkpoints with different ages
	oldState := agentstate.NewAgentState()
	err := saver.Save(ctx, "old_thread", oldState)
	require.NoError(t, err)

	recentState := agentstate.NewAgentState()
	err = saver.Save(ctx, "recent_thread", recentState)
	require.NoError(t, err)

	// Manually set timestamp for old checkpoint
	saver.mu.Lock()
	if cp, ok := saver.checkpoints["old_thread"]; ok {
		cp.info.UpdatedAt = time.Now().Add(-2 * time.Hour)
	}
	saver.mu.Unlock()

	assert.Equal(t, 2, saver.Size())

	// Cleanup checkpoints older than 1 hour
	removed := saver.CleanupOld(1 * time.Hour)
	assert.Equal(t, 1, removed)
	assert.Equal(t, 1, saver.Size())

	// Old checkpoint should be removed
	exists, err := saver.Exists(ctx, "old_thread")
	require.NoError(t, err)
	assert.False(t, exists)

	// Recent checkpoint should still exist
	exists, err = saver.Exists(ctx, "recent_thread")
	require.NoError(t, err)
	assert.True(t, exists)
}

func TestInMemorySaver_StateIsolation(t *testing.T) {
	saver := NewInMemorySaver()
	ctx := context.Background()
	threadID := "thread123"

	// Save original state
	original := agentstate.NewAgentState()
	original.Set("key1", "value1")
	err := saver.Save(ctx, threadID, original)
	require.NoError(t, err)

	// Load state
	loaded, err := saver.Load(ctx, threadID)
	require.NoError(t, err)

	// Modify loaded state
	loaded.Set("key1", "modified")
	loaded.Set("key2", "new")

	// Load again and verify original is unchanged
	loaded2, err := saver.Load(ctx, threadID)
	require.NoError(t, err)

	val, ok := loaded2.Get("key1")
	assert.True(t, ok)
	assert.Equal(t, "value1", val) // Should still be original value

	_, ok = loaded2.Get("key2")
	assert.False(t, ok) // Should not have new key
}

func TestDefaultCheckpointerConfig(t *testing.T) {
	config := DefaultCheckpointerConfig()

	require.NotNil(t, config)
	assert.Equal(t, 10, config.MaxHistorySize)
	assert.Equal(t, 24*time.Hour, config.MaxCheckpointAge)
	assert.False(t, config.EnableCompression)
	assert.True(t, config.AutoCleanup)
	assert.Equal(t, 1*time.Hour, config.CleanupInterval)
}

func TestCheckpointerWithAutoCleanup(t *testing.T) {
	baseSaver := NewInMemorySaver()
	config := &CheckpointerConfig{
		MaxCheckpointAge: 100 * time.Millisecond,
		AutoCleanup:      true,
		CleanupInterval:  50 * time.Millisecond,
	}

	wrapper := NewCheckpointerWithAutoCleanup(baseSaver, config)
	defer wrapper.Stop()

	ctx := context.Background()

	// Save a checkpoint
	state := agentstate.NewAgentState()
	state.Set("key", "value")
	err := wrapper.Save(ctx, "thread1", state)
	require.NoError(t, err)

	// Manually set old timestamp
	baseSaver.mu.Lock()
	if cp, ok := baseSaver.checkpoints["thread1"]; ok {
		cp.info.UpdatedAt = time.Now().Add(-200 * time.Millisecond)
	}
	baseSaver.mu.Unlock()

	// Wait for auto cleanup to run
	time.Sleep(150 * time.Millisecond)

	// Checkpoint should be cleaned up
	exists, err := wrapper.Exists(ctx, "thread1")
	require.NoError(t, err)
	assert.False(t, exists)
}

func TestCheckpointerWithAutoCleanup_AllMethods(t *testing.T) {
	baseSaver := NewInMemorySaver()
	config := DefaultCheckpointerConfig()
	config.AutoCleanup = false // Disable for this test

	wrapper := NewCheckpointerWithAutoCleanup(baseSaver, config)
	ctx := context.Background()

	// Test Save
	state := agentstate.NewAgentState()
	state.Set("key", "value")
	err := wrapper.Save(ctx, "thread1", state)
	require.NoError(t, err)

	// Test Load
	loaded, err := wrapper.Load(ctx, "thread1")
	require.NoError(t, err)
	assert.NotNil(t, loaded)

	// Test Exists
	exists, err := wrapper.Exists(ctx, "thread1")
	require.NoError(t, err)
	assert.True(t, exists)

	// Test List
	infos, err := wrapper.List(ctx)
	require.NoError(t, err)
	assert.Equal(t, 1, len(infos))

	// Test Delete
	err = wrapper.Delete(ctx, "thread1")
	require.NoError(t, err)

	exists, err = wrapper.Exists(ctx, "thread1")
	require.NoError(t, err)
	assert.False(t, exists)
}

func BenchmarkInMemorySaver_Save(b *testing.B) {
	saver := NewInMemorySaver()
	ctx := context.Background()
	state := agentstate.NewAgentState()
	state.Set("key", "value")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		saver.Save(ctx, fmt.Sprintf("thread%d", i%100), state)
	}
}

func BenchmarkInMemorySaver_Load(b *testing.B) {
	saver := NewInMemorySaver()
	ctx := context.Background()
	state := agentstate.NewAgentState()
	state.Set("key", "value")

	// Pre-populate
	for i := 0; i < 100; i++ {
		saver.Save(ctx, fmt.Sprintf("thread%d", i), state)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		saver.Load(ctx, fmt.Sprintf("thread%d", i%100))
	}
}

func BenchmarkInMemorySaver_SaveAndLoad(b *testing.B) {
	saver := NewInMemorySaver()
	ctx := context.Background()
	state := agentstate.NewAgentState()
	state.Set("key", "value")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		threadID := fmt.Sprintf("thread%d", i%100)
		saver.Save(ctx, threadID, state)
		saver.Load(ctx, threadID)
	}
}
