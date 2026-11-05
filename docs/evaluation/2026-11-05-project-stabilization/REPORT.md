# 项目稳定化验收（2026-11-05）

## 范围与环境

- Go 1.26.10，darwin/arm64，race 检测开启；golangci-lint v2.15.0。
- 跨仓核心基线 `bd6e6a0741c8af4a3ab8724da8fdfe1df7ee6f9d`，通过临时 modfile 绑定当前源码，`GOWORK=off`；不改下游依赖文件。
- Contrib：`e0c75339183908b57118594776da781a398e7b5b`。
- Examples：`b900dfee8e1ca78666e38c25a85ce20b66d7fed7`。
- Harness：`2368ac069cd49c0e865b27864fe2dee6cb202492`。
- 数据库为本次独立创建、仅映射到本地回环地址的 PostgreSQL 16 临时容器；退款效果账本为案例自有临时 SQLite。没有接入真实登录、支付或订单服务，没有调用付费模型。

## 结果

| 验证 | 结果 | 证据 |
|---|---|---|
| 原冲突 GOROOT 下构建与 vet | 通过 | `make build vet` 使用 go.mod 工具链 |
| 独立 frameworkbench 只读依赖与竞态契约 | 通过 | `make test-frameworkbench` |
| v2 lint、导入分层 | 通过 | 0 issues、0 findings |
| 默认环境跨仓完整竞态测试 | 15/15 模块命令通过，1541 个测试事件通过，124 个跳过 | 新 `compatcheck` 入口的首次实际运行 |
| 配置 PostgreSQL 后跨仓完整竞态测试 | 15/15 模块命令通过，1620 个测试事件通过，101 个跳过 | [compatibility.json](compatibility.json) |
| PostgreSQL 重启、同步提交专项首次执行 | 2 个测试失败 | 随机端口在容器重启后由 32774 变为 32775，旧 DSN 连接拒绝；保留失败事件 |
| 固定端口后专项重新执行 | 2 个测试通过 | `TestPrimaryRestart`、`TestSyncCommit`；覆盖上一轮 101 个跳过中的 2 项 |

测试事件数包含子测试和测试入口，不能解释为独立业务样本数或模型成功率。专项复测是独立运行，不覆盖初次失败记录。其余 99 个跳过事件主要属于未配置的外部服务、真实模型及辅助子进程入口；不能据此宣称所有外部集成都通过。

## 业务恢复场景

以下结果均来自当前源码绑定下的既有测试，并保留在 [outcomes.json](outcomes.json) 中：

| 场景 | 实际验证 |
|---|---|
| 退款审批与副作用之间进程退出 | `TestRefundRecoveryAcrossProcesses`、`TestRefundGraphRecoveryAcrossProcesses` 通过；恢复后仍为同一业务回执、只有一笔退款 |
| 无授权、无效订单、过期或事实变更 | `TestRefundBusinessGuards`、`TestApprovalRejectsExpiredChangedAndUnauthorizedRequests`、`TestRefundGraphRechecksReviewedOrder`、`TestRejectedRefundHasNoEffect` 通过 |
| 重复业务 ID、改参数、并发累计额度 | `TestLedgerDeduplicatesAndRejectsChangedPayload`、`TestConcurrentRequestsCannotOverRefund` 通过 |
| 旧持久格式 | `TestLegacyLedgerIsNotSilentlyReinitialized` 通过，旧账本被明确拒绝 |
| 真实 Postgres 存储契约与崩溃接管 | `TestConformance`、`TestProcessCrashTakeover` 等通过；该模块主矩阵有 77 个通过事件、2 个待专项验证的跳过事件 |
| 跨连接等待恢复、旧持有者失效与 Inbox 去重 | Harness Postgres 的 `TestWaitRestartResumeRelease`、`TestTakeoverFencesOldHolder`、`TestInboxAppendOnceAcrossConnections` 均通过，无跳过 |
| 数据库重启、同步提交 | 固定宿主端口后 `TestPrimaryRestart`、`TestSyncCommit` 均通过 |

退款中的审核人仍是演示授权规则，回执仍是本地效果账本记录；Postgres 实库测试验证存储与执行恢复。它们都不能代替真实业务的身份认证、租户授权或支付方回执对账。

## 复现与证据处理

```sh
make test-frameworkbench
make compatibility COMPAT_OUT=/tmp/goagent-compatibility-new
# 在独立测试环境中配置 GOAGENT_PGSTORE_DSN，重启专项另配 GOAGENT_PGSTORE_CONTAINER。
make compatibility COMPAT_OUT=/tmp/goagent-business-new
```

重启验证须固定端口。首次失败是测试容器部署配置问题，修正端口后复用相同测试与源码验证，没有修改生产实现。

原始日志位于本次运行的 `/tmp/goagent-stabilization-compatibility` 和 `/tmp/goagent-stabilization-business`。仓库中保存结构化摘要、关键场景的原始事件字段及日志 SHA-256；本机父目录归一化为 `${WORKSPACE_ROOT}`，不保存 DSN、密码或完整测试输出。临时数据库验证完毕后删除。复现要求、外部业务输入和逐场景验收见 [业务接入与故障恢复验收](../../development/BUSINESS_ACCEPTANCE.md)。

## 尚未验证

尚未收到实际业务的测试服务地址、认证方式及测试数据范围，因此未执行真实登录身份、真实订单/支付接口、生产负载、远程支付对账与业务版本升级验收。该部分需要上述输入后继续；本报告只确认本地退款案例和实际 PostgreSQL 的恢复契约。
