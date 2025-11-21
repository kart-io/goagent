# LLM 提供商指南

## 概述

GoAgent 支持多种 LLM 提供商，提供统一的接口抽象。本指南详细说明各提供商的配置和使用方法。

## 支持的提供商

| 提供商 | 常量 | 特点 |
|-------|------|------|
| OpenAI | `ProviderOpenAI` | GPT 系列，广泛支持 |
| Anthropic | `ProviderAnthropic` | Claude 系列，长上下文 |
| Google Gemini | `ProviderGemini` | Gemini 系列 |
| DeepSeek | `ProviderDeepSeek` | 高性价比 |
| Cohere | `ProviderCohere` | 企业级 NLP |
| HuggingFace | `ProviderHuggingFace` | 开源模型 |
| Ollama | `ProviderOllama` | 本地部署 |
| SiliconFlow | `ProviderSiliconFlow` | 国内服务 |
| Kimi | `ProviderKimi` | 月之暗面 |

## 统一接口

所有提供商都实现 `Client` 接口：

```go
type Client interface {
    // 文本补全
    Complete(ctx context.Context, req *CompletionRequest) (*CompletionResponse, error)

    // 对话
    Chat(ctx context.Context, messages []Message) (*CompletionResponse, error)

    // 返回提供商类型
    Provider() Provider

    // 检查可用性
    IsAvailable() bool
}
```

流式客户端额外实现 `StreamClient` 接口：

```go
type StreamClient interface {
    Client

    // 流式补全
    CompleteStream(ctx context.Context, req *CompletionRequest) (<-chan *StreamChunk, error)

    // 流式对话
    ChatStream(ctx context.Context, messages []Message) (<-chan *StreamChunk, error)
}
```

## 提供商配置

### OpenAI

```go
import "github.com/kart-io/goagent/llm/providers"

// 基本配置
client := providers.NewOpenAIClient(
    os.Getenv("OPENAI_API_KEY"),
    "gpt-4",
)

// 高级配置
client := providers.NewOpenAIClientWithConfig(providers.OpenAIConfig{
    APIKey:      os.Getenv("OPENAI_API_KEY"),
    Model:       "gpt-4-turbo",
    BaseURL:     "https://api.openai.com/v1",  // 可自定义端点
    MaxTokens:   4096,
    Temperature: 0.7,
    Timeout:     60,
})
```

**支持的模型：**

- `gpt-4`
- `gpt-4-turbo`
- `gpt-4o`
- `gpt-3.5-turbo`

### Anthropic

```go
import "github.com/kart-io/goagent/llm/providers"

// 基本配置
client := providers.NewAnthropicClient(
    os.Getenv("ANTHROPIC_API_KEY"),
    "claude-3-sonnet-20240229",
)

// 高级配置
client := providers.NewAnthropicClientWithConfig(providers.AnthropicConfig{
    APIKey:      os.Getenv("ANTHROPIC_API_KEY"),
    Model:       "claude-3-opus-20240229",
    MaxTokens:   4096,
    Temperature: 0.7,
})
```

**支持的模型：**

- `claude-3-opus-20240229`
- `claude-3-sonnet-20240229`
- `claude-3-haiku-20240307`

### Google Gemini

```go
import "github.com/kart-io/goagent/llm/providers"

// 基本配置
client := providers.NewGeminiClient(
    os.Getenv("GOOGLE_API_KEY"),
    "gemini-pro",
)

// 高级配置
client := providers.NewGeminiClientWithConfig(providers.GeminiConfig{
    APIKey:      os.Getenv("GOOGLE_API_KEY"),
    Model:       "gemini-1.5-pro",
    MaxTokens:   4096,
    Temperature: 0.7,
})
```

**支持的模型：**

- `gemini-pro`
- `gemini-1.5-pro`
- `gemini-1.5-flash`

### DeepSeek

```go
import "github.com/kart-io/goagent/llm/providers"

// 基本配置
client := providers.NewDeepSeekClient(
    os.Getenv("DEEPSEEK_API_KEY"),
    "deepseek-chat",
)

// 高级配置
client := providers.NewDeepSeekClientWithConfig(providers.DeepSeekConfig{
    APIKey:      os.Getenv("DEEPSEEK_API_KEY"),
    Model:       "deepseek-coder",
    BaseURL:     "https://api.deepseek.com",
    MaxTokens:   4096,
    Temperature: 0.7,
})
```

**支持的模型：**

- `deepseek-chat`
- `deepseek-coder`

### Cohere

```go
import "github.com/kart-io/goagent/llm/providers"

// 基本配置
client := providers.NewCohereClient(
    os.Getenv("COHERE_API_KEY"),
    "command",
)

// 高级配置
client := providers.NewCohereClientWithConfig(providers.CohereConfig{
    APIKey:      os.Getenv("COHERE_API_KEY"),
    Model:       "command-r-plus",
    MaxTokens:   4096,
    Temperature: 0.7,
})
```

**支持的模型：**

- `command`
- `command-r`
- `command-r-plus`

### HuggingFace

```go
import "github.com/kart-io/goagent/llm/providers"

// 基本配置
client := providers.NewHuggingFaceClient(
    os.Getenv("HUGGINGFACE_API_KEY"),
    "meta-llama/Llama-2-70b-chat-hf",
)

// 高级配置
client := providers.NewHuggingFaceClientWithConfig(providers.HuggingFaceConfig{
    APIKey:      os.Getenv("HUGGINGFACE_API_KEY"),
    Model:       "mistralai/Mixtral-8x7B-Instruct-v0.1",
    MaxTokens:   4096,
    Temperature: 0.7,
})
```

### Ollama（本地部署）

```go
import "github.com/kart-io/goagent/llm/providers"

// 基本配置
client := providers.NewOllamaClient(
    "http://localhost:11434",
    "llama2",
)

// 高级配置
client := providers.NewOllamaClientWithConfig(providers.OllamaConfig{
    BaseURL:     "http://localhost:11434",
    Model:       "codellama",
    MaxTokens:   4096,
    Temperature: 0.7,
})
```

**常用模型：**

- `llama2`
- `codellama`
- `mistral`
- `mixtral`

### SiliconFlow

```go
import "github.com/kart-io/goagent/llm/providers"

// 基本配置
client := providers.NewSiliconFlowClient(
    os.Getenv("SILICONFLOW_API_KEY"),
    "Qwen/Qwen2-72B-Instruct",
)
```

### Kimi

```go
import "github.com/kart-io/goagent/llm/providers"

// 基本配置
client := providers.NewKimiClient(
    os.Getenv("KIMI_API_KEY"),
    "moonshot-v1-8k",
)
```

**支持的模型：**

- `moonshot-v1-8k`
- `moonshot-v1-32k`
- `moonshot-v1-128k`

## 使用示例

### 基本对话

```go
client := providers.NewOpenAIClient(apiKey, "gpt-4")

messages := []llm.Message{
    llm.SystemMessage("你是一个有帮助的助手。"),
    llm.UserMessage("你好！"),
}

resp, err := client.Chat(context.Background(), messages)
if err != nil {
    log.Fatal(err)
}

fmt.Println(resp.Content)
```

### 流式对话

```go
client := providers.NewOpenAIClient(apiKey, "gpt-4")

messages := []llm.Message{
    llm.UserMessage("请写一首诗。"),
}

stream, err := client.ChatStream(context.Background(), messages)
if err != nil {
    log.Fatal(err)
}

for chunk := range stream {
    if chunk.Error != nil {
        log.Printf("错误: %v\n", chunk.Error)
        break
    }
    fmt.Print(chunk.Delta)
    if chunk.Done {
        break
    }
}
```

### 自定义参数

```go
client := providers.NewOpenAIClient(apiKey, "gpt-4")

req := &llm.CompletionRequest{
    Messages: []llm.Message{
        llm.UserMessage("生成一个创意故事开头。"),
    },
    Temperature: 0.9,  // 更高的创造性
    MaxTokens:   500,
    TopP:        0.95,
}

resp, err := client.Complete(context.Background(), req)
if err != nil {
    log.Fatal(err)
}

fmt.Println(resp.Content)
```

## 提供商切换

GoAgent 的统一接口使得切换提供商非常简单：

```go
func createClient(provider string) llm.Client {
    switch provider {
    case "openai":
        return providers.NewOpenAIClient(
            os.Getenv("OPENAI_API_KEY"),
            "gpt-4",
        )
    case "anthropic":
        return providers.NewAnthropicClient(
            os.Getenv("ANTHROPIC_API_KEY"),
            "claude-3-sonnet-20240229",
        )
    case "deepseek":
        return providers.NewDeepSeekClient(
            os.Getenv("DEEPSEEK_API_KEY"),
            "deepseek-chat",
        )
    default:
        return providers.NewOllamaClient(
            "http://localhost:11434",
            "llama2",
        )
    }
}

// 使用
client := createClient(os.Getenv("LLM_PROVIDER"))
agent := builder.NewAgentBuilder(client).Build()
```

## Token 使用统计

所有提供商响应都包含 Token 使用信息：

```go
resp, err := client.Chat(ctx, messages)
if err != nil {
    log.Fatal(err)
}

if resp.Usage != nil {
    fmt.Printf("输入 Token: %d\n", resp.Usage.PromptTokens)
    fmt.Printf("输出 Token: %d\n", resp.Usage.CompletionTokens)
    fmt.Printf("总 Token: %d\n", resp.Usage.TotalTokens)
}
```

## 错误处理

```go
resp, err := client.Chat(ctx, messages)
if err != nil {
    // 检查错误类型
    switch {
    case strings.Contains(err.Error(), "rate limit"):
        // 速率限制，等待重试
        time.Sleep(time.Second * 5)
    case strings.Contains(err.Error(), "invalid api key"):
        // API 密钥错误
        log.Fatal("请检查 API 密钥")
    default:
        log.Printf("LLM 调用失败: %v\n", err)
    }
}
```

## 最佳实践

### 1. 使用环境变量

```bash
export OPENAI_API_KEY="sk-..."
export ANTHROPIC_API_KEY="sk-ant-..."
```

### 2. 设置超时

```go
ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
defer cancel()

resp, err := client.Chat(ctx, messages)
```

### 3. 重试机制

```go
func chatWithRetry(client llm.Client, messages []llm.Message, maxRetries int) (*llm.CompletionResponse, error) {
    var lastErr error
    for i := 0; i < maxRetries; i++ {
        resp, err := client.Chat(context.Background(), messages)
        if err == nil {
            return resp, nil
        }
        lastErr = err
        time.Sleep(time.Second * time.Duration(i+1))
    }
    return nil, lastErr
}
```

### 4. 选择合适的模型

根据任务选择合适的模型：

- **简单对话**：GPT-3.5-Turbo、DeepSeek Chat
- **复杂推理**：GPT-4、Claude-3-Opus
- **代码生成**：DeepSeek Coder、CodeLlama
- **长文本**：Claude-3（200K）、Kimi（128K）

### 5. 成本优化

- 使用较小的模型处理简单任务
- 限制 MaxTokens
- 缓存常见请求的响应

## 相关文档

- [快速入门](QUICKSTART.md)
- [架构概述](../architecture/ARCHITECTURE.md)
