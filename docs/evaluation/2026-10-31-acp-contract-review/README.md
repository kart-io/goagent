# ACP 契约调研与设计修订验证

本目录形成于主仓只有 ACP 设计文档的调研阶段。随后拉取 contrib 确认已有 ACP 实现，并已在其基线上落实剩余修复，见[执行记录](../../../../goagent-contrib/docs/ACP_CONTRACT_FIXES.md)。本目录提供独立探针，验证 SDK 行为与修订设计的存储前提，不作为 ACP 端到端验收。

```sh
python3 docs/evaluation/2026-10-31-acp-contract-review/reproduce.py
```

要求 Python 3、可用的 Go/race 工具链以及 Go 模块源访问。脚本使用临时模块，固定 SDK v0.13.5，通过本地 replace 引用当前 GoAgent 工作树，不改主项目依赖；进程结束自动删除临时目录。

- `TestObserveReplacementPromptLosesCancellation`：成功观察旧请求返回后新请求无法取消的 SDK 缺陷，PASS 不表示契约正确。
- `TestObserveCloseDoesNotCancelPrompt`：确认 SDK 不替适配器取消关闭会话的任务，同样属于缺陷/行为观察。
- `TestMetadataPersistsWithoutChangingHistory`：确认私有元数据事件跨 FileStore 重开仍存在，且不进入 DeriveMessages；不代表 ACP 的列表、分页或元数据追加实现已完成。

[最初 SDK 探针日志](result.log)保留调研原始结果；[修订验证日志](revision-verification.log)记录三项探针的 `go test -race` 结果。

## 修订交付与边界

| 原调研发现 | 已修订的设计要求 | 实施任务 |
| --- | --- | --- |
| 替换请求丢失取消 | 适配层拥有独立 token/cancel，按 token 退出 | 2 |
| close 不取消及资源泄漏 | 关闭状态机、等待执行停止、OnClose 逆序释放与超时责任 | 2、4 |
| 长命令清理期限过期 | 回收阶段才创建 5 秒清理 context，错误可见 | 6 |
| 文件转发与写入契约不一致 | 双能力转发、副作用前校验、父目录、版本错误、Server 内协调 | 5 |
| done-only 答案遗漏 | 最终输出补齐、草稿改写明确标识、取消优先 | 3 |
| 重启后 cwd 丢失 | 持久元数据、严格筛选与游标分页 | 7 |

上述修订分别落实到[设计规格](../../superpowers/specs/2026-10-31-acp-agent-design.md)和[实施计划](../../superpowers/plans/2026-10-31-acp-agent.md)。新计划移除旧完整代码草稿，以明确接口、断言和逐项失败/通过验证步骤替代。

本轮检查了两份文档的本地链接、代码围栏、8 项任务覆盖、误勾选与已知旧错误示例；`git diff --check` 通过。本目录探针不替代产品测试；执行阶段另已运行 contrib 的实际 ACP 契约测试与 examples 集成，真实编辑器联调尚未进行，未修改第三方 SDK。未提交、推送或发布。
