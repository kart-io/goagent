# GoAgent 执行可靠性与治理实施计划

> 实施方式：独立任务按 dispatching-parallel-agents 并行实施；每项按 test-driven-development 验证；最终按 requesting-code-review 独立审阅。

**目标：**完成七项执行可靠性与治理优化，并验证独立 Harness 兼容性。
**架构：**复用 StreamRunnable、durable.Store 和根预算，公共接口向后兼容；执行机制留核心，场景组件留 Harness。
**技术栈：**Go 1.25、现有本地 Provider 测试设施、OTel，无新外部依赖。
**设计：**`docs/superpowers/specs/2026-10-30-execution-hardening-design.md`。

## 全局约束

不增加真实模型调用；不自动发布；不修改独立 Harness 的公开接口；不把未知用量当零；第三方副作用与阻塞函数保持宿主负责的明确边界。
所有实现前先跑新增失败测试；提交只包含自身所有权内文件。共享文件必须顺序交接。

## 审阅重点

损坏/不匹配的检查点；消费者提前取消与异常终态；恢复报告不得执行写操作；崩溃/并发预算不可超额放行；满队列、observer 阻塞、TTL 回收不破坏在途审批。

## 任务与文件

- [x] T1 初始化与记忆契约：`builder/configurable_agent.go`、`builder/events.go`、`builder/tool_events.go`、`builder/builder.go`、`builder/config_options.go` 与新增 memory/recovery 测试。提供 `WithMemoryFailurePolicy`（默认尽力、可选严格），读写错误保持 errors.Is。测试 Exists/Load 失败、错误状态类型、记忆读写失败/观察、各入口一致性。先失败测试，再实现，再 `go test ./builder`。
- [x] T2 流式组合：`core/runnable.go`、`core/stream_runnable.go` 与组合器测试。保留公开签名；Sequence 空输入恒等、Pipe/Sequence 单一终态、取消、缺失/重复终态和迟到错误都由共同执行源验证；回调配置不丢失。先失败测试，再实现，再 `go test -race ./core`。
- [x] T3 运行诊断：`core/durable/diagnostics.go`、`workflow/diagnostics.go` 及测试。只读函数接受现有 Runner/Graph、RunID、显式 operation ID，输出状态、恢复障碍和允许动作。以审批等待、完成、pending、版本变化和故障 Store 验证，不新增自动重试。先失败测试，再实现，再 `go test ./core/durable ./workflow`。
- [x] T4 预算扩展：`core/budget`、`builder/model_call.go`、新增 `builder/model_budget.go`、`tools/dispatcher.go` 及预算回归测试。保持 Reserve 原行为，新增估计预留入口和工具准入；注入宿主估计函数、截止时间、工具次数、预留与实际差额，覆盖未知用量/并发/重启/配置漂移。T1 完成后才编辑 Builder 公共配置；T5 Dispatcher 编辑必须等待 T4 交接。先失败测试，再实现，再 `go test ./core/budget ./builder ./tools`。
- [x] T5 资源治理：新增 `core/observe/async.go`、`tools/pipeline.go`、`tools/dispatcher.go` 与测试。实现显式生命周期的有界异步 observer、授权 TTL/Prune、可选排队上限及独立准入槽位。测试 observer 阻塞/关闭、丢弃统计、TTL 过期、队列满、审批不阻塞执行槽位、排队取消。先失败测试，再实现，再 `go test -race ./core/observe ./tools`。
- [x] T6 Provider 契约：`llm/providers` 或既有测试辅助内建立共用工具流测试用例，不扩大生产 Provider 能力声明。测试完整工具参数、正常/异常结束、取消和未知 usage，不依赖远端。已有覆盖用证据映射，缺口补测试，`go test ./llm/...`。
- [x] T7 文档与整体验证：`docs/guides/EXECUTION_CONTRACT.md`、新 `EXTENSION_CONTRACT.md`/治理指南、`docs/development/PRIORITY_IMPROVEMENTS.md`、README、CHANGELOG。记录范围、验收命令与限制。先过计划独立审阅；实现后检查每项验收，全量 `GO_TEST_MODE=true go test -race -timeout 120s ./...`、导入检查、Harness 跨仓验证，最终独立审阅并本地提交。

## 执行记录

起点 1cba257，隔离分支 feat/execution-hardening。项目 GOROOT 配置与 PATH 工具链不匹配，所有本地 Go 子进程使用 env -u GOROOT，不修改用户环境。

计划审阅完成：已采纳旧预算 JSON/intent 零值兼容、两段排队超时暂停、未完成诊断不推断失败三项修正，详见设计末节。实施前无剩余阻塞项。

## 完成记录与验证证据

- T1：先行失败矩阵确认初始化/记忆问题，包级race通过；严格写入非事务与宿主errorHandler边界已记录。
- T2：畸形流、空序列、取消、父子回调和Batch配置测试先失败后通过，core包race通过；父回调/Batch修复的迁移语义已写入扩展契约。
- T3：Runner/Graph只读诊断验证通过，无执行锁/写操作，不从未完成状态推断失败。
- T4：先行失败后完成预算接入；旧JSON、legacy ledger、completed模型intent、并发/重启/配置漂移、未知用量、工具回放与宿主输入键均有回归。审阅发现的零值继承、易失工具预算和损坏Token估计均已补失败测试并修复。
- T5：审批和执行槽分离，排队及替代授权超时测试先失败后通过；观察器/授权TTL专项race通过；第三方忽略ctx的生命周期限制已明确。
- T6：既有Provider证据映射加三Provider共享新测试，无生产能力虚增，无付费调用；llm全包及新矩阵重复race通过。
- 全仓：`env -u GOROOT GO_TEST_MODE=true go test -race -timeout 120s ./...`通过，原始日志 `/tmp/goagent-hardening-race.log`。最后的预算日志完整性修复后另跑budget全包race通过。
- 跨仓：`env -u GOROOT GO_TEST_MODE=true ../goagent-harness/scripts/verify-goagent.sh <本工作区>`通过发布基线、本地两模块全量测试及vet，日志 `/tmp/goagent-hardening-harness.log`。
- 导入分层0 findings，git diff --check通过。独立最终审阅无剩余P0/P1/P2。Docker与真实模型验证未启用，不把单测视为生产负载验证。
- 只做本地提交，不自动push或发布；金额换算、独立子任务配额与跨主机调度明确由宿主/成熟系统负责。
