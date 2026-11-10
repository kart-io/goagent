# 旧入口与无效配置迁移

本轮删除兼容名称、旧流协议、工具中间件旧钩子及没有执行路径的配置，不保留转发别名。调用方须迁移到实际执行的接口；编译失败或严格配置解码失败用于暴露仍未迁移的调用。

## 工具调用与流式执行

`tools.ToolCall` 已删除，使用 `tools.ToolCallRequest`。`llm.ToolCall`、`builder.ToolCall` 和 `interfaces.ToolCall` 是不同业务对象，不在本次删除范围。

`core/streaming_alias.go` 的类型、常量、工厂变量以及 `core/execution` 的旧 `StreamingAgent/StreamOutput/LegacyStreamChunk` 体系已删除。流式执行使用 `core.Runnable[I,O].Stream` 或 `core.StreamRunnable[I,O]`，消费 `core.StreamChunk[O]` 的 `Data/Error/Done/Event`。调用方应区分一次调用完成和业务流程完成；不能把通道关闭简单当作已持久提交或业务成功。参见[流式执行说明](GROUNDED_RAG_AND_STREAMING.md)。

这不是把 `StreamOutput.Next()` 换成一个同名包装器；旧流实现须接入现行泛型执行源，不再公开两套数据块和工厂入口。

## 强类型工具中间件

装配入口固定为：

```go
func WithMiddleware(tool interfaces.Tool, middlewares ...middleware.ToolMiddlewareFunc) interfaces.Tool
```

列表中的第一个函数处于最外层。`ToolMiddleware/BaseToolMiddleware/Chain` 旧生命周期已删除；日志、缓存、限流统一通过函数包装调用器：

```go
wrapped := tools.WithMiddleware(tool,
    middleware.Logging(),
    middleware.Caching(),
    middleware.RateLimit(middleware.WithQPS(10)),
)
```

要保留状态对象并读取其能力时，显式传入该对象的 `Wrap` 方法：

```go
logging := middleware.NewLoggingMiddleware()
wrapped := tools.WithMiddleware(tool, logging.Wrap)
```

不能再把任意对象直接传给 `WithMiddleware`，也不能靠运行时类型判断接回旧钩子。空中间件函数或返回空调用器的装配以配置错误失败。日志计时属于每次调用，不写入调用方 `ToolInput.Args`；底层返回的部分结果和错误一起保留。工具包装不因此自动获得底层可选持久能力，具体边界见[工具中间件指南](TOOL_MIDDLEWARE.md)。

## Runtime 与重试

删除 `execution.RuntimeConfig`、`DefaultRuntimeConfig`、`RuntimeMetrics`、`NewRuntimeMetrics` 及 `RuntimeManager.Metrics()`。此前的自动保存、保存间隔、大小限制和指标开关没有执行路径，不能迁为同名隐藏默认行为。保存须由宿主明确执行；观测使用实际的 `execution.Scope`、Observer/Tracer 和模型 UsageHook。`core/execution` 包随后整体删除，见[持久化、人工介入与 Runtime](#持久化人工介入与-runtime)。

`core.RunnableConfig.RetryPolicy`、`core.RetryPolicy`、`core.BackoffPolicy` 已删除。模型暂时错误使用 `AgentBuilder.WithRequestError` 与 `builder.RetryTransient`，每次请求独立预留预算，已输出的响应不自动重发；参见[Builder 参考](BUILDER_API_REFERENCE.md)。`tools.RetryPolicy` 是真实工具执行策略，继续保留。

## 通用缓存

`cache.InMemoryCache/LRUCache/MultiTierCache`、三个对应构造器及专属 `CacheEntry` 已删除。仅需要 TTL 时使用 `cache.NewSimpleCache`；禁用缓存使用 `cache.NewNoOpCache`。SimpleCache 没有容量、LRU 或多级回填契约，不能宣称构造器替换保留了这些能力。

`cache.CacheConfig` 仅保留实际生效的 `Enabled/DefaultTTL`，删除 `Type/MaxSize/CleanupInterval`。配置文件删除 `type/max_size/cleanup_interval`，严格解码会拒绝这些旧键。需要容量或多级语义的宿主须显式注入实现相应契约的 `cache.Cache`，见[缓存指南](CACHING_GUIDE.md)。

## 检查点切回

`DistributedCheckpointerConfig.FailbackDelay` 和生产无调用的私有 `tryFailback` 已删除。配置文件删除 `failback_delay`。故障切到副库后仍由宿主协调写入、核对全部数据及删除结果后重建实例；主库重新健康不会触发自动切回。此改动不增加跨进程 fencing、CAS 或零 RPO 能力。`core/checkpoint` 包随后整体删除，见下节。

## 工作流续跑与历史账本

子图审批统一使用 `workflow.WithResume(ctx, operationKey, workflow.ResumeRequest{Key: approvalKey, Input: input})`，不再把裸审批字符串识别为有效请求。`operationKey` 与请求内的 `Key` 标识不同边界，不能互换。没有载荷的审批仍可使用 nil `Input`；显式 JSON `null` 仍是一个提供过的载荷。实际宿主授权和审批校验保持必需。

`durable.WithResume` 继续用于普通持久操作及叶子中断的原始输入；这个能力不等同于子图审批的旧裸键协议，不能一并删除。

在线持久操作只读取现行 operation-v2。旧 operation-v1 记录须经独立的 `core/durable/migrate` 组件显式转换；在线执行不再自动兼容旧版本，也不会因为拒绝旧版本而重新执行业务。

迁移操作必须停止相关写入并保留备份；使用 `OperationRef{Key,ID}` 明确列出目标，在相同 Store/作用域上先用默认干跑的 `migrate.OperationsV1` 获取报告，校验通过后再传 `OperationOptions{Apply:true}`。组件按存储 ID 排序持锁，全部读取校验完成后通过一次 `Store.SaveAll` 原子转换。转换只改变快照版本，原 Data 字节、身份、输入指纹、结果及次数不变，不调用业务函数。混合的有效 v2 记录不改写，重复应用安全；提交结果未知时重新干跑核对。Store 必须满足 Lock 和 SaveAll 契约。

不要通过删除旧账本来解决版本拒绝；账本记录着已发生的外部效果，清空后重新运行可能重复这些效果。迁移的实际接口、错误边界及可执行示例见迁移包文档；不得让迁移工具和在线执行同时处理同一组运行。

## 持久化、人工介入与 Runtime

以下包与接口已删除，状态持久化、人工介入与恢复统一使用 `core/durable` + `core/session`：

| 已删除 | 迁移方式 |
|---|---|
| `core/checkpoint` 整包（`Checkpointer`、`NewInMemorySaver`、`DistributedCheckpointer` 等）；Builder 的 `WithCheckpointer`、`WithAutoSaveEnabled`、`AgentConfig.EnableAutoSave`；`errors.NewStateCheckpointError` | `Shutdown` 不再保存状态。状态跨进程恢复用 `WithSessionLog(store).WithSessionState()`，状态类型须实现 `MarshalJSON`/`UnmarshalJSON`；会话历史用 `WithSessionLog(session.Store)`，崩溃恢复用 `core/durable` 与 `WithDurableScope`，图级暂停与恢复用 `workflow.CompileDurable` |
| `core/execution` 整包（`Runtime`、`NewRuntime`、`RuntimeManager`、`ToolWithRuntime` 等）；`core/middleware.MiddlewareRequest.Runtime` | 中间件读取 `State` 与 `Metadata`；宿主装配用顶层 `execution.Scope`，见[宿主接入](HOST_INTEGRATION.md) |
| `core.InterruptManager`、`InterruptableExecutor`、`InterruptConfig` 等 | 持久图的 `ApprovalNodes` 或 `durable.AwaitInput`，见[人工介入](HUMAN_IN_THE_LOOP.md)及 `examples/human-in-the-loop`、`examples/human-in-the-loop-interrupt` |
| Builder 的 `WithStore` | 对话记忆用 `WithMemory`；其他键值数据由宿主直接持有 `store.Store` |
| 泛型参数 `C` 与 `WithContext(C)`：`AgentBuilder[C, S]`、`ConfigurableAgent[C, S]` | 改为 `AgentBuilder[S]`、`ConfigurableAgent[S]`，如 `NewAgentBuilder[any, *core.AgentState]` → `NewAgentBuilder[*core.AgentState]`；应用数据经 `context.Context` 或 `WithMetadata` 传递 |
| `interfaces` 的 `Lifecycle`、`LifecycleManager`、`HealthStatus` 等生命周期与健康状态定义 | 插件用 `plugin.Starter`、`plugin.Dependent`；其他组件由宿主自行管理生命周期 |
| `testing/testutil` | 使用标准库 `testing` 断言，或 `testing/mocks` |
| `core` 的 `Chain`、`BaseChain`、`ChainableAgent`、`Orchestrator`、`PluginRegistry`、`LifecycleManager`、`TimeoutConfig` | 组合用 `core.RunnablePipe`/`RunnableSequence` 或 `workflow`；多 Agent 用 `multiagent` |
| `tools` 的 `ToolRuntime`、`RuntimeToolAdapter`、`UserInfoTool`、`SavePreferenceTool`、`UpdateStateTool` | 工具所需数据经 `context.Context` 或构造时注入；`StreamWriter` 进度推送没有替代 |
| 示例 `examples/integration/{human-in-loop,langchain-phase1,langchain-complete}`、`examples/advanced/tool-runtime` | 人工介入见上述 HITL 示例 |

## 保留的现行契约

统一 JSON facade 的稳定编码、当前文件格式的崩溃恢复、业务 CloneSnapshot 协议、泛型便利类型和对外服务协议适配继续保留。它们承载现行数据或能力契约，不是被删除旧框架接口的重命名入口。本轮没有自动迁移用户数据、改变外部依赖版本或发布模块。
