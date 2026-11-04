# 关联模块失败修复与复验

> 后续真实 Agent 验证已执行：DeepSeek/Gemini 业务及原生循环、Harness 压缩、重排/入口绑定和 PostgreSQL 路径通过；MiniMax 首次国际端点返回 401，按用户指定国内端点重测后 6/6 通过。见[真实验证报告](../2026-11-04-live-agent-validation/REPORT.md)。

日期：2026-11-04。用户要求修复此前联合验证失败。本轮已修复三个关联仓库的源码和依赖，**14 个关联模块的完整竞态测试全部通过**；没有使用 `-short` 跳过原失败用例。Postgres 批量写另经真实数据库完整竞态验证。原始失败结果保留在 [compatibility.json](compatibility.json)，修复后结果见 [compatibility-fixed.json](compatibility-fixed.json)。

三个关联仓库均在 `fix/goagent-compatibility` 分支，改动尚未提交或发布；主仓库此前稳定化改动和用户的沙箱研究资料均保留。

## 修复内容

| 范围 | 根因与修复 |
|---|---|
| contrib/retrieval 与下游模块 | 将旧 `Client.Chat/Complete` 调用改为 `llm.Chat/Complete`；测试模型实现真正的 CompleteStream。四类消费者验证完整分片收集、请求参数、请求错误、终态错误、缺少 Done 和取消，失败流不产生有效输出。document、pdf、cohere、qdrant、vertexai 随依赖修复恢复通过。新版 lint 检出的三处多余格式化中间字符串也已清理，针对包重新测试通过 |
| contrib/httpagent | 删除已失效的 Execute 对照。两个现行入口分别构造独立模型、Agent 和工具计数器，验证 `AsRunnable().Invoke` 与原生流均恰好执行一次工具并得到相同答案 |
| examples/business-cases | 业务决策直接使用 `llm.Client` 和 `llm.Complete`；删除 MiniMax 示例中不再使用的同步接口。新增分片收集、终态用量、完整 JSON 但缺 Done、终态错误的业务回归；失败时不发送回复，模型调用预算仍只记一次 |
| Harness | compaction 与 plan-agent 摘要迁移到现行流式接口。修复真实过期竞态：在锁定更新中重新读取记录并复核期限，只有状态提交成功才取消子任务，避免旧的过期扫描覆盖新 TurnStarted 已清除的期限。原等待/退避测试改用可控时钟，仍检查顺序、失败次数、下一次时间及退避上限 |
| Postgres | Store、scoped Store 实现 SaveAll，SessionStore 实现 AppendAll。整批使用同一数据库事务，预先验证、固定顺序加锁并保留逐 ID 租约 fencing；序号或租约冲突、SQL 失败整批回滚；空事件批次只比较，不创建缺失会话。无数据库 schema 变更 |
| 依赖和入口 | 全部 14 个模块锁定已发布 GoAgent v0.47.0，最低 Go 1.26，并 tidy 相应依赖。contrib 的 test-all、lint-all、CI 使用临时工作区引用同仓库当前模块；CI 补齐 httpagent。未把绝对 replace 写进发布模块，未改变用户已有工作区的模块列表。examples 的 work 入口及现有本地工作区版本同步为 Go 1.26 |

## 验证结果

环境为 Go 1.27.2、darwin/arm64；仅对子进程使用 `env -u GOROOT` 绕过此前确认的 PATH/GOROOT 冲突。本轮未在最低 Go 1.26 的独立工具链重跑。

| 检查 | 结果 |
|---|---|
| 14 模块联合源码：`go test -race -count=1 -timeout=120s ./...` | 全部退出 0；临时 modfile 选择本地核心及关联模块，逐包结果见 compatibility-fixed.json |
| contrib 正式测试入口 `bash scripts/test-all.sh -short -timeout=120s` | 11 个模块的 vet 和 race 测试均通过，使用已发布 v0.47.0 核心加本地 contrib 模块；需要外部服务的用例保持原条件跳过 |
| contrib 正式 lint 入口 | 11 个模块均为 0 issues，golangci-lint v2.15.0 |
| Harness 发布版依赖、GOWORK=off | 完整离线 race、vet、lint 均通过；不是只靠本地核心 replace 才能通过 |
| Harness 原失败时序组 | 四项到期/排序/退避用例 race 重跑 40 轮通过；流式摘要另有 40 轮重复验证。精确过期竞争用例已在修复前变红 |
| examples 发布版依赖、GOWORK=off | 完整 race 和 vet 通过；修改的 business-cases 包 lint 为 0 issues |
| Postgres 完整实库 race | **76 PASS（含子测试）、0 FAIL、0 SKIP**，27.489s；覆盖一致性、重启、进程接管、同步提交和新增批量事务用例 |
| 主仓库 | make coverage、vet、导入层级复验通过，54 个有测试包通过，原覆盖率门禁保持通过 |
| 独立审查 | 无确认缺陷或阻断项；额外复验了 Harness 流/过期/排序测试、工作区模块选择、shell 语法与三仓 diff |

Postgres 使用本机已有 postgres:16-alpine 镜像新建专用临时容器，仅绑定 localhost；测试结束已删除，未访问生产或用户数据库。实库完整日志见 [pgstore-race.txt](pgstore-race.txt)。新增 SQL 失败回滚回归还做了临时 Go overlay 变异验证：故意将批量实现退化为逐项事务时，两个回滚用例都按预期失败。这是回归有效性的负例，不是尚未修复的失败。

## 复现入口

```sh
# contrib：使用发布核心及本地全部 contrib 模块
cd ../goagent-contrib
env -u GOROOT GO_TEST_MODE=true bash scripts/test-all.sh -timeout=120s
env -u GOROOT bash scripts/lint-all.sh

# 需要联调未发布核心改动时，可显式指定已有 checkout
GOAGENT_CORE_ROOT=../goagent bash scripts/test-all.sh -timeout=120s

# Harness 和 examples 可直接验证发布核心，不必使用本地 replace
cd ../goagent-harness
env -u GOROOT GOWORK=off GO_TEST_MODE=true go test -race -count=1 -timeout=120s ./...
cd ../goagent-examples
env -u GOROOT GOWORK=off GO_TEST_MODE=true go test -race -count=1 -timeout=120s ./...
```

正确配置工具链的机器无需取消 GOROOT。lint 要使用 v2.15.0。Postgres 实库回归可使用 contrib 的 `scripts/test-pgstore.sh` 创建独立容器，或为该模块提供专用测试库 DSN；重启测试必须使用可独占的临时数据库实例。

本地工作区验证与发布验收有不同范围：contrib 内部的 retrieval 修改尚未发布，document 等模块的旧发布依赖不会自动获得这些源码修复。同仓库测试使用当前源码；后续联合发布时须按依赖顺序发布 retrieval 等模块并更新下游版本锁定。本轮没有推送、打标签、部署或调用真实模型。

历史 HITL 的 Gemini 退款完成声明仍是应被拒绝的已归档负例，不修改原始答案或评分将它变绿。当前修复与新流式业务回归均未以该错误声明作为成功结果。生产业务服务和可选的 Harness 平台/实机 Docker 测试未纳入本轮验收。
