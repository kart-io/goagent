# Provider Registry 使用指南

Provider Registry 是 GoAgent 的动态 LLM provider 注册和管理系统，允许运行时发现、创建和切换不同的 LLM providers。

## 目录

- [概述](#概述)
- [核心概念](#核心概念)
- [快速开始](#快速开始)
- [高级用法](#高级用法)
- [最佳实践](#最佳实践)
- [API 参考](#api-参考)

## 概述

### 为什么需要 Registry？

在传统方式中，你需要直接导入并调用每个 provider 的构造函数：

```go
// 旧方式 - 直接导入
import "github.com/kart-io/goagent/contrib/llm-providers/openai"
client, _ := openai.New(...)
```

使用 Registry，你可以：

1. **动态选择 Provider**: 根据配置或运行时条件选择 provider
2. **插件式架构**: 按需导入 providers，减少依赖
3. **统一接口**: 使用相同的 API 创建所有 providers
4. **自动发现**: 列出所有可用的 providers

### 架构设计

```
┌─────────────────────────────────────────┐
│        应用程序代码                      │
│  registry.New(ProviderOpenAI, ...)      │
└──────────────┬──────────────────────────┘
               │
               ▼
┌─────────────────────────────────────────┐
│       llm/registry (注册表)              │
│  - Register()                           │
│  - Get()                                │
│  - List()                               │
│  - New()                                │
└──────────────┬──────────────────────────┘
               │
               ▼
┌─────────────────────────────────────────┐
│    Contrib Providers (自动注册)         │
│  ┌─────────┐  ┌─────────┐  ┌─────────┐ │
│  │ OpenAI  │  │DeepSeek │  │ Gemini  │ │
│  │ init()  │  │ init()  │  │ init()  │ │
│  └─────────┘  └─────────┘  └─────────┘ │
└─────────────────────────────────────────┘
```

## 核心概念

### 1. 自动注册

每个 contrib provider 在其 `init()` 函数中自动注册到全局注册表：

```go
// contrib/llm-providers/openai/provider.go
func init() {
    registry.Register(constants.ProviderOpenAI, func(opts ...agentllm.ClientOption) (agentllm.Client, error) {
        return New(opts...)
    })
}
```

### 2. 空白导入 (Blank Import)

使用空白导入 `_` 来触发 provider 的 `init()` 函数：

```go
import _ "github.com/kart-io/goagent/contrib/llm-providers/openai"
```

这会注册 provider，但不会在代码中直接使用该包。

### 3. Provider Factory

Registry 存储的是 `ProviderFactory` 函数，而不是 provider 实例：

```go
type ProviderFactory func(...ClientOption) (Client, error)
```

这允许延迟创建 provider 实例，每次调用都会创建新实例。

## 快速开始

### 基本示例

```go
package main

import (
    "context"
    "fmt"

    // 导入需要的 providers（触发自动注册）
    _ "github.com/kart-io/goagent/contrib/llm-providers/openai"
    _ "github.com/kart-io/goagent/contrib/llm-providers/deepseek"

    agentllm "github.com/kart-io/goagent/llm"
    "github.com/kart-io/goagent/llm/constants"
    "github.com/kart-io/goagent/llm/registry"
)

func main() {
    // 使用 registry 创建 provider
    client, err := registry.New(
        constants.ProviderOpenAI,
        agentllm.WithAPIKey("your-api-key"),
        agentllm.WithModel("gpt-3.5-turbo"),
    )
    if err != nil {
        panic(err)
    }

    // 使用 client 发送请求
    resp, err := client.Complete(context.Background(), &agentllm.CompletionRequest{
        Messages: []agentllm.Message{
            {Role: "user", Content: "Hello!"},
        },
    })

    fmt.Println(resp.Content)
}
```

### 列出所有可用 Providers

```go
providers := registry.List()
for _, p := range providers {
    fmt.Printf("Available: %s\n", p)
}
```

### 检查 Provider 是否可用

```go
if registry.IsRegistered(constants.ProviderOpenAI) {
    fmt.Println("OpenAI is available")
}
```

## 高级用法

### 1. 动态 Provider 选择

根据配置文件或环境变量选择 provider：

```go
func createProvider(config *Config) (agentllm.Client, error) {
    // 从配置读取 provider 名称
    providerName := config.LLM.Provider

    // 映射到 constants.Provider
    var provider constants.Provider
    switch providerName {
    case "openai":
        provider = constants.ProviderOpenAI
    case "deepseek":
        provider = constants.ProviderDeepSeek
    case "gemini":
        provider = constants.ProviderGemini
    default:
        return nil, fmt.Errorf("unknown provider: %s", providerName)
    }

    // 使用 registry 创建
    return registry.New(
        provider,
        agentllm.WithAPIKey(config.LLM.APIKey),
        agentllm.WithModel(config.LLM.Model),
    )
}
```

### 2. Provider Fallback 链

实现自动 fallback 到可用的 provider：

```go
func createProviderWithFallback() (agentllm.Client, error) {
    // 定义 provider 优先级
    priorities := []constants.Provider{
        constants.ProviderOpenAI,
        constants.ProviderDeepSeek,
        constants.ProviderGemini,
        constants.ProviderOllama, // 本地 fallback
    }

    // 按优先级尝试
    for _, provider := range priorities {
        if !registry.IsRegistered(provider) {
            continue
        }

        client, err := registry.New(provider, agentllm.WithAPIKey(getAPIKey(provider)))
        if err == nil {
            log.Printf("Using provider: %s", provider)
            return client, nil
        }
        log.Printf("Provider %s failed: %v, trying next...", provider, err)
    }

    return nil, fmt.Errorf("all providers failed")
}
```

### 3. Multi-Provider 策略

同时使用多个 providers 进行负载均衡或冗余：

```go
type MultiProvider struct {
    clients []agentllm.Client
    current int
}

func NewMultiProvider(providers []constants.Provider) (*MultiProvider, error) {
    mp := &MultiProvider{
        clients: make([]agentllm.Client, 0, len(providers)),
    }

    for _, provider := range providers {
        client, err := registry.New(provider, agentllm.WithAPIKey(getAPIKey(provider)))
        if err != nil {
            return nil, err
        }
        mp.clients = append(mp.clients, client)
    }

    return mp, nil
}

func (mp *MultiProvider) Complete(ctx context.Context, req *agentllm.CompletionRequest) (*agentllm.CompletionResponse, error) {
    // Round-robin 策略
    client := mp.clients[mp.current]
    mp.current = (mp.current + 1) % len(mp.clients)

    return client.Complete(ctx, req)
}
```

### 4. 按需导入 Providers

只导入实际需要的 providers 以减少依赖：

```go
// development.go
//go:build dev

import (
    _ "github.com/kart-io/goagent/contrib/llm-providers/openai"
    _ "github.com/kart-io/goagent/contrib/llm-providers/deepseek"
    _ "github.com/kart-io/goagent/contrib/llm-providers/gemini"
    _ "github.com/kart-io/goagent/contrib/llm-providers/ollama"
)
```

```go
// production.go
//go:build !dev

import (
    _ "github.com/kart-io/goagent/contrib/llm-providers/openai"
    _ "github.com/kart-io/goagent/contrib/llm-providers/deepseek"
)
```

### 5. 自定义 Provider 注册

你也可以注册自己的自定义 provider：

```go
import "github.com/kart-io/goagent/llm/registry"

func init() {
    registry.Register("my-custom-provider", func(opts ...agentllm.ClientOption) (agentllm.Client, error) {
        return NewMyCustomProvider(opts...)
    })
}
```

## 最佳实践

### 1. 集中管理导入

在一个文件中集中管理所有 provider 导入：

```go
// providers.go
package main

import (
    _ "github.com/kart-io/goagent/contrib/llm-providers/openai"
    _ "github.com/kart-io/goagent/contrib/llm-providers/deepseek"
    _ "github.com/kart-io/goagent/contrib/llm-providers/gemini"
    // ... 其他 providers
)
```

### 2. 使用配置文件

将 provider 选择放在配置文件中：

```yaml
# config.yaml
llm:
  provider: "openai"
  model: "gpt-3.5-turbo"
  api_key: "${OPENAI_API_KEY}"

  fallback_providers:
    - "deepseek"
    - "ollama"
```

### 3. 错误处理

始终检查 provider 是否已注册：

```go
if !registry.IsRegistered(provider) {
    return fmt.Errorf("provider %s not registered - missing import?", provider)
}

client, err := registry.New(provider, opts...)
if err != nil {
    return fmt.Errorf("failed to create provider %s: %w", provider, err)
}
```

### 4. 测试

在测试中使用 registry 进行 mock：

```go
func TestWithRegistry(t *testing.T) {
    // 保存原始 registry
    defer registry.Clear()

    // 注册 mock provider
    registry.Register(constants.ProviderOpenAI, func(opts ...agentllm.ClientOption) (agentllm.Client, error) {
        return &MockClient{}, nil
    })

    // 运行测试
    client, _ := registry.New(constants.ProviderOpenAI)
    // ...
}
```

### 5. 避免 Panic

使用 `New()` 而不是 `MustNew()`：

```go
// ✓ 推荐
client, err := registry.New(provider, opts...)
if err != nil {
    // 优雅处理错误
}

// ✗ 避免（除非你确定 provider 一定存在）
client := registry.MustNew(provider, opts...)
```

## API 参考

### registry.Register

注册一个 provider 工厂函数。

```go
func Register(provider constants.Provider, factory ProviderFactory)
```

**参数**:
- `provider`: Provider 常量（例如 `constants.ProviderOpenAI`）
- `factory`: 创建 provider 实例的工厂函数

**Panic**:
- 如果 `factory` 为 `nil`
- 如果同一个 provider 被注册两次

### registry.Get

获取指定 provider 的工厂函数。

```go
func Get(provider constants.Provider) (ProviderFactory, error)
```

**返回**:
- `ProviderFactory`: 工厂函数
- `error`: 如果 provider 未注册

### registry.MustGet

获取 provider 工厂函数，未注册则 panic。

```go
func MustGet(provider constants.Provider) ProviderFactory
```

### registry.List

返回所有已注册的 providers 列表。

```go
func List() []constants.Provider
```

### registry.IsRegistered

检查 provider 是否已注册。

```go
func IsRegistered(provider constants.Provider) bool
```

### registry.New

使用注册的工厂函数创建 provider 实例。

```go
func New(provider constants.Provider, opts ...ClientOption) (Client, error)
```

这是创建 provider 的推荐方式。

### registry.MustNew

创建 provider 实例，失败则 panic。

```go
func MustNew(provider constants.Provider, opts ...ClientOption) Client
```

### registry.Unregister

取消注册一个 provider（主要用于测试）。

```go
func Unregister(provider constants.Provider)
```

### registry.Clear

清空所有注册（主要用于测试）。

```go
func Clear()
```

## 常见问题

### Q: 为什么需要空白导入 `_`？

A: 空白导入确保包的 `init()` 函数被调用，从而触发 provider 注册。没有空白导入，provider 不会被注册到 registry。

### Q: 可以动态加载 providers 吗？

A: 目前 Go 不支持真正的动态加载（插件）。但你可以在编译时使用 build tags 选择性导入 providers。

### Q: Registry 是线程安全的吗？

A: 是的。Registry 使用 `sync.RWMutex` 保护并发访问。

### Q: 如何在测试中 mock providers？

A: 使用 `registry.Clear()` 清空注册，然后注册你的 mock provider：

```go
defer registry.Clear()
registry.Register(constants.ProviderOpenAI, func(...) (Client, error) {
    return &MockClient{}, nil
})
```

### Q: 可以同时使用 registry 和直接导入吗？

A: 可以。两种方式可以共存：

```go
// 直接导入
import "github.com/kart-io/goagent/contrib/llm-providers/openai"
client1 := openai.New(...)

// 使用 registry
client2, _ := registry.New(constants.ProviderOpenAI, ...)
```

### Q: Registry 有性能开销吗？

A: 几乎没有。Registry 查找是 O(1) 的 map 操作，overhead 可以忽略不计。

## 示例代码

完整示例代码请参考：
- [基础示例](../../../examples/basic/13-provider-registry/main.go)
- [Contrib Providers](../../../contrib/llm-providers/)
- [Registry 实现](registry.go)

## 总结

Provider Registry 提供了：

✅ **灵活性**: 运行时动态选择 providers
✅ **可扩展性**: 轻松添加新 providers
✅ **解耦**: 应用代码不直接依赖具体 providers
✅ **简洁**: 统一的 API 创建所有 providers
✅ **向后兼容**: 可以与直接导入方式共存

开始使用 Registry，让你的 LLM 集成更加灵活和强大！
