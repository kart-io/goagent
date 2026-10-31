# GoAgent Builder API 参考

`builder` 包的方法速查，按用途分组。签名以 `go doc github.com/kart-io/goagent/builder` 为准；设计说明见 [builder README](../../builder/README.md)，入门见[快速入门](QUICKSTART.md)。

## 创建

| 函数 | 说明 |
|---|---|
| `NewSimpleBuilder(client llm.Client) *SimpleAgentBuilder` | 常用入口，等价于 `NewAgentBuilder[any, *core.AgentState]` |
| `NewAgentBuilder[C any, S core.State](client)` | 自定义上下文类型 `C` 与状态类型 `S`；`S` 不是 `*core.AgentState` 时必须 `WithState` |
| `Build() (*ConfigurableAgent[C, S], error)` | 等价于 `BuildContext(context.Background())` |
| `BuildContext(ctx) (*ConfigurableAgent[C, S], error)` | 把宿主上下文传给检查点初始化与回调，拒绝 nil 或已取消的上下文，见[宿主接入](HOST_INTEGRATION.md) |

未设置存储与检查点时，Build 使用内存存储与 `checkpoint.NewInMemorySaver()`。

## 基础配置

| 方法 | 默认值 | 说明 |
|---|---|---|
| `WithSystemPrompt(string)` | 空 | 系统提示词 |
| `WithTools(...interfaces.Tool)` | 无 | 追加工具 |
| `WithMaxIterations(int)` | 10 | 模型请求上限，包含最终回答；非正数忽略 |
| `WithTimeout(time.Duration)` | 5 分钟 | 单次执行超时，排队等待会话串行锁也计入 |
| `WithMaxTokens(int)` | 2000 | 模型最大输出 Token |
| `WithTemperature(float64)` | 0.7 | 采样温度 |
| `WithOutputFormat(OutputFormat)` | `OutputFormatDefault` | 在系统提示词后追加格式说明：`OutputFormatPlainText`、`OutputFormatMarkdown`、`OutputFormatJSON` |
| `WithCustomOutputFormat(string)` | 无 | 自定义格式说明（`OutputFormatCustom`） |
| `WithInputSerializer(InputSerializer)` | `DefaultInputSerializer` | 把任意输入转为提示文本 |
| `WithVerbose(bool)` | false | 追加日志与计时中间件 |

## 原生工具流

以下选项只作用于原生工具流：客户端实现 `llm.ToolStreamClient`（OpenAI、DeepSeek、Gemini Provider）、配置了工具，并通过 `AsRunnable`、`ExecuteStream` 或 `Start` 执行。

| 方法 | 说明 |
|---|---|
| `WithParallelToolCalls(n int)` | 同一轮工具调用并发上限；默认 0 按顺序。只有每个工具都实现 `interfaces.ConcurrentCallTool`（如 `Dispatcher` 的工具）时才并发 |
| `WithReturnDirectTools(names ...string)` | 单个终态工具的结果直接作为答案，省去总结请求；工具名须唯一注册 |
| `WithEmptyAnswerRetries(n int)` | 模型返回空回答时重问次数；默认 0，每次重问占用一次迭代额度 |
| `WithOffload(*contextpolicy.Offload)` | 超长工具结果换成引用，经读取工具分页读回；`offload` 须同时注册为工具，见[上下文治理](CONTEXT_POLICY.md) |
| `WithPreStep(...PreStepHook)` | 每次模型请求前改写 `Step.Messages`，或以 `PreStepResult{Halt: true}` 直接结束 |
| `WithIdentifiedPreStep(identity, hook)` | 带身份的 PreStep；身份改变后恢复旧的持久运行以 `intent changed` 失败 |
| `WithRequestHook(...RequestHook)` | 发送前替换 `ModelRequest.Client` 或修改 `Request` 参数 |
| `WithRequestError(maxRecoveries int, ...RequestErrorHook)` | 模型请求失败时决定重试、替换消息或替换客户端；`maxRecoveries` 为单步恢复上限（默认 3） |
| `RetryTransient(retries, backoff)` | 原样重试 `llm.ClassifyError` 判定为暂时性的失败（429、5xx、连接错误）；已输出文本后不重试 |
| `WithTurnStopping(...TurnStoppingHook)` | 模型给出最终答复时返回非空字符串，作为 user 消息续跑 |
| `WithPromptSections(...PromptSection)` | 每轮开始时计算一次，进入 system，保持提示词缓存前缀稳定 |
| `WithPromptContexts(...PromptContext)` | 每步重算，作为一条 user 消息附在本次请求末尾，不进入历史 |
| `WithSessionLog(session.Store)` | 会话写入 [core/session](../../core/session/README.md) 事件日志，模型历史从日志推导 |
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
| `WithRetriever(interfaces.DocumentRetriever, ...RetrievalConfig)` | 4 篇、12000 字符 | 每次请求检索证据，来源写入输出元数据 `retrieval_sources` |
| `WithMemory(interfaces.MemoryManager)` | 无 | 对话记忆；配置 `WithSessionLog` 时不再读取记忆 |
| `WithMemoryFailurePolicy(MemoryFailurePolicy)` | `MemoryFailureBestEffort` | `MemoryFailureStrict` 在记忆读写失败时返回错误 |
| `WithMaxConversationHistory(int)` | 20 | 加载的历史轮数，0 或负数不限制 |
| `WithSessionID(string)` | `session-<纳秒时间戳>` | 会话 ID，用于检查点与记忆 |
| `WithStore(store.Store)` | 内存存储 | 长期键值存储 |
| `WithCheckpointer(checkpoint.Checkpointer)` | `InMemorySaver` | 会话检查点 |
| `WithAutoSaveEnabled(bool)` | true | 执行后保存检查点 |
| `WithState(S)` | `core.NewAgentState()` | 初始状态 |
| `WithContext(C)` | 零值 | 应用上下文 |

需要进程重启后续跑的多步任务使用 `core/durable` 与 `workflow`，见[原生编排](NATIVE_ORCHESTRATION.md)。

## 中间件、回调与元数据

| 方法 | 说明 |
|---|---|
| `WithMiddleware(...middleware.Middleware)` | `core/middleware` 中间件链，如 `NewLoggingMiddleware`、`NewTimingMiddleware`、`NewCacheMiddleware`、`NewRateLimiterMiddleware` |
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
| `MonitoringAgent(client, checkInterval)` | 限流与缓存中间件，MaxIterations 100，Temperature 0.3 |
| `ResearchAgent(client, sources)` | 缓存中间件，MaxTokens 4000，Temperature 0.5，MaxIterations 15 |

快速函数返回 `*SimpleAgent`（`ConfigurableAgent[any, *core.AgentState]`）。

## ConfigurableAgent

| 方法 | 说明 |
|---|---|
| `AsRunnable() *core.StreamRunnable[any, *AgentOutput]` | 推荐入口：`Invoke`、`Stream`、`Batch`、`Pipe`；需要 `llm.StreamClient` |
| `ExecuteStream(ctx, input) (<-chan AgentEvent, error)` | 同一事件源的通道形式 |
| `Start(ctx, input) *Handle` | 后台运行；`Handle` 提供 `Steer`、`Inject`、`Redirect`、`Followup`、`Interrupt`、`Wait`。只支持原生工具流 |
| `Execute(ctx, input)` | 同步、不执行工具；配置 `WithOffload`/`WithReturnDirectTools`、持久执行或根预算时报错 |
| `ExecuteWithTools(ctx, input)` | 流式客户端委派 `AsRunnable().Invoke`，其他客户端保留旧工具循环 |
| `Initialize(ctx)`、`Shutdown(ctx)` | 生命周期；Build 已调用 Initialize |
| `GetState()`、`GetMetrics()` | 当前状态与指标 |

同一 Agent 的所有执行入口按会话串行；跨会话并发应创建独立 Agent。入口行为对照见[执行契约](EXECUTION_CONTRACT.md)。

## 自定义状态

```go
type CustomState struct {
    *core.AgentState
    Tenant string
}

agent, err := builder.NewAgentBuilder[any, *CustomState](client).
    WithState(&CustomState{AgentState: core.NewAgentState(), Tenant: "t1"}).
    WithSystemPrompt("你是一个企业助手").
    Build()
if err != nil {
    return err
}
fmt.Println(agent.GetState().Tenant)
```
