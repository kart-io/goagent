package registry

import (
	"fmt"
	"sync"

	agentllm "github.com/kart-io/goagent/llm"
	"github.com/kart-io/goagent/llm/constants"
)

// ProviderFactory 是创建 LLM provider 的工厂函数类型
type ProviderFactory func(...agentllm.ClientOption) (agentllm.Client, error)

// registry 存储所有已注册的 provider 工厂函数
var (
	registry = make(map[constants.Provider]ProviderFactory)
	mu       sync.RWMutex
)

// Register 注册一个 provider 工厂函数
// 通常在 provider 包的 init() 函数中调用
func Register(provider constants.Provider, factory ProviderFactory) {
	mu.Lock()
	defer mu.Unlock()

	if factory == nil {
		panic(fmt.Sprintf("registry: Register factory for %s is nil", provider))
	}

	if _, dup := registry[provider]; dup {
		panic(fmt.Sprintf("registry: Register called twice for provider %s", provider))
	}

	registry[provider] = factory
}

// Get 获取指定 provider 的工厂函数
func Get(provider constants.Provider) (ProviderFactory, error) {
	mu.RLock()
	defer mu.RUnlock()

	factory, ok := registry[provider]
	if !ok {
		return nil, fmt.Errorf("registry: provider %s not registered", provider)
	}

	return factory, nil
}

// MustGet 获取指定 provider 的工厂函数，如果不存在则 panic
func MustGet(provider constants.Provider) ProviderFactory {
	factory, err := Get(provider)
	if err != nil {
		panic(err)
	}
	return factory
}

// List 返回所有已注册的 provider 列表
func List() []constants.Provider {
	mu.RLock()
	defer mu.RUnlock()

	providers := make([]constants.Provider, 0, len(registry))
	for provider := range registry {
		providers = append(providers, provider)
	}

	return providers
}

// IsRegistered 检查指定的 provider 是否已注册
func IsRegistered(provider constants.Provider) bool {
	mu.RLock()
	defer mu.RUnlock()

	_, ok := registry[provider]
	return ok
}

// Unregister 取消注册一个 provider（主要用于测试）
func Unregister(provider constants.Provider) {
	mu.Lock()
	defer mu.Unlock()

	delete(registry, provider)
}

// Clear 清空所有注册（主要用于测试）
func Clear() {
	mu.Lock()
	defer mu.Unlock()

	registry = make(map[constants.Provider]ProviderFactory)
}

// New 使用注册的工厂函数创建 provider 实例
// 这是推荐的创建 provider 的方式
func New(provider constants.Provider, opts ...agentllm.ClientOption) (agentllm.Client, error) {
	factory, err := Get(provider)
	if err != nil {
		return nil, err
	}

	return factory(opts...)
}

// MustNew 使用注册的工厂函数创建 provider 实例，如果失败则 panic
func MustNew(provider constants.Provider, opts ...agentllm.ClientOption) agentllm.Client {
	client, err := New(provider, opts...)
	if err != nil {
		panic(err)
	}
	return client
}
