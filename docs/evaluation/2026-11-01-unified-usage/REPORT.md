# 统一 Token 计量与归属验证

日期：2026-11-01。基线 `c089f05`。环境 Go 1.25.0，darwin/arm64。

## 变更

按用户要求删除兼容模式，所有预算采用一种字段证据和归属模型：

- 移除 AttributionVersion、AttributionEnabled、双格式选择与 Builder 中的对应分支。
- 唯一持久格式标记 goagent-model-budget；Config.Version 仅保留宿主策略指纹，不决定实现。
- Entries 的值对象保存 Estimate、Evidence、AgentCallKey；删除重复的 Estimates/Evidence/AttemptOwners 映射。
- Reserve/ReserveEstimated/RecordUsage 只转发同一证据路径并继承 context lineage；直接调用无 Agent 上下文时明确 Unattributed。
- 绑定预算的 Builder 自动创建身份，未命名角色为 anonymous；WithAgentID 用于命名角色，调用键仍独立。
- Snapshot 与 Attribution.Root 共用 rootSnapshot。partial 默认完整保留，不再有降级分支。MaxAgentCalls 默认1000。

## 回归过程

默认预算保存 partial 的回归先失败：旧实现将其保留为整条 unknown，见[RED](red-partial.log)。统一后同一输入保留 Prompt=10、Total未知，通知提交状态与账本一致。

预算侧默认单一证据测试先RED，见[预算 RED](red-budget.log)。集成时还发现便利 ReserveEstimated 曾遗漏 context lineage、证据登记报 attempt owner changed；新增便利 API 回归后修复为继承相同链，见[链路 RED](red-context.log)。

一个旧恢复 fixture 的图 RunID 与预算 RootKey 不同，以前因绕过身份建立而未暴露。现在对齐 fixture 的同一逻辑根，不放松冲突校验；跨重启的两 Agent 结果、工具效果与费用仍正确隔离。

旧格式和旧模型意图明确拒绝，不转换、不覆盖原数据、不重新发送模型请求。保留既有记录的限制属于格式边界，不保留另一套读写实现。

## 验证

所有命令使用前缀 `env -u GOROOT GOTOOLCHAIN=go1.25.0`。

| 命令 | 结果 | 证据 |
| --- | --- | --- |
| `go test ./...` | 根模块通过，未变更包可能使用缓存 | [suite.log](suite.log) |
| `go test -race ./builder ./core/usage ./core/budget ./core/observe ./tools ./execution ./llm` | 通过 | [race.log](race.log) |
| 归属、旧意图拒绝与子进程恢复聚焦 race 重复5轮 | 通过 | [stress.log](stress.log) |
| `go run ./examples/agent-usage` | 首次与回放 Root=18、父自身9、子4/5、父子树18，请求5且unknown1 | [example.log](example.log) |
| 统一固定账本基准 | 已执行 | [benchmark.log](benchmark.log) |
| 格式、链接、diff、独立只读审核 | 通过，无未处理实质发现 | [verification.log](verification.log) |

统一测试覆盖 partial/未知字段、已知 Total 差额、Owner 冲突、空链不可改已有归属、默认节点上限、不可变快照、并发、保存失败及子进程退出。Snapshot 与归属根汇总一致；保存模型结果后登记前退出，重启不重复发送。

## 成本与边界

本机统一账本样本：1000条 RecordEvidence 约52µs、230KB、9次分配；10000条约522µs、1.84MB、37次分配。值对象避免逐条指针复制及多套映射；事务仍克隆并校验，成本随账本规模增加。这是本机样本，不是性能保证。

只支持当前格式，不提供自动迁移；已有旧持久运行需由原实现或宿主明确处置，不能更换执行键重跑副作用。Observer 仍可丢事件；可靠累计查同一预算账本。未报告字段、未归属调用和真实货币费用仍需分别处理。

当前 API 见[指南](../../guides/MULTI_AGENT_USAGE.md)，本轮计划见[统一计划](../../superpowers/plans/2026-11-01-unified-usage-accounting.md)。
