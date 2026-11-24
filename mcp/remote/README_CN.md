# remote MCP 客户端工具

[English](README.md) | [中文](README_CN.md)

`remote` 把官方 MCP Go SDK（`github.com/modelcontextprotocol/go-sdk/mcp`）的会话适配为 goagent 工具。Agent 需要调用远端 MCP 服务器经 HTTP（`StreamableClientTransport`）或 stdio（`CommandTransport`）提供的工具时使用它。`Tools` 返回的 `interfaces.Tool` 可直接交给 Builder；协议协商、传输、分页与取消由 SDK 负责。

## 用法

```go
session, err := remote.Connect(ctx, &mcp.StreamableClientTransport{Endpoint: "https://mcp.example.com/mcp"})
if err != nil {
    return err
}
defer session.Close() // 工具使用期间保持会话打开

mcpTools, err := remote.Tools(ctx, session,
    remote.WithToolName(func(name string) (string, bool) {
        return "docs_" + name, !strings.HasPrefix(name, "admin_") // 返回 false 跳过该工具
    }),
    remote.WithToolErrorsAsRejected(),
    remote.WithDurablePolicy(durable.OperationPolicy{MaxAttempts: 1}),
)
if err != nil {
    return err
}
agent, err := builder.NewSimpleBuilder(client).WithTools(mcpTools...).Build()
```

远端工具要求输入且未配置处理器时，调用以可保存的检查点失败：

```go
var pending *remote.InputRequiredError
if errors.As(err, &pending) {
    // 向用户展示 pending.Requests，再构造 responses。
    result, err = pending.Continue(ctx, session, responses)
}
```

## 主要 API

| 标识符 | 说明 |
|---|---|
| `Connect(ctx, transport)` | 通过任意 SDK `mcp.Transport` 协商 MCP 会话；调用方负责 `Close` |
| `ConnectWithOptions(ctx, transport, *mcp.ClientOptions)` | 同上，可传入 SDK 客户端选项，如旧协议的 `ElicitationHandler` 或 sampling 处理器 |
| `Tools(ctx, session, opts...)` | 分页列出全部远端工具，逐个适配为 `interfaces.Tool` |
| `WithToolName(func)` | 决定暴露给模型的名称；返回 false 时跳过该工具 |
| `WithToolErrorsAsRejected()` | 远端 `IsError` 结果以 `tools.Rejected` 交给模型，而不是中止运行 |
| `WithDurablePolicy(durable.OperationPolicy)` | 声明持久运行中的日志策略（`tools.DurablePolicyProvider`） |
| `WithInputHandler(handler, maxRounds)` | 由宿主回调在同一次工具调用内收集输入，最多 `maxRounds` 轮（1–64） |
| `WithDurableInput(version)` | 持久运行中遇到输入请求时以 `durable.AwaitInput` 暂停（`tools.InterruptibleTool`） |
| `InputRequiredError` | 可 JSON 序列化的检查点：工具名、参数、请求状态与输入请求 |
| `InputRequiredError.Continue(ctx, session, responses)` | 发送续接；仍需输入时返回新的 `*InputRequiredError` |

## 行为说明

- 调用成功时以完整的 `*mcp.CallToolResult`（文本、图片、结构化内容）作为 `ToolOutput.Result` 返回。
- 远端错误结果（`IsError`）默认是 Go error。连接与协议错误始终是 Go error。
- 工具列表是 `Tools` 调用时的快照；服务端变更后重新列出。
- 认证、请求大小限制与 HTTP 客户端在 SDK transport 上配置，不在本包。
- `Connect` 声明 form elicitation 能力，并在选项未设置时关闭 SDK 的多轮往返，使新协议的输入请求交给本适配层。适配层不会自动批准或猜测输入。
- `Continue` 要求每个待处理请求都有一个类型匹配的响应；elicitation 响应的 action 必须是 `accept`、`decline` 或 `cancel`。重连后的会话能否续接由服务端决定。
- 使用 `WithInputHandler` 时，用尽 `maxRounds`、回调出错或被取消，返回的错误中仍包含 `*InputRequiredError`。
- `WithDurableInput` 以 `MaxAttempts: 1` 把每次远端调用记入日志，恢复时回放已完成的调用，并接受 `mcp.InputResponseMap` 的 JSON 作为恢复值。它要求非空版本，不能与 `WithDurablePolicy` 同用；context 不能持久等待（`durable.CanWait` 为 false）时，输入请求按上文方式处理。
- 无效的选项组合由 `Tools` 以 `CodeInvalidConfig` 错误返回。

## 另见

- [MCP 概览](../README.md)
- [检索、事件流、MCP 与可恢复步骤](../../docs/guides/PRIORITY_CAPABILITIES_CN.md)
- [插件](../../docs/guides/PLUGIN_SYSTEM_GUIDE_CN.md)
- [协议接入](../../docs/guides/PROTOCOL_ADAPTERS_CN.md)
