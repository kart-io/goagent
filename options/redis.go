package options

import (
	"fmt"
	"time"

	"github.com/spf13/pflag"
)

// RedisOptions Redis 配置选项
type RedisOptions struct {
	Addr         string        `mapstructure:"addr" yaml:"addr" json:"addr"`
	Password     string        `mapstructure:"password" yaml:"password" json:"password"`
	DB           int           `mapstructure:"db" yaml:"db" json:"db"`
	PoolSize     int           `mapstructure:"pool_size" yaml:"pool_size" json:"pool_size"`
	MinIdleConns int           `mapstructure:"min_idle_conns" yaml:"min_idle_conns" json:"min_idle_conns"`
	DialTimeout  time.Duration `mapstructure:"dial_timeout" yaml:"dial_timeout" json:"dial_timeout"`
	ReadTimeout  time.Duration `mapstructure:"read_timeout" yaml:"read_timeout" json:"read_timeout"`
	WriteTimeout time.Duration `mapstructure:"write_timeout" yaml:"write_timeout" json:"write_timeout"`
}

// NewRedisOptions 创建默认 Redis 配置
func NewRedisOptions() *RedisOptions {
	return &RedisOptions{
		Addr:         "localhost:6379",
		Password:     "",
		DB:           0,
		PoolSize:     10,
		MinIdleConns: 5,
		DialTimeout:  5 * time.Second,
		ReadTimeout:  3 * time.Second,
		WriteTimeout: 3 * time.Second,
	}
}

// Validate 验证 Redis 配置
func (o *RedisOptions) Validate() error {
	if o.Addr == "" {
		return fmt.Errorf("redis addr is required")
	}

	if o.DB < 0 {
		return fmt.Errorf("redis db must be >= 0")
	}

	if o.PoolSize < 0 {
		return fmt.Errorf("redis pool size must be >= 0")
	}

	if o.MinIdleConns < 0 {
		return fmt.Errorf("redis min idle conns must be >= 0")
	}

	if o.MinIdleConns > o.PoolSize {
		return fmt.Errorf("redis min idle conns (%d) cannot exceed pool size (%d)", o.MinIdleConns, o.PoolSize)
	}

	return nil
}

// Complete 补充默认值
func (o *RedisOptions) Complete() error {
	if o.PoolSize == 0 {
		o.PoolSize = 10
	}

	if o.MinIdleConns == 0 {
		o.MinIdleConns = 5
	}

	if o.DialTimeout == 0 {
		o.DialTimeout = 5 * time.Second
	}

	if o.ReadTimeout == 0 {
		o.ReadTimeout = 3 * time.Second
	}

	if o.WriteTimeout == 0 {
		o.WriteTimeout = 3 * time.Second
	}

	return nil
}

// AddFlags 添加命令行标志
func (o *RedisOptions) AddFlags(fs *pflag.FlagSet) {
	fs.StringVar(&o.Addr, "redis-addr", o.Addr, "Redis server address")
	fs.StringVar(&o.Password, "redis-password", o.Password, "Redis password")
	fs.IntVar(&o.DB, "redis-db", o.DB, "Redis database number")
	fs.IntVar(&o.PoolSize, "redis-pool-size", o.PoolSize, "Redis connection pool size")
	fs.IntVar(&o.MinIdleConns, "redis-min-idle-conns", o.MinIdleConns, "Redis minimum idle connections")
	fs.DurationVar(&o.DialTimeout, "redis-dial-timeout", o.DialTimeout, "Redis dial timeout")
	fs.DurationVar(&o.ReadTimeout, "redis-read-timeout", o.ReadTimeout, "Redis read timeout")
	fs.DurationVar(&o.WriteTimeout, "redis-write-timeout", o.WriteTimeout, "Redis write timeout")
}
