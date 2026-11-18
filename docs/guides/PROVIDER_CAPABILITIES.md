# Provider Capability Boundaries and Regression

[English](PROVIDER_CAPABILITIES.md) | [中文](PROVIDER_CAPABILITIES_CN.md)

This table describes the repository's current adapter implementations, not the server-side capabilities of every model from a vendor of the same name. Every Provider implements `llm.Client.CompleteStream` (carrying finish status and usage); when you need a complete response, collect it with `llm.Complete` / `llm.Chat`.

| Provider | Native tool streaming | Embedding declared by default |
|---|---|---|
| OpenAI | Yes | Yes; you still need to choose an available endpoint/model |
| DeepSeek | Yes | No; the old compatible HTTP method is kept |
| Gemini | Yes | Yes, with a separate embedding model |
| Anthropic | Yes | No |
| Kimi | Yes | No |
| SiliconFlow | Yes | No |
| Ollama | Yes | No |

The DeepSeekStreamingProvider and GeminiStreamingProvider wrappers inherit the corresponding base interfaces; both currently support structured streaming and native tool streaming. Capabilities not yet implemented are rejected at the Builder's native entry points or at build time, rather than faked with simulated tokens or placeholder results.

## Implementation Notes

- All built-in Providers implement `CapabilityChecker`. Kimi, SiliconFlow, and Ollama are presets of `OpenAIProvider`, and `Provider()` returns their own identity; they do not declare Embedding (`Embed` always uses the OpenAI embedding model), vision, or JSON mode, and do not accept explicit reasoning configuration. Whether tool calling and streaming work depends on the chosen model.
- Gemini's `Embed` sends real API requests using a separate `EmbeddingModel` (default `gemini-embedding-001`) and validates that vectors are non-empty, not all zeros, and finite; cancellation and upstream errors are returned as-is.
- DeepSeek's `Embed` method sends a compatible request to `/embeddings` with a hard-coded model name. This is not a verified official capability, so Embedding is not declared and `HasEmbedding` returns false. See [Lists Models](https://api-docs.deepseek.com/api/list-models/) for the official model catalog; custom endpoints need separate verification.
- `llm.HasEmbedding` / `AsEmbedder` check both the method and the capability declaration; custom clients that do not implement `CapabilityChecker` are judged by interface only.

## Verification Scope

```sh
go test -race -count=1 ./llm/providers -run 'TestProviderCapabilityContractMatrix|TestGeminiEmbeddingCannotReturnMockSuccess|TestNativeProviders'
```

The capability matrix covers 7 base Providers and 2 wrappers: declarations match the executable interfaces, unsupported Builder entry points are rejected before any request, and Gemini returns no simulated vectors on upstream failure. OpenAI and DeepSeek additionally have local SSE protocol regressions, and Gemini has a local HTTP protocol regression (`gemini_protocol_test.go`). Real-model verification includes DeepSeek, Gemini's official compatible endpoint, the [old public GeminiProvider methods](../evaluation/2026-10-27-gemini-provider/REPORT.md) (all six methods passed twice each), and [native structured streaming, tool loop, and Embedding](../evaluation/2026-10-27-gemini-native-support/REPORT.md) (12/12 passed). These results do not prove that other vendors' live requests, authentication, model availability, or error-stream handling have passed production acceptance.

For Gemini native verification, run `go run ./examples/llm/gemini-verify -live -suite native` (requires `GEMINI_API_KEY`). The default model is `gemini-2.5-flash`; `GEMINI_BASE_URL` is the native root URL, which differs from the compatible path required by agentbench's `gemini-openai`. See the [case description](../../examples/llm/gemini-verify/README.md) for configuration, results, and limitations.

`CompleteStream` / `CompleteToolsStream` return unified `stop` / `tool_calls` finish reasons. `llm.Message.ProviderState` and the terminal `StreamChunk.ProviderState` preserve native signatures; the Builder carries them forward automatically and does not display them as answer text. When one response contains multiple tools, results are matched by ID and returned within the same user content; the actual concurrent execution policy for tools is decided by the Builder.
