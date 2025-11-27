# LLM Provider 使用指南

本文档介绍如何在 GoAgent 中使用 LLM Providers，包括传统方式和新的 Registry 方式。

## 两种使用方式

GoAgent 支持两种方式创建和使用 LLM Providers：

### 方式 1: 直接导入 (传统方式)

直接导入 provider 包并调用构造函数：

```go
import "github.com/kart-io/goagent/contrib/llm-providers/openai"

client, err := openai.New(
    agentllm.WithAPIKey("your-api-key"),
    agentllm.WithModel("gpt-3.5-turbo"),
)
```

**优点**:
- 简单直接
- 类型安全
- IDE 自动补全

**缺点**:
- 无法运行时动态选择 provider
- 代码与具体 provider 耦合

### 方式 2: Registry (推荐方式)

使用 Provider Registry 动态创建 provider：

```go
import _ "github.com/kart-io/goagent/contrib/llm-providers/openai"
import "github.com/kart-io/goagent/llm/registry"

client, err := registry.New(
    constants.ProviderOpenAI,
    agentllm.WithAPIKey("your-api-key"),
    agentllm.WithModel("gpt-3.5-turbo"),
)
```

**优点**:
- 运行时动态选择 provider
- 解耦，易于测试和切换
- 支持配置驱动
- 自动发现可用 providers

**缺点**:
- 需要额外的空白导入
- 运行时错误（如果 provider 未注册）

## 快速开始

### 使用单个 Provider

**传统方式**:
```go
package main

import (
    "context"
    "fmt"

    "github.com/kart-io/goagent/contrib/llm-providers/openai"
    agentllm "github.com/kart-io/goagent/llm"
)

func main() {
    client, err := openai.New(
        agentllm.WithAPIKey("your-api-key"),
    )
    if err != nil {
        panic(err)
    }

    resp, _ := client.Complete(context.Background(), &agentllm.CompletionRequest{
        Messages: []agentllm.Message{
            {Role: "user", Content: "Hello!"},
        },
    })

    fmt.Println(resp.Content)
}
```

**Registry 方式**:
```go
package main

import (
    "context"
    "fmt"

    _ "github.com/kart-io/goagent/contrib/llm-providers/openai"
    agentllm "github.com/kart-io/goagent/llm"
    "github.com/kart-io/goagent/llm/constants"
    "github.com/kart-io/goagent/llm/registry"
)

func main() {
    client, err := registry.New(
        constants.ProviderOpenAI,
        agentllm.WithAPIKey("your-api-key"),
    )
    if err != nil {
        panic(err)
    }

    resp, _ := client.Complete(context.Background(), &agentllm.CompletionRequest{
        Messages: []agentllm.Message{
            {Role: "user", Content: "Hello!"},
        },
    })

    fmt.Println(resp.Content)
}
```

## 高级用法

### 配置驱动的 Provider 选择

使用 Registry 可以轻松实现配置驱动：

```go
// config.yaml
llm:
  provider: "openai"  # 或 "deepseek", "gemini", 等
  api_key: "${OPENAI_API_KEY}"
  model: "gpt-3.5-turbo"
```

```go
type Config struct {
    LLM struct {
        Provider string `yaml:"provider"`
        APIKey   string `yaml:"api_key"`
        Model    string `yaml:"model"`
    } `yaml:"llm"`
}

func createClient(config *Config) (agentllm.Client, error) {
    // 映射配置到 provider 常量
    var provider constants.Provider
    switch config.LLM.Provider {
    case "openai":
        provider = constants.ProviderOpenAI
    case "deepseek":
        provider = constants.ProviderDeepSeek
    case "gemini":
        provider = constants.ProviderGemini
    default:
        return nil, fmt.Errorf("unknown provider: %s", config.LLM.Provider)
    }

    // 使用 registry 创建
    return registry.New(
        provider,
        agentllm.WithAPIKey(config.LLM.APIKey),
        agentllm.WithModel(config.LLM.Model),
    )
}
```

### Provider Fallback

实现自动 fallback 到可用的 provider：

```go
func createProviderWithFallback() (agentllm.Client, error) {
    // 按优先级尝试
    providers := []constants.Provider{
        constants.ProviderOpenAI,
        constants.ProviderDeepSeek,
        constants.ProviderOllama,  // 本地 fallback
    }

    for _, p := range providers {
        if !registry.IsRegistered(p) {
            continue
        }

        client, err := registry.New(p, agentllm.WithAPIKey(getAPIKey(p)))
        if err == nil {
            log.Printf("Using provider: %s", p)
            return client, nil
        }
        log.Printf("Provider %s failed: %v", p, err)
    }

    return nil, fmt.Errorf("all providers failed")
}
```

### 多 Provider 并行

同时使用多个 providers：

```go
type MultiProvider struct {
    clients map[constants.Provider]agentllm.Client
}

func NewMultiProvider(providerList []constants.Provider) (*MultiProvider, error) {
    mp := &MultiProvider{
        clients: make(map[constants.Provider]agentllm.Client),
    }

    for _, p := range providerList {
        client, err := registry.New(p, agentllm.WithAPIKey(getAPIKey(p)))
        if err != nil {
            return nil, err
        }
        mp.clients[p] = client
    }

    return mp, nil
}

func (mp *MultiProvider) CompleteWithProvider(ctx context.Context, provider constants.Provider, req *agentllm.CompletionRequest) (*agentllm.CompletionResponse, error) {
    client, ok := mp.clients[provider]
    if !ok {
        return nil, fmt.Errorf("provider %s not available", provider)
    }
    return client.Complete(ctx, req)
}
```

## 迁移指南

### 从传统方式迁移到 Registry

**步骤 1**: 添加空白导入

```go
// 旧代码
import "github.com/kart-io/goagent/contrib/llm-providers/openai"

// 新代码 - 添加空白导入
import (
    _ "github.com/kart-io/goagent/contrib/llm-providers/openai"
    "github.com/kart-io/goagent/llm/registry"
    "github.com/kart-io/goagent/llm/constants"
)
```

**步骤 2**: 替换构造函数调用

```go
// 旧代码
client, err := openai.New(opts...)

// 新代码
client, err := registry.New(constants.ProviderOpenAI, opts...)
```

**步骤 3**: 测试并验证

运行测试确保一切正常工作。

### 渐进式迁移

你不需要一次性迁移所有代码。两种方式可以共存：

```go
import (
    "github.com/kart-io/goagent/contrib/llm-providers/openai"  // 直接导入
    _ "github.com/kart-io/goagent/contrib/llm-providers/deepseek"  // 空白导入
    "github.com/kart-io/goagent/llm/registry"
)

func main() {
    // 方式 1: 直接使用
    client1, _ := openai.New(agentllm.WithAPIKey("key1"))

    // 方式 2: 使用 registry
    client2, _ := registry.New(constants.ProviderDeepSeek, agentllm.WithAPIKey("key2"))

    // 两者都可以正常工作
}
```

## 选择哪种方式？

### 使用传统方式（直接导入）如果：

- ✓ Provider 在编译时已知且固定
- ✓ 不需要运行时切换
- ✓ 代码简单，provider 数量少
- ✓ 需要最大的类型安全

### 使用 Registry 方式如果：

- ✓ 需要根据配置选择 provider
- ✓ 需要实现 provider fallback
- ✓ 需要运行时发现可用 providers
- ✓ 需要易于测试和 mock
- ✓ 构建插件式架构

## 常见问题

### Q: Registry 会增加运行时开销吗？

A: 几乎没有。Registry 查找是 O(1) 的 map 操作，overhead 可以忽略不计。

### Q: 可以混合使用两种方式吗？

A: 可以。两种方式可以在同一个项目中共存。

### Q: Registry 方式会破坏现有代码吗？

A: 不会。这是完全向后兼容的新增功能。所有现有代码继续正常工作。

### Q: 如何调试 Registry 相关问题？

A: 使用 `registry.List()` 查看所有已注册的 providers，使用 `registry.IsRegistered()` 检查特定 provider。

### Q: 可以注册自定义 provider 吗？

A: 可以。调用 `registry.Register()` 注册你的自定义 provider。

## 示例代码

完整示例请参考：

- [基础用法](examples/basic/06-all-providers/main.go) - 传统方式
- [Registry 用法](examples/basic/13-provider-registry/main.go) - Registry 方式
- [Registry 文档](llm/registry/README.md) - 完整 Registry 指南

## 相关文档

- [Provider Registry API](llm/registry/README.md)
- [Contrib Providers](contrib/llm-providers/)
- [LLM Provider 开发指南](docs/guides/LLM_PROVIDERS.md)
