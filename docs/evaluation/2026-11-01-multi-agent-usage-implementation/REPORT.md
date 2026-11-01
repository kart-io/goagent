# 多 Agent 身份、用量钩子与归属交付

本报告记录首次实现快照；用户后续要求统一处理，当前实现不再维护双格式或 AttributionVersion。最新验证见[统一计量报告](../2026-11-01-unified-usage/REPORT.md)。

日期：2026-11-01。基线 `3621579`。分支 `codex/pi-core-contracts-20261101`。Go 1.25.0，darwin/arm64。

## 已实现

| 设计阶段 | 交付 |
| --- | --- |
| U1 | core/usage Owner/Invocation、WithAgentID、父子 context 链、统一根身份、零模型中间节点、观察元数据副本隔离 |
| U2 | model_attempt_start/model_usage、独立尝试键、失败/取消/回放证据、字段 mask/缓存、OpenAI/DeepSeek 标准流与 Gemini 转换 |
| U3 | 同预算账本的可选 v2 版本、原子预留与归属、证据单调合并、Self/Inclusive/角色/Root 查询、字段未知计数、持久恢复与溢出检查 |
| U4 | 接入指南与通用离线 examples/agent-usage：两个并行子 Agent、失败重试、根报表与回放；不把业务场景加入核心 |

保持单一执行源、Observer 和现有预算 Store。没有新增运行时依赖、计费系统、后台调度器或供应商定价表。普通 v1 账本、缺省序列化模型意图兼容；v2 显式启用且旧代码拒绝读取，不就地升级活跃旧运行。

## 验证结果

所有 Go 命令前缀 `env -u GOROOT GOTOOLCHAIN=go1.25.0`。

| 命令 | 结果 | 证据 |
| --- | --- | --- |
| `go test ./...` | 根模块通过，未变更包可能使用缓存 | [suite.log](suite.log) |
| `go test -race ./builder ./core/usage ./core/budget ./core/observe ./tools ./execution ./llm` | 通过 | [race.log](race.log) |
| `go test -race ./builder -run '^TestAgentUsage\|^TestMultiAgentUsage' -count=5` | 五轮通过，包含新并发与子进程回归 | [stress.log](stress.log) |
| `go test ./builder -run '^TestAgentUsageProcessExit' -count=1` | 保存模型结果后、登记证据前真正子进程退出，重启补登记且不重发请求 | [process.log](process.log) |
| Gemini 错误/取消/partial 协议 race 重复20轮 | 通过 | [gemini-race.log](gemini-race.log) |
| `go run ./examples/agent-usage` | 首次与回放均 root=18、请求5、unknown=1；父自身9、子4/5、父子树18 | [example.log](example.log) |
| v1/v2 固定账本基准 | 已运行，增加归属有实际内存和 CPU 成本 | [benchmark.log](benchmark.log) |
| 格式、文档链接、diff、独立审核修复 | 通过 | [verification.log](verification.log) |

现有多 Agent 回归进一步覆盖内存/持久与 v1/v2 四种组合：根18、父自身9，回放后账本不变。Owner 改变被拒绝；同名角色并发有独立调用键与 Visit。拒绝准入不发实际尝试事件。Observer 的篡改、panic 和异步队列隔离不改变账本。

## 独立审核与 RED → GREEN

独立只读审核发现的重要问题均已处理：

1. **重试 panic 复用上次用量**：旧路径第二次尝试 panic 时保留第一次 lastTurn，根错误记6而非3。回归先失败，再在每次尝试前清空证据，未知尝试仍为 unknown。见[RED](red-panic.log)。
2. **v1 partial 被误称可靠提交**：v1 不保存 mask，部分报告曾变成完整零值。修复为保守 unknown，partial 通知 AccountingCommitted=false；可靠字段用 v2。见[RED](red-v1-partial.log)。
3. **未知 Total 被算作零消费**：v2 的 Snapshot 与 Attribution 仅在 Total 字段已知时算差额；prompt-only、估计100不会虚构节省100，补 Total=25 才得到-75。ledger 回归先RED后GREEN。
4. **Gemini 失败/断流/取消丢用量**：保留已解析 UsageMetadata 和跨帧字段。MAX_TOKENS、SAFETY、blocked、usage后断流、取消、无usage失败均有协议测试。见[RED](red-gemini.log)。
5. **跨帧 partial 覆盖缓存**：Generic Reader 与 Gemini 都按字段合并累计报告，不重复相加；缺失字段不覆盖已知缓存。见[Reader RED](red-frames.log)、[Gemini RED](red-gemini-frames.log)。

另加非法负数用量回归：拒绝无效证据但仍发出不含非法 Token 的 usage_invalid 观察终态，根账本保留 unknown，见[RED](red-invalid.log)。

新 Builder API 的 RED 编译证据见[red-builder.log](red-builder.log)。归档失败日志仅展开制表符、清理行尾空白，原始 SHA256 另存。新增 API 已实施，不再是前轮设计提案。

## 性能与限制

本机20次基准样本：v2 1000条约170µs、350KB/次，10000条约1.65ms、2.86MB/次；v1 1000条约49µs、87KB/次。事务继续克隆并验证账本，规模增大有线性开销；不是对生产性能或供应商费用的承诺。请求数和 MaxAgentCalls 均应按运行有界设置。

SDK scalar 不保留“字段缺失/显式零”区别时保守标 unknown；宿主可用显式 mask 表示已知零。DeepSeek 私有缓存字段未被 SDK 暴露的部分不推断。Complete 仅代表基本三字段完整，UnknownCachedEntries 单独保留。

Observer 可以丢事件，统计投影需查询账本修复；它不是 exactly-once 消息总线。Token 不是货币费用。真实供应商账单、隐藏 SDK 请求、供应商内部重试和跨主机计费服务没有在本轮验收。后台任务与租户业务治理仍由宿主承担。

当前接入见[使用指南](../../guides/MULTI_AGENT_USAGE.md)，计划状态见[实施计划](../../superpowers/plans/2026-11-01-multi-agent-hooks-usage.md)。
