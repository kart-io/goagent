# GoAgent 设计概述

当前公共架构见 [架构概述](../architecture/ARCHITECTURE.md)。本页说明已有机制的设计选择，不把研究计划描述为已实现 API。

## 组合是基础

以 `core.Runnable[I,O]` 统一函数、Agent 与工作流的执行。Builder 的 `AsRunnable()` 返回 `Runnable[any,*builder.AgentOutput]`；`core.Agent` 则使用 `*core.AgentInput/*core.AgentOutput`，两套输出不是同一个类型。异构组合应显式转换。

旧 `interfaces.Agent/Runnable` 保留兼容，不再作为新编排入口。Builder 的旧 Execute 保留同步、不隐式调工具的语义，不能替代统一执行路径。

## 模型循环与状态图并存

Agent 循环适合动态选择工具与专业探索。Graph 适合固定步骤、条件路由、有限循环、并行汇合与审批。两者可以互相组合：Agent 成为图节点，Workflow 或子 Agent 适配成工具。

本项目支持状态图思想，但不声称 LangGraph API 或调度语义兼容。Graph 当前是单一控制路径加显式并行节点，没有通用字段级 reducer、运行时 Send 或任意位置 interrupt 的等价公共 API。

## 显式状态与身份

- 泛型状态由业务定义，普通节点返回整个状态；并行 Clone/Merge 由宿主实现。
- SessionID 标识对话；RunID/稳定作用域/CallID 标识可恢复执行，不能混作可任意复用的用户输入字段。
- 图拓扑自动形成指纹，闭包代码与外部配置不能自动求完整身份，宿主须维护 DurableConfig.Version。
- 宿主 Scope 管理派生上下文，不关闭借用的 Store、Observer 或 Tracer。

## 执行成功不等于已回滚或永不重跑

持久模型与工具回放避免再次请求已保存的完成记录；结果未知时需要核对。外部系统的幂等、协调和补偿仍由业务负责。任意节点函数、回调或中间件不因处于图内就自动获得 exactly-once 效果。

审批是可恢复等待，Stream Done 只结束本次调用。中间文本和 Preview 不表示业务完成。提前停止消费流必须取消 context，忽略 context 的第三方实现不能被强制终止。

## 扩展点有明确职责

| 扩展点 | 作用 |
| --- | --- |
| `PreStep` / `WithIdentifiedPreStep` | 请求前改写消息或结束本步；持久模式记录结果，身份变化拒绝旧意图 |
| `RequestHook` | 调整请求/客户端；持久模式要求确定性 |
| `RequestError` / `RetryTransient` | 有界恢复请求错误；持久运行只允许原样重试，各尝试分别占预算 |
| `TurnStopping` | 在轮次结束前提供续跑输入，仍受步骤预算限制 |
| `tools.Pipeline` | 工具准入、审批、守卫、Around/Post 等执行包装 |
| `core/middleware` | 请求处理链，可改输入输出或短路；其效果不是自动持久事务 |
| `core/observe` | 只读观察；不是恢复账本或业务决策源 |

`WithCommunicator` 当前仅保存 metadata，不提供内置 Agent 消息总线。角色、能力路由和待澄清任务由宿主/Harness 定义。

## 依据与验收

实现依据：[Runnable](../../core/runnable.go)、[Builder](../../builder/builder.go)、[循环钩子](../../builder/loop_hooks.go)、[Graph](../../workflow/graph.go)、[持久图](../../workflow/durable.go)、[Dispatcher](../../tools/dispatcher.go)。

现有历史设计与评测保留当时的版本、术语和路径。当前签名以源码与 [API 参考](../api/CORE_API.md) 为准，迁移见 [上下文策略](../guides/CONTEXT_POLICY.md) 和 [Harness 迁移](../guides/HARNESS_MIGRATION.md)。
