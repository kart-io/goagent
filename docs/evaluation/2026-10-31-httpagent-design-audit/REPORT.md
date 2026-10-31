# HTTP / Web / WebSocket / Webhook / Hooks 设计审核

日期：2026-10-31。审核对象：`57c0f05` 中的 HTTP Agent 设计；当前主库代码基线 `f364a20`。本轮修改设计与研究材料，**没有实现 httpagent 产品模块，也没有宣称完成线上服务验收**。

## 结论

原稿方向合理，但不宜直接按原文编码：**4 项 P1、6 项 P2**。最关键的问题是误把 Builder.Execute 当成原生工具执行入口、误用方法集推断流式能力，以及默认同步 Webhook 与外部平台快速确认要求不匹配。

已修订[设计文档](../../superpowers/specs/2026-10-31-http-agent-design.md)，引入显式执行绑定、唯一终态所有者、构造前准入登记、可实施停服顺序及 queued/inline Webhook 接纳模式。修订是设计层面的处理，不代表产品代码已经修复或实现。

独立只读审阅另核对了代码与生命周期约束，报告 3 项 P1、4 项 P2；本报告合并外部协议调研发现，而非把两套发现数字相加。此前“nothing important left”只检查早期设计缺失问题，不是完整协议/代码一致性审核，本轮发现取代该宽泛结论。

## 审核发现与修订

| 编号 | 级别 | 原稿问题及影响 | 修订依据与处理 |
| --- | --- | --- | --- |
| F01 | P1 | `/invoke`、入站 Webhook 直接 Execute，SSE 则 ExecuteStream。同一 Plugin 工具在不同入口可能根本不执行；预算和 durable 语义也不同 | [Execute](../../../builder/configurable_agent.go:97)、[原生适配](../../../builder/runnable.go:11)。改为 Factory 提供统一 RunFunc；同步响应只改变传输方式。answer_only 必须显式选择 |
| F02 | P1 | `runner.(StreamRunner)` 对底层仅同步模型的 ConfigurableAgent 也成功，501 与 WS 回退承诺不成立 | [实际检查](../../../builder/events.go:53)、[模型能力](../../../llm/capabilities.go:200)。显式声明能力，执行前拒绝；禁止流式失败后重执行业务 |
| F03 | P1 | 未要求在 Factory/Upgrade/accepted 前原子纳管。Shutdown 可能漏掉仍在访问共享 Plugin 资源的构造过程，停服顺序也可能先等请求再取消 | [构建回调](../../../builder/configurable_agent.go:53)、[资源约定](../../../plugin/host.go:19)。先登记所有者，BeginShutdown 先拒绝/取消，再并行等待 HTTP 和 Handler 回收 |
| F04 | P1 | 默认等 Agent 执行后再 ACK Webhook，可能超过平台时限，引起投递失败或重复处理；“双向支持”不能因此推导平台兼容 | Slack/GitHub/Stripe 官方要求快速确认，见下文。增加宿主持久 Acceptor + worker 接口；inline 必须显式标注同步限制。没有持久接纳就不返回伪成功 |
| F05 | P2 | “只一个终态”没有明确提交点。done、ctx 取消、生产者 EOF、编码失败和 cleanup 可能被不同分支判成不同结果 | [事件生产者](../../../builder/events.go:87)。候选终态等生产者退出，预编码后唯一冻结；退出超时只关闭传输并标为未确认，不伪造业务终态；WS ready 表示已释放连接执行槽 |
| F06 | P2 | 504 承诺忽略 Builder 私有超时：可能只有无终态 EOF，HTTP 外层 ctx 尚未超时，无法可靠推断原因 | [执行期限](../../../builder/execution_gate.go:10)、[尽力发送](../../../builder/events.go:90)。示例共用外层期限；明确 DeadlineExceeded 才 timeout，未知 EOF 为 stream_incomplete |
| F07 | P2 | 适配器生命周期观察者未固定，若直接装作 core.Callback，通知失败可能把已经完成的业务变成失败，OnStart 时机也不相同 | [Callback 构建期](../../../builder/configurable_agent.go:85)、[OnEnd](../../../builder/configurable_agent.go:187)。独立 Observer/DeliverySink，错误与 panic 单独报告；原 Hook 保持业务语义 |
| F08 | P2 | 一连接一运行、单 Agent 执行门不能保护两个 Factory 实例写同一业务会话 | [实例级执行门](../../../builder/execution_gate.go:27)、[宿主会话约定](../../guides/HOST_INTEGRATION.md)。默认按运行隔离，共享会话由宿主锁/持久租约协调 |
| F09 | P2 | 只限制 WS 64 帧不等于字节内存有界；SSE/JSON 没有写期限和 Flush 能力契约，context 超时不能代替阻塞 socket 写控制 | 明确单事件/队列字节/连接数/全局所有者上限、ResponseController 写期限与 Flush 检查；包括通知在内的工作保持有限容量 |
| F10 | P2 | 自定义 WebSocket/事件结构没有完整版本、事件顺序和字段冻结约定；改名 AgentEvent 无法自然兼容前端标准协议 | 明确 goagent.v1、schema_version、seq、稳定 DTO 和预编码；seq 不等于可恢复日志。AG-UI 可另适配，但不能虚称兼容 |

等级说明：P1 会直接改变核心业务执行或交付保证，应在实施前消除；P2 为协议、资源与边界缺口，必须进入实现与验收。没有把尚未实现的功能当作现有代码 Bug。

## 本地验证：4 个探针均通过

本地运行完整 Builder、Plugin、Provider 和实际工具；模型响应由实际 httptest HTTP 服务脚本化。HTTP 停服与 WebSocket 使用真实本地 TCP 连接。不是线上模型，也不是新 HTTP 产品模块的测试。

| 探针 | 实际结果 |
| --- | --- |
| Execute 与原生工具路径 | 同一个安装 Plugin 工具的 Agent：Execute 的 tool_calls=0，AsRunnable.Invoke 的 tool_calls=1 |
| 方法集与模型能力 | 方法断言为 true，ExecuteStream 实际返回 `requires llm.StreamClient` |
| HTTP 停服 | Shutdown 等待活动请求超时，不取消请求 context；30ms 期限返回 deadline exceeded |
| 已升级 WS 停服 | http.Server.Shutdown 返回成功后，WS 仍能正常双向收发；手动关闭后清理 |

运行：Go 1.25.0，`go test -race -count=1 -v`，4/4 通过，1.813 秒。见 [原始日志](probes.log)、[探针源码](audit_test.go.txt)、[版本与命令](review-baseline.json)。测试通过表示**旧假设已被反证且边界得到复现**，不表示原设计可直接交付。

复现：在主库根目录创建本地目录，将源码恢复成 Go 测试再运行。`.claude` 不进入产品包扫描与发布。

```sh
mkdir -p .claude/httpagent-design-probes
cp docs/evaluation/2026-10-31-httpagent-design-audit/audit_test.go.txt .claude/httpagent-design-probes/audit_test.go
env -u GOROOT GOTOOLCHAIN=go1.25.0 go test -race -count=1 -v ./.claude/httpagent-design-probes
```

本轮还执行现有 Builder 的 UnifiedEvents/Runnable 相关竞态回归，见 [代码契约回归](builder-contracts.log)。没有修改产品代码，不将文档检查冒充完整 HTTP/SSE/WS 产品验收。

## 官方资料与研究结论

### HTTP/SSE 与停服

Go 1.25 的 http.Server.Shutdown 会等活动请求，不负责取消它们；已接管的 WebSocket 也不在其回收范围。因此适配器需要先关闭准入并取消，再等待 HTTP 与 WS 的各自所有者退出。[Go HTTP 文档](https://pkg.go.dev/net/http@go1.25.0#Server.Shutdown)

SSE 是 UTF-8 的按行事件流，业务事件以空行分隔；心跳可用注释。Fetch POST 客户端必须处理网络分块与事件边界之间的差异，连接结束不能替代业务完成事件。自动重连和 Last-Event-ID 属于 EventSource/恢复机制讨论，当前一次性执行 API 不应自动重跑。[WHATWG SSE 规范](https://html.spec.whatwg.org/multipage/server-sent-events.html)

HTTP/SSE 的写入期限必须单独处理。ResponseController 能报告底层不支持的能力，并能设置响应写期限；中间件须正确转发或提供 Unwrap。这个约束也应覆盖演示部署，不能只写“支持 CORS”。[Go ResponseController](https://pkg.go.dev/net/http@go1.25.0#ResponseController)

### WebSocket

Gorilla 的连接并发约定是一个 reader、一个 writer；控制帧需要持续读取来处理。缓冲区大小不等于消息大小限制。浏览器原生 WebSocket 没有自动应用背压，服务端仍需限制输出字节和排队量。[Gorilla v1.5.3 文档](https://pkg.go.dev/github.com/gorilla/websocket@v1.5.3)、[浏览器 WebSocket](https://developer.mozilla.org/en-US/docs/Web/API/WebSocket)

建议保留同一连接单运行和显式 cancel，暂不扩展实时音频、跨连接取消或断线恢复。此为根据当前输入与核心能力作出的设计判断，不是 WebSocket 协议强制要求。

### Webhook 的确认与交付

Slack 要求 3 秒内返回 2xx，失败后会重试；GitHub 要求 10 秒内应答；Stripe 要求在复杂处理前快速确认。因此耗时 Agent 应位于持久接纳之后，由 worker 执行。[Slack Events API](https://docs.slack.dev/apis/events-api/#responding-to-events)、[GitHub Webhook 最佳实践](https://docs.github.com/en/webhooks/using-webhooks/best-practices-for-using-webhooks#respond-within-10-seconds)、[Stripe Webhook](https://docs.stripe.com/webhooks#quickly-return-a-2xx-response)

HTTP 202 表示已接受处理，不表示处理已经完成；它本身也不提供持久性或 exactly-once。修订稿要求“先持久接纳再确认”属于本项目对宿主 Acceptor 的额外交付契约，不能声称是 RFC 的自动保证。[RFC 9110 §15.3.3](https://www.rfc-editor.org/rfc/rfc9110.html#name-202-accepted)

接收去重、worker 交付和工具副作用幂等是不同层次；出站一次投递只说明本次尝试，不等于收件方仅处理一次。默认 best-effort 模式仍可支持双向协议，但不能宣传为开箱即用的可靠后台任务服务。具体平台的 body/challenge/status 由接收器明确映射。

### 是否应直接采用 AG-UI

AG-UI 面向 Agent 与前端交互，标准化运行、文本、工具与状态事件，其传输可基于 SSE/WS。它比 ACP/MCP 更接近 Web 页面场景；但标准 HTTP 客户端有其 RunAgentInput 与事件要求，单纯换 SSE 事件名字不构成兼容。[官方概述](https://docs.ag-ui.com/introduction)、[架构](https://docs.ag-ui.com/concepts/architecture)

当前 AgentEvent 仅有 tool 名称与 step，缺少完整 toolCallId 和参数增量；AG-UI 有专门的工具调用事件。若目标是接入 CopilotKit 等现成前端，应单独做有能力边界的 AG-UI Adapter/Codec 并验证，不能让第一版自定义 httpagent API 承担未承诺的完整协议。[AG-UI 事件](https://docs.ag-ui.com/concepts/events)、[当前 AgentEvent](../../../builder/events.go:25)

## 实施顺序建议

1. 先实现统一 Binding、能力声明、运行所有者和终态仲裁；用真实 Plugin 工具验证跨入口语义一致。
2. 接 JSON/SSE、期限、写入控制与停服；再接 WebSocket 的 read/write loop、取消、容量和连接回收。
3. 接入独立 Observer、出站通知和入站 Acceptor/worker；可靠队列/outbox 后端由宿主明确选择，示例不能冒充其持久保证。
4. 完成 Web 页面和真实浏览器测试；最后运行模块完整竞态、vet、lint，并验证断线、并发、超时和资源不配合情形。

当前建议：**采用修订设计推进实施计划**。如果验收目标包括具体第三方平台和进程崩溃后的可靠交付，须先选定宿主接纳/队列后端；这是部署前置条件，不应隐含为框架内置功能。

## 审查评分与限制

评分为本次人工式技术判断，不是自动化测量：原稿接口一致性 60、生命周期 72、生态定位 90、可验证性 78，综合 75，建议退回修订。修订稿的接口/生命周期/边界已明确，可用于规划，建议通过设计修订审阅；产品交付必须另以验收结果判定，当前没有实现完成评分。

外部网页按 2026-10-31 可见内容核对；Go/Gorilla 固定到项目基线版本。没有安装新运行服务、没有下载模型、没有改动已有业务代码或沙盒调研文件。


## 修订稿二次核对

独立审阅者核对原 7 项代码契约发现，认为统一 Binding、显式能力、构造前登记、停服顺序、Observer 隔离与共享会话要求已经覆盖。另发现 F05 修订过程曾同时写入“生产者退出超时即结束客户端”和“EOF 后才能冻结终态”，如果把前者解释为业务终态就会矛盾。

已明确选择：执行仍未退出时，客户端只得到传输层未确认状态（或流关闭），不会得到伪造的 completed/failed/cancelled；所有者保留容量和清理责任，实际退出后才提交一次业务终态及通知。Web 页面已有“无终态 EOF 显示未确认完成”的约定与之配套。该项已纳入验收，无需新增产品范围，也未将文档修订当作实现验证。
