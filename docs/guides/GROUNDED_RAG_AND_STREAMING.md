# Grounded RAG and Streaming Execution

[English](GROUNDED_RAG_AND_STREAMING.md) | [中文](GROUNDED_RAG_AND_STREAMING_CN.md)

goagent can plug retrieval components (the keyword, vector, hybrid, and reranking retrievers in goagent-contrib, or any retriever implementing `interfaces.DocumentRetriever`) into the Builder and return the sources of the reference documents the model actually received. Model token streaming uses the separate LLM streaming interface.

## Retrieval-Augmented Generation

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

`retriever` only needs to implement `interfaces.DocumentRetriever.GetRelevantDocuments(ctx, query)`. `retrieval.KeywordRetriever`, `VectorStoreRetriever`, `HybridRetriever`, and `RerankingRetriever` from `github.com/kart-io/goagent-contrib/retrieval` in goagent-contrib can all be plugged in directly, without depending on any particular vector database.

The convenience function `builder.RAGAgent(client, retriever)` also accepts objects with a `Retrieve(ctx, query)` method, as well as `interfaces.VectorStore`. When a vector store is passed directly, the top 4 results are retrieved by default. Passing `nil` applies only the prompt preset and performs no retrieval; unsupported objects or a typed nil return a configuration error.

Behavior:

- Each execution retrieves first and then calls the model. A retrieval failure returns an error directly instead of silently degrading to an ungrounded answer.
- Documents are numbered `[1]`, `[2]`, and the model is prompted to cite the corresponding numbers; source information is for verifying citations and does not mean the correctness of the model's citations has been verified automatically.
- An empty result explicitly tells the model that evidence is insufficient. nil documents and empty text are skipped.
- `MaxContextRunes` limits the number of Unicode characters in the document bodies, excluding the fixed prompt and citation numbers; it is not a token count. Truncation past the limit does not break Chinese UTF-8; the original documents stay unchanged.
- Retrieved content and the original question are passed to the model separately; conversation memory stores the original question, not the augmented reference material.
- All execution entry points (`AsRunnable`, `ExecuteStream`, `Start`) retrieve first.
- `ConfigureForRAG()` only sets the timing middleware, `MaxTokens=3000`, and `Temperature=0.3`; it does not cache answers, so that identical questions do not hit stale evidence. When caching is needed, explicitly configure a cache policy that distinguishes users, sessions, and knowledge versions.

### Filtering by Tenant or Permission

`RetrievalConfig.Filter` produces a metadata filter for each request, and the retriever executes it via `interfaces.RetrievalRequest`:

```go
WithRetriever(retriever, builder.RetrievalConfig{
    Filter: func(ctx context.Context, query string) (map[string]any, error) {
        tenant, ok := tenantFrom(ctx)
        if !ok {
            return nil, errors.New("missing tenant")
        }
        return map[string]any{"tenant_id": tenant}, nil
    },
})
```

- Each key in the filter must equal the document metadata field of the same name; multiple keys must all match.
- The retriever must implement `interfaces.RequestRetriever`, otherwise `Build` returns a configuration error; it does not fall back to unfiltered retrieval.
- If `Filter` returns an error or an empty filter, this execution fails without calling the model, to avoid retrieving all data when the tenant is missing.
- goagent-contrib's `VectorStoreRetriever` (backed by the in-memory vector store or Qdrant) supports filtering; the keyword, hybrid, multi-query, and reranking retrievers do not yet.

Output metadata:

| Field | Meaning |
|---|---|
| `retrieval_sources` | `[]builder.RetrievalSource`: number, document ID, source, and retrieval score; contains only the documents actually passed to the model |
| `retrieval_document_count` | Number of documents actually used |
| `retrieval_context_runes` | Number of body characters actually used |
| `retrieval_truncated` | Whether evidence was omitted because of the document count or character budget |

Retriever implementations (BM25, hybrid retrieval, reranking, Qdrant, etc.) live in [goagent-contrib/retrieval](https://github.com/kart-io/goagent-contrib/tree/master/retrieval); all satisfy `interfaces.DocumentRetriever` and can be passed directly to `RAGAgent` or `WithRetriever`.

## Real Model Token Streaming

Every `llm.Client` (such as the OpenAI, DeepSeek, and Gemini providers) can be called directly:

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

`Delta` is the new text and `Content` is the accumulated text. A usage frame may follow `finish_reason`, so only the terminal chunk is marked `Done=true`. Read errors in the stream are returned through `chunk.Error`. OpenAI-compatible services (including DeepSeek) send `stream_options.include_usage` by default to obtain usage; strict gateways that reject unknown fields can turn it off with `llm.WithStreamUsage(false)`, in which case the stream usually carries no usage.

For Agent-level event streams, use `agent.AsRunnable().Stream` or `ExecuteStream`; see [Execution Contract](EXECUTION_CONTRACT.md) and [event stream](PRIORITY_CAPABILITIES.md#unified-event-stream).
