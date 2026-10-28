# SQL 变更工单审核：规则门禁、模型复核与审批作废

```text
gate（纯 Go 规则）──reject──────────────────────────────────▶ END（rejected，不调用模型）
   └─pass/review──▶ review（模型，严格 JSON）──reject──────▶ END（rejected）
                       ├─test 且规则 pass 且 approve+low──▶ apply ──▶ END（applied）
                       └─其余（prod、escalate、非 low、规则警告）──▶ apply_approved（ApprovalNodes，暂停）
                                                                   ├─批准前置校验（运行锁内）：哈希不符──▶ 拒绝批准，保持等待
                                                                   └─执行前再重读比对──▶ applied / invalidated
```

- 8 张合成工单：T-101 白名单内的 test 插入（应自动通过）；T-102 DROP、T-103 TRUNCATE、T-104 混合 DDL 与 DML、T-105 无 WHERE 的 UPDATE（规则拒绝）；T-106 自报删除 240 万行（规则警告，应转人工）；T-107 prod 单行更新（进入审批）；T-108 小改动，离线测试中模型返回非法 JSON。
- 规则门禁 `Check` 是纯函数，逐条语句给出 `ok`/`warn`/`block` 与命中规则。任一 `block` 即拒绝，**规则拒绝的工单不进入模型节点**。关键字先剥离单、双引号字符串字面量再识别（`'x WHERE y'` 不算 WHERE）。
  - R1：禁止 `DROP`/`TRUNCATE`。
  - R2：同一工单不得混合 DDL 与 DML。
  - R3：`UPDATE`/`DELETE` 必须在字面量之外、括号深度 0 处带 `WHERE`；子查询里的 `WHERE`（如 `SET status = (SELECT s FROM demo_cfg WHERE id = 1)`）不算，括号不配对视为没有 `WHERE`。条件按 `OR` 拆分（不论括号层级，宁可误拦），任一分支不引用列（`WHERE 1`、`1 = 1`、`TRUE`、`'a' = 'a'`、`NOW() > 0`，函数名不算列）或比较两侧文本相同（`id = id`）即视为恒真并拦截。这是示例级规则，不做语义分析：`id >= 0`、`id IS NULL OR id IS NOT NULL` 这类依赖值域的恒真条件拦不住，由 R5 与人工审批兜底。
  - R4：语句白名单为 `INSERT`、带 WHERE 的 `UPDATE`/`DELETE`、`CREATE TABLE/INDEX`、只含单一 `ADD` 子句的 `ALTER TABLE`（出现逗号或 `DROP` 即拦截，`DECIMAL(10,2)` 这类带逗号的列定义也会被保守拦截）。语句中含分号（**末尾单个分号也一律拒绝**，工单每条语句本就单独存放）、`--`、`/*`、`#` 注释、反斜杠或未闭合引号即拦截，不论是否位于字面量内。工单没有任何语句也拒绝。
  - R5：DML 影响行数超过 10000 只给警告，转人工审批。`EstRows` 是提交人在工单里**自报**的估算值，规则不核实；低报即可绕过 R5，真实系统应以 `EXPLAIN` 或影子库估算替代。
- 模型只能否决或升级：自动执行要求规则 `pass`、test 环境、模型 `approve`+`low` 同时成立，模型给出任何更宽松的结论都无法越过规则。
- 模型输出用 `opscase.StrictJSON` 解析，再校验枚举与 summary。解析失败、未知字段、多余内容、非法枚举一律收敛为 `escalate`/`high`，原因写入 `ModelError`，不重试。模型调用失败（含根预算拒绝）同样转人工，不中止整批工单。
- 路由取舍：**转人工也走审批暂停**（与 prod 共用 `apply_approved` 节点），不单独标记 manual。这样人工结论只有一个入口：批准即执行，不批准就一直等待。
- 作废：状态里记录审核时**整张工单**（id、env、statements、est_rows）JSON 的 sha256，只改 EstRows 或 Env 也算改动。
  - 批准：`CompileDurable` 注册 `workflow.WithBeforeApprove`，在运行锁内、批准写入前重新读取模拟工单系统中的当前内容比对哈希。不一致就拒绝批准（`ErrTicketChanged`，包装为 `批准前置校验拒绝`），检查点保持 Waiting、审批键不变，不执行变更；宿主不必先读快照再批准，两步之间的窗口不存在。工单改回审核时的内容后可用原审批键再次批准；以新内容再次 `Run` 则开启新一轮审核，旧运行一直停在等待。
  - 执行：`apply` 与 `apply_approved` 执行前再重读比对一次，不一致标记 `invalidated`、不执行。审批路径上它只拦批准之后到执行之间的改动；自动执行路径不经过审批，靠它拦复核之后的改动。
- 重审：运行 ID 为 `sql/<工单号>@<哈希前 12 位>`。工单被修改后再次 `Run`，会以新内容开启新一轮审核；内容不变则只回放。
- 模拟数据库按“工单号 + 整张工单哈希”去重，崩溃后重试返回原回执，只执行一次。
- 一个根预算覆盖整批工单的模型请求（含重审），与工单、执行记录保存在同一持久化目录；同目录再次运行 0 次请求。

```sh
dir=$(mktemp -d)
GOAGENT_LIVE_PROVIDER=deepseek go run ./examples/orchestration/ops/sql-review -dir "$dir"   # 记下等待中工单的 RunID 与 ApprovalKey
GOAGENT_LIVE_PROVIDER=deepseek go run ./examples/orchestration/ops/sql-review -dir "$dir" -approve-run <RunID> -approve-key <ApprovalKey>
```

| 测试 | 断言 |
| --- | --- |
| `TestCheckRules` | 8 张工单的规则结论；R2、R3 命中；恰好 10000 行不警告；白名单外语句、空语句、空工单被拦截；子查询里的 WHERE、括号不配对、`WHERE 1`/`1 = 1`/`TRUE`/`'a' = 'a'`/`id = id`/`OR 1 = 1`/`NOW() > 0` 被 R3 拦截，外层另有 WHERE 的子查询、括号条件、函数与列比较放行；分号拼接多语句、末尾分号、`--`/`#`/`/*` 注释、反斜杠、未闭合引号被拦截；ALTER 多子句或 DROP 被拦截；字面量内的 WHERE 不算数；字面量内的关键字与 `''` 转义不误拦 |
| `TestScriptedModelFailureEscalates` | T-107 的复核请求在传输层失败：该工单转人工等待审批，其他工单照常处理 |
| `TestParseVerdict` | 代码块包裹的合法输出可解析；非 JSON、未知字段、多余内容、非法枚举、空 summary、`null`、字段名大小写不符、重复键都收敛为 `escalate`/`high` |
| `TestApplyChangeIsIdempotent` | 同一工单同一内容重复执行返回原回执，只执行一次 |
| `TestScriptedBatch` | 模型只看到 4 张通过规则的工单；只有 T-101 自动执行；T-108 非法输出转人工；请求数 4 等于预算预留；回放 0 请求、0 执行 |
| `TestScriptedApproveApplies` | prod 暂停 → 错误审批键被拒 → 批准后执行一次且不再请求模型 → 重复批准被拒 → 回放不再执行 |
| `TestScriptedEditBlocksApproval` | 暂停期间修改工单语句：批准被前置校验拒绝（`ErrTicketChanged`），仍为 waiting、审批键不变，重复批准同样被拒，新旧版本都未执行；再次运行只重审该工单（1 次请求），模型拒绝新内容 |
| `TestScriptedRevertedEditApproves` | 暂停期间改动后批准被拒；工单改回原内容后用原审批键批准，执行一次且不再请求模型 |
| `TestScriptedNonStatementEditBlocksApproval` | 暂停期间只改 EstRows 或 Env：批准被拒、不执行；再次运行以新内容重审（1 次请求），EstRows 超限转审批、改为 test 环境则自动执行 |
| `TestScriptedEditBeforeAutoApplyInvalidates` | 自动执行路径：复核之后、执行之前工单被修改，执行节点比对后标记 `invalidated`，不执行 |
| `TestLiveSQLReview` | 真实模型：每张工单的状态必须等于规则去向与模型结论推出的唯一状态——规则拒绝的不调用模型；模型 reject 即 rejected；只有规则去向为自动且 approve+low 的 test 工单 applied，模型降级（escalate、非 low、非法输出）转为 waiting 不算失败；只有 applied 的工单留下执行记录。T-107 未被模型拒绝时必须 waiting，批准后执行一次且 0 请求；被模型拒绝则跳过批准流程。请求数等于预算预留；回放 0 请求 |

审批键不是身份凭证。真实系统要先认证审批人并鉴权，再调用 `Approve`。
