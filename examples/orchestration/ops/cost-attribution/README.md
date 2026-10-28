# 云费用环比归因：金额只来自工具，多币种不相加，空值不是 0

```text
问题 ──▶ 费用 Agent ──cost_query（totals / compare / breakdown）──▶ Authorize 核对账号 ──▶ 合成账本（结果带 ref，如 E3fa29c）
                   └─submit_conclusion──▶ 账本核对证据 + 复算最高涨幅 + summary 数字核验 + 跨币种合计检查
                                         ├─不通过──▶ 交还模型修复（最多 2 次）
                                         └─通过──▶ 渲染“结论：<币种>/<大类>”直接作为最终回答
```

- 合成账本：3 个账号、2 个币种、5 个大类、2026-07 至 2026-09，金额以整数“分”存储和计算，展示时格式化为两位小数。
  - `cn-prod`（CNY）：08 月 `database/rds` 金额缺失（null）。
  - `us-ml`（USD）：数据从 08 月才开始。
  - `us-legacy`（USD）：调用者无权访问；其 `network` 09 月大涨，若被越权查询会干扰结论。
- 按数据应得 `USD/ai`：环比 +87.5%。CNY 的 `compute` 差值 1000.00 在数值上更大，但涨幅只有 8.1%；不同币种不比较金额，只比较百分比。
- `cost_query` 的差值、百分比、排序都由代码计算：
  - `compare` 按币种分组，每组按差值降序排出 `ranked`；
  - 金额不完整的项放进 `unranked` 并注明缺失的产品，不当 0，也不参与排序；
  - 数据起点之前的月份标注 `no_data`，不计算增长；多账号聚合时只要有一个账号处于起点之前，整格就是 `no_data`，不会用其余账号的金额当基数；
  - 基数为 0 的项不计算百分比（不输出 `+Inf%`/`NaN`），放进 `unranked` 并注明。
- 参数组合不合法（如 `compare` 缺 `base_month`、`breakdown` 缺 `category`、月份超出账期）返回 `*tools.ArgumentError`，交还模型改正。
- 授权用 `DispatchConfig.Authorize`：调用者只绑定 `cn-prod`、`us-ml`，绑定关系来自宿主，不来自模型参数。
  - `account_ids` 含未授权账号即拒绝。按框架约定这是致命错误，整个 Agent 中止，工具不会执行。
  - 不传 `account_ids` 时默认只查已授权账号。Schema 只约束账号 ID 的格式，不列出账号，授权判断全部交给 `Authorize`。
- `submit_conclusion` 用 [opscase](../internal/opscase/opscase.go) 的 `Submitter`：证据核对、有界修复、直接返回与回放恢复都与 [alert-rca](../alert-rca/README.md) 相同。`Check` 另加三条宿主核验：
  - `CheckTop`：用 `Compare` 对已授权账号复算 2026-09 相对 2026-08 的环比，跨币种只比较 pct（交叉相乘，不用浮点），`top_currency/top_category` 与涨幅最大的项不符就交还模型，提示里给出应得的项。模型不能把差值最大的 `CNY/compute` 当作答案。
  - `CheckSummary`：先整段剔除时间（`10:05`、`10:05:12`）与日期（`2026-09`、`2026-09-30`），再识别百分比（含 `88%` 这样的整数）、带小数的数、前置币种的数（`USD 9999`、`$9999`、`¥…`）、后面紧跟 元/美元/CNY/USD 的整数金额。去掉千分位逗号后必须与账本中某个字段值完全相等（`Ledger.Values()`）。省略负号引用负值（“下降 100.00”对应 `-100.00`）时，同一句里必须有下降、减少、降低之类的方向词，否则视为方向反转而拒绝。`涨幅:99.9%`、`1125.0`、`600 美元` 都会被拒；“9 月”“USD 9 月”不算金额。
  - 同一句里同时出现 CNY 与 USD，并带“合计”或“总计”，即视为跨币种相加而拒绝。
  - 后两条只是示例级启发式：数字只核对“出现在某个字段值里”，不核对它属于哪一项，也不是语义判断。证据核对同样只证明摘录出自工具结果，不证明摘录支撑结论。
- 宿主只发布通过核验的结论：`Decision` 由结论的 `top_currency/top_category` 生成，最终回答由结论渲染；模型没有提交就报错。

```sh
GOAGENT_LIVE_PROVIDER=deepseek go run ./examples/orchestration/ops/cost-attribution -dir /tmp/cost-attribution-1
```

| 测试 | 断言 |
| --- | --- |
| `TestCompareCents` | 环比差值精确到分、百分比四舍五入到一位小数、币种内按差值降序；同币种多账号相加 |
| `TestMissingAmountNotZero` | CNY `database` 的 08 月缺失：不进 `ranked`，标注 `incomplete` 与缺失产品，不会算出假涨幅 |
| `TestNoDataBeforeStart` | `us-ml` 07 月标注 `no_data`，不是 0，不计算增长 |
| `TestNoDataAcrossAccounts` | `us-ml` + `us-legacy` 对比 08/07：`us-legacy` 的 07 月金额不能单独当基数，整格保持 `no_data`，不进 `ranked` |
| `TestZeroBaseUnranked` | 新增产品基数为 0：不输出 `+Inf%`/`NaN`，放进 `unranked` 并注明基数为 0 |
| `TestCostQueryArgumentErrors` | 7 种不合法的参数组合都返回 `*tools.ArgumentError` |
| `TestCheckSummary` | 编造数字、只匹配尾部的数字被拒；跨币种合计被拒；分句各自只涉及一个币种时放行 |
| `TestCheckSummaryNumbers` | `88%`、`600 美元`、`1125.0`、`1,125 USD` 被拒；`1,125.00` 能对上 `1125.00`；“9 月比 8 月”不误报 |
| `TestCheckSummaryBypasses` | `涨幅:99.9%`、`USD 9999`、`$9999`、`USD 87.0%`、无方向词的 `3.2%`（实为 `-3.2%`）、方向词在另一句都被拒；“下降 100.00”、`-100.00`、带时间或日期的句子放行 |
| `TestCheckTop` | `USD/ai` 通过；`CNY/compute`、`USD/compute`、`CNY/ai` 被拒，提示应得 `USD/ai（87.5%）` |
| `TestScriptedConclusionAccepted` | 一次提交即通过，最终回答由结论渲染；共 3 次请求，与预算预留数相等；同目录再次运行 0 次请求，也不执行工具，已核验结论由回放恢复 |
| `TestScriptedAnswerBoundToConclusion` | 不提交直接作答报“没有提交通过核验的结论” |
| `TestScriptedTopRepair` | 提交 `CNY/compute` 被复算拒绝并交还一次，改正为 `USD/ai` 后通过，共 3 次请求 |
| `TestScriptedArgumentRepair` | `compare` 缺 `base_month` 交还模型一次，改正后通过，错误调用不进入账本，共 4 次请求 |
| `TestScriptedSummaryRepair` | 编造的 `88.0%`、跨币种合计各交还一次，第 3 次提交通过，共 4 次请求 |
| `TestScriptedRepairLimit` | 连续提交编造的数字：交还 2 次后第 3 次中止，没有被接受的结论 |
| `TestScriptedUnauthorizedAborts` | `account_ids` 含 `us-legacy`：Agent 中止，工具未执行，模型没有收到任何结果 |
| `TestLiveCostAttribution` | 真实模型得出 `USD/ai`，结论通过核验，修复不超过 2 次，请求数等于预算预留，回放 0 次请求 |

回放说明：持久回放时工具与 `submit_conclusion` 都不会重新执行。账本（含 summary 核验用的字段值）与已接受的结论都经 `OnResult` 从日志重建，`Decision` 仍由结论生成；调查中途崩溃后恢复，新的提交照样能核对已回放的结果。
