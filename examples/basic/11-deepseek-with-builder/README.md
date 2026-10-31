# DeepSeek + AgentBuilder 示例

用 `builder.NewAgentBuilder[any, core.State]` 与 DeepSeek（`deepseek-chat`）构建 Agent，演示工具、回调、常用配置和预设。

## 运行

```bash
export DEEPSEEK_API_KEY=your-api-key
go run ./examples/basic/11-deepseek-with-builder
```

未设置 `DEEPSEEK_API_KEY` 时进入模拟模式：只打印功能说明和一段示例代码，不发起请求。

## 内容

| 示例 | 函数 | 要点 |
| --- | --- | --- |
| 1 基础 | `runBasicAgentBuilder` | `WithSystemPrompt`、`WithState(agentstate.NewAgentState())`，`Execute` |
| 2 工具 | `runAgentBuilderWithTools` | `WithTools(calculator, weather, time)`、`WithMaxIterations(10)`、`WithTimeout(30s)`、`WithVerbose(true)`，`ExecuteWithTools("请帮我计算 15 * 8，然后告诉我现在的时间")` |
| 3 回调 | `runAgentBuilderWithMiddleware` | `core.NewCostTrackingCallback(pricing)` 经 `WithCallbacks` 接入，结束后打印 `GetTotalTokens()` 与 `GetTotalCost()` |
| 4 配置 | `runAgentBuilderWithConfig` | 用 `builder.AgentConfig` 的字段调用 `WithMaxIterations`、`WithTimeout`、`WithStreamingEnabled`、`WithAutoSaveEnabled`、`WithMaxTokens`、`WithTemperature`、`WithMetadata` 等 |
| 5 预设 | `runChatbotAgent` | `ConfigureForChatbot()`（限流、输入长度校验、开启流式、温度 0.8）后进行三轮对话 |

工具均由 `tools.NewFunctionToolBuilder` 创建：`calculator` 解析以空格分隔的 `a op b` 四则运算，`get_weather` 返回模拟数据，`get_current_time` 返回当前时间。

```go
agent, err := builder.NewAgentBuilder[any, core.State](llmClient).
    WithSystemPrompt("你是一个智能助手，可以使用工具来帮助用户完成任务。").
    WithTools(calculatorTool, weatherTool, timeTool).
    WithState(agentstate.NewAgentState()).
    WithMaxIterations(10).
    WithTimeout(30 * time.Second).
    Build()
output, err := agent.ExecuteWithTools(ctx, input)
```

其余预设 `ConfigureForRAG()`、`ConfigureForAnalysis()` 见 [builder/presets.go](../../../builder/presets.go)。
