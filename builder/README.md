# builder Agent 构建器

`builder` 用流畅 API 构建 `ConfigurableAgent`，并实现原生工具循环、循环扩展点与运行句柄。方法与默认值速查见 [Builder API 参考](../docs/guides/BUILDER_API_REFERENCE.md)，入门见[快速入门](../docs/guides/QUICKSTART.md)，各执行入口的能力对照见[执行契约](../docs/guides/EXECUTION_CONTRACT.md)。

## 核心类型

- `AgentBuilder[C any, S core.State]`：泛型构建器，`C` 是应用上下文类型，`S` 是状态类型。`NewSimpleBuilder(client)` 返回 `SimpleAgentBuilder`（`AgentBuilder[any, *core.AgentState]`）。
- `ConfigurableAgent[C, S]`：`Build` / `BuildContext` 的结果。`Build` 校验配置，补齐默认的内存存储（未配置 `WithCheckpointer` 时不加载也不保存状态），组装中间件链并调用 `Initialize`。
- `AgentConfig`：配置项，`DefaultAgentConfig()` 给出默认值（MaxIterations 10、Timeout 5 分钟、MaxTokens 2000、Temperature 0.7、MaxConversationHistory 20、EnableAutoSave true）。

## 使用

```go
agent, err := builder.NewSimpleBuilder(client).
    WithSystemPrompt("你是一个专业的数据分析师").
    WithTools(calculatorTool, searchTool).
    WithMaxIterations(5).
    WithTimeout(2 * time.Minute).
    WithTemperature(0.5).
    WithOutputFormat(builder.OutputFormatJSON).
    WithMemory(memory.NewInMemoryManager(memory.DefaultConfig())).
    WithSessionID("session-001").
    WithMiddleware(middleware.NewTimingMiddleware()).
    Build()
if err != nil {
    return err
}
output, err := agent.AsRunnable().Invoke(ctx, "分析这些数据……")
```

输出格式：`OutputFormatPlainText`、`OutputFormatMarkdown`、`OutputFormatJSON` 在系统提示词后追加对应说明；`WithCustomOutputFormat("请以 YAML 格式输出结果")` 使用自定义说明。

场景预设 `ConfigureForRAG`、`ConfigureForChatbot`、`ConfigureForAnalysis` 与快速函数 `QuickAgent`、`ChatAgent`、`RAGAgent`、`AnalysisAgent`、`WorkflowAgent`、`MonitoringAgent`、`ResearchAgent` 的具体配置见 [API 参考](../docs/guides/BUILDER_API_REFERENCE.md#场景预设与快速函数)。

## 执行入口

| 入口 | 客户端要求 | 行为 |
|---|---|---|
| `AsRunnable().Invoke/Stream/Batch/Pipe` | `llm.Client` | 推荐入口；配置了工具且实现 `llm.ToolStreamClient` 时走原生工具循环 |
| `ExecuteStream` | `llm.Client` | 同一事件源的通道形式（`sources`、`step`、`text`、`usage`、`error`、`done`） |
| `Start` | `llm.ToolStreamClient` 且配置了工具 | 后台运行，返回 `*Handle` |

所有入口按会话串行执行，排队等待计入 Timeout；跨会话并发应创建独立 Agent。

`Build` / `BuildContext` 为生成的 Agent 保存配置快照，后续修改 Builder 的选项、工具列表、回调列表和顶层元数据不会追溯修改已有 Agent。客户端、状态、存储、记忆、回调对象及元数据中的业务对象仍按引用共享；跨会话应创建独立 Builder，并按需提供独立状态。

## 运行中交互与循环扩展点

以下能力只作用于原生工具流（`llm.ToolStreamClient` 且配置了工具）。

```go
h := agent.Start(ctx, "修复登录页的样式")
_ = h.Steer("顺便检查移动端")   // 当前工具批次结束后作为 user 消息追加；模型即将结束时唤醒续跑
_ = h.Inject("用户偏好深色主题") // 随下一次请求发送，不唤醒
_ = h.Redirect("先别改 CSS")     // 取消进行中的模型请求，保留已流出的文本后重新请求；工具执行中降级为 Steer
_ = h.Followup("再写个测试")     // 本轮结束后作为新一轮输入，续接对话
out, err := h.Wait(ctx)         // h.Interrupt(cause) 中止运行
pending := h.PendingInputs()   // 独立快照：仍在队列、尚未被执行循环领取的输入
```

入队成功不代表处理完成。Inject 不唤醒、Steer 到达过晚或额度耗尽时可能留下输入；失败时未领取的 Followup 也会保留。`PendingInputs` 不消费、不自动续跑，已领取但处理失败的输入不在其中。`Wait` 的 context 只控制等待；等待超时不代表 Handle 结束。收件箱是进程内对象，不提供跨重启可靠接收，详见[组合与输入契约](../docs/guides/AGENT_COMPOSITION.md)。

`Interrupt` 后，Handle 等待本轮 ExecuteStream 退出（包括其同步执行的工具与清理），再完成 Wait。宿主工具和钩子必须合作返回；需要限制等待时间时给 Wait 单独设置 context。取消原因与已收到的执行错误通过错误链保留，不把取消解释成业务效果已撤销。

| 选项 | 时机 | 能力 |
|---|---|---|
| `WithPreStep` / `WithIdentifiedPreStep` | 领取收件箱输入之后、发送请求之前 | 改写消息（如压缩），或以 `Halt` 不请求模型直接结束；带身份的版本在配置改变后拒绝恢复旧的持久运行 |
| `WithOffload` | 同 PreStep | 超长工具结果换成引用，见[上下文治理](../docs/guides/CONTEXT_POLICY.md) |
| `WithRequestHook` | 发送模型请求前 | 替换客户端（路由、换模型）、调整参数 |
| `WithRequestError` | 模型请求失败 | 依据 `llm.ClassifyError` 的分类决定重试、压缩（替换消息）或回退（替换客户端）；`RetryTransient` 是原样重试暂时性失败的组件 |
| `WithTurnStopping` | 模型给出最终答复、没有待处理的 Steer | 返回一条 user 消息续跑（目标驱动、Stop 钩子） |
| `WithPromptSections` | 每轮开始时计算一次 | 追加到 system，同一轮内字节不变，保持提示词缓存前缀 |
| `WithPromptContexts` | 每步重算 | 作为一条 user 消息附在本次请求末尾，不进入之后的历史 |

持久运行中，PreStep、TurnStopping、提示词段落与上下文、按步领取的 Steer/Inject 和唤醒决定记入日志，恢复时回放而不重新计算；以 `Start` 开始的持久运行须同样以 `Start` 恢复。持久运行中，`Steer`、`Inject`、`Redirect`、`Followup` 写入持久收件箱后才返回成功，写入失败时返回错误；`SteerOnce` 还按键去重，投递方崩溃后重投不产生第二条；用同一执行键再次 `Start` 会恢复未领取的输入，并按记录的批次继续 Followup 轮次，宿主不需要重建，也不应重复提交。Request 钩子须是确定性的；RequestError 的原样重试在同一条回合记录内进行，持久运行中替换消息或客户端的决定以错误结束；Redirect 只在非持久运行中生效（持久运行中降级为 Steer）。被进程停止或崩溃打断的模型请求默认不重发（可能已计费），恢复时报 `durable.ErrOperationAttention`；`WithInterruptedModelResend` 允许重发，同一回合最多发出 3 次，每次各占一份预算，开关须在运行的整个生命周期内保持一致。子流程等待宿主续跑时，执行以 `ErrWaiting`（`*WaitingError`）暂停，见[可组合编排](../docs/guides/COMPOSABLE_ORCHESTRATION.md)。

## 会话日志

`WithSessionLog(store)` 把会话记入 [`core/session`](../core/session/README.md) 的事件日志：模型历史从日志推导（不再读取记忆），每次模型请求前记录与日志不同的对话，PreStep 等对历史的改写记为 `history/rewrite`，因此每次请求都能由日志重建。持久运行可用 `session.DurableStore(store, id)` 作为 durable 日志，操作快照与对话位于同一份日志；`session.BuildTrajectory` 把日志导出为训练轨迹。只支持原生工具流。

## 代码结构

```text
builder/
├── builder.go              # AgentBuilder、Build/BuildContext
├── agent_options.go        # 提示词、状态、上下文、回调、元数据
├── config_options.go       # AgentConfig 与运行配置
├── runtime_options.go      # 存储、记忆、检查点、中间件
├── tool_options.go         # WithTools
├── configurable_agent.go   # 中间件链执行与生命周期
├── runnable.go             # AsRunnable
├── events.go               # ExecuteStream 与事件
├── tool_events.go          # 原生工具循环
├── loop_hooks.go           # PreStep、Request、RequestError、TurnStopping、提示词段落与上下文
├── handle.go               # Start 与 Handle
├── offload.go              # WithOffload
├── retrieval.go            # WithRetriever
├── session_log.go          # WithSessionLog
├── durable_turn.go         # 持久模型回合
├── model_call.go           # 模型调用计数与用量记录
├── model_budget.go         # WithTokenEstimator
├── memory_policy.go        # WithMemoryFailurePolicy
├── waiting.go              # ErrWaiting、WaitingError
├── presets.go              # ConfigureFor* 场景预设
├── quick_builders.go       # 快速创建函数
└── contextpolicy/          # Offload 与结果存储
```

## 多 Agent 用量钩子

`WithUsageHook(func(ctx context.Context, e budget.UsageEvent))` 直接获取逐尝试 Token 和可用的累计统计，父钩子向子 Agent 继承。无需自行筛选 Observer 事件；查询错误与回调 panic 不改变业务结果。未绑定预算时仍提供单次用量，详见下述指南。

`WithAgentID` 标识角色；子 Agent 使用父 context 共享预算与 Observer。model_attempt_start/model_usage 提供尝试键、父子身份和字段证据，可靠累计通过统一预算的 Attribution 查询。Self、Inclusive、Root 不可重复相加，见[指南](../docs/guides/MULTI_AGENT_USAGE.md)与[离线示例](https://github.com/kart-io/goagent-examples/tree/main/agent-usage)。

Token 统计钩子还可读取供应商报告的 `ReasoningTokens`、`ToolUsePromptTokens` 和 `ResponseModel`；先检查对应 `KnownFields`。通过 `WithReasoning(usage.ReasoningConfig{Effort: "low"})` 设置本 Agent 的显式推理控制，Gemini 思考预算使用 `BudgetSet: true` 与 `BudgetTokens`。不支持的客户端明确报错。见[用量指南](../docs/guides/MULTI_AGENT_USAGE.md)。
