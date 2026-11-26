# GoAgent Builder API 参考

[English](BUILDER_API_REFERENCE.md) | [中文](BUILDER_API_REFERENCE_CN.md)

`builder` 包的方法速查，按用途分组。签名以 `go doc github.com/kart-io/goagent/builder` 为准；设计说明见 [builder README](../../builder/README_CN.md)，入门见[快速入门](QUICKSTART_CN.md)。

## 创建

| 函数 | 说明 |
|---|---|
| `NewSimpleBuilder(client llm.Client) *SimpleAgentBuilder` | 常用入口，等价于 `NewAgentBuilder[*core.AgentState]` |
| `NewAgentBuilder[S core.State](client)` | 自定义状态类型 `S`；`S` 不是 `*core.AgentState` 时必须 `WithState` |
| `Build() (*ConfigurableAgent[S], error)` | 等价于 `BuildContext(context.Background())` |
| `BuildContext(ctx) (*ConfigurableAgent[S], error)` | 把宿主上下文传给初始化回调，拒绝 nil 或已取消的上下文，见[宿主接入](HOST_INTEGRATION_CN.md) |

状态只在进程内保留，Build 不加载也不保存状态；会话持久化使用 `WithSessionLog` 与 `WithDurableScope`。

## 基础配置

| 方法 | 默认值 | 说明 |
|---|---|---|
| `WithSystemPrompt(string)` | 空 | 系统提示词 |
| `WithTools(...interfaces.Tool)` | 无 | 追加工具 |
| `WithMaxIterations(int)` | 10 | 模型请求上限，包含最终回答；非正数忽略 |
| `WithTimeout(time.Duration)` | 5 分钟 | 单次执行超时，排队等待会话串行锁也计入 |
| `WithMaxTokens(int)` | 未设置，沿用客户端配置 | 模型最大输出 Token |
| `WithTemperature(float64)` | 未设置，沿用客户端配置 | 采样温度，支持显式 `0`；Builder 值优先于客户端配置 |
| `WithOutputFormat(OutputFormat)` | `OutputFormatDefault` | 在系统提示词后追加格式说明：`OutputFormatPlainText`、`OutputFormatMarkdown`、`OutputFormatJSON` |
| `WithCustomOutputFormat(string)` | 无 | 自定义格式说明（`OutputFormatCustom`） |
| `WithInputSerializer(InputSerializer)` | `DefaultInputSerializer` | 把任意输入转为提示文本 |
| `WithVerbose(bool)` | false | 追加日志与计时中间件 |

## 原生工具流

以下选项只作用于原生工具流：客户端实现 `llm.ToolStreamClient`（OpenAI、DeepSeek、Gemini、Anthropic Provider）、配置了工具，并通过 `AsRunnable`、`ExecuteStream` 或 `Start` 执行。

| 方法 | 说明 |
|---|---|
| `WithParallelToolCalls(n int)` | 同一轮工具调用并发上限；默认 0 按顺序。只有每个工具都实现 `interfaces.ConcurrentCallTool`（如 `Dispatcher` 的工具）时才并发 |
| `WithReturnDirectTools(names ...string)` | 单个终态工具的结果直接作为答案，省去总结请求；工具名须唯一注册 |
| `WithEmptyAnswerRetries(n int)` | 模型返回空回答时重问次数；默认 0，每次重问占用一次迭代额度 |
| `WithOffload(*contextpolicy.Offload)` | 超长工具结果换成引用，经读取工具分页读回；`offload` 须同时注册为工具，见[上下文治理](CONTEXT_POLICY_CN.md) |
| `WithPreStep(...PreStepHook)` | 每次模型请求前改写 `Step.Messages`，或以 `PreStepResult{Halt: true}` 直接结束 |
| `WithIdentifiedPreStep(identity, hook)` | 带身份的 PreStep；身份改变后恢复旧的持久运行以 `intent changed` 失败 |
| `WithRequestHook(...RequestHook)` | 发送前替换 `ModelRequest.Client` 或修改 `Request` 参数 |
| `WithRequestError(maxRecoveries int, ...RequestErrorHook)` | 模型请求失败时决定重试、替换消息或替换客户端；`maxRecoveries` 为单步恢复上限（默认 3） |
| `RetryTransient(retries, backoff)` | 原样重试 `llm.ClassifyError` 判定为暂时性的失败（429、5xx、连接错误）；已输出文本后不重试 |
| `WithTurnStopping(...TurnStoppingHook)` | 模型给出最终答复时返回非空字符串，作为 user 消息续跑 |
| `WithPromptSections(...PromptSection)` | 每轮开始时计算一次，进入 system，保持提示词缓存前缀稳定 |
| `WithPromptContexts(...PromptContext)` | 每步重算，作为一条 user 消息附在本次请求末尾，不进入历史 |
| `WithSessionLog(session.Store)` | 会话写入 [core/session](../../core/session/README_CN.md) 事件日志，模型历史从日志推导 |
| `WithTokenEstimator(TokenEstimator)` | 根预算开启 Token 上限时的请求 Token 预估 |
| `WithDurableScope(string)` | 持久执行必需：同一持久节点内每个逻辑调用的稳定名称 |

持久运行中，PreStep、TurnStopping、提示词段落与上下文按步骤记入日志并在恢复时回放；RequestError 的原样重试在同一条回合记录内进行，替换消息或客户端的决定以错误结束。

```go
offload, err := contextpolicy.NewOffload(contextpolicy.OffloadConfig{
    Store:              contextpolicy.NewMemoryStore(),
    MaxToolResultRunes: 8000,
})
if err != nil {
    return nil, err
}
return builder.NewSimpleBuilder(client).
    WithSystemPrompt("你是一个客服助手").
    WithTools(searchTool, offload). // offload 本身是读取工具，须一起注册
    WithMaxIterations(12).
    WithParallelToolCalls(4).
    WithOffload(offload).
    WithSessionLog(session.NewMemoryStore()).
    WithPromptSections(builder.PromptSection{
        Name: "rules",
        Text: func(builder.PromptScope) string { return "只回答与订单有关的问题。" },
    }).
    WithRequestHook(func(ctx context.Context, req *builder.ModelRequest) error {
        req.Request.Temperature = 0.2
        return nil
    }).
    WithRequestError(3, builder.RetryTransient(3, func(retry int) time.Duration {
        return time.Duration(retry) * 200 * time.Millisecond
    })).
    WithTurnStopping(func(ctx context.Context, s *builder.Stopping) (string, error) {
        if s.Answer == "" && s.Continued == 0 {
            return "请给出最终答复。", nil
        }
        return "", nil
    }).
    Build()
```

## 检索、记忆与状态

| 方法 | 默认值 | 说明 |
|---|---|---|
| `WithRetriever(interfaces.DocumentRetriever, ...RetrievalConfig)` | 4 篇、12000 字符 | 每次请求检索证据，来源写入输出元数据 `retrieval_sources`；`RetrievalConfig.Filter` 按请求生成元数据过滤条件，需检索器实现 `interfaces.RequestRetriever` |
| `WithSessionLog(session.Store)` | 无 | 多轮历史来自会话日志；不配置时每次调用没有历史 |
| `WithCompaction(*contextpolicy.Compaction)` | 无 | 对话超过阈值时压缩较早的消息；仅作用于原生工具流 |
| `WithLongTermMemory(*memory.Memory)` | 无 | 跨会话长期记忆：每轮召回、轮末登记抽取任务；需要 `WithSessionLog` 与请求上下文中的 `memory.WithNamespace`；召回仅作用于原生工具流；Request 钩子不得改写或移动召回消息 |
| `WithModelCache(builder.ModelCache)` | 无 | 模型调用缓存：以完整请求为键，`Scope` 声明可信的隔离身份（配置了长期记忆时默认取其命名空间）；`Erase` 为 `EraseOff` / `EraseAuto`（默认）/ `EraseRequired`；使用标签删除时 `TagKey` 至少 32 字节；`Build` 校验配置，见[缓存指南](CACHING_GUIDE_CN.md#模型调用缓存) |
| `WithSessionID(string)` | `session-<纳秒时间戳>` | 会话日志的缺省会话 ID；请求上下文中的 `tools.WithSessionID` 优先 |
| `WithState(S)` | `core.NewAgentState()` | 初始状态 |

需要进程重启后续跑的多步任务使用 `core/durable` 与 `workflow`，见[原生编排](NATIVE_ORCHESTRATION_CN.md)。

## 用户消息附带图片

把 `builder.UserInput`（或其指针）作为 `ExecuteStream` 的输入，即可在用户消息上附带图片；其他输入类型仍走配置的 `InputSerializer`。

```go
agent, err := builder.NewSimpleBuilder(client).
    WithSessionLog(sessions).
    WithImageResolver(resolve, builder.ImagePolicy{Identity: "images/v1", KeepRecentTurns: 3}).
    Build()
events, err := agent.ExecuteStream(ctx, builder.UserInput{
    Text:   "这张图里有什么？",
    Images: []llm.ImagePart{{Ref: "image:42", MIME: "image/png", SHA256: sum}},
})
```

- **存引用，不存 URL。** 历史、会话日志（含 `TurnStart.Images`）、持久化意图，以及 `PreStep` 与 Request 钩子看到的消息，都按传入原样保留 `llm.ImagePart`，通常只有 `Ref`、`URL` 为空。轮次身份是（`Text`、`Images`）这一对。
- **解析。** `WithImageResolver(r, policy)` 注册 `ImageResolver func(ctx, ImageRequest) (url string, err error)`。它只在构造出站模型请求时、每次真实请求之前调用，仅为该次请求填入 `URL`（https 地址或 `data:` URI）。已记录的轮次重放时不调用，模型调用缓存命中时也不再调用。多次调用 `WithImageResolver` 以最后一次为准；解析器为 nil 或策略上限为负，`Build` 返回 `CodeInvalidConfig`。
- **`ImageRequest`。** `Part` 是历史中记录的图片；`TurnAge` 从当前轮向前按会话轮次计数（0 为当前轮输入，1 为上一轮输入，依此类推）。轮次指会话日志的 `turn/start` 记录；引导、纠正、续跑、记忆召回与摘要都不开启新轮次。
- **错误。** 返回 `builder.ErrImageUnavailable`（可被包装）表示该引用永远解析不了，例如图片已删除或与摘要不符；该图片降级为占位文本，请求继续。其他错误视为暂时性错误：请求失败，并像提供方错误一样经过 RequestError 钩子。
- **没有解析器时。** 没有 URL 的图片或空图片部件，会在写会话日志之前以 `CodeInvalidInput` 终止运行；包括 `Ref` 与 `URL` 都为空的部件，以及未配置解析器时只有 `Ref` 的部件。解析器返回空 URL 时请求以 `CodeInvalidOutput` 失败。提供方对只有 `Ref`、`URL` 为空的部件也会拒绝。

`ImagePolicy` 字段：

| 字段 | 说明 |
|---|---|
| `Identity` | 标识解析器与策略配置（例如版本与编码设置）。它进入模型调用缓存键，不同配置之间不共用缓存的轮次；不属于持久化意图 |
| `KeepRecentTurns` | 只附带 `TurnAge` 不超过该值的用户轮次的图片，更早的图片变为占位文本；0 表示全部附带 |
| `MaxImagesPerRequest` | 单次请求附带图片数上限，保留最新的；0 表示不限 |
| `Placeholder` | `func(p llm.ImagePart, reason string) string` 生成追加到消息中、代替降级图片的文本；`reason` 为 `不可用`（`ErrImageUnavailable`）或 `不在当前上下文中`（超出 `KeepRecentTurns` 或 `MaxImagesPerRequest`）。为 nil 时文本为 `[图片 <ref>：<reason>]` |

**压缩。** `contextpolicy.CompactionConfig.ImageTokens` 是一张附带图片的估算 token 数，在 `Estimate` 之外为消息中的每张图片累加；0 表示 `constants.DefaultImageTokens`（1500）。交给摘要模型的转写里，每张图片以 `[图片 <ref>]`（没有 ref 时为 `[图片]`）追加在消息文本之后，摘要请求不携带图片。

**管理适配器。** native 管理适配器（`management/adapters`）对 JSON 字符串任务输入仍按纯文本处理。任务输入是含 `images` 键的 JSON 对象时，严格解码为 `builder.UserInput`：

```json
{"text": "这张图里有什么？", "images": [{"ref": "image:42", "mime": "image/png", "sha256": "..."}]}
```

`images` 必须是数组，每项 `ref` 非空；`mime`、`sha256` 可省略。图片项里出现 `url` 或出现任何未知键，都以 `CodeInvalidInput` 拒绝，出站地址只来自宿主的解析器。没有精确小写 `images` 键的对象不会被当作 `UserInput`，与其他 JSON 输入一样保持原行为，按普通值渲染。

提供方支持：

| 提供方 | 图片 |
|---|---|
| `OpenAIProvider` | 声明 `CapabilityVision`，图片作为图片部件发送 |
| MiniMax | 声明 `CapabilityVision`；带图片的请求发送 `max_completion_tokens`（视觉接口只读这个上限）而不是 `max_tokens` |
| Kimi、SiliconFlow、Ollama 预设 | 不声明视觉能力；图片部件照常转发，是否被接受取决于所选模型 |
| Anthropic | 拒绝 |
| 原生 Gemini | 拒绝 |
| DeepSeek | 拒绝 |

## 中间件、回调与元数据

| 方法 | 说明 |
|---|---|
| `WithMiddleware(...middleware.Middleware)` | `core/middleware` 中间件链，如 `NewLoggingMiddleware`、`NewTimingMiddleware`、`NewRateLimiterMiddleware` |
| `WithCallbacks(...core.Callback)` | 执行回调，如 `core.NewStdoutCallback`、`core.NewMetricsCallback` |
| `WithErrorHandler(func(error) error)` | 包装或替换执行错误 |
| `WithMetadata(key, value)` | 写入 Agent 元数据 |

## 场景预设与快速函数

| 方法 / 函数 | 配置内容 |
|---|---|
| `ConfigureForRAG()` | 计时中间件，MaxTokens 3000，Temperature 0.3 |
| `ConfigureForChatbot()` | 限流（每分钟 20 次）与输入长度校验，Temperature 0.8 |
| `ConfigureForAnalysis()` | 计时与输出结构化中间件，Temperature 0.1，MaxIterations 20 |
| `QuickAgent(client, systemPrompt)` | 仅系统提示词 |
| `ChatAgent(client, userName)` | 聊天提示词 + `ConfigureForChatbot` |
| `RAGAgent(client, retriever)` | `ConfigureForRAG` + 检索；`retriever` 可为 `DocumentRetriever`、带 `Retrieve` 方法的对象或 `VectorStore`（取前 4 条），nil 时只有提示词 |
| `AnalysisAgent(client, dataSource)` | `ConfigureForAnalysis`，数据源写入状态 |
| `WorkflowAgent(client, workflows)` | 日志、熔断与校验中间件，MaxIterations 15 |
| `MonitoringAgent(client, checkInterval)` | 限流与计时中间件，MaxIterations 100，Temperature 0.3 |
| `ResearchAgent(client, sources)` | 计时中间件，MaxTokens 4000，Temperature 0.5，MaxIterations 15 |

快速函数返回 `*SimpleAgent`（`ConfigurableAgent[*core.AgentState]`）。

## ConfigurableAgent

| 方法 | 说明 |
|---|---|
| `AsRunnable() *core.StreamRunnable[any, *AgentOutput]` | 推荐入口：`Invoke`、`Stream`、`Batch`、`Pipe`；接受任意 `llm.Client` |
| `ExecuteStream(ctx, input) (<-chan AgentEvent, error)` | 同一事件源的通道形式 |
| `Start(ctx, input) *Handle` | 进程内后台运行；`Handle` 提供 `Steer`、`Inject`、`Redirect`、`Followup`、`Interrupt`、`Wait`、`PendingInputs`。只支持原生工具流，不是持久后台调度服务 |
| `Initialize(ctx)`、`Shutdown(ctx)` | 生命周期；Build 已调用 Initialize |
| `GetState()`、`GetMetrics()` | 当前状态与指标 |

同一 Agent 的所有执行入口按会话串行；跨会话并发应创建独立 Agent。入口行为对照见[执行契约](EXECUTION_CONTRACT_CN.md)。

`Handle.PendingInputs() []builder.PendingInput` 返回未被循环领取的队列快照；每项有 `Kind`（steer/inject/redirect/followup）与 `Text`。同队列有序，跨队列不承诺全局顺序。查询不消费，已领取不代表处理成功。入队与快照不持久化；运行收敛后的剩余输入由宿主处置，见[组合指南](AGENT_COMPOSITION_CN.md)。

`Interrupt` 请求合作式取消；`Wait` 仍等待本轮执行源退出及同步工具清理，等待 context 超时只结束等待。持久 Followup 链必须由宿主按原批次重建，Start/WithResume 不自动恢复其调度。

## 自定义状态

```go
type CustomState struct {
    *core.AgentState
    Tenant string
}

agent, err := builder.NewAgentBuilder[*CustomState](client).
    WithState(&CustomState{AgentState: core.NewAgentState(), Tenant: "t1"}).
    WithSystemPrompt("你是一个企业助手").
    Build()
if err != nil {
    return err
}
fmt.Println(agent.GetState().Tenant)
```

## Agent 身份与用量

`WithUsageHook(budget.UsageHook)` 接收单次模型证据和可用的 Attribution 累计报告；只读、支持并发、向子 Agent 继承，子非 nil 钩子替换继承项。也可用 `budget.WithUsageHook` 绑定 context，不替换 Observer。详见[专用钩子契约](../design/TOKEN_USAGE_HOOK.md)。

`WithAgentID(id)` 设置逻辑角色，独立于 DurableScope；绑定预算会自动建立调用身份，未命名角色为 anonymous。`observe.Event.Agent/Usage` 与 model_attempt_start/model_usage 提供只读元数据；`budget.Config.MaxAgentCalls` 限制调用节点数，所有预算统一保存字段证据，`Controller.Attribution` 返回 Self/Inclusive/Root 及未知字段统计。`AsRunnable().Invoke` 与流式入口共用计量路径。见[多 Agent 用量指南](MULTI_AGENT_USAGE_CN.md)。

### 显式推理配置

`WithReasoning(usage.ReasoningConfig)` 覆盖当前 Agent 的请求配置。OpenAI 兼容/DeepSeek 使用 `Effort`；Gemini 使用受支持的 level，或 `BudgetSet: true` 与 `BudgetTokens`。不支持的客户端明确拒绝。每请求复制配置，RequestHook 修改本次设置不会污染默认值。

UsageHook 可读取 `Model.ResponseModel`、`Model.ReasoningConfigured`、`Model.Reasoning`、`Model.Tokens.ReasoningTokens`、`Model.Tokens.ToolUsePromptTokens`。先检查 `KnownFields`，不将可选细项重复加进 TotalTokens。详情见[可解释性契约](../design/TOKEN_USAGE_EXPLAINABILITY.md)。
