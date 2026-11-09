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
- `Connect` 默认声明 form elicitation 能力，并由适配层显式处理新协议多轮输入；`ConnectWithOptions` 可注入旧协议的 `ElicitationHandler` 或其他官方 SDK 选项。

## 人工输入与审批续接

没有配置输入处理器时，`NeedsInput` 返回可用 `errors.As` 提取的 `*remote.InputRequiredError`，包含原工具名、原始参数、requestState 和输入请求；可 JSON 序列化保存。调用 `pending.Continue(ctx, session, responses)` 继续；若又需输入，再次返回新的检查点。是否允许新连接恢复由远端服务决定。

```go
var pending *remote.InputRequiredError
if errors.As(err, &pending) {
    // 展示 pending.Requests，由用户选择 accept/decline/cancel 后构造 responses。
    result, err = pending.Continue(ctx, session, responses)
}
```

希望在同一工具调用中完成多轮交互时，使用 `remote.WithInputHandler(hostInputHandler, 8)`；宿主回调负责收集响应，最多处理指定轮数（1–64）。达到轮数上限、回调失败或取消时保留检查点；错误链仍可提取取消原因和待处理输入。不会自动批准或猜测人工答案。续接保留原参数和 requestState，最终 `IsError` 仍遵循 `WithToolErrorsAsRejected`。

旧协议通过官方反向 `elicitation/create` 请求收集输入，必须在 `ConnectWithOptions` 传入 `ElicitationHandler`。单独的 `WithInputHandler` 用于2026协议返回的多轮结果，不能代替旧版反向处理器。

## 将 GoAgent 发布为 MCP 服务

`management/mcpserver` 将已发布 Agent 暴露为工具，提供 task_get/task_wait/task_cancel/task_respond，与 HTTP/A2A 使用同一个管理任务服务。完整装配、工具契约和协议边界见 [协议接入指南](../docs/guides/PROTOCOL_ADAPTERS.md)。
