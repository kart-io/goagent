<!-- generated-by: gsd-doc-writer -->
# Builder API 参考

Builder 创建可配置 Agent；新业务通过 `BuildContext(ctx)` 构建，再以 `agent.AsRunnable().Invoke/Stream` 执行。With 方法修改并返回同一个 Builder，不支持并发配置；不要在 Build 后继续修改并共享它来模拟独立会话。

## 创建与构建

```go
func NewAgentBuilder[C any, S core.State](client llm.Client) *AgentBuilder[C, S]
func NewSimpleBuilder(client llm.Client) *SimpleAgentBuilder

type SimpleAgentBuilder = AgentBuilder[any, *core.AgentState]
type SimpleAgent = ConfigurableAgent[any, *core.AgentState]
```

| 方法 | 返回 | 行为 |
|---|---|---|
| `BuildContext(ctx context.Context)` | `(*ConfigurableAgent[C,S],error)` | 校验配置，将 ctx 传给检查点初始化和 OnStart，操作/回调返回后检查取消 |
| `Build()` | 同上 | 委派 BuildContext(context.Background()) |
| `WithState(state S)` | `*AgentBuilder[C,S]` | 指定状态；未提供时仅 *core.AgentState 自动创建，其余 S 必须提供 |

默认创建内存命名空间 Store 和内存 Checkpointer。构建初始化会执行 Exists/Load 和 OnStart；检查点错误、类型错误及取消会失败，不能当成“无历史”。忽略 context 的宿主调用须自行返回，取消不回滚回调已发生的外部效果。

源码：[builder.go](../../builder/builder.go)、[configurable_agent.go](../../builder/configurable_agent.go)。

## 执行入口选择

| 入口 | 签名概要 | 契约 |
|---|---|---|
| AsRunnable() | `*core.StreamRunnable[any,*builder.AgentOutput]` | 推荐；Invoke 与 Stream 使用 ExecuteStream 的同一源 |
| ExecuteStream(ctx,input any) | `(<-chan builder.AgentEvent,error)` | 读取 sources/step/text/usage/error/done 事件 |
| Execute(ctx,input any) | `(*builder.AgentOutput,error)` | 保留同步且不隐式调用工具的旧行为；绑定预算/持久日志或配置 Offload/ReturnDirectTools 时拒绝 |
| ExecuteWithTools(ctx,input any) | 同上 | 支持流时委派 AsRunnable；否则保留旧工具循环，不能绑定根预算或持久日志 |
| Start(ctx,input string) | `*builder.Handle` | 原生工具流交互句柄，可 Steer/Inject/Followup/Redirect/Interrupt，结果通过 Wait(ctx) 获取 |

AsRunnable 需要 llm.StreamClient。推荐的原生工具循环需要 llm.ToolStreamClient 与已注册工具；不能仅凭 StreamClient 推断支持所有扩展点。同一 Agent 实例共享串行执行门，排队受 context 约束；独立并行子 Agent 需要独立实例和会话。

builder.AgentOutput 含 Result、State、Metadata、Duration、Timestamp、TokenUsage，与 core.AgentOutput 不同。Stream 的中间文本不是业务成功证据；提前停止消费必须取消 context。

## 常用选项与实际默认值

以下方法均返回 `*AgentBuilder[C,S]`。默认值来自 [DefaultAgentConfig](../../builder/config_options.go)，不是 Provider 的统一能力承诺。

| 方法与参数 | 默认值 | 说明 |
|---|---|---|
| WithSystemPrompt(string) | 空 | 设置系统提示词 |
| WithTools(...interfaces.Tool) | 无工具 | 追加工具；持久调用使用 Dispatcher.Tools 返回的包装工具 |
| WithMaxIterations(int) | 10 | 原生循环最大模型步骤，最后请求通常用于最终回答 |
| WithTimeout(time.Duration) | 5 分钟 | 执行期限，与父 context、根预算期限取更早者 |
| WithTemperature(float64) | 0.7 | 模型采样参数，具体支持取决于 Provider |
| WithMaxTokens(int) | 2000 | 每次请求输出预算，不是跨 Agent 总限额 |
| WithSessionID(string) | 自动生成 session-时间戳 | 会话检查点与历史身份；重建/恢复时宿主提供稳定值 |
| WithMaxConversationHistory(int) | 20 | 加载的历史对话轮数；0 或负数不限制 |
| WithMemoryFailurePolicy(MemoryFailurePolicy) | MemoryFailureBestEffort | 可设 MemoryFailureStrict，读写失败保留错误链并中止 |
| WithParallelToolCalls(int) | 0，顺序执行 | 小于2不并行；同轮工具均声明并发能力才并行 |
| WithEmptyAnswerRetries(int) | 0，不重问 | 空回答可追加重问，消耗步骤与预算，不是网络重试 |
| WithReturnDirectTools(...string) | 无 | 替换名单；本轮单个匹配调用时直接返回结果，多个调用仍走模型循环 |
| WithVerbose(bool) | false | 添加默认日志/计时中间件 |
| WithStreamingEnabled(bool) | false | 保留配置标记；不代替 Stream 入口或赋予客户端流能力 |
| WithAutoSaveEnabled(bool) | true | 控制会话状态自动保存，不等于工具效果幂等 |
| WithSaveInterval(time.Duration) | 30 秒 | 仅设置配置值，不表示框架启动后台定时保存任务 |

## 状态、记忆与装配

| 方法 | 参数类型 | 责任 |
|---|---|---|
| WithContext | C | 运行时业务依赖，不替代每次调用的 context.Context |
| WithStore | store.Store | 命名空间键值存储，不是 durable.Store |
| WithCheckpointer | checkpoint.Checkpointer | 加载与保存 core.State，不提供工具调用回放 |
| WithMemory | interfaces.MemoryManager | 读写对话记忆；与 Store 不是同一 API |
| WithSessionLog | session.Store | 从追加写日志推导历史，取代此路径的记忆历史读取；只支持原生工具流 |
| WithRetriever | interfaces.DocumentRetriever, ...RetrievalConfig | 默认最多4文档、正文12000 Unicode 字符；不缓存答案 |
| WithDurableScope | string | 持久调用的稳定作用域；同节点兄弟逻辑调用必须不同 |
| WithMiddleware | ...middleware.Middleware | 追加中间件 |
| WithCallbacks | ...core.Callback | 追加生命周期回调；可嵌入 core.BaseCallback 只覆盖所需方法 |
| WithErrorHandler | func(error)error | 转换执行错误，不代替 RequestError 或业务重试 |
| WithInputSerializer | builder.InputSerializer | 输入序列化，默认 fmt.Sprintf |
| WithMetadata | string, interface{} | 添加元数据 |
| WithCommunicator | interface{} | 仅写 communicator 元数据，不自动实现 Agent 间通信 |
| WithTelemetry | interface{} | 仅写 telemetry_provider 元数据；窄事件与 OTel 接入使用 core/observe |

源码：[agent_options.go](../../builder/agent_options.go)、[runtime_options.go](../../builder/runtime_options.go)、[session_log.go](../../builder/session_log.go)、[retrieval.go](../../builder/retrieval.go)。会话日志与持久图/工具日志是不同契约；session.DurableStore 可把持久快照写入同一会话日志。

宿主可用 execution.NewScope、Scope.Bind、execution.ConfigureBuilder 统一身份、预算与观察装配。Scope 不创建默认额度或关闭宿主资源，详见[宿主标准接入](HOST_INTEGRATION.md)。

## 原生循环扩展点

除 RequestError 原样重试也适用于无工具流式回答外，下列扩展点用于 ToolStreamClient 且配置工具的原生循环。签名与类型见 [loop_hooks.go](../../builder/loop_hooks.go)。

| 方法 | 作用 | 持久运行约束 |
|---|---|---|
| WithPreStep(...PreStepHook) | 模型请求前改写 Step.Messages，或以 PreStepResult{Halt,Answer} 提前结束 | 结果记入日志并回放；不在此执行业务效果 |
| WithIdentifiedPreStep(identity string,hook PreStepHook) | 为转换规则设置版本/配置身份 | 身份进入意图，变化后拒绝旧运行 |
| WithRequestHook(...RequestHook) | 调整请求或客户端，工具列表只读 | 须保持确定性，宿主负责配置版本 |
| WithRequestError(maxRecoveries int,...RequestErrorHook) | 请求失败时决定重试、消息调整或客户端替换 | 持久路径只允许原样重试，不能替换消息或客户端 |
| WithTurnStopping(...TurnStoppingHook) | 返回非空 user 消息续跑 | 受步骤与预算上限约束，结果回放 |
| WithPromptSections(...PromptSection) | 进入 system 的提示词段落，每轮固定 | 结果回放 |
| WithPromptContexts(...PromptContext) | 每步附加上下文，不留在后续历史 | 结果回放 |
| WithOffload(*contextpolicy.Offload) | 工具长结果换为引用，装为带身份 PreStep | 读取工具须显式注册；持久模式须有可恢复结果存储与 Dispatcher |

Step 含 Index、Messages、Inputs、Final；PreStepHook 为 `func(context.Context,*Step)(PreStepResult,error)`。PreStep 不是动态业务路由或任意副作用的执行入口。

### 请求重试与根预算

```go
configured := builder.NewSimpleBuilder(client).
    WithRequestError(2, builder.RetryTransient(2, nil))
```

RetryTransient 处理可重试的429、5xx与连接失败；已输出内容时不原样重试，遵守 Retry-After 与 context 期限。每次尝试独立占用根预算，恢复回放不再发请求；父 Agent 的重试配置不自动传给子 Agent。

跨 Agent 的请求、工具、预留Token与期限由 core/budget.Controller 控制，不能用 MaxIterations/MaxTokens 代替。启用根预算 Token 上限时须通过 WithTokenEstimator(TokenEstimator) 提供保守估计，覆盖输入、工具定义与输出预算；策略版本放在 budget.Config.TokenPolicyVersion。

## 输出格式

WithOutputFormat(builder.OutputFormatJSON) 等选项把格式要求加入提示词，WithCustomOutputFormat(string) 设置自定义格式提示。它们不等于 JSON Schema 强制输出或宿主业务校验。ReturnDirectTools 直接返回工具结果，启用者负责最终格式与完整性。

## 预配置与快捷构建

`ConfigureForRAG()`、`ConfigureForChatbot()`、`ConfigureForAnalysis()` 在同一个 Builder 上设置提示词、参数或中间件，见 [presets.go](../../builder/presets.go)。它们不是独立执行引擎。

以下函数均返回 `(*builder.SimpleAgent,error)`，内部调用 Build，因此需要控制初始化上下文时优先显式使用 Builder + BuildContext：

| 函数 | 参数 |
|---|---|
| QuickAgent | llm.Client, systemPrompt string |
| RAGAgent | llm.Client, retriever interface{} |
| ChatAgent | llm.Client, userName string |
| AnalysisAgent | llm.Client, dataSource interface{} |
| WorkflowAgent | llm.Client, workflows map[string]interface{} |
| MonitoringAgent | llm.Client, checkInterval time.Duration |
| ResearchAgent | llm.Client, sources []string |

这些是预配置助手：WorkflowAgent 不会把任意 map 编译成 workflow.Graph，MonitoringAgent 不会自动启动定时检查，ResearchAgent 不会因 sources 字段而自动安装搜索工具。RAGAgent 的非空 retriever 才接入 DocumentRetriever、Retrieve 方法或 VectorStore；nil 保留提示词预设。实现与实际装配见 [quick_builders.go](../../builder/quick_builders.go)。

## 无密钥的最小示例

```go
package main

import (
    "context"
    "fmt"
    "time"

    "github.com/kart-io/goagent/builder"
    "github.com/kart-io/goagent/core/budget"
    "github.com/kart-io/goagent/execution"
    "github.com/kart-io/goagent/llm"
)

func main() {
    if err := run(context.Background()); err != nil {
        panic(err)
    }
}

func run(ctx context.Context) error {
    const runID = "builder-reference-demo"
    controller, err := budget.New(budget.Config{RootKey: runID, Version: "v1", MaxModelRequests: 2})
    if err != nil {
        return err
    }
    scope, err := execution.NewScope(execution.Config{RunID: runID, Budget: controller, Timeout: 10*time.Second})
    if err != nil {
        return err
    }
    runCtx, cancel, err := scope.Bind(ctx)
    if err != nil {
        return err
    }
    defer cancel()
    configured, err := execution.ConfigureBuilder(builder.NewSimpleBuilder(llm.NewMockStreamClient()), scope, "assistant")
    if err != nil {
        return err
    }
    agent, err := configured.BuildContext(runCtx)
    if err != nil {
        return err
    }
    result, err := agent.AsRunnable().Invoke(runCtx, "演示 Builder 统一执行入口")
    if err != nil {
        return err
    }
    fmt.Println(result.Result)
    return nil
}
```

## 旧接口迁移

| 旧用法或旧说明 | 当前选择 |
|---|---|
| interfaces.Agent/Runnable 作为新编排协议 | core.Runnable；Builder 通过 AsRunnable 接入 |
| WithModelRetry、ModelRetries、ModelRetryBackoff | WithRequestError + RetryTransient |
| WithContextPolicy / WithContextPolicies / WithPolicyToolAdapter | WithOffload；自定义转换用 WithIdentifiedPreStep，见[迁移指南](CONTEXT_POLICY.md) |
| WithCommunicator 自动实现多 Agent 通信 | 宿主显式编排，组合子 Agent 工具与 Workflow |
| WithOutputFormatJSON 自动验证业务 DTO | 宿主或工具 Schema/结果校验器验证 |
| 所有 Builder 方法永久向后兼容 | 按 CHANGELOG 和当前源码迁移；0.x 版本可有破坏性变更 |

更多输出、工作流与等待语义见[核心 API](../api/CORE_API.md)、[扩展契约](EXTENSION_CONTRACT.md)、[执行治理](EXECUTION_GOVERNANCE.md)。
