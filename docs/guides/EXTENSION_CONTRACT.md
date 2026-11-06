# 执行入口与扩展契约

推荐通过 `agent.AsRunnable().Invoke/Stream` 接入。`ExecuteStream` 与它共享执行源；Builder 接受任意 `llm.Client`，配置工具时需要 `llm.ToolStreamClient`。

## 公共契约与变更验证

当前 0.x 版本以以下行为作为公共契约；版本号并不承诺所有接口永久不变。修改签名或恢复行为须在 CHANGELOG 中提供迁移说明，并验证所有关联模块，不增加只为掩盖破坏的兼容别名。

| 边界 | 必须保持的行为 | 回归位置 |
|---|---|---|
| `Runnable` / `StreamRunnable` | Invoke 与 Stream 共用执行源；成功只有一个终态；缺失/重复终态、迟到错误和取消不发布成功结果 | `core/stream_runnable_test.go`、`core/composition_stream_test.go`、`builder/runnable_test.go` |
| `llm.Client` / `ToolStreamClient` | 完整工具调用只来自成功终止块；保留工具 ID、原生续接状态、错误和已知用量，未知值不伪造为零 | `llm/providers/native_contract_test.go`、`structured_hardening_contract_test.go`、`builder/agent_usage_test.go` |
| `durable.Store` / 持久操作 | Lock、原子 Save/SaveAll 与读取语义遵循接口注释；已完成操作回放；未知效果先核对或显式允许重试 | `core/durable/storetest`、`operation_wait_test.go`、`save_all_test.go` |
| 审批与中断恢复 | 错误输入保持等待；已消费恢复键不再次应用状态修改；宿主授权不能由审批键替代 | `workflow/resume_test.go`、`resume_process_test.go`、`core/durable/interrupt_test.go` |
| 根预算 | 父子调用共用额度；持久运行恢复不清零；未知用量、配置漂移与并发请求按账本规则处理 | `core/budget/unified_test.go`、`limits_test.go`、`builder/model_budget_test.go` |

执行 `make test-contracts` 验证这些边界，`make test-frameworkbench` 验证独立模块，`make compatibility` 验证本地 Harness、Contrib 和 Examples 的实际调用方。跨仓报告记录当前核心依赖目录、源码提交、完整测试日志与跳过项。函数或状态结构变化须主动提升宿主流程版本，不能仅以 Go 编译通过断言旧检查点可恢复。带数据库、真实模型或业务授权的部分，还需相应环境的验收证据。

## 扩展点

| 扩展点 | 是否改变执行 | 错误与超时 | 恢复语义 |
|---|---|---|---|
| `core/observe.Observer` | 只读观察，不是检查点 | 同步sink须及时返回；panic隔离；可显式包装AsyncObserver | 事件不是持久账本，回放有Replayed标识 |
| `WithUsageHook` / `budget.WithUsageHook` | 只读Token统计通知，不改变执行 | 同步且支持并发；query/回调panic不改变业务；未绑定预算仅单次证据 | 父子继承，子非nil替换；回放标识保留，累计不能相加；见[用量指南](MULTI_AGENT_USAGE.md) |
| `core.Callback` | 生命周期通知 | StreamRunnable的OnStart/OnEnd错误使调用失败；OnError返回值忽略，panic按错误处理；旧RunnableFunc的OnEnd返回值仍忽略 | 父子回调独立，宿主避免重复注册副作用 |
| `core/middleware` | 可改请求与结果 | 返回错误中止，受调用context约束 | 中间件副作用不自动幂等，需宿主版本与执行键 |
| `WithOutputValidator` / `StreamRunnable.WithFinalValidator` | 只读最终验收，不能追加模型请求 | 错误/取消/panic阻止成功终态，验收器可能多次调用 | 回放重新验收，不回滚已保存历史或外部效果 |
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

纯文本执行不支持的循环钩子、提示词扩展和SessionLog现在在BuildContext期报错；RequestError原样重试与OutputValidator仍可使用。详见[能力迁移说明](EXECUTION_ACCEPTANCE.md#构建期能力检查与迁移)。
