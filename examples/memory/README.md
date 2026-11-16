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
| A 隔离与删除 | `isolation.go` | 未绑定命名空间时 8 个方法与 3 个记忆工具全部拒绝；同一 ID 在 4 个命名空间互不相干（含 `alice`/`alice2` 前缀）；回答期间 `Forget`，本轮任务被丢弃、抽取模型一次也不调用；回答期间 `Purge`，旧任务消失、本轮任务丢弃、删除日志有记录、其他用户不受影响；`Hydrate` 换命名空间返回 `ErrMemoryNamespaceChanged`，遗忘后只还原未遗忘的引用 |
| B 抽取管线 | `extraction.go` | 会话 A 抽取的事实在会话 B 以 `RecallPrefix` 召回块出现，会话 B 的日志里没有召回正文；同一任务重复登记只处理一次，失败后重试；`update` 取代旧记录并继承来源，`noop` 不写入；模型持续失败与非 JSON 回复按 `JobAttempts` 标记 failed，非法候选丢弃，整理期间目标被并发修改时降级并计入 `Stats.Conflicts`；中止轮次只写一条 `reflection`，抽取请求里没有召回块与 `memory_search` 的调用和结果 |
| C 缓存一致性 | `cache.go` | `ModelCache` 的 `EraseRequired` 构建校验（TTL、Tagged、TagKey、OnErase）；`Forget`/`Rewound`/`Purge` × 三种擦除装配的表驱动用例：删除后都不命中，只有 `EraseAuto`+`OnErase` 物理清除条目；`Rewound` 切开一轮后旧记录恢复、新记录失效、为可见部分补登任务 |
| D 预算与保留 | `budget.go` | 核心记忆总在召回块最前；`RecallRunes` 按 rune 截断且块内容与 `Retrieval.Records` 一致；`Weights` 改变排序，两个半衰期后新近性为 1/4 但记录仍在；TTL、`MaxRecords` 淘汰（不动钉住的）、失效保留期后物理删除；换嵌入模型后旧向量不被采用，`Reindex` 后恢复，遗忘后向量键被清理 |

`main_test.go` 另以表驱动测试验证并发写入的不变量：同一 ID 并发 `Remember`、同一 `Revision` 并发 `Update` 都只有一个成功，其余得到 `CodeConflict`。

## 使用时要注意的地方

示例运行中发现的、文档没有写明的行为：

- 模型调用缓存要求客户端能给出身份：`ScriptedClient` 没有模型名，必须实现 `CacheIdentity() string`（见 `common.go` 的 `cachedClient`），否则缓存静默不生效。
- 抽取任务的 Key 形如 `会话ID/turn/start 事件序号`，例如 `s-forget/1`，中间没有字面的 `turn`。
- `RecallRunes` 只约束条目部分，`RecallPrefix` 与固定说明行（约 59 个 rune）不计入，召回块总长会超过 `RecallRunes`。
- 宿主按契约在 `session.Rewind` 之后调用 `Rewound`，即使没有记录受影响也会改变记忆版本、清除该命名空间的缓存条目，因此带长期记忆时"重新生成"不会命中缓存。场景 C 为了演示命中，重新生成时只退回会话日志。
- `ProcessPending` 的返回值包含处理时被丢弃的任务。
- 回复不是 JSON 时，Worker 在同一次尝试内会再请求一次让模型重写，每次尝试最多两次模型请求。
- `EraseOff` 且 TTL 超过 `EraseWithin` 时构建会输出一条 WARN 日志，属于预期输出。
