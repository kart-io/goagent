# LLM 提供商指南

`llm/providers` 为每个提供商提供一个 `NewXxxWithOptions(opts ...llm.ClientOption)` 构造函数，返回实现 `llm.Client` 的客户端。接口与选项见 [LLM API 参考](../api/LLM_API.md)；流式、工具和 Embedding 的实际支持见 [Provider 能力边界](PROVIDER_CAPABILITIES.md)。

## 配置来源

`APIKey`、`BaseURL`、`Model` 按以下顺序取第一个非空值：显式选项（`llm.WithAPIKey` / `WithBaseURL` / `WithModel`）→ 环境变量 → 内置默认值。API Key 没有默认值，除 Ollama 外缺失时构造函数返回错误。构造函数不发请求，构造成功不代表服务或模型可用。

| 提供商 | 构造函数 | API Key 环境变量 | BaseURL / Model 环境变量 | 默认 BaseURL | 默认模型 |
|---|---|---|---|---|---|
| OpenAI | `NewOpenAIWithOptions` | `OPENAI_API_KEY` | `OPENAI_BASE_URL` / `OPENAI_MODEL` | `https://api.openai.com/v1` | `gpt-4-turbo-preview` |
| Anthropic | `NewAnthropicWithOptions` | `ANTHROPIC_API_KEY` | `ANTHROPIC_BASE_URL` / `ANTHROPIC_MODEL` | `https://api.anthropic.com` | `claude-3-5-sonnet-20241022` |
| Gemini | `NewGeminiWithOptions` | `GEMINI_API_KEY` | `GEMINI_BASE_URL` / `GEMINI_MODEL` | `https://generativelanguage.googleapis.com` | `gemini-2.5-flash` |
| DeepSeek | `NewDeepSeekWithOptions` | `DEEPSEEK_API_KEY` | `DEEPSEEK_BASE_URL` / `DEEPSEEK_MODEL` | `https://api.deepseek.com/v1` | `deepseek-chat` |
| Cohere | `NewCohereWithOptions` | `COHERE_API_KEY` | `COHERE_BASE_URL` / `COHERE_MODEL` | `https://api.cohere.ai` | `command-r-plus` |
| HuggingFace | `NewHuggingFaceWithOptions` | `HUGGINGFACE_API_KEY` | `HUGGINGFACE_BASE_URL` / `HUGGINGFACE_MODEL` | `https://api-inference.huggingface.co` | `meta-llama/Meta-Llama-3-8B-Instruct` |
| Kimi | `NewKimiWithOptions` | `KIMI_API_KEY` | `KIMI_BASE_URL` / `KIMI_MODEL` | `https://api.moonshot.cn/v1` | `moonshot-v1-8k` |
| SiliconFlow | `NewSiliconFlowWithOptions` | `SILICONFLOW_API_KEY` | `SILICONFLOW_BASE_URL` / `SILICONFLOW_MODEL` | `https://api.siliconflow.cn/v1` | `Qwen/Qwen2-7B-Instruct` |
| Ollama | `NewOllamaWithOptions` | 不需要 | `OLLAMA_BASE_URL` / `OLLAMA_MODEL` | `http://localhost:11434` | `llama2` |

环境变量名也以常量形式定义在 `llm/constants`（如 `constants.EnvOpenAIAPIKey`）。默认模型只是兜底值，生产环境应显式指定当前可用的模型。

### 提供商说明

- **Gemini**：基于 Google GenAI SDK 调用原生 API，使用 API Key 认证。`GEMINI_BASE_URL` 填原生根地址，不要填 OpenAI 兼容的 `/v1beta/openai` 路径。向量模型通过 `llm.WithEmbeddingModel` 或 `GEMINI_EMBEDDING_MODEL` 指定，默认 `gemini-embedding-001`。原生工具循环在 `Message.ProviderState` 中保留签名，自管消息历史时须原样回传。验证案例见 [gemini-verify](../../examples/llm/gemini-verify/README.md)。
- **DeepSeek**：`NewDeepSeekWithOptions` 直接实现结构化流与原生工具流；`NewDeepSeekStreamingWithOptions` 只是嵌入它的包装。旧 `Embed` 方法发送兼容请求，但不声明 Embedding 能力。
- **OpenAI**：`WithBaseURL` 可指向 OpenAI 兼容端点，兼容程度需自行验证。
- **Ollama**：`NewOllamaClientSimple(model)` 等价于只设置模型的 `NewOllamaWithOptions`；未设置超时时使用 120 秒。

## 基本对话

```go
client, err := providers.NewDeepSeekWithOptions(
    llm.WithAPIKey(os.Getenv("DEEPSEEK_API_KEY")),
    llm.WithModel("deepseek-chat"),
)
if err != nil {
    return err
}

resp, err := client.Chat(ctx, []llm.Message{
    llm.SystemMessage("你是一个有帮助的助手。"),
    llm.UserMessage("你好！"),
})
if err != nil {
    return err
}
fmt.Println(resp.Content)
if resp.Usage != nil {
    fmt.Printf("输入 %d / 输出 %d / 合计 %d Token\n",
        resp.Usage.PromptTokens, resp.Usage.CompletionTokens, resp.Usage.TotalTokens)
}
```

`CompletionRequest` 的 `Temperature`、`MaxTokens`、`TopP`、`Model` 为零值时使用客户端配置。

## 流式对话

只有实现 `llm.StreamClient` 的客户端（OpenAI、DeepSeek、Gemini）支持结构化流：

```go
sc := llm.AsStreamClient(client)
if sc == nil {
    return fmt.Errorf("%s 不支持结构化流", client.Provider())
}
ctx, cancel := context.WithCancel(ctx)
defer cancel() // 提前退出时释放连接
stream, err := sc.ChatStream(ctx, []llm.Message{llm.UserMessage("请写一首诗。")})
if err != nil {
    return err
}
for chunk := range stream {
    if chunk.Error != nil {
        return chunk.Error
    }
    fmt.Print(chunk.Delta)
}
```

## 切换提供商

按配置选择构造函数，返回统一的 `llm.Client`，再交给 Builder：

```go
func createClient(name string) (llm.Client, error) {
    switch name {
    case "openai":
        return providers.NewOpenAIWithOptions(llm.WithModel("gpt-4o"))
    case "gemini":
        return providers.NewGeminiWithOptions(llm.WithModel("gemini-2.5-flash"))
    case "deepseek":
        return providers.NewDeepSeekWithOptions(llm.WithModel("deepseek-chat"))
    default:
        return nil, fmt.Errorf("不支持的 provider：%s", name)
    }
}

client, err := createClient(os.Getenv("LLM_PROVIDER"))
if err != nil {
    return err
}
agent, err := builder.NewSimpleBuilder(client).Build()
```

上例中 API Key 从各自的环境变量读取。`AsRunnable` 与原生工具循环要求客户端实现 `StreamClient` / `ToolStreamClient`，选择提供商时先查 [能力矩阵](PROVIDER_CAPABILITIES.md)。

## 错误处理与重试

用 `llm.ClassifyError` 判断失败类型，不要匹配错误文本：

```go
resp, err := client.Chat(ctx, messages)
if err != nil {
    switch c := llm.ClassifyError(err); {
    case c.ShouldCompress:
        // 超出上下文窗口：缩短消息后再试
    case c.Retryable:
        // 限流、5xx 或暂时性连接错误
    default:
        return err
    }
}
```

分类依赖 `*llm.ProviderError`，各 Provider 的流式与非流式请求失败都会构造它。在 Builder 中重试模型请求用 `WithRequestError(n, builder.RetryTransient(n, nil))`，见 [核心 API](../api/CORE_API.md#模型请求恢复)。

## 超时

`llm.WithTimeout` 与请求 ctx 的关系见 [Provider 最佳实践](PROVIDER_BEST_PRACTICES.md#超时配置)。调用级期限用 ctx 控制：

```go
ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
defer cancel()
resp, err := client.Chat(ctx, messages)
```

## 相关文档

- [快速入门](QUICKSTART.md)
- [Provider 最佳实践](PROVIDER_BEST_PRACTICES.md)
- [Provider 能力边界](PROVIDER_CAPABILITIES.md)
