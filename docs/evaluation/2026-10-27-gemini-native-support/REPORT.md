# Gemini 原生工具循环与真实 Embedding

2026-10-27，已实现 GeminiProvider 的结构化流、AsRunnable 原生工具循环、工具结果续接与真实 Embedding。通过 Gemini 原生 API 实测，六类案例各两次，**12/12 通过**；共 16 次生成请求及 4 次 Embedding 请求。所有结果保存在 [results.json](results.json)，没有筛选最佳结果。

## 实现内容

- 新增 `CompleteStream`、`ChatStream`、`CompleteToolsStream`，统一 `stop` / `tool_calls` 结束原因，输出终态用量；不在流中途交付可执行工具调用。
- 保留完整原生 Content/Parts，包括空文本部分中的签名；通过 `llm.ProviderState` 和 Builder 的 assistant 消息回传。校验原始内容与逻辑 ToolCalls 一致，避免错用或静默丢弃签名。依据 [Google 签名续接要求](https://ai.google.dev/gemini-api/docs/generate-content/thought-signatures)，不合并有签名与无签名的部分。
- 工具响应按逻辑调用 ID 匹配，同时保留供应商原始 ID；一次响应多个调用时合并成一条 user Content，顺序与调用一致。缺失、重复、错配或残缺参数拒绝处理。
- `Embed` 使用 [Google Embedding API](https://ai.google.dev/gemini-api/docs/embeddings)，模型独立于聊天模型。配置优先级：`WithEmbeddingModel` / `LLMOptions.EmbeddingModel` → `GEMINI_EMBEDDING_MODEL` → `gemini-embedding-001`。验证非空、非全零、有限数值，不生成占位向量。
- 能力声明、HasEmbedding / AsEmbedder 与 Builder 的能力检查同步更新；两个 Gemini 构造器均继承支持。

## 真实结果

| 场景 | 通过 | 生成请求 | Embedding 请求 | 生成 Token |
|---|---:|---:|---:|---:|
| AsRunnable.Invoke 普通答案 | 2/2 | 2 | 0 | 191 |
| AsRunnable.Stream 流式答案 | 2/2 | 2 | 0 | 193 |
| 查询 → 工具结果 → 模型最终答案 | 2/2 | 4 | 0 | 704 |
| 终态工具直接返回 | 2/2 | 2 | 0 | 353 |
| 两次连续查询 → 最终答案 | 2/2 | 6 | 0 | 2,376 |
| 两段不同文本向量 | 2/2 | 0 | 4 | 不提供 |
| 合计 | 12/12 | 16 | 4 | 3,817 |

连续查询必须先查 FIRST-1，从工具结果读取关联订单 SECOND-2，再查询关联订单并回答退款期限 7 天。记录包含实际工具执行顺序、模型调用数及用量完整标记，未将正确答案直接作为工具返回代替推理。

Embedding 使用 `gemini-embedding-001`；每次比较中文订单文本与英文天文文本，均得到 **3,072 维、有限、非零且不同的向量**。两者余弦相似度约 0.541369。报告保留输入文本、向量维度和完整向量的 SHA-256，而非打印大数组。该检查验证真实接入和非固定返回，不是检索质量评测。

全部生成用量为输入 2,635、输出 148、供应商总量 3,817 Token。总量可能包含未拆分的思考用量，按供应商报告保留。Embed 接口不暴露用量，因此 **3,817 不是全部 20 次请求的总用量或成本**。

## 复跑

设置 `GEMINI_API_KEY`，从仓库根目录执行：

```sh
go run ./examples/llm/gemini-verify -live -suite native \
  -model gemini-2.5-flash -embedding-model gemini-embedding-001 \
  -repeats 2 -out /tmp/gemini-native-support.json
```

去掉 `-live` 仅显示计划。结果文件必须不存在，文件权限 0600；任一失败会保留结果并以非零状态退出。每生成请求最多 2,048 输出 Token，温度 0.2，每个案例超时 90 秒。普通输出与直返最多 1 次生成、普通循环最多 2 次、连续查询最多 3 次；每次向量案例发起 2 次请求，无 SDK 自动重试。

模型列表预检已确认聊天与向量模型可用。本轮使用 Google GenAI SDK v1.76.0、Go 1.25、基于 `c9d4e6c` 的未提交工作区；提交号不包含本轮修改。接入与代码指纹见 [run-context.json](run-context.json)。

## 回归与边界

Go 1.25 全仓短测试、llm/Builder/agentbench 竞态测试及 golangci-lint v2.6.2 通过。离线端到端测试覆盖单次循环、直返、同轮多个工具、连续工具调用、签名与空签名部分保留、消息 JSON 序列化续接、无原始 ID 的调用匹配；截断、重复 ID、未提供的工具不会执行。向量协议验证独立模型配置、返回类型、取消、空输入、异常或畸形响应。

一次响应多个工具及异常签名边界由本地协议回归验证；真实模型案例覆盖一次和两次连续查询。工具执行是本地合成只读查询，未接入生产订单、支付或权限服务。仅本次列出的模型和场景经过真实调用，不能外推成所有 Gemini 型号或多模态能力的验收。

旧字符串 `Stream` 保留兼容接口，其类型无法携带中途错误；推荐新 `CompleteStream` / `ChatStream` 或 AsRunnable 结构化流。旧阶段[六方法验证报告](../2026-10-27-gemini-provider/REPORT.md)保留原记录，其中“不支持原生循环/Embedding”描述的是实现前状态。
