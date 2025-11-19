# GoAgent LLM Providers

## 支持的 LLM 提供商

GoAgent 支持多种大语言模型提供商，让你可以灵活选择最适合的 AI 模型。

### 已支持的提供商

| 提供商 | 模型示例 | 配置方式 | 特点 |
|--------|---------|----------|------|
| **OpenAI** | GPT-3.5, GPT-4, GPT-4-Turbo | API Key | 最成熟，功能最全面 |
| **Anthropic Claude** ✨ | Claude 3 Opus, Sonnet, Haiku | API Key | 长上下文，安全性强 |
| **Cohere** ✨ | Command, Command-R, Command-R-Plus | API Key | RAG 优化，企业级 |
| **HuggingFace** ✨ | Llama 3, Mixtral, BLOOM, Flan-T5 | API Key | 开源模型，多样选择 |
| **Google Gemini** | Gemini Pro, Gemini Ultra | API Key | Google 的多模态模型 |
| **DeepSeek** | DeepSeek-Coder, DeepSeek-Chat | API Key | 中文优化，编程能力强 |
| **Ollama** | Llama2, Mistral, Phi, CodeLlama | 本地运行 | 完全本地化，隐私安全 |
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

### Anthropic Claude 支持 (新增)

Anthropic Claude 提供高质量的长上下文对话能力，注重安全性和准确性。

#### 支持的 Claude 模型

| 模型 | 上下文 | 特点 | 最佳用途 |
|------|--------|------|---------|
| **claude-3-opus-20240229** | 200K tokens | 最强能力，最高质量 | 复杂任务，深度分析 |
| **claude-3-sonnet-20240229** | 200K tokens | 性能与成本平衡 | 日常对话，代码生成 |
| **claude-3-haiku-20240307** | 200K tokens | 最快响应速度 | 简单任务，实时交互 |

#### 基本使用

```go
package main

import (
    "context"
    "fmt"
    "log"
    "os"

    "github.com/kart-io/goagent/llm"
    "github.com/kart-io/goagent/llm/providers"
)

func main() {
    // 创建 Anthropic 客户端
    client, err := providers.NewAnthropic(&llm.Config{
        APIKey: os.Getenv("ANTHROPIC_API_KEY"),
        Model:  "claude-3-sonnet-20240229", // 默认模型
    })
    if err != nil {
        log.Fatal(err)
    }

    // 发送请求
    ctx := context.Background()
    resp, err := client.Complete(ctx, &llm.CompletionRequest{
        Messages: []llm.Message{
            {Role: "user", Content: "解释量子计算的基本原理"},
        },
        MaxTokens: 1000,
    })
    if err != nil {
        log.Fatal(err)
    }

    fmt.Println(resp.Content)
}
```

#### 环境变量配置

```bash
export ANTHROPIC_API_KEY="your-api-key"
export ANTHROPIC_BASE_URL="https://api.anthropic.com/v1"  # 可选
export ANTHROPIC_MODEL="claude-3-sonnet-20240229"         # 可选
```

#### 完整示例

查看完整示例：[examples/llm/anthropic/main.go](../../examples/llm/anthropic/main.go)

---

### Cohere 支持 (新增)

Cohere 提供企业级 LLM 服务，特别优化了检索增强生成(RAG)能力。

#### 支持的 Cohere 模型

| 模型 | 特点 | 最佳用途 |
|------|------|---------|
| **command** | 标准对话模型 | 通用对话，文本生成 |
| **command-light** | 轻量快速模型 | 实时响应，简单任务 |
| **command-r** | RAG 优化 | 文档检索，知识问答 |
| **command-r-plus** | 增强 RAG 能力 | 复杂检索，多文档分析 |

#### 基本使用

```go
package main

import (
    "context"
    "fmt"
    "log"
    "os"

    "github.com/kart-io/goagent/llm"
    "github.com/kart-io/goagent/llm/providers"
)

func main() {
    // 创建 Cohere 客户端
    client, err := providers.NewCohere(&llm.Config{
        APIKey: os.Getenv("COHERE_API_KEY"),
        Model:  "command",  // 默认模型
    })
    if err != nil {
        log.Fatal(err)
    }

    // 支持对话历史
    ctx := context.Background()
    resp, err := client.Complete(ctx, &llm.CompletionRequest{
        Messages: []llm.Message{
            {Role: "user", Content: "什么是机器学习？"},
            {Role: "assistant", Content: "机器学习是人工智能的一个分支..."},
            {Role: "user", Content: "它有哪些应用？"},
        },
        MaxTokens: 500,
    })
    if err != nil {
        log.Fatal(err)
    }

    fmt.Println(resp.Content)
}
```

#### 环境变量配置

```bash
export COHERE_API_KEY="your-api-key"
export COHERE_BASE_URL="https://api.cohere.ai/v1"  # 可选
export COHERE_MODEL="command"                      # 可选
```

#### 完整示例

查看完整示例：[examples/llm/cohere/main.go](../../examples/llm/cohere/main.go)

---

### HuggingFace 支持 (新增)

HuggingFace Inference API 允许你访问数千个开源模型，无需自己部署。

#### 热门模型推荐

| 模型 | 参数量 | 特点 | 最佳用途 |
|------|--------|------|---------|
| **meta-llama/Meta-Llama-3-8B-Instruct** | 8B | 最新 Llama 3 | 通用对话 |
| **mistralai/Mixtral-8x7B-Instruct-v0.1** | 47B (8x7B MoE) | 混合专家模型 | 复杂推理 |
| **google/flan-t5-xxl** | 11B | Google 指令微调 | 任务执行 |
| **bigscience/bloom** | 176B | 多语言支持 | 多语言生成 |

#### 基本使用

```go
package main

import (
    "context"
    "fmt"
    "log"
    "os"
    "time"

    "github.com/kart-io/goagent/llm"
    "github.com/kart-io/goagent/llm/providers"
)

func main() {
    // 创建 HuggingFace 客户端
    client, err := providers.NewHuggingFace(&llm.Config{
        APIKey:  os.Getenv("HUGGINGFACE_API_KEY"),
        Model:   "meta-llama/Meta-Llama-3-8B-Instruct",
        Timeout: 120,  // 模型加载可能需要较长时间
    })
    if err != nil {
        log.Fatal(err)
    }

    // 首次请求可能需要等待模型加载
    ctx := context.Background()
    resp, err := client.Complete(ctx, &llm.CompletionRequest{
        Messages: []llm.Message{
            {Role: "user", Content: "什么是深度学习？"},
        },
        MaxTokens: 500,
    })
    if err != nil {
        log.Fatal(err)
    }

    fmt.Println(resp.Content)
}
```

#### 模型加载说明

HuggingFace Inference API 采用按需加载模型：
- **冷启动**：首次请求可能需要 20-60 秒加载模型
- **自动重试**：Provider 会自动重试最多 5 次
- **建议超时**：设置 120-180 秒的超时时间

#### 环境变量配置

```bash
export HUGGINGFACE_API_KEY="your-api-key"
export HUGGINGFACE_BASE_URL="https://api-inference.huggingface.co"  # 可选
export HUGGINGFACE_MODEL="meta-llama/Meta-Llama-3-8B-Instruct"      # 可选
```

#### 完整示例

查看完整示例：[examples/llm/huggingface/main.go](../../examples/llm/huggingface/main.go)

---

### 选择合适的 LLM Provider

| 场景 | 推荐 Provider | 原因 |
|------|--------------|------|
| **生产环境** | OpenAI GPT-4 / Claude 3 Opus | 最稳定，功能最全 |
| **长上下文任务** | Anthropic Claude 3 | 200K token 上下文 |
| **RAG/检索应用** | Cohere Command-R | RAG 专门优化 |
| **开源模型** | HuggingFace | 数千个模型可选 |
| **本地开发** | Ollama | 免费，数据私有 |
| **中文场景** | DeepSeek / Kimi | 中文优化 |
| **代码生成** | DeepSeek-Coder / CodeLlama | 专门优化 |
| **成本敏感** | Ollama / HuggingFace | 本地或按需付费 |
| **多模态** | Google Gemini | 支持图像输入 |

### 示例：切换不同的 Provider

```go
// 根据环境选择 Provider
func createLLMClient() llm.Client {
    env := os.Getenv("LLM_PROVIDER")

    switch env {
    case "anthropic":
        // Anthropic Claude
        client, _ := providers.NewAnthropic(&llm.Config{
            APIKey: os.Getenv("ANTHROPIC_API_KEY"),
        })
        return client

    case "cohere":
        // Cohere
        client, _ := providers.NewCohere(&llm.Config{
            APIKey: os.Getenv("COHERE_API_KEY"),
        })
        return client

    case "huggingface":
        // HuggingFace
        client, _ := providers.NewHuggingFace(&llm.Config{
            APIKey: os.Getenv("HUGGINGFACE_API_KEY"),
        })
        return client

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