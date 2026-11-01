# Agent 钩子与 Token 计量审核验证

日期：2026-11-01。代码基线 `de6c61a`。这是实施前审核记录；后续实现见[交付报告](../2026-11-01-multi-agent-usage-implementation/REPORT.md)。环境 Go 1.25.0，darwin/arm64。

## 本轮完成

- 审核 Builder、RunnableTool、Dispatcher、budget、Observer、execution.Scope 的实际调用链。
- 新增父 Agent + 两个子 Agent + 汇总的真实组件离线测试，覆盖内存预算与持久预算/回放。
- 补充[能力审核](../../research/2026-11-01-agent-hooks-usage-audit.md)、[技术设计](../../design/MULTI_AGENT_HOOKS_AND_USAGE.md)和[实施计划](../../superpowers/plans/2026-11-01-multi-agent-hooks-usage.md)。

没有修改生产 Go 代码、依赖或默认行为；设计中的 WithAgentID、逐尝试用量事件、v2 归属账本及 Attribution 查询尚未实现。不能把下面现有能力测试通过理解成新 API 已可用。

## 已执行验证

Go 命令统一使用前缀 `env -u GOROOT GOTOOLCHAIN=go1.25.0`。

| 命令 | 结果 | 证据 |
| --- | --- | --- |
| `go test -race ./builder -run '^TestMultiAgentUsageRootIsNotSumOfNestedOutputs$' -count=1` | 内存与持久两种情况均通过 | [multi-agent.log](multi-agent.log) |
| `go test -race ./builder ./core/budget ./core/observe ./tools ./execution` | 通过；未变更包可能使用缓存 | [race.log](race.log) |
| 文档本地链接、格式与 diff 检查 | 通过 | [verification.log](verification.log) |

受控模型报告父分析 3、子 A 4、子 B 5、父汇总 6。实际根账本 18，父输出自身用量 9；四次模型调用，不因子工具结果中的 usage 再次累加。持久回放账本不变，父客户端两次调用、每个子客户端一次。

本轮针对性包回归覆盖既有重试、未知用量、模型结果补登记、审批/子 Agent 调用和 observer 行为；没有重新跑根模块全套或实际供应商计费验证。

## 独立设计审核

只读设计审核提出四项实质边界，已补入设计和计划：

1. v2 同一账本保存零模型消耗的中间 Agent 节点及完整祖先链，节点数量有界；不能仅靠有费用的叶节点重建树。
2. 预留之外，回放及证据登记同样校验 Owner；整体工具回放保留历史归属，宿主改子配置须提升工具版本。
3. 非 Scope 入口的根 RunID 来源、冲突拒绝、Agent Visit 与观察操作 Visit 的关联，以及旧同步入口的能力边界。
4. partial 字段单调补齐、已知冲突拒绝，恢复交付来源不改变原始证据或重复增加修订。

以上是对待实施规格的修订，不是声称相关未来恢复路径已验收。后续按 U1～U4 分阶段实施，根累计继续复用现有预算，不增加第二套计费或多 Agent 运行时。
