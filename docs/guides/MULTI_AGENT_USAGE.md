# 多 Agent 钩子与 Token 统计

GoAgent 以同一根预算账本统计父 Agent、子 Agent 和汇总请求，Observer 提供只读通知。所有预算使用同一套字段证据与归属模型，不创建默认存储，不改变业务路由或权限。

## 开启身份与归属

```go
controller, err := budget.New(budget.Config{
    RootKey: "business-run-123", Version: "policy-20261101",
    MaxModelRequests: 100,
    MaxAgentCalls: 50,
})
// 检查 err；需要跨进程恢复时使用 NewDurable(config, hostStore)。
ctx := budget.WithController(hostCtx, controller)
agent, err := builder.NewSimpleBuilder(client).
    WithAgentID("coordinator").BuildContext(ctx)
```

每个 Agent 自动建立调用身份；WithAgentID 显式命名角色，未设置时为 anonymous。子 Agent 使用独立实例/会话和传入 context，继承根预算及 Observer。持久调用仍需 WithDurableScope；AgentID 不替代它。`execution.ConfigureBuilder` 会设置 SessionID、DurableScope 和 AgentID。

MaxAgentCalls 约束保存的调用节点数量，包括零模型调用的中间 Agent；为0时使用 DefaultMaxAgentCalls=1000，负值拒绝。同一逻辑调用恢复不重复占槽。同名角色可以有多个不同调用键。根 RunID 由 Scope 或 Controller.RootKey 确定，已有父身份/观察根不一致时在发请求前拒绝。

不经统一入口的隐藏 SDK 请求不会自动计量。ModelInvoking=true 是可信宿主声明，不是模型调用探测器。完整钩子覆盖 ExecuteStream、AsRunnable 和 Start；旧同步 Execute 不提供该计量路径，显式 AgentID 的旧工具选择路径会拒绝，避免漏记选择请求。

## 观察钩子

直接获取 Token 与累计统计可使用专用钩子，无需实现 Observer 或筛选事件：

```go
agent, err := builder.NewSimpleBuilder(client).
    WithUsageHook(func(ctx context.Context, e budget.UsageEvent) {
        // e.Model.Tokens：本次尝试；KnownFields 标明哪些字段已报告。
        // e.Model.AgentID / AgentCallKey：Agent 归属。
        if e.HasStatistics {
            total := e.Statistics.Root.KnownTokens.TotalTokens
            _ = total // 时点累计；不要将多个回调的累计值相加。
            // e.Statistics.Calls 中按 AgentCallKey 查 Self / Inclusive。
        }
        // 回放标识 e.Model.Replayed；尝试键与 Revision 可用于投影去重。
    }).Build()
```

也可通过 `budget.WithUsageHook(ctx, hook)` 统一绑定父子调用。父钩子默认向子 Agent 继承；子 Builder 设置非 nil 钩子会替换继承项，nil 配置保留继承。直接 WithUsageHook(ctx,nil) 清除。Observer 可以同时工作；需要多个目标时在一个钩子中分发。

没有预算时仍通知单次用量，HasStatistics=false；不会隐式创建账本。查询失败时 StatisticsError 非 nil，单次 Model 证据仍保留；记账失败时 AccountingCommitted=false，不能将其当作已可靠登记。回调及查询异常不改变业务结果。

钩子同步执行、必须支持并发并及时返回；不要在钩子内同步等待或重新执行当前 Agent。框架提供脱离执行取消信号的有界 context，以便取消后仍交付已知用量，但无法强制终止忽略 context 的宿主代码。开启累计钩子会在每次通知时查询账本，有实际开销；未配置时没有这次额外查询。详细契约见[设计](../design/TOKEN_USAGE_HOOK.md)。

通用观察仍可使用 Observer：

实现 `observe.Observer`，通过 `observe.WithObserver(ctx, sink)` 或 execution.Scope 注入。后代继承该 sink；不要在每层重复注册同一累加器。父层 PreStep/RequestError 等控制钩子仍与子层独立。在这些钩子或工具 Pipeline 中可用 usage.FromContext(ctx) 读取当前调用身份；身份标签不替代授权。

```go
func (s *Sink) Observe(ctx context.Context, e observe.Event) {
    if e.Type != "model_usage" || e.Usage == nil { return }
    u := *e.Usage // 副本；不依赖其交付次数计费
    // u.AgentID / AgentCallKey / ParentAgentCallKey：归属
    // u.AttemptKey / Attempt / Revision：尝试与证据去重
    // u.Tokens / KnownFields：报告值及字段是否已知
    // u.Replayed / AccountingCommitted：回放与持久登记状态
    // 将它投影到宿主展示/指标系统；丢事件后用账本查询修复。
}
```

新增 `model_attempt_start` 在预算准入之后、调用客户端前发出，不证明请求已经到达供应商；准入拒绝不发该事件。`model_usage` 覆盖成功、失败、取消及回放补登记。现有 model_start/end 仍描述逻辑模型操作，不能和逐尝试事件再累加一次。

`Event.Agent` 含 Agent 的 Owner 与本次 VisitID/ParentVisitID；`Event.VisitID` 保持观察操作的访问身份，两者用途不同。调用恢复保留 AgentCallKey，生成新 Agent VisitID。元数据不包含对话正文或原始供应商错误。

同步 Observer 必须及时返回，panic 被隔离；AsyncObserver 有界且可能丢事件。失败通知不改变业务成功，不提供 exactly-once。可靠累计必须查询预算账本，不能将 hook 的累加值作为最终账单。

## Self、Inclusive 与 Root

```go
reportCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
defer cancel()
report, err := controller.Attribution(reportCtx)
// report.Root：根累计
// report.Calls：按逻辑 Agent 调用，Owner + Self + Inclusive
// report.Agents：按 AgentID 的 Self 汇总
// report.Unattributed：没有 Agent 上下文的直接预算调用
```

失败或审批暂停后也可以查询。若 Wait 自身超时，执行可能仍在收敛，查询只是时点快照。子 Agent 结束不代表外层 OwnedRunnable.Close 已完成；工具及 Handle 的终态按各自生命周期处理。

父分析 3、子 A 4、子 B 5、父汇总 6 时，父 Self=9、Inclusive=18，根为18。不要相加父子输出、Inclusive 或连续根快照。并行时也不能用两个根快照差值推算某个子 Agent。

原 AgentOutput.TokenUsage 保持本层逻辑调用口径；它在回放时仍可包含历史用量，不代表本次新增消费。根账本按唯一模型尝试键去重；真正重试占新的尝试键。

## 未知、缓存、部分报告

`llm.Usage` 新增 CachedTokens 和可选 KnownFields，`Tokens()` 取值，`Fields()` 返回字段位图。nil mask 保持旧三字段语义；显式 mask 可标明已报告的零值。缓存一般是输入的子集，不加到 TotalTokens。

```go
fields := usage.Total | usage.Cached
u := &llm.Usage{TotalTokens: 19, CachedTokens: 4, KnownFields: &fields}
```

统一账本保留字段证据，允许未知字段后续补齐；相同证据幂等、已知字段改变报错。跨流帧只更新后来已知的字段，不累加累计帧，也不让缺失字段覆盖前帧证据。回放通知不改变原始来源或增加证据 Revision。

报表提供 UnknownUsageEntries、PartialUsageEntries，以及每个汇总中的 UnknownPromptEntries/UnknownCompletionEntries/UnknownTotalEntries/UnknownCachedEntries。Complete 只表示基本三字段完整，不承诺缓存已知、供应商账单完整或外部隐藏调用都已捕获。Total 未知时不计算 KnownTokenDelta，不把预估当作实际节省。

所有预算默认保留部分字段，不再存在降级为旧格式 unknown 的模式；成功提交的 model_usage 通知设置 AccountingCommitted=true。Reserve/ReserveEstimated/RecordUsage 是同一证据实现的便利入口；需要字段 mask 时使用 RecordEvidence。直接调用没有 Agent 上下文时明确记入 Unattributed，不猜测归属。

当前 OpenAI/DeepSeek 标准流、Gemini 传递 SDK 可用的缓存与字段信息。SDK 将缺失和显式零都解码为 scalar 时，保守标未知；DeepSeek 私有缓存字段未被 SDK 暴露的部分不伪造。货币定价、生效时间与账单核对由宿主提供。

## 恢复与迁移

只有一种账本结构和格式标记 goagent-model-budget，不提供格式选择开关。Config.Version 仅作为宿主预算策略指纹，不选择实现版本。旧格式明确拒绝读取，不自动迁移；已有持久运行需由原实现或宿主明确处置，不能换执行键重跑已有业务。

模型结果保存后、用量登记前中断时，恢复读取已保存结果补登记，不再次发请求。账本提交后、通知前中断可能漏事件，查询仍正确。Owner/祖先链在预留、证据登记和回放时都校验；改 AgentID/父关系会拒绝。同一工具整体回放不重新进入子 Builder，采用历史归属；改变闭包中的子配置需提升工具 Version。

账本事务保留现有锁、克隆与完整校验，启用归属增加 CPU/内存开销，大账本查询和更新仍有线性成本。宿主应按业务运行限制请求与调用节点数量。

## 离线运行与证据

所有案例代码已迁至 kart-io/goagent-examples，以下命令在该仓库根目录执行（当前新 API 需先 make work 联调本地 GoAgent）。按两种业务口径统计的完整案例见 [Token 统计案例](https://github.com/kart-io/goagent-examples/tree/main/agent-usage)：

```sh
# 完整任务：父 Agent + 两个并行子 Agent + 普通工具，总量 18
go run ./agent-usage/task
# 单 Agent：writer 总量 10
go run ./agent-usage/agent -mode single
# 子 Agent：writer 为 10，包含父分析与汇总的任务总量 17
go run ./agent-usage/agent -mode child
```

案例同时演示逐尝试钩子、角色 Self、调用 Self/Inclusive 和完成后的 Root 查询，不需要模型凭据。模型 usage 为预设值，实际接入时替换模型客户端。

```sh
go run ./agent-usage
```

例子包含两个并行子 Agent、一次失败重试和持久回放。两次输出均为 Root=18、父 Self=9、父 Inclusive=18；请求5次，其中一次缺少 usage，unknown=1。临时目录由示例自己创建并清理，不需要模型凭据。

示例通过父 Builder.WithUsageHook 取得所有后代通知，打印 hook: live=5 replay=2；通知次数不等于新增费用，回放后的根累计仍为18。

实现依据见[设计](../design/MULTI_AGENT_HOOKS_AND_USAGE.md)，验证见[交付报告](../evaluation/2026-11-01-multi-agent-usage-implementation/REPORT.md)。

## 推理明细、实际模型与配置

```go
agent, err := builder.NewSimpleBuilder(client).
    WithReasoning(usage.ReasoningConfig{Effort: "low"}). // OpenAI 兼容/DeepSeek；模型需支持
    WithUsageHook(func(ctx context.Context, e budget.UsageEvent) {
        fmt.Println(e.Model.Model, e.Model.ResponseModel) // 请求名与响应实际名
        if e.Model.KnownFields&usage.Reasoning != 0 {
            fmt.Println("已知推理 Token", e.Model.Tokens.ReasoningTokens)
        }
        if e.Model.KnownFields&usage.ToolUsePrompt != 0 {
            fmt.Println("工具相关输入", e.Model.Tokens.ToolUsePromptTokens)
        }
        // ReasoningConfigured/Reasoning 记录显式请求设置，不推断上游默认。
    }).Build()
```

Gemini 2.5 可用 `usage.ReasoningConfig{BudgetSet: true, BudgetTokens: 128}` 请求单次思考预算；模型支持零预算时，显式 0 与未设置仍可区分。客户端默认值通过 `llm.WithReasoning(config)` 设置。无实现的 Provider 明确拒绝；自定义客户端实现 `llm.ReasoningRequestValidator` 后才接受 Builder 的显式设置。

推理与工具输入分项不能再加到供应商 TotalTokens。未报告的字段仍为未知，不用总量减法推算。UsageSummary 提供 `UnknownReasoningEntries`、`UnknownToolUsePromptEntries`；基本用量 Complete 不代表所有可选细项已知。实际响应模型未提供时 ResponseModel 为空。

[设计契约](../design/TOKEN_USAGE_EXPLAINABILITY.md)记录字段、配置和回放规则。
