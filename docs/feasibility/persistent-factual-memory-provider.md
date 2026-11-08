# 持久事实记忆 Provider 可行性记录

日期：2026-11-08。状态：已确定参考 Provider 路径，持久实现与跨进程验收在 T5 完成。

## 结论

GoAgent 核心保持 Provider 无关；在 `examples/fact-memory/postgres` 增加独立 Go module，作为契约与端到端验收使用的 Postgres Provider。模块使用 pgx v5.10.0，Go 版本与核心一致，不被核心 `go.mod` 引用。其接口实现通过公开的 `memory` 契约，不导入核心内部包。

## 代码依据

- 同一代码库的 `goagent-contrib/durable/pgstore` 已使用 pgx v5.10.0 和 Go 1.27，并提供 Postgres 连接池、事务、schema 与持久化测试模式。
- 该现有模块持久化 durable session/run，不具备事实正文、来源、作用域过滤、幂等 tombstone、索引 outbox、事实变更链、重建与清空任务；不能直接充当 MemoryService Provider。
- GoAgent 核心当前未依赖 pgx；因此 Provider 放在示例下的嵌套 module，不改变核心依赖图。

## 选择理由与代价

Postgres 事务适合原子提交 Fact、幂等摘要、ChangeRecord 和 IndexMutation；既有 pgx 使用经验可复用。该参考模块增加独立 schema、数据库集成测试与本地 Postgres 环境要求；它只证明接口可持久实现，不成为所有 GoAgent 用户的默认数据库。

## 验收条件

- 通过唯一 schema 前缀隔离测试数据，初始化/清理是显式命令或 helper，不在构造器中隐式迁移。
- 使用真实 Postgres；环境缺少 DSN 时测试失败，不将跳过记为通过。
- 用独立进程完成写入、关闭、重启、读取、幂等重放、DeleteScope 恢复和索引重建。
- `go list -deps ./...` 检查核心模块不依赖 pgx；嵌套 Provider 单独运行 race 测试和 conformance suite。

当前 `GOAGENT_PGSTORE_DSN` 未设置；Docker 可用。T5 负责启动临时 Postgres、执行上述非 SKIP 持久验收并记录准确命令和结果。
