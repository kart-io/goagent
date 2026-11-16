# 长期记忆分布式后端设计（第一期：共享缓存与共享向量索引）

日期：2026-11-16
状态：待用户审批（已按 DBRE、架构、并发正确性三份审核修订）

## 1. 意图

长期记忆已经能在多实例下共享记录、状态与抽取任务（`durable/pgstore` + 提交时 Basis 复核 + `memory/worker` 租约锁）。但两块仍只有进程内实现，导致多实例部署时：

- **向量索引**（`memory.Index`）只有 `NewMemoryIndex()`：每个实例只知道自己写入的向量，其他实例写入的记录在召回时缺向量分，语义召回变差。重启后索引为空，要靠 Reindex 重建。
- **按标签删除的缓存**（`cache.Tagged`）只有 `SimpleCache`：各实例各算各的，同一次模型调用在 N 个实例上最多算 N 次；`Forget` 触发的 `OnErase` 只清本进程，其他实例的旧条目不再命中（键含记忆版本），但留在内存直到 TTL。`EraseRequired` 强制 `TTL ≤ EraseWithin`，所以物理删除承诺在本地缓存下也成立，代价是 TTL 被压短、命中率低；`EraseAuto` 允许 TTL 更长，此时只告警。

共享缓存的价值因此是：**跨实例命中**，以及**删除立即生效于所有实例**（不必为了擦除时限压短 TTL）。

本期在 goagent-contrib 提供这两块的共享实现：Redis 版 `cache.Tagged`、pgvector 版 `memory.Index`。**默认仍为本地**：不配置时一切照旧（FileStore、`NewMemoryIndex`、`SimpleCache`），分布式能力是宿主显式选择的可选模块。

## 2. 目标、非目标、反目标

**目标**
- G1：contrib 新模块 `cache/redis`，实现 `cache.Bytes` 与 `cache.Tagged`（见 4.4），在 Redis 单机、哨兵、原生 Cluster 下满足 4.4 的契约 S1，正常路径满足 S2。
- G2：contrib 新模块 `memory/pgvector`，实现 `memory.Index`，行为与 `MemoryIndex` 一致（见 4.3）。
- G3：goagent 增加两套后端无关的一致性测试：`cache/cachetest`（Tagged 契约）、`memory/memorytest` 增加 Index 契约；`SimpleCache`、`MemoryIndex` 与两个新模块都跑同一套。
- G4：用 Docker 启动真实 Redis（含 Cluster）与 pgvector 运行集成测试，脚本与 `scripts/test-pgstore.sh` 同构。

**非目标**
- Worker 横向扩展（按命名空间分片加锁）：改锁设计，第二期单独设计。
- Redis 作为 `durable.Store` 或会话日志后端。
- 对向量检索做 ANN 近似索引（HNSW/IVFFlat）：见 4.3 的规模说明。
- goagent 主库引入任何外部依赖。
- `WAITAOF`（等待从节点落盘）、混沌测试套件：本期不做。

**反目标**（技术上能跑也算失败）
- `DeleteTag` 返回后，此前已完成的 `SetTagged` 写入的、带该标签的条目被读到。
- Redis 的淘汰、过期、主从切换（在配置了 `WaitReplicas` 的前提下）、Cluster 迁槽导致已删除内容"复活"。
- 不配置分布式模块的宿主行为或依赖发生变化。

## 3. 约束

- contrib 每个子目录是独立 Go 模块，主模块不承担其依赖；新模块各有 `go.mod`。
- 新模块依赖的 goagent API（`cache.Tagged`、`memory.Index`、新增的一致性测试包、4.4 的契约文字与 ModelCache 改动）目前只在 goagent 本地 master 上，未发布，**须在同一个 goagent 版本中一起发布**。开发期用 contrib 的临时工作区（`GOAGENT_CORE_ROOT=../goagent bash scripts/test-all.sh`）联调；合入 contrib 前须先发布 goagent 新版本并更新版本锁定——这是对外操作，由用户决定时机。
- 外部服务测试在未设置连接变量时跳过（与 pgstore 的 `GOAGENT_PGSTORE_DSN` 一致）；CI 不连真实 Redis Cluster。
- contrib `durable/pgstore/memory_test.go` 是用户未提交的文件，不碰；contrib 当前分支 `feat/pgstore-snapshot-lz4` 不碰，新工作从 contrib master 开分支（或 worktree）。
- 测试一律 `taskset -c 2,3,6-27`，`TMPDIR=/tmp`，禁止 `-mod=mod`。

## 4. 方案

### 4.1 Approaches（Redis 缓存）

| 方案 | 做法 | 取舍 |
|---|---|---|
| A：Lua 原子 | 标签用 Set 记成员，`SetTagged`/`DeleteTag` 各一个 Lua 脚本 | 最简单，删除即物理删除；但脚本内所有键须同槽，Cluster 下只能把全部缓存压进一个槽，放弃分片 |
| **B：标签令牌 + 物理删除（用户选定）** | 每个标签有一个随机令牌，条目写入时记下各标签的令牌；`DeleteTag` 先删令牌（立即生效），再按成员集合物理删除 | 不依赖多键原子，单机/哨兵/Cluster 通用；读取多一次令牌查询；实现约为 A 的两倍 |

用户选择 B，理由：不确定是否使用 Cluster，B 在所有拓扑下都成立。

### 4.2 Redis 缓存（`goagent-contrib/cache/redis`）

**客户端**：`github.com/redis/go-redis/v9`，接受 `redis.UniversalClient` 中的单机、哨兵（Failover）、Cluster 客户端；`Ring` 客户端在 `New` 时拒绝（客户端分片，主从切换与迁槽语义不同）。

**键布局**（`P` 为配置的前缀，默认 `goagent:cache:`；下表中 `<key>`、`<tag>` 是占位符，不是字面花括号）：

| 键 | 类型 | 内容 |
|---|---|---|
| `P e:<key>` | String | 条目：编码后的值 + 写入时各标签的令牌 |
| `P g:<tag>` | String | 标签的当前令牌：16 字节随机数 |
| `P t:<tag>` | ZSET | 标签的成员：元素为缓存键，分数为该键条目的过期时刻（服务端毫秒时间） |

键与标签文本中的 `{`、`}` 须转义（编码方式由实现者定），否则会被 Cluster 当作 hash tag，把数据集中到单槽。

**令牌用随机数，不用递增计数**：令牌缺失视为"不可校验"，按未命中处理。令牌键被淘汰、过期、主从切换丢失时，只会让该标签下的条目多删，不会让旧条目复活。递增计数在键丢失后从 0 重来，旧条目可能恰好匹配而复活。

**执行顺序规则**：下文的编号步骤必须**前一步完成后再发下一步**，不得把不同步骤合进同一个 pipeline——Cluster 下 go-redis 把 pipeline 按节点拆开并发执行，跨节点没有顺序，论证会失效。同一步内发往多个键的同类命令（如读多个令牌）可以 pipeline。

**成员登记脚本** `reg(t, key, ttl)`：只涉及 `t` 一个键的 Lua 脚本，不跨槽：
```
now = TIME（毫秒）
ZREMRANGEBYSCORE t -inf now          -- 剪掉已过期条目的成员，集合大小以存活条目为界
ZADD t GT (now+ttl) key              -- GT：并发写入取最大过期时刻，不会被短 TTL 写入压低
if PTTL(t) < ttl then PEXPIRE t ttl  -- PTTL 为 -1（无过期）时也会设置，t 不会永久存在
```

**操作**：

- `SetTagged(key, value, ttl, tags...)`
  1. 每个标签 `SET g:<tag> <随机> NX GET PX ttl`（Redis ≥ 7）得到当前令牌（已存在返回现值，不存在写入新令牌）；再对已存在的令牌键做与 `reg` 相同的"PTTL 小于 ttl 才延长"（单键脚本），避免令牌先于条目过期造成无谓未命中。
  2. 每个标签 `reg(t:<tag>, key, ttl)`。**先登记再写条目**：只要条目存在，成员就已登记；写者在第 3 步前后崩溃都不会留下集合外的条目。
  3. `SET e:<key> <编码(值, {tag: 令牌})> PX ttl`。
  4. 每个标签再 `reg` 一次，与 `DeleteTag` 的"先移除成员、后删条目"配合（见下方论证 P2）。
  5. **复查**：再读一次各标签令牌，任一与第 1 步不同或缺失则 `DEL e:<key>` 并返回 nil。
- `GetBytes(key)`：`GET e:<key>`，解码出标签令牌；再一步 pipeline 读各 `g:<tag>` 逐个比较。任何一个不相等或缺失 → 返回未命中，并 `DEL e:<key>`（可能误删同一时刻写入的新值，属允许的多删）。无标签条目直接返回。
- `DeleteTag(tag)`
  1. `DEL g:<tag>`。完成即满足 S1：此后所有带旧令牌的条目读取都未命中；下一次 `SetTagged` 生成新令牌。`WaitReplicas > 0` 时见"复制与持久化"。
  2. `ZSCAN t:<tag>` 分批（默认每批 500）；每批**先 `ZREM t:<tag> 本批成员`，完成后再 `UNLINK` 本批的 `e:` 键**（跨槽的键按节点分组发送）。**不在最后删除 `t:<tag>`**：集合清空时 Redis 自动删除它；整体删除会抹掉并发写入刚登记的新成员。
  - 返回错误时由 `memory` 的擦除重做在 `EraseWithin` 内重试。
- `SetBytes`（无标签写入）为 `SET e:<key> PX ttl`；`Delete` 为 `DEL e:<key>`。只实现 `cache.Bytes` 与 `cache.Tagged`，不实现 `cache.Cache`，因此没有 `Clear`、`Has`、`GetStats`。

**正确性论证**（实现的测试须覆盖这些交错）。契约见 4.4：S1 为安全性（读不到），S2 为物理删除。"已完成"指 `SetTagged` 在 `DeleteTag` 开始前已返回；与之并发的写入可以排在删除前或后。

- **P1（S1）**：`DeleteTag` 第 1 步完成后，带旧令牌的条目读取时令牌缺失或不等，未命中。随机令牌不会重复，令牌键丢失只会多删。不依赖复查与扫描。
- **P2（S2，正常路径）**：设写者 W 在 `DeleteTag` D 开始前已完成。W 的第 4 步已把 `k` 登记进 `t`，此后 W 不再改 `t`；D 的 ZSCAN 返回扫描全程都在集合中的元素。`k` 只可能被其他 `DeleteTag` 的 `ZREM` 或 `reg` 的过期剪除移出：前者紧接着删除 `e:k`；后者意味着条目已过期。若条目在 D 返回后仍存在而 `k` 不在集合中，则条目的 `SET e` 晚于某次 `UNLINK e`，其第 4 步 `reg` 又晚于 `SET e`，从而晚于那次 `ZREM`——`k` 应在集合中，矛盾。
- 同键不同标签的覆盖、多标签条目只删其中一个标签、无标签 `Set` 覆盖带标签条目：旧集合中的残留成员只造成多删。
- 复查（第 5 步）的作用：令牌在第 1 步之后被删时，写者自己删除刚写的条目，减少只能靠 TTL 回收的残留。

**S2 的例外：以下情形条目始终读不到（P1），但物理删除只靠 TTL 回收**：
- 写者在第 2 步之后、第 4 步之前失败或崩溃，且恰有 `DeleteTag` 在其间移除了成员（此时写者尚未完成，不属于 S2 要求删除的写入）。
- 客户端超时后 go-redis 重试，旧连接上的 `SET e` 迟到。
- 另一个 `DeleteTag` 在 `ZREM` 与 `UNLINK` 之间崩溃。
- `t:` 键被 `maxmemory` 淘汰（只有 `noeviction` 能避免；`t:` 带过期，`volatile-*` 策略同样会淘汰它）。
- 已过期的键由 Redis 的过期回收周期清理，从内存中消失的时间没有硬上限。
- `t:` 所在节点的时钟向前跳变：`reg` 按该节点 `TIME` 清理"已过期"成员，可能提前移除条目尚未过期的成员。

因此 S2 的完整表述是：正常路径下 `DeleteTag` 返回时已物理删除；例外情形下在 TTL 加过期回收周期内回收。只有 `EraseRequired`（`TTL ≤ EraseWithin`）下这一兜底才落在 `EraseWithin` 内。

**前提：令牌的读写都到主节点。** 读到从节点上滞后的旧令牌会让已删除条目可读。`New` 检查：
- Cluster/Failover 客户端开启了 `ReadOnly`、`RouteByLatency`、`RouteRandomly`、`ReplicaOnly` 之一 → 拒绝；
- 对连接的节点执行 `ROLE`，返回从节点 → 拒绝；
- `CONFIG GET maxmemory-policy` 不是 `noeviction` → 记录告警（说明 S2 的淘汰例外）；`CONFIG` 被禁用的托管服务跳过此检查。

**值编码**：只存 `[]byte`（接口已收窄，见 4.4）。条目格式带版本前缀，便于以后演进。

**配置**：`redis.Config{Client, Prefix, DeleteBatch, WaitReplicas, WaitTimeout}`；`New` 时 `PING` 校验连通、校验服务端版本 ≥ 7，并执行上述前提检查。

**支持范围**：Redis ≥ 7 的单机、哨兵、原生 Cluster。云厂商的代理模式集群、KeyDB、Dragonfly 等兼容实现不在支持与测试范围内，文档写明。

**复制与持久化**（用户决定）：
- `WaitReplicas int`（默认 0）、`WaitTimeout`（默认 1 秒）。大于 0 时，`DeleteTag` 第 1 步在**同一条连接**上发送 `DEL g:<tag>` 与 `WAIT WaitReplicas WaitTimeout`（`WAIT` 只统计本连接此前的写入）：Cluster 用 `MasterForKey(g:<tag>)` 取得主节点客户端再取单连接；单机与哨兵直接取单连接。确认数不足返回错误，由擦除重做重试。
- `WAIT` 只说明删除已复制到若干从节点，不保证主从切换时被提升的正是这些从节点；文档写明它降低而非消除切换回滚的窗口。默认 0 是弱保证：切换或从持久化重启的窗口内，已删除条目可能重新可读；有合规要求的部署须设置。
- 物理删除的标准：`EraseWithin` 之后，该条目在主节点上不可访问，且已从主节点内存删除或处于 S2 例外中（在 TTL 加过期回收周期内回收）。RDB/AOF 文件、备份与从节点上的残留由运维负责，部署文档给出建议（缩短备份保留、AOF 重写周期等）。

### 4.3 pgvector 索引（`goagent-contrib/memory/pgvector`）

**依赖**：pgx/v5（与 pgstore 相同）+ `github.com/pgvector/pgvector-go`。

**表**（`pgvector.Schema` 由宿主纳入迁移，`New` 校验 `SchemaVersion`，与 pgstore 约定一致）：

```sql
CREATE TABLE goagent_memory_vectors (
  ns     text[] NOT NULL,   -- 命名空间原样存为数组：Postgres text 不能含 \x00，也避免自定义编码的前缀歧义
  id     text   NOT NULL,
  dim    int    NOT NULL,
  vec    vector NOT NULL,   -- 不固定维度：不同嵌入模型维度不同
  PRIMARY KEY (ns, id)
);
```

`CREATE EXTENSION vector` 需要较高权限，不放进 `Schema`；由宿主（或 DBA）预先执行，`New` 检查扩展已安装，缺失时返回说明该语句的错误。

**行为须与 `MemoryIndex` 一致**（由 4.4 的一致性测试锁定；pgvector 存 float32、`MemoryIndex` 存 float64，分数在 1e-5 内一致即可，一致性测试不构造分差小于 1e-4 的近似并列）：
- 写入前归一化，零向量与非有限值返回错误。
- **维度**（与 4.4 的契约改动一起）：同一命名空间允许不同维度的向量共存；`Search` 只考虑与查询同维度的向量。
- `Search` 只查精确命名空间（不含子命名空间），按余弦相似度降序、分数相同按 ID 升序，取前 k；`k <= 0` 返回空。SQL 为 `WHERE ns = $1 AND dim = $2 ORDER BY vec <=> $3, id LIMIT $4`，Score = `1 - 距离`。
- `IDs`、`Clear`、`Delete` 只作用于精确命名空间；`Clear` 不递归。

**规模说明**：命名空间内精确扫描（主键前缀过滤 + 逐行距离），不建 ANN 索引。长期记忆按租户/用户分命名空间，单命名空间记录数受 `Retention.MaxRecords` 约束（通常数千以内），精确扫描足够，且排序与 `MemoryIndex` 一致（分数误差内）；不固定维度也使 HNSW 无法使用。向量较大时行数据走 TOAST，扫描成本随命名空间大小线性增长；文档写明规模上限、建议给数据库角色设置 `statement_timeout`，以及以后改 ANN 的路径。

**连接**：可与 pgstore 共用同一个 `pgxpool.Pool`；连接池须指向主库（从库复制延迟会让刚删除的向量仍被召回）。不放进 pgstore，避免把 `vector` 扩展变成 pgstore 的前置条件。

### 4.4 goagent 侧改动

- **`cache.Tagged` 契约改写为两条性质**，替换"必须原子或线性一致……须用事务或脚本"：
  - S1（必须）：`DeleteTag` 返回后，在它开始前已完成的 `SetTagged` 写入的、带该标签的条目不可读。可用原子操作，或用令牌/版本校验使旧写入在读取时失效（引用 Redis 实现为例）。
  - S2（实现须写明）：这些条目何时从存储中物理删除；不能在返回时删除的情形须写明回收上限。
- **`memory.Index` 契约改动**：去掉"同一命名空间内维度须一致"，改为"不同维度的向量可共存，`Search` 只考虑与查询同维度的向量"。`MemoryIndex` 同步修改。理由：持久化索引下更换嵌入模型（维度变化）后，命名空间里留着旧模型向量，新向量写入一律被拒，`Reindex` 在写入阶段失败、走不到清理旧键那一步（`memory/indexing.go` 的 `reindex`），索引无法自行恢复；进程内索引每次重启为空，这个问题此前没有暴露。放开维度后，新旧模型向量共存，`adopted` 已按模型过滤召回结果，`Reindex` 写入新向量并清理旧模型的键。同维度换模型本来就靠 `adopted` 过滤，不受影响。
- **新接口 `cache.Bytes`，`ModelCache.Cache` 改用它**（用户决定，破坏性改动）。`cache.Cache` 的 `Set` 收任意 Go 值，Redis 只能存字节，工具缓存中间件写的是 `*interfaces.ToolOutput`，`string` 写入读回会变成 `[]byte`；收窄后误用在编译期暴露。
  ```go
  type Bytes interface {
      GetBytes(ctx context.Context, key string) ([]byte, error) // 未命中返回 errors.ErrCacheMiss
      SetBytes(ctx context.Context, key string, value []byte, ttl time.Duration) error
      Delete(ctx context.Context, key string) error
  }
  ```
  `SimpleCache` 实现它（`GetBytes` 遇到非 `[]byte` 的值返回 `CodeInvalidInput`，`ModelCache` 按损坏条目删除）。`ModelCache` 内部改用 `GetBytes`/`SetBytes`，去掉对 `string` 的兼容分支；`EraseRequired` 仍通过类型断言要求同时实现 `cache.Tagged`。CHANGELOG 写迁移步骤：传 `SimpleCache` 的调用方无需改动；自定义实现补上两个方法。
- **`builder.ModelCache` 写入报错时也删除**：`store` 在 `write` 返回错误时调用一次 `Delete(key)`（超时可能已在服务端生效），减少只能靠 TTL 回收的残留。
- 新包 `cache/cachetest`：`RunTagged(t, open func(t *testing.T, name string) interface{ cache.Bytes; cache.Tagged })`，同一 `name` 打开的是共享同一份数据的两个实例（模拟两个进程），不同 `name` 互相隔离。覆盖：S1、幂等删除、标签替换、TTL、并发 `SetTagged`×`DeleteTag`（确定性 + 压力两类）、跨实例删除、不同 `name` 互不影响。`SimpleCache` 测试改为调用它；`SimpleCache` 特有的"不误删"测试（`cache/tagged_test.go` 中依赖原子实现的用例）留在原文件，不进入通用套件。
- `memory/memorytest` 新增 `RunIndex(t, newIndex func(t) memory.Index)`：覆盖 4.3 列出的全部行为，含不同维度共存；另加一个 `Memory` 级测试：换嵌入模型（维度变化）后 `Reindex` 收敛——旧模型的键被删、新模型召回正常。`MemoryIndex` 测试改为调用它。
- 文档：`CACHING_GUIDE.md`、`memory/README.md` 增加"分布式部署"小节，内容见 4.6。

### 4.5 测试与脚本（contrib）

- `scripts/test-redis.sh`：Docker 启动固定小版本的 `redis:7.4.x` 单机容器跑 `cache/redis` 全部测试；带 `--cluster` 时起 6 节点（3 主 3 从）Cluster（设置 `cluster-announce-ip`，等待 `cluster_state:ok` 后再跑）再跑一遍。结束删除容器。连接变量 `GOAGENT_REDIS_ADDR`（Cluster 为逗号分隔地址）；未设置时跳过。
- `scripts/test-pgvector.sh`：Docker 启动固定版本的 `pgvector/pgvector:0.8.x-pg17` 跑 `memory/pgvector` 测试；连接变量 `GOAGENT_PGVECTOR_DSN`。
- 两个模块都调用 4.4 的一致性测试。
- Redis 特有测试：
  - 令牌键被删除后旧条目不复活；Cluster 下跨槽写入与删除正常；不同前缀互不影响。
  - 确定性交错（测试钩子注入）：SetTagged 第 4、5 步之间注入 `DeleteTag`，条目被删且读不到；两个 `DeleteTag` 并发且中间有完成的写入（P2 的场景），返回后条目物理不存在；第 3 步失败注入，条目不可读。
  - `t:` 集合有界：写入大量短 TTL 条目并过期后，集合大小回落；`t:` 键带过期。
  - `WaitReplicas`：单机无从节点时 `DeleteTag` 超时返回错误；Cluster 下用 `CLIENT PAUSE` 暂停从节点，断言返回错误。
- 端到端：一个测试用两个 `builder.ModelCache`（模拟两个进程，共享同一 Redis）+ 共享 pgstore 记忆，进程甲 `Forget`，断言进程乙的缓存条目被物理删除、不再命中。需要同时连 Redis 与 Postgres，放在单独的仅测试模块 `integration/distributedmemory`（不发布），使 `cache/redis` 的使用者不必引入 pgx 与 pgstore；任一连接变量缺失即跳过。
- contrib README 模块表、测试小节补两行。

### 4.6 迁移与部署（写进 4.4 的文档小节）

- **所有实例使用相同的 `TagKey`**：标签是 HMAC，`TagKey` 不同则同一记忆在各实例上的标签不同，`DeleteTag` 删不到其他实例写的条目。
- **每个持有记忆的进程都要接上擦除**：包括只跑 `memory/worker` 的进程——它执行的 `Forget`/`Purge` 也要通过 `OnErase` 调到共享缓存。
- **切换缓存**：混合部署期间（部分实例仍用 `SimpleCache`）删除不会到达本地缓存，跨实例删除承诺不成立。建议一次性切换，或切换期间关闭 ModelCache。从 Redis 回退到本地无需迁移数据，Redis 中的条目按 TTL 过期。
- **切换向量索引**：切到 pgvector 后表为空，召回暂时只有非向量分；宿主对现有命名空间逐个调用 `Reindex(ctx, false)` 回填。回退到本地索引同样靠 `Reindex`。
- **Redis 淘汰策略**：S1 不受淘汰影响（令牌设计）；S2 要求 `noeviction` 或保证不触及 `maxmemory`。

## 5. 留给实现者的决定

- 条目编码的具体字节格式、令牌长度（≥16 字节随机）。
- 键与标签中花括号的转义编码。
- Cluster 测试容器的具体编排方式（单容器多端口或 `redis-cli --cluster create`）；镜像的具体小版本号。
- 测试钩子的形式（包内未导出的函数变量即可，不进入公开 API）。

## 6. 风险

- **读取多一次往返**：每次 `Get` 多一次 pipeline 读令牌。模型调用缓存的命中路径节省的是秒级模型调用，可接受；在 README 写明。
- **S2 例外靠 TTL**：见 4.2。`EraseAuto` 下 TTL 可以大于 `EraseWithin`，例外条目的物理删除可能晚于 `EraseWithin`（始终不可读）。
- **`WAIT` 不能消除切换回滚**：见 4.2"复制与持久化"。
- **版本发布顺序**：contrib 新模块依赖未发布的 goagent；发布 goagent 是对外操作，需用户确认。
- **pgvector 规模**：单命名空间超过数万条时精确扫描变慢；文档给出上限与 ANN 迁移路径。
- **换嵌入模型的过渡期**：新旧模型向量共存、召回按模型过滤，旧向量占用 `Search` 的前 k 名额，召回条数可能少于 k，直到 `Reindex` 清理完毕。

## 7. 用户的决定

- Redis 方案选 B（标签令牌 + 物理删除），以支持 Cluster。
- 主从切换回滚：可配置等待从节点确认（`WaitReplicas`，默认 0），不配置时为文档写明的弱保证。
- 物理删除标准：`EraseWithin` 之后主节点上不存在；持久化文件与备份由运维负责。审核后细化为 4.2 末尾的表述（正常路径删除；例外情形在 TTL 加过期回收周期内回收）。
- 默认部署仍为本地，分布式模块显式选用；测试用 Docker 启动真实 Redis。
- 缓存接口收窄：新增只收发字节的 `cache.Bytes`，`ModelCache.Cache` 改用它，接受破坏性改动（4.4）。

## 8. 设计者的决定（可推翻）

- 令牌用随机数、`DeleteTag` 以删除令牌键生效、`SetTagged` 写后复查（来自动工检查）。
- 采纳审核意见修正删除协议：成员集合改为以过期时刻为分数的 ZSET（剪除过期成员，集合有界）；写者"登记 → 写条目 → 再登记 → 复查"；`DeleteTag` 每批"先移除成员、后删条目"，不整体删除集合；各步不合并 pipeline。也考虑过"每代一个成员集合"（`t:<tag>:<令牌>`），能同样避免抹掉新成员，但需要额外记录旧代集合以便清理，未采用。
- `WAIT` 与 `DEL` 走同一条连接；`Ring` 客户端拒绝；`New` 用 `ROLE` 拒绝从节点、对非 `noeviction` 策略告警。
- 最低 Redis 7；代理模式集群与兼容实现不支持。
- Redis 实现不提供 `Clear`/`Has`/`GetStats`（`ModelCache` 不用，按需再加）；不加错误回调（`DeleteTag` 的错误已返回给擦除重做，`Get` 的错误按未命中处理并由调用方的观测事件记录）。
- 向量索引用 pgvector、独立模块、精确扫描、不建 ANN；`ns` 存 `text[]`；`CREATE EXTENSION` 不放进 `Schema`；分数误差 1e-5 内视为一致；不提供单独的检索连接池（宿主可给数据库角色设 `statement_timeout`）。
- `memory.Index` 放开同命名空间维度一致的要求，以修复换模型后索引无法恢复（见 4.4）。
- 端到端测试放在不发布的独立集成模块。
- Worker 横向扩展放第二期。
