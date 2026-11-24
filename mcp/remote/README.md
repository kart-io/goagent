# remote: MCP Client Tools

[English](README.md) | [中文](README_CN.md)

`remote` adapts sessions of the official MCP Go SDK (`github.com/modelcontextprotocol/go-sdk/mcp`) to goagent tools. Reach for it when an Agent should call tools served by a remote MCP server over HTTP (`StreamableClientTransport`) or stdio (`CommandTransport`). `Tools` returns `interfaces.Tool` values that go straight to the Builder; protocol negotiation, transport, pagination, and cancellation stay in the SDK.

## Usage

```go
session, err := remote.Connect(ctx, &mcp.StreamableClientTransport{Endpoint: "https://mcp.example.com/mcp"})
if err != nil {
    return err
}
defer session.Close() // keep the session open while the tools are in use

mcpTools, err := remote.Tools(ctx, session,
    remote.WithToolName(func(name string) (string, bool) {
        return "docs_" + name, !strings.HasPrefix(name, "admin_") // false skips the tool
    }),
    remote.WithToolErrorsAsRejected(),
    remote.WithDurablePolicy(durable.OperationPolicy{MaxAttempts: 1}),
)
if err != nil {
    return err
}
agent, err := builder.NewSimpleBuilder(client).WithTools(mcpTools...).Build()
```

When a remote tool asks for input and no handler is configured, the call fails with a saveable checkpoint:

```go
var pending *remote.InputRequiredError
if errors.As(err, &pending) {
    // Show pending.Requests to the user, then build responses.
    result, err = pending.Continue(ctx, session, responses)
}
```

## Key API

| Identifier | Description |
|---|---|
| `Connect(ctx, transport)` | Negotiates an MCP session over any SDK `mcp.Transport`; the caller must `Close` it |
| `ConnectWithOptions(ctx, transport, *mcp.ClientOptions)` | Same, with SDK client options such as an `ElicitationHandler` for the old protocol or sampling handlers |
| `Tools(ctx, session, opts...)` | Lists all pages of remote tools and adapts each to `interfaces.Tool` |
| `WithToolName(func)` | Chooses the name exposed to the model; returning false omits the tool |
| `WithToolErrorsAsRejected()` | Returns remote `IsError` results to the model as `tools.Rejected` instead of aborting |
| `WithDurablePolicy(durable.OperationPolicy)` | Declares the journal policy for durable runs (`tools.DurablePolicyProvider`) |
| `WithInputHandler(handler, maxRounds)` | Host callback that collects input within the same tool call, up to `maxRounds` (1-64) |
| `WithDurableInput(version)` | In durable runs, pauses on input requests with `durable.AwaitInput` (`tools.InterruptibleTool`) |
| `InputRequiredError` | JSON-serializable checkpoint: tool name, arguments, request state, and input requests |
| `InputRequiredError.Continue(ctx, session, responses)` | Sends the continuation; returns a new `*InputRequiredError` if more input is needed |

## Behavior Notes

- A successful call returns the complete `*mcp.CallToolResult` (text, images, structured content) as `ToolOutput.Result`.
- A remote error result (`IsError`) is a Go error by default. Connection and protocol errors are always Go errors.
- The tool list is a snapshot taken by `Tools`; list again after the server changes it.
- Authentication, request limits, and the HTTP client are configured on the SDK transport, not here.
- `Connect` declares form elicitation and disables the SDK's multi-round trip unless options set it, so new-protocol input requests reach this adapter. The adapter never auto-approves or guesses input.
- `Continue` requires one response per pending request, with a matching type; an elicitation response must have action `accept`, `decline`, or `cancel`. Whether a reconnected session may continue is up to the server.
- With `WithInputHandler`, exhausting `maxRounds`, a handler error, or cancellation returns an error that still contains the `*InputRequiredError`.
- `WithDurableInput` journals each remote call with `MaxAttempts: 1`, replays completed calls on resume, and accepts the JSON of an `mcp.InputResponseMap` as the resume value. It requires a non-empty version, cannot be combined with `WithDurablePolicy`; when the context cannot wait durably (`durable.CanWait` is false), input requests are handled as described above.
- Invalid option combinations are returned by `Tools` as `CodeInvalidConfig` errors.

## See also

- [MCP overview](../README.md)
- [Retrieval, Event Streams, MCP, and Resumable Steps](../../docs/guides/PRIORITY_CAPABILITIES.md)
- [Plugins](../../docs/guides/PLUGIN_SYSTEM_GUIDE.md)
- [Protocol Adapters](../../docs/guides/PROTOCOL_ADAPTERS.md)
