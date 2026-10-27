# 执行契约与能力状态

## 推荐入口

新编排统一使用 `core.Runnable[I, O]`。现有 `core.Agent` 是该泛型接口在 `AgentInput/AgentOutput` 上的专用接口；旧 `interfaces.Agent/Runnable` 已标记弃用，不再扩展。类型不同的 Runnable 需要显式转换输入输出，不进行隐式消息丢弃。

Builder 通过 `AsRunnable()` 接入相同契约：

```go
agent, err := builder.NewSimpleBuilder(client).
    WithRetriever(retriever).
    WithTools(lookupTool).
    Build()
if err != nil { return err }
runnable := agent.AsRunnable()
output, err := runnable.Invoke(ctx, "退款政策是什么？")
// output 是经过检索、工具执行、中间件和完成回调后的 *builder.AgentOutput。
```

`Invoke` 和 `Stream` 共用 `ExecuteStream` 执行源；Invoke 只收集最终输出，**不会再额外调用一次同步模型**。这条入口要求模型实现 `llm.StreamClient`，配置工具时还要求工具调用能力。旧 `Execute()` 不自动调用工具的语义仍保留，因此迁移到 AsRunnable 后会执行已配置工具。

以下对照用于选择入口；旧 API 示例不代表新能力在旧路径也生效。

| Builder 入口 | 工具与上下文策略 | 同一 Builder 的并发保护 | 用途 |
|---|---|---|---|
| `AsRunnable().Invoke/Stream` | 自动执行已配置工具；原生 `ToolStreamClient` 路径支持 ContextPolicy 和 ReturnDirect | 所有入口共享串行执行门 | 新业务与 Runnable 编排的推荐入口 |
| `ExecuteStream` | 与 AsRunnable 共用执行源及能力 | 获取同一串行执行门 | 直接消费 Builder 事件 |
| `Execute` | 不自动调用工具；配置 ContextPolicy/ReturnDirect 时明确报错 | 获取同一串行执行门 | 保留已有同步调用语义 |
| `ExecuteWithTools` | 支持 StreamClient 时委派 AsRunnable；否则保留旧工具循环 | 获取同一串行执行门 | 兼容存量工具调用 |

仅实现 `StreamClient` 不等于支持原生工具流。配置 ContextPolicy 或 ReturnDirect 时还须满足 `ToolStreamClient` 等构建校验；不同 Provider 的能力不能由方法名称推断。

```go
ctx, cancel := context.WithCancel(ctx)
defer cancel()
chunks, err := runnable.Stream(ctx, "退款政策是什么？")
if err != nil { return err }
for chunk := range chunks {
    if chunk.Error != nil { return chunk.Error }
    if chunk.Event != nil {
        // Event.Type: sources、step、text、usage、done。
        // Event.Delta 是新增文本；Event.Usage 是累计用量快照，不要再次求和。
        fmt.Print(chunk.Event.Delta)
    }
    if chunk.Done {
        // Data 是最终输出，中间件可能修改它；中间文本仅供暂态展示。
        fmt.Println(chunk.Data.Result)
    }
}
if err := ctx.Err(); err != nil { return err }
```

标准流以一个 `Done=true` 块终止：`Error != nil` 表示失败，只有无错误才表示成功。取消时通道关闭，终止块尽力发送，以 `ctx.Err()` 为准。提前停止读取必须取消 context。

`NewStreamRunnable` 显式绑定执行源，统一支持 Batch、Pipe、WithConfig、WithCallbacks。执行源必须串行发送，恰好提交一个最终块，并传播 emit 错误；缺少最终块、重复终止、终止后出错和回调失败都不会产生成功块。

同一 Builder 的状态和记忆属于一个会话。Execute、ExecuteWithTools、ExecuteStream 及所有 AsRunnable 适配器共享可取消的串行执行门，配置的 Timeout 包含排队时间，Batch 保持输入顺序。跨会话并发应创建独立 Builder。工具或 Builder 回调不能重入调用同一个会话；尚未消费完事件时应先取消原调用再发起新调用。Initialize、Shutdown 及配置变更仍应由调用方安排在执行前后。Runnable 回调是额外的编排层回调，Builder 已配置的回调仍独立执行，不要把同一个副作用回调注册两次。

`Pipe` 的现有语义是等待上游最终输出，再执行下游；不会逐 Token 变换上游流。通用编排使用新的 [workflow.Graph[T]](../../workflow/README.md)，支持条件分支、显式并行 Clone/Merge 和有界循环；仍不提供跨节点逐块 Transform。

编排与恢复按入口区分：

| 组件 | 已提供 | 不应据此推断 |
|---|---|---|
| `workflow.Graph[T].Compile` | 同一状态类型的分支、并行汇合、有界循环、节点事件转发 | 异构字段自动映射、跨节点流变换、自动持久化 |
| `workflow.Graph[T].CompileDurable` | 节点及并行分支检查点、条件分支、循环预算、审批和单机重启恢复；AsRunnable 统一入口 | 节点内部调用栈恢复、持久化流事件重放、跨主机接管 |
| `core/durable` | 已注册顺序步骤的持久化、审批、进程重启恢复 | 自动保存任意 Agent 调用栈、自动恢复 workflow 图 |

`core/durable` 当前使用本地文件存储，步骤按至少一次语义执行；外部写操作需要服务端幂等。`CompileDurable` 通过同一 Store 保存状态和节点位置；拓扑变化拒绝旧检查点，函数或状态结构变化须提升版本。业务写入须使用 `workflow.DurableKey(ctx)` 或业务请求 ID 去重，宿主负责审批授权。详见 [workflow 恢复契约](../../workflow/README.md#节点边界的持久化恢复)。

## 可选上下文治理

`WithContextPolicy` 为原生工具流增加执行内存中的大结果卸载、分页读取和显式历史摘要。默认关闭；摘要计入 MaxIterations、model_calls 和累计用量，未知用量不补零。最后一轮恢复已卸载原文，避免只剩无法读取的引用。它不限制进程内存、不恢复跨进程工具证据，也不应用于旧 Execute。详见[上下文策略](CONTEXT_POLICY.md)。

入门代码统一见[快速入门与迁移](QUICKSTART.md)。主模块复杂流程使用 [workflow.Graph[T]](../../workflow/README.md)。

## 减少模型请求与 Token

OpenAI、DeepSeek 与 Gemini Provider 实现可选 `llm.ToolStreamClient`：模型每轮可以选择工具或直接流式回答。典型一次工具任务从三次请求（调用工具、确认 READY、生成答案）减少到两次；不需要工具时只请求一次。Builder 自动优先使用该能力，`AsRunnable().Invoke` 与 `ExecuteStream` 共用优化路径。旧 Execute 保留同步语义；ExecuteWithTools 对支持 StreamClient 的 Provider 委派统一入口，对非流式 Provider 保留兼容循环。该迁移会统一完成回调和原生模型调用预算，已有调用方应检查预算配置。

新路径将系统指令只放在 system 消息中，将历史保留为原生角色消息，工具结果通过 tool_call_id 关联到 assistant.tool_calls。检索指令和参考资料各发送一次。保留模型要求的 reasoning_content 用于工具续接，但不会作为答案文本展示。未静默截断历史或工具结果，也没有引入答案缓存。

`MaxIterations` 在原生工具流中限制全部模型请求（包含最终答案）；额度耗尽时明确失败，不再执行无法供下一轮使用的额外工具。显式开启的单工具直接返回无需下一轮，因此允许在最后一次请求后执行。累计用量包含全部回合，元数据 `model_calls`、`usage_reports`、`usage_complete` 标识调用次数和用量是否完整；未知用量不伪造。

已有工具结果且只剩最后一次模型请求时，Builder 不再提供普通工具，把剩余额度用于基于现有结果回答；未配置直返工具时标记 `final_answer_only=true`。显式配置的直返工具仍可供选择，因为它们无需额外请求收尾。首轮的单请求直返不受影响；有更多额度时仍可连续查询。如果最后回合返回无法在本轮结束的工具调用，明确失败且不执行。该策略不保证已有证据足以回答，业务提示仍须要求缺乏证据时如实说明。

应用系统提示应区分“原样返回工具结果”和“根据证据推理”：前者的合法状态值不能仅因看起来像空值而改成拒答，后者不能因工具返回成功就认定证据充分。评测应用已修正这处提示冲突；框架不会覆盖调用方的 `WithSystemPrompt`。

原生流的 `model_start` 步骤标记新模型回合。模型可能在工具调用前输出说明文字，因此所有 text 增量都是暂态内容；最终答案必须读取 done.Output.Result。需要逐回合展示时在 model_start 重置当前文本面板。中断、输出截断、参数损坏或调用 ID 重复不会执行不完整的工具调用。

OpenAI 和 DeepSeek 专用 Provider 共用原生 SSE 解码与完成状态检查。DeepSeek 可直接通过 `providers.NewDeepSeekWithOptions` 接入，无需借用 OpenAI 构造器；新流式路径不执行隐式重试。其他 Provider 若未实现 ToolStreamClient，仍保留已有兼容能力，不因为声明 streaming 就假定支持原生工具循环。

运行时用 `llm.HasStreaming(client)` / `llm.HasToolStreaming(client)` 检查实际接口。声明中的 `structured_streaming` 与 `tool_streaming` 分别对应 StreamClient 和 ToolStreamClient；旧的 `streaming` 也可能仅表示字符串流。接口可用不保证所选模型或自定义端点支持相同协议，仍需业务端点测试。

## 流式能力声明

通过可选 `core.StreamCapability.StreamMode()` 查询：

| 入口 | 能力 | 边界 |
|---|---|---|
| Builder.AsRunnable | tokens | 原生答案增量；支持 ToolStreamClient 时工具与答案共用原生对话循环，其他 Provider 保留兼容路径 |
| ReAct.Stream | steps | 推理步骤和工具结果，不是逐 Token 输出 |
| ToT 和其他基类最终块实现 | final | 完整结果后返回一个块 |
| SelfReflectiveAgent.Stream | final | 委派真实任务后返回最终结果 |
| 旧 StreamingLLMAgent.ExecuteStream | 兼容展示 | 完整模型响应后分块，不改善首字延迟 |

旧消息式接口的迁移从本次开始：不新增调用方；现有调用方迁移到泛型接口后，才在明确宣布的下一个主版本删除。当前没有发布删除日期或承诺所有旧接口已消失。

## 能力状态

这里的“已验证”表示存在本地回归测试，不表示已经完成生产负载验证。

| 状态 | 能力 | 已验证与限制 |
|---|---|---|
| 已验证入口 | Builder RAG、原生事件流、Runnable 适配 | 来源限制、用量、错误、取消和组合执行；不自动证明事实正确性 |
| 已验证入口 | 官方 MCP 客户端适配 | HTTP/stdio 工具互操作；交互授权仍由应用处理 |
| 已验证入口 | core/durable | 显式顺序步骤、审批和真实进程重启；至少一次执行，外部服务按幂等键去重 |
| 实验 | 多种推理策略、自动反思与学习 | 需要业务题集验证；策略多不等于成功率高 |
| 基础设施集成 | NATS、Redis、PostgreSQL | NATS 当前消息路径为 Core Publish/Subscribe，不能据此宣称持久化消费和故障重投递 |
| 词汇基线 | LexicalOverlapReranker | 词汇重合打分，无交叉编码器推理 |
| 弃用 | CrossEncoderReranker、interfaces.Agent/Runnable | 保留迁移入口，不再作为新开发推荐 |

自反思 Agent 不再返回固定的 `Task completed`。必须传入 `reflection.WithTaskExecutor(executor)`，其中 executor 实现 `Invoke(context.Context, *core.AgentInput) (*core.AgentOutput, error)`。未配置时返回 `ErrTaskExecutorRequired`，执行失败、nil 输出和 partial 状态不会被伪装成成功。自动反思本身仍是实验能力，不代表任务效果已经改善。

原 `NewCrossEncoderReranker(model, topN)` 迁移为 `NewLexicalOverlapReranker(topN)`；如需真实模型排序，使用 `NewLLMRerankerWithClient` 或 `CohereReranker`。

OpenAI 兼容 Provider 改为发送 `tools` 并解析 `tool_calls`，保留旧 `function_call` 响应解析。工具流按索引合并并发调用，流中错误通过 error 事件返回。仅接受旧请求协议的服务需升级兼容端点，不会通过隐藏重试重复调用模型。

## 对照与下一步

终态工具可使用 `WithReturnDirectTools("order_status")` 减少一次模型请求：

```go
agent, err := builder.NewSimpleBuilder(client).
    WithTools(orderStatusTool).
    WithReturnDirectTools("order_status").
    WithMaxIterations(1).
    Build()
// 检查 err 后，通过 agent.AsRunnable().Invoke(ctx, question) 执行。
```

仅 `ExecuteStream/AsRunnable` 原生工具路径生效，Build 会验证 Provider 支持 `ToolStreamClient` 且名称唯一注册。本轮只有一个工具且命中名单时，成功工具的字符串直接输出，其他结果 JSON 编码；工具失败、空结果与取消不会转成成功。最终答案必须已经满足业务展示要求，模型不会再总结、补引用或执行输出格式指令。多工具调用继续模型循环，所以需要更大的 MaxIterations。`ExecuteWithTools` 在流式 Provider 上也应用此设置；旧 `Execute` 会明确报错并提示迁移，避免静默忽略。

直接返回仍经过记忆、状态、检查点、中间件和回调，元数据标明 `answer_source=tool`、`return_direct_tool`。工具前模型说明仍属临时文本，最终以 `done.Output` 为准。[其他框架成本控制调研](../evaluation/2026-10-26-framework-cost-research.md) 解释了选择依据与后续工作。

[GoAgent 评测](../../evaluation/agentbench/README.md) 使用固定题集和请求预算验证 Builder 原生工具路径。评测已纳入主模块；它不代表所有推理策略的综合成绩，也不是纯框架开销测试。

下一步用真实业务标注替换合成题，并对格式成功、工具正确性、引用蕴含和成本分别评分。业务数据尚未提供，不用合成三题代替业务验收。

当前不新增分布式调度器。需要跨主机接管时，先明确最长任务时间、消息丢失容忍度、重复副作用要求和工作节点规模；再验证持久化队列确认、租约与 fencing、幂等记录、崩溃重投递和故障注入。FileStore 与 Core NATS 不满足这些验收条件，应复用成熟持久化消息/工作流系统，而非把本地锁包装成高可用承诺。


## 通用图执行

`workflow.NewGraph[T]().Compile(ctx)` 生成标准 StreamRunnable，单次调用、步骤流、批处理、组合和回调共用同一执行源。AddRunnable 接入同类型节点，Adapt 显式转换异构 Agent 输入与输出。默认 MaxSteps=100，每次进入节点计一步；超限通过 errors.Is(err, workflow.ErrMaxSteps) 检查。

条件路由只能返回预先声明的目标，每次选择一个后继；普通多入边不等待未选中路径。并行节点必须提供 Clone 和 Merge，按注册顺序汇合；MaxConcurrency 默认 4，约束每个并行节点的 worker。并行失败取消同组分支并等待退出，函数必须响应 context。

节点事件透传并附带 Step 作用域；节点 Done 不作为图 Done。图模式声明为 steps，不代表边上逐 Token 执行。自定义 Runnable 的缺失/重复终态和迟到错误会阻止下游。原始数据中的指针/map/slice 不自动深复制；`Compile` 没有隐式重试、事务回滚、自动持久化或跨主机接管；持久化入口 `CompileDurable` 的 JSON 状态与恢复限制见上文。

参见[完整 API 与状态边界](../../workflow/README.md)以及[离线示例](../../examples/workflow/main.go)。

## 原生工具执行与持久图事件

`tools.Dispatcher` 为图节点和 Builder 工具提供同一调度入口，详见[原生编排与工具体系](NATIVE_ORCHESTRATION.md)。默认不重试，失败结果不能转为成功，工具执行键由宿主 context 注入。

`DurableGraph.AsRunnable` 的 Stream Done 表示本次调用结束；业务完成检查 `Data.Done`。审批等待返回 `Data.Waiting=true`、`Data.Done=false`，兼容 `Run` 保留 `durable.ErrApprovalRequired`。已保存并行分支在恢复时跳过，Merge 保持无副作用并可能重做；节点内部模型循环仍作为整体执行。
