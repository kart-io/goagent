// Package adapters provides adapters to integrate store with common/options
package adapters

import (
	"gorm.io/gorm/logger"

	agentErrors "github.com/kart-io/goagent/errors"
	"github.com/kart-io/goagent/options"
	"github.com/kart-io/goagent/store"
	"github.com/kart-io/goagent/store/factory"
	"github.com/kart-io/goagent/store/memory"
	"github.com/kart-io/goagent/store/postgres"
	"github.com/kart-io/goagent/store/redis"
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
			return nil, agentErrors.NewInvalidConfigError("options_adapter", "redis", "redis options are required for redis store").
				WithComponent("options_adapter").
				WithOperation("new_store")
		}

		// Validate Redis options using common validation
		if err := opts.Redis.Validate(); err != nil {
			return nil, agentErrors.Wrap(err, agentErrors.CodeInvalidConfig, "invalid redis options").
				WithComponent("options_adapter").
				WithOperation("new_store")
		}

		// Complete Redis options (set defaults)
		if err := opts.Redis.Complete(); err != nil {
			return nil, agentErrors.Wrap(err, agentErrors.CodeInvalidConfig, "failed to complete redis options").
				WithComponent("options_adapter").
				WithOperation("new_store")
		}

		// Convert common RedisOptions to store redis options
		redisOpts := []redis.RedisOption{}
		if opts.Redis.Password != "" {
			redisOpts = append(redisOpts, redis.WithPassword(opts.Redis.Password))
		}
		if opts.Redis.DB != 0 {
			redisOpts = append(redisOpts, redis.WithDB(opts.Redis.DB))
		}
		if opts.Prefix != "" {
			redisOpts = append(redisOpts, redis.WithPrefix(opts.Prefix))
		}
		if opts.Redis.TTL > 0 {
			redisOpts = append(redisOpts, redis.WithTTL(opts.Redis.TTL))
		}
		if opts.Redis.PoolSize > 0 {
			redisOpts = append(redisOpts, redis.WithPoolSize(opts.Redis.PoolSize))
		}
		if opts.Redis.MinIdleConns > 0 {
			redisOpts = append(redisOpts, redis.WithMinIdleConns(opts.Redis.MinIdleConns))
		}
		if opts.Redis.MaxRetries > 0 {
			redisOpts = append(redisOpts, redis.WithMaxRetries(opts.Redis.MaxRetries))
		}
		if opts.Redis.DialTimeout > 0 {
			redisOpts = append(redisOpts, redis.WithDialTimeout(opts.Redis.DialTimeout))
		}
		if opts.Redis.ReadTimeout > 0 {
			redisOpts = append(redisOpts, redis.WithReadTimeout(opts.Redis.ReadTimeout))
		}
		if opts.Redis.WriteTimeout > 0 {
			redisOpts = append(redisOpts, redis.WithWriteTimeout(opts.Redis.WriteTimeout))
		}

		return redis.New(opts.Redis.Addr, redisOpts...)

	case "mysql":
		if opts.MySQL == nil {
			return nil, agentErrors.NewInvalidConfigError("options_adapter", "mysql", "mysql options are required for mysql store").
				WithComponent("options_adapter").
				WithOperation("new_store")
		}

		// Validate MySQL options
		if err := opts.MySQL.Validate(); err != nil {
			return nil, agentErrors.Wrap(err, agentErrors.CodeInvalidConfig, "invalid mysql options").
				WithComponent("options_adapter").
				WithOperation("new_store")
		}

		// MySQL store uses the same implementation as PostgreSQL with different driver
		// Note: This requires updating postgres store to support MySQL driver
		// For now, return error
		return nil, agentErrors.New(agentErrors.CodeNotImplemented, "mysql store not yet implemented, use postgres instead").
			WithComponent("options_adapter").
			WithOperation("new_store")

	case "postgres":
		if opts.Postgres == nil {
			return nil, agentErrors.NewInvalidConfigError("options_adapter", "postgres", "postgres options are required for postgres store").
				WithComponent("options_adapter").
				WithOperation("new_store")
		}

		// Validate Postgres options
		if err := opts.Postgres.Validate(); err != nil {
			return nil, agentErrors.Wrap(err, agentErrors.CodeInvalidConfig, "invalid postgres options").
				WithComponent("options_adapter").
				WithOperation("new_store")
		}

		// Complete Postgres options (set defaults)
		if err := opts.Postgres.Complete(); err != nil {
			return nil, agentErrors.Wrap(err, agentErrors.CodeInvalidConfig, "failed to complete postgres options").
				WithComponent("options_adapter").
				WithOperation("new_store")
		}

		// Use DSN from PostgresOptions
		postgresOpts := []postgres.PostgresOption{}
		if opts.TableName != "" {
			postgresOpts = append(postgresOpts, postgres.WithTableName(opts.TableName))
		}
		if opts.Postgres.MaxOpenConns > 0 {
			postgresOpts = append(postgresOpts, postgres.WithMaxOpenConns(opts.Postgres.MaxOpenConns))
		}
		if opts.Postgres.MaxIdleConns > 0 {
			postgresOpts = append(postgresOpts, postgres.WithMaxIdleConns(opts.Postgres.MaxIdleConns))
		}
		if opts.Postgres.ConnMaxLifetime > 0 {
			postgresOpts = append(postgresOpts, postgres.WithConnMaxLifetime(opts.Postgres.ConnMaxLifetime))
		}
		postgresOpts = append(postgresOpts, postgres.WithLogLevel(convertLogLevel(opts.Postgres.LogLevel)))
		postgresOpts = append(postgresOpts, postgres.WithAutoMigrate(opts.Postgres.AutoMigrate))

		return postgres.New(opts.Postgres.DSN(), postgresOpts...)

	default:
		return nil, agentErrors.NewInvalidConfigError("options_adapter", "type", "unsupported store type").
			WithComponent("options_adapter").
			WithOperation("new_store").
			WithContext("store_type", opts.Type)
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
			return nil, agentErrors.NewInvalidConfigError("options_adapter", "redis", "redis options are required for redis store").
				WithComponent("options_adapter").
				WithOperation("new_store_from_factory")
		}

		// Validate and complete
		if err := opts.Redis.Validate(); err != nil {
			return nil, err
		}
		if err := opts.Redis.Complete(); err != nil {
			return nil, err
		}

		factoryConfig.Redis = &redis.Config{
			Addr:         opts.Redis.Addr,
			Password:     opts.Redis.Password,
			DB:           opts.Redis.DB,
			Prefix:       opts.Prefix,
			TTL:          opts.Redis.TTL,
			PoolSize:     opts.Redis.PoolSize,
			MinIdleConns: opts.Redis.MinIdleConns,
			MaxRetries:   opts.Redis.MaxRetries,
			DialTimeout:  opts.Redis.DialTimeout,
			ReadTimeout:  opts.Redis.ReadTimeout,
			WriteTimeout: opts.Redis.WriteTimeout,
		}

	case "postgres":
		if opts.Postgres == nil {
			return nil, agentErrors.NewInvalidConfigError("options_adapter", "postgres", "postgres options are required for postgres store").
				WithComponent("options_adapter").
				WithOperation("new_store_from_factory")
		}

		// Validate and complete
		if err := opts.Postgres.Validate(); err != nil {
			return nil, err
		}
		if err := opts.Postgres.Complete(); err != nil {
			return nil, err
		}

		factoryConfig.Postgres = &postgres.Config{
			DSN:             opts.Postgres.DSN(),
			TableName:       opts.TableName,
			MaxIdleConns:    opts.Postgres.MaxIdleConns,
			MaxOpenConns:    opts.Postgres.MaxOpenConns,
			ConnMaxLifetime: opts.Postgres.ConnMaxLifetime,
			LogLevel:        convertLogLevel(opts.Postgres.LogLevel),
			AutoMigrate:     opts.Postgres.AutoMigrate,
		}

	default:
		return nil, agentErrors.NewInvalidConfigError("options_adapter", "type", "unsupported store type").
			WithComponent("options_adapter").
			WithOperation("new_store_from_factory").
			WithContext("store_type", opts.Type)
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
		return nil, agentErrors.Wrap(err, agentErrors.CodeInvalidConfig, "invalid redis options").
			WithComponent("options_adapter").
			WithOperation("create_redis_store")
	}

	// Complete options
	if err := a.options.Complete(); err != nil {
		return nil, agentErrors.Wrap(err, agentErrors.CodeInvalidConfig, "failed to complete redis options").
			WithComponent("options_adapter").
			WithOperation("create_redis_store")
	}

	// Create options
	redisOpts := []redis.RedisOption{}
	if a.options.Password != "" {
		redisOpts = append(redisOpts, redis.WithPassword(a.options.Password))
	}
	if a.options.DB != 0 {
		redisOpts = append(redisOpts, redis.WithDB(a.options.DB))
	}
	if a.prefix != "" {
		redisOpts = append(redisOpts, redis.WithPrefix(a.prefix))
	}
	if a.options.TTL > 0 {
		redisOpts = append(redisOpts, redis.WithTTL(a.options.TTL))
	}
	if a.options.PoolSize > 0 {
		redisOpts = append(redisOpts, redis.WithPoolSize(a.options.PoolSize))
	}
	if a.options.MinIdleConns > 0 {
		redisOpts = append(redisOpts, redis.WithMinIdleConns(a.options.MinIdleConns))
	}
	if a.options.MaxRetries > 0 {
		redisOpts = append(redisOpts, redis.WithMaxRetries(a.options.MaxRetries))
	}
	if a.options.DialTimeout > 0 {
		redisOpts = append(redisOpts, redis.WithDialTimeout(a.options.DialTimeout))
	}
	if a.options.ReadTimeout > 0 {
		redisOpts = append(redisOpts, redis.WithReadTimeout(a.options.ReadTimeout))
	}
	if a.options.WriteTimeout > 0 {
		redisOpts = append(redisOpts, redis.WithWriteTimeout(a.options.WriteTimeout))
	}

	return redis.New(a.options.Addr, redisOpts...)
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
		return nil, agentErrors.Wrap(err, agentErrors.CodeInvalidConfig, "invalid mysql options").
			WithComponent("options_adapter").
			WithOperation("create_mysql_store")
	}

	// MySQL store 暂不支持，项目无实现计划
	return nil, agentErrors.New(agentErrors.CodeNotImplemented, "mysql store not supported").
		WithComponent("options_adapter").
		WithOperation("create_mysql_store")
}
