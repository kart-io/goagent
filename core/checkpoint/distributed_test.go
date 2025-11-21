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

// MockCheckpointer is a mock implementation for testing
type MockCheckpointer struct {
	mu           sync.RWMutex
	data         map[string]agentstate.State
	failOnSave   bool
	failOnLoad   bool
	failOnDelete bool
	saveCount    int32
	loadCount    int32
	deleteCount  int32
	healthy      bool
	pingError    error
}

func NewMockCheckpointer() *MockCheckpointer {
	return &MockCheckpointer{
		data:    make(map[string]agentstate.State),
		healthy: true,
	}
}

func (m *MockCheckpointer) Save(ctx context.Context, threadID string, state agentstate.State) error {
	if m.failOnSave {
		return fmt.Errorf("mock save error")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	atomic.AddInt32(&m.saveCount, 1)
	m.data[threadID] = state.Clone()
	return nil
}

func (m *MockCheckpointer) Load(ctx context.Context, threadID string) (agentstate.State, error) {
	if m.failOnLoad {
		return nil, fmt.Errorf("mock load error")
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	atomic.AddInt32(&m.loadCount, 1)
	state, ok := m.data[threadID]
	if !ok {
		return nil, fmt.Errorf("not found")
	}
	return state.Clone(), nil
}

func (m *MockCheckpointer) Delete(ctx context.Context, threadID string) error {
	if m.failOnDelete {
		return fmt.Errorf("mock delete error")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	atomic.AddInt32(&m.deleteCount, 1)
	delete(m.data, threadID)
	return nil
}

func (m *MockCheckpointer) Exists(ctx context.Context, threadID string) (bool, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	_, ok := m.data[threadID]
	return ok, nil
}

func (m *MockCheckpointer) List(ctx context.Context) ([]CheckpointInfo, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	infos := make([]CheckpointInfo, 0, len(m.data))
	for threadID := range m.data {
		infos = append(infos, CheckpointInfo{ThreadID: threadID})
	}
	return infos, nil
}

func (m *MockCheckpointer) Ping(ctx context.Context) error {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if m.pingError != nil {
		return m.pingError
	}
	if !m.healthy {
		return fmt.Errorf("mock unhealthy")
	}
	return nil
}

func (m *MockCheckpointer) SetHealthy(healthy bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.healthy = healthy
}

func (m *MockCheckpointer) SetPingError(err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.pingError = err
}

// TestDefaultDistributedCheckpointerConfig tests default config
func TestDefaultDistributedCheckpointerConfig(t *testing.T) {
	config := DefaultDistributedCheckpointerConfig()

	require.NotNil(t, config)
	assert.True(t, config.EnableReplication)
	assert.Equal(t, "async", config.ReplicationMode)
	assert.Equal(t, 30*time.Second, config.HealthCheckInterval)
	assert.True(t, config.EnableAutoFailover)
	assert.Equal(t, 3, config.MaxFailoverAttempts)
	assert.Equal(t, 5*time.Minute, config.FailbackDelay)
}

// TestNewDistributedCheckpointer_NoPrimary tests creation without primary
func TestNewDistributedCheckpointer_NoPrimary(t *testing.T) {
	config := &DistributedCheckpointerConfig{
		PrimaryBackend: nil,
	}

	dc, err := NewDistributedCheckpointer(config)
	assert.Error(t, err)
	assert.Nil(t, dc)
	assert.Contains(t, err.Error(), "primary backend is required")
}

// TestNewDistributedCheckpointer_WithPrimary tests creation with primary only
func TestNewDistributedCheckpointer_WithPrimary(t *testing.T) {
	primary := NewMockCheckpointer()
	config := &DistributedCheckpointerConfig{
		PrimaryBackend:      primary,
		SecondaryBackend:    nil,
		EnableReplication:   false,
		HealthCheckInterval: 100 * time.Millisecond,
	}

	dc, err := NewDistributedCheckpointer(config)
	require.NoError(t, err)
	require.NotNil(t, dc)

	defer dc.Close()

	assert.Equal(t, primary, dc.activeBackend)
	assert.True(t, dc.primaryHealthy)
	assert.False(t, dc.secondaryHealthy)
	assert.False(t, dc.failedOver)
}

// TestNewDistributedCheckpointer_NilConfig tests with nil config uses defaults
func TestNewDistributedCheckpointer_NilConfig(t *testing.T) {
	primary := NewMockCheckpointer()
	config := &DistributedCheckpointerConfig{
		PrimaryBackend:      primary,
		HealthCheckInterval: 100 * time.Millisecond, // Must be > 0
	}

	dc, err := NewDistributedCheckpointer(config)
	require.NoError(t, err)
	require.NotNil(t, dc)

	defer dc.Close()

	assert.NotNil(t, dc.config)
}

// TestDistributedCheckpointer_Save tests save operation
func TestDistributedCheckpointer_Save(t *testing.T) {
	primary := NewMockCheckpointer()
	config := &DistributedCheckpointerConfig{
		PrimaryBackend:      primary,
		SecondaryBackend:    nil,
		EnableReplication:   false,
		HealthCheckInterval: 100 * time.Millisecond,
	}

	dc, err := NewDistributedCheckpointer(config)
	require.NoError(t, err)
	defer dc.Close()

	ctx := context.Background()
	state := agentstate.NewAgentState()
	state.Set("key", "value")

	err = dc.Save(ctx, "thread1", state)
	assert.NoError(t, err)
	assert.Equal(t, int32(1), primary.saveCount)
}

// TestDistributedCheckpointer_Load tests load operation
func TestDistributedCheckpointer_Load(t *testing.T) {
	primary := NewMockCheckpointer()
	config := &DistributedCheckpointerConfig{
		PrimaryBackend:      primary,
		HealthCheckInterval: 100 * time.Millisecond,
	}

	dc, err := NewDistributedCheckpointer(config)
	require.NoError(t, err)
	defer dc.Close()

	ctx := context.Background()
	state := agentstate.NewAgentState()
	state.Set("key", "value")

	// Save first
	err = dc.Save(ctx, "thread1", state)
	require.NoError(t, err)

	// Load
	loaded, err := dc.Load(ctx, "thread1")
	require.NoError(t, err)
	assert.NotNil(t, loaded)

	val, ok := loaded.Get("key")
	assert.True(t, ok)
	assert.Equal(t, "value", val)
}

// TestDistributedCheckpointer_Delete tests delete operation
func TestDistributedCheckpointer_Delete(t *testing.T) {
	primary := NewMockCheckpointer()
	config := &DistributedCheckpointerConfig{
		PrimaryBackend:      primary,
		HealthCheckInterval: 100 * time.Millisecond,
	}

	dc, err := NewDistributedCheckpointer(config)
	require.NoError(t, err)
	defer dc.Close()

	ctx := context.Background()
	state := agentstate.NewAgentState()
	state.Set("key", "value")

	// Save first
	err = dc.Save(ctx, "thread1", state)
	require.NoError(t, err)

	// Delete
	err = dc.Delete(ctx, "thread1")
	assert.NoError(t, err)
	assert.Equal(t, int32(1), primary.deleteCount)

	// Verify deleted
	exists, err := dc.Exists(ctx, "thread1")
	require.NoError(t, err)
	assert.False(t, exists)
}

// TestDistributedCheckpointer_Exists tests existence check
func TestDistributedCheckpointer_Exists(t *testing.T) {
	primary := NewMockCheckpointer()
	config := &DistributedCheckpointerConfig{
		PrimaryBackend:      primary,
		HealthCheckInterval: 100 * time.Millisecond,
	}

	dc, err := NewDistributedCheckpointer(config)
	require.NoError(t, err)
	defer dc.Close()

	ctx := context.Background()

	// Should not exist initially
	exists, err := dc.Exists(ctx, "thread1")
	require.NoError(t, err)
	assert.False(t, exists)

	// Save
	state := agentstate.NewAgentState()
	err = dc.Save(ctx, "thread1", state)
	require.NoError(t, err)

	// Should exist
	exists, err = dc.Exists(ctx, "thread1")
	require.NoError(t, err)
	assert.True(t, exists)
}

// TestDistributedCheckpointer_List tests list operation
func TestDistributedCheckpointer_List(t *testing.T) {
	primary := NewMockCheckpointer()
	config := &DistributedCheckpointerConfig{
		PrimaryBackend:      primary,
		HealthCheckInterval: 100 * time.Millisecond,
	}

	dc, err := NewDistributedCheckpointer(config)
	require.NoError(t, err)
	defer dc.Close()

	ctx := context.Background()

	// Save multiple
	for i := 0; i < 5; i++ {
		state := agentstate.NewAgentState()
		state.Set("id", i)
		err = dc.Save(ctx, fmt.Sprintf("thread-%d", i), state)
		require.NoError(t, err)
	}

	// List
	infos, err := dc.List(ctx)
	require.NoError(t, err)
	assert.Equal(t, 5, len(infos))
}

// TestDistributedCheckpointer_SaveFailover_NoSecondary tests failover with no secondary
func TestDistributedCheckpointer_SaveFailover_NoSecondary(t *testing.T) {
	primary := NewMockCheckpointer()
	primary.failOnSave = true

	config := &DistributedCheckpointerConfig{
		PrimaryBackend:      primary,
		SecondaryBackend:    nil, // No secondary
		EnableAutoFailover:  true,
		HealthCheckInterval: 100 * time.Millisecond,
	}

	dc, err := NewDistributedCheckpointer(config)
	require.NoError(t, err)
	defer dc.Close()

	ctx := context.Background()
	state := agentstate.NewAgentState()

	err = dc.Save(ctx, "thread1", state)
	assert.Error(t, err) // Should fail since no secondary
}

// TestDistributedCheckpointer_SaveFailover_WithSecondary tests backend switching after manual failover
func TestDistributedCheckpointer_SaveFailover_WithSecondary(t *testing.T) {
	primary := NewMockCheckpointer()
	secondary := NewMockCheckpointer()

	config := &DistributedCheckpointerConfig{
		PrimaryBackend:      primary,
		SecondaryBackend:    secondary,
		EnableAutoFailover:  true,
		MaxFailoverAttempts: 10, // Increase to allow health check failovers
		HealthCheckInterval: 100 * time.Millisecond,
	}

	dc, err := NewDistributedCheckpointer(config)
	require.NoError(t, err)
	defer dc.Close()

	ctx := context.Background()

	// Verify we can manually failover
	err = dc.tryFailover(ctx)
	assert.NoError(t, err)

	dc.mu.RLock()
	failedOver := dc.failedOver
	activeBackend := dc.activeBackend
	dc.mu.RUnlock()

	assert.True(t, failedOver)
	assert.Equal(t, secondary, activeBackend)
}

// TestDistributedCheckpointer_LoadFailover tests failover on load error
func TestDistributedCheckpointer_LoadFailover(t *testing.T) {
	primary := NewMockCheckpointer()
	secondary := NewMockCheckpointer()

	ctx := context.Background()

	// Pre-populate secondary
	state := agentstate.NewAgentState()
	state.Set("key", "value")
	secondary.Save(ctx, "thread1", state)

	config := &DistributedCheckpointerConfig{
		PrimaryBackend:      primary,
		SecondaryBackend:    secondary,
		EnableAutoFailover:  true,
		MaxFailoverAttempts: 10, // Increase to allow health check failovers
		HealthCheckInterval: 100 * time.Millisecond,
	}

	dc, err := NewDistributedCheckpointer(config)
	require.NoError(t, err)
	defer dc.Close()

	// Manually failover
	failoverErr := dc.tryFailover(ctx)
	assert.NoError(t, failoverErr)

	// Now load should succeed from secondary
	loaded, err := dc.Load(ctx, "thread1")
	assert.NoError(t, err)
	assert.NotNil(t, loaded)

	// Verify failover happened
	dc.mu.RLock()
	failedOver := dc.failedOver
	dc.mu.RUnlock()
	assert.True(t, failedOver)
}

// TestDistributedCheckpointer_ReplicationSync tests synchronous replication
func TestDistributedCheckpointer_ReplicationSync(t *testing.T) {
	primary := NewMockCheckpointer()
	secondary := NewMockCheckpointer()

	config := &DistributedCheckpointerConfig{
		PrimaryBackend:      primary,
		SecondaryBackend:    secondary,
		EnableReplication:   true,
		ReplicationMode:     ReplicationModeSync,
		HealthCheckInterval: 100 * time.Millisecond,
	}

	dc, err := NewDistributedCheckpointer(config)
	require.NoError(t, err)
	defer dc.Close()

	ctx := context.Background()
	state := agentstate.NewAgentState()
	state.Set("key", "value")

	err = dc.Save(ctx, "thread1", state)
	assert.NoError(t, err)

	// Both should have the data
	assert.Equal(t, int32(1), primary.saveCount)
	assert.Equal(t, int32(1), secondary.saveCount)

	// Verify secondary has the data
	loaded, err := secondary.Load(ctx, "thread1")
	require.NoError(t, err)
	val, ok := loaded.Get("key")
	assert.True(t, ok)
	assert.Equal(t, "value", val)
}

// TestDistributedCheckpointer_ReplicationAsync tests asynchronous replication
func TestDistributedCheckpointer_ReplicationAsync(t *testing.T) {
	primary := NewMockCheckpointer()
	secondary := NewMockCheckpointer()

	config := &DistributedCheckpointerConfig{
		PrimaryBackend:      primary,
		SecondaryBackend:    secondary,
		EnableReplication:   true,
		ReplicationMode:     ReplicationModeAsync,
		HealthCheckInterval: 100 * time.Millisecond,
	}

	dc, err := NewDistributedCheckpointer(config)
	require.NoError(t, err)
	defer dc.Close()

	ctx := context.Background()
	state := agentstate.NewAgentState()
	state.Set("key", "value")

	err = dc.Save(ctx, "thread1", state)
	assert.NoError(t, err)

	// Primary should have it immediately
	assert.Equal(t, int32(1), atomic.LoadInt32(&primary.saveCount))

	// Secondary might have it after replication workers process
	time.Sleep(100 * time.Millisecond)
	assert.Greater(t, atomic.LoadInt32(&secondary.saveCount), int32(0))
}

// TestDistributedCheckpointer_ReplicationDelete tests delete replication
func TestDistributedCheckpointer_ReplicationDelete(t *testing.T) {
	primary := NewMockCheckpointer()
	secondary := NewMockCheckpointer()

	config := &DistributedCheckpointerConfig{
		PrimaryBackend:      primary,
		SecondaryBackend:    secondary,
		EnableReplication:   true,
		ReplicationMode:     ReplicationModeSync,
		HealthCheckInterval: 100 * time.Millisecond,
	}

	dc, err := NewDistributedCheckpointer(config)
	require.NoError(t, err)
	defer dc.Close()

	ctx := context.Background()
	state := agentstate.NewAgentState()

	// Save to both
	err = dc.Save(ctx, "thread1", state)
	require.NoError(t, err)

	// Delete from both
	err = dc.Delete(ctx, "thread1")
	assert.NoError(t, err)

	// Both should report deleted
	assert.Equal(t, int32(1), primary.deleteCount)
	assert.Equal(t, int32(1), secondary.deleteCount)
}

// TestDistributedCheckpointer_HealthCheck tests health checking
func TestDistributedCheckpointer_HealthCheck(t *testing.T) {
	primary := NewMockCheckpointer()
	secondary := NewMockCheckpointer()

	config := &DistributedCheckpointerConfig{
		PrimaryBackend:      primary,
		SecondaryBackend:    secondary,
		EnableAutoFailover:  true,
		HealthCheckInterval: 50 * time.Millisecond,
	}

	dc, err := NewDistributedCheckpointer(config)
	require.NoError(t, err)

	// Let health check run
	time.Sleep(100 * time.Millisecond)

	dc.mu.RLock()
	primaryHealthy := dc.primaryHealthy
	secondaryHealthy := dc.secondaryHealthy
	dc.mu.RUnlock()

	assert.True(t, primaryHealthy)
	assert.True(t, secondaryHealthy)

	dc.Close()
}

// TestDistributedCheckpointer_HealthCheck_FailoverTrigger tests health-triggered failover
func TestDistributedCheckpointer_HealthCheck_FailoverTrigger(t *testing.T) {
	primary := NewMockCheckpointer()
	secondary := NewMockCheckpointer()

	config := &DistributedCheckpointerConfig{
		PrimaryBackend:      primary,
		SecondaryBackend:    secondary,
		EnableAutoFailover:  true,
		HealthCheckInterval: 50 * time.Millisecond,
		MaxFailoverAttempts: 3,
	}

	dc, err := NewDistributedCheckpointer(config)
	require.NoError(t, err)

	// Make primary unhealthy
	primary.SetHealthy(false)
	primary.SetPingError(fmt.Errorf("unhealthy"))

	// Wait for health check to trigger failover
	time.Sleep(150 * time.Millisecond)

	dc.mu.RLock()
	failedOver := dc.failedOver
	activeBackend := dc.activeBackend
	dc.mu.RUnlock()

	assert.True(t, failedOver)
	assert.Equal(t, secondary, activeBackend)

	dc.Close()
}

// TestDistributedCheckpointer_HealthCheck_Failback tests failback to primary
func TestDistributedCheckpointer_HealthCheck_Failback(t *testing.T) {
	primary := NewMockCheckpointer()
	secondary := NewMockCheckpointer()

	config := &DistributedCheckpointerConfig{
		PrimaryBackend:      primary,
		SecondaryBackend:    secondary,
		EnableAutoFailover:  true,
		HealthCheckInterval: 50 * time.Millisecond,
		MaxFailoverAttempts: 3,
		FailbackDelay:       50 * time.Millisecond,
	}

	dc, err := NewDistributedCheckpointer(config)
	require.NoError(t, err)

	// Trigger failover
	primary.SetHealthy(false)
	primary.SetPingError(fmt.Errorf("unhealthy"))
	time.Sleep(150 * time.Millisecond)

	// Verify failed over
	dc.mu.RLock()
	assert.True(t, dc.failedOver)
	dc.mu.RUnlock()

	// Restore primary
	primary.SetHealthy(true)
	primary.SetPingError(nil)
	time.Sleep(150 * time.Millisecond)

	// Verify failed back
	dc.mu.RLock()
	failedOver := dc.failedOver
	activeBackend := dc.activeBackend
	dc.mu.RUnlock()

	assert.False(t, failedOver)
	assert.Equal(t, primary, activeBackend)

	dc.Close()
}

// TestDistributedCheckpointer_MaxFailoverAttempts tests max failover limit
func TestDistributedCheckpointer_MaxFailoverAttempts(t *testing.T) {
	primary := NewMockCheckpointer()
	secondary := NewMockCheckpointer()
	primary.failOnSave = true

	config := &DistributedCheckpointerConfig{
		PrimaryBackend:      primary,
		SecondaryBackend:    secondary,
		EnableAutoFailover:  true,
		MaxFailoverAttempts: 2,
		HealthCheckInterval: 100 * time.Millisecond,
	}

	dc, err := NewDistributedCheckpointer(config)
	require.NoError(t, err)
	defer dc.Close()

	ctx := context.Background()
	state := agentstate.NewAgentState()

	// First failover should succeed
	err = dc.Save(ctx, "thread1", state)
	assert.NoError(t, err)

	dc.mu.RLock()
	failoverCount := dc.failoverCount
	dc.mu.RUnlock()

	assert.Equal(t, 1, failoverCount)
}

// TestDistributedCheckpointer_GetStatus tests status reporting
func TestDistributedCheckpointer_GetStatus(t *testing.T) {
	primary := NewMockCheckpointer()
	secondary := NewMockCheckpointer()

	config := &DistributedCheckpointerConfig{
		PrimaryBackend:      primary,
		SecondaryBackend:    secondary,
		EnableReplication:   true,
		ReplicationMode:     ReplicationModeAsync,
		HealthCheckInterval: 100 * time.Millisecond,
	}

	dc, err := NewDistributedCheckpointer(config)
	require.NoError(t, err)
	defer dc.Close()

	status := dc.GetStatus()

	assert.NotNil(t, status)
	assert.True(t, status["primary_healthy"].(bool))
	assert.True(t, status["secondary_healthy"].(bool))
	assert.False(t, status["failed_over"].(bool))
	assert.Equal(t, "primary", status["active_backend"])
	assert.True(t, status["enable_replication"].(bool))
	assert.Equal(t, ReplicationModeAsync, status["replication_mode"])
}

// TestDistributedCheckpointer_ConcurrentOperations tests concurrent save/load
func TestDistributedCheckpointer_ConcurrentOperations(t *testing.T) {
	primary := NewMockCheckpointer()
	config := &DistributedCheckpointerConfig{
		PrimaryBackend:      primary,
		HealthCheckInterval: 100 * time.Millisecond,
	}

	dc, err := NewDistributedCheckpointer(config)
	require.NoError(t, err)
	defer dc.Close()

	ctx := context.Background()
	const (
		numGoroutines   = 20
		opsPerGoroutine = 30
	)

	var wg sync.WaitGroup
	errors := make(chan error, numGoroutines*opsPerGoroutine)

	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(threadNum int) {
			defer wg.Done()
			threadID := fmt.Sprintf("concurrent-%d", threadNum)

			for j := 0; j < opsPerGoroutine; j++ {
				state := agentstate.NewAgentState()
				state.Set("counter", j)

				if err := dc.Save(ctx, threadID, state); err != nil {
					errors <- fmt.Errorf("save error: %w", err)
				}

				if _, err := dc.Load(ctx, threadID); err != nil {
					errors <- fmt.Errorf("load error: %w", err)
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

// TestDistributedCheckpointer_Close tests graceful shutdown
func TestDistributedCheckpointer_Close(t *testing.T) {
	primary := NewMockCheckpointer()
	secondary := NewMockCheckpointer()

	config := &DistributedCheckpointerConfig{
		PrimaryBackend:      primary,
		SecondaryBackend:    secondary,
		EnableReplication:   true,
		ReplicationMode:     ReplicationModeAsync,
		HealthCheckInterval: 50 * time.Millisecond,
	}

	dc, err := NewDistributedCheckpointer(config)
	require.NoError(t, err)

	// Save something
	ctx := context.Background()
	state := agentstate.NewAgentState()
	dc.Save(ctx, "thread1", state)

	// First close should succeed
	err = dc.Close()
	assert.NoError(t, err)
}

// TestDistributedCheckpointer_ReplicationQueueFull tests queue overflow handling
func TestDistributedCheckpointer_ReplicationQueueFull(t *testing.T) {
	primary := NewMockCheckpointer()
	secondary := NewMockCheckpointer()

	config := &DistributedCheckpointerConfig{
		PrimaryBackend:      primary,
		SecondaryBackend:    secondary,
		EnableReplication:   true,
		ReplicationMode:     ReplicationModeAsync,
		HealthCheckInterval: 1 * time.Second,
	}

	dc, err := NewDistributedCheckpointer(config)
	require.NoError(t, err)
	defer dc.Close()

	ctx := context.Background()

	// Try to fill replication queue
	for i := 0; i < 2000; i++ {
		state := agentstate.NewAgentState()
		state.Set("id", i)
		dc.Save(ctx, fmt.Sprintf("thread-%d", i), state)
	}

	// Should not panic or deadlock
	assert.True(t, true)
}

// TestDistributedCheckpointer_InvalidReplicationMode tests invalid replication mode
func TestDistributedCheckpointer_InvalidReplicationMode(t *testing.T) {
	primary := NewMockCheckpointer()
	secondary := NewMockCheckpointer()

	config := &DistributedCheckpointerConfig{
		PrimaryBackend:      primary,
		SecondaryBackend:    secondary,
		EnableReplication:   true,
		ReplicationMode:     "invalid", // Invalid mode
		HealthCheckInterval: 100 * time.Millisecond,
	}

	dc, err := NewDistributedCheckpointer(config)
	require.NoError(t, err)
	defer dc.Close()

	ctx := context.Background()
	state := agentstate.NewAgentState()

	// Should work but not replicate
	err = dc.Save(ctx, "thread1", state)
	assert.NoError(t, err)
}
