# Provider 能力边界与回归

本表描述仓库当前适配器实现，不代表同名供应商所有模型的服务端能力。`Stream` 字符串流不能代替携带结束状态和用量的 `llm.StreamClient`；`AsRunnable` 要求后者。

| Provider | Complete / Chat | 字符串流 | 结构化流 | 原生工具流 | 旧工具接口 | 默认声明 Embedding |
|---|---|---|---|---|---|---|
| OpenAI | 有 | 有 | 有 | 有 | 有 | 有，仍须选用可用端点/模型 |
| DeepSeek | 有 | 有 | 有 | 有 | 有 | 无；旧兼容 HTTP 方法保留 |
| Gemini | 有 | 有 | 有 | 有 | 有 | 有，独立 Embedding 模型 |
| Anthropic | 有 | 有 | 无 | 无 | 无 | 无 |
| Cohere | 有 | 有 | 无 | 无 | 无 | 无 |
| HuggingFace | 有 | 有 | 无 | 无 | 无 | 无 |
| Kimi | 有 | 无 | 无 | 无 | 无 | 无 |
| SiliconFlow | 有 | 无 | 无 | 无 | 无 | 无 |
| Ollama | 有 | 无 | 无 | 无 | 无 | 无 |

DeepSeekStreamingProvider 与 GeminiStreamingProvider 包装器继承对应基础接口；二者当前均支持结构化流和原生工具流。尚未实现的能力会在 Builder 原生入口或构建阶段拒绝，不通过模拟 Token 或占位结果冒充。

## 实现说明

- 所有内置 Provider 都实现 `CapabilityChecker`；Kimi、SiliconFlow、Ollama 只声明 Complete/Chat。
- Gemini 的 `Embed` 发送真实 API 请求，使用独立的 `EmbeddingModel`（默认 `gemini-embedding-001`），校验向量非空、非全零且有限；取消与上游错误原样返回。
- DeepSeek 的 `Embed` 方法向 `/embeddings` 发送兼容请求，模型名硬编码，不构成已验证的官方能力，因此不声明 Embedding，`HasEmbedding` 返回 false。官方模型目录见 [Lists Models](https://api-docs.deepseek.com/api/list-models/)，自定义端点需另行验证。
- `llm.HasEmbedding` / `AsEmbedder` 同时检查方法与能力声明；没有实现 `CapabilityChecker` 的自定义客户端只按接口判断。
- OpenAI、DeepSeek、Gemini 的旧工具方法（`GenerateWithTools` / `StreamWithTools`）使用 `llm.ToolCallResponse` / `llm.ToolChunk`，与 `llm/common` 中同名的类型不是同一类型。

## 验证范围

```sh
go test -race -count=1 ./llm/providers -run 'TestProviderCapabilityContractMatrix|TestGeminiEmbeddingCannotReturnMockSuccess|TestNativeProviders'
```

能力矩阵覆盖 9 个基础 Provider 与 2 个包装器：声明与可执行接口一致、不支持的 Builder 入口在请求前拒绝、Gemini 在上游失败时不返回模拟向量。OpenAI、DeepSeek 另有本地 SSE 协议回归，Gemini 有本地 HTTP 协议回归（`gemini_protocol_test.go`）。真实模型验证包括 DeepSeek、Gemini 官方兼容端点、[GeminiProvider 旧公开方法](../evaluation/2026-10-27-gemini-provider/REPORT.md)（六个方法各两次全部通过）和[原生结构化流、工具循环与 Embedding](../evaluation/2026-10-27-gemini-native-support/REPORT.md)（12/12 通过）。这些结果不证明其余供应商的线上请求、认证、模型可用性或错误流处理已通过生产验收。

Gemini 原生验证可运行 `go run ./examples/llm/gemini-verify -live -suite native`（需 `GEMINI_API_KEY`）。默认模型为 `gemini-2.5-flash`；`GEMINI_BASE_URL` 是原生根地址，与 agentbench 的 `gemini-openai` 所需兼容路径不同。配置、结果与限制见[案例说明](../../examples/llm/gemini-verify/README.md)。

`CompleteStream` / `ChatStream` / `CompleteToolsStream` 返回统一的 `stop` / `tool_calls` 结束原因。`llm.Message.ProviderState` 和终态 `StreamChunk.ProviderState` 用于保留原生签名；Builder 自动续接，不将其作为答案文本展示。一次响应多个工具时按 ID 匹配并在同一 user 内容中回传结果；工具实际并发执行策略由 Builder 决定。
