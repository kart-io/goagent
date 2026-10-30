# LLM Provider 最佳实践指南

本文档介绍在 GoAgent 中创建和切换 LLM Provider 的推荐方式。各 Provider 的配置项见 [LLM 提供商指南](LLM_PROVIDERS.md)。

## 推荐方式：直接使用 Provider 构造函数

每个 Provider 在 `llm/providers` 中提供 `NewXxxWithOptions(opts ...llm.ClientOption)` 构造函数，返回实现 `llm.Client` 的客户端：

```go
import (
    "github.com/kart-io/goagent/llm"
    "github.com/kart-io/goagent/llm/providers"
)

client, err := providers.NewOpenAIWithOptions(
    llm.WithAPIKey(os.Getenv("OPENAI_API_KEY")),
    llm.WithModel("gpt-4"),
)
if err != nil {
    return err
}
```

可用的构造函数：`NewOpenAIWithOptions`、`NewAnthropicWithOptions`、`NewGeminiWithOptions`、`NewDeepSeekWithOptions`、`NewCohereWithOptions`、`NewHuggingFaceWithOptions`、`NewOllamaWithOptions`、`NewSiliconFlowWithOptions`、`NewKimiWithOptions`；OpenAI、DeepSeek、Gemini 等客户端本身实现 `llm.StreamClient`；`NewDeepSeekStreamingWithOptions`、`NewGeminiStreamingWithOptions` 是保留的流式包装。

GoAgent 没有按名称查找 Provider 的注册表，`llm.NewClientWithOptions` 已废弃且不会创建客户端。

## 使用场景

### 多 Provider 支持

在应用中按配置选择构造函数，返回统一的 `llm.Client`：

```go
func createClient(provider string) (llm.Client, error) {
    switch provider {
    case "openai":
        return providers.NewOpenAIWithOptions(
            llm.WithAPIKey(os.Getenv("OPENAI_API_KEY")),
            llm.WithModel("gpt-4"),
        )
    case "deepseek":
        return providers.NewDeepSeekWithOptions(
            llm.WithAPIKey(os.Getenv("DEEPSEEK_API_KEY")),
            llm.WithModel("deepseek-chat"),
        )
    default:
        return nil, fmt.Errorf("不支持的 provider：%s", provider)
    }
}
```

### Provider 回退

构造函数只校验配置、不发起请求，构造成功不代表服务可用。回退应基于实际调用的错误：

```go
func chatWithFallback(ctx context.Context, primary, backup llm.Client, messages []llm.Message) (*llm.CompletionResponse, error) {
    resp, err := primary.Chat(ctx, messages)
    if err == nil {
        return resp, nil
    }
    return backup.Chat(ctx, messages)
}
```

回退会让同一请求发送两次，只用于幂等、可重复的调用。

### 测试

测试中使用 `testing/mocks` 的 `MockLLMClient`，按顺序返回预设响应：

```go
import (
    "github.com/kart-io/goagent/llm"
    "github.com/kart-io/goagent/testing/mocks"
)

client := mocks.NewMockLLMClient()
client.SetResponses(llm.CompletionResponse{Content: "预设回答"})
```

## 超时配置

`llm.WithTimeout` 的参数是 `time.Duration`，按整秒保存，正数向上取整；未设置时为 60 秒：

```go
llm.WithTimeout(30 * time.Second) // 30 秒
llm.WithTimeout(30)               // 写错单位：30 纳秒，向上取整为 1 秒
```

OpenAI Provider 与 DeepSeek 的流式方法用它限制等待响应开始的时间：非流式请求在生成完毕后才返回，因而整体受此限；流式请求在首块到达后不再受限，长回复不会被截断。流开始后只受 ctx 控制，消费者不再读取时必须取消 ctx，否则 goroutine 与连接不会释放。其他 Provider 及 DeepSeek 的非流式方法将它作为整个 HTTP 请求的时限。

## 相关文档

- [LLM 提供商指南](LLM_PROVIDERS.md) - 各 Provider 的配置与使用示例
