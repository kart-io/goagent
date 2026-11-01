# Multi-Agent Hooks and Usage Implementation Plan

> **For agentic workers:** Use superpowers:executing-plans or superpowers:subagent-driven-development after the design is selected for implementation. Steps use checkbox syntax.

**Status:** 核心 U1～U3 与通用离线示例已实现；当前公开 API 见[使用指南](../../guides/MULTI_AGENT_USAGE.md)。业务场景继续由独立 examples 项目维护，本轮在核心仓库交付无业务假设的 agent-usage 示例。

**Goal:** 复用统一模型入口、Observer 和根预算，补显式父子 Agent 归属、逐次尝试用量通知及持久报表。

**Architecture:** 新 `core/usage` 仅包含不可变身份/用量值类型和 context 传播；既有预算账本是唯一可靠累计依据，Observer 是可丢弃通知。Builder 与 Runnable/Dispatcher 保留现有执行与权限路径。

**Tech Stack:** Go 1.25、既有 durable.Store、Observer/OTel，无新运行时依赖。

**Spec:** [设计](../../design/MULTI_AGENT_HOOKS_AND_USAGE.md)

## Global Constraints

- 不新建 Agent 调度器、独立计费账本、默认存储、全局 HookManager 或价格表。
- 未启用归属时保持 v1 账本/旧模型意图兼容；新版本不改写活跃旧运行。
- 输入正文、凭据、原始错误不进入元数据；用量未知不等于零。
- 统计事件失败不改变业务执行，持久账本失败沿用现有错误契约；事件不提供 exactly-once。
- 下列阶段分别验收，不把未来能力写成当前 API。

## Review Focus

- 同 AgentID 并发，逻辑调用键仍不同；重启 CallKey 稳定、VisitID 改变。
- 失败/取消仍有费用；无 usage 与部分字段不伪装成完整零费用。
- 回放不新增请求，Observer 重投不新增累计，未知升级为已知不重复加总。
- 子 Agent 完成与 OwnedRunnable.Close、父工具终态的顺序必须区分。
- 父子 Inclusive 重叠；根累计只统计唯一 AttemptKey，不累加快照/输出。

### Task U1: 身份与生命周期元数据

**Files:** 新建 `core/usage/identity.go`、`identity_test.go`；修改 `builder/config_options.go`、`builder/builder.go`、`builder/events.go`、`core/observe/event.go`、`core/observe/async.go`、`execution/scope.go`。

**Interfaces:** `usage.Owner` / `Invocation` 按设计字段；`usage.WithInvocation(ctx, Invocation) context.Context`、`usage.FromContext(ctx) (Invocation, bool)`；Builder `WithAgentID(string)`；Event 可选 Agent 身份。ConfigureBuilder 同时设置 AgentID 与 DurableScope。

- [x] 先写 `TestParentChildInvocationIdentity`、`TestSameAgentRoleConcurrentCalls`、`TestReplayKeepsOwnerChangesVisit`、`TestDeniedToolHasNoChildStart`；验证事件父子关系与实际模型调用数，不能仅断言事件名称。
- [x] 用 `go test ./core/usage ./builder ./execution` 观察缺少类型/行为的 RED。
- [x] 在实际 Agent 入口建立身份，保留父 context 的根预算/观察者；在同步与异步交付中复制扩展元数据。
- [x] 增加直接 budget/observer 接入的根身份冲突测试、无 Scope 统一 RunID 测试、Agent Visit 与事件 Visit 的关联测试；旧同步 Execute/旧工具选择入口不能被误标为完整计量。
- [x] 验证 observer 修改副本/抛 panic/队列溢出不影响执行或账本；无观察且无归属时避免额外链路分配。
- [x] 相关包 race、现有调用/预算/清理回归通过后提交。

### Task U2: 逐尝试用量观察钩子

**Files:** 新建 `core/usage/evidence.go`、相关测试；修改 `builder/model_call.go`、`builder/events.go`、`llm/stream_client.go`、必要 Provider 的 usage 转换，以及 Observer 文档。

**Interfaces:** `usage.ModelEvidence` 按设计包含 Owner、OperationKey、AttemptKey、Attempt、Provider/Model、Tokens、KnownFields、状态、来源、Revision、Replayed、AccountingCommitted。Event 关联这些元数据；新增事件 `model_attempt_start`、`model_usage`，既有 model 操作事件语义不变。

- [x] 写实际 modelOperation 测试，覆盖原生工具/无工具流、首次成功、失败有 usage、未知、取消、预算拒绝、重试两次、回放、回放补登记。
- [x] RED 后在统一 reserve/send/record 路径接入；每个真正调用客户端入口的尝试单独标识。不要从完成 Agent 输出反推模型尝试。
- [x] 扩展字段可用性协议：旧 Usage 无标记时兼容旧三字段；新 Provider 只标实际字段。预估与实际证据分开，缓存明细不另加总。
- [x] 测试 partial → 补齐只新增字段、已知字段冲突拒绝、provider → saved 交付不改原来源/Revision；同一证据重投不新增费用。
- [x] 验证 after-commit/before-notify 故障时事件可以缺失但模型不重发；不靠 Observer 实现可靠累计。
- [x] 相关包 race、Provider 协议测试及旧模型意图 fixture 通过后提交。

### Task U3: 可选持久归属查询

**Files:** 修改 `core/budget/controller.go`；新建 `core/budget/attribution.go`、`attribution_test.go`、`attribution_process_test.go`；修改 `builder/model_call.go`。

**Interfaces:** Config 可选 `AttributionVersion`、启用时正数 `MaxAgentCalls`；`RegisterAgent(ctx, lineage []usage.Owner) error`、`ReserveAttributed(ctx, key string, tokens int, lineage []usage.Owner) error`；`Controller.Attribution(ctx) (AttributionReport, error)`。Report 包含根 Snapshot、按 CallKey/AgentID 的 Self 汇总、完整性与 unattributed，排序与副本独立。Inclusive 从唯一尝试集合派生。

- [x] 写 v1 兼容与 v2 拒绝测试：旧记录不被自动升级；新归属配置不与旧意图混用；Owner/父关系/根冲突拒绝。
- [x] RED 后在 v2 账本保存 AgentCalls 与 AttemptOwners。Agent 进入时幂等登记完整祖先链，模型预留时同事务校验；全零尝试节点也保留并受 MaxAgentCalls 限制。恢复 Visit 变化不修改 Owner。
- [x] 测试 root → A[零模型] → B[5 Token]、同键改 AgentID/父关系、模型回放跳过 reserve 但仍校验归属、整工具回放沿用历史归属及 Version 变化拒绝。
- [x] 故障注入并用子进程验证：预留后退出、模型结果保存后用量登记前退出、用量登记后通知前退出；未知保留且已保存结果不重发。
- [x] 父并行子调用报表为 root=18、parent self=9、child A=4、child B=5；回放后不变；unknown/partial/整数溢出均测试。
- [x] 对归属关闭/开启分别跑现有账本分配基准及万条记录规模对照，记录 CPU/分配，不引入逐事件全量账本扫描；测试与基准结果附报告后提交。

### Task U4: 宿主示例与接入文档

**Files:** 核心指南 `docs/guides/MULTI_AGENT_USAGE.md`；业务示例放 `kart-io/goagent-examples` 独立变更。

**Interfaces:** 使用 U1～U3 的公开 API，不新增业务路由或供应商定价核心接口。

- [x] 示例展示父子树、逐尝试 Token、Self/Inclusive/Root、unknown，触发一次失败与重放。
- [x] 展示 Observer 作为只读钩子和账本作为补偿查询源；有意丢事件后查询仍准确。
- [x] 明确隐藏 SDK 调用、Provider 内部重试和货币定价边界；固定流程继续用 typed workflow。
- [x] 全模块与相关包 race、文档链接、独立审核通过后分别提交，发布前按既有兼容流程处理。

## 执行说明

实际回归名称以 builder/agent_usage_test.go、agent_usage_process_test.go、core/usage、core/budget/attribution*_test.go、core/observe/metadata_test.go 和 Provider 协议测试为准。U1 的身份/并发/回放用实际父子 Builder 及既有准入测试验证，不机械复制计划中的测试名。U4 通用案例为 examples/agent-usage，业务案例不加入核心；未修改其他仓库。状态由 KnownFields 推导，模型元数据并入 Event.Usage，避免重复状态或 Model 字段。

Ruling: v1 不支持字段 mask，partial 在旧账本保守保留 unknown，通知不声称已可靠登记；需要可靠 partial 使用新运行的 v2。代价是旧账本无法累计这类新格式的部分已知数值，避免给出伪精确账单。

Ruling: SDK scalar 无法区分缺失与显式零，Provider 保守标 unknown；宿主自定义 Usage 可显式声明零。代价是部分供应商报表完整性较保守。

独立审核发现并用 RED→GREEN 修复：panic 重试复用旧证据、v1 partial 错误提交标记、未知 Total 差额、Gemini 错误/取消与跨帧证据丢失。完整验证与性能边界见[交付报告](../../evaluation/2026-11-01-multi-agent-usage-implementation/REPORT.md)。
