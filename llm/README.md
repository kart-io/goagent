# llm: LLM Integration Layer

[English](README.md) | [中文](README_CN.md)

`llm` defines the model client interfaces, message and streaming types, configuration options, and error classification; `llm/providers` provides the implementation for each provider. For full documentation of interfaces and options see the [LLM API Reference](../docs/api/LLM_API.md); for each provider's configuration and defaults see the [LLM Providers Guide](../docs/guides/LLM_PROVIDERS.md); the [capability matrix](../docs/guides/PROVIDER_CAPABILITIES.md) is authoritative for which providers actually support native tool streaming and Embedding.

## Package Layout

```text
llm/
├── client.go         # Client, Message, CompletionRequest/Response, LLMOptions
├── complete.go       # Complete, Chat: collect a stream into a full response
├── stream_client.go  # ToolStreamClient, StreamChunk, StreamReader/Writer
├── capabilities.go   # Capability, CapabilityChecker, optional interfaces and Has*/As* detection functions
├── tools.go          # ToolCall and default parameters
├── provider_error.go # ProviderError, ClassifyError
├── options.go        # ClientOption (With*)
├── common/           # BaseProvider, retries, HTTP error mapping, message conversion
├── providers/        # OpenAI, Anthropic, Gemini, DeepSeek, Ollama, Kimi, SiliconFlow
```

## Capability Overview

| Provider | Native tool streaming `ToolStreamClient` | Declares Embedding |
|---|:---:|:---:|
| OpenAI | ✓ | ✓ |
| DeepSeek | ✓ | - |
| Gemini | ✓ | ✓ |
| Anthropic (official SDK) | ✓ | - |
| Kimi, SiliconFlow, Ollama (OpenAI-compatible presets) | ✓ | - |

`llm.Client` has only `CompleteStream` and `Provider`; every provider implements it and `CapabilityChecker`. When you need a full response, use `llm.Complete` / `llm.Chat` to collect the stream. The native tool loop requires `ToolStreamClient`; check for the interface with `llm.HasToolStreaming`.

## Creating a Client and Chatting

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
    llm.SystemMessage("You are a helpful assistant"),
    llm.UserMessage("Hello!"),
})
if err != nil {
    return err
}
fmt.Println(resp.Content)
```

When `WithAPIKey`, `WithBaseURL`, or `WithModel` are left empty, the `<PROVIDER>_API_KEY`, `<PROVIDER>_BASE_URL`, and `<PROVIDER>_MODEL` environment variables are read (for example `OPENAI_API_KEY`), then defaults are used. `WithBaseURL` can point to a compatible endpoint.

## Streaming Responses

```go
ctx, cancel := context.WithCancel(ctx)
defer cancel() // releases the connection on early exit
chunks, err := client.CompleteStream(ctx, &llm.CompletionRequest{Messages: messages})
if err != nil {
    return err
}
text, err := llm.NewStreamReader(chunks).ReadAll(ctx)
```

When processing chunk by chunk, check `chunk.Error` and print `chunk.Delta`; the `Done` chunk carries the finish reason and usage.

## Native Tool Streaming

Tools are usually used through the Builder (it handles tool execution, result continuation, and `ProviderState`); see [builder](../builder/). When calling `CompleteToolsStream` directly, read the complete `ToolCalls` only from an error-free `Done` chunk:

```go
tc := llm.AsToolStreamClient(client)
if tc == nil {
    return errors.New("this provider does not support native tool streaming")
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
        // Append to the next request: a message with Role assistant carrying ToolCalls/ReasoningContent/ProviderState,
        // plus one result message per call with Role tool and the matching ToolCallID.
        for _, call := range chunk.ToolCalls {
            fmt.Println(call.Function.Name, call.Function.Arguments)
        }
    }
}
```

`interfaces.Tool` is an interface (`Name`, `Description`, `ArgsSchema`, `Invoke`) and can be constructed with `tools.NewFunctionTool` and others; see [tools](../tools/).

## Text Embedding

```go
embedder := llm.AsEmbedder(client) // returns nil if Embedding is not implemented or not declared
if embedder == nil {
    return errors.New("this provider does not support Embedding")
}
vector, err := embedder.Embed(ctx, "This is some text to be vectorized")
```

The Gemini embedding model is set with `llm.WithEmbeddingModel` or `GEMINI_EMBEDDING_MODEL`, defaulting to `gemini-embedding-001`.

## Error Classification

`llm.ClassifyError(err)` returns `Reason`, `Retryable`, `ShouldCompress`, and `ShouldFallback`, based on the `*llm.ProviderError` in the error chain (status code and `RetryAfter`) and on context-overflow error text. Every provider constructs a `ProviderError` when streaming and non-streaming requests fail. For model retries in the Builder use `WithRequestError` and `builder.RetryTransient`.

## Related Packages

- [builder](../builder/) - Agent builder
- [tools](../tools/) - tool system
- [interfaces](../interfaces/) - interface definitions

## Explicit Zero Temperature

`llm.WithTemperature(0)` sets a zero temperature on the client; when the Builder has not called `builder.WithTemperature`, no temperature is sent and the client configuration decides, and when set explicitly it takes precedence over the client. When constructing a request directly, use `llm.CompletionRequest{Temperature: 0, TemperatureSet: true}`. A JSON `"temperature":0` automatically preserves the explicit setting, while legacy requests that do not set it still use the default fallback. Custom providers should use `BaseProvider.ResolveTemperature(req)`; see [LLM API](../docs/api/LLM_API.md).

`llm.WithReasoning(usage.ReasoningConfig{Effort: "low"})` sets an explicit reasoning default for clients that support it; Gemini can set `BudgetSet: true, BudgetTokens: 128`. A per-request `CompletionRequest.Reasoning` can override the default. The `ResponseModel` of a structured stream preserves the model name the provider actually used, and `Usage` provides reasoning and tool-input breakdowns backed by field evidence; do not count them into the total again. For implementation details and unknown semantics see the [design contract](../docs/design/TOKEN_USAGE_EXPLAINABILITY.md).
