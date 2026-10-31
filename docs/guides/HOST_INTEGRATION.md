# 宿主标准接入

`execution.Scope` 组合宿主已有的执行身份、会话、日志、预算、期限与观察者，不创建新的运行引擎。它与 Builder 内部持有状态、存储和检查点的 `core/execution.Runtime` 无关，只负责装配。

## 构建与运行

完整可运行代码见 [离线示例](../../examples/execution-scope/main.go)，不需要模型密钥：

```bash
go run ./examples/execution-scope
```

典型顺序是 `execution.NewScope(config)` → `scope.Bind(requestCtx)` → `execution.ConfigureBuilder(b, scope, agentID)` → `b.BuildContext(runCtx)` → `agent.AsRunnable().Invoke/Stream(runCtx, input)`。每一步都检查错误，并对 Bind 返回的取消函数使用 defer。ConfigureBuilder 返回配置后的 Builder；泛型参数由 Builder 推导。Builder 是可变对象，不应并发配置。

`BuildContext(ctx)` 拒绝 nil、已取消和已超时上下文，将同一上下文传入检查点初始化和 OnStart 回调，并在存储操作、各回调返回后检查取消。旧 `Build()` 仍使用 Background。框架不能强制中断忽略上下文的同步实现，也不回滚初始化回调已经发生的外部效果。宿主应使用支持上下文的存储与回调。

## 身份与恢复

- `Config.RunID` 必填且无首尾空白；Budget 的 RootKey 必须与 RunID 一致。恢复时保留 RunID、agentID、预算版本和配置。
- SessionID 为空时使用 RunID；显式 SessionID 作为宿主会话身份保留。多个运行可以共享会话，宿主仍须协调不同 Agent 实例对同一会话的并发写入。
- `ConfigureBuilder` 设置会话与持久作用域，之后不要另行覆盖这些身份。工作流的 `RunRequest.RunID` 使用 `scope.RunID()`；节点执行键继续由图派生，不能全部替换成根 RunID。
- `observe.RunIDFromContext(ctx)` 可以读取观察身份。Bind 检查已有执行键、会话、观察身份和预算冲突。

每次 Bind 使用新的宿主请求上下文；不要重用上次已绑定的上下文。任何预绑定 Journal 都会被拒绝，包括没有执行键的 Journal；这防止宿主请求误继承其他运行的日志。已有匹配的裸身份及同一预算控制器可接受。

## 资源与期限

`Config` 可选 Journal、Budget、Timeout、Observer、Tracer。绑定 Journal 时如同时使用预算，预算必须持久化；仅使用 Journal 不强制创建预算。Scope 不关闭 Store、Observer 或 Tracer，宿主负责这些资源的共享与关闭。未显式配置 Observer/Tracer 时继承父上下文。Scope 只取消派生上下文，实际期限取父上下文、Scope Timeout 与预算期限中最早者。

Scope 不提供跨进程调度、分布式会话互斥或自动副作用补偿；多实例共享持久运行需要支持租约的 Store（如 goagent-contrib 的 `durable/pgstore`）。持久审批与恢复由现有 Runner、工作流和 Dispatcher 负责；其行为见 [执行治理](EXECUTION_GOVERNANCE.md)。恢复集成测试见 [execution/integration_test.go](../../execution/integration_test.go)，覆盖审批暂停、执行后检查点失败、重建恢复与完成回放，并验证副作用和预算不重复。

## 开销与验收

[实测报告](../evaluation/2026-10-31-execution-overhead/REPORT.md) 保存三种预算模式、10/100/1000 操作矩阵、固定账本微基准与原始数据。探针可独立运行：

```bash
go run ./evaluation/executionbench/cmd/run -operations=10,100,1000 -modes=none,memory,file -timeout=10m
```

这是本机合成负载，文件模式只包含预算持久化，没有真实模型、工具日志或业务服务。接入真实服务后仍须验证典型任务的延迟、取消、恢复、预算和资源生命周期。
