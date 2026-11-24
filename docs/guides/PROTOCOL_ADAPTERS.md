# A2A and MCP Integration

[English](PROTOCOL_ADAPTERS.md) | [中文](PROTOCOL_ADAPTERS_CN.md)

All adapters live in GoAgent. `management/a2a`, `management/httpapi`, and `management/mcpserver` use the same `management.Service`; tasks, sessions, approvals, and commands are stored only in the management Store. `management/protocol` handles shared identity mapping and state observation and does not create a second task store. Native Agents and Graphs continue to execute through `management/adapters`.

## Service Assembly

1. Create the management service as described in the [management integration notes](../../management/README.md), inject the Store, Session Store, HistoryReader, Authorizer, and versioned Factory, then register and publish Agents.
2. The host starts `management.NewWorker(service).Run(ctx)`; on stop, use Worker.Shutdown and wait for executors to exit. Adapter constructors do not start a Worker, create tables, or listen on ports.
3. Pass the same service to `a2a.New`, `httpapi.New`, and `mcpserver.NewHTTPHandler`. For a complete, compilable routing and authentication example, see [example_test.go](../../management/protocol/example_test.go).
4. The A2A `Config.URL` is the public RPC URL; the Agent Card is served at `/.well-known/agent-card.json`. Each Handler binds one Agent; multiple Agents can be mounted on different hosts or different discovery paths. The MCP `Config.Agents` maps registered Agents to tool names.

The host authentication callback produces a `management.Actor` from a trusted login state and must not trust the tenant/subject submitted by the caller directly. Both the A2A Card and RPC are authenticated; MCP initialization and tools/list are protected by host HTTP middleware, and every tools/call goes through the Actor callback and management service authorization again. `mcpserver.New` returns the official Server, which the host can assemble with stdio or another transport; for stdio, the host should bind the Actor to that process explicitly.

For HTTP, prefer `NewHTTPHandler`: it uses the official SDK's stateless Streamable HTTP and supports MCP 2026-07-28 multi-round input. Business state still lives in the management Store; "stateless" describes only the transport. The SDK's default stateful HTTP negotiates the old protocol, and old clients must provide an `ElicitationHandler` to handle human input. When old-style reverse calls are needed, the host should use `mcpserver.New` with stateful HTTP or stdio; the stateless transport of `NewHTTPHandler` does not support old-protocol reverse requests, and old clients can use task_get/task_respond instead. For the old protocol, the server uses the official `Session.Elicit` to process rounds continuously, up to 64 rounds per call; with the 2026 protocol, a checkpoint is returned for each round and the host controls the pace.

## Identity and Task State

| Concept | Shared management identity |
| --- | --- |
| A2A `taskId` / MCP `task_id` | Task.ID; can be queried, replied to, or canceled across entry points |
| A2A `contextId` / MCP `session_id` | Session.ID; when omitted, a new session is prepared through the same Worker |
| A2A `messageId` / MCP `message_id` | Idempotency key isolated per Actor and Agent; the same key with different content is rejected |
| A2A `metadata.goagent` / MCP structuredContent | Public TaskView: revision, input, result, wait and approval identities |

Clients must generate a stable and distinct message_id (at most 256 bytes) for each new message; network retries reuse the original ID and original input. A normal reply carries `expected_revision`, `wait_id`, and `input`; an approval reply carries `approval_id`, `approval_revision`, `decision` (approve/reject), and an optional reason. Approval identities come from TaskView.approvals; normal input cannot be used as an approval.

queued → A2A submitted, running/canceling → working, waiting_input/waiting_approval/attention → input-required, succeeded → completed, canceled → canceled, other failures → failed. attention means the host needs to check an unknown effect; it must not be rerun as a normal wait for input.

## A2A

Uses the JSON-RPC/SSE and protocol types of the official `a2a-go/v2 v2.7.0`, providing Agent Card, SendMessage, SendStreamingMessage, GetTask, CancelTask, and SubscribeToTask.

The initial message uses the user role, a messageId, and one text/plain Text part or application/json Data part. To reply to an existing task, keep taskId/contextId, use a new messageId, and put the following object in a single Data part:

```json
{"expected_revision":4,"wait_id":"补充信息","input":{"answer":"用户回答"}}
```

For approval, use:

```json
{"approval_id":"审批ID","approval_revision":1,"decision":"approve","reason":"用户已确认"}
```

By default, SendMessage waits until completion or a wait for input; `returnImmediately=true` returns after a new task is admitted or a resume command starts applying. A streaming reply first sends a full Task snapshot, then state/result events; a resume does not wait for the executor to finish before sending the first event. Status message IDs are stable within the same wait revision, and a new revision uses a new ID. Disconnecting a subscription or an HTTP timeout does not cancel the background task; cancellation requires an explicit CancelTask call. Subscribing to a terminal task returns UnsupportedOperation; use GetTask to read the result.

Streaming updates come from polling durable state (100ms by default), not from a model token stream, and do not promise to replay every intermediate revision. Push notifications, file/multi-part input, extended Cards, message history, and ListTasks currently return explicit not-supported errors. The management Store has no ordering contract by state update time as A2A requires, so ListTasks is not faked with creation-time ordering; task lists can still be queried through the management HTTP API.

Calling an external Agent:

```go
client, err := remote.Connect(ctx, "https://agent.example", authenticatedHTTPClient)
if err != nil { return err }
defer client.Destroy()
tool, err := remote.Tool(client, "remote_agent", "调用远端 Agent")
// Pass tool to builder.WithTools; you can also use the client's streaming, query, and cancel APIs directly.
```

Here remote is `github.com/kart-io/goagent/a2a/remote`. The tool actions are send/get/cancel; send requires message_id and input and may carry context_id/task_id. A successful tool call only means the protocol request succeeded; the host/model must still check the business state of the returned Task. The default durable policy is MaxAttempts=1, which prevents unknown external effects from being repeated automatically.

In a durable run, pass `remote.WithDurableInput("v1")`; when send hits input-required, it pauses with `durable.AwaitInput`, and `Info` is the task's JSON. The host resumes with any JSON, and that value is sent as the next message to the same task (message ID `<message_id>/input/<n>`); sends that already completed are replayed from the journal and not resent. In this mode the tool declares `tools.InterruptibleTool` and no longer provides the durable policy above; outside a durable run, behavior is unchanged. Authentication, HTTP timeouts, and allowed remote addresses are managed by the host.

**Numeric boundary:** The GoAgent A2A HTTP entry point keeps the raw Data before SDK decoding, so durable input and idempotency comparison do not round `9007199254740993` to `9007199254740992`. The official client decodes returned `any` values such as Metadata/Data as float64; `a2a/remote.Tool` inherits this result-decoding boundary. Large integer identifiers or high-precision decimals that need an exact cross-platform representation should use JSON strings; do not rely on the SDK's `any` numbers for accounting or identity comparison. JSON number text emitted by the server keeps its precision.

## MCP Tool Contract

Uses the official `modelcontextprotocol/go-sdk v1.8.0`. These are MCP tools, not the native MCP tasks extension; tasks/get and tasks/update are not declared.

| Tool | Input | Behavior |
| --- | --- | --- |
| Tool name in Config.Agents | message_id, input, optional session_id | Submits a task and returns the current TaskView |
| task_get | task_id | Reads state, result, waits/approvals |
| task_wait | task_id | Waits until completion or human input; returns a TaskView or InputRequiredResult |
| task_cancel | task_id, message_id, expected_revision, optional reason | Admits the cancellation; canceling does not mean the producer has exited |
| task_respond | task_id, response | Submits normal input/approval asynchronously; returns task_id and command_id |

Before sending a form elicitation, task_wait checks the client capabilities of the current request; `elicitation:{}` is equivalent to form, and if it is not declared or only URL is supported, MissingRequiredClientCapabilities is returned. Query/completion results that need no input do not require this capability.

The `input` field of a normal input form is a **JSON-encoded string**; for example, to input the string "确认", fill in `"确认"`; for an object, fill in `{"answer":true}`. Approval forms use decision and reason. Only accept submits normal input; an approval decline maps to reject; cancel and a normal-input decline keep the wait state and do not cancel the underlying task.

2026 clients must send back the original arguments, requestState, and inputResponses keyed by the keys of inputRequests. One wait point is handled at a time; a subsequent wait returns a new state. requestState is location and revision information, not an authorization credential; Actor, task ownership, and revision are revalidated on every request. Do not let the model generate approval decisions automatically; the host should show the source and the operation and let the user choose.

If task_wait times out, use task_get to check the state or task_respond to reply asynchronously; the server does not treat a timeout as cancellation. Request only non-sensitive business information in forms; sensitive information such as credentials should be handled by a separate secure flow in the host.

For human input bridging, checkpoint saving, and reconnection examples for remote MCP tools, see the [MCP README](../../mcp/README.md). Whether recovery across Sessions is possible depends on the remote service; this service stores identities and waits in the shared task service, allowing the same authorized Actor to continue on a new connection.

## Verification and References

```bash
env -u GOROOT go test -race -count=1 ./management/protocol ./mcp/remote
```

The tests use the real official SDKs, HTTP transport, and management Worker, covering cross-entry-point query/reply/cancel, non-bypassable approvals, Worker restart, multi-round input, capability negotiation, message idempotency, first events, and numeric precision. The fixture is a transactional in-memory Store; these tests are not an acceptance of production database failure recovery.

Specification references: [A2A](https://a2a-protocol.org/latest/specification/), [MCP 2026-07-28 elicitation](https://modelcontextprotocol.io/specification/2026-07-28/client/elicitation). For the implementation and the scope of this acceptance, see the [verification report](../evaluation/2026-11-09-protocol-adapters/REPORT.md).
