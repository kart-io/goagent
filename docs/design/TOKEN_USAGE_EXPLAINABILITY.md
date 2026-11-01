# Token 用量可解释性

沿用同一 `interfaces.TokenUsage`、`usage.Fields`、预算账本与通知路径，新增可选字段，不增加版本开关或第二套计量。

## 字段与归属

`ReasoningTokens` 对应供应商明确报告的思考/推理 Token，`ToolUsePromptTokens` 对应明确报告的工具相关输入。字段位为 `usage.Reasoning`、`usage.ToolUsePrompt`，原有四个位不变。`BasicFields` 仍是输入、输出、总量；`AllFields` 包含全部六项。

这些分项按自身字段独立累计，不能再次加进 `TotalTokens`，也不能假设所有供应商的 CompletionTokens 组成一致。TotalTokens 始终保留供应商值。校验、溢出检查、不可变证据合并、Self/Inclusive/Root 均覆盖新增字段。UsageSummary 增加 UnknownReasoningEntries、UnknownToolUsePromptEntries；基本用量 Complete 不要求这些可选细项完整。

SDK 的 scalar 字段不能区分缺失与明确零，因此 Provider 仅将非零细项标为已知；宿主直接提供明确的 KnownFields 时可记录已知零。缺失细项不会用减法推算，也不会覆盖此前流帧已经报告的值。

## 实际模型与显式设置

StreamChunk.ResponseModel 保存供应商响应中的模型名/版本，未报告则为空，不用请求别名填充。用量通知的 Model 保留请求标识，ResponseModel 独立记录响应标识。供应商若仍返回别名，不宣称已拿到不可变模型版本。同步 CompletionResponse 也提供 ResponseModel，保留各 Provider 原有 Model 语义。

`usage.ReasoningConfig` 是请求配置，含 Effort、BudgetTokens、BudgetSet。BudgetSet 区分显式零预算和未设置，-1 表示请求动态预算。配置为值对象，Clone 每请求复制；hook 不会通过修改请求污染 Agent 或客户端默认配置。

`llm.WithReasoning(config)` 设置客户端默认值；`Builder.WithReasoning(config)` 覆盖本 Agent 请求，不自动替换其它子 Builder 的设置。CompletionRequest.Reasoning 为 nil 时继承客户端默认，非 nil 空配置显式清除默认。通知的 ReasoningConfigured 与 Reasoning 记录解析后的显式请求配置，不推断供应商默认值，也不证明上游完全采纳该参数。

Gemini 支持 thinking budget 或受支持的 thinking level；OpenAI 兼容 / DeepSeek 支持 reasoning effort，拒绝 token budget。配置通用校验后，模型特有限制仍可能返回 API 错误。其它内建 Provider 目前明确拒绝该配置；自定义 Client 通过 llm.ReasoningRequestValidator 验证自己真正实现的控制。

参数预算约束单次模型请求，不是任务总 Token 预算；任务准入继续使用 budget.Controller 的统一策略。

## 流、失败与回放

细项和模型名在累计帧之间保留。OpenAI 工具协议出错时，终止块仍携带已经收到的 usage 和模型名，保留错误并禁止执行不完整工具。记录失败请求消费不意味着请求成功。

现有模型操作日志保存同一个 StreamChunk 及解析后的模型 metadata；回放沿用原始实际模型、配置和字段证据，不重新发请求，不重复累计。默认 nil 设置保持持久请求/metadata 的省略语义，不进行隐式格式迁移。

Provider 返回错误、字段未知与模型生成不合法参数仍分别保留；不靠重试或补零制造成功结果。

会话 TurnEnd 用量复用统一 TokenUsage 值类型，复制所有字段后写入，不再维护只含三个字段的独立结构。会话汇总仍不是字段证据账本；字段是否已知以模型通知及归属报告为准。
