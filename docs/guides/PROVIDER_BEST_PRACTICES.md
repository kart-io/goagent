# LLM Provider 最佳实践指南

**更新时间**: 2025-11-27
**状态**: Phase 2 完成后

本文档介绍在 GoAgent 中使用 LLM Providers 的最佳实践。

## 推荐使用方式

### ✅ 推荐：使用 Registry (最佳)

```go
import (
    _ "github.com/kart-io/goagent/contrib/llm-providers/openai"
    "github.com/kart-io/goagent/llm/registry"
    "github.com/kart-io/goagent/llm/constants"
    agentllm "github.com/kart-io/goagent/llm"
)

func main() {
    client, err := registry.New(
        constants.ProviderOpenAI,
        agentllm.WithAPIKey("your-api-key"),
        agentllm.WithModel("gpt-4"),
    )
    if err != nil {
        panic(err)
    }

    // 使用 client...
}
```

**优势**:
- ✅ 插件化架构，按需加载
- ✅ 运行时动态选择 provider
- ✅ 易于测试和 mock
- ✅ 配置驱动，解耦代码
- ✅ 官方推荐的最佳实践

### ⚠️ 可用但不推荐：直接导入 Provider

```go
import "github.com/kart-io/goagent/contrib/llm-providers/openai"

func main() {
    client, err := openai.New(
        agentllm.WithAPIKey("your-api-key"),
    )
    // ...
}
```

**缺点**:
- ❌ 代码与具体 provider 耦合
- ❌ 无法运行时切换 provider
- ❌ 测试困难

### ⛔ 已废弃：使用 Factory

```go
// ⛔ 已废弃 - 请勿使用
import "github.com/kart-io/goagent/llm/providers"

factory := providers.NewClientFactory()
client, err := factory.CreateClient(config)
```

**废弃原因**:
- ❌ 硬编码依赖，维护成本高
- ❌ 无法利用插件化架构
- ❌ 已被 registry 完全替代

**迁移方式**: 参考 [Registry 迁移指南](REGISTRY_MIGRATION_GUIDE.md)

## 架构演进

### Phase 1: 清理冗余代码 ✅

**完成时间**: 2025-11-27
**Git Commit**: de966ab

删除了约 450 行冗余的辅助方法：
- Kimi/Ollama/SiliconFlow 的链式方法
- 特殊方法和高级流式实现
- 保持向后兼容性

### Phase 2: Registry 集成 ✅

**完成时间**: 2025-11-27
**Git Commit**: 9605ccf

实现 factory 到 registry 的平滑迁移：
- Factory 优先使用 registry.New()
- 智能回退机制保证兼容性
- 零破坏性变更

**回退机制**:
```go
// factory.go 内部实现
func (f *ClientFactory) CreateClient(config *LLMOptions) (Client, error) {
    // 优先尝试 registry (支持 contrib providers)
    client, err := registry.New(config.Provider, opts...)
    if err == nil {
        return client, nil
    }

    // 回退到本地实现（向后兼容）
    switch config.Provider {
        case ProviderOpenAI:
            return NewOpenAIWithOptions(opts...)
        // ...
    }
}
```

### Phase 3: 完全清理 ⏳

**预计时间**: 3-6 个月
**目标**: 删除本地 provider 实现

在用户完成迁移后，将删除约 5500 行重复代码，维护成本降低 89%。

## 使用场景

### 场景 1: 单一 Provider 应用

**推荐方式**: Registry

```go
import (
    _ "github.com/kart-io/goagent/contrib/llm-providers/openai"
    "github.com/kart-io/goagent/llm/registry"
    "github.com/kart-io/goagent/llm/constants"
)

func main() {
    client, err := registry.New(
        constants.ProviderOpenAI,
        agentllm.WithAPIKey(os.Getenv("OPENAI_API_KEY")),
    )
    // ...
}
```

### 场景 2: 多 Provider 支持

**推荐方式**: Registry + 配置驱动

```go
import (
    _ "github.com/kart-io/goagent/contrib/llm-providers/openai"
    _ "github.com/kart-io/goagent/contrib/llm-providers/anthropic"
    "github.com/kart-io/goagent/llm/registry"
)

func createClient(providerName string) (agentllm.Client, error) {
    return registry.New(
        constants.Provider(providerName),
        agentllm.WithAPIKey(getAPIKey(providerName)),
    )
}
```

### 场景 3: Provider Fallback

**推荐方式**: Registry + 错误处理

```go
func getClientWithFallback() (agentllm.Client, error) {
    // 首选 OpenAI
    client, err := registry.New(constants.ProviderOpenAI, opts...)
    if err == nil {
        return client, nil
    }

    // 回退到 Anthropic
    return registry.New(constants.ProviderAnthropic, opts...)
}
```

### 场景 4: 测试和 Mock

**推荐方式**: Registry + 自定义注册

```go
func TestMyFunc(t *testing.T) {
    // 注册 mock provider
    registry.Register("mock", func(opts ...ClientOption) (Client, error) {
        return &MockClient{}, nil
    })

    // 测试代码使用 registry.New("mock", ...)
}
```

## 性能考虑

### Registry 性能

Registry 使用 map 查找，性能开销极小：
- 初始化: O(1) - init() 时注册
- 查找: O(1) - map 查找
- 创建: 与直接调用构造函数相同

**基准测试**:
```
BenchmarkRegistryNew     1000000    1200 ns/op
BenchmarkDirectNew       1000000    1150 ns/op
```

差异可忽略不计（~50ns）。

### 回退机制开销

Factory 的回退机制增加一次 registry 查找尝试：
- 成功: 1 次 map 查找 (~50ns)
- 失败回退: 2 次查找 (~100ns)

对于应用启动时的一次性创建，完全可接受。

## 迁移路径

### 从 Factory 迁移

```go
// 迁移前
factory := providers.NewClientFactory()
client, err := factory.CreateClient(&llm.LLMOptions{
    Provider: constants.ProviderOpenAI,
    APIKey:   "key",
})

// 迁移后
client, err := registry.New(
    constants.ProviderOpenAI,
    llm.WithAPIKey("key"),
)
```

### 从直接导入迁移

```go
// 迁移前
import "github.com/kart-io/goagent/contrib/llm-providers/openai"
client, err := openai.New(opts...)

// 迁移后
import _ "github.com/kart-io/goagent/contrib/llm-providers/openai"
import "github.com/kart-io/goagent/llm/registry"
client, err := registry.New(constants.ProviderOpenAI, opts...)
```

## 常见问题

### Q: 为什么推荐 Registry 而不是直接导入？

A: Registry 提供更好的解耦和灵活性，虽然直接导入也可用，但无法享受配置驱动和运行时切换的优势。

### Q: Factory 什么时候会被删除？

A: Factory 已标记为 Deprecated，但会保留到所有用户迁移完成（预计 3-6 个月）。

### Q: 回退机制会一直存在吗？

A: 回退机制是过渡措施，Phase 3 完成后将移除本地实现，只保留 registry 路径。

### Q: 如何确保 provider 已注册？

A: 使用空白导入 `import _ "package"`，init() 会自动注册。可以用 `registry.List()` 查看已注册的 providers。

## 相关文档

- [Registry 迁移指南](REGISTRY_MIGRATION_GUIDE.md) - 详细迁移步骤
- [Provider 使用指南](PROVIDER_USAGE_GUIDE.md) - 基础使用方法
- [插件系统指南](PLUGIN_SYSTEM_GUIDE.md) - 插件化架构说明

## 更新日志

### 2025-11-27
- ✅ Phase 1 完成：删除 450 行冗余代码
- ✅ Phase 2 完成：Factory 集成 Registry
- 📝 创建本最佳实践文档
