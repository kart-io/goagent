# GoAgent Quickstart

[English](QUICKSTART.md) | [中文](QUICKSTART_CN.md)

New applications should use `agent.AsRunnable()` throughout. Synchronous and streaming calls share the same execution source, and configured tools, retrievers, middleware, and completion callbacks all go through the same path. Requires Go 1.27+ and a client implementing `llm.Client` (`CompleteStream`); the built-in providers (OpenAI and its compatible presets, DeepSeek, Gemini, Anthropic) all also implement native tool streaming (`llm.ToolStreamClient`).

```sh
go get github.com/kart-io/goagent
export OPENAI_API_KEY=...
export OPENAI_MODEL=...
# Optional: export OPENAI_BASE_URL=https://api.deepseek.com
```

## Create and Invoke an Agent

```go
package main

import (
    "context"
    "fmt"
    "log"
    "time"

    "github.com/kart-io/goagent/builder"
    "github.com/kart-io/goagent/llm/providers"
)

func main() {
    client, err := providers.NewOpenAIWithOptions()
    if err != nil { log.Fatal(err) }
    agent, err := builder.NewSimpleBuilder(client).
        WithSystemPrompt("You are a helpful assistant.").
        WithMaxIterations(6).
        WithTimeout(30 * time.Second).
        Build()
    if err != nil { log.Fatal(err) }
    output, err := agent.AsRunnable().Invoke(context.Background(), "Hello, please introduce yourself.")
    if err != nil { log.Fatal(err) }
    fmt.Println(output.Result)
}
```

`Invoke` collects the final result of the real stream; it does not make an extra synchronous model call. A plain answer needs one request; a typical tool task needs one request to choose the tool and one to answer. MaxIterations includes the final answer.

## Tools, RAG, and Memory

```go
agent, err := builder.NewSimpleBuilder(client).
    WithTools(lookupTool).
    WithRetriever(retriever, builder.RetrievalConfig{MaxDocuments: 4, MaxContextRunes: 12000}).
    WithMaxIterations(8).
    Build()
// After checking err, call it via agent.AsRunnable().Invoke(ctx, question).
```

Tools implement `interfaces.Tool`, providing Name, Description, ArgsSchema, and Invoke; a function can be wrapped with `tools.NewFunctionTool`. Retrievers implement `interfaces.DocumentRetriever`; BM25, vector, hybrid retrieval, and reranking implementations live in goagent-contrib.

- For a single terminal tool whose result is already complete and presentable, configure `WithReturnDirectTools("lookup")` explicitly; the model is then not called again to summarize or add citations.
- `WithSessionLog(store).WithSessionID(sessionID)` enables multi-turn conversations; `WithCompaction` compresses earlier messages into a summary when the conversation gets too long.
- Long-running tasks can use `WithOffload` to replace oversized tool results with references that are read back page by page through a read tool; see [Context Governance](CONTEXT_POLICY.md). Disabled by default.
- For retriever implementations see [goagent-contrib/retrieval](https://github.com/kart-io/goagent-contrib/tree/master/retrieval); [MCP and Resumable Steps](PRIORITY_CAPABILITIES.md) shows external tools and approval resumption.

## Streaming

```go
ctx, cancel := context.WithCancel(context.Background())
defer cancel() // Must cancel when you stop consuming early.
chunks, err := agent.AsRunnable().Stream(ctx, question)
if err != nil { return err }
for chunk := range chunks {
    if chunk.Error != nil { return chunk.Error }
    if chunk.Event != nil {
        fmt.Print(chunk.Event.Delta) // Transient text; may include commentary before a tool call.
    }
    if chunk.Done {
        fmt.Println(chunk.Data.Result) // Final result after middleware and callbacks.
    }
}
if err := ctx.Err(); err != nil { return err }
```

Events also include sources, step, and usage. usage is a cumulative snapshot; do not sum it repeatedly. A Builder's memory and state belong to a single session; all Builder execution entry points serialize execution for that session, queueing time counts toward Timeout, and cross-session concurrency should use separate Builders.

## Migrating from Legacy APIs

| Old call | Recommended for new applications | Behavior change |
|---|---|---|
| `agent.Execute(ctx, input)` (removed) | `agent.AsRunnable().Invoke(ctx, input)` | Requires a streaming model and executes configured tools |
| `agent.ExecuteWithTools(ctx, input)` (removed) | `agent.AsRunnable().Invoke(ctx, input)` | Requires `llm.ToolStreamClient`; the budget includes the final answer |
| `agent.ExecuteStream(ctx, input)` | `agent.AsRunnable().Stream(ctx, input)` | Keeps the same event source and adds Runnable composition |

The Builder accepts any `llm.Client`; when tools are configured it also requires `llm.ToolStreamClient`, checked at Build time. The old `Execute` did not run tools; after migration, configured tools will be called, so confirm tool side effects and budgets. `Pipe` waits for the upstream final result before calling downstream; for complex branching, parallel joins, and bounded loops use [workflow.Graph](../../workflow/README.md) and plug in existing Agents via Adapt. See the [Execution Contract](EXECUTION_CONTRACT.md) for detailed boundaries.

## Migration Checks and Continuous Verification

When upgrading from v0.42 or earlier to v0.46, also handle the following changes; for full signatures and removal lists see the [CHANGELOG](../../CHANGELOG.md).

| Version | Changes integrators must handle |
|---|---|
| v0.43 | `llm.Client` is unified as a streaming interface and `llm.StreamClient` is merged into it; collect synchronously with `llm.Complete` / `llm.Chat`. Tool calling implements `llm.ToolStreamClient`. Custom clients, cache wrappers, and test mocks must be migrated accordingly. |
| v0.43 | The old `interfaces.Agent/Runnable` is replaced by `core.Runnable`; the local `mcp/core`, `mcp/toolbox`, and `mcp/tools` are removed. Use `interfaces.Tool` and `tools.Pipeline` for local tools and `mcp/remote` for remote MCP. |
| v0.44 | Embeddings, Document/Case vectors, and related store interfaces are unified on `[]float32`; update custom embedders, vector store signatures, and vector literals. |
| v0.45 | Semantic cache drops its standalone OpenAI Embedding implementation; pass a client created by `providers.NewOpenAIWithOptions` instead. Use `llm.WithEmbeddingModel` to explicitly keep an existing cache model. Vectors from the old default `text-embedding-3-small` and the new default `text-embedding-ada-002` are incompatible; rebuild the cache when switching models. |
| v0.46 | The optional `RetrievalConfig.Filter` builds filter conditions per request; the retriever must also implement `interfaces.RequestRetriever`. If the filter function returns an error or an empty condition, execution fails instead of falling back to unfiltered retrieval; tenant and authorization conditions are supplied by the trusted host. |

```sh
# Check layering constraints on real Go imports.
bash scripts/verify_imports.sh
```

Legacy entry points and interfaces have been removed, and leftover calls fail to compile. When migrating item by item, confirm the provider's ToolStreamClient support, automatic tool execution, budgets, and callback semantics; do not bulk-replace.

[Provider Continuous Regression](PROVIDER_REGRESSION.md) provides offline gates and bounded real tests. Refund applications can reuse [refund.Service](../../workflow/refund/README.md); serial graph recovery uses [CompileDurable](../../workflow/README.md#durable-recovery-at-node-boundaries).

Complex business logic can be built entirely on [GoAgent Native Orchestration and Tooling](NATIVE_ORCHESTRATION.md): the Dispatcher serves both graph nodes and the Builder, and the durable graph's AsRunnable provides unified invocation, step events, and approval resumption. No other orchestration framework is needed.
