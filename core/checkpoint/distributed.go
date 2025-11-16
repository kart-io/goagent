package checkpoint

import (
	"context"
	"fmt"
	"sync"
	"time"

	agentstate "github.com/kart-io/goagent/core/state"
)

const (
	ReplicationModeSync  = "sync"
	ReplicationModeAsync = "async"
)

// DistributedCheckpointerConfig holds configuration for distributed checkpointer
type DistributedCheckpointerConfig struct {
	// PrimaryBackend is the primary checkpointer (e.g., Redis)
	PrimaryBackend Checkpointer

	// SecondaryBackend is the backup checkpointer (e.g., PostgreSQL, optional)
	SecondaryBackend Checkpointer

	// EnableReplication enables automatic replication to secondary backend
	EnableReplication bool

	// ReplicationMode determines how replication works
	// - "sync": Write to both backends synchronously
	// - "async": Write to primary, then replicate to secondary asynchronously
	ReplicationMode string

	// HealthCheckInterval is how often to check backend health
	HealthCheckInterval time.Duration

	// EnableAutoFailover enables automatic failover to secondary on primary failure
	EnableAutoFailover bool

	// MaxFailoverAttempts limits the number of failover attempts
	MaxFailoverAttempts int

	// FailbackDelay is the delay before attempting to fail back to primary
	FailbackDelay time.Duration
}

// DefaultDistributedCheckpointerConfig returns default configuration
func DefaultDistributedCheckpointerConfig() *DistributedCheckpointerConfig {
	return &DistributedCheckpointerConfig{
		EnableReplication:   true,
		ReplicationMode:     "async",
		HealthCheckInterval: 30 * time.Second,
		EnableAutoFailover:  true,
		MaxFailoverAttempts: 3,
		FailbackDelay:       5 * time.Minute,
	}
}

// DistributedCheckpointer provides high-availability checkpointing with multiple backends.
//
// Features:
//   - Primary/secondary backend architecture
//   - Automatic failover on primary failure
//   - Replication modes: sync or async
//   - Health monitoring
//   - Automatic failback to primary
//
// Suitable for:
//   - Production environments requiring high availability
//   - Critical applications that cannot tolerate data loss
//   - Multi-region deployments
//   - Disaster recovery scenarios
type DistributedCheckpointer struct {
	config *DistributedCheckpointerConfig

	// Current active backend
	activeBackend Checkpointer

	// Backend health status
	primaryHealthy   bool
	secondaryHealthy bool

	// Failover state
	failedOver     bool
	failoverCount  int
	lastFailoverAt time.Time
	lastFailbackAt time.Time

	// Async replication
	replicationQueue chan replicationTask
	replicationWg    sync.WaitGroup

	// Health check
	healthCheckStop chan struct{}
	healthCheckWg   sync.WaitGroup

	// Synchronization
	mu sync.RWMutex
}

// replicationTask represents an async replication task
type replicationTask struct {
	operation string // "save" or "delete"
	threadID  string
	state     agentstate.State
}

// NewDistributedCheckpointer creates a new distributed checkpointer
func NewDistributedCheckpointer(config *DistributedCheckpointerConfig) (*DistributedCheckpointer, error) {
	if config == nil {
		config = DefaultDistributedCheckpointerConfig()
	}

	if config.PrimaryBackend == nil {
		return nil, fmt.Errorf("primary backend is required")
	}

	dc := &DistributedCheckpointer{
		config:           config,
		activeBackend:    config.PrimaryBackend,
		primaryHealthy:   true,
		secondaryHealthy: config.SecondaryBackend != nil,
		replicationQueue: make(chan replicationTask, 1000),
		healthCheckStop:  make(chan struct{}),
	}

	// Start async replication workers if enabled
	if config.EnableReplication && config.ReplicationMode == "async" && config.SecondaryBackend != nil {
		dc.startReplicationWorkers()
	}

	// Start health check
	dc.startHealthCheck()

	return dc, nil
}

// Save persists the current state for a thread/session
func (dc *DistributedCheckpointer) Save(ctx context.Context, threadID string, state agentstate.State) error {
	dc.mu.RLock()
	active := dc.activeBackend
	secondary := dc.config.SecondaryBackend
	replicationMode := dc.config.ReplicationMode
	enableReplication := dc.config.EnableReplication
	dc.mu.RUnlock()

	// Save to active backend
	if err := active.Save(ctx, threadID, state); err != nil {
		// Try failover if enabled
		if dc.config.EnableAutoFailover {
			if failoverErr := dc.tryFailover(ctx); failoverErr == nil {
				// Retry with new active backend
				return dc.activeBackend.Save(ctx, threadID, state)
			}
		}
		return fmt.Errorf("failed to save to active backend: %w", err)
	}

	// Replicate to secondary if enabled
	if enableReplication && secondary != nil {
		switch replicationMode {
		case "sync":
			// Synchronous replication
			if err := secondary.Save(ctx, threadID, state.Clone()); err != nil {
				// Log error but don't fail the operation
				// In production, you might want to use a logger here
				fmt.Printf("failed to replicate to secondary: %v", err)
			}
		case "async":
			// Asynchronous replication
			select {
			case dc.replicationQueue <- replicationTask{
				operation: "save",
				threadID:  threadID,
				state:     state.Clone(),
			}:
			default:
				// Queue full, skip replication
			}
		}
	}

	return nil
}

// Load retrieves the saved state for a thread/session
func (dc *DistributedCheckpointer) Load(ctx context.Context, threadID string) (agentstate.State, error) {
	dc.mu.RLock()
	active := dc.activeBackend
	dc.mu.RUnlock()

	state, err := active.Load(ctx, threadID)
	if err != nil {
		// Try failover if enabled
		if dc.config.EnableAutoFailover {
			if failoverErr := dc.tryFailover(ctx); failoverErr == nil {
				// Retry with new active backend
				return dc.activeBackend.Load(ctx, threadID)
			}
		}
		return nil, fmt.Errorf("failed to load from active backend: %w", err)
	}

	return state, nil
}

// List returns information about all saved checkpoints
func (dc *DistributedCheckpointer) List(ctx context.Context) ([]CheckpointInfo, error) {
	dc.mu.RLock()
	active := dc.activeBackend
	dc.mu.RUnlock()

	return active.List(ctx)
}

// Delete removes the checkpoint for a thread/session
func (dc *DistributedCheckpointer) Delete(ctx context.Context, threadID string) error {
	dc.mu.RLock()
	active := dc.activeBackend
	secondary := dc.config.SecondaryBackend
	replicationMode := dc.config.ReplicationMode
	enableReplication := dc.config.EnableReplication
	dc.mu.RUnlock()

	// Delete from active backend
	if err := active.Delete(ctx, threadID); err != nil {
		return fmt.Errorf("failed to delete from active backend: %w", err)
	}

	// Replicate deletion to secondary if enabled
	if enableReplication && secondary != nil {
		switch replicationMode {
		case ReplicationModeSync:
			_ = secondary.Delete(ctx, threadID)
		case ReplicationModeAsync:
			select {
			case dc.replicationQueue <- replicationTask{
				operation: "delete",
				threadID:  threadID,
			}:
			default:
			}
		}
	}

	return nil
}

// Exists checks if a checkpoint exists for a thread/session
func (dc *DistributedCheckpointer) Exists(ctx context.Context, threadID string) (bool, error) {
	dc.mu.RLock()
	active := dc.activeBackend
	dc.mu.RUnlock()

	return active.Exists(ctx, threadID)
}

// Close shuts down the distributed checkpointer
func (dc *DistributedCheckpointer) Close() error {
	// Stop health check
	close(dc.healthCheckStop)
	dc.healthCheckWg.Wait()

	// Stop replication workers
	if dc.config.EnableReplication && dc.config.ReplicationMode == "async" {
		close(dc.replicationQueue)
		dc.replicationWg.Wait()
	}

	return nil
}

// tryFailover attempts to failover to secondary backend
func (dc *DistributedCheckpointer) tryFailover(ctx context.Context) error {
	dc.mu.Lock()
	defer dc.mu.Unlock()

	if dc.failedOver {
		return fmt.Errorf("already failed over")
	}

	if dc.config.SecondaryBackend == nil {
		return fmt.Errorf("no secondary backend available")
	}

	if dc.failoverCount >= dc.config.MaxFailoverAttempts {
		return fmt.Errorf("max failover attempts reached")
	}

	// Switch to secondary
	dc.activeBackend = dc.config.SecondaryBackend
	dc.failedOver = true
	dc.failoverCount++
	dc.lastFailoverAt = time.Now()

	return nil
}

// tryFailback attempts to fail back to primary backend
func (dc *DistributedCheckpointer) tryFailback(ctx context.Context) error {
	dc.mu.Lock()
	defer dc.mu.Unlock()

	if !dc.failedOver {
		return nil // Already on primary
	}

	// Check if enough time has passed since last failover
	if time.Since(dc.lastFailoverAt) < dc.config.FailbackDelay {
		return fmt.Errorf("failback delay not met")
	}

	// Check primary health
	if !dc.primaryHealthy {
		return fmt.Errorf("primary backend not healthy")
	}

	// Switch back to primary
	dc.activeBackend = dc.config.PrimaryBackend
	dc.failedOver = false
	dc.lastFailbackAt = time.Now()

	return nil
}

// startReplicationWorkers starts async replication workers
func (dc *DistributedCheckpointer) startReplicationWorkers() {
	const numWorkers = 3

	for i := 0; i < numWorkers; i++ {
		dc.replicationWg.Add(1)
		go dc.replicationWorker()
	}
}

// replicationWorker processes async replication tasks
func (dc *DistributedCheckpointer) replicationWorker() {
	defer dc.replicationWg.Done()

	ctx := context.Background()

	for task := range dc.replicationQueue {
		dc.mu.RLock()
		secondary := dc.config.SecondaryBackend
		dc.mu.RUnlock()

		if secondary == nil {
			continue
		}

		switch task.operation {
		case "save":
			_ = secondary.Save(ctx, task.threadID, task.state)
		case "delete":
			_ = secondary.Delete(ctx, task.threadID)
		}
	}
}

// startHealthCheck starts background health monitoring
func (dc *DistributedCheckpointer) startHealthCheck() {
	dc.healthCheckWg.Add(1)
	go dc.healthCheckLoop()
}

// healthCheckLoop periodically checks backend health
func (dc *DistributedCheckpointer) healthCheckLoop() {
	defer dc.healthCheckWg.Done()

	ticker := time.NewTicker(dc.config.HealthCheckInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			dc.checkBackendHealth()
		case <-dc.healthCheckStop:
			return
		}
	}
}

// checkBackendHealth checks the health of both backends
func (dc *DistributedCheckpointer) checkBackendHealth() {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Check primary
	primaryHealthy := true
	if pinger, ok := dc.config.PrimaryBackend.(interface{ Ping(context.Context) error }); ok {
		if err := pinger.Ping(ctx); err != nil {
			primaryHealthy = false
		}
	}

	// Check secondary
	secondaryHealthy := false
	if dc.config.SecondaryBackend != nil {
		if pinger, ok := dc.config.SecondaryBackend.(interface{ Ping(context.Context) error }); ok {
			if err := pinger.Ping(ctx); err == nil {
				secondaryHealthy = true
			}
		}
	}

	dc.mu.Lock()
	dc.primaryHealthy = primaryHealthy
	dc.secondaryHealthy = secondaryHealthy
	dc.mu.Unlock()

	// Try failover if primary is down and auto-failover is enabled
	if !primaryHealthy && dc.config.EnableAutoFailover && !dc.failedOver {
		_ = dc.tryFailover(ctx)
	}

	// Try failback if primary is back up
	if primaryHealthy && dc.failedOver {
		_ = dc.tryFailback(ctx)
	}
}

// GetStatus returns the current status of the distributed checkpointer
func (dc *DistributedCheckpointer) GetStatus() map[string]interface{} {
	dc.mu.RLock()
	defer dc.mu.RUnlock()

	status := map[string]interface{}{
		"primary_healthy":    dc.primaryHealthy,
		"secondary_healthy":  dc.secondaryHealthy,
		"failed_over":        dc.failedOver,
		"failover_count":     dc.failoverCount,
		"replication_mode":   dc.config.ReplicationMode,
		"enable_replication": dc.config.EnableReplication,
	}

	if dc.failedOver {
		status["active_backend"] = "secondary"
		status["time_since_failover"] = time.Since(dc.lastFailoverAt).String()
	} else {
		status["active_backend"] = "primary"
	}

	if dc.config.ReplicationMode == "async" {
		status["replication_queue_size"] = len(dc.replicationQueue)
	}

	return status
}
