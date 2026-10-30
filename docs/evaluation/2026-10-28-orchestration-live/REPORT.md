# 可组合编排真实模型案例验证

日期：2026-10-28。分支：`codex/composable-orchestration-r2`，基于 `0963c04`。环境：Linux、Go 1.25.0。

模型：

- DeepSeek `deepseek-flash`：逐 token 流式，推理模型。
- MiniMax `MiniMax-M3.1-Flash`：国内端点，一次性返回，`reasoning_effort=low`。
- Gemini `gemini-2.5-flash`：原生 GenAI SDK，已关闭 SDK 重试。

所有业务数据均为合成数据，业务效果均在本地模拟。案例与运行方式见 [examples/orchestration/live](../../../examples/orchestration/live/README.md)。

## 最终结果

8 个案例共 9 个 Live 测试（流式摘要案例含两个测试）。每个模型跑两轮，第一轮带 `-race`，并包含离线与脚本化测试；Gemini 额外跑了第 3 轮。

| 案例 | DeepSeek | MiniMax | Gemini |
| --- | --- | --- | --- |
| 工单分诊 | 2/2 | 2/2 | 3/3 |
| 长日志排障（崩溃 → 恢复 → 回放） | 2/2 | 2/2 | 2/3 |
| 并行诊断与观测报告 | 2/2 | 2/2 | 3/3 |
| 流式摘要 | 2/2 | 2/2 | 3/3 |
| 预算争用 | 2/2 | 2/2 | 3/3 |
| 文稿审校循环（崩溃 → 恢复 → 回放） | 2/2 | 2/2 | 3/3 |
| 多级委派 | 2/2 | 2/2 | 3/3 |
| 人工审批暂停 | 2/2 | 2/2 | 3/3 |
| 本地化（崩溃 → 换术语表 → 恢复 → 回放） | 2/2 | 2/2 | 3/3 |

Gemini 第 1 轮的长日志排障有 1 次失败，原因是模型抄错引用键，已修复，见下文。其余全部通过。

原始日志：

- DeepSeek：[第 1 轮](live-deepseek.log)、[第 2 轮](live-deepseek-round2.log)
- MiniMax：[第 1 轮](live-minimax.log)、[第 2 轮](live-minimax-round2.log)
- Gemini：[第 1 轮](live-gemini.log)、[第 2 轮](live-gemini-round2.log)、[第 3 轮](live-gemini-round3.log)

日志已检查，不含任何 API Key。案例设计过程中的失败日志全部保留在 [design-iteration/](design-iteration/)。

## 关键数据

三个模型在编排层面的计数完全一致，差别只在 Token 和流式分块上。

| 案例 | 三个模型一致的结果 |
| --- | --- |
| 工单分诊 | A1001 批准，A1002、A1003 拒绝；每张工单 4 次请求（父 3 + 子 1）；回放 0 次请求、0 个新子会话 |
| 长日志排障 | 崩溃前 2 次请求 + 恢复后 3 次 = 预算预留 5；恢复时 `fetch_logs` 执行 0 次 |
| 并行诊断 | CPU 异常、DB 正常、网络异常 → 升级；4 次请求 |
| 预算争用 | 3 个子 Agent 共享额度 2：成功 2、拒绝 1、实际请求 2 |
| 文稿审校循环 | 第 2 轮通过；崩溃前 2 次 + 恢复后 2 次 = 预留 4 |
| 多级委派 | 含税总价 9031.2；专家创建、清理各 1 次，换算 1 次、计税 1 次；5 次请求 |
| 人工审批暂停 | R-SMALL 直接打款，R-REJECT 拒绝不打款，R-LARGE 审批后打款；崩溃恢复不重复付款 |
| 本地化 | 恢复时翻译工具执行 0 次、校对工具 1 次；崩溃前 2 次 + 恢复后 2 次 = 预留 4 |

| 指标 | DeepSeek | MiniMax | Gemini |
| --- | --- | --- | --- |
| 工单分诊每张 Token | 2,292–2,566 | 2,629–2,745 | 2,348–2,861 |
| 多级委派 Token | 3,020–3,088 | 3,520–3,640 | 2,119–2,356 |
| 流式摘要：预览段数 | 68–76 段 | 1 段 | 2 段 |
| 流式摘要：首段 / 完成 | 1.28–1.51 s / 1.59–1.84 s | 首段即完成，2.76–3.35 s | 几乎同时，2.31–3.85 s |

一轮全部 Live 测试约 40 次模型请求。

## 案例暴露并修复的问题

### 框架缺陷：Agent 结束观测晚于终态事件（已修复）

MiniMax 第 2 轮的并行诊断出现 `agent_start` 4 个、`agent_end` 3 个的情况（[日志](design-iteration/minimax-agent-end-after-return.log)），业务结果是对的。

原因在 `builder/events.go`：`endAgent` 通过 `defer` 在发出 `done` 之后才执行，而 `AsRunnable` 收到 `done` 就立即返回。宿主如果随后马上关闭记录器，就会丢失 `agent_end`；OTel 中 Agent 的 span 也可能晚于父节点结束。

- 修复：在发出终态事件之前结束观测（`endAgent` 只会生效一次，`defer` 仍覆盖 panic 路径）。
- 回归测试：`builder/events_observe_test.go`，修复前第 1 次迭代就失败，修复后 300 次 × 3 轮带 `-race` 通过。
- 案例同时改为关闭记录器之后再读丢弃数。

### 案例设计问题（已修正，失败日志保留）

| 现象 | 原因 | 修正 | 日志 |
| --- | --- | --- | --- |
| DeepSeek 审校或写作节点报 `finish_reason=length` | `deepseek-flash` 是推理模型，偶尔推理过长，2048 的上限不够；正常一次约 300 Token | 节点输出上限放宽到 8192 | [审校](design-iteration/deepseek-review-length-reviewer.log)、[写作](design-iteration/deepseek-review-length-writer.log) |
| Gemini 审校三轮都不通过 | 字数要求只写在审校清单，写作任务书没提 | 字数要求写进任务书 | [日志](design-iteration/gemini-full-before-fixes.log) |
| DeepSeek 终稿合格却被判"修改" | 模型数不准字数 | 字数检查从审校清单中移除 | [日志](design-iteration/deepseek-review-char-count.log) |
| Gemini 翻译不调用 `lookup_term` | 术语表策略已注入全部译法，工具成了多余 | 原文加入只能经工具查询的商品编号 | [日志](design-iteration/gemini-full-before-fixes.log) |
| MiniMax 终稿丢失术语译法 | 校对 Agent 看不到术语表，润色时改写了术语 | 校对 Agent 也挂上术语表策略 | [日志](design-iteration/minimax-localize-term-rewrite.log) |
| MiniMax 第 2 轮 8 个测试失败 | HTTP 429 限流：三个模型并行跑，且两轮间隔太短 | 单独重跑，全部通过 | [日志](design-iteration/minimax-round2-http429.log) |

### 框架缺陷：模型抄错引用键会中止 Agent（已修复）

Gemini 第 1 轮长日志排障失败 1 次（[日志](live-gemini.log)），报错 `unknown evidence reference`。

- 原因：Gemini 调用 `read_context_result` 时把 64 位十六进制引用键的最后一位抄错（`…32c9` 写成 `…32c4`）。错误键已经出现在 SDK 返回的原始参数里，不是框架改写的。
- 失败位置在恢复进程，不在第一个进程。案例的崩溃点设在读取工具执行之前，错误键当时没有被校验；恢复进程回放已保存的模型回合，重新执行读取时才查不到。早先版本的本报告写成"第一个进程、崩溃注入之前"，是错误的。
- 复现：加探针用真实 Gemini 跑 12 次，复现 1 次，约每 45 次抄键错 1 次；DeepSeek、MiniMax 未出现。
- 修复（`builder/contextpolicy/offload.go`）：模型看到的引用改为按卸载顺序编号的短别名 `r1`、`r2`…；键不存在时不再报错中止，而是把错误和合法键列表作为工具结果交还模型。引用文本变化使旧日志无法复用，默认版本升为 `v2`。
- 回归测试：`TestOffloadAliasStableAndMissRecoverable`、`TestPolicyReadWrongKeyIsReturnedToModel`。修复后全量测试带 `-race` 通过；长日志排障真实模型复跑 Gemini 3/3、DeepSeek 2/2、MiniMax 2/2（[Gemini](alias-fix-gemini.log)、[DeepSeek](alias-fix-deepseek.log)、[MiniMax](alias-fix-minimax.log)）。

### 工具参数错误会中止整个 Agent（已增加可选处理）

原先任何工具错误都会中止 Agent，模型传错参数也没有改正的机会。现在 `DispatchConfig.ReturnArgumentErrors` 开启后，参数校验错误和工具返回的 `tools.ArgumentError` 会作为结果交还模型；授权、深度、重入、超时等宿主侧错误仍然中止。默认关闭，现有行为与执行日志不变。契约见 [原生编排指南](../../guides/NATIVE_ORCHESTRATION.md#参数错误交还模型)。

- 回归测试：`tools/argument_error_test.go`、`builder/argument_error_test.go`，覆盖错误分类、同轮混合调用、日志回放与开关切换、组合工具不创建子会话、直接返回工具。
- 本轮 8 个案例都没有开启该选项，所以没有真实模型数据。

## 验证了什么

- **根预算**：父 Agent、子 Agent、孙级工具、并行分支、循环各轮的请求都经过根预算准入。首次运行时预留数等于实际请求数；回放不新增准入；跨进程恢复时“崩溃前请求 + 恢复后新增 = 预留”，说明没有请求被重发。
- **Runnable 工具化与嵌套**：子会话创建数等于清理数，出错路径也一样。深度超限和 Dispatcher 重入在执行前快速失败，并带类型错误。
- **持久图**：分支循环按轮次检查点恢复；审批节点能暂停、拒绝错误的审批键，批准后恢复时不需要再次审批；外部效果在崩溃重试时靠业务单号去重。
- **同节点多 Agent**：两个 Agent 用同名工具、相同调用 ID，日志也互不干扰。
- **自定义策略**：配置哈希进入请求意图，配置一变，就在请求模型前拒绝复用旧日志。
- **观测与流原型**：同第一批案例；修复后 Agent 结束观测先于终态事件发出。

## 限制

- 每个模型 2–3 轮，样本小，只能说明在这些合成样例上的结果，不代表准确率。
- 模型给出的处置建议等自由文本没有被断言，可能包含原始数据之外的推测。
- MiniMax 适配器从 `examples/workflow/business-cases` 移植，两份代码暂时各自维护。
- 没有测量吞吐、成本上限，也没有在生产网络环境下测时延。

## 复现

```sh
for p in deepseek minimax gemini; do
  env -u GOROOT GOAGENT_LIVE=1 GOAGENT_LIVE_PROVIDER=$p \
    go test -race -count=1 -v -timeout 30m ./examples/internal/livemodel/ ./examples/orchestration/live/...
done
```

不要同时对同一提供方跑多轮，以免触发限流。
