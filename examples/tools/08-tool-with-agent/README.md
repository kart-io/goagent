# 08-tool-with-agent 工具与 Agent 集成示例

把内置计算器和三个自定义工具交给 Agent，由模型按问题选择工具。

## 运行

```bash
export OPENAI_API_KEY=your-api-key   # 不设置时进入演示模式
go run ./examples/tools/08-tool-with-agent
```

- **设置了 `OPENAI_API_KEY`**：用 `providers.NewOpenAIWithOptions` 创建模型 `gpt-4` 的客户端，依次执行四个问题（计算、天气、时间、翻译），打印回答以及 `output.Metadata["tool_calls"]`（若有）。
- **未设置**：不调用 LLM，直接 `Invoke` 每个工具并打印结果。

## 工具

| 工具名 | 来源 | 说明 |
| --- | --- | --- |
| `calculator` | `compute.NewCalculatorTool()` | 表达式求值 |
| `weather` | `tools.NewFunctionToolBuilder` | 随机生成的模拟天气 |
| `current_time` | `tools.NewFunctionToolBuilder` | 当前时间，可选输出格式 |
| `translate` | `tools.NewFunctionToolBuilder` | 内置词表的模拟翻译 |

## 关键代码

```go
agent, err := builder.NewSimpleBuilder(client).
    WithSystemPrompt(`你是一个智能助手，可以使用以下工具来帮助用户：...`).
    WithTools(toolList...).
    WithMaxIterations(5).
    WithTimeout(60 * time.Second).
    WithVerbose(true).
    Build()

output, err := agent.ExecuteWithTools(ctx, "计算 (25 + 17) * 3 的结果")
```
