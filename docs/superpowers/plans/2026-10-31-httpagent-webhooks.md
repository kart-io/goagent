# HTTP Agent、WebSocket、双向 Webhook 与 Web 页面实施计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox syntax for tracking.

**Goal:** 为 Plugin 装配的 GoAgent 提供统一 HTTP/SSE/WS 执行入口、Hook 观察、双向 Webhook 与可运行 Web 页面。

**Architecture:** contrib/httpagent 独立模块，Factory 返回显式 Binding，单一运行所有者控制取消、资源、冻结终态与通知。queued Webhook 使用宿主持久 Acceptor，worker 调用同一 Handler.Run；examples 提供浏览器页面及明示 inline 的离线示例。

**Tech Stack:** Go 1.25、net/http、gorilla/websocket v1.5.3、GoAgent v0.32.2、标准 JS Fetch/WebSocket。

**Spec:** docs/superpowers/specs/2026-10-31-http-agent-design.md（1974c6c 审核修订）。

## Global Constraints

- 独立 contrib 模块，不改主库执行行为；沿用 Factory/Plugin/Dispatcher。
- 输入/单事件 1 MiB，WS 队列 64 项/4 MiB，运行64、WS连接128；超时/心跳按设计默认值。
- 构造前登记；流真实退出前不释放资源；未退出的超时表示未确认，不伪造终态。
- 每项先 RED 再 GREEN；Go 命令 env -u GOROOT GOTOOLCHAIN=go1.25.0。
- 仅本地提交实现；示例未发布依赖使用外置临时 go.work 验证，不提交本机路径；如需发布由后续指令处理。

## Review Focus

1. Factory 忽略取消后迟到返回：资源不可提前清理、容量不提前释放（任务2）。
2. 输出不可编码或过大：只冻结一次错误，不重跑工具（任务2/4）。
3. queued 持久接纳的重复事件与网络断开：HTTP 接纳和执行生命周期分开（任务3）。
4. WS 终态与 ready 之间的新请求/取消：不得覆盖或重复运行（任务4）。
5. Browser 中文字符/SSE 跨块、无终态 EOF：显示未确认且不自动重提（任务5）。

### Task 1: 执行绑定与稳定 DTO

**Files:** contrib/httpagent/{go.mod,types.go,binding.go,binding_test.go}。
**Interfaces:** RunRequest、RunFunc、Binding{Run,Streaming,Mode}、Native(StreamFunc)、Answer(InvokeFunc)；Output/Event/RunEvent/Failure。
- [x] 写 TestNativeWaitsForProducerAndKeepsTools、TestAnswerDoesNotClaimStreaming、TestNativeRejectsMissingTerminal；断言真实 Builder 工具调用与等待关闭，而非方法断言。
- [x] 运行新模块测试，确认 API 尚不存在导致 RED。
- [x] 实现 Native 捕获候选终态并消费至 EOF；Answer 显式非流式；序列化 DTO。
- [x] 模块 go test -race 全通过并提交。

### Task 2: 所有者、JSON、SSE、取消与停服

**Files:** contrib/httpagent/{handler.go,run.go,transport.go,handler_test.go}。
**Interfaces:** New(Config)(*Handler,error)、ServeHTTP、Run(ctx,RunRequest)(*Output,error)、BeginShutdown、Shutdown；Config 的 Factory/Observer/Report 等注入。
- [x] 写真实 HTTP tests：JSON/SSE 同一 Run，输入400/413/405/501，未知EOF，编码错误；Factory 构造中停服、迟到清理、超时无伪终态、容量与重复 Shutdown。
- [x] 先运行并记录 RED，再实现同步准入登记+跟踪 worker、外层期限、单一冻结、延迟清理和 ResponseController 写入控制。
- [x] go test -race 全模块通过，提交。

### Task 3: 双向 Webhook 与独立 Observer

**Files:** contrib/httpagent/{webhook.go,webhook_test.go}，补充 run.go。
**Interfaces:** Receiver{Decode,Mode,Acceptor}、Incoming、Receipt、Acceptor.Accept、DeliverySink.Deliver、Sender；worker 经 Handler.Run 保留接纳 run_id。
- [x] 写 TestQueuedAcknowledgesAfterPersistentAdmission、TestInlineUsesSameBinding、TestSenderFailureCannotRepeatBusiness、TestObserverPanicDoesNotChangeOutcome、TestDeliveryUsesStableIDAndBudget。
- [x] RED 后实现原始body映射、显式queued/inline、持久接纳契约、冻结事件投递、重定向拒绝和独立错误报告。
- [x] 实际临时磁盘接纳测试重复事件复用 run_id；go test -race 全模块通过，提交。

### Task 4: WebSocket 入口

**Files:** contrib/httpagent/{websocket.go,websocket_test.go}。
**Interfaces:** GET /ws、goagent.v1，run/cancel → accepted/event/ready/error；复用任务2所有者与任务3通知。
- [x] 写真实 WS tests：多次运行、busy、cancel、断线、心跳、超大帧/结果、构造中Shutdown、慢消费者与ready顺序。
- [x] RED 后实现单reader/writer、登记upgrade、可取消队列、字节上限、心跳、关闭和所有者复用。
- [x] race 全模块通过，提交。

### Task 5: 可运行 Web 页面与示例

**Files:** examples/orchestration/httpagent/{main.go,main_test.go,web/index.html,web/app.js,web/style.css,README.md}；examples go.mod/go.sum；contrib README/httpagent README。
**Interfaces:** 示例进程挂载 /api +静态页，SSE/WS模式、停止按钮，实际 Plugin 工具与可选真实模型；明确inline Webhook及通知日志。
- [x] 写示例HTTP/Plugin工具集成和JS parser tests（中文分块、CRLF、多data、EOF无终态）。RED 后实现。
- [x] 使用外置 go.work 联调新模块；Go测试与浏览器实际发送、停止、SSE/WS切换均通过。
- [x] 文档记录完整curl、worker/queued接纳注入和真实验证证据，提交。

### Task 6: 最终验证与独立审阅

**Files:** 主库 docs/evaluation/2026-10-31-httpagent-implementation/REPORT.md 与证据；实施ledger。
- [x] contrib/httpagent 完整 race/vet/lint，受影响示例完整测试与构建，导入边界及浏览器验证。
- [x] 按 executing-plans 进行一次全分支独立审阅；重要发现单次TDD修复，模块全测试通过。
- [x] 更新计划/验收报告，说明可靠队列后端为宿主依赖；保留其他工作区修改。
