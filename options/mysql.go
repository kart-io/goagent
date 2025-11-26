package options

import (
	"fmt"
	"time"

	"github.com/spf13/pflag"
)

// MySQLOptions MySQL 配置选项
type MySQLOptions struct {
	Host            string        `mapstructure:"host" yaml:"host" json:"host"`
	Port            int           `mapstructure:"port" yaml:"port" json:"port"`
	User            string        `mapstructure:"user" yaml:"user" json:"user"`
	Password        string        `mapstructure:"password" yaml:"password" json:"-"` // json:"-" 防止序列化泄露
	Database        string        `mapstructure:"database" yaml:"database" json:"database"`
	Charset         string        `mapstructure:"charset" yaml:"charset" json:"charset"`
	ParseTime       bool          `mapstructure:"parse_time" yaml:"parse_time" json:"parse_time"`
	Loc             string        `mapstructure:"loc" yaml:"loc" json:"loc"`
	MaxIdleConns    int           `mapstructure:"max_idle_conns" yaml:"max_idle_conns" json:"max_idle_conns"`
	MaxOpenConns    int           `mapstructure:"max_open_conns" yaml:"max_open_conns" json:"max_open_conns"`
	ConnMaxLifetime time.Duration `mapstructure:"conn_max_lifetime" yaml:"conn_max_lifetime" json:"conn_max_lifetime"`
	LogLevel        string        `mapstructure:"log_level" yaml:"log_level" json:"log_level"`
	AutoMigrate     bool          `mapstructure:"auto_migrate" yaml:"auto_migrate" json:"auto_migrate"`
}

// NewMySQLOptions 创建默认 MySQL 配置
func NewMySQLOptions() *MySQLOptions {
	return &MySQLOptions{
		Host:            "localhost",
		Port:            3306,
		User:            "root",
		Password:        "",
		Database:        "test",
		Charset:         "utf8mb4",
		ParseTime:       true,
		Loc:             "Local",
		MaxIdleConns:    10,
		MaxOpenConns:    100,
		ConnMaxLifetime: time.Hour,
		LogLevel:        "silent",
		AutoMigrate:     false,
	}
}

// String 返回安全的字符串表示（隐藏密码）
func (o *MySQLOptions) String() string {
	password := "***"
	if o.Password == "" {
		password = "(empty)"
	}
	return fmt.Sprintf("MySQLOptions{Host:%s, Port:%d, User:%s, Password:%s, Database:%s}",
		o.Host, o.Port, o.User, password, o.Database)
}

// Validate 验证 MySQL 配置
func (o *MySQLOptions) Validate() error {
	if o.Host == "" {
		return fmt.Errorf("mysql host is required")
	}

	if o.Port <= 0 || o.Port > 65535 {
		return fmt.Errorf("mysql port must be between 1 and 65535")
	}

	if o.User == "" {
		return fmt.Errorf("mysql user is required")
	}

	if o.Database == "" {
		return fmt.Errorf("mysql database is required")
	}

	if o.MaxIdleConns < 0 {
		return fmt.Errorf("mysql max idle conns must be >= 0")
	}

	if o.MaxOpenConns < 0 {
		return fmt.Errorf("mysql max open conns must be >= 0")
	}

	if o.MaxIdleConns > o.MaxOpenConns && o.MaxOpenConns > 0 {
		return fmt.Errorf("mysql max idle conns (%d) cannot exceed max open conns (%d)", o.MaxIdleConns, o.MaxOpenConns)
	}

	validLogLevels := map[string]bool{
		"silent": true,
		"error":  true,
		"warn":   true,
		"info":   true,
	}
	if !validLogLevels[o.LogLevel] {
		return fmt.Errorf("mysql log level must be one of: silent, error, warn, info")
	}

	return nil
}

// Complete 补充默认值
func (o *MySQLOptions) Complete() error {
	if o.Charset == "" {
		o.Charset = "utf8mb4"
	}

	if o.Loc == "" {
		o.Loc = "Local"
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

// DSN 生成 MySQL 连接字符串
func (o *MySQLOptions) DSN() string {
	return fmt.Sprintf("%s:%s@tcp(%s:%d)/%s?charset=%s&parseTime=%t&loc=%s",
		o.User,
		o.Password,
		o.Host,
		o.Port,
		o.Database,
		o.Charset,
		o.ParseTime,
		o.Loc,
	)
}

// AddFlags 添加命令行标志
func (o *MySQLOptions) AddFlags(fs *pflag.FlagSet) {
	fs.StringVar(&o.Host, "mysql-host", o.Host, "MySQL host")
	fs.IntVar(&o.Port, "mysql-port", o.Port, "MySQL port")
	fs.StringVar(&o.User, "mysql-user", o.User, "MySQL user")
	fs.StringVar(&o.Password, "mysql-password", o.Password, "MySQL password")
	fs.StringVar(&o.Database, "mysql-database", o.Database, "MySQL database")
	fs.StringVar(&o.Charset, "mysql-charset", o.Charset, "MySQL charset")
	fs.BoolVar(&o.ParseTime, "mysql-parse-time", o.ParseTime, "MySQL parse time")
	fs.StringVar(&o.Loc, "mysql-loc", o.Loc, "MySQL location")
	fs.IntVar(&o.MaxIdleConns, "mysql-max-idle-conns", o.MaxIdleConns, "MySQL max idle connections")
	fs.IntVar(&o.MaxOpenConns, "mysql-max-open-conns", o.MaxOpenConns, "MySQL max open connections")
	fs.DurationVar(&o.ConnMaxLifetime, "mysql-conn-max-lifetime", o.ConnMaxLifetime, "MySQL connection max lifetime")
	fs.StringVar(&o.LogLevel, "mysql-log-level", o.LogLevel, "MySQL log level (silent, error, warn, info)")
	fs.BoolVar(&o.AutoMigrate, "mysql-auto-migrate", o.AutoMigrate, "MySQL auto migrate")
}
