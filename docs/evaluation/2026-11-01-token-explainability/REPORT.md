# Token 可解释性修复交付

日期：2026-11-01。实现[差异分析建议](../2026-11-01-token-difference-analysis/REPORT.md)，仍只有一套 API、证据和账本，没有 v1/v2 选择开关。

## 已实现

- 统一 TokenUsage 增加 ReasoningTokens、ToolUsePromptTokens，字段位图、已知零、未知计数、不可变合并、溢出检查、自身/子树/根汇总均覆盖新增细项。总量始终是供应商原值，不加细项、不用差额反推。
- Gemini 提取 thoughts/tool-use 输入；OpenAI 兼容与 DeepSeek 提取 reasoning 明细。SDK scalar 无法区分缺失和零时保守标未知。同步响应和结构化流均保留可用细项。
- StreamChunk 与通知保存 ResponseModel；请求名与实际响应名独立。模型操作日志保留原值，持久回放不重发、不重复累计。
- 显式 ReasoningConfig 支持 effort 或 token budget；nil 继承、非 nil 空设置清除默认。通知记录请求意图，不伪造供应商默认或实际采纳状态。每请求复制配置，RequestHook 不污染默认值。
- 未实现控制的内建 Provider 与未声明验证能力的自定义 Client 明确拒绝设置；模型特有约束仍可能由 API 拒绝。
- OpenAI 工具协议错误终止块保留此前已经收到的用量及模型名，仍保留错误且不执行不完整工具。
- 会话 TurnEnd 复用统一 TokenUsage 并复制整值，不再手写三个字段的独立映射。
- 真实案例输出细项可用性、实际模型和请求配置，并检查钩子、角色、Root 全六字段一致。

[设计契约](../../design/TOKEN_USAGE_EXPLAINABILITY.md)、[使用指南](../../guides/MULTI_AGENT_USAGE.md)、[真实案例](https://github.com/kart-io/goagent-examples/blob/main/agent-usage/live/README.md)。

## 验证

以下命令均完成且退出码为 0：

```sh
env -u GOROOT GOTOOLCHAIN=go1.25.0 go test ./...
env -u GOROOT GOTOOLCHAIN=go1.25.0 go test -race ./builder ./core/session ./core/usage ./core/budget ./llm/providers ./examples/agent-usage/...
```

[全量日志](suite.log)、[race 日志](race.log)。RED 证据记录新增字段/API、持久化钩子、协议错误留证、会话字段和细项比较的原始失败；修改后均通过，不把 RED 当作交付通过。日志仅规范化空白字符。

独立只读审核发现并复核了四项修复：同步 Model 语义保持、每请求配置复制、未支持设置拒绝、协议错误保留已知证据。额外复核了统一会话值类型和全字段比较，未发现阻断问题。审核没有修改文件或执行收费请求。

## 真实模型

加载 `source ~/.zshrc` 后使用实际凭据。Gemini 显式每请求 thinking-budget=128；DeepSeek/MiniMax 显式 reasoning-effort=low。MiniMax 使用国内官方端点。没有自动重试。

| 平台 / 场景 | 请求 | 供应商总量 | 已知推理用量 | 返回推理字段的请求数 | 实际响应模型 | 结果 |
| --- | --- | --- | --- | --- | --- | --- |
| Gemini / task | 5 | 862 | 164 | 3/5 | gemini-2.5-flash | 通过 |
| MiniMax / task | 5 | 2426 | 19 | 3/5 | MiniMax-M3.1-Flash | 通过 |
| DeepSeek / single | 1 | 59 | 14 | 1/1 | deepseek-flash | 通过 |
| DeepSeek / task | 1 | 458 | 20 | 1/1 | deepseek-flash | 模型工具参数校验失败 |

四次场景共 12 次请求、3805 已知 TotalTokens，包含失败场景消费。推理字段未报告的请求仍标未知，不能把表中的已知推理累计当作完整账单。工具相关输入在这批真实响应中未获得非零证据，保持未知；该字段的非零传递通过协议测试（3 Token）验证，不冒充真实调用返回。

DeepSeek 请求名是 deepseek-chat，而响应名是 deepseek-flash，证明新元数据能够揭示别名实际路由。原先仅记录请求名看不到这项差别。供应商若仍返回别名，不把它伪称不可变版本。

DeepSeek task 在父 Agent 第一次响应中生成了名为 parameter 的额外属性，严格 schema 拒绝；两个子 Agent 尚未执行，消费和失败均保留。随后独立执行 single 验证成功路径，没有将 failed task 重新标记通过，也没有把失败证据替换为成功样本。

三次成功场景与失败消费的输入、输出、总量、缓存、推理、工具输入已知值均再次独立核对：逐尝试之和 = Agent Self 之和 = Root。真实响应先于最后一次“全字段校验器”增强，归档时已用独立脚本逐项核验；底层 Token 数据提取代码未变。新配置不同于上一轮默认设置，这批数据不能直接作为严格的节省比例基准。

原始证据：[Gemini](gemini.json)、[MiniMax](minimax.json)、[DeepSeek single](deepseek-single.json)、[DeepSeek task 失败](deepseek-task-failed.json)。相应日志同名 .log；不包含凭据、真实业务对话或推理正文，归档前按加载后的环境密钥值检查。

本次真实运行未执行收费的持久回放；离线回归已验证回放保留模型名、配置、细项且根累计不增加。金额仍由宿主结合供应商定价核对，未引入费用推算。

代码、文档和证据摘要见 [checksums.json](checksums.json)。
