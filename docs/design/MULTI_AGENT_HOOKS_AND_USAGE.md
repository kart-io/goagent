# Agent / 子 Agent 钩子与 Token 归属设计

日期：2026-11-01。基线：`de6c61a`。状态：**核心功能已实施**；实现覆盖身份、观察钩子、字段证据与可选 v2 归属账本。接入见[指南](../guides/MULTI_AGENT_USAGE.md)，验证见[交付报告](../evaluation/2026-11-01-multi-agent-usage-implementation/REPORT.md)。定位：GoAgent 继续是可嵌入、可恢复、可计量的通用编排内核。

## 1. 结论与方案

已有根预算账本、具名模型操作、重试去重、嵌套 Runnable、上下文传播、Observer 和工具 Pipeline。缺少的不是另一个多 Agent 框架，而是跨这些已有机制的一致调用身份、逐尝试用量通知和账本归属查询。

比较三种方案：只累加父子输出会重复计算并漏掉失败；仅给 Observer 加 Token 字段不能解决丢事件和持久恢复；**采用扩展现有身份、Observer 和预算账本的方案**。保留单一执行源，不增加第二套计费账本、全局 HookManager、消息代理、调度器或沙盒服务。业务路由、定价、租户账单和可靠事件投递仍由宿主负责。

## 2. 当前支持与缺口

| 领域 | 当前实现 | 本设计补充 |
| --- | --- | --- |
| 父调用子 Agent/workflow | RunnableTool、Factory、Dispatcher、AsChildRunnable | 身份关联和事件分类，不新增 Agent 继承体系 |
| 根用量与限额 | budget.Controller，父子共享 context，按模型尝试键记录 | 按 Agent 调用归属查询 |
| 恢复与重试 | 重放不预留新请求；真正重试占新尝试键 | 逐次尝试事件、证据修订与稳定去重键 |
| 执行钩子 | PreStep、Request、RequestError、TurnStopping、Pipeline | 保留其行为边界，不拿请求前钩子统计消耗 |
| 只读钩子 | observe.Observer / OTel / AsyncObserver | Agent 父子身份、逐尝试用量事件 |
| 用量输出 | AgentOutput.TokenUsage 是本层逻辑调用用量；root_budget 是累计根快照 | 明确 self、inclusive、root 口径和 unknown |
| 供应商字段 | llm.Usage 有输入/输出/总量；TokenUsage 另有缓存字段 | 缓存及部分字段的可用性协议，不能将未报告当成零 |

审核证据见[能力核对](../research/2026-11-01-agent-hooks-usage-audit.md)，当前可运行反例见[多 Agent 计量回归](../../builder/multi_agent_usage_audit_test.go)。

## 3. 身份：业务运行、逻辑调用与本次访问分开

新增窄数据包 `core/usage`，仅放值类型和 context 身份传播，不拥有存储、供应商客户端或调度循环；budget 与 observe 均可依赖它，不互相依赖。

```go
// core/usage 中的公开值类型。
type Owner struct {
    RunID              string // 根业务运行
    AgentID            string // 宿主配置的逻辑角色
    AgentCallKey       string // 一个逻辑 Agent 调用，恢复时稳定
    ParentAgentCallKey string // 根 Agent 为空
}
type Invocation struct {
    Owner
    VisitID       string // 每次实际进入生成，重启/回放时改变
    ParentVisitID string
}
```

- 新增 `AgentBuilder.WithAgentID(string)`。`execution.ConfigureBuilder` 同时设置 AgentID 与既有 DurableScope；DurableScope 仍只负责持久命名空间，不因仅统计 Token 强制开启日志。
- 持久 AgentCallKey 从已派生的 Agent 执行键取得；非持久调用生成独立键。相同 AgentID 并发两次有不同 CallKey。同一逻辑调用恢复沿用 CallKey，但生成新 VisitID。
- Builder 进入时从 context 读取父调用，再装入自身身份；子 Agent 自动继承同一根预算、观察者与根 RunID。不能在 Factory 中 Bind 一个新根 Scope 或换根预算来伪造独立统计。
- 宿主需可靠归属时显式配置 AgentID。只有观察时可使用匿名显示标签；不得把匿名标签当稳定的角色账单维度。开启账本归属但缺少身份，应在发模型请求前报错。
- Owner 与调用链元数据不进入 system prompt、工具结果或模型输入。标签由宿主提供，有长度限制，不使用用户正文、凭据或原始错误作为身份。

同名 Agent 角色、Agent 调用、会话、执行键、模型尝试键和 OTel SpanID 都不是同一概念。Span/Visit 可用于调试，不能作为恢复去重或计费键。

根身份有明确优先规则：使用 execution.Scope 时取其 RunID；直接接入 budget 时必须等于 Controller.RootKey；已有父 Invocation 或 observe.RunID 必须与该根相同，否则在模型请求前报错。无 Scope/预算时沿用已有父 Invocation 或 observe.RunID；两者冲突报错；均不存在才为此根调用生成随机 RunID，并在创建观察 span 前绑定，避免 Observer 再生成另一个根。durable.ExecutionKey 可能是节点派生键，不能反推为根 RunID。

`Invocation.VisitID` 是 Agent 一次访问的身份，`observe.Event.VisitID` 保持现有的某次 agent/model/tool 观察操作身份；二者不强行相等。每个后代事件携带所属 Agent.VisitID，ParentVisitID 指父 Agent 的 VisitID，不是父模型事件 ID。Trace/Span 继续用 OTel 原生关系。

入口范围：完整逐尝试钩子覆盖 ExecuteStream、AsRunnable 及 Start 共用的流式执行源。旧同步 Execute 不经过 modelOperation，且目前拒绝根预算；本设计不声称它也有完整计量，不扩大其功能。旧非原生工具选择路径在显式启用完整计量时先拒绝或要求宿主适配，不能静默漏记工具选择请求。

## 4. 钩子：控制与观察分离

不增加与现有机制重复的 PreAgent/PreTool 控制总线。行为控制继续使用 Builder 钩子、Pipeline/Authorize 与宿主编排；只读统计通过现有 `observe.WithObserver` 接收扩展事件。

为 `observe.Event` 增加可选 `Agent`、`Model`、`Usage` 值元数据。嵌套数据只包含可复制的标量/值类型；若采用指针封装，发送和异步入队必须深复制，接收者不能修改后影响后续事件或账本。

| 事件/钩子 | 时机 | 用途与限制 |
| --- | --- | --- |
| agent_start | 身份与入口校验完成、即将进入 Agent 执行源 | 包含父调用身份；失败初始化不假造已开始的子 Agent |
| agent_end/error/waiting | Agent 执行源收敛 | 表示本次调用，不代表外层 OwnedRunnable.Close 已完成 |
| model_attempt_start（新增） | 预算准入成功、即将调用客户端入口 | 每次实际 SDK 尝试一条；准入失败不发；不证明网络已发送 |
| model_usage（新增） | 尝试结束并完成可用证据登记，或回放补齐证据后 | 包含尝试键、用量、状态、来源、Replayed、AccountingCommitted |
| tool_start/end/error/replayed | 沿用 Dispatcher 事件及 Pipeline/OnResult | 拒绝或 Factory 创建失败不能伪造 child agent_start；成功工具终态在适配器清理之后 |
| 根统计查询 | 宿主在成功、失败、取消或审批暂停之后按需查询 | 查询共享账本，不通过减两个并发快照计算某个 Agent 的费用 |

当前 model_start/end 描述逻辑模型操作，model_retry 描述重试访问。新增逐尝试事件，不能悄悄把既有事件改成逐次 HTTP 请求，否则旧观察者会重复计数。

Observer 的 panic 仍隔离，同步钩子须及时返回，AsyncObserver 仍有界且可能丢事件。统计钩子不改变业务成功/失败，不产生自动重试。需要改变执行的策略必须放在明确的控制扩展点。

父级在 context 注册 Observer 后可观察所有经过统一入口的后代；PreStep/RequestError 等 Builder 配置仍仅属于本 Agent，不自动复制给子 Agent。宿主自行组合多个 Observer；避免既在父 context 注册又在每个子 Agent 重复包同一计数器。

## 5. Token 证据与每次尝试

定义 `core/usage.ModelEvidence`：Owner、逻辑操作键、AttemptKey、Attempt、Provider、Model、状态、字段可用性、TokenUsage、来源和证据修订。Provider/Model 为实际发送时的有效配置；不支持读取有效配置的客户端标 unknown，不用初始 Builder 配置冒充回退后的模型。

原始证据来源与交付来源分开：供应商报告保留 provider 原来源，恢复读取用 delivery=saved 标记，不改写原来源；缺少原来源的旧记录标 legacy_unknown。宿主准入 estimated 放在预留元数据，不能写成真实消耗。无 usage 为 unknown；非 nil 但只报告部分字段时为 partial；报告零与未报告是两种状态。

`llm.Usage` 扩展缓存与字段可用性必须可选、兼容旧构造器。旧记录没有 mask 时采用明确的 legacy 三字段语义；新 Provider 只声明实际报告的字段。缓存 Token 通常是输入 Token 的子集，不能另加到总量；reasoning 等明细也不能无条件重复相加。报告的 TotalTokens 保留原值，不擅自用输入输出和覆盖供应商证据。

登记顺序：

1. 使用现有 reserve 路径，将 AttemptKey 与不可变 Owner 一起提交，再调用客户端。准入失败无已付费请求证据。
2. 每次重试使用现有派生尝试键，失败尝试已知 usage 也要保留；未知不记为零。
3. 持久模型结果与预算证据沿用现有恢复机制。先保存结果再登记用量时中断，恢复只能补登记，不能重新发送已保存模型请求。
4. 账本写入成功后才发 `AccountingCommitted=true` 的 model_usage；失败只可发未提交观察事件，并按现有执行错误规则返回。日志持久化不代表 Observer 已收到。
5. 回放可通知 UI，但 Replayed=true、逻辑操作键与 AttemptKey 不变。未知证据后来被补齐时版本递增；相同已知证据重复提交幂等，不同已知证据拒绝。

事件投递 ID 与证据去重 ID 分开。宿主统计消费者按 `(RunID, AttemptKey, EvidenceRevision)` 更新投影，不能将每次回放通知累加为新消耗。账本提交后、通知前崩溃时允许没有通知；宿主用查询修复投影。不承诺事件 exactly-once，也不把工具 OnResult 作为新增模型费用的来源。

证据合并按字段单调进行：unknown → partial → 更完整证据；已知字段只允许相同值，未报告字段可以补齐。新增已知字段才增加 Revision；相同值重投或从 saved 回放不增加。原始 Provider/Model 和来源是持久证据的一部分；本次交付是 live/saved 单独标记，不因恢复读取而改写原来源。保存的旧证据无原来源信息时标 legacy_unknown，不伪造 provider。预估放在准入元数据，不能用 estimated 升级实际证据。

未绑定预算时可发只读事件供临时统计，但不提供跨崩溃的可靠累计；需要可靠统计时绑定宿主提供的 budget.Controller（可选持久 Store）。不为纯统计隐式创建存储或无限预算。自定义客户端内部重试、直接 SDK 调用及隐藏模型调用仍需宿主适配，不能仅凭 ModelInvoking=true 自动捕获。

## 6. 账本归属：扩展现有预算，不增加第二个总账

新增显式归属配置 `AttributionVersion` 及正数 `MaxAgentCalls`。关闭时保留现有 v1 账本与序列化意图；开启时使用 v2 记录，增加 `AgentCalls[AgentCallKey]Owner`、`AttemptOwners[AttemptKey]AgentCallKey` 和字段可用性证据。Agent 进入时幂等登记调用节点与完整祖先链；模型预留时同一次 Store 锁/提交校验/补齐该链并绑定尝试，避免出现只保存叶节点而无法重建祖先。

身份 context 内保留不可变祖先链，查询返回副本。完全没有模型尝试的 Agent 也登记调用节点，但不据此证明外部 SDK 没有费用；它不消耗模型请求额度。`MaxAgentCalls` 限制节点数量，已登记节点恢复不重复占槽。登记只保存拓扑，不保存任务调度状态，不构成另一套子任务运行时。

旧 v1 代码必须拒绝 v2 记录，不能因忽略新字段而写丢归属。不要就地切换活跃 Run 的策略或为了统计换执行键重跑业务；旧运行留在旧版本完成，新运行显式启用。存量不具备归属的证据标 unattributed，不从 hash 反推 AgentID。

提供 `Controller.Attribution(ctx) (AttributionReport, error)`：根 Snapshot、按 AgentCallKey 的 Self 汇总、按 AgentID 的角色汇总、unknown/partial 数量、unattributed 与完整性。报表输出确定排序、独立副本；不包含模型正文。v2 同一 AttemptKey 改 Owner、父关系冲突、跨 Root 关联均报错。校验总量时保留无法归属桶，不默默丢记录。

既有 Snapshot.KnownUsageEntries 表示存在 usage 对象，不升级为“每个字段都已报告”的承诺。新增报表提供字段级 unknown/partial 计数及完整性；只报告 TotalTokens 时不把输入、输出或缓存的零值当成已知事实。U2 可以先发布观察元数据，完整 partial 持久口径及可靠归属需 U3 的 v2 支撑，不能对未存储的字段标 AccountingCommitted=true。

预留校验不是唯一防线：每次证据登记、模型操作回放和用量补登记也校验 Owner/祖先。启用 v2 时把稳定 Owner 及必要身份指纹纳入模型持久意图/结果，身份变化拒绝旧结果，不通过换执行键绕过。整个 Runnable 工具回放时子 Builder 不会重新进入，继续使用历史已提交归属；宿主改变闭包/Factory 中的子 Agent 身份时须提升工具 Version，不能承诺核心能探测不执行的任意闭包配置。

恢复中的 Agent Visit 变化不改 Owner，重新登记不会改父关系。父子链要求同根、无环；节点因工具整体回放而没有再次执行时，查询仍能由已提交账本重建其历史归属，不能依赖本次 observer_start 是否出现。

必要拓扑反例：root → A（无模型调用）→ B（5 Token）。A 的节点仍保存 AgentID 与 root 父关系，Self=0、Inclusive=5；root Inclusive=5。禁止仅从有模型请求的叶节点构造调用树。

Self 与 Inclusive 均从唯一尝试集合计算：

```text
root = Σ 每个唯一模型 AttemptKey 的已知用量
self(agent-call) = 归属该调用的唯一尝试之和
inclusive(agent-call) = 自身和后代调用的唯一尝试集合之和
```

根总量不能再加子 Agent 输出，也不能把每个 Agent 的 inclusive 相加。共享父子关系是树形调用关系；如宿主复用同一外部任务，只绑定一个计量所有者，其他关系为引用，不能多父计费。未知保留计数；整数求和有溢出检查。

## 7. 示例与费用边界

主 Agent 分析 3 Token，子 A 使用 4，子 B 使用 5，主 Agent 汇总 6：

| 视图 | Self | Inclusive |
| --- | --- | --- |
| 主 Agent 调用 | 9 | 18 |
| 子 A 调用 | 4 | 4 |
| 子 B 调用 | 5 | 5 |
| 根运行账本 | 18 | 不再叠加子节点 |

现有代码已经能正确得到根 18、本层输出 9，并在持久回放时不新增请求。本设计补每个调用的归属查询与通知。并行时每个事件携带自身 Owner，禁止用 root_before/root_after 的差值估计子调用。

Token 数不等于货币费用。宿主维护生效时间、供应商、模型、缓存等价格表和计价规则，使用稳定请求键去重；unknown/partial 不能生成看似精确的最终账单。GoAgent 不内置报价、不据 TokenUsage 推断退款或自动扣款。

## 8. 实施顺序与验收

1. **U1 身份与生命周期**：值类型、WithAgentID、context 传播、Observer 元数据；验证同名并发、嵌套、拒绝/Factory 失败、清理边界和 observer 丢弃。
2. **U2 尝试用量钩子**：统一 modelOperation 接入逐尝试事件、unknown/partial、失败/取消、重试、回放与证据修订；覆盖无工具 StreamClient 与原生工具流。
3. **U3 可选持久归属**：版本化现有预算账本、原子预留/归属、查询与兼容；重启和真实故障窗口验证不重复发送/计费。
4. **U4 宿主案例**：父 Agent + 两个并行子 Agent + 汇总；输出 Self/Inclusive/Root 和未知状态，接入 OTel 或宿主计价器。业务案例放 goagent-examples。

实现采用 Event.Agent 和 Event.Usage（模型元数据包含在 Usage 中），不增加重复的 Model 字段；证据状态由 KnownFields 推导，Replayed 表示恢复交付。Complete 只针对基本三字段，缓存未知另行统计。v1 不保存 partial mask：不足三字段时保留 unknown 且不承诺部分证据已提交；可靠字段归属使用 v2。实施与验收任务见[计划](../superpowers/plans/2026-11-01-multi-agent-hooks-usage.md)。
