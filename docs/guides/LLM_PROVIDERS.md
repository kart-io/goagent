# GoAgent LLM Providers

## 支持的 LLM 提供商

GoAgent 支持多种大语言模型提供商，让你可以灵活选择最适合的 AI 模型。

### 已支持的提供商

| 提供商 | 模型示例 | 配置方式 | 特点 |
|--------|---------|----------|------|
| **OpenAI** | GPT-3.5, GPT-4, GPT-4-Turbo | API Key | 最成熟，功能最全面 |
| **Google Gemini** | Gemini Pro, Gemini Ultra | API Key | Google 的多模态模型 |
| **DeepSeek** | DeepSeek-Coder, DeepSeek-Chat | API Key | 中文优化，编程能力强 |
| **Ollama** ✨ | Llama2, Mistral, Phi, CodeLlama | 本地运行 | 完全本地化，隐私安全 |
| **SiliconFlow** | 各种开源模型 | API Key | 国内服务商 |
| **Kimi** | Kimi Chat | API Key | 长上下文支持 |

### Ollama 支持 (新增)

Ollama 允许你在本地运行大语言模型，无需 API Key，数据完全私有。

#### 安装 Ollama

```bash
# macOS
brew install ollama

# Linux
curl -fsSL https://ollama.ai/install.sh | sh

# Windows
# 下载安装包：https://ollama.ai/download/windows
```

#### 启动 Ollama 服务

```bash
# 启动 Ollama 服务
ollama serve

# 拉取模型（在另一个终端）
ollama pull llama2
ollama pull mistral
ollama pull codellama
ollama pull phi
```

#### 使用 Ollama Provider

```go
package main

import (
    "context"
    "fmt"
    "log"

    "github.com/kart-io/goagent/llm"
    "github.com/kart-io/goagent/llm/providers"
    "github.com/kart-io/goagent/builder"
)

func main() {
    // 创建 Ollama 客户端
    ollamaClient := providers.NewOllamaClientSimple("llama2")

    // 检查 Ollama 是否运行
    if !ollamaClient.IsAvailable() {
        log.Fatal("Ollama is not running. Please start it with: ollama serve")
    }

    // 方式 1: 直接使用客户端
    ctx := context.Background()
    response, err := ollamaClient.Chat(ctx, []llm.Message{
        llm.SystemMessage("You are a helpful assistant."),
        llm.UserMessage("What is Go?"),
    })

    if err != nil {
        log.Fatal(err)
    }

    fmt.Println(response.Content)

    // 方式 2: 创建 Agent
    agent, err := builder.NewAgentBuilder(ollamaClient).
        WithSystemPrompt("You are a helpful AI assistant.").
        WithTools(/* your tools */).
        Build()

    if err != nil {
        log.Fatal(err)
    }

    // 使用 agent...
}
```

#### Ollama 高级配置

```go
// 详细配置
config := providers.DefaultOllamaConfig()
config.BaseURL = "http://localhost:11434"  // 默认地址
config.Model = "mistral"                   // 选择模型
config.Temperature = 0.7                   // 温度参数
config.MaxTokens = 2000                    // 最大 token 数
config.Timeout = 120                       // 超时时间（秒）

client := providers.NewOllamaClient(config)

// 列出可用模型
models, err := client.ListModels()
if err == nil {
    for _, model := range models {
        fmt.Printf("Available model: %s\n", model)
    }
}

// 拉取新模型（如果需要）
err = client.PullModel("codellama")
```

### 支持的 Ollama 模型

| 模型 | 大小 | 用途 | 命令 |
|------|------|------|------|
| **llama2** | 3.8GB / 7B | 通用对话 | `ollama pull llama2` |
| **mistral** | 4.1GB / 7B | 高质量推理 | `ollama pull mistral` |
| **codellama** | 3.8GB / 7B | 代码生成 | `ollama pull codellama` |
| **phi** | 1.6GB / 2.7B | 轻量级模型 | `ollama pull phi` |
| **neural-chat** | 4.1GB / 7B | 对话优化 | `ollama pull neural-chat` |
| **starling-lm** | 4.1GB / 7B | 强化学习优化 | `ollama pull starling-lm` |
| **llama2:13b** | 7.4GB / 13B | 更强的推理能力 | `ollama pull llama2:13b` |
| **llama2:70b** | 39GB / 70B | 最强推理能力 | `ollama pull llama2:70b` |

### 选择合适的 LLM Provider

| 场景 | 推荐 Provider | 原因 |
|------|--------------|------|
| **生产环境** | OpenAI GPT-4 | 最稳定，功能最全 |
| **本地开发** | Ollama | 免费，数据私有 |
| **中文场景** | DeepSeek / Kimi | 中文优化 |
| **代码生成** | DeepSeek-Coder / CodeLlama | 专门优化 |
| **成本敏感** | Ollama / SiliconFlow | 本地或低成本 |
| **多模态** | Google Gemini | 支持图像输入 |

### 示例：切换不同的 Provider

```go
// 根据环境选择 Provider
func createLLMClient() llm.Client {
    env := os.Getenv("LLM_PROVIDER")

    switch env {
    case "ollama":
        // 本地 Ollama
        return providers.NewOllamaClientSimple("llama2")

    case "openai":
        // OpenAI
        return providers.NewOpenAIClient(os.Getenv("OPENAI_API_KEY"))

    case "gemini":
        // Google Gemini
        return providers.NewGeminiClient(os.Getenv("GEMINI_API_KEY"))

    case "deepseek":
        // DeepSeek
        return providers.NewDeepSeekClient(os.Getenv("DEEPSEEK_API_KEY"))

    default:
        // 默认使用 Ollama（本地）
        client := providers.NewOllamaClientSimple("llama2")
        if client.IsAvailable() {
            return client
        }
        // 如果 Ollama 不可用，回退到 OpenAI
        return providers.NewOpenAIClient(os.Getenv("OPENAI_API_KEY"))
    }
}
```

### 完整示例

查看完整的 Ollama 示例：[examples/basic/04-ollama-agent/](examples/basic/04-ollama-agent/)

这个示例展示了：
- 基本的 Ollama 客户端使用
- 创建 Ollama Agent
- 带工具的 Ollama Agent
- 列出和切换模型
- 错误处理和回退策略