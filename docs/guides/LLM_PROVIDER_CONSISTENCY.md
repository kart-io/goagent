# LLM Provider 接口一致性分析

## 接口一致性对比表

所有 LLM Provider 都实现了统一的 `llm.Client` 接口，确保了完全的互换性。

### 核心接口方法

| 方法 | OpenAI | DeepSeek | Gemini | **Ollama** | 说明 |
|------|--------|----------|--------|------------|------|
| `Complete(ctx, req)` | ✅ | ✅ | ✅ | ✅ | 文本补全 |
| `Chat(ctx, messages)` | ✅ | ✅ | ✅ | ✅ | 对话 |
| `Provider()` | ✅ | ✅ | ✅ | ✅ | 返回提供商类型 |
| `IsAvailable()` | ✅ | ✅ | ✅ | ✅ | 检查可用性 |

### 参数一致性

所有 Provider 的 `Complete` 方法都接受相同的参数：

```go
type CompletionRequest struct {
    Messages    []Message  // 消息列表
    Temperature float64    // 温度参数 (0.0-2.0)
    MaxTokens   int       // 最大 token 数
    Model       string    // 模型名称（可选）
    Stop        []string  // 停止序列（可选）
    TopP        float64   // Top-p 采样（可选）
}
```

### 返回值一致性

所有 Provider 返回相同的响应格式：

```go
type CompletionResponse struct {
    Content      string  // 生成的内容
    Model        string  // 使用的模型
    TokensUsed   int     // 使用的 token 数
    FinishReason string  // 结束原因
    Provider     string  // 提供商标识
}
```

## 使用示例对比

### 1. OpenAI

```go
config := &llm.Config{
    Provider: llm.ProviderOpenAI,
    APIKey:   "your-api-key",
    Model:    "gpt-3.5-turbo",
}
client, _ := providers.NewOpenAI(config)

response, _ := client.Chat(ctx, []llm.Message{
    llm.UserMessage("Hello"),
})
```

### 2. DeepSeek

```go
config := &llm.Config{
    Provider: llm.ProviderDeepSeek,
    APIKey:   "your-api-key",
    Model:    "deepseek-chat",
}
client, _ := providers.NewDeepSeek(config)

response, _ := client.Chat(ctx, []llm.Message{
    llm.UserMessage("Hello"),
})
```

### 3. Gemini

```go
config := &llm.Config{
    Provider: llm.ProviderGemini,
    APIKey:   "your-api-key",
    Model:    "gemini-pro",
}
client, _ := providers.NewGemini(config)

response, _ := client.Chat(ctx, []llm.Message{
    llm.UserMessage("Hello"),
})
```

### 4. Ollama（新增）

```go
// 方式 1: 简单创建
client := providers.NewOllamaClientSimple("llama2")

// 方式 2: 详细配置
config := providers.DefaultOllamaConfig()
config.Model = "mistral"
client := providers.NewOllamaClient(config)

// 使用方式完全一致
response, _ := client.Chat(ctx, []llm.Message{
    llm.UserMessage("Hello"),
})
```

## 切换 Provider 的最佳实践

### 1. 工厂模式

```go
func CreateLLMClient(providerType string) llm.Client {
    switch providerType {
    case "openai":
        return createOpenAIClient()
    case "deepseek":
        return createDeepSeekClient()
    case "gemini":
        return createGeminiClient()
    case "ollama":
        return createOllamaClient()
    default:
        // 默认使用 Ollama（本地）
        return createOllamaClient()
    }
}
```

### 2. 环境变量配置

```go
func GetLLMClient() llm.Client {
    provider := os.Getenv("LLM_PROVIDER")

    // 所有 provider 使用相同的接口
    var client llm.Client

    switch provider {
    case "ollama":
        client = providers.NewOllamaClientSimple("llama2")
    case "openai":
        config := &llm.Config{
            Provider: llm.ProviderOpenAI,
            APIKey:   os.Getenv("OPENAI_API_KEY"),
        }
        client, _ = providers.NewOpenAI(config)
    // ... 其他 provider
    }

    return client
}
```

### 3. 自动降级策略

```go
func GetLLMClientWithFallback() llm.Client {
    // 优先使用本地 Ollama
    ollamaClient := providers.NewOllamaClientSimple("llama2")
    if ollamaClient.IsAvailable() {
        return ollamaClient
    }

    // 降级到 OpenAI
    if apiKey := os.Getenv("OPENAI_API_KEY"); apiKey != "" {
        config := &llm.Config{
            Provider: llm.ProviderOpenAI,
            APIKey:   apiKey,
        }
        if client, err := providers.NewOpenAI(config); err == nil {
            return client
        }
    }

    // 最后尝试 DeepSeek
    // ...

    return nil
}
```

## Ollama Provider 的特殊功能

虽然所有 Provider 都实现了相同的核心接口，但 Ollama 还提供了一些额外的功能：

```go
// Ollama 特有的功能
client := providers.NewOllamaClientSimple("llama2")

// 1. 列出本地模型
models, err := client.ListModels()

// 2. 拉取新模型
err := client.PullModel("mistral")

// 3. 链式配置
client.WithModel("codellama").
       WithTemperature(0.5).
       WithMaxTokens(2000)
```

## 总结

✅ **完全一致的接口**：Ollama provider 与其他 provider 实现了完全相同的 `llm.Client` 接口

✅ **无缝切换**：可以在不修改业务代码的情况下切换不同的 LLM provider

✅ **统一的参数和返回值**：所有 provider 使用相同的请求和响应格式

✅ **向下兼容**：添加 Ollama 不会破坏现有代码

这种设计允许开发者：
1. **灵活选择**：根据需求选择合适的 LLM provider
2. **成本优化**：在开发环境使用免费的 Ollama，生产环境使用 OpenAI
3. **隐私保护**：敏感数据使用本地 Ollama，其他数据使用云服务
4. **故障恢复**：实现自动降级和故障转移