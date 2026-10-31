# MCP 工具

包含两部分：

- `mcp/remote`：基于官方 SDK（`github.com/modelcontextprotocol/go-sdk/mcp`）连接远端 MCP 服务器，把其工具适配为 `interfaces.Tool`，可直接交给 builder。
- `mcp/core`、`mcp/toolbox`、`mcp/tools`：进程内的工具箱，管理带分类与 Schema 的 `MCPTool`，提供注册、搜索、权限、调用历史和统计。

## 目录结构

```text
mcp/
├── remote/     # 远端 MCP 服务器工具适配
├── core/       # MCPTool、ToolBox 等接口与数据类型，BaseTool
├── toolbox/    # StandardToolBox、MemoryRegistry、StandardExecutor、PermissionManager
└── tools/      # 内置工具
```

## 远端 MCP 服务器

```go
session, err := remote.Connect(ctx, &mcp.CommandTransport{Command: exec.Command("my-mcp-server")})
if err != nil {
    return err
}
defer session.Close()

mcpTools, err := remote.Tools(ctx, session,
    remote.WithToolName(func(name string) (string, bool) { return "remote_" + name, true }),
    remote.WithToolErrorsAsRejected(),
)
if err != nil {
    return err
}

agent, err := builder.NewSimpleBuilder(client).WithTools(mcpTools...).Build()
```

- `Connect` 通过任意 SDK `mcp.Transport`（如 `StreamableClientTransport`、`CommandTransport`）协商会话，调用方负责 `Close`；会话须在工具使用期间保持打开。认证与传输限制在 SDK transport 上配置。
- `Tools` 分页列出全部远端工具。工具结果以完整的 `*mcp.CallToolResult` 作为 `ToolOutput.Result` 返回。
- 远端返回错误结果（`IsError`）时默认作为 Go error 中止运行；`WithToolErrorsAsRejected()` 改为以 `tools.Rejected` 交给模型。连接与协议错误始终是 Go error。
- `WithToolName` 决定暴露给模型的名称，返回 false 时跳过该工具。
- `WithDurablePolicy(p)` 为工具声明持久运行中的操作策略（`tools.DurablePolicyProvider`），如 `MaxAttempts: 1` 使中断的调用在恢复时不被重跑。
- 需要客户端补充输入的结果（`NeedsInput`）返回错误，需直接通过 SDK 会话处理。

## 进程内工具箱

### 核心接口

```go
type MCPTool interface {
    Name() string
    Description() string
    Category() string
    Schema() *ToolSchema
    Execute(ctx context.Context, input map[string]interface{}) (*ToolResult, error)
    Validate(input map[string]interface{}) error
    RequiresAuth() bool
    IsDangerous() bool
}
```

`MCPTool` 与 `interfaces.Tool` 不同，不能直接交给 builder。`ToolBox` 接口提供 `Register`、`Unregister`、`Get`、`List`、`ListByCategory`、`Search`、`Execute`、`ExecuteBatch`、`GetMetadata`、`ListMetadata`、`Validate`、`HasPermission`、`Statistics`；`toolbox.NewStandardToolBox()` 是其实现，另有 `GetCallHistory`、`ClearHistory`、`SetPermissionManager`、`SetMaxHistorySize`（默认 100 条）。

### 内置工具

| 工具名称 | 分类 | 说明 | `IsDangerous` / `RequiresAuth` |
|---|---|---|---|
| `read_file` | filesystem | 读取文件内容 | 否 / 否 |
| `write_file` | filesystem | 写入文件内容 | 是 / 是 |
| `list_directory` | filesystem | 列出目录内容 | 否 / 否 |
| `search_files` | filesystem | 搜索文件 | 否 / 否 |
| `http_request` | network | 发送 HTTP 请求 | 否 / 否 |
| `json_parse` | data | 解析 JSON 数据 | 否 / 否 |
| `shell_execute` | system | 执行 Shell 命令 | 是 / 是 |

`tools.RegisterBuiltinTools(tb)` 注册 `tools.BuiltinTools` 中的全部实例，`tools.GetToolsByCategory(category)` 按分类筛选。

### 注册与执行

```go
tb := toolbox.NewStandardToolBox()
if err := tools.RegisterBuiltinTools(tb); err != nil {
    return err
}

result, err := tb.Execute(ctx, &core.ToolCall{
    ToolName: "read_file",
    Input:    map[string]interface{}{"path": "/tmp/test.txt"},
})
if err != nil {
    return err
}
if result.Result.Success {
    fmt.Println(result.Result.Data)
}
```

`Execute` 依次校验调用、在 `UserID` 非空时检查权限、由 `StandardExecutor` 先调用工具的 `Validate` 再 `Execute`、更新统计与调用历史；`ID` 为空时自动生成。

### 权限与速率限制

```go
pm := toolbox.NewPermissionManager()
tb.SetPermissionManager(pm)

pm.SetPermission(&core.ToolPermission{
    UserID:            "user-1",
    ToolName:          "http_request",
    Allowed:           true,
    MaxCallsPerMinute: 60,
})
pm.SetPermission(&core.ToolPermission{
    UserID:   "user-1",
    ToolName: "shell_execute",
    Allowed:  false,
    Reason:   "需要管理员权限",
})

allowed, err := tb.HasPermission("user-1", "shell_execute") // false，err 为 *core.ErrPermissionDenied
```

未设置权限的用户与工具按默认策略处理（默认允许，可用 `SetDefaultPolicy` 修改）。另有 `GrantAll`、`DenyAll`、`RevokePermission`、`ListUserPermissions`、`GetRateLimitStatus`。权限与限流状态只保存在内存中。

`IsDangerous`、`RequiresAuth` 与 `ToolPermission.AllowDangerousOps` 只作为元数据记录（出现在 `ToolMetadata` 中），工具箱执行时不据此拦截调用。

### 自定义工具

嵌入 `core.BaseTool` 并实现 `Execute` 与 `Validate`（`BaseTool` 的这两个方法返回 `core.ErrNotImplemented`）：

```go
type EchoTool struct {
    *core.BaseTool
}

func NewEchoTool() *EchoTool {
    return &EchoTool{BaseTool: core.NewBaseTool("echo", "回显输入", "custom", &core.ToolSchema{
        Type: "object",
        Properties: map[string]core.PropertySchema{
            "text": {Type: "string", Description: "要回显的文本"},
        },
        Required: []string{"text"},
    })}
}

func (t *EchoTool) Validate(input map[string]interface{}) error {
    if s, ok := input["text"].(string); !ok || s == "" {
        return &core.ErrInvalidInput{Field: "text", Message: "必须是非空字符串"}
    }
    return nil
}

func (t *EchoTool) Execute(ctx context.Context, input map[string]interface{}) (*core.ToolResult, error) {
    return &core.ToolResult{
        Success:   true,
        Data:      map[string]interface{}{"text": input["text"]},
        Timestamp: time.Now(),
    }, nil
}
```

`PropertySchema` 支持 `Type`、`Description`、`Default`、`Enum`、`Format`、`Pattern`、`Minimum`/`Maximum`、`MinLength`/`MaxLength` 与数组的 `Items`。`BaseTool` 的 `SetRequiresAuth`、`SetIsDangerous` 设置对应元数据。

### 统计

```go
stats := tb.Statistics()
fmt.Println(stats.TotalTools, stats.TotalCalls, stats.SuccessfulCalls, stats.FailedCalls)
fmt.Printf("平均延迟: %.2f ms\n", stats.AverageLatency)
for name, count := range stats.ToolUsage {
    fmt.Println(name, count)
}
```

## 示例

见 [examples/mcp](../examples/mcp/README.md)：`basic_tools`（注册、执行、统计与历史）、`tool_chain`（多工具顺序调用）、`custom_tool`（自定义工具）。
