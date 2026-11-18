# memory

离线演示长期记忆（`memory` 包）的边界，每个边界都有断言，期望不符时示例以非零状态退出并打印实际值。抽取模型与主模型由 `testing/mocks.ScriptedClient` 按脚本回答，嵌入由确定性的假 Embedder 给出，存储用 `durable.NewFileStore` 放在临时目录。不需要 API 密钥、网络或 Docker。

```bash
go run ./examples/memory
go test -race ./examples/memory
```

模型回复是脚本，示例只证明管线与边界逻辑，不代表真实模型的抽取或召回质量。召回质量的评测见 `memory/memeval`。

## 场景

| 场景 | 文件 | 验证的边界 |
| --- | --- | --- |
| A 隔离与删除 | `isolation.go` | 未绑定命名空间时 8 个方法与 4 个记忆工具全部返回 `CodeInvalidInput`，绑定后同样的写入成功；同一 ID 在 4 个命名空间互不相干（含 `alice`/`alice2` 前缀）；回答期间 `Forget`，本轮任务被丢弃、抽取模型一次也不调用；回答期间 `Purge`，旧任务消失、本轮任务丢弃、删除日志有记录、其他用户不受影响；`Hydrate` 换命名空间返回 `ErrMemoryNamespaceChanged`，遗忘后只还原未遗忘的引用 |
| B 抽取管线 | `extraction.go` | 会话 A 抽取的事实在会话 B 以 `RecallPrefix` 召回块出现，会话 B 的日志里没有召回正文；同一任务重复登记只处理一次，失败后重试；`update` 取代旧记录并继承来源，`noop` 不写入；模型持续失败与非 JSON 回复按 `JobAttempts` 标记 failed，非法候选丢弃，整理期间目标被并发修改时降级并计入 `Stats.Conflicts`，target 不存在时计入 `Stats.InvalidTargets`；中止轮次只写一条 `reflection`，抽取请求里没有召回块与 `memory_search` 的调用和结果 |
| C 缓存一致性 | `cache.go` | `ModelCache` 的 `EraseRequired` 构建校验（TTL、Tagged、TagKey、OnErase）；构建失败时断言 `CodeInvalidConfig` 与原因关键字；`Forget`/`Rewound`/`Purge` × 三种擦除装配的表驱动用例：删除后都不命中，只有 `EraseAuto`+`OnErase` 物理清除条目；`Rewound` 切开一轮后旧记录恢复、新记录失效、为可见部分补登任务 |
| D 预算与保留 | `budget.go` | 核心记忆总在召回块最前；整个召回块（含块头）不超过 `RecallRunes` 且按 rune 计，块内容与 `Retrieval.Records` 一致；`Weights` 改变排序，两个半衰期后新近性为 1/4 但记录仍在；TTL、`MaxRecords` 淘汰（不动钉住的）、失效保留期后物理删除；换嵌入模型后旧向量不被采用，`Reindex` 后恢复，按 `append(ns, "#"+scope)` 的布局核对遗忘后向量键被清理 |

`main_test.go` 另以表驱动测试验证并发写入的不变量：同一 ID 并发 `Remember`、同一 `Revision` 并发 `Update` 都只有一个成功，其余得到 `CodeConflict`。

## 使用时要注意的地方

- **场景 C 的"重新生成"违反宿主契约，不要照抄。** `common.go` 的 `regenerate` 只调用 `session.Rewind`，故意不调用 `Memory.Rewound`，用来得到缓存命中的正向对照。宿主在 `Rewind` 之后必须调用 `Rewound`，见 `eraseOps` 的 `Rewound` 行；这样做之后带长期记忆的重新生成不会命中缓存。
- `ScriptedClient` 没有模型名，示例用 `cachedClient` 声明 `CacheIdentity()` 才能使模型调用缓存生效，规则见 [CACHING_GUIDE](../../docs/guides/CACHING_GUIDE.md)。
- 任务 Key 格式、非 JSON 回复的修正重试、Worker 回复格式、`Stats.Conflicts` 与 `InvalidTargets` 的区别、向量键布局，见 [memory/README](../../memory/README.md)。
