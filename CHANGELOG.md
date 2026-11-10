# Changelog

All notable changes to GoAgent will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### 新增

- `cache.WithMaxEntries(n)`：限制 `SimpleCache` 条目数，超出时先回收过期条目再淘汰其他条目。
- `tools/shell.ShellToolBuilder.WithEnv(names...)`：按名称把宿主环境变量透传给子进程。

### 破坏性变更

- `tools`：`WithMiddleware`/`MiddlewareTool` 移入 `tools/middleware`，改为 `middleware.Wrap(tool, middlewares...)` 与 `*middleware.WrappedTool`，用法不变。`tools` 根包不再导入 `tools/middleware`，只用 Builder 的程序不再链接其日志中间件带入的 OTLP/gRPC：`builder` 的依赖包由 394 个降到 183 个；`apicheck` 拒绝 `tools` 根包重新导入 `tools/middleware`。迁移：`tools.WithMiddleware(` 改为 `middleware.Wrap(`。
- `tools`：删除没有生产使用方、与 Dispatcher 平行的 `ToolExecutor`（`NewToolExecutor`、`ToolCallRequest`、`ToolResult`、`RetryPolicy`、`ExecutorOption` 及 `With*`）、`ToolGraph`/`ToolGraphBuilder`/`ToolNode`，以及三套工具结果缓存 `ToolCache`/`SimpleToolCache`/`MemoryToolCache`/`ShardedToolCache`、`CachedTool`/`NewCachedTool`、`NewHighPerformanceCache`/`NewLowMemoryCache` 与分片缓存选项；对应示例 `retry-jitter`、`optimization/02-cache-presets`、`advanced/parallel-execution` 一并删除。迁移：工具调用经 `tools.NewDispatcher`（并发、超时、持久回放与重试策略见 `DispatchConfig`），结果缓存用 `tools/middleware.Caching`。
- `core`：删除没有实现方的旧 Agent 契约 `Agent`、`BaseAgent`、`AgentExecutor`、`AgentInput`/`AgentOutput`/`AgentOptions`/`AgentStep`/`AgentToolCall`、`FastInvoker`/`TryInvokeFast`/`IsFastInvoker`、`DefaultAgentOptions` 与执行器选项（`BaseAgent.Invoke` 恒返回未实现）；`observability` 删除包装它的 `InstrumentedAgent`/`WrapAgent`。`Runnable`、`StreamRunnable`、`Callback`、`Generator` 不变。依赖它们的示例 `basic/01-simple-agent`、`basic/03-agent-with-memory`、`basic/09-deepseek-simple/invokefast`、`generator/` 一并删除。迁移：Agent 用 `builder` 构建并经 `AsRunnable()` 接入 `core.Runnable`。
- `interfaces`：删除没有实现与使用方的 `Store`、`ConversationMemory`、`ToolExecutor`、`ToolResult`；`memory` 删除同样无人使用的 `ConversationStore`。键值存储用 `store.Store`，对话历史用 `MemoryManager`。
- `llm/cache`：整包删除。语义缓存键只含消息文本与模型名，不区分会话与用户，共用时会跨用户返回他人回答或被投毒，且仓库与扩展中没有使用方。需要时由宿主按租户/会话作用域实现。
- `tools/shell`：子进程只继承宿主的 `PATH`、`LANG`、`LC_ALL`、`TZ`、`TMPDIR`，不再继承全部环境；白名单中的 `cat` 等可读取 `/proc/self/environ`，此前宿主的 API key 会进入工具结果。需要其他变量时用 `WithEnv` 显式透传。
- `tools/middleware.Caching`：缓存键按会话（`tools.WithSessionID`）与 `ToolInput.CallerID` 隔离，自定义 `WithCacheKeyFunc` 只决定作用域内的键；默认缓存最多 `constants.DefaultMiddlewareCacheCapacity`（10000）条。此前不同会话或调用者以相同参数调用会命中彼此的结果，模型变换参数可使内存在 TTL 内无界增长。
- `core/durable`：超时（`failed/deadline`）与崩溃、取消一样按结果未知的中断处理：先经 `Reconcile` 核对，`RetryFailed` 不再直接重跑，需 `Reconcile` 确认未生效或声明 `RetryInterrupted`。此前工具已产生副作用但应答超过 `DispatchConfig.Timeout` 时，恢复会再执行一次。依赖“超时即重跑”的自定义策略需改为提供 `Reconcile` 或声明 `RetryInterrupted`；`tools.Dispatcher` 默认策略已声明 `RetryInterrupted`，Builder 的 `WithInterruptedModelResend` 现在也重发被运行截止时间或根预算截止打断的模型请求。

- `interfaces.ToolInput`：删除只写不读的 `Context` 字段，工具从 `Invoke(ctx, input)` 的 ctx 取得取消与请求范围的值。迁移：删除构造 `ToolInput` 时的 `Context: ctx`。
- `core/durable`、`tools`：`run` 或工具已成功返回而 ctx 恰好结束时，结果照常记为 completed 并返回 ctx 错误，恢复时回放而不再执行；此前成功结果被改记为 `failed/canceled`，默认策略下恢复会重复副作用（如重复付款）。非持久调用仍返回 ctx 错误。
- `builder.WithSessionState`：失败或中断的轮次对状态的修改不再泄漏到下一轮与会话日志；下一轮开始时回到该轮开始前的状态，日志中已有更新的状态时以日志为准。
- `core/session`：退回恢复的是退回位置当时的视图，退回到更早一次退回之前时，那次退回遮蔽的消息重新可见；此前被遮蔽的消息永久丢失。`BuildTrajectory` 的 `Turns` 不再计入被退回的轮次。
- `workflow.DurableGraph`：节点已成功返回而调用方恰好取消时，结果仍以独立的有界 ctx（`constants.DurableCompletionSaveTimeout`）写入检查点，恢复时不再重跑节点；此前节点的副作用会重复。
- `builder.WithSessionState`：重放已完成的持久轮次后，`GetState` 返回该轮结束时记录的状态；此前停在轮次开始前的状态。
## [0.52.0] - 2026-11-10

### 新增

- 在核心仓库新增 `management/protocol`、`management/a2a`、`management/mcpserver`：A2A/HTTP/MCP 共用管理任务、身份、审批与取消；A2A 使用官方 JSON-RPC/SSE，MCP 提供 Agent 工具和显式长任务工具契约。
- `a2a/remote` 支持官方 Agent Card 发现与远端 Agent 工具调用；`mcp/remote` 新增可保存的输入检查点、有界人工输入回调和旧协议宿主处理器选项。详见[协议接入指南](docs/guides/PROTOCOL_ADAPTERS.md)。
- `mcp/remote.WithDurableInput`、`a2a/remote.WithDurableInput`：持久运行中远端要求输入时改为 durable 等待，接入 management 的等待与恢复流程；每次远端调用记入日志，恢复时回放已完成的调用，不重发不带状态的初始调用。`a2a/remote.Tool` 新增可变参数选项。
- `builder.WithSessionState`：配合 `WithSessionLog`，每轮结束时把状态编码进该轮的 `turn/end`（`session.TurnEnd.State`），开始一轮时从会话日志恢复，替代已删除的 `WithCheckpointer` 自动保存。编码格式由状态类型的 `MarshalJSON`/`UnmarshalJSON` 决定，未实现时 `Build` 返回 `INVALID_CONFIG`；`*core.AgentState` 新增这两个方法，恢复后的值为 JSON 通用类型（数字为 float64），需要保留原始类型时自定义状态类型。同一 Agent 服务多个会话时状态按会话隔离，没有记录的会话从构建时的状态开始；退回会话后状态随之退回。新增 `session.LastState`。`last_response`/`last_timestamp` 改在记录轮次结束之前写入状态。

### 破坏性变更

- `core`：删除没有使用方的旧执行模型 `Chain`/`BaseChain`/`ChainableAgent`、`Orchestrator`（`Execute` 恒返回未实现）、`PluginRegistry` 及类型适配器、`LifecycleManager`/`BaseLifecycle`、`TimeoutConfig`；`tools` 删除 `ToolRuntime`、`RuntimeToolAdapter` 及 `UserInfoTool`/`SavePreferenceTool`/`UpdateStateTool`，`ToolRuntime.StreamWriter` 的工具进度推送没有替代（框架的工具循环从未调用 `ExecuteWithRuntime`），工具需要的状态或存储在构造时注入。串联 Agent 改用 `workflow` 图或 `multiagent`。
- 持久化与人工介入只保留 `core/durable` + `core/session` 一套：删除 `core/checkpoint`、`core/execution`、`core/interrupt.go`（`InterruptManager` 等）与 `testing/testutil`；builder 删除 `WithCheckpointer`、`WithStore`、`WithAutoSaveEnabled`、`AgentConfig.EnableAutoSave`，`Shutdown` 不再保存状态，跨进程保留状态改用 `WithSessionState`；`core/middleware.MiddlewareRequest.Runtime`、`errors.NewStateCheckpointError`、`errors.CodeStateCheckpoint`（`STATE_CHECKPOINT`）与 `constants` 中只供 checkpoint 使用的 `CheckpointReplicationModeSync`/`CheckpointReplicationModeAsync`/`DefaultCheckpointHealthCheckInterval`/`DefaultCheckpointMaxFailoverAttempts` 一并删除。跨进程恢复改用 `WithSessionLog`/`WithDurableScope` 或 `workflow.CompileDurable`，人工介入见 [HITL 指南](docs/guides/HUMAN_IN_THE_LOOP.md)。
- `interfaces`：删除没有实现与使用方的 `Lifecycle`、`LifecycleAware`、`Reloadable`、`DependencyAware`、`LifecycleManager`、`LifecycleState`、`HealthStatus`/`HealthState` 及其构造函数（`interfaces/lifecycle.go`）。插件的启动、停止与依赖改用 `plugin.Starter`、`plugin.Dependent`。
- `builder`：删除只服务旧 Runtime 的泛型参数 `C` 与 `WithContext`：`AgentBuilder[C, S]`、`ConfigurableAgent[C, S]` 改为 `AgentBuilder[S]`、`ConfigurableAgent[S]`，`NewAgentBuilder[any, *core.AgentState]` 改为 `NewAgentBuilder[*core.AgentState]`。
- `tools/shell`：`CommonShellTools()` 不再预设 `find`、`git`、`curl`、`wget`（可经参数执行任意程序或写任意文件），需要时用 `NewShellTool` 显式列入并配合 `tools/policy`。调用方传入的 `timeout` 只能缩短构造时的超时；输出最多保留 1 MiB，结果新增 `truncated`；`command` 与管道命令中的换行按命令分隔符拒绝。新增 `ShellToolBuilder.WithRootDir` 把工作目录限制在指定目录内。
- `tools/http`：配置了 baseURL 的 `APITool` 拒绝其他 scheme/主机的绝对 URL 与跨主机重定向，避免默认请求头中的凭据发往第三方；响应体上限 10 MiB。需要访问多个主机时为每个主机各建一个工具，或不设 baseURL。未配置 baseURL 时默认拒绝连接回环、私网、链路本地与云元数据地址（按实际拨号 IP 判断，并且不走环境变量代理），需要访问内网时使用 `APIToolBuilder.WithPrivateNetwork()`。
- `builder`：未调用 `WithTemperature`/`WithMaxTokens` 时请求不再携带 0.7 与 2000，改由 LLM 客户端配置决定；`AgentConfig` 新增 `TemperatureSet`，移除 `constants.DefaultAgentMaxTokens`。依赖旧默认值的调用方显式调用这两个选项。
- `core/durable`：`RetryFailed` 只重跑结果已知的失败（普通失败、超时）；停在 pending 或因取消而失败的记录须由 `Reconcile` 核对未生效或声明 `RetryInterrupted` 才重跑。`Reconcile` 对这两种中断记录都先行核对（此前只核对 pending，取消后的记录无法对账续跑）。`tools.Dispatcher` 的默认策略与 Builder 循环扩展点（PreStep、TurnStopping、提示词上下文、收件箱等的日志记录）显式声明 `RetryInterrupted`，恢复行为不变；非幂等工具应通过 `DurableOperationPolicy` 提供 `Reconcile` 或去掉 `RetryInterrupted`。
- `core`：`ToChannel` 在 ctx 取消后只在消费方正在接收或缓冲有空位时送出取消错误块，否则直接关闭 channel，避免消费方放弃读取后 goroutine 永久阻塞；调用方应在 channel 关闭后检查 `ctx.Err()`。
- `core/middleware`：`TimingMiddleware.GetTimings` 只返回最近 `constants.DefaultTimingWindow`（1000）条记录，`GetAverageLatency` 仍按全部记录计算；`CacheMiddleware` 条目上限为 `constants.DefaultMiddlewareCacheCapacity`（10000），写满时先回收过期条目再淘汰最早过期的条目，读到过期条目时删除。

### 修复与验证

- `scripts/apicheck`：新增按路径前缀声明的导入分层表，非测试代码只能导入同层或更低层的包，未声明所在层的包本身即为一条发现；此前没有专门规则的 `tools`、`llm`、`workflow`、`memory`、`store` 等包的反向导入现在会被拒绝。分层见 [导入层级](docs/architecture/IMPORT_LAYERING.md)。
- `memory`：`HierarchicalMemory` 与 `LongTermMemory` 写入向量库时不再持有读写锁，改变条目所在层或向量索引的操作另以写锁串行，`Get`、`Search`、`GetByType`、`GetStats`、`VectorSearch` 不再被远端向量写入阻塞；交给向量库的元数据改为快照。
- `store`：`StoreWithCache` 不再在持锁时访问后端，缓存命中不被其他 key 的冷读或写入阻塞；冷读改以写入计数判断期间是否有写入，避免发布旧值。
- `memory`：`ChromaVectorStore` 的写入与删除改用读锁，可并发发出请求，仅 `Clear` 独占；`HierarchicalMemory.VectorSearch` 在锁外调用向量库检索。
- `tools/policy`：命令规则与会话授权把 `command` 与 `args` 数组连成词序列匹配（参数名由新增的 `Policy.ArgsArg` 指定，默认 `args`），拒绝规则 `git push` 对 `{command:"git", args:["push"]}` 生效，授权不再从子命令扩大到整个程序。
- 嵌套 Agent 在公共流式与 Handle 执行入口隔离会话日志轮次，避免未配置日志的子调用写入或结束父轮次；补充失败历史、持久恢复与独立子日志回归。
- 实库证据门禁拒绝关键子测试跳过、包级失败、损坏或未完成的事件流，并核对摘要统计；模块发现与版本探测共享有界启动时限。
- 普通流式聊天支持 `WithSessionLog`，原生管理适配器不再要求配置占位工具；历史、Fork、故障后的轮次处理和持久回放共用现有会话协议，保留纯聊天原有模型操作键。
- 管理 Worker 启动恢复分批排空未完成任务和命令，移除10000条截断，保留事务失败回滚与取消语义。
- 增加管理服务、HTTP、原生与 Graph 适配器的离线行为回归；覆盖率门禁新增 management 75%、adapters 75%、httpapi 80%。
- 新增 `make test-management` 和 `make compatibility-postgres`；实库门禁检查明确的关键场景通过事件，不能将跳过、仅子测试通过或失败后重跑视为同一次验收通过。

## [0.51.0] - 2026-11-08

### 新增

- 核心新增 `management`：统一 Agent 注册和配置发布、Session 创建与 Fork、Task 提交/取消/恢复、Approval 决定、持久 Worker/Outbox 以及按 Agent、Session、Task 查询运行和用量。宿主显式注入存储、认证授权和固定版本执行工厂。
- `management/adapters` 提供原生 Builder 与 Graph 适配；`management/httpapi` 提供标准库 HTTP Handler 和 OpenAPI。管理层复用 `execution.Scope`、durable 与 budget，核心不新增 PostgreSQL、contrib 或 Harness 依赖。

### 修复

- Agent 停用后拒绝通过 Fork 创建新 Session；重新启用可重试先前拒绝的请求，已成功请求的幂等重投仍返回原响应。

### 迁移与验证

- 通用管理入口由 contrib 迁入核心，直接使用 `github.com/kart-io/goagent/management`；领域错误使用 `errors.ErrManagement*`，JSON 使用 `codec/json`。不保留旧根包转发、错误别名或版本回退，详见[管理层迁移指南](docs/guides/MANAGEMENT_MIGRATION.md)。PostgreSQL 实现、数据库集成案例与可选 Harness 适配仍在 contrib。
- 已归档[20组HTTP与数据库扩展案例](docs/evaluation/2026-11-08-management-expanded-cases/REPORT.md)、[DeepSeek与Gemini真实模型案例](docs/evaluation/2026-11-08-management-live-model/REPORT.md)及[验收可靠性修复](docs/evaluation/2026-11-08-management-live-reliability/REPORT.md)。真实模型验证覆盖审批续跑、多轮会话、精确 Fork、版本绑定和实际用量；合成退款只写入隔离数据库。

## [0.50.0] - 2026-11-08

### Changed

- **破坏性**：共享常量和默认策略统一使用 `constants`，错误统一使用 `errors`；工作流起止、多 Agent 完成标记、会话事件及工具名称的旧入口已删除，不保留转发别名。调用方按[常量迁移指南](docs/guides/CONSTANTS_MIGRATION.md)更新导入和符号。
- **破坏性**：`tools.WithMiddleware` 只接受 `middleware.ToolMiddlewareFunc`，删除旧钩子生命周期；状态对象须显式传 `.Wrap`。日志按每次调用独立计时，不修改调用方参数，错误时保留部分结果。见[工具中间件指南](docs/guides/TOOL_MIDDLEWARE.md)。
- **破坏性**：子图审批统一使用 `workflow.WithResume` 和结构化 `ResumeRequest`，不再识别裸审批键；叶子中断及普通持久操作的原始输入仍由 `durable.WithResume` 提供。无载荷与显式 JSON `null` 保持区别。
- 在线持久操作只接受 operation-v2；旧 operation-v1 须显式离线迁移，不能删除账本后强行重跑。检查点故障切换后保持副库权威，不因主库重新健康自动切回。

### Added

- `core/durable/migrate.OperationsV1`：默认只读预览，显式 `Apply` 才按有序锁和原子 `SaveAll` 转换目标账本；保留身份、输入指纹、结果、次数及业务数据，不执行业务回调。提供[操作步骤和可执行示例](core/durable/migrate/README.md)。
- 退役接口与无效配置的 AST 门禁：覆盖别名、匿名嵌入、函数变量及接收者别名，防止旧入口回流；合法领域类型、现行 JSON facade 与崩溃恢复契约保持。
- 格式化业务错误统一使用现有 `Newf`/专用构造器并补齐分类和组件，保留合法 `%w` 错误链；治理检查覆盖 `fmt` 导入别名及 `%%w` 转义。
- 框架错误统一由 `errors` 包处理：清理68个历史哨兵，统一保留46个实际使用的对象，保持身份与文本并提供错误码；45处即时业务错误补齐分类及组件。旧领域错误入口删除，文案改为 `Message*`，已联动案例和扩展；详见错误处理迁移指南。
- 顶层 `constants` 包按领域集中维护共享常量与默认值；错误码、错误分类及提示由 `errors` 包按领域维护；共享协议常量的旧转发入口全部删除，直接使用 `constants`；`Provider` 具名类型及8个typed枚举一并迁入中央，精确值及其余常量类型保持不变。见[常量迁移指南](docs/guides/CONSTANTS_MIGRATION.md)；错误入口迁移到 `errors`，见错误处理迁移指南。导入检查约束常量包只依赖标准库。
- MiniMax 国内 Anthropic 有界回归入口与每周验证矩阵：最多三个请求、一次合成工具效果，验证精确参数、回执纠正及零新增请求重放，不生成报告文件。
- `scripts/stabilitysoak`：可配置时长的离线混合长稳验证，默认24小时；验证治理、会话、恢复、审批和取消，检查资源采样门槛并提供仅本机可读的内存状态。
- `durable.WithReplayOnly`：`Do` 仅回放已完成记录，缺记录或未完成时返回 `ErrReplayOnly`，不执行或核对新效果。

- `builder.WithOutputValidator`：不可被纠正额度跳过的最终输出验收，覆盖中间件、完成回调、工具直返、纯文本与持久回放；`core.StreamRunnable.WithFinalValidator`在外层完成回调后验收。
- `builder.WithDispatcher`、`execution.NewGovernedBuilder`及治理配置报告：统一工具集合，显式预算/期限/授权/有限准入与排队，并检查运行使用创建时的Scope.Bind。
- `workflow.DurableConfig.StateVersion/Components`、`DurableGraph.Manifest/CheckCompatibility`：恢复版本声明与只读批量升级检查，空声明保留旧计划摘要；不自动迁移或授权恢复。
- 长会话、大工具结果、共享/独立会话Go基准及离线治理恢复示例。兼容检查增加`-core-dir`，Makefile默认验证当前工作树。
- **迁移要求**：PreStep、RequestHook、TurnStopping、PromptSections/Contexts、SessionLog缺少原生工具流时，现在在BuildContext期拒绝，不能依赖静默忽略或运行期才报错。纯文本最终验收改用OutputValidator；WithDispatcher不能混用WithTools。详见[验收与治理指南](docs/guides/EXECUTION_ACCEPTANCE.md)。

- `codec` 包及子包 `codec/json`、`codec/yaml`：goagent 统一的数据格式编解码入口，所有 JSON、YAML 转换都经由它们，规则只在此定义一次；结构与 Kubernetes 一致（`k8s.io/apimachinery/pkg/util/json`、`sigs.k8s.io/yaml` 一种格式一个包，`runtime.Serializer` 按格式选择）。`json.Marshal`、`json.Unmarshal`、`yaml.Marshal`、`yaml.Unmarshal` 用于格式确定的场景，JSON 另有 `json.Encode`（每值一行）、`json.Decode`、`json.DecodeNext`、`json.NewDecoder`、`json.Valid`、`json.Compact`（与 v1 `json.Compact` 一样不改写字符串原文）；格式在运行时才确定时用 `codec.ByName`、`codec.ByExt` 取得 `Codec` 接口（`Name`、`Marshal`、`Unmarshal`），`codec.ReadFile(path, v, opts...)` 按扩展名读取文件；`codec.Convert(in, out)` 按 json 标签把 map 等通用值弱类型转换为结构体（原 `core` 插件桥直接使用 mapstructure），与 JSON 一致跳过 `json:"-"` 字段（mapstructure 原先会把输入中的键 `-` 赋给它）。
  - `codec/json` 的默认规则就是 v1 的全部默认选项（`encoding/json.DefaultOptionsV1()`，`GOEXPERIMENT=jsonv2` 下 v1 本身即 v2 加这组选项），编码字节、解码结果与错误都与 v1 相同：map 键排序，nil 切片与 map 编码为 `null`，转义 `<`、`>`、`&` 与 U+2028/U+2029，非法 UTF-8 替换为 U+FFFD；数字、布尔的 `omitempty` 仍省略零值，无导出字段的结构体（如 `errors.New` 的返回值）编码为 `{}`，`,string` 也作用于布尔与字符串字段，指针接收者的 `MarshalJSON` 只在值可寻址时调用，map 键不调用 `MarshalJSON`；解码到已有值时 `null` 保留原值，类型错误跳过该字段继续解码、返回第一个错误，语法错误不修改目标；解码容忍重复键，字段名按 `strings.EqualFold` 匹配，Go 数组接受任意长度的 JSON 数组，base64 忽略换行，时间按宽松 RFC 3339 解析；`time.Duration` 为整数纳秒。错误类型与 v1 相同：编码为 `UnsupportedValueError`、`UnsupportedTypeError`、`MarshalerError`，解码为 `SyntaxError`、`UnmarshalTypeError`、`InvalidUnmarshalError`。
  - `codec/yaml` 与 `sigs.k8s.io/yaml` 相同：解码先转为 JSON 再按 json 标签解码，编码先得到 JSON 再转为块风格 YAML，结构体无需 yaml 标签，`llm.LLMOptions` 的显式 `temperature: 0` 等自定义解码照常生效。按 YAML 1.2 解析，`y`、`no`、`on` 仍是字符串，形如日期的值保持原文，标量键转为字符串；只接受一个文档。编码按 JSON 词法单元构造 YAML 节点，不把 JSON 当 YAML 重新解析，含 DEL、C1 控制字符的字符串也能编码；`y`、`no`、`on` 等 YAML 1.1 布尔词、`<<` 与含制表符的字符串加双引号，PyYAML、`yaml.v2` 等 YAML 1.1 读取方不会误读。
  - `json.Strict` 用于配置文件：未知字段、重复键、非法 UTF-8 报错，字段名区分大小写，遇到第一个错误即停止并返回带 JSON 路径的 `SemanticError`，`time.Duration` 另接受 `30s`、`1m30s` 等字符串。
  - 其余代码只经 `codec` 及其子包拿结果：`codec/json` 提供类型 `RawMessage`（JSON 原文，公开 API 中原 `jsontext.Value` 均改为它，二者为同一类型）、`Number`、`Decoder`、v1 错误类型 `UnsupportedValueError`、`UnsupportedTypeError`、`MarshalerError`、`SyntaxError`、`UnmarshalTypeError`、`InvalidUnmarshalError`、`Strict` 的错误类型 `SemanticError`、`Option`，选项 `Strict`、`UseNumber`、`NoEscapeHTML`、`Indent`、`IndentPrefix`，YAML 函数也接受这些选项。`json.Encode` 复用编码器，逐行写事件的分配与复用单个编码器相同。
  - `scripts/apicheck` 检查除 `codec` 及其子包外的全部代码（含测试与 `examples/`）不导入 `encoding/json`、`encoding/json/v2`、`encoding/json/jsontext`、`encoding/xml`、YAML 库、mapstructure 及 sonic、json-iterator、go-json、easyjson、gjson、sjson、jsonparser、go-toml、BurntSushi/toml 等第三方格式库；`utils`、`cache` 可导入 `codec` 及其子包，`codec` 及其子包只相互依赖，不依赖 goagent 其他包。
- `llm.LLMOptions` 实现 `UnmarshalJSONFrom`，自定义解码沿用调用方的解码选项（如 `json.Strict` 的未知字段报错与时长字符串）。
- 可从配置文件加载的配置类型补齐 json 标签，字段名为 snake_case（如 `cache.CacheConfig` 的 `default_ttl`、`observability.TelemetryConfig` 的 `trace_endpoint`），函数、接口、Logger、Store 等不可加载的字段为 `json:"-"`；涉及 `cache`、`core`（`TimeoutConfig`、`InterruptConfig`、`LifecycleManagerConfig`、`RunnableConfig`、`RetryPolicy`）、`core/checkpoint`、`core/durable.OperationPolicy`、`core/execution`、`builder`（`RetrievalConfig`、`contextpolicy.OffloadConfig`）、`devtools`、`llm`、`llm/cache`、`llm/common`、`memory`、`observability`、`tools`、`tools/middleware`、`utils/httpclient`、`workflow.DurableConfig`。这些类型原先按 Go 字段名编码，自行用 JSON 编码过它们的调用方需改用新字段名；goagent 不持久化这些类型，意图指纹与持久记录不受影响。`builder.AgentConfig` 保持既有的 Go 字段名格式。
- `examples/skills/02-weather-skill` 改用 `codec.ReadFile`，主模块不再依赖 `gopkg.in/yaml.v3`。
- `json.Pretty`：两空格缩进的编码选项，`Marshal` 与 `Encode` 均可使用，替代各处的 `json.Indent("  ")`。
- `tools/toolargs`：`Parse` 解析模型给出的工具参数，float64 表示不了的数字保留为 `json.Number`；`Validate` 按原文精确校验 schema 的数值约束；`Float64`、`Compare`、`IsInteger` 供工具读取与比较 `json.Number`。
- `llm.ToolCall.ArgumentsObject`：解析 `Arguments` 并要求是 JSON 对象。

### Changed

- JSON 编解码全部改用标准库 `encoding/json/v2` 与 `encoding/json/jsontext`，最低 Go 版本升为 1.27.0，开发工具链为 Go 1.27.3。迁移保持既有编码结果：数字、布尔、指针、接口字段的 `omitempty` 改为 `omitzero`，结构体字段去掉 v1 中本就不生效的 `omitempty`；持久意图、事件、会话、工具结果等可能含 map 的编码按键排序；`time.Duration` 仍编码为整数纳秒；Dispatcher 日志回放的工具结果中数字仍为 `json.Number`。工具参数 Schema 把 `omitzero` 字段视为可选。
- `workflow.DurableGraph` 每个节点执行前保存的意图快照只更新位置与键，状态沿用上一次提交的原文，不再重新编码：每个节点少一次状态编码，8 节点图在内存存储上耗时与内存各降约 9%，`FileStore` 上内存降约 8%。从旧版本写入的检查点恢复时，意图快照保留原文，不再改写为当前版本的编码。
- 调用方自己直接调用 `encoding/json/v2` 时默认行为与 `codec/json` 不同：nil 切片与 map 编码为 `[]`、`{}`，不做 HTML 转义，拒绝非法 UTF-8 与重复键，字段名精确匹配，数字、布尔的 `omitempty` 不省略零值，拒绝编码无导出字段的结构体（含非 nil 的 `error` 字段），`,string` 不作用于布尔与字符串，遇到类型错误立即停止解码，错误为 `SemanticError`；编码含 `time.Duration` 字段的类型（如 `observe.Event`）会报 `no default representation`。与 goagent 交换数据时使用 `codec/json`；`json.RawMessage` 与 `jsontext.Value` 是同一类型，已有代码无需改动即可传入。
- 模型给出的工具参数中，float64 能精确表示的数字仍是 `float64`，大整数（如雪花 ID）与高精度小数改为 `json.Number`，校验、`Authorize`、审批、durable 意图、缓存键与执行看到同一个值；原先这类数字被舍入，类型化工具声明 `int64` 也会拿到错误的值。数字所在位置的 schema 用到 `anyOf`、`patternProperties`、元组 `items` 等无法精确判断的关键字时作为参数错误交还模型；舍入后才越界的合法值（如 `9007199254740993` 对 `multipleOf: 3`）会被保守拒绝。超出 float64 范围的数字（如 `1e400`）仍视为无效参数。
- `core/state` 深拷贝解码回原值的类型：结构体不再变成 map，`time.Time`、`uint64` 大数与 `json.Number` 保持原类型。

### Removed

- `tools.ToolCall` 兼容别名；使用 `ToolCallRequest`。删除旧 `core/execution` 流协议及 `core/streaming_alias.go`，使用现行泛型 `Runnable/StreamRunnable`。
- 通用缓存的 `InMemoryCache/LRUCache/MultiTierCache`、对应构造器及专属 `CacheEntry`；`CacheConfig` 删除无执行路径的 `Type/MaxSize/CleanupInterval`。`SimpleCache` 只提供自身 TTL 契约，不作为 LRU 或多级能力的等价替代。
- Runtime 无效配置和指标容器、Runnable 空重试配置，以及检查点的 `FailbackDelay` 与无调用切回方法。RuntimeManager 改为零参构造；模型恢复使用真实的 Builder `RequestError` 路径。完整接口清单见[兼容入口迁移指南](docs/guides/COMPATIBILITY_CLEANUP.md)。
- `utils/json` 包与 `github.com/bytedance/sonic` 依赖：改用 `codec/json`；`Marshal`、`Unmarshal`、`RawMessage`、`Number` 改用 `codec/json` 中的同名函数与类型，`MarshalIndent` 改用 `json.Marshal(v, json.Indent(...))`（缩进与前缀只能含空格与制表符），`Decode` 改用 `codec.Convert`，`Get`、`GetString` 等路径查询没有替代。

### Fixed

- 并发工具回放测试采用可取消的入场屏障，保留模型请求数、真实工具执行数和并发峰值断言，消除日志 I/O 耗时导致的时序性误报；生产执行逻辑不变。
- 存储、会话和缓存输入输出取得独立快照；Update 错误、panic 或复制失败不发布部分记录、版本和事件，业务绝对 TTL 与缓存淘汰期限独立维护。
- 修复 Runtime 注册表并发、状态读写锁活性、配置池重置、完成回调错误传播及取消边界；TypedState 保持数组、字节、时间和大整数的具体类型与精确值。
- 检查点提交与复制入队保持顺序，副库复制与切换共享屏障，旧任务不覆盖切换后的写入或复活已删除数据；会话首次写入中断可安全恢复。
- 工具缓存保留完整输出及所有权，注入资源不被默认工厂误创建或关闭；实际工具选择与请求模型成本统计接入真实执行路径。同步更新本仓示例和评测入口。
- Builder 构建时复制配置与元数据容器，后续配置不再追溯修改已有 Agent，记忆与检查点使用一致的会话身份。
- 旧 `ChainableAgent` 保留多节点之间的任务、会话与选项，标准调用使用节点的 `Invoke`；流式、批量、管道和配置副本接入同一链执行源。
- Runtime 工具的存储操作继承本次调用的上下文；偏好工具仅在明确不存在时创建记录，读取故障不再覆盖已有数据。
- 旧工具执行器检查空结果和失败状态，失败或业务拒绝不再满足依赖节点的成功条件；结果校验失败不自动重试。
- 会话日志记录 system 与工具定义的清空，跨轮导出不再保留已移除的配置。
- 旧工具执行器拒绝超时后的成功结果，且不自动重试已报告成功的迟到操作；执行前及退避期间响应父上下文取消。
- 会话日志的最后一个已完成持久轮次支持同执行键重建重放，不重复追加输入、消息或轮次；完成重放禁止创建新持久操作，最终输出验收仍生效。

- Anthropic 原生 `ProviderState` 续接时直接回放 `tool_use.input` 的 JSON 原文，避免 SDK 解码到 `any` 后再次舍入大整数与高精度小数；保留思考块签名。修复覆盖持久化后的实际续接分支。
- 同时传入 `json.Strict` 与 `json.UseNumber` 时，后一个选项的解码钩子不再覆盖前一个。
- `codec/yaml` 解码保留 float64 表示不了的数字原文，配合 `json.UseNumber` 可得到精确值。
- `json.Encode` 编码失败时不写出任何内容，逐行 JSON 不会留下半条记录。
- Gemini 回放时丢弃编码为 `{}` 的 part（含空 `thoughtSignature`、`partMetadata`），避免请求被拒。
- Anthropic 回放历史工具调用时发送参数原文，大整数不再被改写。Gemini SDK 构造请求时自行经 float64 转换，仍会丢精度。

### 升级须知

- 本版本包含公开 API 删除和配置收敛，按 0.x 约定升级 minor；请先完成[接口迁移](docs/guides/COMPATIBILITY_CLEANUP.md)，严格配置解码会拒绝已删除的键。
- 有 operation-v1 账本时，先停写、备份，在相同 Store/Scope 下提供明确身份清单，执行[离线迁移](core/durable/migrate/README.md)的预览、应用和复核后再恢复写入。迁移不是在线兼容开关，也不替代宿主授权、业务版本校验或外部效果核对。
- 已构建的 Agent 现在持有配置快照；修改 Builder 后需重新构建才能应用新配置。状态、存储及嵌套业务对象仍按引用共享，跨会话使用独立 Builder。
- 直接使用旧 `ToolExecutor` 的工具须显式返回 `Success=true` 表示成功；空结果和普通失败结果会报告错误，业务拒绝不再解锁依赖节点。
- goagent 经 `codec/json` 编码持久数据，与 v1 逐字节一致，调用方放入持久状态的自定义类型（含带 `omitempty` 的数字、布尔字段）编码结果也不变；JSON 编码保持不代表账本版本兼容，operation-v1 须按上述步骤迁移，现行 v2 仍须通过原身份及业务版本校验才能恢复。回归测试覆盖 HTML 转义、非法 UTF-8、`omitempty` 与 nil 的编码。
- 按 map 读取数值参数的工具：大整数与高精度小数现在是 `json.Number`，`args["x"].(float64)` 会取值失败，改用 `toolargs.Float64(args["x"])`；需要精确值时直接使用 `json.Number`。类型化工具（`tools.NewTypedFunctionTool`）无需改动。

## [0.49.0] - 2026-11-05

### Added

- `make test-frameworkbench`、`make test-contracts`、`make compatibility`：独立评测只读依赖、公共执行契约和本地关联仓库验证入口。跨仓验证包含嵌套模块，使用临时 modfile 及受控 GOFLAGS 覆盖测试和子构建的依赖，保留通过、失败及跳过证据；Unix 上取消、SIGTERM 和超时会清理同进程组后代。
- 业务接入与故障恢复验收指南，记录本地退款及 PostgreSQL 接管、重启与同步提交的验证结果，明确真实业务接口仍需测试环境验收。

### Fixed

- `llm.Complete`（及 `llm.Chat`）：请求已取消或超时时不再调用提供方；取消与读流关闭/终态同时就绪时返回上下文错误，不再随机报告 `io.ErrUnexpectedEOF` 或成功；提供方返回 nil 流时直接失败，不再挂起到调用方超时。
- `tools/shell`：Unix 上超时或取消时结束同进程组后代，修复子进程持有输出管道使超时失效、并在超时后继续产生效果的问题；父命令结束后的输出等待有界，保留退出码、部分输出、`ExitError` 和上下文错误链。Shell、执行环境与跨仓验证器复用内部进程组机制。

### Changed

- 开发工具链统一为 `go.mod` 声明的 Go 1.26.10，最低版本仍为 Go 1.26.0；Makefile 清除陈旧 GOROOT，CI 按模块声明选择工具链。独立 frameworkbench 同步依赖并纳入 CI 契约检查。
- 整体覆盖率门槛由 45% 提高为 75%，新增 `multiagent` ≥85% 门槛；既有核心和 Provider 阈值不变。

## [0.48.1] - 2026-11-05

### Fixed

- Gemini 流式响应中不含任何字段的 part（如 Gemini 3 收尾帧的空文本）不再写入原生状态；此前带工具调用的下一轮回放会被 API 以 `required oneof field 'data' must have one initialized field` 拒绝。只带 `thoughtSignature` 的 part 仍原样保留。

## [0.48.0] - 2026-11-05

### Added

- 新增 `multiagent` 包：`Sequential`、`Loop`、`RouterSupervisor` 图构造器返回未编译的 `workflow.Graph`，由宿主选择 `Compile` 或 `CompileDurable`；`ParallelStep`、`AddStep`、`GuardRoute` 供手写组合流程复用；`SupervisorTools` 把成员 Agent 包装为模型工具；文本接力层 `Conversation`、`TextStep`、`TextNext`、`CloneConversation`、`MergeTurns`。路由无效时可转入 `Fallback`，错误满足 `errors.Is(err, multiagent.ErrInvalidRoute)`。与 v0.17 删除的旧 `multiagent` 同名，但 API 与语义不兼容，旧代码不能直接迁移。见[多 Agent 预置模式](docs/guides/MULTI_AGENT_PATTERNS.md)与离线示例 `examples/multi-agent-patterns`。
- 新增 `testing/mocks.ScriptedClient`：实现 `llm.ToolStreamClient` 的脚本化模型替身，按顺序匹配“请求断言 → 回复”，可回复结构化工具调用，收到额外请求时报错，并提供深拷贝的请求历史与按规则的匹配次数。
- `scripts/apicheck` 新增规则：基础与执行机制包禁止导入 `multiagent`，`multiagent` 禁止导入 `execution` 与 `plugin`。
- `workflow/refund.Service` 新增可选 `OnRegister` 钩子：`Register` 新插入申请后在同一事务内调用，可写入争议判定等与申请同生共灭的记录；返回错误时整个事务回滚，重复登记不调用，为 nil 时行为不变。

## [0.47.1] - 2026-11-04

### Added

- 覆盖率门禁增加 `core/durable`、`core/budget`、`workflow`、`builder` 模块阈值，按语句数加权统计；`make coverage` 与 CI 使用相同检查器，缺少关键数据时失败。检查脚本兼容 macOS 系统 Bash，可显式传入 profile 路径。

### Fixed

- 默认模型断言隔离 `ANTHROPIC_MODEL`，避免宿主模型配置使离线测试失败；模拟 Docker 的 Unix 测试不再依赖 macOS 缺少的 `setsid` 命令。

## [0.47.0] - 2026-11-04

### Added

- `workflow.ResumeRequest`、`DurableGraph.Resume` 与 `WithResumeHandler`：在已保存的审批暂停处接收人工输入，由宿主校验并更新业务状态；修改与放行原子落盘后继续执行。`RunRequest.Resume` 与 `workflow.WithResume` 分别支持 Runnable 入口和嵌套子图；无效输入保持等待，已消费的恢复键不能重复应用修改。新增 HITL 指南及离线通过/拒绝/编辑示例。
- `durable.AwaitInput`：工作流节点、并行分支与声明可中断的工具可以在执行中途等待人工输入，从检查点恢复；首次持久化的输入固定，非法 JSON 或校验失败时保持等待。`GraphSnapshot.Interrupts`、`ChildContinuation.Interrupts` 给出中断键，`Resume` 同时接受审批键与中断键。工具经 `FunctionToolBuilder.WithInterruptible(version)` 或 `tools.InterruptibleTool` 显式开启。`validate` panic 时按拒绝处理，panic 值不持久化，经新增的 `WaitError.Cause` 或 `ResumeRejectedError` 链可取得 `*errors.PanicError`。

### Changed

- 持久图节点返回中断等待时记为暂停而非失败；并行节点中分支等待不再取消兄弟分支。迁移：依赖“节点返回 `*durable.WaitError` 即失败”的宿主需改为处理 `ErrApprovalRequired` 与 `Snapshot.Interrupts`；已有工具开启中断时须提升版本，并等进行中的运行结束后再切换。`Interrupts` 非空时须用 `Interrupts[i].Key` 回应，不要用 `Key` 或 `ChildContinuation.ApprovalKey`（此时为空）。

### Fixed

- `core.InterruptManager` 不再忽略检查点保存错误：保存失败时释放中断 ID，不发布中断、不等待人工响应，也不执行后续业务。该管理器的等待状态仍限于进程内，跨进程恢复使用持久图。
- PR 导入分层检查移除已删除的 `--strict` 参数，避免在运行测试前失败。
- Makefile 按库项目执行 `go build ./...`，跨平台目标仅检查编译；`make run` 默认运行离线宿主示例，可通过 `EXAMPLE` 指定示例，CI 同时验证这两个入口。删除依赖不存在的统一应用二进制的安装、容器和初始化目标，迁移见开发指南。
- 中英文 README、Builder 与执行契约文档移除旧同步入口和本地 MCP 工具箱的过期能力描述；快速入门补齐 v0.43–v0.46 的迁移清单，开发指南同步 Go 与 lint 工具版本。

## [0.46.0] - 2026-11-04

### Added

- `interfaces.RetrievalRequest`（`Query`、`TopK`、`Filter`）与可选接口 `interfaces.RequestRetriever`，用于按租户、权限等元数据过滤检索结果。`builder.RetrievalConfig.Filter` 按请求（通常从 ctx）生成过滤条件：检索器未实现 `RequestRetriever` 时 `Build` 返回配置错误，`Filter` 返回错误或空条件时本次执行失败且不调用模型，不会退回无过滤检索。

## [0.45.0] - 2026-11-04

### Changed

- **破坏性**：`llm/cache.EmbeddingProvider` 只保留 `Embed`，删除 `cache.OpenAIEmbeddingProvider`、`cache.OpenAIEmbeddingConfig` 以及 `MockEmbeddingProvider` 的 `EmbedBatch`、`Dimension`。语义缓存只调用 `Embed`，自带的 OpenAI 实现与 `providers.OpenAIProvider.Embed` 重复。`OpenAIProvider.Embed` 改为使用 `llm.WithEmbeddingModel` 指定的模型，未指定时仍为 `text-embedding-ada-002`。迁移：`cache.NewOpenAIEmbeddingProvider(&cache.OpenAIEmbeddingConfig{APIKey: k, BaseURL: u, Model: m})` 改为 `providers.NewOpenAIWithOptions(llm.WithAPIKey(k), llm.WithBaseURL(u), llm.WithEmbeddingModel(m))`；注意原实现默认 `text-embedding-3-small`，新默认为 `text-embedding-ada-002`，两者向量不兼容，已有缓存需按原模型显式指定或清空重建。

## [0.44.0] - 2026-11-04

### Changed

- **破坏性**：核心向量统一为 `[]float32`，涉及 `llm.EmbeddingClient.Embed`（OpenAI、Gemini、DeepSeek 实现）、`memory.TextEmbedder`、`memory.VectorStore.Add/Search`、`memory.SearchResult.Embedding`、`interfaces.Document.Embedding` 与 `interfaces.Case.Embedding`。上游 SDK、`memory.MemoryEntry`、`SimpleVectorStore`、语义缓存与 contrib `retrieval.Embedder` 本就是 float32，原先 `StoreTyped` 一次写入要在 float32 与 float64 间转换四次；现在去掉了这些转换，`memory.SimpleEmbeddingModel` 等 `EmbeddingModel` 也能直接传给 `WithEmbedder`。迁移：自定义 embedder 与 `memory.VectorStore` 实现把签名中的 `[]float64` 改为 `[]float32`；构造 `Document`/`Case` 时向量字面量改为 `[]float32{...}`；需要 float64 计算的调用方自行逐元素转换。JSON 序列化格式不变。

## [0.43.1] - 2026-11-04

### Fixed

- `llm/providers`：`WithCustomHeaders` 对 OpenAI、Kimi、SiliconFlow、Ollama、Anthropic 与 Gemini 不生效，只有 DeepSeek 带上了自定义请求头；其中 Kimi、SiliconFlow、Ollama 与 Anthropic 是 0.43.0 改用 SDK 时丢失的。现在所有内置 Provider 都会发送，并覆盖 SDK 设置的同名头。只认 `x-api-key` 的 Anthropic 网关可用它补发该头。

## [0.43.0] - 2026-11-04

### Fixed

- `llm/providers`：OpenAI 兼容服务（含 DeepSeek、Kimi、SiliconFlow、Ollama）的上游对流式请求回了以换行结尾的普通 JSON（如代理不支持 SSE）时，go-openai 返回内含空 `*openai.APIError` 的错误，构造 `ProviderError` 时解引用导致读流 goroutine panic、进程退出。现在报告"上游返回的不是 SSE 事件流"。
- `builder`：`WithInputSerializer` 只作用于已删除的同步路径，`AsRunnable`、`ExecuteStream` 与 `Start` 一直用 `fmt.Sprint` 序列化输入，用它做脱敏时原文仍会发给模型并写入记忆。现在所有入口都经过配置的序列化器；传入 nil 时 Build 报错。
- `core/checkpoint`：`InMemorySaver.Save` 保存的是调用方对象的引用。Builder 每轮传入同一个运行时状态，之后的修改会改写已保存的检查点，历史中各版本也都指向同一对象。现在保存时复制一份。

### Changed

- **破坏性**：`llm.Client` 只保留 `CompleteStream` 与 `Provider`；`llm.StreamClient` 并入 `llm.Client`，`ToolStreamClient` 改为内嵌 `Client`。同步与流式两套实现各自处理参数、错误与用量，Builder 早已只走流式路径。新增 `llm.Complete(ctx, client, req)` 与 `llm.Chat(ctx, client, messages)` 收集流为完整响应：只有无错误的终止块算完成，流在终止块前关闭返回 `io.ErrUnexpectedEOF`。迁移：`client.Complete(ctx, req)` 改为 `llm.Complete(ctx, client, req)`，`client.Chat(ctx, msgs)` 改为 `llm.Chat(ctx, client, msgs)`，`ChatStream(ctx, msgs)` 改为 `CompleteStream(ctx, &llm.CompletionRequest{Messages: msgs})`；自定义客户端实现 `CompleteStream`，只有同步实现时把结果作为一个 `Done` 块返回。
- **破坏性**：`llm/cache.CachedLLMClient` 与 `evaluation.BudgetClient` 改为实现 `CompleteStream`。缓存命中返回一个 `FinishReason` 为 `cache_hit` 的完整块，未命中时转发上游流，只缓存成功终止的回答；预算客户端在整条流结束前持有锁，按终止块的用量计账，调用方放弃读取时必须取消 ctx。
- **破坏性**：`builder` 未配置 `WithCheckpointer` 时不再创建 `checkpoint.NewInMemorySaver()`。该实例为每个 Agent 私有，`Initialize` 时必然为空，却在每轮自动保存时快照全部状态并无上限地追加历史。依赖默认检查点器的代码改为显式 `WithCheckpointer(checkpoint.NewInMemorySaver())`；`runtime.State` 本身在进程内跨轮保留，不受影响。
- **破坏性**：Builder 配置工具时要求 `llm.ToolStreamClient`，不满足时 `Build` 返回 `INVALID_CONFIG`，不再等到执行时才失败。迁移：工具场景的自定义客户端补上 `CompleteToolsStream`。Builder 回调的 `OnLLMEnd` 只由已删除的同步路径调用，现在不再触发；用量与结束通知改用 `OnEnd` 或 `WithUsageHook`。
- **破坏性**：`testing/mocks.MockLLMClient` 实现 `llm.ToolStreamClient`，可直接交给 Builder；`MockStreamingLLMClient.Stream(ctx, req) <-chan llm.StreamChunk` 改为标准的 `CompleteStream`，按 `SetStreamChunks` 发送增量，最后是终止块。
- **破坏性**：`AnthropicProvider` 改用官方 SDK `github.com/anthropics/anthropic-sdk-go/v2`，实现 `StreamClient` 与 `ToolStreamClient`，可用于 `AsRunnable` 与原生工具循环；思考块经 `ProviderState` 回放。迁移：SDK 发送 API 版本 `2026-10-21`，该版本拒绝 `temperature`、`top_p`、`top_k`，Anthropic 请求不再携带温度与 top_p（Claude 4.7 及之后的模型本就只接受默认值，更早的模型将失去非默认温度）；默认模型由 `claude-3-5-sonnet-20241022` 改为 `claude-opus-5-5`；不再内置 3 次重试，改用 `WithRequestError(n, builder.RetryTransient(n, backoff))`；API Key 改以 `Authorization: Bearer` 发送；删除 `AnthropicRequest`、`AnthropicMessage`、`AnthropicResponse` 等手写协议类型与只服务于它们的常量（`AnthropicAPIVersion`、`AnthropicMessagesPath`、`HeaderAnthropicVersion`、`SSEDataPrefix` 等）。
- **破坏性**：Kimi、SiliconFlow、Ollama 改为 `OpenAIProvider` 的预设，删除各自手写的 HTTP 客户端，从而支持结构化流、原生工具流与 `AsRunnable`。`NewKimiWithOptions`、`NewSiliconFlowWithOptions`、`NewOllamaWithOptions`、`NewOllamaClientSimple` 改为返回 `*OpenAIProvider`（`KimiClient`、`SiliconFlowClient`、`OllamaClient` 删除），`Provider()` 仍返回各自身份；预设不声明 Embedding、视觉与 JSON 模式，不接受显式推理配置。迁移：Ollama 改走 `/v1` 兼容端点，默认地址变为 `http://localhost:11434/v1`，自定义 `OLLAMA_BASE_URL` 须补上 `/v1`；`ListModels()` 改为 `ListModels(ctx)`，读取 `/models`。

### Removed

- **破坏性**：`interfaces` 中无使用方的旧协议与推理类型：`Agent`、`Runnable`、`Input`、`Output`、`Message`、`StreamChunk`、`Plan`、`Step`、`State`，`Checkpointer`、`Checkpoint`、`CheckpointMetadata`，`ReasoningPattern` 及 `Reasoning*`、`ThoughtNode`、`ProgramCode`、`SkeletonPoint`、`ReasoningStrategy` 与 `Strategy*` 常量。迁移：`interfaces.Agent/Runnable` 改用 `core.Runnable[I, O]` 或 Builder 的 `AsRunnable`（残留用法会编译失败）；检查点改用 `core/checkpoint.Checkpointer`，持久执行用 `core/durable`；推理策略已在 v0.17.0 删除，无替代。
- **破坏性**：`memory.Embedder` 与 `llm.BatchEmbeddingClient`、`llm.CapabilityBatchEmbedding`：没有使用方，也没有 Provider 实现批量接口。迁移：单条向量用 `llm.EmbeddingClient`，`memory.WithEmbedder` 接收 `memory.TextEmbedder`。
- **破坏性**：`errors` 中随 `planning`、`multiagent` 包遗留的错误码 `PLANNING_FAILED`、`PLAN_VALIDATION`、`PLAN_EXECUTION_FAILED`、`PLAN_NOT_FOUND`、`MULTIAGENT_REGISTRATION`、`MULTIAGENT_CONSENSUS`、`MULTIAGENT_MESSAGE` 及对应的 `NewPlan*Error`、`NewPlanningError`、`NewMultiAgent*Error`。迁移：宿主需要时在自身包内定义错误码，用 `errors.New(code, msg)` 构造。
- **破坏性**：Cohere 与 HuggingFace Provider（`NewCohereWithOptions`、`NewHuggingFaceWithOptions`、`ProviderCohere`、`ProviderHuggingFace` 及其环境变量、默认值常量）与 `examples/llm/cohere`、`examples/llm/huggingface`，连同只服务于它们的 `common.MessagesToPrompt`、`common.DefaultPromptFormatter`。二者只有同步实现，无法用于 `AsRunnable` 与原生工具循环，下游没有使用方。迁移：服务提供 OpenAI 兼容端点时用 `NewOpenAIWithOptions(llm.WithBaseURL(...))`；检索重排的 `CohereReranker` 在 goagent-contrib，不受影响。
- **破坏性**：进程内工具箱 `mcp/core`、`mcp/toolbox`、`mcp/tools` 及 `examples/mcp`，连同 `tools.ValidateToolSchema`、`tools.ValidateInputWithSchema`。它不经 MCP 协议，`MCPTool` 也无法转换为 `interfaces.Tool` 交给 Agent，注册、权限与调用历史与 `tools.Registry`、`tools.Dispatcher`/`Pipeline` 重复。迁移：工具实现 `interfaces.Tool`（或用 `tools.NewFunctionTool`）经 `builder.WithTools` 注册，权限与审批用 `tools.Pipeline` 与 `tools/policy`；HTTP 请求用 `tools/http`；连接 MCP 服务器用 `mcp/remote`。
- **破坏性**：`ConfigurableAgent.Execute` 与 `ExecuteWithTools`，以及只服务于它们的同步处理器、基于单个 prompt 的旧工具循环和 `ExecuteStream` 中的旧工具选择路径。`Execute` 不执行工具，与 `AsRunnable` 语义不同；两条入口还各自绕开了持久日志、根预算、`WithOffload` 与 `WithReturnDirectTools`，只能在运行时报错拦截。迁移：改用 `agent.AsRunnable().Invoke(ctx, input)`，它会执行已配置的工具，需确认工具副作用与预算；中间件、`errorHandler`、`OnError`、`OnEnd` 照常经过。
- **破坏性**：`llm.ToolCallingClient`、`llm.TokenStreamingClient`、`HasToolCalling`、`HasTokenStreaming`、`AsToolCaller`、`llm.ToolCallResponse`、`llm.ToolChunk`，`llm/common` 的 `ToolCall`、`ToolCallResponse`、`ToolChunk`，以及 OpenAI、DeepSeek、Gemini、Anthropic Provider 的 `GenerateWithTools`、`StreamWithTools` 和字符串流 `Stream(ctx, prompt)`（`DeepSeekStreamingProvider`、`GeminiStreamingProvider` 上的同名方法随之消失）。`llm.FullClient` 改为组合 `ToolStreamClient`。迁移：能力探测用 `HasToolStreaming`/`AsToolStreamClient`；工具调用用 `CompleteToolsStream`，完整的 `ToolCalls` 只在成功终止块中出现；字符串流改用 `CompleteStream` 读取 `Delta`，并检查块上的 `Error`。
- `scripts/apicheck` 的 `-mode migration` 与 `-strict`：它只按名字匹配 `interfaces.Agent/Runnable` 与 `Execute/ExecuteWithTools`，这些 API 已全部删除，残留调用由编译器报出。`examples/llm/gemini-verify` 的默认套件随之由 `legacy` 改名为 `provider`，改测 `CompleteStream` 与 `CompleteToolsStream`。
- **破坏性**：内置 Provider 的 `Complete`、`Chat`、`ChatStream`、`IsAvailable`，`llm.HasStreaming`、`llm.AsStreamClient`，DeepSeek 手写协议类型（`DeepSeekRequest`、`DeepSeekResponse`、`DeepSeekMessage` 等）。同步 `Complete` 是流式路径之外的第二套请求实现，参数是其子集；`IsAvailable` 会发一次计费的最小补全，OpenAI 兼容预设还固定用 `gpt-3.5-turbo` 探测，对 Ollama 等服务必然失败。迁移：完整响应用 `llm.Complete`；检查 Ollama 等兼容端点是否可达用 `OpenAIProvider.ListModels(ctx)`（读取 `/models`，不计费）。`testing/mocks` 与 `llm.MockStreamClient` 一并去掉这些方法，`mocks.MockLLMClient.Complete` 保留为脚本化响应的来源。

## [0.42.1] - 2026-11-04

### Fixed

- `llm`：额度耗尽不再按限流重试。服务端常以 429 报告额度或套餐用量耗尽，原先 `RetryTransient` 照常退避重发，直到用尽重试次数；MiniMax 实测每次请求为此白等数分钟。`ProviderError` 增加 `QuotaExhausted`，此时 `Retryable` 为 false，`ClassifyError` 归为新的 `ReasonQuota`（`ShouldFallback` 为 true）。OpenAI 兼容服务按 `insufficient_quota` 与 MiniMax 的用量上限（错误码 2056）识别。

## [0.42.0] - 2026-11-03

### Added

- `builder`：`WithReplayablePartialOutput` 声明调用方只使用最终结果或能丢弃中途输出。开启后模型请求输出了部分内容再失败时，`RetryTransient` 照常重发。DeepSeek 过载时常先输出几个字节再停住，原先这类失败一律不重试，`Invoke` 调用也是如此。

### Fixed

- `builder` 测试：已输出内容后不重试的用例原先用了不可重试的错误，没有真正覆盖这条规则，改用 503。

## [0.41.0] - 2026-11-03

### Added

- `llm`：`WithStreamIdleTimeout` 与 `WithStreamTimeout` 限制流式响应的空闲间隔与总时长，超过时中断并报告可重试的超时，`RetryTransient` 会重发尚未输出文本的请求。服务端过载时流常在首包后停住或一点点地发，原先只有等响应头的超时（`WithTimeout`），之后可以无限期等待；DeepSeek 实测单次请求被拖到 3～5 分钟，正常为数秒。默认不限制，目前作用于 OpenAI 兼容的工具流（含 DeepSeek）。

## [0.40.0] - 2026-11-03

### Added

- `builder`：`PromptScope` 增加 `Final` 与 `Deadline`，`ModelRequest` 增加 `Final`。最后一次请求不再提供工具，`PromptContext` 现在可以据此告诉模型直接作答、说明未完成的部分，也能按剩余时间提示收尾；Request 钩子可以按步骤调整推理强度。`PromptSection` 计算时二者为零值。

## [0.39.1] - 2026-11-03

### Fixed

- `llm/providers`：OpenAI 兼容的原生工具流（含 DeepSeek）在结束帧前断开时，返回的 `io.ErrUnexpectedEOF` 现在包成 `ProviderError`，`ClassifyError` 判为可重试，`RetryTransient` 会重发尚未输出文本的请求。原先它被归为未知错误，直接中止运行。`errors.Is(err, io.ErrUnexpectedEOF)` 仍成立。
- `builder`：`WithTimeout`（默认 5 分钟）到期时，`AsRunnable().Invoke` 与 `ExecuteStream` 有时报告 `io.ErrUnexpectedEOF` 而不是超时，调用方无法判断该调大超时。原因有两处：终态事件与已结束的执行 ctx 竞争而被随机丢弃；提供方在 ctx 结束后不发终态直接关闭流时，被当成连接中断。现在终态事件按调用方的 ctx 投递，流在 ctx 结束后关闭时返回 ctx 的错误。

## [0.39.0] - 2026-11-03

### Added

- `builder`：`WithToolErrorFeedback` 把可恢复的工具失败作为业务拒绝交给模型，不再中止运行，默认关闭。覆盖本轮未提供的工具、参数不是 JSON 对象（持久运行中同样生效），以及工具返回 error（仅非持久运行：失败结果不入日志，恢复时会重新执行）。运行 ctx 已结束、预算耗尽、panic 与持久等待仍中止。
- `builder`：`Stopping.Blocked`，本轮由 TurnStopping 发起的续跑次数，不含 Steer 唤醒。验证类钩子据此限制重试，用户 Steer 后仍会执行。

### Changed

- `builder`：TurnStopping 只在续跑后还剩至少一步带工具的请求时执行。原先倒数第二步也会续跑，但下一步不提供工具，模型无法按反馈修改。额度不足、没有询问钩子就结束时，结果元数据记 `turn_stopping_skipped: true`，宿主可据此识别未经确认的答复。

### Fixed

- `tools/policy`：带 Command 的拒绝与询问规则不再能被绕过。原先命令含 `;&|$` 或引号时一律不匹配带 Command 的规则，与整工具放行规则同时存在时 `git push && true` 会被放行；`/usr/bin/git push` 也不匹配 `git push`。现在拒绝与询问规则按首词文件名比较，命令无法判断时至少裁决为 Ask。放行规则仍精确匹配。

## [0.38.1] - 2026-11-03

### Fixed

- `harness/env`：`DockerExecutor` 超时或取消落在容器创建期间时不再留下容器。原先 `docker run` 被中断后守护进程仍会建好容器，随后的删除找不到它。现在先创建再启动，创建不随 ctx 取消（上限 1 分钟），镜像不在本地时先拉取；容器没能启动时退出码仍与 `docker run` 一致（127、126）。

## [0.37.0] - 2026-11-02

### Added

- `builder`：`Handle.SteerOnce` 按键去重。持久运行中键与输入原子落盘，投递方崩溃后重投、输入已被领取或进程已重启都不会产生第二条。
- `core/durable`：`Inbox.AppendOnce`。
- `builder`：`WithInterruptedModelResend` 让持久运行恢复时重发被进程停止或崩溃打断的模型请求，同一回合最多 3 次。默认不开启，因为可能重复计费；每次重发各占一份预算，额度不足时不发送。
- `core/durable`：`OperationPolicy.RetryInterrupted` 和 `OperationAttempt`。

### Changed

- `core/durable`：操作执行中 ctx 被取消时，失败分类一律记为 `canceled`，不再取决于函数返回的错误是否包含 `context.Canceled`。

## [0.36.1] - 2026-11-02

### Fixed

- `builder`：在工具里运行的子 Agent 继承父 Agent 的 ctx，会领取父 `Handle` 的 `Steer`、`Inject` 输入并被唤醒多跑一轮，父 Agent 收不到这些输入。
- `builder`：父 `Handle` 的 Followup 轮次中，工具里运行的子 Agent 会继承父 Agent 上一轮的对话。权限更窄的子 Agent 因此能看到父 Agent 的内容。

## [0.36.0] - 2026-11-02

### Added

- `durable.Store.SaveAll`：把多个 ID 的快照作为一次原子写入。`durable.FileStore` 用提交日志实现，提交点之后崩溃也会前滚。
- `session.Store.AppendAll`：把多个会话的追加作为一次原子提交，任一序号不匹配时都不写入。
- `durable.OpenInbox`：保存运行中输入的持久收件箱。
- `core/durable/storetest` 与 `core/session/sessiontest` 增加批量写入的一致性用例。

### Changed

- **破坏性变更**：`durable.Store` 和 `session.Store` 接口各增加一个方法，自定义实现须提供 `SaveAll` 或 `AppendAll`。写入前可用 `durable.ValidateWrites`、`session.ValidateBatches` 检查参数，再用上述一致性用例验证。
- **破坏性变更**：持久运行中的 `Handle` 输入方法在写入持久收件箱后才返回，写入失败时返回错误，宿主要处理这个错误。
- **破坏性变更**：用同一执行键再次 `Start` 时，`Handle` 会恢复未领取的输入，并按记录的批次继续 Followup 轮次。原来由宿主重建 Followup 的代码要删除重复提交，否则同一批输入会多处理一次。

### Fixed

- `builder.Handle`：持久运行中，入队成功的输入在被领取前随进程退出而丢失；已领取的 Followup 批次在下一轮完成前崩溃也会丢失。

## [0.35.0] - 2026-11-02

### Changed

- **破坏性变更**：`mcp/tools.ReadFileTool` 实现了 Schema 中已声明的 `encoding` 参数：未知编码返回 `INVALID_INPUT`，内容不符合编码（含默认的 `utf-8`）返回 `ENCODING_ERROR`。原来读取二进制文件会成功，但内容在 JSON 序列化时被静默损坏。读取二进制文件请不要使用此工具。
- **破坏性变更**：图的节点取消后，`workflow` 会等节点结束才返回，因此节点必须响应取消并关闭流；忽略取消的节点会阻塞图，直到它自行结束。
- `core/middleware`：某个中间件的 `OnBefore` 失败且链终止时，已进入的中间件会收到 `OnError`，可经 `RequestFromContext` 收尾。原来它们得不到任何通知。

### Fixed

- `workflow`：经 `Adapt` 的节点被取消后，图不等节点结束就返回，`AddParallel` 失败时兄弟分支的清理和副作用会发生在图返回之后。
- `ObservabilityMiddleware`：靠后的中间件 `OnBefore` 失败时，span 不结束、活跃计数不归还。

## [0.34.0] - 2026-11-02

### Changed

- **破坏性变更**：最低 Go 版本升至 1.26.0，CI 同步使用 Go 1.26。修复 `golang.org/x/net` 漏洞所需的版本要求 Go 1.26；构建时请使用 Go 1.26.10 或更高版本，以包含标准库的安全修复。
- 升级依赖以修复 `govulncheck` 报告的漏洞：OpenTelemetry 1.38 → 1.47、`google.golang.org/grpc` 1.76 → 1.85、`golang.org/x/net` 0.47 → 0.62、`golang.org/x/text` 0.31 → 0.43、`golang.org/x/crypto` 0.44 → 0.58、`golang.org/x/sys` 0.41 → 0.49。
- **破坏性变更**：`mcp/tools.ShellExecuteTool` 不再返回 `Success: true` 的占位结果，改为返回 `NOT_SUPPORTED` 错误。它从未执行过命令；需要执行命令请使用带白名单的 `tools/shell`。
- **破坏性变更**：`BooleanOutputParser` 改为按整词匹配，无法判定的文本返回错误，而不再因子串命中给出结果。例如 `nothing` 原来返回 `false`，现在报错。
- `mcp/tools.WriteFileTool` 拒绝未知的 `mode`，原来会按覆盖写处理。
- `middleware.ObservabilityMiddleware` 的 `error.type` 指标标签改用错误码或错误类型，不再使用错误消息，依赖原标签值的看板需要调整。
- `builder` 与 `llm/providers` 的两条错误信息改为小写开头（`agent usage requires ...`、`gemini does not support reasoning effort ...`），以符合 staticcheck ST1005。按完整文本匹配这两条错误的调用方需同步调整。

### Added

- `core/middleware.RequestFromContext`：`OnError` 可取得当前请求，用于收尾保存在 `Metadata` 中的状态。
- `mcp/tools.JSONParseTool` 实现了 Schema 中已声明的 `path` 参数，支持 `$.a.b[0]`，结果写入 `value`。

### Fixed

- `ObservabilityMiddleware`：handler 失败时 span 不结束、活跃计数不归还；`RecordToolCall`、`RecordLLMCall` 把错误记到调用方的父 span 上。
- `BooleanOutputParser`：`incorrect`、`不是` 被判为 `true`。
- `JSONOutputParser`：字符串值中的括号导致提取错误；`T` 为接口类型时 `GetFormatInstructions` panic。
- `ListOutputParser`：吃掉 `3D printing`、`1.5 apples` 开头的内容；逗号分隔的 `1, 2, 3` 丢失首项。
- `StructuredOutputParser`：无法解析到非字符串字段，现在按 `FieldSchema.Type` 转换，失败返回 `ErrTypeConversion`。
- `mcp/tools`：`list_directory` 对 `.` 或隐藏目录递归时返回空结果；`search_files` 的 `max_results` 与 `http_request` 的 `timeout` 忽略 Go 整数。
- `LLMToolEmulatorMiddleware`：工具名缺失时 panic；`SetEmulateAll` 存在数据竞争。
- `ContextEnrichmentMiddleware`：同步富化器的 panic 未被捕获、`Timeout` 未生效，且结果写入与异步富化器存在数据竞争。
- `AdaptiveMiddleware`：`Metrics` 读写存在数据竞争；成功率从 0 起步导致规则在启动时误触发。
- `DynamicPromptMiddleware`：没有基础提示词时结果带前导空行。

## [0.33.0] - 2026-11-01

### Added

- 多 Agent 身份与 Token 归属：`AgentBuilder.WithAgentID` 标识角色；`budget.Controller` 新增 `RegisterAgent`、`ReserveAttributed`、`RecordEvidence`、`Attribution`，可按调用查询 Self、Inclusive 与 Root 用量。三者不可重复相加，见[多 Agent 指南](docs/guides/MULTI_AGENT_USAGE.md)。
- `budget.Config.MaxAgentCalls`：限制保存的调用节点数量。为 0 时取 `DefaultMaxAgentCalls`（1000），负值拒绝。
- Token 用量钩子：`AgentBuilder.WithUsageHook` 与 `budget.WithUsageHook`，无需实现 Observer 即可获取单次用量和累计统计。
- `core/usage` 包：`Evidence`、`ModelEvidence`、`Fields` 标明 Provider 实际报告了哪些字段，`Owner`、`Invocation` 表示调用归属。
- `interfaces.TokenUsage` 新增 `ReasoningTokens`、`ToolUsePromptTokens`；`TotalTokens` 保留 Provider 上报值，不再由子类相加推算。
- 推理配置：`usage.ReasoningConfig`、`llm.WithReasoning`、`AgentBuilder.WithReasoning`，由 `llm.ValidateReasoningRequest` 校验。OpenAI、DeepSeek、Gemini 实现了校验，其余 Provider 拒绝该配置。
- `Handle.PendingInputs`：返回尚未被执行循环领取的输入快照，不消费输入。

### Changed

- **破坏性变更**：预算持久记录格式由 `goagent-model-budget-v1` 改为 `goagent-model-budget`，加载旧记录会返回 `unsupported budget record format`，不提供自动转换。迁移：让使用 `budget.NewDurable` 的进行中运行在升级前结束，或继续用原版本恢复它们；不要靠清理日志或更换键绕过，否则已发生的用量和副作用会丢失。
- 绑定预算的 Builder 会自动建立调用身份，未设置 `WithAgentID` 时为 `anonymous`。
- 设置了 `AgentID` 或用量钩子且带工具的 Agent，如果客户端未实现 `llm.ToolStreamClient` 会直接报错，避免漏记工具选择请求。完整计量请使用 `ExecuteStream`、`AsRunnable` 或 `Start`。

### Fixed

- `Handle` 在取消时会等待本轮执行真正结束后才报告完成，`Wait` 与结束后的 `PendingInputs` 不再早于工具和清理逻辑返回。

## [0.32.2] - 2026-10-31

### Fixed

- 修复显式 `temperature=0` 在配置复制、Builder、请求序列化和 Provider 发送时被默认温度覆盖或省略的问题。支持 `TemperatureSet` 区分显式零与缺省；内置 9 个 Provider 保留零值，未设置请求继续使用原有回落规则。

## [0.32.1] - 2026-10-31

只改文档，代码与 v0.32.0 相同。

### Added

- 设计文档与实施计划：ACP（Agent Client Protocol）Agent 端（`docs/superpowers/specs/2026-10-31-acp-agent-design.md`、`docs/superpowers/plans/2026-10-31-acp-agent.md`）。实现在 contrib 的 [`goagent-contrib/acp`](https://github.com/kart-io/goagent-contrib/tree/master/acp) 模块，当前版本 `acp/v0.1.1`，变更见其 [CHANGELOG](https://github.com/kart-io/goagent-contrib/blob/master/acp/CHANGELOG.md)。

## [0.32.0] - 2026-10-31

### Added

- `plugin` 包：Agent 能力包。插件返回工具、提示词片段、循环钩子、中间件与回调；`Host` 按依赖顺序启停持有资源的插件，启动失败时回滚，经 `Env.Provide`/`plugin.Lookup` 共享资源；`plugin.Install` 校验冲突后装进任意 `AgentBuilder`，支持 `Only` 与 `Dispatch`。插件 PreStep 的持久身份含插件名与版本。插件名不得含 `/`、`@` 或 `.`。

### Removed

- `core.Plugin`、`core.BasePlugin`、`core.EnhancedPluginRegistry`、`core.GlobalEnhancedPluginRegistry`、`core.PluginConfig`、`core.Container`、`core.ResolveTyped`：插件提供的 `DynamicRunnable` 无法被 Agent 使用。迁移：把插件改为实现 `plugin.Plugin`，`GetTools` 返回的能力改为 `Contribution.Tools` 中的 `interfaces.Tool`，`Init`/`Start`/`Stop` 改为 `plugin.Starter`，`Container.Register` 改为在 `Start` 中 `env.Provide`，`ResolveTyped` 改为 `plugin.Lookup`，`GetMiddleware` 改为 `Contribution.Middleware`；`GetAgents` 无对应项（需要时用 `tools.NewRunnableTool` 包装为工具）；`interfaces.DependencyAware` 声明的依赖改为 `plugin.Dependent.Requires`；`PluginConfig.Config` 改为由插件构造函数接收类型化配置；不再支持同名多版本注册（`GetPlugin(name, version)`、`GetLatestPlugin`），Host 内插件名唯一。`core.PluginRegistry` 等 Runnable 注册表不变。

## [0.31.0] - 2026-10-31

### Removed

- `core.PluginConfig` 的 `Name`、`Version`、`Dependencies`、`Enabled` 字段与 `core.PluginDependency` 类型：注册中心从未读取它们。名称与版本取自 `Plugin.Name()`、`Plugin.Version()`，启动顺序依赖用 `interfaces.DependencyAware` 声明；迁移时删除这些字段的赋值即可。

## [0.30.1] - 2026-10-31

### Fixed

- 非流式请求（`Complete`、`Chat`、`GenerateWithTools`）的失败不带 `*llm.ProviderError`，`llm.ClassifyError` 对所有 Provider 都返回 `unknown`。OpenAI、DeepSeek、Anthropic、Cohere、HuggingFace、Ollama、Kimi、SiliconFlow 现在都给 HTTP 非 2xx 响应附上状态码与 `Retry-After`，连接错误也会附上。错误码不变，错误文本末尾多出原因（如 `HTTP 429`）。
- 新增 `common.WithHTTPStatus` 与 `common.RetryAfterDuration`，`common.MapHTTPError` 返回的错误也带上 `ProviderError`。

## [0.30.0] - 2026-10-31

### Fixed

- `tools/shell`：`ExecuteScript`、`ExecutePipeline` 传入的参数此前被丢弃，只执行了不带参数的 `bash`；`timeout` 只接受 float64，Go 调用方传 int 时被忽略。`ExecutePipeline` 经 `bash -c` 执行，现拒绝含 shell 元字符的命令，避免参数绕过白名单。
- `core/middleware.CircuitBreakerMiddleware`：成功不清零失败计数；打开期间自身的拒绝被计为失败并推迟半开，持续有请求时熔断器不再恢复。
- `core/middleware.CacheMiddleware`：挂在 Builder 上时从不写入缓存。
- `middleware.ObservabilityMiddleware`：`OnAfter` 不结束 span，span 不会导出。处理器出错时链只调用 `OnError`，该次 span 仍无法结束。
- `utils/httpclient.SetDefault` 在首次 `Default()` 之前调用时被覆盖；`Default`、`SetDefault`、`ResetDefault` 改为并发安全。
- `testing/mocks.MockTool` 的 `Schema()` 改名为 `ArgsSchema()`，此前不满足 `interfaces.Tool`。
- Ollama 未设置超时时应使用 120 秒，此前比较的默认值不对，实际为 60 秒。

### Removed

- **破坏性**：`parsers.OutputParser` 的 `ParseWithPrompt`（忽略 prompt，内置解析器上恒返回 NOT_IMPLEMENTED）。
- **破坏性**：占位实现 `core/middleware.RetryMiddleware`（不重试，只包装错误）与 `llm.NewClientWithOptions`（恒返回错误）及 `llm/factory.go`、`llm/examples.go`；模型请求重试用 `WithRequestError(n, builder.RetryTransient(n, backoff))`，客户端用 `llm/providers` 的构造函数。
- **破坏性**：不生效的选项：llm 的 `WithProxy`、`WithRetryCount`、`WithRetryDelay`、`WithRateLimiting`、`WithCache`、`WithStreamingEnabled`、`WithOrganizationID` 及 `LLMOptions` 对应字段；builder 的 `WithTelemetry`、`WithCommunicator`、`WithStreamingEnabled`、`WithSaveInterval` 及 `AgentConfig.EnableStreaming`、`SaveInterval`。
- 过时的脚本 `check-release.sh`、`verify_option_pattern.sh`、`verify_error_refactoring.sh`；导入检查中针对已删除 `agents/` 目录的规则。

### Changed

- 按当前代码重写过时的文档与示例说明。

## [0.29.1] - 2026-10-31

### Fixed

- 会话日志：持久运行在工具结果记入日志之后中断，以同一执行键恢复时，回放的对话被当作截断，记为 `history/rewrite` 并重复追加已记录的消息。从 `Fork` 出的会话继续持久运行时同样出现。

### Added

- `session.Recorder.Replayed`：对话是否等于某个事件处日志推导的对话，供恢复时跳过回放到已记录位置的同步。
- 从分叉点继续持久运行的用法与测试，见 `core/session/README.md`。

## [0.29.0] - 2026-10-31

### Added

- `builder.RetryTransient(retries, backoff)`：原样重试暂时性失败（429、5xx、连接错误）的 RequestError 组件，行为同原 `WithModelRetry`：已输出文本后不重试，优先按 `Retry-After` 等待，不越过截止时间。
- `RequestFailure.Emitted`：失败的请求已向消费者输出内容。

### Changed

- RequestError 钩子在模型回合操作内询问：原样重试在同一条回合记录内进行，持久运行中也生效；替换消息或客户端仍只在非持久运行中生效，持久运行中以错误结束（此前持久运行不询问钩子）。没有工具的流式回答同样询问钩子，只支持原样重试。

### Removed

- **破坏性**：`builder.WithModelRetry` 与 `AgentConfig.ModelRetries`、`ModelRetryBackoff`。迁移：`WithModelRetry(n, backoff)` 改为 `WithRequestError(n, builder.RetryTransient(n, backoff))`。

## [0.28.0] - 2026-10-31

### Added

- `builder.WithOffload(offload)`：把 `contextpolicy.Offload` 装为带身份的 PreStep，超长的工具结果换成引用、经读取工具分页读回，最后一次请求前还原原文。持久运行恢复时 PreStep 的结果被回放，Builder 用回放的对话重建引用表，读取不受影响。
- `builder.WithIdentifiedPreStep(identity, hook)`：身份进入 PreStep 记录的意图，配置改变后恢复旧的持久运行在请求模型前失败，而不是回放按旧配置算出的结果。
- `builder.Step.Final`：本步是最后一次模型请求。
- `contextpolicy.Offload` 的 `Reduce`、`Track`、`Identity`；实例按结果作用域区分引用表，可供多次执行共享。

### Removed

- **破坏性**：`contextpolicy.Policy`、`Descriptor`、`Turn`、`Factory`、`ToolProvider`、`FinalTurnRestorer`、`StatsProvider`、`Releaser`，`builder.WithContextPolicies`、`WithPolicyToolAdapter`，以及旧 `builder.WithContextPolicy`、`ContextPolicy`、`HistorySummaryConfig`。迁移见 `docs/guides/CONTEXT_POLICY.md`：结果卸载用 `WithOffload`（读取工具与业务工具一起注册），历史摘要用 goagent-harness `preset` 的 `compaction` 组件，自定义策略改写为 `WithIdentifiedPreStep`。
- 元数据 `context_offloaded_results`、`policy.<id>.*`、`summary_calls`；改用 `Offload.Stats()`。

### Changed

- **破坏性**：Offload 的引用增加 `stored` 字段（证据的键与摘要），默认 Version 改为 `v3`；用旧版本记录、尚未结束的持久运行须在升级前结束。

## [0.27.0] - 2026-10-31

### 宿主接入与开销

- 新增 BuildContext，向初始化存储与回调传递宿主上下文并检查取消；Build 保持原入口行为。
- 新增顶层 execution.Scope 和 ConfigureBuilder，统一运行、会话、日志、预算、期限及观察装配，并检查上下文冲突；新增离线示例与接入指南。
- 预算账本改为类型化深复制，持久读取前跳过无用内存复制；持久格式、锁及同步保存语义保持一致，新增隔离性与分配回归测试。
- 新增离线执行探针与固定账本微基准，保存三模式实测原始数据及测量边界。

### 执行可靠性与治理

- 初始化严格传播检查点Exists/Load及状态类型错误；新增 `WithMemoryFailurePolicy`，默认尽力、可选严格，所有入口共用记忆错误处理与结构化观察，取消直接stderr记忆错误输出。
- Pipe/Sequence统一流终态与取消，空Sequence恒等；显式父回调生效，Batch逐输入Invoke并保留有序部分结果。父子重复副作用回调及依赖子自定义Batch的调用方须按扩展契约迁移。
- 根预算新增保守Token预留、工具调用次数与绝对截止时间；宿主提供版本化TokenEstimator，未知用量不补零，预留不退款。零值新字段保持旧日志及completed模型意图兼容，配置漂移拒绝恢复。
- Runner/持久图新增只读Diagnose，报告等待、完成、版本障碍与结果未知，显式操作记录提供失败证据，不自动恢复副作用。
- Dispatcher独立准入/执行并发和有界排队，新增ErrDispatcherBusy；审批不再占用执行槽，需限制审批量的宿主应显式配置MaxAdmissionConcurrency。排队/准入暂停单次Timeout，校验授权日志执行累计计时。
- 新增固定worker的有界AsyncObserver和可取消Close、丢弃统计；Pipeline新增GrantTTL和PruneGrants，过期授权不会继续生效。
- 补齐OpenAI、DeepSeek、Gemini工具流、未知usage和停止消费后取消契约测试；新增扩展/治理文档与发布验收清单。

### Removed

- **破坏性**：Preset 与编码工具迁至独立模块 `github.com/kart-io/goagent-harness`：`harness/preset` 改为新模块的 `preset`，`tools/coding` 改为新模块的同名路径；两个工程 Agent 示例随之迁出。不保留转发兼容层。配置格式、Builder 与循环扩展点不变，`harness/env`、`tools/policy`、`tools/spill` 留在核心。当前为本地拆分，发布本版本前须先发布新模块，迁移与发布顺序见 `docs/guides/HARNESS_MIGRATION.md`。

### Fixed

- `TestOnResultReplayOrderAfterConcurrentRun` 使用同步屏障等待首批并发调用全部进入工具体，消除磁盘与调度延迟导致的峰值断言偶发失败；保留并发上限与恢复交付顺序的验证。

- `harness/env.LocalFS` 的条件写入改为同进程所有实例按真实路径协调，删除也参与同一协调；修复并发创建时偶发误报悬空链接。Unix 本地文件写入改为同目录临时文件同步后原子发布，避免覆盖过程中读到截断内容；`CreateIfAbsent` 使用硬链接发布，避免覆盖抢先创建的文件。明确 `IfVersion` 不协调直接写入或其他进程，替换会更换 inode，发布后同步失败须重新读取确认；已取消的写入和删除不再改动文件。

## [0.26.0] - 2026-10-31

### Added

- `core/session/sessiontest`：`session.Store` 实现的一致性测试（比较并交换与并发追加、事件往返、按字节序列出、分叉、锁），并以 `session.DurableStore` 视图运行 `core/durable/storetest`；`Options.Leases` 为租约后端另跑接管与写入隔离用例。`FileStore` 与 `MemoryStore` 改用它测试。
- `session.DurableLockName`：`DurableStore` 为快照加锁所用的名称，供以租约实现锁的 `Store` 在追加快照时校验持有者。

## [0.25.0] - 2026-10-31

### Added

- `testing/replay`：把会话日志中的持久运行转为回归测试（cut-point replay）。`Replayer.Prepare` 返回重放一轮所需的 ctx（执行键与只读原日志的 durable 存储）和该轮之前的会话副本；`Cut` 选中的边界及之后实跑，`Live` 选中的边界单独实跑，实跑结果与记录不同之后的边界一律实跑；`Report` 给出每个边界的处理方式、记录值与实跑值。

### Fixed

- 会话日志 `turn/start` 中的执行键改为宿主传入的执行键：此前记录的是 DurableScope 派生之后的内部键，按它恢复或重放会再派生一次，找不到记录。

## [0.24.0] - 2026-10-31

### Added

- `core/session`：追加写的会话事件日志。`DeriveMessages` 从日志推导模型历史（`history/rewrite` 替换区间内的消息，`session/rewind` 与中止的轮次撤销区间内的事件）；`FileStore`（每个会话一个 JSONL 文件，按行原子追加，忽略写入中崩溃留下的不完整行）与 `MemoryStore`，`Append` 按期望序号做比较并交换；`Rewind`、`Fork`。
- `session.DurableStore`：以会话日志实现 `durable.Store`，持久操作的快照作为 `boundary/snapshot` 事件写入同一份日志，通过 durable 的存储一致性测试。
- 轨迹导出：`session.BuildTrajectory` 给出模型最终看到的对话、system 提示词、工具定义与工具调用统计，`WriteOpenAI`（OpenAI 微调格式）、`WriteShareGPT`（LLaMA-Factory 的 sharegpt 格式）。
- `builder.WithSessionLog`：原生工具流的历史从会话日志推导，每次模型请求前记录与日志不同的对话；中断的轮次在持久运行以同一执行键恢复时续写，否则记为中止、不进入历史。

### Changed

- 自动发版规则：推送到 master 时只有 `fix`、`perf`、`refactor` 发补丁版本，`feat` 发 minor；`docs`、`test`、`chore`、`ci`、`style`、`build` 不再单独发版，随下一次发版一起发布。需要单独发版时，手动运行 Auto Tag and Release 并指定版本类型。
- 提交作用域可以含任意字符（如 `fix(llm/common):`），此前只识别小写字母，带斜杠、数字或连字符的提交被漏判。
- 破坏性变更除标题中的 `type!:` 外，也识别正文中以 `BREAKING CHANGE:` 开头的行；0.x 阶段仍只升 minor。

## [0.23.1] - 2026-10-31

库代码与 0.23.0 相同。

### Fixed

- PR 流水线的覆盖率评论步骤缺少 `pull-requests: write` 权限而报 403；来自 fork 的 PR 令牌只读，改为跳过评论，报告同时写入 job summary。

### Changed

- 整体覆盖率统计（`scripts/check_coverage.sh` 与 PR 评论）不再计入 `examples/` 下没有测试的示例程序。
- 只测试 `llm/common` 的 6 个用例从 `llm/providers` 移回 `llm/common`。
- CHANGELOG 中 0.23.0 的条目此前列在 Unreleased 下，已归入 0.23.0。

## [0.23.0] - 2026-10-30

### Added

- Agent 句柄：`ConfigurableAgent.Start(ctx, input)` 返回 `*Handle`，运行中可 `Steer`（下一步追加，结束前唤醒续跑）、`Inject`（下一步追加，不唤醒）、`Redirect`（取消进行中的模型请求，保留已流出的文本后重新请求；工具执行中或持久运行中降级为 Steer）、`Followup`（本轮结束后续接对话开始新一轮）、`Interrupt`，`Wait` 取得结果；轮次结束后再追加输入返回 `ErrHandleDone`。只支持原生工具流。
- 循环扩展点（原生工具流）：`WithPreStep`（改写消息或不请求模型直接结束）、`WithRequestHook`（替换客户端或调整参数）、`WithRequestError`（按错误分类重试、压缩或回退，单步恢复次数有上限）、`WithTurnStopping`（返回 user 消息续跑）。
- 提示词组装：`WithPromptSections` 追加到 system 且同一轮内字节不变；`WithPromptContexts` 每步重算，附在请求末尾、不进入之后的历史。
- 持久运行中 PreStep、TurnStopping、提示词段落与上下文、收件箱输入按步骤记入日志，恢复时回放；RequestError 钩子与 Redirect 只在非持久运行中生效。
- `llm.ClassifyError`：把模型请求错误分为限流、服务端、网络、超出上下文、鉴权、不存在、请求错误、取消，并给出 `Retryable`、`ShouldCompress`、`ShouldFallback` 提示。
- `harness/preset` 的 `claude-code-hooks` 组件支持 `UserPromptSubmit`（阻止或补充用户输入）、`Stop`（阻止结束并续跑，提供 `stop_hook_active`）、`SessionStart`（每个会话一次，输出进入 system）。
- `harness/preset` 的 `plan-mode` 组件：计划模式中只能调用只读工具，`exit_plan_mode` 提交计划并经审批后退出，`enter_plan_mode` 进入；模式从对话中恢复，切换不改变 system 前缀。
- `harness/preset` 的 `compaction` 组件：对话超过阈值时用 `Host.CompactionModel` 把较早的消息压缩为摘要，请求因超出上下文失败时压缩后重试。
- `preset.Configure` 把装配结果（系统提示词、工具、循环扩展点）交给 Builder；`Assembly.Loop` 与 `Assembler.AddPreStep`/`AddRequestError`/`AddTurnStopping`/`AddPromptSection`/`AddPromptContext` 供组件贡献循环扩展点。
- `builder.Step.Inputs`（本步新收到的用户输入条数）、`builder.Stopping.Continued`（本轮已续跑次数）。

### Changed

- `builder.PromptScope.SessionID` 改为与工具调用一致的会话 ID：宿主在 ctx 中声明的会话优先（此前总是 `WithSessionID` 的值）。
- `claude-code-hooks` 的 `path` 文件中的 `UserPromptSubmit`、`Stop`、`SessionStart` 钩子此前被忽略，现在生效，并同样计入授权摘要：已授权的项目若文件中含这些钩子，需要重新授权。

## [0.22.0] - 2026-10-30

### Added

- `harness/preset`：声明式自定义 Agent。一个 preset 由组件行组成，`preset.Load` 按 内置 → 用户 → 项目 → 本地 → 宿主 叠加并严格校验，`preset.Build` 装配出系统提示词、经 Pipeline 的工具、工作区与装配清单 `Manifest`。
  - 合并：preset 与组件按 id 合并，同 id 整行替换并保留位置，`remove`/`replace`/`locked` 显式表达删除、换实现与锁定；`Set.Dump()` 标注每行来源、覆盖与删除。
  - 信任：权限上限 `limits` 只由可信层声明；项目层与本地层的扩权项（超出上限、`allow` 规则、替换或删除可信层组件、宿主进程、秘密引用）须经 `TrustStore` 授权，否则 `Build` 返回 `*TrustRequiredError`；授权绑定项目根目录与扩权项摘要。提供 `FileTrustStore`、`MemoryTrustStore`。
  - 内置组件 `persona`、`instructions`、`workspace`、`coding-tools`、`permissions`、`spill`；`preset.Register` 注册自定义组件，秘密经 `preset.Secret` 引用。
  - 沙箱模式（read-only / workspace-write / danger-full-access）是对执行环境的要求，执行器做不到即 Build 失败；没有规则匹配的调用一律需要审批。
- `tools/spill`：超长工具结果写入宿主目录，提供 `read_spill` 分段读取；写入失败时只返回预览不报错，`Prune` 清理过期文件。
- `harness/env`：`DockerExecutor.ReadOnly` 以只读方式挂载工作区，`Capabilities.ReadOnlyRoot`；`env.ReadOnly` 包装器拒绝写入与删除（`ErrReadOnly`）。
- `coding.Workspace.ProjectInstructionsFrom` 读取指定的项目说明文件。
- `harness/preset` 的 `mcp` 组件：stdio 与 streamable-http 两种传输，工具名为 `mcp__<组件 id>__<工具名>`（可用 `aliases` 改名、`tools` 取子集），环境变量与请求头可引用秘密（`preset.Value`），stdio 服务只继承少量宿主环境变量；远端错误结果交给模型，持久运行中不重跑中断的 MCP 调用；服务信息与工具摘要记入装配清单。
- `harness/preset` 的 `claude-code-hooks` 组件：兼容 Claude Code 的 `PreToolUse`/`PostToolUse` 命令钩子（stdin JSON、退出码 2 阻止、`permissionDecision`/`decision`/`additionalContext`），裁决按 deny > ask > allow 与权限规则一起聚合；编码工具以 Claude Code 的名称出现。`path` 指向的工作区文件属于不可信内容，其中的命令需要授权，文件改动后授权失效。
- `harness/preset` 的 `skills` 组件：读取 `SKILL.md` 技能目录，提示词列出名称与描述，`load_skill` 读取说明与技能目录内的文件。
- `harness/preset` 的 `subagent` 组件：把任务交给另一个 preset 装配的子 Agent，由 `Host.RunAgent`（或 `preset.BuilderRunner`）运行；子 Agent 的调用同样经过父 Agent 的钩子与守卫、沙箱不宽于父 Agent，`toolFilter` 在初始化前生效，引用有环与超过 `maxDepth` 时报错。
- `preset.Spec` 新增 `Resolve`（Build 时从工作区补全配置，补全内容计入授权摘要）、`References`（引用的其他 preset）、`FilterTools`（子 Agent 工具过滤）；`Assembler` 新增 `AfterPublish`、`Depth`。
- `Assembly.PinManifest`：持久运行首次执行时把装配清单摘要写入日志，恢复时不一致返回 `preset.ErrManifestChanged`。
- `mcp/remote.Tools` 新增选项 `WithToolName`（改名或跳过工具）、`WithToolErrorsAsRejected`（远端错误结果作为 `tools.Rejected`）、`WithDurablePolicy`（声明持久日志策略）；不传选项时行为不变。

### Fixed

- `mcp/remote` 的工具返回错误结果时，错误信息为 `<nil>`：服务端把错误放在文本内容中，此前只读取了 `GetError()`。

### Changed

- **破坏性**：删除 `coding.SpillHook` 与 `coding.SpillDir`，改用 `tools/spill`。溢出文件不再写入工作区的 `.goagent/spill/`，而是写入宿主目录，模型用 `read_spill` 而非 `read_file` 读取。迁移：`coding.SpillHook(ws, n)` 改为 `store, _ := spill.New(dir, n)`，把 `store.Hook()` 加入 `Pipeline.Post`、`store.ReadTool()` 注册为工具。

- **破坏性**：`tools.Pipeline` 的调用前钩子全部执行，裁决按 Stop > Deny > Substitute > Ask > Allow 聚合，与钩子顺序无关；此前第一个非 Allow 的裁决生效，获批的 Ask 之后其他钩子的 Deny 不再执行。
  - 多个 Deny 的反馈以 `; ` 合并交给模型；多个 Substitute 视为错误。
  - 多个 Ask 合并为一次审批，`ApprovalRequest.Reason` 为各钩子理由以 `; ` 合并；只有每个提出 Ask 的钩子都给出 `SessionGrant` 时才提供"本会话允许"。
  - 会话授权按钩子分别记录，一个钩子的授权不再满足另一个钩子的 Ask。
  - 迁移：依赖"前面的钩子放行后，后面的钩子不再执行"的宿主，需在后面的钩子里自行判断。
- **破坏性**：`tools/policy` 在所有匹配的规则中取最严格的裁决（Deny > Ask > Allow），与规则顺序无关，`Default` 只在没有规则匹配时使用；此前取第一条匹配的规则。`coding.DefaultPolicy()` 的规则互不重叠，行为不变。迁移：靠"先写 allow 特例、再写 ask/deny 通配"实现例外的规则需改为让通配规则不覆盖该例外（例如改用更窄的 `Command` 前缀）。

## [0.21.0] - 2026-10-30

### Added

- `tools.Pipeline`（`DispatchConfig.Pipeline`）：Dispatcher 对参数合法的调用依次执行调用前钩子 → 审批 → 守卫 → 执行包装 → 工具 → 调用后钩子，对任何工具生效。
  - `PreHook` 裁决 `PreAllow`/`PreDeny`（以 `tools.Rejected` 把反馈交给模型修订）/`PreAsk`/`PreStop`（以 `*StopError` 终止运行）/`PreSubstitute`（改为执行另一个已注册工具，按其 Schema、自定义校验与 `Authorize` 检查后重新经过准入，结果元数据记录替代关系）。管线不改写参数。
  - `Approver` 结果为闭合集合 `AllowedOnce`/`AllowedSession`/`Rejected`/`Cancelled`/`Unavailable`，没有审批人或超时（`ApprovalTimeout` 默认 10 分钟，迟到的批准不算数）按拒绝处理。`PreAsk` 可附带 `SessionGrant`，"本会话允许"后放行同会话中匹配的调用；会话由 `tools.WithSessionID` 声明，Builder 以 `WithSessionID` 自动设置；没有会话 ID 时会话级批准按单次批准处理。同一会话中工具与参数相同的并发审批请求只询问一次，发起询问的调用被取消时其余调用重新询问。
  - `Guard` 只能拒绝，在审批之后执行，人的批准也无法越过；`Around` 洋葱式包住工具执行；`PostHook` 可替换结果或追加 `AdditionalContext`。
  - 准入阶段（调用前钩子、审批、守卫）在操作日志之外执行：终止、取消与审批出错不留记录、不消耗尝试次数；拒绝结果与执行结果一样写入日志，回放已完成的调用不再询问。单次 `Timeout` 约束校验、授权、日志读写、执行包装、工具与调用后钩子，在准入阶段暂停计时；等待审批时占用并发槽位。
- `interfaces.ToolOutput` 新增 `AdditionalContext`（Builder 在本批工具结果之后作为一条 user 消息注入）与预留的 `Labels`（数据来源与可信度标签，目前不传播）。
- `harness/env`：执行环境能力接口 `FileSystem`（工作区相对路径、条件写入 `WriteIntent`）与 `Executor`（含 `Capabilities`）。实现 `LocalFS`（每次操作重新解析并限定路径）、`LocalExecutor`（Unix 上按进程组清理）、`DockerExecutor`（断网一次性容器，超时删除）；包装器 `Observed`（先读后改）与 `Checkpointed`（改动记录与回滚）。
- `tools/policy`：按工具名与命令前缀裁决 Allow/Ask/Deny 的规则，`Policy.Hook()` 作为调用前钩子；会话批准收窄为命令前缀（`CommandGrant`），列入 `SessionTools` 的工具可整工具批准（`ToolGrant`），含 shell 控制符的命令只能单次批准。
- `tools/coding`：编码 Agent 工作区工具 `read_file`、`write_file`、`edit_file`、`multi_edit`、`glob`、`grep`、`run_command`、`git_status`/`git_diff`/`git_log`/`git_commit`、`todo_write`，经 `env.FileSystem` 与 `env.Executor` 访问工作区。`coding.DefaultPolicy()` 放行读写、命令与提交需审批；`WithLinter` 在写入后检查，失败时撤销写入并把输出交给模型；`SpillHook` 把超长结果写入 `.goagent/spill/`，模型只看到预览与路径；`Workspace.Changes()`/`Revert()` 回滚；`ProjectInstructions()` 读取 AGENTS.md 与 CLAUDE.md。写入类工具拒绝 `.git` 目录，git 工具关闭 fsmonitor、hooks 与外部 diff。示例见 `examples/tools/09-coding-agent`。

### Changed

- Builder 默认 `SessionID` 改为 `session-<纳秒时间戳>`，此前精确到秒，同一秒内创建的 Agent 会共享会话级批准与记忆会话。

## [0.20.0] - 2026-10-30

### Changed

- **破坏性**：`errors.AgentError` 删除导出字段 `Stack`，改为方法 `StackFrames()`。构造时只记录程序计数器，调用 `StackFrames`/`FormatStack` 时才解析；`New` 约从 960ns、656B、3 次分配降到 170ns、32B、1 次分配，`Wrap` 加 `Error()` 约从 1.8µs、18 次分配降到 540ns、11 次分配；结果逃逸到堆上时 `New` 为约 230ns、2 次分配。迁移：`err.Stack` 改为 `err.StackFrames()`。
- **破坏性**：`errors.GetCode(nil)` 返回空字符串，此前返回 `CodeInternal`，使 `IsCode(nil, CodeInternal)` 为 true。依赖 nil 得到 `CodeInternal` 的调用方需先判断 `err != nil`。
- `New`/`Wrap` 不再预先分配 `Context`，未写入时为 nil；`GetContext` 与读取 `Context` 不受影响。
- `ErrorWithRetry`/`ErrorWithDuration` 返回附带信息的副本，不再修改传入的错误；错误链上已有 `*AgentError` 时沿用其错误码，此前被包装后的错误码一律为 `CodeInternal`。

### Fixed

- `Wrap(nil, ...)` 及以 cause 为参数的辅助函数（如 `NewLLMRequestError(p, m, nil)`）在链式调用 `With*` 时 panic；现 `With*`、`Error`、`Unwrap`、`Is`、`Format`、`LogValue` 等方法对 nil 的 `*AgentError` 安全，nil 进 nil 出。
- `mcp/toolbox` 的 `ExecuteWithRetry` 在工具每次都以 `Success=false` 返回而不报错时，`ChainOutputParser.Parse`/`ParseWithPrompt` 未配置解析器时，`ReActOutputParser.ParseWithRetry` 的 `maxRetries <= 0` 时，返回值为 nil 的 `*AgentError`：`err != nil` 成立但没有原因，调用 `Error()` 会 panic；现返回带原因的错误。
- `NewRetrievalEmbeddingError`、`NewParserError`、`NewParserInvalidJSONError` 的内容预览按字节截断，会把中文等多字节字符切成非法 UTF-8；现按字符截断。

## [0.19.0] - 2026-10-30

### Added

- `memory.WithEmbedder` 与 `memory.TextEmbedder`：为 `HierarchicalMemory` 配置嵌入生成后，`Store`/`StoreTyped` 写入的短期与长期记忆立即生成向量并写入向量存储，可被 `VectorSearch` 检索。接受任何实现 `Embed(ctx, text) ([]float64, error)` 的类型，如 `llm.EmbeddingClient`（目前为 `OpenAIProvider`，固定使用 text-embedding-ada-002，1536 维，向量存储维度须一致）。`string`、`[]byte`、`fmt.Stringer` 直接取文本，其余类型取 JSON 序列化结果；nil、空文本或无法序列化的内容不生成向量，条目照常写入。嵌入生成失败返回 `CodeRetrievalEmbedding` 错误，条目不写入。未配置时行为不变。

### Fixed

- DeepSeek 流式请求超过超时（默认 60 秒）被截断：`CompleteStream`/`ChatStream`/`CompleteToolsStream`/`Stream`/`StreamWithTools` 与 `DeepSeekStreamingProvider.StreamWithMetadata` 的超时现只限制等待响应头，流开始后不再计时。
- DeepSeek `Stream`、`StreamWithTools`、`StreamWithMetadata` 无法解析真实 SSE 响应（`data: ...`），且要读完整个响应才产出首块；现改走与 OpenAI 相同的原生 SSE 流，边收边产出。
- 宿主替换 `http.DefaultTransport`（otelhttp、httpmock 等）后 OpenAI/DeepSeek 首次请求 panic；现每次请求使用当时的 `http.DefaultTransport`。等待响应头超时的错误满足 `net.Error` 且 `Timeout()` 为 true。
- OpenAI `Stream`、`StreamWithTools` 请求阶段的错误丢失状态码与 `Retry-After`；现可通过 `errors.As` 取得 `*llm.ProviderError`。
- `HierarchicalMemory.VectorSearch` 并发调用时在读锁下写共享条目的 `Metadata`，可导致 `fatal error: concurrent map writes`；现返回条目副本，相似度写在副本的 `Metadata["similarity_score"]`。
- `VectorSearch` 的 `threshold` 参数此前不生效；现丢弃相似度低于 `threshold` 的结果。
- `WithShortTermCapacity` 此前不生效（短期记忆总以容量 100 构造）。
- 短期条目因容量淘汰、`Forget` 或访问晋升离开时，向量存储残留孤儿向量或误删长期条目的向量；现每个 ID 只保留一个向量：另一层仍持有带向量的同 ID 条目时恢复为它的向量，否则删除。
- `utils/httpclient.GetOrCreateClient` 的缓存键只含 BaseURL 与 Headers，超时、重试等配置不同的客户端被复用为先创建者，例如两个地址与请求头相同但 `WithTimeout` 不同的 DeepSeek Provider，非流式请求共用先创建者的超时；现缓存键包含 `Config` 的全部字段。
- `HierarchicalMemory.Get` 在读锁下更新访问计数并晋升条目，并发 `Get` 同一 key 存在数据竞争；现改持写锁。

### Changed

- DeepSeek `Stream`、`StreamWithTools`、`StreamWithMetadata` 不再自动重试（此前请求阶段最多发 3 次，包括 429 与超时），由调用方控制重试。
- DeepSeek `StreamWithMetadata` 请求阶段的 429 错误码由 `LLM_RATE_LIMIT` 改为 `LLM_REQUEST`，状态码与 `Retry-After` 通过 `*llm.ProviderError` 提供。
- DeepSeek `StreamWithTools`：`tool_call` 块在流正常结束后按工具调用 index 顺序发出，排在所有 `content`/`tool_name`/`tool_args` 之后；参数为空的工具调用不再被丢弃；流在 `finish_reason` 前中断时发出 `error` 块（`io.ErrUnexpectedEOF`）；返回通道缓冲由 100 改为 1；请求不再显式携带 `tool_choice: "auto"`。
- DeepSeek `StreamWithMetadata` 现在发送 `WithSystemPrompt` 设置的 system 消息并携带 `stream_options.include_usage`；正常结束时一定发出 `finish` 块，流中断时以 `error` 块结束。
- 流式方法在流开始后只受 ctx 控制：消费者不再读取时必须取消 ctx，否则 goroutine 与连接不会释放。
- `LongTermMemory.Store`（含 `StoreTyped` 的长期类型与 `Consolidate`）改为先写向量、成功后才写入条目：向量写入失败时返回错误且不留下条目，`Consolidate` 失败的条目保留在短期记忆中。
- `VectorSearch` 返回条目副本，修改返回值不再影响已存储的记忆；同 ID 同时存在于短期与长期时，优先返回持有向量的那一个。

### Known Issues

- `Stream`（返回 `<-chan string`）在流中断或解码失败时只打印日志并关闭通道，调用方无法区分正常结束与中断；需要区分时使用 `CompleteStream`/`ChatStream`。

## [0.18.0] - 2026-10-30

### Added

- `memory.AsVectorStore`：把 `InMemoryVectorStore`、`ChromaVectorStore` 等 `SimpleVectorStore` 适配为 `HierarchicalMemory` 需要的 `VectorStore`。此前包内没有任何实现满足该接口，分层记忆无法接入内置向量存储。元数据不随向量保存，搜索结果只含 ID 与相似度。

### Fixed

- OpenAI Provider 应用 `llm.WithTimeout`（未设置时为 60 秒），此前完全忽略该配置。超时用于限制等待响应开始的时间：非流式请求整体受限，流式请求在首块到达后不再受限，长回复不会被截断。
- `llm.WithTimeout` 对正数时长向上取整到秒，此前不足 1 秒的值被截断为 0 而当作未设置。

### Changed

- OpenAI 非流式请求超过 60 秒（或所设超时）会返回错误，此前没有时限；耗时较长的推理模型请求需用 `llm.WithTimeout` 调大。

## [0.17.2] - 2026-10-30

### Fixed

- 示例中的 `llm.WithTimeout(30)` 等整数写法实为纳秒，按整秒保存后为 0，被当作未设置而回退到默认超时；改为 `30*time.Second` 等（`examples/basic/05-provider-consistency`、`09-deepseek-simple`、`examples/llm/huggingface`，以及 `llm/providers` 的测试）。

### Removed

- `scripts/migrate-to-registry.sh`：会把代码改写为导入不存在的 `llm/registry` 与 `contrib/llm-providers`，执行后无法编译。

### Documentation

- 删除或改写仍在介绍 v0.17.0 已删除包的文档（推理预设、toolkits、stream、reflection、Redis 检查点等），迁到 goagent-contrib 的包注明新导入路径。
- `PROVIDER_BEST_PRACTICES.md` 按实际 API 重写：直接使用 `providers.NewXxxWithOptions`，不存在按名称查找 Provider 的注册表。
- `USER_GUIDE.md` 按当前 API 重写；`LLM_PROVIDERS.md`、`CORE_API.md`、`LLM_API.md`、`tools/README.md`、`memory/README.md`、`mcp/README.md`、各示例 README 等文档中不存在的 API（`WithConfig`、Builder Agent 的 `Invoke`、`providers.WithXxx`、`tools.ToolInput`、`IsLLMError` 等）改为实际 API。所有代码块逐块编译检查，签名清单与示意片段除外。
- 配置了工具的示例改用 `ExecuteWithTools`：`Execute` 只调用一次模型，不执行工具。
- 注明 OpenAI Provider 不读取 `llm.WithTimeout`，需用 ctx 截止时间控制超时；`memory.HierarchicalMemory` 需要应用提供 `memory.VectorStore` 实现。
- 修复 64 处失效的相对链接（带日期的评测记录除外）。

## [0.17.1] - 2026-10-30

只改文档与发布流程，代码与 v0.17.0 相同。

- 发布说明中的仓库内相对链接改写为指向对应 tag 的完整地址，v0.17.0 说明里的定位文档链接此前在 Release 页面上无法打开。
- 本文件去掉从未发布的 `[1.0.0]` 标签（改为“初始提取”）、重复的 `[Unreleased]` 计划列表与失效的尾部链接。
- 设计文档记录 contrib `durable/pgstore/v0.1.1`：作用域墓碑事务改为强制同步提交，并补测获取租约结果不确定时的清理。

## [0.17.0] - 2026-10-30

按[定位决策](docs/research/2026-10-29-goagent-positioning.md)收敛为可恢复编排内核，删除外围模块。主模块 `go.mod` 由 125 行降到 81 行，不再依赖 aiplatform、cohere、qdrant、google api、gorm 系列、pgx、lib/pq、mysql、go-sqlite3（cgo）、nats、goquery、pdf、viper、go-redis。删除的代码保留在 `v0.16.1`，需要时可从该版本恢复。

### Breaking

| 删除的包 | 去向 |
| --- | --- |
| `retrieval` | 迁到 [`goagent-contrib/retrieval`](https://github.com/kart-io/goagent-contrib/tree/master/retrieval)（BM25、混合检索、重排、内存向量库、RAG、OpenAI/HuggingFace 嵌入）；Qdrant、Cohere（嵌入与重排）、Vertex AI 分别在子模块 `retrieval/qdrant`、`retrieval/cohere`、`retrieval/vertexai`。`NewEmbedder` 使用 Cohere 或 Vertex AI 前须空导入对应子包，未导入时返回指明导入路径的错误，`GetSupportedProviders` 不再包含这两项 |
| `document` | 迁到 [`goagent-contrib/document`](https://github.com/kart-io/goagent-contrib/tree/master/document)；`PDFLoader` 在子模块 `document/pdf`（`pdf.NewPDFLoader`） |
| `tools/practical` | 迁到 [`goagent-contrib/tools/practical`](https://github.com/kart-io/goagent-contrib/tree/master/tools/practical)；`DatabaseQueryTool` 在子模块 `tools/database`（`database.NewDatabaseQueryTool`） |
| `agents`、`agents/{base,cot,tot,got,sot,pot,metacot,react,executor,specialized}`、`performance` | 推理策略整体删除。改用原生工具循环 `builder.NewAgentBuilder(...).Build()` 与 `AsRunnable()`，多步流程用 `workflow` Graph；Shell/HTTP 等专用 Agent 改为把 `tools/shell`、`tools/http` 交给原生工具循环 |
| Builder 推理工厂：`CoTAgent`、`QuickCoTAgent`、`ReActAgent`、`QuickReActAgent`、`ToTAgent`、`QuickToTAgent`、`GoTAgent`、`GoTAgentForSlowAPI`、`QuickGoTAgent`、`SoTAgent`、`QuickSoTAgent`、`PoTAgent`、`QuickPoTAgent`、`MetaCoTAgent`、`QuickMetaCoTAgent`、`SupervisorAgent`、`AgentExecutor`、`BuildReasoningAgent` 及 `WithChainOfThought`、`WithZeroShotCoT`、`WithFewShotCoT`、`WithReAct`、`WithTreeOfThought`、`WithBeamSearchToT`、`WithMonteCarloToT`、`WithGraphOfThought`、`WithSkeletonOfThought`、`WithProgramOfThought`、`WithMetaCoT` | 同上 |
| `multiagent`、`distributed` | 进程内子 Agent 用 `tools.NewRunnableToolFactory` 包装，支持持久续跑；跨主机调度不自建，需要时适配 Temporal 等工作流系统。断路器见 `core/middleware` |
| `stream` | 使用 `core.StreamRunnable` 与 Builder 的原生流 |
| `store/postgres`、`store/redis`、`store/factory`、`store/adapters`、`options` | 存储后端由宿主实现 `store.Store`；持久恢复的多实例存储使用 [`goagent-contrib/durable/pgstore`](https://github.com/kart-io/goagent-contrib/tree/master/durable/pgstore) |
| `core/checkpoint` 的 `RedisCheckpointer`、`RedisCheckpointerConfig`、`NewRedisCheckpointer`、`NewRedisCheckpointerFromClient`、`DefaultRedisCheckpointerConfig` | 实现 `checkpoint.Checkpointer`；进程内使用 `checkpoint.NewInMemorySaver` |
| `reflection`、`planning`、`prompt`、`toolkits`、`tools/plugingen` | 删除，无替代 |

同时删除依赖上述包的测试（`integration` 真实服务测试、`testing/test_token_usage.go`、`tools/toolkits_integration_test.go`）与示例（推理策略、multiagent、RAG、文件/网页工具、流式等 40 余个示例目录）。GitHub Release 不再附带 plugingen 二进制。导入检查器删除推理工厂的兼容例外。

## [0.16.1] - 2026-10-30

### Fixed

- v0.16.0 的发布工作流在 Go 1.25 下失败，未创建 GitHub Release：`builder` 的 panic 测试断言调用栈含 `builder.panicTool.`，而 Go 1.25 把返回闭包的 `panicTool` 内联后帧名为 `Test….funcN.panicTool.1`；改为匹配 `panicTool.`。功能代码与 v0.16.0 相同，v0.16.0 的变更与迁移说明见下文。
- golangci-lint v2.6.2 的 errcheck：`core/lifecycle_manager.go` 的 `AgentError.WithContext` 与 `tools/dispatcher.go` 的 `Settle` 显式丢弃返回值，行为不变。

## [0.16.0] - 2026-10-30

### Breaking

- 持久操作记录升级为 `goagent-operation-v2`：新增 `waiting` 状态、`Continuation`、`Reconciled` 字段。v1 记录仍可读取并按原语义处理，写入一律为 v2；直接读取日志文件、按版本号 `goagent-operation-v1` 过滤的宿主代码需同时接受 v2。降级到旧版本后无法读取 v2 记录，回滚前须结束进行中的持久运行。
- OpenAI、DeepSeek、Gemini 原生流的请求错误现在包装为 `*llm.ProviderError`（错误文本不变）。OpenAI/DeepSeek 仍在外层包一层 `AgentError`；Gemini 原样返回的 `genai.APIError` 改为经 `ProviderError` 包装，按具体类型断言的代码需改用 `errors.As`。OpenAI 客户端的 HTTP 层多一层只读包装，用来截取 `Retry-After`。
- 同轮工具并发改为显式开启：`WithParallelToolCalls` 默认 0，小于 2 时按顺序执行，与 master 一致；n>=2 时最多 n 个并发，不另设上限传 `math.MaxInt`。本分支早先的“默认并发”已撤回。迁移：依赖同轮并发的代码显式设置 `WithParallelToolCalls(n)`；原先为关闭并发而设置的 `WithParallelToolCalls(1)` 可以删除。并发时执行先后不保证与模型顺序一致（`Dispatcher.MaxConcurrency=1` 也一样），结果仍按模型顺序交还，某个调用失败不取消其他调用。
- `contextpolicy.ResultStore` 新增 `Release(ctx, runKey)`，自定义实现须补齐：删除该运行的全部证据，运行不存在时返回 nil。Builder 为非持久执行自动生成的结果作用域在执行结束后经新增的 `contextpolicy.Releaser`（`Offload` 已实现）自动释放；宿主经 `WithResultScope` 提供作用域（包括持久执行）时由宿主在运行不再恢复后调用 `Release`。没有持久日志时 Builder 不再用执行键作结果作用域，改用临时作用域。释放后该运行键不可复用：迟到的 `PutIfAbsent` 返回 `contextpolicy.ErrReleased`。`NewDurableResultStore` 的底层存储实现新增的 `durable.Scoped`（`durable.FileStore` 已实现）时才支持 `Release`：每次运行的证据写入独立作用域，`Release` 整体删除并留下一个墓碑以拒绝迟到写入（包括本机其他进程）；持久证据布局随之改为 `scopes/` 子目录，升级前写入的证据不再可读，进行中的持久运行须在升级前结束。读取证据不再加锁，读取不存在的引用不产生文件。已释放运行的 `Get` 返回 `contextpolicy.ErrReleased`（内存版与持久版一致，此前为 `os.ErrNotExist`），持久版 `Release` 写下墓碑后、删除证据前崩溃时，再次 `Release` 之前也读不到残留证据；`ErrReleased` 与 `durable.ErrScopeRemoved` 的文本改为“拒绝读写”。`durable.FileStore` 作用域的 `Load` 在墓碑存在时返回 `durable.ErrScopeRemoved`，仍不加锁、不产生文件；墓碑经 `PruneReleased`/`PruneRemoved` 回收后（残留证据一并删除）`Get` 返回 `os.ErrNotExist`。按 `os.ErrNotExist` 判断“已释放”的调用方需同时识别 `ErrReleased`。
- `contextpolicy.ResultStore` 新增 `PruneReleased(ctx, olderThan)`，`durable.Scoped` 新增 `PruneRemoved(ctx, olderThan)`，自定义实现须补齐：回收释放时间早于 `olderThan` 的墓碑（内存版按释放时间，`FileStore` 按墓碑文件 mtime，并在闸门排他锁内删除崩溃残留目录），重复 `Release` 不刷新释放时间。阈值内的迟到写入仍被拒绝；墓碑回收后该运行键迟到的写入会成功并可能留下残留，宿主应把 `olderThan` 设得大于最长的重试或恢复窗口并定期调用。`FileStore` 加锁后核对闸门仍是路径上的当前文件，回收删除闸门不破坏写入与释放的互斥。
- panic 统一转为 `*errors.PanicError`（`github.com/kart-io/goagent/errors`），可用 `errors.As` 取 `Value` 与 `Stack`。错误文本改为“<位置> 发生 panic”，例如 `工具 "op" 发生 panic`、`工具 "op" 的授权回调 发生 panic`、`工具 "op" 的结果回调 发生 panic`、`持久操作 发生 panic`，替代 `tool "op" panicked`、`operation panicked` 等；按旧文本匹配的代码需改用 `errors.As`。Dispatcher 与持久操作的错误文本仍不含 panic 值，`RunnableTool` 保持在文本中附带 panic 值。
- 同步 `Execute` 不再向调用方抛出中间件链（中间件、模型客户端）内的 panic，改为返回 `*errors.PanicError`，并先经过 errorHandler 与 `OnError`。errorHandler、`OnError`、`OnEnd`、`OnStart`（`Build`/`Initialize`）、`OnShutdown`（`Shutdown`）自身的 panic 同样转为该错误返回，但不再交给 errorHandler 或任何回调，也不再调用剩余回调。非流式客户端的 `ExecuteWithTools` 旧工具循环中（工具、模型、检索、记忆）的 panic 也转为该错误，与该循环的普通错误一样不经过 errorHandler 与回调；`ExecuteStream` 启动 goroutine 前（如持久能力检查）的 panic 转为返回的错误并释放执行权。迁移：调用方原先用 `recover` 接住这类 panic 的，改为检查返回的错误并用 `errors.As` 识别。
- core Runnable 层（`safeInvoke`/`RunnableFunc`、`core.StreamRunnable`、`TypedToDynamicAdapter`、`LifecycleManager` 的 Init/Start/Stop/HealthCheck）经 `PanicHandlerRegistry.HandlePanic` 得到的错误，现在总能用 `errors.As` 取到 `*errors.PanicError`（Op 为 `<component>.<operation>`），`observe.ErrorKind` 因此返回 `panic` 而非 `execution`。`DefaultPanicHandler` 仍返回同文本的 `*AgentError`（CodeInternal），其 `Cause` 改为该 `PanicError`，`Unwrap` 不再为 nil；自定义 `PanicHandler` 的接口不变、仍被调用并决定错误文本，返回值不含 `PanicError` 时由 registry 包装补上，因此不能再对结果做 `err.(*AgentError)` 等具体类型断言，需改用 `errors.As`；handler 返回 nil 时改为返回该 `PanicError`，不再吞掉 panic。传给 handler 与 logger 的调用栈来自该 `PanicError`；panic 值已是 `*PanicError` 时沿用它最初的调用栈。`core.StreamRunnable` 的 `OnStart`、`OnEnd`、`OnError` 自身 panic 时返回 `*PanicError`，不再调用剩余回调，也不再交给 `OnError`；此前 `OnError` 的 panic 会直接抛出使进程退出，`OnStart`/`OnEnd` 的 panic 还会再交给各回调的 `OnError`。执行源的 panic 仍交给 `OnError`。同样的语义（回调自身 panic 时返回 `*PanicError` 替代原结果，不再调用剩余回调，也不交给 `OnError`）扩展到所有调用 `core.Callback` 的位置：`RunnableFunc`、`BaseAgent`、`BaseChain`（`OnChainStart`/`OnChainEnd`/`OnChainError`，含 `Stream`）、`CallbackManager`（`OnStart`/`OnEnd`/`OnError`/`TriggerCallbacks`）、`retrieval.BaseRetriever`、`document` 各加载器，以及 ReAct、SoT、GoT、MetaCoT、PoT、ToT 与 `BaseReasoningAgent` 的回调（含 `OnLLM*`、`OnTool*`、`OnAgentFinish`）；此前这些位置的回调 panic 直接抛给调用方，`BaseChain.Stream` 中会使进程退出。`OnError`、`OnChainError`、`OnChainEnd` 等返回值被忽略的回调，其 panic 现在同样替代原错误返回；`BaseAgent.Invoke` 中 `OnAgentFinish` 的 panic 不再被 `ErrNotImplemented` 掩盖，`BaseChain.Stream` 中 `OnChainEnd` 的 panic 作为最终块的错误。新增 `core.NotifyCallbacks` 与 `CallbackManager.NotifyError` 供自定义组件复用这一语义。`LifecycleManager.HealthCheckAll` 中 HealthCheck panic 时，`HealthStatus.Message` 改为 `PanicError` 的文本（如 `lifecycle_manager.health_check 发生 panic`），`Details` 改为 `{"error_kind": "panic", "panic_op": <Op>}`，与 handler 无关；不再含 panic 值与调用栈（此前默认 handler 带 `panic_value`/`stack_trace`、自定义 handler 为空），二者由 `PanicLogger`/`PanicMetricsCollector` 接收。

### Added

- `durable.Store` 的文档写明完整契约（等锁并保留 ctx 错误、不可重入、长时间持锁与同时持有多个 ID、持锁写入、原子持久写入且全部字段原样保存、`Load` 读到最近一次成功写入且成功时非 nil、不存在时返回 `os.ErrNotExist`），`durable.Scoped` 写明 `RemoveScope` 返回时不保证进行中的持锁者已结束、重复调用不刷新墓碑时间。新增哨兵错误 `durable.ErrLockUnavailable` 与 `durable.ErrLeaseLost`：`FileStore` 在 ctx 结束前未获得运行锁或作用域闸门时，返回同时包装 `ErrLockUnavailable` 和 ctx 错误的错误，`errors.Is(err, context.DeadlineExceeded)` 等判断不受影响；错误文本由裸的 ctx 错误（如 `context deadline exceeded`）改为 `持久运行锁不可用: <ctx 错误>`。新增一致性测试包 `core/durable/storetest`：自定义 Store 用 `storetest.Run` 在同一后端的两个实例上验证上述契约中可在通用环境检验的部分（不含长时间持锁、进程退出后释放与 `ErrLeaseLost`），以及跨实例审批续跑与持久操作回放；`FileStore` 已接入。以租约实现锁的后端可设置 `storetest.Options.Leases`（TTL 与 `Expire`、`Crash` 两个钩子），额外验证崩溃后接管、续约维持长时间持锁、接管后旧持有者写入返回 `ErrLeaseLost`（含同一进程内的旧持有者）、过期但未被接管时仍可写入。
- `tools.Rejected(message, data)` 与 `tools.RejectedResult`：业务拒绝作为工具结果交给模型，经 Dispatcher 持久执行时记为已完成、恢复时只回放；此前 Dispatcher 把任何 `Success=false` 结果当作失败并中止执行。直接返回工具收到业务拒绝时不作为答案，继续下一轮。业务拒绝只认 `Rejected` 构造的结果：`Result` 是包外无法构造的类型（JSON 编码仍为 `{"error":"rejected","message":...,"data":...}`），透传上游 JSON 的工具拿到同形状的 map 按普通失败处理；Dispatcher 日志在 `ToolOutput` 之外以 `goagent_rejected` 字段标记拒绝，回放时据此重建。迁移：按 `Result.(map[string]any)` 读取拒绝内容的代码改用 `tools.RejectedResult` 取 message，或对 `Result` 做 JSON 编码；此前写入、没有该标记的拒绝记录回放时按普通失败处理，进行中的持久运行须在升级前结束。
- `tools.ReconcilableTool`：执行中断、结果未知的调用在重跑前按原执行键核对，已生效则补记为完成而不再执行。`durable.OperationPolicy` 新增 `Reconcile` 与 `Resumable`。
- Builder 新增 `WithModelRetry(n, backoff)`（默认 0，不重试）：原生流路径在 429、5xx、连接错误时于同一回合内重试，已输出文本后不重试；优先按 `Retry-After` 等待，不越过截止时间，`Retry-After` 超过 1 分钟时不等待、直接失败（等待期间持有回合的操作锁）；OpenAI/DeepSeek 的 `Retry-After` 无法解析时视为未给出、改用 backoff，此前按 60 秒处理；每次重试先占根预算（预算键为回合键派生的 `retry/N`），计入 `new_model_invocations`，不计入 `model_calls` 与 `MaxIterations`；持久日志仍为一条回合记录，配置不进入意图。新增观测事件 `model_retry`。
- `llm.ProviderError`（`StatusCode`、`RetryAfter`、`Retryable()`）。`Retryable` 对没有状态码的错误按白名单判断：网络超时、连接被拒绝或重置、连接意外关闭（EOF）、DNS 暂时失败可重试；证书与 TLS 错误、域名不存在、不支持的协议等不重试（`*url.Error` 总是实现 `net.Error`，不能据此认定为连接错误）。OpenAI/DeepSeek 截取 `Retry-After` 响应头，Gemini 读取 `google.rpc.RetryInfo`。
- 嵌套续跑：`RunnableToolConfig.Resumable`、`workflow.DurableGraph.AsChildRunnable`、`durable.WaitError`/`durable.Waits`、`durable.WithResume`/`ResumeInput`、`builder.WaitingError`/`builder.ErrWaiting`。子图审批暂停时父 Agent 以 `ErrWaiting` 结束，宿主按等待路径提供审批后用同一执行键再次调用即可续跑，已完成的模型回合与工具调用只回放。未被采纳的审批保持等待并标记 `Rejected`，`ChildContinuation.Reason` 给出原因类别：`approval_mismatch`（审批键不符，`workflow.ErrApprovalMismatch`）或 `before_approve`（`WithBeforeApprove` 的钩子返回错误，`*workflow.ApprovalRejectedError`）；检查点读写失败、钩子 panic、解锁失败等照常作为失败返回，不再被改写成未采纳的等待而丢失原始错误。等待只能由声明 `OperationPolicy.Resumable` 的持久操作承载：其他操作收到 `*durable.WaitError` 时记为失败（`FailureKind` 为 `not_resumable`）并返回 `durable.ErrWaitNotResumable`，此后返回 `OperationError`，不再记成永远无法续跑的等待；`durable.CanWait` 报告当前操作能否承载等待，`AsChildRunnable` 在不可续跑的操作内于执行任何节点前失败。`AsChildRunnable` 的检查点与节点内持久操作写在父操作日志的 Store（新增 `durable.JournalStore`）中，不使用 `DurableConfig.Store`，子图与父流程因此总是共用同一 Store，崩溃后续跑不会因子图 Store 不同（如进程内存）而从头重跑。观测的错误类别对等待记为 `waiting`。

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
- `ExecuteStream` 中工具执行失败时，终态 `error` 事件的 `Tool` 填为失败的工具名（工具选择路径与原生工具流的顺序、并发执行均适用），此前为空，消费者无法知道是哪个工具失败。
- 破坏性变更：`(*errors.AgentError).Error()` 不再输出 `Context`（此前追加 `(k=v, ...)`），避免 URL、模型参数、用户输入随错误文本进入日志、事件与模型输入；`*AgentError` 新增 `LogValue()` 实现 `slog.LogValuer`，结构化日志以 group 输出含 `context` 的全部字段；新增 `errors.RedactURL`，连接类错误与各工具、存储、通信组件写入 `url`/`endpoint` 前去掉 userinfo、query 与 fragment。按 `Error()` 文本匹配 Context 键值的代码需改用 `GetContext` 或 `LogValue`；`distributed.Coordinator` 不再因非 2xx 响应体含 `timeout` 等关键字而故障转移。
- `InputValidator` 的校验错误 Message 带上具体原因与参数名（如 `required parameter validation failed: required parameter is missing: query`），不含参数值，`Error()` 交还模型时仍可据此改正。
- `postgres.New` 连接失败时不再把 DSN 写入错误，Context 的 `endpoint` 改为由 pgx 解析出的 `host/dbname`，解析失败则不写，key=value 形式 DSN 中的密码不再进入 Context 与 `Error()`；`github.com/jackc/pgx/v5` 由间接依赖变为直接依赖。
- `specialized.HTTPAgent` 请求失败时，错误 Context 的 `url`、底层 `*url.Error` 进入错误文本的 URL 以及请求日志中的 `url` 均经 `errors.RedactURL` 去掉 userinfo、query 与 fragment，此前为原始 URL。
- 推理 Agent 的 LLM 降级分支不再吞掉 panic：LLM 调用返回 `*errors.PanicError`（回调或 LLM 客户端 panic 被恢复）时，SoT 加权聚合、GoT 思考扩展与 LLM 评分、CoT 工具结果后的再推理、MetaCoT 的问题分解/追问/综合/自我批评/改进不再退回默认答案、中性分或跳过该步，而是返回该错误、以失败结束且不再调用模型；`agents.HybridRouter` 中某个策略返回 `*PanicError` 时直接返回，不再投票或退回 fallback。普通 LLM 错误的降级行为不变。
- `workflow.DurableGraph` 中存储/序列化、检查点解码与分支路由的 panic 改为返回 `*errors.PanicError`（保留 panic 值与调用栈，可用 `errors.As` 识别），此前为丢弃 panic 信息的固定文本错误；错误文本由 `... panicked` 变为 `... 发生 panic`。
- `tools.ValidateInputWithSchema`（`mcp/toolbox.StandardToolBox.Validate` 使用）的校验错误 Message 带上字段名（如 `required field is missing: name`、`expected string: tags[1]`），不含参数值，此前字段名只在 Context，`Error()` 交还模型时无从知道是哪个字段。
- `InputValidator` 的自定义校验（`ValidatableTool.Validate`）返回非 `AgentError` 时，`Error()` 只给固定的 `tool custom validation failed: 校验未通过`，原文仅放在 Context 的 `validation_error`，此前原文（可能含参数值）直接拼进 Message；返回 `AgentError` 时取其 Message 与参数名。
- `distributed.Coordinator` 改为按错误类型决定故障转移：`context.DeadlineExceeded`、`net.Error` 超时、`ECONNREFUSED`/`ECONNRESET` 以及 Context `status_code` 为 502/503/504 时转移，调用方 ctx 已结束时不转移；此前靠匹配 `Error()` 文本，网关类状态码与 `Client.Timeout` 超时都不会转移，而仅文本含 `timeout` 等关键字的错误会被误转移。
- MetaCoT 搜索失败后回退为直接回答时，直接回答返回的 `*errors.PanicError` 不再被丢弃，本次执行以该错误失败；普通错误仍只留空答案。推理 Agent 与 `HybridRouter` 的降级分支统一用 `agents/base.IsPanic` 判断。
- `agents.SupervisorAgent` 子任务 goroutine 中路由器或子 Agent 直接 panic 时不再导致进程退出：panic 转为 `*errors.PanicError` 写入该任务的 `TaskResult`。
- `agents.SupervisorAgent.Invoke` 如实反映子任务结果，此前不论子任务是否失败都返回 nil 并计为成功：任一子任务结果含 `*errors.PanicError` 时返回这些错误（`errors.Join`，可用 `errors.As` 识别）；全部子任务失败时返回包装全部子任务错误的 `CodeAgentExecution`；部分失败仍返回汇总结果，`Status` 为 `partial`、`Metadata["tasks_failed"]` 为失败数，计入新增的 `partial_tasks` 指标而不计入 `successful_tasks`。返回错误时同时返回 `Status` 为 `failed` 的汇总输出。新增 `SupervisorConfig.FailOnAnyError`（默认 false），为 true 时任一子任务失败即返回错误。重试等待改为响应 ctx 取消，`*PanicError` 不再重试（即使匹配 `RetryableErrors`）。
- `core.InterruptManager.CancelInterrupt` 对已取消的中断返回 `CodeAgentNotFound`，此前同 ID 重复取消返回 nil；保存状态快照期间的首次取消仍然生效。
- `planning.AgentExecutor` 的可重试错误判断改为不区分大小写的子串匹配，此前只匹配前缀/后缀，`dial tcp ...: connection refused by peer`、`Service Unavailable` 等错误不会重试。
- `errors.RedactURL` 堵住绕过：逗号分隔的多地址（NATS 集群）逐段脱敏，query 中逗号之后的片段一并丢弃；密码含未转义 `/`、`?`、`#` 时丢弃 scheme 之后到最后一个 `@` 为止的内容（`host:port/path` 且路径含 `@` 的地址会被过度裁剪）；key=value 形式 DSN 只保留 `host`、`port`、`dbname`。凭据放在路径中的 URL（Slack webhook、Telegram bot）不处理。
- `*errors.AgentError` 实现 `zapcore.ObjectMarshaler`，zap 引擎（含 kart-io/logger 的 `Errorw(..., "error", err)`）以对象输出与 `LogValue` 相同的字段（含 `context`），此前只有 `Error()` 文本、Context 丢失；同时实现 `fmt.Formatter`，`%v`/`%s` 同 `Error()`，`%+v` 追加按键排序的 `context: k=v, ...`（`zap.Error` 的 `errorVerbose` 字段随之包含 Context）。zap 日志中该字段由字符串变为对象；`go.uber.org/zap` 由间接依赖变为直接依赖。
- 新增 `errors.RedactURLError`：返回把错误链上 `*url.Error` 的 URL 经 `RedactURL` 脱敏后的新错误，不修改原错误，`Unwrap` 仍得到原底层错误（如 `*net.OpError`），`errors.Is(context.DeadlineExceeded)`、`Timeout()` 与外层 `*AgentError` 的 Code 不受影响。`tools/practical` 的 `api_caller`（`auth.location=query` 时注入的 api key）与 `web_scraper`、`tools/http.APITool`、`mcp/tools.HTTPRequestTool`、`distributed.Client` 全部请求方法、`document.WebLoader`/`PDFLoader` 的传输与 URL 解析错误，在返回、写入 `ToolOutput.Error`/`ToolResult.Error` 或交给回调前统一脱敏，此前 resty/net/http 错误文本中的完整 URL（含 query 凭据）会经 observation 交给模型；`distributed.Client` 的 Debug 日志中的 `endpoint`/`url` 同样脱敏；`specialized.HTTPAgent` 改用该函数（不再就地改写底层 `*url.Error`），成功路径 `ToolCalls[].Input.url` 也脱敏。
- `utils/httpclient.NewClient` 创建的 resty 客户端改用对错误参数做 URL 脱敏的 logger，重试时的 `WARN RESTY ... Attempt` 日志不再打印 query 中的 api key 与 userinfo。`Config.Debug=true` 时 resty 以字符串打印的请求日志中，绝对 URL 与带 query/fragment 的请求 URI 同样经 `errors.RedactURL` 脱敏；请求与响应日志中的 `Authorization`、`Proxy-Authorization`、`Cookie`、`Set-Cookie` 及名称含 key/token/secret 的头替换为 `[REDACTED]`（只改日志副本，不影响实际请求）。
- `api_caller.ExecuteWithRuntime` 从 runtime state（键 `api_key_<url>`）读取的 api key 真正用于请求，此前写在重新解析出的参数副本上、`Execute` 又解析原始参数，注入从不生效；`credentials` 缺省时不再因写 nil map 而 panic。
- `executor.AgentExecutor` 的 `Execute` 与 `RunGenerator` 经 `core.CallSafely` 调用 `Memory.SaveContext`：SaveContext panic 或返回 `*errors.PanicError` 时本次执行失败，`Execute` 返回 Status 为 `failed` 的输出与该错误，`RunGenerator` 在成功输出之后再产出一条 `failed` 输出与该错误，此前 panic 直接崩溃调用方、PanicError 被当作普通错误吞掉；普通错误仍尽力而为、不影响结果。
- `tools/practical.WebScraperTool` 成功与失败结果中的 `url`，以及 `document.WebLoader` 回调的 `url`、`document.PDFLoader` 远程来源回调的 `source`，改为经 `errors.RedactURL` 脱敏，此前原样带出 userinfo 与 query 凭据；本地文件路径与文档 `Metadata["source"]`（数据）保持原值。
- `tools/practical.WebScraperTool` 以请求 URL 解析页面相对链接与图片时先去掉请求的 userinfo，只含 fragment 的自引用不再继承请求 query；此前 `links`/`images` 会带出 `user:pass@` 与 query 中的令牌。页面中的绝对链接保持原样。
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

## 初始提取 - 2025-11-15

该段原标为 1.0.0，但项目从未发布 v1.0.0；内容对应首个 tag v0.0.1 之前的初始代码，其中多数模块已在 0.17.0 删除。

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

## 版本说明

### 版本号

遵循[语义化版本](https://semver.org/lang/zh-CN/)。1.0 之前，破坏性变更只升次版本号（如 0.16 → 0.17）。

### 发布流程

1. 在本文件对应版本下记录变更
2. 合入 master 后由 Auto Tag 工作流按提交类型计算版本并打 tag
3. Release 工作流从本文件提取同名版本段落作为发布说明

---

[Unreleased]: https://github.com/kart-io/goagent/compare/v0.50.0...HEAD
[0.50.0]: https://github.com/kart-io/goagent/compare/v0.49.2...v0.50.0
[0.49.0]: https://github.com/kart-io/goagent/compare/v0.48.1...v0.49.0
[0.48.1]: https://github.com/kart-io/goagent/compare/v0.48.0...v0.48.1
[0.48.0]: https://github.com/kart-io/goagent/compare/v0.47.1...v0.48.0
[0.47.1]: https://github.com/kart-io/goagent/compare/v0.47.0...v0.47.1
[0.47.0]: https://github.com/kart-io/goagent/compare/v0.46.0...v0.47.0
[0.46.0]: https://github.com/kart-io/goagent/compare/v0.45.0...v0.46.0
[0.45.0]: https://github.com/kart-io/goagent/compare/v0.44.0...v0.45.0
[0.44.0]: https://github.com/kart-io/goagent/compare/v0.43.1...v0.44.0
[0.43.1]: https://github.com/kart-io/goagent/compare/v0.43.0...v0.43.1
[0.43.0]: https://github.com/kart-io/goagent/compare/v0.42.1...v0.43.0
[0.42.1]: https://github.com/kart-io/goagent/compare/v0.42.0...v0.42.1
[0.42.0]: https://github.com/kart-io/goagent/compare/v0.41.0...v0.42.0
[0.41.0]: https://github.com/kart-io/goagent/compare/v0.40.0...v0.41.0
[0.40.0]: https://github.com/kart-io/goagent/compare/v0.39.1...v0.40.0
[0.39.1]: https://github.com/kart-io/goagent/compare/v0.39.0...v0.39.1
[0.39.0]: https://github.com/kart-io/goagent/compare/v0.38.1...v0.39.0
[0.38.1]: https://github.com/kart-io/goagent/compare/v0.37.0...v0.38.1
[0.37.0]: https://github.com/kart-io/goagent/compare/v0.36.1...v0.37.0
[0.36.1]: https://github.com/kart-io/goagent/compare/v0.36.0...v0.36.1
[0.36.0]: https://github.com/kart-io/goagent/compare/v0.35.0...v0.36.0
[0.35.0]: https://github.com/kart-io/goagent/compare/v0.34.0...v0.35.0
[0.34.0]: https://github.com/kart-io/goagent/compare/v0.33.0...v0.34.0
[0.33.0]: https://github.com/kart-io/goagent/compare/v0.32.2...v0.33.0
[0.32.2]: https://github.com/kart-io/goagent/compare/v0.32.1...v0.32.2
[0.32.1]: https://github.com/kart-io/goagent/compare/v0.32.0...v0.32.1
[0.32.0]: https://github.com/kart-io/goagent/compare/v0.31.0...v0.32.0
[0.31.0]: https://github.com/kart-io/goagent/compare/v0.30.1...v0.31.0
[0.30.1]: https://github.com/kart-io/goagent/compare/v0.30.0...v0.30.1
[0.30.0]: https://github.com/kart-io/goagent/compare/v0.29.1...v0.30.0
[0.29.1]: https://github.com/kart-io/goagent/compare/v0.29.0...v0.29.1
[0.29.0]: https://github.com/kart-io/goagent/compare/v0.28.0...v0.29.0
[0.28.0]: https://github.com/kart-io/goagent/compare/v0.27.0...v0.28.0
[0.27.0]: https://github.com/kart-io/goagent/compare/v0.26.0...v0.27.0
[0.26.0]: https://github.com/kart-io/goagent/compare/v0.25.0...v0.26.0
[0.25.0]: https://github.com/kart-io/goagent/compare/v0.24.0...v0.25.0
[0.24.0]: https://github.com/kart-io/goagent/compare/v0.23.1...v0.24.0
[0.23.1]: https://github.com/kart-io/goagent/compare/v0.23.0...v0.23.1
[0.23.0]: https://github.com/kart-io/goagent/compare/v0.22.0...v0.23.0
[0.22.0]: https://github.com/kart-io/goagent/compare/v0.21.0...v0.22.0
[0.21.0]: https://github.com/kart-io/goagent/compare/v0.20.0...v0.21.0
[0.20.0]: https://github.com/kart-io/goagent/compare/v0.19.0...v0.20.0
[0.19.0]: https://github.com/kart-io/goagent/compare/v0.18.0...v0.19.0
[0.18.0]: https://github.com/kart-io/goagent/compare/v0.17.2...v0.18.0
[0.17.2]: https://github.com/kart-io/goagent/compare/v0.17.1...v0.17.2
[0.17.1]: https://github.com/kart-io/goagent/compare/v0.17.0...v0.17.1
[0.17.0]: https://github.com/kart-io/goagent/compare/v0.16.1...v0.17.0
[0.16.1]: https://github.com/kart-io/goagent/compare/v0.16.0...v0.16.1
[0.16.0]: https://github.com/kart-io/goagent/compare/v0.15.0...v0.16.0
