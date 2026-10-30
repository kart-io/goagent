# 可追溯 RAG 与流式执行

goagent 可以将现有关键词、向量、混合检索和重排序组件接入 Builder，并返回模型实际收到的参考文档来源。ReAct 的同步执行、步骤流和 Generator 共用执行循环；模型 Token 流使用独立的 LLM 流式接口。

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
output, err := agent.Execute(ctx, "退款政策是什么？")
```

`retriever` 只需实现 `interfaces.DocumentRetriever.GetRelevantDocuments(ctx, query)`。现有 `retrieval.KeywordRetriever`、`VectorStoreRetriever`、`HybridRetriever` 和 `RerankingRetriever` 都可直接接入，无需依赖某种向量数据库。

便捷函数 `builder.RAGAgent(client, retriever)` 还接受具有 `Retrieve(ctx, query)` 方法的对象，以及 `interfaces.VectorStore`。直接传入向量存储时默认检索前 4 个结果。为保留旧调用，传入 `nil` 仍只应用提示词预设，不执行检索；不支持的对象或带类型的 nil 返回配置错误。

行为约定：

- 每次执行先检索，再调用模型。检索失败直接返回错误，不悄悄退化为无依据回答。
- 文档使用 `[1]`、`[2]` 编号，提示模型引用相应编号；来源信息用于核验引用，不代表模型引用正确性已经过自动验证。
- 空结果明确告诉模型证据不足。nil 文档和空文本被跳过。
- `MaxContextRunes` 限制文档正文的 Unicode 字符数，不包括固定提示词与引用编号，也不是 Token 数。超限截断不会破坏中文 UTF-8；原文档保持不变。
- 检索内容和原始问题分开传给模型；对话记忆保存原问题，不保存扩充后的参考材料。
- `ExecuteWithTools()` 同样接入检索；普通 `Execute()` 保持原有语义，不自动执行已注册工具。
- `ConfigureForRAG()` 不再隐式缓存答案，以免相同问题命中已过期证据。需要缓存时，显式配置能区分用户、会话和知识版本的缓存策略。

输出元数据：

| 字段 | 含义 |
|---|---|
| `retrieval_sources` | `[]builder.RetrievalSource`：编号、文档 ID、来源和检索分数，仅包含实际传给模型的文档 |
| `retrieval_document_count` | 实际采用的文档数 |
| `retrieval_context_runes` | 实际采用的正文字符数 |
| `retrieval_truncated` | 是否因文档数或字符预算省略了证据 |

检索器实现（BM25、混合检索、重排、Qdrant 等）位于 [goagent-contrib/retrieval](https://github.com/kart-io/goagent-contrib/tree/master/retrieval)，均满足 `interfaces.DocumentRetriever`，可直接传给 `RAGAgent` 或 `WithRetriever`。

## 真实模型 Token 流

OpenAI Provider 实现 `llm.StreamClient`，可直接调用：

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

`Delta` 是新增文本，`Content` 是累计文本。`finish_reason` 后可能还有用量帧，因此只有终止块标记 `Done=true`。流中读取错误通过 `chunk.Error` 传回，不再仅写日志。兼容服务需支持 `stream_options.include_usage`，这是新的结构化流入口使用的请求选项。

需要旧的 `StreamOutput.Next()` 接口时，使用 `stream.NewStreamingLLMAgentWithRealStreaming(client, config)`：

- 支持结构化 `StreamClient`，也兼容现有 `TokenStreamingClient`。
- `Close()` 取消上游请求；同步 `Execute()` 收集同一条真实流。
- 结构化流转发文本，并以 `metadata` 块返回 `usage` 和 `finish_reason`。
- Token-only 接口使用 Provider 自身配置，不能携带流中错误或用量；需要这些信息时使用结构化接口。
- 不自动重试已输出部分文本的请求，避免重复文本。

原 `StreamingLLMAgent` 保留兼容行为：等待完整响应后再分块输出，不改善模型首字延迟。

## 后续优势建设顺序

1. 用相同业务数据集评测 ReAct、ToT 等策略：成功率、模型调用数、Token 成本、总延迟，避免用策略数量代表效果。
2. 补齐真实搜索、LLM/CrossEncoder 重排序与 MCP 协议互操作，并明确模拟组件边界。
3. 验证持久化中断在进程重启后恢复，覆盖重复工具调用和幂等处理。
4. 在这些行为稳定后逐步统一 Agent/Runnable 适配，保留已有调用方的迁移路径。

## 后续能力

参见 [检索、统一事件流、MCP 与可恢复步骤](PRIORITY_CAPABILITIES.md) 和 [合成评测记录](../evaluation/README.md)。
