# 多框架调研与 GoAgent 可组合编排方案复审

日期：2026-10-27  
状态：R2 设计复审；仅修订文档，尚未实现  
代码基线：`3bfeea70fcce35c3e925a295cc42546c1ec80b1b`  
对应：[需求文档](../superpowers/specs/2026-10-27-composable-orchestration-requirements.md)、[实现方案](../superpowers/plans/2026-10-27-composable-orchestration.md)

## 1. 审核结论

原方案的原生渐进扩展、保留 Runnable 和检查点契约方向成立。但初版偏重接口覆盖，提前引入完整四范式流引擎、反射路径编译器和另一套 Span 标识；子 Agent 的全链路预算、清理责任与一次尝试后的恢复限制则不够充分。

R2 建议先完成受控工具化、父子预算与最小可观察性，再实现确定性的持久引用和有限上下文扩展。跨阶段流处理先用同步 Source/Map/Fold 原型验证价值；通用反射字段映射、完整四范式与独立调试执行器移入后续候选。此结论来自官方设计与本地代码对照，不是框架性能排名，也不表示其他框架缺少未调研的功能。

## 2. 调研范围与来源

只将已读取的官方文档作为外部事实依据。文档可能随 main/网站更新，以下记录访问日期而不声称锁定框架发行版本；具体 API 在 GoAgent 落地前仍需按选定版本核验。原有 Eino 调研继续作为补充。

| 编号 | 框架与官方页面 | 核实到的设计 | 本项目取舍 |
| --- | --- | --- | --- |
| S1 | [LangGraph Functional API](https://docs.langchain.com/oss/python/langgraph/functional-api) | task 输出持久化；恢复从边界重放并复用完成结果；未完成 task 可能再执行，副作用需要幂等 | 保留模型/工具细粒度日志；不能把普通 Runnable 工具包装视为可恢复子工作流 |
| S2 | [LangGraph Streaming](https://docs.langchain.com/oss/python/langgraph/streaming) | 区分消息、状态更新、自定义数据等流投影 | 区分观察事件、数据增量和已提交状态；流展示不等于图边增量调度 |
| S3 | [LangGraph Graph API](https://docs.langchain.com/oss/python/langgraph/graph-api) | 状态字段 reducer 明确接收当前值与更新值 | 保留显式 Clone/Merge；流聚合先用增量 Fold，暂不建设反射字段调度器 |
| S4 | [LangChain 自定义中间件](https://docs.langchain.com/oss/python/langchain/middleware/custom) | 节点前后钩子与模型/工具包裹钩子有不同职责；包裹钩子能多次调用 handler | 借鉴生命周期分层；首版上下文策略不获得重复调用模型的权力 |
| S5 | [OpenAI Agents SDK Tools](https://openai.github.io/openai-agents-python/tools/) | AgentAsTool 支持结构化输入和结果提取；子运行会话状态不自动继承父对话 | 保留显式 Decode/Encode、默认不复制历史；本地授权上下文与历史分别处理 |
| S6 | [OpenAI Agents SDK Context](https://openai.github.io/openai-agents-python/context/) | 本地 context 不自动发送给模型；嵌套运行不默认深复制应用状态 | 不把会话隔离误写为授权上下文完全隔离；身份、预算、截止时间显式继承 |
| S7 | [OpenAI Agents SDK Usage](https://openai.github.io/openai-agents-python/usage/) | 按请求计量；嵌套运行用量汇入外层；检查点包含已知用量 | 增加父子共用请求准入与按稳定操作键去重的用量账本；不能重复累加子输出中的 TokenUsage |
| S8 | [OpenAI Agents SDK Tracing](https://openai.github.io/openai-agents-python/tracing/) | 分层 trace/span、处理器扩展，以及敏感数据记录开关 | 复用项目已有 OTel；正文默认关闭，不新增与 OTel 平行的 Span 树 |
| S9 | [Google ADK Artifacts](https://adk.dev/artifacts/) | 产物具有会话/用户作用域与版本，支持指定版本读取 | 引用必须固定内容摘要与运行作用域；不以 latest 读取恢复证据 |
| S10 | [Google ADK Context Compaction](https://adk.dev/context/compaction/) | 上下文压缩是运行时能力，可按 Token 或窗口触发；各语言配置不同 | 保留现有字符计数含义，Token 估算另列；摘要作为受计量模型操作，不能藏在任意钩子里 |
| S11 | [Temporal Retry Policies](https://docs.temporal.io/encyclopedia/retry-policies) | 重试预算、不可重试错误和超时分开配置 | 保留复合 Agent 默认不重跑；分类重试需要新的持久错误契约，不能只增加内存 sentinel |
| S12 | [Temporal Child Workflows / Go](https://docs.temporal.io/develop/go/workflows/child-workflows) | 子工作流具有独立执行历史及父关闭行为 | 普通工具化不等于子工作流；父退出取消当前子调用，不支持脱离父生命周期继续 |
| S13 | [Temporal Activity Definition](https://docs.temporal.io/activity-definition) | Activity 可能在报告成功前崩溃后重做；幂等由目标服务落实 | 不宣称本地日志提供外部 exactly-once；粗粒度包装会扩大重做范围 |
| S14 | [Eino 流处理](https://www.cloudwego.io/docs/eino/core_modules/chain_and_graph_orchestration/stream_programming_essentials/) | 四种输入输出范式和自动适配 | 保留为后续完整流能力的参照，首轮不照搬自动适配运行时 |

S7 证明了嵌套用量归属的参考价值；全局请求准入、保守预留和具体存储协议是 GoAgent 本次设计推导，并非声称 SDK 已提供同一预算机制。S3 的状态 reducer 与本方案的数据流 Fold 也不是同一个协议，只借鉴明确更新规则。

## 3. 本地代码约束

| 证据 | 现状与影响 |
| --- | --- |
| `core/runnable.go`、`core/stream_runnable.go` | 已有唯一最终快照语义；增量文本主要在 Event.Delta，不能从中间 Data 零值猜测数据存在性 |
| `core/generator.go` | 已有 Generator/Map/Take/Collect 等基础；未采用标准 Done 检查点语义，复用前须核对取消与错误，不能直接套作新运行时 |
| `workflow/execute.go` | consumeNode 等待合法终态及 EOF 才推进下游；流管道应包为一个节点 |
| `tools/dispatcher.go` | 全程持有并发槽，默认工具尝试预算为 3；父子同实例可能等槽，改为无条件释放槽会破坏并发限制 |
| `builder/execution_gate.go` | Builder 状态与记忆属于会话，排队计入超时；子 factory 需独立会话并保留父 deadline |
| `builder/tool_events.go`、`builder/durable_turn.go` | 请求预算与用量当前属于单 Agent；回放保存的模型回合仍包含原用量，需分开累计用量与本次新增用量 |
| `core/durable/operation.go` | 记录只保存 execution/canceled/deadline 等错误类别；内存 errors.Is 不能单独解决跨进程等待和分类重试 |
| `observability/tracer.go`、`tracing.go`、`telemetry.go` | 已有 OTel TraceProvider、工具/Agent/模型 span；新增自有 TraceID/SpanID 生成器会重复建设 |
| `builder/context_policy.go` | 原文卸载、读取工具、摘要和最后回合恢复耦合；先抽取内部边界，再开放受限扩展 |

## 4. 复审发现与处置

### R-01 全链路预算缺失，严重

初版只限制 8 层深度和每个 Builder 的 MaxIterations。两个子 Agent 各自用满额度时，父配置不能解释全链路请求数。改为在暴露模型型工具之前提供根级请求准入，父、子、摘要共用；既有单 Agent MaxIterations 仍作为局部上限。账本区分累计已知用量和本次新增请求，回放不再扣额度。持久预算先保存预留再发请求，崩溃造成未知发送状态时保守占用额度，不自动释放。

独立审查另要求：持久执行拒绝内存预算；用量登记必须读取失败操作的 Value，不能在 err 非 nil 时提前丢弃已知用量。登记故障后从日志补齐，不重新调用模型。

### R-02 粗粒度一次尝试不提供续跑，严重

初版固定工具最多一次有助于避免重复付费，但父包装 pending 后即使内层已有完成记录，也不会自动进入子 Agent 续跑。R2 保留安全默认，同时将模式明确命名为不透明复合调用：completed 复用，failed/pending 停止并交宿主处理。可恢复嵌套 Agent、分类重试、嵌套审批要有独立 continuation 契约，后续单列，不当作 M1 已具备的恢复能力。

### R-03 历史、身份和资源生命周期需要分开，高

factory 新建 Builder 不代表 SessionID、MemoryManager 和可变依赖自动隔离。R2 要求新会话 ID/命名空间，受信本地身份和预算保持显式继承；factory 同时返回清理函数，成功、失败、取消均调用。父 deadline 不能被工具默认超时重新延长；默认 30 秒 Dispatcher 对长子任务需宿主显式配置，不能在工具内部重置超时。

### R-04 第二套流运行时过早，高

Reader/Frame、双消费者 reducer 视图和四范式适配给首轮增加状态机与 goroutine 生命周期。R2 将其替换为候选 Source/Map/Fold 原型：Source 的返回值决定成功，数据回调逐项背压，Fold 增量聚合，不再创建 reducer 消费队列。只读处理先证明首帧收益、取消和错误行为；通过原型门槛再承诺稳定 API。

### R-05 重建 Span 树重复现有设施，高

core/observe 可保留窄事件出口，但身份取自已有 OTel SpanContext。调试 EventID/VisitID 只用于排序与关联，不能伪装成另一套 TraceID，也不能用于幂等。OTel 未配置时可以只有事件运行 ID，不应为了离线查看器强制启用全局 SDK。

### R-06 持久引用缺少不可变写入约束，高

稳定 key 仍可能被不同内容覆盖；ID/版本也无法覆盖全部配置变化。R2 要求 PutIfAbsent 与摘要冲突检测，策略 descriptor 包含规范化配置摘要。恢复读取固定摘要与版本；引用先保存、请求后保存。框架只保证结构与身份，不声称任意压缩算法保留语义。

### R-07 反射映射收益尚未证明，中

现有 Adapt 接受类型化 Go 函数，Graph 的状态也已统一。大部分应用可用短函数完成字段提取。R2 先提供订单和并行 Merge 示例验证重复量；只有至少两个独立场景确认路径配置显著简化适配时，才启动反射映射设计。首版不发布 string path、默认值优先级与动态类型编译器。

### R-08 调试器不能证明节点无副作用，中

Effect=false 只是宿主声明，内存 Store 不能隔离闭包里的真实 API。R2 首轮只读导出；单节点模拟通过显式注入 fake 工具/服务的测试示例完成，不把标签判断包装为安全执行边界。

## 5. 修订后的取舍

| 方向 | 初版 | R2 |
| --- | --- | --- |
| 工具化 | 通用适配、factory、固定一次尝试 | 保留；先补预算、独立会话/清理和不透明恢复限制 |
| 数据流 | P0 完整四范式与 Reader 协议 | P1 原型；同步 Source/Map/Fold，旧图终态不变 |
| 字段映射 | P1 反射路径编译器 | P2 候选；当前继续使用类型化 Adapt |
| 上下文 | 多策略接口及持久 Store | 先抽取内部组件、固定配置/内容身份，再公开窄扩展 |
| 调试 | 自有 Span + 查看器 + 执行器 | OTel 复用 + 只读 JSONL；HTML 后置，移除任意节点执行器 |
| 成本与恢复 | 分散在功能条目 | 新增跨切面 FR-06，作为模型型工具发布前置条件 |

## 6. 建议交付与审核依据

先交付根预算与受控工具化，以及足够定位失败的 OTel/JSONL；再验证持久引用与上下文组合。只读流原型可独立进行，不阻塞工具适配。完整四范式、反射映射和嵌套恢复要通过各自准入门槛，不以补齐外部框架功能列表作为启动理由。

验收必须包含并发预算抢占、重启预算不重置、子会话记忆不串用、清理失败不掩盖主错误、pending 包装不重跑、引用摘要冲突、回放用量去重、OTel 父子关系和流迟到错误。默认全部使用本地模拟模型；真实模型验证单独显式启用并记录预算。

设计复审阶段没有修改 Go 实现或进行压测。后续已完成[独立框架性能对测](../evaluation/2026-10-27-framework-performance-v2/REPORT.md)：在相同合成任务下记录暖执行开销，32,000 次有效操作全部通过；GoAgent 小图与工具循环开销较低，Eino 的 64 段流完成中位数更低，GoAgent 首段更早。该证据支持先验证流原型、保留当前图边界，不代表 R2 尚未实现功能的性能，也不推导生产吞吐或恢复能力排名。
