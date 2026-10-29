# 嵌套恢复与失败处理：需求说明

日期：2026-10-29  
状态：已实施（2026-10-29），与原文的差异见第 4 节  
代码基线：`c167cf9`（`codex/composable-orchestration-r2`）  
来源：[定位决策](../../research/2026-10-29-goagent-positioning.md) 第 5 节第 1、2 项；[R2 需求](2026-10-27-composable-orchestration-requirements.md) FR-02 第 7、8 条、FR-06 第 4 条；[复审 R-02](../../research/2026-10-27-framework-orchestration-review.md)

实施顺序：先升级操作记录格式（第 0 节），再做失败处理（第 2 节，范围小，可独立交付），最后做嵌套恢复（第 1 节）。

## 0. 共同基础：操作记录 v2

当前 `OperationRecord`（`core/durable/operation.go`）只有 `pending`、`completed`、`failed` 三种状态，失败原因只记 `FailureKind`：`ambiguous`（执行中崩溃）、`execution`、`canceled`、`deadline`，不保存错误原文。

v2 新增：

| 字段 | 用途 | 使用方 |
| --- | --- | --- |
| `Status: "waiting"` | 子流程正常暂停（如等待审批），既不是成功也不是失败 | 第 1 节 |
| `Continuation` | 等待状态的续跑定位信息（JSON，不含业务正文） | 第 1 节 |
| `Reconciled` | 该记录由核对补全，而非本次执行得出 | 第 2 节 |

快照版本改为 `goagent-operation-v2`。读取时接受 v1 并按现有语义解释，写入一律用 v2。v1 记录不会被重新解释成 waiting。

---

## 1. 嵌套恢复与审批续跑

### 1.1 要解决的场景

父 Agent 通过工具调用子流程，子流程需要中途暂停或可能被中断，之后父流程要能接着跑完。例如：

> 退款 Agent 调用 `refund_workflow` 工具，工具内部是一个 `DurableGraph`，在“财务审批”节点暂停。审批人批准后，父 Agent 应从这次工具调用处继续：子图跑完剩余节点，结果交还模型，模型给出最终回复。已完成的模型回合不重新请求，已执行的退款不重复执行。

### 1.2 现状与缺口

| 情况 | 当前行为（代码依据） | 问题 |
| --- | --- | --- |
| 子流程进入审批等待 | `DurableGraph.AsRunnable` 把等待当作成功返回，快照 `Waiting=true`（`workflow/durable_runnable.go`）。包成工具后，宿主必须在 `Encode` 里拒绝它，否则等待会被编码成成功结果交给模型 | `ErrToolWorkflowWaiting` 已定义（`tools/runnable_tool.go:20`），但框架里没有任何地方产生它，只能靠宿主自己返回 |
| 宿主按约定返回等待错误 | `durable.Do` 记为 `failed/execution`，与真正失败无法区分 | 父 Agent 本次执行失败 |
| 审批通过后重新运行父 Agent | `RunnableTool` 的策略是 `MaxAttempts: 1`（`tools/runnable_tool.go:124`），记录为 failed 时直接返回 `OperationError`，不再进入 factory | 父流程无法续跑，只能由宿主绕开父 Agent 手工收尾 |
| 子流程执行中进程崩溃 | 记录停在 `pending/ambiguous`，恢复时同样返回 `OperationError` | 即使子流程内部的每一步都已持久化，也不能续跑 |

已经具备的条件：`durable.Do` 给子流程派生的执行键是 `ChildKey(父键, "tool/"+CallID)`，由父键和调用 ID 确定，重启后不变。所以只要子流程用同一个 Store 并接上这个派生键，它内部的模型回合、工具调用和图检查点在重新进入时都会回放，不会重复执行。缺的是允许重新进入的契约，以及能表达“等待”的状态。

### 1.3 需求

**R1-1 可续跑工具声明。** 新增可选接口 `tools.ResumableTool`。声明它即承诺：子流程的全部模型调用、工具调用和状态都记录在派生执行键下，重新进入是安全的。`NewRunnableToolFactory` 增加配置项开启该声明。factory 每次都必须用同一 Store 和派生作用域构建子 Agent 或子图；首次调用时检查 `durable.HasJournal`，没有日志就在调用子流程前失败。

**R1-2 等待信号。** `core/durable` 新增 `WaitError{Continuation json.RawMessage}`。`DurableGraph` 嵌套在工具里运行（上下文中有父执行键）时，审批暂停返回 `WaitError`，不再把等待当成功返回；独立运行时的现有行为不变。`durable.Do` 遇到 `WaitError` 记为 `waiting`，保存 Continuation，等待不消耗尝试次数。

**R1-3 等待向上传递。** Dispatcher 把 `waiting` 原样向上返回，不当作工具结果交给模型。Builder 的持久回合遇到等待时，本轮已提交的模型回合保留，本次执行以“等待”结束：

- `AsRunnable().Invoke` 返回专用错误 `builder.ErrWaiting`，可以用 `errors.As` 取出等待列表；
- 每个等待项包含：父执行键、调用 ID、工具名、Continuation；
- 同一轮并发的多个工具调用，已完成的照常记录，只有等待的那几个列出。

**R1-4 续跑入口。** 宿主用同一执行键再次调用父 Agent，并通过 context 带上续跑输入（例如 `durable.WithResume(ctx, callID, approvalKey)`）。执行过程：

1. 父 Agent 回放已完成的模型回合，不发新请求；
2. 到达等待中的工具调用，`ResumableTool` 重新进入 factory，把审批键交给子图；
3. 子图从检查点继续，跑完后记录改为 completed，结果交还模型；
4. 父 Agent 继续后续回合。

审批是否有效由宿主判断，和现有 `ApprovalKey` 的约定一致。

**R1-5 中断后续跑。** 对 `ResumableTool`，`pending/ambiguous` 记录允许重新进入，受 `MaxAttempts` 限制。没有声明 `ResumableTool` 的工具维持现状，返回 `OperationError` 交给宿主处理。

**R1-6 预算与计量。** 回放的模型回合不占新额度（沿用 FR-06）；续跑中真正新发的请求照常占用根预算；根预算账本本来就持久化，续跑不会重置额度。

**R1-7 边界不变。** 嵌套深度上限 8、父 deadline 不能被延长、子会话独立，这些规则不变。不支持子流程脱离父生命周期后台运行，也不支持跨主机接管。

### 1.4 验收用例

1. 父 Agent → 子图审批暂停：父执行返回 `ErrWaiting`，模型请求数等于暂停前的实际请求数，没有向模型交出等待结果。
2. 审批后续跑：已完成模型回合的新增请求为 0，子图只执行审批之后的节点，退款效果键只执行一次，最终答案正确。
3. 在子图执行中杀掉进程后重启续跑：子图内已完成节点不重跑；未声明 `ResumableTool` 的工具仍返回 `OperationError`。
4. 同一轮两个工具并发，一个完成、一个等待：续跑时完成的那个只回放、不重新执行。
5. 续跑时提供错误的审批键：被拒绝，状态不变。
6. 子 Agent 没有接日志却声明 `ResumableTool`：首次调用就失败，子模型零请求。
7. 嵌套两层（父 Agent → 子 Agent → 孙图审批）：等待项的路径完整，续跑能一路回到孙图。
8. 读取 v1 记录的回归测试：旧记录的行为与当前一致。

### 1.5 涉及代码

`core/durable`（记录 v2、`WaitError`、续跑 context）、`tools`（Dispatcher 处理 waiting、`ResumableTool`、`RunnableTool` 策略）、`builder`（持久回合遇到等待时停止、`ErrWaiting`）、`workflow`（嵌套运行时返回 `WaitError`），以及退款业务案例的端到端测试。

---

## 2. 失败处理：业务结果、结果核对与就近重试

### 2.1 要解决的问题

| 问题 | 当前行为（代码依据） |
| --- | --- |
| A. 业务拒绝被当作故障 | 工具用 `error` 表达“订单不存在”这类结论时，Builder 直接中止整次执行（`builder/tool_events.go:245`）；恢复时 Dispatcher 对普通工具默认 `RetryFailed: true`、`MaxAttempts: 3`（`tools/dispatcher.go:340`），白白重试 |
| B. 崩溃后结果未知 | 记录停在 `pending/ambiguous`，普通工具恢复时直接重跑；做不到幂等的工具无法先确认操作是否已生效 |
| C. 临时故障冒到最外层 | 原生模型路径不做隐式重试（FR-06 第 4 条）。子 Agent 的一次 429 变成 `RunnableTool` 的失败，而它只允许尝试一次，整条流程停下等人处理 |

### 2.2 设计思路

不引入一套错误分类体系，而是按失败的性质把它放到对应的位置处理：

1. **业务结论是结果，不是错误。** 模型需要知道“订单不存在”才能如实回复用户。`error` 只表示调用没有跑完。
2. **结果未知时先核对，再决定是否重跑。** 框架已经给每次调用一个稳定的 `ToolInput.ExecutionKey`，接口注释要求有副作用的服务用它做幂等（`interfaces/tool.go:132`），可以用这个键去目标系统查询。
3. **在故障发生的地方重试。** 临时故障在最内层的模型调用上退避重试，不冒到复合工具。复合工具保持一次尝试；进程崩溃交给第 1 节的嵌套续跑处理。

不经 Dispatcher 的工具已经支持第 1 点：`Success=false` 的结果会作为普通工具消息交给模型（`builder/tool_events.go:264`）。但持久模式必须经 Dispatcher，而 Dispatcher 把参数错误以外的 `Success=false` 结果一律转成 `tool %q failed` 错误，所以需要让 Dispatcher 识别业务拒绝（见 R2-1）。

### 2.3 需求

**R2-1 业务拒绝辅助函数与约定。** `tools` 新增 `Rejected(message string, data any) *interfaces.ToolOutput`，返回 `Success=false`，`Result` 为 `{"message": ..., "data": ...}`（与 Builder 现有提取 `message` 字段的逻辑一致）。文档与工具接口注释写明：

- 目标系统明确给出的结论（不存在、无权限、余额不足、状态不允许）用 `Rejected` 返回；
- `error` 只用于调用没有完成（网络中断、服务不可用、超时、宿主侧错误）。

直接返回工具收到 `Rejected` 时沿用现有行为：不作为最终答案，继续下一轮模型请求。

存量迁移：逐个检查 `tools/` 内建工具和业务案例中用 `error` 表达业务结论的地方，改为 `Rejected`。

**R2-2 结果核对接口。** 新增可选接口：

```go
type ReconcilableTool interface {
    Reconcile(ctx context.Context, input *interfaces.ToolInput) (out *interfaces.ToolOutput, found bool, err error)
}
```

`input` 带有与原调用相同的 `ExecutionKey`、`CallID` 和 `Args`。`durable.OperationPolicy` 新增可选的 `Reconcile` 函数，Dispatcher 为实现该接口的工具填入。`durable.Do` 在持有记录锁时遇到 `pending` 记录：

| 核对结果 | 处理 |
| --- | --- |
| `found=true` | 用返回结果把记录写为 completed，`Reconciled=true`，不执行工具，不消耗尝试次数 |
| `found=false` | 按现有策略重新执行 |
| 返回错误 | 记录不变，本次调用返回该错误 |

不实现核对的工具保持现状：

- 遵守执行键幂等约定的工具，照常重跑；
- 既不能幂等也不能核对的工具，用已有的 `DurablePolicyProvider` 把 `MaxAttempts` 设为 1，结果未知时交给宿主处理。

不需要新增“幂等声明”。

**R2-3 模型调用就近重试。** Builder 新增 `WithModelRetry(maxRetries int, backoff func(attempt int) time.Duration)`，默认 0，即保持当前不重试的行为。只作用于原生 `ToolStreamClient` 路径，规则如下：

1. **可重试的情况**：HTTP 429、5xx，以及收到响应前的连接错误。400、401、403、404、422 及其他 4xx 不重试。
2. **已输出内容时不重试**：本回合已向消费者发出过文本增量或工具调用片段时不重试，避免重复内容。
3. **预算**：每次重试先通过根预算准入（FR-06），额度不足时在发送前失败；重试计入 `model_calls`，不计入 `MaxIterations`。
4. **等待时间**：429 带 `Retry-After` 时按它等待，否则用 `backoff`。等待超过剩余截止时间时立即失败，不延长父 deadline。
5. **持久模式**：重试发生在同一个模型回合操作内部，日志仍只有一条回合记录；失败尝试已取得的用量按 FR-06 第 5 条登记。

Provider 需要提供的错误信息和实现约束见 2.4 节。

子 Agent 是用 Builder 构建的，配置了这一选项后，子流程内部的临时故障在子流程里就被消化，不会冒到父流程的 `RunnableTool`。

**R2-4 观测。** `core/observe` 新增两类事件：

- `model_retry`：记录尝试序号、状态码类别、等待时长；
- 工具返回 `error` 时标注是否为 `Rejected` 以外的失败，按工具统计次数，用于发现仍在用 `error` 表达业务结论的工具。

### 2.4 Provider 错误信息现状与实现约束

基于代码核对（2026-10-29，go-openai v1.41.2、genai v1.76.0）。

#### 原生流路径的错误来源

| Provider | 原生流实现 | 状态码 | `Retry-After` | 请求前失败与流中失败能否区分 |
| --- | --- | --- | --- | --- |
| OpenAI | go-openai `CreateChatCompletionStream`（`llm/providers/openai_stream.go`） | 有：`*openai.APIError` / `*openai.RequestError` 的 `HTTPStatusCode`；外层 `NewLLMRequestError` 用 `Wrap` 保留原因（`errors/helpers.go:180`），可用 `errors.As` 取出 | **没有**：SDK 错误类型不带响应头 | 能：建流失败由函数返回；之后的失败放在终止块的 `Error` 里 |
| DeepSeek | 复用 OpenAI 实现，换了 BaseURL 和自定义 `HTTPClient`（`llm/providers/deepseek_stream.go:19`） | 同 OpenAI | **没有**，但已有自定义 HTTP 客户端，可以在这里截取响应头 | 同 OpenAI |
| Gemini | genai `GenerateContentStream`（`llm/providers/gemini.go:204`） | 有：`genai.APIError.Code`（值类型，需用 `errors.As(err, &genai.APIError{})` 的值形式）；流中错误原样返回、未包装 | 不走响应头；429 的等待时长在 `APIError.Details` 的 `google.rpc.RetryInfo.retryDelay` 里（如 `"30s"`） | 能：首个分片在返回通道前同步拉取，首帧前的错误由函数返回 |

非流式旧路径另有一套：`common.MapHTTPError` 把 429 转成 `NewLLMRateLimitError` 并解析 `Retry-After`（`llm/common/base.go:368`），`common.ExecuteWithRetry` 做退避。原生路径没有用到它们。`common.ParseRetryAfter` 和现有的退避与抖动计算可以复用。

#### 需要改动的地方

1. **统一的 Provider 错误类型。** `llm` 包新增 `ProviderError{StatusCode int; RetryAfter time.Duration; BeforeResponse bool; Err error}`，由各 Provider 构造，Builder 只认这个类型，不导入任何 SDK 的错误类型。判断规则放在 `llm/providers` 内部：
   - OpenAI/DeepSeek：`errors.As` 取 `*openai.APIError` 或 `*openai.RequestError` 的状态码；
   - Gemini：取 `genai.APIError.Code`，并从 `Details` 解析 `RetryInfo`；
   - 没有状态码且不是 context 错误的网络错误，视为连接错误，可重试。
2. **补上 OpenAI/DeepSeek 的 `Retry-After`。** go-openai 拿不到响应头，需要在 HTTP 客户端这一层截取。DeepSeek 已经有 `deepSeekStreamHTTPClient`，OpenAI 也换成同样的包装。每次请求在 ctx 里放一个接收器，包装层遇到 429 时把 `Retry-After` 写进去，Provider 构造 `ProviderError` 时读出。
3. **Gemini 流中错误补上包装。** 让它也带上 `ProviderError`，与 OpenAI 路径一致。

#### 预算与持久化的约束

4. **每次尝试用独立的预算键。** 根预算按操作键预留，同一键第二次预留返回 `ErrReservationExists`（`core/budget/controller.go:164`）。重试必须用确定性派生的新键（如 `ChildKey(操作键, "attempt/2")`），用量也按这个键登记。这样每次尝试都占一份额度，崩溃后已预留的额度保守保留（符合 FR-06 第 2 条）。
5. **重试放在模型回合操作内部。** 重试循环放在 `modelOperation`（`builder/model_call.go:53`）的 `call` 里：持久日志仍只有一条回合记录。重试中途崩溃时，记录停在 pending，恢复时返回需宿主处理的错误，与当前模型回合的行为一致。
6. **重试配置不进入持久意图。** 改变重试次数不应让已有运行无法恢复。
7. **判断是否已输出内容。** `readToolTurn` 需要返回本回合是否已向消费者发出文本增量或工具事件。建流失败，或第一个分片就是错误时，可以重试；否则不重试。

### 2.5 验收用例

1. 工具返回 `Rejected`：模型收到 message，执行不中止，日志为 completed；恢复时只回放，工具零执行。
2. 工具返回 `error`：执行中止，恢复时按现有策略重试（回归）。
3. 执行中崩溃、工具实现 `Reconcile` 且 `found=true`：工具零执行，记录为 completed 且 `Reconciled=true`，尝试次数不变。
4. 同上但 `found=false`：工具执行一次；`Reconcile` 返回错误时记录不变、执行报错。
5. 不实现核对、`MaxAttempts=1` 的工具执行中崩溃：返回 `OperationError`（回归）。
6. 模型第一次返回 503、第二次成功，`WithModelRetry(2, …)`：只得到一个答案，`model_calls=2`，根预算占用 2，持久日志只有一条回合记录。
7. 429 的 `Retry-After` 超过剩余截止时间：不等待，立即失败。
8. 已发出文本增量后连接中断：不重试，直接失败。
9. 返回 400：不重试。
10. 根预算只剩 1 次额度：重试在发送前被拒绝。
11. 子 Agent 内模型调用遇到一次 429 后成功：父 Agent 看到的是成功的工具结果，`RunnableTool` 记录为 completed。
12. 默认配置（不开启重试、工具不实现新接口）：行为与当前一致。
13. 一次回合重试两次：预算账本中有三个不同的预留键，失败尝试的已知用量各自登记、不重复累加。
14. 持久模式下在第二次尝试前杀掉进程：恢复时不再发请求，返回需宿主处理的错误，已预留的额度不退回。
15. OpenAI 兼容服务返回 429 和 `Retry-After: 2`：等待约 2 秒后重试（用注入的时钟测试）；DeepSeek 路径行为相同。
16. Gemini 返回 429，`Details` 中 `retryDelay` 为 `"3s"`：按 3 秒等待。
17. 修改重试次数后恢复已有运行：不因意图变化被拒绝。

### 2.6 涉及代码

`tools`（`Rejected`、`ReconcilableTool`、Dispatcher 填入核对函数）、`core/durable`（`OperationPolicy.Reconcile`、`Reconciled` 字段）、`builder`（`WithModelRetry`、`modelOperation` 内的重试与按尝试派生预算键、`readToolTurn` 返回是否已输出）、`llm`（`ProviderError`）、`llm/providers`（OpenAI、DeepSeek、Gemini 构造 `ProviderError`，OpenAI/DeepSeek 的 HTTP 包装层截取 `Retry-After`，Gemini 解析 `RetryInfo`）、`core/observe`（新增事件），以及内建工具与业务案例中业务结论的迁移。

---

## 3. 不在范围内

- 后台自动重试调度、跨主机接管、分布式租约；
- 子流程脱离父生命周期后台运行；
- 自动补偿（撤销已执行的副作用）；
- 等待的超时与提醒（由宿主处理）；
- 工具层的通用自动重试：工具内部的临时故障由工具自身处理（如 HTTP 客户端自带重试），框架只在恢复时按现有策略重跑。

需要这些能力时，优先评估适配成熟工作流系统（如 Temporal），而不是在 FileStore 上自建。

## 4. 实施记录

实施于 2026-10-29，基线 `c167cf9`。验证：`go test -race -count=1 ./...` 226 个包全部通过；本次改动文件在 golangci-lint v2（latest）下无新增问题，lint 报告的 `tools/dispatcher.go` 164、190 行为既有问题；`scripts/verify_imports.sh` 报告的 `utils/httpclient` 导入 `errors` 来自既有提交 `c34f4b2`，与本次无关。

与上文需求的差异：

| 需求 | 实际实现 | 原因 |
| --- | --- | --- |
| R1-1 新增 `tools.ResumableTool` 接口 | 改为 `RunnableToolConfig.Resumable` 配置，通过 `DurableOperationPolicy` 交给 Dispatcher；可续跑工具的 `MaxAttempts` 为 3 | 策略本来就经 `DurablePolicyProvider` 传递，不需要另一个接口；`MaxAttempts: 1` 会让中断后的 pending 记录永远无法重新进入 |
| R1-2 图在“嵌套运行”时自动返回 `WaitError` | 显式调用 `DurableGraph.AsChildRunnable()`，运行 ID 取当前执行键，审批键从 `durable.ResumeInput` 读取；`ChildContinuation` 含 `run_id`、`node`、`approval_key` | 自动判断“是否嵌套”会改变图嵌在另一个持久图节点里的现有行为 |
| R1-4 续跑输入 | `durable.WithResume(ctx, key, input)`，key 取等待路径最后一项的 `Key` | 与等待发生处的执行键一一对应，多处等待可分别提供 |
| R1 补充 | 审批未被采纳（键不符、被 `WithBeforeApprove` 拒绝）时仍报告为等待并标记 `Rejected`；图已越过审批时忽略续跑输入 | 否则一次错误的审批会把记录写成 failed，之后再也无法续跑；中断后宿主再次调用时通常仍带着同一审批输入 |
| R1-2 `durable.Do` 遇到 `WaitError` 记为 `waiting` | 只有声明 `Resumable` 的操作记为 `waiting`；其他操作记为 `failed/not_resumable` 并返回 `durable.ErrWaitNotResumable`，再次调用返回 `OperationError`。`durable.CanWait(ctx)` 报告最内层操作能否承载等待，`AsChildRunnable` 不满足时在执行任何节点前失败 | 不可续跑的操作以后不会重新进入，记成等待后每次只返回已存的等待，审批后用 `WithResume` 也续不上，且没有诊断；在子图入口提前检查可避免执行审批前的节点 |
| R1-3 `builder.ErrWaiting` | `ErrWaiting` 哨兵加 `*WaitingError{Waits}`；同一轮并发调用中有真失败时优先报告失败 | 等待不能掩盖同轮的真实失败 |
| R2-1 `Rejected` 的 `Result` 为 `map` | `Result` 为包外无法构造的类型，JSON 编码为 `{"error":"rejected","message","data"}`；`RejectedResult` 只认该类型。Dispatcher 日志在 `ToolOutput` 字段之外写 `goagent_rejected` 标记，回放时重建 | 按形状识别时，透传上游 JSON 的工具拿到同形状数据，失败会被当作已完成的业务拒绝；标记放在工具写不到的层级，经 JSON 往返后仍可识别 |
| R2-3 重试计入 `model_calls` | 计入 `new_model_invocations`（实际请求数），不计入 `model_calls`（文档定义为逻辑回合数） | 与 `COMPOSABLE_ORCHESTRATION.md` 已有的指标定义一致 |
| R2-3 预算键 | 第 N 次重试的预算键为 `ChildKey(回合键, "retry/N")`；持久回合记录新增 `model_attempt` 字段，恢复时据此找回登记用量的键 | 账本中同一键不能重复预留，已登记的用量不可改写 |
| R2-4 观测 | 每次重试一对 `model_retry` 事件（路径 `retry/N`、错误类别），不在 `Event` 上新增状态码与等待时长字段；工具失败沿用已有的 `tool_error` 事件统计，未另加计数；`ErrorKind` 对等待返回 `waiting` | 不改变 JSONL 事件结构 |
