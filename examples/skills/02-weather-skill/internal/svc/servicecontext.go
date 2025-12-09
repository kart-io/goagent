// Package svc 服务依赖容器
//
// ServiceContext 作为依赖注入容器
// 管理所有服务级别的依赖，包括配置、缓存、LLM 客户端等
package svc

import (
	"os"
	"sync"
	"time"

	"github.com/kart-io/goagent/examples/skills/02-weather-skill/internal/config"
	"github.com/kart-io/goagent/llm"
	"github.com/kart-io/goagent/llm/providers"
)

// ServiceContext 服务上下文
//
// 作为依赖注入容器，管理所有服务级别依赖
// 遵循单一职责原则，只负责依赖管理
type ServiceContext struct {
	Config    *config.Config
	Cache     *WeatherCache
	LLMClient llm.Client
}

// NewServiceContext 创建服务上下文
func NewServiceContext(c *config.Config) *ServiceContext {
	svc := &ServiceContext{
		Config: c,
	}

	// 初始化缓存（如果启用）
	if c.Skill.Cache.Enabled {
		svc.Cache = NewWeatherCache(time.Duration(c.Skill.Cache.TTL) * time.Second)
	}

	// 初始化 LLM 客户端
	svc.LLMClient = createLLMClient(c)

	return svc
}

// createLLMClient 创建 LLM 客户端
func createLLMClient(c *config.Config) llm.Client {
	// 根据配置选择提供商
	switch c.LLM.Provider {
	case "deepseek":
		if apiKey := os.Getenv("DEEPSEEK_API_KEY"); apiKey != "" {
			client, err := providers.NewDeepSeekWithOptions(
				llm.WithAPIKey(apiKey),
				llm.WithModel(c.LLM.Model),
				llm.WithMaxTokens(c.LLM.MaxTokens),
				llm.WithTemperature(c.LLM.Temperature),
			)
			if err == nil {
				return client
			}
		}
	case "openai":
		if apiKey := os.Getenv("OPENAI_API_KEY"); apiKey != "" {
			client, err := providers.NewOpenAIWithOptions(
				llm.WithAPIKey(apiKey),
				llm.WithModel(c.LLM.Model),
				llm.WithMaxTokens(c.LLM.MaxTokens),
				llm.WithTemperature(c.LLM.Temperature),
			)
			if err == nil {
				return client
			}
		}
	}

	// 返回 nil 表示没有可用的 LLM 客户端
	return nil
}

// ============================================================================
// 缓存实现
// ============================================================================

// WeatherCache 天气缓存
type WeatherCache struct {
	data map[string]*cacheItem
	ttl  time.Duration
	mu   sync.RWMutex
}

// cacheItem 缓存项
type cacheItem struct {
	value     interface{}
	expiredAt time.Time
}

// NewWeatherCache 创建天气缓存
func NewWeatherCache(ttl time.Duration) *WeatherCache {
	return &WeatherCache{
		data: make(map[string]*cacheItem),
		ttl:  ttl,
	}
}

// Get 获取缓存
func (c *WeatherCache) Get(key string) (interface{}, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	item, exists := c.data[key]
	if !exists {
		return nil, false
	}

	// 检查是否过期
	if time.Now().After(item.expiredAt) {
		return nil, false
	}

	return item.value, true
}

// Set 设置缓存
func (c *WeatherCache) Set(key string, value interface{}) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.data[key] = &cacheItem{
		value:     value,
		expiredAt: time.Now().Add(c.ttl),
	}
}

// Delete 删除缓存
func (c *WeatherCache) Delete(key string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.data, key)
}

// Clear 清空缓存
func (c *WeatherCache) Clear() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.data = make(map[string]*cacheItem)
}

// Size 返回缓存大小
func (c *WeatherCache) Size() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return len(c.data)
}
