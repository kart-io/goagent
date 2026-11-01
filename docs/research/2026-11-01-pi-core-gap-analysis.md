# Pi Agent 对照：GoAgent 核心缺口与采纳范围

日期：2026-11-01。GoAgent 基线 `fcff360`，Pi 固定源码 `2c8d1feb147f6b3d97e855da803bfe7b19f3c403`。本文只描述该快照，不推断未来版本或 npm 发布状态。相关 package.json 标注 1.4.0。原 `badlogic/pi-mono` 在调研时跳转到 `earendil-works/pi`。

## 先明确比较对象

Pi 编码 CLI 使用 AgentSession + pi-agent-core + 树形 SessionManager；同仓 pi-durable 是实验性独立运行时，依赖 pi-ai/Chord，不是 CLI 默认会话层的别名。pi-env 提供远端 ExecutionEnv，不分配容器或 VM。不能把所有包拼成一个已统一采用的产品架构。

固定来源：[项目说明](https://github.com/earendil-works/pi/blob/2c8d1feb147f6b3d97e855da803bfe7b19f3c403/README.md)、[AgentSession](https://github.com/earendil-works/pi/blob/2c8d1feb147f6b3d97e855da803bfe7b19f3c403/packages/coding-agent/src/core/agent-session.ts)、[Durable 依赖](https://github.com/earendil-works/pi/blob/2c8d1feb147f6b3d97e855da803bfe7b19f3c403/packages/durable/package.json)、[远端环境](https://github.com/earendil-works/pi/blob/2c8d1feb147f6b3d97e855da803bfe7b19f3c403/packages/env/README.md)。

## 补齐之前调研中容易遗漏的现有能力

| 能力 | GoAgent 代码证据 | 差距与处理 |
| --- | --- | --- |
| 把 Agent/函数/图作为工具 | [RunnableTool](../../tools/runnable_tool.go) | 已支持；复用泛型 Runnable，不创建专用 Agent 类型体系 |
| 按调用创建独立资源 | 同文件的 Factory、OwnedRunnable；[调用作用域](../../tools/call_scope.go) | 已有深度、预算前提和有界清理；不是后台任务所有权 |
| 可恢复嵌套流程 | [OperationPolicy](../../core/durable/operation.go)、[AsChildRunnable](../../workflow/durable_runnable.go)、[嵌套审批验证](../../builder/nested_resume_test.go) | 已支持稳定子键、waiting、恢复和 Reconcile；不能整体标成缺失 |
| 插话与下一轮输入 | [Handle](../../builder/handle.go) | 已有四种控制输入；本轮增加未领取快照并修复失败清空 Followup |
| 事件与并发 | [工具循环](../../builder/tool_events.go)、[并发回归](../../builder/parallel_tools_test.go) | 已有有界并发及确定结果顺序；不改成 Pi 的默认并发策略 |
| 模型历史与压缩基础 | [会话日志](../../builder/session_log.go)、[循环钩子](../../builder/loop_hooks.go)、[结果外置](../../builder/contextpolicy/offload.go) | 已有日志派生历史、history/rewrite、PreStep 和不可变结果引用；业务摘要策略由 Harness 装配 |
| 会话分支 | [Store.Fork](../../core/session/store.go) | 已支持历史复制；树形导航、标签及外部效果派生由宿主实现 |
| 持久执行 | [durable](../../core/durable/operation.go) | 已有具名操作回放；不是保存 Go 栈，也不自动使外部操作幂等 |

Harness 的 preset 子 Agent 默认禁用子层 journal，仅代表那一种装配模式。它不否定 GoAgent 核心的 Resumable 工具和嵌套持久图。另一方面，有了嵌套图也不等于已经提供独立 child run 调度平台。

## 本轮真实发现并补齐的缺口

Handle 入队方法返回 nil 仅代表接收进程内输入。失败路径以前把全部 Followup 从队列取出后直接结束，尚未执行的输入消失；此外，Inject 不唤醒、Steer 在最后一轮或结束检查之后到达，本来就可能没有下一次领取机会。

修复失败路径，只有开始下一轮才领取 Followup；新增只读 `PendingInputs` 供宿主查看仍在队列的输入。保持现有预算和完成语义，不为了消耗剩余队列偷偷创建新执行。已领取输入即使处理失败也不会重新出现在快照中，因此它不是业务投递确认或重试清单。设计见[采纳边界](../design/PI_CORE_ADOPTION.md)，用法见[组合指南](../guides/AGENT_COMPOSITION.md)。

## 后续事项按层归属

| 事项 | 归属与前提 |
| --- | --- |
| 独立持久子任务的父子关系、前后台、取消与等待 | 基于现有 durable 定义可选契约；先有真实宿主需求与失败恢复规格 |
| 跨主机接管、租约、后台投递 | 宿主或成熟工作流系统；不恢复已移除的实验性 distributed/multiagent 全家桶 |
| 业务意图、澄清、workflow/Agent 路由 | 宿主与 goagent-examples；核心保留 AddBranch/Runnable 组合基础 |
| 会话树 UI、编码工具、扩展资源装配 | goagent-harness/宿主 |
| 远端 FS 批量协议、沙盒回收与资源台账 | goagent-harness 可选模块；cwd 不等于安全根目录 |
| 工具发现、Code Mode | 后置可选适配；所有实际调用仍经过授权、预算与重放策略 |

Pi 的关键启发是明确循环与产品层、模型投影与持久历史、子工作所有权与会话分支。会话回到旧分支并不撤销文件、Git、数据库或外部 API。safe 重跑策略也不保证外部 exactly-once。

来源：[Pi 工具循环](https://github.com/earendil-works/pi/blob/2c8d1feb147f6b3d97e855da803bfe7b19f3c403/packages/agent/src/agent-loop.ts)、[会话树](https://github.com/earendil-works/pi/blob/2c8d1feb147f6b3d97e855da803bfe7b19f3c403/packages/coding-agent/src/core/session-manager.ts)、[Durable 与子 Agent](https://github.com/earendil-works/pi/blob/2c8d1feb147f6b3d97e855da803bfe7b19f3c403/packages/durable/README.md)、[环境语义](https://github.com/earendil-works/pi/blob/2c8d1feb147f6b3d97e855da803bfe7b19f3c403/packages/env/docs/semantics.md)。

## 验证边界

本轮验证 GoAgent 的实际执行链与新增输入契约，见[报告](../evaluation/2026-11-01-pi-core/REPORT.md)。前轮在 Harness 调研中对固定 Pi 源码选择运行过 230 项测试；该结果不代表 GoAgent 已具备 Pi 的所有产品能力。本轮不重新宣称全量 Pi CLI、模型供应商、远端 daemon 或分布式后台调度经过验收。
