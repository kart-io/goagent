<!-- generated-by: gsd-doc-writer -->
# LLM 提供商指南

## 概述

GoAgent 为不同 Provider 提供基础请求接口，但结构化流、原生工具流和 Embedding 支持各不相同。准确签名见 [LLM API 参考](../api/LLM_API.md)，适配器边界见 [Provider 能力表](PROVIDER_CAPABILITIES.md)。下面的模型名称是配置示例，不是当前服务端支持目录；运行前由宿主选择可用模型。

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
    Provider() constants.Provider

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

原生 Agent 工具循环还要求 `llm.ToolStreamClient`，其 `CompleteToolsStream(context.Context, *llm.CompletionRequest, []interfaces.Tool)` 返回 `(<-chan *llm.StreamChunk, error)`。检查 `llm.HasToolStreaming(client)`，不能根据字符串流、包装器名称或旧工具接口推断支持。未实现结构化流的 Provider 不能直接用于 Builder 的 `AsRunnable` 入口。上面的接口定义片段使用 `llm` 包内类型名，`constants` 来自 `llm/constants`。

## 提供商配置

### OpenAI

```go
import (
    "github.com/kart-io/goagent/llm"
    "github.com/kart-io/goagent/llm/providers"
)

// 基本配置
client, err := providers.NewOpenAIWithOptions(
    llm.WithAPIKey(os.Getenv("OPENAI_API_KEY")),
    llm.WithModel("gpt-4"),
)

// 高级配置
client, err := providers.NewOpenAIWithOptions(
    llm.WithAPIKey(os.Getenv("OPENAI_API_KEY")),
    llm.WithModel("gpt-4-turbo"),
    llm.WithBaseURL("https://api.openai.com/v1"),
    llm.WithMaxTokens(4096),
    llm.WithTemperature(0.7),
    llm.WithTimeout(60 * time.Second), // 限制等待响应开始的时间，流式读取不受此限
)
```

**模型配置示例（非当前支持清单）：**

- `gpt-4`
- `gpt-4-turbo`
- `gpt-4o`
- `gpt-3.5-turbo`

### Anthropic

```go
import (
    "github.com/kart-io/goagent/llm"
    "github.com/kart-io/goagent/llm/providers"
)

// 基本配置
client, err := providers.NewAnthropicWithOptions(
    llm.WithAPIKey(os.Getenv("ANTHROPIC_API_KEY")),
    llm.WithModel("claude-3-sonnet-20240229"),
)

// 高级配置
client, err := providers.NewAnthropicWithOptions(
    llm.WithAPIKey(os.Getenv("ANTHROPIC_API_KEY")),
    llm.WithModel("claude-3-opus-20240229"),
    llm.WithMaxTokens(4096),
    llm.WithTemperature(0.7),
)
```

**模型配置示例（非当前支持清单）：**

- `claude-3-opus-20240229`
- `claude-3-sonnet-20240229`
- `claude-3-haiku-20240307`

### Google Gemini

```go
import (
    "github.com/kart-io/goagent/llm"
    "github.com/kart-io/goagent/llm/providers"
)

// 基本配置
client, err := providers.NewGeminiWithOptions(
    llm.WithAPIKey(os.Getenv("GEMINI_API_KEY")),
    llm.WithModel("gemini-pro"),
)

// 高级配置
client, err := providers.NewGeminiWithOptions(
    llm.WithAPIKey(os.Getenv("GEMINI_API_KEY")),
    llm.WithModel("gemini-1.5-pro"),
    llm.WithMaxTokens(4096),
    llm.WithTemperature(0.7),
)
```

**模型配置示例（非当前支持清单）：**

- `gemini-pro`
- `gemini-1.5-pro`
- `gemini-1.5-flash`

### DeepSeek

```go
import (
    "github.com/kart-io/goagent/llm"
    "github.com/kart-io/goagent/llm/providers"
)

// 基本配置
client, err := providers.NewDeepSeekWithOptions(
    llm.WithAPIKey(os.Getenv("DEEPSEEK_API_KEY")),
    llm.WithModel("deepseek-chat"),
)

// 高级配置
client, err := providers.NewDeepSeekWithOptions(
    llm.WithAPIKey(os.Getenv("DEEPSEEK_API_KEY")),
    llm.WithModel("deepseek-coder"),
    llm.WithBaseURL("https://api.deepseek.com"),
    llm.WithMaxTokens(4096),
    llm.WithTemperature(0.7),
)
```

**模型配置示例（非当前支持清单）：**

- `deepseek-chat`
- `deepseek-coder`

### Cohere

```go
import (
    "github.com/kart-io/goagent/llm"
    "github.com/kart-io/goagent/llm/providers"
)

// 基本配置
client, err := providers.NewCohereWithOptions(
    llm.WithAPIKey(os.Getenv("COHERE_API_KEY")),
    llm.WithModel("command"),
)

// 高级配置
client, err := providers.NewCohereWithOptions(
    llm.WithAPIKey(os.Getenv("COHERE_API_KEY")),
    llm.WithModel("command-r-plus"),
    llm.WithMaxTokens(4096),
    llm.WithTemperature(0.7),
)
```

**模型配置示例（非当前支持清单）：**

- `command`
- `command-r`
- `command-r-plus`

### HuggingFace

```go
import (
    "github.com/kart-io/goagent/llm"
    "github.com/kart-io/goagent/llm/providers"
)

// 基本配置
client, err := providers.NewHuggingFaceWithOptions(
    llm.WithAPIKey(os.Getenv("HUGGINGFACE_API_KEY")),
    llm.WithModel("meta-llama/Llama-2-70b-chat-hf"),
)

// 高级配置
client, err := providers.NewHuggingFaceWithOptions(
    llm.WithAPIKey(os.Getenv("HUGGINGFACE_API_KEY")),
    llm.WithModel("mistralai/Mixtral-8x7B-Instruct-v0.1"),
    llm.WithMaxTokens(4096),
    llm.WithTemperature(0.7),
)
```

### Ollama（本地部署）

```go
import (
    "github.com/kart-io/goagent/llm"
    "github.com/kart-io/goagent/llm/providers"
)

// 基本配置
client, err := providers.NewOllamaWithOptions(
    llm.WithBaseURL("http://localhost:11434"),
    llm.WithModel("llama2"),
)

// 高级配置
client, err := providers.NewOllamaWithOptions(
    llm.WithBaseURL("http://localhost:11434"),
    llm.WithModel("codellama"),
    llm.WithMaxTokens(4096),
    llm.WithTemperature(0.7),
)
```

**常用模型：**

- `llama2`
- `codellama`
- `mistral`
- `mixtral`

### SiliconFlow

```go
import (
    "github.com/kart-io/goagent/llm"
    "github.com/kart-io/goagent/llm/providers"
)

// 基本配置
client, err := providers.NewSiliconFlowWithOptions(
    llm.WithAPIKey(os.Getenv("SILICONFLOW_API_KEY")),
    llm.WithModel("Qwen/Qwen2-72B-Instruct"),
)
```

### Kimi

```go
import (
    "github.com/kart-io/goagent/llm"
    "github.com/kart-io/goagent/llm/providers"
)

// 基本配置
client, err := providers.NewKimiWithOptions(
    llm.WithAPIKey(os.Getenv("KIMI_API_KEY")),
    llm.WithModel("moonshot-v1-8k"),
)
```

**模型配置示例（非当前支持清单）：**

- `moonshot-v1-8k`
- `moonshot-v1-32k`
- `moonshot-v1-128k`

## 使用示例

### 基本对话

```go
client, err := providers.NewOpenAIWithOptions(llm.WithAPIKey(apiKey), llm.WithModel("gpt-4"))
if err != nil {
    log.Fatal(err)
}

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
client, err := providers.NewOpenAIWithOptions(llm.WithAPIKey(apiKey), llm.WithModel("gpt-4"))
if err != nil {
    log.Fatal(err)
}

messages := []llm.Message{
    llm.UserMessage("请写一首诗。"),
}

ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
defer cancel() // 包含提前返回和停止消费的路径
stream, err := client.ChatStream(ctx, messages)
if err != nil {
    log.Fatal(err)
}

for chunk := range stream {
    if chunk == nil {
        return fmt.Errorf("收到空响应块")
    }
    if chunk.Error != nil {
        return chunk.Error
    }
    fmt.Print(chunk.Delta)
}
```

该片段应放在返回 error 的函数内。完整的终态检查及可运行离线示例见 [LLM API](../api/LLM_API.md#离线流式示例)。直接请求、结构化流与 Agent Runnable 的终态是不同层次，不能只因已经输出文本就报告成功。

### 自定义参数

```go
client, err := providers.NewOpenAIWithOptions(llm.WithAPIKey(apiKey), llm.WithModel("gpt-4"))
if err != nil {
    log.Fatal(err)
}

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
func createClient(provider string) (llm.Client, error) {
    switch provider {
    case "openai":
        return providers.NewOpenAIWithOptions(
            llm.WithAPIKey(os.Getenv("OPENAI_API_KEY")),
            llm.WithModel("gpt-4"),
        )
    case "anthropic":
        return providers.NewAnthropicWithOptions(
            llm.WithAPIKey(os.Getenv("ANTHROPIC_API_KEY")),
            llm.WithModel("claude-3-sonnet-20240229"),
        )
    case "deepseek":
        return providers.NewDeepSeekWithOptions(
            llm.WithAPIKey(os.Getenv("DEEPSEEK_API_KEY")),
            llm.WithModel("deepseek-chat"),
        )
    default:
        return providers.NewOllamaWithOptions(
            llm.WithBaseURL("http://localhost:11434"),
            llm.WithModel("llama2"),
        )
    }
}

// 使用
client, err := createClient(os.Getenv("LLM_PROVIDER"))
if err != nil {
    log.Fatal(err)
}
// 使用统一执行入口前验证具体 Provider 的结构化流能力。
if !llm.HasStreaming(client) {
    return fmt.Errorf("该 Provider 未实现结构化流")
}
agent, err := builder.NewSimpleBuilder(client).BuildContext(ctx)
```

## Token 使用统计

用量证据可能缺失，必须检查 `Usage != nil`。nil 表示未知，不能当作零成本；流中的 `llm.Usage` 与同步响应的 `interfaces.TokenUsage` 也不是同一结构体。

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
    classified := llm.ClassifyError(err)
    log.Printf("模型请求失败: reason=%s retryable=%t", classified.Reason, classified.Retryable)
    return err
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
agent, err := builder.NewSimpleBuilder(client).
    WithRequestError(2, builder.RetryTransient(2, nil)).
    BuildContext(ctx)
```

这里使用 Builder 请求恢复组件，调用 `agent.AsRunnable()` 时生效；它不修改宿主直接调用 `client.Chat` 的行为。每次重试计入根请求预算，已输出文本后不重试；子 Agent 不继承父 Agent 的重试配置。旧 `WithModelRetry` 已删除。Provider/SDK 内部重试需单独核对，避免叠加放大请求。不要通过忽略宿主 context 的循环重试所有错误。

### 4. 选择合适的模型

按任务样本验证模型的工具选择、输出质量、上下文、延迟和费用；不能从构造函数或文档中的历史名称推导当前可用性。更换 Provider 后重新检查需要的可执行接口，不能只确认 Complete/Chat 可以调用。

### 5. 成本优化

- 使用较小的模型处理简单任务
- 限制 MaxTokens
- 需要缓存时核实所用 Provider/中间件的具体实现和回放语义，不把配置字段当成通用缓存保证

## 相关文档

- [快速入门](QUICKSTART.md)
- [架构概述](../architecture/ARCHITECTURE.md)
