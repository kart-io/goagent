# Changelog

All notable changes to GoAgent will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Breaking

- 同轮工具并发改为显式开启：`WithParallelToolCalls` 默认 0，小于 2 时按顺序执行，与 master 一致；n>=2 时最多 n 个并发，不另设上限传 `math.MaxInt`。本分支早先的“默认并发”已撤回。迁移：依赖同轮并发的代码显式设置 `WithParallelToolCalls(n)`；原先为关闭并发而设置的 `WithParallelToolCalls(1)` 可以删除。并发时执行先后不保证与模型顺序一致（`Dispatcher.MaxConcurrency=1` 也一样），结果仍按模型顺序交还，某个调用失败不取消其他调用。
- `contextpolicy.ResultStore` 新增 `Release(ctx, runKey)`，自定义实现须补齐：删除该运行的全部证据，运行不存在时返回 nil。Builder 为非持久执行自动生成的结果作用域在执行结束后经新增的 `contextpolicy.Releaser`（`Offload` 已实现）自动释放；宿主经 `WithResultScope` 提供作用域（包括持久执行）时由宿主在运行不再恢复后调用 `Release`。没有持久日志时 Builder 不再用执行键作结果作用域，改用临时作用域。释放后该运行键不可复用：迟到的 `PutIfAbsent` 返回 `contextpolicy.ErrReleased`。`NewDurableResultStore` 的底层存储实现新增的 `durable.Scoped`（`durable.FileStore` 已实现）时才支持 `Release`：每次运行的证据写入独立作用域，`Release` 整体删除并留下一个墓碑以拒绝迟到写入（包括本机其他进程）；持久证据布局随之改为 `scopes/` 子目录，升级前写入的证据不再可读，进行中的持久运行须在升级前结束。读取证据不再加锁，读取不存在的引用不产生文件。已释放运行的 `Get` 返回 `contextpolicy.ErrReleased`（内存版与持久版一致，此前为 `os.ErrNotExist`），持久版 `Release` 写下墓碑后、删除证据前崩溃时，再次 `Release` 之前也读不到残留证据；`ErrReleased` 与 `durable.ErrScopeRemoved` 的文本改为“拒绝读写”。`durable.FileStore` 作用域的 `Load` 在墓碑存在时返回 `durable.ErrScopeRemoved`，仍不加锁、不产生文件；墓碑经 `PruneReleased`/`PruneRemoved` 回收后（残留证据一并删除）`Get` 返回 `os.ErrNotExist`。按 `os.ErrNotExist` 判断“已释放”的调用方需同时识别 `ErrReleased`。
- `contextpolicy.ResultStore` 新增 `PruneReleased(ctx, olderThan)`，`durable.Scoped` 新增 `PruneRemoved(ctx, olderThan)`，自定义实现须补齐：回收释放时间早于 `olderThan` 的墓碑（内存版按释放时间，`FileStore` 按墓碑文件 mtime，并在闸门排他锁内删除崩溃残留目录），重复 `Release` 不刷新释放时间。阈值内的迟到写入仍被拒绝；墓碑回收后该运行键迟到的写入会成功并可能留下残留，宿主应把 `olderThan` 设得大于最长的重试或恢复窗口并定期调用。`FileStore` 加锁后核对闸门仍是路径上的当前文件，回收删除闸门不破坏写入与释放的互斥。
- panic 统一转为 `*errors.PanicError`（`github.com/kart-io/goagent/errors`），可用 `errors.As` 取 `Value` 与 `Stack`。错误文本改为“<位置> 发生 panic”，例如 `工具 "op" 发生 panic`、`工具 "op" 的授权回调 发生 panic`、`工具 "op" 的结果回调 发生 panic`、`持久操作 发生 panic`，替代 `tool "op" panicked`、`operation panicked` 等；按旧文本匹配的代码需改用 `errors.As`。Dispatcher 与持久操作的错误文本仍不含 panic 值，`RunnableTool` 保持在文本中附带 panic 值。
- 同步 `Execute` 不再向调用方抛出中间件链（中间件、模型客户端）内的 panic，改为返回 `*errors.PanicError`，并先经过 errorHandler 与 `OnError`。errorHandler、`OnError`、`OnEnd`、`OnStart`（`Build`/`Initialize`）、`OnShutdown`（`Shutdown`）自身的 panic 同样转为该错误返回，但不再交给 errorHandler 或任何回调，也不再调用剩余回调。非流式客户端的 `ExecuteWithTools` 旧工具循环中（工具、模型、检索、记忆）的 panic 也转为该错误，与该循环的普通错误一样不经过 errorHandler 与回调；`ExecuteStream` 启动 goroutine 前（如持久能力检查）的 panic 转为返回的错误并释放执行权。迁移：调用方原先用 `recover` 接住这类 panic 的，改为检查返回的错误并用 `errors.As` 识别。
- core Runnable 层（`safeInvoke`/`RunnableFunc`、`core.StreamRunnable`、`TypedToDynamicAdapter`、`LifecycleManager` 的 Init/Start/Stop/HealthCheck）经 `PanicHandlerRegistry.HandlePanic` 得到的错误，现在总能用 `errors.As` 取到 `*errors.PanicError`（Op 为 `<component>.<operation>`），`observe.ErrorKind` 因此返回 `panic` 而非 `execution`。`DefaultPanicHandler` 仍返回同文本的 `*AgentError`（CodeInternal），其 `Cause` 改为该 `PanicError`，`Unwrap` 不再为 nil；自定义 `PanicHandler` 的接口不变、仍被调用并决定错误文本，返回值不含 `PanicError` 时由 registry 包装补上，因此不能再对结果做 `err.(*AgentError)` 等具体类型断言，需改用 `errors.As`；handler 返回 nil 时改为返回该 `PanicError`，不再吞掉 panic。传给 handler 与 logger 的调用栈来自该 `PanicError`；panic 值已是 `*PanicError` 时沿用它最初的调用栈。`core.StreamRunnable` 的 `OnStart`、`OnEnd`、`OnError` 自身 panic 时返回 `*PanicError`，不再调用剩余回调，也不再交给 `OnError`；此前 `OnError` 的 panic 会直接抛出使进程退出，`OnStart`/`OnEnd` 的 panic 还会再交给各回调的 `OnError`。执行源的 panic 仍交给 `OnError`。同样的语义（回调自身 panic 时返回 `*PanicError` 替代原结果，不再调用剩余回调，也不交给 `OnError`）扩展到所有调用 `core.Callback` 的位置：`RunnableFunc`、`BaseAgent`、`BaseChain`（`OnChainStart`/`OnChainEnd`/`OnChainError`，含 `Stream`）、`CallbackManager`（`OnStart`/`OnEnd`/`OnError`/`TriggerCallbacks`）、`retrieval.BaseRetriever`、`document` 各加载器，以及 ReAct、SoT、GoT、MetaCoT、PoT、ToT 与 `BaseReasoningAgent` 的回调（含 `OnLLM*`、`OnTool*`、`OnAgentFinish`）；此前这些位置的回调 panic 直接抛给调用方，`BaseChain.Stream` 中会使进程退出。`OnError`、`OnChainError`、`OnChainEnd` 等返回值被忽略的回调，其 panic 现在同样替代原错误返回；`BaseAgent.Invoke` 中 `OnAgentFinish` 的 panic 不再被 `ErrNotImplemented` 掩盖，`BaseChain.Stream` 中 `OnChainEnd` 的 panic 作为最终块的错误。新增 `core.NotifyCallbacks` 与 `CallbackManager.NotifyError` 供自定义组件复用这一语义。`LifecycleManager.HealthCheckAll` 中 HealthCheck panic 时，`HealthStatus.Message` 改为 `PanicError` 的文本（如 `lifecycle_manager.health_check 发生 panic`），`Details` 改为 `{"error_kind": "panic", "panic_op": <Op>}`，与 handler 无关；不再含 panic 值与调用栈（此前默认 handler 带 `panic_value`/`stack_trace`、自定义 handler 为空），二者由 `PanicLogger`/`PanicMetricsCollector` 接收。

### Added

- Builder 新增 `WithParallelToolCalls(n)`（默认 0 按顺序执行，n>=2 开启并发）与标记接口 `interfaces.ConcurrentCallTool`。

- Builder 新增 `WithEmptyAnswerRetries(n)`（默认 0，关闭时行为与日志不变）：模型以 stop 结束且无正文、无工具调用时，不追加空 assistant 消息，改为追加固定 user 消息重问；每次重问占用一次 MaxIterations 额度并消耗一次预算，持久模式下空回合按已完成记录、恢复时回放。超过次数或已是最后一轮仍按原方式报错。

- 新增 `tools.ArgumentErrorResult`，识别 Dispatcher 交还模型的参数错误结果。

- `DispatchConfig.OnResult` 在每次工具结果（首次执行、日志回放、交还模型的参数错误）后同步回调 `tools.DispatchResult`，宿主可在崩溃恢复后重建证据编号等状态；回调失败或 panic 中止本次调用，不影响日志。Builder 同轮并发执行时，回调经新增的 `tools.DeferResults` 推迟到整轮结束后按模型给出的顺序触发，首次执行与恢复回放顺序一致；宿主自行并发调用 `Invoke` 时顺序不确定，应从 `ExecutionKey` 派生编号。回调不受 `DispatchConfig.Timeout` 约束。

- 新增 `tools.DispatchTimeoutProvider` 与 `RunnableToolConfig.Timeout`，工具可声明经 Dispatcher 调用时的超时，子 Agent 不再受 30 秒默认值限制。

- 持久图新增 `workflow.WithBeforeApprove`（`CompileDurable` 增加可变参数 `opts ...DurableOption[T]`），`durable.Runner` 新增 `BeforeApprove`：在运行锁内、批准生效前核对外部数据，拒绝或 panic 时不改动检查点。

- 预算新增 `Controller.Remaining(ctx)`；nil 控制器返回 `budget.Unlimited`。

- 批量评测增加按预先声明任务契约选择直返/推理的模式，，原始答案标签不参与路由。

- 新增固定种子的 12 类批量评测、逐条结果持久化、默认与直返分组统计，以及千级执行边界和 SDK 协议回归；支持导入脱敏业务题集。

- Builder 新增显式 `WithReturnDirectTools`，单个终态工具成功后直接结束原生工具循环，省去模型总结请求；多工具保持常规循环，保留真实用量和完成处理。

- 新增可选 `llm.ToolStreamClient` 与 OpenAI 原生工具流，Builder 自动复用工具对话轮次直接回答，消除额外 READY/答案请求；去除流式请求中的重复系统提示，保留原生消息与工具调用 ID。

- 新增 `core.NewStreamRunnable` 与 Builder `AsRunnable()`，同步、流式、Batch 和 Pipe 绑定同一事件执行源，提供流式能力声明与类型化来源。
- 新增 `evaluation/agentbench`，提供请求预算、实际 SDK 协议检查、批量评测和历史失败记录。

- Builder retrieval integration via `interfaces.DocumentRetriever` and `WithRetriever`, with Unicode-safe evidence limits and numbered source metadata.
- OpenAI `CompleteStream` and `ChatStream` with structured deltas, terminal usage, and asynchronous error propagation.
- Real streaming agent support for structured and token-only clients, including upstream cancellation on reader close.

### Changed

- `observe.ErrorKind` 新增类别 `panic`：错误链中可用 `errors.As` 识别出 `*errors.PanicError` 时返回它，优先于其他类别；此前 panic 归为 `execution`。现有类别为 `canceled`、`deadline`、`waiting`、`attention`、`panic`、`execution`。panic 结束的观测仍为 `<类型>_error` 事件、span 状态为 Error（描述为 `panic`），Event 与 span 都不含 panic 值和调用栈。按 `ErrorKind == "execution"` 统计失败的观测方需同时计入 `panic`。

- `ExecuteStream`（及 `AsRunnable`）执行 goroutine 内的 panic（上下文策略、工具、中间件、检索、记忆、回调、模型客户端等）不再使进程退出：转为 `*errors.PanicError`，经 error 事件交给调用方，agent 观测以失败结束，调用方不再先收到 `io.ErrUnexpectedEOF`。模型调用的 panic 不再重新抛出，同样转为该错误。中间件链（中间件、模型、工具）内的 panic 在交给 errorHandler 与 `OnError` 前即转为该错误，与普通错误走同一路径。框架自己启动、会调用宿主代码的其他 goroutine 同样不再因 panic 使进程退出：有错误出口的转为 `*errors.PanicError` 交给该出口，没有的只交给全局 `PanicHandlerRegistry` 记录（指标与日志）。具体为：`BaseChain` 的步骤（`Invoke` 与 `Stream`，步骤 panic 时不论 `StopOnError` 都以 `failed` 终止本链并交给 `OnChainError`）、`BaseRunnable.Batch` 的 invoker、`RunnablePipe`/`RunnableSequence` 调用下游 `Stream`、`core.ToChannel` 的 Generator；`tools.ToolExecutor` 的工具与 errorHandler（作为该调用的错误结果，panic 不重试，顺序执行同样返回该错误）；`distributed.CircuitBreaker` 的 `OnStateChange`（只记录）；stream 包的 `Multiplexer`（`AddConsumer` 中 `OnStart` 的 panic 作为返回的错误、消费者不加入；`OnChunk`/`OnError` 的 panic 使该消费者停止接收，其他消费者照常，`Start` 返回这些 panic；`OnError`/`OnComplete` 的 panic 只记录；同一消费者的回调串行执行，任一回调 panic 后不再收到任何回调，包括已触发的 `OnError`/`OnComplete`。源结束（含 EOF）时 `Start` 不再等到 ctx 结束（默认流超时 5 分钟）才返回，而是在各消费者处理完已缓冲的数据与 `OnError`/`OnComplete` 后返回；`OnError`/`OnComplete` 改在该消费者自己的处理协程中、排在它已缓冲的数据块之后调用，此前在独立 goroutine 中触发，可能早于数据块；ctx 结束时不再处理缓冲数据，消费者收到 `OnError`(ctx 的错误) 与 `OnComplete` 后 `Start` 返回；被 `RemoveConsumer`/`Close` 移除的消费者不再收到结束回调，`Close` 也不再等待结束回调；`Start` 运行中 `AddConsumer` 加入的消费者从加入时起接收后续数据并照常收到 `OnError`/`OnComplete`，`Start` 等它处理完才返回，此前它没有处理协程，收不到数据块，只在独立 goroutine 中收到结束回调；源结束后（即使 `Start` 尚未返回）加入的消费者留给下一次 `Start`）、`StreamManager.Process`（handler panic 作为返回的错误，不再调用 `OnError`/`OnComplete`）与 `Transform`/`Filter`、`Writer` 的 `TransformFunc`、各流中间件与 `DataPipelineAgent`/`StreamingLLMAgent` 的处理 goroutine（作为流错误写出；`TransformMiddleware` 不再把转换函数同时设为 writer 的 `TransformFunc`，每个块只转换一次，转换 panic 的错误块因此能写出）、`TeeMiddleware` 的消费者（只记录，此后不再投递给它）、`MultiModeStream` 的 Callback 与 `FilterStream`/`TransformStream`（只记录，该模式或输出流随之结束）；以及检索（`EnsembleRetriever`、`HybridRetriever`）、`toolkits.ToolkitExecutor` 并行执行、MCP `StandardToolBox.ExecuteBatch` 与 `StandardExecutor.ExecuteWithTimeout`、`AggregatedSearchEngine`、`multiagent` 的并行/层级协作、共识投票与消息路由、`performance.AgentPool` 工厂与 `BatchExecutor`、`ContextEnrichmentMiddleware` 异步 enricher、`planning.AgentExecutor` 步骤、`reflection` 后台反思、`HierarchicalMemory` 后台整理、`devtools.Recorder` 的 Writer（由 `Close` 返回），`checkpoint.DistributedCheckpointer` 的异步复制与健康检查把后端 panic 按普通失败处理（复制忽略、记为不健康）。`core.InterruptManager` 的 `OnInterruptCreated`/`OnInterruptResolved` 钩子改在锁外调用，钩子内可以调用 manager 的方法（此前 `onResolved` 在持锁时调用，会死锁）；`onResolved` 在响应交给等待中的 `CreateInterrupt` 之前执行，`CreateInterrupt` 返回时它已执行完；`onResolved` 的 panic 由 `RespondToInterrupt` 作为 `*errors.PanicError` 返回，等待方照常收到响应，`onCreated` 的 panic 由 `CreateInterrupt` 返回并撤销该中断；中断的状态快照改在登记之前保存，中断对外可见时快照已经写入。`CancelInterrupt` 后 `CreateInterrupt` 返回 `CodeContextCanceled` 错误，此前返回 nil 响应与 nil 错误，`ExecuteWithInterrupts` 随即解引用 nil 而 panic；`CreateInterrupt` 无论收到响应、被取消、超时还是 ctx 结束，返回时都清理该中断的内部状态，`ListPendingInterrupts` 不再列出已失效的中断，之后的 `RespondToInterrupt` 返回 not found，等待方退出前已被接受的响应以该响应为准；重复响应同一中断返回 `CodeInvalidInput` 错误，不再覆盖响应并再次调用 `onResolved`；`onCreated` 执行期间到达的响应先暂存，`RespondToInterrupt` 立即返回，`onResolved` 由 `CreateInterrupt` 在 `onCreated` 完成后调用（其 panic 只记录），对同一中断 `onCreated` 一定先于 `onResolved` 完成，`onCreated` 内响应同一中断也不会死锁；`onCreated` panic 时期间暂存的响应随中断一起丢弃，不调用 `onResolved`。`CreateInterrupt` 传入的 ID 与尚未结束清理的中断重复时立即返回 `CodeAlreadyExists` 错误，不覆盖原中断的登记与状态快照、不调用 `onCreated`，此前会覆盖原登记，原等待方收不到响应只能等到超时；checkpointer 的 `Save` panic 时 `CreateInterrupt` 返回 `*errors.PanicError` 并释放占用的 ID；自动生成的中断 ID 改为 `interrupt_<UUID>`，此前由秒级时间戳与进程内计数器组成，进程在同一秒内重启后会重复，多个进程共用 checkpointer 时快照 key 冲突；已结束的中断的 ID 可以复用。需要在等待期间响应的调用方应预先设置 `Interrupt.ID`，`CreateInterrupt` 不回写传入对象。新增 `core.CallSafely`、`core.RecoverPanic`、`core.ReportPanic` 供自定义组件在调用宿主代码或自起 goroutine 时复用。

- `DispatchConfig.Timeout` 改为单次调用的默认超时，工具声明的超时替代它；计时从取得并发槽位开始，不再包含排队。需要限制排队加执行总耗时的调用方应在 ctx 上设置截止时间。
- 原生工具循环的结构化工具结果不再做 HTML 转义，`<`、`>`、`&` 原样交给模型与直接答案，也不带尾随换行。结果含这些字符且运行到一半的持久运行，恢复时后续模型回合的意图摘要会变化而被拒绝；升级前需排空这类运行。

- 直接返回工具遇到 Dispatcher 交还的参数错误时，不再中止，改为按普通工具结果交还模型改正并继续循环；最后一轮仍传错时按 MaxIterations 耗尽报错并附带参数错误。其他失败仍中止。

- 持久模式的无效工具计划按原因分别报错（缺少 ID、ID 重复、未提供的工具、参数不是 JSON 对象）并带工具名，替代统一的 `model returned invalid tool plan`；日志仍只记录失败类别。

- Builder 原生工具循环在已有工具结果且只剩最后一次请求时停止提供工具，将额度用于最终回答，避免无预算收尾的重复调用；应用评测提示区分原样返回和证据推理。

- 原生工具循环中的字符串结果直接传入工具正文，避免额外 JSON 引号/转义干扰模型原样输出；结构化结果继续使用 JSON。

- OpenAI 工具请求采用 `tools/tool_calls`，保留旧 `function_call` 响应兼容；支持并发工具分片合并、用量传递和流中错误。仅支持旧 `functions` 请求的服务需要升级端点。
- 自反思任务必须显式提供 `WithTaskExecutor`，不再返回模拟成功结果；各执行入口委派给同一真实执行器。
- GoT 非极简模式构图时，一轮扩展没有新增节点（LLM 失败、输出无法解析或全部被剪枝）即停止构图，不再对同一批候选反复调用 LLM 空转到 ctx 截止（无截止时间时永不返回，master 上已存在）；LLM 持续失败时 `Invoke` 在两次调用后返回该错误。构图期间 ctx 被取消时 `Invoke`、`InvokeFast`、`RunGenerator` 返回 ctx 错误，不再带着半张图继续执行。

- `RAGAgent` now uses supplied retrievers or vector stores. Unsupported non-nil inputs fail validation; nil preserves the prompt-only preset.
- `ConfigureForRAG` no longer caches answers implicitly or rewrites the question as context. Applications should configure caching explicitly when evidence freshness and session isolation are accounted for.
- ReAct `Stream` now emits intermediate reasoning/tool snapshots and one terminal chunk. `Invoke`, `InvokeFast`, `Stream`, and `RunGenerator` share the execution loop; consumers stopping early must cancel their context.

- 推理 Agent 的工具回调 panic、或工具返回 `*errors.PanicError` 时中止本次执行并以失败结束，不再写入 observation 继续调用模型（ReAct、CoT、ToT、MetaCoT）；各推理 Agent 的 `Stream` 与 GoT/SoT 并行 worker 在 goroutine 边界恢复宿主 panic 并转为 `*errors.PanicError`，不再使进程退出。
- `core.InterruptManager`：`RespondToInterrupt` 传入 nil 响应返回 `CodeInvalidInput`，此前在持锁时 panic 并永久占住锁、整个 manager 卡死；保存状态快照期间的 `CancelInterrupt` 不再被静默丢弃，`CreateInterrupt` 在保存完成后不再登记并返回 `CodeContextCanceled`；内部状态改为每个 ID 一个条目，所有加锁处统一 defer 解锁；已取消但等待方尚未退出的 ID 不可复用。
- `stream.Multiplexer` 被 `RemoveConsumer`/`Close` 移除的消费者不再收到缓冲数据与结束回调；错误计数改为原子操作，消除数据竞争；`AddConsumer` 改在锁外调用 `OnStart`，回调中可以调用 `Stats`/`Consumers`/`Close`。`TeeMiddleware` 对同一消费者按顺序串行回调，panic 后不再调用它。
- `tools.DeferResults`：推迟的调用改由 deliver 结束观测，结果回调失败时记为 `tool_error`，此前 invoke 返回时即按成功结束；`AsRunnable` 不再推迟，避免延迟的 `tool_end` 返回 `context canceled`；未 deliver 的调用在 `DeferResults` 的 ctx 结束时放弃交付并以该错误结束观测。
- 子进程测试统一设置截止时间：新增 `panictest.CombinedOutput`/`panictest.ChildTimeout`，超时即杀掉并回收子进程、输出可定位的错误，测试超时退出时不再遗留孤儿进程。
- panic 转成的错误文本不再包含 panic 值与调用栈：默认 `PanicHandler` 的 Message 固定为“panic 已恢复”，不再向 Context 写入 `panic_value`/`stack_trace`；`RunnableTool`、`durable.Runner` 与 `workflow.DurableGraph` 的批准前置校验改为返回 `*errors.PanicError`。panic 值与调用栈只能用 `errors.As` 取 `*errors.PanicError` 读取，按旧文本（`panic recovered`、文本含 panic 值）匹配的代码需改用 `errors.As`；自定义 handler 的错误文本同样不应包含二者。
- `CallbackManager`、`Container.Resolve`、`EnhancedPluginRegistry` 与 `PluginRegistry.Register` 改为在锁外调用宿主回调、工厂与插件方法，回调或工厂中增删回调、解析其他服务、访问注册表不再死锁；`CallbackManager` 本轮只通知复制下来的回调，增删从下一轮生效；`Container` 并发首次解析可能各调用一次工厂，先写入者胜出。
- `core.InterruptManager.CreateInterrupt` 传入 nil 中断返回 `CodeInvalidInput`，此前对 nil 解引用 panic。
- `core.InterruptManager.CancelInterrupt` 对不存在或已清理的 ID 返回 `CodeAgentNotFound`（与 `RespondToInterrupt` 一致），此前返回 nil，调用方无法区分是否真的取消。
- `mcp/remote` 的 stdio 互操作测试改用 `exec.CommandContext` 并设置 `WaitDelay`，超时后子进程被杀掉并回收。
### Deprecated

- `interfaces.Agent/Runnable` 为旧消息协议，新增编排统一使用 `core.Runnable[I,O]`；现有 API 保留迁移路径。
- 旧 `CrossEncoderReranker` 名称改为准确的 `LexicalOverlapReranker`，不代表实际交叉编码器推理。

⚠️ **Cache API Deprecation** - 简化缓存实现，减少过度设计

以下缓存函数已标记为 **Deprecated**，将在 v2.0.0 中移除：

- `cache.NewInMemoryCache()` - 请改用 `cache.NewSimpleCache()`
  - 理由：实现过于复杂，包含不必要的特性（maxSize、cleanupInterval 参数）
  - 迁移：将 `NewInMemoryCache(100, 5*time.Minute, 1*time.Minute)` 改为 `NewSimpleCache(5*time.Minute)`

- `cache.NewLRUCache()` - 请改用 `cache.NewSimpleCache()`
  - 理由：LRU 驱逐逻辑在实际场景中很少使用，SimpleCache 基于 TTL 的方式更简单有效
  - 迁移：将 `NewLRUCache(100, 5*time.Minute, 1*time.Minute)` 改为 `NewSimpleCache(5*time.Minute)`

- `cache.NewMultiTierCache()` - 请改用 `cache.NewSimpleCache()`
  - 理由：多级缓存在单进程应用中过于复杂，实际使用场景有限
  - 迁移：单进程应用使用 `NewSimpleCache()` 即可，分布式场景建议使用 Redis + 本地缓存方案

**向后兼容性**：所有 deprecated 函数在 v1.x 版本中仍然可用，不会影响现有代码。

**迁移指南**：
- 完整的迁移指南请参考 [`docs/guides/CACHING_GUIDE.md`](docs/guides/CACHING_GUIDE.md)
- 扫描工具：`go run tools/migrate-cache.go scan ./...`
- 自动迁移：`go run tools/migrate-cache.go replace ./...`

**相关变更**：
- 已将 `tools/middleware/caching.go` 从 `NewLRUCache` 迁移到 `NewSimpleCache` [commit: d7bd117]
- 已更新示例代码 `examples/integration/langchain-inspired/langchain_demo.go` 使用 `SimpleCache`
- 新增文档 `docs/guides/CACHING_GUIDE.md` - 详细的缓存使用指南
- 更新文档 `docs/guides/TOOL_MIDDLEWARE.md` - 推荐使用 `SimpleCache`

### Performance

- **utils/parser.go**: Pre-compile regular expressions to improve performance by 60-87%
  - Added 13 package-level pre-compiled regex variables
  - Optimized `RemoveMarkdown()` to avoid compiling 8 regexes on every call (85% faster)
  - Optimized `ExtractJSON()`, `ExtractList()`, `ExtractAllCodeBlocks()` methods (50-95% faster)
  - Added regex cache for dynamic patterns in `ExtractCodeBlock()`, `ExtractKeyValue()`, `ExtractSection()`
  - Reduced memory allocations by ~40-50%
  - Added comprehensive benchmark tests in `utils/parser_bench_test.go` (20 benchmarks)
  - Excellent concurrent performance: 2.5-9.6x speedup on 28-core CPU
  - Eliminated all staticcheck SA6000 warnings
  - See `/tmp/goagent-regex-analysis/performance_report.md` for detailed analysis

### Added - Advanced Reasoning Patterns

#### New Agent Types
- **Chain-of-Thought (CoT)** - Step-by-step reasoning with justification
  - Zero-shot and few-shot modes
  - Configurable reasoning depth
  - Self-verification support
  - Location: `agents/cot/`

- **Tree-of-Thought (ToT)** - Multi-path reasoning exploration
  - Multiple search strategies: DFS, BFS, Beam Search, MCTS
  - Configurable branching factor and depth
  - Thought evaluation and pruning
  - Parallel path exploration
  - Location: `agents/tot/`

- **Graph-of-Thought (GoT)** - DAG-based complex reasoning
  - Directed Acyclic Graph structure
  - Parallel node execution with topological sorting
  - Cycle detection
  - Multiple merge strategies (vote, weighted, LLM)
  - Location: `agents/got/`

- **Program-of-Thought (PoT)** - Code generation and execution
  - Multi-language support (Python, JavaScript, Go)
  - Safe mode with sandboxing
  - Code validation
  - Execution result capture
  - Location: `agents/pot/`

- **Skeleton-of-Thought (SoT)** - Fast long-form content generation
  - Skeleton generation and parallel elaboration
  - Dependency-aware scheduling
  - Multiple aggregation strategies (sequential, hierarchical, weighted)
  - Timeout control
  - Location: `agents/sot/`

- **Meta-CoT / Self-Ask** - Deep analysis with self-refinement
  - Automatic question decomposition
  - Recursive sub-question solving
  - Self-critique and improvement
  - Confidence estimation
  - Evidence collection and verification
  - Location: `agents/metacot/`

#### Builder API Extensions
- `WithChainOfThought()` - Create CoT agents
- `WithZeroShotCoT()` - Zero-shot CoT preset
- `WithFewShotCoT(examples)` - Few-shot CoT with examples
- `WithTreeOfThought()` - Create ToT agents
- `WithDFSToT()`, `WithBFSToT()` - Search strategy presets
- `WithBeamSearchToT(width, depth)` - Beam search configuration
- `WithMCTSToT(iterations)` - MCTS configuration
- `WithGraphOfThought()` - Create GoT agents
- `WithProgramOfThought()` - Create PoT agents
- `WithSkeletonOfThought()` - Create SoT agents
- `WithMetaCoT()` - Create Meta-CoT agents
- Location: `builder/reasoning_presets.go`

### Infrastructure

#### CI/CD
- **GitHub Actions Workflows**:
  - `ci.yml` - Continuous integration (test, lint, build)
  - `release.yml` - Automated releases on tag push
  - `pr.yml` - Pull request validation with coverage reporting
  - `nightly.yml` - Nightly builds and benchmarks
- **Automated Release Process**:
  - Multi-platform binary builds (Linux, macOS, Windows)
  - SHA256 checksum generation
  - Automatic GitHub Release creation
  - pkg.go.dev publication
- **Release Management**:
  - `scripts/create_release.sh` - Interactive release helper script
  - `.github/RELEASE.md` - Comprehensive release documentation
  - Pre-release support (alpha, beta, rc)

### Documentation

- **Comprehensive Agent Documentation** (`agents/README.md`):
  - Quick comparison table for all reasoning patterns
  - Decision tree for pattern selection
  - Detailed usage examples for each pattern
  - Configuration options reference
  - Performance comparison
  - Best practices guide
  - Testing instructions
- **Release Management Guide** (`.github/RELEASE.md`):
  - Semantic versioning guidelines
  - Step-by-step release process
  - Emergency hotfix procedures
  - Version compatibility matrix
  - Troubleshooting guide

### Changed

- **Import Layer Compliance**: Fixed Layer 1 import violations in `interfaces/` package
- **Test Suite**: Added comprehensive test coverage for all new reasoning patterns
- **Mock Implementations**: Improved test mocks for better interface compliance

### Fixed

- Import layering violations in `interfaces/memory.go` and `interfaces/tool.go`
- Test compilation errors in reasoning agent test files
- Mock setup issues in test suites

## [1.0.0] - 2025-11-15

### Added - Core Framework

#### Phase 1: Foundation
- **State Management** - Thread-safe state management with `core/state.go`
- **Runtime & Context** - Runtime environment and context propagation
- **Store System** - Long-term storage with hierarchical namespaces
  - InMemoryStore implementation
  - RedisStore for distributed systems
  - PostgresStore for persistent storage
- **Checkpointer** - Session persistence and recovery
  - InMemoryCheckpointer
  - RedisCheckpointer for distributed checkpointing
  - DistributedCheckpointer with high availability

#### Phase 2: Middleware & Business Logic
- **Middleware Framework** - Extensible middleware architecture
- **Advanced Middleware**:
  - DynamicPromptMiddleware - Dynamic prompt enhancement
  - ToolSelectorMiddleware - Intelligent tool selection
  - RateLimiterMiddleware - Rate limiting protection
  - AuthenticationMiddleware - Identity verification
  - ValidationMiddleware - Input validation
  - TransformMiddleware - Data transformation
  - CircuitBreakerMiddleware - Circuit breaker pattern
  - CacheMiddleware - Response caching
- **LLM Abstraction** - Multi-provider LLM support
  - OpenAI integration
  - Google Gemini integration
  - DeepSeek integration
- **Memory Management** - Conversation and case-based memory

#### Phase 3: Advanced Features
- **Agent Builder** - Fluent API for agent construction
- **Pre-configured Agent Templates**:
  - QuickAgent - Simple agent creation
  - RAGAgent - Retrieval-augmented generation
  - ChatAgent - Conversational agents
  - AnalysisAgent - Data analysis (low temperature, high precision)
  - WorkflowAgent - Workflow orchestration
  - MonitoringAgent - System monitoring
  - ResearchAgent - Research and information gathering
- **Vector Database** - Memory-based vector storage and RAG retrieval
- **Tool System**:
  - Parallel tool execution with worker pool
  - Tool dependency graph with topological sorting
  - LRU cache with TTL support
  - Tool registry
- **Stream Processing**:
  - Stream manager with buffering
  - Stream multiplexing
  - Rate limiting for streams
  - Stream transformations

#### Enterprise Features
- **OpenTelemetry Integration**:
  - Distributed tracing with W3C Trace Context
  - Metrics collection
  - Agent-specific tracer API
  - HTTP and NATS carrier propagation
- **Multi-Agent Communication**:
  - MemoryCommunicator for local communication
  - NATSCommunicator for distributed systems
  - Message routing with pattern matching
  - Session management
- **Observability Middleware** - Integration with tracing and metrics

### Architecture

- **4-Layer Architecture**:
  - Layer 1: Foundation (interfaces, errors, cache, utils)
  - Layer 2: Business Logic (core, LLM, memory, storage)
  - Layer 3: Implementation (agents, tools, middleware)
  - Layer 4: Examples and Tests
- **Import Layering** - Strict import rules to prevent circular dependencies
- **Verification Tools** - Automated import layering verification script

### Documentation

- **User Guides**:
  - Quick Start Guide
  - LangChain Integration Guide
  - LLM Provider Documentation
  - Migration Guide
  - Production Deployment Guide
- **Architecture Documentation**:
  - Architecture Overview
  - Import Layering Rules
  - Import Verification Guide
- **Development Documentation**:
  - Testing Best Practices
  - Test Coverage Reports
  - Contributing Guidelines
- **Examples**:
  - Basic usage examples
  - Advanced patterns
  - Integration examples
  - Streaming examples
  - Observability examples
  - Multi-agent examples

### Testing

- **Test Coverage**: >80% overall coverage
- **Test Suites**:
  - Unit tests for all core components
  - Integration tests for complex workflows
  - Benchmark tests for performance
- **Testing Tools**:
  - Mock implementations for testing
  - Test helpers and utilities

### Performance

- Builder construction: ~100μs/op
- Agent execution: ~1ms/op (excluding LLM calls)
- Middleware overhead: <5%
- Parallel tool execution: Linear scaling to 100+ concurrent
- Cache hit rate: >90% with LRU
- OpenTelemetry overhead: <2% at 10% sampling
- NATS messaging: <1ms latency, 1000+ msg/s throughput

### Changed

- Refactored from goagent monolithic architecture
- Extracted pkg/agent as standalone framework
- Reorganized documentation structure
- Consolidated import layering rules

### Fixed

- Memory leaks in checkpointer implementations
- Race conditions in state management
- Import circular dependency issues
- Tool execution timeout handling

### Security

- Added authentication middleware
- Implemented rate limiting
- Added input validation middleware
- Secure context propagation in distributed tracing

## [Unreleased]

### Planned Features

- Additional LLM providers (Anthropic Claude, Cohere, Hugging Face)
- Production vector database integration (Qdrant, Milvus, Pinecone)
- Graphical workflow designer
- Enhanced monitoring dashboard
- Agent versioning and A/B testing
- Performance optimizations (connection pooling, batch processing)

---

## Version History

### Version Numbering

GoAgent follows [Semantic Versioning](https://semver.org/):

- **MAJOR** version for incompatible API changes
- **MINOR** version for new functionality in a backward compatible manner
- **PATCH** version for backward compatible bug fixes

### Release Process

1. Update CHANGELOG.md with changes
2. Update version in code
3. Create git tag (e.g., v1.0.0)
4. Push tag to GitHub
5. Create GitHub release with notes
6. Update documentation

### Migration Guides

For breaking changes, see:
- [Migration Guide](docs/guides/MIGRATION_GUIDE.md) - Detailed upgrade instructions
- [Migration Summary](docs/guides/MIGRATION_SUMMARY.md) - Quick reference

---

**Note**: This is the initial release (1.0.0) extracted from the goagent project.
Historical development is documented in the [archive](docs/archive/) directory.

[1.0.0]: https://github.com/kart-io/goagent/releases/tag/v1.0.0
[Unreleased]: https://github.com/kart-io/goagent/compare/v1.0.0...HEAD
