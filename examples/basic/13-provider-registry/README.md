# Provider Registry 完整示例

本示例展示如何使用 GoAgent 的 Provider Registry 系统动态管理和创建 LLM Providers。

## 什么是 Provider Registry？

Provider Registry 是一个动态的 provider 注册和管理系统，允许：

- ✅ **运行时动态选择** - 根据配置选择 provider
- ✅ **插件式架构** - 按需导入 providers
- ✅ **自动发现** - 列出所有可用 providers
- ✅ **统一接口** - 使用相同 API 创建所有 providers
- ✅ **易于测试** - 轻松 mock 和切换 providers

## 运行示例

```bash
go run main.go
```

**无需 API Key** - 示例展示 registry 功能，不发送实际请求。

## 示例包含的内容

### 1. 列出所有已注册的 Providers

```go
providers := registry.List()
for _, provider := range providers {
    fmt.Printf("  - %s\n", provider)
}
```

**输出**:
```
已注册的 Providers:
  - openai
  - deepseek
  - gemini
  - anthropic
  - cohere
  - huggingface
  - ollama
  - kimi
  - siliconflow
```

### 2. 检查 Provider 是否已注册

```go
if registry.IsRegistered(constants.ProviderOpenAI) {
    fmt.Println("✓ OpenAI provider 已注册")
}
```

### 3. 使用 registry.New 创建 Provider

```go
client, err := registry.New(
    constants.ProviderOpenAI,
    agentllm.WithAPIKey("your-api-key"),
    agentllm.WithModel("gpt-3.5-turbo"),
)
```

### 4. 手动获取工厂函数

```go
factory, err := registry.Get(constants.ProviderDeepSeek)
if err != nil {
    log.Fatal(err)
}

client, err := factory(
    agentllm.WithAPIKey("your-api-key"),
    agentllm.WithModel("deepseek-chat"),
)
```

### 5. 动态选择 Provider

```go
// 从配置读取 provider 名称
selectedProvider := constants.ProviderOpenAI

client, err := registry.New(
    selectedProvider,
    agentllm.WithAPIKey("your-api-key"),
)
```

### 6. 批量创建多个 Providers

```go
providerNames := []constants.Provider{
    constants.ProviderOpenAI,
    constants.ProviderDeepSeek,
    constants.ProviderGemini,
}

clients := make(map[constants.Provider]agentllm.Client)
for _, providerName := range providerNames {
    if registry.IsRegistered(providerName) {
        client, err := registry.New(
            providerName,
            agentllm.WithAPIKey("your-api-key"),
        )
        if err == nil {
            clients[providerName] = client
        }
    }
}
```

### 7. Provider Fallback 链

```go
// 定义 provider 优先级列表
providerPriority := []constants.Provider{
    constants.ProviderOpenAI,
    constants.ProviderDeepSeek,
    constants.ProviderGemini,
    constants.ProviderOllama, // Fallback 到本地模型
}

// 按优先级尝试创建
var client agentllm.Client
for _, provider := range providerPriority {
    if !registry.IsRegistered(provider) {
        continue
    }

    client, err = registry.New(provider, agentllm.WithAPIKey("your-api-key"))
    if err == nil {
        fmt.Printf("✓ 成功使用 provider: %s\n", provider)
        break
    }
}
```

## 核心概念

### 自动注册

每个 contrib provider 在其 `init()` 函数中自动注册：

```go
// contrib/llm-providers/openai/provider.go
func init() {
    registry.Register(constants.ProviderOpenAI, func(opts ...agentllm.ClientOption) (agentllm.Client, error) {
        return New(opts...)
    })
}
```

### 空白导入

使用空白导入 `_` 触发 provider 的 `init()` 函数：

```go
import _ "github.com/kart-io/goagent/contrib/llm-providers/openai"
```

这会注册 provider，但不会在代码中直接使用该包。

### Provider Factory

Registry 存储的是工厂函数，而不是 provider 实例：

```go
type ProviderFactory func(...ClientOption) (Client, error)
```

每次调用都会创建新的 provider 实例。

## 实际应用场景

### 场景 1: 配置驱动的应用

```go
// config.yaml
llm:
  provider: "openai"  # 可以改为 "deepseek", "gemini" 等
  api_key: "${OPENAI_API_KEY}"
```

```go
func createClient(config Config) (agentllm.Client, error) {
    var provider constants.Provider
    switch config.LLM.Provider {
    case "openai":
        provider = constants.ProviderOpenAI
    case "deepseek":
        provider = constants.ProviderDeepSeek
    default:
        return nil, fmt.Errorf("unknown provider")
    }

    return registry.New(provider, agentllm.WithAPIKey(config.LLM.APIKey))
}
```

### 场景 2: 多环境支持

```go
// 开发环境使用本地 Ollama
// 生产环境使用 OpenAI
func createClientForEnv() (agentllm.Client, error) {
    if os.Getenv("ENV") == "production" {
        return registry.New(constants.ProviderOpenAI, agentllm.WithAPIKey(os.Getenv("OPENAI_API_KEY")))
    }
    return registry.New(constants.ProviderOllama, agentllm.WithModel("llama2"))
}
```

### 场景 3: A/B 测试

```go
// 随机选择 provider 进行 A/B 测试
func createRandomClient() (agentllm.Client, error) {
    providers := []constants.Provider{
        constants.ProviderOpenAI,
        constants.ProviderDeepSeek,
    }

    selected := providers[rand.Intn(len(providers))]
    return registry.New(selected, agentllm.WithAPIKey(getAPIKey(selected)))
}
```

## 与传统方式对比

### 传统方式（直接导入）

```go
import "github.com/kart-io/goagent/contrib/llm-providers/openai"

client, err := openai.New(agentllm.WithAPIKey("key"))
```

**优点**: 简单、类型安全
**缺点**: 无法运行时切换

### Registry 方式（推荐）

```go
import _ "github.com/kart-io/goagent/contrib/llm-providers/openai"

client, err := registry.New(constants.ProviderOpenAI, agentllm.WithAPIKey("key"))
```

**优点**: 灵活、可配置、易测试
**缺点**: 需要空白导入

## 最佳实践

### 1. 集中管理导入

创建 `providers.go` 文件：

```go
// providers.go
package main

import (
    _ "github.com/kart-io/goagent/contrib/llm-providers/openai"
    _ "github.com/kart-io/goagent/contrib/llm-providers/deepseek"
    _ "github.com/kart-io/goagent/contrib/llm-providers/gemini"
)
```

### 2. 使用配置文件

将 provider 选择放在配置中，而不是硬编码。

### 3. 实现 Fallback

总是有备用 provider，尤其是本地 Ollama 作为最后的 fallback。

### 4. 错误处理

始终检查 provider 是否已注册：

```go
if !registry.IsRegistered(provider) {
    return fmt.Errorf("provider %s not registered", provider)
}
```

## 相关资源

- [Registry 完整文档](../../../llm/registry/README.md)
- [Provider 使用指南](../../../docs/guides/PROVIDER_USAGE_GUIDE.md)
- [Contrib Providers](../../../contrib/llm-providers/)
- [06-all-providers](../06-all-providers/) - 传统方式示例

## 常见问题

### Q: 为什么需要空白导入？

A: 空白导入确保包的 `init()` 函数被调用，触发 provider 注册。

### Q: Registry 有性能开销吗？

A: 几乎没有。Registry 是简单的 map 查找，overhead 可忽略。

### Q: 可以混合使用两种方式吗？

A: 可以。传统方式和 Registry 方式可以在同一项目中共存。

### Q: 如何测试使用 Registry 的代码？

A: 使用 `registry.Clear()` 和 `registry.Register()` 注册 mock provider：

```go
func TestWithMock(t *testing.T) {
    defer registry.Clear()

    registry.Register(constants.ProviderOpenAI, func(...) (Client, error) {
        return &MockClient{}, nil
    })

    // 测试代码
}
```

## 总结

Provider Registry 提供了灵活、强大的方式来管理 LLM Providers：

- ✅ 动态选择 - 运行时根据配置选择
- ✅ 解耦 - 代码与具体 provider 解耦
- ✅ 易测试 - 轻松 mock 和替换
- ✅ 可扩展 - 容易添加新 providers
- ✅ 向后兼容 - 与传统方式共存

开始使用 Registry，让你的 LLM 集成更加灵活！
