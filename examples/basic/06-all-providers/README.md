# All Providers Example - 所有 Provider 测试示例

本示例展示如何使用所有支持的 LLM Providers。

## 运行示例

```bash
# 设置 API Keys
export OPENAI_API_KEY="your-openai-key"
export GEMINI_API_KEY="your-gemini-key"
export DEEPSEEK_API_KEY="your-deepseek-key"
export ANTHROPIC_API_KEY="your-anthropic-key"
export COHERE_API_KEY="your-cohere-key"
export HUGGINGFACE_API_KEY="your-huggingface-key"

# 运行示例
go run main.go
```

## 代码说明

本示例使用**传统方式**（直接导入）创建 providers：

```go
import "github.com/kart-io/goagent/llm/providers"

client, err := providers.NewOpenAIWithOptions(opts...)
```

### 新方式：使用 Provider Registry

GoAgent 现在也支持使用 **Provider Registry** 方式动态创建 providers。

使用 Registry 的优势：
- ✓ 运行时动态选择 provider
- ✓ 配置驱动，易于切换
- ✓ 支持 provider fallback
- ✓ 更易于测试和 mock

**Registry 示例代码**:

```go
import (
    _ "github.com/kart-io/goagent/contrib/llm-providers/openai"
    "github.com/kart-io/goagent/llm/registry"
    "github.com/kart-io/goagent/llm/constants"
)

client, err := registry.New(constants.ProviderOpenAI, opts...)
```

## 相关示例

- **本示例**: 传统方式使用多个 providers
- [13-provider-registry](../13-provider-registry/) - Registry 方式的完整示例
- [Provider 使用指南](../../docs/guides/PROVIDER_USAGE_GUIDE.md) - 两种方式的详细对比

## 向后兼容

两种方式**完全向后兼容**，可以在同一项目中共存：

```go
// 传统方式
client1, _ := openai.New(opts...)

// Registry 方式
client2, _ := registry.New(constants.ProviderOpenAI, opts...)

// 两者都可以正常工作
```

## 支持的 Providers

本示例测试以下 providers：

1. **OpenAI** - GPT-3.5, GPT-4
2. **Gemini** - Google Gemini Pro
3. **DeepSeek** - DeepSeek Chat
4. **Anthropic** - Claude 3
5. **Cohere** - Command models
6. **HuggingFace** - Open source models

## 更多信息

- [Registry 完整文档](../../llm/registry/README.md)
- [Contrib Providers](../../contrib/llm-providers/)
- [迁移指南](../../docs/guides/PROVIDER_USAGE_GUIDE.md#迁移指南)
