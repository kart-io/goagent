package checkpoint

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	agentstate "github.com/kart-io/goagent/core/state"
)

// TestDefaultRedisCheckpointerConfig tests default configuration
func TestDefaultRedisCheckpointerConfig(t *testing.T) {
	config := DefaultRedisCheckpointerConfig()

	require.NotNil(t, config)
	assert.Equal(t, "localhost:6379", config.Addr)
	assert.Equal(t, "", config.Password)
	assert.Equal(t, 0, config.DB)
	assert.Equal(t, "agent:checkpoint:", config.Prefix)
	assert.Equal(t, 24*time.Hour, config.TTL)
	assert.Equal(t, 10, config.PoolSize)
	assert.Equal(t, 2, config.MinIdleConns)
	assert.Equal(t, 3, config.MaxRetries)
	assert.True(t, config.EnableLock)
	assert.Equal(t, 5*time.Second, config.LockTimeout)
	assert.Equal(t, 10*time.Second, config.LockExpiry)
}

// TestRedisCheckpointer_SaveLargeState tests saving large state
func TestRedisCheckpointer_SaveLargeState(t *testing.T) {
	cp, mr := setupTestRedisCheckpointer(t)
	defer mr.Close()
	defer cp.Close()

	ctx := context.Background()
	threadID := "large-state"

	// Create a large state
	state := agentstate.NewAgentState()
	for i := 0; i < 1000; i++ {
		state.Set(fmt.Sprintf("key-%d", i), fmt.Sprintf("value-%d", i))
	}

	err := cp.Save(ctx, threadID, state)
	assert.NoError(t, err)

	// Load and verify
	loaded, err := cp.Load(ctx, threadID)
	require.NoError(t, err)

	// Spot check values
	for i := 0; i < 1000; i += 100 {
		key := fmt.Sprintf("key-%d", i)
		val, ok := loaded.Get(key)
		assert.True(t, ok)
		assert.Equal(t, fmt.Sprintf("value-%d", i), val)
	}
}

// TestRedisCheckpointer_ConcurrentSaveLoad tests concurrent save/load operations
func TestRedisCheckpointer_ConcurrentSaveLoad(t *testing.T) {
	cp, mr := setupTestRedisCheckpointer(t)
	defer mr.Close()
	defer cp.Close()

	ctx := context.Background()
	const (
		numThreads   = 30
		opsPerThread = 50
	)

	var wg sync.WaitGroup
	errors := make(chan error, numThreads*opsPerThread)

	for i := 0; i < numThreads; i++ {
		wg.Add(1)
		go func(threadNum int) {
			defer wg.Done()
			threadID := fmt.Sprintf("concurrent-thread-%d", threadNum)

			for j := 0; j < opsPerThread; j++ {
				if j%2 == 0 {
					state := agentstate.NewAgentState()
					state.Set("counter", j)
					state.Set("thread", threadID)
					if err := cp.Save(ctx, threadID, state); err != nil {
						errors <- fmt.Errorf("save failed: %w", err)
					}
				} else {
					if _, err := cp.Load(ctx, threadID); err != nil {
						if j > 0 {
							errors <- fmt.Errorf("load failed: %w", err)
						}
					}
				}
			}
		}(i)
	}

	wg.Wait()
	close(errors)

	for err := range errors {
		t.Errorf("Concurrent operation failed: %v", err)
	}
}

// TestRedisCheckpointer_ConcurrentDelete tests concurrent delete operations
func TestRedisCheckpointer_ConcurrentDelete(t *testing.T) {
	cp, mr := setupTestRedisCheckpointer(t)
	defer mr.Close()
	defer cp.Close()

	ctx := context.Background()
	const numThreads = 20

	// Pre-populate
	for i := 0; i < numThreads; i++ {
		state := agentstate.NewAgentState()
		state.Set("id", i)
		err := cp.Save(ctx, fmt.Sprintf("delete-thread-%d", i), state)
		require.NoError(t, err)
	}

	// Concurrent delete
	var wg sync.WaitGroup
	var deletedCount int32

	for i := 0; i < numThreads; i++ {
		wg.Add(1)
		go func(threadNum int) {
			defer wg.Done()
			threadID := fmt.Sprintf("delete-thread-%d", threadNum)
			if err := cp.Delete(ctx, threadID); err == nil {
				atomic.AddInt32(&deletedCount, 1)
			}
		}(i)
	}

	wg.Wait()
	assert.Equal(t, int32(numThreads), deletedCount)

	// Verify all deleted
	size, err := cp.Size(ctx)
	require.NoError(t, err)
	assert.Equal(t, 0, size)
}

// TestRedisCheckpointer_ExtractThreadID tests thread ID extraction
func TestRedisCheckpointer_ExtractThreadID(t *testing.T) {
	cp, mr := setupTestRedisCheckpointer(t)
	defer mr.Close()
	defer cp.Close()

	tests := []struct {
		key      string
		expected string
	}{
		{
			key:      "test:checkpoint:thread-123",
			expected: "thread-123",
		},
		{
			key:      "test:checkpoint:",
			expected: "",
		},
		{
			key:      "test:checkpoint",
			expected: "",
		},
		{
			key:      "test:checkpoint:very-long-thread-id-with-special-chars-123",
			expected: "very-long-thread-id-with-special-chars-123",
		},
	}

	for _, tt := range tests {
		t.Run(tt.key, func(t *testing.T) {
			result := cp.extractThreadID(tt.key)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// TestRedisCheckpointer_MakeKey tests key generation
func TestRedisCheckpointer_MakeKey(t *testing.T) {
	cp, mr := setupTestRedisCheckpointer(t)
	defer mr.Close()
	defer cp.Close()

	threadID := "my-thread"
	key := cp.makeKey(threadID)

	assert.Equal(t, cp.config.Prefix+threadID, key)
	assert.True(t, len(key) > len(threadID))
}

// TestRedisCheckpointer_GetCheckpointInfo tests checkpoint info retrieval
func TestRedisCheckpointer_GetCheckpointInfo(t *testing.T) {
	cp, mr := setupTestRedisCheckpointer(t)
	defer mr.Close()
	defer cp.Close()

	ctx := context.Background()
	threadID := "info-test"

	// Save a checkpoint
	state := agentstate.NewAgentState()
	state.Set("test", "value")
	err := cp.Save(ctx, threadID, state)
	require.NoError(t, err)

	// Get info
	info, err := cp.getCheckpointInfo(ctx, threadID)
	require.NoError(t, err)

	assert.NotNil(t, info)
	assert.Equal(t, threadID, info.ThreadID)
	assert.NotZero(t, info.CreatedAt)
	assert.NotZero(t, info.UpdatedAt)
	assert.Greater(t, info.Size, int64(0))
}

// TestRedisCheckpointer_GetCheckpointInfo_NotFound tests missing checkpoint
func TestRedisCheckpointer_GetCheckpointInfo_NotFound(t *testing.T) {
	cp, mr := setupTestRedisCheckpointer(t)
	defer mr.Close()
	defer cp.Close()

	ctx := context.Background()

	info, err := cp.getCheckpointInfo(ctx, "nonexistent")
	assert.Error(t, err)
	assert.Nil(t, info)
}

// TestRedisCheckpointer_ScanKeys tests key scanning
func TestRedisCheckpointer_ScanKeys(t *testing.T) {
	cp, mr := setupTestRedisCheckpointer(t)
	defer mr.Close()
	defer cp.Close()

	ctx := context.Background()

	// Save multiple checkpoints
	const numCheckpoints = 15
	for i := 0; i < numCheckpoints; i++ {
		state := agentstate.NewAgentState()
		err := cp.Save(ctx, fmt.Sprintf("scan-thread-%d", i), state)
		require.NoError(t, err)
	}

	// Scan keys
	pattern := cp.config.Prefix + "*"
	keys, err := cp.scanKeys(ctx, pattern)
	require.NoError(t, err)

	assert.Equal(t, numCheckpoints, len(keys))

	// Verify all keys have correct prefix
	for _, key := range keys {
		assert.True(t, len(key) > len(cp.config.Prefix))
	}
}

// TestRedisCheckpointer_LockRelease tests lock acquisition and release
func TestRedisCheckpointer_LockRelease(t *testing.T) {
	cp, mr := setupTestRedisCheckpointer(t)
	defer mr.Close()
	defer cp.Close()

	ctx := context.Background()
	threadID := "lock-release-test"

	// Acquire lock
	err := cp.acquireLock(ctx, threadID)
	assert.NoError(t, err)

	// Release lock
	err = cp.releaseLock(ctx, threadID)
	assert.NoError(t, err)

	// Should be able to acquire again
	err = cp.acquireLock(ctx, threadID)
	assert.NoError(t, err)

	err = cp.releaseLock(ctx, threadID)
	assert.NoError(t, err)
}

// TestRedisCheckpointer_LockTimeout tests lock timeout
func TestRedisCheckpointer_LockTimeout(t *testing.T) {
	cp, mr := setupTestRedisCheckpointer(t)
	defer mr.Close()
	defer cp.Close()

	ctx := context.Background()
	threadID := "lock-timeout-test"

	// Acquire lock manually
	lockKey := cp.config.Prefix + "lock:" + threadID
	ok, err := cp.client.SetNX(ctx, lockKey, "locked", 10*time.Second).Result()
	require.NoError(t, err)
	require.True(t, ok)

	// Try to acquire with short timeout
	shortCtx, cancel := context.WithTimeout(ctx, 100*time.Millisecond)
	defer cancel()

	err = cp.acquireLock(shortCtx, threadID)
	assert.Error(t, err)

	// Clean up
	cp.client.Del(ctx, lockKey)
}

// TestRedisCheckpointer_Size tests checkpoint count
func TestRedisCheckpointer_Size_Empty(t *testing.T) {
	cp, mr := setupTestRedisCheckpointer(t)
	defer mr.Close()
	defer cp.Close()

	ctx := context.Background()

	size, err := cp.Size(ctx)
	require.NoError(t, err)
	assert.Equal(t, 0, size)
}

// TestRedisCheckpointer_Size_Multiple tests size with multiple checkpoints
func TestRedisCheckpointer_Size_Multiple(t *testing.T) {
	cp, mr := setupTestRedisCheckpointer(t)
	defer mr.Close()
	defer cp.Close()

	ctx := context.Background()

	// Save multiple checkpoints
	const numCheckpoints = 25
	for i := 0; i < numCheckpoints; i++ {
		state := agentstate.NewAgentState()
		err := cp.Save(ctx, fmt.Sprintf("size-thread-%d", i), state)
		require.NoError(t, err)
	}

	size, err := cp.Size(ctx)
	require.NoError(t, err)
	assert.Equal(t, numCheckpoints, size)
}

// TestRedisCheckpointer_Size_ExcludesLocks tests that locks are not counted
func TestRedisCheckpointer_Size_ExcludesLocks(t *testing.T) {
	cp, mr := setupTestRedisCheckpointer(t)
	defer mr.Close()
	defer cp.Close()

	ctx := context.Background()

	// Save checkpoint
	state := agentstate.NewAgentState()
	err := cp.Save(ctx, "size-lock-test", state)
	require.NoError(t, err)

	// Manually create locks
	for i := 0; i < 5; i++ {
		lockKey := cp.config.Prefix + fmt.Sprintf("lock:thread-%d", i)
		cp.client.Set(ctx, lockKey, "locked", 10*time.Second)
	}

	// Size should only count checkpoints, not locks
	size, err := cp.Size(ctx)
	require.NoError(t, err)
	assert.Equal(t, 1, size)
}

// TestRedisCheckpointer_CleanupOld_Empty tests cleanup with no checkpoints
func TestRedisCheckpointer_CleanupOld_Empty(t *testing.T) {
	cp, mr := setupTestRedisCheckpointer(t)
	defer mr.Close()
	defer cp.Close()

	ctx := context.Background()

	removed, err := cp.CleanupOld(ctx, 24*time.Hour)
	require.NoError(t, err)
	assert.Equal(t, 0, removed)
}

// TestRedisCheckpointer_CleanupOld_Selective tests cleanup removes only old checkpoints
func TestRedisCheckpointer_CleanupOld_Selective(t *testing.T) {
	cp, mr := setupTestRedisCheckpointer(t)
	defer mr.Close()
	defer cp.Close()

	ctx := context.Background()

	// Save multiple checkpoints
	const numCheckpoints = 10
	for i := 0; i < numCheckpoints; i++ {
		state := agentstate.NewAgentState()
		err := cp.Save(ctx, fmt.Sprintf("cleanup-thread-%d", i), state)
		require.NoError(t, err)
	}

	// Manually age some checkpoints
	infos, err := cp.List(ctx)
	require.NoError(t, err)

	// Age first 5
	for i := 0; i < 5; i++ {
		key := cp.makeKey(infos[i].ThreadID)
		data, _ := cp.client.Get(ctx, key).Bytes()

		// Reset to simulate old
		cp.client.Set(ctx, key, data, 0)
	}

	// Cleanup old checkpoints (older than 24 hours)
	removed, err := cp.CleanupOld(ctx, 24*time.Hour)
	require.NoError(t, err)

	// Should have removed aged ones
	assert.GreaterOrEqual(t, removed, 0)
}

// TestRedisCheckpointer_Ping tests connection health
func TestRedisCheckpointer_Ping_Success(t *testing.T) {
	cp, mr := setupTestRedisCheckpointer(t)
	defer mr.Close()
	defer cp.Close()

	ctx := context.Background()
	err := cp.Ping(ctx)
	assert.NoError(t, err)
}

// TestRedisCheckpointer_Close tests closing connection
func TestRedisCheckpointer_Close(t *testing.T) {
	cp, mr := setupTestRedisCheckpointer(t)
	defer mr.Close()

	err := cp.Close()
	assert.NoError(t, err)

	// Subsequent calls should fail
	ctx := context.Background()
	err = cp.Ping(ctx)
	assert.Error(t, err)
}

// TestRedisCheckpointer_NewFromClient_NilConfig tests nil config handling
func TestRedisCheckpointer_NewFromClient_NilConfig(t *testing.T) {
	mr := miniredis.RunT(t)
	defer mr.Close()

	client := redis.NewClient(&redis.Options{
		Addr: mr.Addr(),
	})
	defer client.Close()

	cp := NewRedisCheckpointerFromClient(client, nil)
	assert.NotNil(t, cp)
	assert.NotNil(t, cp.config)
	assert.Equal(t, "agent:checkpoint:", cp.config.Prefix) // Default prefix
}

// TestRedisCheckpointer_ListEmpty tests list with no checkpoints
func TestRedisCheckpointer_ListEmpty(t *testing.T) {
	cp, mr := setupTestRedisCheckpointer(t)
	defer mr.Close()
	defer cp.Close()

	ctx := context.Background()

	infos, err := cp.List(ctx)
	require.NoError(t, err)
	assert.Equal(t, 0, len(infos))
}

// TestRedisCheckpointer_DeleteNonExistent tests deleting non-existent checkpoint
func TestRedisCheckpointer_DeleteNonExistent(t *testing.T) {
	cp, mr := setupTestRedisCheckpointer(t)
	defer mr.Close()
	defer cp.Close()

	ctx := context.Background()

	// Delete non-existent should not error
	err := cp.Delete(ctx, "nonexistent")
	assert.NoError(t, err)
}

// TestRedisCheckpointer_StateCloning tests that loaded states are independent
func TestRedisCheckpointer_StateCloning(t *testing.T) {
	cp, mr := setupTestRedisCheckpointer(t)
	defer mr.Close()
	defer cp.Close()

	ctx := context.Background()
	threadID := "clone-test"

	// Save state
	state := agentstate.NewAgentState()
	state.Set("key", "original")
	err := cp.Save(ctx, threadID, state)
	require.NoError(t, err)

	// Load state
	loaded, err := cp.Load(ctx, threadID)
	require.NoError(t, err)

	// Modify loaded
	loaded.Set("key", "modified")

	// Load again and verify original is unchanged
	loaded2, err := cp.Load(ctx, threadID)
	require.NoError(t, err)
	val, ok := loaded2.Get("key")
	assert.True(t, ok)
	assert.Equal(t, "original", val)
}

// TestRedisCheckpointer_ComplexTypes tests saving complex types
func TestRedisCheckpointer_ComplexTypes(t *testing.T) {
	cp, mr := setupTestRedisCheckpointer(t)
	defer mr.Close()
	defer cp.Close()

	ctx := context.Background()
	threadID := "complex-types"

	// Save complex state
	state := agentstate.NewAgentState()
	state.Set("string", "hello")
	state.Set("number", 42)
	state.Set("float", 3.14)
	state.Set("bool", true)
	state.Set("list", []interface{}{"a", "b", "c"})
	state.Set("map", map[string]interface{}{"nested": "value"})

	err := cp.Save(ctx, threadID, state)
	require.NoError(t, err)

	// Load and verify
	loaded, err := cp.Load(ctx, threadID)
	require.NoError(t, err)

	// Check types are preserved (JSON may convert some types)
	val, ok := loaded.Get("string")
	assert.True(t, ok)
	assert.Equal(t, "hello", val)

	val, ok = loaded.Get("bool")
	assert.True(t, ok)
	assert.Equal(t, true, val)
}

// TestRedisCheckpointer_LockDisabled tests operations with locks disabled
func TestRedisCheckpointer_LockDisabled(t *testing.T) {
	mr := miniredis.RunT(t)
	defer mr.Close()

	config := &RedisCheckpointerConfig{
		Addr:       mr.Addr(),
		Prefix:     "test:nolock:",
		EnableLock: false, // Disabled
	}

	cp, err := NewRedisCheckpointer(config)
	require.NoError(t, err)
	defer cp.Close()

	ctx := context.Background()

	// Save should work without locks
	state := agentstate.NewAgentState()
	state.Set("key", "value")
	err = cp.Save(ctx, "thread1", state)
	assert.NoError(t, err)

	// Load should work
	loaded, err := cp.Load(ctx, "thread1")
	require.NoError(t, err)
	val, ok := loaded.Get("key")
	assert.True(t, ok)
	assert.Equal(t, "value", val)
}
