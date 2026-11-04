# 项目稳定化与关联模块验证

> 后续更新：此前关联模块失败已修复，14 模块完整竞态验证全部通过，见[修复报告](REPAIR.md)。下文保留首次稳定化检查结果和当时的阻塞记录。

日期：2026-11-04。基线 `020028a`（本地标签 `v0.47.0`）加本次未提交改动。主仓库测试、覆盖率门禁、vet、lint 和导入边界通过；关联生态仍存在迁移阻塞，不能据此宣布整体可发布。没有修改关联仓库、调用真实模型或访问生产业务服务。

## 已落实的改动

- Anthropic 默认模型测试清空 `ANTHROPIC_MODEL`，开发者的模型配置不再改变断言结果。复现时显式设置 `host-override`：修复前失败，修复后通过。
- 模拟 Docker 的 Unix 测试移除外部 `setsid`。后台创建进程已重定向输入输出，模拟客户端被结束时不需要另建会话；原超时、清理、启动失败和镜像拉取用例在 macOS 下通过。本轮没有改动生产 Docker 执行器，也没有访问 Docker 守护进程。
- `scripts/coveragecheck` 使用 Go 标准库读取 profile，按语句数加权，重复覆盖块合并。缺失数据、语句数冲突、非法输入、报告写入错误均失败。
- 新增精确包级门禁：`core/durable` ≥70%，`core/budget`、`workflow`、`builder` 各 ≥75%。子包不混入包级门禁；整体统计仍包含所有非示例包。保留整体 45% 与三个 Provider 文件阈值，单文件也改为语句加权。
- `make coverage` 自动执行门禁；脚本默认读取 `coverage/coverage.out`，CI 显式传入 `coverage.out`。保留过滤后的 `.lib.out` 供现有报告消费，脚本不再依赖 Bash 关联数组或 bc。同步开发指南、导入层级、Eino 对照中的旧 GoAgent API，以及 PR 的 Go 缓存版本。

## 本地验证

环境：Go 1.27.2，darwin/arm64。本机 PATH 中的 Go 与导出的 GOROOT 指向不同版本，验证仅对子进程使用 `env -u GOROOT`；没有更改用户的全局环境配置。仓库声明最低 Go 1.26，本轮未另行在该最低版本运行。

| 检查 | 结果 |
|---|---|
| `ANTHROPIC_MODEL=host-override make coverage` | 54 个有测试包通过；最终一轮沿用部分包的有效缓存，标记见 checks.txt |
| `go vet ./...` | 通过 |
| `bash scripts/verify_imports.sh` | 0 findings |
| golangci-lint v2.15.0 `run ./...` | 0 issues |
| `evaluation/frameworkbench` 独立模块竞态测试 | 通过；仅运行契约测试，未运行跨框架性能对测 |
| 覆盖率检查器回归 | 加权、示例排除、精确包匹配、重复块、非法数据、写入失败及七项真实门禁缺失/零覆盖拒绝均通过 |
| macOS 系统 `/bin/bash` 执行包装器 | 合成 profile 全部门禁通过 |
| `git diff --check` | 通过 |

最终覆盖率见 [checks.txt](checks.txt)：整体 **79.3%**，durable **84.4%**，budget **88.4%**，workflow **84.4%**，builder **90.7%**；OpenAI、Gemini、common/base 文件分别 88.3%、89.4%、90.1%。测试行数不作为覆盖率证据。

本机原有 golangci-lint 为 v1，不能读取仓库 v2 配置；本轮将仓库指定的 v2.15.0 安装到临时目录再验证，没有替换用户原工具。没有运行或查询远程 CI。

复现主仓库检查：

```sh
env -u GOROOT ANTHROPIC_MODEL=host-override make coverage
env -u GOROOT go vet ./...
env -u GOROOT bash scripts/verify_imports.sh
golangci-lint run ./...  # 使用仓库指定的 v2.15.0
```

正确配置工具链的机器无需取消 GOROOT。真实门禁的回归可单独运行 `go test -race -count=1 ./scripts/coveragecheck`。

## 关联模块兼容性及迁移顺序

使用本机三个关联仓库的当前工作树，14 个模块各以临时 modfile 将 GoAgent 和其他本地关联模块替换为当前源码，设置 `GOWORK=off`、`GO_TEST_MODE=true`，运行 `go test -short -count=1 -timeout=120s ./...`。因此没有修改关联仓库的 go.mod、go.sum 或工作区配置，也不代表它们已发布版本的兼容性。逐模块结果、通过包与错误见 [compatibility.json](compatibility.json)。

**4 个模块通过，10 个未通过。** 通过模块是 `contrib/acp`、`contrib/tools/database`、`contrib/tools/practical` 和 examples 下独立的 `orchestration/httpagent`。其余阻塞如下：

| 模块 | 证据及下一步 |
|---|---|
| goagent-harness | `preset/compaction.go:128` 仍调用 `Client.Chat`，应迁至 `llm.Chat(ctx, client, messages)`；`effects.TestTargetOrderHoldsUntilRepeatedFailure` 在两轮检查中均得到 running 而非 waiting，须独立排查，不能当作接口迁移附带忽略 |
| contrib/retrieval | reranker、multi_query、rag 仍调用同步方法，迁至 `llm.Chat` / `llm.Complete`；测试客户端补齐 `CompleteStream`，验证无错误 Done 及提前关闭流的失败语义 |
| contrib/document、document/pdf、retrieval/cohere、qdrant、vertexai | 被上述 retrieval 编译失败阻塞，应先迁移 retrieval，再逐模块复验 |
| contrib/httpagent | `binding_test.go:156` 仍调用已删除的 Agent.Execute，迁至 `AsRunnable().Invoke` 后复验输出与工具执行语义 |
| contrib/durable/pgstore | Store、scope 缺少 `SaveAll`，SessionStore 缺少 `AppendAll`；必须按现有契约实现数据库事务原子性及序号冲突检查，再通过 storetest/sessiontest。不能用循环单写冒充原子批量写 |
| goagent-examples | business-cases 的 completionClient 仍要求旧 Complete 方法，Gemini/DeepSeek Provider 不再满足；迁移该适配层及测试模型后复验 |

建议的发布约束：本轮不再改公共运行时 API；下一次联合升级先完成上述迁移和完整复验，再发布关联模块版本并更新依赖锁定。此处是明确的后续迁移清单，本轮没有为兼容旧调用恢复已删除接口，也未实现关联仓库的迁移。涉及数据库的变更需要专用测试库验证，当前没有把编译检查当作事务验收。

## 宿主与业务验证边界

- 当前 GoAgent 的 `workflow/refund`、`execution`、`evaluation/agentbench` 针对退款守卫、提交失败恢复、Scope 日志/预算绑定、租户隔离及答案/调用契约的离线竞态测试通过。
- 关联 examples 的 `workflow/refund-recovery` 包通过，包括独立进程恢复、拒绝无副作用、并发退款额度守卫、幂等和审批权限用例。它使用合成订单及本地账本，未调用真实支付接口。
- 将历史 HITL 的 results.json 复制到临时目录后运行 `scripts/hitl_live.py --review`，预期退出码 **1**：Gemini 的退款完成声明仍不符合事实。复核副本见 [hitl-review.json](hitl-review.json)，[原报告](../2026-11-04-hitl-live/REPORT.md)和原始数据均未修改；没有新增模型请求。
- 实际宿主的订单/支付服务、认证、数据库事务、共享存储接管、生产负载和告警尚未验证。生产试运行应在上述跨仓库迁移完成后，绑定具体宿主及验收数据执行；本轮离线结果不能替代它。

## 长任务合成开销

[matrix.json](matrix.json) 是当前代码的单次离线矩阵：10、100、1000 次操作 × 无预算、内存预算、文件预算。只调用无副作用工具和固定合成 usage，不调用模型。最终采样期间没有并发运行测试、编译或其他性能探针；初始化、最终统计与清理不计入操作循环。

```sh
env -u GOROOT go run ./evaluation/executionbench/cmd/run \
  -operations=10,100,1000 -modes=none,memory,file -timeout=5m > matrix.json
```

单次数据不用于声称相对历史基线的性能提升；文件存储同步开销及增长账本应在实际宿主存储和任务规模下复验。GC 后堆与 goroutine 是进程采样，不是峰值或无泄漏证明。

全部 9 个案例完成指定操作，结果如下。单位为总耗时 ms、P95 µs、累计分配 MB（十进制）；均为单次合成样本。

| 操作数 | 模式 | 总耗时 ms | P95 µs | 分配 MB | goroutine 差值 |
|---|---|---|---|---|---|
| 10 | none | 0.119 | 79.625 | 0.053 | 0 |
| 10 | memory | 0.114 | 44.208 | 0.094 | 0 |
| 10 | file | 267.835 | 30733.292 | 0.464 | 0 |
| 100 | none | 0.401 | 5.625 | 0.347 | 0 |
| 100 | memory | 2.011 | 27.250 | 4.212 | 0 |
| 100 | file | 2816.409 | 30090.459 | 23.987 | 0 |
| 1000 | none | 5.811 | 6.542 | 3.439 | 0 |
| 1000 | memory | 101.715 | 314.959 | 388.908 | 0 |
| 1000 | file | 31341.680 | 35898.625 | 2685.104 | 0 |
