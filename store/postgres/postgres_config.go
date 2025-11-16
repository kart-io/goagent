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
		MaxIdleConns:    10,
		MaxOpenConns:    100,
		ConnMaxLifetime: time.Hour,
		LogLevel:        logger.Silent,
		AutoMigrate:     true,
	}
}
