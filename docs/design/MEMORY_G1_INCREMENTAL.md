# 长期记忆 G1：增量提交与恢复协议（提案）

日期：2026-11-11。状态：**已实施（未提交）**，用户 2026-11-11 批准按提议顺序实施；与提案的差异见第 10 节，验收结果见 [G1 7.8](MEMORY_G1_STORAGE.md)。上级文档：[G1 存储改造](MEMORY_G1_STORAGE.md)。

## 1. 背景与目标

G1a 的"整份重写"模型在两种存储上都达不到写吞吐阈值。原因是每次提交写入的字节等于整份 state。

| 存储 | 5k 条记录的串行吞吐 | 每次提交的写入 | 依据 |
|---|---|---|---|
| FileStore | 15.7～22.5/s | 约 5～6 MiB | [G1 7.3](MEMORY_G1_STORAGE.md) |
| pgstore | 6.8～10/s | 约 5～6 MiB，另有等量 WAL，表膨胀到 0.76～1 GiB | [G1 7.7](MEMORY_G1_STORAGE.md) |

**目标**：每次提交写入的字节与本次修改量成正比，而不是与 state 大小成正比。同时满足：

1. 5k 条记录时的全部阈值不变（第 8 节）。
2. 保持 G1 已有的保证：
   - 锁内修改、加锁后核对。
   - 作用域代际（Purge）。
   - 结果未知按 CommitID 判定。
   - 跨实例缓存只省解码。
   - G0 的轮次绑定与恢复语义。
3. 删除优先：被遗忘的内容在有界时间内从存储中物理消失（第 6 节）。
4. 只使用现有 `durable.Store` 与 `durable.Scoped` 接口：
   - 没有"列出 ID"与"删除 ID"，只能整体删除作用域。
   - 不修改存储契约。

**非目标**：G2 的派生关系与级联重算；冷热分离（用户已否决）。

## 2. 存储布局（每个作用域）

| ID | 内容 | 写入时机 |
|---|---|---|
| `memory/head` | **提交点**。版本头、标量、基础快照指针、覆盖层（按键合并的变更） | 每次提交，一次 `Save` |
| `memory/base/0`、`memory/base/1` | 两个基础快照槽位，格式沿用 G1a 的数据块编码（gob → zstd → base64，16 块并行；第 6 版由 gzip 改为 zstd，`memory-state-blob/2`） | 只在压缩时写入非活动槽位 |
| `memory/compact` | 只用于加锁：同一作用域同一时刻只有一个压缩者 | — |

- state 的锁改为 `memory/head`，含义不变：持锁才能提交。
- meta、retiring 等根作用域的快照不变。
- G1a 的 `memory/state` 单快照格式没有发布过，没有存量数据，直接替换，不需要迁移。

### 2.1 head 的格式

`Data` 是 JSON 对象，小字段在前，覆盖层在最后：

```text
{
  "seq": 1234,
  "commit_id": "uuid",
  "next_incarnation": …, "last_forget_seq": …, "migration_id": …,
  "base": {"slot": 0, "seq": 1170, "commit_id": "uuid", "raw_bytes": …, "chunk_bytes": [...]},
  "logical_bytes": …,            // 整个 state 的逻辑大小，MaxStateBytes 按它判断（第 7 节）
  "overlay_bytes": …,            // 覆盖层解压后的字节数，解码时限长
  "erase_pending_since": "…",    // 覆盖层中有删除、且基础快照里仍有旧内容时的最早时间；为空表示没有待擦除的内容
  "ops": …,                      // 幂等键，见 5.3
  "overlay": "gob → zstd → base64"
}
```

- **覆盖层**是自基础快照以来的变更，按 (类型, 键) 合并：
  - 同一个键只保留最后一个值或删除标记（墓碑），每项带最后修改它的 `seq`。
  - 合并而不是逐笔追加，有三个原因：
    - head 的大小只与"被改动过的不同键"有关，有上界（第 4 节）。
    - 遗忘时直接把旧值替换为墓碑，被遗忘的内容在本次提交就从 head 消失（第 6 节）。
    - 还原 state 只需"基础快照 + 一次覆盖"。
- **基础快照**自带 `seq` 与 `commit_id`。head 记录所引用快照的这两个值，读到槽位后核对。

## 3. 读写协议

### 3.1 materialize：还原当前 state

```text
materialize(head):
  base ← 缓存中作用域相同、base.commit_id 相同的已解码基础快照；否则 Load(memory/base/<slot>) 并解码
  若 base 的 commit_id ≠ head.base.commit_id → 槽位已被后来的压缩改写，从 Load(head) 重来（最多 3 次，否则返回 CodeConflict）
  state ← base 的副本（复制映射）⊕ head.overlay（墓碑即删除）；标量取 head
  缓存 (作用域, head.commit_id) → state，以及 (作用域, base.commit_id) → base
```

- 结果只由（基础快照, head）两个不可变输入决定，是**确定性的**：任何实例、任何时刻还原同一个 head，得到逐字节相同的 state。
- G1a 的 `normalize` 保证缓存路径与完整解码路径等价，这一点保留。
- 解码时校验：
  - 覆盖层限长并核对 `overlay_bytes`。
  - 基础快照沿用 G1a 的块数、块长与标量交叉核对。
  - 覆盖层中每项的 `seq` 都在 (base.seq, head.seq] 之内。

### 3.2 read（不加锁）

```text
Load(meta) → Load(head) →（head.commit_id 命中缓存则直接返回）→ materialize → Load(meta)，两次作用域相同才采用
```

- 稳态下，读取只传输 head（覆盖层有上界，见第 4 节），不再传输整份 state。
- 跨实例时，只要对方没有压缩，基础快照的缓存就仍然有效，只需重新应用覆盖层。
- 这同时消除了 pgstore 上"每次读取传输 5 MiB"的问题（G1 7.7）。

### 3.3 mutate（持 `memory/head` 锁）

```text
1. Load(meta)；expectScope 检查；Lock(memory/head)；再 Load(meta) 核对作用域（同 G1a）
2. Load(head)；materialize（缓存命中时不解码，即"加锁后核对 CommitID"）
3. 若提供了幂等键且已在 head.ops 中 → 不修改，返回当前 state（5.3）
4. tx := 只读视图 + 变更集；fn(tx)。fn 只能经 tx.Put*/tx.Delete*/tx.Set* 修改，变更集即本次的增量
5. 新 head = 旧 head 的覆盖层 ⊕ 变更集（按键合并，带 seq）；seq+1；新 commit_id；
   更新 logical_bytes（第 7 节）与 erase_pending_since（第 6 节）；normalize；检查 MaxStateBytes
6. 覆盖层超过硬上限 → 在锁内同步压缩（4.3），否则 Save(head)
7. Save 的错误分类与 G1a 相同：
   - ErrLeaseLost：整体重做
   - ErrScopeRemoved：转为作用域改变
   - 其他：Load(head) 并比较 commit_id，判定成功或返回 ErrMemoryCommitUnknown；有时间上限
8. 成功后写入缓存：新 state 由旧 state ⊕ 变更集得到，与完整还原等价（测试强制）
```

- **变更集 API**：修改函数不再直接改映射，而是通过 `tx` 的方法修改。G1a 评审 I2 指出"值内切片共享"只是约定，这样把它变成结构上的保证：
  - 读取得到的是值的拷贝。
  - 写入是整体替换。
  - 修改函数看不到也改不了缓存中的对象。
- **提交只有一次 `Save(head)`**：
  - FileStore 上是一次小文件替换，两次 fsync。
  - pgstore 上是一个事务。
  - 不使用 `SaveAll`，FileStore 的 `SaveAll` 很昂贵（G1 3.6）。

## 4. 压缩

### 4.1 触发条件（满足其一）

| 条件 | 默认值 | 理由 |
|---|---|---|
| 覆盖层解压后的字节数 | ≥ 256 KiB，或 ≥ 基础快照 `raw_bytes` 的 5% | head 每次提交都整份重写，覆盖层决定每次提交的开销与 WAL 增量 |
| 覆盖层的项数 | ≥ 2,000 | 限制还原时的应用开销 |
| `erase_pending_since` 早于 | `EraseWithin`（默认 5 分钟） | 删除承诺，见第 6 节 |
| 硬上限：覆盖层 ≥ 1 MiB | — | 压缩者失效时也不让 head 无限增长：由写入方在锁内同步压缩 |

### 4.2 后台压缩（常规路径，不阻塞写入方）

由 `Maintain` 或 Worker 执行，也可以由提交后发现达到阈值的写入方异步触发：

```text
1. Lock(memory/compact)（拿不到就跳过：已有压缩者）
2. Load(head) 得到 H0；materialize 得到 S0（seq = H0.seq）
3. 编码 S0，Save 到非活动槽位 1-H0.base.slot（新基础快照 seq = H0.seq，commit_id = 新 UUID）   ← 耗时的部分，不持 state 锁
4. Lock(memory/head)；Load(head) 得到 H1
   - 作用域已变、或 H1.base ≠ H0.base（不应发生）→ 放弃
   - 新 head = H1 的标量 + 指向新槽位 + H1.overlay 中 seq > H0.seq 的项（即压缩期间新提交的变更，原样保留）
   - erase_pending_since 按剩余覆盖层重新计算
   - Save(head)；解锁
5. 擦除旧槽位：把 H0.base.slot 写成空的占位快照（第 6 节）
6. 解锁 memory/compact
```

- 第 3 步写的是非活动槽位：
  - 持锁的写入方与读者都只引用活动槽位。
  - 仍持有更早 head 的读者若去读这个槽位，会因 `commit_id` 不符而重试（3.1）。
  - 压缩锁保证同一时刻只有一个压缩者，不会有两个压缩者交替改写两个槽位。
- 第 4 步只在锁内做"读 head、写 head"，持锁时间与一次普通提交相当。写入方不会被基础快照的写入阻塞。
- **锁顺序**：`memory/compact` → `memory/head`，与 G1 的锁顺序表无交叉（G1 4.1）。

### 4.3 同步压缩（硬上限，兜底）

写入方在持有 `memory/head` 的情况下：

1. 把当前 state 写入非活动槽位。
2. 写入指向新槽位、覆盖层为空的 head。

这一步只在后台压缩失效时发生，计数并告警。它的耗时计入 mutate 的 p99，不计入 p95 预算（第 8 节说明）。

### 4.4 故障与恢复

| 崩溃或失败点 | 结果 | 恢复 |
|---|---|---|
| 第 3 步之前或期间 | head 未变，非活动槽位可能是半成品（`Save` 原子，所以要么是旧值要么是新值） | 无需处理；下次压缩覆盖 |
| 第 3 步之后、第 4 步的 `Save(head)` 之前 | head 仍指向旧槽位，新槽位里是没被引用的完整快照 | 无需处理；下次压缩覆盖 |
| 第 4 步 `Save(head)` 结果未知 | — | `Load(head)`，按 `commit_id` 判定；未生效则按上一行处理 |
| 第 4 步成功、第 5 步之前 | 旧槽位仍保存着压缩前的基础快照（可能含已遗忘的内容） | `erase_pending_since` 不清空，改为"旧槽位待擦除"标记；下次 `Maintain` 先擦除旧槽位 |
| 进程在持有 `memory/compact` 时退出 | FileStore 的锁随进程释放；pgstore 的租约在 `LeaseTTL` 后过期 | 下一个压缩者继续 |
| 两次压缩之间读者读取的槽位被改写 | `base.commit_id` 不符 | 读者从 `Load(head)` 重来，最多 3 次。两次压缩之间至少隔数百次提交，3 次内仍冲突视为异常并报错 |

**确定性恢复**：任何时刻，当前 state = materialize(当前 head)。head 是唯一的提交点，基础快照被 head 引用时不可变。不存在需要重放的日志，也就没有"重放到一半"的状态。

## 5. 一致性、缓存与幂等

### 5.1 跨实例一致性与缓存失效

- 读者看到的永远是 `Load(head)` 时的最新提交，论证与 G1 3.6 第 2 条相同。缓存只在 `commit_id` 相同时代替还原，基础快照的缓存只在 `base.commit_id` 相同时代替解码。
- 缓存分两级：
  - 已解码的基础快照（大、少变）。
  - 已还原的 state（基础快照 ⊕ 覆盖层，随每次提交变化）。
  - 两者都按估计的堆占用计入 `StateCacheBytes`。还原出的 state 复制了基础快照的映射，它的占用按"映射本身 + 覆盖层"估计，不按整份重复计算。
- 失效时机：
  - head 的 `commit_id` 变化：还原的 state 失效，基础快照通常仍可复用。
  - 压缩：基础快照的缓存失效。
  - Purge：作用域变化，两级都失效。

### 5.2 Purge 与命名空间代际

不变。代际仍是作用域 ID：

- head、两个槽位与压缩锁都在作用域内，`RemoveScope` 一次全部删除。
- 压缩者在第 4 步加锁后核对作用域，Purge 期间的压缩被放弃。
- 写入旧作用域会得到 `ErrScopeRemoved`，转为作用域改变（G1 3.6 第 5 条）。

### 5.3 结果未知与幂等重试

- **判定**：同 G1a，`Load(head)` 比较 `commit_id`。head 很小，复查的成本可以忽略。
- **幂等键**：`mutate` 可以带一个幂等键 `opKey`，例如持久执行键加操作序号。
  - 提交时把它写入 head 的 `ops`（键 → seq）。
  - 重试时，键已存在就不再修改，直接返回当前 state。
  - `ops` 按条数（默认 1,000）与时间（默认 24 小时）裁剪，压缩时并入基础快照的同名字段。
  - 这样 `memory_save` 在"提交后、写日志前"崩溃再重试，不会产生重复记录。这是 G1 第 10 节列出的已知风险，此方案一并解决。
- 不带幂等键的操作沿用 G1 4.1 的约定：Update 依靠 `expect`，Forget 本身幂等，Remember 冲突即说明已提交。

## 6. 删除优先：物理擦除的时限

整份重写时，遗忘在一次提交后就从存储中消失。增量模型下，被遗忘的内容在三处可能残留，分别处理：

| 位置 | 处理 | 时限 |
|---|---|---|
| head 的覆盖层 | 覆盖层按键合并，遗忘时旧值被墓碑替换 | 本次提交 |
| 活动槽位（基础快照） | 置 `erase_pending_since`，触发压缩（4.1） | `EraseWithin`，默认 5 分钟 |
| 非活动槽位（更早的基础快照） | 压缩第 5 步写成空的占位快照 | 与上一行同一次压缩 |

- pgstore 的旧行版本、FileStore 被替换的旧文件，与整份重写模型下的处理相同：前者由 vacuum 回收，后者在 rename 时释放。
- `EraseWithin` 与治理文档的承诺对齐，要写进 `memory_forget` 的说明与运维文档。`RollbackWindow` 的删除日志不受影响。

## 7. MaxStateBytes 与逻辑大小

- 整份重写时按解压后的总字节数判断。增量模型下改为**逻辑大小** `logical_bytes`：每个对象单独 gob 编码的长度之和。
- 维护方式：
  - 压缩时精确计算。
  - 每次提交按变更集增减：放入新值加上它的长度、减去旧值的长度；删除减去旧值的长度。
  - 只需编码被改动的对象，开销与修改量成正比。
- 规则不变：
  - 只拒绝"让 state 变大并超过上限"的提交。
  - 删除与收缩类操作永远放行。
  - 默认 8 MiB，维持 5k 条记录的支持容量。

## 8. 性能预算与验收标准

**口径**：5k 条记录；两组数据（真实中文、低压缩率）；28 核与 4 核；FileStore（NVMe）与 pgstore（PostgreSQL 16，NVMe）。阈值沿用 G1 7.3，不放宽。

| 指标 | 阈值 | 本方案的预算（估计，待测） | 预算依据 |
|---|---|---|---|
| mutate p95：FileStore | ≤ 100ms | ≤ 40ms | Load(head) ≤ 2ms + 复制映射约 2ms + 修改与编码覆盖层约 3ms + Save(head) 约 18ms（17ms fsync） |
| mutate p95：pgstore | ≤ 200ms | ≤ 50ms | 加锁约 4ms + Load(head) 约 2ms + CPU 约 6ms + Save(head) 约 6ms + 解锁约 4ms（G1 7.7 的分解） |
| 单命名空间串行吞吐 | ≥ 20/s | ≥ 35/s | 由上两行推出 |
| 热读、跨实例读 p95 | ≤ 30ms | ≤ 6ms | 只读 head，复用基础快照的缓存 |
| 冷读 p95（无缓存） | ≤ 30ms | ≤ 25ms | 与 G1a 的冷读相同（读并解码基础快照），再加覆盖层 |
| 完整召回 p95（热、跨实例） | ≤ 30ms | ≤ 20ms | 两次读取各 ≤ 6ms，召回计算 ≤ 8ms（取决于 bigram 索引，见 [MEMORY_G1_BIGRAM_INDEX.md](MEMORY_G1_BIGRAM_INDEX.md)） |
| 压缩期间写入方的持锁时间 | — | ≤ 一次普通提交的 2 倍 | 4.2 第 4 步 |
| 擦除时限 | `EraseWithin` | 5 分钟 | 第 6 节 |
| 每次提交的 WAL（pgstore，含分摊的压缩） | 新增指标 | 平均 ≤ 512 KiB（现为 5～7 MiB） | head 平均约为覆盖层上限的一半，加上每 N 次提交 1 次基础快照的分摊 |
| 快照表大小（pgstore，稳态，默认 autovacuum，单命名空间 1 次提交/秒） | 新增指标 | ≤ 活跃数据的 3 倍 | 每次提交只更新 head 这一行；口径见第 10 节 |

**验收方法**：

- 沿用 `memory/state_bench_test.go` 与 G1 7.6 的 pgstore 测试套件。新增：
  - 覆盖层从 0 增长到上限的整个过程中的 p95。
  - 压缩与写入并发时的写入方 p95。
  - WAL 与表大小。
- 阈值逐项断言。
- **p99 单独报告**：同步压缩（4.3）只在后台压缩失效时出现，测试中关闭后台压缩，单独测量它的耗时。
- **正确性先于性能**，以下测试全部通过后才看性能：
  - 每个测试结束时运行 `checkInvariants`。
  - 新增不变量：
    - materialize 的结果与"从头重放全部变更集"的参考模型相同。
    - 缓存路径与完整还原逐字节等价。
  - 故障注入：
    - 4.4 的每一行。
    - 结果未知（TCP 代理在 COMMIT 前、后各断开一次）。
    - 租约接管。
    - Purge 与压缩、写入并发。
  - 模型测试：随机操作序列在 `-race` 下运行，比较 materialize 与参考模型。
  - 压力测试：10k 条记录；包含压缩、遗忘与擦除时限的检查。

## 9. 风险与取舍

| 风险或取舍 | 等级 | 应对 |
|---|---|---|
| 复杂度上升：三类快照、两把锁、压缩状态机 | 高 | 协议只有一个提交点（head）；模型测试与故障注入覆盖 4.4 的全部状态 |
| 遗忘的内容在基础快照中最多残留 `EraseWithin` | 中 | 时限可配置；`Maintain` 不运行时由写入方同步压缩兜底（4.1 硬上限）；写进治理文档 |
| 压缩被饿死（`Maintain` 不运行、写入持续） | 中 | 4.1 的硬上限由写入方同步压缩；计数并告警 |
| head 随覆盖层变大，每次提交的开销上升 | 中 | 阈值 256 KiB 或 5%；验收要求覆盖层在整个增长范围内都满足 p95 |
| 修改函数改为通过 `tx` 修改：G1b 的写法改变 | 中 | G1b 尚未开始，没有存量调用方；同时解决评审 I2 |
| 两次压缩之间读者重试 | 低 | 两次压缩之间至少隔数百次提交，重试上限 3 次 |
| 逻辑大小与 G1a 的"解压后字节数"不同 | 低 | 只影响 `MaxStateBytes` 的计量口径；默认值按逻辑大小重新标定，维持 5k 容量 |

## 10. 实施记录：与提案的差异（2026-11-11）

代码：

- `memory/state_head.go`：head、覆盖层、materialize、压缩与擦除。
- `memory/state_tx.go`：变更集与 `tx`。
- `memory/durable_state.go`：`mutate`、`read`。
- `memory/state_cache.go`：两级缓存。

| 项 | 提案 | 实施 | 原因 |
|---|---|---|---|
| 写基础快照槽位 | 只持 `memory/compact` 锁 | 另外持该槽位自己的锁，写完即释放；加锁顺序为 `memory/compact` → `memory/base/N` → `memory/head` | 存储契约要求 `Save` 时持有该 ID 的锁。pgstore 会校验，FileStore 不校验，参考模型测试在 pgstore 上发现了这一违规。测试用的 `hookStore` 现在也强制这条契约 |
| 硬上限时的同步压缩 | 写入方在持有 head 锁时压缩 | 写入方提交、释放 head 锁之后，同步执行一次完整压缩（等待压缩锁） | 写入方从不写槽位，槽位只由持压缩锁者写入，不需要为写入方另设锁顺序 |
| 旧槽位的擦除 | 压缩第 5 步 | head 增加 `stale_slot` 标记。压缩切换后写占位快照，再在 head 锁内清除标记；标记存在时，任何一次写入之后都会触发压缩，由压缩先完成擦除 | 第 5 步之前崩溃也能在下一次写入时补做 |
| 擦除标记 | `erase_pending_since` | 另外在覆盖层的每一项上记 `Erase`。删除基础快照中已有的键时自动置位；遗忘这类"保留记录、清空内容"的修改调用 `tx.<表>.MarkErasure(key)` | 压缩时据此重新计算剩余覆盖层是否仍需擦除 |
| 幂等键 | 1,000 条、24 小时，压缩时并入基础快照 | 保留在 head 中：256 条、24 小时，按序号淘汰最旧的 | head 每次提交都整份重写，条数直接影响提交开销；不写入基础快照，压缩逻辑更简单 |
| 修改接口 | `tx.Put*`/`Delete*`/`Set*` | 泛型的 `table[T]`：`Get`（返回拷贝，含切片）、`Keys`（按字典序）、`Len`、`Put`（整体替换并规范化）、`Delete`、`MarkErasure`；标量用 `Set*` | 一套实现覆盖六类对象；`Keys` 有序，修改函数的结果与映射遍历顺序无关 |
| 逻辑大小 | 每个对象单独 gob 编码的长度 | 按字段长度加固定开销计算（`kind.size`），压缩时精确重算，每次提交按变更增减 | 单个对象的 gob 编码会带上类型描述，不能代表它在 state 中的占用，而且慢 |

**新指标的口径**（用户 2026-11-11 要求写明）：

- **WAL/提交**：
  - 在独立的 PostgreSQL 实例上（无其他负载），对 5k 条记录的 state 连续做 1,000 次串行提交。
  - 计算方式：`(pg_current_wal_lsn() 之差) / 1,000`。
  - 期间产生的全部 WAL 都计入：加锁与解锁的事务、head 的写入、压缩（基础快照、head 切换、旧槽位擦除）、autovacuum 与检查点之后的整页写。
- **活跃数据**：快照表中当前可见行的 `data` 列逻辑长度之和，即 `SELECT sum(octet_length(data)) FROM goagent_durable_snapshot`，取解压后的长度。不含死元组、TOAST 的分块开销与索引。
- **表大小**：`pg_total_relation_size('goagent_durable_snapshot')`，含堆、TOAST、索引与 FSM/VM。
  - 稳态口径（用户 2026-11-12 确认）：以固定速率串行提交 5 分钟，默认 autovacuum，每 15 秒采样，取后半段的最大值。原口径是"1,000 次提交后立即测量"，但 autovacuum 还没有机会运行，测到的是瞬时值。
  - 倍数 = 表大小 / 活跃数据。
  - 倍数与单命名空间的提交速率近似成正比，所以报告时注明速率。结果见 [G1 7.8](MEMORY_G1_STORAGE.md)。
- 两项都是**暂定目标**：WAL/提交 ≤ 512 KiB，表大小 ≤ 活跃数据的 3 倍。

