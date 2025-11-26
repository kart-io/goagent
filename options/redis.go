package options

import (
	"fmt"
	"time"

	"github.com/spf13/pflag"
)

// MaxRedisDB is the maximum Redis database number (0-15 by default)
const MaxRedisDB = 15

// RedisOptions Redis 配置选项
type RedisOptions struct {
	Addr         string        `mapstructure:"addr" yaml:"addr" json:"addr"`
	Password     string        `mapstructure:"password" yaml:"password" json:"-"` // json:"-" 防止序列化泄露
	DB           int           `mapstructure:"db" yaml:"db" json:"db"`
	PoolSize     int           `mapstructure:"pool_size" yaml:"pool_size" json:"pool_size"`
	MinIdleConns int           `mapstructure:"min_idle_conns" yaml:"min_idle_conns" json:"min_idle_conns"`
	MaxRetries   int           `mapstructure:"max_retries" yaml:"max_retries" json:"max_retries"`
	TTL          time.Duration `mapstructure:"ttl" yaml:"ttl" json:"ttl"`
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
		MaxRetries:   3,
		TTL:          0, // 0 = no expiration
		DialTimeout:  5 * time.Second,
		ReadTimeout:  3 * time.Second,
		WriteTimeout: 3 * time.Second,
	}
}

// String 返回安全的字符串表示（隐藏密码）
func (o *RedisOptions) String() string {
	password := "***"
	if o.Password == "" {
		password = "(empty)"
	}
	return fmt.Sprintf("RedisOptions{Addr:%s, DB:%d, Password:%s, PoolSize:%d, MinIdleConns:%d, MaxRetries:%d, TTL:%v}",
		o.Addr, o.DB, password, o.PoolSize, o.MinIdleConns, o.MaxRetries, o.TTL)
}

// Validate 验证 Redis 配置
func (o *RedisOptions) Validate() error {
	if o.Addr == "" {
		return fmt.Errorf("redis addr is required")
	}

	if o.DB < 0 || o.DB > MaxRedisDB {
		return fmt.Errorf("redis db must be between 0 and %d", MaxRedisDB)
	}

	if o.PoolSize < 0 {
		return fmt.Errorf("redis pool size must be >= 0")
	}

	if o.MinIdleConns < 0 {
		return fmt.Errorf("redis min idle conns must be >= 0")
	}

	if o.PoolSize > 0 && o.MinIdleConns > o.PoolSize {
		return fmt.Errorf("redis min idle conns (%d) cannot exceed pool size (%d)", o.MinIdleConns, o.PoolSize)
	}

	if o.MaxRetries < 0 {
		return fmt.Errorf("redis max retries must be >= 0")
	}

	if o.TTL < 0 {
		return fmt.Errorf("redis ttl must be >= 0")
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

	if o.MaxRetries == 0 {
		o.MaxRetries = 3
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
	fs.IntVar(&o.DB, "redis-db", o.DB, "Redis database number (0-15)")
	fs.IntVar(&o.PoolSize, "redis-pool-size", o.PoolSize, "Redis connection pool size")
	fs.IntVar(&o.MinIdleConns, "redis-min-idle-conns", o.MinIdleConns, "Redis minimum idle connections")
	fs.IntVar(&o.MaxRetries, "redis-max-retries", o.MaxRetries, "Redis maximum retry attempts")
	fs.DurationVar(&o.TTL, "redis-ttl", o.TTL, "Redis default key TTL (0 = no expiration)")
	fs.DurationVar(&o.DialTimeout, "redis-dial-timeout", o.DialTimeout, "Redis dial timeout")
	fs.DurationVar(&o.ReadTimeout, "redis-read-timeout", o.ReadTimeout, "Redis read timeout")
	fs.DurationVar(&o.WriteTimeout, "redis-write-timeout", o.WriteTimeout, "Redis write timeout")
}
