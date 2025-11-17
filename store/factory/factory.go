package factory

import (
	agentErrors "github.com/kart-io/goagent/errors"
	"github.com/kart-io/goagent/store"
	"github.com/kart-io/goagent/store/memory"
	"github.com/kart-io/goagent/store/postgres"
	"github.com/kart-io/goagent/store/redis"
)

// StoreType represents the type of store to create
type StoreType string

const (
	// Memory creates an in-memory store
	Memory StoreType = "memory"
	// Postgres creates a PostgreSQL store
	Postgres StoreType = "postgres"
	// Redis creates a Redis store
	Redis StoreType = "redis"
)

// Config holds configuration for creating a store
type Config struct {
	// Type specifies which store implementation to use
	Type StoreType

	// PostgresConfig is used when Type is Postgres
	PostgresConfig *postgres.Config

	// RedisConfig is used when Type is Redis
	RedisConfig *redis.Config
}

// NewStore creates a new store based on the configuration
func NewStore(config *Config) (store.Store, error) {
	if config == nil {
		return nil, agentErrors.NewInvalidConfigError("store_factory", "config", "config cannot be nil")
	}

	switch config.Type {
	case Memory:
		return memory.New(), nil

	case Postgres:
		if config.PostgresConfig == nil {
			config.PostgresConfig = postgres.DefaultConfig()
		}
		return postgres.New(config.PostgresConfig)

	case Redis:
		if config.RedisConfig == nil {
			config.RedisConfig = redis.DefaultConfig()
		}
		return redis.New(config.RedisConfig)

	default:
		return nil, agentErrors.NewInvalidConfigError("store_factory", "type", "unknown store type").
			WithContext("store_type", string(config.Type))
	}
}

// NewMemoryStore creates a new in-memory store
func NewMemoryStore() store.Store {
	return memory.New()
}

// NewPostgresStore creates a new PostgreSQL store with the given config
func NewPostgresStore(config *postgres.Config) (store.Store, error) {
	return postgres.New(config)
}

// NewRedisStore creates a new Redis store with the given config
func NewRedisStore(config *redis.Config) (store.Store, error) {
	return redis.New(config)
}
