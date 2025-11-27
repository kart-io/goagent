# Provider Registry 迁移指南

本文档提供从传统 provider 使用方式迁移到 Provider Registry 的详细指南。

## 为什么要迁移？

### 使用 Provider Registry 的优势

1. **运行时灵活性** - 可以根据配置动态选择 provider
2. **解耦架构** - 代码不再直接依赖具体的 provider 实现
3. **易于测试** - 可以轻松 mock providers 进行单元测试
4. **配置驱动** - 支持通过配置文件或环境变量选择 provider
5. **Fallback 支持** - 可以实现自动 fallback 到备用 providers
6. **插件式架构** - 按需导入 providers，减少依赖

### 何时迁移？

建议在以下场景迁移到 Registry：

- ✅ 需要支持多个 LLM providers
- ✅ 需要根据环境（开发/生产）切换 provider
- ✅ 需要实现 provider fallback 机制
- ✅ 需要配置驱动的应用
- ✅ 需要更好的测试性

## 快速迁移步骤

### 步骤 1: 添加空白导入

**迁移前**:
```go
import "github.com/kart-io/goagent/contrib/llm-providers/openai"
```

**迁移后**:
```go
import (
    _ "github.com/kart-io/goagent/contrib/llm-providers/openai"  // 添加空白导入
    "github.com/kart-io/goagent/llm/registry"                     // 导入 registry
    "github.com/kart-io/goagent/llm/constants"                    // 导入常量
)
```

### 步骤 2: 替换构造函数调用

**迁移前**:
```go
client, err := openai.New(
    agentllm.WithAPIKey("your-api-key"),
    agentllm.WithModel("gpt-3.5-turbo"),
)
```

**迁移后**:
```go
client, err := registry.New(
    constants.ProviderOpenAI,  // 使用 provider 常量
    agentllm.WithAPIKey("your-api-key"),
    agentllm.WithModel("gpt-3.5-turbo"),
)
```

### 步骤 3: 测试并验证

运行测试确保一切正常工作：

```bash
go test ./...
```

## 详细迁移示例

### 示例 1: 简单应用

**迁移前** (`main.go`):
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
        agentllm.WithAPIKey("sk-..."),
        agentllm.WithModel("gpt-3.5-turbo"),
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

**迁移后** (`main.go`):
```go
package main

import (
    "context"
    "fmt"

    _ "github.com/kart-io/goagent/contrib/llm-providers/openai"  // 空白导入
    agentllm "github.com/kart-io/goagent/llm"
    "github.com/kart-io/goagent/llm/constants"
    "github.com/kart-io/goagent/llm/registry"
)

func main() {
    client, err := registry.New(
        constants.ProviderOpenAI,  // 使用常量
        agentllm.WithAPIKey("sk-..."),
        agentllm.WithModel("gpt-3.5-turbo"),
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

### 示例 2: 多 Provider 应用

**迁移前**:
```go
// 需要为每个 provider 写不同的代码
import (
    "github.com/kart-io/goagent/contrib/llm-providers/openai"
    "github.com/kart-io/goagent/contrib/llm-providers/deepseek"
    "github.com/kart-io/goagent/contrib/llm-providers/gemini"
)

func createOpenAI() (agentllm.Client, error) {
    return openai.New(agentllm.WithAPIKey("key1"))
}

func createDeepSeek() (agentllm.Client, error) {
    return deepseek.New(agentllm.WithAPIKey("key2"))
}

func createGemini() (agentllm.Client, error) {
    return gemini.New(agentllm.WithAPIKey("key3"))
}
```

**迁移后**:
```go
// 统一的创建逻辑
import (
    _ "github.com/kart-io/goagent/contrib/llm-providers/openai"
    _ "github.com/kart-io/goagent/contrib/llm-providers/deepseek"
    _ "github.com/kart-io/goagent/contrib/llm-providers/gemini"
)

func createClient(provider constants.Provider, apiKey string) (agentllm.Client, error) {
    return registry.New(provider, agentllm.WithAPIKey(apiKey))
}

// 使用
client1, _ := createClient(constants.ProviderOpenAI, "key1")
client2, _ := createClient(constants.ProviderDeepSeek, "key2")
client3, _ := createClient(constants.ProviderGemini, "key3")
```

### 示例 3: 配置驱动应用

**迁移前** - 硬编码 provider:
```go
func main() {
    // 硬编码使用 OpenAI
    client, _ := openai.New(agentllm.WithAPIKey(os.Getenv("API_KEY")))
    // ...
}
```

**迁移后** - 配置驱动:
```go
// config.yaml
llm:
  provider: "openai"  # 可以随时改为 "deepseek", "gemini" 等
  api_key: "${API_KEY}"
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

func createClientFromConfig(cfg *Config) (agentllm.Client, error) {
    // 映射配置到 provider 常量
    providerMap := map[string]constants.Provider{
        "openai":   constants.ProviderOpenAI,
        "deepseek": constants.ProviderDeepSeek,
        "gemini":   constants.ProviderGemini,
        "ollama":   constants.ProviderOllama,
    }

    provider, ok := providerMap[cfg.LLM.Provider]
    if !ok {
        return nil, fmt.Errorf("unknown provider: %s", cfg.LLM.Provider)
    }

    return registry.New(
        provider,
        agentllm.WithAPIKey(cfg.LLM.APIKey),
        agentllm.WithModel(cfg.LLM.Model),
    )
}
```

## Provider 映射表

从传统导入迁移到 Registry 的映射表：

| 传统方式 | Registry 方式 | Provider 常量 |
|---------|---------------|--------------|
| `openai.New(...)` | `registry.New(constants.ProviderOpenAI, ...)` | `constants.ProviderOpenAI` |
| `deepseek.New(...)` | `registry.New(constants.ProviderDeepSeek, ...)` | `constants.ProviderDeepSeek` |
| `gemini.New(...)` | `registry.New(constants.ProviderGemini, ...)` | `constants.ProviderGemini` |
| `anthropic.New(...)` | `registry.New(constants.ProviderAnthropic, ...)` | `constants.ProviderAnthropic` |
| `cohere.New(...)` | `registry.New(constants.ProviderCohere, ...)` | `constants.ProviderCohere` |
| `huggingface.New(...)` | `registry.New(constants.ProviderHuggingFace, ...)` | `constants.ProviderHuggingFace` |
| `ollama.New(...)` | `registry.New(constants.ProviderOllama, ...)` | `constants.ProviderOllama` |
| `kimi.New(...)` | `registry.New(constants.ProviderKimi, ...)` | `constants.ProviderKimi` |
| `siliconflow.New(...)` | `registry.New(constants.ProviderSiliconFlow, ...)` | `constants.ProviderSiliconFlow` |

## 高级迁移模式

### 模式 1: 渐进式迁移

不需要一次性迁移所有代码。两种方式可以共存：

```go
import (
    "github.com/kart-io/goagent/contrib/llm-providers/openai"  // 传统导入
    _ "github.com/kart-io/goagent/contrib/llm-providers/deepseek"  // Registry 导入
)

func main() {
    // 传统方式（旧代码保持不变）
    client1, _ := openai.New(agentllm.WithAPIKey("key1"))

    // Registry 方式（新代码使用 registry）
    client2, _ := registry.New(constants.ProviderDeepSeek, agentllm.WithAPIKey("key2"))

    // 两者都正常工作
}
```

### 模式 2: 条件编译

使用 build tags 在不同环境使用不同方式：

```go
// +build dev

import (
    _ "github.com/kart-io/goagent/contrib/llm-providers/ollama"  // 开发环境用 Ollama
)

func createClient() (agentllm.Client, error) {
    return registry.New(constants.ProviderOllama, agentllm.WithModel("llama2"))
}
```

```go
// +build prod

import (
    _ "github.com/kart-io/goagent/contrib/llm-providers/openai"  // 生产环境用 OpenAI
)

func createClient() (agentllm.Client, error) {
    return registry.New(constants.ProviderOpenAI, agentllm.WithAPIKey(os.Getenv("OPENAI_API_KEY")))
}
```

### 模式 3: 工厂模式

封装 provider 创建逻辑：

```go
type ClientFactory struct {
    defaultProvider constants.Provider
    apiKeys         map[constants.Provider]string
}

func NewClientFactory(defaultProvider constants.Provider) *ClientFactory {
    return &ClientFactory{
        defaultProvider: defaultProvider,
        apiKeys:         make(map[constants.Provider]string),
    }
}

func (f *ClientFactory) SetAPIKey(provider constants.Provider, key string) {
    f.apiKeys[provider] = key
}

func (f *ClientFactory) CreateClient(provider constants.Provider, opts ...agentllm.ClientOption) (agentllm.Client, error) {
    // 自动添加 API key
    if key, ok := f.apiKeys[provider]; ok {
        opts = append(opts, agentllm.WithAPIKey(key))
    }

    return registry.New(provider, opts...)
}

func (f *ClientFactory) CreateDefaultClient(opts ...agentllm.ClientOption) (agentllm.Client, error) {
    return f.CreateClient(f.defaultProvider, opts...)
}
```

## 测试迁移

### 迁移前的测试

```go
func TestWithOpenAI(t *testing.T) {
    client, err := openai.New(agentllm.WithAPIKey("test-key"))
    // 无法轻松 mock
}
```

### 迁移后的测试

```go
func TestWithRegistry(t *testing.T) {
    // 清理 registry
    defer registry.Clear()

    // 注册 mock provider
    registry.Register(constants.ProviderOpenAI, func(opts ...agentllm.ClientOption) (agentllm.Client, error) {
        return &MockClient{}, nil
    })

    // 测试代码
    client, err := registry.New(constants.ProviderOpenAI)
    assert.NoError(t, err)
    assert.IsType(t, &MockClient{}, client)
}
```

## 常见问题

### Q: 迁移后性能会受影响吗？

A: 不会。Registry 查找是 O(1) 的 map 操作，性能影响可以忽略不计。

### Q: 必须迁移吗？

A: 不必须。传统方式继续支持，两种方式可以共存。只有在需要 Registry 的优势时才迁移。

### Q: 迁移工作量大吗？

A: 通常很小。对于简单应用，只需要修改导入和构造函数调用。

### Q: 如何验证迁移成功？

A: 运行现有测试，确保所有功能正常。使用 `registry.List()` 验证 providers 已注册。

### Q: 可以部分迁移吗？

A: 可以。建议先迁移新代码，旧代码保持不变，渐进式迁移。

## 迁移检查清单

完成迁移后，使用以下清单验证：

- [ ] 所有使用的 providers 都有空白导入
- [ ] 所有 `provider.New()` 调用已替换为 `registry.New()`
- [ ] 使用正确的 provider 常量（如 `constants.ProviderOpenAI`）
- [ ] 所有测试通过
- [ ] 运行 `registry.List()` 验证 providers 已注册
- [ ] 文档已更新
- [ ] 团队成员已了解新的使用方式

## 获取帮助

如果在迁移过程中遇到问题：

1. 查看 [Registry 完整文档](../llm/registry/README.md)
2. 查看 [Provider 使用指南](../docs/guides/PROVIDER_USAGE_GUIDE.md)
3. 查看示例代码：
   - [传统方式示例](../examples/basic/06-all-providers/)
   - [Registry 方式示例](../examples/basic/13-provider-registry/)
4. 提交 Issue 到 GitHub

## 总结

Provider Registry 迁移的关键点：

1. ✅ **简单** - 只需修改导入和构造函数
2. ✅ **安全** - 完全向后兼容，可以渐进式迁移
3. ✅ **灵活** - 带来运行时灵活性和配置驱动能力
4. ✅ **可选** - 不是强制的，根据需求选择

开始你的迁移之旅，享受 Provider Registry 带来的灵活性！
