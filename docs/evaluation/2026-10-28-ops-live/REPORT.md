# 运维 AI 案例真实模型验证

日期：2026-10-28。分支：`codex/composable-orchestration-r2`，基于 `95bc8c6` 加本轮框架修复（未提交，见 CHANGELOG `[Unreleased]`）。环境：Linux、Go 1.27。案例见 [examples/orchestration/ops](../../../examples/orchestration/ops/README.md)，模型配置与 [上一批评测](../2026-10-28-orchestration-live/REPORT.md) 相同：DeepSeek `deepseek-flash`、MiniMax `MiniMax-M3.1-Flash`（国内端点）、Gemini `gemini-2.5-flash`。

业务数据全部是合成的。测试断言的是按规则应得的结论与编排不变量，不校验模型的自由文本。

## 最终结果

每个模型每个案例跑 2 轮，带 `-race`；逐个案例执行，间隔 20 秒。

| 案例 | DeepSeek | MiniMax | Gemini |
| --- | --- | --- | --- |
| 告警根因 | 2/2 | 2/2 | 2/2 |
| SQL 工单审核 | 2/2 | 2/2 | 2/2 |
| 容量两层复评 | 2/2 | 2/2 | 2/2 |
| 费用归因 | 2/2 | 2/2 | 2/2 |
| 受限子调查 | 2/2 | 2/2 | 2/2 |

原始日志：[DeepSeek](live-deepseek.log)、[MiniMax](live-minimax.log)、[Gemini](live-gemini.log)。日志不含本机路径，并按各密钥的值检查过，不含 API Key。

框架修复前的最终一轮（Gemini 告警根因 1/2，失败为结论被接受后的空回答）保留在 [design-iteration/round4-before-framework-fixes](design-iteration/round4-before-framework-fixes/)。本轮提交结论即结束运行，那一轮请求不再存在。

## 关键数据

| 案例 | 三个模型一致的结果 |
| --- | --- |
| 告警根因 | 根因 `config_change`；结论通过逐字核对；回答由已核验结论渲染；请求数等于预算预留 |
| SQL 工单审核 | 每张工单的状态等于由规则去向与模型结论推出的唯一应得状态；只有应得 `applied` 的工单留下执行记录；4 次请求 |
| 容量两层复评 | 初筛 2 批全部有效；6 项进入深评，子 Agent 创建 6、清理 6；keep 与证据不足的项都不可执行；8 次请求 |
| 费用归因 | `USD/ai`，并经宿主按 `Compare` 复算确认；summary 中的金额与百分比都出自工具结果 |
| 受限子调查 | `inventory-svc`；3 个子调查各只调用授权工具；同轮派出的子调查真并发；结论引用子调查内部的证据并通过核对 |

| 案例 | 请求数（DeepSeek / MiniMax / Gemini） | 结论修复次数 | 备注 |
| --- | --- | --- | --- |
| 告警根因 | 4,4 / 5,3 / 3,6 | DeepSeek 两轮各 1 次，MiniMax 1 轮 1 次 | 修复原因都是摘录过短，例如把空数组 `[]` 当作“日志正常”的证据 |
| SQL 工单审核 | 4 / 4 / 4 | — | |
| 容量两层复评 | 8 / 8 / 8 | — | 6 轮合计 execute 36、keep 18、manual_review 18 |
| 费用归因 | 3,4 / 3,2 / 2,3 | 0 | |
| 受限子调查 | 9,10 / 8,11 / 9,7 | 0 | 子调查并发峰值 3，仅 Gemini 1 轮为 2（模型分两轮派出） |

与修复前相比，告警根因、费用归因、受限子调查的成功运行各少 1 次请求（提交即结束）。

## 本轮修复

修复前由两个独立审查得出：一个核实下文“框架问题”并复现，一个审查案例代码。两份报告的发现都已处理。

### 框架

| 问题 | 修复 |
| --- | --- |
| 工具结果被 HTML 转义，模型无法原样摘录 | 原生工具流与两条旧工具路径都不再转义 `<>&` |
| 直接返回工具与“参数错误交还模型”不兼容，提交结论后必须多请求一轮 | 直接返回工具遇到参数错误时交还模型改正；最后一轮仍错则按额度用尽返回 |
| 空回答直接中止，持久模式下不可恢复 | 新增 `WithEmptyAnswerRetries(n)`，默认关闭 |
| 持久模式下无效工具计划报错不含原因 | 拆分为缺少 ID、ID 重复、未提供的工具、参数非对象，并带工具名 |
| 同一轮多个工具调用串行 | 全部为 Dispatcher 工具时默认并发，`WithParallelToolCalls(1)` 关闭；消息与事件顺序不变 |
| 回放不经过 Dispatcher 内部的包装器，宿主状态无法重建（已复现引用号冲突） | `DispatchConfig.OnResult`，首次执行与回放都回调 |
| Dispatcher 只有一个含排队的超时 | 工具可声明自己的超时，计时不含排队 |
| 审批“先查快照再批准”存在检查窗口 | `workflow.WithBeforeApprove`、`durable.Runner.BeforeApprove`，在运行锁内校验 |
| 预算无法查询余量 | `Controller.Remaining` |

并发默认开启是破坏性变更：`examples/workflow/business-cases` 的两个效果有先后依赖，回归测试发现数据竞争，已在该示例显式设置 `WithParallelToolCalls(1)`。

### 案例

| 问题 | 修复 |
| --- | --- |
| 结论在 Agent 结束后才落盘，这个窗口崩溃则目录永久无法恢复（已复现） | 不再另行落盘：已接受结论经 `OnResult` 从提交记录恢复，回放同样可得 |
| 引用号按执行顺序分配，并发与崩溃恢复后不确定 | 由执行键派生（形如 `E3fa29c`），经 `OnResult` 登记，回放重建账本 |
| 摘录可丢负号、截断单词（`100.00` 冒充 `-100.00`、`-api`） | 判为截断 |
| 金额核验可绕过（`涨幅:99.9%`、`USD 9999`、去负号） | 先剔除时间日期再核对；识别前置币种；省略负号须有方向词 |
| 费用归因的最高项由模型决定、宿主不复核 | 宿主用 `Compare` 复算，不一致交还模型 |
| 受限子调查不检查 operation 与 downstream 是否配套 | 增加配套检查 |
| SQL R3 可被子查询中的 WHERE 或恒真条件绕过 | 只认外层 WHERE，拒绝不引用列的条件 |
| SQL 审批期间工单被改，批准前无锁内校验 | 改用 `WithBeforeApprove` 比对工单哈希 |
| SQL Live 测试比文档宽松 | 逐张核对应得状态 |
| 容量规则在部分输入下过不了自己的护栏 | 扩容时 request、limit 都不低于当前值 |
| 容量案例检查从不执行的理由文本 | 删除 |

已删除的绕开代码：接受结果里“现在直接输出最终回答”的提示、防重复提交分支、三个案例的“最终回答须与结论一致”校验、宿主侧结论落盘、`opscase.Dispatch` 强制 `MaxConcurrency=1`。

## 早期设计迭代中发现的问题

修正前的日志保留在 [design-iteration/](design-iteration/)：round1 为首轮，round2、round3 为修正过程中的完整轮次。

| 问题 | 现象 | 修正 |
| --- | --- | --- |
| 主 Agent 看不到原文却要原样摘录 | 受限子调查：DeepSeek 每次修复 1 次，Gemini 两轮核验失败中止（round1） | 子调查给出经核对的摘录，宿主只把通过的摘录交给主 Agent |
| 发布的结论未绑定已核验结论 | 模型不提交或提交后改口，结论仍被发布 | 回答由已接受结论渲染 |
| 摘录核对过松 / 过严 | 子串匹配让 `in_use=5` 冒充 `in_use=50`；改为单字段匹配后 Gemini 跨字段照抄被拒 | 按模型看到的原始顺序展开为文本，按词边界匹配 |
| 输出上限过小导致静默降级 | Gemini 初筛因 `MAX_TOKENS` 作废，所有项降级，测试仍通过（round3） | 输出上限调到 8192；Live 测试要求初筛有效且至少一次深评 |
| MiniMax 限流 | 并行执行多个案例包时 HTTP 429（round1） | 逐个案例执行，间隔 20 秒 |

## 仍未解决与限制

- 本轮未观察到空回答、`MALFORMED_FUNCTION_CALL`、`unexpected EOF`；日志不记录空回答重问是否触发。迭代中 Gemini 出现过的 `unexpected EOF` 原因仍未定位。
- 核验只证明摘录出自工具结果，不证明摘录支撑结论；金额只核对“出现在某个字段值里”，不核对属于哪一项。
- SQL 规则拦不住需要值域才能判断的恒真条件（如 `id >= 0`），影响行数为工单自报。
- 受限子调查在子调查执行中崩溃不能自动恢复（组合工具只允许一次尝试，需要宿主处理）。
- 每个模型每个案例 2 轮，样本小，只说明在这些合成样例上的表现。

## 复现

```sh
for p in deepseek minimax gemini; do
  for c in alert-rca sql-review capacity-review cost-attribution scoped-lookup; do
    env -u GOROOT GOAGENT_LIVE=1 GOAGENT_LIVE_PROVIDER=$p \
      go test -race -count=2 -v -timeout 30m ./examples/orchestration/ops/$c/
    sleep 20
  done
done
```
