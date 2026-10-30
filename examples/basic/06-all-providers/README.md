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

本示例直接使用 `llm/providers` 中各 Provider 的构造函数创建客户端：

```go
import "github.com/kart-io/goagent/llm/providers"

client, err := providers.NewOpenAIWithOptions(opts...)
```

## 支持的 Providers

本示例测试以下 providers：

1. **OpenAI** - GPT-3.5, GPT-4
2. **Gemini** - Google Gemini Pro
3. **DeepSeek** - DeepSeek Chat
4. **Anthropic** - Claude 3
5. **Cohere** - Command models
6. **HuggingFace** - Open source models
