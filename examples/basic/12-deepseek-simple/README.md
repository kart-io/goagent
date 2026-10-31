# DeepSeek Agent 简化示例

在示例内部定义几个辅助函数，把 `builder.NewAgentBuilder` 的常用组合封装成一行调用。辅助函数只存在于本示例的 `main.go`，不是框架 API。

## 运行

```bash
export DEEPSEEK_API_KEY=your-api-key   # 未设置时打印提示并退出
go run ./examples/basic/12-deepseek-simple
```

会发起真实的 DeepSeek 请求（模型 `deepseek-chat`）。

## 辅助函数

| 函数 | 作用 |
| --- | --- |
| `mustCreateLLM` | `providers.NewDeepSeekWithOptions`，温度 0.7、最大 2000 tokens、超时 30 秒 |
| `quickAgent` | 系统提示 + `WithState(agentstate.NewAgentState())` |
| `quickAgentWithTools` | 在 `quickAgent` 基础上加 `WithTools` 与 `WithVerbose(true)` |
| `chatbot` | 使用 `ConfigureForChatbot()` 预设 |
| `run` / `runWithTools` | 分别调用 `Execute` / `ExecuteWithTools`，返回 `output.Result`，出错时 panic |
| `simpleTool` | 用 `tools.NewFunctionToolBuilder` 创建只有一个 `input` 字符串参数的工具 |

## 内容

1. **最简对话**：`run(quickAgent(apiKey, "你是一个友好的 AI 助手"), "用一句话介绍 Go 语言")`。
2. **工具**：`calculator`（固定返回 `"120"`）与 `get_weather`（返回固定天气文本），以 `runWithTools` 执行"计算 15 * 8 的结果"。
3. **聊天机器人**：三轮对话。

```go
calculator := simpleTool("calculator", "计算数学表达式，如 '15 * 8'",
    func(ctx context.Context, input string) (string, error) { return "120", nil })
agent := quickAgentWithTools(apiKey, "你是智能助手，可以使用工具帮助用户", calculator, weather)
output := runWithTools(agent, "计算 15 * 8 的结果")
```
