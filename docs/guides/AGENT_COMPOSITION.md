# Agent、workflow 与运行中输入

GoAgent 提供通用执行基础。宿主决定任务路由，Runnable 统一承载 Agent、工具与流程；不要求所有任务先经过一个主 Agent。

## 选择入口

| 情况 | 建议 |
| --- | --- |
| 用户信息不足 | 宿主返回澄清问题，保留业务状态，补齐后再路由 |
| 意图明确、步骤固定 | 直接调用编译后的 workflow Runnable，无需新增模型请求 |
| 工具选择需要运行时探索 | 调用 Agent，由模型在本次允许的工具中选择 |
| 主任务需要专业子 Agent | 将子 Agent 的 Runnable 适配为工具，经 Dispatcher 暴露给父 Agent |
| 多个独立工作可并行 | 使用有界 Graph.AddParallel；显式 Clone/Merge，明确分支失败规则 |
| 多个 Agent 按顺序、循环或分流协作 | 使用 `multiagent` 的 Sequential、Loop、RouterSupervisor，见[多 Agent 预置模式](MULTI_AGENT_PATTERNS.md) |
| 需要人工审批与恢复 | 使用 DurableGraph/AsChildRunnable、Resumable 工具及宿主提供的日志和恢复输入 |

路由规则与置信阈值属于业务。不要把路由结果直接当成工具授权；真正执行仍需校验与准入。`workflow.Graph.AddBranch` 可用于类型化的分流，未知意图应有显式澄清或拒绝路径。

## 同一种流程可以直接运行或暴露为工具

已编译 Graph 实现 Runnable，可直接 Invoke，也可传给 `tools.NewRunnableTool`；提供给 Agent 时使用 `dispatcher.Tools()`。可编译、可运行的离线示例见 [ExampleNewRunnableTool_workflow](../../tools/runnable_tool_example_test.go)：

```sh
go test ./tools -run '^ExampleNewRunnableTool_workflow$' -v
```

例子直接提供工具调用参数，不发模型请求。真实子 Agent 使用 `child.AsRunnable()` 适配，并显式声明 `ModelInvoking: true`，提供共享根预算。需要每次独立会话与资源时用 `NewRunnableToolFactory`；不要在并发请求间意外共享可变 Agent/会话状态。

父子 Dispatcher 使用独立实例以避免祖先占槽时重入死锁，根预算与限制向下传递。详细配置见[可组合编排](COMPOSABLE_ORCHESTRATION.md)。

## 可恢复子流程不等于后台任务服务

`DurableGraph.AsChildRunnable()` 使用父操作日志的 Store 和派生执行键；外层工具需声明 `Resumable: true`。审批等待返回 `durable.WaitError`，父 Builder 以 `builder.ErrWaiting` 暂停，不能把等待当作模型可见的成功工具结果。

宿主从等待路径取得续跑定位信息，完成业务鉴权后用 `durable.WithResume` 提供批准。审批键是恢复凭据，不是用户有权审批的证明。恢复时保持执行键、作用域、版本和输入意图稳定。可参考[实际嵌套审批回归](../../builder/nested_resume_test.go)及[图入口](../../workflow/README.md)。

只有所有效果都记在正确派生键下的可恢复流程才能声明 Resumable。普通外部 API、命令或包含未记录模型请求的闭包不能直接打开它来获得安全重试；pending 未知结果应先 Reconcile 或交宿主处理。

这些机制不提供后台任务租约、持久消息投递或跨主机接管。后台子 Agent 需要独立任务身份、所有者、取消策略、预算、状态查询、结果投递与回收；由宿主调度系统管理。

## Handle 的输入与完成边界

| 方法 | 领取时机 | 可能保留未领取输入的情况 |
| --- | --- | --- |
| Steer | 下一步；结束检查时可唤醒有剩余额度的循环 | 最后一轮、结束检查之后到达、失败或取消 |
| Inject | 随下一步，不主动唤醒 | 当前答复结束且没有下一步 |
| Redirect | 非持久模型请求中取消并重新请求；其他情况转 Steer | 失败或无法再发请求；转为 Steer 后按其规则 |
| Followup | 本轮成功后，队列合并成下一轮输入 | 本轮失败或取消 |
| Interrupt | 合作式取消 | 不等于所有外部副作用都已停止 |

入队返回 nil 不代表处理完成。使用以下模式在运行收敛后处理仍留在队列的输入：

```go
out, err := handle.Wait(waitCtx)
pending := handle.PendingInputs()
// err/out 表示本次运行结果；pending 仅表示尚未被循环领取的输入。
// 宿主决定展示或安排新执行，不能据此自动重试已发生的业务效果。
```

PendingInputs 是独立只读快照，调用不消费输入；同一队列有序，跨队列没有全局到达顺序。运行中也可查询，但后续会变化。Wait 的 context 只控制等待，超时不取消 Handle；只有实际运行结束后的快照才稳定。需要中止时显式 Interrupt，Handle 会等本轮 ExecuteStream 退出，包括其同步执行的工具与清理。工具和钩子必须合作返回，Wait 可用独立的有界 context；等待超时不能视为清理完成。执行源返回的错误与取消原因均保留在错误链中；Handle 在生产端关闭后读取私有终态，不依赖可能被取消丢弃的公开 error 事件。公开 ExecuteStream 的取消事件仍为 best-effort。

Handle 收件箱是进程内对象。要求跨重启可靠接收输入时，宿主先持久化收件箱及接收 ID，再管理投递与确认。已领取输入即使请求失败也不再出现在 PendingInputs；该接口不提供“未成功处理的全部消息”或 exactly-once。

Followup 的派生执行键只标识轮次内的持久操作，不会持久保存轮次调度。若第 1 个 Followup 在审批处等待，重启后仅 `Start(ctxWithResume, 原输入)` 会回放第 0 轮并结束，不会自动进入该 Followup。宿主必须按原轮次与原合并批次重建 Followup 链，并在对应前一轮结束前入队；不同批次划分可能导致恢复意图不匹配。`WithResume` 提供审批凭据，不重建调度。完整验证见[Handle 生命周期回归](../../builder/handle_lifecycle_test.go)。

回放可能立即完成，不能假设在 Start 返回后紧接着调用 Followup 一定赶得上。宿主需要显式同步边界（回归中用 RequestHook 协调）保证批次重建；固定多轮任务更适合用持久 Graph 显式编排，避免依赖 goroutine 调度。Handle 的成功返回只描述本次实际重建的轮次，不证明宿主持久收件箱中的全部工作已完成。

## 区分三种终态

- `AgentEvent.done`：一次 ExecuteStream 调用完成，含中间件处理后的最终 Output。text/tool_end 不是最终答案或完整运行终态。
- `Handle.Wait`：全部 Followup 轮次结束，或运行失败/暂停；返回最后一轮结果。ErrWaiting 仍需宿主续跑。
- `DurableGraph.AsRunnable` 的流 Done：本次调用结束；业务是否完成检查 GraphSnapshot.Done，Waiting 表示仍在等待。

工具并发执行时，GoAgent 保持模型调用顺序的结果、tool_end 和 OnResult 处理，不复制 Pi 的完成顺序事件语义。更多见[执行契约](EXECUTION_CONTRACT.md)。

## 会话与外部环境各自管理

`WithSessionLog` 记录模型历史和 history/rewrite；PreStep 改写后续上下文，Offload 将长结果外置并按引用读取。UI 详情、凭据和审计信息不应全部进入模型上下文；脱敏时检查文本与结构化副本。会话日志的重写记录可能包含旧内容，不等于删除历史秘密。

`session.Store.Fork` 复制历史事件，不撤销工具副作用。宿主分别维护 SessionID、RunID/ExecutionKey、子任务身份和 SandboxID；从旧历史继续时明确选择原环境或新的环境/产物。不要更换执行键来绕过 pending 或重复执行业务。

定位和后续准入见[Pi 核心采纳设计](../design/PI_CORE_ADOPTION.md)及[能力对照](../research/2026-11-01-pi-core-gap-analysis.md)。
