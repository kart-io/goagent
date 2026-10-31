# All Providers Example - 所有 Provider 测试示例

依次用 `llm/providers` 的构造函数创建 6 个 Provider 客户端，各发一条 `Chat` 请求并打印回复、Provider 与模型名。未设置对应 API Key 的 Provider 会被跳过；Ollama 需本地运行（`ollama serve`，模型 `llama2`）。

## 运行示例

```bash
export OPENAI_API_KEY="..."       # gpt-3.5-turbo
export GEMINI_API_KEY="..."       # gemini-pro
export DEEPSEEK_API_KEY="..."     # deepseek-chat
export SILICONFLOW_API_KEY="..."  # Qwen/Qwen2-7B-Instruct
export KIMI_API_KEY="..."         # moonshot-v1-8k

go run ./examples/basic/06-all-providers
```

## 代码说明

```go
client, err := providers.NewOpenAIWithOptions(
	llm.WithProvider(constants.ProviderOpenAI),
	llm.WithAPIKey(apiKey),
	llm.WithModel("gpt-3.5-turbo"),
	llm.WithMaxTokens(100),
	llm.WithTemperature(0.7),
)
```

其余 Provider 分别使用 `NewGeminiWithOptions`、`NewDeepSeekWithOptions`、`NewSiliconFlowWithOptions`、`NewKimiWithOptions` 与 `NewOllamaClientSimple`。所有客户端都实现 `llm.Client`，测试函数 `testProvider` 先调用 `IsAvailable()`，再以 10 秒超时调用 `Chat`。
