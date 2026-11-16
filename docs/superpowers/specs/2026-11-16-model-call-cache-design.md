# 模型调用缓存：替换整轮 CacheMiddleware

日期：2026-11-16。状态：已按架构与安全两份审核修订（第 10 节），待用户批准。

## 1. 意图

`middleware.CacheMiddleware` 包住整轮 Agent 执行（`builder/configurable_agent.go` 的 `chain.Execute`），以 `fmt.Sprint(request.Input)` 为键，命中时跳过整轮处理器。由此产生三个问题：

1. **跨用户命中**：共享实例时，不同用户以相同输入请求拿到彼此的回答。
2. **同会话返回旧答案**：历史已变化，相同输入仍命中。
3. **命中时不写会话日志**：跳过处理器，这一轮不进入会话日志、持久日志与长期记忆抽取。

分支 `fix/cache-middleware-session-scope`（`3aedb41a`，未推送）只修了第 1 项，而且是按会话 ID 隔离的；审核发现会话 ID 不能作为隔离依据（第 3 节）。用户要求从根本上解决：把缓存下沉到单次模型调用，以完整请求为键，由宿主声明可信的隔离身份，并且允许与长期记忆同时使用。

成功的标志：

- 不同隔离身份（租户、用户）永远不共享缓存；同一身份下请求有任何变化都不命中。
- 命中只省掉一次模型调用，会话日志、持久日志、工具执行、记忆抽取与未命中时相同（差异只在第 5.5 节列出的计数与证据来源）。
- 遗忘、回退或 Purge 之后，被删内容不会经缓存回到模型或用户（逻辑删除），并按配置在 `EraseWithin` 内从缓存存储中物理删除。

## 2. 目标、非目标与反目标

目标：

- Builder 级的模型调用缓存，覆盖 `readJournaledTurn` 与 `readPlainModelTurn` 两个入口。
- 键包含宿主声明的隔离身份、会话 ID、客户端身份与解析后的模型、完整消息（含真实召回块）、采样参数、规范化后的工具定义，以及长期记忆的版本。
- 与长期记忆共存：记忆版本进入键（逻辑失效）；遗忘、回退、Purge 时经通用擦除回调按标签物理删除（可配置）。
- 删除 `CacheMiddleware` 及其相关代码，不保留兼容层。

非目标：

- 语义缓存（按相似度命中）。已删除的 `llm/cache` 证明这类键无法隔离用户；本设计只做精确匹配。
- 缓存工具结果。已由 `tools/middleware.Caching` 负责（按会话与调用者隔离）。
- provider 侧的 prompt caching（Anthropic、OpenAI、Gemini 的前缀缓存），与本设计无关，可以并存。
- 嵌入（embedding）调用的缓存。
- 缓存值的加密。缓存后端的保密由宿主负责（第 5.7 节）。
- 单飞（single-flight）。两个相同请求并发到达时各自未命中、各调一次模型，答案可能不同。不合并：持久日志按操作键记录，合并会让两个操作共享一个结果。

反目标（即使实现了也算失败）：

- 任何情况下把一个隔离身份的回答返回给另一个隔离身份。
- 命中时少写或多写任何会话日志事件，或改变持久运行的恢复语义（包括带预算的运行）。
- 遗忘、回退或 Purge 提交之后，仍能取到依据被删内容生成的缓存回答。
- `EraseRequired` 下，遗忘、回退或 Purge 提交超过 `EraseWithin` 后，缓存存储中仍有带被删内容的条目。
- 为通过检查而放宽 Builder 对长期记忆的现有约束（例如请求钩子不得改写召回消息）。

## 3. 约束（来自现有代码）

- **分层**（`scripts/apicheck/main.go` 的 `layers`）：`llm`、`core/middleware` 低于 `tools`、`memory`；`cache` 在第 1 层，`memory` 与 `builder` 都可以导入它。缓存逻辑放在 `builder`（最高层）。不要在 `llm` 层包装 `Client`：包装会让 `llm.ToolStreamClient`、`ReasoningRequestValidator` 等类型断言失效，且 `llm` 层拿不到会话。`memory` 不导入 `cache`，只暴露通用擦除回调（第 5.4 节）。
- **会话 ID 不是隔离依据**：`DefaultAgentConfig()` 在构建时生成 `session-<UnixNano>`（`builder/config_options.go:207`），`sessionLogID` 在调用方没传 `tools.WithSessionID` 时返回它（`builder/session_log.go:129`）。共享一个 Agent 的所有用户因此拿到同一个会话 ID；`tools.WithSessionID` 又是调用方任意可设的字符串。隔离身份必须由宿主显式声明（第 5.1 节的 `Scope`）。
- **模型调用的汇合点**是 `builder/model_call.go` 的 `modelOperation`。预算预留（`reserve`）与调用计数在 `send` 内、`invoke` 之前发生（`model_call.go:123-151`），所以缓存查找不能放进 `invoke`，必须在 `modelOperation` 中、调用 `call` 之前分支（第 5.2 节）。
- **预算记账**：`budget.RecordEvidence` 对没有预留的键返回 `usage has no reservation`（`core/budget/attribution.go:120`），回放时 `recordUsage(..., true)` 也会走到它。命中既然不预留，就必须在实时和回放两条路径上都跳过记账。
- **非持久路径没有校验**：`readJournaledTurn` 在非持久时传入的 `validate` 为 nil（`builder/durable_turn.go:43`），`readPlainModelTurn` 也没有。缓存写入前必须另行校验。
- **模型名解析**：`withModelMetadata` 在没开观测、预算、用量钩子时直接返回（`builder/agent_usage.go:96`），不能作为键的来源。
- **持久意图不能当缓存键**：`readJournaledTurn` 计算意图时用 `recallIntent` 把召回块换成占位消息，不同命名空间的召回会得到相同意图。缓存键必须使用真实消息。
- **长期记忆的删除承诺**（以代码为准，`docs/design/MEMORY_GOVERNANCE.md` 0.1 节已在 `28b25d3a` 更正）：
  - `Forget` 立即清空正文，并在 `Config.EraseWithin`（默认 5 分钟）内从基础快照物理擦除。记录已遗忘时直接返回（`memory/memory.go:575`），所以"重试 `Forget`"不会重做任何清理。
  - `Forget` 的索引清理先在提交前经 `withBeforeSave` 登记 `queueIndex`，再由 `Maintain` 重做（`memory.go:585-591`、`memory/job.go:351`）。缓存清理沿用这个机制。
  - `Purge` 把命名空间切到新作用域，旧作用域进入 retiring，由 `cleanupRetiring` 可重复地物理删除（`memory/scope.go`）。
  - `Rewound` 在一次提交中失效记录、恢复被取代的记录与关系、写回退标记、补登任务；不改变 `LastForgetSeq` 与 `MarkerHorizon`（`memory.go:629` 起）。因此需要新增一个回退序号，见 5.3 节。
  - TTL 过期、`MaxRecords` 淘汰、合并与反思不抬高任何序号。它们不需要：被淘汰的内容不再被召回，下一次请求的召回块自然不同，键随之变化。记忆版本只覆盖"删除类"操作。
- **读命名空间**：`memory.TurnMemory.Read` 的类型是 `[][]string`，但目前 `Memory.Turn` 只填写一个（`memory/recall.go`）。本设计按列表处理键与标签，不新增多读功能，测试只覆盖单个命名空间。
- **Worker 可能在独立进程**：物理删除发生在记忆的维护与删除路径中，那里没有 Builder。所以擦除回调配置在 `memory.Config`，由宿主在每个进程里装配。
- 项目规则：破坏性变更不做兼容；规则写成数据（表、结构体字面量）；测试表驱动。

## 4. 方案

考虑过三种失效方式：

| 方式 | 做法 | 取舍 |
|---|---|---|
| A. 记忆版本进入键 | 每次调用前读取各读命名空间的作用域、`LastForgetSeq` 与 `LastRewindSeq` 算进键；删除后键变化，旧条目不可达 | 正确性不依赖任何通知；多进程共享缓存时同样成立。旧条目在物理删除前残留 |
| B. 删除时主动清缓存 | 遗忘、回退、Purge 时按命名空间删除缓存条目 | 物理上及时清除；单独使用时正确性依赖删除是否成功 |
| C. A + B | A 保证逻辑删除，并用于写入前的复核（第 5.2 节）；B 负责物理删除 | 两套机制都要实现，但职责不重叠 |

**用户选择 C，审核后再次确认保留 A。** 架构审核指出：召回块已在请求里，删除后请求本身会变，A 在逻辑上大多冗余。保留 A 的理由有两点：

- 它是不依赖召回实现细节的第二道保证；
- 写入前比对版本，才能关闭"删除之后，一次进行中的调用再把旧回答写回缓存"的竞态。

缓存的层级也考虑过：保留整轮缓存并在命中时补写日志（LangGraph 节点缓存的做法），需要伪造一整轮事件，与持久运行的恢复语义冲突，不采用。调研依据：LangChain、AutoGen、LangChainGo、Vercel AI SDK、Mastra 都只缓存单次模型调用，以完整消息为键；Mastra 默认按用户作用域隔离。

## 5. 设计

### 5.1 配置

Builder 增加一个选项（名称由实施者定，建议 `WithModelCache`），参数是结构体字面量：

```go
mc := builder.ModelCache{
	Cache:  c,                 // cache.Cache；物理删除需要实现 cache.Tagged
	TTL:    5 * time.Minute,   // 必须 > 0
	Scope:  func(ctx context.Context) (string, bool) { return tenantUser(ctx) }, // 可信隔离身份；返回 false 则本次不缓存
	Erase:  builder.EraseAuto, // EraseOff / EraseAuto（默认）/ EraseRequired
	TagKey: key,               // 标签摘要的 HMAC 密钥，使用标签时必填，至少 32 字节
}
builder.WithModelCache(mc)
```

长期记忆一侧只接一个通用回调，`memory` 不知道"缓存"：

```go
memory.Config{
	// ...
	// OnErase 在删除类操作需要清除派生副本时被调用；失败由维护任务重做（第 5.4 节）。
	OnErase: mc.Eraser(), // func(ctx context.Context, ns []string, scope string) error
}
```

`ModelCache` 另提供 `DeleteSession(ctx, sessionID) error`，供宿主在删除或回退会话时调用（第 5.4 节）。

`Scope` 的默认值：
- 配置了长期记忆时，`Scope` 可以省略，默认取 `memory.NamespaceFrom(ctx)`，即 `memory.WithNamespace` 绑定的可信身份。命名空间为空时不缓存。
- 没有长期记忆时，`Scope` 必填。

`Build` 的校验（写成规则表，沿用 `validateLongTermMemory` 的方式）：

| 条件 | 结果 |
|---|---|
| `Cache == nil` 或 `TTL <= 0` | 配置错误 |
| `Scope == nil` 且没有配置长期记忆 | 配置错误：没有可信的隔离身份 |
| `Erase` 为 `EraseRequired` 且缓存未实现 `cache.Tagged` | 配置错误 |
| `Erase` 为 `EraseRequired`，配置了长期记忆，但记忆未配置 `OnErase` | 配置错误（`memory` 导出只读方法报告是否配置了擦除回调，名称由实施者定） |
| `Erase` 为 `EraseRequired` 且 `TTL > memory.Config.EraseWithin` | 配置错误：物理删除失败时，残留时长的上限就是 TTL |
| 缓存实现了 `cache.Tagged`，`Erase` 不是 `EraseOff`，但 `len(TagKey) < 32` | 配置错误 |
| `Erase` 为 `EraseAuto` 且缓存未实现 `cache.Tagged` | 构建成功，记录一条警告：物理删除退回 TTL |
| `Erase` 不是 `EraseRequired` 且 `TTL > memory.Config.EraseWithin` | 构建成功，记录一条警告：被删内容可能在缓存中残留到 TTL |

独立的 Worker 进程不经过 Builder，必须用同一个 `ModelCache`（同一后端、同一 `TagKey`）装配 `OnErase`。这一点写进使用文档。`EraseRequired` 时，Builder 只能校验本进程的记忆实例。

### 5.2 何时缓存与放在哪里

- **何时缓存**：`Scope(ctx)` 返回 `true` 且身份非空，并且模型身份可以解析（第 5.3 节）时才缓存，否则这次调用既不查也不写。`readJournaledTurn`、`readPlainModelTurn` 是自由函数，拿不到 Agent，由 Agent 在调用前把缓存配置、隔离身份与会话 ID 经 ctx（builder 包内私有键）传入。
- **查找**：放在 `modelOperation` 中、调用 `call` 之前。持久路径在 `durable.Do` 的回调内，因此已记录的结果照常从日志回放，不查缓存；非持久路径在 `call(ctx)` 之前。键在查找前**只计算一次**，写入时复用同一个键。
- **命中分支**：直接得到结果，不执行 `reserve`、调用计数、`model_attempt_start` 与 `send`。持久路径把结果写成 `savedTurn{Cached: true, EvidenceSource: "cache"}`（新增 `Cached` 字段）。
  - `recordUsage` 在命中时（实时：`lastAttempted` 仍为 -1，现有判断已经跳过）以及回放 `Cached` 记录时都跳过，不对没有预留的键记账。
  - 命中后仍要调用 `validate`，与写入前的校验相同；校验失败按未命中处理，删除该条目后正常调用模型。
- **写入**：只在下列条件同时满足时写入：
  - 结果来自真实调用：不是命中，也不是回放，`attempted && !record.Replayed`；
  - 没有错误；
  - `FinishReason` 为 `stop` 或 `tool_calls`；
  - 通过 `validateDurableTurn` 的校验，非持久路径也要做：`allowEmpty=false`，`feedback` 模式下的非法工具调用不写；
  - 回答不含 `ProviderState`（第 5.7 节）。
  
  写入前重新读取一次记忆版本，与键中的版本不同就放弃写入（说明调用期间发生了删除）。回放从不写入，所以不会刷新 TTL。
- **计数**：`tool_events.go` 的 `metadata["model_calls"]` 只计真实调用，另加 `metadata["model_cache_hits"]`；`usage_complete` 按真实调用数比较。`events.go` 的纯聊天路径同理。命中的事实由 `modelOperation` 告知调用方，传递方式由实施者定。

### 5.3 键

键是下列内容的 SHA-256，前缀标明版本。各字段用长度前缀拼接，不依赖 JSON 的字段顺序：

| 组成 | 来源 | 作用 |
|---|---|---|
| 键格式版本 | 常量 | 改变格式时整体失效 |
| 隔离身份 | `Scope(ctx)`，或长期记忆的命名空间 | 跨租户、跨用户隔离 |
| 会话 ID | `sessionLogID(ctx)` | 同一身份下的会话隔离 |
| 客户端身份 | 客户端实现 `interface{ CacheIdentity() string }` 时用它（可以包含 base URL、部署名、默认参数）；否则用 `client.Provider()` 加解析后的模型名 | 换模型、换端点不命中 |
| 解析后的模型名 | `req.Model` 非空时取它，否则取客户端的 `ModelName()`。两者都取不到，且客户端没有 `CacheIdentity`，就不缓存 | 不同默认模型的客户端不互相命中 |
| 完整 `CompletionRequest` | 原请求经 `json.Marshal`，召回块为真实内容（不经 `recallIntent`）。`llm/temperature.go` 的 `MarshalJSON` 会把显式的 `Temperature=0` 写出，与未设置区分开 | 历史与参数变化不命中 |
| 工具定义 | 名称、描述，以及把 `ArgsSchema()` 解析后再规范化序列化的结果；解析失败就不缓存 | 工具变化不命中，且不受 schema 字符串里键顺序的影响 |
| 记忆版本 | 每个读命名空间的 `(命名空间, 作用域, LastForgetSeq, LastRewindSeq)`，按命名空间排序 | 遗忘、回退、Purge 后不命中 |

关于 `LastRewindSeq`：
- 它是新增的状态标量，放在 `stateScalars`，与 `LastForgetSeq` 并列，进入 head。`Rewound` 提交时把它设为本次提交的 Seq。
- JSON 只增不改：已有数据读出为 0，旧程序忽略这个字段，不升级 `memory-state-blob/3` 与 `memory-head/3`（我方决定）。
- 实施时要核对两件事：
  - head 与 state 的解码对未知字段是否宽松。`codec/json` 默认宽松，只有 `Strict` 拒绝未知字段，要逐个核对调用点。
  - `state_codec.go` 对头和第一块做结构体相等比较，新字段两处都要同步。
- 发现解码严格的话，改为升级版本并补迁移，并且先回报。
- 混合版本部署时，旧程序重写 head 会把它清零，导致回退之后的条目重新可达，但物理删除仍然生效。这一点写进升级说明。

记忆版本在**每次调用前**读取最新状态，不能用轮次开始时的 `TurnMemory.Seq`：同一轮内调用 `memory_forget` 之后，下一步请求也必须不命中。`memory` 导出一个只读方法提供这组值（建议 `Memory.CacheVersion(ctx, read [][]string)`），内部读取 meta 与 head；Builder 不直接解读记忆的内部状态。

### 5.4 标签与物理删除

可选接口放在 `cache` 包：

```go
// Tagged 由支持按标签删除的缓存实现。
type Tagged interface {
	SetTagged(ctx context.Context, key string, value []byte, ttl time.Duration, tags ...string) error
	DeleteTag(ctx context.Context, tag string) error
}
```

- 删除任一标签，就删除带该标签的条目本身，连同它的其他标签。
- `SimpleCache` 实现这个接口。Redis 等外部后端由宿主或 goagent-contrib 实现，例如每个标签一个集合。
- 实现要求，写进接口文档：标签集合的过期时间不短于条目 TTL；`DeleteTag` 是幂等的；标签多时分批删除。

每条缓存条目的标签都是 `hex(HMAC-SHA256(TagKey, 长度前缀拼接的字段))`，不含明文的用户 ID 或会话 ID：

- `session` 加会话 ID：`ModelCache.DeleteSession` 用它删除。
- 每个读命名空间一个：`memory` 加命名空间组件加作用域。

触发物理删除需要同时满足：`Erase` 不是 `EraseOff`，缓存实现了 `Tagged`，记忆配置了 `OnErase`。`mc.Eraser()` 返回的回调对 `(ns, scope)` 计算 `memory` 标签并调用 `DeleteTag`。

| 操作 | 调用 | 失败与重做 |
|---|---|---|
| `Purge` | `cleanupRetiring` 的 `retire` 步骤对旧作用域调用 `OnErase`，与 `RemoveScope`、索引清理并列，且不因它们失败而跳过 | 条目留在 retiring，下一次清理重做；retiring 本身在宽限期内重复执行 |
| `Forget`、`Rewound` | 在提交前经 `withBeforeSave` 登记一条新的队列项 `queueErase`（`Namespace`、`Scope`、`Until = 提交时间 + EraseWithin`），提交后立即调用一次 `OnErase` | `Maintain` 每一轮对仍在 `Until` 之前的 `queueErase` 再调用一次 `OnErase`，过了 `Until` 且最后一次成功时才注销。这样能清掉提交之后、版本复核之前那一小段窗口里写入的条目。`Until` 已过但最后一次仍失败时，保留登记并把错误交给维护的 `onError` |

- 作用域已经变了的登记（命名空间被 Purge 过），按现有规则交给 retiring 处理，直接注销。
- 遗忘与回退会清掉该命名空间当前作用域下的全部缓存条目，粒度较粗。这是有意的取舍：这两种操作很少发生，命中率的损失可以接受。

**没有长期记忆的会话回退**：`Memory.Rewound` 不会被调用，缓存靠键里的完整历史保证不命中。物理残留交给宿主在 `session.Rewind` 或删除会话之后调用 `mc.DeleteSession`，写进使用文档。

### 5.5 命中时的行为

- 缓存值是序列化后的 `[]byte`：前缀标明格式版本，后面是聚合后的 `llm.StreamChunk` 的 JSON。进程内缓存与外部缓存使用同一格式，读出后得到的是独立副本。格式版本不认识或解码失败，按未命中处理，并删除该条目。
- 对外事件与持久回放保持一致（`modelOperation` 现有的回放分支）：先发 `step: model_cached`，`Content` 非空时再发一整段 `text`；不重放多段增量（我方决定）。`ReasoningContent` 与未命中时一样不经 `emit`。
- 不调用模型，不预留预算，不记录用量。可观测事件标记来自缓存，例如在 `observe` 的模型事件中增加 `Cached` 字段，名称由实施者定。
- 命中时本轮 turn 的 `Usage` 为零值（命中不消耗 token）。调用方用 `observe` 模型事件的 `Cached` 与结果元数据 `model_cache_hits` 判断是否命中。
- 结果照常从 `modelOperation` 返回：写持久日志，进入会话日志与工具循环。同一会话里，命中的工具调用 ID 可能与之前某一轮重复（待确认：是否与 `session_log.go` 中按 ID 索引的工具元数据冲突；冲突的话，命中时为工具调用重新生成 ID，并保证回放一致）。

### 5.6 删除 CacheMiddleware，更新文档

删除范围：

- `core/middleware`：`CacheMiddleware`、`CacheEntry`、`cacheShard`、`NewCacheMiddleware*`、`MiddlewareChain` 中对 `cached_response` 的特殊处理，以及相关测试。
- 撤回 `3aedb41a` 新增的 `MiddlewareRequest.SessionID`：它只为 `CacheMiddleware` 服务。分支未推送，直接放弃。
- `builder/long_term_memory.go` 中"与 CacheMiddleware 互斥"的校验与注释。
- `builder/quick_builders.go` 的监控与研究两个快速构建器，以及 `examples/builder/advanced`、`examples/integration/langchain-phase2` 中的用法：改为 `WithModelCache` 或移除。
- 只被它使用的常量（`constants.DefaultMiddlewareCacheShards`、`DefaultMiddlewareCacheCapacity`，以及 `MaxCacheShards` 若没有其他使用方），以及 `scripts/apicheck` 中的相应登记；在 `retired_api.go` 登记已删除的名称。

文档（与代码同一变更）：

- `docs/design/MEMORY_GOVERNANCE.md`：
  - 0.1 节增加"模型调用缓存"一行：`Forget`、`Rewound`、`Purge` 之后逻辑上不可达；物理删除按 `Erase` 模式执行，`EraseRequired` 下在 `EraseWithin` 内完成；失败时以 TTL 为上限。
  - 增加 `Rewound` 一列，或在表下说明它与 `Forget` 相同。
  - 1.8、2.2 第 12 条、5.2、7 节中与 `CacheMiddleware` 相关的条目改写。
- `docs/api/MIDDLEWARE_API.md` 删除缓存中间件一节。
- 新增模型缓存的使用文档，包括：
  - 第 5.7 节的信任边界；
  - Worker 进程如何装配；
  - 删除或回退会话时调用 `DeleteSession`；
  - 混合版本升级说明；
  - 预期命中场景（第 5.8 节）。
- CHANGELOG 的 `[Unreleased]`。

### 5.7 信任边界与数据

- **缓存后端与会话存储同等可信。** 能写入缓存的人可以伪造回答，包括伪造工具调用让 Agent 执行。命中后的工具调用仍要经过与写入前相同的 `validateDurableTurn` 校验（工具必须存在，参数必须合法），但这道校验挡不住"合法但恶意"的调用。外部缓存必须有访问控制，不能与不可信服务共享。
- **缓存值含有明文**：模型回答、工具调用参数（可能含个人信息）、推理文本。保密（加密、网络隔离）由宿主负责，写进使用文档。
- **标签不含明文身份**：标签都是 HMAC 摘要，不会通过 Redis 键名、监控或慢查询日志泄露用户 ID 与会话 ID。缓存键本来就是 SHA-256 摘要。
- **不缓存带 `ProviderState` 的回答**：它是提供方的思考签名或加密推理状态，后续请求要把它原样带回。去掉它再缓存会让下一轮请求失败，原样缓存又把不透明的提供方状态带出了它的生命周期。代价是，开启思考签名的推理模型回答不会被缓存（我方决定，见第 9 节）。

### 5.8 适用场景与预期命中

- 有会话日志时，同一会话的下一轮请求包含上一轮的问答，一定不同于之前的请求。命中主要发生在：
  - 没有会话日志的无状态 Agent：同一身份下重复的相同问题，例如固定提示的分类、抽取与路由；
  - 同一身份下以相同历史重复提交，例如用户重新生成、前端重试；
  - 工具循环中两次完全相同的中间请求。
- 持久运行的崩溃恢复已经由持久日志回放覆盖，不依赖缓存。
- 这一节写进使用文档，避免宿主对有状态对话期待高命中率。

## 6. 物理删除的模式

| 模式 | 行为 | 适用 |
|---|---|---|
| `EraseOff` | 只做逻辑删除，旧条目留到 TTL 到期 | 进程内缓存、TTL 很短，或没有合规要求 |
| `EraseAuto`（默认） | 条件满足（第 5.4 节）就做物理删除，否则退回 TTL，并在构建时记录警告 | 大多数场景 |
| `EraseRequired` | 条件不满足或 `TTL > EraseWithin` 时构建失败 | 有删除个人数据合规要求的生产环境，建议使用 |

物理删除失败不影响逻辑上的正确性：第 5.3 节的键已经让旧条目不可达，失败只会推迟物理清除。在 `EraseRequired` 下，推迟的上限是 TTL，而 TTL 不超过 `EraseWithin`。

## 7. 测试与验收

测试按调用方的方式调用，断言调用方可见的结果，采用表驱动。

| 场景 | 断言 |
|---|---|
| 两个隔离身份、相同的默认会话 ID、相同输入与历史 | 不命中，模型各调用一次 |
| 同一身份、两个会话、相同输入与历史 | 不命中 |
| `Scope` 返回 false 或空身份 | 不查也不写 |
| 同一身份、同一会话、完全相同的请求 | 命中，模型调用次数不增加；会话日志与未命中时逐事件相同 |
| 同一会话、相同输入、历史不同 | 不命中 |
| 换模型、换客户端端点（`CacheIdentity` 不同）、换温度、显式 `Temperature=0` 与未设置、增减工具 | 不命中 |
| 客户端无法解析模型名、工具 schema 解析失败 | 不缓存 |
| 工具 schema 只是键顺序不同 | 命中 |
| 持久运行：命中后崩溃恢复 | 从持久日志回放，结果一致，不查缓存，不写缓存 |
| 带预算的持久运行：命中、崩溃恢复、回放 | 不预留、不记账，回放不报 `usage has no reservation`；预算余额与没有这次调用时相同 |
| 不带预算的运行：命中 | 证据来源为 `cache`；`model_calls` 不计这次命中，`model_cache_hits` 加一；`usage_complete` 不因命中变为 false |
| 同一轮先命中，`memory_forget` 后的下一步 | 不命中 |
| `Forget`、`Rewound`、`Purge` 之后的相同请求 | 不命中；`Tagged` 缓存中对应标签的条目已被物理删除 |
| `Forget` 提交后 `OnErase` 失败，再重试 `Forget` | 重试直接返回；下一轮 `Maintain` 完成删除 |
| 进行中的调用跨过一次 `Forget` 返回 | 写入前复核版本不一致，放弃写入；在 `Until` 之前写入的条目会被下一轮 `Maintain` 删除 |
| `Purge` 时 `OnErase` 失败 | Purge 生效，条目不可达；下次 `cleanupRetiring` 重做删除 |
| 同一命名空间的两个会话，其中一个会话的轮次被回退 | 两个会话中依赖该命名空间的条目都不再命中（粗粒度，符合 5.4 节） |
| `DeleteSession` | 该会话的条目被删除，其他会话不受影响 |
| 第 5.1 节的每条配置规则 | 分别报错或记录警告 |
| 出错、截断、取消、空回答、非法工具调用、带 `ProviderState` 的回答 | 不缓存 |
| 缓存值被改写成未知版本或非法 JSON | 按未命中处理，并删除该条目 |
| 缓存值被改写成调用不存在工具的回答 | 命中校验失败，按未命中处理 |
| 命中时的流式输出 | `model_cached` 加一条完整的 `text` 事件，`Content` 与未命中时一致 |
| 标签 | 不含明文的会话 ID 与命名空间；`TagKey` 不同，得到的标签也不同 |

性能：测量每次模型调用读取记忆版本的开销，以及写入前复核的开销。FileStore 与 pgstore 各测一组，用真实数据，写入评测记录。开销明显的话，由实施者提出优化（例如复用进程内的 state 缓存），但不能改用轮次开始时的旧值。

可观测：命中、未命中、写入失败、写入因版本复核放弃、`OnErase` 失败，各有计数或事件。写入失败只记录，不影响主流程。

验收：
- `go build ./...`、`go vet ./...`、`go test -race ./...`、`make lint`、`scripts/verify_imports.sh` 全部通过；
- goagent-contrib 的 `management` 等使用方编译通过，使用 `goagent-downstream-impact-check` 的方法验证。

## 8. 留给实施者的决定

- Builder 选项、`ModelCache` 字段、`memory` 导出方法、`observe` 字段、`savedTurn.Cached`、`queueErase` 的具体命名。
- 命中的事实如何从 `modelOperation` 传回 `readToolTurn` 的调用方，用于计数。
- 长度前缀拼接的具体编码。
- `quick_builders` 中两个快速构建器：改用 `WithModelCache`（需要宿主提供缓存实例和 `Scope`），还是直接去掉缓存。

## 9. 我方决定（请审阅）

- 命中时只发一整段 `text` 事件，不重放增量。
- 命中不占预算、不计为模型调用，证据来源记为 `cache`。
- `LastRewindSeq` 只增字段，不升级存储格式版本。
- 不缓存带 `ProviderState` 的回答：这会让启用思考签名的推理模型完全不走缓存。
- 标签摘要用宿主提供的 `TagKey` 做 HMAC，而不是不带密钥的哈希：不带密钥的话，用户 ID 可以被字典反推。
- 配置了长期记忆时，`Scope` 默认取记忆的命名空间。

## 10. 审核记录

2026-11-16，架构审核（58 分）与安全审核（62 分）的结论都是"需修改"。审核采纳情况：

- 已采纳：
  - 会话 ID 不能作为隔离依据（改为 `Scope`）；
  - 命中与预算记账冲突（命中分支）；
  - 模型名与客户端身份不完整；
  - 工具 schema 规范化；
  - Forget 与 Rewound 的删除无法重试（`queueErase`）；
  - 删除与写入的竞态（写入前复核加 `Until` 内重复删除）；
  - 非持久路径不校验；
  - 回放刷新 TTL；
  - 缓存值的序列化；
  - TTL 校验；
  - "同一实例"校验的 panic 隐患（删除这条规则，改用回调）；
  - 标签拼接有歧义、标签泄露明文身份（HMAC）；
  - 0.1 节缺少缓存一行；
  - 命中场景说明；
  - 单飞；
  - 可观测。
- 经用户决定：
  - 保留记忆版本（A），理由见第 4 节；
  - memory 改用通用擦除回调；
  - 缓存值不内置加密，在文档中写明信任边界。
- 未采纳："`TemperatureSet` 不进键"。`llm/temperature.go:34` 的 `MarshalJSON` 已经把显式的 `Temperature=0` 写出，与未设置区分开。
