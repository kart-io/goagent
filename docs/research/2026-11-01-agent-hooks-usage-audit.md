# Agent / 子 Agent 钩子与 Token 计量审核

日期：2026-11-01。基线：`de6c61a`。本文是实施前审核快照；后续已落地能力见[指南](../guides/MULTI_AGENT_USAGE.md)。范围：GoAgent 核心；没有把 Harness preset 的装配限制等同于核心能力。

## 1. 当前结论

已有可恢复 Agent 组合与根级 Token 预算设计，但不是完整的按 Agent 归属报表。当前用法应将同一 budget.Controller 放在父 context，让后代继承；根账本是累计依据，不能累加父子所有输出或观察事件。

| 审核项 | 证据 | 结论 |
| --- | --- | --- |
| 子 Agent 工具化 | [RunnableTool](../../tools/runnable_tool.go)、[调用作用域](../../tools/call_scope.go) | 支持共享 Runnable、按调用 Factory、深度限制、根预算前提和有界清理 |
| 持久子流程 | [模型作用域](../../builder/durable_turn.go)、[嵌套审批](../../builder/nested_resume_test.go) | 稳定子执行键、审批等待、可恢复流程已有；不等于独立后台调度服务 |
| 根请求与 Token 预算 | [Controller](../../core/budget/controller.go) | 并发共享、预留、已知/未知证据、持久恢复与去重已有 |
| 失败和重试计量 | [modelOperation](../../builder/model_call.go)、[重试回归](../../builder/model_retry_test.go) | 实际重试使用新尝试键；失败已知用量保留；回放不是新费用 |
| 本层输出与根累计 | [工具循环](../../builder/tool_events.go)、[finishEvents](../../builder/events.go) | 本层 TokenUsage 不包含后代；root_budget 累计整个共享根，二者不能相加 |
| 控制钩子 | [Builder 钩子](../../builder/loop_hooks.go)、[Pipeline](../../tools/pipeline.go) | 请求前调整、失败恢复、工具准入与结果钩子已有；不是通用逐请求用量完成钩子 |
| 只读观察 | [Observer](../../core/observe/event.go)、[异步观察](../../core/observe/async.go) | 支持同根 OTel 父子 span、运行/操作/访问身份；事件不含 Token 与显式 Agent 角色/逻辑父调用身份 |
| 身份装配 | [execution.Scope](../../execution/scope.go) | ConfigureBuilder 目前设置 SessionID 和 DurableScope；没有独立通用 AgentID/ParentAgentCallKey |
| 字段完整性 | [llm.Usage](../../llm/stream_client.go)、[TokenUsage](../../interfaces/token.go) | 流协议仅输入/输出/总量；统计结构有 CachedTokens，但当前转换路径无法从 llm.Usage 取得它。未报告不能当作缓存用量为零 |

`ModelInvoking: true` 是可信宿主声明和预算前提检查，不是自动捕获隐藏 SDK 请求的探测器。Factory 若抛弃父 context 或换根 Controller，框架不能保证所有外部调用都被统计。各子 Agent 自有 RequestError 等控制钩子；context Observer 与根预算可继承。

当前接入方式：宿主创建 `budget.New` 或 `NewDurable`，通过 `budget.WithController` / execution.Scope 绑定根 context；子 Agent 始终使用传入 context。执行结束后调用同一个 Controller 的 Snapshot，分别展示 KnownTokens、UnknownUsageEntries 和预留量。失败与取消也应查询，不只读取成功输出；查询使用独立有界 context。若 Handle.Wait 自身超时，快照只是时点状态，不能当作最终账单。详细接入见[根预算指南](../guides/COMPOSABLE_ORCHESTRATION.md)。

## 2. 本轮验证的真实组合

新增 [TestMultiAgentUsageRootIsNotSumOfNestedOutputs](../../builder/multi_agent_usage_audit_test.go)：实际父 Builder 经 Dispatcher 同轮调用两个子 Builder，再汇总。子工具结果刻意包含其 TokenUsage，验证不会再次加到账本。

- 父分析 3，子 A 4，子 B 5，父汇总 6。
- 父输出 TokenUsage 为 9；根账本为 18，4 次请求，4 条已知证据。
- 持久运行用同一执行键重新构建父 Agent 回放，账本不变，父客户端仍只请求 2 次，各子客户端只请求 1 次。
- 同时覆盖内存与持久预算，原有模型操作/重试/未知证据测试补充验证失败场景。

这是受控模型流与真实执行组件的离线验证，不验证供应商账单或自定义客户端隐藏调用。完整命令及日志见[验证记录](../evaluation/2026-11-01-agent-hooks-usage/REPORT.md)。

## 3. 缺少的设计与风险

1. **归属缺口**：根 Entries 的尝试键不可逆，当前 Snapshot 不能查询某个 Agent/子树的 Self 用量；不能靠名字、SpanID 或根快照差值可靠补出。
2. **钩子缺口**：Request 在请求前、RequestError 在失败时、OnResult 在工具结果交付时，均不能覆盖每次模型尝试的成功/失败/取消用量。成功后的流 usage 是累计快照，也可能因取消丢失。
3. **角色与调用混用风险**：同名 Agent 可并发多次；DurableScope 负责命名空间，SessionID 负责对话，不能统称 AgentID 作为去重依据。
4. **统计与可靠记账混用风险**：AsyncObserver 可以丢事件，回放可以重新发事件；直接对钩子做累加会漏算或重复算。
5. **费用精度边界**：缓存、部分字段、未知、预估和实际报告需区分；Token 总量本身不足以给出精确货币费用。
6. **生命周期边界**：子 Agent 自身结束早于其外层 OwnedRunnable.Close；完整子工具结束才包含该适配器清理。不能把 child agent_end 当作资源全部释放。

没有发现需要为上述目标新增多 Agent 调度框架的理由。新增能力应复用已有 Observer 与预算账本，精确设计见[多 Agent 钩子与归属](../design/MULTI_AGENT_HOOKS_AND_USAGE.md)。该文的新 API 当前尚未实施。
