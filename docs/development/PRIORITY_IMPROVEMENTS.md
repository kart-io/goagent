# 当前优化与验收清单

基线：`1cba257`，Harness 拆分后的 GoAgent 执行内核。设计与计划见 [execution-hardening 设计](../superpowers/specs/2026-10-30-execution-hardening-design.md) 与 [计划](../superpowers/plans/2026-10-30-execution-hardening.md)。

| 任务 | 交付 | 验收 |
|---|---|---|
| T1 | 初始化严格传播错误，可选严格记忆模式 | 全入口读写失败矩阵、typed-nil、回调与错误链 |
| T2 | Pipe/Sequence共同流终态、取消、父回调与Batch | 畸形流、消费者取消、有序部分结果、克隆隔离 |
| T3 | Runner/Graph只读诊断 | 未完成不判失败、显式操作证据、版本障碍、无写入 |
| T4 | 保守Token预留、工具次数、根期限 | 旧JSON/ledger/intent恢复、并发、重启、未知用量、配置漂移 |
| T5 | 两阶段准入、有界排队、异步观察、授权TTL | 审批不占执行槽、排队超时、丢弃关闭、过期回收 |
| T6 | 三Provider工具流契约 | 完整参数、finish reason、中断取消、未知usage、429分类 |
| T7 | 扩展契约、治理与发布文档 | 全量race、导入检查、Harness跨仓验证与独立审阅 |

执行入口见 [执行契约](../guides/EXECUTION_CONTRACT.md)，扩展行为见 [扩展契约](../guides/EXTENSION_CONTRACT.md)，运维接入见 [治理指南](../guides/EXECUTION_GOVERNANCE.md)。

不新增分布式调度器、默认Token估算或模型价格表；真实业务成功率和生产负载仍由宿主数据验证。
