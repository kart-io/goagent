package redis

import (
	"time"

	"github.com/kart-io/goagent/core"
)

// Config holds configuration for Redis store
type Config struct {
	// Addr is the Redis server address (host:port)
	Addr string

	// Password for Redis authentication
	Password string

	// DB is the Redis database number
	DB int

	// Prefix is the key prefix for all store keys
	Prefix string

	// TTL is the default time-to-live for keys (0 = no expiration)
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
}

// DefaultConfig returns default Redis configuration
func DefaultConfig() *Config {
	return &Config{
		Addr:         "localhost:6379",
		Password:     "",
		DB:           0,
		Prefix:       "agent:store:",
		TTL:          0,   // 无过期时间
		PoolSize:     100, // 连接池大小，适合高并发场景
		MinIdleConns: 10,  // 最小空闲连接，保持足够的预热连接
		MaxRetries:   3,
		DialTimeout:  core.DefaultDBConnectionTimeout,
		ReadTimeout:  core.DefaultDBOperationTimeout,
		WriteTimeout: core.DefaultDBOperationTimeout,
	}
}
