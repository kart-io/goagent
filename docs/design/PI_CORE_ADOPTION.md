# Pi 设计在 GoAgent 中的采纳边界

日期：2026-11-01。状态：本轮实施范围已确定；实现与验证结果见文末。

## 定位与方案

GoAgent 是嵌入 Go 服务的可恢复、可计量、可观测 Agent 编排内核。沿用现有 Runnable、Dispatcher、Graph、会话日志与宿主提供的存储，不引入 Pi/TypeScript 依赖或第二套执行循环。依据用户确认的分层建议，采用“先补现有执行契约和真实缺口”的方案。整体移植 Pi CLI 会引入产品假设；新建多 Agent 调度平台则越过现有单机恢复边界，均不采用。

本轮交付：固定来源的能力对照、Handle 未领取输入查询与失败时 Followup 保留、针对性回归，以及通用组合使用指南。业务意图分类、会话 UI、沙盒供应商、租约服务、分布式后台调度不进入核心。

## 输入契约

新增 `Handle.PendingInputs() []PendingInput`，`PendingInput` 包含 `Kind` 与 `Text`，Kind 为 `steer`、`inject`、`redirect` 或 `followup`。结果为锁保护下的独立快照；同一队列保持顺序，不承诺不同队列的全局到达顺序。调用不消费输入、不自动重试，可在运行中或 Wait 之后查询。

“待处理”只表示仍在 Handle 队列、尚未被执行循环领取。已领取不等于已发送给模型，更不等于业务完成；快照不能用于推导副作用是否发生。失败时不删除尚未领取的 Followup；成功开始下一轮时才移出该队列。

Steer 到达过晚、Inject 不唤醒、MaxIterations 耗尽、调用失败或取消时，可能留下待处理输入。宿主在 Wait 返回后决定展示、丢弃或创建新执行；核心不为清空队列绕过预算或自动开始新 Run。Wait 自身等待超时不表示 Handle 已结束，稳定终态须等待实际运行收敛。

Handle 是进程内控制对象，入队成功不表示输入已经持久保存。持久操作只保护已提交的执行决定；要求跨进程可靠接收的服务必须先写宿主收件箱，再提交执行，并维护自己的接收 ID/投递确认。PendingInputs 不是消息账本或 exactly-once 承诺。

## 现有能力优先复用

- `NewRunnableTool` / Factory：Agent、Graph 与普通函数共享工具适配，不新增独立 Agent 继承体系。
- `RunnableToolConfig.Resumable`、`durable.ChildKey`、`OperationPolicy.Reconcile`、`DurableGraph.AsChildRunnable`：已有可恢复嵌套流程及审批等待。不能因 Harness preset 默认禁用子 journal，就说 GoAgent 不支持持久子流程。
- `WithSessionLog`、PreStep、Offload、Store.Fork：已有模型历史、上下文改写、结果外置和分支基础。补清身份与投影，不新增同义 Fork。
- Handle.Wait、AgentEvent.done、GraphSnapshot.Done：各有不同范围，不增加一个含义模糊的全局 idle 事件。

## 后续能力的准入条件

独立 child run 如有宿主需求，应另行定义父子身份、前后台所有权、取消、等待和恢复契约；首先复用 durable 与成熟宿主调度系统。不能把 goroutine 或普通 Resumable 工具宣称为后台持久任务服务。

动态工具发现只改变模型声明，不替代 Dispatcher 授权；Code Mode 后置为可选适配，固定流程使用 typed workflow。远端批量文件协议、资源台账和 Sandbox 生命周期属于 Harness，核心只消费能力接口。会话 Fork 不回滚文件、数据库或外部 API。

## 验证

针对实际 Builder/Handle 执行链验证：失败与取消保留 Followup；成功后待处理输入为空；结束边界与额度耗尽留下输入；查询快照不影响队列且支持并发访问。受控模型仅替代外部供应商，使用真实工具循环。现有嵌套审批、预算与工具并发测试一并回归。

本轮已完成上述范围，相关包 race、根模块测试、离线 Example 与独立代码审查通过，见[验证报告](../evaluation/2026-11-01-pi-core/REPORT.md)。独立持久后台子任务与其他外围事项仍为后续评估，不列入已交付能力。
