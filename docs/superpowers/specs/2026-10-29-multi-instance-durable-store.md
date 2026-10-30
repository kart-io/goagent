# 多实例 durable.Store：接口设计

日期：2026-10-29（2026-10-30 按评审修订）  
状态：P0 已实施（见第 11 节）；P1、P2 待评审  
代码基线：`1c4fa91`（`codex/composable-orchestration-r2`）  
来源：[定位决策](../../research/2026-10-29-goagent-positioning.md) 第 4、5 节；[嵌套恢复与失败处理](2026-10-29-nested-resume-and-failure-handling.md)

## 1. 背景

持久恢复、子流程续跑、根预算和上下文证据都建立在 `durable.Store`（`core/durable/runner.go`）上，目前唯一实现是 `FileStore`：本机文件锁加原子 rename。宿主服务一旦部署多个实例，就会出现两个问题：

- 进程 A 崩溃后，只有能读到 A 本地目录的进程才能续跑，另一台实例无法接管；
- 审批回调可能落到任意实例，而运行状态只在 A 的磁盘上。

被删除的 `store/postgres`、`store/redis` 实现的是 KV 接口 `store.Store`，不提供锁和快照语义，无法替代。

## 2. 现有契约（以代码为准）

```go
type Store interface {
	Lock(context.Context, string) (func() error, error)
	Load(context.Context, string) (*Snapshot, error)
	Save(context.Context, string, *Snapshot) error
}
```

| # | 约定 | 依据 |
| --- | --- | --- |
| C1 | `Lock` 等待到获得锁或 ctx 结束；同一 ID 不可重入，同一进程内也互斥 | `file_store.go` 的 `TryLockContext(ctx, 20ms)`，每次 `flock.New` 新开文件描述符 |
| C2 | 锁会**长时间持有**并**嵌套**：图 → 操作 → 子操作 → 预算。并行分支在同一进程内并发，都会去锁同一个根预算 ID | `workflow/durable.go:164`、`operation.go:252` 持锁期间调用 `run`；`workflow/durable_parallel.go` |
| C3 | 持有者进程退出后锁必须释放 | `Store` 文档注释 |
| C4 | 所有 `Save` 都在调用方持有该 ID 的锁期间调用；并行分支的图快照只由协调者串行写入 | runner ×4、`operation.go`、`core/budget/controller.go:148`、`workflow/durable.go:183/209`、`builder/contextpolicy/store.go:178` |
| C5 | `Save` 原子且持久，全部字段原样保存；返回 nil 后崩溃不丢失 | `FileStore.Save` 的 fsync 与 rename |
| C6 | `Load` 不需要持锁，读到最近一次成功写入，成功时快照非 nil；不存在时返回 `os.ErrNotExist` | `runner.go` 直接访问 `snapshot.Plan`；各调用方按 `os.ErrNotExist` 分支 |
| C7 | 最后一次 `Save` 可能在调用方 ctx 已取消后，用 `context.WithoutCancel` 加 5 秒超时执行 | `operation.go:334`、`builder/model_call.go:49` |
| C8 | 可选的 `Scoped`：墓碑后 `Lock`/`Save`/`Load` 返回 `ErrScopeRemoved`；名称不可复用；重复删除不刷新墓碑时间；`PruneRemoved` 回收过期墓碑 | `Scoped` 文档注释，由 `builder/contextpolicy` 使用 |
| C9 | 生产代码共 7 处 `Lock`，其中 3 处**只为读取**而加锁 | 见下表 |

| 调用点 | 用途 |
| --- | --- |
| `core/durable/runner.go:107` | 运行步骤 |
| `core/durable/operation.go:252` | 持久操作 `Do` |
| `core/durable/operation.go:191` | `InspectOperation`，只读 |
| `workflow/durable.go:164` | 图运行 |
| `workflow/durable.go:378` | `DurableGraph.snapshot`，只读 |
| `core/budget/controller.go:112` | 预算事务；`Remaining`、`Snapshot` 以 `write=false` 进入，只读 |
| `builder/contextpolicy/store.go:162` | 证据写入 |

C2 和 C3 决定了核心难点：锁必须能跨进程、能在持有者崩溃后释放，又要能持有几分钟而不误释放。C9 意味着换成租约实现后，只读查询也要写租约行，持有者崩溃时跨实例查询需要等一个 TTL（见 4.6）。

## 3. 设计目标与非目标

**目标**

1. 多个实例共享同一后端时，任何实例都能续跑另一个实例中断或暂停的运行。
2. 持有者崩溃后，其他实例在有界时间（租约 TTL 加一次轮询间隔）内接管。
3. 租约丢失后，旧持有者的写入被拒绝，**包括同一进程内的旧持有者**；日志不会被两个写者交替覆盖。
4. 不改 `durable.Store` 接口；`FileStore` 与新实现通过同一套一致性测试。
5. 核心模块不新增外部依赖，后端实现放在独立模块。

**非目标**

- 不保证外部副作用恰好一次。租约丢失后可能有两个执行者同时执行同一操作，仍由现有契约兜底：外部效果按派生执行键幂等。
- 不做跨实例调度、任务分发和自动拉起续跑；发现与触发续跑由宿主负责。
- 不做跨地域复制；不解决预算账本随请求数增长的问题（见 7.4，单独立项）。

## 4. 方案：进程内闸门 + 租约 + 令牌校验，接口不变

### 4.1 进程内闸门

根 Store 为每个 `(命名空间, ID)` 维护一个进程内闸门（按键的互斥量），所有 `Scope()` 返回的对象共享根 Store 的闸门表。`builder/contextpolicy` 每次都会新建 `Scope(run)` 对象，闸门表不能放在作用域对象里。

- `Lock` 先在 ctx 内获得闸门，再去后端获取租约；闸门**一直持有到这次 Lock 返回的 unlock 被调用**，不论后端租约是否已过期。
- 因此进程内同一 ID 任何时刻至多一个逻辑持有者。闸门条目记录该持有者的令牌，只能由它自己的 unlock 删除。
- 同进程的等待者在闸门上等待，持有者 unlock 时立即交接，不去后端轮询。并行分支争用根预算 ID 时只在进程内排队。

这一层修正了初稿“进程内按 ID 查令牌”的漏洞：初稿中，G1 失去租约后，同进程的 G2 能拿到新令牌并覆盖表项，G1 随后的 Save 查到 G2 的令牌而通过校验。有了闸门，G2 在 G1 unlock 之前拿不到闸门，也就无法获取租约。

### 4.2 租约

租约是后端的一行（或一个键）`{ns, id, token, owner, expires_at}`，令牌每次获取时随机生成。

| 动作 | 语义 |
| --- | --- |
| 获取 | 没有租约或租约已过期时写入新令牌，否则按轮询间隔（带抖动）重试到 ctx 结束。获取语句使用 Store 内部的 ctx 执行，不随调用方 ctx 取消，执行完再检查调用方 ctx；结果不确定（网络中断、ctx 已取消）时，返回错误前尽力执行一次“按自己的令牌删除”，避免留下无人持有的租约 |
| 续约 | Store 每个 `RenewInterval` 用**一条语句批量续约**本实例持有的全部租约，使用 Store 自己的 context 和独立的小连接池，不跟随 `Lock` 的 ctx（C7）。续约语句不带过期条件：租约过期但未被接管时，续约成功即恢复持有 |
| 解锁 | unlock 闭包捕获自己的令牌：停止该租约的续约，用独立 ctx 和超时执行“按自己的令牌删除”，再释放闸门。删除失败或租约已被接管时返回 nil 并记指标，让租约自然过期；此前的写入都受令牌保护，不应让一次已成功的运行失败 |

TTL 必须大于后端一次主备切换的时长，否则一次切换会让所有长持锁者同时失去租约、被别的实例接管。默认 30 秒、续约间隔 10 秒，可配置。

### 4.3 写入的令牌校验

`Save(ctx, id, snap)` 从闸门条目取出当前持有者的令牌（C4 保证 Save 由持有者调用），在**同一个原子操作**中校验“后端租约的令牌与之一致”，一致才写入，同时顺带延长 `expires_at`；不一致或租约不存在时返回 `ErrLeaseLost`。

- 隔离只依赖令牌：任何接管都会改写令牌，所以 Save **不检查** `expires_at`。租约过期但没被接管时写入并不危险，检查过期反而会让一次超过 TTL 的切换之后所有写入集体失败，包括 C7 的最后一次写入。
- 本地不做“距上次续约超过 TTL 即视为丢失”的判定，以后端校验为准。
- 后端行锁（Postgres `FOR UPDATE`、Redis 单线程脚本）保证并发的接管排在本次写入之后。

### 4.4 时钟

过期判断全部使用后端时间（Postgres 的 `now()`、Redis 的 `PX`），不比较各实例的本地时钟。Postgres 的 `now()` 是事务开始时间，事务排在锁后面时会偏旧；这不影响正确性，因为 4.3 的隔离只看令牌，实现者不要为此改用 `clock_timestamp()` 或在 Save 中补过期检查。

### 4.5 读取

`Load` 必须与 `Save` 落在同一个可线性化的主节点上（C6）。预算事务是“持锁、Load、修改、Save”的读改写，Load 读到滞后副本时 Save 的令牌仍然有效，结果是丢更新、预算超发；`Do` 也会把已完成的记录读成 pending 而重新执行。

- Postgres 实现只接受直连主库的连接池，不支持读副本或读写分离代理。
- Redis 实现禁止从副本读。

### 4.6 只读加锁

C9 的 3 处只读调用在租约实现下每次都会获取并删除租约；持有者崩溃时，跨实例的状态查询要等满一个 TTL。P1 接受这一点并写进文档；是否在核心为只读路径提供不加锁的读取，列入第 10 节待确认，不在本设计中改动调用方。

### 4.7 作用域

| 操作 | 语义 |
| --- | --- |
| `Scope(name)` | 返回落在独立命名空间的 Store，不产生任何写入 |
| `RemoveScope` | 两段式：先用一个短事务（或单个脚本）写墓碑并提交，墓碑生效后数据即不可读；再分批删除该命名空间的快照与租约。中途崩溃时重复调用可续删，与 `FileStore` 一致。墓碑已存在时不刷新时间（C8） |
| 互斥 | 写墓碑与 `Lock`、`Save` 的墓碑检查互斥，见 6.1 |
| 持锁中的写者 | 墓碑生效后，下一次 `Save` 返回 `ErrScopeRemoved` |
| `PruneRemoved` | 在与写入互斥的前提下删除早于阈值的墓碑与残留数据 |

`RemoveScope` **不等待**持锁者：等待会在持有者崩溃时阻塞到 TTL 过期，而两种方式的结果相同。契约已经写明这一点（`Scoped` 文档），`storetest` 的“持锁期间删除作用域”用例两种行为都接受。

## 5. 核心模块的改动（P0，已实施）

- `Store`、`Scoped` 的文档写明第 2 节的契约；
- 新增 `ErrLockUnavailable`（ctx 结束前未获得锁，同时包装 ctx 错误）、`ErrLeaseLost`（持有者已失去锁时写入被拒）；
- 新增一致性测试包 `core/durable/storetest`，`FileStore` 已接入。

## 6. 后端实现（独立模块）

放在 `goagent-contrib`（每个子目录一个 `go.mod`），主模块不引入 pgx 和 go-redis。

### 6.1 Postgres（首选）

**前置条件**（写进包文档，`New` 能检查的就检查）：

| 项 | 要求 | 原因 |
| --- | --- | --- |
| 连接 | 直连主库；可以经过 pgbouncer 事务模式（本设计不依赖会话状态），此时 pgx 须关闭预编译语句缓存（如 `QueryExecModeExec`） | 4.5 |
| 复制 | 高可用部署须同步复制（`synchronous_standby_names`），并对旧主做隔离（fencing） | 异步复制下主库切换会丢失已确认的写入：持久操作回退后被重新执行，预算账本回退导致超额准入，破坏“崩溃从不退还”的保证（`controller.go:2`） |
| 提交 | 每个写事务执行 `SET LOCAL synchronous_commit = on` | 不受宿主全局 `synchronous_commit=off` 影响 |
| 超时 | 写事务 `SET LOCAL lock_timeout`、`statement_timeout`，明显小于 5 秒；建议设置 `idle_in_transaction_session_timeout` | C7 的最后一次写入只有 5 秒 |
| 隔离级别 | READ COMMITTED | 下文的语句按该级别设计 |

**表结构**（`pgstore.Schema`，由宿主纳入迁移流程；`New` 只校验版本，不自动建表）：

```sql
CREATE TABLE goagent_durable_schema (version int NOT NULL);
CREATE TABLE goagent_durable_snapshot (
  ns   text  NOT NULL,          -- '' 为根，其余为作用域名
  id   text  NOT NULL,
  data bytea NOT NULL,          -- Snapshot 的 JSON 原文；不用 jsonb，避免改写字节
  updated_at timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (ns, id)
);
CREATE TABLE goagent_durable_lease (
  ns text NOT NULL, id text NOT NULL,
  token uuid NOT NULL, owner text NOT NULL,
  expires_at timestamptz NOT NULL,
  PRIMARY KEY (ns, id)
) WITH (fillfactor = 70);       -- 续约走 HOT 更新
CREATE INDEX ON goagent_durable_lease (expires_at);
CREATE TABLE goagent_durable_scope (
  ns text PRIMARY KEY,
  removed_at timestamptz NOT NULL   -- 有行即墓碑
);
```

**作用域互斥**：用事务级 advisory 锁，键为 `hashtextextended(ns, 0)`。写入方（`Lock` 的获取事务、`Save`）取共享锁 `pg_advisory_xact_lock_shared`，`RemoveScope` 写墓碑和 `PruneRemoved` 取排他锁 `pg_advisory_xact_lock`。

- 初稿用作用域行的 `FOR SHARE`：作用域行尚不存在时锁不住任何东西，首次写入与删除并发会把数据残留在已删除的作用域里；高频 `FOR SHARE` 还会产生 MultiXact。advisory 事务锁不占会话，提交即释放，没有这两个问题。
- 哈希冲突只会让无关作用域多等一次，不影响正确性。
- 根命名空间 `''` 不加作用域锁、不查墓碑。

**各操作**：

| 操作 | 语句要点 |
| --- | --- |
| 获取 | 先用普通 SELECT 预检租约不存在或已过期，避免每次轮询都锁冲突行（`ON CONFLICT DO UPDATE` 即使 WHERE 为假也会锁住冲突行）。预检通过后一个事务：作用域共享锁；查墓碑；`INSERT ... ON CONFLICT (ns,id) DO UPDATE SET token=…, owner=…, expires_at=now()+ttl WHERE lease.expires_at < now() RETURNING token`，无返回即未获得 |
| 写入 | 一个事务：`SET LOCAL`（见前置条件）；作用域共享锁；查墓碑；单条语句 `WITH l AS (UPDATE lease SET expires_at = now()+ttl WHERE ns=$1 AND id=$2 AND token=$3 RETURNING 1) INSERT INTO snapshot ... SELECT ... WHERE EXISTS (SELECT 1 FROM l) ON CONFLICT (ns,id) DO UPDATE ...`。UPDATE 持有租约行锁直到提交，影响 0 行即 `ErrLeaseLost` |
| 续约 | 每个周期一条：`UPDATE lease SET expires_at = now()+ttl FROM unnest($ns, $id, $token) AS h(ns,id,token) WHERE (lease.ns, lease.id, lease.token) = (h.ns, h.id, h.token) RETURNING lease.ns, lease.id`；未返回的条目只记指标，以写入时的校验为准 |
| 读取 | 单条语句读快照，并联查 `scope` 判断墓碑 |
| 解锁 | `DELETE FROM lease WHERE ns=$1 AND id=$2 AND token=$3` |
| 删除作用域 | 事务 1：排他锁；`INSERT INTO scope ... ON CONFLICT DO NOTHING`（不刷新时间）；提交。之后每批 1000 行删除快照和租约，直到删完 |
| 回收墓碑 | 每个过期墓碑一个事务：排他锁；删除残留数据和墓碑 |
| 清理过期租约 | 持久操作 ID 由执行键派生，崩溃留下的租约行可能永远不会再被获取。Store 定期执行 `DELETE FROM lease WHERE expires_at < now() - $grace`；删除是安全的，被删租约的旧持有者写入时因令牌不存在而被拒 |

根命名空间快照的保留与清理由宿主负责，本设计不提供。

**接口草案**：

```go
package pgstore

// Schema 是建表语句与版本，由宿主纳入自己的迁移流程。
const Schema = `...`

type Config struct {
	Pool          *pgxpool.Pool // 直连主库
	RenewPool     *pgxpool.Pool // 续约专用的小连接池；为 nil 时使用 Pool（文档提示高峰期续约可能排队）
	LeaseTTL      time.Duration // 默认 30s，须大于主备切换时长
	RenewInterval time.Duration // 默认 LeaseTTL/3
	PollInterval  time.Duration // 默认 50ms，叠加抖动；只用于跨实例等待
	ExpiredGrace  time.Duration // 过期租约保留多久后清理，默认 10*LeaseTTL
	Owner         string        // 仅用于诊断，默认 hostname:pid
	Meter         metric.Meter  // OTel 指标，为 nil 时不记录
}

// New 校验表结构版本。返回值实现 durable.Store 与 durable.Scoped。
func New(ctx context.Context, cfg Config) (*Store, error)

// Close 先拒绝新的 Lock，在 ctx 截止前等待现有持有者 unlock，再停止续约并释放剩余租约。
func (s *Store) Close(ctx context.Context) error
```

MySQL 可以用同样的表结构实现（`GET_LOCK` 不适合，原因同 advisory 会话锁），等确认宿主使用的数据库后再定是否提供。

### 6.2 Redis（P2，有条件提供）

仅适用于外部效果已严格按执行键幂等、能接受“故障切换后快照回退”的宿主；正式环境优先 Postgres。

| 项 | 设计 |
| --- | --- |
| 键 | 作用域：`{ns}:snap` 为 HASH（字段为 id）、`{ns}:lease:<id>`、`{ns}:tomb`，花括号是 Cluster 哈希标签，同一作用域落在同一槽。根命名空间不用花括号（空的 `{}` 不构成哈希标签），根不需要多键脚本 |
| 获取 | 脚本：查墓碑；`SET lease token NX PX ttl` |
| 写入 | 脚本：查墓碑；`GET lease == token` 时写快照并 `PEXPIRE` 租约 |
| 续约、解锁 | 脚本：比较令牌后 `PEXPIRE` 或 `DEL` |
| 删除作用域 | 先写墓碑，再 `UNLINK {ns}:snap` 与该作用域的租约键；不需要 SCAN 全库 |
| 必需配置 | `maxmemory-policy noeviction`（否则淘汰会静默删除快照、租约或墓碑）；AOF `appendfsync always`；`min-replicas-to-write` 限制脑裂期间的写入；只读主节点 |

**持久性限制**：主从复制是异步的，故障切换可能丢失已确认的写入。`WAIT` 只能降低概率，不提供强一致：被选为新主的副本不一定是确认过写入的那个。丢失的后果是快照回退，持久操作被重新执行、预算回退；旧持有者的令牌不在新主上，续约和写入都会失败，所以只会出现两个**执行者**，不会出现两个都生效的写者。只有脑裂（两个主同时接受写入）才会双写都生效。快照全部常驻内存，部署前需按并发运行数和快照大小评估容量。

## 7. 性能评估

### 7.1 往返次数（Postgres，同机房单次往返约 0.5–1ms）

| 动作 | 往返 | 说明 |
| --- | --- | --- |
| 获取 | 2 | 预检 SELECT；获取事务（BEGIN、锁、查墓碑、upsert、COMMIT 用 pgx 批量发送） |
| Load | 1 | |
| Save | 1–2 | 事务内各语句批量发送 |
| 解锁 | 1 | |

一次预算事务（获取、Load、Save、解锁）约 5–6 次往返；每个模型请求要做 Reserve 和 RecordUsage 两次，共约 10–12 次，即 5–12ms，相对秒级的模型请求可以接受。初稿写的“约 4 次往返”是错的。

### 7.2 根预算键的争用

`RootKey` 标识一个逻辑运行（`controller.go:28`），争用只发生在同一运行的并行分支之间，而它们通常在同一进程内：

- 进程内由 4.1 的闸门直接交接，`Controller.mu` 本来也在进程内串行化，吞吐受临界区（约 5ms）限制，单个运行约 100–200 次预算事务/秒；
- 只有同一运行的操作分布在多个实例上时（例如接管期间）才会走跨实例轮询，那时交接延迟主要来自 50ms 的轮询间隔。

如需降低跨实例交接延迟，P2 可加 `LISTEN/NOTIFY` 唤醒。

### 7.3 续约与表膨胀

批量续约每个 Store 每个周期 1 条语句，与持锁数无关；按 1000 个并发持锁、10 秒间隔，约每秒更新 100 行。lease 表 `fillfactor = 70` 保证 HOT 更新，需调高该表的 autovacuum 频率并监控死元组数。

### 7.4 快照大小

快照包含模型输出，通常为 KB 级。例外是预算账本：`Entries` 随请求数线性增长，每条约 150 字节，每次事务整体重写，`MaxModelRequests=1000` 时单条约 150KB，总写入量随请求数平方增长，会带来 TOAST 与 WAL 放大。这是 `core/budget` 的问题，FileStore 下同样存在，单独立项，不在本设计中解决。

## 8. 迁移与发布

- **不能混跑**：FileStore 实例与 pgstore 实例的锁互不可见，滚动发布期间互斥完全失效。切换必须停机或排空：先让所有实例停止接收新运行，等进行中和等待审批的运行结束（或者确认可以放弃），再切到 pgstore。
- **导入**：根命名空间的快照可以导入，`Snapshot.RunID` 即存储 ID，P1 提供离线导入命令。作用域数据（`builder/contextpolicy` 的证据）**无法导入**：FileStore 只保存作用域名的摘要，名称不可恢复。依赖证据的运行必须在切换前结束。
- **表结构版本**：`goagent_durable_schema.version` 由 `New` 校验，不匹配时拒绝启动。

## 9. 监控指标（OTel）

| 指标 | 用途 |
| --- | --- |
| 获取等待时长（直方图，按是否跨实例） | 争用与交接延迟 |
| 接管次数（获取时前一个租约已过期） | 崩溃与续约失败的频率 |
| `ErrLeaseLost` 次数 | 隔离实际生效的次数 |
| 续约失败次数、续约延迟 | TTL 是否够用 |
| unlock 时租约已丢失或删除失败的次数 | 排查隔离与孤儿租约 |
| 当前持有租约数、持有时长 | 容量与长持锁 |
| Save 延迟、连接池等待时间 | 是否逼近 C7 的 5 秒 |
| 快照大小分布 | 7.4 的预算账本增长 |
| lease 表死元组数 | 7.3 的膨胀 |

## 10. 实施顺序与待确认

| 阶段 | 内容 | 验证 |
| --- | --- | --- |
| P0（已完成） | 契约、哨兵错误、`storetest`，`FileStore` 接入 | 见第 11 节 |
| P1 | `goagent-contrib/durable/pgstore`；`storetest` 增加 `Options.LeaseTTL` 与模拟持有者崩溃的钩子，补接管、令牌隔离（含同进程旧持有者）、获取结果不确定后的清理用例；离线导入命令 | 本地 Docker Postgres（含同步复制）运行 `storetest.Run`；两个进程的崩溃续跑测试；主库重启期间的写入失败与恢复测试 |
| P2 | `redisstore`；`LISTEN/NOTIFY` 唤醒；可选接口 `LeaseLocker`（`Lock` 额外返回租约丢失时取消的 ctx，让旧持有者尽早停止模型请求），需改动第 2 节 C9 表中全部 7 处调用点 | 同 P1 |

| 待确认 | 说明 |
| --- | --- |
| 宿主数据库 | 内部服务使用 Postgres 还是 MySQL，决定 P1 先做哪一个；是否具备同步复制 |
| 扩展仓库位置 | 新建 `goagent-contrib`，还是放在 goagent 仓内作为独立子模块（需要多模块 tag） |
| TTL 默认值 | 30 秒意味着崩溃后最多约 30 秒才能被接管；须与实际主备切换时长对齐 |
| 只读路径 | C9 的 3 处只读查询是否改为不加锁的读取，需要核心改动，单独评估 |
| 预算账本增长 | 7.4，单独立项 |
| 续跑触发 | 是否提供“列出等待中或租约过期的运行”的查询接口，供宿主扫描续跑；需要的话利用 `lease(expires_at)` 索引并为快照补索引 |
| 是否需要 Redis | 6.2 的限制较多，若宿主都有 Postgres，可以不做 |

## 11. P0 实施记录

实施于 2026-10-29，2026-10-30 按代码评审修订，基线 `1c4fa91`。

| 项 | 实现 |
| --- | --- |
| 契约 | `core/durable/runner.go` 中 `Store`、`Scoped`、`ErrLockUnavailable` 的文档注释 |
| 哨兵错误 | `ErrLockUnavailable`、`ErrLeaseLost`。`FileStore.Lock` 与作用域闸门在 ctx 结束时返回 `%w: %w` 同时包装哨兵错误和 ctx 错误，I/O 错误原样返回（`lockError`）。此前这两处在 ctx 结束时返回裸的 ctx 错误（如 `context deadline exceeded`）：flock 的 `TryLockContext` 只在出错时返回 `ok=false`，原来的 `run lock unavailable` 分支走不到 |
| 测试套件 | `core/durable/storetest`，`FileStore` 在 `core/durable/file_store_conformance_test.go` 接入：同一目录上的两个 `FileStore` 各自打开锁文件，flock 在两者之间互斥，相当于两个进程 |

用例（15 个）：互斥（断言持锁期间等待者未获得、Lock 等到 ctx 结束才返回）、不可重入、不同 ID 互不阻塞、同实例同时持有多个 ID、跨实例读写（全部字段原样往返）、不存在返回 `ErrNotExist`、取消后仍可写入、写入原子、审批跨实例续跑、操作跨实例回放；作用域：隔离、与根同名 ID 互不阻塞、删除、持锁期间删除（接受等待与不等待两种实现）、墓碑回收（含重复删除不刷新时间，时间余量 300ms）。

与设计的差异：

| 设计 | 实际 | 原因 |
| --- | --- | --- |
| `Options.LeaseTTL` 与接管、隔离用例 | 随 P1 加入 | P0 没有租约实现，无法验证；还需要“模拟持有者崩溃”的钩子 |
| 端到端用例使用 `DurableGraph` | 改用 `durable.Runner` 的审批步骤，外加 `durable.Do` 跨实例回放 | `core` 不应导入上层的 `workflow`；`DurableGraph` 的持久化同样只经过 `Store` 的三个方法 |
| 持有者退出后释放（C3） | 套件不覆盖 | 通用套件无法杀死实现内部的持有者；`FileStore` 已有子进程测试 |

验证：

- 代码评审（独立 agent）后补了同实例多锁、全字段往返、持锁期间删除作用域三个用例，修正了用例失败时 goroutine 泄漏、互斥用例未断言等待、墓碑回收余量过小等问题。
- 套件有效性：用临时的内存实现逐项注入违约，均被对应用例抓到后删除临时代码：
  - 每实例一把全局锁：同实例同时持有多个 ID、作用域与根同名 ID、互斥、作用域删除失败；
  - 保存时丢掉 `Approved`：跨实例读写失败；
  - 作用域删除不等待且写入不查墓碑：持锁期间删除作用域失败；
  - Lock 不等待立即返回：互斥、不可重入失败；
  - 同一内存实现不注入违约（删除不等待、写入查墓碑）时全部通过。
  
  “写入原子”是概率性检测：半成品窗口 50µs 时 5 次运行 5 次失败。
- `TestFileStoreConformance` 在 `-race -count=10` 下全部通过；`go vet ./...` 与 `scripts/verify_imports.sh` 无问题；`go test -race -count=1 ./core/... ./workflow/... ./builder/... ./tools/...` 通过；`go test ./...` 共 199 个包、5618 个测试通过。
- 未执行 golangci-lint：本机为 v1，仓库配置要求 v2。
