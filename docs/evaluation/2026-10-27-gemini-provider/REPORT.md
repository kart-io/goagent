# 旧 GeminiProvider 原生 API 验证

2026-10-27，直接验证原有 `NewGeminiWithOptions` 与 `GeminiStreamingProvider` 公开方法。修复前真实请求失败；修复后使用 `gemini-2.5-flash`，六个方法各运行两次，**12/12 通过**。本次走 Gemini 原生 API，没有使用 OpenAI 兼容端点。

## 发现与修复

修复前使用 Vertex AI SDK，并把 API Key 传入项目 ID。一次真实 `Complete` 返回 `Unauthenticated`，明确指出该接口不接受 API Key；[原始失败](before-fix.txt)已脱敏保留。这个失败发生在生成结果之前，没有用量报告，不能把未知用量写成零。

按照 [Google GenAI SDK 迁移说明](https://ai.google.dev/gemini-api/docs/migrate)，将底层改为 `google.golang.org/genai v1.76.0`，显式配置 `BackendGeminiAPI` 与 API Key。保留原有公开构造器和方法，移除不再使用的 Vertex AI SDK 依赖。

同时修复：

- 配置与请求中的系统提示使用 `systemInstruction`；用户最后一条消息只发送一次。
- 请求模型覆盖、输出上限、温度、TopP、Stop 参数逐次构建，不再修改共享模型，避免并发串参。
- 保留工具 JSON Schema 的 `required`、`enum`、`additionalProperties`；无效 Schema 在请求前拒绝。
- 正确读取 SDK `Part.FunctionCall`，生成工具 ID（服务未提供时），保留参数与真实用量。
- 空候选、缺失用量、安全拦截、截断、HTTP 错误和取消不再造成数组越界或伪装成功。SDK 对畸形 `candidates:[null]` 的解析 panic 仅在调用边界转成错误，含同步和流式回归。
- 工具流在完整结束后才交付工具调用；类型化事件流报告错误或完成。`StreamWithContext.total_tokens` 使用供应商用量，不再把文本片段数伪装成 Token 数。
- 尊重 `GEMINI_BASE_URL` 原生根地址，默认模型更新为本次实测的 `gemini-2.5-flash`，显式关闭 SDK 自动重试。

## 真实结果

| 方法 | 通过 | 验证内容 | 用量可见 |
|---|---:|---|---|
| Complete | 2/2 | 系统提示、答案 42 | 是 |
| Chat | 2/2 | 多轮历史、答案 42 | 是 |
| Stream | 2/2 | 文本流输出 42 | 否 |
| GenerateWithTools | 2/2 | lookup、SHARED-1 参数、宿主只读执行 | 是 |
| StreamWithTools | 2/2 | 完整工具调用、参数及宿主只读执行 | 否 |
| StreamWithContext | 2/2 | 文本 42、完成事件和用量 | 是 |

修复后共 12 个方法调用，对应 12 个生成请求，无 SDK 自动重试。另保留修复前的一次失败请求。修复后 8 次调用的接口暴露 usage，已知合计 **752 Token**（输入 418、输出 46）；这是部分用量，**不是全部 12 次的总量**。供应商总量与输入输出之和不同，未采集思考细项，不重算或估算费用。其余 4 次旧流接口不提供用量字段。

所有结果见 [after-fix.json](after-fix.json)，包括逐次答案、工具参数、通过状态、耗时及可见用量。未筛选失败或只保存最佳结果。数据与工具均为合成只读案例，模型请求为真实 Google 服务。

## 复跑与范围

Go 1.25；基于 `c9d4e6c` 的未提交工作区，该提交号不包含本轮修改。配置为每请求最多 2,048 输出 Token，温度 0.2，90 秒超时，串行执行；未显式指定思考配置。代码指纹见 [run-context.json](run-context.json)。

设置 `GEMINI_API_KEY` 后，在仓库根目录运行：

```sh
go run ./examples/llm/gemini-verify -live \
  -model gemini-2.5-flash -repeats 2 \
  -out /tmp/gemini-provider-results.json
```

去掉 `-live` 只显示计划。结果文件必须不存在；任何检查失败返回非零退出码。

验证的是现有 Provider 的六个公开方法。仍未实现 `llm.StreamClient` / `ToolStreamClient`，因此不声称支持 `AsRunnable` 原生工具循环、工具结果消息续接或多轮 thought signature 传递。`Stream` 字符串接口不能承载中途错误，需可靠错误和完成事件时使用 `StreamWithContext`。Embedding、图像、其他型号与 Vertex AI OAuth 路径均未在本轮验证。

这轮结果独立于此前的 [Gemini 兼容端点订单测试](../2026-10-27-gemini-tenant-orders/REPORT.md)，不合并评分或做性能排名。

## 工程回归

Go 1.25 全仓短测试、`llm/...`、Builder 和 agentbench 的竞态测试均通过；golangci-lint v2.6.2 为 0 issues。协议测试使用本地 HTTP 服务，检查原生 URL 与认证头、环境隔离、系统提示、无重复历史、并发参数隔离、Schema 保真、无自动重试、真实结束条件、取消、缺失字段及畸形响应；这些离线检查不算作真实模型运行数。
