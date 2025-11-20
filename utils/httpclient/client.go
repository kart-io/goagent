// Package httpclient 提供统一的 HTTP 客户端管理
package httpclient

import (
	"net/http"
	"sync"
	"time"

	"github.com/go-resty/resty/v2"
)

// Client HTTP 客户端管理器
//
// 封装 resty.Client，提供统一的配置和实例管理
type Client struct {
	resty  *resty.Client
	config *Config
}

// Config HTTP 客户端配置选项
type Config struct {
	// Timeout 请求超时时间
	Timeout time.Duration

	// RetryCount 重试次数
	RetryCount int

	// RetryWaitTime 重试等待时间
	RetryWaitTime time.Duration

	// RetryMaxWaitTime 最大重试等待时间
	RetryMaxWaitTime time.Duration

	// BaseURL 基础 URL（可选）
	BaseURL string

	// Headers 默认请求头
	Headers map[string]string

	// Debug 是否启用调试模式
	Debug bool

	// DisableKeepAlive 是否禁用 Keep-Alive
	DisableKeepAlive bool

	// MaxIdleConnsPerHost 每个主机的最大空闲连接数
	MaxIdleConnsPerHost int
}

// DefaultConfig 返回默认配置
func DefaultConfig() *Config {
	return &Config{
		Timeout:             30 * time.Second,
		RetryCount:          3,
		RetryWaitTime:       1 * time.Second,
		RetryMaxWaitTime:    5 * time.Second,
		Headers:             make(map[string]string),
		Debug:               false,
		DisableKeepAlive:    false,
		MaxIdleConnsPerHost: 100,
	}
}

// NewClient 创建新的 HTTP 客户端
//
// 如果 config 为 nil，则使用默认配置
func NewClient(config *Config) *Client {
	if config == nil {
		config = DefaultConfig()
	}

	// 创建 resty 客户端
	restyClient := resty.New().
		SetTimeout(config.Timeout).
		SetRetryCount(config.RetryCount).
		SetRetryWaitTime(config.RetryWaitTime).
		SetRetryMaxWaitTime(config.RetryMaxWaitTime).
		SetHeaders(config.Headers)

	// 设置基础 URL
	if config.BaseURL != "" {
		restyClient.SetBaseURL(config.BaseURL)
	}

	// 调试模式
	if config.Debug {
		restyClient.SetDebug(true)
	}

	// 连接池配置
	transport := &http.Transport{
		DisableKeepAlives:   config.DisableKeepAlive,
		MaxIdleConnsPerHost: config.MaxIdleConnsPerHost,
	}
	restyClient.SetTransport(transport)

	return &Client{
		resty:  restyClient,
		config: config,
	}
}

// R 返回一个新的请求
//
// 使用方式：
//
//	client.R().SetHeader("key", "value").Get(url)
func (c *Client) R() *resty.Request {
	return c.resty.R()
}

// Resty 返回底层的 resty 客户端
//
// 用于需要直接访问 resty 客户端的场景
func (c *Client) Resty() *resty.Client {
	return c.resty
}

// GetClient 返回底层的 resty 客户端（别名方法）
//
// Deprecated: 使用 Resty() 方法代替
func (c *Client) GetClient() *resty.Client {
	return c.resty
}

// SetTimeout 设置超时时间
func (c *Client) SetTimeout(timeout time.Duration) *Client {
	c.resty.SetTimeout(timeout)
	c.config.Timeout = timeout
	return c
}

// SetRetryCount 设置重试次数
func (c *Client) SetRetryCount(count int) *Client {
	c.resty.SetRetryCount(count)
	c.config.RetryCount = count
	return c
}

// SetHeader 设置默认请求头
func (c *Client) SetHeader(key, value string) *Client {
	c.resty.SetHeader(key, value)
	if c.config.Headers == nil {
		c.config.Headers = make(map[string]string)
	}
	c.config.Headers[key] = value
	return c
}

// SetHeaders 批量设置默认请求头
func (c *Client) SetHeaders(headers map[string]string) *Client {
	c.resty.SetHeaders(headers)
	if c.config.Headers == nil {
		c.config.Headers = make(map[string]string)
	}
	for k, v := range headers {
		c.config.Headers[k] = v
	}
	return c
}

// SetBaseURL 设置基础 URL
func (c *Client) SetBaseURL(baseURL string) *Client {
	c.resty.SetBaseURL(baseURL)
	c.config.BaseURL = baseURL
	return c
}

// SetDebug 设置调试模式
func (c *Client) SetDebug(debug bool) *Client {
	c.resty.SetDebug(debug)
	c.config.Debug = debug
	return c
}

// Config 返回客户端配置（只读）
func (c *Client) Config() *Config {
	// 返回副本以避免外部修改
	configCopy := *c.config
	if c.config.Headers != nil {
		configCopy.Headers = make(map[string]string)
		for k, v := range c.config.Headers {
			configCopy.Headers[k] = v
		}
	}
	return &configCopy
}

// 全局默认客户端
var (
	defaultClient *Client
	once          sync.Once
)

// Default 获取默认的 HTTP 客户端（单例模式）
//
// 首次调用时会创建一个使用默认配置的客户端
// 后续调用返回同一个实例
func Default() *Client {
	once.Do(func() {
		defaultClient = NewClient(nil)
	})
	return defaultClient
}

// SetDefault 设置默认的 HTTP 客户端
//
// 注意：此函数不是线程安全的，应该在程序初始化阶段调用
func SetDefault(client *Client) {
	defaultClient = client
}

// ResetDefault 重置默认客户端
//
// 下次调用 Default() 时会创建新的默认客户端
// 注意：此函数不是线程安全的
func ResetDefault() {
	defaultClient = nil
	once = sync.Once{}
}
