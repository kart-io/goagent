# 项目验收稳定化实施计划

用户先要求按项目分析建议执行；验收完成后，于 2026-11-15 明确授权提交并推送当前分支。

**目标：**消除已复现的测试时序依赖，取得一次无重试的串行完整验收，同步当前支持范围与后续验证计划。

**方案：**截止时间测试使用标准库 `testing/synctest` 的虚拟时间与现有 `session.DurableStore(session.NewMemoryStore(), id)`，保留真实执行、日志与预算路径；FileStore 的磁盘持久性由现有后端和进程恢复测试验证。等待模型启动必须同时观察调用提前结束。完整验收串行执行包，使用与 CI 一致的 10 分钟包超时；不降低断言、覆盖率门槛或跳过耗时用例。

**边界：**不改变生产执行策略，不增加模型调用或数据库依赖，不直接实现新的记忆算法和分布式调度。真实业务收益与容量目标先形成可执行评测清单。

## 任务与验收

- [x] 修复 `builder/model_resend_test.go`：复用现有存储适配器，确定性触发 deadline，启动与结束等待有失败出口；保留重发次数、回放与预算断言。失败依据为前次原测试与诊断日志；修改后定向 `-race -count=10` 通过。
- [x] 修复 `tools/dispatcher_interrupt_test.go`：虚拟时间隔离 20ms 工具截止时间，仍断言效果只发生一次、对账与回放正确；定向 `-race -count=10` 通过。
- [x] 统一 Makefile 的测试超时与包并行参数，更新测试指南；以 `-p=1 -timeout=10m` 完整运行主模块的 race 与覆盖率，不同时运行另一套全量检查。
- [x] 更新 MEMORY_DESIGN、MEMORY_GOVERNANCE、MEMORY_G1_STORAGE、MEMORY_G2_DERIVATION 和 docs 索引的状态；新增当前能力、升级边界及后续评测清单的指南，保留历史设计与测量原始结论。
- [x] 执行覆盖率门禁、独立 frameworkbench、build、vet、导入分层与格式检查；在 `.claude/verification-report.md` 留下实际结果和未覆盖边界。

## 重点检查

模型调用前已失败时测试不得挂起；虚拟时间测试只使用泡内创建的内存资源；回放不重复副作用与预算；磁盘和跨进程测试不删减；纯聊天召回、记忆管理及多实例能力不能被描述为已实现。

## 执行记录

- 两个时序用例以 `-race -count=10` 定向运行通过。原 FileStore 的崩溃、工具循环取消恢复与底层存储契约测试保留。
- 变异验证（仅 `/tmp` overlay，不改工作区）：删除重发开关时两个中断分支均失败；跳过 `Reconcile` 时效果次数断言得到 2 而不是 1，测试失败。
- 独立只读审查完成：没有新增阻塞项；已核对虚拟时间隔离、断言、FileStore 覆盖和迁移边界。
- 主模块完整 race 与覆盖率以单套 `-p 1 -timeout 10m -count=1` 运行，结果待命令结束后回填。

- 第一轮完整串行验收：Memory 通过（387.539 秒，91.3%）、Skills 通过（4.741 秒）；唯一失败为 `scripts/compatcheck/TestVerifyStartupDeadlineStopsDiscoveryAndVersion/mod`，1 秒配置时限在假命令的后代写入 started 前结束。原用例定向复跑通过，记录为进程启动窗口的时序不稳定，未忽略这次失败。
- 补充修复 `scripts/compatcheck/process_unix_test.go`：真实进程测试保留 5 秒配置时限与独立 15 秒外层看门狗，继续要求到达指定阻塞点、外层 ctx 未结束与后代无副作用，错误断言收紧为 DeadlineExceeded。生产超时机制不变；修复后重新做完整验收。

- 启动时限用例 race 连续3次通过；临时移除生产代码的启动时限（仅 overlay）后，测试在15秒看门狗处按预期失败，证明测试仍能发现配置时限失效。补充复审无阻塞项。
- CI 使用的 golangci-lint v2.15.0 本地运行通过（0 issues）；独立 frameworkbench 的 race 契约通过。

## 最终结果（2026-11-15）

第二轮完整主模块race+coverage：136个包结果齐全、74个测试包通过，无失败或竞态报告；整体覆盖率81.5%，全部12项门禁通过。Memory 385.971秒、91.4%；compatcheck 59.996秒、83.4%；Skills 5.375秒、94.4%。独立frameworkbench、build、vet、导入检查和v2.15.0 lint均通过。详细过程、失败证据、三个预期失败的变异及独立审查归档在 `.claude/project-stabilization-2026-11-14/VERIFICATION.md`。验收时尚未提交或发布，未发起真实模型或实库验收；后续用户已授权提交与推送。
