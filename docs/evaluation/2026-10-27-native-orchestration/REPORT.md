# GoAgent 原生编排与工具体系验证

本轮基于提交 `8b4aa16` 开发，改动尚未提交。图执行、工具调度与恢复均由 GoAgent 自身实现，没有添加其他编排框架依赖。已有的 google/jsonschema-go v0.4.3 从间接依赖调整为直接依赖，版本未变化。[源码与验证上下文](run-context.json)。

## 已实现

- 持久并行：分支输入在执行前冻结并保存，成功结果逐分支保存；重启跳过已保存结果。Merge 按注册顺序执行，失败时保留分支结果。
- 统一执行：DurableGraph.AsRunnable 提供 Invoke/Stream/Batch；审批暂停以 Waiting 状态返回，兼容 Run 保留原错误语义。Snapshot 提供不推进执行的状态读取。
- 统一工具：Dispatcher 同时接入 workflow.Adapt 和 Builder.WithTools，提供 Schema/自定义校验、授权钩子、共享并发门、含排队的超时、严格失败传播，默认不重试。
- 执行键：节点、分支及顺序步骤向 context 注入稳定执行键，工具从宿主获得 ToolInput.ExecutionKey，模型参数不能覆盖它。
- 原生退款案例：新增 -engine graph，使用并行准备、业务审批、Dispatcher 工具与 SQLite 账本。旧 steps 模式及其检查点保持兼容。

## 测试证据

| 检查 | 结果 |
|---|---|
| 全库 Go 1.25 race，未使用 short | 64 个测试包通过，退出码 0 |
| 全库构建 | 通过 |
| golangci-lint 2.6.2 | 0 issues |
| 严格导入检查 | 0 findings |
| workflow 示例迁移检查 | 0 findings |
| 独立 CLI 崩溃恢复实跑 | 退出码 86 后恢复成功，重复运行同一回执，账本 1 笔 / 9900 分 |

日志：[全库竞态](all-race.log)、[lint](lint.log)、[最后 Runnable/兼容性复测](runnable-final.log)、[退款实跑结果](refund-smoke.json)。SQLite 依赖仍输出已有 C const 编译警告，不影响检查结果。

重点回归包括：分支失败后仅重跑未保存分支、Merge 失败不重跑分支、分支保存失败沿用效果键、并发上限与取消后等待退出、子进程在效果落盘后崩溃、分支序列化 panic 转错误、旧串行 v1 审批检查点兼容；工具嵌套 Schema 校验、整数参数不损失精度、未授权不执行、失败无重试、排队/执行超时，以及 Builder 实际 Provider 协议路径使用同一个 Dispatcher。

Builder 集成测试使用本地 HTTP 协议服务；退款 CLI 无模型请求，订单和审批身份为合成演示数据。本轮没有新增付费模型调用，不将上述结果称为真实模型实测或生产支付验证。

## 边界

外部副作用仍是至少一次执行，必须由业务幂等键去重；一个节点内部多次写操作需各自的业务 ID，不能重复使用一个执行键代表不同效果。Merge/Router 必须无副作用。FileStore 仍限可靠本地文件系统，尚无跨主机租约、自动补偿或持久事件重放。跨节点 Collect/Transform、字段映射 DSL 和可视化调试器未在本轮实现。

使用说明见[原生编排与工具体系](../../guides/NATIVE_ORCHESTRATION.md)。
