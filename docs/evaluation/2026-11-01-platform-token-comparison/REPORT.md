# 三平台真实模型 Token 消耗对比

日期：2026-11-01。执行 `source ~/.zshrc` 后，使用真实 Gemini、DeepSeek、MiniMax API。结论：**统计链路在三个平台均一致，实际 Token 数量不相同。**

## 相同场景的成功结果

| 平台 / 配置模型名 | 单 Agent | 父子 Agent | 多 Agent + 普通工具 | 三场景总量 |
| --- | --- | --- | --- | --- |
| Gemini / gemini-2.5-flash | 41 | 252 | 1020 | 1313 |
| DeepSeek / deepseek-chat | 19 | 653 | 1569 | 2241 |
| MiniMax / MiniMax-M3.1-Flash | 212 | 1158 | 2570 | 3940 |

表中数字是供应商报告的 TotalTokens，不是估算或货币费用。九个成功场景合计 27 次模型请求、7494 Token。所有 single 场景为 1 次模型请求，child 为 3 次模型请求及 1 次子 Agent 委派；最终 task 场景三平台均为 5 次模型请求及 3 次工具调用。

最终完整任务采用同一提示和工具定义：coordinator 调用 research、review；researcher 使用 lookup_order，以显式 `order_id=synthetic-order` 查询，然后父 Agent 汇总。两个子 Agent 和普通工具均真实执行一次。

这是一轮端到端功能比较，不是模型成本排名或分词器基准。比较的是相同场景，而不是保证逐字相同的模型响应；不同平台的工具序列、推理与输出可以不同。本次最终 task 的请求数量恰好一致，Token 总量仍然不同。同平台重复运行也不保证同一数值，例如上一轮 Gemini 单 Agent 为 44，本轮为 41。

## 统计一致性

九个成功场景全部通过统一入口校验：

- 钩子每次模型尝试用量之和 = 各 Agent Self 用量之和 = 任务 Root。
- 尝试键不重复、证据已记账；钩子通知数 = 模型请求预留数。
- 父 Agent Inclusive = 任务 Root，子调用的父键正确。
- 完整任务必须执行两个子 Agent 和普通工具，不能只生成文字即视为完成。
- 归档时再次核对输入、输出、总量、缓存各字段的已知值之和；未提供的缓存字段不等于已知零。最终九个场景 unknown=0、partial=0，表示基本三字段完整。

Gemini 通过原生 Provider，其余通过 DeepSeek Provider / OpenAI 兼容 Provider。通用字段保留供应商口径，不强制 `输入 + 输出候选 = 总量`。Gemini 总量还包括思考及工具相关 Token，见 [官方 UsageMetadata](https://ai.google.dev/api/generate-content#UsageMetadata)。缓存是独立证据字段，不再次加到 TotalTokens。

机器可读汇总：[comparison.json](comparison.json)，每行含对应原始证据文件；原始文件保留逐尝试、角色、调用树与工具计数。最终账单未核对，不依据本轮 Token 数直接推算平台费用。

## 失败与调整记录

1. [MiniMax 国际端点首轮](minimax-r1.json)返回 HTTP 401，无 usage。环境未设置端点，因此显式改用[官方国内端点](https://platform.minimax.cn/docs/api-reference/text-openai-api) `https://api.minimax.cn/v1`，随后[国内站三场景](minimax-cn-r1.json)通过。没有修改凭据，没有自动跨端点回退；这是独立的显式执行。
2. [DeepSeek 首轮](deepseek-r1.json)的 single、child 通过，task 在普通工具执行前因参数校验失败；记录 718 Token。额外一次[诊断](deepseek-task-diagnostic.json)记录 713 Token，确认无参数工具收到名为 `"{}"` 的额外属性，被严格校验拒绝。没有放宽校验。
3. 将 lookup_order 改为带显式 order_id 的工具，并补充离线用例。先验证原定义拒绝该命名参数，再修改案例定义；相关包 race 测试通过。之后三平台均用新定义重新执行 task，成功结果为表中的 1020、1569、2570。首轮成功的 single、child 定义和逻辑未变，直接复用，未重复付费运行。
4. 所有首轮结果、失败、诊断、最终 task 均保留，未覆盖或隐藏。本批全部执行共预留 43 次模型请求，已知消耗 12871 Token，另有 MiniMax 401 的一条未知 usage；不能据缺少 usage 认定无消费。这些额外执行的用量不混入成功场景比较表。

归档前使用加载后的环境密钥值检查所有 JSON/日志，未保存凭据或真实业务对话。字段及源码摘要见 [checksums.json](checksums.json)。修改仅涉及案例，不更改核心统计 API 或增加平行版本。

## 复现

```sh
source ~/.zshrc
# 模型可替换为对应 MODEL 环境变量；每次使用不存在的新输出目录。
go run ./examples/agent-usage/live -provider gemini -model gemini-2.5-flash -out /tmp/new-usage-gemini
go run ./examples/agent-usage/live -provider deepseek -model deepseek-chat -out /tmp/new-usage-deepseek
MINIMAX_BASE_URL=https://api.minimax.cn/v1 go run ./examples/agent-usage/live -provider minimax -model MiniMax-M3.1-Flash -out /tmp/new-usage-minimax
```

每个场景独立预算、最多 12 次模型请求、每次输出上限 2048、120 秒时限，无自动重试。此次九个通过的结果分别保存在首轮 single/child 与命名参数版本 task，参照 comparison.json 的 evidence 字段定位，不把两种 task 定义混为同轮。

[案例运行说明](https://github.com/kart-io/goagent-examples/blob/main/agent-usage/live/README.md)。
