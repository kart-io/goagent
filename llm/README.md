# llm LLM 集成层

`llm` 定义模型客户端接口、消息与流式类型、配置选项和错误分类；`llm/providers` 提供各提供商的实现。接口与选项的完整说明见 [LLM API 参考](../docs/api/LLM_API.md)，每个 Provider 的配置与默认值见 [LLM 提供商指南](../docs/guides/LLM_PROVIDERS.md)，实际支持的字符串流、结构化流、工具与 Embedding 以 [能力矩阵](../docs/guides/PROVIDER_CAPABILITIES.md) 为准。

## 包结构

```text
llm/
├── client.go         # Client、Message、CompletionRequest/Response、LLMOptions
├── stream_client.go  # StreamClient、StreamChunk、StreamReader/Writer
├── capabilities.go   # Capability、CapabilityChecker、可选接口与 Has*/As* 检测函数
├── tools.go          # ToolCall、ToolCallResponse、ToolChunk
├── provider_error.go # ProviderError、ClassifyError
├── options.go        # ClientOption（With*）
├── common/           # BaseProvider、重试、HTTP 错误映射、消息转换
├── constants/        # Provider 常量、环境变量名、默认值
├── providers/        # OpenAI、Anthropic、Gemini、DeepSeek、Cohere、HuggingFace、Ollama、Kimi、SiliconFlow
└── cache/            # 基于向量相似度的语义缓存
```

## 能力概览

| Provider | 结构化流 `StreamClient` | 原生工具流 `ToolStreamClient` | 字符串流 | 声明 Embedding |
|---|:---:|:---:|:---:|:---:|
| OpenAI | ✓ | ✓ | ✓ | ✓ |
| DeepSeek | ✓ | ✓ | ✓ | - |
| Gemini | ✓ | ✓ | ✓ | ✓ |
| Anthropic、Cohere、HuggingFace | - | - | ✓ | - |
| Kimi、SiliconFlow、Ollama | - | - | - | - |

所有 Provider 都实现 `Complete` / `Chat` 与 `CapabilityChecker`。Builder 的 `AsRunnable` / `ExecuteStream` 需要结构化流，原生工具循环需要 `ToolStreamClient`。用 `llm.HasStreaming`、`llm.HasToolStreaming` 检查接口；`CapabilityStreaming` 不能证明支持结构化流。

## 创建客户端与对话

```go
client, err := providers.NewOpenAIWithOptions(
    llm.WithAPIKey(os.Getenv("OPENAI_API_KEY")),
    llm.WithModel("gpt-4o"),
    llm.WithMaxTokens(2000),
)
if err != nil {
    return err
}

resp, err := client.Chat(ctx, []llm.Message{
    llm.SystemMessage("你是一个有帮助的助手"),
    llm.UserMessage("你好！"),
})
if err != nil {
    return err
}
fmt.Println(resp.Content)
```

`WithAPIKey`、`WithBaseURL`、`WithModel` 留空时读取 `<PROVIDER>_API_KEY`、`<PROVIDER>_BASE_URL`、`<PROVIDER>_MODEL` 环境变量（如 `OPENAI_API_KEY`），再回退到默认值。`WithBaseURL` 可指向兼容端点。

## 流式响应

```go
sc := llm.AsStreamClient(client)
if sc == nil {
    return errors.New("该 Provider 不支持结构化流")
}
ctx, cancel := context.WithCancel(ctx)
defer cancel() // 提前退出时释放连接
chunks, err := sc.ChatStream(ctx, messages)
if err != nil {
    return err
}
text, err := llm.NewStreamReader(chunks).ReadAll(ctx)
```

逐块处理时检查 `chunk.Error`，打印 `chunk.Delta`；`Done` 块携带结束原因与用量。

## 原生工具流

一般通过 Builder 使用工具（它处理工具执行、结果续接和 `ProviderState`），见 [builder](../builder/)。直接调用 `CompleteToolsStream` 时，只在无错误的 `Done` 块中读取完整的 `ToolCalls`：

```go
tc := llm.AsToolStreamClient(client)
if tc == nil {
    return errors.New("该 Provider 不支持原生工具流")
}
chunks, err := tc.CompleteToolsStream(ctx, &llm.CompletionRequest{Messages: messages}, []interfaces.Tool{weatherTool})
if err != nil {
    return err
}
for chunk := range chunks {
    if chunk.Error != nil {
        return chunk.Error
    }
    if !chunk.Done {
        continue
    }
    if chunk.FinishReason == "tool_calls" {
        // 下一轮请求追加：Role 为 assistant、带 ToolCalls/ReasoningContent/ProviderState 的消息，
        // 以及每个调用一条 Role 为 tool、ToolCallID 对应的结果消息。
        for _, call := range chunk.ToolCalls {
            fmt.Println(call.Function.Name, call.Function.Arguments)
        }
    }
}
```

`interfaces.Tool` 是接口（`Name`、`Description`、`ArgsSchema`、`Invoke`），可用 `tools.NewFunctionTool` 等构造，见 [tools](../tools/)。

## 文本嵌入

```go
embedder := llm.AsEmbedder(client) // 未实现或未声明 Embedding 时返回 nil
if embedder == nil {
    return errors.New("该 Provider 不支持 Embedding")
}
vector, err := embedder.Embed(ctx, "这是一段需要向量化的文本")
```

Gemini 的向量模型通过 `llm.WithEmbeddingModel` 或 `GEMINI_EMBEDDING_MODEL` 指定，默认 `gemini-embedding-001`。

## 错误分类

`llm.ClassifyError(err)` 返回 `Reason`、`Retryable`、`ShouldCompress`、`ShouldFallback`，依据错误链中的 `*llm.ProviderError`（状态码与 `RetryAfter`）和上下文超长的错误文本。`ProviderError` 目前只由 OpenAI、DeepSeek、Gemini 的流式路径构造。Builder 中的模型重试用 `WithRequestError` 与 `builder.RetryTransient`。

## 语义缓存

`llm/cache` 用向量相似度复用 `Complete` / `Chat` 的响应：

```go
cached, err := cache.NewCachedLLMClient(&cache.CachedLLMClientConfig{
    Client: client,
    EmbeddingProvider: cache.NewOpenAIEmbeddingProvider(&cache.OpenAIEmbeddingConfig{
        APIKey: os.Getenv("OPENAI_API_KEY"),
    }),
    CacheConfig: cache.DefaultSemanticCacheConfig(), // 阈值 0.95、10000 条、24 小时
})
if err != nil {
    return err
}
defer cached.Close()

resp, err := cached.Chat(ctx, messages)
fmt.Printf("命中率: %.2f%%\n", cached.Stats().HitRate*100)
```

`cache.EmbeddingProvider` 使用 `[]float32` 向量，与 `llm.EmbeddingClient` 不是同一接口。`CachedLLMClient` 只实现 `llm.Client`，不提供流式接口。

## 相关包

- [builder](../builder/) - Agent 构建器
- [tools](../tools/) - 工具系统
- [interfaces](../interfaces/) - 接口定义
