# GoAgent 工具示例

内置工具与自定义工具的使用示例。除 08 外均不调用 LLM；02 需要访问 `https://httpbin.org`。

| 示例 | 内容 | 使用的包 |
| --- | --- | --- |
| [01-calculator](01-calculator/) | `CalculatorTool` 表达式求值，`AdvancedCalculatorTool` 按操作名计算，错误输入处理 | `tools/compute` |
| [02-http-api](02-http-api/) | `APITool` 的 GET/POST/PUT/DELETE、Builder、Bearer 认证、404 与超时 | `tools/http` |
| [03-shell](03-shell/) | `ShellTool` 命令白名单、工作目录、超时、`CommonShellTools` | `tools/shell` |
| [04-search](04-search/) | `SearchTool` 搭配模拟引擎、Google/DuckDuckGo 引擎、聚合引擎 | `tools/search` |
| [07-function-tool](07-function-tool/) | `NewFunctionTool`、`FunctionToolBuilder`、`NewBaseTool` 创建自定义工具 | `tools` |
| [08-tool-with-agent](08-tool-with-agent/) | 把工具交给 `builder.NewSimpleBuilder` 构建的 Agent 自动调用 | `builder`、`tools` |

从仓库根目录运行：

```bash
go run ./examples/tools/01-calculator
```

编码修复与计划模式示例在 `github.com/kart-io/goagent-harness`，见 [Harness 迁移指南](../../docs/guides/HARNESS_MIGRATION.md)。

## 自定义工具的三种方式

```go
// 1. NewFunctionTool：名称、描述、JSON Schema、函数
t1 := tools.NewFunctionTool("my_tool", "工具描述",
    `{"type":"object","properties":{"param":{"type":"string"}}}`,
    func(ctx context.Context, args map[string]interface{}) (interface{}, error) {
        return args["param"], nil
    })

// 2. FunctionToolBuilder：链式构建
t2 := tools.NewFunctionToolBuilder("my_tool").
    WithDescription("工具描述").
    WithArgsSchema(`{"type":"object"}`).
    WithFunction(fn).
    MustBuild()

// 3. NewBaseTool：直接处理 *interfaces.ToolInput / *interfaces.ToolOutput
t3 := tools.NewBaseTool("my_tool", "工具描述", `{"type":"object"}`,
    func(ctx context.Context, in *interfaces.ToolInput) (*interfaces.ToolOutput, error) {
        return &interfaces.ToolOutput{Result: "ok", Success: true}, nil
    })
```

## 相关文档

- [工具系统 API 参考](../../docs/api/TOOL_API.md)
- [工具中间件指南](../../docs/guides/TOOL_MIDDLEWARE.md)
