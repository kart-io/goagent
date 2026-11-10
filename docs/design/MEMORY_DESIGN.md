# 记忆系统设计

## 0. 结论

记忆按功能分为四类，各有唯一的载体：

| 功能 | 时间尺度 | 载体 | 参考 |
|---|---|---|---|
| 工作记忆：本次请求发给模型的内容 | 短期 | 每步由 PreStep 组装的消息 | MemGPT 主内存、CoALA Working |
| 情景记忆：本会话发生过什么 | 中期 | 会话事件日志 + 递归摘要 | Generative Agents 记忆流、MemoryBank 分层摘要 |
| 语义记忆：跨会话成立的事实、偏好、实体关系 | 长期 | 记录库 + 索引 + 时序图 | Mem0、Zep、A-MEM |
| 程序记忆：教训与可复用做法 | 长期 | 记录库中的 `reflection`、`skill` | Reflexion、Voyager |

已定的三项决策：

1. 对话压缩下沉到 goagent 核心（`contextpolicy.Compaction`），goagent-harness 只保留编码场景的扩展（计划保留、空转提示、待办）。
2. 长期记忆的抽取异步执行：轮末只登记任务，由宿主运行的 Worker 处理，任务按"会话 + 轮次"幂等。
3. 做时序知识图谱（第三阶段），实体与关系边带有效期，矛盾时使旧边失效而不删除。

只采用应用层方案。改造模型结构的方案（Memorizing Transformers、MemoryLLM、WISE、Titans、Memory³、MemLong）与"不绑定模型"冲突，不采用；MemOS 的激活记忆只借用一点：稳定内容（system、核心记忆）放在请求前部，利于服务商的提示缓存。

## 1. 现状与要删除的部分

| 现有 | 问题 | 处理 |
|---|---|---|
| `interfaces.MemoryManager` 对话部分 + `builder.WithMemory` | 与会话日志并存双写；用 `config.SessionID` 而不用请求的会话 ID，会串会话；Rewind 不回滚 | 删除，历史只来自会话日志 |
| `MemoryManager` 的 Case、KV 部分 | 案例检索对任意查询全量命中；无命名空间 | 删除，由第二阶段的记录库替代 |
| `memory.HierarchicalMemory`、`ShortTermMemory`、`LongTermMemory`、`MemoryConsolidator` | 长期正文只在内存；TTL 无效；同 key 多版本；空删除 panic；合并器未被调用 | 删除 |
| `memory.MemoryType` 中的 episodic/semantic/procedural | 只是标签，写入同一层 | 删除，由 `Record.Kind` 替代 |
| `interfaces.VectorStore` 与其实现 | 可用 | 保留，适配为 `memory.Index` |
| goagent-harness `preset/compaction.go` | 只用 goagent 时长会话无长度控制 | 通用部分移入 `contextpolicy.Compaction` |

## 2. 第一阶段：工作记忆与中期记忆

### 2.1 历史只来自会话日志

- 删除 `WithMemory`、`readMemory`/`writeMemory`、`AgentConfig.MaxConversationHistory`、`MemoryFailurePolicy`。
- 多轮历史一律由 `WithSessionLog(store)` 提供；会话 ID 由 `tools.WithSessionID(ctx)` 给出，缺省用 `WithSessionID`。进程内使用 `session.NewMemoryStore()`。
- 历史长度由 2.2 的 Compaction 控制，不再按轮数截断。

### 2.2 `contextpolicy.Compaction`

PreStep 组件，接口与 `Offload` 一致（`WithCompaction(c)` 注册 PreStep 与请求失败钩子）。

```go
type CompactionConfig struct {
    MaxTokens    int    // 触发阈值，默认 100000
    KeepTokens   int    // 原样保留的最近消息，默认 MaxTokens/4
    Instructions string // 对摘要的补充要求
    Model        llm.Client // 为 nil 时不调用模型，省略旧消息并写明省略了什么
    Estimate     func(llm.Message) int // 默认按 UTF-8 字节数/3
    Keep         func(span []llm.Message, limit int) []llm.Message // 被压缩区间中原样保留的消息，默认保留第一条与最近的用户原话
    Annotate     func(span []llm.Message) string // 摘要附注，harness 用它附加空转提示与待办；不交给摘要模型
}
```

行为（自 harness 迁移，语义不变）：

- 估算长度超过 `MaxTokens` 时，把 system 之后、保留区之前的消息换成一条以 `[此前对话的摘要]` 开头的用户消息。保留区从非 tool 消息开始，不拆开工具调用与结果；本步新输入总在保留区内。
- 摘要提示把旧摘要与新消息一起交给模型（递归摘要），旧摘要不截断；摘要长度要求不超过 `KeepTokens/2`。
- 摘要失败时退化为省略说明，旧摘要原样接在后面；取消与预算耗尽时返回错误。摘要调用计入根预算。
- 请求因超出上下文失败时只保留最近一次交互强制压缩一次后重试（非持久运行）。
- `Identity()` 由配置计算，作为 PreStep 身份进入持久日志。
- 改写由 `Recorder.Sync` 记为带 `Surface` 的 `history/rewrite`：原文仍在日志中，Rewind 撤销摘要，回放不再调用模型。

### 2.3 `recall_history` 工具

模型取回被摘要掉的原文（MemGPT 的换入）。由 `contextpolicy.NewRecallTool(store)` 提供，会话 ID 取自请求上下文，只能读本会话。

- 参数：`query`（子串，可选）、`before`（返回该序号之前的消息）、`limit`（默认 20，最大 100）。
- 来源：`session.Archived`，即日志中被 `history/rewrite` 改写掉的消息事件（不含已被 Rewind 撤销的区间，也不含改写时原样带入、仍可见的消息）。
- 返回：`[{seq, role, content, tool}]` 与 `next_before`；单条内容超过 4000 字时截断并标注。

### 2.4 验收

- 旧 `WithMemory` 的测试改写为 `WithSessionLog`；双会话交替调用互不可见。
- Compaction：超过阈值时压缩、不拆工具调用、摘要失败退化、请求超长重试、Rewind 后摘要消失、持久恢复不重复调用模型、`Model=nil` 时只省略。
- recall_history：能取回被摘要的原文；取不到其他会话与已撤销区间。

## 3. 第二阶段：长期记忆

### 3.1 记录

```go
type Record struct {
    ID          string
    Namespace   []string   // 由宿主根据可信身份给出：{tenant, user}、{tenant, agent} 等
    Kind        Kind       // fact | episode | reflection | skill
    Content     string
    Keywords    []string
    Entities    []string   // 第三阶段图谱的锚点
    Importance  float64    // 0-1，抽取时打分
    ValidFrom   time.Time
    InvalidAt   time.Time  // 零值表示有效
    SupersededBy string
    Sources     []Source   // 内容依据的全部轮次（会话 ID 与事件序号区间）；合并更新时继承旧记录的来源
    CreatedAt, LastAccess time.Time
    AccessCount int
    Pinned      bool       // 核心记忆
    EmbedModel  string     // 写入索引时的嵌入模型
    Reflected   bool       // 情景已参与反思
}
```

- 正文存于 `memory.RecordStore`（默认基于 `store.Store`），向量索引 `memory.Index` 是投影，可由正文重建；索引记录嵌入模型名，换模型时重建。
- 命名空间来自上下文中的可信身份（`memory.WithNamespace(ctx, ...)`），检索在取 topK 之前按命名空间过滤。没有命名空间时拒绝读写。

### 3.2 异步形成与演化

1. 轮次结束（`turn/end` 写入成功后，重放已完成的持久轮次时也登记）登记一条任务，Key 为"会话/turn/start 序号"，重复登记不产生新任务，存于 `store.Store` 的 `{"memory","jobs"}`。上一轮未结束、在下一轮开始时以中止结束的，登记 `Aborted` 任务。
2. 宿主运行 `go mem.Run(ctx, onError)`；Worker 从会话日志读该轮的消息事件（跳过注入的召回块，已被退回的轮次跳过），请求抽取模型得到候选。同一任务存储只运行一个 Worker。
3. 每个候选检索同命名空间、同类别最相似的 `ReconcileCandidates` 条（不更新访问时间），由模型判定 add / update / invalidate / noop（Mem0）。update 与 invalidate 写入新记录，旧记录写 `InvalidAt` 与 `SupersededBy`（Zep），不物理删除。
4. 中止的轮次只写一条 `reflection` 教训（Reflexion）；未参与反思的有效情景累计重要度达到 `ReflectionThreshold` 时提炼结论并标记这些情景（Generative Agents）。
5. 记录 ID 由任务 Key 与序号决定，崩溃后重做同一任务覆盖而不是重复；任务失败按 `JobAttempts` 重试后标记 failed，错误记在任务中。
6. 宿主在 `session.Rewind` 之后调用 `mem.Rewound(ctx, session, to)`：任一来源位于被退回轮次的记录失效，只被这些记录取代的旧记录恢复有效。合并更新的记录带有旧记录与本轮两个来源，回退本轮时合并记录失效、旧记录恢复，较早轮次的事实不会随之丢失。

### 3.3 召回

- 混合检索：相关性取向量相似度与字符对重合（对中英文都适用）的较大者，与新近性（半衰期 `HalfLife`）、重要性按 `Weights` 加权（Generative Agents）；命名空间与类别在排序前过滤。召回会读取命名空间内全部记录，规模由 `Retention.MaxRecords` 限制。
- 自动召回：每轮第一次请求前的 PreStep 以本轮输入为查询，召回块以 `[相关长期记忆]` 开头，作为用户消息插在本轮输入之前（不作为 system），并说明只作参考、与用户当前所说冲突时以用户为准；持久运行回放 PreStep 结果，不再召回。
- 工具：`memory_search`、`memory_save`、`memory_forget`，作用域为当前命名空间。
- 核心记忆：`Pinned` 的记录（MemGPT Working Context）每轮都放在召回块最前，与召回结果共用 `RecallRunes`。

### 3.4 遗忘与保留

- 衰减只影响排序（MemoryBank 遗忘曲线）。
- 删除只有三种：TTL 到期、命名空间容量超限时淘汰得分最低的有效记录、按命名空间整体删除（合规）。
- 失效记录保留 `InvalidRetention` 后清理。

## 4. 第三阶段：时序知识图谱

- `Config.Graph` 非空时，抽取提示词要求在同一个 JSON 中输出关系 `{subject, predicate, object, fact, exclusive}`，实体带名称、类型与别名；用户本人统一记为 `用户`。
- 实体判重（Graphiti）：先按命名空间、类型与规范化名称（含别名）精确匹配，新出现的别名并入已有实体；匹配不到的，取同类型中名称向量余弦或字符对重合不低于 0.5 的至多 5 个实体作为候选，一轮中所有有候选的实体合成一次模型请求判定是否为同一对象，只接受该实体自己的候选 ID，判为重复时名称并入别名。没有候选时不请求模型。翻译名与别称（如"Acme"与"爱克米"）只能靠名称向量找到，没有配置 Embedder 时会成为两个节点。
- 关系边带 `ValidFrom`/`InvalidAt`（Zep 时序图），`Edge.ValidAt(t)` 判断某一时刻是否成立。生效时间取轮次开始事件的时间，即对话发生的时间，而不是 Worker 处理的时间。相同的有效关系不重复写入；`exclusive` 的谓词（主体同时只能有一个客体，如"住在"）出现新客体时，旧边失效并以 `SupersededBy` 指向新边；已有关系比新关系生效更晚时（迟到的旧事实），新边写入即失效并指向已有关系。非排他的关系并存。
- 存储：`memory.Graph` 接口，`NewGraph(store.Store)` 把实体、边与社区存于 `{"memory","entities"|"edges"|"communities",…}`；遍历时读取整个命名空间，规模随有效关系数线性增长。
- 召回（HippoRAG）：名称或别名出现在查询中的实体、以及召回记录 `Entities` 中的实体作为种子，取距种子 `GraphHops`（默认 2）跳内的有效边构成子图，在无向图上运行 Personalized PageRank（续走概率 0.5，种子权重为其有效关系数的倒数，即节点特异性，避免"用户"这类枢纽主导），按两端实体得分之和取 `GraphRelations`（默认 8）条，以"[关系 自日期] 陈述"放在召回块中；`memory_search` 同时返回关系。HippoRAG 用包含实体的段落数计算特异性，这里以关系数代替。
- 社区（Zep 社区子图）：`Config.Communities` 为真时，Worker 每个任务后在有效边上做标签传播（Graphiti；异步更新、并列时保留当前标签或取最小者，保证确定），成员不少于 3 个的成为社区。有效关系数超过 12 的枢纽实体不参与划分，否则个人记忆中所有实体都会经"用户"连成一个社区；枢纽与成员间的关系仍计入社区的事实。社区指纹由成员与涉及成员的有效边决定，指纹不变时不重写摘要，变化时以最近 40 条关系请求模型写摘要（Graphiti 用成员摘要两两归并，这里实体没有摘要，直接用关系）。召回时按成员 PageRank 得分之和附上至多 `GraphCommunities`（默认 2）个社区摘要，以"[主题 名称] 摘要"放在召回块中。
- `Rewound` 使来自被退回轮次的边失效、恢复只被它们取代的旧边，并删除指纹因此变化的社区（不调用模型，下一次任务重建）；`Purge` 清空图；`Prune` 删除失效超过保留期的边。
- 未实现：边的模型判重（同义谓词如"就职于"与"在…工作"会成为两条边）、实体摘要、Leiden 社区划分与 GraphRAG 的全局 map-reduce 问答。

## 5. 迁移

| 旧 | 新 |
|---|---|
| `WithMemory(memory.NewInMemoryManager(...))` | `WithSessionLog(session.NewMemoryStore())`，会话 ID 用 `WithSessionID` 或 `tools.WithSessionID(ctx)` |
| `WithMaxConversationHistory(n)` | `WithCompaction(contextpolicy.NewCompaction(...))` |
| `WithMemoryFailurePolicy` | 删除；会话日志写入失败即本轮失败 |
| `MemoryManager.AddCase/SearchSimilarCases/Store/Retrieve` | `memory.Memory` 的 `Remember`/`Recall`；纯键值数据用 `store.Store` |
| `memory.HierarchicalMemory`、`InMemoryManager`、`InMemoryVectorStore`、`ChromaVectorStore` 等 | `memory.New(memory.Config{...})`、`memory.NewMemoryIndex`；外部向量库实现 `memory.Index` |
| goagent-harness `preset` 的 `compaction` | 组件保留，内部改用 `contextpolicy.Compaction` 并以 `Keep`/`Annotate` 注入编码场景逻辑 |

## 6. 评测

`memory/memeval` 把数据集的每个会话作为一轮写入会话日志（轮次时间取会话日期）并按时间顺序抽取，再逐题取上下文、作答与判定。对比的方案：

| 方案 | 上下文 |
|---|---|
| `recent` | 不用长期记忆，从最近的会话往前取原文，默认与召回块同为 4000 字符 |
| `memory` | 召回块（记录） |
| `graph` | 召回块（记录 + 关系） |
| `community` | 召回块（记录 + 关系 + 社区摘要） |

- 数据：LongMemEval 读 `longmemeval_oracle.json`、`longmemeval_s_cleaned.json` 等（HF `xiaowu0162/longmemeval-cleaned`），每题自带历史，ID 含 `_abs` 的是弃答题；LOCOMO 读 `locomo10.json`，类别编号以官方评测代码为准（1 多跳、2 时间、3 开放域、4 单跳、5 对抗），第 5 类按弃答评分。
- 抽样：`-sample N` 按题型分层抽题，各题型名额与题数成正比（题数足够时每类至少一道），题型内按数据集顺序等间隔抽取，使题目分散在各段对话中；被抽中题目的对话仍完整灌入。
- 评分：判定用 LongMemEval 官方按题型的提示词（原文照录），回答含 yes 即正确；LOCOMO 另算官方的词级 F1（未做 Porter 词干化，略偏低），作答要求日期写成 "7 May 2023" 的格式以与标准答案一致；证据召回率是召回块所依据的会话（`Retrieval.Sessions`：记录的全部来源与关系的来源）中含答案所在会话的比例。
- 统计：`-repeats K` 重复整个评测，每轮重新抽取记忆。准确率以题目为单位（各轮先平均），给出 95% Wilson 区间与各轮范围；两两比较按同一轮同一道题配对，用双侧精确符号检验给出 p 值，p ≥ 0.05 的差异不作结论。
- 运行：`go run ./scripts/memeval -format locomo -data locomo10.json -sample 200 -repeats 2 -concurrency 8 -chat deepseek -judge openai:gpt-4o-2024-08-06 -embed gemini:gemini-embedding-001 -out docs/evaluation/<日期>-memory/locomo.md -results locomo.jsonl`。长期记忆方案每个会话有一次抽取和若干次整理调用，`longmemeval_s` 每题约 50 个会话，先用较小的 `-sample` 估算费用。官方判定模型是 gpt-4o-2024-08-06，换用其他模型时结果只在本仓库内可比。
- 结果存入 `docs/evaluation/`。[2026-11-10-memory](../evaluation/2026-11-10-memory/README.md)：LOCOMO 分层抽样 200 题 × 2 轮、gpt-4o 判定，准确率 recent 25.0%、memory 46.5%、graph 53.0%、community 54.8%；memory 显著好于 recent，graph 显著好于 memory（p = 0.013），community 与 graph 无显著差异（p = 0.48），因此社区摘要默认关闭。
