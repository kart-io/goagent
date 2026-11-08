# 持久事实记忆示例

此目录包含独立可选的 PostgreSQL/pgvector Provider。核心 GoAgent 只依赖 `memory` 包的公开接口；业务宿主可以直接实现 `MemoryService`，也可以把自有 `FactStore`、`MemoryIndex` 和 `EmbedderResolver` 注入默认服务。

## 运行持久化验收

需要 Docker 和本地 PostgreSQL/pgvector。测试不跳过：未设置 DSN 时会失败。

```sh
docker run --rm -d --name goagent-fact-memory \
  -e POSTGRES_PASSWORD=localtest \
  -p 55432:5432 pgvector/pgvector:pg16

cd examples/fact-memory/postgres
GOAGENT_PGSTORE_DSN='postgres://postgres:localtest@localhost:55432/postgres?sslmode=disable' go test -race ./... -count=1
```

测试创建独立随机 schema，并在退出时删除。案例覆盖真实 PostgreSQL 写入和独立进程重启召回、稳定幂等重放、删除后不可读、scope 隔离、并发 revision CAS、无正文变更链、签名分页游标跨重启续页与筛选绑定、清空封锁与恢复、pgvector tombstone、并发写入下的在线索引代际追平，以及 outbox 退避、租约恢复、多 Worker 领取互斥、失败终止和授权重试。核心模块 `go.mod` 不依赖 pgx 或 pgvector。

## 宿主组合方式

```go
store, err := postgres.Open(ctx, dsn, "agent_memory")
if err != nil { return err }
defer store.Close()
if err := store.Migrate(ctx); err != nil { return err } // 迁移由宿主显式触发

index, err := postgres.OpenIndex(ctx, dsn, "agent_memory")
if err != nil { return err }
defer index.Close()

service, err := memory.NewDefaultMemoryService(memory.MemoryServiceConfig{
    Store: store,
    Capabilities: store.Capabilities(),
    Limits: memory.DefaultFactLimits(),
    Index: index,
    Embedders: embedderResolver, // 宿主实现 memory.EmbedderResolver
})
if err != nil { return err }
worker, err := memory.NewFactWorker(memory.FactWorkerConfig{
    Store: store, Index: index, Embedders: embedderResolver,
    BatchSize: 64, Concurrency: 4,
})
if err != nil { return err }
```

由宿主启动 `worker.Run(ctx)` 并负责周期调度、关闭和租户业务权限。数据库 schema 名称必须符合 PostgreSQL 标识符约束；Provider 构造函数不会隐式执行迁移。`memory/conformance.Run` 可从外部 package 对自有 `MemoryService` 运行相同生命周期检查。

## 当前参考实现边界

Provider 已实现事实事务、审计、幂等记录、索引 outbox、作用域清理、签名且绑定查询条件的持久分页游标、带上限的退避/重试、向量检索和在线重建扫描、追平及代际切换。完整 PM-01～PM-35 conformance 覆盖、重建故障边界和 durable Run 端到端案例仍属于计划剩余工作；当前不得据此宣称完整验收通过。已执行案例和未覆盖要求见 `docs/evaluation/persistent-factual-memory/REPORT.md`。
