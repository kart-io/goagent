package redis

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"

	agentErrors "github.com/kart-io/goagent/errors"
	"github.com/kart-io/goagent/store"
)

// Store is a Redis-backed implementation of the store.Store interface.
//
// Features:
//   - Connection pooling for high performance
//   - Namespace-based key organization
//   - Optional TTL for automatic expiration
//   - JSON serialization for complex values
//   - Thread-safe operations
//
// Suitable for:
//   - Production deployments
//   - Distributed systems
//   - High-throughput scenarios
//   - Shared state across services
type Store struct {
	client *redis.Client
	config *Config
}

// New creates a new Redis-backed store
func New(config *Config) (*Store, error) {
	if config == nil {
		config = DefaultConfig()
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

	return &Store{
		client: client,
		config: config,
	}, nil
}

// NewFromClient creates a Store from an existing client
func NewFromClient(client *redis.Client, config *Config) *Store {
	if config == nil {
		config = DefaultConfig()
	}

	return &Store{
		client: client,
		config: config,
	}
}

// Put stores a value with the given namespace and key
func (s *Store) Put(ctx context.Context, namespace []string, key string, value interface{}) error {
	redisKey := s.makeKey(namespace, key)

	// Get existing value to preserve created timestamp
	existing, err := s.Get(ctx, namespace, key)
	now := time.Now()

	var created time.Time
	var metadata map[string]interface{}

	if err == nil && existing != nil {
		created = existing.Created
		metadata = existing.Metadata
	} else {
		created = now
		metadata = make(map[string]interface{})
	}

	// Create store value
	storeValue := &store.Value{
		Value:     value,
		Metadata:  metadata,
		Created:   created,
		Updated:   now,
		Namespace: namespace,
		Key:       key,
	}

	// Serialize to JSON
	data, err := json.Marshal(storeValue)
	if err != nil {
		return agentErrors.Wrap(err, agentErrors.CodeStoreSerialization, "failed to serialize value").
			WithComponent("redis_store").
			WithOperation("put").
			WithContext("namespace", namespace).
			WithContext("key", key)
	}

	// Store in Redis
	if s.config.TTL > 0 {
		err = s.client.Set(ctx, redisKey, data, s.config.TTL).Err()
	} else {
		err = s.client.Set(ctx, redisKey, data, 0).Err()
	}

	if err != nil {
		return agentErrors.Wrap(err, agentErrors.CodeStoreConnection, "failed to store value in Redis").
			WithComponent("redis_store").
			WithOperation("put").
			WithContext("namespace", namespace).
			WithContext("key", key)
	}

	return nil
}

// Get retrieves a value by namespace and key
func (s *Store) Get(ctx context.Context, namespace []string, key string) (*store.Value, error) {
	redisKey := s.makeKey(namespace, key)

	// Get from Redis
	data, err := s.client.Get(ctx, redisKey).Bytes()
	if err != nil {
		if errors.Is(err, redis.Nil) {
			return nil, agentErrors.NewStoreNotFoundError(namespace, key)
		}
		return nil, agentErrors.Wrap(err, agentErrors.CodeStoreConnection, "failed to get value from Redis").
			WithComponent("redis_store").
			WithOperation("get").
			WithContext("namespace", namespace).
			WithContext("key", key)
	}

	// Deserialize
	var storeValue store.Value
	if err := json.Unmarshal(data, &storeValue); err != nil {
		return nil, agentErrors.Wrap(err, agentErrors.CodeStoreSerialization, "failed to deserialize value").
			WithComponent("redis_store").
			WithOperation("get").
			WithContext("namespace", namespace).
			WithContext("key", key)
	}

	return &storeValue, nil
}

// Delete removes a value by namespace and key
func (s *Store) Delete(ctx context.Context, namespace []string, key string) error {
	redisKey := s.makeKey(namespace, key)

	err := s.client.Del(ctx, redisKey).Err()
	if err != nil {
		return agentErrors.Wrap(err, agentErrors.CodeStoreConnection, "failed to delete key from Redis").
			WithComponent("redis_store").
			WithOperation("delete").
			WithContext("namespace", namespace).
			WithContext("key", key)
	}

	return nil
}

// Search finds values matching the filter within a namespace
func (s *Store) Search(ctx context.Context, namespace []string, filter map[string]interface{}) ([]*store.Value, error) {
	// Get all keys in namespace
	pattern := s.makePattern(namespace)
	keys, err := s.scanKeys(ctx, pattern)
	if err != nil {
		return nil, err
	}

	results := make([]*store.Value, 0)

	// Iterate through keys and filter
	for _, redisKey := range keys {
		data, err := s.client.Get(ctx, redisKey).Bytes()
		if err != nil {
			continue // Skip keys that can't be read
		}

		var storeValue store.Value
		if err := json.Unmarshal(data, &storeValue); err != nil {
			continue // Skip invalid data
		}

		// Apply filter
		if matchesFilter(&storeValue, filter) {
			results = append(results, &storeValue)
		}
	}

	return results, nil
}

// List returns all keys within a namespace
func (s *Store) List(ctx context.Context, namespace []string) ([]string, error) {
	pattern := s.makePattern(namespace)
	redisKeys, err := s.scanKeys(ctx, pattern)
	if err != nil {
		return nil, err
	}

	// Extract actual keys from Redis keys
	keys := make([]string, 0, len(redisKeys))
	for _, redisKey := range redisKeys {
		// Remove prefix and namespace to get the key
		key := s.extractKey(namespace, redisKey)
		if key != "" {
			keys = append(keys, key)
		}
	}

	return keys, nil
}

// Clear removes all values within a namespace
func (s *Store) Clear(ctx context.Context, namespace []string) error {
	pattern := s.makePattern(namespace)
	keys, err := s.scanKeys(ctx, pattern)
	if err != nil {
		return err
	}

	if len(keys) == 0 {
		return nil
	}

	// Delete all keys in batch
	err = s.client.Del(ctx, keys...).Err()
	if err != nil {
		return agentErrors.Wrap(err, agentErrors.CodeStoreConnection, "failed to clear namespace").
			WithComponent("redis_store").
			WithOperation("clear").
			WithContext("namespace", namespace)
	}

	return nil
}

// Close closes the Redis connection
func (s *Store) Close() error {
	return s.client.Close()
}

// makeKey creates a Redis key from namespace and key
func (s *Store) makeKey(namespace []string, key string) string {
	nsKey := s.config.Prefix + namespaceToKey(namespace)
	if !strings.HasSuffix(nsKey, "/") {
		nsKey += "/"
	}
	return nsKey + key
}

// makePattern creates a Redis pattern for scanning namespace keys
func (s *Store) makePattern(namespace []string) string {
	nsKey := s.config.Prefix + namespaceToKey(namespace)
	if !strings.HasSuffix(nsKey, "/") {
		nsKey += "/"
	}
	return nsKey + "*"
}

// extractKey extracts the key part from a full Redis key
func (s *Store) extractKey(namespace []string, redisKey string) string {
	prefix := s.config.Prefix + namespaceToKey(namespace)
	if !strings.HasSuffix(prefix, "/") {
		prefix += "/"
	}

	if !strings.HasPrefix(redisKey, prefix) {
		return ""
	}

	return strings.TrimPrefix(redisKey, prefix)
}

// scanKeys scans Redis keys matching the pattern
func (s *Store) scanKeys(ctx context.Context, pattern string) ([]string, error) {
	var keys []string
	var cursor uint64

	for {
		var scanKeys []string
		var err error

		scanKeys, cursor, err = s.client.Scan(ctx, cursor, pattern, 100).Result()
		if err != nil {
			return nil, agentErrors.Wrap(err, agentErrors.CodeStoreConnection, "failed to scan keys").
				WithComponent("redis_store").
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

// Ping tests the connection to Redis
func (s *Store) Ping(ctx context.Context) error {
	return s.client.Ping(ctx).Err()
}

// Size returns the approximate number of keys in all namespaces
func (s *Store) Size(ctx context.Context) (int, error) {
	pattern := s.config.Prefix + "*"
	keys, err := s.scanKeys(ctx, pattern)
	if err != nil {
		return 0, err
	}
	return len(keys), nil
}

// namespaceToKey converts a namespace slice to a string key.
func namespaceToKey(namespace []string) string {
	if len(namespace) == 0 {
		return "/"
	}
	return "/" + joinNamespace(namespace)
}

// joinNamespace joins namespace components with "/".
func joinNamespace(namespace []string) string {
	if len(namespace) == 0 {
		return ""
	}
	result := namespace[0]
	for i := 1; i < len(namespace); i++ {
		result += "/" + namespace[i]
	}
	return result
}

// matchesFilter checks if a store.Value matches the given filter.
func matchesFilter(value *store.Value, filter map[string]interface{}) bool {
	if len(filter) == 0 {
		return true
	}

	for key, filterValue := range filter {
		metaValue, ok := value.Metadata[key]
		if !ok {
			return false
		}
		if metaValue != filterValue {
			return false
		}
	}

	return true
}
