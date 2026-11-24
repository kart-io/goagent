# common: Provider Building Blocks

[English](README.md) | [中文](README_CN.md)

`common` provides shared building blocks for LLM provider implementations: `BaseProvider` (option handling, environment fallback, parameter resolution, HTTP client creation), retry with exponential backoff, HTTP status to error mapping, capability declaration, and message conversion helpers. The built-in clients in [llm/providers](../providers/README.md) embed it. Reach for it when you write your own `llm.Client` and want the same configuration and error behavior as the built-in providers; application code that only calls models does not need it.

## Usage

```go
type myProvider struct {
    *common.BaseProvider
    *common.ProviderCapabilities
}

func newMyProvider(opts ...llm.ClientOption) (*myProvider, error) {
    base := common.NewBaseProvider(opts...)
    // explicit option -> environment variable -> default
    base.ApplyProviderDefaults(constants.Provider("myllm"), "https://api.example.com/v1", "my-model", "MYLLM_BASE_URL", "MYLLM_MODEL")
    if err := base.EnsureAPIKey("MYLLM_API_KEY", constants.Provider("myllm")); err != nil {
        return nil, err
    }
    return &myProvider{
        BaseProvider:         base,
        ProviderCapabilities: common.NewProviderCapabilities(llm.CapabilityCompletion, llm.CapabilityStreaming),
    }, nil
}

func (p *myProvider) send(ctx context.Context, body any) (string, error) {
    client := p.NewHTTPClient(common.HTTPClientConfig{
        BaseURL: p.Config.BaseURL,
        Headers: map[string]string{"Authorization": "Bearer " + p.Config.APIKey},
    })
    return common.ExecuteWithRetry(ctx, common.DefaultRetryConfig(), p.ProviderName(), func(ctx context.Context) (string, error) {
        resp, err := client.R().SetContext(ctx).SetBody(body).Post("/chat")
        if err != nil {
            return "", err
        }
        if resp.IsError() {
            return "", common.MapHTTPError(common.RestyResponseToHTTPError(resp), p.ProviderName(), p.ModelName(), nil)
        }
        return resp.String(), nil
    })
}
```

A complete client must still implement `CompleteStream` and `Provider()` from `llm.Client`.

## Key API

| Identifier | Description |
|---|---|
| `BaseProvider`, `NewBaseProvider(opts...)` | Holds the resolved `*llm.LLMOptions` in `Config` |
| `ApplyProviderDefaults`, `EnsureAPIKey`, `EnsureBaseURL`, `EnsureModel` | Fill provider identity, base URL, model, and API key from option, then environment variable, then default |
| `GetModel`, `GetMaxTokens`, `ResolveTemperature`, `GetTopP`, `GetTimeout`, `WithTimeout` | Resolve request parameters against client configuration |
| `ResolveReasoning`, `RejectReasoning` | Resolve the request's reasoning config, or reject it for providers without support |
| `NewHTTPClient(HTTPClientConfig)` | Returns a cached `*httpclient.Client` merging `WithCustomHeaders` |
| `RetryConfig`, `DefaultRetryConfig()`, `ExecuteWithRetry` | Exponential backoff with jitter for retryable errors |
| `IsRetryable(err)` | Uses `llm.ClassifyError` for `*llm.ProviderError`, otherwise rate-limit, timeout, and request error codes |
| `HTTPError`, `MapHTTPError`, `WithHTTPStatus`, `RestyResponseToHTTPError` | Map a non-2xx response to an `*errors.AgentError` with a `*llm.ProviderError` cause |
| `ProviderCapabilities`, `NewProviderCapabilities` | Embed to implement `HasCapability` / `Capabilities` |
| `ConvertMessages`, `ConvertMessagesWithRoleMapping`, `ToStandardMessage` | Convert `llm.Message` slices to provider message types |
| `RetryAfterDuration`, `ParseRetryAfter`, `GenerateCallID` | Retry-After parsing and tool call ID generation |

## Behavior Notes

- `GetMaxTokens` and `GetTimeout` return 0 when nothing is configured: no `max_tokens` is sent and no timeout is applied. `GetTemperature` and `GetTopP` fall back to `constants.DefaultTemperature` / `constants.DefaultTopP`; `ResolveTemperature` honors an explicit zero (`CompletionRequest.TemperatureSet`).
- `EnsureAPIKey` rejects a client-level reasoning config for providers other than OpenAI, DeepSeek, and Gemini.
- `RetryConfig.Retries` counts retries after the first attempt; 0 means no retry. `MaxAttempts` is deprecated; setting both is an error, as are negative retries or delays. `DefaultRetryConfig()` gives 3 attempts.
- `ExecuteWithRetry` returns non-retryable errors immediately, stops on context cancellation or deadline (returned as an `AgentError` that still matches `context.Canceled` / `context.DeadlineExceeded` via `errors.Is`), and wraps the last error with the attempt count. With `GO_TEST_MODE=true` the base delay becomes 10ms.
- `MapHTTPError` maps 400 to invalid input, 401/403 to invalid API key config, 404 to an LLM response error, 429 to a rate-limit error, and 5xx and other statuses to a request error. `ParseRetryAfter` falls back to 60 seconds; `RetryAfterDuration` returns 0 for missing or unparsable values.
- `NewHTTPClient` reuses clients with the same configuration from a global cache; config custom headers override `HTTPClientConfig.Headers`.

## See also

- [LLM Provider Guide](../../docs/guides/LLM_PROVIDERS.md)
- [Provider Capability Boundaries](../../docs/guides/PROVIDER_CAPABILITIES.md)
- [Constants Migration](../../docs/guides/CONSTANTS_MIGRATION.md)
- [llm](../README.md), [llm/providers](../providers/README.md)
