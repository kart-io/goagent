# 模型调用缓存：替换整轮 CacheMiddleware

日期：2026-11-16。状态：设计已与用户确认方案，待实施。

## 1. 意图

`middleware.CacheMiddleware` 包住整轮 Agent 执行（`builder/configurable_agent.go` 的 `chain.Execute`），以 `fmt.Sprint(request.Input)` 为键，命中时跳过整轮处理器。由此产生三个问题：

1. **跨用户命中**：共享实例时，不同用户以相同输入请求拿到彼此的回答。
2. **同会话返回旧答案**：历史已变化，相同输入仍命中。
3. **命中时不写会话日志**：跳过处理器，这一轮不进入会话日志、持久日志与长期记忆抽取。

分支 `fix/cache-middleware-session-scope`（`3aedb41a`，未推送）只修了第 1 项。用户要求从根本上解决：把缓存下沉到单次模型调用，以完整请求为键，并且允许与长期记忆同时使用。

成功的标志：

- 不同会话永远不共享缓存；同一会话历史变化后不命中。
- 命中只省掉一次模型调用，会话日志、持久日志、工具执行、记忆抽取与未命中时完全相同。
- 遗忘、回退或 Purge 之后，被删内容不会经缓存回到模型或用户（逻辑删除），并按配置在缓存存储中物理删除。

## 2. 目标、非目标与反目标

目标：

- Builder 级的模型调用缓存，覆盖 `readJournaledTurn` 与 `readPlainModelTurn` 两个入口。
- 键包含作用域、provider 与解析后的模型、完整消息（含真实召回块）、采样参数、工具定义，以及长期记忆的版本。
- 与长期记忆共存：记忆版本进入键（逻辑失效），遗忘、回退、Purge 时按标签物理删除（可配置）。
- 删除 `CacheMiddleware` 及其相关代码，不保留兼容层。

非目标：

- 语义缓存（按相似度命中）。已删除的 `llm/cache` 证明这类键无法隔离用户；本设计只做精确匹配。
- 缓存工具结果。已由 `tools/middleware.Caching` 负责（按会话与调用者隔离）。
- provider 侧的 prompt caching（Anthropic、OpenAI、Gemini 的前缀缓存），与本设计无关，可以并存。
- 嵌入（embedding）调用的缓存。

反目标（即使实现了也算失败）：

- 任何情况下把一个会话的回答返回给另一个会话。
- 命中时少写或多写任何日志事件，或改变持久运行的恢复语义。
- 遗忘、回退或 Purge 提交之后，仍能取到依据被删内容生成的缓存回答。
- 为通过检查而放宽 Builder 对长期记忆的现有约束（例如请求钩子不得改写召回消息）。

## 3. 约束（来自现有代码）

- **分层**（`scripts/apicheck/main.go` 的 `layers`）：`llm`、`core/middleware` 低于 `tools`、`memory`；`cache` 是根包。缓存逻辑放在 `builder`（最高层），由它读取会话 ID 与记忆状态。不要在 `llm` 层包装 `Client`：包装会让 `llm.ToolStreamClient`、`ReasoningRequestValidator` 等类型断言失效，且 `llm` 层拿不到会话。
- **模型调用的唯一汇合点**是 `builder/model_call.go` 的 `modelOperation`。它负责预算预留、可观测事件、持久日志记录、重试与重发。缓存必须位于它传入的 `invoke` 内部（即"真正调用模型"这一步），这样命中结果仍经过日志记录与校验。
- **持久意图不能当缓存键**：`readJournaledTurn` 计算意图时用 `recallIntent` 把召回块换成占位消息，不同命名空间的召回会得到相同意图。缓存键必须使用真实消息。
- **长期记忆的删除承诺**（以代码为准）：
  - `Forget` 立即清空正文，并在 `Config.EraseWithin`（默认 5 分钟）内从基础快照物理擦除（`memory/memory.go` 的 `Forget`）。`docs/design/MEMORY_GOVERNANCE.md` 0.1 节写的"正文保留到 InvalidRetention"已过时，另行修正，不在本设计范围内。
  - `Purge` 把命名空间切到新作用域，旧作用域进入 retiring，由 `cleanupRetiring` 可重复地物理删除（`memory/scope.go`）。
  - 状态中可用于判断失效的量：作用域 `metaState.Current`（Purge 时更换）、`LastForgetSeq`（每次遗忘抬高，`memory/memory.go` 的 `forget`）。回退（`Rewound`）只写入回退标记，不改变上述两项；`MarkerHorizon` 只在标记按保留期回收时推进（`Prune`），回退当时不变。因此需要新增一个回退序号，见 5.3 节。
- **读命名空间**：`memory.TurnMemory.Read` 的类型是 `[][]string`，但目前 `Memory.Turn` 只填写一个（`memory/recall.go`），代码中没有多读命名空间的入口。本设计按列表处理键与标签，不新增多读功能，测试只覆盖单个命名空间。
- **Worker 可能在独立进程**：物理删除发生在记忆的清理与删除路径中，那里没有 Builder，所以缓存实例与删除模式必须配置在 `memory.Config`。
- 项目规则：破坏性变更不做兼容；规则写成数据（表、结构体字面量）；测试表驱动。

## 4. 方案

考虑过三种失效方式：

| 方式 | 做法 | 取舍 |
|---|---|---|
| A. 记忆版本进入键 | 每次调用前读取各读命名空间的作用域、`LastForgetSeq` 与 `MarkerHorizon` 算进键；删除后键变化，旧条目不可达 | 正确性不依赖任何通知；多进程共享缓存时同样成立。旧条目在 TTL 到期前物理残留 |
| B. 删除时主动清缓存 | 遗忘、回退、Purge 时按命名空间删除缓存条目 | 物理上及时清除；单独使用时正确性依赖删除是否成功 |
| C. A + B | A 保证逻辑删除，B 负责物理删除；B 失败或后端不支持时退回 TTL | 两套机制都要实现，但职责不重叠 |

**用户选择 C。** B 挂在记忆已有的删除路径上，复用它们的重试与幂等，不依赖跨进程通知；B 是否启用由配置决定（第 6 节）。

缓存的层级也考虑过：保留整轮缓存并在命中时补写日志（LangGraph 节点缓存的做法），需要伪造一整轮事件，与持久运行的恢复语义冲突，不采用。调研依据：LangChain、AutoGen、LangChainGo、Vercel AI SDK、Mastra 都只缓存单次模型调用，以完整消息为键；Mastra 默认按用户作用域隔离。

## 5. 设计

### 5.1 配置

Builder 增加一个选项（名称由实施者定，建议 `WithModelCache`），参数是结构体字面量：

```go
builder.WithModelCache(builder.ModelCache{
	Cache: c,          // cache.Cache；做物理删除时须实现 cache.Tagged
	TTL:   10 * time.Minute,
})
```

长期记忆一侧：

```go
memory.Config{
	// ...
	ResponseCache: memory.ResponseCache{
		Cache: c,                    // 与 Builder 同一个实例
		Erase: memory.EraseAuto,     // EraseOff / EraseAuto（默认）/ EraseRequired
	},
}
```

`Build` 的校验（写成规则表，沿用 `validateLongTermMemory` 的方式）：

| 条件 | 结果 |
|---|---|
| 配置了模型缓存且配置了长期记忆，但 `memory.Config.ResponseCache.Cache` 为空或不是同一实例 | 配置错误：清理时会删错地方或漏删 |
| `Erase` 为 `EraseRequired` 且缓存未实现 `cache.Tagged` | 配置错误 |
| `Erase` 为 `EraseAuto` 且缓存未实现 `cache.Tagged` | 构建成功，记录一条说明：物理删除退回 TTL |
| `Erase` 不是 `EraseRequired`、物理删除可能不可用，且 `TTL > memory.Config.EraseWithin` | 构建成功，记录一条警告：遗忘后被删内容可能在缓存中残留超过 `EraseWithin` |

`memory.New` 同样校验 `EraseRequired` 与 `cache.Tagged`，因为独立 Worker 进程不经过 Builder。

### 5.2 何时缓存与放在哪里

- 只在本次执行有会话时缓存：会话 ID 与 `ConfigurableAgent.sessionLogID` 一致（`tools.WithSessionID` 优先，其次 Builder 的 `WithSessionID`）。`readJournaledTurn`、`readPlainModelTurn` 是自由函数，拿不到 Agent，所以由 Agent 在调用前把会话 ID 与缓存配置经 ctx（builder 包内私有键）或参数传入，方式由实施者定。没有会话的调用不查也不写缓存。
- **查找**放在 `modelOperation` 中：持久回放之后（已记录的结果照常从日志回放，不查缓存），预算预留（`validateModelBudget`、按尝试预留）与模型调用计数之前。命中不占预算、不计为一次模型调用（本设计的我方决定：缓存的目的就是省调用）。
- **写入**放在结果通过校验之后：持久路径是 `durable.Do` 带 `validate` 成功返回之后，非持久路径是 `readToolTurn` 成功返回之后。只缓存 `FinishReason` 为 `stop` 或 `tool_calls` 的回答；出错、截断、取消的不缓存。
- 命中的结果仍要作为本次操作的结果写入持久日志（崩溃恢复时从日志回放，不再查缓存）。日志中的证据来源记为缓存（现有 `EvidenceSource` 统一写 `"provider"`，新增一个值，如 `"cache"`），用量记 0，使用量审计反映真实发生的调用（我方决定）。

### 5.3 键

键是下列内容规范化 JSON 的 SHA-256，前缀标明版本：

| 组成 | 来源 | 作用 |
|---|---|---|
| 键格式版本 | 常量 | 改变格式时整体失效 |
| 会话 ID | `sessionLogID(ctx)` | 跨会话隔离 |
| provider 与解析后的模型名 | `client.Provider()`、与 `withModelMetadata` 相同的模型解析 | 换模型不命中 |
| 完整 `CompletionRequest` | 原请求，召回块为真实内容（不经 `recallIntent`） | 历史与参数变化不命中 |
| 工具定义 | 名称、描述、参数 schema，与持久意图相同 | 工具变化不命中 |
| 记忆版本 | 每个读命名空间的 `(命名空间, 作用域, LastForgetSeq, LastRewindSeq)`，按命名空间排序 | 遗忘、回退、Purge 后不命中 |

`LastRewindSeq` 是新增的状态标量（放在 `stateScalars`，与 `LastForgetSeq` 并列，进入 head），`Rewound` 提交时设为本次提交的 Seq。JSON 中只增不改：已有数据读出为 0，旧程序忽略该字段，不升级 `memory-state-blob/3`、`memory-head/3`（我方决定；若实施时发现 head 解码对未知字段严格，需改为升级版本并补迁移，须先回报）。它在 head 中，读取不需要解码整份 state。

记忆版本必须在**每次调用前**读取最新状态，不能用轮次开始时的 `TurnMemory.Seq`：同一轮内调用 `memory_forget` 后的下一步请求也必须不命中。`memory` 包导出一个只读方法提供这组值（建议 `Memory.CacheVersion(ctx, read [][]string)`），内部读取 meta 与 head；Builder 不直接解读记忆内部状态。

### 5.4 标签与物理删除

可选接口放在 `cache` 包：

```go
// Tagged 由支持按标签删除的缓存实现。
type Tagged interface {
	SetTagged(ctx context.Context, key string, value any, ttl time.Duration, tags ...string) error
	DeleteTag(ctx context.Context, tag string) error
}
```

删除任一标签即删除带该标签的条目本身（连同它的其他标签）。`SimpleCache` 实现它。Redis 等外部后端由宿主或 goagent-contrib 实现（例如每个标签一个集合）。

每条缓存条目的标签：

- `session:<会话ID>`：宿主删除会话时可以调用 `DeleteTag` 一并清除。
- 每个读命名空间一个 `memory:<命名空间>:<作用域>`。

物理删除的触发点，都在记忆的现有删除路径上（`Erase` 不为 `EraseOff` 且缓存实现了 `Tagged`）：

| 操作 | 删除的标签 | 时机 |
|---|---|---|
| `Purge` | 旧作用域的标签 | `cleanupRetiring` 的 `retire` 步骤内，与 `RemoveScope`、索引清理并列；失败时条目留在 retiring，下次清理重做 |
| `Forget` | 当前作用域的标签 | 遗忘提交之后；失败时返回错误或记入后续擦除步骤，由实施者按 `EraseWithin` 的现有重试机制挂接 |
| `Rewound` | 当前作用域的标签 | 回退提交之后，同上 |

遗忘与回退会清掉该命名空间的全部缓存条目，粒度较粗。这是有意的取舍：这两种操作很少发生，命中率的损失可以接受。

### 5.5 命中时的行为

- 缓存保存聚合后的 `*llm.StreamChunk`（经 `snapshot.Clone` 隔离，与现有缓存做法一致）。
- 注意 `readToolTurn` 只累加 `Delta` 并用累加结果覆盖 `Content`（`builder/tool_events.go`）。命中时构造一个 `Delta` 等于完整 `Content`、其余字段（工具调用、FinishReason、ProviderState 等）取自缓存的 chunk，放进只含它的 channel 交给 `readToolTurn`。
- "与未命中相同"指会话日志、持久日志与返回结果相同；对外发出的流式文本事件在命中时是一整段，而不是多段增量（我方决定，不为此缓存每段增量）。
- 不调用模型，不记录 token 用量（按 0 计）。可观测事件标记来自缓存，例如在 `observe` 的模型事件中增加 `Cached` 字段，名称由实施者定。
- 结果照常返回 `modelOperation`：写持久日志、通过校验、进入会话日志与工具循环。

### 5.6 删除 CacheMiddleware

删除范围：

- `core/middleware`：`CacheMiddleware`、`CacheEntry`、`cacheShard`、`NewCacheMiddleware*`、`MiddlewareChain` 中对 `cached_response` 的特殊处理，以及相关测试。
- 撤回 `3aedb41a` 新增的 `MiddlewareRequest.SessionID`：它只为 `CacheMiddleware` 服务。分支未推送，直接改写。
- `builder/long_term_memory.go` 中"与 CacheMiddleware 互斥"的校验与注释。
- `builder/quick_builders.go` 的监控与研究两个快速构建器，以及 `examples/builder/advanced`、`examples/integration/langchain-phase2` 中的用法：改为 `WithModelCache` 或移除。
- 只被它使用的常量（`constants.DefaultMiddlewareCacheShards`、`DefaultMiddlewareCacheCapacity`，以及 `MaxCacheShards` 若无其他使用方），以及 `scripts/apicheck` 中的相应登记；在 `retired_api.go` 登记已删除的名称。
- 文档：`docs/api/MIDDLEWARE_API.md`、`docs/design/MEMORY_GOVERNANCE.md`（1.8、2.2 第 12 条、5.2、7 节中与 `CacheMiddleware` 相关的条目）、CHANGELOG 的 `[Unreleased]`（替换 `3aedb41a` 写入的条目）。

## 6. 物理删除的模式

| 模式 | 行为 | 适用 |
|---|---|---|
| `EraseOff` | 只做逻辑删除，旧条目留到 TTL 到期 | 进程内缓存、TTL 很短，或没有合规要求 |
| `EraseAuto`（默认） | 缓存实现 `cache.Tagged` 就做物理删除，否则退回 TTL 并在构建时说明 | 大多数场景 |
| `EraseRequired` | 缓存未实现 `cache.Tagged` 时构建与 `memory.New` 失败 | 有删除个人数据合规要求的生产环境 |

物理删除失败不会影响正确性（第 5.3 节的键已经让旧条目不可达），只会推迟物理清除。

## 7. 测试与验收

测试按调用方的方式调用，断言调用方可见的结果，采用表驱动。

| 场景 | 断言 |
|---|---|
| 两个会话、相同输入与历史 | 模型各调用一次；各自得到自己的回答 |
| 同一会话、相同输入、历史不同 | 不命中 |
| 同一会话、完全相同的请求 | 命中，模型调用次数不增加；会话日志与未命中时逐事件相同 |
| 没有会话 | 不查也不写缓存 |
| 换模型、换温度、增减工具 | 不命中 |
| 持久运行：命中后崩溃恢复 | 从持久日志回放，结果一致，不再查缓存 |
| 同一轮先命中，`memory_forget` 后的下一步 | 不命中 |
| `Forget`、`Rewound`、`Purge` 之后的相同请求 | 不命中；`Tagged` 缓存中对应标签的条目已被物理删除 |
| `Purge` 时 `DeleteTag` 失败 | Purge 生效，条目不可达；下次 `cleanupRetiring` 重做删除 |
| 同一命名空间的两个会话，其中一个会话的轮次被回退 | 两个会话中依赖该命名空间的条目都不再命中（粗粒度，符合 5.4 节） |
| 第 5.1 节的四条配置规则 | 分别报错或记录说明 |
| 出错、截断、取消的回答 | 不缓存 |
| 命中时的预算与可观测 | 不预留预算、不计模型调用次数；持久日志证据来源为缓存、用量为 0；可观测事件标记来自缓存 |
| 命中时的流式输出 | 一条完整文本事件，`Content` 与未命中时一致 |

性能：测量每次模型调用读取记忆版本的开销（FileStore 与 pgstore 各一组，用真实数据），写入评测记录。读取开销若明显，由实施者提出优化（例如复用进程内的 state 缓存），但不能改用轮次开始时的旧值。

验收：`go build ./...`、`go vet ./...`、`go test -race ./...`、`make lint`、`scripts/verify_imports.sh` 全部通过；goagent-contrib 的 `management` 等使用方编译通过（使用 `goagent-downstream-impact-check` 的方法）。

## 8. 留给实施者的决定

- Builder 选项、`memory` 导出方法、`observe` 字段的具体命名。
- 规范化 JSON 的写法：复用持久意图的序列化方式，或另写；须保证字段顺序稳定。
- `Forget` 与 `Rewound` 物理删除失败时的处理：随返回错误报出，还是挂到 `EraseWithin` 的后续擦除步骤。须满足"失败可重试，且最终在 `EraseWithin` 内完成或明确报错"。
- `quick_builders` 中两个快速构建器是改用 `WithModelCache`（需要宿主提供缓存实例）还是直接去掉缓存。
