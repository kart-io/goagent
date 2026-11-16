# memory 长期记忆

跨会话的长期记忆：记录库保存正文，向量索引是可重建的投影，抽取与整理由 Worker 异步完成。会话内的历史与压缩由会话日志（`core/session`）与 `contextpolicy.Compaction` 负责，不在本包。设计见 [记忆系统设计](../docs/design/MEMORY_DESIGN.md)。

## 记录

| 类别 | 含义 | 来源 |
|---|---|---|
| `fact` | 稳定的事实、偏好与决定 | 抽取、`memory_save` |
| `episode` | 某一轮做了什么、结果如何 | 抽取 |
| `reflection` | 从多次经历提炼的结论；失败轮次的教训 | 反思、中止的轮次 |
| `skill` | 验证有效、可复用的做法 | 抽取、`memory_save` |

`Record` 带有命名空间、关键词、实体、重要度（0 到 1）、来源与有效期。`Sources` 是内容依据的全部轮次（会话与事件序号区间），合并更新时继承旧记录的来源。被新事实否定或合并、被遗忘或来源轮次被退回的记录写入 `InvalidAt`（被否定或合并时 `SupersededBy` 指向新记录），不再召回，保留期后删除。`Pinned` 的记录是核心记忆，每轮都放入召回块。

## 命名空间

所有读写都在 `memory.WithNamespace(ctx, ...)` 绑定的命名空间内进行，没有绑定时返回 `errors.CodeInvalidInput`。命名空间须由宿主依据可信身份（如租户与用户）给出，不能取自模型或用户输入。

## 使用

```go
fs, err := durable.NewFileStore(dir) // 须实现 durable.Scoped；PostgreSQL 见 goagent-contrib/durable/pgstore
mem, err := memory.New(memory.Config{
    Durable:  fs,                      // 记录、图谱与抽取任务都存于其中
    Index:    memory.NewMemoryIndex(), // 可选，与 Embedder 同时提供
    Embedder: embedClient,             // llm.EmbeddingClient
    EmbedModel: "text-embedding-3-small",
    Model:    extractClient,           // 抽取、整理、教训与反思
    Sessions: sessions,                // 与 WithSessionLog 相同的会话日志
})
go mem.Run(ctx, func(err error) { log.Println(err) }) // 不运行 Worker 时改用 go mem.RunMaintenance(ctx, time.Minute, onError)

agent, err := builder.NewSimpleBuilder(client).
    WithSessionLog(sessions).
    WithLongTermMemory(mem).
    WithTools(mem.Tools()...).
    Build()
```

| 方法 | 说明 |
|---|---|
| `Remember(ctx, Record)` | 写入或覆盖一条记录；先写索引再写正文 |
| `Recall(ctx, Query)` | 混合召回：向量相似度与字符对重合取较大者为相关性，与新近性（半衰期 `HalfLife`）、重要性按 `Weights` 加权；查询含字符对时不返回相关性为 0 的记录；更新访问时间 |
| `RecallBlock(ctx, query)` / `Retrieve(ctx, query)` | 核心记忆在前、召回结果在后的注入文本，不超过 `RecallRunes`；`Retrieve` 另返回写入块中的记录、关系与社区，`Sessions()` 给出它们依据的会话 |
| `Snapshot(ctx, query)` / `Hydrate(ctx, s, refs)` | 召回分两步：`Snapshot` 只返回引用（可写入持久日志），`Hydrate` 按引用还原召回块，丢弃已删除、失效或不在读绑定内的项；宿主绑定的命名空间与快照不同时返回 `errors.ErrMemoryNamespaceChanged` |
| `Turn(ctx)` / `WithTurn` / `CheckTurn` | 轮次的记忆绑定 `TurnMemory`：轮次内的读写与任务都使用开始时的写命名空间 |
| `Forget(ctx, id)` / `Purge(ctx)` | 使一条记录失效 / 删除整个命名空间。记录向量在 `Index` 的 `append(ns, "#"+scope)` 下（实体在其下的 `#entity`），键形如 `ID#实例号.ContentRev#模型摘要`，`scope` 可从 `CacheVersion` 取得，据此核对遗忘后的清理 |
| `Rewound(ctx, session, to)` | 宿主在 `session.Rewind` 后调用：有消息被遮蔽的轮次整轮失效，来自这些轮次的关系派生与实体名称删除，只被它们取代的旧记录与旧关系恢复；回退点切开一轮时为可见部分补登抽取任务；回退时仍在进行的轮次之后登记会被截断或丢弃 |
| `Reindex(ctx, force)` | 为缺少当前版本向量的记录与实体补写向量（`force` 时全部重写），删除过期的键；换嵌入模型或升级后运行，不写记录 |
| `Prune(ctx)` | 按 `Retention` 删除过期、超出容量与超过保留期的失效记录，以及保留期外的已完成任务与回退标记；Worker 处理完一批任务后执行 |
| `Enqueue` / `Job` / `DroppedJobs` | 登记抽取任务（Builder 在每轮结束登记）/ 查询任务状态，已完成的任务带统计 `Stats`（写入的记录与关系、丢弃的关系、复核不成立而降级的整理决定 `Conflicts`、模型给出不存在的 target 而降级为新增的整理决定 `InvalidTargets`）/ 因作用域切换、遗忘、回退或 state 超限而丢弃的任务数 |
| `Run` / `ProcessPending` | Worker：每轮先执行 `Maintain`，再处理各命名空间的待处理任务；多个实例同时运行时由锁保证同一时刻只有一个在处理 |
| `Maintain(ctx)` / `RunMaintenance(ctx, interval, onError)` | 不需要模型的维护：完成 Purge 遗留的旧作用域清理、Forget 崩溃后残留的向量清理，每 `ReindexEvery` 轮补写向量并清理过期的删除标记。**不运行 Worker 的部署必须运行 `RunMaintenance`** |
| `Tools()` | `memory_search`、`memory_save`、`memory_forget` |
| `MigrateFromStore(ctx, src, namespaces, opts)` | 从 G0 的 `store.Store` 迁入记录，见下文"从 G0 迁移" |

## 时序知识图谱

`Config.Graph` 为真时，抽取同时输出实体与关系。每条关系以 `from` 指向同一次抽取中的一条记录，缺少合法 `from` 的关系丢弃。关系与实体名称记下依据的记录：记录被遗忘、改写正文或回退时，只由它支撑的关系失效、名称删除，由它合并或反思出的记录随之失效，相关社区删除后重写。

- 实体：先按类型与规范化名称（含别名）精确匹配；匹配不到的取名称向量或字符对相似的同类型实体为候选，由模型一次判定是否为同一对象，判为重复时名称并入别名。翻译名需要配置 `Embedder` 才能找到。
- 关系：带有效期，生效时间是对话发生的时间。`exclusive` 的谓词（如"住在"）出现新客体时旧边失效并指向新边，迟到的旧事实写入即失效；`Edge.ValidAt(t)` 查询某一时刻成立的关系。
- 召回：以查询中出现的实体与召回记录的实体为种子，取 `GraphHops` 跳内的有效边，按 Personalized PageRank 取至多 `GraphRelations` 条，放入召回块并由 `memory_search` 返回；也可直接调用 `Related`。
- 社区：`Config.Communities` 为真时，Worker 处理完一批任务后用标签传播划分社区，为成员或关系变化的社区请求模型写摘要（`RefreshCommunities` 可手动执行）；召回时附上与种子最相关的 `GraphCommunities` 个摘要。

## 分布式部署

多个进程共享同一份长期记忆时，缓存与向量索引需要换成外部后端：

| 组件 | 进程内默认 | 共享后端（`goagent-contrib`） |
|---|---|---|
| 模型调用缓存 | `cache.SimpleCache` | `github.com/kart-io/goagent-contrib/cache/redis`（Redis 7 及以上，单机、哨兵与 Cluster） |
| 向量索引 | `memory.MemoryIndex` | `github.com/kart-io/goagent-contrib/memory/pgvector` |

换法：Redis 缓存实现 `cache.Bytes` 与 `cache.Tagged`，作为 `builder.ModelCache.Cache` 传入；pgvector 实现 `memory.Index`，作为 `memory.Config.Index` 传入。自行实现 `memory.Index` 时用 `memorytest.RunIndex` 验证契约。

部署须知：

- **所有实例使用相同的 `TagKey`**：标签是 HMAC，`TagKey` 不同则同一记忆在各实例上的标签不同，`DeleteTag` 删不到其他实例写的条目。
- **每个持有记忆的进程都要接上擦除**：包括只跑 `memory/worker` 的进程，它执行的 `Forget`、`Purge` 也要通过 `OnErase` 调到共享缓存。
- **切换缓存**：混合部署期间（部分实例仍用 `SimpleCache`）删除不会到达本地缓存，跨实例删除承诺不成立。建议一次性切换，或切换期间关闭 ModelCache。从 Redis 回退到本地无需迁移数据，Redis 中的条目按 TTL 过期。
- **切换向量索引**：切到 pgvector 后表为空，召回暂时只有非向量分；宿主对现有命名空间逐个调用 `Reindex(ctx, false)` 回填。回退到本地索引同样靠 `Reindex`。
- **Redis 淘汰策略**：令牌设计使“删除后读不到”不受淘汰影响；“返回时已物理删除”要求 `maxmemory-policy` 为 `noeviction`（或用量不触及 `maxmemory`），否则被淘汰的成员集合会让部分条目只能等 TTL 回收。
- **换嵌入模型**：`Index` 允许同一命名空间共存不同维度的向量，`Search` 只考虑与查询同维度的向量。维度变化时，旧向量因维度不同被 `Search` 跳过、不占名额，`Reindex(ctx, false)` 写入新键并删除旧键；同维度换模型时，旧向量仍会进入 `Search` 结果，靠 `adopted` 按模型过滤，占用前 k 名额、召回条数可能少于 k，须 `Reindex` 清理。

## 评测

`memory/memeval` 与 `scripts/memeval` 在 LongMemEval、LOCOMO 上对比只看最近原文的基线与长期记忆、图谱、社区各配置，见设计文档第 6 节。

## 抽取流程

1. 轮次结束（含中止）后 Builder 登记任务，Key 为 `会话 ID/轮次起始序号`（如 `s1/1`；回退切开一轮时补登的任务为 `会话 ID/起始/截止`），重复登记不产生新任务。
2. Worker 从会话日志读该轮的消息（跳过召回块，以及 `memory_search`、`memory_save`、`memory_forget` 的调用与结果）。轮次已被退回时跳过；中止的轮次只写一条教训。
3. 抽取模型输出候选；每个候选与同类最相似的 `ReconcileCandidates` 条旧记录比较，判定新增、合并更新、否定旧记录或忽略。相关性大于 0 即入选，没有更高的下限；没有入选记录时直接新增，不请求模型。
4. 未参与反思的情景累计重要度达到 `ReflectionThreshold` 时提炼结论。
5. 记录 ID 由任务 Key 与序号决定，崩溃后重做同一任务覆盖而不是重复；失败按 `JobAttempts` 重试后标记 failed。回复不是合法 JSON 时，同一次尝试内把原回复交回模型修正一次，所以每个请求在一次尝试内至多发两次；因截断（`finish_reason` 不是 `stop`）的回复不修正。
6. 任务存于命名空间的 state，与登记它的作用域绑定：Purge 之后旧作用域的任务随之消失，处理中的任务写回时发现作用域已切换即丢弃；登记之后该命名空间发生过 Forget 的任务也丢弃，不让遗忘的内容被重新抽取。丢弃计入 `DroppedJobs`。

多个 Worker 可以同时运行，同一时刻只有持锁的一个在处理。召回会读取命名空间内的全部记录，命名空间较大时用 `Retention.MaxRecords` 限制规模。

## Worker 回复格式

`Model` 的每次回复取第一个 `{` 到最后一个 `}` 之间的内容解析为 JSON，可以带代码块或前后说明。各类请求期望的字段（以 `worker.go`、`graph.go`、`community.go` 中的提示词与结构体为准）：

| 请求 | 回复 |
|---|---|
| 抽取 | `{"memories":[{"kind":"fact\|episode\|skill","content":"…","keywords":[],"entities":[],"importance":0.0}]}`；`Graph` 为真时同一对象另含 `"relations":[{"from":0,"subject":{"name":"…","type":"…","aliases":[]},"predicate":"…","object":{…},"fact":"…","exclusive":true}]`，`from` 是 `memories` 的下标 |
| 整理 | `{"op":"add\|update\|invalidate\|noop","target":"已有记忆的 id","content":"update 时合并后的完整内容"}` |
| 教训（中止的轮次） | `{"lesson":"…","keywords":[],"importance":0.0}`，`lesson` 为空时不写 |
| 反思 | `{"insights":[{"content":"…","keywords":[],"importance":0.0}]}` |
| 实体判重 | `{"resolutions":[{"index":0,"duplicate_of":"候选实体的 id，不是同一对象时为空串"}]}` |
| 社区摘要 | `{"name":"…","summary":"…"}` |

## 从 G0 迁移

G0 把记录与图谱以 JSON 文本存在 `store.Store` 中。升级步骤：

1. 停止 Agent 流量与 Worker，让持久运行结束；备份旧存储与旧索引。
2. 配置 `Durable`；需要数据回滚能力时设置 `RollbackWindow`。
3. 迁移：

   ```go
   report, err := mem.MigrateFromStore(ctx, memory.MigrationSource{Store: oldStore, Index: oldIndex},
       [][]string{{tenantID, userID}}, memory.MigrateOptions{ClearLegacy: true})
   ```

   每个命名空间两阶段提交，失败或冲突（`report.Namespaces[i].Status`）处理后重跑即可，已迁移的不再导入，Purge 之后重跑也不会导入。只迁入记录，没有取代者的失效记录视为已遗忘并清空正文；关系、实体与社区由之后的抽取重建。旧任务列在 `report.Jobs`，不迁入；`report.Unlisted` 是旧任务中出现、但不在清单里的命名空间。`ClearLegacy` 删除这个命名空间的旧数据、它的旧任务与旧向量。
4. 恢复流量，启动 Worker（或 `RunMaintenance`）。

回滚数据时从迁移前的备份恢复，再按 `Deletions` 重放回滚窗口内的遗忘与 Purge；没有开启删除日志时，迁移后遗忘的内容会重新出现。

## 代码结构

```text
memory/
├── record.go   # Record、Kind、命名空间
├── job.go      # 抽取任务、待处理队列、Maintain
├── migrate.go  # 从 G0 的 store.Store 迁移
├── durable_state.go # 命名空间 state 的读写（mutate/read）
├── index.go    # Index 接口与进程内 MemoryIndex
├── memory.go   # Memory：写入、召回、遗忘、保留
├── worker.go   # 抽取、整理、教训与反思
├── graph.go    # 实体判重与带有效期的关系
├── rank.go     # 多跳召回与 Personalized PageRank
├── community.go # 标签传播划分社区与社区摘要
├── tools.go    # memory_search / memory_save / memory_forget
└── memeval/    # LongMemEval、LOCOMO 评测
```
