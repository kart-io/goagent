# 跨平台 Token 差异原因分析

日期：2026-11-01。依据[三平台真实执行证据](../2026-11-01-platform-token-comparison/REPORT.md)，另增加本地 HTTP 请求抓取，不新增收费模型请求。

结论：目前没有发现 GoAgent 重复请求、重复注入初始提示或重复累计导致这批差异的证据。主要差异已经体现在供应商返回的输入 Token 上；同时各平台输出及额外 Token 的口径、推理默认设置不完全一致。现有证据不能进一步证明某个平台固定增加多少 Token，也不能完全排除未覆盖路径的实现问题。

## 1. 数值拆解

完整任务三平台均为 5 次模型请求、3 次工具调用：

| 平台 | PromptTokens | CompletionTokens | TotalTokens | Total − Prompt − Completion |
| --- | --- | --- | --- | --- |
| Gemini | 683 | 49 | 1020 | 288 |
| DeepSeek | 1485 | 84 | 1569 | 0 |
| MiniMax | 2384 | 186 | 2570 | 0 |

MiniMax 比 DeepSeek 多 1001 Token，其中输入差 899，约占总差异的 89.8%；输出差 102。不能将主要差异解释为“模型回答特别长”。

单 Agent 的初始提示仅 38 个系统提示字符和 14 个用户提示字符，各只有一次请求：Gemini 输入 15、DeepSeek 输入 18、MiniMax 输入 209。DeepSeek 与 MiniMax 输入相差 191；task 内没有工具的 reviewer 也相差 191（25 对 216）。这提示可能存在模型消息模板或服务端固定开销，但只是推断，不能据此宣布 MiniMax 固定加收 191 Token。

不同模型的分词方法会影响计数，供应商实际 usage 是计量依据，见 [DeepSeek 官方说明](https://api-docs.deepseek.com/quick_start/token_usage/)。仅从短文本长度无法反算供应商的全部输入编码开销。

## 2. 代码与本地请求验证

新增 [request_analysis_test.go](https://github.com/kart-io/goagent-examples/blob/main/agent-usage/live/request_analysis_test.go)，使用本地 HTTP 服务器在第一轮拦截请求，以占位凭据生成相同诊断模型名的请求并返回固定 400，避免任何外部调用。它验证：

- single、task 首轮三平台的系统提示正文和用户提示正文逐字相同。
- OpenAI 兼容的 DeepSeek、MiniMax 各只有一条 system 和一条 user；Gemini 只有一个 systemInstruction 和一个 user 内容，没有重复注入初始提示。
- 统一诊断模型名后，DeepSeek 与 MiniMax 的整个请求 JSON 结构和值完全一致，包括工具定义；只是实际端点不同。
- single 请求体 DeepSeek/MiniMax 均为 245 字节，Gemini 为 234；task 初始请求分别为 836、836、810。不能用 HTTP JSON 字节数当 Token，但这些结果排除了这两条首轮路径中客户端生成了数量级不同提示内容的解释。

抓取证据见 [request-payloads.json](request-payloads.json)，仅包含离线合成提示和请求 body，没有实际凭据、认证头或真实业务对话。该检查并未拦截上轮所有真实后续请求，不能冒充全链路逐字相同的证明。

真实执行中，各场景的钩子尝试键唯一，通知数等于请求数，逐尝试累计等于 Agent Self 之和及 Root；最终 task 三平台请求数同为 5，没有被重试次数放大。流式 Provider 取供应商累计 usage，而非逐帧求和；根账本按尝试键登记，不再相加父 Inclusive。

## 3. 统计解释上的代码缺口

Gemini 适配器把 `candidatesTokenCount` 映射为 CompletionTokens，将 `totalTokenCount` 原样保存。根据 [官方 UsageMetadata](https://ai.google.dev/api/generate-content#UsageMetadata)，total 还包括思考与工具相关输入。因此表中 Gemini 的差额 288 不能简单判为漏账，也不能全部命名为“思考 Token”；目前通用接口未单列这些分项，缺少对应证据。

代码保留 TotalTokens 是正确方向，但跨平台解释性不足：Gemini 的 CompletionTokens 是候选输出，不能默认它与其它平台 completion 的细分组成相同。OpenAI 兼容适配器目前也没有单独转交 reasoning token 明细。需要增强字段证据才能精确拆解，而不能用 total 减法伪造供应商未报告的字段。

案例没有显式统一思考预算。MiniMax M3.1-Flash 默认 reasoning_effort=max，并强制思考；温度参数当前不起作用，见 [官方参数说明](https://platform.minimax.cn/docs/api-reference/text-openai-api)。本地抓取显示代码发给三个平台的温度都是 0.7，但这不表示模型实际采用相同采样或思考设置。现有案例适合验证计量闭环，不能当作完全受控的模型效率比较。

## 4. 建议

优先完善可解释性：保留供应商明确报告的 reasoning、工具相关输入及其 KnownFields、实际响应模型名；继续以供应商 TotalTokens 为根累计，不要求输入与可见输出强行相加相等。不引入双版本或第二套账本。

随后另建受控评测：区分无工具单轮、带相同工具 schema 的首轮、完整 Agent 任务；记录实际响应长度、推理配置、轮数与缓存。对能配置的供应商统一限制，对无法统一的参数明确记录差异；重复多次再比较分布。不要把必要的工具历史或推理续接直接删除来降低数字，否则可能破坏后续调用。

本次 MiniMax 完整任务缓存输入 1870/2384，DeepSeek 为 640/1485。缓存是输入子集，既不能重复加到总量，也不能简单从总量扣掉当作“未消费”；费用对比还需要各平台的缓存、输入、输出等定价依据。此次不推算费用。

所有案例包及新请求验证的 race 测试通过，见 [tests.log](tests.log)。这是分析和本地诊断，没有修改核心 Token 统计契约。
