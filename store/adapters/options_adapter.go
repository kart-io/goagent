// Package adapters provides adapters to integrate store with common/options
package adapters

import (
	"fmt"

	"gorm.io/gorm/logger"

	"github.com/kart-io/goagent/store"
	"github.com/kart-io/goagent/store/factory"
	"github.com/kart-io/goagent/store/memory"
	"github.com/kart-io/goagent/store/postgres"
	"github.com/kart-io/goagent/store/redis"
	"github.com/kart-io/k8s-agent/common/options"
)

// StoreOptions extends common options with store-specific settings
type StoreOptions struct {
	Type     string                   `mapstructure:"type" yaml:"type" json:"type"` // "memory", "redis", "postgres", "mysql"
	Redis    *options.RedisOptions    `mapstructure:"redis" yaml:"redis" json:"redis"`
	MySQL    *options.MySQLOptions    `mapstructure:"mysql" yaml:"mysql" json:"mysql"`
	Postgres *options.PostgresOptions `mapstructure:"postgres" yaml:"postgres" json:"postgres"`

	// Store-specific settings
	Prefix    string `mapstructure:"prefix" yaml:"prefix" json:"prefix"`             // Key prefix for namespacing
	TableName string `mapstructure:"table_name" yaml:"table_name" json:"table_name"` // Table name for SQL stores
}

// NewStoreOptions creates default store options
func NewStoreOptions() *StoreOptions {
	return &StoreOptions{
		Type:      "memory",
		Redis:     options.NewRedisOptions(),
		MySQL:     options.NewMySQLOptions(),
		Postgres:  options.NewPostgresOptions(),
		Prefix:    "agent:store:",
		TableName: "agent_stores",
	}
}

// NewStore creates a store instance from common options
func NewStore(opts *StoreOptions) (store.Store, error) {
	switch opts.Type {
	case "memory":
		return memory.New(), nil

	case "redis":
		if opts.Redis == nil {
			return nil, fmt.Errorf("redis options are required for redis store")
		}

		// Validate Redis options using common validation
		if err := opts.Redis.Validate(); err != nil {
			return nil, fmt.Errorf("invalid redis options: %w", err)
		}

		// Complete Redis options (set defaults)
		if err := opts.Redis.Complete(); err != nil {
			return nil, fmt.Errorf("failed to complete redis options: %w", err)
		}

		// Convert common RedisOptions to store redis.Config
		config := &redis.Config{
			Addr:         opts.Redis.Addr,
			Password:     opts.Redis.Password,
			DB:           opts.Redis.DB,
			Prefix:       opts.Prefix,
			TTL:          0, // Store-specific, not in common options
			PoolSize:     opts.Redis.PoolSize,
			MinIdleConns: opts.Redis.MinIdleConns,
			MaxRetries:   3, // Default, not in common options
			DialTimeout:  opts.Redis.DialTimeout,
			ReadTimeout:  opts.Redis.ReadTimeout,
			WriteTimeout: opts.Redis.WriteTimeout,
		}

		return redis.New(config)

	case "mysql":
		if opts.MySQL == nil {
			return nil, fmt.Errorf("mysql options are required for mysql store")
		}

		// Validate MySQL options
		if err := opts.MySQL.Validate(); err != nil {
			return nil, fmt.Errorf("invalid mysql options: %w", err)
		}

		// MySQL store uses the same implementation as PostgreSQL with different driver
		// Note: This requires updating postgres store to support MySQL driver
		// For now, return error
		return nil, fmt.Errorf("mysql store not yet implemented, use postgres instead")

	case "postgres":
		if opts.Postgres == nil {
			return nil, fmt.Errorf("postgres options are required for postgres store")
		}

		// Validate Postgres options
		if err := opts.Postgres.Validate(); err != nil {
			return nil, fmt.Errorf("invalid postgres options: %w", err)
		}

		// Complete Postgres options (set defaults)
		if err := opts.Postgres.Complete(); err != nil {
			return nil, fmt.Errorf("failed to complete postgres options: %w", err)
		}

		// Use DSN from PostgresOptions
		config := &postgres.Config{
			DSN:             opts.Postgres.DSN(),
			TableName:       opts.TableName,
			MaxIdleConns:    opts.Postgres.MaxIdleConns,
			MaxOpenConns:    opts.Postgres.MaxOpenConns,
			ConnMaxLifetime: opts.Postgres.ConnMaxLifetime,
			LogLevel:        convertLogLevel(opts.Postgres.LogLevel),
			AutoMigrate:     opts.Postgres.AutoMigrate,
		}

		return postgres.New(config)

	default:
		return nil, fmt.Errorf("unsupported store type: %s", opts.Type)
	}
}

// NewStoreFromFactory creates a store using the factory pattern with common options
func NewStoreFromFactory(opts *StoreOptions) (store.Store, error) {
	// Convert to factory config
	factoryConfig := &factory.Config{
		Type: factory.StoreType(opts.Type),
	}

	switch opts.Type {
	case "memory":
		// No additional config needed for memory store

	case "redis":
		if opts.Redis == nil {
			return nil, fmt.Errorf("redis options are required")
		}

		// Validate and complete
		if err := opts.Redis.Validate(); err != nil {
			return nil, err
		}
		if err := opts.Redis.Complete(); err != nil {
			return nil, err
		}

		factoryConfig.RedisConfig = &redis.Config{
			Addr:         opts.Redis.Addr,
			Password:     opts.Redis.Password,
			DB:           opts.Redis.DB,
			Prefix:       opts.Prefix,
			PoolSize:     opts.Redis.PoolSize,
			MinIdleConns: opts.Redis.MinIdleConns,
			DialTimeout:  opts.Redis.DialTimeout,
			ReadTimeout:  opts.Redis.ReadTimeout,
			WriteTimeout: opts.Redis.WriteTimeout,
		}

	case "postgres":
		if opts.Postgres == nil {
			return nil, fmt.Errorf("postgres options are required")
		}

		// Validate and complete
		if err := opts.Postgres.Validate(); err != nil {
			return nil, err
		}
		if err := opts.Postgres.Complete(); err != nil {
			return nil, err
		}

		factoryConfig.PostgresConfig = &postgres.Config{
			DSN:             opts.Postgres.DSN(),
			TableName:       opts.TableName,
			MaxIdleConns:    opts.Postgres.MaxIdleConns,
			MaxOpenConns:    opts.Postgres.MaxOpenConns,
			ConnMaxLifetime: opts.Postgres.ConnMaxLifetime,
			LogLevel:        convertLogLevel(opts.Postgres.LogLevel),
			AutoMigrate:     opts.Postgres.AutoMigrate,
		}

	default:
		return nil, fmt.Errorf("unsupported store type: %s", opts.Type)
	}

	return factory.NewStore(factoryConfig)
}

// convertLogLevel converts string log level to gorm logger.LogLevel
func convertLogLevel(level string) logger.LogLevel {
	switch level {
	case "silent":
		return logger.Silent
	case "error":
		return logger.Error
	case "warn":
		return logger.Warn
	case "info":
		return logger.Info
	default:
		return logger.Silent
	}
}

// RedisStoreAdapter adapts RedisOptions to create a Redis store
type RedisStoreAdapter struct {
	options *options.RedisOptions
	prefix  string
}

// NewRedisStoreAdapter creates a new Redis store adapter
func NewRedisStoreAdapter(opts *options.RedisOptions, prefix string) *RedisStoreAdapter {
	if prefix == "" {
		prefix = "agent:store:"
	}
	return &RedisStoreAdapter{
		options: opts,
		prefix:  prefix,
	}
}

// CreateStore creates a Redis store from common RedisOptions
func (a *RedisStoreAdapter) CreateStore() (store.Store, error) {
	// Validate options
	if err := a.options.Validate(); err != nil {
		return nil, fmt.Errorf("invalid redis options: %w", err)
	}

	// Complete options
	if err := a.options.Complete(); err != nil {
		return nil, fmt.Errorf("failed to complete redis options: %w", err)
	}

	// Create config
	config := &redis.Config{
		Addr:         a.options.Addr,
		Password:     a.options.Password,
		DB:           a.options.DB,
		Prefix:       a.prefix,
		TTL:          0,
		PoolSize:     a.options.PoolSize,
		MinIdleConns: a.options.MinIdleConns,
		MaxRetries:   3,
		DialTimeout:  a.options.DialTimeout,
		ReadTimeout:  a.options.ReadTimeout,
		WriteTimeout: a.options.WriteTimeout,
	}

	return redis.New(config)
}

// MySQLStoreAdapter adapts MySQLOptions to create a MySQL-backed store
type MySQLStoreAdapter struct {
	options   *options.MySQLOptions
	tableName string
}

// NewMySQLStoreAdapter creates a new MySQL store adapter
func NewMySQLStoreAdapter(opts *options.MySQLOptions, tableName string) *MySQLStoreAdapter {
	if tableName == "" {
		tableName = "agent_stores"
	}
	return &MySQLStoreAdapter{
		options:   opts,
		tableName: tableName,
	}
}

// CreateStore creates a store backed by MySQL
// Note: Currently uses PostgreSQL implementation with MySQL DSN
func (a *MySQLStoreAdapter) CreateStore() (store.Store, error) {
	// Validate options
	if err := a.options.Validate(); err != nil {
		return nil, fmt.Errorf("invalid mysql options: %w", err)
	}

	// Create config using PostgreSQL store with MySQL DSN
	config := &postgres.Config{
		DSN:             a.options.DSN(),
		TableName:       a.tableName,
		MaxIdleConns:    a.options.MaxIdleConns,
		MaxOpenConns:    a.options.MaxOpenConns,
		ConnMaxLifetime: a.options.ConnMaxLifetime,
		LogLevel:        convertLogLevel(a.options.LogLevel),
		AutoMigrate:     a.options.AutoMigrate,
	}

	// TODO: Implement MySQL-specific store or update postgres store to support MySQL
	_ = config
	return nil, fmt.Errorf("mysql store adapter not yet fully implemented")
}
