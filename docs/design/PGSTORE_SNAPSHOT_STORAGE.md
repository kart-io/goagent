# pgstore：快照列的 TOAST 存储方式（迁移提案）

日期：2026-11-11。状态：**已实施为可选调优（未提交）**：goagent-contrib 新增 `SnapshotCompressionLZ4`、`SnapshotCompressionPGLZ` 两个常量、`TestSnapshotCompressionTuning` 与 README 一节；没有修改 `Schema`、`SchemaVersion` 与适配器代码。增量提交之后的测量见 [G1 7.8](MEMORY_G1_STORAGE.md)。对象：`goagent-contrib/durable/pgstore` 的 `goagent_durable_snapshot.data` 列（`bytea`）。

## 1. 问题与实测

- PostgreSQL 对超过约 2 KB 的列值做 TOAST，默认用 pglz 压缩（本机 `default_toast_compression = pglz`）。
- 长期记忆的 state 已经是 gzip 加 base64 的数据，pglz 再压缩时 CPU 开销大、收益小。
- 实测（G1 7.7，5k 条记录，28 核，PostgreSQL 16.15，NVMe，诊断只作用于测试 schema）：

| 列设置 | Save 耗时（真实中文） | Save 耗时（低压缩率） | 串行吞吐（真实中文 / 低压缩率） |
|---|---|---|---|
| 默认（EXTENDED + pglz） | 73～83ms | 94～109ms | 8.4～9.8/s / 6.8～8.0/s |
| `SET STORAGE EXTERNAL`（不压缩） | 28.5ms | 46.5ms | 17.9/s / 12.7/s |
| `SET COMPRESSION lz4` | 34.2ms | 38.7ms | 15.3/s / 14.7/s |

两种设置都能把 Save 耗时减半，但单靠它们仍达不到 20/s，根本原因在"整份重写"（见 [增量提交](MEMORY_G1_INCREMENTAL.md)）。增量提交实施后，单次提交只写 head（≤ 256 KiB），基础快照只在压缩时写入，本项的收益会转移到以下几处：

- 压缩的耗时。
- 冷读：解码基础快照前的 detoast。
- 较大的 head（覆盖层接近上限时）。

## 2. 方案比较

| 方案 | 对长期记忆 | 对同表的其他数据（Agent 持久运行的 JSON 快照，可压缩） | 前提 |
|---|---|---|---|
| A：`SET STORAGE EXTERNAL` | Save 最快（不压缩） | **失去压缩**，可压缩的 JSON 快照占用变大，WAL 也变大 | 无 |
| B：`SET COMPRESSION lz4` | 与 A 接近（低压缩率数据甚至更快） | 保留压缩，lz4 的 CPU 开销远低于 pglz | PostgreSQL ≥ 14，且服务端编译了 lz4（主流发行版与托管服务通常具备；用 `pg_column_compression()` 验证） |
| C：维持现状 | 慢 | 不变 | — |

**建议：B（lz4）**。它对长期记忆的收益与 A 相当，又不牺牲同表其他数据的压缩。最终取舍以第 5 节的测量为准，特别是 Agent 快照在 A 下的体积增量。

## 3. 表结构版本与兼容性

- 现状：`pgstore.New` 要求 `goagent_durable_schema.version == SchemaVersion`（当前为 1），严格相等。
- 列的存储方式是**物理参数**：
  - 不改变列类型、数据格式与读写语义。
  - 新旧二进制都能正确读写，已有的 pglz 值与新的 lz4 值可以在同一列中共存，detoast 透明。
- 如果把它做成 `SchemaVersion = 2`，滚动升级会出问题：
  - 迁移后旧二进制 `New` 失败（表结构版本为 2，需要 1）。
  - 升级前新二进制同样失败。
  - 回滚旧二进制前还必须先回滚表结构。
  - 在不改变逻辑结构的前提下，这是不必要的停机约束。

**建议：不升级 `SchemaVersion`**，把存储方式作为可选的调优项交付：

1. `schema.go` 新增常量 `SnapshotStorageTuning`，内容是一条 `ALTER TABLE goagent_durable_snapshot ALTER COLUMN data SET COMPRESSION lz4;`，与 `Schema` 一样由宿主纳入自己的迁移流程，`New` 不自动执行。
2. 新建表时，在 `Schema` 中直接带上这一列设置。已有的表执行一次调优语句即可。
3. README 写明：作用、前提、对已有数据的影响（只影响之后写入的值）、回滚方法。
4. 可选的诊断：新增 `pgstore.CheckStorage(ctx, pool)`，报告该列当前的 `attstorage` 与 `attcompression`。它不阻止启动。

如果评审仍要求版本化：

- 改为"兼容区间"校验：`MinSchemaVersion ≤ version ≤ SchemaVersion`。
- 先发布只放宽校验的版本，再执行迁移。
- 这需要两次发布，成本明显更高。

## 4. 迁移、回滚与运维

| 步骤 | 语句或动作 | 影响 |
|---|---|---|
| 迁移 | `ALTER TABLE goagent_durable_snapshot ALTER COLUMN data SET COMPRESSION lz4;`（方案 A 为 `SET STORAGE EXTERNAL`） | 只改元数据，不重写表；已有的值保持原状，直到下一次被改写 |
| 锁 | 获取表级锁的时间很短。确切的锁级别待在 PostgreSQL 14～17 上实测，结果写进 README | 执行时设置 `lock_timeout`（例如 2s），失败重试，避免排在长事务后面阻塞业务 |
| 回滚 | `SET COMPRESSION pglz`（或 `SET STORAGE EXTENDED`） | 同样只改元数据，之后写入的值恢复原方式；无数据迁移 |
| 观测 | 快照表与 TOAST 表的大小、每次提交的 WAL、Save 与 Load 的 p95 | 迁移前后各记录一次 |

## 5. 验证计划（实施前不视为已证明）

1. **长期记忆负载**：G1 7.7 的基准。
   - 矩阵：1k、3k、5k × 两组数据 × {pglz, lz4, EXTERNAL} × {28 核, 4 核}。
   - 记录：Save 与 Load 的 p50/p95、mutate 吞吐、WAL 与表大小。
   - 增量提交实施后再测一遍，那时 head 与基础快照的写入比例不同。
2. **其他数据的体积**：用代表性的 Agent 持久运行快照（JSON），比较三种设置下的 TOAST 表大小与 WAL。A 的体积增量是取舍的关键数据。
3. **兼容性**：
   - 同一张表中先写 pglz 值，迁移后写 lz4 值，再回滚。
   - 每个阶段新旧二进制交叉读写，结果一致。
   - 运行 pgstore 的现有测试套件（`storetest` 一致性、租约、子进程崩溃）。
4. **前提检查**：服务端不支持 lz4 时，`SET COMPRESSION lz4` 报错且表不变；文档给出改用方案 A 的方法。

**验收**：

- 兼容性与回滚的测试全部通过。
- 5k 时 Save 的 p95 相比默认设置至少降低 40%。
- 其他数据的体积增量（方案 A）或 CPU 开销（方案 B）可以接受，由评审根据第 2 项的数据确认。
