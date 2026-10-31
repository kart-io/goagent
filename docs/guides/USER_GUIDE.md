# GoAgent 使用指南

新应用以 Builder.AsRunnable 和类型化 Workflow 组合业务。Go 版本要求见 [go.mod](../../go.mod)；精确签名见 [核心 API](../api/CORE_API.md) 和 [Builder 参考](BUILDER_API_REFERENCE.md)。

## 选择入口

| 需求 | 入口 |
| --- | --- |
| 模型回答、检索、原生工具循环 | `builder.NewSimpleBuilder(client)` → `BuildContext(ctx)` → `AsRunnable().Invoke/Stream` |
| 固定步骤、分支、循环与并行 | `workflow.Graph[T]` → Compile |
| 节点/分支级恢复、审批 | CompileDurable 与持久模型/工具记录 |
| 子 Agent 或子图作为能力 | NewRunnableTool / NewRunnableToolFactory，经 Dispatcher 调用 |
| 已有同步无流客户端 | 旧 Execute 路径；不能假设预算、持久和原生循环扩展点都生效 |

不要把 Builder 的输出与 core.AgentOutput 混用，不通过 interfaces.Agent/Runnable 开始新编排。

## 运行第一个案例

以下命令使用仓内模拟模型，无需密钥：

```bash
go run ./examples/execution-scope
go run ./examples/workflow
```

接入真实模型见 [快速入门](QUICKSTART.md)、[LLM API](../api/LLM_API.md) 和 [Provider 能力](PROVIDER_CAPABILITIES.md)。支持 StreamClient 不等于支持 ToolStreamClient；工具场景先检查具体 Provider 的原生能力。

## 工具与业务效果

工具实现 interfaces.Tool，其 Invoke 输入是 *interfaces.ToolInput，不是裸 map。参数由 ArgsSchema 声明，结果为 *interfaces.ToolOutput。使用 Dispatcher.Tools() 注册到 Builder，获得校验、调用身份和持久回放路径；裸工具调用不会自动写入操作日志。

工具失败、模型可修订的参数拒绝、审批等待和业务不支持要分别处理。ReturnArgumentErrors 只作用于模型调用的特定路径，不是自动询问用户的对话管理功能。详见 [Tool API](../api/TOOL_API.md)。

## 会话与恢复

WithSessionID 与 WithMemory 组合记忆；WithSessionLog 使用会话日志推导模型历史。它们不能替代宿主的待澄清任务管理。

会话历史、Agent 会话状态检查点和工作流 durable.Store 职责不同。RunID/CallID/Agent作用域来自宿主并跨恢复稳定；新任务不能误用已完成运行身份。统一装配见 [宿主接入](HOST_INTEGRATION.md)。

需要恢复的业务效果经持久操作记录及外部幂等键管理；未知结果先核对，不换新 ID 重做。审批暂停时查看 Waiting/Done，而不是只看 Invoke 返回 nil error 或流的 Done。见 [原生编排](NATIVE_ORCHESTRATION.md)。

## 流、取消与并发

Invoke 和 Stream 共用执行源。流中的文本、工具事件和预览不是成功完成证据；消费到最终结果仍需处理 error/context。提前停止读取时必须取消 ctx。

同一个有状态 Builder Agent 的执行入口共享串行执行门；不同会话并发应使用独立实例、SessionID 和必要的记忆隔离。不同实例的同会话互斥由宿主处理。工作流并行输入的 map/slice/pointer 使用显式 Clone 隔离，Merge 按注册顺序接收结果。

## 上下文、重试与观察

- 长工具结果通过 WithOffload 和已注册的读取工具处理；旧 ContextPolicy/WithContextPolicies 已删除，迁移见 [上下文指南](CONTEXT_POLICY.md)。
- 自定义请求前改写通过 WithIdentifiedPreStep 管理持久身份；角色和业务分派不放进通用上下文钩子。
- 模型暂时错误使用 WithRequestError 与 RetryTransient；WithModelRetry 已删除。每次尝试分别占预算，子 Agent 不自动继承父层重试钩子。
- WithMiddleware 接收 core/middleware 的处理链协议；工具准入与 Around/Post 由 tools.Pipeline 处理，详见 [中间件 API](../api/MIDDLEWARE_API.md)。
- Observer 只读，不是业务状态或检查点；宿主负责关闭借用资源。

检索、文档加载和数据库扩展由 [goagent-contrib](https://github.com/kart-io/goagent-contrib) 提供；Preset、压缩、技能和编码工具由 [goagent-harness](https://github.com/kart-io/goagent-harness) 提供。业务案例见 [goagent-examples](https://github.com/kart-io/goagent-examples)。
