# 记忆后台整理与上下文装配设计

状态：本地实现与专项验收已完成，正式版本发布另设依赖门禁。使用方式见[运行与恢复指南](../guides/MEMORY_RUNTIME_CN.md)。执行入口：[统一实施方案](../superpowers/plans/2026-11-22-agent-system-execution.md)。现有协议以[删除治理](MEMORY_GOVERNANCE.md)、[G2派生](MEMORY_G2_DERIVATION.md)及当前代码为准。

## 1. 图示与现状

| 图中层次 | 当前实现 | 本次补充 |
|---|---|---|
| Transcripts | core/session事件、活动视图、压缩原文取回 | 保持原日志为权威记录 |
| Memory notes | fact/episode/reflection/skill，抽取、合并、否定、去重 | 补后台形成的可靠重试与成本准入 |
| Representations | 时序图谱、社区摘要、跨会话反思 | 不另建存储；overview保留为后续候选 |
| Injected context | Pinned/查询召回、Snapshot/Hydrate | 补预算分区、选取解释和纯聊天支持 |

现有Worker已能整理跨会话记录。会话压缩处理上下文长度，长期抽取形成记录，两者职责不同。改动前的自动注入限原生工具流：每turn Snapshot一次，每步Hydrate；Pinned仍受RecallRunes总量限制。

## 2. 后台派生重试

`memory/worker.go`在一批抽取任务后调用社区/反思维护，随后可能移除jobs登记；派生失败且无新任务时，不能据此保证再次处理。新增持久dirty状态，复用现有queue分片和Worker锁。

```go
type DerivationWork struct {
    DirtySeq, DoneSeq int64
    GeneratorVersion string
    Attempts int
    RetryAt time.Time
    LastErrorCode string
    Paused bool
    BlockedBudgetRootKey string
}
func (m *Memory) MarkDerivationsDirty(context.Context) error
```

DirtySeq按community/reflection类别独立递增，不等于任意state提交序号。输入变化时登记，计算锁外完成、提交时复核；旧结果不能清掉更新的dirty。派生输出、访问统计和预算序号不再次触发同类工作。

在state锁内先登记queue再提交dirty，间隙失败只留下可收敛的多余登记。宿主按已知namespace调用MarkDerivationsDirty，不假定Store能列出全部namespace。

MarkDerivationsDirty也是宿主修复输入/配置/生成器后的显式技术重试入口：在state锁内先登记queue，再对community/reflection两个类别原子提交DirtySeq递增及Paused、Attempts、RetryAt与技术错误清除；不得清除BlockedBudgetRootKey、RequestSeq或任何预算账本。普通输入变化仅走内部dirty登记，Worker不能循环调用此公开入口解除暂停。换预算窗口也不代替此操作。

失败退避1秒至5分钟，同输入普通失败最多5次后Paused。预算耗尽不计普通失败，记录BlockedBudgetRootKey并等待宿主停止旧循环、重建Memory绑定新维护Controller。相同窗口重启不解锁；换窗口不清技术失败次数、dirty或历史账本。

新持续重试只在后台准入就绪后启用，未配置时保留原批次行为。

## 3. 后台成本与清理

改动前chatJSON直接调用llm.Chat，维护embedding也直接调用Client，不自动消费Agent根预算。本次增加可选Config.Background，JSON标记`-`，不改变Memory.Identity：

```go
type BackgroundConfig struct {
    Budget *budget.Controller
    MaxInputRunes, MaxOutputRunes, MaxDerivationsPerPass int
    MaxOutputTokens int
    EstimatorVersion string
    EstimateChat func(*llm.CompletionRequest) (int,error)
    EstimateEmbedding func(string) (int,error)
    SingleRequestClients bool
}
```

启用时要求持久Budget、正数请求/token上限和本地限额、非nil估算器，EstimatorVersion匹配TokenPolicyVersion。客户端须由受信配置保证一次调用至多一次物理请求、无隐式重试/拆分，并用本地HTTP计数验证；不能兑现则拒绝严格配置。

MaxDerivationsPerPass仅限制community/reflection每namespace每类别一轮启动的单元数（一个社区或一次反思，失败也占一个），不限制抽取jobs；只在单元之间让出执行，不按单次chat截断JSON修复。每个实际请求的硬限额仍由维护Controller控制。社区按稳定ID顺序逐个生成，立即按Basis复核并提交，复用syncCommunitiesTx；单轮让出时记deferred_pass_limit、保留dirty，不计技术失败。下轮用已存Fingerprint跳过已完成项，不因后续失败丢弃此前有效摘要；只有该类别全部工作收敛且DirtySeq仍匹配时推进DoneSeq，不新增cursor表。

保留现有社区按时间/ID选择最近CommunityFacts条关系的语义，不另加隐式字符截断。反思暂保留现有整体输入；派生单元的任一完整请求超过MaxInputRunes，调用前标记input_oversized并Paused，不反复重试、不标已完成。宿主修正限额/材料后显式重新登记。首轮不增加层级MapReduce；不能承诺小于单个原子单元所需请求量的维护窗口也能完成它。

每次chat、JSON修复、反思、社区和维护Embed调用前，按完整输入与MaxOutputTokens保守估计并ReserveAttributed，RecordEvidence写入同一请求，均显式nil lineage。实际CompletionRequest设置MaxTokens；MaxOutputRunes只限制产物大小。未知usage保留预留，不退款。

维护窗口耗尽与单轮让出分开处理：前者保留BlockedBudgetRootKey，不增加抽取Job.Attempts或派生Attempts，未完成Job保持pending；已完成社区保留。中断的原子单元在新窗口重新计算仍会收费，不能拿旧预留直接重发；RequestSeq和账本必须反映所有实际请求。

namespace持久递增RequestSeq，为每次实际请求派生新key。背景ctx仅传递取消/期限与可信namespace，绑定维护Controller，隔离旧Agent归属和Journal。不要只覆盖Controller却沿用旧lineage。

RunMaintenance不新增chat，但Reindex可能付费调用Embedding。删除/旧索引清理先执行，预算耗尽只延后缺向量修补，保留index-repair登记；Model/Sessions未配置的维护部署也能恢复修补。

在线Recall和memory_save/replace内部embedding由宿主在线Client准入，不借维护额度。未提供在线准入的严格示例关闭Index/Embedder，保留文本召回。

## 4. 引用装配与纯聊天

现有Snapshot/Hydrate继续承担context pack，不保存第二份正文。新增`memory.Config.ContextLayout *ContextLayout`，JSON标签为`context_layout,omitempty`；nil不输出该字段，保持旧Identity编码。接口为：

```go
type ContextLayout struct { CoreRunes int }
type Selection struct { Ref Ref; Section, Reason string; Runes int }
type SnapshotExplanation struct { Selected, Omitted []Selection; TotalRunes int }
func (m *Memory) ExplainSnapshot(context.Context,string) (Snapshot,SnapshotExplanation,error)
```

ContextLayout非nil时要求`0 <= CoreRunes <= RecallRunes-固定块头字符数`，无合法空间或负值时构建失败。CoreRunes=0关闭Pinned分区，不能把被排除的Pinned重新放入动态候选绕过限额；nil保持原选择行为。动态召回使用剩余额度，按完整行选取并回收空余预算，总量含块头受RecallRunes限制。分区只用于初选，Hydrate保持原引用和顺序、只过滤无效项，不重选。Explain来自同次选择且无正文。非nil布局参与Identity。

纯聊天复用turn绑定、memory/recall、memory/use及意图占位。TurnStart新增omitempty RecallMode：新轮次为plain-memory/1或plain-none/1；旧纯聊天缺字段保持原语义，不创建召回操作或改变model/answer/0意图。

新协议首次模型尚未开始时可按正常顺序创建引用；已有model/use却缺recall则报告状态异常，不能降级旧模式。原生工具流协议保持。不得靠注册虚假工具获得自动召回。

## 5. 删除与业务数据边界

自动注入只持久引用，正文作为低信任消息按请求Hydrate。删除、失效、正文变更、scope切换或越权引用退出后续请求；恢复不重新检索替换旧引用。宿主不得配置改写或移动召回消息的RequestHook：新纯聊天保持禁止RequestHook；原生工具流沿用恢复时检测意图变化的既有防护，本次不新增其首次请求拦截保证。

宿主不直接复制召回块进PlanningInput/State/system。独立授权的业务来源、模型已生成的Plan/Artifact/回答按业务契约保存，Forget不改写这些输出或撤销外部效果。完整擦除需要宿主另行管理会话/运行与外部副本。

后台只消费当前有效Memory对象，不重扫全历史重新学回遗忘内容。现有reflection失效规则保持，不因本次改动把经历过期等同于删除经验。

## 6. 状态格式与升级

DerivationWork、RequestSeq及暂停状态纳入完整`memory-head/4`和`memory-state-blob/4`：覆盖基础快照、overlay、克隆、容量、压缩、删除和回滚。保留/2、/3读取，旧读者拒绝/4。同一存储禁止新旧Memory写者混跑。

升级前停流量/Worker并备份；宿主按已知namespace登记维护。回退需恢复备份并按RollbackWindow重放删除日志，不能只回退二进制；未开启删除日志不能承诺回滚后无遗忘内容复活。

本次不预建Representation表、RefKind或overview配额。以后若采纳新派生对象，再独立设计并提升相应格式版本，不暗改已发布/4布局。

## 7. 后续候选：namespace overview

图中的稳定概览与用户需求相关，保留为候选，不纳入本次实现或验收。它应复用现有存储和引用机制，仅依赖当前有效fact，保存明确Basis与生成版本；事实更新、过期、遗忘或回退后立即停止采用，迟到结果不能写回。

概览不得成为system指令或被模型直接认证为业务来源。启用前需在相同上下文与成本预算下证明收益；当前没有实现、没有效果结论，也不承诺生成Wiki文件。

## 8. 外部方案取舍

截至2026-11-22的调研：[LangGraph记忆概念](https://docs.langchain.com/oss/python/concepts/memory)区分会话状态与跨会话存储；[LangMem记忆模型](https://langchain-ai.github.io/langmem/concepts/conceptual_guide/)提供profile/collection选择。本项目已有Record集合、图谱/社区与删除依据，继续复用这些机制；profile不是必须补建的数据层。

[LangMem延迟整理](https://langchain-ai.github.io/langmem/guides/delayed_processing/)展示了去抖调度。当前优先补持久重试、逐社区进展和限额分流；观察到重复维护成本后再考虑有界去抖，不混用失败RetryAt。记忆写入/置顶准入属于后续治理：先复用宿主工具注册和授权，具体策略须覆盖工具及Worker修改；本次整理能力不代表已解决记忆投毒。以上为项目取舍，没有效果收益结论。
