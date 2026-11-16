# 长期记忆分布式后端设计（第一期：共享缓存与共享向量索引）

日期：2026-11-16
状态：待用户审批

## 1. 意图

长期记忆已经能在多实例下共享记录、状态与抽取任务（`durable/pgstore` + 提交时 Basis 复核 + `memory/worker` 租约锁）。但两块仍只有进程内实现，导致多实例部署时：

- **向量索引**（`memory.Index`）只有 `NewMemoryIndex()`：每个实例只知道自己写入的向量，其他实例写入的记录在召回时缺向量分，语义召回变差。
- **按标签删除的缓存**（`cache.Tagged`）只有 `SimpleCache`：`Forget`/`Rewound`/`Purge` 触发的 `OnErase` 只清掉本进程的缓存条目；其他实例的旧条目虽因键中的记忆版本变化不再命中，却留在内存直到 TTL，不满足 `EraseRequired` 的物理删除承诺。

本期在 goagent-contrib 提供这两块的共享实现：Redis 版 `cache.Tagged`、pgvector 版 `memory.Index`。**默认仍为本地**：不配置时一切照旧（FileStore、`NewMemoryIndex`、`SimpleCache`），分布式能力是宿主显式选择的可选模块。

## 2. 目标、非目标、反目标

**目标**
- G1：contrib 新模块 `cache/redis`，实现 `cache.Cache` 与 `cache.Tagged`，满足"只多删不漏删"，在 Redis 单机、哨兵、Cluster 下都成立。
- G2：contrib 新模块 `memory/pgvector`，实现 `memory.Index`，行为与 `MemoryIndex` 一致（见 4.3）。
- G3：goagent 增加两套后端无关的一致性测试：`cache/cachetest`（Tagged 契约）、`memory/memorytest` 增加 Index 契约；`SimpleCache`、`MemoryIndex` 与两个新模块都跑同一套。
- G4：用 Docker 启动真实 Redis（含 Cluster）与 pgvector 运行集成测试，脚本与 `scripts/test-pgstore.sh` 同构。

**非目标**
- Worker 横向扩展（按命名空间分片加锁）：改锁设计，第二期单独设计。
- Redis 作为 `durable.Store` 或会话日志后端。
- 对向量检索做 ANN 近似索引（HNSW/IVFFlat）：见 4.3 的规模说明。
- goagent 主库引入任何外部依赖。

**反目标**（技术上能跑也算失败）
- 任何情况下出现漏删：`DeleteTag` 返回后，此前开始的 `SetTagged` 写入的、带该标签的条目被读到。
- Redis 的淘汰、过期、主从切换、Cluster 迁槽导致已删除内容"复活"。
- 不配置分布式模块的宿主行为或依赖发生变化。

## 3. 约束

- contrib 每个子目录是独立 Go 模块，主模块不承担其依赖；新模块各有 `go.mod`。
- 新模块依赖的 goagent API（`cache.Tagged`、`memory.Index` 现状、新增的一致性测试包）目前只在 goagent 本地 master 上，未发布。开发期用 contrib 的临时工作区（`GOAGENT_CORE_ROOT=../goagent bash scripts/test-all.sh`）联调；合入 contrib 前须先发布 goagent 新版本并更新版本锁定——这是对外操作，由用户决定时机。
- 外部服务测试在未设置连接变量时跳过（与 pgstore 的 `GOAGENT_PGSTORE_DSN` 一致）；CI 不连真实 Redis Cluster。
- contrib `durable/pgstore/memory_test.go` 是用户未提交的文件，不碰；contrib 当前分支 `feat/pgstore-snapshot-lz4` 不碰，新工作从 contrib master 开分支（或 worktree）。
- 测试一律 `taskset -c 2,3,6-27`，`TMPDIR=/tmp`，禁止 `-mod=mod`。

## 4. 方案

### 4.1 Approaches（Redis 缓存）

| 方案 | 做法 | 取舍 |
|---|---|---|
| A：Lua 原子 | 标签用 Set 记成员，`SetTagged`/`DeleteTag` 各一个 Lua 脚本 | 最简单，删除即物理删除；但脚本内所有键须同槽，Cluster 下只能把全部缓存压进一个槽，放弃分片 |
| **B：标签代数 + 物理删除（用户选定）** | 每个标签有一个"代"，条目写入时记下各标签的代；`DeleteTag` 先换代（立即生效），再按成员集合物理删除 | 不依赖多键原子，单机/哨兵/Cluster 通用；读取多一次代的查询；实现约为 A 的两倍 |

用户选择 B，理由：不确定是否使用 Cluster，B 在所有拓扑下都成立。

### 4.2 Redis 缓存（`goagent-contrib/cache/redis`）

**客户端**：`github.com/redis/go-redis/v9`，接受 `redis.UniversalClient`（单机、哨兵、Cluster 同一接口）。

**键布局**（`P` 为配置的前缀，默认 `goagent:cache:`）：

| 键 | 内容 |
|---|---|
| `P e:{key}` | 条目：编码后的值 + 写入时各标签的代 |
| `P g:{tag}` | 标签的当前代：一个随机令牌（16 字节） |
| `P t:{tag}` | 标签的成员集合（Set），元素为缓存键 |

**代用随机令牌，不用递增计数**：令牌缺失视为"不可校验"，按未命中处理。这样代的键被淘汰（`maxmemory` 策略）、过期、主从切换丢失时，只会让该标签下的条目多删，不会让旧条目复活。递增计数在键丢失后从 0 重来，旧条目可能恰好匹配而复活，违反反目标。

**操作**：

- `SetTagged(key, value, ttl, tags...)`
  1. 对每个标签 `SET g:{tag} <随机> NX GET`（Redis ≥ 7，一步完成：已存在则返回现值，不存在则写入新令牌）得到当前令牌；
  2. `SET e:{key} <编码(值, {tag: 令牌})> PX ttl`；
  3. 每个标签 `SADD t:{tag} key`，并把 `t:{tag}` 与 `g:{tag}` 的过期延长到不短于 ttl（`PEXPIRE ... GT`；新建的 `g:{tag}` 在第 1 步即带 PX ttl，所以令牌键总有过期）；
  4. **复查**：再读一次各标签令牌，与第 1 步不同或缺失则 `DEL e:{key}` 并返回 nil。理由：换代若发生在复查之前，条目被自己删掉；若发生在复查之后，SADD 早于换代、也就早于 DeleteTag 的 SSCAN 开始，而 SSCAN 保证返回全程都在集合中的元素，该键必被物理删除。由此 `DeleteTag` 返回后不会残留带旧令牌的条目，物理删除不依赖重做。
  - 各步用 pipeline 发送；Cluster 下 go-redis 按节点拆分 pipeline，不要求同槽。
- `Get(key)`：`GET e:{key}`，解码出标签令牌；再一次 pipeline `GET g:{tag}` 逐个比较。任何一个不相等或缺失 → 返回未命中，并 `DEL e:{key}`（可能误删同一时刻写入的新值，属允许的多删）。无标签条目直接返回。
- `DeleteTag(tag)`
  1. `DEL g:{tag}`——令牌缺失即不可校验，此后所有带旧令牌的条目读取即未命中，这一步完成即满足契约；下一次 `SetTagged` 会生成新令牌。用 DEL 而不是写入新令牌，令牌键不会因为没有过期而无限累积；
  2. `SSCAN t:{tag}` 分批（默认每批 500）`DEL e:{key}`，再 `DEL t:{tag}`。与并发 `SetTagged` 的交错由其第 4 步复查兜住。
- `Set`/`Delete`/`Has`/`Clear`/`GetStats`：实现 `cache.Cache`。`Has` 与 `Get` 同样校验令牌。`Clear` 只删除前缀下的键（`SCAN` 分批），不得 `FLUSHDB`。

**并发正确性论证**（实现的测试须覆盖这些交错）。"之前开始"按线性一致理解：在 `DeleteTag` 开始前已完成的 `SetTagged` 必须被删；与之并发的可以排在它前或后。`SetTagged` 的线性化点是第 1 步读到令牌的那一刻：
- `SetTagged` 读到令牌 t → `DeleteTag` 换成 t' → `SetTagged` 写入带 t 的条目：读取比较 t ≠ t'，未命中。✅
- `DeleteTag` 换代后 `SetTagged` 才开始：读到 t'，条目有效，与这次删除无关（不是"之前开始的写入"）。✅
- 代的键丢失（淘汰、过期）：`SET NX GET` 生成新令牌，旧条目令牌不匹配，未命中。✅
- **前提：令牌的读写都到主节点。** 客户端开启从节点读（`ReadOnly`、`RouteByLatency`、`RouteRandomly`）时，读到滞后的旧令牌会让已删除条目可读，所以 `New` 对 go-redis 的 Cluster/Failover/Ring 客户端检查这些选项并拒绝。
- 主从切换、持久化重启使令牌回到旧值的情形见 4.2 末尾"复制与持久化"。

**值编码**：`cache.Tagged.SetTagged` 只收 `[]byte`；`cache.Cache.Set` 收 `interface{}`，本实现只接受 `[]byte` 与 `string`，其他类型返回 `CodeInvalidInput`（Redis 无法保存任意 Go 值；`builder.ModelCache` 只写 `[]byte`）。条目格式带版本前缀，便于以后演进。

**统计**：`GetStats` 的 Hits/Misses/Sets 为本进程计数（`atomic.Int64`），`Size` 返回前缀下条目数的近似值或 -1（godoc 写明，不做全量 SCAN）。

**配置**：`redis.Config{Client, Prefix, DeleteBatch}`；`New` 时 `PING` 校验连通，并校验服务端版本 ≥ 7。

**支持范围**：Redis ≥ 7 的单机、哨兵、原生 Cluster。云厂商的代理模式集群、KeyDB、Dragonfly 等兼容实现不在支持与测试范围内，文档写明。

**复制与持久化**（用户决定）：
- 配置 `WaitReplicas int`（默认 0）。大于 0 时，`DeleteTag` 第 1 步后对令牌所在主节点执行 `WAIT WaitReplicas WaitTimeout`（`WaitTimeout` 默认 1 秒），确认数不足返回错误，由 `memory` 的擦除重做在 `EraseWithin` 内重试。默认 0 是弱保证：主从切换或从持久化重启的窗口内，已删除条目可能重新可读；文档写明，有合规要求的部署须设置。
- 物理删除的标准是：`EraseWithin` 之后，**主节点内存中**不存在该条目（由 SetTagged 写后复查保证）。RDB/AOF 文件、备份与从节点上的残留由运维负责，部署文档给出建议（缩短备份保留、AOF 重写周期等）。

### 4.3 pgvector 索引（`goagent-contrib/memory/pgvector`）

**依赖**：pgx/v5（与 pgstore 相同）+ `github.com/pgvector/pgvector-go`。

**表**（`pgvector.Schema` 由宿主纳入迁移，`New` 校验 `SchemaVersion`，与 pgstore 约定一致）：

```sql
CREATE EXTENSION IF NOT EXISTS vector;
CREATE TABLE goagent_memory_vectors (
  ns     text   NOT NULL,   -- 命名空间按 memory 的 spaceKey 规则编码为单个字符串
  id     text   NOT NULL,
  dim    int    NOT NULL,
  vec    vector NOT NULL,   -- 不固定维度：不同嵌入模型维度不同
  PRIMARY KEY (ns, id)
);
```

**行为须与 `MemoryIndex` 一致**（由 4.4 的一致性测试锁定；pgvector 存 float32、`MemoryIndex` 存 float64，分数在 1e-6 内一致即可，一致性测试不构造分差小于 1e-5 的近似并列）：
- 写入前归一化，零向量返回错误；同一命名空间内维度不一致返回 `CodeInvalidInput`。
- `Search` 只查精确命名空间（不含子命名空间），按余弦相似度降序、分数相同按 ID 升序，取前 k；`k <= 0` 返回空。Score = `1 - (vec <=> q)`。
- `IDs`、`Clear`、`Delete` 只作用于精确命名空间；`Clear` 不递归。

**规模说明**：命名空间内精确扫描（主键前缀过滤 + 逐行距离），不建 ANN 索引。长期记忆按租户/用户分命名空间，单命名空间记录数受 `Retention.MaxRecords` 约束（通常数千以内），精确扫描足够，且排序与 `MemoryIndex` 一致（分数误差内）；不固定维度也使 HNSW 无法使用。文档写明规模上限与以后改 ANN 的路径。

**与 pgstore 的关系**：独立模块，可与 pgstore 共用同一个 `pgxpool.Pool`；不放进 pgstore，避免把 `vector` 扩展变成 pgstore 的前置条件。

### 4.4 goagent 侧改动（小）

- `cache.Tagged` 契约文字放宽：由"必须原子或线性一致……须用事务或脚本"改为"必须线性一致：可用原子操作，或用代/版本校验使旧写入在读取时失效"，并引用 Redis 实现作为第二种做法的例子。
- 新包 `cache/cachetest`：`RunTagged(t, newCache func(t) interface{cache.Cache; cache.Tagged})`，覆盖：幂等删除、标签替换、TTL、并发 `SetTagged`×`DeleteTag` 不漏删（确定性 + 压力两类）、`Clear` 只清自身。`SimpleCache` 测试改为调用它。
- `memory/memorytest` 新增 `RunIndex(t, newIndex func(t) memory.Index)`：覆盖 4.3 列出的全部行为。`MemoryIndex` 测试改为调用它。
- 文档：`CACHING_GUIDE.md`、`memory/README.md` 增加"分布式部署"小节，说明默认本地、如何换成 Redis 与 pgvector、Redis 的淘汰策略无需特殊配置（因令牌设计）、Cluster 支持。

### 4.5 测试与脚本（contrib）

- `scripts/test-redis.sh`：Docker 启动 `redis:7` 单机容器跑 `cache/redis` 全部测试；带 `--cluster` 时用 `redis:7` 起 6 节点（3 主 3 从）Cluster 再跑一遍。结束删除容器。连接变量 `GOAGENT_REDIS_ADDR`（Cluster 为逗号分隔地址）；未设置时跳过。
- `scripts/test-pgvector.sh`：Docker 启动 `pgvector/pgvector:pg17` 跑 `memory/pgvector` 测试；连接变量 `GOAGENT_PGVECTOR_DSN`。
- 两个模块都调用 4.4 的一致性测试；另加 Redis 特有测试：代的键被删除后旧条目不复活、Cluster 下跨槽 pipeline 正常、`Clear` 不影响前缀外的键。
- 端到端：一个测试用两个 `builder.ModelCache`（模拟两个进程，共享同一 Redis）+ 共享 pgstore 记忆，进程甲 `Forget`，断言进程乙的缓存条目被物理删除、不再命中。需要同时连 Redis 与 Postgres，放在单独的仅测试模块 `integration/distributedmemory`（不发布），使 `cache/redis` 的使用者不必引入 pgx 与 pgstore；任一连接变量缺失即跳过。
- SetTagged 第 4 步复查须有确定性交错测试：在第 3、4 步之间注入 `DeleteTag`（测试钩子），断言条目被删且读不到。
- contrib README 模块表、测试小节补两行。

## 5. 留给实现者的决定

- 条目编码的具体字节格式、令牌长度（≥16 字节随机）。
- `Size` 返回近似值还是 -1。
- Cluster 测试容器的具体编排方式（单容器多端口或 `redis-cli --cluster create`）。
- 命名空间编码复用 memory 的哪种规则（须与 `MemoryIndex` 隔离语义一致，含 `alice` 与 `alice2` 前缀相同的情形）。

## 6. 风险

- **读取多一次往返**：每次 `Get` 多一次 pipeline 读代。模型调用缓存的命中路径节省的是秒级模型调用，可接受；在 README 写明。
- **物理删除依赖重做**：`DeleteTag` 第 2 步与并发写入交错可能留下带旧令牌的条目，靠读取时删除与 `EraseWithin` 内的重做清除；这些条目始终不可读。
- **版本发布顺序**：contrib 新模块依赖未发布的 goagent；发布 goagent 是对外操作，需用户确认。
- **pgvector 规模**：单命名空间超过数万条时精确扫描变慢；文档给出上限与 ANN 迁移路径。

## 7. 用户的决定

- Redis 方案选 B（标签代数 + 物理删除），以支持 Cluster。
- 主从切换回滚：可配置等待从节点确认（`WaitReplicas`，默认 0），不配置时为文档写明的弱保证。
- 物理删除标准：`EraseWithin` 之后主节点内存中不存在；持久化文件与备份由运维负责。
- 默认部署仍为本地，分布式模块显式选用；测试用 Docker 启动真实 Redis。

## 8. 设计者的决定（可推翻）

- Redis 方案 B 中，代用随机令牌、`DeleteTag` 以 `DEL g:{tag}` 换代、`SetTagged` 写后复查（来自动工检查）。
- 最低 Redis 7；代理模式集群与兼容实现不支持；客户端开从节点读时 `New` 拒绝。
- 向量索引用 pgvector、独立模块、精确扫描、不建 ANN；分数误差 1e-6 内视为一致。
- 端到端测试放在不发布的独立集成模块。
- Worker 横向扩展放第二期。
