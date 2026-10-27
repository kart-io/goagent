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

## 本轮修复与兼容性

- Kimi、SiliconFlow、Ollama 现在实现 CapabilityChecker，仅声明已有 Complete/Chat。
- Gemini 的 Embed 已实现真实 API 请求，使用独立 `EmbeddingModel`（默认 `gemini-embedding-001`）；校验非空、非全零和有限向量。取消与上游错误正常传播，不再返回固定向量或不支持错误。
- DeepSeek 的旧 Embed 方法仍发送兼容 `/embeddings` 请求，但硬编码的兼容模型不构成已验证的官方服务能力，因此不再默认声明 Embedding。官方当前模型目录参考 [Lists Models](https://api-docs.deepseek.com/api/list-models/)，自定义端点需另行验证。
- `llm.HasEmbedding` / `AsEmbedder` 现在同时检查方法和显式能力声明；声明不支持时分别返回 false / nil。没有 CapabilityChecker 的自定义客户端保留接口检测行为。
- 旧 `common.ToolCallResponse` 兼容路径与标准 `llm.ToolCallResponse` 不混为同一类型；内置 OpenAI、DeepSeek、Gemini 的旧工具方法实际采用标准 llm 类型。

## 验证范围

```sh
go test -race -count=1 ./llm/providers -run 'TestProviderCapabilityContractMatrix|TestGeminiEmbeddingCannotReturnMockSuccess|TestNativeProviders'
```

能力矩阵覆盖 9 个基础 Provider 与 2 个包装器：声明与可执行接口一致、不支持的 Builder 入口在请求前拒绝、Gemini 在上游失败时不返回模拟向量。OpenAI/DeepSeek 另有本地 SSE 协议回归；真实模型验证已包括 DeepSeek、Gemini 官方兼容端点与[GeminiProvider 原生 API](../evaluation/2026-10-27-gemini-provider/REPORT.md)。GeminiProvider 保留旧公开方法，底层已改为 Google GenAI SDK，六个方法各实测两次全部通过，后续已补齐结构化流、原生工具循环、签名续接和真实 Embedding，新增[原生能力实测](../evaluation/2026-10-27-gemini-native-support/REPORT.md) 12/12 通过。此矩阵不证明其余供应商线上请求、认证、模型可用性或错误流处理已通过生产验收。

Gemini 原生验证可运行 `go run ./examples/llm/gemini-verify -live -suite native`（需 `GEMINI_API_KEY`）。默认模型为 `gemini-2.5-flash`；`GEMINI_BASE_URL` 是原生根地址，与 agentbench 的 `gemini-openai` 所需兼容路径不同。配置、结果与限制见[案例说明](../../examples/llm/gemini-verify/README.md)。

`CompleteStream` / `ChatStream` / `CompleteToolsStream` 返回统一的 `stop` / `tool_calls` 结束原因。`llm.Message.ProviderState` 和终态 `StreamChunk.ProviderState` 用于保留原生签名；Builder 自动续接，不将其作为答案文本展示。一次响应多个工具时按 ID 匹配并在同一 user 内容中回传结果；工具实际并发执行策略由 Builder 决定。
