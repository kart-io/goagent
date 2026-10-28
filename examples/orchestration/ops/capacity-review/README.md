# 容器容量两层复评：规则建议、批量初筛、按需深评，AI 只能降级

```text
12 个工作负载 ──Rule（纯 Go）──▶ scale_down / scale_up / keep / insufficient_evidence + 新 request/limit
      │
      ├─初筛：每批 6 个，各一次无工具 Agent 调用 ──严格 JSON 校验──不通过──▶ 整批作废
      │
      ├─深评：规则可执行 + 初筛同方向 + 证据完整 ──▶ Dispatcher 并行调用 deep_review（每项一个子 Agent）
      │                                              └─严格 JSON + 数值护栏──不通过──▶ 该项降级
      ▼
最终矩阵（纯 Go）：execute / manual_review / keep
```

- 合成场景：12 个工作负载，只看 CPU（毫核），各带当前 request/limit、7 天 P99 与 Max、证据是否完整。按规则应得：`cap-a/d/h` 缩容，`cap-b/f/k` 扩容，`cap-c/g/j` 保持，`cap-e/i/l` 证据不足。
- 规则引擎：P99 超过 request 的 90% 或 Max 超过 limit 的 90% 扩容；两者都低于 40% 缩容；新值取 P99×1.25、Max×1.3 并向上取整到 50。扩容只要任一项超线就触发，另一项算出的新值可能低于当前值（如 request 500、limit 2000、P99 480、Max 600 会得到 limit 800），所以扩容时 request、limit 各自取新值与当前值的较大者；缩容要求两项都低于 40%，新值必然低于当前值，不需要钳制。证据不完整不给建议。
- 初筛：用 `opscase.StrictJSON` 解析 `{"items":[{"id","direction","evidence_complete"}]}`。以下任一情况整批作废，本批的可执行项降为人工复核，原因记入 `BatchErrors`：
  - 漏掉、重复或多出 id；
  - 方向不在枚举内，或 `evidence_complete` 缺失、为 `null`；
  - 字段名大小写不符（如 `"ID"`）、字段重复或出现未知字段（`StrictJSON` 逐层检查，数组里的对象也查）；
  - 证据不完整的项给出 `scale_*`；
  - 输出不是合法 JSON，或模型调用失败（包括被根预算拒绝）。
- 深评：`deep_review` 由 `tools.NewRunnableToolFactory` 创建，每次调用新建一个带唯一 `SessionID` 的子 Agent，调用结束即清理。模型只能看到宿主按可信数据生成的输入，工具参数也只有 `id`。
  - 宿主用 goroutine 经 `Dispatcher.Invoke` 并行调用，并发上限由 `MaxConcurrency=3` 控制。选它而不是 workflow 并行节点，是因为候选数在运行时才确定。并发由宿主发起，不经过 Builder 原生工具循环，同轮工具调用默认并发与 `WithParallelToolCalls` 与本案例无关；测试只按集合比对深评对象，不依赖顺序。
  - 超时用 `RunnableToolConfig.Timeout` 声明在工具上（2 倍单次请求超时），只计执行时间，不含等待并发名额的排队。
  - `CallID` 由 id 派生，持久回放时直接取已保存结果，不新建子 Agent。
- 深评护栏在宿主强制，任一不满足就把该项降为 `manual_review` 并清空计划：
  - 回显 id 必须一致，verdict 必须是 `confirm`；
  - request ≥ P99，limit ≥ Max，limit ≥ request，两者都不超过当前值 2 倍；
  - 计划必须夹在规则计划与当前值之间，request 与 limit 分别比较：缩容时 规则值 ≤ 计划 ≤ 当前值，扩容时 当前值 ≤ 计划 ≤ 规则值。深评只能比规则更保守，不能更激进；
  - 计划与当前值完全相同时不可执行，降为 `manual_review`。不选 `keep`：规则已经判定需要调整，深评确认“不变”与规则矛盾，应交给人裁决，而不是让 AI 把规则结论改写成 keep。
  - reason 只是说明文字，宿主从不执行它，所以不对其内容做写操作检查；可执行的只有经护栏校验的 request/limit 数值。
- 规则计划在 `TestRulesAndGuard` 里过同一套护栏（规则值自己就是上下界的一端）。
- 最终矩阵只有一条通往 `execute` 的路：规则给出 `scale_*`，初筛同方向且证据完整，深评 `confirm` 并通过护栏，计划取深评给出的数值（不会比规则更激进）。`keep` 项始终是 `keep`，`insufficient_evidence` 始终是 `manual_review`，AI 怎么说都不会让它们变成可执行。
- 根预算覆盖初筛与深评的全部请求，默认额度 8（2 次初筛 + 6 次深评）。深评名额按配置预先分配：额度减去初筛批数，按 id 顺序发放，超出的项记“预算不足，未深评”。这一步不读预算快照，所以首次运行与回放的结果一致。

```sh
GOAGENT_LIVE_PROVIDER=deepseek go run ./examples/orchestration/ops/capacity-review -dir /tmp/capacity-review-1
```

| 测试 | 断言 |
| --- | --- |
| `TestRulesAndGuard` | 12 项规则结论符合预期，规则自身的建议都通过护栏；只由 P99 或只由 Max 触发的扩容（另一项新值低于当前值）取当前值，规则计划仍通过护栏；护栏的每条规则都能拦下对应的越界计划，包括缩容/扩容比规则更激进（request、limit 分别检查）和与当前值相同；介于规则值与当前值之间的保守计划可以通过 |
| `TestScriptedNormalMatrixAndReplay` | 6 项可执行、3 项 keep、3 项人工复核；共 8 次请求，与预算预留数相等；6 个子 Agent 全部清理；同目录再次运行 0 次请求，也不新建子 Agent |
| `TestScriptedScreenBatchVoid` | 第 1 批出现漏 id、重复 id、多出 id、证据不完整却给 scale、非法 JSON、字段名大小写不符（`"ID"`）、`evidence_complete` 为 null 时整批作废，本批不深评；第 2 批照常可执行 |
| `TestScriptedDeepDowngrade` | 深评低于 P99、超过 2 倍、回显 id 不符、非法 JSON、reject，各自降为人工复核并清空计划；reason 含 `kubectl apply` 但数值合规的 cap-d 仍可执行（reason 不被执行）；cap-a 给出比规则更激进的缩容（request=P99、limit=Max）、cap-b 确认与当前值相同的空操作都降为人工复核，cap-d 取介于规则与当前值之间的保守值仍可执行 |
| `TestScriptedKeepNeverExecutable` | 初筛把 keep 项判成 scale_down 时，这些项不进入深评，结论仍是 keep |
| `TestScriptedBudgetLimit` | 额度为 5 时只深评前 3 个候选，其余记“预算不足”；额度为 1 时第 2 批初筛被根预算拒绝而作废，没有可执行项 |
| `TestLiveCapacityReview` | 真实模型跑全流程：可执行项都满足三个条件并通过护栏，keep 与证据不足项都不可执行，子 Agent 创建数等于清理数，请求数等于预算预留，回放 0 次请求 |

回放说明：初筛的模型回合与深评工具结果都已持久化。回放时不发请求，也不新建子 Agent，矩阵由同一批已保存的输出重新计算。
