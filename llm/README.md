# llm LLM 集成层

`llm` 定义模型客户端接口、消息与流式类型、配置选项和错误分类；`llm/providers` 提供各提供商的实现。接口与选项的完整说明见 [LLM API 参考](../docs/api/LLM_API.md)，每个 Provider 的配置与默认值见 [LLM 提供商指南](../docs/guides/LLM_PROVIDERS.md)，实际支持的原生工具流与 Embedding 以 [能力矩阵](../docs/guides/PROVIDER_CAPABILITIES.md) 为准。

## 包结构

```text
llm/
├── client.go         # Client、Message、CompletionRequest/Response、LLMOptions
├── complete.go       # Complete、Chat：把流收集为完整响应
├── stream_client.go  # ToolStreamClient、StreamChunk、StreamReader/Writer
├── capabilities.go   # Capability、CapabilityChecker、可选接口与 Has*/As* 检测函数
├── tools.go          # ToolCall 与默认参数
├── provider_error.go # ProviderError、ClassifyError
├── options.go        # ClientOption（With*）
├── common/           # BaseProvider、重试、HTTP 错误映射、消息转换
├── constants/        # Provider 常量、环境变量名、默认值
├── providers/        # OpenAI、Anthropic、Gemini、DeepSeek、Ollama、Kimi、SiliconFlow
└── cache/            # 基于向量相似度的语义缓存
```

## 能力概览

| Provider | 原生工具流 `ToolStreamClient` | 声明 Embedding |
|---|:---:|:---:|
| OpenAI | ✓ | ✓ |
| DeepSeek | ✓ | - |
| Gemini | ✓ | ✓ |
| Anthropic（官方 SDK） | ✓ | - |
| Kimi、SiliconFlow、Ollama（OpenAI 兼容预设） | ✓ | - |

`llm.Client` 只有 `CompleteStream` 与 `Provider`，所有 Provider 都实现它与 `CapabilityChecker`；需要完整响应时用 `llm.Complete` / `llm.Chat` 收集流。原生工具循环需要 `ToolStreamClient`，用 `llm.HasToolStreaming` 检查接口。

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

resp, err := llm.Chat(ctx, client, []llm.Message{
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
ctx, cancel := context.WithCancel(ctx)
defer cancel() // 提前退出时释放连接
chunks, err := client.CompleteStream(ctx, &llm.CompletionRequest{Messages: messages})
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

`llm.ClassifyError(err)` 返回 `Reason`、`Retryable`、`ShouldCompress`、`ShouldFallback`，依据错误链中的 `*llm.ProviderError`（状态码与 `RetryAfter`）和上下文超长的错误文本。各 Provider 的流式与非流式请求失败都会构造 `ProviderError`。Builder 中的模型重试用 `WithRequestError` 与 `builder.RetryTransient`。

## 语义缓存

`llm/cache` 用向量相似度复用已成功完成的响应：

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

resp, err := llm.Chat(ctx, cached, messages)
fmt.Printf("命中率: %.2f%%\n", cached.Stats().HitRate*100)
```

`cache.EmbeddingProvider` 使用 `[]float32` 向量，与 `llm.EmbeddingClient` 不是同一接口。`CachedLLMClient` 只实现 `llm.Client`：命中时直接返回缓存答案，终止块的 `FinishReason` 为 `cache_hit`；未命中时转发上游流，只缓存成功的终止答案。它不实现 `ToolStreamClient`。

## 相关包

- [builder](../builder/) - Agent 构建器
- [tools](../tools/) - 工具系统
- [interfaces](../interfaces/) - 接口定义

## 显式零温

`llm.WithTemperature(0)` 设置客户端零温；使用 Builder 时同时设置 `builder.WithTemperature(0)`（Builder 默认值为 `0.7`，优先于客户端）。直接构造请求使用 `llm.CompletionRequest{Temperature: 0, TemperatureSet: true}`。JSON 的 `"temperature":0` 会自动保留显式设置，未设置的旧请求仍沿用默认回落。自定义 Provider 应使用 `BaseProvider.ResolveTemperature(req)`，详见 [LLM API](../docs/api/LLM_API.md)。

`llm.WithReasoning(usage.ReasoningConfig{Effort: "low"})` 设置支持客户端的显式推理默认值；Gemini 可设置 `BudgetSet: true, BudgetTokens: 128`。单次 `CompletionRequest.Reasoning` 可覆盖默认值。结构化流的 `ResponseModel` 保留供应商实际模型名，`Usage` 提供有字段证据的推理和工具输入分项；不要把它们再次计入总量。实现及未知语义见[设计契约](../docs/design/TOKEN_USAGE_EXPLAINABILITY.md)。
