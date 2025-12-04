package postgres

import (
	"time"

	"gorm.io/gorm/logger"
)

// Config holds configuration for PostgreSQL store
type Config struct {
	// DSN is the PostgreSQL Data Source Name
	// Example: "host=localhost user=postgres password=secret dbname=agent port=5432 sslmode=disable"
	DSN string

	// TableName is the name of the table to use for storage
	TableName string

	// MaxIdleConns is the maximum number of idle connections
	MaxIdleConns int

	// MaxOpenConns is the maximum number of open connections
	MaxOpenConns int

	// ConnMaxLifetime is the maximum lifetime of a connection
	ConnMaxLifetime time.Duration

	// ConnMaxIdleTime is the maximum amount of time a connection may be idle
	ConnMaxIdleTime time.Duration

	// LogLevel is the GORM log level
	LogLevel logger.LogLevel

	// AutoMigrate enables automatic table creation
	AutoMigrate bool
}

// DefaultConfig returns default PostgreSQL configuration
func DefaultConfig() *Config {
	return &Config{
		DSN:             "host=localhost user=postgres password=postgres dbname=agent port=5432 sslmode=disable",
		TableName:       "agent_stores",
		MaxIdleConns:    25,              // 最大空闲连接，提高连接复用率
		MaxOpenConns:    100,             // 最大打开连接
		ConnMaxLifetime: 5 * time.Minute, // 连接最大生命周期，避免连接过期
		ConnMaxIdleTime: 5 * time.Minute, // 空闲连接超时时间，及时释放资源
		LogLevel:        logger.Silent,
		AutoMigrate:     true,
	}
}
