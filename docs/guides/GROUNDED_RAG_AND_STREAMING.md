# 可追溯 RAG 与流式执行

goagent 可以将检索组件（goagent-contrib 中的关键词、向量、混合检索和重排序，或任意实现 `interfaces.DocumentRetriever` 的检索器）接入 Builder，并返回模型实际收到的参考文档来源。模型 Token 流使用独立的 LLM 流式接口。

## 检索增强生成

```go
agent, err := builder.NewSimpleBuilder(client).
    WithSystemPrompt("根据参考文档回答问题。").
    ConfigureForRAG().
    WithRetriever(retriever, builder.RetrievalConfig{
        MaxDocuments:    4,
        MaxContextRunes: 12000,
    }).
    Build()
if err != nil {
    return err
}
output, err := agent.AsRunnable().Invoke(ctx, "退款政策是什么？")
```

`retriever` 只需实现 `interfaces.DocumentRetriever.GetRelevantDocuments(ctx, query)`。goagent-contrib 中 `github.com/kart-io/goagent-contrib/retrieval` 的 `retrieval.KeywordRetriever`、`VectorStoreRetriever`、`HybridRetriever` 和 `RerankingRetriever` 都可直接接入，无需依赖某种向量数据库。

便捷函数 `builder.RAGAgent(client, retriever)` 还接受具有 `Retrieve(ctx, query)` 方法的对象，以及 `interfaces.VectorStore`。直接传入向量存储时默认检索前 4 个结果。传入 `nil` 只应用提示词预设，不执行检索；不支持的对象或带类型的 nil 返回配置错误。

行为约定：

- 每次执行先检索，再调用模型。检索失败直接返回错误，不悄悄退化为无依据回答。
- 文档使用 `[1]`、`[2]` 编号，提示模型引用相应编号；来源信息用于核验引用，不代表模型引用正确性已经过自动验证。
- 空结果明确告诉模型证据不足。nil 文档和空文本被跳过。
- `MaxContextRunes` 限制文档正文的 Unicode 字符数，不包括固定提示词与引用编号，也不是 Token 数。超限截断不会破坏中文 UTF-8；原文档保持不变。
- 检索内容和原始问题分开传给模型；对话记忆保存原问题，不保存扩充后的参考材料。
- 所有执行入口（`AsRunnable`、`ExecuteStream`、`Execute`、`ExecuteWithTools`）都先检索；`Execute()` 不自动执行已注册工具。
- `ConfigureForRAG()` 只设置计时中间件、`MaxTokens=3000` 与 `Temperature=0.3`，不缓存答案，以免相同问题命中已过期证据。需要缓存时，显式配置能区分用户、会话和知识版本的缓存策略。

输出元数据：

| 字段 | 含义 |
|---|---|
| `retrieval_sources` | `[]builder.RetrievalSource`：编号、文档 ID、来源和检索分数，仅包含实际传给模型的文档 |
| `retrieval_document_count` | 实际采用的文档数 |
| `retrieval_context_runes` | 实际采用的正文字符数 |
| `retrieval_truncated` | 是否因文档数或字符预算省略了证据 |

检索器实现（BM25、混合检索、重排、Qdrant 等）位于 [goagent-contrib/retrieval](https://github.com/kart-io/goagent-contrib/tree/master/retrieval)，均满足 `interfaces.DocumentRetriever`，可直接传给 `RAGAgent` 或 `WithRetriever`。

## 真实模型 Token 流

实现 `llm.StreamClient` 的 Provider（如 OpenAI、DeepSeek、Gemini）可直接调用：

```go
ctx, cancel := context.WithCancel(ctx)
defer cancel()
chunks, err := client.CompleteStream(ctx, &llm.CompletionRequest{
    Messages: []llm.Message{llm.UserMessage("介绍这个项目")},
    MaxTokens: 500,
})
if err != nil {
    return err
}
for chunk := range chunks {
    if chunk.Error != nil {
        return chunk.Error
    }
    fmt.Print(chunk.Delta)
    if chunk.Done && chunk.Usage != nil {
        fmt.Printf("\nTokens: %d\n", chunk.Usage.TotalTokens)
    }
}
```

`Delta` 是新增文本，`Content` 是累计文本。`finish_reason` 后可能还有用量帧，因此只有终止块标记 `Done=true`。流中读取错误通过 `chunk.Error` 传回。OpenAI 兼容服务需支持 `stream_options.include_usage`。

Agent 级的事件流用 `agent.AsRunnable().Stream` 或 `ExecuteStream`，见[执行契约](EXECUTION_CONTRACT.md)与[事件流](PRIORITY_CAPABILITIES.md#统一事件流)。
