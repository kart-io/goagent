# 受限子调查：按方向授权单一工具，结论引用子调查证据

```text
告警 ──▶ 主调查 Agent ──delegate_lookup{focus:metrics}──▶ 子调查（只有 metrics_query）──┐
                     ├─delegate_lookup{focus:logs}─────▶ 子调查（只有 logs_query）─────┼─▶ 共享账本（ref 如 E3fa29c）
                     ├─delegate_lookup{focus:traces}───▶ 子调查（只有 traces_query）───┘   （同一轮的子调查并发执行）
                     └─submit_conclusion──▶ 宿主按共享账本核对证据 + operation 配套检查
                                            ├─不通过──▶ 交还主模型修复（最多 2 次）
                                            └─通过──▶ 渲染“结论：<downstream>”直接作为最终回答
```

- 合成场景：`order-svc` 下单 p99 从 180ms 升到约 1.2s。指标显示调用 `inventory-svc` 的耗时上升，日志里有 `inventory-svc.reserve` 超时重试，链路里慢 span 集中在 `reserve` 的锁等待。干扰项是 `payment-svc.authorize` 的轻微变慢。按数据应得的结论是 `inventory-svc` / `reserve`。
- `delegate_lookup` 由 `tools.NewRunnableToolFactory` 创建，参数为 `{focus, question}`。每次调用新建一个子会话：唯一 `SessionID`、独立 Dispatcher、`WithMaxIterations(4)`，工具集只有 `focus` 对应的那一个数据工具。子调查整体在主 Dispatcher 的一次调用里运行，超时用 `RunnableToolConfig.Timeout` 声明（5 个请求超时），不受 Dispatcher 默认 30 秒限制。
  - factory 只拿到 ctx，拿不到解码后的参数，所以按 `focus` 收窄工具集放在 `OwnedRunnable.Runnable` 的执行入口里完成。
  - 子调查交给主 Agent 的只有 `Encode` 的结果 `{focus, summary, refs, evidence}`，原始工具结果不进入主上下文。子调查在回答里按“证据：<ref>｜<原样片段>”列出摘录，宿主逐条核对：只保留本会话产生的 ref、且摘录是该结果中按原顺序连续、首尾不截断的一段的条目，编造或截断（如截掉数字末位）的直接丢弃。主 Agent 从 `evidence` 照抄 ref 与摘录提交结论。
  - 最初的版本只给主 Agent 摘要和引用号，主 Agent 看不到原文却被要求原样摘录：DeepSeek 每次都要修复 1 次，Gemini 两轮都因核验失败而中止。现在的版本改为把宿主核对过的摘录随报告交给主 Agent。
- 三个数据工具经同一个 `opscase.Ledger` 包装，父子共用一本账。引用号由各子会话工具调用的执行键派生，三个子调查并发执行也互不撞号、与完成顺序无关。主 Agent 在 `submit_conclusion` 里引用子调查内部产生的 ref 时，宿主仍能逐字核对摘录。核对只证明摘录出自该次结果，不证明摘录支撑结论。
- `submit_conclusion` 另有 `Check`：`operation` 必须是 `downstream` 的操作，`inventory-svc` / `authorize` 这类不配套的组合交还主模型改正。核对不通过作为 `tools.ArgumentError` 交还主模型，第 3 次仍不通过就中止。提交与有界修复由 `opscase.Submitter` 实现。
- 结论绑定：`submit_conclusion` 以 `WithReturnDirectTools` 注册，通过核验即由结论渲染最终回答并结束运行；`Decision` 取自它的 `downstream`。模型没有提交通过核验的结论就作答则报错。主 Agent `MaxIterations` 为 7（不再为总结预留一轮），主、子 Agent 都开启 `WithEmptyAnswerRetries(1)`。
- 禁止嵌套：`delegate_lookup` 设置 `MaxDepth: 1`。子会话里若再出现委派，会在新建子会话之前以 `tools.ErrNestedExecutionLimit` 被拒，整条链快速失败。
- 父子全部模型请求计入同一份持久根预算（额度 30）。同目录再次运行只回放：0 次请求，0 个新子会话。

## 框架行为说明

- **未授予的工具**：子 Agent 的工具集里根本没有其他方向的工具。模型仍然调用时，持久模式下 Builder 在提交回合前就以“模型请求了未提供的工具”拒绝。这是致命错误，不交还模型改正，该子会话中止并清理；同一轮的其他子调查不被取消，照常跑完后整条链再以该错误中止。
- **并行**：同一轮的多个 `delegate_lookup` 默认并发执行，峰值受主 Dispatcher 的 `MaxConcurrency: 3` 限制。账本编号、`Result.Lookups` 的排列（按 metrics、logs、traces）都与完成顺序无关。
- **内层参数错误**：子 Dispatcher 开启参数错误交还时，子调查传错参数由子模型自己改正。若子 Dispatcher 不交还，`*tools.ArgumentError` 穿出子会话后，主 Dispatcher 按 95bc8c6 的屏障把它当作工具执行失败而中止，不会要求主模型改正它没有传过的参数；返回的仍是原始错误值。

```sh
GOAGENT_LIVE_PROVIDER=deepseek go run ./examples/orchestration/ops/scoped-lookup -dir /tmp/scoped-lookup-1
```

| 测试 | 断言 |
| --- | --- |
| `TestScriptedScopedLookup` | 同一轮派出三个方向，脚本模型用屏障要求三个子调查同时在执行，并发峰值为 3；每个子会话只调用自己的工具并各产生一个 ref；主 Agent 只看到摘要、引用号与核对通过的摘录，子调查截断或编造的摘录被丢弃；结论引用子调查内部的 ref 通过核验，最终回答由结论渲染；共 8 次请求，与预算预留数相等；换目录重跑引用号与主 Agent 看到的内容完全相同；回放 0 次请求、0 个子会话，结论由回放恢复且与首次一致 |
| `TestScriptedAnswerBoundToConclusion` | 不提交结论就直接作答报“没有提交通过核验的结论” |
| `TestScriptedPairCheck` | 提交 `inventory-svc` / `authorize` 被拒并交还一次，提示可选操作；改正后通过 |
| `TestScriptedRecoverAfterLookups` | 子进程在三个 `delegate_lookup` 写入主日志后退出；同目录恢复时子调查从日志回放、不新建子会话，账本由报告中核对过的摘录补登，提交通过核验，只请求 1 次 |
| `TestScriptedUnofferedTool` | metrics 子调查调用未授予的 `logs_query`：回合被拒，该工具没有执行，错误不交给模型；logs、traces 子调查照常跑完，整条链随后中止，子会话创建 = 清理 = 3，共 6 次请求 |
| `TestScriptedNestedRejected` | 子 Agent 被误挂上 `delegate_lookup`：第 2 层委派返回 `tools.ErrNestedExecutionLimit`，没有新建第 2 层子会话 |
| `TestScriptedRepair` | 非法方向、不存在的引用各交还主模型一次；参数不合法时不创建子会话；改正后通过，共 10 次请求 |
| `TestScriptedRepairLimit` | 连续提交编造的摘录：交还 2 次后第 3 次中止，没有被接受的结论 |
| `TestScriptedInnerArgumentErrorBarrier` | traces 子调查传错操作：子 Dispatcher 交还时由子模型改正；不交还时整条链中止，主模型没有收到 `invalid_arguments` |
| `TestLiveScopedLookup` | 真实模型得出 `inventory-svc` / `reserve`，至少 2 个子调查，子会话创建 = 清理，每个子会话只调用授权工具，结论通过核验，请求数等于预算预留，回放 0 次请求 |

回放说明：回放时 `delegate_lookup` 与 `submit_conclusion` 都由主 Dispatcher 的日志直接返回，不会重新创建子会话，也不会重新核验；子会话记录（`Lookups`）只覆盖本进程实际执行的调用。主 Dispatcher 的 `OnResult` 在回放时把各报告中核对过的摘录补登到账本，并从提交调用的参数恢复 `Accepted`，宿主不另行落盘。所以主 Agent 提交前崩溃也能恢复，只是恢复后的核对依据是报告里的摘录，而不是子调查的完整结果。子调查进行中崩溃不在此列：组合工具只允许一次尝试，恢复时需要宿主处理。
