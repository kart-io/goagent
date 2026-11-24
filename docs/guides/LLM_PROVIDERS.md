<!-- generated-by: gsd-doc-writer -->
# LLM Provider Guide

[English](LLM_PROVIDERS.md) | [中文](LLM_PROVIDERS_CN.md)

`llm/providers` offers one `NewXxxWithOptions(opts ...llm.ClientOption)` constructor per provider, returning a client that implements `llm.Client`. See the [LLM API reference](../api/LLM_API.md) for interfaces and options; see [Provider Capability Boundaries](PROVIDER_CAPABILITIES.md) for actual support of streaming, tools, and embeddings.

## Configuration Sources

`APIKey`, `BaseURL`, and `Model` each take the first non-empty value in this order: explicit option (`llm.WithAPIKey` / `WithBaseURL` / `WithModel`) → environment variable → built-in default. The API key has no default; except for Ollama, the constructor returns an error when it is missing. Constructors send no requests, so a successful construction does not mean the service or model is available.

| Provider | Constructor | API key env var | BaseURL / Model env vars | Default BaseURL | Default model |
|---|---|---|---|---|---|
| OpenAI | `NewOpenAIWithOptions` | `OPENAI_API_KEY` | `OPENAI_BASE_URL` / `OPENAI_MODEL` | `https://api.openai.com/v1` | `gpt-4-turbo-preview` |
| Anthropic | `NewAnthropicWithOptions` | `ANTHROPIC_API_KEY` | `ANTHROPIC_BASE_URL` / `ANTHROPIC_MODEL` | `https://api.anthropic.com` | `claude-opus-5-5` |
| Gemini | `NewGeminiWithOptions` | `GEMINI_API_KEY` | `GEMINI_BASE_URL` / `GEMINI_MODEL` | `https://generativelanguage.googleapis.com` | `gemini-2.5-flash` |
| DeepSeek | `NewDeepSeekWithOptions` | `DEEPSEEK_API_KEY` | `DEEPSEEK_BASE_URL` / `DEEPSEEK_MODEL` | `https://api.deepseek.com/v1` | `deepseek-chat` |
| Kimi | `NewKimiWithOptions` | `KIMI_API_KEY` | `KIMI_BASE_URL` / `KIMI_MODEL` | `https://api.moonshot.cn/v1` | `moonshot-v1-8k` |
| SiliconFlow | `NewSiliconFlowWithOptions` | `SILICONFLOW_API_KEY` | `SILICONFLOW_BASE_URL` / `SILICONFLOW_MODEL` | `https://api.siliconflow.cn/v1` | `Qwen/Qwen2-7B-Instruct` |
| Ollama | `NewOllamaWithOptions` | Not required | `OLLAMA_BASE_URL` / `OLLAMA_MODEL` | `http://localhost:11434/v1` | `llama2` |

Environment variable names and Provider types are defined centrally in `github.com/kart-io/goagent/constants` (e.g. `constants.EnvOpenAIAPIKey`). Default models are only fallbacks; production deployments should explicitly specify a model that is currently available.

### Provider Notes

- **Gemini**: Calls the native API through the Google GenAI SDK with API key authentication. Set `GEMINI_BASE_URL` to the native root URL, not the OpenAI-compatible `/v1beta/openai` path. Specify the embedding model with `llm.WithEmbeddingModel` or `GEMINI_EMBEDDING_MODEL`; the default is `gemini-embedding-001`. The native tool loop keeps signatures in `Message.ProviderState`; if you manage message history yourself, send them back unchanged. See [gemini-verify](../../examples/llm/gemini-verify/README.md) for the verification case.
- **DeepSeek**: `NewDeepSeekWithOptions` implements structured streaming and native tool streaming directly; `NewDeepSeekStreamingWithOptions` is only a wrapper that embeds it. The old `Embed` method sends a compatible request but does not declare the Embedding capability.
- **OpenAI**: `WithBaseURL` can point to an OpenAI-compatible endpoint; verify the degree of compatibility yourself.
- **Anthropic**: Based on the official `anthropic-sdk-go/v2`; supports structured streaming and native tool streaming, and the SDK does no implicit retries. The API version `2026-10-21` sent by the SDK rejects sampling parameters, so `WithTemperature` and `TopP` have no effect on Anthropic. Thinking blocks are kept in `Message.ProviderState` along with the native tool loop; if you manage message history yourself, send them back unchanged. The SDK sends the API key as `Authorization: Bearer`; when going through a proxy or gateway that only accepts `x-api-key`, also send that header with `WithCustomHeaders(map[string]string{"X-Api-Key": key})`.
- **Kimi, SiliconFlow, Ollama**: Return `*OpenAIProvider` and support structured streaming and native tool streaming over the OpenAI-compatible protocol; `ListModels(ctx)` reads `/models`.
- **Ollama**: Uses the `/v1` compatible endpoint; a custom `OLLAMA_BASE_URL` must include `/v1`. `NewOllamaClientSimple(model)` is equivalent to `NewOllamaWithOptions` with only the model set; when no timeout is set, there is no timeout.

## Basic Chat

```go
client, err := providers.NewDeepSeekWithOptions(
    llm.WithAPIKey(os.Getenv("DEEPSEEK_API_KEY")),
    llm.WithModel("deepseek-chat"),
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
if resp.Usage != nil {
    fmt.Printf("input %d / output %d / total %d tokens\n",
        resp.Usage.PromptTokens, resp.Usage.CompletionTokens, resp.Usage.TotalTokens)
}
```

`llm.Chat` / `llm.Complete` collect the `CompleteStream` stream and return a response only on a successful terminal chunk. When `Temperature`, `MaxTokens`, `TopP`, or `Model` in `CompletionRequest` is zero, the client configuration is used.

## Streaming Chat

Every `llm.Client` provides structured streaming through `CompleteStream`:

```go
ctx, cancel := context.WithCancel(ctx)
defer cancel() // release the connection on early exit
stream, err := client.CompleteStream(ctx, &llm.CompletionRequest{
    Messages: []llm.Message{llm.UserMessage("Write a poem.")},
})
if err != nil {
    return err
}
for chunk := range stream {
    if chunk == nil {
        return fmt.Errorf("received nil chunk")
    }
    if chunk.Error != nil {
        return chunk.Error
    }
    fmt.Print(chunk.Delta)
}
```

## Switching Providers

Choose the constructor from configuration, return a uniform `llm.Client`, and hand it to the Builder:

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
        return nil, fmt.Errorf("unsupported provider: %s", name)
    }
}

client, err := createClient(os.Getenv("LLM_PROVIDER"))
if err != nil {
    return err
}
agent, err := builder.NewSimpleBuilder(client).BuildContext(ctx)
```

In the example above, API keys are read from each provider's environment variable. The native tool loop requires the client to implement `ToolStreamClient`; check the [capability matrix](PROVIDER_CAPABILITIES.md) before choosing a provider.

## Error Handling and Retries

Use `llm.ClassifyError` to determine the failure type; do not match on error text:

```go
resp, err := llm.Chat(ctx, client, messages)
if err != nil {
    switch c := llm.ClassifyError(err); {
    case c.ShouldCompress:
        // context window exceeded: shorten messages and retry
    case c.Retryable:
        // rate limit, 5xx, or transient connection error
    default:
        return err
    }
}
```

Classification relies on `*llm.ProviderError`, which every Provider constructs for both streaming and non-streaming request failures. To retry model requests in the Builder, use `WithRequestError(n, builder.RetryTransient(n, nil))`; see [Core API](../api/CORE_API.md#模型请求恢复).

## Timeouts

For the relationship between `llm.WithTimeout` and the request ctx, see [Provider Best Practices](PROVIDER_BEST_PRACTICES.md#timeout-configuration). Control per-call deadlines with ctx:

```go
ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
defer cancel()
resp, err := llm.Chat(ctx, client, messages)
```

## Related Documents

- [Quickstart](QUICKSTART.md)
- [Provider Best Practices](PROVIDER_BEST_PRACTICES.md)
- [Provider Capability Boundaries](PROVIDER_CAPABILITIES.md)
