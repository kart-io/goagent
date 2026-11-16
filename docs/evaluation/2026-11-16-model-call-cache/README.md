# 模型调用缓存：记忆版本读取开销与整体验收

日期：2026-11-16　分支：feat/model-call-cache

## 1. 性能测量

基准：`builder/model_cache_bench_test.go` 的 `BenchmarkModelCacheVersion`。

### 环境

- CPU：Intel Core i7-14700KF，28 个逻辑核；运行时 `taskset -c 2,3,6-27`（核 0、8 有故障，已排除）。
- 存储：FileStore 目录为 `b.TempDir()`，`TMPDIR=GOTMPDIR=/tmp`，/tmp 是 tmpfs（内存），不是机械盘；因此 FileStore 的文件读写接近内存速度，生产用磁盘或 PostgreSQL 时 CacheVersion 的绝对耗时会更高。
- Go 1.27.3，`-benchtime=2s`，连续运行 3 次。（推断：样本量只有 3 次运行，表中范围足以判断量级，不足以做统计意义上的比较。）

### 数据规模

- 一个命名空间（t1/alice）下 1000 条长期记忆记录：中文内容（6 类句式轮换）加 UUID 编号，ID 为 UUID，重要度各不相同；非重复合成文本。
- 命中路径：先让一次未命中写入缓存，再用相同请求、新的会话日志反复执行 `Invoke`，模型零调用；每次迭代的 Agent 构建在计时之外。

### 结果（FileStore，3 次运行的范围）

| 项目 | 耗时 | 内存 | 分配次数 |
|---|---|---|---|
| `CacheVersion`（单个读命名空间） | 20.5 - 20.8 微秒 | 23.9 KB | 61 |
| 命中路径，带长期记忆（含 CacheVersion） | 200 - 214 微秒 | 531 KB | 1561 - 1563 |
| 命中路径，无长期记忆 | 21.1 - 22.2 微秒 | 16.6 KB | 213 |

### 结论

- CacheVersion 约占带长期记忆的命中路径的 10%（20.7 / 205 微秒）；其余约 90% 是长期记忆召回等与缓存无关的步骤（推断：主要是 1000 条记录的召回与历史注入，未单独度量），这部分在未命中时同样存在。
- 命中路径的缓存自身开销（无长期记忆那一行）约 22 微秒，相对一次真实模型调用（百毫秒到秒级）可以忽略。
- 未命中写入时，写后复核会多读一次 CacheVersion：FileStore 约 20 微秒，pgstore 是一次往返。只发生在写入路径，命中路径不受影响。
- CacheVersion 不取锁、只读 meta 与 head（推断：据此耗时与记录条数无关，只随读命名空间数量线性增长；本次只测了 1000 条、单个命名空间）。
- pgstore 组：pgstore 实现位于 goagent-contrib，本模块无法构造，基准中该组无条件 `b.Skip`；pgstore 组需在 goagent-contrib 环境中另行运行。本次未测，其结果可能因网络往返显著高于 FileStore。

复现命令：

```
TMPDIR=/tmp GOTMPDIR=/tmp taskset -c 2,3,6-27 go test -run '^$' -bench BenchmarkModelCache -benchmem -benchtime=2s ./builder
```

## 2. 整体验收

| 命令 | 结果 |
|---|---|
| `go build ./...` | 通过 |
| `go vet ./...` | 无问题 |
| `taskset -c 2,3,6-27 go test -race ./...`（TMPDIR=/tmp） | 退出码 0，74 个包 ok，无失败 |
| `make lint` | golangci-lint v2.15.0，0 issues |
| `scripts/verify_imports.sh` | 0 findings |

## 3. 下游编译差异

方法：为每个下游模块各写一份临时 go.work（use 该模块与 goagent），分别指向 master 的 worktree 与本工作区，执行 `GOWORK=<file> go vet ./...` 并比较；未使用 `-mod=mod`，未修改任何下游文件。覆盖 19 个模块：goagent-contrib 下的 acp、document、document/pdf、durable/pgstore、httpagent、management、management/harness、retrieval（及 cohere、qdrant、vertexai）、tools/database、tools/practical，以及 oncall-agent、goagent-examples（及 orchestration/protocol-agent、orchestration/httpagent）、goagent-harness（及 integration/postgres）。

结果：master 与本分支的输出逐模块完全一致，没有新增报错。

- 下游源码中没有 `CacheMiddleware`、`NewCacheMiddleware`、`DefaultMiddlewareCacheShards`、`MaxCacheShards` 的任何引用（grep 全部下游 Go 源码），因此没有需要列出的使用 `CacheMiddleware` 的位置；预期的破坏面在这些仓库中为空。
- `goagent-contrib/management/harness` 在 master 上就失败（依赖 goagent-harness 模块缓存版本的 `background/notify.go` 调用 `h.SteerOnce` 参数不符），两边一致。该失败会让类型检查止于依赖，因此对它补做了源码 grep，也没有引用。模块缓存中的 goagent-harness 各版本同样没有引用。
- 完成后已删除 worktree 与临时文件；下游四个仓库的 `git status` 与操作前一致（goagent-contrib 仍只有用户原有的未提交 `durable/pgstore/memory_test.go`）。

## 4. SimpleCache 并发写入：标签索引的锁开销

本分支给 SimpleCache 加了标签索引。最初 `Set` 也改为在 `tagMu` 下写入，并发写入因此退化 3.9 至 4.6 倍；工具缓存（`tools/middleware/caching.go`）只用 `Set`，也付出了这份代价。现在不带标签的 `Set` 恢复无锁的 `sync.Map.Swap`，只有覆盖了带标签的旧条目时才取锁，摘除旧条目留下的索引；`SetTagged`、`DeleteTag`、`Delete` 与过期清理仍在锁内。

基准：`cache/simple_cache_bench_test.go` 的 `BenchmarkSimpleCacheSetParallel`（`b.RunParallel`），配置与工具缓存相同（`WithoutBackgroundCleanup`、`WithMaxEntries(10000)`），1024 个键交错覆盖，不触发淘汰。master 对照在 `git worktree add /tmp/mc-master master` 上运行同一基准的 Set 子项（master 没有 SetTagged），测完已删除 worktree。

环境同第 1 节：`taskset -c 2,3,6-27`（24 个可用核），Go 1.27.3，`-count 5 -cpu 1,8,24`，默认 benchtime。

### 结果（5 次运行的范围，单位 ns/op）

| 子项 | GOMAXPROCS | master | 本分支（改前，全局锁） | 本分支（现在） | 现在 / master |
|---|---|---|---|---|---|
| Set | 1 | 142.0 - 143.8 | 152.4 - 153.7 | 144.3 - 158.7 | 约 1.0 - 1.1 倍 |
| Set | 8 | 44.6 - 48.5 | 206.6 - 209.9 | 51.9 - 63.1 | 约 1.2 - 1.3 倍 |
| Set | 24 | 59.3 - 63.1 | 233.6 - 237.4 | 65.0 - 77.1 | 约 1.1 - 1.2 倍 |
| SetTagged | 1 | — | 272.0 - 274.6 | 273.4 - 283.1 | — |
| SetTagged | 8 | — | 300.3 - 310.3 | 310.1 - 382.3 | — |
| SetTagged | 24 | — | 321.0 - 351.4 | 346.3 - 367.1 | — |

"改前"一列是上一轮的数据；master 与"现在"两列来自本轮，同一台机器上依次运行。内存：Set 在 master 为 136 B/op、4 次分配，本分支为 152 B/op、4 次分配（条目多了 tags 字段）；SetTagged 为 184 B/op、5 次分配。

### 结论

- 不带标签的 Set 在 8 核和 24 核下与 master 相差 1.1 至 1.3 倍（按区间两端算，最坏 1.41 倍），在 1.5 倍的目标以内；剩下的差距主要来自条目多出的 tags 字段，以及运行之间的波动（本分支 8 核 5 次运行的极差约 20%）。
- 带标签的写入仍然串行，开销与改前相同；只有模型调用缓存使用它，每次模型调用最多写一次。
- 正确性：带标签条目只在锁内写入，所以锁外任何可读的带标签条目都已登记在它的每个标签下，DeleteTag 会把它们删尽，不会漏删。无锁 Set 覆盖带标签条目后、取锁摘除旧索引之前，DeleteTag 可能多删这个不带标签的新值，这是可以接受的。并发测试是 `cache/tagged_test.go` 的 `TestSimpleCacheTaggedNoMissedDelete`。

复现命令：

```
TMPDIR=/tmp GOTMPDIR=/tmp taskset -c 2,3,6-27 go test -run '^$' -bench BenchmarkSimpleCacheSetParallel -benchmem -cpu 1,8,24 -count 5 ./cache/
```
