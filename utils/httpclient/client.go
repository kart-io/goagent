// Package httpclient 提供统一的 HTTP 客户端管理
package httpclient

import (
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"sort"
	"strings"
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
		MaxIdleConnsPerHost: 100, // 优化：每个主机保持 100 个空闲连接，提升 LLM 调用性能
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

	// 连接池配置优化：提升 LLM API 调用性能
	// 1. 基于 DefaultTransport 克隆，保留 Proxy、DialContext 等默认行为
	// 2. 优化连接池参数，减少连接建立开销（10-20ms/次）
	var transport *http.Transport
	if t, ok := http.DefaultTransport.(*http.Transport); ok {
		transport = t.Clone()
	} else {
		// 回退机制：如果 DefaultTransport 被替换为非 *http.Transport 类型，创建新的标准 Transport
		transport = &http.Transport{
			Proxy:                 http.ProxyFromEnvironment,
			ForceAttemptHTTP2:     true,
			MaxIdleConns:          100,
			IdleConnTimeout:       90 * time.Second,
			TLSHandshakeTimeout:   10 * time.Second,
			ExpectContinueTimeout: 1 * time.Second,
		}
	}

	// 应用性能优化配置
	transport.DisableKeepAlives = config.DisableKeepAlive
	// 关键优化：增加每个主机的最大空闲连接数
	// LLM Provider 通常只访问单一主机，高并发场景下需要更多连接
	transport.MaxIdleConnsPerHost = config.MaxIdleConnsPerHost
	// 设置全局最大空闲连接数（防止连接泄漏）
	transport.MaxIdleConns = 100
	// 增加空闲连接超时时间，降低连接重建频率
	transport.IdleConnTimeout = 90 * time.Second

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

// 全局客户端缓存：优化 LLM Provider HTTP 客户端复用
// Key: 基于 (BaseURL, Headers) 生成的唯一标识
// Value: 复用的 HTTP 客户端实例
var (
	clientCache   sync.Map // map[string]*Client
	cachingConfig = &CachingConfig{
		Enabled:   true,
		KeyFields: []string{"BaseURL", "Headers"},
	}
)

// CachingConfig 控制客户端缓存行为
type CachingConfig struct {
	Enabled   bool     // 是否启用缓存
	KeyFields []string // 用于生成缓存 key 的字段
}

// generateCacheKey 根据配置生成缓存 key
// 相同配置的客户端将复用同一实例，提升性能
func generateCacheKey(config *Config) string {
	if config == nil || !cachingConfig.Enabled {
		return ""
	}

	// 构建可排序的键值对
	var parts []string

	// BaseURL 是必须的
	if config.BaseURL != "" {
		parts = append(parts, "baseurl:"+config.BaseURL)
	}

	// Headers 需要排序以保证一致性
	if len(config.Headers) > 0 {
		var headerPairs []string
		for k, v := range config.Headers {
			headerPairs = append(headerPairs, k+":"+v)
		}
		sort.Strings(headerPairs)
		parts = append(parts, "headers:"+strings.Join(headerPairs, ","))
	}

	// 生成 SHA256 哈希作为缓存 key
	data := strings.Join(parts, "|")
	hash := sha256.Sum256([]byte(data))
	return hex.EncodeToString(hash[:])
}

// GetOrCreateClient 获取或创建 HTTP 客户端（带缓存）
// 对于相同配置的请求，将复用同一客户端实例，减少连接建立开销
func GetOrCreateClient(config *Config) *Client {
	if config == nil {
		config = DefaultConfig()
	}

	// 生成缓存 key
	cacheKey := generateCacheKey(config)

	// 如果禁用缓存或无法生成 key，直接创建新客户端
	if cacheKey == "" {
		return NewClient(config)
	}

	// 尝试从缓存获取
	if cached, ok := clientCache.Load(cacheKey); ok {
		if client, ok := cached.(*Client); ok {
			return client
		}
	}

	// 缓存未命中，创建新客户端
	client := NewClient(config)

	// 存入缓存（使用 LoadOrStore 避免并发创建）
	actual, _ := clientCache.LoadOrStore(cacheKey, client)
	return actual.(*Client)
}

// SetCachingEnabled 控制是否启用客户端缓存
// 注意：修改此设置不影响已缓存的客户端
func SetCachingEnabled(enabled bool) {
	cachingConfig.Enabled = enabled
}

// ClearCache 清空客户端缓存
// 用于测试或需要强制重建客户端的场景
func ClearCache() {
	clientCache.Range(func(key, value interface{}) bool {
		clientCache.Delete(key)
		return true
	})
}

// GetCacheStats 获取缓存统计信息（用于监控和调试）
func GetCacheStats() map[string]int {
	count := 0
	clientCache.Range(func(key, value interface{}) bool {
		count++
		return true
	})

	return map[string]int{
		"cached_clients": count,
		"enabled":        boolToInt(cachingConfig.Enabled),
	}
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

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
