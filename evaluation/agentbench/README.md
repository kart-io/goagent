# GoAgent 任务评测

评测包已纳入主 Go 模块，使用实际 OpenAI 兼容 SDK、统一请求计量和严格评分。无独立模块依赖，根目录测试会包含本包。

## 离线验证

在仓库根目录执行：

```sh
GOMAXPROCS=2 GO_TEST_MODE=true go test -p 2 -race ./evaluation/agentbench/... ./builder -short -count=1 -timeout 120s
```

本包覆盖 800 次终态文本 SDK 协议验证、40 次并发执行隔离，以及普通工具、无证据、预算、取消、缺失用量和截断响应。Builder 另有 20 类 × 80 组执行矩阵。假模型成绩不计作真实模型成功率。

## 生成与执行

从本目录运行；结果目录须不存在：

```sh
# 默认只生成题集，不调用模型。
go run ./cmd/stress -count 120 -seed 20261026 -out /tmp/goagent-suite-preview

go run ./cmd/stress -live -provider deepseek -model deepseek-flash \
  -mode contract -count 120 -seed 20261026 -workers 4 -out /tmp/goagent-suite-live
```

固定种子覆盖 12 类任务，各 10 题：中文订单、英文退款、多行、JSON、Unicode、转义、长结果、状态边界、证据筛选、计算、无证据拒答、无工具算术。模板内样本相关，不代替独立生产样本。

- `-mode all`：80 道终态题各执行默认/直返，40 道其他题执行默认，共 200 次。
- `-mode baseline`：全部 120 题执行默认循环。
- `-mode contract`：按预先声明的 `direct_eligible` 选择直返或推理，共 120 次；不读取答案、不根据失败结果事后切换。
- `-repeats` 增加重复次数，`-workers` 限制并发，`-timeout` 设置单任务超时。
- `-max-calls`、`-max-output-tokens`、`-token-stop` 设置请求预算；`-provider deepseek` 直接使用 DeepSeek 专用 Provider。

Provider 配置按名称隔离：默认 `openai` 读取 `OPENAI_API_KEY/BASE_URL/MODEL`；`deepseek` 读取 `DEEPSEEK_API_KEY/BASE_URL/MODEL`，未配置地址时使用官方端点。`gemini-openai` 读取 `GEMINI_API_KEY/BASE_URL/MODEL`，默认地址为 Google 官方 OpenAI 兼容端点 `https://generativelanguage.googleapis.com/v1beta/openai`（自定义地址也须包含兼容路径）。`-model` 覆盖对应 MODEL 环境变量，三组凭据不会交叉回退。

默认每任务最多两次模型请求、每次输出上限 512 Token、已观测用量阈值 8000、超时 45 秒。阈值不等于硬性计费上限，最后一次请求可能超额。无隐藏重试。成功要求最终输出去除首尾空白后严格匹配；有证据时必须执行工具。调用契约另外要求工具恰好一次，终态直返/无工具一次请求，其余两次请求。

输出 `dataset.json`、`manifest.json`、即时追加的 `results.jsonl` 和 `summary.json`。统计按配置和类别列出成功、调用契约、完整用量、全部请求/Token、P50/P95；失败保留在分母。失败或中断返回非零退出码，已完成记录保留。未知用量不补作已知零值。并发延迟不能直接当作纯框架开销。

系统提示版本 `task-semantics-v2` 区分原样输出和事实推理。已有工具结果的最后一轮只保留显式终态工具，其他情况下使用剩余预算回答；不保证证据充分。生产原样输出业务应显式配置 `WithReturnDirectTools`，推理任务保持模型回答。

## 自有数据与小题集

[退款客服题集](testdata/refund-cases.json)覆盖退款期限提取、状态直返和资料不足拒答。离线回归使用本地模拟模型运行真实 GoAgent SDK 和工具循环，验证 12 次正常运行与 4 次响应缺少完成标记的失败运行：

```sh
# 从仓库根目录运行，无需 API Key。
go test -race -count=1 -v ./evaluation/agentbench -run '^TestRefundScenario$'

# 仅生成退款案例运行计划，不调用模型。
go run ./evaluation/agentbench/cmd/stress \
  -dataset evaluation/agentbench/testdata/refund-cases.json \
  -mode all -repeats 3 -workers 1 -out /tmp/goagent-refund-preview
```

已保留 [DeepSeek 真实测试报告](../../docs/evaluation/2026-10-27-deepseek-refund/REPORT.md)：GoAgent 12/12 通过，共 21 个模型请求。报告注明原始参数和当前运行命令的区别；示例题不代表生产业务验收。

新增 [DeepSeek 原生 Provider 参数实测](../../docs/evaluation/2026-10-27-deepseek-native-refund/REPORT.md)使用 [refund-parameter-cases.json](testdata/refund-parameter-cases.json)：答案、调用契约和订单参数均为 12/12，共 21 个模型请求、8,466 Token。它是本轮重新执行的合成题实测，历史报告保持原样。写操作恢复另见[退款审批与崩溃恢复案例](../../examples/workflow/refund-recovery/README.md)。

`-dataset /path/to/cases.json` 替代生成题集：

```json
[
  {"id":"order-1","category":"订单状态","direct_eligible":true,
   "question":"查询订单状态，只输出完整结果。","evidence":"已发货。","answer":"已发货。"},
  {"id":"policy-1","category":"退款推理","direct_eligible":false,
   "question":"退款期限是多少天？只输出数字。","evidence":"配送 3 天，退款期限 30 天。","answer":"30"}
]
```

`answer` 仅用于判分，不发送模型。`evidence` 是固定的工具返回资料，不能伪造标准答案代替业务数据。默认无参数查询工具不评价参数抽取；可用下述标注扩展。固定证据工具不执行真实业务查询，不能验证权限、写操作幂等或持久化恢复。

### 参数标注与真实业务准备

在 Case 中添加 `lookup.parameters`（JSON Schema 对象）和 `lookup.expected_arguments`（期望参数对象），即可分别统计答案与工具参数是否正确。参数标准答案不发送给模型；少字段、多字段、错误值或多次调用均不能通过参数评分。未提供标注时不计入参数评分分母。完整示例见 [parameter-cases.json](testdata/parameter-cases.json)，它是合成样例，不是业务验收集。

```json
"lookup": {
  "parameters": {
    "type": "object",
    "properties": {"order_id": {"type": "string"}},
    "required": ["order_id"],
    "additionalProperties": false
  },
  "expected_arguments": {"order_id": "DEMO-1001"}
}
```

逐次结果记录 `actual_tool_arguments` 和 `tool_arguments_passed`；汇总记录 `tool_arguments_evaluated`、`tool_arguments_passed`。`passed` 保留答案评分含义；`contract_passed` 同时要求答案、调用次数和已标注的参数都通过。即使错误参数下固定工具返回了正确答案，整体契约仍判失败。比较按 JSON 结构进行，数字使用精确有理数比较；订单号等标识应使用字符串，避免 SDK 数值解码丢失精度。导入只校验对象 schema 和标签基本结构，不充当完整 JSON Schema 校验器。

```sh
# 仓库根目录；去掉 -live 可先预览计划，不调用模型。
go run ./evaluation/agentbench/cmd/stress -live \
  -provider deepseek -model deepseek-flash \
  -dataset /path/to/labeled-business-cases.json \
  -mode contract -repeats 3 -workers 1 \
  -max-calls 2 -max-output-tokens 2048 -token-stop 8000 \
  -out /tmp/goagent-business-results
```

输出目录权限为 0700，数据文件为 0600。manifest 不记录凭据或端点；题集、实际参数和结果仍可能包含业务信息。题集来源、独立验收集、评分门槛及人工证据检查见[业务评测要求](BUSINESS_EVALUATION.md)。

### 按参数查询的隔离订单后端

[tenant-order-cases.json](testdata/tenant-order-cases.json)为六道合成题，覆盖租户同号订单、跨租户不可见、不存在订单、事实提取和资料不足。它使用 `lookup.orders` 替代固定 `evidence`，两者不能混用。模型只获得工具 schema，实际工具返回由查询参数和宿主绑定的租户决定；后端不读取 `answer` 或 `expected_arguments`。

```json
"lookup": {
  "parameters": {
    "type": "object",
    "properties": {"order_id": {"type": "string"}},
    "required": ["order_id"],
    "additionalProperties": false
  },
  "expected_arguments": {"order_id": "SHARED-1"},
  "orders": {
    "tenant_id": "A",
    "records": [
      {"tenant_id": "A", "order_id": "SHARED-1", "text": "A 的订单结果"},
      {"tenant_id": "B", "order_id": "SHARED-1", "text": "B 的订单结果"}
    ]
  }
}
```

后端仅接受非空字符串 `order_id`，拒绝额外参数（包括模型传入的 `tenant_id`）、缺失参数和错误类型。跨租户订单与不存在的订单均返回 `NOT_FOUND`，不泄露跨租户存在性。同租户查错订单会返回那个订单的真实测试记录，由答案和参数评分发现错误。

逐次结果与汇总标注 `lookup_backend=tenant_orders`，记录 `lookup_misses`；不同后端分别汇总，manifest 包含 `tenant_order_cases` 和提示版本 v4。旧固定证据模式保留，历史报告不改写。

```sh
go run ./evaluation/agentbench/cmd/stress -live \
  -provider deepseek -model deepseek-flash \
  -dataset evaluation/agentbench/testdata/tenant-order-cases.json \
  -mode contract -repeats 2 -workers 1 \
  -max-calls 2 -max-output-tokens 2048 -token-stop 8000 -timeout 90s \
  -out /tmp/goagent-tenant-orders-live
```

去掉 `-live` 可生成计划。已完成的[DeepSeek 实测](../../docs/evaluation/2026-10-27-deepseek-tenant-orders/REPORT.md)为 12/12，共 16 次请求、7,068 Token。此后端是隔离测试数据库，租户绑定模拟宿主授权上下文；没有接入真实订单服务、登录身份或生产权限系统。

小题集入口 `go run ./cmd/run -live -dataset testdata/cases.json` 使用环境中的模型与凭据；终态小题集为 `testdata/direct-cases.json`，可加 `-direct-tools`。该入口支持 `-max-calls`、`-max-output-tokens`、`-token-stop`、`-repeats` 和 `-timeout`。

历史报告仅保留 GoAgent 记录，数据清理标记写在 `record_processing`；记录未重新调用模型，汇总按保留记录计算。原有失败仍在。完整说明见 [评测报告](../../docs/evaluation/README.md)。

### 真实 Gemini 测试

先设置 `GEMINI_API_KEY`，从仓库根目录执行：

```sh
go run ./evaluation/agentbench/cmd/stress -live \
  -provider gemini-openai -model gemini-2.5-flash \
  -dataset evaluation/agentbench/testdata/tenant-order-cases.json \
  -mode contract -repeats 2 -workers 1 \
  -max-calls 2 -max-output-tokens 2048 -token-stop 8000 -timeout 90s \
  -out /tmp/goagent-gemini-orders-live
```

这条路径以 OpenAI 协议适配器调用 Google 官方服务，**不是 OpenAI 模型，也不是旧 `GeminiProvider` 的验证**。模型须在当前 Key 可用列表中；本轮选择 `gemini-2.5-flash`。Google 可能将完整工具调用一次性放在结束帧中且省略索引，解析器仅对包含唯一 ID、名称与完整对象参数的独立结束帧补索引；不接受无索引的分片或混合帧。新型号和其他能力需另行验证。

[真实 Gemini 报告](../../docs/evaluation/2026-10-27-gemini-tenant-orders/REPORT.md)保留首次失败、协议诊断、修复后的冒烟和完整题集结果。Token 总量使用供应商报告值，可能包含未拆出的思考 Token，不强行等于输入与可见输出之和。

旧 `GeminiProvider` 已另行修复并完成[原生 API 验证](../../docs/evaluation/2026-10-27-gemini-provider/REPORT.md)，入口为 `go run ./examples/llm/gemini-verify -live`。该验证与此处 `gemini-openai` 路径分别记录。

GeminiProvider 的原生工具循环和 Embedding 已补齐；真实验证运行 `go run ./examples/llm/gemini-verify -live -suite native`，见[能力报告](../../docs/evaluation/2026-10-27-gemini-native-support/REPORT.md)。此处 stress 命令的 `gemini-openai` 保留其兼容端点含义。
