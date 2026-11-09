# A2A 与 MCP 接入

所有适配器均位于 GoAgent。`management/a2a`、`management/httpapi` 和 `management/mcpserver` 使用同一个 `management.Service`，任务、会话、审批和命令只保存在管理 Store 中。`management/protocol` 负责公共身份映射与状态观测，不建立第二套任务库。原生 Agent 与 Graph 继续使用 `management/adapters` 执行。

## 服务装配

1. 按 [management 接入说明](../../management/README.md) 创建管理服务，注入 Store、Session Store、HistoryReader、Authorizer 和版本化 Factory，注册并发布 Agent。
2. 宿主启动 `management.NewWorker(service).Run(ctx)`；停止时使用 Worker.Shutdown 并等待执行器退出。适配器构造函数不会启动 Worker、建表或监听端口。
3. 将同一个 service 传入 `a2a.New`、`httpapi.New` 和 `mcpserver.NewHTTPHandler`。完整、可编译的路由和认证示例见 [example_test.go](../../management/protocol/example_test.go)。
4. A2A 的 `Config.URL` 是公开 RPC URL；Agent Card 挂在 `/.well-known/agent-card.json`。每个 Handler 绑定一个 Agent；多个 Agent 可挂不同主机或不同发现路径。MCP 的 `Config.Agents` 将已注册 Agent 映射为工具名。

宿主认证回调从可信登录态产生 `management.Actor`，不能直接信任调用者提交的 tenant/subject。A2A Card 和 RPC 都经过认证；MCP 初始化与 tools/list 由宿主 HTTP 中间件保护，每次 tools/call 再经过 Actor 回调和管理服务授权。`mcpserver.New` 返回官方 Server，可由宿主装配 stdio 或其他 transport；stdio 的 Actor 应由宿主为该进程显式绑定。

HTTP 首选 `NewHTTPHandler`：使用官方 SDK 的无状态 Streamable HTTP，支持 MCP 2026-07-28 多轮输入。业务状态仍在管理 Store；无状态只描述 transport。默认 SDK 有状态 HTTP 会协商旧协议，旧客户端需提供 `ElicitationHandler` 才能处理人工输入。需要旧版反向调用时，宿主应使用 `mcpserver.New` 配合有状态 HTTP 或 stdio；`NewHTTPHandler` 的无状态 transport 不支持旧协议反向请求，旧客户端可改用 task_get/task_respond。服务端对旧协议使用官方 `Session.Elicit` 连续处理，单次最多64轮；2026协议逐轮返回检查点，由宿主控制处理节奏。

## 身份与任务状态

| 概念 | 共同管理身份 |
| --- | --- |
| A2A `taskId` / MCP `task_id` | Task.ID，可跨入口查询、回复或取消 |
| A2A `contextId` / MCP `session_id` | Session.ID；缺省时经同一个 Worker 准备新会话 |
| A2A `messageId` / MCP `message_id` | 按 Actor 与 Agent 隔离的幂等键；同键不同内容拒绝 |
| A2A `metadata.goagent` / MCP structuredContent | 公开 TaskView：修订、输入、结果、等待与审批身份 |

客户端必须为每条新消息生成稳定且不同的 message_id（最多256字节），网络重试复用原 ID 和原输入。普通回复携带 `expected_revision`、`wait_id` 和 `input`；审批回复携带 `approval_id`、`approval_revision`、`decision`（approve/reject）及可选 reason。审批身份从 TaskView.approvals 获取，不能将普通输入当作审批。

queued → A2A submitted，running/canceling → working，waiting_input/waiting_approval/attention → input-required，succeeded → completed，canceled → canceled，其他失败 → failed。attention 表示需要宿主核对未知效果，不能直接当作普通等待输入重跑。

## A2A

使用官方 `a2a-go/v2 v2.7.0` 的 JSON-RPC/SSE 与协议类型，提供 Agent Card、SendMessage、SendStreamingMessage、GetTask、CancelTask 和 SubscribeToTask。

初次消息使用 user 角色、messageId，以及一个 text/plain Text part 或 application/json Data part。回复原任务时，保留 taskId/contextId，使用新的 messageId，并将以下对象放入单个 Data part：

```json
{"expected_revision":4,"wait_id":"补充信息","input":{"answer":"用户回答"}}
```

审批使用：

```json
{"approval_id":"审批ID","approval_revision":1,"decision":"approve","reason":"用户已确认"}
```

默认 SendMessage 等到完成或等待输入；`returnImmediately=true` 在接纳新任务或恢复命令开始应用后返回。流式回复先给完整 Task 快照，再给状态/结果事件；恢复不等执行器完成才发首事件。同一等待修订的状态消息 ID 稳定，新修订使用新 ID。断开订阅或 HTTP 超时不取消后台任务；取消须显式调用 CancelTask。终态订阅返回 UnsupportedOperation，应使用 GetTask 读取结果。

流式更新来自持久状态轮询（默认100ms），不是模型 token 流，也不承诺重放每个中间修订。推送通知、文件/多 part 输入、扩展 Card、消息历史及 ListTasks 当前明确返回不支持。管理 Store 没有 A2A 要求的状态更新时间排序契约，因此没有用创建时间排序冒充 ListTasks；任务列表仍可经管理 HTTP 查询。

调用外部 Agent：

```go
client, err := remote.Connect(ctx, "https://agent.example", authenticatedHTTPClient)
if err != nil { return err }
defer client.Destroy()
tool, err := remote.Tool(client, "remote_agent", "调用远端 Agent")
// 将 tool 交给 builder.WithTools；也可直接用 client 的流式、查询、取消接口。
```

此处 remote 为 `github.com/kart-io/goagent/a2a/remote`。工具 action 为 send/get/cancel；send 需要 message_id 和 input，可带 context_id/task_id。工具调用成功只说明协议请求成功，宿主/模型仍须检查返回 Task 的业务状态。默认 durable 策略 MaxAttempts=1，防止未知外部效果被自动重复执行。认证、HTTP 超时和允许访问的远端地址由宿主管理。

**数值边界：** GoAgent A2A HTTP 入口在 SDK 解码前保留原始 Data，持久输入与幂等比较不会把 `9007199254740993` 舍入为 `9007199254740992`。官方客户端将返回的 Metadata/Data 等 `any` 解码为 float64；`a2a/remote.Tool` 继承这一结果解码边界。需要精确跨平台表示的大整数标识或高精度小数应使用 JSON 字符串；不要依赖 SDK 的 any 数字做账务或身份比较。服务端发出的 JSON 数字原文仍保持精度。

## MCP 工具契约

使用官方 `modelcontextprotocol/go-sdk v1.8.0`。这些是 MCP 工具，不是原生 MCP tasks 扩展；没有宣告 tasks/get 或 tasks/update。

| 工具 | 输入 | 行为 |
| --- | --- | --- |
| Config.Agents 中的工具名 | message_id、input、可选 session_id | 提交任务，返回当前 TaskView |
| task_get | task_id | 读取状态、结果、等待/审批 |
| task_wait | task_id | 等到完成或人工输入，返回 TaskView 或 InputRequiredResult |
| task_cancel | task_id、message_id、expected_revision、可选 reason | 接纳取消；canceling 不代表生产者已退出 |
| task_respond | task_id、response | 异步提交普通输入/审批，返回 task_id 与 command_id |

task_wait 发 form elicitation 前检查当前请求的客户端能力；`elicitation:{}` 与 form 等价，未声明或仅支持 URL 时返回 MissingRequiredClientCapabilities。无需输入的查询/完成结果不要求此能力。

普通输入表单的 `input` 字段是 **JSON 编码字符串**，例如输入字符串“确认”应填写 `"确认"`；对象填写 `{"answer":true}`。审批表单使用 decision 与 reason。accept 才提交普通输入；审批 decline 映射 reject；cancel 和普通输入 decline 保留等待状态，不会取消底层任务。

2026客户端必须回传原 arguments、requestState，以及按 inputRequests 的键关联的 inputResponses。一次处理一个等待点，后续等待会返回新状态。requestState 是定位与修订信息，不是授权凭据，每次请求重新校验 Actor、任务归属与修订。不要把审批决定交给模型自动生成；宿主应展示来源和操作内容，让用户选择。

若 task_wait 超时，使用 task_get 查状态或 task_respond 异步回复；服务器不把超时当作取消。只在 form 中请求非敏感业务信息，凭据等敏感信息应由宿主的独立安全流程处理。

远端 MCP 工具的人工输入桥接、检查点保存与重连示例见 [MCP README](../../mcp/README.md)。是否能跨 Session 恢复取决于远端服务；本服务将身份和等待保存在共同任务服务，允许同一授权 Actor 在新连接中继续。

## 验证与依据

```bash
env -u GOROOT GO_TEST_MODE=true go test -race -count=1 ./management/protocol ./mcp/remote
```

测试使用真实官方 SDK、HTTP transport 和管理 Worker，覆盖跨入口查询/回复/取消、审批不可绕过、Worker 重启、多轮输入、能力协商、消息幂等、首事件以及数值精度。夹具是事务内存 Store；这些测试不等于生产数据库故障恢复验收。

规范依据：[A2A](https://a2a-protocol.org/latest/specification/)、[MCP 2026-07-28 elicitation](https://modelcontextprotocol.io/specification/2026-07-28/client/elicitation)。实现与本次验收范围见 [验证报告](../evaluation/2026-11-09-protocol-adapters/REPORT.md)。
