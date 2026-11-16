# 长期记忆分布式后端 实现计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 为长期记忆提供共享的 Redis 标签缓存与 pgvector 向量索引，默认部署仍为本地。

**Architecture:** goagent 侧收窄 ModelCache 的缓存接口为 `cache.Bytes`、改写 `cache.Tagged` 与 `memory.Index` 契约，并提供后端无关的一致性测试 `cache/cachetest`、`memorytest.RunIndex`；goagent-contrib 新增独立模块 `cache/redis`、`memory/pgvector` 与仅测试的 `integration/distributedmemory`，都跑同一套一致性测试，并用 Docker 起真实服务验证。

**Tech Stack:** Go 1.27、`github.com/redis/go-redis/v9`、`github.com/jackc/pgx/v5`、`github.com/pgvector/pgvector-go`、testify、Docker（`redis:7.4`、`pgvector/pgvector:0.8.x-pg17`）。

**Spec:** `docs/superpowers/specs/2026-11-16-distributed-memory-backends-design.md`（68fcf98e）。章节号 §x.y 均指该文档。

## Global Constraints

- 所有注释、错误信息、提交信息、文档用简体中文；标识符沿用项目约定。
- 测试一律 `taskset -c 2,3,6-27`，`TMPDIR=/tmp`；禁止 `-mod=mod`。
- goagent 主库不引入任何新外部依赖。
- goagent 工作分支 `feat/distributed-memory`（基于 `docs/distributed-memory-spec`），目录 `/home/hellotalk/code/go/src/github.com/kart-io/goagent`。
- contrib 工作在新 worktree `/home/hellotalk/code/go/src/github.com/kart-io/goagent-contrib-dm`，分支 `feat/distributed-memory`，基于 `origin/master`（ec87829）。不碰 `/home/hellotalk/code/go/src/github.com/kart-io/goagent-contrib` 主工作区（其分支 `feat/pgstore-snapshot-lz4` 与未跟踪文件 `durable/pgstore/memory_test.go` 属于用户）。
- contrib 新模块 `go 1.27.0`，`require github.com/kart-io/goagent v0.53.0`（发布后改为新版本）；开发期一律用 `GOAGENT_CORE_ROOT=/home/hellotalk/code/go/src/github.com/kart-io/goagent bash scripts/with-local-modules.sh <命令>` 联调。
- 外部服务测试在连接变量缺失时 `t.Skip`：`GOAGENT_REDIS_ADDR`（Cluster 为逗号分隔地址）、`GOAGENT_PGVECTOR_DSN`。
- Redis：最低 7；拒绝 `Ring` 客户端；键前缀默认 `goagent:cache:`；`DeleteBatch` 默认 500；`WaitReplicas` 默认 0、`WaitTimeout` 默认 1 秒；令牌 16 字节随机数。
- 一致性测试中 pgvector 与 `MemoryIndex` 的分数在 1e-6 内视为一致，不构造分差小于 1e-5 的近似并列。
- 每个任务结束 `gofmt -l` 无输出、`go vet` 通过；goagent 侧任务结束再跑 `golangci-lint run ./...`（另一个项目的 lint 并行时会以 exit 2 报 "parallel golangci-lint is running"，等待后重跑）。

## Review Focus

1. 键或标签文本含 `{`、`}`（Cluster hash tag）：键应正常分布、读写删正确 → Task 4 `TestKeyEscaping`。
2. 同一个键先带标签写、再无标签 `SetBytes` 覆盖、再 `DeleteTag`：新值可以被多删，但绝不能让旧的带标签值复活 → Task 2 cachetest 用例「无标签覆盖」。
3. `ttl <= 0` 传给 Redis `SetBytes`/`SetTagged`：返回 `CodeInvalidInput`，不写入永不过期的键 → Task 4 `TestInvalidTTL`。
4. 命名空间 `["alice"]` 与 `["alice2"]`、`["alice","x"]`：互不可见；组件含 NUL 或为空白时返回 `CodeInvalidInput`（pgvector 的 `text[]` 也存不了 NUL）→ Task 3 RunIndex 用例「精确命名空间」「非法命名空间」。
5. 换嵌入模型（维度变化）后 Search 用旧维度查询：只返回同维度向量、不报错 → Task 3 RunIndex 用例「不同维度共存」。

---

### Task 1: `cache.Bytes` 接口与 ModelCache 改用它

**Files:**
- Create: `cache/bytes.go`
- Modify: `cache/simple_cache.go`（新增 `GetBytes`、`SetBytes`）
- Modify: `builder/model_cache.go`（`Cache` 字段类型、`lookup`、`write`、`store`、godoc）
- Modify: 编译不过的调用方：`examples/memory/cache.go`（`untaggedCache` 改为嵌入 `cache.Bytes`）、`examples/builder/advanced/main.go`、`examples/integration/langchain-phase2/phase2_demo.go`、`builder/*_test.go`
- Modify: `CHANGELOG.md`（Unreleased 下写破坏性改动与迁移步骤，§4.4）
- Test: `cache/bytes_test.go`、`builder/model_cache_test.go`

**Interfaces:**
- Produces:
  ```go
  // cache/bytes.go
  type Bytes interface {
      GetBytes(ctx context.Context, key string) ([]byte, error) // 未命中返回 errors.ErrCacheMiss（github.com/kart-io/goagent/errors）
      SetBytes(ctx context.Context, key string, value []byte, ttl time.Duration) error
      Delete(ctx context.Context, key string) error
  }
  var _ Bytes = (*SimpleCache)(nil)
  ```
  `builder.ModelCache.Cache` 类型变为 `cache.Bytes`；`tagged()` 仍对它做 `cache.Tagged` 类型断言。

- [ ] **Step 1: 写失败测试** `cache/bytes_test.go`，表驱动 `TestSimpleCacheBytes`：
  - `SetBytes("k", []byte("v"), time.Minute)` 后 `GetBytes("k")` 返回 `[]byte("v")`；
  - 不存在的键 `errors.Is(err, agentErrors.ErrCacheMiss)`；
  - 过期后（ttl 10ms，等待 30ms）为未命中；
  - `Set("k", "str", ...)` 写入非字节值后 `GetBytes("k")` 返回错误且 `agentErrors.IsCode(err, agentErrors.CodeInvalidInput)`（按项目现有判错方式）。
  - `builder/model_cache_test.go` 新增 `TestModelCacheStoreWriteErrorDeletes`：用一个 `SetBytes`/`SetTagged` 总返回错误、记录 `Delete` 调用的假缓存，跑一次可缓存的模型调用，断言写入失败后对同一键调用过一次 `Delete`。
- [ ] **Step 2:** `taskset -c 2,3,6-27 env TMPDIR=/tmp go test ./cache/ ./builder/ -run 'Bytes|StoreWriteError'` → 编译失败（`GetBytes` 未定义）。
- [ ] **Step 3: 实现。** `SimpleCache.GetBytes` 复用 `Get` 的命中/过期/统计路径，值不是 `[]byte` 时返回 `CodeInvalidInput`；`SetBytes` 调用 `Set`。`ModelCache`：`lookup` 改用 `GetBytes`（删除 `string` 分支；`CodeInvalidInput` 走与解码失败相同的"按损坏删除"路径）；`write` 的无标签分支改用 `SetBytes`；`store` 在 `write` 返回错误时，发出原有 `model_cache_write_error` 事件后调用一次 `Delete(e.key)`（失败发 `model_cache_delete_error`）。`Cache` 字段 godoc 改为"缓存后端，只收发字节；EraseRequired 时还须实现 cache.Tagged"。
- [ ] **Step 4:** `taskset -c 2,3,6-27 env TMPDIR=/tmp go test -race ./cache/... ./builder/... ./examples/...` 通过；`go build ./...` 通过。
- [ ] **Step 5: 提交** `feat(cache)!: ModelCache 改用只收发字节的 cache.Bytes，外部缓存无法保存任意 Go 值，误用应在编译期暴露`

### Task 2: `cache.Tagged` 契约改写与 `cache/cachetest`

**Files:**
- Modify: `cache/tagged.go`（契约注释改为 §4.4 的 S1/S2）
- Create: `cache/cachetest/cachetest.go`
- Create: `cache/cachetest/simple_cache_test.go`
- Keep: `cache/tagged_test.go` 中依赖 SimpleCache 原子实现的"不误删"用例（`TestSimpleCacheTaggedNoMissedDelete`、`TestSimpleCacheUntagStaleKeepsCurrentTags`）原样保留；与通用套件重复的其余用例可删除。

**Interfaces:**
- Consumes: Task 1 的 `cache.Bytes`。
- Produces:
  ```go
  package cachetest
  type Cache interface{ cache.Bytes; cache.Tagged }
  // open 每次调用返回一个实例；同一个 t 内同名的实例共享数据（模拟两个进程），不同名互相隔离；每个子测试的数据初始为空。
  func RunTagged(t *testing.T, open func(t *testing.T, name string) Cache)
  // 供后端注入"SetTagged 写完条目、复查之前"的交错：未导出钩子由各后端自己的测试实现，cachetest 只做黑盒用例。
  ```

- [ ] **Step 1: 写套件**，`RunTagged` 以表驱动 `t.Run` 运行下列用例（a、b 为 `open(t,"x")` 两次，c 为 `open(t,"y")`）：
  - 「读写」：a 写 b 读到相同字节。
  - 「删除标签跨实例生效」：a `SetTagged(k, v, ttl, "s")`，b `DeleteTag("s")`，a、b `GetBytes(k)` 均为 `ErrCacheMiss`。
  - 「幂等删除」：不存在的标签、重复 `DeleteTag` 都返回 nil。
  - 「标签替换」：`SetTagged(k, v1, "s1")` 后 `SetTagged(k, v2, "s2")`；`DeleteTag("s1")` 后读到 v2 或未命中均可，`DeleteTag("s2")` 后必为未命中。
  - 「多标签只删其一」：`SetTagged(k, v, "s","m")`，`DeleteTag("m")` 后未命中。
  - 「无标签覆盖」：`SetTagged(k, v1, "s")`、`SetBytes(k, v2)`、`DeleteTag("s")` 后读到 v2 或未命中，绝不是 v1。
  - 「TTL」：ttl 50ms，等待 200ms 后未命中。
  - 「删除后新写入可读」：`DeleteTag("s")` 返回后再 `SetTagged(k, v, "s")`，读到 v。
  - 「并发不漏删」：8 个写者循环 `SetTagged(ki, vi, "s")` 并记录每次返回时刻；另一协程反复 `DeleteTag("s")`，每次记录开始时刻；结束后对每次删除，断言其开始前已返回的写入，若之后没有更晚的写入覆盖同一键，则在该次删除返回后读取为未命中（实现：最后一次删除完成后不再写入，断言全部键未命中；运行 2 秒或 `-short` 下 200ms）。
  - 「不同名隔离」：c 写入同名键与标签，a `DeleteTag` 不影响 c。
- [ ] **Step 2:** `cache/cachetest/simple_cache_test.go` 的 `TestSimpleCache` 用 per-t 的 `map[string]*cache.SimpleCache` 实现 `open`（`t.Cleanup` 里 `Close`），调用 `RunTagged`。
- [ ] **Step 3:** 改写 `cache/tagged.go` 契约注释：S1（必须）与 S2（实现须写明物理删除时机及例外的回收上限），删除"须用事务或脚本"的说法，写明可用令牌/版本校验（以 goagent-contrib `cache/redis` 为例）；SimpleCache 一句：在 tagMu 下写入与删除，S2 在 `DeleteTag` 返回时成立。
- [ ] **Step 4:** `taskset -c 2,3,6-27 env TMPDIR=/tmp go test -race -count=3 ./cache/...` 通过。
- [ ] **Step 5: 提交** `feat(cache): 提供后端无关的 Tagged 一致性测试，契约改写为不可读与物理删除两条性质，使令牌校验的外部后端可以满足`

### Task 3: `memory.Index` 维度契约、`RunIndex` 与分布式部署文档

**Files:**
- Modify: `memory/index.go`（接口注释、`MemoryIndex.Upsert` 去掉维度检查、`Search` 只考虑同维度向量）
- Create: `memory/memorytest/index.go`（`RunIndex`）
- Create: `memory/memorytest/memory_index_test.go`（`TestMemoryIndex` 调用 `RunIndex`）
- Modify/Test: `memory` 包内新增 `TestReindexAfterEmbedModelChange`（放在现有 indexing 测试文件旁，用包内已有的假嵌入器模式）
- Modify: `memory/README.md`、`docs/guides/CACHING_GUIDE.md`新增"分布式部署"小节，内容为 §4.6 五条 + 如何换成 Redis 与 pgvector（模块路径 `github.com/kart-io/goagent-contrib/cache/redis`、`.../memory/pgvector`）
- Modify: `CHANGELOG.md`（`memory.Index` 契约变更）

**Interfaces:**
- Produces: `func RunIndex(t *testing.T, newIndex func(t *testing.T) memory.Index)`；每次调用返回初始为空的独立索引。

- [ ] **Step 1: 写 `RunIndex`**，表驱动用例：
  - 「归一化」：写入 `[3,4]`，以 `[3,4]` 查询 Score 在 1e-6 内等于 1。
  - 「零向量与非有限值」：`[0,0]`、`[NaN,1]`、`[+Inf,1]` 的 `Upsert` 与 `Search` 均返回错误。
  - 「排序与并列」：`a=[1,0]`、`b=[1,0]`、`c=[0,1]`，查询 `[1,0]` k=3 → ID 顺序 `a,b,c`。
  - 「k 截断与 k<=0」：k=1 只返回 a；k=0、k=-1 返回空且无错误。
  - 「覆盖写入」：同 ID 再次 Upsert 后只出现一次、分数按新向量。
  - 「不同维度共存」：同一命名空间写 `x=[1,0]`、`y=[1,0,0]`，二者都成功；`Search([1,0])` 只返回 x，`Search([1,0,0])` 只返回 y；`IDs` 返回两者。
  - 「精确命名空间」：`["t","alice"]` 写 a，`["t","alice","sub"]` 写 b，`["t","alice2"]` 写 c：在 `["t","alice"]` 上 Search/IDs 只见 a；`Clear(["t","alice"])` 后 b、c 仍在。
  - 「非法命名空间」：`nil`、`["t"," "]`、`["t","a\x00b"]` 的 `Upsert` 返回 `CodeInvalidInput`。
  - 「删除」：`Delete` 后不再出现；删除不存在的 ID 返回 nil。
  - 「空命名空间」：`IDs` 与 `Search` 返回空、无错误。
- [ ] **Step 2:** `TestMemoryIndex` 调用 `RunIndex`；运行 → 「不同维度共存」失败（维度检查报 `CodeInvalidInput`）。
- [ ] **Step 3: 实现**：`Upsert` 删除维度一致检查；`Search` 跳过维度与查询不同的向量（不再报错）。接口注释改为"不同维度的向量可共存，Search 只考虑与查询同维度的向量"。
- [ ] **Step 4: 写 `TestReindexAfterEmbedModelChange`**：用模型名 `m1`、维度 2 的嵌入器写入记录并建索引；换成模型名 `m2`、维度 3 的嵌入器构造新的 `Memory`（同一 store、同一 `MemoryIndex`）；`Reindex(ctx,false)` 返回 nil；之后 `IDs` 中不再有 `m1` 写入的键，召回能按 `m2` 的向量命中该记录。
- [ ] **Step 5:** `taskset -c 2,3,6-27 env TMPDIR=/tmp go test -race ./memory/...` 通过。写文档小节。
- [ ] **Step 6: 提交** `feat(memory): 索引允许不同维度向量共存并提供 Index 一致性测试，持久化索引换嵌入模型后 Reindex 才能收敛`

### Task 4: contrib `cache/redis` 核心协议（单机）

**Files:**（均在 contrib worktree）
- Create: `cache/redis/go.mod`（module `github.com/kart-io/goagent-contrib/cache/redis`）、`redis.go`（Config、New、类型）、`keys.go`（键与转义）、`entry.go`（条目编码）、`tagged.go`（SetTagged、DeleteTag、GetBytes 校验）、`scripts.go`（Lua）
- Create: `cache/redis/redis_test.go`、`protocol_test.go`
- Create: `scripts/test-redis.sh`（本任务只做单机部分）

**Interfaces:**
- Consumes: `cachetest.RunTagged`、`cache.Bytes`、`cache.Tagged`。
- Produces:
  ```go
  package redis
  type Config struct {
      Client       goredis.UniversalClient
      Prefix       string        // 默认 "goagent:cache:"
      DeleteBatch  int           // 默认 500
      WaitReplicas int           // Task 5 实现
      WaitTimeout  time.Duration // 默认 1s，Task 5 实现
  }
  type Cache struct{ /* 未导出 */ }
  func New(ctx context.Context, cfg Config) (*Cache, error)
  // 方法：GetBytes、SetBytes、Delete、SetTagged、DeleteTag
  var hookAfterWrite func() // 未导出测试钩子：SetTagged 第 4 步之后、第 5 步之前调用；hookBetweenRemoveAndUnlink 同理供 DeleteTag 使用
  ```

协议严格按 §4.2「操作」与「成员登记脚本」实现，要点：
- `SetTagged` 五步顺序：取令牌（`SET g NX GET PX ttl`，已存在时单键脚本"PTTL < ttl 才 PEXPIRE"）→ `reg` → `SET e PX ttl` → `reg` → 复查；**每步完成后再发下一步**，同一步内多标签可 pipeline。
- `reg` 是单键 Lua：`TIME` 取服务端毫秒 → `ZREMRANGEBYSCORE t -inf now` → `ZADD t GT now+ttl key` → `PTTL(t) < ttl` 时 `PEXPIRE t ttl`。
- `DeleteTag`：`DEL g` → `ZSCAN` 分批，每批先 `ZREM` 完成后再 `UNLINK` 条目（按节点分组），不删整个 `t`。
- `GetBytes`：读条目、一步 pipeline 读各令牌，任一缺失或不等 → `DEL e` 并返回 `ErrCacheMiss`。
- 条目编码：1 字节版本号 + 标签数 + 每个（标签长度、标签、令牌）+ 值；格式由实现者定，须有版本前缀。
- 键与标签转义：`{`、`}` 编码后再拼键（如百分号编码 `%7B`/`%7D`，`%` 自身编码为 `%25`）。
- `ttl <= 0` 返回 `CodeInvalidInput`。
- `New`：`PING`；`INFO server` 解析 `redis_version` 主版本 < 7 拒绝；`*goredis.Ring` 拒绝。

- [ ] **Step 1: 写测试**（`GOAGENT_REDIS_ADDR` 缺失则 Skip；每个 open 用随机前缀实现隔离，同名共享前缀）：
  - `TestConformance`：`cachetest.RunTagged`。
  - `TestDeleteDuringWrite`：设 `hookAfterWrite` 在钩子里执行 `DeleteTag("s")`；`SetTagged` 返回 nil，之后 `GetBytes` 未命中，且 `EXISTS e:<key>` 为 0。
  - `TestConcurrentDeleteTagsKeepsNewMember`（§4.2 P2）：用 `hookBetweenRemoveAndUnlink` 让 D1 停在第一批之后；期间完成一次 `SetTagged(k,"s")`；启动 D2 并等它返回；放行 D1；断言 `EXISTS e:k` 为 0。
  - `TestWriteFailureLeavesNoReadable`：在第 3 步注入错误（钩子返回错误），`SetTagged` 返回错误；之后 `DeleteTag("s")`，`GetBytes` 未命中且 `EXISTS e:k` 为 0（因为第 2 步已登记）。
  - `TestTokenLossNoResurrection`：写入后直接 `DEL g:s`（模拟淘汰），`GetBytes` 未命中；再 `SetTagged` 另一键后旧键仍未命中。
  - `TestMemberSetBounded`：1000 次 ttl=50ms 的 `SetTagged`（不同键，同标签），等待 200ms 后再写一次，`ZCARD t:s` ≤ 1；`PTTL t:s` > 0。
  - `TestKeyEscaping`：键 `a{b}c`、标签 `{x}`，读写删正确；落在 Redis 中的键名不含 `{`。
  - `TestInvalidTTL`：`SetBytes`/`SetTagged` 传 0 返回 `CodeInvalidInput`，`DBSIZE` 不变。
  - `TestPrefixIsolation`：两个前缀互不影响。
- [ ] **Step 2:** `scripts/test-redis.sh`：与 `test-pgstore.sh` 同构，`docker run -d --rm --name goagent-redis-test-$$ -p 127.0.0.1:${REDIS_TEST_PORT:-56379}:6379 redis:7.4`，等 `redis-cli ping` 为 PONG，导出 `GOAGENT_REDIS_ADDR`，`go vet ./... && go test -race -count=1 "$@" ./...`，trap 删除容器。运行 → 失败（未实现）。
- [ ] **Step 3: 实现**上述协议。
- [ ] **Step 4:** `cd <worktree> && GOAGENT_CORE_ROOT=<goagent> bash scripts/with-local-modules.sh taskset -c 2,3,6-27 env TMPDIR=/tmp bash scripts/test-redis.sh` 全部通过；连续跑 3 次无偶发失败。
- [ ] **Step 5: 提交** `feat(cache/redis): 提供 Redis 版标签缓存，令牌校验使删除立即对所有实例生效且在 Cluster 下成立`

### Task 5: `cache/redis` 前提检查、WaitReplicas 与 Cluster

**Files:**
- Modify: `cache/redis/redis.go`（New 前提检查）、`tagged.go`（DeleteTag 第 1 步的 WAIT）
- Create: `cache/redis/cluster_test.go`、`wait_test.go`
- Modify: `scripts/test-redis.sh`（`--cluster`）

**Interfaces:** Consumes Task 4 的 `Config`、`New`、`Cache`；不新增导出符号。

要点（§4.2「前提」「复制与持久化」）：
- `New` 拒绝：`*goredis.Ring`；`ClusterOptions` 中 `ReadOnly`/`RouteByLatency`/`RouteRandomly` 任一为真；`FailoverOptions` 的 `ReplicaOnly`、`RouteByLatency`、`RouteRandomly`（用客户端 `Options()` 取得）；单机客户端 `ROLE` 首元素不是 `master`；Cluster 对每个主节点不检查（客户端只路由到主节点）。`CONFIG GET maxmemory-policy` 成功且不是 `noeviction` 时用 `log/slog` 记一条告警；命令出错（托管服务禁用）静默跳过。
- `WaitReplicas > 0`：取 `g:<tag>` 所在主节点（Cluster 用 `MasterForKey`，单机/哨兵直接用客户端），在其 `Conn()` 单连接上 pipeline `DEL g` + `WAIT n timeoutMs`；返回的确认数 < n 时返回错误（包含确认数）。

- [ ] **Step 1: 写测试**：
  - `TestNewRejects`（表驱动）：Ring 客户端、`ReadOnly` 的 Cluster 客户端、连接到从节点（用 `REPLICAOF` 起第二个容器不现实时，可用 `ROLE` 被替换的假客户端；以能测为准）均返回错误。
  - `TestWaitReplicasNoReplica`：单机 `WaitReplicas=1, WaitTimeout=100ms`，`DeleteTag` 返回错误，且 `GetBytes` 已是未命中（令牌已删，S1 仍成立）。
  - `TestClusterConformance`（`GOAGENT_REDIS_CLUSTER=1` 时运行，地址取 `GOAGENT_REDIS_ADDR`）：`cachetest.RunTagged` + `TestDeleteDuringWrite` 同款 + 跨槽的 1000 个键全部被 `DeleteTag` 删除（扫各主节点 `SCAN` 前缀为 0）。
  - `TestClusterWaitPausedReplica`：`WaitReplicas=1`，对令牌主节点的从节点 `CLIENT PAUSE 2000 WRITE`，`DeleteTag` 返回错误。
- [ ] **Step 2: 脚本 `--cluster`**：6 个 `redis:7.4` 容器（`--network host` 不可用时用自建网络 + `cluster-announce-ip 127.0.0.1` 与端口映射 57001–57006），`redis-cli --cluster create ... --cluster-replicas 1 --cluster-yes`，轮询 `cluster info` 直到 `cluster_state:ok`，导出 `GOAGENT_REDIS_ADDR=127.0.0.1:57001,...`、`GOAGENT_REDIS_CLUSTER=1` 再跑测试。
- [ ] **Step 3: 实现。**
- [ ] **Step 4:** 单机与 `--cluster` 两种模式都通过（命令同 Task 4，加 `--cluster`）。
- [ ] **Step 5: 提交** `feat(cache/redis): 拒绝会读到滞后令牌的客户端配置并支持等待从节点确认删除，降低主从切换让已删内容复活的窗口`

### Task 6: contrib `memory/pgvector`

**Files:**
- Create: `memory/pgvector/go.mod`、`schema.go`、`pgvector.go`、`pgvector_test.go`
- Create: `scripts/test-pgvector.sh`

**Interfaces:**
- Produces:
  ```go
  package pgvector
  const SchemaVersion = 1
  const Schema = `...` // 建 goagent_memory_vectors_schema(version) 与 goagent_memory_vectors，不含 CREATE EXTENSION
  type Config struct{ Pool *pgxpool.Pool }
  type Index struct{ /* 未导出 */ }
  func New(ctx context.Context, cfg Config) (*Index, error) // 检查 vector 扩展已安装、SchemaVersion 一致
  var _ memory.Index = (*Index)(nil)
  ```
- 表结构见 §4.3（`ns text[]`、`id`、`dim`、`vec vector`，主键 `(ns, id)`）。`Search`：`WHERE ns = $1 AND dim = $2 ORDER BY vec <=> $3, id LIMIT $4`，Score = `1 - 距离`。写入前在 Go 侧归一化并校验零向量/非有限值（与 `MemoryIndex` 同样的错误码）。向量参数用 `pgvector.NewVector`，不要求宿主在连接池上注册类型。扩展缺失时错误信息包含 `CREATE EXTENSION vector`。

- [ ] **Step 1: 写测试**：`TestConformance` 调用 `memorytest.RunIndex`（每个子测试 `TRUNCATE goagent_memory_vectors`）；`TestNewWithoutExtension`（在未安装扩展的独立数据库中 `New` 返回包含 `CREATE EXTENSION vector` 的错误）；`TestSchemaVersionMismatch`。
- [ ] **Step 2:** `scripts/test-pgvector.sh`：与 `test-pgstore.sh` 同构，镜像 `pgvector/pgvector:0.8.0-pg17`（拉取失败时用可用的 0.8.x 标签并写进脚本），端口 `${PGVECTOR_TEST_PORT:-55434}`，执行 `CREATE EXTENSION vector` 后导出 `GOAGENT_PGVECTOR_DSN`；测试自己执行 `Schema`。
- [ ] **Step 3: 实现。**
- [ ] **Step 4:** `GOAGENT_CORE_ROOT=<goagent> bash scripts/with-local-modules.sh taskset -c 2,3,6-27 env TMPDIR=/tmp bash scripts/test-pgvector.sh` 通过。
- [ ] **Step 5: 提交** `feat(memory/pgvector): 提供 pgvector 版共享向量索引，多实例召回能看到其他实例写入的向量`

### Task 7: 端到端集成模块与 contrib 文档

**Files:**
- Create: `integration/distributedmemory/go.mod`（仅测试，不发布）、`e2e_test.go`
- Create: `scripts/test-distributed.sh`（同时起 Redis 与 pgvector 容器，运行本模块）
- Modify: contrib `README.md`（模块表三行、测试小节）

**Interfaces:** Consumes Task 1 的 `builder.ModelCache`、Task 4 的 `redis.New`、Task 6 的 `pgvector.New`、contrib `durable/pgstore`。

- [ ] **Step 1: 写测试** `TestForgetErasesOtherInstanceCache`：两套 `memory.Memory`（同一 pgstore、同一 pgvector 索引）与两个 `builder.ModelCache`（同一 Redis、同一 `TagKey`、`Erase: EraseRequired`，各自装配 `Eraser()` 到 `OnErase`），用一个计数调用次数的假模型：
  - 进程乙发起调用 → 模型调用 1 次；进程甲同请求 → 命中（总调用仍为 1）；
  - 进程甲 `Forget` 该命名空间的记录 → 擦除完成后，Redis 中带该命名空间标签的条目 `EXISTS` 为 0；进程乙再次调用 → 模型调用 2 次。
  - 另一个用例 `TestRecallSeesOtherInstanceVectors`：甲写入记录并建索引，乙的召回结果包含该记录且带向量分。
- [ ] **Step 2:** 运行 → 通过前先确认失败原因是断言而非连接；实现所需装配代码（仅测试代码）。
- [ ] **Step 3:** `GOAGENT_CORE_ROOT=<goagent> bash scripts/with-local-modules.sh taskset -c 2,3,6-27 env TMPDIR=/tmp bash scripts/test-distributed.sh` 通过；再跑 `GOAGENT_CORE_ROOT=<goagent> taskset -c 2,3,6-27 env TMPDIR=/tmp bash scripts/test-all.sh`（无外部服务时新模块全部 Skip、其余模块通过）。
- [ ] **Step 4: 提交** `test(integration): 用真实 Redis 与 pgvector 验证一个实例遗忘后其他实例的缓存被物理删除`

## 发布（全部任务通过、整体审核无问题后；用户已授权"没有问题就发布新版本"）

1. goagent：`feat/distributed-memory` 合并到本地 master，跑全量测试与 lint；`CHANGELOG.md` 把 Unreleased 改为 `v0.54.0`；打 tag `v0.54.0`；推送 master 与 tag（推送前向用户报告即将推送的提交数与 tag）。
2. contrib：新模块 `go.mod` 改为 `github.com/kart-io/goagent v0.54.0` 并 `go mod tidy`（`GOWORK=off`、`GOFLAGS=-mod=mod` 不使用；用 `go get github.com/kart-io/goagent@v0.54.0`）；不用工作区再跑一遍三个测试脚本；提交，合并到 contrib master 并推送；新模块按 contrib 既有惯例打模块 tag（`cache/redis/v0.1.0`、`memory/pgvector/v0.1.0`）。
