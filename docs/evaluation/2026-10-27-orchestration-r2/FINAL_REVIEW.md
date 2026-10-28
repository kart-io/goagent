# R2 整分支独立审查与修复

> 清理说明：本目录的测试日志及 Python 脚本已删除；历史测试结论和现存 JSON/Markdown 汇总保留。

审查范围：`b60e033..72f67f0`，由一次独立上下文审查完成；原结论为 With fixes。无 Critical、无另列 Minor。依执行协议，四个 Important 统一在一轮修复，先验证失败、后验证通过，不重复派发审查。

| 问题 | 最终行为 | RED→GREEN 测试 |
| --- | --- | --- |
| 旧 Execute/非流式 ExecuteWithTools 忽略显式预算 | 请求前拒绝，提示统一入口；未绑定预算的旧同步行为保留 | TestLegacyEntriesRejectBoundRootBudgetBeforeRequest |
| JSON 深复制改变 RawMessage，identity 策略也失败 | 分别复制消息、ToolCalls、ProviderState 对象及字节切片；保留空白和转义 | TestIdentityPolicyPreservesOpaqueProviderBytes |
| 策略可改写与原生续接状态绑定的助手正文 | 带 ProviderState 的消息 Content 受保护，在下次准入前失败 | TestPolicyRejectsBoundAssistantContentBeforeAdmission |
| 恢复后重用 RunID 下的 EventID/VisitID | 每次观察调用创建独立 invocation nonce；业务 RunID/OperationKey 不变 | TestDurableResumesHaveDistinctObservationIdentities |

上述回归均经历先失败、修复后通过的验证；对应原始日志已清理。全量 race、build、lint 和 imports 的最终结果见 [交付记录](REPORT.md)。

审查明确保留的边界及维护者裁决：

1. 外部 SDK 隐藏请求/重试与自定义模型调用由宿主控制；统一入口预算不会自动拦截。错误外推会漏计实际请求。
2. 子 SessionID 唯一性与预算/结果后端的实际归属由宿主构造校验；泛型接口无法证明物理后端。误配会混用记忆或运行范围。
3. 回调、Source、Close、Writer 必须合作响应 context；无法强行终止不合作函数。误用可能阻塞清理或关闭。
4. 可恢复子工作流、嵌套审批与外部 exactly-once 在本轮后置；opaque 工具不假装恢复 pending。错误解除一次尝试约束会重复副作用。
5. 本地模型与微基准不能证明真实 Provider 质量或生产性能；工具循环开销已披露。将其外推到线上会导致错误选型。

独立审查将 `new_model_invocations` 视为文档所述根 context 执行范围的计数，没有将其列为每个子对象自己的局部计数保证。审查没有修改工作区。
