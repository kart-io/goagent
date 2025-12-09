# GoAgent LLM API 参考

本文档提供 GoAgent LLM 客户端的完整 API 参考。

## 目录

- [Client 接口](#client-接口)
- [消息类型](#消息类型)
- [请求和响应](#请求和响应)
- [LLM 选项](#llm-选项)
- [提供商实现](#提供商实现)

---

## Client 接口

### llm.Client

```go
package llm

// Client 定义 LLM 客户端接口
type Client interface {
    // Complete 生成文本补全
    Complete(ctx context.Context, req *CompletionRequest) (*CompletionResponse, error)

    // Chat 进行对话
    Chat(ctx context.Context, messages []Message) (*CompletionResponse, error)

    // Provider 返回提供商类型
    Provider() constants.Provider

    // IsAvailable 检查 LLM 是否可用
    IsAvailable() bool
}
```

---

## 消息类型

### llm.Message

```go
type Message struct {
    // Role 消息角色：system, user, assistant
    Role string `json:"role"`

    // Content 消息内容
    Content string `json:"content"`

    // Name 可选的消息名称
    Name string `json:"name,omitempty"`
}

// 便捷构造函数
func NewMessage(role, content string) Message
func SystemMessage(content string) Message
func UserMessage(content string) Message
func AssistantMessage(content string) Message
```

### 使用示例

```go
import "github.com/kart-io/goagent/llm"

messages := []llm.Message{
    llm.SystemMessage("你是一个有帮助的助手"),
    llm.UserMessage("你好，请介绍一下自己"),
}
```

---

## 请求和响应

### llm.CompletionRequest

```go
type CompletionRequest struct {
    // Messages 消息列表
    Messages []Message `json:"messages"`

    // Temperature 温度参数 (0.0-2.0)
    Temperature float64 `json:"temperature,omitempty"`

    // MaxTokens 最大 token 数
    MaxTokens int `json:"max_tokens,omitempty"`

    // Model 模型名称
    Model string `json:"model,omitempty"`

    // Stop 停止序列
    Stop []string `json:"stop,omitempty"`

    // TopP Top-p 采样
    TopP float64 `json:"top_p,omitempty"`
}
```

### llm.CompletionResponse

```go
type CompletionResponse struct {
    // Content 生成的内容
    Content string `json:"content"`

    // Model 使用的模型
    Model string `json:"model"`

    // TokensUsed 使用的 token 数
    TokensUsed int `json:"tokens_used,omitempty"`

    // FinishReason 结束原因
    FinishReason string `json:"finish_reason,omitempty"`

    // Provider 提供商
    Provider string `json:"provider,omitempty"`

    // Usage 详细的 Token 使用统计
    Usage *interfaces.TokenUsage `json:"usage,omitempty"`
}
```

### interfaces.TokenUsage

```go
type TokenUsage struct {
    // PromptTokens 提示词 token 数
    PromptTokens int `json:"prompt_tokens"`

    // CompletionTokens 补全 token 数
    CompletionTokens int `json:"completion_tokens"`

    // TotalTokens 总 token 数
    TotalTokens int `json:"total_tokens"`
}
```

---

## LLM 选项

### llm.LLMOptions

```go
type LLMOptions struct {
    // 基础配置
    Provider constants.Provider `json:"provider"`
    APIKey   string             `json:"api_key"`
    BaseURL  string             `json:"base_url,omitempty"`
    Model    string             `json:"model"`

    // 生成参数
    MaxTokens      int             `json:"max_tokens"`
    Temperature    float64         `json:"temperature"`
    TopP           float64         `json:"top_p,omitempty"`
    ResponseFormat *ResponseFormat `json:"response_format,omitempty"`

    // 网络配置
    Timeout  int    `json:"timeout"`
    ProxyURL string `json:"proxy_url,omitempty"`

    // 重试配置
    RetryCount int           `json:"retry_count,omitempty"`
    RetryDelay time.Duration `json:"retry_delay,omitempty"`

    // 速率限制
    RateLimitRPM int `json:"rate_limit_rpm,omitempty"`

    // 缓存配置
    CacheEnabled bool          `json:"cache_enabled,omitempty"`
    CacheTTL     time.Duration `json:"cache_ttl,omitempty"`

    // 流式响应
    StreamingEnabled bool `json:"streaming_enabled,omitempty"`

    // 其他配置
    OrganizationID string            `json:"organization_id,omitempty"`
    SystemPrompt   string            `json:"system_prompt,omitempty"`
    CustomHeaders  map[string]string `json:"custom_headers,omitempty"`
}

// DefaultLLMOptions 返回默认配置
func DefaultLLMOptions() *LLMOptions
```

### llm.ResponseFormat

```go
type ResponseFormat struct {
    Type string `json:"type"` // "text" 或 "json_object"
}

// 预定义格式
var (
    ResponseFormatText = &ResponseFormat{Type: "text"}
    ResponseFormatJSON = &ResponseFormat{Type: "json_object"}
)
```

### ClientOption 函数

```go
type ClientOption func(*LLMOptions)

// 基础配置
func WithProvider(provider constants.Provider) ClientOption
func WithAPIKey(apiKey string) ClientOption
func WithBaseURL(baseURL string) ClientOption
func WithModel(model string) ClientOption

// 生成参数
func WithMaxTokens(maxTokens int) ClientOption
func WithTemperature(temperature float64) ClientOption
func WithTopP(topP float64) ClientOption
func WithResponseFormat(format *ResponseFormat) ClientOption

// 网络配置
func WithTimeout(timeout time.Duration) ClientOption
func WithProxy(proxyURL string) ClientOption

// 重试配置
func WithRetryCount(count int) ClientOption
func WithRetryDelay(delay time.Duration) ClientOption

// 其他
func WithOrganizationID(orgID string) ClientOption
func WithSystemPrompt(prompt string) ClientOption
func WithCustomHeaders(headers map[string]string) ClientOption
```

---

## 提供商实现

### constants.Provider

```go
package constants

type Provider string

const (
    ProviderOpenAI      Provider = "openai"
    ProviderAnthropic   Provider = "anthropic"
    ProviderGemini      Provider = "gemini"
    ProviderOllama      Provider = "ollama"
    ProviderCohere      Provider = "cohere"
    ProviderHuggingFace Provider = "huggingface"
    ProviderDeepSeek    Provider = "deepseek"
    ProviderKimi        Provider = "kimi"
    ProviderSiliconFlow Provider = "siliconflow"
)
```

### OpenAI

```go
package providers

// NewOpenAIWithOptions 创建 OpenAI 客户端
func NewOpenAIWithOptions(opts ...ClientOption) (*OpenAIProvider, error)

// 支持的模型
// - gpt-4, gpt-4-turbo, gpt-4o
// - gpt-3.5-turbo
// - text-embedding-ada-002
```

**使用示例：**

```go
import "github.com/kart-io/goagent/llm/providers"

client, err := providers.NewOpenAIWithOptions(
    providers.WithAPIKey(os.Getenv("OPENAI_API_KEY")),
    providers.WithModel("gpt-4"),
    providers.WithTemperature(0.7),
    providers.WithMaxTokens(2000),
)

response, err := client.Complete(ctx, &llm.CompletionRequest{
    Messages: []llm.Message{
        llm.UserMessage("Hello!"),
    },
})
```

### Anthropic (Claude)

```go
// NewAnthropicWithOptions 创建 Anthropic 客户端
func NewAnthropicWithOptions(opts ...ClientOption) (*AnthropicProvider, error)

// 支持的模型
// - claude-3-opus-20240229
// - claude-3-sonnet-20240229
// - claude-3-haiku-20240307
```

**使用示例：**

```go
client, err := providers.NewAnthropicWithOptions(
    providers.WithAPIKey(os.Getenv("ANTHROPIC_API_KEY")),
    providers.WithModel("claude-3-sonnet-20240229"),
)
```

### Google Gemini

```go
// NewGeminiWithOptions 创建 Gemini 客户端
func NewGeminiWithOptions(opts ...ClientOption) (*GeminiProvider, error)

// 支持的模型
// - gemini-pro
// - gemini-pro-vision
// - gemini-1.5-pro
```

**使用示例：**

```go
client, err := providers.NewGeminiWithOptions(
    providers.WithAPIKey(os.Getenv("GOOGLE_API_KEY")),
    providers.WithModel("gemini-pro"),
)
```

### DeepSeek

```go
// NewDeepSeekWithOptions 创建 DeepSeek 客户端
func NewDeepSeekWithOptions(opts ...ClientOption) (*DeepSeekProvider, error)

// 支持的模型
// - deepseek-chat
// - deepseek-coder
```

**使用示例：**

```go
client, err := providers.NewDeepSeekWithOptions(
    providers.WithAPIKey(os.Getenv("DEEPSEEK_API_KEY")),
    providers.WithModel("deepseek-chat"),
)
```

### Ollama (本地部署)

```go
// NewOllamaWithOptions 创建 Ollama 客户端
func NewOllamaWithOptions(opts ...ClientOption) (*OllamaProvider, error)

// 支持任何 Ollama 支持的模型
// - llama2, llama3
// - mistral, mixtral
// - codellama
// - 等等
```

**使用示例：**

```go
client, err := providers.NewOllamaWithOptions(
    providers.WithBaseURL("http://localhost:11434"),
    providers.WithModel("llama3"),
)
```

### Cohere

```go
// NewCohereWithOptions 创建 Cohere 客户端
func NewCohereWithOptions(opts ...ClientOption) (*CohereProvider, error)

// 支持的模型
// - command
// - command-light
// - command-nightly
```

### HuggingFace

```go
// NewHuggingFaceWithOptions 创建 HuggingFace 客户端
func NewHuggingFaceWithOptions(opts ...ClientOption) (*HuggingFaceProvider, error)
```

### Kimi (月之暗面)

```go
// NewKimiWithOptions 创建 Kimi 客户端
func NewKimiWithOptions(opts ...ClientOption) (*KimiProvider, error)

// 支持的模型
// - moonshot-v1-8k
// - moonshot-v1-32k
// - moonshot-v1-128k
```

**使用示例：**

```go
client, err := providers.NewKimiWithOptions(
    providers.WithAPIKey(os.Getenv("KIMI_API_KEY")),
    providers.WithModel("moonshot-v1-32k"),
)
```

### SiliconFlow

```go
// NewSiliconFlowWithOptions 创建 SiliconFlow 客户端
func NewSiliconFlowWithOptions(opts ...ClientOption) (*SiliconFlowProvider, error)
```

---

## 流式响应

### StreamClient

```go
package llm

type StreamClient interface {
    // StreamComplete 流式补全
    StreamComplete(ctx context.Context, req *CompletionRequest) (<-chan *StreamChunk, error)
}

type StreamChunk struct {
    Content      string `json:"content"`
    Done         bool   `json:"done"`
    FinishReason string `json:"finish_reason,omitempty"`
}
```

### 使用示例

```go
import "github.com/kart-io/goagent/llm"

// 假设 client 实现了 StreamClient 接口
streamClient := client.(llm.StreamClient)

chunks, err := streamClient.StreamComplete(ctx, &llm.CompletionRequest{
    Messages: []llm.Message{
        llm.UserMessage("写一首诗"),
    },
})

for chunk := range chunks {
    fmt.Print(chunk.Content)
    if chunk.Done {
        break
    }
}
```

---

## 完整示例

```go
package main

import (
    "context"
    "fmt"
    "os"

    "github.com/kart-io/goagent/llm"
    "github.com/kart-io/goagent/llm/providers"
)

func main() {
    ctx := context.Background()

    // 创建 OpenAI 客户端
    client, err := providers.NewOpenAIWithOptions(
        providers.WithAPIKey(os.Getenv("OPENAI_API_KEY")),
        providers.WithModel("gpt-4"),
        providers.WithTemperature(0.7),
        providers.WithMaxTokens(2000),
        providers.WithTimeout(30 * time.Second),
        providers.WithRetryCount(3),
    )
    if err != nil {
        panic(err)
    }

    // 检查可用性
    if !client.IsAvailable() {
        panic("LLM client not available")
    }

    // 发送请求
    response, err := client.Complete(ctx, &llm.CompletionRequest{
        Messages: []llm.Message{
            llm.SystemMessage("你是一个有帮助的助手"),
            llm.UserMessage("请用一句话介绍 Go 语言"),
        },
    })
    if err != nil {
        panic(err)
    }

    fmt.Printf("Response: %s\n", response.Content)
    fmt.Printf("Tokens used: %d\n", response.TokensUsed)
    fmt.Printf("Provider: %s\n", response.Provider)
}
```

---

## 相关文档

- [核心 API 参考](CORE_API.md)
- [LLM 提供商指南](../guides/LLM_PROVIDERS.md)
- [Provider 最佳实践](../guides/PROVIDER_BEST_PRACTICES.md)
