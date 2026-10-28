# 可组合编排 R2

本轮已实现根模型请求预算、受控 Runnable 工具、OTel 关联观察、不可变结果引用与窄上下文策略。`core/flow` 为实验性同步增量原型。现有 Runnable 接口、Pipe、Graph 和 Adapt 的必需契约保持不变。

## 离线运行

所有示例使用本地确定性模型，无需密钥、没有真实退款等副作用：

```sh
go run ./examples/orchestration/runnable-tool
go run ./examples/orchestration/agent-tool
go run ./examples/orchestration/field-adapter
go run ./examples/orchestration/parallel-diagnostics
go run ./examples/orchestration/stream-pipeline
go run ./examples/orchestration/context-recovery /tmp/goagent-evidence-demo
go run ./examples/orchestration/composed-order /tmp/goagent-order-demo
# 使用相同目录再次运行 composed-order：新增模型请求应为 0。
```

订单示例将普通规则图和独立政策子 Agent 注册成工具，借助类型化 Adapt 映射订单字段。首次实际模型请求 4 次，根额度 4 次，已知用量 8 token；同一目录重放不会新建子 Agent 或再次调用模型。长证据在下一请求前先保存不可变引用。JSONL 只输出执行关联元数据。

## 根预算和用量

宿主用 `budget.New(Config{RootKey, Version, MaxModelRequests})` 或 `budget.NewDurable(config, store)` 构造账本，以 `budget.WithController(ctx, controller)` 传入根调用。RootKey 是受信业务运行 ID，不能来自模型参数。父子调用继承同一 context，不能另建预算来重置额度。

显式绑定根预算时，旧 Execute 和不支持流的 ExecuteWithTools 在请求前报错；迁移到 AsRunnable 统一入口，避免静默忽略额度。未绑定预算的旧入口行为保留。

统一模型入口在实际请求前预留；成功回放跳过预留。预留不退款，崩溃时可能无法判断请求是否发送。持久日志配内存预算会在请求前拒绝。同一 RootKey 改版本或上限会报错。账本和模型操作不是跨文件事务；结果已保存而用量登记失败时返回错误，后续读取已有记录修复用量，不重新付费取证。

`Snapshot` 分开报告 ReservedRequests、KnownUsageEntries、UnknownUsageEntries、KnownTokens。未知包含预留后没有证据的条目，不能当作零费用；子输出的 TokenUsage 不再作为另一笔请求加入根账本。输出 `root_budget` 是根快照，`new_model_invocations` 是本次执行范围新增实际调用数，`model_calls` 是本层逻辑回合数。父子并行时以根账本及 transport 计数为准。Provider 内部重试、宿主自定义客户端或隐藏模型调用需宿主另行管控；这个预算限制统一入口的请求次数。

## Runnable 工具与子会话

`tools.NewRunnableTool` 包装共享无状态 Runnable；宿主提供名称、对象 Schema、Version、Decode 和 Encode。原对象的资源仍由宿主关闭。适配器必须经过 `Dispatcher.Tools()` 才具有持久调用能力。

有状态子 Agent 使用 `NewRunnableToolFactory`，每次返回 `OwnedRunnable{Runnable, Close}`。必须显式提供 Close，即便它是 no-op。部分构造失败仍清理已获得资源，成功、执行失败、取消均清理。清理 context 独立于父取消、最长 5 秒；忽略 context 的实现不能被强制终止。清理错误阻止工具记录 completed。

宿主负责为每个 child 分配独立 SessionID、MemoryManager 命名空间及 Dispatcher。不同对象指针无法证明隔离；`agent-tool` 示例测试并发真实记忆隔离，并拒绝复用 SessionID。父历史不自动复制，输入解码只传必要业务字段。模型型工具须声明 `ModelInvoking: true`，缺根预算会在 factory 前拒绝；这项声明不是对隐藏模型调用的自动检测。

默认最大嵌套深度 8，祖先更小的限制继续生效。同一执行链重入同一 Dispatcher 在并发槽排队前拒绝；独立子 Dispatcher 允许嵌套。deadline 逐层继承；长工具需宿主显式配置 Dispatcher Timeout。

复合工具为不透明、一次尝试模式：相同版本 completed 可回放；pending/failed 返回 attention，不自动进入子检查点。Encode 必须拒绝等待或未完成的业务输出。此模式不提供嵌套审批、handoff 或 exactly-once 外部副作用保证。

## 窄上下文策略

旧 `WithContextPolicy` 保留内存分页、最后回合恢复和摘要行为；旧配置在持久工具循环仍拒绝。新入口 `WithContextPolicies(factories...)` 每次执行创建独立策略，两种入口同时使用报错。要求 native `llm.ToolStreamClient` 和已配置的业务工具。

策略只转换非 system 的 Content。Builder 对消息、工具调用、ProviderState 对象和原始数据字节分别做深复制，保持 opaque 字节完全不变，并验证：Round、RemainingCalls、消息数量、Role、Name、ToolCallID、ToolCalls、ReasoningContent、ProviderState 均不能修改。带 ProviderState 的消息正文也受到保护，避免破坏原生签名/续接关系。描述符 ID 唯一，Version 和 ConfigHash 非空且执行期间固定；顺序及配置加入模型意图。宿主自定义语义必须自行版本化，Durable 标记是能力声明，仍需恢复测试。

`contextpolicy.NewOffload` 消费共享 ResultStore；阈值、保留轮数、分页、排除项、Namespace 均进入指纹。内存实例采用独立 nonce；持久实例从已记录回合和工具 ID 派生稳定引用。Namespace 必须标识真实后端配置，切换后端需修改它或 Version。

持久模式用 `NewDurableResultStore(store)`，并配置 `WithPolicyToolAdapter` 将策略工具注册到宿主 Dispatcher。缺 adapter、返回裸工具、改变工具身份或与业务工具重名都会在首个模型请求前失败。`composed-order` 中的 journalTools 是完整示例。策略读取工具同样有标准 CallID 和版本意图。

存储要求先 PutIfAbsent 再发带引用的请求；同一逻辑 key 不同内容拒绝，读回重新验摘要。ResultScope 必须由宿主 context 绑定，默认使用当前稳定 execution key（内存执行使用随机 scope）。模型只能提交本次策略已授权的 key，无法自行选择 RunKey；独立 store 锁不复用当前操作锁。最后本地回合先恢复原文、再移除读取工具，可能恢复大上下文，不能保证输入永不超模型窗口。持久摘要本轮不支持。

## 观察与报告

`observe.WithTracer(ctx, existingTracer)` 复用宿主 OTel；不设置全局 Provider。`WithObserver` 接收窄元数据事件。OperationKey 用于跨恢复关联，VisitID/EventID 包含本次调用的独立身份，区分重复访问与恢复；业务 RunID 和 OperationKey 保持稳定，恢复产生新 span。未配置观察时不创建运行身份或 span。自定义 observer 必须及时返回，panic 被隔离。

`devtools.NewRecorder` 默认队列 64、最多 10,000 事件，写入异步，溢出累计 Dropped。新 JSONL 文件权限 0600、目录 0700；Close 返回写入错误。自定义 Writer 必须合作返回。事件结构不含输入、输出及原始异常文本；宿主应使用不含敏感信息的节点名和运行 ID。

Graph.Describe 或编译产物可选 Description 返回排序、独立的 `goagent-graph-v1` 拓扑。`devtools.WriteReport(writer, description, events)` 输出转义、无脚本、无外部资源的 HTML，展示循环、分支、等待、失败、回放和观察丢弃。它不能执行、审批或恢复运行，缺少事件也不能推断业务状态。

## 流原型和后续范围

`core/flow.Source` 串行同步 yield，Map 不启动 goroutine；Fold.Init 每次执行独立创建 accumulator。默认最多 1024 项，可显式限制字节并提供 Size。任何 Source、Map、Add、preview 或 Finish 错误均阻止最终 Done，即使 Source 吞掉 yield 错误也不能假成功。preview 只能发事件，不能推进图。合作式源须响应 context 和 yield 错误，并在返回前停止生产者。

采用结论：保留实验包用于只读增量演示，不作为完整四范式稳定 API。反射 Mapping、嵌套可恢复工作流、持久摘要和通用 Debugger.Run 继续推迟，需独立需求与恢复协议。类型化映射优先使用已有 Adapt/Clone/Merge。

## 迁移、验证和回退

没有新配置时不添加预算/策略意图字段。普通工具原意图 fixture 和已有执行测试保持通过。新工具版本、策略配置或根预算配置变化会拒绝旧运行；不能删除 pending、退款或增加重试次数来强行恢复。

先停止新版本运行，保留对应版本 worker 处理存量。账本、引用和检查点统一按业务运行保留；活跃引用不能提前清理。测试与性能实测见 [交付验证记录](../evaluation/2026-10-27-orchestration-r2/REPORT.md)。真实模型仅由宿主显式接入并设置预算，本轮交付证据使用本地模型。

## 持久 Agent 作用域与升级

所有在持久 context 中执行的 Builder Agent 必须配置 `WithDurableScope("stable-name")`；同一父节点中的逻辑调用名称必须不同且跨重启稳定。`composed-order` 分别使用 `order-review` 和 `policy`，模型日志、工具日志和默认上下文结果引用位于对应 Agent 子作用域；根预算与 OTel context 继续共享。无工具 StreamClient 保留 R2 的 `model/answer/0` 日志和预算统计，原生工具循环使用 `model/turn/N`。

检测到旧的无作用域 `model/turn/0` 或 `model/answer/0` 记录时提前拒绝，避免换键重跑。已有目录不能直接当作新运行；先核对业务效果，再制定版本迁移。演示新运行可使用新的空目录，不能通过删除旧评测或检查点来掩盖迁移问题。详细契约见[多 Agent 作用域与入口约束](NATIVE_ORCHESTRATION.md#多-agent-作用域与入口约束)。
