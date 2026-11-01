# Token 统计专用钩子

状态：已实现。沿用统一字段证据和预算账本，不建立额外累加器或版本模式。

入口为 `budget.WithUsageHook(ctx, hook)`；Builder 的 `WithUsageHook` 在执行入口绑定同一个 context 钩子。签名 `func(context.Context, budget.UsageEvent)`，只读、支持并发、不返回控制错误。未配置时不额外查询账本。

UsageEvent 包含 Model（逐尝试证据）、Statistics（同一账本累计查询）、HasStatistics、StatisticsError 和 ErrorKind。没有预算仍交付单次 Model，HasStatistics=false；查询失败或 panic 也交付单次证据。记账失败不覆盖已收到的有效 Token，AccountingCommitted=false。非法证据不作为有效 Token 交付。

回调在记账之后、账本锁之外执行，可再次只读查询预算。统计查询和回调使用保留身份且脱离原取消信号的5秒 context；超时是合作式约束，宿主钩子必须及时返回。回调 panic 和查询错误不改变业务结果，不触发重试。

父钩子随 context 传给子 Agent；子 Builder 非 nil 钩子替换继承项，nil 配置保留继承。直接 WithUsageHook(ctx,nil) 清除。Observer 独立工作。需要多个统计目标时由宿主在一个钩子内分发，框架不重复注册回调。

回放同样通知但标 Replayed，不能按投递次数计费。Statistics 是时点累计，可能包含并发任务；不能相加多个快照，也不能通过根快照差值推算子 Agent 消耗。根据 Model.AgentCallKey 查 Calls 取得 Self/Inclusive。可靠累计始终查同一账本。

启用钩子但使用未计量的旧工具选择客户端时，在模型请求前拒绝；纯 StreamClient 无工具路径仍支持。控制钩子、业务路由与后台调度语义不变。

使用见[指南](../guides/MULTI_AGENT_USAGE.md)，回归与审核见[交付报告](../evaluation/2026-11-01-token-usage-hook/REPORT.md)。
