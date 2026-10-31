# Basic Examples

简单、专注的示例，每个演示一个功能或概念。从仓库根目录运行，例如 `go run ./examples/basic/01-simple-agent`。

| 示例 | 内容 | 需要 |
| --- | --- | --- |
| 01-simple-agent | 自定义 Agent 实现 `Invoke`、`Stream`、`InvokeFast`，以及回调 | 无 |
| 03-agent-with-memory | 带 `MemoryManager` 的分析 Agent、数据处理链与简单编排器 | 无 |
| 04-ollama-agent | Ollama 本地模型：对话、Agent、补全、模型列表、工具调用 | 本地 Ollama |
| 05-provider-consistency | 不同 Provider 通过同一 `llm.Client` 接口调用；`LLM_PROVIDER` 选择 Provider | `OPENAI_API_KEY` / `DEEPSEEK_API_KEY` / `GEMINI_API_KEY` 或 Ollama |
| [06-all-providers](06-all-providers/) | 逐个测试 OpenAI、Gemini、DeepSeek、Ollama、SiliconFlow、Kimi | 各 Provider 的 API Key |
| [07-smart-agent-with-tools](07-smart-agent-with-tools/) | 时间、HTTP API、天气三个工具的创建与直接调用 | 网络 |
| [09-deepseek-simple](09-deepseek-simple/) | DeepSeek Provider 对话与 `NewAgentBuilder`；子目录 [invokefast](09-deepseek-simple/invokefast/) 串行调用多个 Agent | `DEEPSEEK_API_KEY` |
| [11-deepseek-with-builder](11-deepseek-with-builder/) | `NewAgentBuilder` 的工具、回调、配置与 `ConfigureForChatbot` 预设 | `DEEPSEEK_API_KEY`（未设置时为模拟模式） |
| [12-deepseek-simple](12-deepseek-simple/) | 用示例内辅助函数封装 Builder 的常用组合 | `DEEPSEEK_API_KEY` |
| 13-simple-builder-deepseek | `builder.NewSimpleBuilder` 构建 DeepSeek Agent | `DEEPSEEK_API_KEY` |
| 15-simple-builder-kimi | `builder.NewSimpleBuilder` 构建 Kimi Agent | `KIMI_API_KEY` 或 `MOONSHOT_API_KEY` |
