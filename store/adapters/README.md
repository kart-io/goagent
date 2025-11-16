# Store Options Adapter

This package provides adapters to integrate the `store` package with `common/options` configuration system.

## Overview

The adapter allows you to:

- Use standardized `common/options` configurations for store backends
- Leverage built-in validation, command-line flags, and health checks
- Maintain consistent configuration across the application
- Easily switch between different store backends based on environment

## Features

### 1. Unified Configuration

The `StoreOptions` struct provides a unified configuration interface that integrates with `common/options`:

```go
type StoreOptions struct {
    Type     string                // "memory", "redis", "postgres", "mysql"
    Redis    *options.RedisOptions // From common/options
    MySQL    *options.MySQLOptions // From common/options
    Postgres *PostgresOptions      // Extended from MySQLOptions

    // Store-specific settings
    Prefix    string // Key prefix for namespacing
    TableName string // Table name for SQL stores
}
```

### 2. Configuration Validation

All configurations are validated using `common/options` validation rules:

- Port number validation
- Connection pool validation
- Timeout validation
- Required field validation

### 3. Multiple Usage Patterns

#### Pattern 1: Direct Adapter Usage

```go
// Use RedisOptions from common package
redisOpts := options.NewRedisOptions()
redisOpts.Addr = "localhost:6379"

// Create adapter with custom prefix
adapter := NewRedisStoreAdapter(redisOpts, "myapp:")

// Create store
store, err := adapter.CreateStore()
```

#### Pattern 2: Unified Store Options

```go
// Create unified options
opts := NewStoreOptions()
opts.Type = "redis"
opts.Redis.Addr = "localhost:6379"

// Create store with automatic validation
store, err := NewStore(opts)
```

#### Pattern 3: Environment-Based Configuration

```go
func CreateStore(env string) (store.Store, error) {
    opts := NewStoreOptions()

    switch env {
    case "development":
        opts.Type = "memory"
    case "production":
        opts.Type = "redis"
        opts.Redis.Addr = os.Getenv("REDIS_ADDR")
    default:
        opts.Type = "postgres"
        // Configure postgres...
    }

    return NewStore(opts)
}
```

## Store Types

### Memory Store

- No external dependencies
- Suitable for development and testing
- Data is not persisted

### Redis Store

- Uses `common/options.RedisOptions`
- Supports connection pooling
- Includes health checks
- Suitable for distributed systems

### PostgreSQL Store

- Extended configuration based on MySQLOptions
- Supports automatic migration
- JSONB storage for flexible data types
- Connection pooling

### MySQL Store (Coming Soon)

- Will use `common/options.MySQLOptions`
- Currently returns "not implemented" error
- Planned for future release

## Configuration Sources

The adapter supports multiple configuration sources:

### 1. Command-Line Flags

```go
fs := pflag.NewFlagSet("app", pflag.ContinueOnError)
redisOpts.AddFlags(fs)  // Adds --redis.addr, --redis.password, etc.
fs.Parse(os.Args[1:])
```

### 2. Configuration Files (via Viper)

```yaml
store:
  type: redis
  prefix: "myapp:cache:"

redis:
  addr: "localhost:6379"
  pool_size: 20
  min_idle_conns: 5
```

```go
viper.UnmarshalKey("store", storeOpts)
viper.UnmarshalKey("redis", storeOpts.Redis)
```

### 3. Environment Variables

```go
opts.Redis.Addr = os.Getenv("REDIS_ADDR")
opts.Redis.Password = os.Getenv("REDIS_PASSWORD")
```

## Health Checks

The adapter leverages health check capabilities from `common/options`:

```go
// Check Redis health
ctx := context.Background()
if err := opts.Redis.Health(ctx, logger); err != nil {
    log.Printf("Redis unhealthy: %v", err)
}
```

## Testing

Run tests with:

```bash
go test ./store/adapters
```

## Migration Guide

If you're migrating from direct store usage to the adapter:

### Before (Direct Usage)

```go
import "github.com/kart-io/goagent/store/redis"

config := &redis.Config{
    Addr:     "localhost:6379",
    Password: "",
    DB:       0,
}
store, err := redis.New(config)
```

### After (With Adapter)

```go
import (
    "github.com/kart-io/k8s-agent/common/options"
    "github.com/kart-io/goagent/store/adapters"
)

redisOpts := options.NewRedisOptions()
redisOpts.Addr = "localhost:6379"

adapter := adapters.NewRedisStoreAdapter(redisOpts, "")
store, err := adapter.CreateStore()
```

## Benefits

1. **Consistency**: Use the same configuration pattern across all services
2. **Validation**: Built-in validation rules from `common/options`
3. **Observability**: Health checks and metrics integration
4. **Flexibility**: Easy switching between store backends
5. **Maintainability**: Centralized configuration management

## Future Enhancements

- [ ] MySQL store implementation
- [ ] MongoDB adapter
- [ ] Cassandra adapter
- [ ] Configuration hot-reload support
- [ ] Metrics collection integration
- [ ] Distributed tracing support
