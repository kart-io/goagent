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

	// Postgres is used when Type is Postgres
	Postgres *postgres.Config

	// Redis is used when Type is Redis
	Redis *redis.Config
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
		if config.Postgres == nil {
			config.Postgres = postgres.DefaultConfig()
		}

		opts := []postgres.PostgresOption{}
		cfg := config.Postgres

		if cfg.TableName != "" {
			opts = append(opts, postgres.WithTableName(cfg.TableName))
		}
		if cfg.MaxOpenConns > 0 {
			opts = append(opts, postgres.WithMaxOpenConns(cfg.MaxOpenConns))
		}
		if cfg.MaxIdleConns > 0 {
			opts = append(opts, postgres.WithMaxIdleConns(cfg.MaxIdleConns))
		}
		if cfg.ConnMaxLifetime > 0 {
			opts = append(opts, postgres.WithConnMaxLifetime(cfg.ConnMaxLifetime))
		}
		opts = append(opts, postgres.WithLogLevel(cfg.LogLevel))
		opts = append(opts, postgres.WithAutoMigrate(cfg.AutoMigrate))

		return postgres.New(cfg.DSN, opts...)

	case Redis:
		if config.Redis == nil {
			config.Redis = redis.DefaultConfig()
		}

		opts := []redis.RedisOption{}
		cfg := config.Redis

		if cfg.Password != "" {
			opts = append(opts, redis.WithPassword(cfg.Password))
		}
		if cfg.DB != 0 {
			opts = append(opts, redis.WithDB(cfg.DB))
		}
		if cfg.Prefix != "" {
			opts = append(opts, redis.WithPrefix(cfg.Prefix))
		}
		if cfg.TTL > 0 {
			opts = append(opts, redis.WithTTL(cfg.TTL))
		}
		if cfg.PoolSize > 0 {
			opts = append(opts, redis.WithPoolSize(cfg.PoolSize))
		}
		if cfg.MinIdleConns > 0 {
			opts = append(opts, redis.WithMinIdleConns(cfg.MinIdleConns))
		}
		if cfg.MaxRetries > 0 {
			opts = append(opts, redis.WithMaxRetries(cfg.MaxRetries))
		}
		if cfg.DialTimeout > 0 {
			opts = append(opts, redis.WithDialTimeout(cfg.DialTimeout))
		}
		if cfg.ReadTimeout > 0 {
			opts = append(opts, redis.WithReadTimeout(cfg.ReadTimeout))
		}
		if cfg.WriteTimeout > 0 {
			opts = append(opts, redis.WithWriteTimeout(cfg.WriteTimeout))
		}

		return redis.New(cfg.Addr, opts...)

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
	if config == nil {
		config = postgres.DefaultConfig()
	}

	opts := []postgres.PostgresOption{}

	if config.TableName != "" {
		opts = append(opts, postgres.WithTableName(config.TableName))
	}
	if config.MaxOpenConns > 0 {
		opts = append(opts, postgres.WithMaxOpenConns(config.MaxOpenConns))
	}
	if config.MaxIdleConns > 0 {
		opts = append(opts, postgres.WithMaxIdleConns(config.MaxIdleConns))
	}
	if config.ConnMaxLifetime > 0 {
		opts = append(opts, postgres.WithConnMaxLifetime(config.ConnMaxLifetime))
	}
	opts = append(opts, postgres.WithLogLevel(config.LogLevel))
	opts = append(opts, postgres.WithAutoMigrate(config.AutoMigrate))

	return postgres.New(config.DSN, opts...)
}

// NewRedisStore creates a new Redis store with the given config
func NewRedisStore(config *redis.Config) (store.Store, error) {
	if config == nil {
		config = redis.DefaultConfig()
	}

	opts := []redis.RedisOption{}

	if config.Password != "" {
		opts = append(opts, redis.WithPassword(config.Password))
	}
	if config.DB != 0 {
		opts = append(opts, redis.WithDB(config.DB))
	}
	if config.Prefix != "" {
		opts = append(opts, redis.WithPrefix(config.Prefix))
	}
	if config.TTL > 0 {
		opts = append(opts, redis.WithTTL(config.TTL))
	}
	if config.PoolSize > 0 {
		opts = append(opts, redis.WithPoolSize(config.PoolSize))
	}
	if config.MinIdleConns > 0 {
		opts = append(opts, redis.WithMinIdleConns(config.MinIdleConns))
	}
	if config.MaxRetries > 0 {
		opts = append(opts, redis.WithMaxRetries(config.MaxRetries))
	}
	if config.DialTimeout > 0 {
		opts = append(opts, redis.WithDialTimeout(config.DialTimeout))
	}
	if config.ReadTimeout > 0 {
		opts = append(opts, redis.WithReadTimeout(config.ReadTimeout))
	}
	if config.WriteTimeout > 0 {
		opts = append(opts, redis.WithWriteTimeout(config.WriteTimeout))
	}

	return redis.New(config.Addr, opts...)
}
