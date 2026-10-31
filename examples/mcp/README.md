# MCP 工具箱示例

演示 `mcp/toolbox`（`StandardToolBox`）、`mcp/tools`（内置工具）与 `mcp/core`（`MCPTool` 接口、`BaseTool`、`ToolSchema`）的用法。均不调用 LLM。

| 示例 | 内容 | 网络 |
| --- | --- | --- |
| [basic_tools](basic_tools/) | 注册内置工具；列出、按分类列出、搜索工具；读取元数据；执行 `read_file`、`http_request`、`json_parse`；参数校验；统计与调用历史 | 请求 `https://httpbin.org/get` |
| [tool_chain](tool_chain/) | 用示例内的 `ToolChainExecutor` 保存中间结果：读取并解析 JSON 配置、`ExecuteBatch` 并行请求、按配置条件写文件、读不存在文件时重试 3 次、HTTP 结果转换 | 请求 `https://httpbin.org` |
| [custom_tool](custom_tool/) | 自定义 `text_analyzer`、`calculator`、`streaming_counter` 三个工具，含 `Validate` 与流式结果 | 否 |

```bash
go run ./examples/mcp/basic_tools
go run ./examples/mcp/tool_chain
go run ./examples/mcp/custom_tool
```

## 内置工具

`tools.RegisterBuiltinTools(tb)` 注册：`read_file`、`write_file`、`list_directory`、`search_files`、`http_request`、`json_parse`、`shell_execute`。

```go
tb := toolbox.NewStandardToolBox()
if err := tools.RegisterBuiltinTools(tb); err != nil {
    log.Fatal(err)
}
result, err := tb.Execute(ctx, &core.ToolCall{
    ID:       "call-1",
    ToolName: "read_file",
    Input:    map[string]interface{}{"path": "/tmp/test.txt"},
})
```

## 自定义工具

嵌入 `*core.BaseTool`（提供名称、描述、分类、Schema），再实现 `Execute` 与 `Validate`：

```go
type TextAnalyzerTool struct {
    *core.BaseTool
}

func NewTextAnalyzerTool() *TextAnalyzerTool {
    schema := &core.ToolSchema{
        Type: "object",
        Properties: map[string]core.PropertySchema{
            "text": {Type: "string", Description: "要分析的文本"},
        },
        Required: []string{"text"},
    }
    return &TextAnalyzerTool{
        BaseTool: core.NewBaseTool("text_analyzer", "分析文本的统计信息", "text", schema),
    }
}

func (t *TextAnalyzerTool) Execute(ctx context.Context, input map[string]interface{}) (*core.ToolResult, error) {
    return &core.ToolResult{Success: true, Data: map[string]interface{}{"result": "..."}}, nil
}

func (t *TextAnalyzerTool) Validate(input map[string]interface{}) error { return nil }
```

流式工具在 `ToolResult` 中设置 `IsStreaming: true` 与 `StreamChannel`，调用方遍历该通道读取结果（见 `custom_tool` 的 `StreamingCounterTool`）。

接口完整定义见 [MCP 模块文档](../../mcp/README.md) 与 [mcp/core/tool.go](../../mcp/core/tool.go)。
