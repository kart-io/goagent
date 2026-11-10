# memory 长期记忆

跨会话的长期记忆：记录库保存正文，向量索引是可重建的投影，抽取与整理由 Worker 异步完成。会话内的历史与压缩由会话日志（`core/session`）与 `contextpolicy.Compaction` 负责，不在本包。设计见 [记忆系统设计](../docs/design/MEMORY_DESIGN.md)。

## 记录

| 类别 | 含义 | 来源 |
|---|---|---|
| `fact` | 稳定的事实、偏好与决定 | 抽取、`memory_save` |
| `episode` | 某一轮做了什么、结果如何 | 抽取 |
| `reflection` | 从多次经历提炼的结论；失败轮次的教训 | 反思、中止的轮次 |
| `skill` | 验证有效、可复用的做法 | 抽取、`memory_save` |

`Record` 带有命名空间、关键词、实体、重要度（0 到 1）、来源（会话与事件序号区间）与有效期。被新事实否定、被遗忘或来源轮次被退回的记录写入 `InvalidAt`（被否定时 `SupersededBy` 指向新记录），不再召回，保留期后删除。`Pinned` 的记录是核心记忆，每轮都放入召回块。

## 命名空间

所有读写都在 `memory.WithNamespace(ctx, ...)` 绑定的命名空间内进行，没有绑定时返回错误。命名空间须由宿主依据可信身份（如租户与用户）给出，不能取自模型或用户输入。

## 使用

```go
mem, err := memory.New(memory.Config{
    Store:    storemem.New(),          // 任意 store.Store，正文以 JSON 文本存于 {"memory","records",命名空间...}
    Index:    memory.NewMemoryIndex(), // 可选，与 Embedder 同时提供
    Embedder: embedClient,             // llm.EmbeddingClient
    EmbedModel: "text-embedding-3-small",
    Model:    extractClient,           // 抽取、整理、教训与反思
    Sessions: sessions,                // 与 WithSessionLog 相同的会话日志
})
go mem.Run(ctx, func(err error) { log.Println(err) })

agent, err := builder.NewSimpleBuilder(client).
    WithSessionLog(sessions).
    WithLongTermMemory(mem).
    WithTools(mem.Tools()...).
    Build()
```

| 方法 | 说明 |
|---|---|
| `Remember(ctx, Record)` | 写入或覆盖一条记录；先写索引再写正文 |
| `Recall(ctx, Query)` | 混合召回：向量相似度与字符对重合取较大者为相关性，与新近性（半衰期 `HalfLife`）、重要性按 `Weights` 加权；更新访问时间 |
| `RecallBlock(ctx, query)` | 核心记忆在前、召回结果在后的注入文本，不超过 `RecallRunes` |
| `Forget(ctx, id)` / `Purge(ctx)` | 使一条记录失效 / 删除整个命名空间 |
| `Rewound(ctx, session, to)` | 宿主在 `session.Rewind` 后调用，使来自被退回轮次的记录失效 |
| `Reindex(ctx, force)` | 为嵌入模型不同或缺少索引的记录重建向量 |
| `Prune(ctx)` | 按 `Retention` 删除过期、超出容量与超过保留期的失效记录；Worker 每个任务后执行 |
| `Enqueue` / `Run` / `ProcessPending` | 抽取任务；Builder 在每轮结束登记 |
| `Tools()` | `memory_search`、`memory_save`、`memory_forget` |

## 抽取流程

1. 轮次结束（含中止）后 Builder 登记任务，Key 为"会话/turn/start 序号"，重复登记不产生新任务。
2. Worker 从会话日志读该轮的消息（跳过注入的召回块）。轮次已被退回时跳过；中止的轮次只写一条教训。
3. 抽取模型输出候选；每个候选与同类最相似的 `ReconcileCandidates` 条旧记录比较，判定新增、合并更新、否定旧记录或忽略。
4. 未参与反思的情景累计重要度达到 `ReflectionThreshold` 时提炼结论。
5. 记录 ID 由任务 Key 与序号决定，崩溃后重做同一任务覆盖而不是重复；失败按 `JobAttempts` 重试后标记 failed。

同一任务存储只应运行一个 Worker。召回会读取命名空间内的全部记录，命名空间较大时用 `Retention.MaxRecords` 限制规模。

## 代码结构

```text
memory/
├── record.go   # Record、Kind、命名空间
├── store.go    # RecordStore 与抽取任务的存储
├── index.go    # Index 接口与进程内 MemoryIndex
├── memory.go   # Memory：写入、召回、遗忘、保留
├── worker.go   # 抽取、整理、教训与反思
└── tools.go    # memory_search / memory_save / memory_forget
```
