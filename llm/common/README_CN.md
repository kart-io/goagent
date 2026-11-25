# common 提供商基础组件

[English](README.md) | [中文](README_CN.md)

`common` 为 LLM 提供商实现提供共享的基础组件：`BaseProvider`（选项处理、环境变量回退、参数解析、HTTP 客户端创建）、指数退避重试、HTTP 状态到错误的映射、能力声明和消息转换辅助函数。[llm/providers](../providers/README_CN.md) 中的内置客户端都嵌入了它。自己编写 `llm.Client` 并希望与内置提供商保持相同的配置和错误行为时使用它；只调用模型的应用代码不需要它。

## 用法

```go
type myProvider struct {
    *common.BaseProvider
    *common.ProviderCapabilities
}

func newMyProvider(opts ...llm.ClientOption) (*myProvider, error) {
    base := common.NewBaseProvider(opts...)
    // 显式选项 -> 环境变量 -> 默认值
    base.ApplyProviderDefaults(constants.Provider("myllm"), "https://api.example.com/v1", "my-model", "MYLLM_BASE_URL", "MYLLM_MODEL")
    if err := base.EnsureAPIKey("MYLLM_API_KEY", constants.Provider("myllm")); err != nil {
        return nil, err
    }
    return &myProvider{
        BaseProvider:         base,
        ProviderCapabilities: common.NewProviderCapabilities(llm.CapabilityCompletion, llm.CapabilityStreaming),
    }, nil
}

func (p *myProvider) send(ctx context.Context, body any) (string, error) {
    client := p.NewHTTPClient(common.HTTPClientConfig{
        BaseURL: p.Config.BaseURL,
        Headers: map[string]string{"Authorization": "Bearer " + p.Config.APIKey},
    })
    return common.ExecuteWithRetry(ctx, common.DefaultRetryConfig(), p.ProviderName(), func(ctx context.Context) (string, error) {
        resp, err := client.R().SetContext(ctx).SetBody(body).Post("/chat")
        if err != nil {
            return "", err
        }
        if resp.IsError() {
            return "", common.MapHTTPError(common.RestyResponseToHTTPError(resp), p.ProviderName(), p.ModelName(), nil)
        }
        return resp.String(), nil
    })
}
```

完整的客户端仍需实现 `llm.Client` 的 `CompleteStream` 与 `Provider()`。

## 主要 API

| 标识符 | 说明 |
|---|---|
| `BaseProvider`、`NewBaseProvider(opts...)` | 在 `Config` 中保存解析后的 `*llm.LLMOptions` |
| `ApplyProviderDefaults`、`EnsureAPIKey`、`EnsureBaseURL`、`EnsureModel` | 按选项、环境变量、默认值的顺序填充提供商身份、基地址、模型和 API key |
| `GetModel`、`GetMaxTokens`、`ResolveTemperature`、`GetTopP`、`GetTimeout`、`WithTimeout` | 结合客户端配置解析请求参数 |
| `ResolveReasoning`、`RejectReasoning` | 解析请求的推理配置，或在不支持的提供商上拒绝 |
| `NewHTTPClient(HTTPClientConfig)` | 返回缓存的 `*httpclient.Client`，合并 `WithCustomHeaders` |
| `RetryConfig`、`DefaultRetryConfig()`、`ExecuteWithRetry` | 对可重试错误做带抖动的指数退避 |
| `IsRetryable(err)` | 对 `*llm.ProviderError` 使用 `llm.ClassifyError`，否则按限流、超时、请求错误码判断 |
| `HTTPError`、`MapHTTPError`、`WithHTTPStatus`、`RestyResponseToHTTPError` | 把非 2xx 响应映射为以 `*llm.ProviderError` 为原因的 `*errors.AgentError` |
| `ProviderCapabilities`、`NewProviderCapabilities` | 嵌入后即实现 `HasCapability` / `Capabilities` |
| `ConvertMessages`、`ConvertMessagesWithRoleMapping`、`ToStandardMessage` | 把 `llm.Message` 切片转换为提供商的消息类型 |
| `RetryAfterDuration`、`ParseRetryAfter`、`GenerateCallID` | Retry-After 解析与工具调用 ID 生成 |

## 行为说明

- 未配置时 `GetMaxTokens` 与 `GetTimeout` 返回 0：不发送 `max_tokens`，不设超时。`GetTemperature` 与 `GetTopP` 回退到 `constants.DefaultTemperature` / `constants.DefaultTopP`；`ResolveTemperature` 尊重显式的 0（`CompletionRequest.TemperatureSet`）。
- 对 OpenAI、DeepSeek、Gemini 以外的提供商，`EnsureAPIKey` 拒绝客户端级推理配置。
- `RetryConfig.Retries` 是首次尝试之后的重试次数，0 表示不重试。重试次数或延迟为负会报错。`DefaultRetryConfig()` 为 3 次尝试。
- `ExecuteWithRetry` 对不可重试错误立即返回；context 取消或到期时停止（返回的 `AgentError` 仍可用 `errors.Is` 匹配 `context.Canceled` / `context.DeadlineExceeded`）；最后一次失败会附带尝试次数。设置 `GO_TEST_MODE=true` 时基础延迟变为 10ms。
- `MapHTTPError` 把 400 映射为无效输入，401/403 映射为 API key 配置无效，404 映射为 LLM 响应错误，429 映射为限流错误，5xx 及其他状态映射为请求错误。`ParseRetryAfter` 回退到 60 秒；`RetryAfterDuration` 对缺失或无法解析的值返回 0。
- `NewHTTPClient` 从全局缓存复用相同配置的客户端；配置中的自定义请求头覆盖 `HTTPClientConfig.Headers`。

## 另见

- [LLM 提供商指南](../../docs/guides/LLM_PROVIDERS_CN.md)
- [Provider 能力边界](../../docs/guides/PROVIDER_CAPABILITIES_CN.md)
- [常量迁移](../../docs/guides/CONSTANTS_MIGRATION_CN.md)
- [llm](../README_CN.md)、[llm/providers](../providers/README_CN.md)
