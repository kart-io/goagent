package providers

// 本文件中的所有常量和类型别名已被删除（Phase 3 清理）。
// 所有功能已迁移到 llm/common 包，请直接使用：
//
// 常量:
//   - common.DefaultTemperature (原 providers.DefaultTemperature)
//   - common.DefaultMaxTokens (原 providers.DefaultMaxTokens)
//   - common.DefaultTopP (原 providers.DefaultTopP)
//   - common.DefaultFrequencyPenalty (原 providers.DefaultFrequencyPenalty)
//   - common.DefaultPresencePenalty (原 providers.DefaultPresencePenalty)
//
// 类型:
//   - common.ToolCall (原 providers.ToolCall)
//   - common.ToolCallResponse (原 providers.ToolCallResponse)
//   - common.ToolChunk (原 providers.ToolChunk)
//
// 迁移示例:
//
//   // 旧代码
//   import "github.com/kart-io/goagent/llm/providers"
//   temp := providers.DefaultTemperature
//
//   // 新代码
//   import "github.com/kart-io/goagent/llm/common"
//   temp := common.DefaultTemperature
//
// 参考文档:
//   - docs/guides/PROVIDER_BEST_PRACTICES.md
//   - .claude/phase3-deprecation-analysis.md
