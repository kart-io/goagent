# 工程入口修复与离线业务验证

日期：2026-11-04。GoAgent 基线 `64c262d3` 加本轮 Makefile、CI 和文档修改；执行内核未修改。退款案例来自本地 goagent-examples `560770079dcab005ca6b59ff452a8ab93c6a1be5`，通过其已有 go.work 引用当前 GoAgent 工作区，示例仓库无修改。

## 工程修复与验证

- PR 导入检查删除失效的 `--strict` 参数；实际执行 `bash scripts/verify_imports.sh` 返回 `imports: 0 findings`。
- `make build` 改为编译库与示例包；`make run` 默认执行离线宿主示例，可用 EXAMPLE 指定包；清理不存在的统一二进制相关目标。
- CI 的 build job 改为直接执行 `make build` 和 `make run`，让实际开发入口受 CI 覆盖。
- 更新中英文 MCP 描述、旧同步入口说明、Go/lint 版本及 v0.43–v0.46 迁移清单。
- `make all` 退出码 0：53 个有测试的包全部通过 `-race`，随后编译成功。原始输出见 [suite.log](suite.log)。
- `make build-all` 退出码 0：Linux amd64/arm64、Windows amd64、Darwin amd64/arm64 的 CGO-disabled 编译通过，见 [crossbuild.log](crossbuild.log)。仅编译，不代表跨平台运行验收。
- `make vet` 通过，见 [vet.log](vet.log)。`make help`、`make version` 和 `git diff --check` 通过，两个修改后的 workflow YAML 可解析。
- `make run` 返回模拟回答，已预留模型请求为 1；`make run EXAMPLE=./examples/workflow` 通过，见 [workflow-example.log](workflow-example.log)。

本机为 Linux/amd64、Go 1.27.3、Intel Core i7-14700KF，GOMAXPROCS=28；没有在本轮重新验证最低 Go 1.26 工具链或远端 GitHub Actions。

## 恢复、审批、幂等与预算证据

退款测试命令在 goagent-examples 根目录执行：

```sh
go list -m -f '{{.Path}} {{.Dir}}' github.com/kart-io/goagent
go test -race -count=1 -timeout 3m -v ./workflow/refund-recovery
```

第一条命令确认模块解析到相邻的当前 GoAgent 工作区；第二条退出码 0，原始输出见 [refund.log](refund.log)。

| 场景 | 实际断言与证据 |
|---|---|
| 图与顺序步骤跨进程恢复 | `TestRefundGraphRecoveryAcrossProcesses`、`TestRefundRecoveryAcrossProcesses`：业务账本已提交、检查点未保存时强制退出；新进程恢复，退款记录始终只有一条，重复调用返回同一回执。 |
| 审批与事实复核 | 拒绝错误审批键、未授权审批人、已过期审批和订单版本变化；失败时不产生退款。 |
| 累计额度与业务幂等 | 并发业务申请不能累计超额；同一 RequestID 的不同订单或金额被拒绝。 |
| Agent + 审批子图 + 持久预算 | 主模块 `TestNestedApprovalPausesAndResumesParentAgent`：审批前无退款，重复等待不重发模型；续跑后只有一次退款，两次模型请求，合成 Token 合计 7，完成后仅回放。 |
| 预算跨进程与工具回放 | 主模块 `TestBudgetSurvivesProcessExit`、`TestTokenAndToolLimitsRemainConservativeAcrossRestart`、`TestDispatcherRootToolBudgetDoesNotChargeReplay`：预留不会因重启丢失，工具重放不重复扣额，超限拒绝。 |
| 模型调用与用量保存间崩溃 | 主模块 `TestAgentUsageProcessExitAfterModelBeforeEvidence` 覆盖模型完成后、用量证据保存前的进程退出与恢复。 |

主模块证据均在 suite.log 中。本轮复用已有断言，没有为文档或构建目标增加实现镜像测试。SQLite 退款跨进程场景和 Agent 预算场景属于不同测试，不能据此声称单一生产业务已完成全链路验收。退款是本地事务模拟账本，未接入支付网关、生产认证或真实模型。

## 长任务预算开销

全部编译和回归任务结束后，串行运行三次矩阵，再运行固定账本微基准。三份矩阵共 27 个场景均完成指定操作；原始结果见 [matrix-1.json](matrix-1.json)、[matrix-2.json](matrix-2.json)、[matrix-3.json](matrix-3.json) 和 [budget-bench.log](budget-bench.log)。

```sh
go build -o /tmp/goagent-followup-probe ./evaluation/executionbench/cmd/run
for sample in 1 2 3; do
  /tmp/goagent-followup-probe -operations=10,100,1000 -modes=none,memory,file -timeout=5m > "matrix-${sample}.json" || exit 1
done
go test ./core/budget -run '^$' -bench '^BenchmarkRecordUsageFixedLedger$' -benchmem -benchtime=200ms -count=3
```

下表为三次运行各指标的中位数；MB 为十进制累计分配量，不是常驻或峰值内存。P95 列为三次运行各自 P95 的中位数，不是合并样本的 P95。

| 操作数 | 模式 | 总耗时 ms | 累计分配 MB | P95 µs |
|---|---|---:|---:|---:|
| 10 | none | 0.177 | 0.059 | 101.011 |
| 10 | memory | 0.220 | 0.100 | 69.150 |
| 10 | file | 2.428 | 0.452 | 284.510 |
| 100 | none | 0.811 | 0.354 | 13.453 |
| 100 | memory | 3.482 | 4.220 | 49.348 |
| 100 | file | 42.949 | 24.203 | 796.385 |
| 1000 | none | 5.961 | 3.408 | 11.192 |
| 1000 | memory | 129.589 | 390.301 | 367.988 |
| 1000 | file | 3472.453 | 2703.430 | 6739.769 |

固定账本的 `RecordUsage` 微基准中位数：10 条约 819 ns/op、2,248 B/op；100 条约 4,372 ns/op、16,584 B/op；1,000 条约 53,123 ns/op、262,384 B/op。

当前事务复制并检查账本，文件模式还加载、序列化并保存整份账本；本轮结果支持“账本规模会放大记账成本”的判断。1,000 次文件模式操作累计分配约 2.70 GB，需要在真实宿主存储与任务规模下确定是否超出服务目标，再评估增量账本或批量提交。没有修改持久化语义，也没有用不同机器上的历史数字声称性能提升或回退。

这是共享开发机、临时目录上的离线合成探针，存在后台负载和文件系统差异。计时不含初始化与清理，不含真实模型、业务服务和支付；不能推断生产吞吐、模型质量、峰值内存或实际费用。生产验收仍需真实宿主入口、测试环境和验收标准。

## 资料保留

suite.log 中本仓库绝对路径前缀替换为 `<repo>`；矩阵的 hostname 替换为 `<host>`，其余原始结果保留。文件摘要见 [checksums.json](checksums.json)。
