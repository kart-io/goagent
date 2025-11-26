package options

import (
	"fmt"
	"time"

	"github.com/spf13/pflag"
)

// PostgresOptions PostgreSQL 配置选项
type PostgresOptions struct {
	Host            string        `mapstructure:"host" yaml:"host" json:"host"`
	Port            int           `mapstructure:"port" yaml:"port" json:"port"`
	User            string        `mapstructure:"user" yaml:"user" json:"user"`
	Password        string        `mapstructure:"password" yaml:"password" json:"-"` // json:"-" 防止序列化泄露
	Database        string        `mapstructure:"database" yaml:"database" json:"database"`
	SSLMode         string        `mapstructure:"ssl_mode" yaml:"ssl_mode" json:"ssl_mode"`
	TimeZone        string        `mapstructure:"time_zone" yaml:"time_zone" json:"time_zone"`
	MaxIdleConns    int           `mapstructure:"max_idle_conns" yaml:"max_idle_conns" json:"max_idle_conns"`
	MaxOpenConns    int           `mapstructure:"max_open_conns" yaml:"max_open_conns" json:"max_open_conns"`
	ConnMaxLifetime time.Duration `mapstructure:"conn_max_lifetime" yaml:"conn_max_lifetime" json:"conn_max_lifetime"`
	LogLevel        string        `mapstructure:"log_level" yaml:"log_level" json:"log_level"`
	AutoMigrate     bool          `mapstructure:"auto_migrate" yaml:"auto_migrate" json:"auto_migrate"`
}

// NewPostgresOptions 创建默认 PostgreSQL 配置
func NewPostgresOptions() *PostgresOptions {
	return &PostgresOptions{
		Host:            "localhost",
		Port:            5432,
		User:            "postgres",
		Password:        "",
		Database:        "postgres",
		SSLMode:         "disable",
		TimeZone:        "UTC",
		MaxIdleConns:    10,
		MaxOpenConns:    100,
		ConnMaxLifetime: time.Hour,
		LogLevel:        "silent",
		AutoMigrate:     false,
	}
}

// String 返回安全的字符串表示（隐藏密码）
func (o *PostgresOptions) String() string {
	password := "***"
	if o.Password == "" {
		password = "(empty)"
	}
	return fmt.Sprintf("PostgresOptions{Host:%s, Port:%d, User:%s, Password:%s, Database:%s, SSLMode:%s}",
		o.Host, o.Port, o.User, password, o.Database, o.SSLMode)
}

// Validate 验证 PostgreSQL 配置
func (o *PostgresOptions) Validate() error {
	if o.Host == "" {
		return fmt.Errorf("postgres host is required")
	}

	if o.Port <= 0 || o.Port > 65535 {
		return fmt.Errorf("postgres port must be between 1 and 65535")
	}

	if o.User == "" {
		return fmt.Errorf("postgres user is required")
	}

	if o.Database == "" {
		return fmt.Errorf("postgres database is required")
	}

	if o.MaxIdleConns < 0 {
		return fmt.Errorf("postgres max idle conns must be >= 0")
	}

	if o.MaxOpenConns < 0 {
		return fmt.Errorf("postgres max open conns must be >= 0")
	}

	if o.MaxIdleConns > o.MaxOpenConns && o.MaxOpenConns > 0 {
		return fmt.Errorf("postgres max idle conns (%d) cannot exceed max open conns (%d)", o.MaxIdleConns, o.MaxOpenConns)
	}

	validSSLModes := map[string]bool{
		"disable":     true,
		"require":     true,
		"verify-ca":   true,
		"verify-full": true,
		"prefer":      true,
	}
	if !validSSLModes[o.SSLMode] {
		return fmt.Errorf("postgres ssl mode must be one of: disable, require, verify-ca, verify-full, prefer")
	}

	validLogLevels := map[string]bool{
		"silent": true,
		"error":  true,
		"warn":   true,
		"info":   true,
	}
	if !validLogLevels[o.LogLevel] {
		return fmt.Errorf("postgres log level must be one of: silent, error, warn, info")
	}

	return nil
}

// Complete 补充默认值
func (o *PostgresOptions) Complete() error {
	if o.SSLMode == "" {
		o.SSLMode = "disable"
	}

	if o.TimeZone == "" {
		o.TimeZone = "UTC"
	}

	if o.MaxIdleConns == 0 {
		o.MaxIdleConns = 10
	}

	if o.MaxOpenConns == 0 {
		o.MaxOpenConns = 100
	}

	if o.ConnMaxLifetime == 0 {
		o.ConnMaxLifetime = time.Hour
	}

	if o.LogLevel == "" {
		o.LogLevel = "silent"
	}

	return nil
}

// DSN 生成 PostgreSQL 连接字符串
func (o *PostgresOptions) DSN() string {
	return fmt.Sprintf("host=%s port=%d user=%s password=%s dbname=%s sslmode=%s TimeZone=%s",
		o.Host,
		o.Port,
		o.User,
		o.Password,
		o.Database,
		o.SSLMode,
		o.TimeZone,
	)
}

// AddFlags 添加命令行标志
func (o *PostgresOptions) AddFlags(fs *pflag.FlagSet) {
	fs.StringVar(&o.Host, "postgres-host", o.Host, "PostgreSQL host")
	fs.IntVar(&o.Port, "postgres-port", o.Port, "PostgreSQL port")
	fs.StringVar(&o.User, "postgres-user", o.User, "PostgreSQL user")
	fs.StringVar(&o.Password, "postgres-password", o.Password, "PostgreSQL password")
	fs.StringVar(&o.Database, "postgres-database", o.Database, "PostgreSQL database")
	fs.StringVar(&o.SSLMode, "postgres-ssl-mode", o.SSLMode, "PostgreSQL SSL mode")
	fs.StringVar(&o.TimeZone, "postgres-timezone", o.TimeZone, "PostgreSQL timezone")
	fs.IntVar(&o.MaxIdleConns, "postgres-max-idle-conns", o.MaxIdleConns, "PostgreSQL max idle connections")
	fs.IntVar(&o.MaxOpenConns, "postgres-max-open-conns", o.MaxOpenConns, "PostgreSQL max open connections")
	fs.DurationVar(&o.ConnMaxLifetime, "postgres-conn-max-lifetime", o.ConnMaxLifetime, "PostgreSQL connection max lifetime")
	fs.StringVar(&o.LogLevel, "postgres-log-level", o.LogLevel, "PostgreSQL log level (silent, error, warn, info)")
	fs.BoolVar(&o.AutoMigrate, "postgres-auto-migrate", o.AutoMigrate, "PostgreSQL auto migrate")
}
