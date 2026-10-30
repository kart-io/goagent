# Changelog

All notable changes to GoAgent will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added

- Agent 句柄：`ConfigurableAgent.Start(ctx, input)` 返回 `*Handle`，运行中可 `Steer`（下一步追加，结束前唤醒续跑）、`Inject`（下一步追加，不唤醒）、`Redirect`（取消进行中的模型请求，保留已流出的文本后重新请求；工具执行中或持久运行中降级为 Steer）、`Followup`（本轮结束后续接对话开始新一轮）、`Interrupt`，`Wait` 取得结果；轮次结束后再追加输入返回 `ErrHandleDone`。只支持原生工具流。
- 循环扩展点（原生工具流）：`WithPreStep`（改写消息或不请求模型直接结束）、`WithRequestHook`（替换客户端或调整参数）、`WithRequestError`（按错误分类重试、压缩或回退，单步恢复次数有上限）、`WithTurnStopping`（返回 user 消息续跑）。
- 提示词组装：`WithPromptSections` 追加到 system 且同一轮内字节不变；`WithPromptContexts` 每步重算，附在请求末尾、不进入之后的历史。
- 持久运行中 PreStep、TurnStopping、提示词段落与上下文、收件箱输入按步骤记入日志，恢复时回放；RequestError 钩子与 Redirect 只在非持久运行中生效。
- `llm.ClassifyError`：把模型请求错误分为限流、服务端、网络、超出上下文、鉴权、不存在、请求错误、取消，并给出 `Retryable`、`ShouldCompress`、`ShouldFallback` 提示。

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

[Unreleased]: https://github.com/kart-io/goagent/compare/v0.18.0...HEAD
[0.18.0]: https://github.com/kart-io/goagent/compare/v0.17.2...v0.18.0
[0.17.2]: https://github.com/kart-io/goagent/compare/v0.17.1...v0.17.2
[0.17.1]: https://github.com/kart-io/goagent/compare/v0.17.0...v0.17.1
[0.17.0]: https://github.com/kart-io/goagent/compare/v0.16.1...v0.17.0
[0.16.1]: https://github.com/kart-io/goagent/compare/v0.16.0...v0.16.1
[0.16.0]: https://github.com/kart-io/goagent/compare/v0.15.0...v0.16.0
