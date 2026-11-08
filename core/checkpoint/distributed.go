package checkpoint

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/kart-io/goagent/constants"
	agentstate "github.com/kart-io/goagent/core/state"
	agentErrors "github.com/kart-io/goagent/errors"
)

// DistributedCheckpointerConfig holds configuration for distributed checkpointer
type DistributedCheckpointerConfig struct {
	// PrimaryBackend is the primary checkpointer (e.g., Redis)
	PrimaryBackend Checkpointer `json:"-"`

	// SecondaryBackend is the backup checkpointer (e.g., PostgreSQL, optional)
	SecondaryBackend Checkpointer `json:"-"`

	// EnableReplication enables automatic replication to secondary backend
	EnableReplication bool `json:"enable_replication"`

	// ReplicationMode determines how replication works
	// - "sync": Write to both backends synchronously
	// - "async": Write to primary, then replicate to secondary asynchronously
	ReplicationMode string `json:"replication_mode"`

	// HealthCheckInterval is how often to check backend health
	HealthCheckInterval time.Duration `json:"health_check_interval"`

	// EnableAutoFailover enables automatic failover to secondary on primary failure
	EnableAutoFailover bool `json:"enable_auto_failover"`

	// MaxFailoverAttempts limits the number of failover attempts
	MaxFailoverAttempts int `json:"max_failover_attempts"`
}

// DefaultDistributedCheckpointerConfig returns default configuration
func DefaultDistributedCheckpointerConfig() *DistributedCheckpointerConfig {
	return &DistributedCheckpointerConfig{
		EnableReplication:   true,
		ReplicationMode:     constants.CheckpointReplicationModeAsync,
		HealthCheckInterval: constants.DefaultCheckpointHealthCheckInterval,
		EnableAutoFailover:  true,
		MaxFailoverAttempts: constants.DefaultCheckpointMaxFailoverAttempts,
	}
}

// DistributedCheckpointer 提供单写者主副库检查点与尽力复制，不保证零数据丢失。
// 复制队列满或副库写入失败时副库可落后；切换前宿主须接受此 RPO。
// 故障切到副库后保持副库为权威来源，禁止未经对账的自动切回。
// 提交与入队串行，复制与切换共享屏障；同一实例的前台存储调用串行执行。
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

	// Async replication
	replicationQueue chan replicationTask
	replicationWg    sync.WaitGroup

	// Health check
	healthCheckStop chan struct{}
	healthCheckWg   sync.WaitGroup

	// Synchronization
	mu sync.RWMutex
	// opMu 保持前台提交与入队的顺序；replicationMu 隔离副库应用与切换。
	// 锁顺序为 opMu -> replicationMu -> mu；异步复制无需等待 opMu。
	opMu          sync.Mutex
	replicationMu sync.Mutex
	closed        bool
	closeOnce     sync.Once
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
		return nil, agentErrors.NewInvalidConfigError("distributed_checkpointer", "primary_backend", "primary backend is required")
	}

	// 配置属于实例，防止宿主在运行时改写后端和复制模式。
	owned := *config
	config = &owned

	dc := &DistributedCheckpointer{
		config:           config,
		activeBackend:    config.PrimaryBackend,
		primaryHealthy:   true,
		secondaryHealthy: config.SecondaryBackend != nil,
		replicationQueue: make(chan replicationTask, 1000),
		healthCheckStop:  make(chan struct{}),
	}

	// Start async replication workers if enabled
	if config.EnableReplication && config.ReplicationMode == constants.CheckpointReplicationModeAsync && config.SecondaryBackend != nil {
		dc.startReplicationWorkers()
	}

	// Start health check
	dc.startHealthCheck()

	return dc, nil
}

// Save persists the current state for a thread/session
func (dc *DistributedCheckpointer) Save(ctx context.Context, threadID string, state agentstate.State) error {
	dc.opMu.Lock()
	defer dc.opMu.Unlock()
	if err := dc.checkOpen(ctx); err != nil {
		return err
	}
	dc.mu.RLock()
	active := dc.activeBackend
	secondary := dc.config.SecondaryBackend
	replicationMode := dc.config.ReplicationMode
	enableReplication := dc.config.EnableReplication
	failedOver := dc.failedOver
	dc.mu.RUnlock()

	// Save to active backend
	if err := active.Save(ctx, threadID, state); err != nil {
		// Try failover if enabled
		if dc.config.EnableAutoFailover {
			if backend, failoverErr := dc.failoverLocked(); failoverErr == nil {
				return backend.Save(ctx, threadID, state)
			}
		}
		return agentErrors.Wrap(err, agentErrors.CodeStateCheckpoint, "failed to save to active backend").
			WithComponent("distributed_checkpointer").
			WithOperation("save").
			WithContext("thread_id", threadID)
	}

	// Replicate to secondary if enabled
	if enableReplication && secondary != nil && !failedOver {
		switch replicationMode {
		case constants.CheckpointReplicationModeSync:
			// Synchronous replication
			if err := secondary.Save(ctx, threadID, state.Clone()); err != nil {
				// Log error but don't fail the operation
				// In production, you might want to use a logger here
				fmt.Printf("failed to replicate to secondary: %v", err)
			}
		case constants.CheckpointReplicationModeAsync:
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
	dc.opMu.Lock()
	defer dc.opMu.Unlock()
	if err := dc.checkOpen(ctx); err != nil {
		return nil, err
	}
	dc.mu.RLock()
	active := dc.activeBackend
	dc.mu.RUnlock()

	state, err := active.Load(ctx, threadID)
	if err != nil {
		// Try failover if enabled
		if dc.config.EnableAutoFailover {
			if backend, failoverErr := dc.failoverLocked(); failoverErr == nil {
				return backend.Load(ctx, threadID)
			}
		}
		return nil, agentErrors.Wrap(err, agentErrors.CodeStateLoad, "failed to load from active backend").
			WithComponent("distributed_checkpointer").
			WithOperation("load").
			WithContext("thread_id", threadID)
	}

	return state, nil
}

// List returns information about all saved checkpoints
func (dc *DistributedCheckpointer) List(ctx context.Context) ([]CheckpointInfo, error) {
	dc.opMu.Lock()
	defer dc.opMu.Unlock()
	if err := dc.checkOpen(ctx); err != nil {
		return nil, err
	}
	dc.mu.RLock()
	active := dc.activeBackend
	dc.mu.RUnlock()

	return active.List(ctx)
}

// Delete removes the checkpoint for a thread/session
func (dc *DistributedCheckpointer) Delete(ctx context.Context, threadID string) error {
	dc.opMu.Lock()
	defer dc.opMu.Unlock()
	if err := dc.checkOpen(ctx); err != nil {
		return err
	}
	dc.mu.RLock()
	active := dc.activeBackend
	secondary := dc.config.SecondaryBackend
	replicationMode := dc.config.ReplicationMode
	enableReplication := dc.config.EnableReplication
	failedOver := dc.failedOver
	dc.mu.RUnlock()

	// Delete from active backend
	if err := active.Delete(ctx, threadID); err != nil {
		return agentErrors.Wrap(err, agentErrors.CodeStateCheckpoint, "failed to delete from active backend").
			WithComponent("distributed_checkpointer").
			WithOperation("delete").
			WithContext("thread_id", threadID)
	}

	// Replicate deletion to secondary if enabled
	if enableReplication && secondary != nil && !failedOver {
		switch replicationMode {
		case constants.CheckpointReplicationModeSync:
			_ = secondary.Delete(ctx, threadID)
		case constants.CheckpointReplicationModeAsync:
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
	dc.opMu.Lock()
	defer dc.opMu.Unlock()
	if err := dc.checkOpen(ctx); err != nil {
		return false, err
	}
	dc.mu.RLock()
	active := dc.activeBackend
	dc.mu.RUnlock()

	return active.Exists(ctx, threadID)
}

// Close shuts down the distributed checkpointer
func (dc *DistributedCheckpointer) Close() error {
	dc.closeOnce.Do(func() {
		dc.opMu.Lock()
		dc.closed = true
		close(dc.healthCheckStop)
		close(dc.replicationQueue)
		dc.opMu.Unlock()
		dc.healthCheckWg.Wait()
		dc.replicationWg.Wait()
	})
	return nil
}

// checkOpen 的调用方持有 opMu。
func (dc *DistributedCheckpointer) checkOpen(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if dc.closed {
		return agentErrors.New(agentErrors.CodeStateCheckpoint, "检查点已关闭").WithComponent("distributed_checkpointer")
	}
	return nil
}

// tryFailover attempts to failover to secondary backend
func (dc *DistributedCheckpointer) tryFailover(ctx context.Context) error {
	dc.opMu.Lock()
	defer dc.opMu.Unlock()
	if err := dc.checkOpen(ctx); err != nil {
		return err
	}
	_, err := dc.failoverLocked()
	return err
}

// failoverLocked 返回切换时锁内选定的后端；调用方持有 opMu，旧复制任务此后只能丢弃。
func (dc *DistributedCheckpointer) failoverLocked() (Checkpointer, error) {
	dc.replicationMu.Lock()
	defer dc.replicationMu.Unlock()
	dc.mu.Lock()
	defer dc.mu.Unlock()

	if dc.failedOver {
		return nil, agentErrors.New(agentErrors.CodeDistributedCoordination, "already failed over").
			WithComponent("distributed_checkpointer").
			WithOperation("failover")
	}

	if dc.config.SecondaryBackend == nil {
		return nil, agentErrors.New(agentErrors.CodeDistributedCoordination, "no secondary backend available").
			WithComponent("distributed_checkpointer").
			WithOperation("failover")
	}

	if dc.failoverCount >= dc.config.MaxFailoverAttempts {
		return nil, agentErrors.New(agentErrors.CodeDistributedCoordination, "max failover attempts reached").
			WithComponent("distributed_checkpointer").
			WithOperation("failover").
			WithContext("max_attempts", dc.config.MaxFailoverAttempts).
			WithContext("current_count", dc.failoverCount)
	}

	// Switch to secondary
	dc.activeBackend = dc.config.SecondaryBackend
	dc.failedOver = true
	dc.failoverCount++
	dc.lastFailoverAt = time.Now()

	return dc.activeBackend, nil
}

// startReplicationWorkers starts async replication workers
func (dc *DistributedCheckpointer) startReplicationWorkers() {
	const numWorkers = 1

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
		dc.replicationMu.Lock()
		dc.mu.RLock()
		secondary := dc.config.SecondaryBackend
		failedOver := dc.failedOver
		dc.mu.RUnlock()

		if secondary == nil || failedOver {
			dc.replicationMu.Unlock()
			continue
		}

		switch task.operation {
		case "save":
			_ = callBackend(func() error { return secondary.Save(ctx, task.threadID, task.state) })
		case "delete":
			_ = callBackend(func() error { return secondary.Delete(ctx, task.threadID) })
		}
		dc.replicationMu.Unlock()
	}
}

// callBackend 在后台 goroutine 中调用可由宿主替换的存储后端，panic 转为 *agentErrors.PanicError 按普通失败处理
// （复制失败被忽略，健康检查记为不健康）。core 导入本包，这里无法使用 core 的 PanicHandler。
func callBackend(call func() error) (err error) {
	defer func() {
		if v := recover(); v != nil {
			err = agentErrors.NewPanicError("检查点存储后端", v)
		}
	}()
	return call()
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
		if err := callBackend(func() error { return pinger.Ping(ctx) }); err != nil {
			primaryHealthy = false
		}
	}

	// Check secondary
	secondaryHealthy := false
	if dc.config.SecondaryBackend != nil {
		if pinger, ok := dc.config.SecondaryBackend.(interface{ Ping(context.Context) error }); ok {
			if err := callBackend(func() error { return pinger.Ping(ctx) }); err == nil {
				secondaryHealthy = true
			}
		}
	}

	dc.mu.Lock()
	dc.primaryHealthy = primaryHealthy
	dc.secondaryHealthy = secondaryHealthy
	failedOver := dc.failedOver
	dc.mu.Unlock()

	// Try failover if primary is down and auto-failover is enabled
	if !primaryHealthy && dc.config.EnableAutoFailover && !failedOver {
		_ = dc.tryFailover(ctx)
	}

}

// GetStatus returns the current status of the distributed checkpointer
func (dc *DistributedCheckpointer) GetStatus() map[string]interface{} {
	dc.mu.RLock()
	defer dc.mu.RUnlock()

	status := map[string]interface{}{
		"auto_failback_supported": false,
		"primary_healthy":         dc.primaryHealthy,
		"secondary_healthy":       dc.secondaryHealthy,
		"failed_over":             dc.failedOver,
		"failover_count":          dc.failoverCount,
		"replication_mode":        dc.config.ReplicationMode,
		"enable_replication":      dc.config.EnableReplication,
	}

	if dc.failedOver {
		status["active_backend"] = "secondary"
		status["time_since_failover"] = time.Since(dc.lastFailoverAt).String()
	} else {
		status["active_backend"] = "primary"
	}

	if dc.config.ReplicationMode == constants.CheckpointReplicationModeAsync {
		status["replication_queue_size"] = len(dc.replicationQueue)
	}

	return status
}
