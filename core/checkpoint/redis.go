package checkpoint

import (
	"context"
	"errors"
	"github.com/kart-io/goagent/utils/json"
	"time"

	"github.com/redis/go-redis/v9"

	agentstate "github.com/kart-io/goagent/core/state"
	agentErrors "github.com/kart-io/goagent/errors"
)

// RedisCheckpointerConfig holds configuration for Redis checkpointer
type RedisCheckpointerConfig struct {
	// Addr is the Redis server address (host:port)
	Addr string

	// Password for Redis authentication
	Password string

	// DB is the Redis database number
	DB int

	// Prefix is the key prefix for all checkpoint keys
	Prefix string

	// TTL is the default time-to-live for checkpoints (0 = no expiration)
	TTL time.Duration

	// PoolSize is the maximum number of connections
	PoolSize int

	// MinIdleConns is the minimum number of idle connections
	MinIdleConns int

	// MaxRetries is the maximum number of retry attempts
	MaxRetries int

	// DialTimeout is the timeout for establishing connections
	DialTimeout time.Duration

	// ReadTimeout is the timeout for read operations
	ReadTimeout time.Duration

	// WriteTimeout is the timeout for write operations
	WriteTimeout time.Duration

	// EnableLock enables distributed locking for concurrent access
	EnableLock bool

	// LockTimeout is the timeout for acquiring locks
	LockTimeout time.Duration

	// LockExpiry is the expiry time for locks
	LockExpiry time.Duration
}

// DefaultRedisCheckpointerConfig returns default Redis checkpointer configuration
func DefaultRedisCheckpointerConfig() *RedisCheckpointerConfig {
	return &RedisCheckpointerConfig{
		Addr:         "localhost:6379",
		Password:     "",
		DB:           0,
		Prefix:       "agent:checkpoint:",
		TTL:          24 * time.Hour, // Default 24 hour expiration
		PoolSize:     10,
		MinIdleConns: 2,
		MaxRetries:   3,
		DialTimeout:  5 * time.Second,
		ReadTimeout:  3 * time.Second,
		WriteTimeout: 3 * time.Second,
		EnableLock:   true,
		LockTimeout:  5 * time.Second,
		LockExpiry:   10 * time.Second,
	}
}

// RedisCheckpointer is a Redis-backed implementation of the Checkpointer interface.
//
// Features:
//   - Distributed checkpoint storage
//   - Optional distributed locking for concurrent access
//   - Automatic expiration with TTL
//   - Connection pooling for high performance
//   - JSON serialization for state data
//
// Suitable for:
//   - Multi-instance deployments
//   - Production environments
//   - Distributed agent systems
//   - High-availability scenarios
type RedisCheckpointer struct {
	client *redis.Client
	config *RedisCheckpointerConfig
}

// checkpointData represents the checkpoint data stored in Redis
type checkpointData struct {
	State     map[string]interface{} `json:"state"`
	ThreadID  string                 `json:"thread_id"`
	CreatedAt time.Time              `json:"created_at"`
	UpdatedAt time.Time              `json:"updated_at"`
	Metadata  map[string]interface{} `json:"metadata"`
	StateSize int64                  `json:"state_size"`
}

// NewRedisCheckpointer creates a new Redis-backed checkpointer
func NewRedisCheckpointer(config *RedisCheckpointerConfig) (*RedisCheckpointer, error) {
	if config == nil {
		config = DefaultRedisCheckpointerConfig()
	}

	// Create Redis client
	client := redis.NewClient(&redis.Options{
		Addr:         config.Addr,
		Password:     config.Password,
		DB:           config.DB,
		PoolSize:     config.PoolSize,
		MinIdleConns: config.MinIdleConns,
		MaxRetries:   config.MaxRetries,
		DialTimeout:  config.DialTimeout,
		ReadTimeout:  config.ReadTimeout,
		WriteTimeout: config.WriteTimeout,
	})

	// Test connection
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := client.Ping(ctx).Err(); err != nil {
		return nil, agentErrors.NewStoreConnectionError("redis", config.Addr, err)
	}

	return &RedisCheckpointer{
		client: client,
		config: config,
	}, nil
}

// NewRedisCheckpointerFromClient creates a RedisCheckpointer from an existing client
func NewRedisCheckpointerFromClient(client *redis.Client, config *RedisCheckpointerConfig) *RedisCheckpointer {
	if config == nil {
		config = DefaultRedisCheckpointerConfig()
	}

	return &RedisCheckpointer{
		client: client,
		config: config,
	}
}

// Save persists the current state for a thread/session
func (c *RedisCheckpointer) Save(ctx context.Context, threadID string, state agentstate.State) error {
	key := c.makeKey(threadID)

	// Acquire lock if enabled
	if c.config.EnableLock {
		if err := c.acquireLock(ctx, threadID); err != nil {
			return agentErrors.Wrap(err, agentErrors.CodeDistributedCoordination, "failed to acquire lock").
				WithComponent("redis_checkpointer").
				WithOperation("save").
				WithContext("thread_id", threadID)
		}
		defer func() { _ = c.releaseLock(ctx, threadID) }()
	}

	// Get existing checkpoint to preserve created timestamp
	_, err := c.Load(ctx, threadID)
	now := time.Now()

	var createdAt time.Time
	if err == nil {
		// Extract created time from existing checkpoint
		createdAt = now // Fallback to now if we can't get it
		// Try to get created from checkpoint metadata
		if existingInfo, err := c.getCheckpointInfo(ctx, threadID); err == nil {
			createdAt = existingInfo.CreatedAt
		}
	} else {
		createdAt = now
	}

	// Create checkpoint data
	data := &checkpointData{
		State:     state.Snapshot(),
		ThreadID:  threadID,
		CreatedAt: createdAt,
		UpdatedAt: now,
		Metadata:  make(map[string]interface{}),
		StateSize: estimateStateSize(state),
	}

	// Serialize to JSON
	jsonData, err := json.Marshal(data)
	if err != nil {
		return agentErrors.Wrap(err, agentErrors.CodeDistributedSerialization, "failed to serialize checkpoint").
			WithComponent("redis_checkpointer").
			WithOperation("save").
			WithContext("thread_id", threadID)
	}

	// Store in Redis
	if c.config.TTL > 0 {
		err = c.client.Set(ctx, key, jsonData, c.config.TTL).Err()
	} else {
		err = c.client.Set(ctx, key, jsonData, 0).Err()
	}

	if err != nil {
		return agentErrors.Wrap(err, agentErrors.CodeStateCheckpoint, "failed to save checkpoint to Redis").
			WithComponent("redis_checkpointer").
			WithOperation("save").
			WithContext("thread_id", threadID)
	}

	return nil
}

// Load retrieves the saved state for a thread/session
func (c *RedisCheckpointer) Load(ctx context.Context, threadID string) (agentstate.State, error) {
	key := c.makeKey(threadID)

	// Get from Redis
	jsonData, err := c.client.Get(ctx, key).Bytes()
	if err != nil {
		if errors.Is(err, redis.Nil) {
			return nil, agentErrors.New(agentErrors.CodeStateLoad, "checkpoint not found").
				WithComponent("redis_checkpointer").
				WithOperation("load").
				WithContext("thread_id", threadID)
		}
		return nil, agentErrors.Wrap(err, agentErrors.CodeStateLoad, "failed to load checkpoint from Redis").
			WithComponent("redis_checkpointer").
			WithOperation("load").
			WithContext("thread_id", threadID)
	}

	// Deserialize
	var data checkpointData
	if err := json.Unmarshal(jsonData, &data); err != nil {
		return nil, agentErrors.Wrap(err, agentErrors.CodeDistributedSerialization, "failed to deserialize checkpoint").
			WithComponent("redis_checkpointer").
			WithOperation("load").
			WithContext("thread_id", threadID)
	}

	// Create state from snapshot
	state := agentstate.NewAgentState()
	for key, value := range data.State {
		state.Set(key, value)
	}

	return state, nil
}

// List returns information about all saved checkpoints
func (c *RedisCheckpointer) List(ctx context.Context) ([]CheckpointInfo, error) {
	pattern := c.config.Prefix + "*"
	keys, err := c.scanKeys(ctx, pattern)
	if err != nil {
		return nil, err
	}

	infos := make([]CheckpointInfo, 0, len(keys))

	for _, key := range keys {
		threadID := c.extractThreadID(key)
		info, err := c.getCheckpointInfo(ctx, threadID)
		if err != nil {
			continue // Skip invalid checkpoints
		}
		infos = append(infos, *info)
	}

	return infos, nil
}

// Delete removes the checkpoint for a thread/session
func (c *RedisCheckpointer) Delete(ctx context.Context, threadID string) error {
	key := c.makeKey(threadID)

	// Acquire lock if enabled
	if c.config.EnableLock {
		if err := c.acquireLock(ctx, threadID); err != nil {
			return agentErrors.Wrap(err, agentErrors.CodeDistributedCoordination, "failed to acquire lock").
				WithComponent("redis_checkpointer").
				WithOperation("delete").
				WithContext("thread_id", threadID)
		}
		defer func() { _ = c.releaseLock(ctx, threadID) }()
	}

	err := c.client.Del(ctx, key).Err()
	if err != nil {
		return agentErrors.Wrap(err, agentErrors.CodeStateCheckpoint, "failed to delete checkpoint from Redis").
			WithComponent("redis_checkpointer").
			WithOperation("delete").
			WithContext("thread_id", threadID)
	}

	return nil
}

// Exists checks if a checkpoint exists for a thread/session
func (c *RedisCheckpointer) Exists(ctx context.Context, threadID string) (bool, error) {
	key := c.makeKey(threadID)

	count, err := c.client.Exists(ctx, key).Result()
	if err != nil {
		return false, agentErrors.Wrap(err, agentErrors.CodeStateLoad, "failed to check checkpoint existence").
			WithComponent("redis_checkpointer").
			WithOperation("exists").
			WithContext("thread_id", threadID)
	}

	return count > 0, nil
}

// Close closes the Redis connection
func (c *RedisCheckpointer) Close() error {
	return c.client.Close()
}

// makeKey creates a Redis key for a thread ID
func (c *RedisCheckpointer) makeKey(threadID string) string {
	return c.config.Prefix + threadID
}

// extractThreadID extracts the thread ID from a Redis key
func (c *RedisCheckpointer) extractThreadID(key string) string {
	if len(key) <= len(c.config.Prefix) {
		return ""
	}
	return key[len(c.config.Prefix):]
}

// getCheckpointInfo retrieves checkpoint metadata
func (c *RedisCheckpointer) getCheckpointInfo(ctx context.Context, threadID string) (*CheckpointInfo, error) {
	key := c.makeKey(threadID)

	jsonData, err := c.client.Get(ctx, key).Bytes()
	if err != nil {
		return nil, err
	}

	var data checkpointData
	if err := json.Unmarshal(jsonData, &data); err != nil {
		return nil, err
	}

	return &CheckpointInfo{
		ThreadID:  threadID,
		CreatedAt: data.CreatedAt,
		UpdatedAt: data.UpdatedAt,
		Metadata:  data.Metadata,
		Size:      data.StateSize,
	}, nil
}

// scanKeys scans Redis keys matching the pattern
func (c *RedisCheckpointer) scanKeys(ctx context.Context, pattern string) ([]string, error) {
	var keys []string
	var cursor uint64

	for {
		var scanKeys []string
		var err error

		scanKeys, cursor, err = c.client.Scan(ctx, cursor, pattern, 100).Result()
		if err != nil {
			return nil, agentErrors.Wrap(err, agentErrors.CodeStateLoad, "failed to scan keys").
				WithComponent("redis_checkpointer").
				WithOperation("scan").
				WithContext("pattern", pattern)
		}

		keys = append(keys, scanKeys...)

		if cursor == 0 {
			break
		}
	}

	return keys, nil
}

// acquireLock acquires a distributed lock for a thread
func (c *RedisCheckpointer) acquireLock(ctx context.Context, threadID string) error {
	lockKey := c.config.Prefix + "lock:" + threadID

	// Use SET NX (set if not exists) with expiry
	deadline := time.Now().Add(c.config.LockTimeout)

	for time.Now().Before(deadline) {
		ok, err := c.client.SetNX(ctx, lockKey, "locked", c.config.LockExpiry).Result()
		if err != nil {
			return agentErrors.Wrap(err, agentErrors.CodeDistributedCoordination, "failed to acquire lock").
				WithComponent("redis_checkpointer").
				WithOperation("acquire_lock").
				WithContext("thread_id", threadID)
		}

		if ok {
			return nil
		}

		// Wait a bit before retrying
		time.Sleep(100 * time.Millisecond)
	}

	return agentErrors.New(agentErrors.CodeDistributedCoordination, "lock timeout").
		WithComponent("redis_checkpointer").
		WithOperation("acquire_lock").
		WithContext("thread_id", threadID).
		WithContext("lock_timeout", c.config.LockTimeout.String())
}

// releaseLock releases a distributed lock for a thread
func (c *RedisCheckpointer) releaseLock(ctx context.Context, threadID string) error {
	lockKey := c.config.Prefix + "lock:" + threadID
	return c.client.Del(ctx, lockKey).Err()
}

// Ping tests the connection to Redis
func (c *RedisCheckpointer) Ping(ctx context.Context) error {
	return c.client.Ping(ctx).Err()
}

// Size returns the number of checkpoints
func (c *RedisCheckpointer) Size(ctx context.Context) (int, error) {
	pattern := c.config.Prefix + "*"
	keys, err := c.scanKeys(ctx, pattern)
	if err != nil {
		return 0, err
	}

	// Filter out lock keys
	count := 0
	lockPrefix := c.config.Prefix + "lock:"
	for _, key := range keys {
		if len(key) < len(lockPrefix) || key[:len(lockPrefix)] != lockPrefix {
			count++
		}
	}

	return count, nil
}

// CleanupOld removes checkpoints older than the specified duration
func (c *RedisCheckpointer) CleanupOld(ctx context.Context, maxAge time.Duration) (int, error) {
	infos, err := c.List(ctx)
	if err != nil {
		return 0, err
	}

	now := time.Now()
	removed := 0

	for _, info := range infos {
		if now.Sub(info.UpdatedAt) > maxAge {
			if err := c.Delete(ctx, info.ThreadID); err == nil {
				removed++
			}
		}
	}

	return removed, nil
}
