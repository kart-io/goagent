# 宿主接入稳定化设计

用户已授权依据上一轮设计建议执行；当前基线a662fae。

## 目标与选择

采用增量可取消构建、薄层装配和先测量后优化；不重写Builder/日志，不自动引入容器或服务部署。
保留Build兼容入口，新增BuildContext避免初始化I/O被Background隔离；装配包放顶层execution，依赖现有公开接口，低层禁止反向依赖。
不把完整宿主框架或全局服务容器塞进核心。

## 接口与约束

1. `AgentBuilder.BuildContext(ctx)`透传ctx到Initialize/检查点/初始化回调；已取消/截止ctx不开始初始化，取消错误保留errors.Is。nil ctx返回清晰错误；初始化后再次检查ctx，取消不能返回可用Agent。旧Build委派Background，不改旧语义。不用后台goroutine强制超时忽略context的第三方函数。
2. 顶层`execution.Config`含RunID、SessionID（空时默认RunID）、Journal durable.Store、Budget *budget.Controller、Timeout time.Duration、Observer、Tracer。RunID必须非空且无首尾空白，Timeout不能负数。Budget存在时RootKey须等于RunID；有Journal和Budget时预算必须持久。Journal缺省允许只做内存执行或仅预算持久化，不隐式创建Store或额度。
3. `execution.NewScope(Config)`严格校验，`Scope.Bind(ctx)`返回context、cancel、error，组合tools会话ID、durable根执行键/日志、budget.Bind、observe运行身份和可选sink/tracer，期限取父context、Timeout与预算绝对期限的最早值。Bind失败不得创建存储或发模型请求。禁止重绑已装配Scope或冲突的运行/会话/预算；预绑定日志context一律拒绝，避免不可比较Store的身份猜测。允许沿用相同Controller与一致的裸执行/会话键。nil/已取消ctx明确失败。每次独立执行从未装配的宿主context重新Bind。
4. `execution.ConfigureBuilder(b,scope,agentID)`返回builder/error；agentID非空且无首尾空白，在同一节点内标识稳定DurableScope。设置SessionID与DurableScope，调用方在此之后不要覆盖；不会自动Build或管理Agent/Observer/Store关闭。Scope只管理自己创建的context取消，不关闭宿主所有的资源。图RunRequest.RunID仍由宿主显式传scope.RunID()，图节点继续派生自身key，根身份不冒充节点key。
5. 集成验证通过公开API装配Builder与持久图：隔离会话、根预算/执行键、审批等待及重建后的恢复、配置冲突在执行前拒绝。不修改独立Harness公开接口，继续跨仓验证。

## 测量与试运行

新增`evaluation/executionbench`离线探针，对无预算/内存预算/文件持久预算运行10/100/1000个固定无外部副作用工具操作，输出JSON。记录总耗时、操作P50/P95、过程分配字节、GC后存活堆、goroutine变化、文件数量/字节与预算快照，成功次数作为正确性验证。File模式只持久预算，不同时开启工具日志，避免把日志成本误标为纯预算差异；另用集成测试验证日志恢复。
计量用固定合成usage，不调用模型、不代表真实模型效果或费用。报告注明机器、工具链、时间、配置及测量范围；不把goroutine/堆瞬时采样称为无泄漏证明。
增加预算ledger的固定规模RecordUsage微基准，用公开API预填10/100/1000条后计时，避免循环无限增长。先记录真实数据；没有证据不改持久格式或记账算法。
实际宿主服务与业务任务尚未指定，已异步询问；此依赖不阻塞仓内实现，但不能据离线探针宣称完成生产试运行。

## 验收

新增功能先失败测试再实现；全仓/race、导入边界、Harness跨仓验证通过。测量命令可复现、报告包含环境和限制。独立计划及代码审阅；只本地提交，不自动push/tag/部署。

## 计划审阅约束（已采纳）

- Initialize在Exists/Load及每个OnStart前后检查ctx.Err；BuildContext初始化返回后复查，忽略ctx但随后返回的实现不能继续下一阶段。
- NewScope拒绝typed-nil Store/Observer/Tracer，零值Scope不可Bind。用JournalStore而不是HasJournal检测预绑定日志；禁止任何已有Scope标记。
- core/observe增加只读RunID读取器，Scope校验既有观测RunID冲突；显式Observer/Tracer覆盖父配置，未配置则继承，不负责关闭。
- 恢复测试断言图节点派生key稳定、效果不重复、根预算不重复消耗，不能把根key误用为节点key。
- 三mode的工具、输入与合成usage一致，操作key各不相同；循环计时排除初始化、清理、文件枚举与最终统计。微基准预填固定规模，不随b.N扩张。
