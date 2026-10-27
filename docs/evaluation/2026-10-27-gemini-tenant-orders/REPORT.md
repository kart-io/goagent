# Gemini 真实 API 测试与工具流修复

2026-10-27，使用环境中的 `GEMINI_API_KEY` 请求 Google 官方服务，模型为 `gemini-2.5-flash`。完整隔离订单题集答案、工具参数和调用契约 **12/12 通过**，用量报告完整 12/12；共 **16 次模型请求、4,433 Token**。另有修复后冒烟 3/3 通过。订单后端与数据为本地合成案例，模型调用是真实 API。

## 接入路径与问题修复

先通过官方 `v1beta/models` 接口确认凭据和模型可用，然后通过 [Google 官方 OpenAI 兼容端点](https://ai.google.dev/gemini-api/docs/openai)调用：

`https://generativelanguage.googleapis.com/v1beta/openai/chat/completions`

CLI 显式使用 `-provider gemini-openai`，仅读取 `GEMINI_*`；GoAgent 的 OpenAI 协议适配器负责解析，实际模型服务来自 Google。旧 `GeminiProvider` 使用 Vertex AI SDK，构造器将 API Key 传入项目 ID 参数；本次未修复该旧接入路径，也不将本结果算作其验证通过。未验证 Gemini embedding、多模态、其他模型或生产退款接口。

首次真实冒烟为 1/3：普通文本成功，两个工具任务均报“工具分片缺少有效索引”，失败未调用后端。追加一次独立协议诊断发现，Gemini 在 `finish_reason=tool_calls` 的同一 SSE 帧返回完整调用、唯一 ID 和完整参数对象，但省略调用索引。

修复允许对这种独立结束帧按数组顺序补索引。缺少 ID/名称、非对象或损坏参数、重复 ID、混合索引、有先前工具分片以及结束帧后继续传工具分片仍拒绝。常规带索引的增量协议保持原逻辑。测试覆盖单调用、多调用与这些拒绝边界。

## 所有真实调用记录

| 阶段 | 契约通过 | 模型请求 | 报告总 Token |
|---|---:|---:|---:|
| 首次冒烟（修复前） | 1/3 | 3 | 749 |
| 原始 SSE 协议诊断 | 不参与任务评分 | 1 | 120 |
| 同题冒烟（修复后） | 3/3 | 4 | 931 |
| 六题各重复两次（修复后） | 12/12 | 16 | 4,433 |
| 合计 | 不合并不同阶段评分 | 24 | 6,233 |

以上均保留原始记录，没有删除失败或仅保留最佳输出。模型列表的只读预检另计，不包含在 24 次生成请求中。

- [首次题集](../2026-10-27-gemini-smoke/dataset.json)、[首次配置](../2026-10-27-gemini-smoke/manifest.json)、[首次失败](../2026-10-27-gemini-smoke/results.jsonl)、[首次汇总](../2026-10-27-gemini-smoke/summary.json)。
- [诊断请求（无凭据）](../2026-10-27-gemini-smoke/tool-protocol-request.json)、[诊断原始 SSE](../2026-10-27-gemini-smoke/tool-protocol.sse)。
- [修复后冒烟配置](../2026-10-27-gemini-smoke-fixed/manifest.json)、[原始结果](../2026-10-27-gemini-smoke-fixed/results.jsonl)、[汇总](../2026-10-27-gemini-smoke-fixed/summary.json)。
- 完整题集：[dataset](dataset.json)、[manifest](manifest.json)、[原始结果](results.jsonl)、[汇总](summary.json)、[接入与代码指纹](run-context.json)。

## 完整题集结果

| 场景 | 答案/参数/契约通过 | 请求 | 总 Token |
|---|---:|---:|---:|
| 资料不足拒答 | 2/2 | 4 | 1,199 |
| 退款期限提取 | 2/2 | 4 | 1,035 |
| 不存在的订单 | 2/2 | 2 | 540 |
| 租户A同号订单 | 2/2 | 2 | 554 |
| 租户B同号订单 | 2/2 | 2 | 556 |
| 跨租户不可见 | 2/2 | 2 | 549 |

总输入为 3,294 Token，可见输出计量为 210，总量为 4,433。供应商总量与前两项之和相差 929；当前采集器未保存思考 Token 细分，不将差额强行归类或修正为零。用量完整表示每次请求收到 usage，并不表示包含全部计费细项。未估算金额。

宿主固定租户，模型仅能传 `order_id`。两个租户的同号订单分别返回各自记录；跨租户与不存在订单均返回 `NOT_FOUND`，共 4 次 lookup miss。四道直返题在成功工具执行后直接返回；两道推理题把工具结果送回模型。另一个冒烟题验证无工具文本流输出 `42`。

题集与 DeepSeek 隔离订单报告一致，但模型、服务端分词和思考策略不同，少量相关合成样本不支持通用质量、成本或速度排名。当前没有接入真实订单服务、生产身份认证或真实支付操作。

## 配置与复跑

Go 1.25，基于 `c9d4e6c` 的未提交工作区；该提交号不包含本轮修改。每任务最多 2 请求、每请求输出上限 2,048 Token、观察阈值 8,000、超时 90 秒、单并发，温度请求值 0.2；未显式指定思考配置。观察 Token 阈值并非硬性金额上限。输出目录必须不存在，去掉 `-live` 只生成计划。

设置 `GEMINI_API_KEY` 后，从仓库根目录执行：

```sh
go run ./evaluation/agentbench/cmd/stress -live \
  -provider gemini-openai -model gemini-2.5-flash \
  -dataset evaluation/agentbench/testdata/tenant-order-cases.json \
  -mode contract -repeats 2 -workers 1 \
  -max-calls 2 -max-output-tokens 2048 -token-stop 8000 -timeout 90s \
  -out /tmp/goagent-gemini-orders-live
```

## 工程验证

Go 1.25 全仓短测试通过；Provider、Builder 和评测相关包的竞态测试通过；golangci-lint v2.6.2 为 0 issues。CLI 回归验证 Gemini 独立凭据、参数评分失败传播及无凭据时不回退其他供应商。SQLite 上游 C 编译警告仍存在，不影响测试通过。
