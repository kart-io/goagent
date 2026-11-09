---
status: issues_found
reviewed: 2026-11-09T03:09:27Z
depth: deep
baseline: f2e0b0b449fcfda416169356291689248e5f455b
files_reviewed: 17
files_reviewed_list:
  - Makefile
  - scripts/compatcheck/main.go
  - scripts/compatcheck/postgres.go
  - scripts/compatcheck/postgres_test.go
  - scripts/coveragecheck/main.go
  - scripts/coveragecheck/main_test.go
  - management/internal/managementtest/store.go
  - management/internal/managementtest/fixture.go
  - management/offline_store_test.go
  - management/offline_service_test.go
  - management/offline_execution_test.go
  - management/offline_query_test.go
  - management/offline_reconciliation_test.go
  - management/httpapi/offline_regression_test.go
  - management/adapters/contracts_test.go
  - docs/evaluation/2026-11-09-management-hardening/patches/goagent-examples.patch
  - docs/evaluation/2026-11-09-management-hardening/patches/goagent-harness.patch
findings:
  critical: 0
  warning: 3
  info: 0
  total: 3
  new: 2
  preexisting: 1
  P0: 0
  P1: 0
  P2: 3
  P3: 0
---

# 验收门禁与新增测试独立审查

确认 **2 项新增 P2 门禁缺陷、1 项既有 P2 超时边界**。未发现本次定向测试中可确认的伪验证、数据竞态或无界等待；两份补丁的常量替换未发现语义遗漏。建议修复新增门禁缺陷后复核。

本报告只评审所列文件及其直接调用契约。未审查 pgstore 锁实现，未运行全量测试，未访问外部模型或数据库，未修改生产源码或已有测试，未提交。补丁完整性、实际源码身份和原验收证据归档由主代理负责，本报告不重复作出认证。

## 新增问题

### VR-01 · P2：关键契约的子测试跳过，完整实库门禁仍成功

**文件：** `/home/hellotalk/code/go/src/github.com/kart-io/goagent/scripts/compatcheck/postgres.go:57-60`；相关状态收集在同文件 `80-87` 行。

**触发：** 必需顶层测试通过，但其业务子测试调用 `t.Skip`。Go 允许父测试最终为 `pass`，整个 `go test` 退出 0。当前检查只查精确名称，子测试的 `skip` 单独存储，没有归并到父契约。`TestConformance` 本身就是子测试套件，因此父级通过不足以证明套件全部执行。

**影响：** `-require-postgres` 和 `-verify-postgres-report` 都会把关键契约部分未执行的结果判为成功。不是仅有畸形 JSON 才能触发。

**证据：** 用临时无外部依赖模块运行真实 `go test -json -race -count=1` 和完整 `compatcheck -require-postgres`，仅令 `TestConformance/durability` 跳过，其余必需名称正常通过。结果：

```text
full_require_postgres_child_skip 0 跨仓验证通过
github.com/kart-io/goagent-contrib/durable/pgstore passed
skipped_tests:
  github.com/kart-io/goagent-contrib/durable/pgstore/TestConformance/durability
```

脚本：`/tmp/goagent-validation-review-I81Lf3/child_skip.py`。
实际生成的报告：`/tmp/goagent-validation-review-I81Lf3/child-skip-report/summary.json`，其中 `01-tests.jsonl` 保留真实 Go 子测试事件；CLI 输出保存在同临时目录的 `child-skip-result.txt`。

该夹具只验证门禁，DSN 和容器名为未使用的占位值，没有数据库操作；**不表示原验收报告中实际发生了跳过**。

**修复方向：** 对每个必需契约，除顶层 `pass` 外，还拒绝其 `契约名 + "/"` 后代的 `skip/fail`；如存在允许跳过的辅助场景，用明确白名单区分。新增真实父子事件用例；现有 `postgres_test.go` 的“只有子测试”用例只证明子测试不能替代顶层通过，没有覆盖“顶层通过、子测试跳过”。

### VR-02 · P2：已有报告复核忽略包级失败和损坏尾部，错误认可不完整证据

**文件：** `/home/hellotalk/code/go/src/github.com/kart-io/goagent/scripts/compatcheck/postgres.go:75-90`；独立复核入口为 `/home/hellotalk/code/go/src/github.com/kart-io/goagent/scripts/compatcheck/main.go:378-384`。

**触发：** summary 仍标记模块 `passed`，必需测试的 `pass` 已存在，但日志包级终结事件被替换为 `fail`，或日志在必需测试之后被截断。解析器把 `Test == ""` 的包事件全部忽略，也把 JSON 解析失败当普通诊断文本跳过，且不要求包有成功终结事件。正常 EOF 不会使 `scanner.Err()` 报错。

**影响：** 已有报告在复制、拼接或归档过程中被截断、混入失败事件后，独立复核仍返回成功；summary 与日志矛盾也不会被发现。此处不是要求密码学身份认证，而是要求验证已有证据自身的完整性和一致性。

**证据：** `/tmp/goagent-validation-review-I81Lf3/probe.py` 为三个必需模块生成通过的 summary 和指定测试事件，分别注入以下情况；直接运行编译自当前源码的 CLI：

| 输入目录 | 变化 | CLI 退出码 |
| --- | --- | --- |
| `valid` | 完整通过事件 | 0，正确 |
| `top_skip` | 必需测试本身跳过 | 1，正确 |
| `top_fail_then_pass` | 同名必需测试先失败后通过 | 1，正确 |
| `package_fail` | 包级终结事件为 `fail` | **0，错误** |
| `truncated_tail` | 末尾为未完成的 `{"Action":"fail","Package":` | **0，错误** |
| `missing_package_end` | 完全没有包级终结事件 | **0，错误** |
| `foreign_json` | 追加其他包的失败事件 | **0，未检测污染** |

以上目录均位于 `/tmp/goagent-validation-review-I81Lf3/`。最小复核命令：

```sh
env -u GOROOT GO_TEST_MODE=true /tmp/goagent-validation-review-I81Lf3/compatcheck \
  -verify-postgres-report /tmp/goagent-validation-review-I81Lf3/truncated_tail
# 实库恢复证据检查通过；退出 0
```

**路径边界：** 正常 `-require-postgres` 执行先通过 `verifyModule` 检查实际子进程退出码和 context 错误，真实失败、超时不会仅因这项解析缺陷而变成成功。这里已确认的误放行主要是新增独立报告复核路径；不把它夸大为实跑过程忽略所有失败。

**修复方向：** 核验相关包的生命周期及最终成功事件，遇到失败事件拒绝；检查事件所属模块边界及 summary 一致性；将普通构建诊断和 JSON 测试流分开，拒绝损坏的事件行及未完成的流。增加包级失败、日志尾部截断、缺少包结束、异包污染用例。

## 既有问题

### VR-03 · P2：模块发现阶段不受指定超时约束

**文件：** `/home/hellotalk/code/go/src/github.com/kart-io/goagent/scripts/compatcheck/main.go:199`，调用链至 `152` 行；真正创建超时在 `235` 行。`221` 行的 `go version` 也使用未设期限的外层 context。

**归属：** 基线已经存在；本轮只把超时暴露给 Makefile 并增加实库目标，没有新引入该问题。

**触发：** 模块发现期间的 `go mod edit -json` 阻塞，例如自动工具链下载等待或 Go 启动器卡住。外层 context 只接收信号，没有 deadline，`-timeout` 在所有模块发现完成后才生效。

**影响：** 即使配置较短模块超时，验收仍可能一直停在发现阶段，且尚未建立输出目录/summary。不能用当前 `-timeout` 作为整个门禁具有截止时间的保证。

**证据：** `probe.py` 在临时 PATH 中放入仅执行 `sleep 30` 的假 `go`，创建最小模块目录，运行：

```sh
timeout -k 3s 1s /tmp/goagent-validation-review-I81Lf3/compatcheck \
  -root /tmp/goagent-validation-review-I81Lf3/repos \
  -out /tmp/goagent-validation-review-I81Lf3/timeout-report -timeout 20ms
```

实际执行约 `1.00s`，由外层 `timeout` 终止，退出 **124**；输出明确停在 `读取 .../goagent/go.mod: signal: killed`。假 Go 只模拟阻塞，不访问网络。真实模块执行阶段的超时与后代进程终止测试本次已通过。

**修复方向：** 为 discovery/版本探测设置明确期限，或增加独立启动阶段/全局期限并向下传递；保留现有每模块超时。增加发现阶段卡住的最小回归测试。

## 覆盖率、测试和迁移的核验结论

**覆盖率：** `coveragecheck/main.go:103` 用 `path.Dir` 精确匹配包，因此 `management/internal/managementtest` 不计入 `management`、`management/adapters`、`management/httpapi` 的独立门禁。新边界测试以及现有子包隔离测试通过。

整体门禁没有排除测试辅助包，这是既有统计口径：合成 profile 中生产数据 `74/100` 不通过，加入已覆盖辅助语句 `20/20` 后变成 `94/120 = 78.3%` 并通过。但对当前 Makefile 所用默认采集方式作定向验证，辅助包实际为 **0/224**，不会抬高整体值。只有输入包含辅助包已覆盖数据（例如改用跨包插桩或合并其他 profile）才会触发该边界。本轮不将其列为新增缺陷，也不据此否定原验收覆盖率。

**测试调用链：** HTTP 测试经真实路由和 Service；离线业务测试经 Service → Store/事务替身，Worker 测试经命令消费 → Factory/Runtime → 持久状态；适配器测试使用真实文件会话日志与预算、Native/Graph 适配器，模型为本地脚本替身。替身的串行事务不证明 PostgreSQL 隔离语义，History 替身的过滤也不独立证明生产 HistoryReader，这些是测试边界而不是本次发现的缺陷。

已检查新增并发回调、原子计数、停止/退出、轮询、幂等及回滚断言。选定高相关场景的三轮 race 验证通过，未发现可触发的竞态或无限等待。不把有限测试通过解释为所有调度情况均已证明安全。

**常量迁移：** 对两份补丁的全部增删行进行映射归一化，移除必要 import 变更后，分别有 **47 / 5 行**语义变更，与下表替换完全一致，没有额外条件或路由逻辑变化。结合旧声明、当前声明和消费点核对：

| 旧名 | 新名 | 协议值/语义 |
| --- | --- | --- |
| `workflow.START` | `constants.WorkflowStart` | `__start__`，图入口 |
| `workflow.END` | `constants.WorkflowEnd` | `__end__`，图结束 |
| `multiagent.Finish` | `constants.MultiagentFinish` | `FINISH`，由 GuardRoute 映射到图结束，未与 `__end__` 混淆 |
| `spill.ToolReadSpill` | `constants.ToolReadSpill` | `read_spill`，工具保留规则、allow 规则和计划模式名单一致 |

去掉的 `tools/spill` import 所在文件只使用该常量，`builtin.go` 仍使用 spill 实现；spill 包没有被遗漏的 `init` 副作用。不重复检查主代理已核验的补丁完整性。

## 本次运行证据

环境为 `go version go1.27.3 linux/amd64`。所有定向执行均使用 `env -u GOROOT GO_TEST_MODE=true`；仅临时目录中新增复现脚本和合成模块，未使用源码 overlay。

1. 门禁脚本自身测试：

   ```sh
   env -u GOROOT GO_TEST_MODE=true go test -race -count=1 -timeout 60s \
     ./scripts/compatcheck ./scripts/coveragecheck
   ```

   退出 0：compatcheck `30.774s`，coveragecheck `1.030s`。包括已有当前源码替换、嵌套构建、取消与进程组测试；这些测试未覆盖 VR-01/02 的输入。

2. 新增测试定向并发验证：

   ```sh
   env -u GOROOT GO_TEST_MODE=true go test -race -count=3 -timeout 60s \
     -run '^(TestOfflineStoreTransactionContract|TestOfflineExecution(InputResume|ApprovalDecisions|Cancel|UnknownEvidenceStopsEffects|PruneDependencies|ResumeRechecksPermission)|TestOfflineQueryRunEvidenceSummaryAndReadOnly|TestOfflineAcceptedTaskSurvivesRequestCancellation|TestNativeFactoryPlainChatRebuildsWithoutRequests|TestNativeFactoryApprovalBoundToStoredDecision|TestGraphFactoryApprovalAndCompletedInspection)$' \
     ./management ./management/httpapi ./management/adapters
   ```

   退出 0：management `2.228s`，httpapi `1.067s`，adapters `1.231s`，无 race 报告。

3. 默认辅助包覆盖率验证：

   ```sh
   env -u GOROOT GO_TEST_MODE=true go test -count=1 -timeout 30s \
     -run '^TestOfflineStoreTransactionContract$' \
     -coverprofile=/tmp/goagent-validation-review-I81Lf3/helper-default.out \
     ./management ./management/internal/managementtest
   ```

   退出 0；profile 中辅助包语句统计为 `covered=0, total=224`。此处只验证采集口径，不评估生产管理包覆盖率。

4. 两个复现 CLI 均从当前仓库构建到临时目录；`probe.py` 的逐项退出码见上文，`child_skip.py` 的完整门禁实跑退出 0。再次运行 `child_skip.py` 时需为其输出目录换一个不存在的新路径，避免复用已有报告。

工具说明：当前会话没有项目文档所列的 sequential-thinking、desktop-commander 或独立 Read/Write 工具，使用可用命令读取、局部调用链分析和 apply_patch 写入唯一报告；未生成额外仓库工作文档。

_审查人：Codex，独立验收门禁与测试审查；深度：deep。_
