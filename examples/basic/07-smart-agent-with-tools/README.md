# 智能 Agent 示例 - 时间获取与 API 调用

演示三个工具的创建与直接调用，不调用 LLM：

| 工具 | 创建方式 | 说明 |
| --- | --- | --- |
| `get_current_time` | `tools.NewFunctionToolBuilder` | 按 `timezone`、`format` 返回时间、时间戳、星期等字段；无效时区回退 UTC |
| `api` | `http.NewAPIToolBuilder()`（30 秒超时） | 通用 HTTP 工具，参数 `method`、`url`、`body`、`headers` |
| `get_weather` | `tools.NewFunctionToolBuilder` | 按 `city` 返回固定的模拟天气数据 |

## 运行

```bash
go run ./examples/basic/07-smart-agent-with-tools
```

示例 2 会向 `https://jsonplaceholder.typicode.com` 发起真实的 GET/POST 请求，需要网络。

## 内容

1. **时间工具**：以 `Asia/Shanghai` 调用 `get_current_time`，打印时间、时区与 Unix 时间戳。
2. **API 工具**：GET `/users/1`、GET `/posts?_limit=3`、POST `/posts`，打印响应 `body` 中的字段。
3. **工具组合**：打印三个工具的名称与描述，依次调用时间工具与天气工具；最后打印一段把这些工具接入 Agent 的示例代码（只打印，不执行）：

```go
agent, err := builder.NewSimpleBuilder(llmClient).
    WithSystemPrompt("你是智能助手，可以获取时间和查询天气").
    WithTools(createTimeTool(), createWeatherAPITool(), createAPITool()).
    Build()
output, err := agent.ExecuteWithTools(ctx, "现在几点了？北京的天气怎么样？")
```

工具调用方式：

```go
output, err := timeTool.Invoke(ctx, &interfaces.ToolInput{
    Args:    map[string]interface{}{"timezone": "Asia/Shanghai"},
    Context: ctx,
})
```

需要真正由 LLM 选择工具的示例见 [tools/08-tool-with-agent](../../tools/08-tool-with-agent/)。
