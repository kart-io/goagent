package redis

import "time"

// RedisOption 定义 Redis Store 配置选项
type RedisOption func(*Config)

// WithPassword 设置 Redis 密码
func WithPassword(password string) RedisOption {
	return func(cfg *Config) {
		cfg.Password = password
	}
}

// WithDB 设置 Redis 数据库索引
func WithDB(db int) RedisOption {
	return func(cfg *Config) {
		if db >= 0 {
			cfg.DB = db
		}
	}
}

// WithPrefix 设置 key 前缀
func WithPrefix(prefix string) RedisOption {
	return func(cfg *Config) {
		if prefix != "" {
			cfg.Prefix = prefix
		}
	}
}

// WithTTL 设置默认 TTL（过期时间）
func WithTTL(ttl time.Duration) RedisOption {
	return func(cfg *Config) {
		if ttl >= 0 {
			cfg.TTL = ttl
		}
	}
}

// WithPoolSize 设置连接池大小
func WithPoolSize(size int) RedisOption {
	return func(cfg *Config) {
		if size > 0 {
			cfg.PoolSize = size
		}
	}
}

// WithMinIdleConns 设置最小空闲连接数
func WithMinIdleConns(n int) RedisOption {
	return func(cfg *Config) {
		if n >= 0 {
			cfg.MinIdleConns = n
		}
	}
}

// WithMaxRetries 设置最大重试次数
func WithMaxRetries(n int) RedisOption {
	return func(cfg *Config) {
		if n >= 0 {
			cfg.MaxRetries = n
		}
	}
}

// WithDialTimeout 设置连接超时
func WithDialTimeout(d time.Duration) RedisOption {
	return func(cfg *Config) {
		if d > 0 {
			cfg.DialTimeout = d
		}
	}
}

// WithReadTimeout 设置读超时
func WithReadTimeout(d time.Duration) RedisOption {
	return func(cfg *Config) {
		if d > 0 {
			cfg.ReadTimeout = d
		}
	}
}

// WithWriteTimeout 设置写超时
func WithWriteTimeout(d time.Duration) RedisOption {
	return func(cfg *Config) {
		if d > 0 {
			cfg.WriteTimeout = d
		}
	}
}

// ApplyRedisOptions 应用选项到配置
func ApplyRedisOptions(config *Config, opts ...RedisOption) *Config {
	if config == nil {
		config = DefaultConfig()
	}

	for _, opt := range opts {
		opt(config)
	}

	return config
}
