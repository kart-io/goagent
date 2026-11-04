# MCP 工具

`mcp/remote` 基于官方 SDK（`github.com/modelcontextprotocol/go-sdk/mcp`）连接远端 MCP 服务器，把其工具适配为 `interfaces.Tool`，可直接交给 builder。

原进程内工具箱（`mcp/core`、`mcp/toolbox`、`mcp/tools`）不经 MCP 协议，也无法交给 Agent，已删除；迁移见 [CHANGELOG](../CHANGELOG.md)。

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
