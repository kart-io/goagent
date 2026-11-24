# Retrieval, Event Streams, MCP, and Durable Steps

[English](PRIORITY_CAPABILITIES.md) | [中文](PRIORITY_CAPABILITIES_CN.md)

This document summarizes four kinds of entry points: the retriever contract in goagent-contrib, the Builder unified event stream, the official MCP client adapter, and `core/durable` sequential steps. All Builder entry points go through the unified streaming execution pipeline.

## Retriever Execution Contract

Retrievers live in the separate module [goagent-contrib/retrieval](https://github.com/kart-io/goagent-contrib/tree/master/retrieval) (import path `github.com/kart-io/goagent-contrib/retrieval`); the APIs in this section and the next are all in that module. They satisfy `interfaces.DocumentRetriever` and can be passed directly to `builder.WithRetriever`.

Built-in retriever constructors bind the retrieval implementation explicitly, and `Invoke/Stream/Batch/Pipe` and `WithConfig/WithCallbacks` all call that implementation. For a custom retriever, use:

```go
r := retrieval.AsRetriever(customDocumentRetriever)
// or
r := retrieval.NewFuncRetriever(func(ctx context.Context, query string) ([]*interfaces.Document, error) {
    return loadDocuments(ctx, query)
})
```

`NewBaseRetriever()` without a bound implementation returns a configuration error; merely embedding BaseRetriever does not give you virtual method dispatch. Batch uses worker goroutines limited by MaxConcurrency, preserves input order, and does not swallow per-input errors. A failure in composite retrieval cancels the remaining work, and backends must still honor the context; MultiQuery/Ensemble do not return partially successful results, and callers that need degradation must implement it explicitly.

Chinese keyword retrieval uses single-character and adjacent two-character Han indexes, mixed with English terms; this is a lexical baseline that needs no dictionary and has no semantic synonym, domain-specific segmentation, or cross-language recall capability. BM25 reads document lengths from the index; the weighted sum in hybrid retrieval uses normalized scores to avoid the scale mismatch between BM25 and vector scores.

## Real LLM Reranking

```go
ranker := retrieval.NewLLMRerankerWithClient(client, 4)
ranker.MaxCandidates = 20
ranker.MaxDocumentRunes = 4000
r := retrieval.NewRerankingRetriever(base, ranker, 20, retrieval.RetrieverConfig{TopK: 4})
```

A single request scores all candidates, identifies documents by candidate index, and verifies that the scores are complete, free of duplicates, and within `[0,1]`. Errors are returned directly, with no fallback to simulated scores. Documents are truncated by Unicode character; exceeding the candidate limit is an error, with default limits of 20 documents and 4000 characters per document. Query length is controlled by the caller. `FetchK` limits the number of candidates sent to reranking; the base retriever's TopK should be no less than FetchK, and the wrapper does not modify the shared retriever.

`NewLLMReranker(topN)` without a Client set returns an error for non-empty candidates. `CrossEncoderReranker` is deprecated; it is actually a lexical-overlap algorithm (same as `LexicalOverlapReranker`) and does no cross-encoder inference. When you need a real model, use the LLM entry point above or `CohereReranker` from `retrieval/cohere`.

## Unified Event Stream

```go
ctx, cancel := context.WithCancel(context.Background())
defer cancel() // must cancel when the consumer exits early
stream, err := agent.ExecuteStream(ctx, "退款政策是什么？")
if err != nil { return err }
for event := range stream {
    switch event.Type {
    case "sources": // event.Sources, numbered consistently with the model's reference material
    case "step":    // event.Step: model_start, model_replayed, tool_start, tool_end
    case "text":    // event.Delta, native model delta
    case "usage":   // event.Usage, cumulative usage snapshot across all model requests
    case "error":   // event.Error (Go); event.Message in JSON
    case "done":    // event.Output, the final result after middleware and completion callbacks
    }
}
```

Any `llm.Client` works; when tools are configured, the client must implement `llm.ToolStreamClient` (OpenAI, DeepSeek, Gemini, Anthropic, and OpenAI-compatible presets such as Kimi, SiliconFlow, and Ollama), otherwise Build returns an error. In the native tool conversation loop, the model chooses a tool or answers directly on each round, which removes the READY confirmation and the separate answer request. Tool arguments are not shown token by token. On the native path, `MaxIterations` counts all model requests and returns an error when the limit is reached; metadata records `model_calls` and `usage_complete`. See [Execution Contract and Request Optimization](EXECUTION_CONTRACT.md).

Middleware wraps the whole execution, including OnBefore, OnAfter, and OnError; intermediate events are transient content, and done is the final indicator of success. The final text modified by OnAfter may also differ from the deltas already emitted. An error termination does not send done afterwards. On cancellation, the error event is sent on a best-effort basis, and the caller should check the context as well. Consumers must keep reading or cancel, to avoid backpressure hanging the upstream. The final request honors the Builder's MaxTokens/Temperature; state is updated after successful generation, and a checkpoint is written when auto-save is enabled (a save failure terminates the run and done is not sent). Unknown usage is never estimated as a real value; `answer_usage_reported` indicates whether usage for the final answer round was reported: it is true when the model reported usage, or when the round came entirely from the model call cache (a hit has no usage to report).

## Official MCP Client Adapter

```go
session, err := remote.Connect(ctx, &mcp.StreamableClientTransport{
    Endpoint: "http://localhost:8080/mcp",
})
if err != nil { return err }
defer session.Close()
tools, err := remote.Tools(ctx, session)
// builder.NewSimpleBuilder(client).WithTools(tools...)
```

`remote` is `github.com/kart-io/goagent/mcp/remote`; `mcp` is `github.com/modelcontextprotocol/go-sdk/mcp`. The official SDK v1.8.0 handles protocol negotiation, transport, pagination, and cancellation; you can also pass a `CommandTransport` to use stdio. Both HTTP and real subprocess stdio have interoperability tests. Tools keep the full CallToolResult (including structured content and other content blocks), and IsError is converted to a Go error (`remote.WithToolErrorsAsRejected()` changes this to a rejection result handed back to the model). `remote.WithToolName` renames or filters tools, and `remote.WithDurablePolicy` sets the operation policy for durable calls through the Dispatcher. Authentication, request size limits, and the HTTPClient are configured by the SDK transport. When interactive input is required, `*remote.InputRequiredError` is returned by default, and its `Continue` can be used to resume on the SDK session; with `remote.WithInputHandler` the host collects input, and `remote.WithDurableInput` pauses with `durable.AwaitInput` in a durable run (see the [MCP README](../../mcp/README.md)). The adapter layer does not approve input requests automatically. The tool list is a snapshot of this discovery; re-read it after it changes.

References: [official SDK](https://github.com/modelcontextprotocol/go-sdk/tree/v1.8.0), [transport notes](https://github.com/modelcontextprotocol/go-sdk/blob/v1.8.0/docs/protocol.md).

## Durable Step Execution

```go
store, err := durable.NewFileStore("./checkpoints")
if err != nil { return err }
runner := &durable.Runner{Store: store, Version: "refund-v1", Steps: []durable.Step{
    {Name: "refund", RequireApproval: true,
     Run: func(ctx context.Context, in durable.StepInput) (json.RawMessage, error) {
         // Pass in.Key to the external service as the idempotency key; in.Data is the previous step's output.
         return refundWithIdempotency(ctx, in.Key, in.Data)
     }},
}}
snapshot, err := runner.Run(ctx, "request-123", json.RawMessage(`{"order":"123"}`), "")
// errors.Is(err, agentErrors.ErrApprovalRequired): save/show the snapshot and wait for the application to authorize.
// After a restart, re-register the steps of the same version, then pass the approved snapshot.Key:
snapshot, err = runner.Run(ctx, "request-123", nil, snapshot.Key)
```

`core/durable` persists the next step position, data, approval state, and idempotency key. FileStore uses a per-run-ID OS file lock, file fsync, atomic replacement in the same directory, and directory sync. Committed steps are not rerun; a change in the run version, step names, or approval settings causes old checkpoints to be rejected. When a function's implementation changes, Version must be bumped, since closure contents cannot be compared automatically. `Runner.BeforeApprove` can check, before an approval takes effect, whether external data was changed during the pause.

A crash after the side effect runs but before the checkpoint is saved **retries the step with the same key**; this is at-least-once execution, requires the external service to deduplicate by key, and does not guarantee exactly once. The approval key is not an authentication credential; the application is responsible for identity, authorization, and the approval UI. The initial data of a run is written only once. FileStore targets local file systems with reliable lock/atomic-replace semantics; for runs shared across multiple instances, use `durable/pgstore` from goagent-contrib.

The tests cover recovery after a real subprocess exits, approval matching, idempotency key reuse after a failure, completed steps not being rerun, lock-wait cancellation, and rejection of corrupted checkpoints. This is for explicitly registered sequential steps; it does not restore arbitrary Agent call stacks or dynamic graphs. For graph-level recovery, see [Native Orchestration](NATIVE_ORCHESTRATION.md).

The [refund approval and crash recovery example](https://github.com/kart-io/goagent-examples/blob/main/workflow/refund-recovery/README.md) combines a checkpointed graph, approval checkpoints, and a standalone SQLite simulated refund service, and verifies idempotent retries after side-effect commit through real process exits.

## Reasoning Output Contract Notes

Reasoning strategies such as ReAct and ToT were removed in v0.17.0; the code remains in `v0.16.1`. The current replacement is the native tool loop and `workflow` Graph; see [Native Orchestration and the Tool System](NATIVE_ORCHESTRATION.md).
