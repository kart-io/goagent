# LLM Provider Best Practices

[English](PROVIDER_BEST_PRACTICES.md) | [中文](PROVIDER_BEST_PRACTICES_CN.md)

This document describes the recommended way to create and switch LLM Providers in GoAgent. For each Provider's configuration options, see the [LLM Provider Guide](LLM_PROVIDERS.md).

## Recommended Approach: Use Provider Constructors Directly

Each Provider offers a `NewXxxWithOptions(opts ...llm.ClientOption)` constructor in `llm/providers` that returns a client implementing `llm.Client`:

```go
import (
    "github.com/kart-io/goagent/llm"
    "github.com/kart-io/goagent/llm/providers"
)

client, err := providers.NewOpenAIWithOptions(
    llm.WithAPIKey(os.Getenv("OPENAI_API_KEY")),
    llm.WithModel("gpt-4"),
)
if err != nil {
    return err
}
```

Available constructors: `NewOpenAIWithOptions`, `NewAnthropicWithOptions`, `NewGeminiWithOptions`, `NewDeepSeekWithOptions`, `NewOllamaWithOptions`, `NewSiliconFlowWithOptions`, `NewKimiWithOptions`; all implement `llm.Client` and `llm.ToolStreamClient` (Kimi, SiliconFlow, and Ollama are OpenAI-compatible presets, and Anthropic is based on the official SDK); `NewDeepSeekStreamingWithOptions` and `NewGeminiStreamingWithOptions` are retained streaming wrappers.

GoAgent has no registry for looking up Providers by name; call the corresponding constructor directly.

## Use Cases

### Multiple Provider Support

Choose the constructor in your application based on configuration, returning a unified `llm.Client`:

```go
func createClient(provider string) (llm.Client, error) {
    switch provider {
    case "openai":
        return providers.NewOpenAIWithOptions(
            llm.WithAPIKey(os.Getenv("OPENAI_API_KEY")),
            llm.WithModel("gpt-4"),
        )
    case "deepseek":
        return providers.NewDeepSeekWithOptions(
            llm.WithAPIKey(os.Getenv("DEEPSEEK_API_KEY")),
            llm.WithModel("deepseek-chat"),
        )
    default:
        return nil, fmt.Errorf("不支持的 provider：%s", provider)
    }
}
```

### Provider Fallback

Constructors only validate configuration and do not send requests, so successful construction does not mean the service is available. Fallback should be based on errors from actual calls:

```go
func chatWithFallback(ctx context.Context, primary, backup llm.Client, messages []llm.Message) (*llm.CompletionResponse, error) {
    resp, err := llm.Chat(ctx, primary, messages)
    if err == nil {
        return resp, nil
    }
    return llm.Chat(ctx, backup, messages)
}
```

Fallback sends the same request twice, so use it only for idempotent, repeatable calls.

### Testing

In tests, use `MockLLMClient` from `testing/mocks`, which returns preset responses in order:

```go
import (
    "github.com/kart-io/goagent/llm"
    "github.com/kart-io/goagent/testing/mocks"
)

client := mocks.NewMockLLMClient()
client.SetResponses(llm.CompletionResponse{Content: "预设回答"})
```

## Timeout Configuration

The argument to `llm.WithTimeout` is a `time.Duration`, stored in whole seconds with positive values rounded up; when unset there is no timeout, and the same holds for Ollama; pass one explicitly when you need a bound, for Ollama for example `constants.DefaultOllamaTimeout`:

```go
llm.WithTimeout(30 * time.Second) // 30 seconds
llm.WithTimeout(30)               // Wrong unit: 30 nanoseconds, rounded up to 1 second
```

The OpenAI Provider and DeepSeek's streaming methods use it to limit how long to wait for the response to start: a non-streaming request returns only after generation finishes, so it is limited as a whole; a streaming request is no longer limited after the first chunk arrives, so long replies are not cut off. After the stream starts, use `llm.WithStreamIdleTimeout` (maximum gap between two pieces of data) and `llm.WithStreamTimeout` (total streaming duration) to limit it (effective for OpenAI, DeepSeek, and Anthropic streaming); when unset, only ctx controls it. When the consumer stops reading, it must cancel ctx, otherwise the goroutine and connection are not released. Other Providers and DeepSeek's non-streaming methods use it as the time limit for the whole HTTP request.

## Related Documents

- [LLM Provider Guide](LLM_PROVIDERS.md) - Configuration and usage examples for each Provider
