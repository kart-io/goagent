# providers 内置 LLM 客户端

[English](README.md) | [中文](README_CN.md)

`providers` 为 OpenAI 及 OpenAI 兼容端点（DeepSeek、Kimi、SiliconFlow、Ollama、MiniMax）、Gemini 和 Anthropic 实现 `llm.Client`。需要一个具体的模型客户端交给 Builder 或 `llm.Chat` / `llm.Complete` 时使用它。每个提供商有一个 `NewXxxWithOptions(opts ...llm.ClientOption)` 构造函数；没有按名称查找提供商的注册表。

## 用法

```go
client, err := providers.NewOpenAIWithOptions(
    llm.WithAPIKey(os.Getenv("OPENAI_API_KEY")),
    llm.WithModel("gpt-4o-mini"),
    llm.WithTimeout(60*time.Second),
    llm.WithStreamIdleTimeout(30*time.Second),
)
if err != nil {
    return err
}
resp, err := llm.Chat(ctx, client, []llm.Message{
    llm.SystemMessage("You are a helpful assistant."),
    llm.UserMessage("Hello!"),
})
if err != nil {
    return err
}
fmt.Println(resp.Content)
```

OpenAI 兼容预设返回 `*OpenAIProvider`，共享其方法：

```go
// Ollama 不需要 API key；BaseURL 必须指向 /v1 端点。
client, err := providers.NewOllamaWithOptions(llm.WithModel("qwen3"))
if err != nil {
    return err
}
models, err := client.ListModels(ctx) // 读取 /models
```

## 主要 API

| 标识符 | 说明 |
|---|---|
| `NewOpenAIWithOptions` → `*OpenAIProvider` | OpenAI Chat Completions；`WithBaseURL` 可指向任意兼容端点 |
| `NewKimiWithOptions`、`NewSiliconFlowWithOptions`、`NewOllamaWithOptions`、`NewMiniMaxWithOptions` | OpenAI 兼容预设，返回 `*OpenAIProvider` |
| `NewOllamaClientSimple(model)` | 只设置模型的 Ollama 客户端 |
| `NewDeepSeekWithOptions` → `*DeepSeekProvider` | DeepSeek；流式与 OpenAI 共用同一 SSE 解码器 |
| `NewGeminiWithOptions` → `*GeminiProvider` | 通过 Google GenAI SDK 调用 Gemini 原生 API |
| `NewAnthropicWithOptions` → `*AnthropicProvider` | 通过 Anthropic 官方 SDK 调用 Messages API |
| `CompleteStream` / `CompleteToolsStream` | 结构化流式与原生工具流（`llm.Client`、`llm.ToolStreamClient`） |
| `Embed` / `EmbedBatch` | `OpenAIProvider` 与 `GeminiProvider` 的向量接口；`DeepSeekProvider.Embed` 是保留的兼容请求 |
| `ValidateReasoning(req)` | 在 OpenAI、DeepSeek、Gemini 上于发送前校验推理配置 |
| `NewDeepSeekStreamingWithOptions`、`NewGeminiStreamingWithOptions` | 保留的包装，提供 `StreamWithMetadata` / `StreamWithContext` |

## 行为说明

- `APIKey`、`BaseURL`、`Model` 依次取显式选项、提供商环境变量、内置默认值（名称定义在 `constants`，如 `constants.EnvOpenAIAPIKey`）。除 Ollama 外，缺少 API key 时构造函数返回错误。构造函数不发起请求。
- 未传 `llm.WithTimeout` 时所有客户端都不设超时。对 OpenAI 兼容客户端、DeepSeek 和 Anthropic，超时只限制等待响应头，长流不会被截断；这些客户端上 `llm.WithStreamIdleTimeout` 与 `llm.WithStreamTimeout` 会以可重试超时中止停滞或过长的流。
- Anthropic、Gemini 和 DeepSeek 流式不做 SDK 隐式重试；一次调用对应一次上游请求。
- Anthropic 不发送 `temperature` 与 `top_p`，拒绝负数 `WithMaxTokens`，max tokens 为 0 时使用 `constants.DefaultClientMaxTokens`，并拒绝非文本响应格式。
- 单次请求的推理配置（`CompletionRequest.Reasoning`）被 OpenAI、MiniMax、Gemini、DeepSeek 接受（DeepSeek 只接受 effort `"none"`，改发 thinking 禁用；OpenAI 兼容路径拒绝 thinking budget）。客户端级 `llm.WithReasoning` 只被 OpenAI、DeepSeek、Gemini 的构造函数接受。其余提供商返回错误而不是忽略。
- `DeepSeekProvider.CompleteToolsStream` 把写进正文的 DSML 工具调用还原为 `ToolCalls`。
- Kimi、SiliconFlow、Ollama 声明保守能力集：不含 Embedding、视觉与 JSON 模式。依赖某项功能前先检查 `HasCapability`。
- Gemini 与 Anthropic 把不透明状态保存在 `Message.ProviderState`；自行管理消息历史时原样回传。
- `CompleteStream` 的调用方停止读取时必须取消 ctx；`llm.Chat` / `llm.Complete` 会代为处理。

## 另见

- [LLM 提供商指南](../../docs/guides/LLM_PROVIDERS_CN.md)
- [Provider 能力边界](../../docs/guides/PROVIDER_CAPABILITIES_CN.md)
- [Provider 最佳实践](../../docs/guides/PROVIDER_BEST_PRACTICES_CN.md)
- [使用 Anthropic 协议调用 MiniMax](../../docs/guides/MINIMAX_ANTHROPIC_CN.md)
- [llm](../README_CN.md)、[llm/common](../common/README_CN.md)
