# GeminiProvider 原生 API 验证

直接使用 `providers.NewGeminiWithOptions` 和 `GeminiStreamingProvider`，通过 Google GenAI SDK 调用 Gemini 原生 `generateContent` / `streamGenerateContent`。保留原有公开方法，不使用 OpenAI 兼容端点。

从仓库根目录运行，先设置 `GEMINI_API_KEY`：

```sh
# 默认仅打印计划，不调用模型。
go run ./examples/llm/gemini-verify

# 六个方法各运行两次；结果文件必须不存在。
go run ./examples/llm/gemini-verify -live \
  -model gemini-2.5-flash -repeats 2 \
  -out /tmp/gemini-provider-results.json
```

每次最多 2,048 输出 Token、90 秒超时，无自动重试；合计 12 个生成请求。可通过 `GEMINI_BASE_URL` 指定原生 API 的根地址，默认为 `https://generativelanguage.googleapis.com`；**不要填写 `/v1beta/openai`**。SDK 自动追加 `/v1beta/models/...`。生成请求数量不等于金额上限。

验证项：

- `Complete`：系统提示与数值答案。
- `Chat`：多轮历史与最终答案。
- `Stream`：字符串流的输出内容。
- `GenerateWithTools`：工具名、调用 ID、订单参数，以及宿主执行合成查询。
- `StreamWithTools`：流式工具调用及同样的参数/执行检查。
- `StreamWithContext`：文本、完成事件与供应商用量。

每个方法独立评分，失败也写入 JSON，任一失败使程序以非零状态退出。输出文件权限为 0600，不记录 Key。

`Stream`、`StreamWithTools` 的旧返回类型没有用量字段，报告只累计其他八次调用的已知用量，不能当作总用量。字符串 `Stream` 不能传递中途错误；需要结束状态与错误时使用包装器的 `StreamWithContext`，取消通过调用方 context 观察。

工具是本地只读合成订单查询，不连接生产订单。`-suite legacy`（默认）只验证旧方法，不将工具结果送回模型。当前 Provider 已实现 `llm.StreamClient` / `ToolStreamClient` 与真实 Embedding；完整循环和向量验证使用下面的 `native` 套件。

本轮[真实结果及修复说明](../../../docs/evaluation/2026-10-27-gemini-provider/REPORT.md)保留了修复前的 Vertex AI 认证失败和修复后 12/12 的完整记录。

## 原生工具循环与 Embedding

```sh
go run ./examples/llm/gemini-verify -live -suite native \
  -model gemini-2.5-flash -embedding-model gemini-embedding-001 \
  -repeats 2 -out /tmp/gemini-native-support.json
```

六类案例各运行两次：AsRunnable.Invoke、AsRunnable.Stream、一次查询后生成答案、工具直返、依赖前次结果的两次连续查询、不同文本的真实向量。最多 20 次上游请求，包含 16 次文本生成和 4 次 Embedding；无自动重试。每个工具执行都检查订单参数，连续查询必须按 FIRST-1 → SECOND-2 顺序完成。

Provider 保留模型原始 Content/Parts 和思考签名，通过 `llm.ProviderState` 随 assistant 消息续接。调用方自行管理消息时，必须连同原始内容和 ToolCalls 原样保留该字段；签名数据不拼入用户提示。工具结果按调用 ID 匹配并批量回传，缺失、重复或不匹配时在请求前报错。工具调用仅在完整结束块中交付。

Embedding 独立配置优先级为 `llm.WithEmbeddingModel` / `LLMOptions.EmbeddingModel` → `GEMINI_EMBEDDING_MODEL` → `gemini-embedding-001`，不会把聊天模型当作向量模型。`-embedding-model` 覆盖环境配置。返回维度以供应商模型为准，本轮是 3,072 维。

```go
client, err := providers.NewGeminiWithOptions(
    llm.WithModel("gemini-2.5-flash"),
    llm.WithEmbeddingModel("gemini-embedding-001"),
)
if err != nil { return err }
agent, err := builder.NewSimpleBuilder(client).
    WithTools(lookupTool).
    WithMaxIterations(3).
    Build()
if err != nil { return err }
output, err := agent.AsRunnable().Invoke(ctx, "查询订单的退款期限")
// output.Result 为最终答案，output.TokenUsage 累计工具循环的生成用量。
vector, embedErr := client.Embed(ctx, "需要检索的文本")
```

`Embed` 返回真实 `[]float64`，拒绝空文本、空向量、全零或非有限值。当前接口不暴露 Embedding 用量，报告的已知 Token 仅含文本生成，不能视为全程费用。不同向量和余弦值用于确认接入与非固定返回，不证明检索质量。

最新[原生能力实测](../../../docs/evaluation/2026-10-27-gemini-native-support/REPORT.md)：12/12 通过。之前的旧方法报告保持历史记录，不再代表当前能力上限。
