package postgres

import (
	"time"

	"gorm.io/gorm/logger"
)

// PostgresOption 定义 PostgreSQL Store 配置选项
type PostgresOption func(*Config)

// WithTableName 设置表名
func WithTableName(tableName string) PostgresOption {
	return func(cfg *Config) {
		if tableName != "" {
			cfg.TableName = tableName
		}
	}
}

// WithMaxIdleConns 设置最大空闲连接数
func WithMaxIdleConns(n int) PostgresOption {
	return func(cfg *Config) {
		if n > 0 {
			cfg.MaxIdleConns = n
		}
	}
}

// WithMaxOpenConns 设置最大打开连接数
func WithMaxOpenConns(n int) PostgresOption {
	return func(cfg *Config) {
		if n > 0 {
			cfg.MaxOpenConns = n
		}
	}
}

// WithConnMaxLifetime 设置连接最大生命周期
func WithConnMaxLifetime(d time.Duration) PostgresOption {
	return func(cfg *Config) {
		if d > 0 {
			cfg.ConnMaxLifetime = d
		}
	}
}

// WithConnMaxIdleTime 设置空闲连接超时时间
func WithConnMaxIdleTime(d time.Duration) PostgresOption {
	return func(cfg *Config) {
		if d > 0 {
			cfg.ConnMaxIdleTime = d
		}
	}
}

// WithLogLevel 设置 GORM 日志级别
func WithLogLevel(level logger.LogLevel) PostgresOption {
	return func(cfg *Config) {
		cfg.LogLevel = level
	}
}

// WithAutoMigrate 设置是否自动迁移
func WithAutoMigrate(enabled bool) PostgresOption {
	return func(cfg *Config) {
		cfg.AutoMigrate = enabled
	}
}

// ApplyPostgresOptions 应用选项到配置
func ApplyPostgresOptions(config *Config, opts ...PostgresOption) *Config {
	if config == nil {
		config = DefaultConfig()
	}

	for _, opt := range opts {
		opt(config)
	}

	return config
}
