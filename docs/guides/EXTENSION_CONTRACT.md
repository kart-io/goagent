# 执行入口与扩展契约

推荐通过 `agent.AsRunnable().Invoke/Stream` 接入。`ExecuteStream` 与它共享执行源；配置原生工具循环时需要 `llm.ToolStreamClient`。旧 `Execute` 保留同步且不隐式调用工具的语义，绑定持久日志或根预算时拒绝执行；非流式 `ExecuteWithTools` 保留已声明的兼容路径。

## 扩展点

| 扩展点 | 是否改变执行 | 错误与超时 | 恢复语义 |
|---|---|---|---|
| `core/observe.Observer` | 只读观察，不是检查点 | 同步sink须及时返回；panic隔离；可显式包装AsyncObserver | 事件不是持久账本，回放有Replayed标识 |
| `core.Callback` | 生命周期通知 | StreamRunnable的OnStart/OnEnd错误使调用失败；OnError返回值忽略，panic按错误处理；旧RunnableFunc的OnEnd返回值仍忽略 | 父子回调独立，宿主避免重复注册副作用 |
| `core/middleware` | 可改请求与结果 | 返回错误中止，受调用context约束 | 中间件副作用不自动幂等，需宿主版本与执行键 |
| `PreStep`、`TurnStopping` | 可改历史、阻止请求或续跑 | 错误中止，续跑受请求预算限制 | 持久执行记录结果，恢复回放 |
| `RequestHook` | 可调整模型请求或客户端 | 错误中止 | 持久模式须确定，变化须提升宿主版本 |
| `RequestErrorHook`（`WithRequestError`；暂时性失败的原样重试用 `builder.RetryTransient`） | 可压缩、回退或重试 | 原生工具流，恢复次数有界；没有工具的流式回答只支持原样重试 | 原样重试在同一条回合记录内，每次尝试各占预算；持久运行中替换消息或客户端以错误结束 |
| Pipeline Pre/审批/Guard | 可拒绝或声明替代调用 | 模型可修订的拒绝是Rejected，error中止；审批有独立上限 | 准入在操作日志之外，完成操作回放跳过准入 |
| `Authorize` | 宿主层授权 | error中止，与Pipeline互补；参数不合法时也可能调用 | 不应仅据函数存在推断已授权回放的其他业务效果 |
| Pipeline Around/Post | 包装执行、改结果或追加上下文 | 计入单次Timeout，异常结果不变成成功 | 冻结后的结果写入操作日志 |
| `OnResult` | 宿主结果交付/状态重建 | 错误使调用失败，不受工具单次Timeout；宿主ctx仍有效 | 首次和回放均可触发，以ExecutionKey去重 |

完整签名以源码Go文档为准；业务最终结果必须来自成功终态，预览文本不证明任务完成。

## Pipe 与 Sequence 的一致性修复

组合器仍等待前一阶段最终输出后才执行后一阶段；没有跨节点逐Token变换。最后阶段的流经共同执行源验证，必须有且只有一个终态，并在其后关闭通道。缺少/重复终态、终态后事件或迟到错误都会失败；空Sequence作为恒等变换。

显式配置的父级回调现在对Invoke、Stream和Batch逐输入生效，子回调仍独立运行；旧调用方若曾同时注册父子相同副作用回调，须去重。Batch改为逐输入Invoke并遵守父级MaxConcurrency，保持结果索引；单个输入失败后仍处理其他输入，返回有序部分结果和首个观察到的错误，不再调用子级自定义Batch。错误输出只作诊断，不视为成功业务结果。

消费者提前停止读取时必须取消ctx。组合器自身发送与等待支持取消，但无法强制终止忽略context的第三方同步函数或生产者；提供终态后不关闭通道的第三方会被等待到ctx取消。未配置ctx期限的宿主应自行管理生命周期。

## 初始化与记忆

宿主推荐使用 `BuildContext(ctx)` 构建，初始化检查点与 OnStart 回调共享该上下文，操作与回调返回后检查取消；旧 `Build()` 使用 Background。取消不回滚外部效果，也无法强制终止忽略 ctx 的实现。标准身份、预算和观察装配见 [宿主接入指南](HOST_INTEGRATION.md)。

初始化的Exists/Load失败明确返回错误，不再当成无历史；不存在才使用新状态。状态类型不匹配（含typed-nil）返回普通错误，失败不替换原状态也不触发初始化回调。

`WithMemoryFailurePolicy(builder.MemoryFailureStrict)` 开启严格记忆模式。默认 `MemoryFailureBestEffort` 保持继续执行，但产生不含原始错误或对话的memory_read/write观察事件。严格读写错误保留errors.Is链，在最终成功终态前失败；流式文本可能已经输出。宿主自定义errorHandler仍可按原契约转换错误。

两条对话写入不是事务：用户消息成功、助手消息失败时可能留下部分记录，失败不回滚模型调用或业务副作用。需要事务语义的宿主应由MemoryManager自身提供幂等/事务能力。

治理与诊断接入见 [执行治理指南](EXECUTION_GOVERNANCE.md)。
