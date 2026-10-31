# DeepSeek 简单示例

用 DeepSeek 演示两种最基本的用法：直接调用 Provider 对话，以及用 `builder.NewAgentBuilder` 构建 Agent。

## 运行

```bash
export DEEPSEEK_API_KEY=your-deepseek-api-key   # 未设置时直接退出
go run ./examples/basic/09-deepseek-simple
```

会发起 4 次真实的 DeepSeek 请求（模型 `deepseek-chat`）。

## 内容

### 示例 1：Provider 直接对话

```go
client, err := providers.NewDeepSeekWithOptions(
    llm.WithAPIKey(apiKey),
    llm.WithModel("deepseek-chat"),
    llm.WithTemperature(0.7),
    llm.WithMaxTokens(1000),
    llm.WithTimeout(30*time.Second),
)
response, err := client.Chat(ctx, []llm.Message{
    llm.SystemMessage("你是一个友好的 AI 助手，擅长用简洁的语言回答问题。"),
    llm.UserMessage("请用一句话解释什么是 AI Agent。"),
})
```

打印回复内容、`response.Usage` 中的 Token 统计与模型名。

### 示例 2：Builder 构建 Agent

```go
agent, err := builder.NewAgentBuilder[any, *agentcore.AgentState](client).
    WithSystemPrompt("你是一个专业的 Go 语言顾问，擅长解答 Go 编程相关的问题。").
    WithMetadata("name", "Go-Advisor").
    Build()
output, err := agent.Execute(ctx, "请列举 Go 语言的三个主要特点。")
```

打印 `output.Result` 与 `output.Duration`。

### 示例 3：结构化输出

以 `Temperature(0.3)` 和要求只输出 JSON 的系统提示构建 Agent，分别生成用户数组和产品对象。输出是模型返回的原始文本，示例不做 JSON 解析或校验。

## 相关示例

- [invokefast](invokefast/)：多个 DeepSeek Agent 串行调用并计时
- [12-deepseek-simple](../12-deepseek-simple/)：带工具的 DeepSeek Agent
