# providers: Built-in LLM Clients

[English](README.md) | [中文](README_CN.md)

`providers` implements `llm.Client` for OpenAI and OpenAI-compatible endpoints (DeepSeek, Kimi, SiliconFlow, Ollama, MiniMax), Gemini, and Anthropic. Reach for it when you need a concrete model client to pass to the Builder or to `llm.Chat` / `llm.Complete`. Each provider has one `NewXxxWithOptions(opts ...llm.ClientOption)` constructor; there is no registry that looks providers up by name.

## Usage

```go
client, err := providers.NewOpenAIWithOptions(
    llm.WithAPIKey(os.Getenv("OPENAI_API_KEY")),
    llm.WithModel("gpt-4o-mini"),
    llm.WithTimeout(60*time.Second),
    llm.WithStreamIdleTimeout(30*time.Second),
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
```

OpenAI-compatible presets return `*OpenAIProvider`, so they share its methods:

```go
// Ollama needs no API key; BaseURL must point to the /v1 endpoint.
client, err := providers.NewOllamaWithOptions(llm.WithModel("qwen3"))
if err != nil {
    return err
}
models, err := client.ListModels(ctx) // reads /models
```

## Key API

| Identifier | Description |
|---|---|
| `NewOpenAIWithOptions` → `*OpenAIProvider` | OpenAI Chat Completions; `WithBaseURL` can point to any compatible endpoint |
| `NewKimiWithOptions`, `NewSiliconFlowWithOptions`, `NewOllamaWithOptions`, `NewMiniMaxWithOptions` | OpenAI-compatible presets returning `*OpenAIProvider` |
| `NewOllamaClientSimple(model)` | Ollama client with only the model set |
| `NewDeepSeekWithOptions` → `*DeepSeekProvider` | DeepSeek; streams through the same SSE decoder as OpenAI |
| `NewGeminiWithOptions` → `*GeminiProvider` | Native Gemini API through the Google GenAI SDK |
| `NewAnthropicWithOptions` → `*AnthropicProvider` | Messages API through the official Anthropic SDK |
| `CompleteStream` / `CompleteToolsStream` | Structured streaming and native tool streaming (`llm.Client`, `llm.ToolStreamClient`) |
| `Embed` / `EmbedBatch` | Embeddings on `OpenAIProvider` and `GeminiProvider`; `DeepSeekProvider.Embed` is a legacy compatible request |
| `ValidateReasoning(req)` | Checks the reasoning configuration on OpenAI, DeepSeek, and Gemini before sending |
| `NewDeepSeekStreamingWithOptions`, `NewGeminiStreamingWithOptions` | Retained wrappers with `StreamWithMetadata` / `StreamWithContext` |

## Behavior Notes

- `APIKey`, `BaseURL`, and `Model` take the explicit option first, then the provider's environment variable, then the built-in default (names in `constants`, e.g. `constants.EnvOpenAIAPIKey`). Except for Ollama, a missing API key is a constructor error. Constructors send no requests.
- No client sets a timeout unless `llm.WithTimeout` is passed. For OpenAI-compatible clients, DeepSeek, and Anthropic the timeout only bounds waiting for response headers, so long streams are not cut; `llm.WithStreamIdleTimeout` and `llm.WithStreamTimeout` abort a stalled or overlong stream with a retryable timeout on these clients.
- Anthropic, Gemini, and DeepSeek streaming make no implicit SDK retries; one call is one upstream request.
- Anthropic does not send `temperature` or `top_p`, rejects a negative `WithMaxTokens`, uses `constants.DefaultClientMaxTokens` when max tokens is 0, and rejects non-text response formats.
- Per-request reasoning (`CompletionRequest.Reasoning`) is accepted by OpenAI, MiniMax, Gemini, and DeepSeek (DeepSeek only effort `"none"`, sent as thinking disabled; a thinking budget is rejected on the OpenAI-compatible path). Client-level `llm.WithReasoning` is accepted only by the OpenAI, DeepSeek, and Gemini constructors. Other providers return an error instead of ignoring it.
- `DeepSeekProvider.CompleteToolsStream` restores DSML tool calls written into the content as `ToolCalls`.
- Kimi, SiliconFlow, and Ollama declare a conservative capability set: no embedding, vision, or JSON mode. Check `HasCapability` before relying on a feature.
- Gemini and Anthropic keep opaque state in `Message.ProviderState`; send it back unchanged when you manage message history yourself.
- Callers of `CompleteStream` must cancel ctx when they stop reading; `llm.Chat` / `llm.Complete` do this for you.

## See also

- [LLM Provider Guide](../../docs/guides/LLM_PROVIDERS.md)
- [Provider Capability Boundaries](../../docs/guides/PROVIDER_CAPABILITIES.md)
- [Provider Best Practices](../../docs/guides/PROVIDER_BEST_PRACTICES.md)
- [MiniMax over the Anthropic protocol](../../docs/guides/MINIMAX_ANTHROPIC.md)
- [llm](../README.md), [llm/common](../common/README.md)
