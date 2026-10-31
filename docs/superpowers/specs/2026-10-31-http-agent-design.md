# GoAgent HTTP 服务适配设计

状态：HTTP 服务、Hooks 和双向 Webhook 范围已确认；具体接口与交付保证待最终设计确认。日期：2026-10-31。

## 目标与定位

用户选择将 GoAgent/Plugin 的能力对外提供为 HTTP 服务。外部系统可以提交输入，获得完整 JSON 结果或 SSE 事件。复用现有 Agent 执行接口、Plugin 装配、工具管线与 context 取消。

用户已选择对外提供 HTTP 服务，并明确要求 Hooks 和双向 Webhook。以下模块划分、具体 API 和默认值由代理提出，待整体设计确认。实现放在独立模块 `github.com/kart-io/goagent-contrib/httpagent`，基于标准库 net/http；主库不增加 HTTP 服务或路由依赖。案例放在 goagent-examples/orchestration/httpagent。

## 方案比较

- 推荐：标准 net/http Handler + JSON/SSE，直接映射 Execute/ExecuteStream，可挂入宿主已有路由和中间件。适合当前“外部系统调用 Agent”的目标。
- Plugin 直接启动监听器：Host.Start 先于 plugin.Install/Agent.Build，直接调用尚未构建的 Agent 会产生生命周期循环，不能作为默认装配方式。
- OpenAI 兼容接口：需要另外定义 messages、模型名、工具调用及响应的转换语义。当前用户未提出此兼容需求，本方案不附带实现。

HTTP 服务适配与 Plugin 保持组合关系：启动 Plugin Host → Install/Build Agent → 挂载 HTTP Handler → 启动宿主 http.Server。停止接收请求并等待执行结束后，再停止共享插件资源。

## 外部接口

Handler 实现 http.Handler，由宿主选择监听地址、路由前缀、HTTP 中间件与 http.Server 配置，不自动打开监听端口。

| 请求 | 行为 |
| --- | --- |
| POST /invoke | 请求 JSON 为 `{"input": ...}`，调用 Execute，成功返回 `{"run_id": "...", "output": <Output>}` |
| POST /stream | 相同请求结构，调用 ExecuteStream，以 text/event-stream 返回 AgentEvent 的稳定 JSON 表示 |

`input` 必须提供且非 null，允许字符串、对象或数组等 JSON 值。顶层未知字段及多个 JSON 文档返回 400。JSON 数字解码保留 json.Number，避免大整数静默失真。请求体默认上限 1 MiB，支持配置；超限返回 413。方法不匹配返回 405，未知路径返回 404。

SSE 帧采用 `event: <AgentEvent.Type>` 与 `data: <Event JSON>`，逐事件 flush。Event 保留 type、sources、step、tool、delta、usage、error 等已有可序列化字段，另带 run_id；output 使用下述 Output DTO。done.output 是最终结果，不能将 text 增量误当最终答案。error/done 为终态；底层流未产生终态就关闭时报告流异常。HTTP 响应提交前的启动错误使用 HTTP JSON 错误；提交后的运行错误使用 SSE error 帧，不能再改状态码。

未实现流式接口的 Runner 在 /stream 返回 501，不伪造流。/invoke 允许仅支持同步的客户端，不强制要求流式模型。

## Go 接口与 HTTP 数据边界

公开类型以以下职责为准，具体命名可由实现阶段在不改语义的前提下调整：

```go
type Runner interface {
    Execute(context.Context, any) (*builder.AgentOutput, error)
}
type StreamRunner interface {
    ExecuteStream(context.Context, any) (<-chan builder.AgentEvent, error)
}
type RunRequest struct {
    ID string
    Input any
    Source string // invoke、stream 或 webhook
    Webhook string // 入站接收器名称，非入站请求为空
}
type Cleanup func(context.Context) error
type Factory func(context.Context, RunRequest) (Runner, Cleanup, error)
```

每次合法输入生成独立 run_id（UUID），通过响应 X-GoAgent-Run-ID、JSON/SSE 和出站通知关联；该 ID 表示 HTTP 运行，不自动覆盖宿主会话或已有持久执行身份。HTTP 请求体不接受任意执行身份来覆盖宿主配置。

Output 显式包含 result、metadata、duration_ms、timestamp、token_usage；不直接序列化 AgentOutput.State，也不把 Go 导出字段名当作稳定 HTTP 字段。metadata/result 序列化失败须返回真实错误。error 响应使用 `{"run_id":"...","error":{"code":"...","message":"..."}}`，在生成运行 ID 前的解析错误可省略 run_id。

参数错误 400、超限 413、未知路由 404、方法错误 405、不支持流式 501、服务已关闭 503、执行超时 504、其他构造/运行错误 500；客户端断开后不强行写新响应。入站映射失败和非法请求不发送 run.started 或伪造运行终态。

默认请求体上限 1 MiB、执行超时 5 分钟、生产者退出等待与 cleanup 超时各 5 秒，均可配置；无效配置在构造 Handler 时返回错误。默认值由代理选择。通过已有 HTTP 中间件向 context 注入业务身份，Factory 可读取。

## 运行与资源约定

以请求级 Factory 创建 Runner，Factory 可捕获共享模型、已启动的 Plugin Host 和业务服务。Factory 同时返回可选 cleanup 函数；构建部分失败也执行已返回的 cleanup。cleanup 接收独立、有界的 context，避免请求取消阻断资源回收。

请求 context 传递给 Factory 和 Agent；客户端断开、HTTP 写入失败或配置的执行期限到达时取消执行。提供可配置请求执行超时，不启动脱离请求的后台任务或自动重试。Runner 和工具必须响应 context；Go 无法强制停止忽略 context 的宿主代码。

对于流式请求，必须确认事件生产者关闭后才释放 Runner 资源，不能只收到 done 就直接关闭；取消后有界等待生产者退出，超时仍由清理协调者等待其实际退出再回收，向宿主错误回调报告超时。共享 Plugin Host 不由请求 cleanup 关闭。

Handler 提供 Shutdown(ctx)：停止接受新执行，取消并等待已登记的运行与延迟清理；新请求返回 503。等待超时返回错误，不把仍在运行的生产者或未释放资源标为成功。宿主须先完成 HTTP 停服与 Handler.Shutdown，再关闭共享 Plugin Host；超时后不能提前释放仍被引用的共享资源。仅有同一个清理所有者可消费并回收流，防止取消分支与正常分支重复 cleanup。

HTTP 层不创建新的 SessionStore、调度器或工作流引擎。会话身份与持久执行由 Factory/宿主选择现有 Builder 和 execution.Scope 配置；不能把按请求创建 Runner 解释成自动多轮会话持久化。认证、租户识别和跨域策略可使用宿主已有 HTTP 中间件。

## Hook 扩展

用户新增要求：HTTP 服务同时支持 hooks。复用现有三层扩展，不将名称相似的接口混为一谈：

| 层级 | 扩展方式 | 作用与限制 |
| --- | --- | --- |
| HTTP 请求 | 标准 `func(http.Handler) http.Handler` 中间件 | 请求校验、上下文注入、请求前后处理；已提交的 SSE 响应不能改写状态码 |
| Agent 执行 | 现有 Callback、Middleware、PreStep、RequestHook、RequestErrorHook、TurnStopping | 通过 Factory 构建 Agent 时注入；Plugin.Contribution 已能贡献这些能力，遵循原有回放与错误语义 |
| 工具调用 | tools.Pipeline 的 Pre、Around、Post、Approver | 通过 Dispatcher 统一注册，沿用既有替代、审批、拒绝与持久调用行为 |

HTTP 适配器另提供请求级生命周期观察入口，用于通知 run.started/completed/failed/cancelled 和报告清理/投递失败；不借观察者改写 Agent 执行裁决。不将观察者错误当作重新执行 Agent 的理由。需要改变输入或输出时，在已有 HTTP 中间件或 Agent 中间件完成。

Hook 注册在宿主代码中；HTTP 请求不能提交 Go 函数、Shell 命令或任意远程 hook 配置。不同请求可并发使用同一 Hook，宿主 Hook 须并发安全且响应 context。各 Hook 在同步与流式路径上保持一致的运行身份和终态语义。

现有循环 Hook 有明确能力边界：PreStep、RequestHook、TurnStopping 主要作用于原生工具流，不能因为新增 HTTP 就声称同步 Execute 自动触发全部循环 Hook。HTTP 服务需公布该边界并用实际 Agent 测试验证。

## 双向 Webhook

用户已明确选择双向支持。以下收发与交付方式是待整体设计确认的具体方案。

### 入站：外部事件触发 Agent

- `POST /webhooks/{name}`，name 映射到宿主预注册的接收器。每个接收器验证并将业务 payload 转为与 /invoke 相同的 input，再经过统一 Factory、执行与资源释放流程。
- 不推测任意供应商的事件格式；宿主可对接订单、工单等 payload。未注册的接收器返回 404，映射失败返回 400，沿用请求大小上限。
- 默认在 HTTP 请求内同步执行并返回结果，断开或超时会取消；不返回没有真实入队的 202。需要立即确认、后台执行或重复投递去重时，由宿主接入其已有持久队列和事件去重存储，另行明确应答契约。

### 出站：Agent 执行通知外部系统

- 回调 URL、HTTP Client、请求头、超时及订阅事件由宿主配置。请求体不接受临时 callback_url，避免外部输入改变部署期的投递目标。
- 统一 JSON 事件信封：schema_version、event_id、run_id、event_type、occurred_at、data。类型包括 run.started、run.completed、run.failed、run.cancelled；终态与实际执行结果一致。需要工具级事件时复用 Dispatcher Hook，不能从普通文本猜测工具执行状态。
- 默认只投递终态事件，每个订阅目标对每次运行最多尝试一次终态通知；宿主需要重投同一事件时保留 event_id，接收方可据此去重。run.completed 携带最终 Output DTO，不把文本增量当最终答案。正常结果、错误或取消只选一种终态；结果确认后发生 HTTP 断开、cleanup 或通知错误，不追发另一个相矛盾的业务终态。
- 通知使用独立、有界的投递 context，请求取消后仍可尝试发送取消通知。投递非 2xx、超时及网络错误通过独立报告入口交给宿主；不会改变已经完成的业务结果，也不会重新执行 Agent 或工具。
- 默认一次、有界、同步投递，每个目标超时默认 5 秒并可配置；没有隐式后台通知任务或无限重试。此模式不承诺进程崩溃后的送达，不声称 exactly-once；如要求持久重试，应单独接入宿主 outbox/队列并验证，不能用内存任务冒充可靠交付。

## 验收与交付

- 先写失败测试，再实现；用真实 httptest.Server/HTTP 客户端验证 JSON、SSE、错误、取消和资源释放，不只断言内部 mock 调用。
- 覆盖同步执行、真实 Agent + Plugin 工具集成、无流式能力、部分构建失败、超大/非法输入、执行错误、异常结束、done 后生产者未关闭、客户端取消及并发请求隔离。
- 独立模块执行 go test -race 与 lint；主库不修改行为时验证导入边界。示例先通过临时工作区验证；依赖未发布前如需推送示例，固定到可获取的提交，不提交本机 replace 路径。
- Hook 测试覆盖调用顺序、拒绝/错误、并发隔离、同步/流式边界和取消；Webhook 覆盖真实 HTTP 接收/投递、业务 payload 映射、终态不重复、回调超时/失败、run_id/event_id 关联以及不重跑业务副作用。
- 补齐包 README、curl 调用示例、Plugin 装配示例、Webhook 与 Hook 接入示例和可重复验证记录。HTTP 验证使用实际本地监听端口；离线模型须明确标识，不声称模型线上质量验收。

## 非目标与交付保证

本轮提供 HTTP 传输、入站映射、出站通知与现有 Hooks 的组合，不新增 Agent 引擎、插件动态加载协议、远程代码执行接口、OpenAI/A2A 兼容接口或持久任务队列。双向 Webhook 不自动包含可靠后台投递、入站事件去重或异步任务轮询。

如果业务要求平台事件立即确认、进程崩溃后继续处理或可靠重投，必须另行选择持久队列/outbox 方案再扩展验收；当前设计不把这些保证隐含在“支持 Webhook”中。所有超时均要求宿主代码配合 context，不能承诺强制停止任意 Go 实现。

用户确认完整设计后，编写实施计划并执行。


## 设计复核

2026-10-31：按 brainstorming 的 Builder Check 流程进行独立只读审阅，结论为 `nothing important left`。HTTP/SSE、双向 Webhook、Hook 复用、取消清理、停服及通知失败不重跑业务的边界均可据此规划；其余内部实现选择由执行者决定。当前仍待用户确认完整设计后进入实施计划和产品代码阶段。
