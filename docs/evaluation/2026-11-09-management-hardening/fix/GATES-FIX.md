---
status: all_fixed
findings: [VR-01, VR-02, VR-03]
fixed: 3
skipped: 0
committed: false
---

# 验收门禁修复

VR-01、VR-02、VR-03 已修复并完成本地验证，Go 源码已稳定。保留原有未提交改动，没有提交、回退或改写其他代理负责的源码。

## 变更

| 问题 | 修复与验证 |
|---|---|
| VR-01 | 必需顶层契约必须通过，其名称加 `/` 匹配的全部后代也必须通过。真实 Go 并行、多层子测试的 skip/fail 均拒绝；关键契约之外的辅助 skip 保持允许。完整 `-require-postgres` 夹具修复前退出 0，修复后退出 1。 |
| VR-02 | 检查包 start/终态、测试 run/终态、全部失败事件、模块路径边界、重复生命周期、JSON 损坏与摘要统计一致性。所有已出现的包必须结束；无测试包仅在存在 `[no test files]` 输出且没有测试执行时允许包级 skip。普通 Go 诊断与依赖 `build-output` 保留兼容，`build-fail` 拒绝。 |
| VR-03 | 发现、Git 版本记录与 Go 版本探测共享由现有 `-timeout` 创建的启动时限；后续每个模块仍有独立总时限。Go 探测复用 `goCommand`，Git 记录复用现有进程组取消与清理机制。没有新增 CLI 参数。 |

修改路径：

- `scripts/compatcheck/main.go`：启动期限及帮助文字。
- `scripts/compatcheck/postgres.go`：契约后代、事件完整性及摘要核对。
- `scripts/compatcheck/postgres_test.go`：补齐既有夹具的真实生命周期，新增真实 Go 事件及针对完整性的变异回归。
- `scripts/compatcheck/process_unix_test.go`：发现与版本卡住的回归，并验证取消后后代不能执行副作用。
- `scripts/README.md`：同步证据约束、启动期限与摘要生成边界。

## 红绿证据

先添加回归、保存失败日志，再修改生产实现。新证据在 [review/repro/gates/fix-8cIyLB](../review/repro/gates/fix-8cIyLB/)，原审查证据未覆盖。

- [修复前定向 race](../review/repro/gates/fix-8cIyLB/red.log)：真实后代 skip、包失败、损坏尾部、缺失开始/结束、跨模块污染、摘要矛盾等回归失败；发现和版本探测均等到外层 4 秒看门狗才结束。
- [修复后定向 race](../review/repro/gates/fix-8cIyLB/gates-green.log)：上述回归全部通过，发现和版本阻塞各约 1 秒结束，取消后的后代副作用检查通过。真实成功事件覆盖并行子测试、无测试包；诊断兼容用例通过。
- [完整 CLI 修复前输出](../review/repro/gates/fix-8cIyLB/gates-cli-red.log) 与 [前后对照](../review/repro/gates/fix-8cIyLB/gates-cli-comparison.log)：原审查三类不完整报告及真实子测试跳过报告由退出 0 改为退出 1。重新运行完整门禁的真实子测试跳过夹具同样退出 1；修复前后原始报告分别归档于 `child-skip-report/`、`child-skip-report-after/`。
- 原审查合成报告还包含重复包 start，新解析器会更早拒绝；针对包失败、截断和缺结束本身的独立验证以新测试生成的完整真实 Go 流为基准，避免依赖这些合成报告的其他缺陷。
- [上一轮完整实库日志复核](../review/repro/gates/fix-8cIyLB/gates-original-evidence.log)：新 CLI 对 `.goagent-hardening-20261109-APraZJ/evidence/compatibility-final` 只读复核退出 0，包括既有辅助 skip 与无测试包。实际三个必需模块日志中没有非 JSON 行；另以下载、编译文本和依赖构建事件补测兼容性。

## 执行验证

所有 Go 命令使用 `env -u GOROOT GO_TEST_MODE=true`。

```sh
env -u GOROOT GO_TEST_MODE=true go test -race -count=1 -timeout 60s -v \
  -run '^(TestPostgresEvidence|TestVerifyStartupDeadline)' ./scripts/compatcheck

env -u GOROOT GO_TEST_MODE=true go test -race -count=1 -timeout 90s -v \
  ./scripts/compatcheck ./scripts/coveragecheck

env -u GOROOT GO_TEST_MODE=true go vet ./scripts/compatcheck ./scripts/coveragecheck
git diff --check -- scripts/compatcheck scripts/README.md
```

定向 race 退出 0，耗时 8.317 秒；[两个验证器全包 race](../review/repro/gates/fix-8cIyLB/gates-verifiers.log) 均退出 0，compatcheck 37.593 秒、coveragecheck 1.029 秒。既有源码替换、嵌套构建、取消和进程组回归全部保留并通过。`go vet` 与差异空白检查退出 0。完整日志保存在 `/tmp/goagent-review-fix-8cIyLB/gates-*.log`。

## 范围与边界

没有运行全跨仓矩阵，没有连接数据库、模型或其他外部服务。真实事件夹具是本地临时 Go 模块，DSN/容器值仅用于满足 CLI 配置检查，不发生数据库操作。原始实库日志的只读复核不等于本轮重新执行数据库恢复场景。

证据检查证明已有事件流的结构与摘要一致性，不提供来源认证，也不能发现同时被完整删除且未在摘要留下痕迹的包；关键契约缺失仍必然拒绝。普通非 JSON 诊断保留兼容，疑似 JSON 事件的损坏行拒绝。单次 `-count=1` 日志不允许拼接重复运行。新的启动期限可能使过慢的发现/工具链准备提前失败，可调现有 `-timeout`；它不是全矩阵共用期限。启动阶段失败可能尚无 summary，已在使用说明中写明。非 Unix 仍沿用原有父进程取消边界。

实现前已核对 `CLAUDE.md`、两个审查报告，以及 `postgres.go`/`postgres_test.go`、`main.go`/`main_test.go`、Unix 进程取消测试与 `internal/processgroup` 的既有模式；复用项目 JSON 编解码和统计逻辑，生产与测试注释均为中文。仓库没有 `.claude/skills` 或 `.agents/skills`；当前工具缺少独立 Read/Write、desktop-commander、sequential-thinking 等，使用命令读取、局部补丁、真实回归与差异检查补偿。按本次限定写入范围，将上下文与验证说明集中于本报告。
