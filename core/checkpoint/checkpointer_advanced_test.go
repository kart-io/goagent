package checkpoint

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	agentstate "github.com/kart-io/goagent/core/state"
)

// TestInMemorySaver_ConcurrentSaveAndLoad tests concurrent save/load operations
func TestInMemorySaver_ConcurrentSaveAndLoad(t *testing.T) {
	saver := NewInMemorySaver()
	ctx := context.Background()
	const (
		numThreads          = 50
		operationsPerThread = 100
	)

	var wg sync.WaitGroup
	errors := make(chan error, numThreads*operationsPerThread)

	// Launch concurrent save and load operations
	for i := 0; i < numThreads; i++ {
		wg.Add(1)
		go func(threadNum int) {
			defer wg.Done()
			threadID := fmt.Sprintf("thread-%d", threadNum)

			for j := 0; j < operationsPerThread; j++ {
				// Alternate between save and load
				if j%2 == 0 {
					state := agentstate.NewAgentState()
					state.Set("counter", j)
					state.Set("thread", threadID)
					if err := saver.Save(ctx, threadID, state); err != nil {
						errors <- fmt.Errorf("save error: %w", err)
					}
				} else {
					if _, err := saver.Load(ctx, threadID); err != nil {
						// It's okay if not found on first iteration
						if j > 0 {
							errors <- fmt.Errorf("load error: %w", err)
						}
					}
				}
			}
		}(i)
	}

	wg.Wait()
	close(errors)

	// Check for errors
	for err := range errors {
		t.Errorf("Concurrent operation failed: %v", err)
	}

	// Verify final state
	assert.Equal(t, numThreads, saver.Size())
}

// TestInMemorySaver_ConcurrentDelete tests concurrent delete operations
func TestInMemorySaver_ConcurrentDelete(t *testing.T) {
	saver := NewInMemorySaver()
	ctx := context.Background()
	const numThreads = 30

	// Pre-populate checkpoints
	for i := 0; i < numThreads; i++ {
		state := agentstate.NewAgentState()
		state.Set("id", i)
		err := saver.Save(ctx, fmt.Sprintf("thread-%d", i), state)
		require.NoError(t, err)
	}

	assert.Equal(t, numThreads, saver.Size())

	// Concurrently delete
	var wg sync.WaitGroup
	var deletedCount int32
	for i := 0; i < numThreads; i++ {
		wg.Add(1)
		go func(threadNum int) {
			defer wg.Done()
			threadID := fmt.Sprintf("thread-%d", threadNum)
			if err := saver.Delete(ctx, threadID); err == nil {
				atomic.AddInt32(&deletedCount, 1)
			}
		}(i)
	}

	wg.Wait()
	assert.Equal(t, int32(numThreads), deletedCount)
	assert.Equal(t, 0, saver.Size())
}

// TestInMemorySaver_ConcurrentExists tests concurrent exists checks
func TestInMemorySaver_ConcurrentExists(t *testing.T) {
	saver := NewInMemorySaver()
	ctx := context.Background()
	threadID := "concurrent-thread"

	// Save a checkpoint
	state := agentstate.NewAgentState()
	err := saver.Save(ctx, threadID, state)
	require.NoError(t, err)

	// Concurrent exists checks
	const numGoroutines = 100
	results := make(chan bool, numGoroutines)
	var wg sync.WaitGroup

	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			exists, err := saver.Exists(ctx, threadID)
			if err == nil {
				results <- exists
			}
		}()
	}

	wg.Wait()
	close(results)

	// All should report exists
	for exists := range results {
		assert.True(t, exists)
	}
}

// TestInMemorySaver_ConcurrentList tests concurrent list operations
func TestInMemorySaver_ConcurrentList(t *testing.T) {
	saver := NewInMemorySaver()
	ctx := context.Background()

	// Pre-populate
	const numCheckpoints = 20
	for i := 0; i < numCheckpoints; i++ {
		state := agentstate.NewAgentState()
		state.Set("id", i)
		err := saver.Save(ctx, fmt.Sprintf("thread-%d", i), state)
		require.NoError(t, err)
	}

	// Concurrent list operations
	const numGoroutines = 50
	results := make(chan int, numGoroutines)
	var wg sync.WaitGroup

	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			infos, err := saver.List(ctx)
			if err == nil {
				results <- len(infos)
			}
		}()
	}

	wg.Wait()
	close(results)

	// All should report same count
	for count := range results {
		assert.Equal(t, numCheckpoints, count)
	}
}

// TestInMemorySaver_LargeStateData tests saving and loading large state objects
func TestInMemorySaver_LargeStateData(t *testing.T) {
	saver := NewInMemorySaver()
	ctx := context.Background()
	threadID := "large-state"

	// Create a large state
	state := agentstate.NewAgentState()
	for i := 0; i < 1000; i++ {
		state.Set(fmt.Sprintf("key-%d", i), fmt.Sprintf("value-%d", i))
	}

	err := saver.Save(ctx, threadID, state)
	require.NoError(t, err)

	// Load and verify
	loaded, err := saver.Load(ctx, threadID)
	require.NoError(t, err)

	// Spot check some values
	for i := 0; i < 1000; i += 100 {
		key := fmt.Sprintf("key-%d", i)
		val, ok := loaded.Get(key)
		assert.True(t, ok)
		assert.Equal(t, fmt.Sprintf("value-%d", i), val)
	}
}

// TestInMemorySaver_HistoryLimits tests history management
func TestInMemorySaver_HistoryLimits(t *testing.T) {
	saver := NewInMemorySaver()
	ctx := context.Background()
	threadID := "history-test"

	// Create multiple versions
	const numVersions = 15
	for i := 0; i < numVersions; i++ {
		state := agentstate.NewAgentState()
		state.Set("version", i)
		err := saver.Save(ctx, threadID, state)
		require.NoError(t, err)
		time.Sleep(1 * time.Millisecond)
	}

	// Get history
	history, err := saver.GetHistory(ctx, threadID)
	require.NoError(t, err)

	// Should have numVersions-1 history entries (current is not in history)
	assert.Equal(t, numVersions-1, len(history))

	// Verify history order
	for i, h := range history {
		val, ok := h.Get("version")
		assert.True(t, ok)
		assert.Equal(t, i, val)
	}
}

// TestInMemorySaver_CleanupOld_MultipleRemove tests cleanup of multiple old checkpoints
func TestInMemorySaver_CleanupOld_MultipleRemove(t *testing.T) {
	saver := NewInMemorySaver()
	ctx := context.Background()

	// Save multiple checkpoints with different ages
	const numCheckpoints = 10
	for i := 0; i < numCheckpoints; i++ {
		state := agentstate.NewAgentState()
		state.Set("id", i)
		err := saver.Save(ctx, fmt.Sprintf("thread-%d", i), state)
		require.NoError(t, err)
	}

	// Set different ages for different checkpoints
	saver.mu.Lock()
	for i := 0; i < numCheckpoints; i++ {
		threadID := fmt.Sprintf("thread-%d", i)
		if cp, ok := saver.checkpoints[threadID]; ok {
			if i%2 == 0 {
				// Make even-indexed checkpoints old
				cp.info.UpdatedAt = time.Now().Add(-25 * time.Hour)
			}
		}
	}
	saver.mu.Unlock()

	// Cleanup
	removed := saver.CleanupOld(24 * time.Hour)
	assert.Equal(t, 5, removed) // Half should be removed
	assert.Equal(t, 5, saver.Size())
}

// TestInMemorySaver_MetadataPreservation tests that metadata is preserved
func TestInMemorySaver_MetadataPreservation(t *testing.T) {
	saver := NewInMemorySaver()
	ctx := context.Background()
	threadID := "metadata-test"

	state := agentstate.NewAgentState()
	state.Set("key", "value")
	err := saver.Save(ctx, threadID, state)
	require.NoError(t, err)

	// Get info and check metadata
	infos, err := saver.List(ctx)
	require.NoError(t, err)
	assert.Equal(t, 1, len(infos))

	info := infos[0]
	assert.NotEmpty(t, info.ID)
	assert.Equal(t, threadID, info.ThreadID)
	assert.NotZero(t, info.CreatedAt)
	assert.NotZero(t, info.UpdatedAt)
	assert.NotNil(t, info.Metadata)
	assert.Greater(t, info.Size, int64(0))
}

// TestInMemorySaver_Versioning tests that created/updated timestamps are correct
func TestInMemorySaver_Versioning(t *testing.T) {
	saver := NewInMemorySaver()
	ctx := context.Background()
	threadID := "version-test"

	// First save
	state1 := agentstate.NewAgentState()
	state1.Set("count", 1)
	beforeFirstSave := time.Now()
	err := saver.Save(ctx, threadID, state1)
	require.NoError(t, err)
	afterFirstSave := time.Now()

	// Get info
	saver.mu.RLock()
	cp1, ok := saver.checkpoints[threadID]
	require.True(t, ok)
	createdAt := cp1.info.CreatedAt
	updatedAt1 := cp1.info.UpdatedAt
	saver.mu.RUnlock()

	// Verify first save timestamps
	assert.True(t, createdAt.After(beforeFirstSave))
	assert.True(t, createdAt.Before(afterFirstSave))
	assert.Equal(t, createdAt, updatedAt1)

	// Wait a bit
	time.Sleep(10 * time.Millisecond)

	// Second save
	state2 := agentstate.NewAgentState()
	state2.Set("count", 2)
	beforeSecondSave := time.Now()
	err = saver.Save(ctx, threadID, state2)
	require.NoError(t, err)
	afterSecondSave := time.Now()

	// Get updated info
	saver.mu.RLock()
	cp2, ok := saver.checkpoints[threadID]
	require.True(t, ok)
	createdAt2 := cp2.info.CreatedAt
	updatedAt2 := cp2.info.UpdatedAt
	saver.mu.RUnlock()

	// Verify second save timestamps
	assert.Equal(t, createdAt, createdAt2)       // Created should not change
	assert.True(t, updatedAt2.After(updatedAt1)) // Updated should advance
	assert.True(t, updatedAt2.After(beforeSecondSave))
	assert.True(t, updatedAt2.Before(afterSecondSave))
}

// TestCheckpointerWithAutoCleanup_Disabled tests with auto-cleanup disabled
func TestCheckpointerWithAutoCleanup_Disabled(t *testing.T) {
	baseSaver := NewInMemorySaver()
	config := &CheckpointerConfig{
		MaxCheckpointAge: 100 * time.Millisecond,
		AutoCleanup:      false, // Disabled
		CleanupInterval:  50 * time.Millisecond,
	}

	wrapper := NewCheckpointerWithAutoCleanup(baseSaver, config)
	defer wrapper.Stop()

	ctx := context.Background()

	// Save checkpoint
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

	// Wait
	time.Sleep(150 * time.Millisecond)

	// Checkpoint should still exist (no auto-cleanup)
	exists, err := wrapper.Exists(ctx, "thread1")
	require.NoError(t, err)
	assert.True(t, exists)
}

// TestCheckpointerWithAutoCleanup_NilConfig tests with nil config
func TestCheckpointerWithAutoCleanup_NilConfig(t *testing.T) {
	baseSaver := NewInMemorySaver()
	wrapper := NewCheckpointerWithAutoCleanup(baseSaver, nil)
	defer wrapper.Stop()

	ctx := context.Background()

	// Should use default config with auto-cleanup enabled
	state := agentstate.NewAgentState()
	state.Set("key", "value")
	err := wrapper.Save(ctx, "thread1", state)
	require.NoError(t, err)

	exists, err := wrapper.Exists(ctx, "thread1")
	require.NoError(t, err)
	assert.True(t, exists)
}

// TestCheckpointerWithAutoCleanup_RapidStopStart tests rapid stop/start
func TestCheckpointerWithAutoCleanup_RapidStopStart(t *testing.T) {
	baseSaver := NewInMemorySaver()
	config := DefaultCheckpointerConfig()

	for i := 0; i < 5; i++ {
		wrapper := NewCheckpointerWithAutoCleanup(baseSaver, config)
		wrapper.Stop()
	}

	// Should not panic or deadlock
	assert.Equal(t, 0, baseSaver.Size())
}

// TestEstimateStateSize tests state size estimation
func TestEstimateStateSize(t *testing.T) {
	tests := []struct {
		name    string
		setup   func() agentstate.State
		checkFn func(int64)
	}{
		{
			name: "empty state",
			setup: func() agentstate.State {
				return agentstate.NewAgentState()
			},
			checkFn: func(size int64) {
				assert.Equal(t, int64(0), size)
			},
		},
		{
			name: "simple state",
			setup: func() agentstate.State {
				state := agentstate.NewAgentState()
				state.Set("key", "value")
				return state
			},
			checkFn: func(size int64) {
				assert.Greater(t, size, int64(0))
			},
		},
		{
			name: "large state",
			setup: func() agentstate.State {
				state := agentstate.NewAgentState()
				for i := 0; i < 100; i++ {
					state.Set(fmt.Sprintf("key-%d", i), fmt.Sprintf("value-%d-with-some-extra-content", i))
				}
				return state
			},
			checkFn: func(size int64) {
				assert.Greater(t, size, int64(1000))
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			state := tt.setup()
			size := estimateStateSize(state)
			tt.checkFn(size)
		})
	}
}

// TestInMemorySaver_EdgeCases tests various edge cases
func TestInMemorySaver_EdgeCases(t *testing.T) {
	saver := NewInMemorySaver()
	ctx := context.Background()

	// Test empty threadID
	state := agentstate.NewAgentState()
	state.Set("key", "value")
	err := saver.Save(ctx, "", state)
	require.NoError(t, err)
	assert.Equal(t, 1, saver.Size())

	exists, err := saver.Exists(ctx, "")
	require.NoError(t, err)
	assert.True(t, exists)

	loaded, err := saver.Load(ctx, "")
	require.NoError(t, err)
	assert.NotNil(t, loaded)

	// Test very long threadID
	longID := "thread-" + string(make([]byte, 10000))
	err = saver.Save(ctx, longID, state)
	require.NoError(t, err)

	exists, err = saver.Exists(ctx, longID)
	require.NoError(t, err)
	assert.True(t, exists)
}

// TestInMemorySaver_StateModificationIsolation tests that modifying history doesn't affect stored state
func TestInMemorySaver_StateModificationIsolation(t *testing.T) {
	saver := NewInMemorySaver()
	ctx := context.Background()
	threadID := "isolation-test"

	// Save initial state
	state1 := agentstate.NewAgentState()
	state1.Set("key", "value1")
	err := saver.Save(ctx, threadID, state1)
	require.NoError(t, err)

	// Get history
	history1, err := saver.GetHistory(ctx, threadID)
	require.NoError(t, err)
	assert.Equal(t, 0, len(history1))

	// Update to new state
	state2 := agentstate.NewAgentState()
	state2.Set("key", "value2")
	err = saver.Save(ctx, threadID, state2)
	require.NoError(t, err)

	// Get history
	history2, err := saver.GetHistory(ctx, threadID)
	require.NoError(t, err)
	assert.Equal(t, 1, len(history2))

	// Modify the history we got
	history2[0].Set("key", "modified")

	// Load and verify original is still intact
	loaded, err := saver.Load(ctx, threadID)
	require.NoError(t, err)
	val, ok := loaded.Get("key")
	assert.True(t, ok)
	assert.Equal(t, "value2", val) // Should still be value2

	// Get history again and verify it's unchanged
	history3, err := saver.GetHistory(ctx, threadID)
	require.NoError(t, err)
	val, ok = history3[0].Get("key")
	assert.True(t, ok)
	assert.Equal(t, "value1", val) // Should still be value1
}
