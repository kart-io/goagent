# GoAgent 优化示例

| 示例 | 说明 |
| --- | --- |
| [02-cache-presets](02-cache-presets/) | 用 `ShardedToolCache` 的预设工厂创建高性能或低内存的工具缓存 |

CoT 与 ReAct 对比、Planning 与混合模式示例依赖的推理策略与 `planning` 包已在 v0.17.0 删除，需要时可从 `v0.16.1` 查看。任务编排请改用原生工具循环与 `workflow` Graph，见[原生编排](../../docs/guides/NATIVE_ORCHESTRATION.md)。
