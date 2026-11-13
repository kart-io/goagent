# 长期记忆 G1：存储改造的实现设计

日期：2026-11-12。状态：第 6 版。三项修订（增量提交、bigram 索引、pgstore 快照列压缩方式）已按用户批准实施，未提交；state 编码由 gzip 改为 zstd。G1a 门槛的完整结果见 7.8：全部通过（表大小按用户 2026-11-12 的确认，以单命名空间 ≤ 1 次提交/秒为目标速率）。G1b 进行中。基线：分支 `feat/memory-g0-recall`，HEAD `a4f09197`（G0 的两个提交，不改写）。

本文是 [长期记忆的删除优先](MEMORY_GOVERNANCE.md) 第 3 节的实现设计。目标与取舍以那份文档为准，本文只写落地所需的盘点、数据模型、操作语义、迁移、回滚、兼容性与验证方案。

修订经过：
- 第 1 版经独立评审（Backend Architect agent）退回，62 分：4 项阻断、9 项重要、9 项建议。
- 第 2 版复审有条件通过，80 分，新发现 1 项阻断、4 项重要、7 项建议。
- 第 3 版处理全部复审意见，对照见第 11 节。
- 第 5 版：按用户选择的"先测 pgstore"完成 7.6 的验证（7.7）。用户据此批准三项修订的**设计**：增量提交、bigram 索引、pgstore 快照列的存储方式。三份提案见第 12 节，均未实施。
- 第 4 版：G1a 原型基准在 5k 条记录时未达阈值，用户选定"方案 1（版本头 + 进程内缓存 + 压缩编码）+ 方案 3（较低的默认上限）"。本版重写 3.2、3.4～3.6 与 7.3，新增 7.6；实测数据推翻了部分预估（gzip 的作用、版本头的放置），见第 11 节"第 4 版"。

**用户决策（2026-11-11）**：
1. 旧数据清理采用方案 A：迁移时 `ClearLegacy`（5.3）。
2. `TrackAccess` 默认关闭（8 节）。
3. `RollbackWindow` 默认为 0，即关闭删除日志；需要保留数据回滚能力的宿主自行开启（4.6、6）。

**范围**：
- 包含：
  - 存储布局、作用域与 `Purge`、版本字段，所有写路径改为锁内修改。
  - 版本化索引、任务队列、Worker 互斥。
  - `Remember` 只创建与新增 `Update`，`LastForgetSeq` 丢弃规则。
  - 删除日志、迁移函数。
- 不包含：
  - G2 的派生关系、级联重算、回退标记、Worker 单次提交。
  - 纯聊天召回。
  - "Request 钩子改动召回消息"的测试缺口，它仍是独立 TODO。
  - 下游仓库迁移。
- **不改变的 G0 语义**：
  - 轮次绑定以 `TurnStart` 为唯一来源。
  - 召回块不落盘，日志只记引用。
  - 意图由日志子集决定，G0 日志的意图字节在 G1 下保持不变（见 4.5）。
  - 命名空间改变时返回 `ErrMemoryNamespaceChanged`。
  - 记忆工具的名称、说明与参数 Schema 不变，因为它们会进入意图。

## 1. 现状盘点

以下均对照 `a4f09197` 的代码确认。

### 1.1 存储

| 数据 | 位置 | 代码 |
|---|---|---|
| 记录 | `store.Store` 的 `{"memory","records",命名空间...}/ID`，值为 JSON 文本 | `memory/store.go` 的 `recordStore` |
| 实体、关系、社区 | `{"memory","entities"/"edges"/"communities",命名空间...}/ID` | `memory/graph.go` 的 `storeGraph` |
| 实体向量 | 实体对象的 `Vector` 字段 | `memory/graph.go` 的 `Entity` |
| 任务 | 全局 `{"memory","jobs"}/Key`，不分命名空间 | `memory/store.go` 的 `putJob`/`pendingJobs` |
| 记录向量 | `Index`，键为记录 ID，命名空间按 NUL 拼接；接口只有 `Upsert/Delete/Search`，读不出向量 | `memory/index.go` |

- `store.Store` 只有逐键的 `Put/Get/Delete/Search/List/Clear`，没有锁、版本或多键原子写。它不能列出子命名空间，`Clear` 是否递归也没有约定（`store/memory` 只清精确的命名空间）。
- 仓库内的实现都是进程内存储。持久的旧数据只可能存在于宿主自己实现的 `store.Store` 中。

### 1.2 写路径

全部是"锁外读取整条，修改后整条写回"：

| 写路径 | 现状 | 问题 |
|---|---|---|
| `Remember` | 先 `Index.Upsert` 再 `Put`；同 ID 覆盖 | 正文写失败时留下"旧正文、新向量"；可覆盖已遗忘的记录 |
| `Recall` | 改 `LastAccess`、`AccessCount` 后整条写回 | 与 `Forget` 交错时让记录复活 |
| `Forget`、`invalidate` | 读出后写 `InvalidAt`，`SupersededBy` 置空，再删索引 | 与上一行交错时被覆盖 |
| `Rewound` | 读出全部记录与关系后逐条写回 | 同上；恢复时不区分失效原因 |
| `Reindex` | 嵌入后整条写回 | 同上；旧向量覆盖新向量 |
| `Prune` | 逐条删除或失效；`MaxRecords` 淘汰按 `LastAccess` 算新近性 | 同上 |
| `Purge` | 逐条删索引 → 清图谱 → 清记录 | 非原子；不影响任务；残留孤儿向量 |
| Worker | `reconcile` 先写新记录再失效旧记录；`reflect`、`resolveEntities`、`syncCommunities` 都在模型调用后用旧副本写回；`ProcessPending` 用列表阶段的旧任务副本写回 | 治理文档 1.3 节列出的全部竞态；注释要求只运行一个 Worker，但没有强制 |
| `Enqueue` | 先查后写，按全局 Key 去重 | 不同命名空间同 Key 时静默丢弃 |

### 1.3 可复用的基础设施

- **`durable.Store`**（`core/durable/runner.go`）：
  - `Lock` 不可重入，不检测死锁；进程退出后锁会释放。
  - `Load` 不加锁，读到的是最近一次成功写入的完整快照。
  - `Save` 原子且持久；持有者已失去锁时可以返回 `ErrLeaseLost`。
  - `SaveAll` 对多个 ID 原子写入。
  - 不能列出 ID。
- **`durable.Scoped`**：
  - `RemoveScope` 写入墓碑，之后读写返回 `ErrScopeRemoved`。名称不能复用；重复调用可以清理残留。
  - `PruneRemoved` 回收墓碑。
- **两种实现**：

  | | FileStore | pgstore |
  |---|---|---|
  | 锁 | flock | advisory 锁加租约，租约过期后可被接管 |
  | 失去锁时 Save | — | 令牌与写入在同一事务内校验，返回 `ErrLeaseLost` 并整体回滚，结果确定 |
  | `RemoveScope` | 等待持锁者释放 | 不等待持锁者，进行中的 Save 直接得到 `ErrScopeRemoved` |

  两者都通过 `core/durable/storetest`。
- **`store.NamespaceKey`**：长度前缀编码。

### 1.4 调用方

- `memory.Config` 的使用方只有本仓库：`scripts/memeval`、`memory/memeval`、`codec/tags_test.go`、`builder` 测试。
- goagent-harness 与 goagent-examples 不使用它。

## 2. 目标架构

```mermaid
flowchart TB
    subgraph M[memory.Memory]
        OP[公开操作] --> MU[mutate：锁内修改]
        OP --> RD[read：两次核对作用域的读取]
        W[Worker：持 memory/worker 锁] --> MU
    end
    MU --> DS[(durable.Store + Scoped)]
    RD --> DS
    MU -.提交后、写后复核.-> IX[Index：版本化键]
    subgraph DSL[durable.Store 中的布局]
        META["根：memory/meta/NK(ns)<br/>Current、Purges、Migrated"]
        RET["根：memory/retiring<br/>全局待删除作用域"]
        Q["根：memory/queue/0..15<br/>按 hash(ns) 分片"]
        DEL["根：memory/deletions/NK(ns)<br/>删除日志（回滚窗口内）"]
        WK["根：memory/worker（只用锁）"]
        SC["作用域 memory:NK(ns)#scopeID"]
        SC --> ST["state：记录、图谱、任务"]
        SC --> AC["access：访问时间（TrackAccess）"]
    end
```

`NK` 即 `store.NamespaceKey`。

- **ID 前缀**：全部以 `memory/` 或 `memory:` 开头，同一个 `durable.Store` 可以与 Agent 的持久运行共用。
- **快照**：
  - meta、retiring 等小快照：`Snapshot{Version: "memory-state/1", Plan: "memory", Data: JSON}`。
  - state：`Snapshot{Version: "memory-state-blob/2", Plan: "memory", Data: [版本头, 数据块…]}`，编码见 3.4。
  - 版本号不认识时拒绝读取。
- **作用域名**：`memory:` + `NK(ns)` + `#` + 作用域 ID。
  - 初始作用域 ID 固定为 `0`。
  - 每次 `Purge` 生成一个随机 UUID 作为新的作用域 ID，满足"名称不可复用"。
- **索引命名空间**：
  - 记录用 `append(ns, "#"+scopeID)`。
  - 实体用 `append(ns, "#"+scopeID, "#entity")`，与记录分开，不占用召回的候选名额。
  - 命名空间组件不得包含 NUL，不应以 `#` 开头。

## 3. 数据模型

### 3.1 新增与变化的类型

```go
// meta：一个命名空间的作用域状态（根作用域）。不存在时等价于 {Current: "0"}。
type metaState struct {
    Current  string    `json:"current"`
    Purges   int64     `json:"purges"`             // Purge 次数；0 表示从未 Purge
    Migrating string    `json:"migrating,omitempty"` // 迁移进行中的迁移 ID（两阶段提交的第一阶段，见 5.1）
    Migrated  time.Time `json:"migrated,omitzero"`   // 迁移完成时间；非零时迁移不再导入（Purge 不清除 meta）
}

// retiring：全局待删除作用域（根作用域），与 meta 用 SaveAll 原子提交。
type retiringState struct {
    Scopes map[string]retiringEntry `json:"scopes"` // 键：作用域名；值：命名空间、加入时间
}

// state：一个命名空间、一个作用域的全部数据
type state struct {
    Seq             int64                `json:"seq"`
    CommitID        string               `json:"commit_id"`        // 每次 Save 生成的随机值，用于判断结果未知的 Save 是否已提交
    MigrationID     string               `json:"migration_id,omitempty"` // 导入旧数据的迁移 ID（5.1）
    NextIncarnation int64                `json:"next_incarnation"`
    LastForgetSeq   int64                `json:"last_forget_seq"`
    Records         map[string]Record    `json:"records"`
    Entities        map[string]Entity    `json:"entities"`         // 不含向量
    Edges           map[string]Edge      `json:"edges"`
    Communities     map[string]Community `json:"communities"`
    Jobs            map[string]Job       `json:"jobs"`             // 待处理与失败的任务
    DoneJobs        map[string]time.Time `json:"done_jobs"`        // 完成、丢弃或最终失败的任务键
}

// deletions：删除日志（根作用域，独立加锁），只在 Config.RollbackWindow > 0 时写入
type deletionLog struct {
    Forgotten []deletion `json:"forgotten"` // 记录 ID 与时间
    Purges    []deletion `json:"purges"`    // 作用域 ID 与时间
}
```

| 类型 | 新增字段 | 说明 |
|---|---|---|
| `Record` | `Incarnation`、`Revision`、`ContentRev`、`InvalidReason`（superseded / rewound / expired / forgotten） | `LastAccess`、`AccessCount` 不再写入 state |
| `Edge`、`Entity`、`Community` | `Incarnation`、`Revision`、`ContentRev` | `Entity.Vector`、`Entity.EmbedModel` 删除 |
| `Job` | `Scope`、`TurnSeq` | 来自轮次绑定 |
| `memory.TurnMemory` | `Scope`、`Seq`，都带 `omitzero` | — |
| `memory.Ref` | `Scope`、`Incarnation`、`ContentRev`，都带 `omitzero` | `RecallProtocol` 改为 `memory-recall/2` |

`underived` 原因属于 G2，G1 不产生。

### 3.2 规模上限

- `Config.MaxStateBytes` 限制一个命名空间的 state **解压后**的字节数（即数据块 gob 编码的总长，版本头的 `raw_bytes`），与压缩率无关；默认 8 MiB，最终取值以 7.3 的结论为准。
  - 存储设置标为 `json:"-"`，不进入召回配置的 `Identity`：调整上限或缓存不应使已有的召回快照失效。
- 只拒绝"让 state 变大并且超过上限"的提交，返回 `CodeStateSave`（错误码表中没有 `CodeResourceExhausted`），提示调小 `MaxRecords` 或缩短保留期。
- **永远放行**：
  - 删除与收缩类操作：`Forget`、`Prune`、`Rewound`、`Purge`、标记任务完成。即使提交后仍超过上限，也不拒绝，删除承诺优先。
  - `Enqueue`：超过上限时丢弃任务并计数，返回 nil，避免整轮执行失败。
- `read` 不受这个限制。

### 3.3 不变量

`checkInvariants` 在每个测试结束、并且系统静止之后运行：

1. 每个对象的 `Incarnation` 在作用域内唯一，并且小于 `NextIncarnation`。
2. `InvalidReason` 不为空，当且仅当 `InvalidAt` 不为零。
3. `forgotten` 是终态。
4. `Jobs` 与 `DoneJobs` 的键互不重叠，并且都属于这个命名空间。
5. 存在待处理任务的作用域，在 queue 分片中有登记。
6. 索引中被采用的键，都对应一条有效对象的当前版本。
7. 索引中没有 forgotten 对象的键，也没有非当前作用域的键（Retiring 清理完成之后）。
8. `retiring` 中不再存在已经完成清理的作用域。

### 3.4 state 的编码与版本头

state 快照的 `Data` 是一个 JSON 数组：

```text
[{"seq":…,"commit_id":…,"next_incarnation":…,"last_forget_seq":…,"migration_id":…,"raw_bytes":…}, "数据块1", …, "数据块16"]
```

- **版本头**是数组的第一个元素，包含 state 的全部标量字段与 `raw_bytes`。
  - 读写方用流式解码器只解析版本头（5k 条记录约 0.4ms），就能得到 `CommitID` 并与缓存核对，不必扫描数据块。
  - 版本头与数据块在**同一个快照**中，一次 `Save` 原子提交，两者不可能不一致。
- **完整性**：
  - 版本头记录每块解压后的长度 `chunk_bytes`，解码时核对块数、逐块限长，并核对总和等于 `raw_bytes`。限长也挡住了读路径上的解压炸弹。
  - 标量另存一份在第一块中，完整解码时与版本头核对。
  - 数组的分隔处容忍 JSON 空白，以适应会重排 JSON 的存储。
- **规范形式**：提交前把 state 改为解码后的形式（`normalize`）：对象的 `Namespace` 设为 ns，空切片设为 nil。
  - 这样写入方放进缓存的 state，与其他实例完整解码得到的 state 逐字节等价，召回结果不因是否命中缓存而不同。
  - 对象属于其他命名空间时拒绝提交（`CodeInvalidInput`），使不变量 4 在写入时就被强制。
- **数据块**：把各映射的条目轮流分到 16 块，每块依次做 gob 编码、zstd 压缩（`github.com/klauspost/compress/zstd`，默认级别）、base64 编码。第 6 版由 gzip 改为 zstd，版本升为 `memory-state-blob/2`，见 7.8。
  - base64 字面量不含引号与转义，解码时按引号定位，不经 JSON 扫描。
  - 16 块并行编解码，解码后按键合并为映射。
  - zstd 帧的校验和发现截断与损坏，错误为 `CodeStateLoad`。
  - 解压的输出以版本头记录的块长度为上限（`WithDecodeAllCapLimit`），长度不符即拒绝，挡住读路径上的解压炸弹。
- **省去的字段**：对象的 `Namespace` 不写入，解码时补为 state 所属的命名空间（不变量 4）。命名空间通常是两个较长的 ID，每个对象重复一份会使数据量增加一成以上。
- **选择 gob 与压缩的依据**（7.3 实测，5k 条记录、真实中文）：
  - gob 解码 11.8ms，JSON 解码 36.5ms，同样的数据 gob 体积约小 30%。
  - gzip 的收益是**减少写入与读取的字节**，而非加快解码：真实中文压缩 1.7 倍，低压缩率数据 1.4 倍。逐块并行之后，压缩成本低于少写的字节带来的节省。
  - 之前在重复度很高的合成数据上测得 28 倍的压缩率，据此得出"gzip 消除 JSON 解析开销"的结论。真实数据推翻了这个结论，见第 11 节。
  - 改用 zstd 的依据（7.8 的冷读分解，5k、4 核）：gzip 解压约占冷读 CPU 的一半。同样 16 块并行，解压耗时 gzip 6.5～7.0ms、zstd 2.0ms；压缩后体积真实中文减少 22%（3,539 → 2,753 KiB）、低压缩率减少 5%。体积变小同时减少了 pgstore 的读取与 JSON 解析。
- **兼容**：gob 按字段名匹配，新增字段读旧数据时为零值，删除的字段被忽略。与 JSON 不同的两点：
  - 字段类型的不兼容修改（如 string 改为 int）会使解码报错，必须升级版本号。
  - 指向零值的指针解码为 nil。编码格式不变时不升版本号。改变格式（例如更换压缩算法）时升级 `memory-state-blob/N`，旧版本拒绝读取新格式。
- **代价**：state 在存储中不再是可读的 JSON。排查时用 `memory` 包提供的解码函数（G1f 随管理工具提供）。

### 3.5 进程内缓存

- `Config.StateCacheBytes`：解码后 state 的 LRU 缓存，按**估计的堆占用**（`raw_bytes` × 2.5）限制总量，默认 64 MiB，设为负数关闭。
  - 7.3 实测解码后的堆占用约为 `raw_bytes` 的 1.8～2.3 倍，2.5 是留有余量的估计，使配置值接近真实内存。
  - 超过容量的单个 state 不缓存。
- 键为 `NK(ns)`，值为（作用域 ID，state）。同一命名空间只保留一份：作用域相同时不以旧的 `Seq` 覆盖新的，作用域不同时直接替换。
- **缓存只省去解码，从不省去 Load**：
  - 每次读、写都先从存储 `Load` 最新的 state 快照并解析版本头。
  - 作用域与 `CommitID` 都与缓存相同时才采用缓存，否则完整解码并替换缓存。
- **共享与只读**：
  - 缓存中的 state 由多个读者共享，`read` 与 `mutate` 返回的 state 只读。
  - `mutate` 在副本上修改：复制各映射，值按结构体复制；值内的切片与原 state 共享，修改函数必须整体替换而不能原地修改它们。
  - 修改函数出错、超过上限或提交失败时，缓存保持不变。
  - "整体替换"目前只是约定。G1b 编写大量修改函数时，把它变成可检查的规则：提供写时复制的取值函数，测试中对 `mutate` 之前已返回的 state 做哈希比对，并在 `-race` 下运行。
- `read` 与 `mutate` 在入口复制 ns：缓存中的对象共享它，不能引用调用方可能修改的切片。

### 3.6 一致性规则

1. **原子提交**：一次提交只有一次 `Save`，版本头、标量与数据块同在一个快照中。
   - `durable.Store` 保证 `Save` 原子，`Load` 读到最近一次成功的 `Save`，而不是滞后的副本。
   - 不使用单独的版本头快照，原因有二：
     - FileStore 上 `SaveAll(版本头, state)` 实测 107ms（事务日志把数据写两遍），额外一次小 `Save` 也要 17ms（两次 fsync）。
     - 两个快照分开写时，崩溃会留下"版本头比数据新"或"比数据旧"的状态，后者会让读者用缓存返回已被删除的内容。
2. **跨进程缓存一致**：读者看到的提交，永远是 `Load` 时存储中的最新提交。
   - 缓存只在 `CommitID` 相同时代替解码。`CommitID` 是每次提交生成的 UUID，不会重复，因此没有 ABA。
   - 所以多实例下的一致性与不用缓存时完全相同：另一实例提交后，本实例的下一次读取立即看到。
   - 代价只是那次读取未命中缓存（7.3 的"跨实例读"）。
3. **加锁后核对**：`mutate` 获得 state 锁、再次核对 meta 之后，重新 `Load` 快照，并按版本头的 `CommitID` 核对缓存。
   - 缓存过期时重新解码。
   - 不会在其他进程已经提交过的旧版本上修改，丢失更新无从发生。
4. **结果未知**：`Save` 返回无法判断结果的错误时：
   - 先移出该命名空间的缓存条目，再 `Load` 并比较版本头的 `CommitID`。
   - 相同才视为成功并写回缓存。
   - 不能确认时返回 `ErrMemoryCommitUnknown`，未确认的 state 不进入缓存。
5. **Purge 与命名空间代际**：作用域 ID 就是命名空间的代际，`Purge` 生成新的 UUID，名称永不复用。
   - 缓存条目带作用域 ID，读写都先从 meta 解析当前作用域，旧代际的条目不会被采用。
   - 再加上 `read` 的 meta 双读与 `mutate` 的加锁后核对，`Purge` 返回之后不会再有读写落在旧代际上。
   - 旧代际的条目在同一命名空间下一次写入缓存时被替换，或被 LRU 淘汰。
6. **读者单调**：同一个读者连续读取，看到的 `Seq` 不减（7.3 的压力测试校验）。原因是每次 `Load` 都读到最新提交，而缓存不会返回比 `Load` 结果更旧的版本。

## 4. 操作语义

### 4.1 两个原语

```go
// mutate：锁内修改当前作用域的 state
func (m *Memory) mutate(ctx, ns []string, expectScope string, fn func(*state) error, opts ...mutateOpt) (state, error)
//  1. Load(meta)；不存在时 Current = "0"。expectScope 非空且不同 → ErrScopeChanged
//  2. Lock(scope/state)
//  3. 再 Load(meta)：Current 已变化 → 解锁，返回 ErrScopeChanged
//  4. Load(state)，只解析版本头：不存在时初始化；作用域与 CommitID 都与缓存相同时取缓存，否则完整解码并替换缓存（3.6 第 3 条）
//     在副本上修改：复制各映射（3.5）
//  5. opts.beforeSave（只有 Enqueue 使用，见 4.3）
//  6. fn(&s)；s.Seq++；s.CommitID = 随机值；normalize（3.4）；编码（3.4）；按解压后的字节数检查 MaxStateBytes；Save；成功后写入缓存
//  7. Save 的错误分三类：
//     - ErrLeaseLost：确定失败，从第 1 步整体重做；共尝试 3 次（重做 2 次），无退避。fn 可能被调用多次，必须是纯函数
//     - Save 与结果未知时的复查共用一个时间上限 MemoryCommitTimeout（30 秒），不随调用方取消；超时按结果未知处理，不无限持锁
//     - ErrScopeRemoved：确定失败，转为 ErrScopeChanged
//     - 其他错误（结果未知）：移出缓存条目，重新 Load 并解析版本头，CommitID 等于本次生成的值即为成功并写回缓存；
//       不等时不能断定失败（服务端可能稍后才完成提交），返回 ErrMemoryCommitUnknown（包装原错误）
//  8. 解锁。Lock、Load 阶段遇到 ErrScopeRemoved 同样转为 ErrScopeChanged

// read：不加锁的一致读取
func (m *Memory) read(ctx, ns []string) (scope string, s state, err error)
//  Load(meta) → Load(state)（与 mutate 第 4 步相同：按版本头核对缓存）→ Load(meta)：两次 Current 不同时重读，最多 3 次，否则返回 CodeConflict
//  state 不存在，或作用域已删除，视为空 state；返回的 state 与缓存共享，只读
```

- **首写不需要锁 meta**：meta 不存在时，所有写入方都得到同一个确定的作用域 `0`。只有 `Purge` 和迁移会在 meta 锁内写 meta，迁移不改变 `Current`，因此并发首写不会产生两个作用域。
- **`ErrMemoryCommitUnknown`**（文中简称 `ErrCommitUnknown`）：调用方可以重试。
  - `Forget`、`Update` 重试是幂等的：Update 依靠 `expect` 修订号。
  - `Remember` 重试时可能得到 `CodeConflict`，说明上一次其实已经提交。
  - `memory_save` 把它转为工具错误，说明"结果未知，请先 `memory_search` 确认"。
- **`fn` 内不做 I/O**：需要模型或嵌入的工作在锁外完成，结果在 `fn` 内按 Basis 复核。
- **加锁顺序**：

  | 持有多把锁的操作 | 顺序 |
  |---|---|
  | Purge | meta → retiring → deletions（都在根作用域，用一次 `SaveAll` 提交） |
  | 清理 | `RemoveScope`、`Index.Clear` 期间不持有任何锁；完成后按 meta → retiring 加锁并移出条目 |
  | 迁移 | meta → state（两阶段，见 5.1） |
  | Enqueue、Forget、Worker 移除登记 | state → queue 分片 |
  | Forget 写删除日志 | 只持有 deletions 锁，并且在取 state 锁之前完成 |
  | Worker | `memory/worker` 先于其他所有锁 |

  这几组锁之间没有交叉，所以不会成环。

- **召回只读一次**：`Snapshot`、`Recall`、`Related`、`Pinned` 改为对同一份 `read` 结果计算。一轮召回只解码一次 state，`Hydrate` 再读一次。

### 4.2 各操作

| 操作 | 实现 | 冲突与错误 |
|---|---|---|
| `Remember`（宿主） | 锁外嵌入 → `mutate`：ID 已存在返回 `CodeConflict`；分配实例号；`Revision = ContentRev = 1` → 提交后写索引并复核（4.4） | 作用域在嵌入期间变化：重试一次，仍变化则 `CodeConflict` |
| `Remember`（`memory_save`） | 同上，`expectScope = TurnMemory.Scope`；见 4.5 的旧绑定规则 | `ErrScopeChanged` 转为工具错误 |
| `Update(id, expect, Patch)` | 正文变化时锁外嵌入 → `mutate`：记录有效且 `Revision == expect`，否则 `CodeConflict`；`Revision++`；正文变化时 `ContentRev++` → 写索引并复核 | G1 不级联 |
| `Forget` | 开启删除日志时**先写日志**（4.6）→ `mutate`：已是 forgotten 时直接返回；否则改为 forgotten，`LastForgetSeq = 新 Seq`；`beforeSave` 在 queue 分片中登记"索引待清理"→ 提交后删除这条记录的全部索引键 | 记录不存在时返回 `CodeStoreNotFound` |
| `Rewound` | `mutate`：来源被回退的有效记录改为 rewound；superseded 记录只有在替代者的原因是 rewound 时才恢复（替代者是 forgotten 时不恢复）；forgotten 不动；关系同理；涉及变化关系的社区删除 → 恢复的记录重写向量，失效的删除向量 | — |
| `Prune` | `mutate`：删除超过保留期的失效对象；TTL 到期改为 expired；超出 `MaxRecords` 的改为 expired（`TrackAccess` 关闭时新近性按 `CreatedAt` 计算）；删除超过保留期的 `DoneJobs` → 删除对应索引键；开启 `TrackAccess` 时同时清理 access 中已删除对象的条目；之后单独锁 deletions，删除超过 `RollbackWindow` 的条目 | — |
| `Reindex(force)` | `read` → 锁外嵌入缺失或过期的有效对象 → 写入版本化键 → 复核 → `IDs` 列出全部键，删除确认过期的键 | 不写 state |
| 召回（`Snapshot` 等） | `read`，不写 state。`TrackAccess` 开启时异步批量 `mutate(access)`，失败忽略 | — |
| `Hydrate` | `read`；引用的作用域不是 Current、实例号或 `ContentRev` 不一致的项丢弃（G0 格式的引用见 4.5） | — |
| `Purge` | meta 的 `Migrating` 非空时拒绝，返回 `CodeConflict`，因为迁移阶段二会写入当时的 Current → Lock(meta) → Lock(retiring) → Lock(deletions，仅在开启删除日志时) → 一次 `SaveAll`：meta 的 `Current` 改为新 UUID、`Purges++`；retiring 加入旧作用域；删除日志追加这次 Purge → 解锁 → 执行一次清理（4.3） | 提交点是这次 `SaveAll` |
| `Enqueue(job)` | 见 4.3 | — |
| `Job(ctx, key)` | `read` 当前作用域，在 `Jobs` 与 `DoneJobs` 中查找 | — |

### 4.3 任务、队列、Worker 与清理

**queue 分片**：16 个快照 `memory/queue/<hash(NK(ns)) % 16>`。每条登记是 `{ns, scope, 原因}`，原因有两种：
- 有任务。
- 索引待清理：由 Forget 登记，表示提交后可能残留向量。

**Enqueue**：

1. 旧绑定没有 `Scope` 时，按 4.5 的规则处理。
2. 调用 `mutate(expectScope = job.Scope)`，其中 `beforeSave` 依次执行：
   1. 任务的键已在 `Jobs` 或 `DoneJobs` 中，就什么都不做。
   2. 先不加锁 Load 对应的 queue 分片；已经登记就跳过第 3 步。这样做安全，理由有两条：
      - 当前作用域的登记，只有持有 state 锁（此刻由本方持有）才能移除。
      - 非当前作用域的登记会被无锁移除，但那时这次 Enqueue 必然会因 `ErrScopeChanged` 失败，任务本来就会被丢弃。
   3. 否则 Lock(分片) → 写入登记 → 解锁。
3. 随后 `fn` 写入任务，`TurnSeq` 取自绑定。
4. 遇到 `ErrScopeChanged` 时返回 nil，并计数"跨越 Purge 丢弃的任务"。已完成轮次的回放会再次调用 Enqueue，所以这里不能报错。

**维护与 Worker 分离**：
- `Memory.Maintain(ctx)` 执行不需要模型的维护：
  - 清理 retiring。
  - `RetireGrace` 之后再执行一次 `Index.Clear`。
  - 处理"索引待清理"登记：**先**在 state 锁内移除登记，**再**执行 `Reindex(false)`。这样，Reindex 期间新提交的 Forget 会重新登记，残留不会被漏掉。当前作用域的这类登记与任务登记一样，只能在持有 state 锁时移除。
  - 每 `ReindexEvery` 轮，对有登记的命名空间执行 `Reindex(false)`。
- `Memory.RunMaintenance(ctx, interval)` 循环调用 `Maintain`。
- `Run`（Worker）每轮先调用 `Maintain`，再处理任务。
- 没有配置 `Model` 与 `Sessions` 的部署，宿主必须运行 `RunMaintenance`，否则 Purge 的残留与 Forget 的向量残留不会被清理。README 与 `New` 的文档注释都写明这一点。

**Worker 每一轮**：

```mermaid
sequenceDiagram
    participant W as Worker
    participant K as memory/worker 锁
    participant R as retiring
    participant Q as queue 分片
    participant S as state(ns, scope)
    participant L as 模型
    W->>K: Lock（多实例互斥）
    W->>R: Maintain：不持锁执行 RemoveScope、Index.Clear，完成后加锁移出；处理索引待清理登记
    W->>Q: Load 全部分片
    loop 每个登记
        alt 作用域不是 Current
            W->>Q: 不取 state 锁，直接移除登记
        else
            W->>S: read，取待处理的任务
            W->>W: LastForgetSeq > TurnSeq → mutate 移入 DoneJobs（丢弃）
            W->>L: 抽取、整理、实体判重（锁外）
            loop 每一项写入
                W->>S: mutate：按 Basis 复核；作用域变化或 LastForgetSeq > TurnSeq 时中止任务
            end
            W->>S: mutate：在锁内读取任务并更新状态（完成或失败加一）
            W->>S: Lock(state) → 没有待处理任务时 Lock(分片) → 移除登记
            W->>K: Save 心跳；收到 ErrLeaseLost 时结束本轮
        end
    end
    W->>K: Unlock
```

- **不取 state 锁就移除非当前作用域的登记**：针对旧作用域的 Enqueue 即使写入成功，也落在即将被删除的作用域中。而且旧作用域的 `Lock` 可能已经返回 `ErrScopeRemoved`。
- **多实例**：`memory/worker` 锁保证同一时刻只有一个 Worker 在处理。pgstore 的租约在进程死亡后会被接管。
  - Worker 每处理完一个任务，对 `memory/worker` 执行一次 `Save` 作为心跳。收到 `ErrLeaseLost` 就结束本轮，以免与接管者重复调用模型。
  - 数据安全不依赖这把锁：每次写入都有 Basis 复核。
- **清理**：
  - Worker 每轮执行。
  - `Purge` 结束前执行一次。
  - `New` 之后的第一次 Worker 轮次执行。
  - 清理以全局 `retiring` 为准，与命名空间是否再被访问无关。
  - 作用域加入 `retiring` 超过 `RetireGrace` 之后，再执行一次 `Index.Clear`，再移出列表。这样提交后才写入的迟到向量也会被清掉。
  - `RetireGrace` 默认 10 分钟，必须大于 Index 单次写入的超时与重试时间之和。
  - 执行 `RemoveScope` 与 `Index.Clear` 期间不持有任何锁，以免 pgstore 分批删除时阻塞 Purge。
- **复核粒度**：
  - 新增记录用确定性 ID，只创建。
  - 失效目标时，要求目标有效且 `Revision` 与展示给模型时相同。
  - 实体按 `Revision` 复核。
  - 社区按成员与边的 `(Incarnation, ContentRev)` 复核。
  - 反思按情景记录的 `(Incarnation, Revision)` 复核，并在同一次提交中设置 `Reflected`。
- **代价与重做**：一个任务约有"写入项数 + 2"次提交。崩溃后重做可能出现两种情况，作为已知限制记入第 10 节，G2 改为一次提交后消除：
  - 第 i 个候选的内容已经变化。
  - 第一次执行时新记录已经创建、旧记录还未失效。

### 4.4 索引写入与复核

所有写索引的地方都遵循同一规则：Remember、Update、Rewound、Reindex、实体向量、迁移。

1. 记录提交之后写入版本化的键：`ID#实例号.ContentRev#嵌入模型摘要`。
2. 写后复核：
   1. `read` 得到当前的 meta 与对象。
   2. 作用域已不是 Current，或者版本已不是自己写的那个，就删除刚写的键。
3. 删除确认过期的键，只删过期的。
4. 第 2 步之后崩溃留下的残留，由 Worker 定期的 `Reindex(false)` 与 `RetireGrace` 之后的再次 `Clear` 清掉。

`Index` 新增 `IDs(ctx, ns)`，用于列出键，以及 `Clear(ctx, ns)`。

**`Clear` 只清精确的命名空间，不递归。** 旧索引与新索引常常是同一个后端，按前缀清理时，`Clear(ns)` 会把 `ns#scope` 下的新向量一起删掉。这条约束写进接口注释，内置的 `MemoryIndex` 用测试验证。

### 4.5 与 G0 的衔接

| G0 的行为或数据 | G1 中的处理 |
|---|---|
| 意图中的占位消息 = 摘要（协议、Identity、Write、引用子集） | `Digest` 按 `s.Protocol` 分版本：`memory-recall/1` 固定使用 G0 的三字段 `Ref` 结构编码；`/2` 使用新结构。新字段带 `omitzero`；`Turn.Scope`、`Turn.Seq` 不进入摘要。**验收**：用 `a4f09197` 生成的日志字节，在 G1 下恢复并比较意图字节，必须一致 |
| 日志中的 G0 快照（`/1`） | `Hydrate` 遇到 `/1` 时召回为空；意图按上一行计算，不变；已完成的步骤照常回放 |
| G0 的 `TurnStart.Memory`，没有 `Scope` | `memory_save`：meta 的 `Purges == 0` 时使用 Current，否则返回工具错误，因为这一轮可能跨越了 Purge。Enqueue：**一律丢弃**并计数，不再按 `Purges` 判断。宿主漏列的命名空间 `LastForgetSeq` 为 0，处理这类任务可能重新抽取迁移前被遗忘的内容；而丢弃只影响升级过渡期内的一轮学习 |
| `CheckTurn` 核对写命名空间 | 不变 |
| `deriveHistory` 过滤遗留召回块 | 不变 |
| 记忆工具的说明与 Schema 进入意图 | G1 不修改 |

宿主按迁移报告重新登记的旧任务，同样没有 `Scope`，也会被丢弃。如果宿主确实需要重新登记，可以先确认内容，再给任务设置当前作用域与 `TurnSeq`：迁移会把 `LastForgetSeq` 设为提交时的 `Seq`，所以 `TurnSeq` 必须不小于这个值。

### 4.6 删除日志

- **开关**：`Config.RollbackWindow` 默认为 0，表示关闭。
- **开启后**：
  - `Forget`：在取 state 锁之前，先锁 `memory/deletions/NK(ns)`，追加 ID 与时间后保存，再执行遗忘。写日志失败则返回错误，不执行遗忘。
    - 写完日志后遗忘失败或崩溃，日志中会多一条，回滚时多删一条本来就被要求删除的内容，没有坏处。
    - 反过来的顺序会漏记，所以必须先写日志。
  - `Purge`：日志条目放进与 meta、retiring 同一次的 `SaveAll`，三者都在根作用域。
  - 超过 `RollbackWindow` 的条目，由 `Prune` 在完成 state 修改之后单独加锁删除。
- **用途**：数据回滚之后，宿主在旧存储上重放这些遗忘与 Purge（6 节）。
- **不开启时**：数据回滚不能保证删除承诺，文档写明。

## 5. 迁移方案

### 5.1 API

```go
// MigrateFromStore 把旧 store.Store 中给定命名空间的记录迁入 durable 存储。关系、实体、社区不迁入；
// 旧任务不迁入，以报告形式返回。调用前须停止 Agent 流量与 Worker（见 5.2）。
func (m *Memory) MigrateFromStore(ctx context.Context, legacy MigrationSource, namespaces [][]string, opts MigrateOptions) (MigrationReport, error)

type MigrationSource struct {
    Store store.Store // 旧记录与任务所在的存储；Clear 须只清精确的命名空间，不递归
    Index Index       // 旧索引（可为 nil）；ClearLegacy 时清空其中的旧命名空间
}

type MigrateOptions struct {
    ClearLegacy bool // 迁移成功（或此前已迁移）后清理旧数据
}
```

**每个命名空间**：

判断顺序：先看 `Migrating`（第 3 步第 4 小步），再看第 1、2 步。否则阶段二已提交、阶段三未完成时崩溃，重跑会被第 2 步误报为冲突。

1. **已迁移**：Load meta；`Migrated` 非零说明已经迁移过，不再导入。若 `ClearLegacy` 为真，仍执行第 5 步。
2. **未迁移但已有数据**：当前 state 中已有记录时，报告冲突并停止这个命名空间，不导入也不清理。这种情况说明违反了"迁移前停止流量"。
3. **导入**：两阶段提交。meta 在根作用域、state 在作用域内，按契约不能放进同一次 `SaveAll`。
   1. **阶段一**：Lock(meta)，写入 `Migrating = 迁移 ID`（随机），保存。持有 meta 锁直到阶段三结束，加锁顺序是 meta → state。
   2. **阶段二**：Lock(state)，在已有 state 上**合并**导入：
      - 保留已有的任务与计数器，`Seq` 只增不减。
      - 旧记录分配实例号，`Revision = ContentRev = 1`。
      - 失效记录：`SupersededBy` 非空时记为 superseded，为空时记为 forgotten（终态）。
      - `LastForgetSeq` 设为本次提交的 `Seq`，`MigrationID` 设为这次的迁移 ID。
      - 保存。
   3. **阶段三**：meta 写入 `Migrated = now`，清除 `Migrating`，保存，然后释放 meta 锁。
   4. **重跑时的判断**：
      - `Migrating` 非空，并且 state 的 `MigrationID` 与它相同：阶段二已提交，只补做阶段三。
      - `Migrating` 非空，但 state 的 `MigrationID` 与它不同：阶段二没有提交，从阶段二重做（沿用同一个迁移 ID）。
4. **向量**：提交之后为有效记录重新嵌入，写入版本化的键，按 4.4 复核。失败时留给 `Reindex(false)`，因为旧索引读不出向量。
5. **ClearLegacy**：
   - `legacy.Store.Clear({"memory","records",ns...})`，以及三个图谱命名空间。
   - 在 `{"memory","jobs"}` 中逐个删除这个命名空间的任务键。
   - 设置了 `legacy.Index` 时执行 `Index.Clear(ns)`。
   - 只有全部成功，才在报告中标记"已清理"。

**崩溃与重跑**：
- 阶段一之前崩溃：什么都没写。
- 阶段一与阶段二之间、阶段二与阶段三之间崩溃：按第 3 步第 4 小步的规则补完，不会重复导入，也不会把空 state 标记为已迁移。
- 阶段三之后崩溃：`Migrated` 已经写入，重跑不会再导入，只补做第 4、5 步。
- `Purge` 不清除 meta，所以 Purge 之后重跑迁移也不会让旧数据复活。

### 5.2 迁移流程

```mermaid
flowchart TB
    A[停止 Agent 流量与 Worker；让持久运行结束] --> B[宿主备份旧 store.Store 与旧索引]
    B --> C[部署 G1；Config.Durable 指向新存储；按需设置 RollbackWindow]
    C --> D[MigrateFromStore：宿主提供命名空间清单，报告附上旧任务中出现的命名空间]
    D --> E{报告中有冲突或失败?}
    E -->|是| F[处理后重跑；已迁移的命名空间不再导入]
    F --> D
    E -->|否| G[按需重新登记报告中的任务]
    G --> H[Reindex false，补齐向量]
    H --> I[恢复流量，启动 Worker]
    I --> J{选择的清理方案}
    J -->|A| K[迁移时已 ClearLegacy]
    J -->|B| L[观察期满后 ClearLegacy]
```

### 5.3 旧数据何时清理（已确认：方案 A）

| 方案 | 做法 | 回滚 | 删除承诺 |
|---|---|---|---|
| A（推荐） | 迁移时 `ClearLegacy: true` | 只能从备份恢复，需要开启删除日志并重放 | 迁移完成即兑现 |
| B | 观察期满后再执行 `ClearLegacy` | 观察期内可以直接回到旧版本，同样需要重放删除日志 | 观察期内，旧存储中保留着迁移后被遗忘的内容 |

已确认采用 A。迁移文档写明：不开启删除日志时，数据回滚只能从迁移前的备份恢复，迁移后的遗忘不会被重放。

## 6. 回滚方案

| 回滚对象 | 做法 | 影响 |
|---|---|---|
| 代码 | `git revert` G1 的提交，保留 G0 的两个提交 | G0 行为不变 |
| 进行中的运行 | **回滚前必须让所有持久运行结束，包括等待审批的运行。** G1 日志中的 `/2` 引用在 G0 中摘要不同，恢复时会因意图冲突失败 | 不提供 G0 侧的兼容代码 |
| 数据 | 用备份恢复旧存储 → 用 `memory/deletions` 中回滚窗口内的记录，在旧存储上重放 Forget 与 Purge → `Reindex(true)` | 迁移后的新增和修改丢失；删除日志保证遗忘不会复活。没有开启删除日志时，回滚会让迁移后遗忘的内容重新出现，文档写明 |
| 索引 | 回滚后 `Reindex(true)` | 版本化的键在 G0 中不会被采用 |

## 7. 验证方案

### 7.1 并发读写

用 `hookStore` 包装 `durable.Store` 与 `Index`，在每次调用前后插入回调，制造确定的交错。

| 交错 | 断言 |
|---|---|
| 召回读取期间 `Forget` 提交 | 之后的召回不含该记录 |
| `Reindex` 锁外嵌入期间 `Update` | 旧键不被采用，新键不被删除 |
| `Remember` 嵌入期间 `Purge` | 返回冲突；新作用域没有这条记录 |
| `Remember` 提交后、写索引前，`Purge` 完成全部清理 | 复核删除迟到的键；不变量 7 成立 |
| Worker 模型调用期间 `Forget` 或 `Purge` | 任务被丢弃或中止 |
| 社区摘要、反思、实体判重期间 `Forget` | 放弃写入 |
| 两个 Memory 并发首写同一个新命名空间 | 都写入作用域 `0`，没有丢失 |
| `Enqueue` 与 Worker 移除登记 | `-race` 下 1000 次随机交错，任务不丢失 |
| 两个 Worker 实例 | 同一时刻只有一个在处理；每个任务只调用一次模型 |
| state 接近 `MaxStateBytes` 时执行 Forget、Prune、Enqueue | Forget 与 Prune 照常提交；Enqueue 丢弃任务、计数并返回 nil |
| Worker 锁的租约被接管 | 原 Worker 在心跳时发现，结束本轮 |
| pgstore：持锁期间租约过期并被接管，接管方先提交 | `mutate` 收到 `ErrLeaseLost` 后整体重做，Forget 最终生效（`fakeStore` 模拟这一语义；有 PostgreSQL 时用真实 pgstore 再测一遍） |

### 7.2 崩溃恢复

用 `crashStore` 包装 `durable.Store` 与 `Index`，在第 N 次调用时返回错误，之后用新的 Memory 实例继续。

| 崩溃点 | 断言 |
|---|---|
| Purge：`SaveAll` 之后、`RemoveScope` 之前 | 旧作用域不可读写；之后的 Worker 清理会删除它，即使这个命名空间以后不再被访问 |
| Purge：`RemoveScope` 之后、`Index.Clear` 之前 | 清理时重做 |
| Forget：提交之后、删除索引之前；这个命名空间之后再没有轮次 | 索引键不被采用；"索引待清理"登记让 `Maintain` 清除残留；不变量 7 成立 |
| Forget：写完删除日志之后、提交之前 | 日志中多出一条，记录仍然有效；回滚重放时删除它，符合"被要求删除"的语义 |
| 只用宿主 API、没有 Worker 的部署，Purge 在 `RemoveScope` 之前崩溃 | `RunMaintenance` 完成清理 |
| Enqueue：登记已写入、任务未保存 | 只多出一条登记，之后被移除 |
| Worker：多次 `mutate` 之间 | 重做时 ID 不重复；任务状态正确 |
| `Save` 结果未知（实际成功） | 按 `CommitID` 判定为成功 |
| `Save` 结果未知（实际失败或无法判断） | 返回 `ErrCommitUnknown`，不误判为成功或失败；重试是幂等的 |
| 迁移：阶段一之后、阶段二之后、阶段三之后、ClearLegacy 中途 | 重跑后结果与一次成功的迁移相同；不重复导入；空 state 不会被标记为已迁移 |

### 7.3 一致性与性能（G1a 的门槛）

- 每个测试结束时运行 `checkInvariants`。

**基准**（`memory/state_bench_test.go`，命令见本节末尾）：

- **存储**：FileStore，放在 NVMe 上的 ext4（tmpfs 会低估 fsync）。一次小 `Save` 的固定开销是 17ms，即两次 fsync。
- **CPU**：同时报告 28 核与 `GOMAXPROCS=4`，因为编解码是并行的。
- **数据**：两组都使用 UUID 作为 ID、会话与任务键，命名空间是两个较长的 ID。
  - **真实中文**：从本仓库 `docs/design` 的中文正文中截取片段。
  - **低压缩率**：均匀随机的常用汉字，是压缩率的下界。
- **规模**：1k、5k、10k 条记录；关系是记录数的 2 倍，实体 1 倍，社区为记录数的 1/20；30% 的失效对象；3,000 个已完成任务键。
- **测量项**：
  - 体积：解压后的字节数、落盘字节数。
  - 写：`mutate` p50/p95、串行吞吐、4 写者吞吐。
  - 读：
    - 热读：本实例刚提交，缓存命中。
    - 冷读：关闭缓存，每次完整解码，即进程重启或被淘汰后的首次读取。
    - 跨实例读：另一实例刚提交，本实例的缓存失效。
  - 缓存：一写五读时的命中率、单个 state 的堆占用、持续淘汰时的读取延迟。
- **阈值**（不变，不放宽）：5k 条记录时，两组数据、两种核数都要满足：
  - `mutate` p95 ≤ 100ms。
  - 召回读取 p95 ≤ 30ms。冷读、热读、跨实例读都按这一个阈值判定，不另设冷启动 SLA。
  - 单命名空间串行写入 ≥ 20 次/秒。
  - pgstore 上 `mutate` p95 ≤ 200ms（7.6）。

**结果**：评审修复后（含 `normalize` 与完整性校验）重测，2026-11-11。

28 核：

| 数据 | 记录数 | 解压后 | 落盘 | mutate p50 / p95 | 串行吞吐 | 热读 p95 | 冷读 p95 | 跨实例读 p95 | 缓存堆占用 |
|---|---|---|---|---|---|---|---|---|---|
| 真实中文 | 1k | 1.3 MiB | 1.1 MiB | 22.1 / 24.3ms | 44.7/s | 1.5ms | 4.0ms | 4.1ms | — |
| 真实中文 | 4k | 4.8 MiB | 3.9 MiB | 38.2 / 45.6ms | 25.6/s | 5.8ms | 11.9ms | 12.1ms | 10.8 MiB |
| 真实中文 | 5k | 5.9 MiB | 4.8 MiB | 44.9 / 52.2ms | 22.0/s | 7.3ms | 15.0ms | 12.3ms | 13.2 MiB |
| 真实中文 | 10k | 11.6 MiB | 9.5 MiB | 69.1 / 82.1ms | 14.2/s | 11.5ms | 26.9ms | 23.5ms | 27.9 MiB |
| 低压缩率 | 1k | 1.4 MiB | 1.4 MiB | 24.4 / 26.5ms | 40.8/s | 2.5ms | 4.6ms | 4.4ms | — |
| 低压缩率 | 4k | 5.2 MiB | 5.1 MiB | 42.8 / 49.5ms | 23.0/s | 7.8ms | 13.6ms | 12.6ms | 12.3 MiB |
| 低压缩率 | 5k | 6.4 MiB | 6.3 MiB | 50.0 / 58.4ms | **19.7/s** | 8.4ms | 16.3ms | 16.2ms | 13.7 MiB |
| 低压缩率 | 10k | 12.7 MiB | 12.5 MiB | 87.7 / 101.8ms | 11.2/s | 16.1ms | 33.0ms | 28.4ms | 27.1 MiB |

4 核（`GOMAXPROCS=4`）：

| 数据 | 记录数 | mutate p50 / p95 | 串行吞吐 | 热读 p95 | 冷读 p95 | 跨实例读 p95 |
|---|---|---|---|---|---|---|
| 真实中文 | 3k | 37.5 / 43.0ms | 26.4/s | 3.3ms | 12.1ms | 12.4ms |
| 真实中文 | 4k | 44.0 / 49.5ms | 22.5/s | 4.4ms | 15.0ms | 17.1ms |
| 真实中文 | 5k | 52.2 / 59.1ms | **19.0/s** | 5.9ms | 19.7ms | 19.8ms |
| 真实中文 | 10k | 89.5 / 99.6ms | 11.1/s | 10.7ms | 38.4ms | 40.3ms |
| 低压缩率 | 3k | 41.3 / 45.8ms | 24.2/s | 3.8ms | 13.8ms | 14.3ms |
| 低压缩率 | 4k | 50.7 / 54.3ms | **19.8/s** | 5.5ms | 17.7ms | 17.7ms |
| 低压缩率 | 5k | 61.3 / 80.0ms | **15.7/s** | 7.4ms | 22.5ms | 22.6ms |
| 低压缩率 | 10k | 101.4 / 106.4ms | 9.9/s | 18.3ms | 40.8ms | 40.1ms |

缓存堆占用一列：1k 时 GC 抖动大于被测量本身，数值无意义，记为"—"；其余规模为解压后字节数的 1.6～2.4 倍。

**5k 条记录的串行吞吐**，修复前后共多次运行：

| 数据 | 28 核 | 4 核 |
|---|---|---|
| 真实中文 | 20.8～22.5/s | 18.4～19.5/s |
| 低压缩率 | 17.6～20.1/s | 15.7～17.3/s |

**达到串行吞吐阈值的规模**（4 核，两次运行的最差值）：

| 记录数 | 真实中文 | 低压缩率 | 低压缩率的解压后大小 |
|---|---|---|---|
| 3k | 25.7/s | 24.2/s | 3.9 MiB |
| 4k | 22.2/s | 19.5/s（未达标） | 5.2 MiB |

**其他场景**：

- **一写五读**：命中率 80% 是构造出来的结果：每次写入后第一读必然未命中，其余命中。它验证的是缓存行为，不是测得的命中率。
- **淘汰**：3 个 5k 条记录的命名空间轮流读取，缓存只容得下一份。30 次读取全部未命中，淘汰 29 次，读取均值 11.3ms、p95 14.5ms，占用 14.7 MiB，不超过容量 22.1 MiB。
- **10k 压力测试**（`TestStateStress10k`，30 秒，两个实例，4 个写者与 4 个读者）：
  - 提交 297 次，读取 7,283 次；写 p95 120ms，读 p95 35ms（受写入争用影响）。
  - 断言全部成立：新增的都在、删除的不再出现、`Seq` 等于成功写入次数、读者看到的 `Seq` 不减、缓存不超过容量、不变量成立。

**测量局限**（评审 I5～I7）：

1. **页缓存是热的**：冷读只是"解码冷"。真正进程重启后的首次读取还要从磁盘读文件，本基准没有测。
2. **只测了读取原语**：测的是 `read`，不是一轮完整召回（排序、PageRank、`Hydrate` 的再读）。G1b 实现召回后要按完整召回重测；读取阈值的口径不变。
3. **只测了 FileStore**：pgstore 上每次读取都要经网络传输整份快照，读取结论不能外推，见 7.6。
4. **语料偏小**："真实中文"语料约 3.4 万字，5k 条记录需要约 90 万字，每个片段平均重复多次，压缩率偏乐观。低压缩率组是下界，结论以它为准。
5. **样本量**：热读、冷读各 30 个样本，跨实例读 15 个，p95 只是第 2 大的值；只有串行吞吐做了重复运行。
6. **磁盘基线**：本机一次小 `Save` 是 17ms，两次 fsync 是写入开销中最大的一项。带掉电保护的 SSD 上不到 1ms，吞吐会高得多；用开发机磁盘决定架构存在偏差，生产存储的数据以 7.6 为准。

**结论（G1a 门槛：未通过，需要决策）**：

- **已达标**：
  - 读取：5k 条记录时冷读、热读、跨实例读的 p95，在两组数据、两种核数下都不超过 23ms，原预估的冷读 40ms 不成立，冷热共用 30ms 阈值。
  - `mutate` p95：5k 条记录时不超过 80ms。
- **未达标**：5k 条记录时的单命名空间串行写吞吐。
  - 28 核上低压缩率组在 17.6～20.1/s 之间，4 核上两组都不达标。
  - 能稳定达标的规模，在 4 核、最差数据下约为 3k 条记录（解压后约 4 MiB）。
  - 每次提交的下限是整份 state 的 I/O（`Load` 约 7ms，`Save` 约 31ms，其中 17ms 是 fsync 固定开销），在"整份重写"的模型内无法再降一半。
- **可选方向**（需要用户决策）：
  1. **方案 3 的严格形式**：把支持的规模定为 3k 条记录，`MaxStateBytes` 默认 4 MiB（解压后）。这是修改规模目标，不是放宽阈值。
  2. **冷热分离**（评审提出）：Prune 时用低频的 `SaveAll` 把失效对象与 `DoneJobs` 移入归档快照，热 state 只保留有效对象。
     - 按 30% 的失效比例，每次提交少写约 30% 的字节。
     - 仍是整份重写，没有撕裂读问题。
     - 5k 时可能刚好越过阈值，但余量很小。
  3. **增量提交**：基础快照与增量快照，基础快照固定槽位轮换（存储接口不能列出或删除 ID），读者按 `CommitID` 检测撕裂读。能明显降低每次提交的字节，但接近原方案 2，需要重新设计与评审。
  4. **先测 pgstore 再决策**（评审建议）：生产存储的瓶颈可能在读路径的整份传输而不是 fsync。按 7.6 测得 pgstore 的数据之后，再在以上方向中选择。
  - 组提交只提高并发吞吐，不改变串行吞吐，不能单独满足阈值。

**运行命令**：

```bash
MEMORY_STATE_BENCH=1 MEMORY_STATE_BENCH_DIR=$HOME/.cache/goagent-bench go test ./memory/ -run TestStateBenchmark -v -count=1 -timeout 30m
# 只测部分规模、指定核数：
GOMAXPROCS=4 MEMORY_STATE_BENCH=1 MEMORY_STATE_BENCH_SIZES=3000,4000,5000 MEMORY_STATE_BENCH_DIR=... go test ./memory/ -run TestStateBenchmarkReport -v
MEMORY_STATE_STRESS=1 MEMORY_STATE_STRESS_SECONDS=30 MEMORY_STATE_BENCH_DIR=... go test ./memory/ -run TestStateStress10k -v
```

### 7.4 迁移

| 场景 | 断言 |
|---|---|
| 旧存储有有效记录、两类失效记录、关系、实体、跨命名空间同 Key 的任务 | 记录逐字段等价；`SupersededBy` 为空的映射为 forgotten；关系与实体不迁入；报告列出全部任务 |
| 重跑 | 已迁移的不再导入 |
| 迁移后 Purge，再重跑 | 不导入，新作用域保持为空 |
| 迁移后遗忘全部记录，Prune 物理删除，再重跑 | 不导入 |
| 迁移之前新命名空间已被写入 | 报告冲突，不导入也不清理 |
| `ClearLegacy` | 只清这个命名空间的四类数据与任务键；旧索引同步清空；其他命名空间不动 |
| G0 时期的任务（没有 Scope）在迁移后重新登记 | 一律丢弃 |
| 迁移后手动设置了 Scope 与 `TurnSeq` 的旧任务 | `TurnSeq` 小于迁移时的 `LastForgetSeq` 时被丢弃 |

### 7.5 回归

- G0 的全部测试保持不变并通过。
- 新增 G0 日志字节的恢复测试：意图字节必须一致（4.5）。日志文件由 `a4f09197` 的代码生成后作为测试数据提交，不在 G1 分支中手工构造。
- `scripts/memeval` 编译通过；小样本冒烟测试中开启 `TrackAccess: true`，与改造前对比召回结果。

### 7.6 pgstore 适配器的一致性测试方案

本机有 Docker 与本地镜像 `postgres:16`，不需要联网。启动方式与现有 pgstore 测试一致：

```bash
docker run -d --rm --name goagent-pg -e POSTGRES_PASSWORD=test -p 55432:5432 postgres:16
export GOAGENT_PGSTORE_DSN='postgres://postgres:test@127.0.0.1:55432/postgres?sslmode=disable'
```

**测试放在哪里**：goagent 不能依赖 goagent-contrib，所以由 goagent 导出一个与存储无关的套件，由 pgstore 的测试调用。这与 `core/durable/storetest` 的做法相同，pgstore 已经用它做存储契约测试。

- `memory/memorytest.Run(t, open func(t) durable.Store)`，只通过公开 API 与一个通用的 `hookStore` 包装器操作。
- 公开的写操作在 G1b 提供，所以套件随 G1b 交付。
- G1a 的原语在 FileStore 上测试。pgstore 在 G1a 只跑已有的 `storetest`，它覆盖 Save、SaveAll 原子性、Load 最新、作用域墓碑与租约。

| 层 | 场景 | 断言 |
|---|---|---|
| 存储契约 | 已有的 `storetest.Run`（Scoped、Leases） | 已在 pgstore 测试中运行；G1 不放宽 |
| 大快照 | 1k、5k、10k 条记录的 state，两组数据（7.3） | 原样往返；bytea 无截断；记录快照大小与 TOAST 后的表大小 |
| 跨实例缓存 | 两个独立连接池的 Memory，模拟两个进程，交替读写 | 另一实例的提交立即可见；加锁后按 `CommitID` 核对，没有丢失更新（对应 FileStore 的 `TestStateCacheAcrossInstances`） |
| 租约接管 | 很短的 `LeaseTTL`：持锁者暂停，超时后被接管，接管方先提交 | 原持锁者的 `Save` 返回 `ErrLeaseLost`，`mutate` 整体重做，两次写入都在 |
| 删除时不等待 | `RemoveScope` 与持锁中的 `mutate` 并发（pgstore 的 `RemoveScope` 不等待持锁者） | `Save` 返回 `ErrScopeRemoved`，转为作用域改变；新作用域没有数据 |
| 结果未知 | COMMIT 之前断开：hook 在 `Save` 时用旁路连接执行 `pg_terminate_backend`。COMMIT 之后、响应之前断开：存储钩子做不到，用标准库 `net` 写一个故障注入的 TCP 代理，转发 COMMIT 后切断连接 | 已提交时按 `CommitID` 判为成功；未提交时返回 `ErrMemoryCommitUnknown`；缓存不含未确认的 state |
| 进程崩溃 | 沿用 pgstore 测试的子进程模式（`GOAGENT_PGSTORE_CHILD`）：持锁的子进程被杀 | 租约到期后其他实例可以写入；state 是最后一次成功的提交 |
| 只读副本 | 不测试：要求 DSN 指向主库，写入 README | `Load` 不能来自滞后的副本（存储契约） |
| 性能 | 7.3 的同一基准，阈值为 5k 条记录时 `mutate` p95 ≤ 200ms，读取阈值与 FileStore 相同 | 另外记录：每次提交的 WAL 增量（`pg_current_wal_lsn` 之差）、锁持有时间、读路径的传输时间。缓存只省解码不省 Load，pgstore 上每次读取都要传输整份快照（5k 条记录约 5 MiB），读取阈值可能在这里失守；若如此，版本头的放置按存储能力区分（pgstore 上 `SaveAll` 是一个事务，可以单独存小版本头），由测量决定 |

**通过标准**：套件全部通过，性能达到阈值。在本机执行前，pgstore 的结论只能写"未验证"。

### 7.7 pgstore 实测结果（2026-11-11）

**环境**：

- 独立容器 `goagent-g1a-pg`，PostgreSQL 16.15，默认配置（`synchronous_commit=on`，`shared_buffers=128MB`），只监听 `127.0.0.1:55432`。
- 数据目录放在 NVMe 上。Docker 的数据根 `/mnt/data` 是机械盘，存储契约测试在其上也通过了，耗时 112s，NVMe 上为 39s。
- 测试套件通过临时 `go.work` 引入 pgstore，以 `pgbench` 构建标签运行，结束后移除，未提交。
- 完整召回用现有的 `Snapshot`（钉住、文本打分、图谱 PageRank、社区、render）与 `Hydrate`，数据来自 `read` 得到的 state。按 G1 设计，召回读一次、`Hydrate` 再读一次。不含向量召回（需要嵌入模型）。

**正确性**（`-race`，连续 3 次全部通过）：

| 场景 | 结果 |
|---|---|
| pgstore 存储契约（`storetest`、租约、子进程崩溃、作用域墓碑） | 通过 |
| 跨实例缓存（两个独立连接池） | 通过：提交立即可见；加锁后核对，没有丢失更新 |
| 并发：2 个实例 × 3 个写者，加 4 个读者 | 通过：无丢失更新，读者 `Seq` 单调，缓存与完整解码逐字节等价 |
| 租约接管（删除租约行，接管方先提交） | 通过：原写入方收到 `ErrLeaseLost`，重做一次，两次写入都在 |
| Purge 语义（持锁期间切换代际并删除旧作用域） | 通过：迟到的写入被拒绝；两个实例（含持有旧缓存的）都读不到旧代际 |
| 结果未知：TCP 代理在 COMMIT 之前切断 | 通过：返回 `ErrMemoryCommitUnknown`，缓存不含未确认的 state |
| 结果未知：TCP 代理在 COMMIT 之后切断 | 通过：按 `CommitID` 判为成功 |

代理按 PostgreSQL 协议消息精确匹配 COMMIT（`'Q'`、长度 11、`commit\0`）。快照数据含有 `"commit_id"`，只匹配子串会误触发。

**性能**（默认 TOAST；28 核与 4 核各两次运行的范围；单位 ms，吞吐为次/秒）：

| 数据 | 记录数 | mutate p95 | 串行吞吐 | 热读 p95 | 冷读 p95 | 跨实例读 p95 | 完整召回 p95 | 跨实例完整召回 p95 | 其中召回计算 p95 | WAL/提交 | 快照表大小 |
|---|---|---|---|---|---|---|---|---|---|---|---|
| 真实中文 | 1k | 34～39 | 31.7～33.3 | 3.0～3.7 | 4.9～7.5 | 5.0～8.0 | 11.8～15.7 | 17.4～19.0 | 7.5～9.8 | 1.1 MiB | 174 MiB |
| 真实中文 | 3k | 70～102 | 13.4～15.5 | 6.9～7.8 | 12.2～15.9 | 13.2～18.1 | 35.7～44.1 | 41.4～47.9 | 21.9～25.1 | 3.1 MiB | 469 MiB |
| 真实中文 | 5k | 109～130（另一批中有一次 4 核 242） | **8.4～9.8** | 8.7～12.3 | 19.1～22.8 | 17.8～24.7 | **56.4～60.0** | **63.0～69.2** | 35.9～36.8 | 5.1 MiB | 763 MiB |
| 低压缩率 | 1k | 41～43 | 26.6～28.2 | 3.2～3.9 | 5.7～7.4 | 5.6～7.6 | 13.1～18.4 | 17.9～20.9 | 8.1～10.3 | 1.5 MiB | 223 MiB |
| 低压缩率 | 3k | 87～216 | 10.6～12.4 | 6.9～8.9 | 14.5～15.8 | 13.1～20.7 | 38.0～44.6 | 44.2～49.4 | 22.9～25.0 | 4.2 MiB | 621 MiB |
| 低压缩率 | 5k | 142～180 | **6.8～8.0** | 11.7～13.2 | 21.6～29.6 | 18.2～25.7 | **61.7～66.6** | **70.6～78.5** | 39.8～41.4 | 6.9 MiB | 1017 MiB |

**5k 条记录时的结论**：

| 门槛 | 结果 |
|---|---|
| 读取（热、冷、跨实例）≤ 30ms | 通过；低压缩率在 4 核上的冷读接近上限 |
| `mutate` p95 ≤ 200ms | 基本通过，但不稳定：出现过一次 242ms |
| 串行吞吐 ≥ 20/s | 失败 |
| 完整召回 ≤ 30ms（热、跨实例） | 失败 |
| 1k 条记录 | 全部达标 |
| 3k 条记录 | 吞吐与完整召回仍然失败 |

**`mutate` 的时间分解**（5k；计时包装器按调用累计）：

| 数据 | 加锁 | 读 meta ×2 | 读 state | Save | 解锁 | CPU |
|---|---|---|---|---|---|---|
| 真实中文 | 3.5～5.1 | 0.1 | 9.3～10.2 | **73～83** | 3.7～4.5 | 11.5～18.6 |
| 低压缩率 | 3.4～7.7 | 0.1～0.2 | 11.8 | **94～109** | 3.8～7.5 | 10.8～17.2 |

**发现**：

1. **Save 的大头是 TOAST 的 pglz 压缩**。PostgreSQL 对已经 gzip 加 base64 的 state 再压缩一遍，代价高、收益小。诊断时只对测试 schema 改列设置，5k 时：
   - `SET STORAGE EXTERNAL`：Save 降到 28～46ms，吞吐为 12.7～17.9/s。
   - `SET COMPRESSION lz4`：Save 降到 34～39ms，吞吐为 14.7～15.3/s。
   - 两者都仍低于 20/s。提案见 [PGSTORE_SNAPSHOT_STORAGE.md](PGSTORE_SNAPSHOT_STORAGE.md)。
2. **写放大**：
   - 每次提交的 WAL 约等于整份 state（5～7 MiB）。
   - 每次 `mutate` 是三个同步提交的事务（加锁、Save、解锁），各 3～5ms。
   - 约 150 次提交后，快照表膨胀到 0.76～1 GiB（死元组来不及回收）。
   - "整份重写"在 pgstore 上的代价比 FileStore 更大，冷热分离只能省约 30%，不足以解决。
3. **完整召回的瓶颈在召回算法，与存储无关**。5k 时召回计算本身就要 34～43ms。CPU 剖析：
   - 文本打分 29ms，其中 `bigrams` 每次查询为每条记录重建字符对集合，占 CPU 的 51%。
   - `Related` 加 PageRank 占 5ms。
   - 两次读取（各 7～20ms）在预算之内。

原始结果与测试套件在会话的 scratchpad 中（`pg-bench2.txt`、`pgharness/`），未进入仓库。

### 7.8 G1a 门槛：增量提交、bigram 索引与 zstd 之后（2026-11-12）

**口径**：

- 阈值不变（7.3）。
- 召回门槛按用户 2026-11-11 的确认，改为**端到端召回** p95 ≤ 30ms（设计目标 ≤ 20ms）。计时从调用 `recallState` 开始，到排好序的结果可以返回为止，含存储读取、查候选、打分与排序。
- pgstore 另有两项暂定目标，口径见 [增量提交 第 10 节](MEMORY_G1_INCREMENTAL.md)：
  - WAL/提交 ≤ 512 KiB。
  - 表大小 ≤ 活跃数据的 3 倍。按用户 2026-11-12 的确认改为**稳态口径**：持续写入并让 autovacuum 运行之后测量；目标速率为单命名空间 ≤ 1 次提交/秒，更高速率只报告。
- 冷召回（进程启动或缓存淘汰后的第一次召回：读取、解码基础快照、构建索引）单独报告，不作为门槛（用户 2026-11-12 确认）。
- **测试机**：i7-14700KF。物理核 0（逻辑 CPU 0、1）与核 8（逻辑 CPU 4、5）有硬件故障，见本节末尾。全部测量都用 `taskset -c 2,3,6-27` 排除这两个核。下文的"28 核"指可用的 24 个逻辑 CPU（`GOMAXPROCS=24`），"4 核"指 `taskset -c 6-9`。

**FileStore**（NVMe，5k，两组数据；28 核 / 4 核）：

| 数据 | mutate p95 | 串行吞吐 | 冷读 p95 | 跨实例读 p95 | 召回（热）p95 | 召回（跨实例）p95 | 冷召回 p95 | 索引构建 | 结论 |
|---|---|---|---|---|---|---|---|---|---|
| 真实中文 | 20.3 / 21.0ms | 54.4 / 54.1/s | 8.6 / 13.2ms | 2.7 / 6.3ms | 0.4 / 1.6ms | 2.2 / 2.4ms | 16.9 / 30.0ms | 5.2 / 9.5ms | 全部通过 |
| 低压缩率 | 20.4 / 20.9ms | 53.7 / 54.1/s | 12.2 / 15.2ms | 1.8 / 5.1ms | 0.0 / 0.0ms | 3.4 / 4.4ms | 22.1 / 31.5ms | 8.9 / 13.0ms | 门槛全部通过；冷召回在 4 核上为 30～32ms |

- 1k、3k 全部通过。10k 的读写仍在阈值之内（mutate p95 ≤ 25ms，冷读 p95 16～33ms）。
- 压缩：5k 时 600 次提交触发 1 次压缩，耗时 30～32ms。
- 淘汰：缓存只容得下一份 state 时，3 个 5k 的命名空间轮流读取，读取 p95 为 8.7ms（28 核）、20.4ms（4 核），占用不超过容量。
- **10k 压力测试**（20 秒）：通过。新增的都在、删除的不再出现、`Seq` 正确、读者单调、不变量成立。
  - 负载：提交 292 次，读取 4,611 次；写 p95 100ms，读 p95 51ms。
  - 偏高的原因：这个测试的缓存容量按第 4 版的估计设置，小于现在一份条目的估计（含索引），所以每次都完整解码。它是最坏情况，只用于验证正确性。

**pgstore**（PostgreSQL 16.15，NVMe，5k；每个规模先串行提交 1,000 次；28 核 / 4 核各一轮）：

| 指标 | 门槛或目标 | 真实中文 | 低压缩率 | 结论 |
|---|---|---|---|---|
| mutate p95 | ≤ 200ms | 15.8～16.2ms | 17.5～18.7ms | 通过 |
| 串行吞吐 | ≥ 20/s | 77.8～79.6/s | 70.5～74.3/s | 通过 |
| 热读 p95 | ≤ 30ms | ≤ 0.4ms | ≤ 0.4ms | 通过 |
| 冷读 p95：28 核 | ≤ 30ms | 16.7ms | 24.5ms | 通过 |
| 冷读 p95：4 核 | ≤ 30ms | 24.1ms | 25.2ms | 通过（gzip 时低压缩率为 30.5～35.2ms） |
| 跨实例读 p95 | ≤ 30ms | 3.2～5.0ms | 3.5～4.6ms | 通过 |
| 召回（热）p95 | ≤ 30ms | 0.8～1.4ms | 0.2～0.3ms | 通过 |
| 召回（跨实例）p95 | ≤ 30ms | 6.8～7.2ms | 4.2～4.7ms | 通过 |
| 冷召回 p95 | 单独报告 | 22.4 / 32.8ms | 27.2 / 40.2ms | 4 核超出 30ms |
| WAL/提交 | ≤ 512 KiB（暂定） | 92 KiB | 145～149 KiB | 通过；整份重写时为 5～7 MiB |
| 表大小（稳态，1 次提交/秒） | ≤ 活跃数据的 3 倍（暂定） | 见下方稳态表 | 2.5 倍 | 通过；更高速率只报告 |

- 1k、3k 的门槛全部通过。
- 每次提交的时间分解（5k，28 核）：head 加锁加解锁 5.6ms，读 meta 0.1ms，读 head 0.2ms，写 head 4.1～4.5ms，压缩分摊 0.1～0.2ms，CPU 2.7～2.8ms。
- lz4 与 pglz 在增量提交之后没有可测量的差别：每次提交只写 head，基础快照很少写入。因此 `SnapshotCompressionLZ4` 保持为可选调优，现在不建议执行。
- 正确性：pgstore 上的参考模型、跨实例缓存、并发、租约接管、Purge 语义、结果未知（COMMIT 之前与之后各一次）全部通过（`-race`，3 次）。memory 包 `-race` 8 次通过。

**冷读的优化（4 核、低压缩率，原超出 0.5～5ms）**：

- 分解（gzip，5k，4 核）：取字节 7.6～14ms，pgstore 的 JSON 解析 4～5ms，解码 13～14ms。CPU profile 中 gzip 解压（`compress/flate`）约占一半，gob 约占 15%。
- 改用 zstd（3.4）之后：解码 9.2～10.1ms（28 核 3.7～5.1ms），冷读 p95 由 30.5～35.2ms 降到 24.1～25.2ms。存储接口不变。

**表大小（稳态口径）**：

- 测量方法：
  - 对 5k 的 state 以固定速率串行提交 5 分钟，默认 autovacuum，每 15 秒采样一次。
  - 稳态倍数取后半段的最大值，即 `pg_total_relation_size` 除以活跃数据。
- 结果（zstd）：

| 数据 | 提交速率（单命名空间） | 稳态倍数 | 结束时 head 覆盖层 |
|---|---|---|---|
| 低压缩率 | 20/s | 33.6 倍 | 248 KiB |
| 低压缩率 | 5/s | 10.8 倍 | 61 KiB |
| 低压缩率 | 1/s | **2.5 倍** | 168 KiB |
| 真实中文 | 20/s | 34.2 倍 | 98 KiB |

- gzip 时的结果相同：低压缩率 0.5/s 为 2.1 倍，2/s 为 4.9 倍，68.9/s（不限速）为 167.6 倍。
- 分析：
  - 每次提交改写一次 head 行，旧版本成为死元组，进入 TOAST。死元组的量约为 head 大小乘以两次 autovacuum 之间的提交数。
  - `autovacuum_naptime` 默认 1 分钟，而且是全局参数，不能按表设置。所以倍数与单命名空间的提交速率近似成正比。
  - 3 倍只在约 ≤ 1 次提交/秒时达标。1/s 一组只跑了 5 分钟，覆盖层还没有长到上限，长期平均会略高。
  - 表级调优（`autovacuum_vacuum_cost_delay = 0`，堆与 TOAST 都设）没有效果：20/s 时为 33.3 倍。因此不提供调优常量。
- 决策（用户 2026-11-12）：按部署速率确定目标。一个命名空间通常每轮对话才提交一次，远低于 1 次/秒，所以目标为单命名空间 ≤ 1 次提交/秒时 ≤ 3 倍。更高速率的倍数只报告；若将来需要支撑，再考虑覆盖层改为追加写入的分段行，不再每次提交整份重写 head。

**冷召回**：等于冷读加构建索引（4 核 9～13ms）。4 核时 FileStore 为 30.0～31.5ms，pgstore 为 32.8～40.2ms。端到端召回的门槛按热与跨实例两种情形判定，都通过。

**测试机的硬件故障（2026-11-12 发现）**：

- 现象：
  - zstd 解压在并行压测中偶发 "CRC check failed"。
  - `-race` 报告过同一 goroutine 与自己"竞态"，这在逻辑上不可能。
- 定位（独立模块，不依赖本仓库代码；用 `taskset` 把进程绑到单个核上）：
  - 物理核 0（逻辑 CPU 0、1）约 91% 的解压失败；物理核 8（逻辑 CPU 4、5）偶发；其余核 0 次。
  - 同样的负载，用 `-tags noasm`（纯 Go 解码器）或 flate 解码时，失败 0 次。
  - Go 1.26.7、1.27.1、1.27.3 都能复现。
- 结论：这是 CPU 硬件故障，与 zstd 或 Go 版本无关。这颗 Raptor Lake 处理器的个别核在 zstd 汇编解码器的指令序列下算错。
- 处理：
  - 本节的全部测量都排除了这两个核。
  - 在这台机器上，偶发的解压失败或数据损坏都应先换核复现，再怀疑代码。

**其他发现与修正**：

- 参考模型在 pgstore 上发现，压缩写基础快照槽位时没有持该 ID 的锁，违反存储契约（FileStore 不校验）。已修正：加锁顺序为 compact → base/N → head。测试用的 `hookStore` 现在也强制这条契约。
- 第 4 版发现的一次疑似竞态检测误报，在 8 次完整的 `-race` 运行（memory 包）与 15 次单独运行中都没有再出现。仍记为观察项。

### 7.9 G1b：记录写路径与完整召回（2026-11-12）

**交付**（计划见 `.claude/plan-g1b.md`）：

- 记录与图谱的读写全部经 `mutate`/`read`。`RecordStore`、`Graph` 接口与 `NewGraph` 已删除；`Config.Graph` 改为 bool；`Config.Durable` 必填，`Config.Store` 只给任务用（G1e 删除）。
- 新增公开 API：`Update(id, expect, Patch)`、`Deletions` 与 `DeletionLog`、`CacheStats`。
- `Remember`、`Forget`、`Rewound`、`Prune`、Worker 的派生写入（`derive`）都是一次 `mutate`；召回只读且只读一次，`Hydrate` 每个命名空间再读一次。
- `TrackAccess`：作用域内的 `memory/access`，按 (命名空间, 作用域) 合并后由一个后台协程写入，失败忽略。
- 删除日志：`memory/deletions/NK(ns)`，先写日志再遗忘。
- 并发未命中按 (命名空间, 作用域, CommitID) 只解码一次（singleflight），等待者各自响应取消。

**与前文的差异**：

- `mutate` 在 fn 没有任何修改（或返回 `errNoChange`）时不提交，`Seq` 只计实际提交。例如重复遗忘、删除不存在的键都不提交。
- `Purge` 是过渡实现：一次 shrinking `mutate` 清空全部表并标记擦除，随后强制压缩，返回时已物理擦除；作用域切换（4.2）在 G1c 实现。删除日志记一条 Purge。
- `memorytest.Run` 的签名与 `storetest` 一致，为 `Run(t, newPair func(t) (a, b durable.Store))`：同一后端上的两个实例模拟两个进程。它只经公开 API 覆盖跨实例读写、重复 ID、修订号冲突、并发写入、交替写入（含压缩）、遗忘、清空。租约接管、结果未知、崩溃等存储层场景仍由 memory 包内的 `hookStore` 测试与 pgstore 的 G1a 测试覆盖（7.8）。
- 租约测试改为强制顺序：接管者用独立的钩子，本方重做时等接管者提交完再加锁，断言接管者 `Seq` 为 1、本方为 2。

**完整召回**（7.3 测量局限第 2 条）：一轮对话的 `Snapshot`（钉住、召回、图谱 PageRank，`Graph` 开启）加 `Hydrate` 的再读。FileStore，NVMe，5k，阈值不变：

| 数据 | 核数 | mutate p95 | 串行吞吐 | 冷读 p95 | 跨实例读 p95 | 端到端召回 热 / 跨实例 p95 | 完整召回 热 / 跨实例 p95 | 冷召回 p95 |
|---|---|---|---|---|---|---|---|---|
| 真实中文 | 28 | 21.8ms | 51.9/s | 10.0ms | 3.0ms | 0.7 / 4.5ms | 10.6 / 15.7ms | 16.2ms |
| 低压缩率 | 28 | 22.4ms | 52.8/s | 12.4ms | 3.8ms | 0.0 / 3.7ms | 8.0 / 11.2ms | 24.6ms |
| 真实中文 | 4 | 22.3ms | 52.3/s | 14.0ms | 6.4ms | 0.9 / 5.2ms | 11.4 / 16.4ms | 31.5ms |
| 低压缩率 | 4 | 21.3ms | 53.2/s | 16.3ms | 6.9ms | 0.0 / 3.1ms | 7.1 / 12.2ms | 38.2ms |

- 门槛全部通过（基准对完整召回也按 30ms 断言）。冷召回照旧单独报告，4 核上超过 30ms。
- 完整召回比端到端召回多 7～11ms，主要是 `relatedOf` 对全部关系排序、扫描全部实体找种子（5k 时 1 万条关系）。规模上去时它先于读取成为瓶颈；需要时可以把实体名与关系邻接表放进与 bigram 索引相同的派生缓存。
- 10k 压力测试（15 秒）通过：提交 298 次，读取 5,261 次，写 p95 61.9ms，读 p95 32.1ms，不变量成立。
- 基准数据的失效记录补上 `InvalidReason`（不变量 2）。

**pgstore**：`memorytest.Run` 由 goagent-contrib 的 `durable/pgstore/memory_test.go` 调用。contrib 依赖已发布的 goagent，所以用临时 `go.work` 指向本地 goagent 验证：套件与 pgstore 全部测试通过（`-race`）。这个测试文件要等 goagent 发布含 `memory/memorytest` 的版本、contrib 升级依赖之后才能在 contrib 单独编译。

## 8. 兼容性

| 变更 | 影响 | 处理 |
|---|---|---|
| `Config.Store`、`Config.Graph`（接口）、`RecordStore`、`NewRecordStore`、`Graph`、`NewGraph` 删除；新增 `Config.Durable`（`json:"-"`，必须实现 `Scoped`）、`Config.Graph bool`、`TrackAccess`、`MaxStateBytes` 与 `StateCacheBytes`（均为 `json:"-"`，不进入 `Identity`）、`RollbackWindow`、`ReindexEvery`、`RetireGrace`；新增 `Maintain`、`RunMaintenance`、`ErrCommitUnknown` | 自定义存储与图谱实现编译失败；本仓库内的 memeval、`codec/tags_test.go` 需要修改 | 一并修改；CHANGELOG 写明迁移步骤 |
| `Remember` 只创建；新增 `Update` | 依赖覆盖写入的调用方得到 `CodeConflict` | 改用 `Update` |
| `Index` 增加 `IDs`、`Clear`；键带版本；实体向量移入 `Index` 的独立子命名空间 | 自定义 `Index` 编译失败；旧向量不再被采用 | 实现新方法；升级后运行 `Reindex(true)` |
| `Entity.Vector`、`Entity.EmbedModel` 删除 | 相关代码编译失败 | 改为查询 `Index` |
| `TrackAccess` 默认关闭 | 新近性改为从创建时间起算，影响召回排序与 `MaxRecords` 淘汰 | 需要旧行为时开启；memeval 冒烟对比 |
| `Job` 增加字段；`Memory.Job` 按命名空间查找；完成的任务只保留键 | 调用方必须绑定命名空间 | — |
| `TurnMemory`、`Ref` 增加字段；协议改为 `/2` | G0 时期仍在进行的持久运行可以恢复，召回为空，意图不变 | 无需处理 |
| G1 日志不能回滚到 G0 恢复 | 回滚前必须让持久运行结束 | 写入回滚步骤 |
| Worker 改为持锁互斥 | 多实例部署时同一时刻只有一个 Worker 工作 | 无需处理 |
| 维护与 Worker 分离 | 不运行 Worker 的部署必须运行 `RunMaintenance` | 写入 README 与迁移步骤 |
| `Index.Clear` 只清精确的命名空间 | 自定义 `Index` 必须遵守 | 写入接口注释 |
| 命名空间组件不得包含 NUL，不应以 `#` 开头 | 包含 NUL 的被拒绝 | 改用其他字符 |

## 9. 实施拆分

每一步单独提交，提交前运行 `/verify`，并给出测试结果、diff 与剩余风险：

1. **G1a 原语与基准**：
   - `state`、`meta`、`retiring`、`mutate`、`read`、`ErrMemoryCommitUnknown`、`checkInvariants`。
   - state 编码与版本头（3.4）、进程内缓存（3.5）。
   - 测试替身：`hookStore`。它合并了原计划的 `crashStore` 与 `fakeStore`：用调用前回调模拟崩溃与错误，用 `afterSave` 模拟"已写入、调用方看到失败"。
   - `Config.Durable`、`MaxStateBytes`（解压后，只拒绝变大）、`StateCacheBytes`。
   - 原型基准与 10k 压力测试（7.3），不达阈值即停。
2. **G1b 记录写路径**：`Remember`、`Update`、`Forget`、`Rewound`、`Prune`；召回只读且只读一次；`TrackAccess`；删除日志。已完成（2026-11-12），见 7.9。
3. **G1c 作用域与 Purge**：meta 切换、全局 retiring 与清理；轮次绑定增加 `Scope`、`Seq`；`Ref` 带版本；`Digest` 分版本；G0 日志字节恢复测试。
4. **G1d 索引**：版本化键、写后复核、`IDs`、`Clear`、`Reindex`；实体向量移入 `Index`。
5. **G1e 任务、维护与 Worker**：state 内的任务、queue 分片（任务与索引待清理）、`Maintain` 与 `RunMaintenance`、Worker 锁与心跳、`LastForgetSeq` 丢弃规则。
6. **G1f 迁移与收尾**：`MigrateFromStore`（两阶段）、memeval 适配、CHANGELOG。

## 10. 风险

| 风险 | 等级 | 应对 |
|---|---|---|
| 整份 state 重写的开销超出预期 | 高 | 第 4 版之后读取与延迟已达标。5k 条记录时串行写吞吐仍在 20 次/秒上下（7.3），需要用户决策 |
| 缓存占用内存 | 中 | 按估计的堆占用计容量（3.5），默认 64 MiB，可关闭；基准实测堆占用为解压后字节数的 1.8～2.3 倍 |
| 并行编解码依赖 CPU 核数 | 中 | 7.3 同时报告 28 核与 4 核（`GOMAXPROCS=4`）的结果 |
| 过渡期 `Entity.Vector` 仍在 state 中 | 中 | G1d 才移入 `Index`；在此之前，配置了 Embedder 时 state 会因向量而显著变大，G1b、G1c 期间不应在生产启用 |
| gob 格式不可读 | 低 | G1f 提供解码工具；格式改变时升级快照版本号 |
| pgstore 上整份重写的写放大 | 高 | 7.7 实测：5k 时 8～10 次/秒，每次提交的 WAL 为 5～7 MiB，表膨胀到 1 GiB；以增量提交解决（第 12 节） |
| 完整召回超出预算 | 高 | 7.7 实测：5k 时 56～79ms，召回计算本身 34～43ms；以 bigram 索引解决（第 12 节） |
| G1 期间 Worker 一个任务多次提交，重做时可能保留内容已变化的候选，或在"已创建、未失效"之间留下两条同时有效的记录 | 中 | 已知限制，计数；G2 改为一次提交 |
| `LastForgetSeq` 粒度粗：同一命名空间中任何一次 Forget 都会丢弃所有在途任务，包括调用了 `memory_forget` 的那一轮 | 中 | 治理文档 5.3 已列为有意的保守做法；增加丢弃计数 |
| `TrackAccess` 默认关闭会改变召回排序与淘汰顺序 | 中 | memeval 冒烟对比 |
| 方案 A 的数据回滚依赖删除日志 | 中 | 需要用户确认；默认关闭时文档写明后果 |
| 全局 queue 的竞争 | 低 | 已分为 16 片，并在加锁前先检查登记 |
| `DoneJobs` 过期之后，用同一执行键回放会重新登记任务 | 低 | 回放已完成轮次的窗口远短于 30 天；轮次被回退或遗忘时，`LastForgetSeq` 与 G2 的回退标记会挡住任务 |
| `memory_save` 在"提交后、记录日志前"崩溃时，可能产生重复记录 | 低 | 后续可以用持久执行键派生记录 ID，不在 G1 范围内 |

## 11. 第 1 版评审意见的处理

| 编号 | 意见 | 第 2 版的处理 |
|---|---|---|
| B1 | 把 `ErrLeaseLost` 当作结果未知，并用 Seq 判断是否提交，会让 Forget 悄悄丢失 | `ErrLeaseLost` 按确定失败处理并整体重做；结果未知时按 `CommitID` 判断（4.1）；7.1 增加用例 |
| B2 | 以"state 非空"判定迁移幂等，会在 Purge 或遗忘后让旧数据复活，也会漏掉清理 | 迁移标记放在 meta 中，`Purge` 不清除 meta；已迁移的命名空间仍可以执行清理；迁移前必须停止流量，遇到冲突报告而不导入（5.1、5.2） |
| B3 | `Ref` 增加字段后，G0 日志恢复时意图会变化 | `Digest` 按协议分版本，新字段带 `omitzero`；用 G0 日志字节做验收测试；明确 G1 日志不能回滚到 G0 恢复（4.5、6） |
| B4 | 新命名空间的 meta 初始化有竞态 | 初始作用域固定为 `0`，只有 Purge 写 meta（4.1） |
| I1 | 清理依赖命名空间再次被访问 | 改为全局 `retiring`，与 meta 用 `SaveAll` 原子提交，Worker 遍历它（4.3） |
| I2 | 迟到的向量与 Forget 之后残留的向量 | 所有写索引的地方都在写后复核；`RetireGrace` 之后再清一次；定期 `Reindex(false)`；新增不变量 7（4.4） |
| I3 | 已删除作用域的登记无法移除；`ErrScopeRemoved` 未定义 | 非当前作用域的登记不取 state 锁直接移除；`ErrScopeRemoved` 统一转为 `ErrScopeChanged`（4.1、4.3） |
| I4 | 全局 queue 的竞争 | 分为 16 片，加锁前先检查登记（4.3） |
| I5 | 迁移与 Purge 之后的 `LastForgetSeq`；G0 遗忘的映射 | 迁移时设 `LastForgetSeq = Seq`；没有 Scope 的旧绑定只在 `Purges == 0` 时才使用；`SupersededBy` 为空的失效记录映射为 forgotten（4.5、5.1） |
| I6 | 读不出旧向量、旧索引不清、任务需要逐键删除、`Clear` 语义未约定 | 一律重新嵌入；`MigrationSource` 增加旧索引；任务逐键删除；文档写明对 `Clear` 的要求（5.1） |
| I7 | 性能阈值起不到把关作用 | 基准前移到 G1a，覆盖 pgstore、锁持有时间、吞吐、WAL 与真实规模；召回只读一次；增加 `MaxStateBytes`（3.2、7.3） |
| I8 | 回滚会让被物理删除或 Purge 的内容复活 | 增加删除日志 `RollbackWindow`；未开启时写明后果（4.6、6） |
| I9 | 多实例时 Worker 没有互斥 | 增加 `memory/worker` 锁（4.3） |
| 建议 1–9 | 实体向量单独存放；`LastForgetSeq` 粒度写入风险；Enqueue 描述统一并返回 nil；Rewound 只恢复被 rewound 替代的记录；`TrackAccess` 影响淘汰；重做的确定性；`DoneJobs` 过期；pgstore 盘点更正；`memory_save` 幂等 | 分别见 2、10、4.3、4.2、4.2、10、10、1.3、10 |

### 第 2 版复审意见的处理

| 编号 | 意见 | 第 3 版的处理 |
|---|---|---|
| N1（阻断） | 迁移用一次 `SaveAll` 同时写 meta 与 state，按契约做不到 | 改为两阶段提交：先写 `Migrating`，再合并写 state（带 `MigrationID`），最后写 `Migrated`；规定重跑时的判断规则；锁顺序表加入 meta → state（5.1、4.1） |
| N2 | `MaxStateBytes` 会拒绝删除，也会让轮次失败 | 只拒绝变大并超限的提交；删除与收缩类操作永远放行；Enqueue 超限时丢弃并返回 nil（3.2） |
| N3 | 维护工作挂在需要 Model 的 Worker 上 | 新增 `Maintain`、`RunMaintenance`，不依赖模型；没有 Worker 的部署必须运行它（4.3、8） |
| N4 | 定期 Reindex 只覆盖有任务登记的命名空间 | Forget 登记"索引待清理"，由 `Maintain` 处理（4.2、4.3） |
| N5 | 删除日志与 Forget 不原子，4.2 与 4.6 矛盾 | Forget 先写日志再提交；Purge 的日志并入同一次 `SaveAll`；规定日志的加锁与清理方式（4.2、4.6） |
| N6 | Worker 锁需要防护 | 每个任务之后保存一次心跳，收到 `ErrLeaseLost` 即结束本轮（4.3） |
| N7 | 结果未知时不能断定失败 | 返回 `ErrCommitUnknown`，并说明各调用方的重试语义（4.1） |
| N8 | 清理期间不应持有锁 | `RemoveScope`、`Index.Clear` 期间不持有任何锁（4.1、4.3） |
| N9 | "只有 Purge 写 meta"不成立 | 改为"Purge 与迁移"（4.1） |
| N10 | `Index.Clear` 的语义未约定 | 只清精确的命名空间，写进接口注释并用测试验证（4.4、8） |
| N11 | 没有 Scope 的旧任务仍可能被处理 | 一律丢弃（4.5） |
| N12 | `RetireGrace` 的取值依据 | 必须大于 Index 写入的超时与重试时间之和（4.3） |
| I3 论据 | "已登记就跳过加锁"的论据不完整 | 已按复审意见改写（4.3） |

### 第 3 版确认意见的处理

确认结论：通过，可以从 G1a 开工。N1、N2、N3、N5 已解决，加锁路径不会成环。以下各项已写入文档：

| 编号 | 意见 | 处理 |
|---|---|---|
| N4 | `Maintain` 先 Reindex 后移除登记，会漏掉期间新提交的 Forget 留下的残留 | 改为先在 state 锁内移除登记，再执行 Reindex（4.3） |
| N1 | 重跑时 `Migrating` 的判断必须先于"已有数据即报冲突" | 写明判断顺序（5.1） |
| 补充 | 迁移阶段一之后崩溃、违规恢复服务并执行 Purge，阶段二会把旧数据导入 Purge 后的作用域 | `Migrating` 非空时 `Purge` 拒绝执行（4.2） |

### 第 4 版：G1a 性能修订

用户决策：采用方案 1（版本头 + CommitID + 压缩 + 有界 LRU）与方案 3（较低的默认上限），保持原有阈值，不放宽。实施中的实测推翻了第 3 版之后给出的几项预估：

| 预估 | 实测 | 处理 |
|---|---|---|
| gzip 能把 JSON 外层解析从 18ms 降到 0.4ms | 那组数据是高度重复的合成数据（28 倍压缩）。真实中文加 UUID 只有 1.7 倍，低压缩率数据 1.4 倍；串行 gzip 的压缩加解压成本超过它的节省 | 保留 gzip，但目的改为减少写入的字节，并按 16 块并行；解码提速改由 gob 承担（3.4） |
| 版本头作为单独的快照，与 state 用 `SaveAll` 一起写 | FileStore 上 `SaveAll` 107ms（事务日志写两遍）；单独一次小 `Save` 17ms | 版本头放进 state 快照，作为数组的第一个元素；只解析版本头就能核对 `CommitID`（3.4、3.6） |
| 冷读约 40ms，可能需要区分冷热 SLA | 5k 条记录时冷读 p95 在 28 核上 13～19ms，4 核上约 21ms | 不区分冷热：冷读、热读、跨实例读都按同一个 30ms 阈值判定 |
| 基准数据的命名空间很短 | 真实命名空间是两个较长的 ID，每个对象重复一份 | 基准改用真实长度的命名空间；编码时省去对象的 `Namespace`（3.4） |
| `MaxStateBytes` 按编码后的字节数 | 压缩后的字节数随数据变化 | 改为按解压后的字节数（3.2） |

### 第 4 版独立评审意见的处理

评审结论：有条件通过，78 分。性能门槛另行决策；结论"G1a 门槛未通过、需要决策"成立。

| 编号 | 意见 | 处理 |
|---|---|---|
| B1（阻断） | 写入方放进缓存的 state 与完整解码的结果不等价（`Namespace`、空切片），召回输出因是否命中缓存而不同；编解码还会悄悄改正属于其他命名空间的对象 | 提交前 `normalize` 为解码后的形式，对象属于其他命名空间时拒绝提交；新增测试 `TestStateCacheEquivalentToFullDecode`，断言 JSON 逐字节相同（3.4） |
| I1 | 缓存中的对象引用了调用方传入的 ns 切片 | `read`、`mutate` 入口复制 ns；新增测试（3.5） |
| I2 | 共享只读只是约定 | 已写明 G1b 的强制手段（3.5） |
| I3 | 块数、`raw_bytes`、版本头标量未校验；读路径没有限长 | 版本头记录各块长度，逐块限长并核对；标量在第一块中另存一份并核对；新增 4 个篡改用例（3.4） |
| I4 | 提交与复查没有时间上限 | `MemoryCommitTimeout` 30 秒，超时按结果未知处理；新增测试，验证调用方取消不传到提交、上限生效（4.1） |
| I5 | 读取达标只覆盖 FileStore 与页缓存已热的情况，测的是 `read` 原语而不是一轮完整召回 | 写入 7.3 的"测量局限"；pgstore 的读路径传输列入 7.6 |
| I6 | 语料偏小，样本少，命中率是构造出来的，4 核缺完整结果 | 写入测量局限；补上 4 核的完整结果表（7.3） |
| I7 | 瓶颈与磁盘强相关 | 7.3 单列"小 Save 基线"，并说明它在带掉电保护的 SSD 上不到 1ms |
| I8 | 压力测试没有注入故障 | 记为 G1e 前补齐：注入租约丢失、结果未知、Purge；断言名称已更正 |
| 建议 1 | 数据块框架不容忍空白 | 已改，并有测试 |
| 建议 2～3 | `heapFactor` 的说法不一致；压力测试的注释与代码不符 | 已更正 |
| 建议 4 | 默认上限 8 MiB 与方案 3 不符 | 取决于 7.3 的规模决策，决策后修改 |
| 建议 5 | 重做次数的表述 | 已更正为"共尝试 3 次"，并写明 fn 必须是纯函数（4.1） |
| 建议 6 | 租约测试没有强制顺序 | 记为 G1b 修正 |
| 建议 7 | 过渡期实体向量仍在 state 中 | 写入第 10 节 |
| 建议 8 | 并发未命中时重复解码；需要指标 | 记为 G1b：按 (ns, CommitID) 合并解码，暴露命中、未命中、淘汰与解码耗时 |
| 建议 9 | gob 的演进规则 | 已补写（3.4） |
| 建议 10 | "COMMIT 之后"的断连做不到 | 7.6 改用 TCP 代理注入 |
| 遗漏的方向 | 冷热分离；先测 pgstore 再决策 | 已加入 7.3 的可选方向 |

## 12. 架构修订（第 5 版，待评审）

**用户决策**（2026-11-11，基于 7.7）：

- 写路径采用**增量提交**；不降低 5k 的支持容量，不做冷热分离。
- 召回采用**预计算的 bigram 索引**。
- pgstore 快照列的存储方式**单独评估**。
- 三项都先设计、评审，获得明确批准后才实施。G1a 的改动保持未提交，不启动 G2。

| 提案 | 文档 | 要点 |
|---|---|---|
| 增量提交与恢复协议 | [MEMORY_G1_INCREMENTAL.md](MEMORY_G1_INCREMENTAL.md) | head 为唯一提交点，内含按键合并的覆盖层；两个基础快照槽位轮换；后台压缩只在切换 head 时短暂持锁；遗忘在 `EraseWithin`（默认 5 分钟）内物理擦除；幂等键；确定性还原 |
| bigram 索引 | [MEMORY_G1_BIGRAM_INDEX.md](MEMORY_G1_BIGRAM_INDEX.md) | 打包的字符对键加倒排表；基础快照索引随解码构建，与之同缓存；覆盖层索引加遮蔽集；结果逐项等价（2,000 个查询的等价测试） |
| 快照列存储方式 | [PGSTORE_SNAPSHOT_STORAGE.md](PGSTORE_SNAPSHOT_STORAGE.md) | 建议 lz4；不升级 `SchemaVersion`（严格相等的校验会破坏滚动升级），作为可选的调优语句交付；只改元数据，可回滚 |

**性能预算与验收标准**：阈值不变，见增量提交提案第 8 节。新增四项：

1. 完整召回（而不只是读取）p95 ≤ 30ms。这是阈值的原意，测量口径按评审 I5 收紧。
2. 每次提交的 WAL ≤ 512 KiB。
3. 表大小 ≤ 活跃数据的 3 倍。
4. 擦除时限。

所有预算都是估计，以实测为准。

**建议的实施顺序**（每步单独评审、单独验收，阈值不变）：

1. **增量提交**：替换 G1a 的单快照格式与 `mutate` 的修改方式。
   - 先做正确性：参考模型测试，故障注入覆盖增量提交提案 4.4 的每一行。
   - 再测 FileStore 与 pgstore 的写吞吐和读取。
   - 它是另外两项的前提：bigram 索引依赖"基础快照稳定"，pgstore 存储方式的收益也要在新的写入比例下重测。
2. **bigram 索引**：先交付等价测试与"召回计算"的基准（state 预先读好，与存储无关），再接入基础快照与覆盖层；最后测完整召回。
3. **pgstore 存储方式**：在 goagent-contrib 中实施，与第 2 步可以并行；用增量提交之后的负载重测，再决定 lz4 还是 EXTERNAL。
4. **重新跑 G1a 的门槛**：两种存储、两组数据、两种核数、1k/3k/5k。全部通过之后，才进入 G1b。

**主要风险**：

| 风险 | 来源 |
|---|---|
| 增量提交的复杂度：三类快照、两把锁、压缩状态机 | 增量提交提案第 9 节 |
| 遗忘在基础快照中最多残留 `EraseWithin` | 增量提交提案第 6 节；需要与治理文档对齐 |
| 修改函数改用 `tx` API，影响 G1b 的写法 | 同时解决评审 I2 |
| bigram 索引的内存与"结果逐项相同"的约束 | bigram 索引提案第 5～7 节 |
| 只用存储方式的调优无法达到吞吐 | 已由 7.7 证实，所以它不是替代方案 |

