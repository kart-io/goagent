# HTTP, SSE, WebSocket, Webhook, and Hooks Integration

[English](HTTP_AGENT.md) | [中文](HTTP_AGENT_CN.md)

GoAgent's network entry points are provided by the standalone extension `github.com/kart-io/goagent-contrib/httpagent`. The main library remains responsible for Agents/tools/graphs/durable execution; the network adapter is responsible for entry points, protocols, and runtime resource ownership, and the web page lives in goagent-examples.

## Documentation and Versions

This document was originally written against contrib commit `cfb3e85`, GoAgent v0.32.2, and Go 1.25. Integrating with the current GoAgent requires Go 1.27+; the specific dependency versions of the extension and examples are defined by their own `go.mod`, and the interfaces by the following documents for the corresponding version.

| What you need | Document |
| --- | --- |
| Run from scratch, copy a complete host program | [User Guide](https://github.com/kart-io/goagent-contrib/blob/master/httpagent/docs/USER_GUIDE.md) |
| Implementation trade-offs for owners, terminal states, cancellation, admission, and notifications | [Extension Design](https://github.com/kart-io/goagent-contrib/blob/master/httpagent/docs/DESIGN.md) |
| All Config fields, defaults, public interfaces, protocols, and errors | [API Reference](https://github.com/kart-io/goagent-contrib/blob/master/httpagent/docs/API_REFERENCE.md) |
| How the GoAgent core and the extension divide responsibilities | [Design entry in this repository](../design/HTTP_AGENT_DESIGN.md) |
| Web page, CLI, bidirectional callbacks, and host WS cases | [Scenarios Guide](https://github.com/kart-io/goagent-examples/blob/main/orchestration/httpagent/SCENARIOS.md) |

## Running the Ready-Made Example

```sh
git clone -b main https://github.com/kart-io/goagent-examples.git
cd goagent-examples/orchestration/httpagent
GOWORK=off go run .
```

Open http://127.0.0.1:8097 . In another terminal, in the same directory, run:

```sh
GOWORK=off go run ./cmd/client -case all -input '查询订单 E100'
```

Expect 4 completed and 1 cancelled; by default it uses an offline scripted model, while actually executing Plugin tools and network transport. There is no need to clone a local contrib working tree; the pinned commit can already be downloaded remotely.

When you need outbound Webhooks, first start the receiver in another terminal in the same directory, then start the example service with WEBHOOK_URL:

```sh
GOWORK=off go run ./cmd/webhook-receiver -listen 127.0.0.1:9000
# In another terminal; replaces the plain service start command above
GOWORK=off WEBHOOK_URL=http://127.0.0.1:9000/events go run .
```

## Installing in a Business Service

```sh
go get github.com/kart-io/goagent-contrib/httpagent@v0.0.0-20261031151618-cfb3e85df18a
```

Implement a Factory that assembles the Builder, Plugin, and Dispatcher under the request context and returns Native(agent.ExecuteStream). Check llm.HasToolStreaming against the real model's capabilities; do not infer it from the existence of the ExecuteStream method. The Factory creates a new request-scoped Agent each time, and shared resources such as the Plugin Host are managed by the service lifecycle. The complete compilable program is in the extension's user guide.

| Feature | Entry point/configuration | Main boundaries |
| --- | --- | --- |
| Synchronous results, streaming tool events | POST /invoke, POST /stream | Both keep the same native execution; Answer explicitly does not support SSE |
| WebSocket | GET /ws, goagent.v1 | One active run per connection; after the terminal state, wait for ready before reuse |
| Own synchronous events | Receiver.Mode=inline | Respond only after the business work completes |
| Worker after durable admission | Receiver.Mode=queued, Acceptor, Handler.Run | The durable queue, deduplication, and worker are provided by the host |
| Outbound notifications | Delivery, NewSender | Bounded single attempt by default; reliable redelivery requires a host outbox |
| Native Hooks and run observation | Plugin Hooks, Config.Observer | Hooks take part in the execution loop; the Observer observes the lifecycle |
| WS for other frameworks | WebSocketUpgrader or ServeWebSocket | The interface contains no Gorilla types; the default implementation still uses Gorilla |

Handler routes have no /api prefix; the example host mounts them with StripPrefix. The complete HTTP/WS messages are defined by the extension's API reference.

## Lifecycle and Durable Execution

run_id is the identity of the current run; it is not a business session lock, an event deduplication key, or a cache of completed results. A shared SessionID requires coordination by the host; when budgets/journals/durable recovery are needed, the Factory should combine [Host Integration](HOST_INTEGRATION.md) and [Execution Governance](EXECUTION_GOVERNANCE.md).

A network disconnect or an unacknowledged exit cannot prove that tool side effects did not happen. When the client has not received the business terminal state, diagnose by run_id; do not blindly retry with a new ID. To stop the service, first cancel with BeginShutdown, then wait for HTTP Shutdown and Handler.Shutdown, and close the shared Host after they succeed. For specific timeouts and resource retention design, see the extension documentation.

For the documentation code blocks and the scope of source comparison, see [Extension documentation verification](https://github.com/kart-io/goagent-contrib/blob/master/httpagent/docs/VERIFICATION.md). For measured evidence, see [Full implementation acceptance](../evaluation/2026-10-31-httpagent-implementation/REPORT.md) and [Replaceable WS interface acceptance](../evaluation/2026-10-31-websocket-interfaces/REPORT.md).

For checks after merging into the main branch, see [Merge acceptance](../evaluation/2026-10-31-httpagent-merge/REPORT.md).
