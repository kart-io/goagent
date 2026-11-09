# 最终验收、治理装配与升级检查

本指南对应执行契约改进。离线完整示例：`go run ./examples/execution-governed`，使用模拟模型，不需要凭据；验证装配、模型最终回执及重建后零新增请求/效果。它不代表真实业务授权或生产负载验证。

## 最终输出验收

`builder.WithOutputValidator(validators ...builder.OutputValidator)` 接收 `func(context.Context, *builder.AgentOutput) error`。它检查中间件处理后的最终输出，覆盖纯文本、原生工具循环、工具直返、PreStep 提前结束及持久回放。Builder 完成回调前后均执行，`AsRunnable` 还通过 `core.StreamRunnable.WithFinalValidator` 在外层完成回调之后检查；配置和回调克隆会保留该终态门。

```go
b.WithOutputValidator(func(ctx context.Context, out *builder.AgentOutput) error {
    if err := ctx.Err(); err != nil { return err }
    if out.Result != expectedReceipt { return fmt.Errorf("最终回执不匹配") }
    return nil
})
```

校验器须只读、可重复调用，不应计费、写数据库或修改结果。返回错误或取消时不发布成功终态，Builder 的错误处理器不能把校验错误清除；panic 转为可识别的错误。流式错误块可以同时带 `Done=true`，成功必须满足 `Done && Error == nil`。

`TurnStopping` 负责可选纠正，`OutputValidator` 负责必须通过的验收。迭代额度不足会跳过前者，但不能跳过后者。校验器不发起重试，也不回滚已经发生的模型调用、工具效果、会话日志、检查点或回调效果。中间文本可能已经发出。下游 `Pipe` 主动变换出的其他结果应由该阶段另行验收。

纯通用 `StreamRunnable` 可用 `WithFinalValidator(func(context.Context, O) error)` 检查自身完成回调后的结果，多个验收器顺序执行。`WithCallbacks`、`WithConfig` 保留验收器；没有配置验收时不增加模型请求。

## 标准治理装配

`execution.NewGovernedBuilder(client, scope, agentID, registry, dispatchConfig)` 返回普通 `SimpleAgentBuilder`、`GovernanceReport` 和错误，复用 Scope、Dispatcher 与 Builder。

创建条件：Scope 有显式根请求预算和正工具额度；配置 Scope 超时或根预算绝对期限；显式提供 Authorize、正 `MaxAdmissionConcurrency` 与 `MaxQueued`；注册至少一个工具，客户端实现 `ToolStreamClient`。其他 Dispatcher 参数沿用原有默认值，宿主仍拥有并关闭资源。

宿主必须调用创建时的 `scope.Bind`，并把派生 context 传给 `BuildContext` 和执行。装配的请求钩子在模型调用前核对 Scope、预算与会话身份，工具授权前再次核对；忘记 Bind 时不会发出模型请求。其他宿主回调和自定义钩子的副作用仍须由宿主控制。

`builder.WithDispatcher(d)` 指定完整工具集，与 `WithTools` 不能混用，配置顺序不影响此约束；nil 派发器在 Build 时报错。需要追加工具时先注册进同一个 Registry，再创建 Dispatcher，避免工具绕开治理。重复 WithDispatcher 采用最后一次配置，重复 Build 不会把派发工具写回 Builder。

报告只描述创建时装配：根期限、根额度、持久预算、日志、工具名单及派发默认参数，不包含函数或可变资源引用，也不证明某次业务操作已获授权。`Dispatch.Timeout` 是默认工具超时，工具声明可覆盖它。Builder 后续配置不会修改既有报告；宿主不得把快照当作动态安全认证。

## 构建期能力检查与迁移

以下配置要求 `ToolStreamClient` 且工具集合非空，否则 BuildContext 在初始化回调与存储访问前报错：PreStep、RequestHook、TurnStopping、PromptSections、PromptContexts。此前有的配置会被忽略，现在统一提前拒绝。`SessionLog` 支持普通流式聊天和原生工具循环，不要求配置工具；纯聊天继续使用原有 `model/answer/0` 持久操作键。

只检查最终格式的纯文本业务应使用 OutputValidator；需要循环干预时配置真实工具流。RequestError 的原样重试仍支持无工具路径。已有 WithTools 用法保持可用，需要统一派发时改为 WithDispatcher 并移除 WithTools。

## 恢复清单和升级前检查

`workflow.DurableConfig` 增加可选 `StateVersion`、`Components map[string]string`，用于声明状态结构和 Agent、工具、提示词、策略等宿主组件版本。非空声明纳入计划摘要；改动后旧运行不再匹配。空字符串配 nil 或空组件 map 保持旧版计划摘要不变。

```go
d, err := graph.CompileDurable(ctx, workflow.DurableConfig{
    Store: journal, Version: "refund-v2", StateVersion: "refund-state-v1",
    Components: map[string]string{"agent": "v3", "payment-tool": "v2"},
})
if err != nil { return err }
manifest := d.Manifest() // 独立副本，可记录到发布产物。
report, err := d.CheckCompatibility(ctx, "run-1", "run-2")
```

Manifest 导出版本、计划摘要和声明副本，不包含业务数据。CheckCompatibility 复用 Diagnose，只读取指定运行；不加运行锁、不写入、不审批、不调用节点或业务状态解码。运行不存在为不兼容；空或重复 ID 报错。组件输入与导出 map 均复制，外部修改不能改变已编译身份。

Compatible 仅表示声明和检查点结构匹配：`outcome_unknown` 也可能匹配，仍须核对外部效果和活跃执行者。读取多个运行不构成一致性快照；不自动列举、迁移、恢复或回滚任务。Go 闭包行为不能自动识别，宿主必须正确提升版本。

升级顺序：归档旧 Manifest 和待处理运行 ID；用新图执行只读检查；对不兼容在途任务先用旧版本完成、排空或执行明确迁移；最后切换新流量。不能为了通过检查而覆盖旧计划摘要。

## 性能基准

历史读取、无变化同步、追加同步和重写同步分别测量，内存/文件存储各覆盖100、1000、10000条消息。变更场景在计时外重建固定历史，使用固定次数以控制夹具构建成本：

```sh
GO_TEST_MODE=true go test -run '^$' -bench '^(BenchmarkSessionHistory|BenchmarkLargeToolResult)$' -benchmem -benchtime=5x -count=3 ./core/session ./builder/contextpolicy
GO_TEST_MODE=true go test -run '^$' -bench '^BenchmarkAgentSessions$' -benchmem -benchtime=300ms -count=3 -cpu=1,4 ./builder
```

会话基准区分同 Agent 串行执行和独立会话并发；模拟模型没有网络延迟，不能把结果当成真实模型耗时。使用工作树验证关联仓库时，明确执行 `go run ./scripts/compatcheck -root /path/to/repos -core-dir /path/to/current/goagent -out /new/report`；Makefile 的 COMPAT_CORE 默认是当前目录。
