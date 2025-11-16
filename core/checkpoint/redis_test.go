package checkpoint

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	agentstate "github.com/kart-io/goagent/core/state"
)

func setupTestRedisCheckpointer(t *testing.T) (*RedisCheckpointer, *miniredis.Miniredis) {
	t.Helper()
	// Create a miniredis server
	mr := miniredis.RunT(t)

	// Create config
	config := &RedisCheckpointerConfig{
		Addr:         mr.Addr(),
		Password:     "",
		DB:           0,
		Prefix:       "test:checkpoint:",
		TTL:          0,
		PoolSize:     10,
		MinIdleConns: 2,
		MaxRetries:   3,
		DialTimeout:  5 * time.Second,
		ReadTimeout:  3 * time.Second,
		WriteTimeout: 3 * time.Second,
		EnableLock:   true,
		LockTimeout:  2 * time.Second,
		LockExpiry:   5 * time.Second,
	}

	// Create checkpointer
	cp, err := NewRedisCheckpointer(config)
	require.NoError(t, err)
	require.NotNil(t, cp)

	return cp, mr
}

func TestNewRedisCheckpointer(t *testing.T) {
	cp, mr := setupTestRedisCheckpointer(t)
	defer mr.Close()
	defer cp.Close()

	assert.NotNil(t, cp)
	assert.NotNil(t, cp.client)
	assert.NotNil(t, cp.config)
}

func TestNewRedisCheckpointer_ConnectionFailure(t *testing.T) {
	config := &RedisCheckpointerConfig{
		Addr:        "localhost:9999", // Non-existent server
		DialTimeout: 1 * time.Second,
	}

	cp, err := NewRedisCheckpointer(config)
	assert.Error(t, err)
	assert.Nil(t, cp)
}

func TestRedisCheckpointer_Save(t *testing.T) {
	cp, mr := setupTestRedisCheckpointer(t)
	defer mr.Close()
	defer cp.Close()

	ctx := context.Background()
	threadID := "thread-1"

	state := agentstate.NewAgentState()
	state.Set("messages", []string{"hello", "world"})
	state.Set("count", 42)

	err := cp.Save(ctx, threadID, state)
	assert.NoError(t, err)

	// Verify the checkpoint was saved
	exists, err := cp.Exists(ctx, threadID)
	require.NoError(t, err)
	assert.True(t, exists)
}

func TestRedisCheckpointer_Save_Update(t *testing.T) {
	cp, mr := setupTestRedisCheckpointer(t)
	defer mr.Close()
	defer cp.Close()

	ctx := context.Background()
	threadID := "thread-1"

	// First save
	state1 := agentstate.NewAgentState()
	state1.Set("value", "initial")
	err := cp.Save(ctx, threadID, state1)
	require.NoError(t, err)

	// Get info
	info1, err := cp.getCheckpointInfo(ctx, threadID)
	require.NoError(t, err)
	created := info1.CreatedAt

	time.Sleep(10 * time.Millisecond)

	// Update
	state2 := agentstate.NewAgentState()
	state2.Set("value", "updated")
	err = cp.Save(ctx, threadID, state2)
	require.NoError(t, err)

	// Get updated info
	info2, err := cp.getCheckpointInfo(ctx, threadID)
	require.NoError(t, err)

	// Created should remain the same
	assert.Equal(t, created.Unix(), info2.CreatedAt.Unix())
	// Updated should be newer
	assert.True(t, info2.UpdatedAt.After(info1.UpdatedAt))
}

func TestRedisCheckpointer_Load(t *testing.T) {
	cp, mr := setupTestRedisCheckpointer(t)
	defer mr.Close()
	defer cp.Close()

	ctx := context.Background()
	threadID := "thread-1"

	// Save a state
	state := agentstate.NewAgentState()
	state.Set("key1", "value1")
	state.Set("key2", 123)

	err := cp.Save(ctx, threadID, state)
	require.NoError(t, err)

	// Load the state
	loaded, err := cp.Load(ctx, threadID)
	require.NoError(t, err)

	// Verify values
	val1, ok := loaded.Get("key1")
	assert.True(t, ok)
	assert.Equal(t, "value1", val1)

	val2, ok := loaded.Get("key2")
	assert.True(t, ok)
	// JSON unmarshaling converts numbers to float64
	assert.Equal(t, float64(123), val2)
}

func TestRedisCheckpointer_Load_NotFound(t *testing.T) {
	cp, mr := setupTestRedisCheckpointer(t)
	defer mr.Close()
	defer cp.Close()

	ctx := context.Background()
	threadID := "nonexistent"

	_, err := cp.Load(ctx, threadID)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

func TestRedisCheckpointer_Delete(t *testing.T) {
	cp, mr := setupTestRedisCheckpointer(t)
	defer mr.Close()
	defer cp.Close()

	ctx := context.Background()
	threadID := "thread-1"

	// Save a state
	state := agentstate.NewAgentState()
	state.Set("test", "value")
	err := cp.Save(ctx, threadID, state)
	require.NoError(t, err)

	// Delete the checkpoint
	err = cp.Delete(ctx, threadID)
	assert.NoError(t, err)

	// Verify it's gone
	exists, err := cp.Exists(ctx, threadID)
	require.NoError(t, err)
	assert.False(t, exists)
}

func TestRedisCheckpointer_Exists(t *testing.T) {
	cp, mr := setupTestRedisCheckpointer(t)
	defer mr.Close()
	defer cp.Close()

	ctx := context.Background()
	threadID := "thread-1"

	// Should not exist initially
	exists, err := cp.Exists(ctx, threadID)
	require.NoError(t, err)
	assert.False(t, exists)

	// Save a checkpoint
	state := agentstate.NewAgentState()
	err = cp.Save(ctx, threadID, state)
	require.NoError(t, err)

	// Should exist now
	exists, err = cp.Exists(ctx, threadID)
	require.NoError(t, err)
	assert.True(t, exists)
}

func TestRedisCheckpointer_List(t *testing.T) {
	cp, mr := setupTestRedisCheckpointer(t)
	defer mr.Close()
	defer cp.Close()

	ctx := context.Background()

	// Save multiple checkpoints
	threads := []string{"thread-1", "thread-2", "thread-3"}
	for _, threadID := range threads {
		state := agentstate.NewAgentState()
		state.Set("thread", threadID)
		err := cp.Save(ctx, threadID, state)
		require.NoError(t, err)
	}

	// List checkpoints
	infos, err := cp.List(ctx)
	require.NoError(t, err)
	assert.Len(t, infos, 3)

	// Verify thread IDs
	foundThreads := make(map[string]bool)
	for _, info := range infos {
		foundThreads[info.ThreadID] = true
	}

	for _, threadID := range threads {
		assert.True(t, foundThreads[threadID], "Thread %s not found", threadID)
	}
}

func TestRedisCheckpointer_WithTTL(t *testing.T) {
	mr := miniredis.RunT(t)
	defer mr.Close()

	config := &RedisCheckpointerConfig{
		Addr:       mr.Addr(),
		Prefix:     "test:ttl:",
		TTL:        100 * time.Millisecond,
		EnableLock: false, // Disable lock for this test
	}

	cp, err := NewRedisCheckpointer(config)
	require.NoError(t, err)
	defer cp.Close()

	ctx := context.Background()
	threadID := "expiring"

	// Save checkpoint with TTL
	state := agentstate.NewAgentState()
	state.Set("test", "value")
	err = cp.Save(ctx, threadID, state)
	require.NoError(t, err)

	// Should exist
	exists, err := cp.Exists(ctx, threadID)
	require.NoError(t, err)
	assert.True(t, exists)

	// Fast forward time
	mr.FastForward(200 * time.Millisecond)

	// Should be expired
	exists, err = cp.Exists(ctx, threadID)
	require.NoError(t, err)
	assert.False(t, exists)
}

func TestRedisCheckpointer_Ping(t *testing.T) {
	cp, mr := setupTestRedisCheckpointer(t)
	defer mr.Close()
	defer cp.Close()

	ctx := context.Background()
	err := cp.Ping(ctx)
	assert.NoError(t, err)
}

func TestRedisCheckpointer_Size(t *testing.T) {
	cp, mr := setupTestRedisCheckpointer(t)
	defer mr.Close()
	defer cp.Close()

	ctx := context.Background()

	// Save checkpoints
	for i := 0; i < 5; i++ {
		state := agentstate.NewAgentState()
		err := cp.Save(ctx, fmt.Sprintf("thread-%d", i), state)
		require.NoError(t, err)
	}

	size, err := cp.Size(ctx)
	require.NoError(t, err)
	assert.Equal(t, 5, size)
}

func TestRedisCheckpointer_CleanupOld(t *testing.T) {
	cp, mr := setupTestRedisCheckpointer(t)
	defer mr.Close()
	defer cp.Close()

	ctx := context.Background()

	// Save checkpoints
	for i := 0; i < 3; i++ {
		state := agentstate.NewAgentState()
		err := cp.Save(ctx, fmt.Sprintf("thread-%d", i), state)
		require.NoError(t, err)
	}

	// Manually manipulate the checkpoint data to simulate old checkpoints
	// Since miniredis FastForward doesn't affect stored data, we'll modify the data
	for i := 0; i < 3; i++ {
		threadID := fmt.Sprintf("thread-%d", i)
		key := cp.makeKey(threadID)

		// Get existing data
		data, _ := cp.client.Get(ctx, key).Bytes()
		var cpData checkpointData
		json.Unmarshal(data, &cpData)

		// Set old timestamps
		cpData.UpdatedAt = time.Now().Add(-25 * time.Hour)
		cpData.CreatedAt = time.Now().Add(-30 * time.Hour)

		// Save back
		newData, _ := json.Marshal(cpData)
		cp.client.Set(ctx, key, newData, 0)
	}

	// Cleanup old checkpoints (older than 24 hours)
	removed, err := cp.CleanupOld(ctx, 24*time.Hour)
	require.NoError(t, err)
	assert.Equal(t, 3, removed)

	// Verify they're gone
	size, err := cp.Size(ctx)
	require.NoError(t, err)
	assert.Equal(t, 0, size)
}

func TestRedisCheckpointer_NewFromClient(t *testing.T) {
	mr := miniredis.RunT(t)
	defer mr.Close()

	client := redis.NewClient(&redis.Options{
		Addr: mr.Addr(),
	})

	config := &RedisCheckpointerConfig{
		Prefix:     "test:",
		EnableLock: false,
	}

	cp := NewRedisCheckpointerFromClient(client, config)
	defer cp.Close()

	assert.NotNil(t, cp)
	assert.Equal(t, client, cp.client)
}

func TestRedisCheckpointer_ConcurrentAccess(t *testing.T) {
	cp, mr := setupTestRedisCheckpointer(t)
	defer mr.Close()
	defer cp.Close()

	ctx := context.Background()
	threadID := "concurrent-thread"

	// Save initial state
	state := agentstate.NewAgentState()
	state.Set("counter", 0)
	err := cp.Save(ctx, threadID, state)
	require.NoError(t, err)

	// Concurrent saves (lock should prevent race conditions)
	const goroutines = 10
	done := make(chan bool, goroutines)

	for i := 0; i < goroutines; i++ {
		go func(n int) {
			s := agentstate.NewAgentState()
			s.Set("counter", n)
			cp.Save(ctx, threadID, s)
			done <- true
		}(i)
	}

	// Wait for all goroutines
	for i := 0; i < goroutines; i++ {
		<-done
	}

	// Should be able to load without error
	_, err = cp.Load(ctx, threadID)
	assert.NoError(t, err)
}

func TestRedisCheckpointer_LockAcquisition(t *testing.T) {
	cp, mr := setupTestRedisCheckpointer(t)
	defer mr.Close()
	defer cp.Close()

	ctx := context.Background()
	threadID := "lock-test"

	// Acquire lock
	err := cp.acquireLock(ctx, threadID)
	assert.NoError(t, err)

	// Try to acquire again (should timeout)
	ctx2, cancel := context.WithTimeout(ctx, 100*time.Millisecond)
	defer cancel()

	err = cp.acquireLock(ctx2, threadID)
	assert.Error(t, err)
	// Error could be either "timeout" or "context deadline exceeded"

	// Release lock
	err = cp.releaseLock(ctx, threadID)
	assert.NoError(t, err)

	// Should be able to acquire again
	err = cp.acquireLock(ctx, threadID)
	assert.NoError(t, err)
}
