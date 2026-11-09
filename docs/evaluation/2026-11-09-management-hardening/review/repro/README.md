# 独立复核证据

这些文件是审查反例与运行输出，未写入生产源码或现有测试。原始审查文件保存在 `scope.json` 指定的快照上。

## CORE-01：普通子 Agent 写入父会话

`nested_test.go.txt` 保存审查 agent 编写的三个反例，使用 `.txt` 后缀以免进入正常包发现；`base_events.go.txt` 是从核心基线 `f2e0b0b449fcfda416169356291689248e5f455b` 提取的原始 `builder/events.go`。

主代理在仓库根独立执行：

```sh
env -u GOROOT GO_TEST_MODE=true go test -race \
  -overlay=docs/evaluation/2026-11-09-management-hardening/review/repro/current-overlay.json \
  ./builder -run '^TestCoreReviewPlainChild' -count=1 -timeout=60s

env -u GOROOT GO_TEST_MODE=true go test -race \
  -overlay=docs/evaluation/2026-11-09-management-hardening/review/repro/baseline-overlay.json \
  ./builder -run '^TestCoreReviewPlainChild' -count=1 -timeout=60s
```

当前实现退出1、三个反例全部失败：[current.log](current.log)。仅替换基线 events.go 后退出0：[baseline.log](baseline.log)。两个 overlay 使用审查机器的绝对路径，复制到其他工作区时须调整路径。

## VR-01 / VR-02：门禁误放行

[gates/confirmed-results.json](gates/confirmed-results.json) 是主代理使用当前源码 `go run ./scripts/compatcheck -verify-postgres-report ...` 的独立复核输出。

| 目录 | 证据 | 实际结果 |
|---|---|---|
| `gates/valid` | 完整通过事件 | 正确通过 |
| `gates/child-skip-report` | 临时模块由真实 `go test -json -race` 产生的子测试跳过事件 | 错误通过 |
| `gates/package_fail` | summary通过，但包终结为失败 | 错误通过 |
| `gates/truncated_tail` | 事件流尾部JSON不完整 | 错误通过 |
| `gates/missing_package_end` | 没有包级结束事件 | 错误通过 |

例如在仓库根执行：

```sh
env -u GOROOT go run ./scripts/compatcheck -verify-postgres-report \
  docs/evaluation/2026-11-09-management-hardening/review/repro/gates/child-skip-report
```

`gates/probe.py` 与 `gates/child_skip.py` 保存 agent 的原始生成脚本。需要重新生成时，将两者复制到新的临时目录，将当前 `scripts/compatcheck` 与 `scripts/coveragecheck` 分别构建为该目录下的 `compatcheck`、`coveragecheck`，再运行脚本；避免在本证据目录重复运行并覆盖记录。占位 DSN/容器名没有被测试使用，这些反例没有访问数据库。

这些反例证明验证器边界有缺陷，不证明上一轮真实数据库验收实际发生了失败或跳过。主代理另行检查原始关键模块846条事件，未发现关键契约后代跳过/失败、相关包缺少成功结束或损坏JSON；详见 [original-evidence-check.json](../original-evidence-check.json)。

## PostgreSQL 既有问题

`review_tombstone_test.go.txt` 是主代理追加的真实事务反例。它在原 pgstore 模块的仓库外源码副本中编译，复用现有 `newSchema/newPool/waitLeaseBlocked` 测试辅助函数，临时 workspace 将核心依赖绑定到本轮源码。

```sh
# 仅独立临时数据库，在包含该反例的仓库外副本中执行：
go test -race -v -count=1 -run '^TestReviewSaveRejectsCommittedTombstoneAfterWait$' -timeout=45s ./...
```

当前与基线两次输出分别为 `postgres-tombstone-current.log`、`postgres-tombstone-baseline.log`。基线对照仅通过 overlay 替换 pgstore.go 为 `87696061ac858e64f6b3a717d11bf1f3749e3a57` 版本，测试及其余源码相同。两次均是 READ COMMITTED 对照通过，REPEATABLE READ 反例失败；测试连接串未归档。

`review_repro_test.go.txt` 与 `review_commit_test.go.txt` 保存 PostgreSQL agent 的离线协议反例。主代理以 `-race -count=5 -run '^TestReviewCancelledDuringAdvisoryWait$' -timeout=20s` 复跑，输出在 `postgres-cancel-reproduction.log`。该测试刻意以“观察到取消后成功返回”为命中条件，因此其 PASS 表示缺陷复现成功，不表示 Lock 取消契约通过。
