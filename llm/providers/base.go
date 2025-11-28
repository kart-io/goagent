package providers

// 本文件中的所有类型和函数别名已被删除（Phase 3 清理）。
// 所有功能已迁移到 llm/common 包，请直接使用：
//
// 类型:
//   - common.BaseProvider (原 providers.BaseProvider)
//   - common.HTTPClientConfig (原 providers.HTTPClientConfig)
//   - common.RetryConfig (原 providers.RetryConfig)
//   - common.ExecuteFunc[T] (原 providers.ExecuteFunc[T])
//   - common.HTTPError (原 providers.HTTPError)
//   - common.ProviderCapabilities (原 providers.ProviderCapabilities)
//   - common.MessageConverter[T] (原 providers.MessageConverter[T])
//   - common.StandardMessage (原 providers.StandardMessage)
//   - common.RoleMapper (原 providers.RoleMapper)
//
// 函数:
//   - common.NewBaseProvider (原 providers.NewBaseProvider)
//   - common.ConfigToOptions (原 providers.ConfigToOptions)
//   - common.DefaultRetryConfig (原 providers.DefaultRetryConfig)
//   - common.ExecuteWithRetry (原 providers.ExecuteWithRetry)
//   - common.NewProviderCapabilities (原 providers.NewProviderCapabilities)
//   - common.MapHTTPError (原 providers.MapHTTPError)
//   - common.RestyResponseToHTTPError (原 providers.RestyResponseToHTTPError)
//   - common.ConvertMessages (原 providers.ConvertMessages)
//   - common.ToStandardMessage (原 providers.ToStandardMessage)
//   - common.ConvertToStandardMessages (原 providers.ConvertToStandardMessages)
//   - common.DefaultRoleMapper (原 providers.DefaultRoleMapper)
//   - common.ConvertMessagesWithRoleMapping (原 providers.ConvertMessagesWithRoleMapping)
//   - common.MessagesToPrompt (原 providers.MessagesToPrompt)
//   - common.DefaultPromptFormatter (原 providers.DefaultPromptFormatter)
//   - common.SecureRandomInt63n (原 providers.secureRandomInt63n)
//
// 迁移示例:
//
//   // 旧代码
//   import "github.com/kart-io/goagent/llm/providers"
//   bp := providers.NewBaseProvider(opts...)
//
//   // 新代码
//   import "github.com/kart-io/goagent/llm/common"
//   bp := common.NewBaseProvider(opts...)
//
// 参考文档:
//   - docs/guides/PROVIDER_BEST_PRACTICES.md
//   - .claude/phase3-deprecation-analysis.md
