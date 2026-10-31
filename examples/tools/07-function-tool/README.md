# 07-function-tool 自定义函数工具示例

演示 `tools` 包中创建自定义工具的三种方式。不调用 LLM。

## 运行

```bash
go run ./examples/tools/07-function-tool
```

## 内容

| 步骤 | 工具名 | 创建方式 | 说明 |
| --- | --- | --- | --- |
| 1 | `random_number` | `tools.NewFunctionTool` | 在 `min`～`max` 之间生成随机整数 |
| 2 | `string_processor` | `tools.NewFunctionToolBuilder` | `uppercase`、`lowercase`、`reverse`、`length`、`word_count` |
| 3 | `counter` | `tools.NewFunctionToolBuilder` | 闭包保存状态，支持 `increment`、`decrement`、`reset`、`get` |
| 4 | `weather` | `tools.NewFunctionToolBuilder` | 按城市返回内置模拟天气 |
| 5 | `current_time` | `tools.NewBaseTool` | 直接读写 `ToolInput`/`ToolOutput`，格式 `rfc3339`、`date`、`time`、`unix`、`human` |

```go
// FunctionTool：函数接收参数 map，返回值写入 ToolOutput.Result
stringTool := tools.NewFunctionToolBuilder("string_processor").
    WithDescription("处理字符串，支持多种操作").
    WithArgsSchema(`{"type":"object","properties":{"text":{"type":"string"},"operation":{"type":"string"}},"required":["text","operation"]}`).
    WithFunction(func(ctx context.Context, args map[string]interface{}) (interface{}, error) {
        // ...
    }).
    MustBuild()

// BaseTool：自己构造 ToolOutput
timeTool := tools.NewBaseTool("current_time", "获取当前时间，支持多种格式", schema,
    func(ctx context.Context, input *interfaces.ToolInput) (*interfaces.ToolOutput, error) {
        return &interfaces.ToolOutput{Result: time.Now().Format(time.RFC3339), Success: true}, nil
    })
```

`FunctionToolBuilder` 还提供 `WithArgsSchemaFromStruct` 从结构体生成参数 Schema，以及返回错误而非 panic 的 `Build()`。
