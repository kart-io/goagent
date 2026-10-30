# 根预算、资源治理与运行诊断

## 根预算

`core/budget.Config` 在原请求次数上增加以下可选字段，零值禁用：

| 字段 | 语义 |
|---|---|
| `MaxReservedTokens` | 宿主估计值的累计准入上限，不是实际账单硬上限 |
| `TokenPolicyVersion` | 开启Token限额时必填，标识宿主估计规则 |
| `MaxToolCalls` | Dispatcher实际调用工具体的总次数；回放和准入拒绝不消耗，重试是新执行 |
| `DeadlineUnixMilli` | 根任务的绝对截止时间（Unix毫秒） |

```go
controller, err := budget.NewDurable(budget.Config{
    RootKey: requestID, Version: "task-v1", MaxModelRequests: 20,
    MaxReservedTokens: 100000, TokenPolicyVersion: "host-estimator-v1",
    MaxToolCalls: 30, DeadlineUnixMilli: time.Now().Add(5*time.Minute).UnixMilli(),
}, journal)
if err != nil { return err }
runCtx, cancel := budget.Bind(ctx, controller)
defer cancel()
agent, err := builder.NewSimpleBuilder(client).
    WithTokenEstimator(hostEstimate).WithTools(dispatcher.Tools()...).Build()
```

`hostEstimate` 的类型为 `builder.TokenEstimator`，参数是只读模型请求及工具列表；必须包含输入、工具定义和最大输出的保守估计。没有默认近似算法；开启Token限额却缺少估计函数、返回零/负数或估计失败时，网络调用前拒绝。每次重试独立预留；已完成操作回放不重新估计。

直接准入可调用 `ReserveEstimated(ctx,key,tokens)`；旧 `Reserve` 在Token限额关闭时保持原行为。无论实际用量较少、取消或崩溃，都不退还预留。`Snapshot.ReservedTokens` 是估计总量，`KnownTokenDelta` 是已知实际Token减去对应预估，仅统计有usage证据的条目；未知用量继续计入UnknownUsageEntries，不能当作零。实际用量超过估计时仍记录真实值，不篡改证据。

根预算在子调用间共享；独立子任务配额和金额定价由宿主基于业务分配，本版不引入价格表或假定模型费率。工具计数以实际经过Dispatcher调用边界为准，嵌套工具边界各自计数，绕过Dispatcher的函数不自动计数。

`Bind` 把根期限传播给整个图、回调和子任务。只用 `WithController` 时Builder和Dispatcher仍会为自身执行绑定期限，但任意宿主图节点与外部回调要获得同一期限，应使用Bind。第三方仍须响应context。

持久运行必须配持久Controller，包括纯工具任务；内存预算不能在重启后保留额度。新增Config字段的零值JSON被省略，旧ledger与completed模型intent保持可恢复；启用或改变新限制、估计版本时拒绝旧运行，需新RootKey/版本，不能偷偷修改原账本。

## Dispatcher 的两阶段准入

- `MaxConcurrency`（默认4）只约束实际执行，不再被审批等待占用。
- `MaxAdmissionConcurrency`（默认0，不限）独立约束校验及Pipeline准入；需控制大量待审批请求的宿主应显式配置。
- `MaxQueued`（默认0，不限）限制两阶段共用的阻塞等待人数；立即取得空槽不占排队名额。超限返回可errors.Is匹配的 `tools.ErrDispatcherBusy`。
- 两段排队仅受宿主/根预算ctx约束。校验、Authorize、日志和执行累计消耗单次Timeout；Pipeline准入与等待执行槽暂停计时。取得槽位后再次检查取消，避免迟到执行。
- 回放、准入拒绝不需要执行槽；取消或排队失败不新建pending操作。第三方不响应context不能靠超时强行停止。

审批不再占执行槽是有意的语义修复。若宿主依赖旧MaxConcurrency同时限制待审批人数，应新增MaxAdmissionConcurrency；它们不保证公平调度。

## 异步观测与授权回收

`observe.NewAsyncObserver(sink,capacity,workers)` 返回实现Observer的适配器。队列有界、入队不等待sink、worker数固定；workers>1时sink须支持并发。`Dropped()`统计队列溢出、关闭后提交及取消关闭时丢弃的排队事件；sink panic被隔离。

宿主须调用 `Close(ctx)`：正常关闭先停止接收再排空；ctx取消时取消sink context并返回，之后可再次Close等待。忽略context的sink仍占原worker，框架不额外创建替代goroutine，也不能强杀它。异步事件保留上下文值并脱离原调用取消，不作为持久审计或计量证据。

`Pipeline.GrantTTL` 设置新会话授权自批准起的有效期，<=0保持不过期。过期授权即使尚未清理也不再生效；`Grants`过滤过期项；宿主定期调用 `PruneGrants()` 回收过期授权和空会话，返回删除的授权条数。`RevokeGrants(sessionID)`显式撤销；清理不修改在途审批。

## 持久运行的只读报告

`Runner.Diagnose(ctx,runID,refs...)` 与 `DurableGraph.Diagnose` 返回 `durable.DiagnosticReport`，可直接JSON编码。报告包含位置、状态、障碍与建议动作，不包含业务状态、工具结果、审批键或原始存储错误。显式 `OperationRef{ParentKey,ID}` 可加入已知操作；不自动遍历未知子任务。

| 报告状态 | 可以得出的结论 |
|---|---|
| `not_found` | 未找到该运行或引用，应核对标识 |
| `completed` | 已保存的续跑位置为完成 |
| `approval_waiting` | 检查点等待宿主审核与批准 |
| `outcome_unknown` | 未完成；可能运行中、崩溃或执行失败，不能直接安全重跑 |
| `blocked` | 版本、拓扑或检查点结构不匹配 |

仅显式操作记录提供pending/failed证据：pending表示结果未知，failed也须核对外部效果与重试策略。建议动作不是执行授权，诊断不修改状态、不批准、不调用节点/路由/协调器、不取执行锁。跨多条Load不是一致性事务，实际恢复前须协调在途执行。图报告不调用宿主状态反序列化函数，也不验证业务状态类型兼容性。

版本迁移必须由宿主明确执行；诊断本身不迁移或修复存储数据。
