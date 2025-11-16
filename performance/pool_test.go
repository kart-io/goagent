package performance

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/kart-io/goagent/core"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestNewAgentPool tests pool creation
func TestNewAgentPool(t *testing.T) {
	factory := func() (core.Agent, error) {
		return NewMockAgent("test", 10*time.Millisecond), nil
	}
	config := DefaultPoolConfig()

	pool, err := NewAgentPool(factory, config)
	require.NoError(t, err)
	require.NotNil(t, pool)
	defer pool.Close()

	stats := pool.Stats()
	assert.Equal(t, 5, stats.TotalCount) // InitialSize = 5
	assert.Equal(t, 5, stats.IdleCount)  // All idle initially
	assert.Equal(t, 0, stats.ActiveCount)
}

// TestNewAgentPool_InvalidFactory tests pool creation with nil factory
func TestNewAgentPool_InvalidFactory(t *testing.T) {
	config := DefaultPoolConfig()
	pool, err := NewAgentPool(nil, config)

	assert.Error(t, err)
	assert.Nil(t, pool)
}

// TestNewAgentPool_FactoryError tests pool creation when factory fails
func TestNewAgentPool_FactoryError(t *testing.T) {
	factory := func() (core.Agent, error) {
		return nil, errors.New("factory error")
	}
	config := PoolConfig{
		InitialSize: 5,
		MaxSize:     10,
	}

	pool, err := NewAgentPool(factory, config)

	assert.Error(t, err)
	assert.Nil(t, pool)
}

// TestDefaultPoolConfig tests default config
func TestDefaultPoolConfig(t *testing.T) {
	config := DefaultPoolConfig()

	assert.Equal(t, 5, config.InitialSize)
	assert.Equal(t, 50, config.MaxSize)
	assert.Equal(t, 5*time.Minute, config.IdleTimeout)
	assert.Equal(t, 30*time.Minute, config.MaxLifetime)
	assert.Equal(t, 10*time.Second, config.AcquireTimeout)
	assert.Equal(t, 1*time.Minute, config.CleanupInterval)
}

// TestAgentPool_AcquireAndRelease tests acquiring and releasing agents
func TestAgentPool_AcquireAndRelease(t *testing.T) {
	factory := func() (core.Agent, error) {
		return NewMockAgent("test", 10*time.Millisecond), nil
	}
	config := DefaultPoolConfig()

	pool, err := NewAgentPool(factory, config)
	require.NoError(t, err)
	defer pool.Close()

	ctx := context.Background()

	// Acquire an agent
	agent, err := pool.Acquire(ctx)
	require.NoError(t, err)
	require.NotNil(t, agent)

	stats := pool.Stats()
	assert.Equal(t, 1, stats.ActiveCount)
	assert.Equal(t, 4, stats.IdleCount)

	// Release the agent
	err = pool.Release(agent)
	require.NoError(t, err)

	stats = pool.Stats()
	assert.Equal(t, 0, stats.ActiveCount)
	assert.Equal(t, 5, stats.IdleCount)
}

// TestAgentPool_AcquireTimeout tests acquire timeout
func TestAgentPool_AcquireTimeout(t *testing.T) {
	factory := func() (core.Agent, error) {
		return NewMockAgent("test", 500*time.Millisecond), nil
	}
	config := PoolConfig{
		InitialSize:    1,
		MaxSize:        1,
		AcquireTimeout: 100 * time.Millisecond,
		IdleTimeout:    5 * time.Minute,
		MaxLifetime:    30 * time.Minute,
	}

	pool, err := NewAgentPool(factory, config)
	require.NoError(t, err)
	defer pool.Close()

	ctx := context.Background()

	// Acquire the only agent
	agent1, err := pool.Acquire(ctx)
	require.NoError(t, err)
	require.NotNil(t, agent1)

	// Try to acquire another (should timeout)
	timeoutCtx, cancel := context.WithTimeout(ctx, 100*time.Millisecond)
	defer cancel()

	agent2, err := pool.Acquire(timeoutCtx)
	assert.Error(t, err)
	assert.Nil(t, agent2)
	assert.Equal(t, ErrPoolTimeout, err)

	// Clean up
	pool.Release(agent1)
}

// TestAgentPool_ExecuteConvenience tests Execute convenience method
func TestAgentPool_ExecuteConvenience(t *testing.T) {
	factory := func() (core.Agent, error) {
		return NewMockAgent("test", 5*time.Millisecond), nil
	}
	config := DefaultPoolConfig()

	pool, err := NewAgentPool(factory, config)
	require.NoError(t, err)
	defer pool.Close()

	ctx := context.Background()
	input := &core.AgentInput{
		Task:        "Test task",
		Instruction: "Test",
		Timestamp:   time.Now(),
	}

	output, err := pool.Execute(ctx, input)
	require.NoError(t, err)
	require.NotNil(t, output)
	assert.Equal(t, "success", output.Status)
}

// TestAgentPool_ConcurrentAcquire tests concurrent acquisition
func TestAgentPool_ConcurrentAcquire(t *testing.T) {
	factory := func() (core.Agent, error) {
		return NewMockAgent("test", 10*time.Millisecond), nil
	}
	config := PoolConfig{
		InitialSize:    5,
		MaxSize:        20,
		IdleTimeout:    5 * time.Minute,
		MaxLifetime:    30 * time.Minute,
		AcquireTimeout: 10 * time.Second,
	}

	pool, err := NewAgentPool(factory, config)
	require.NoError(t, err)
	defer pool.Close()

	ctx := context.Background()
	var wg sync.WaitGroup
	acquiredAgents := make([]core.Agent, 10)
	var mu sync.Mutex

	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			agent, err := pool.Acquire(ctx)
			require.NoError(t, err)
			require.NotNil(t, agent)

			mu.Lock()
			acquiredAgents[idx] = agent
			mu.Unlock()

			time.Sleep(50 * time.Millisecond)
		}(i)
	}

	wg.Wait()

	// Release all agents
	for _, agent := range acquiredAgents {
		if agent != nil {
			pool.Release(agent)
		}
	}

	stats := pool.Stats()
	assert.Equal(t, 10, stats.IdleCount)
	assert.Equal(t, 0, stats.ActiveCount)
}

// TestAgentPool_ReleaseNotInUse tests releasing agent not in use
func TestAgentPool_ReleaseNotInUse(t *testing.T) {
	factory := func() (core.Agent, error) {
		return NewMockAgent("test", 10*time.Millisecond), nil
	}
	config := DefaultPoolConfig()

	pool, err := NewAgentPool(factory, config)
	require.NoError(t, err)
	defer pool.Close()

	ctx := context.Background()

	agent, err := pool.Acquire(ctx)
	require.NoError(t, err)

	// Release it once
	err = pool.Release(agent)
	require.NoError(t, err)

	// Try to release again (should fail)
	err = pool.Release(agent)
	assert.Error(t, err)
	assert.Equal(t, "agent is not in use", err.Error())
}

// TestAgentPool_ReleaseUnknownAgent tests releasing unknown agent
func TestAgentPool_ReleaseUnknownAgent(t *testing.T) {
	factory := func() (core.Agent, error) {
		return NewMockAgent("test", 10*time.Millisecond), nil
	}
	config := DefaultPoolConfig()

	pool, err := NewAgentPool(factory, config)
	require.NoError(t, err)
	defer pool.Close()

	unknownAgent := NewMockAgent("unknown", 10*time.Millisecond)
	err = pool.Release(unknownAgent)

	assert.Error(t, err)
	assert.Equal(t, "agent not found in pool", err.Error())
}

// TestAgentPool_Stats tests pool statistics
func TestAgentPool_Stats(t *testing.T) {
	factory := func() (core.Agent, error) {
		return NewMockAgent("test", 5*time.Millisecond), nil
	}
	config := DefaultPoolConfig()

	pool, err := NewAgentPool(factory, config)
	require.NoError(t, err)
	defer pool.Close()

	ctx := context.Background()

	// Acquire some agents
	agents := make([]core.Agent, 3)
	for i := 0; i < 3; i++ {
		agents[i], _ = pool.Acquire(ctx)
	}

	stats := pool.Stats()
	assert.Equal(t, 3, stats.ActiveCount)
	assert.Equal(t, 2, stats.IdleCount)
	assert.Equal(t, 5, stats.TotalCount)
	assert.Greater(t, stats.UtilizationPct, 0.0)
	assert.Equal(t, int64(3), stats.AcquiredTotal)

	// Release all
	for _, agent := range agents {
		pool.Release(agent)
	}
}

// TestAgentPool_PoolClosed tests operations on closed pool
func TestAgentPool_PoolClosed(t *testing.T) {
	factory := func() (core.Agent, error) {
		return NewMockAgent("test", 10*time.Millisecond), nil
	}
	config := DefaultPoolConfig()

	pool, err := NewAgentPool(factory, config)
	require.NoError(t, err)

	err = pool.Close()
	require.NoError(t, err)

	ctx := context.Background()

	// Try to acquire from closed pool
	_, err = pool.Acquire(ctx)
	assert.Error(t, err)
	assert.Equal(t, ErrPoolClosed, err)
}

// TestAgentPool_ClosablePool tests pool double close
func TestAgentPool_ClosablePool(t *testing.T) {
	factory := func() (core.Agent, error) {
		return NewMockAgent("test", 10*time.Millisecond), nil
	}
	config := DefaultPoolConfig()

	pool, err := NewAgentPool(factory, config)
	require.NoError(t, err)

	// Close multiple times should not error
	err = pool.Close()
	require.NoError(t, err)

	err = pool.Close()
	require.NoError(t, err)
}

// TestAgentPool_Cleanup tests cleanup of idle agents
func TestAgentPool_Cleanup(t *testing.T) {
	factory := func() (core.Agent, error) {
		return NewMockAgent("test", 5*time.Millisecond), nil
	}
	config := PoolConfig{
		InitialSize:     5,
		MaxSize:         20,
		IdleTimeout:     100 * time.Millisecond,
		MaxLifetime:     30 * time.Minute,
		AcquireTimeout:  10 * time.Second,
		CleanupInterval: 50 * time.Millisecond,
	}

	pool, err := NewAgentPool(factory, config)
	require.NoError(t, err)
	defer pool.Close()

	ctx := context.Background()

	// Acquire some agents and release them
	agent1, _ := pool.Acquire(ctx)
	agent2, _ := pool.Acquire(ctx)
	pool.Release(agent1)
	pool.Release(agent2)

	initialStats := pool.Stats()
	assert.Equal(t, 5, initialStats.TotalCount)

	// Wait for cleanup to run
	time.Sleep(200 * time.Millisecond)

	// Some agents should be cleaned up
	stats := pool.Stats()
	assert.Equal(t, 5, stats.TotalCount) // At least initial size should remain
}

// TestAgentPool_MaxSizeEnforcement tests max size enforcement
func TestAgentPool_MaxSizeEnforcement(t *testing.T) {
	creationCount := int32(0)
	factory := func() (core.Agent, error) {
		atomic.AddInt32(&creationCount, 1)
		return NewMockAgent("test", 10*time.Millisecond), nil
	}
	config := PoolConfig{
		InitialSize:    2,
		MaxSize:        5,
		IdleTimeout:    5 * time.Minute,
		MaxLifetime:    30 * time.Minute,
		AcquireTimeout: 100 * time.Millisecond,
	}

	pool, err := NewAgentPool(factory, config)
	require.NoError(t, err)
	defer pool.Close()

	ctx := context.Background()

	// Acquire agents up to max size
	agents := make([]core.Agent, 5)
	for i := 0; i < 5; i++ {
		var err error
		agents[i], err = pool.Acquire(ctx)
		require.NoError(t, err)
	}

	stats := pool.Stats()
	assert.Equal(t, 5, stats.TotalCount)
	assert.Equal(t, 5, stats.ActiveCount)

	// Clean up
	for _, agent := range agents {
		if agent != nil {
			pool.Release(agent)
		}
	}
}

// TestAgentPool_InvalidConfig tests invalid config handling
func TestAgentPool_InvalidConfig(t *testing.T) {
	factory := func() (core.Agent, error) {
		return NewMockAgent("test", 10*time.Millisecond), nil
	}

	testCases := []struct {
		name   string
		config PoolConfig
	}{
		{
			name: "NegativeInitialSize",
			config: PoolConfig{
				InitialSize:     -1,
				MaxSize:         10,
				IdleTimeout:     5 * time.Minute,
				MaxLifetime:     30 * time.Minute,
				AcquireTimeout:  10 * time.Second,
				CleanupInterval: 1 * time.Minute,
			},
		},
		{
			name: "ZeroMaxSize",
			config: PoolConfig{
				InitialSize:     5,
				MaxSize:         0,
				IdleTimeout:     5 * time.Minute,
				MaxLifetime:     30 * time.Minute,
				AcquireTimeout:  10 * time.Second,
				CleanupInterval: 1 * time.Minute,
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			pool, err := NewAgentPool(factory, tc.config)
			require.NoError(t, err)
			require.NotNil(t, pool)
			defer pool.Close()
		})
	}
}

// TestAgentPool_WaitCount tests wait counting
func TestAgentPool_WaitCount(t *testing.T) {
	factory := func() (core.Agent, error) {
		return NewMockAgent("test", 100*time.Millisecond), nil
	}
	config := PoolConfig{
		InitialSize:    1,
		MaxSize:        1,
		IdleTimeout:    5 * time.Minute,
		MaxLifetime:    30 * time.Minute,
		AcquireTimeout: 2 * time.Second,
	}

	pool, err := NewAgentPool(factory, config)
	require.NoError(t, err)
	defer pool.Close()

	ctx := context.Background()

	// Acquire the only agent
	agent, _ := pool.Acquire(ctx)

	// Acquire again in a goroutine (should wait)
	go func() {
		time.Sleep(50 * time.Millisecond)
		pool.Release(agent)
	}()

	_, err = pool.Acquire(ctx)
	require.NoError(t, err)

	stats := pool.Stats()
	assert.Greater(t, stats.WaitCount, int64(0))
}

// TestAgentPool_AverageWaitTime tests average wait time calculation
func TestAgentPool_AverageWaitTime(t *testing.T) {
	factory := func() (core.Agent, error) {
		return NewMockAgent("test", 10*time.Millisecond), nil
	}
	config := PoolConfig{
		InitialSize:    2,
		MaxSize:        2,
		IdleTimeout:    5 * time.Minute,
		MaxLifetime:    30 * time.Minute,
		AcquireTimeout: 2 * time.Second,
	}

	pool, err := NewAgentPool(factory, config)
	require.NoError(t, err)
	defer pool.Close()

	ctx := context.Background()

	// Acquire both agents
	agent1, _ := pool.Acquire(ctx)
	agent2, _ := pool.Acquire(ctx)

	// Release in background
	go func() {
		time.Sleep(100 * time.Millisecond)
		pool.Release(agent1)
	}()

	// Try to acquire (will wait)
	_, err = pool.Acquire(ctx)
	require.NoError(t, err)

	stats := pool.Stats()
	assert.Greater(t, stats.AvgWaitTime, time.Duration(0))

	pool.Release(agent2)
}

// TestPoolStats tests pool stats structure
func TestPoolStats(t *testing.T) {
	stats := PoolStats{
		TotalCount:     10,
		ActiveCount:    3,
		IdleCount:      7,
		MaxSize:        50,
		CreatedTotal:   15,
		AcquiredTotal:  100,
		ReleasedTotal:  97,
		RecycledTotal:  5,
		WaitCount:      10,
		AvgWaitTime:    100 * time.Millisecond,
		UtilizationPct: 6.0,
	}

	assert.Equal(t, 10, stats.TotalCount)
	assert.Equal(t, 3, stats.ActiveCount)
	assert.Equal(t, 7, stats.IdleCount)
	assert.Equal(t, 50, stats.MaxSize)
	assert.Greater(t, stats.UtilizationPct, 0.0)
}

// TestAgentPool_HighConcurrency tests pool under high concurrent load
func TestAgentPool_HighConcurrency(t *testing.T) {
	factory := func() (core.Agent, error) {
		return NewMockAgent("test", 1*time.Millisecond), nil
	}
	config := PoolConfig{
		InitialSize:    10,
		MaxSize:        50,
		IdleTimeout:    5 * time.Minute,
		MaxLifetime:    30 * time.Minute,
		AcquireTimeout: 5 * time.Second,
	}

	pool, err := NewAgentPool(factory, config)
	require.NoError(t, err)
	defer pool.Close()

	ctx := context.Background()
	successCount := int32(0)
	errorCount := int32(0)
	var wg sync.WaitGroup

	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			agent, err := pool.Acquire(ctx)
			if err != nil {
				atomic.AddInt32(&errorCount, 1)
				return
			}

			input := &core.AgentInput{
				Task:        "Test",
				Instruction: "Test",
				Timestamp:   time.Now(),
			}
			_, err = agent.Invoke(ctx, input)
			if err != nil {
				atomic.AddInt32(&errorCount, 1)
			} else {
				atomic.AddInt32(&successCount, 1)
			}

			pool.Release(agent)
		}()
	}

	wg.Wait()

	assert.Equal(t, int32(100), successCount)
	assert.Equal(t, int32(0), errorCount)
}

// TestAgentPool_InitialSizeGreaterThanMaxSize tests size adjustment
func TestAgentPool_InitialSizeGreaterThanMaxSize(t *testing.T) {
	factory := func() (core.Agent, error) {
		return NewMockAgent("test", 10*time.Millisecond), nil
	}
	config := PoolConfig{
		InitialSize:    20,
		MaxSize:        10,
		IdleTimeout:    5 * time.Minute,
		MaxLifetime:    30 * time.Minute,
		AcquireTimeout: 10 * time.Second,
	}

	pool, err := NewAgentPool(factory, config)
	require.NoError(t, err)
	defer pool.Close()

	stats := pool.Stats()
	// InitialSize should be adjusted down to MaxSize
	assert.Equal(t, 10, stats.TotalCount)
}
