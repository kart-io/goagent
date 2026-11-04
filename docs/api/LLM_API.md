<!-- generated-by: gsd-doc-writer -->
# GoAgent LLM API 参考

本文概述 `llm`、`llm/providers`、`llm/common`、`llm/constants` 的公开 API。完整签名以 Go 文档为准：

```bash
go doc -all github.com/kart-io/goagent/llm
go doc -all github.com/kart-io/goagent/llm/providers
```

各 Provider 实际实现的能力见 [Provider 能力边界](../guides/PROVIDER_CAPABILITIES.md)，配置与默认值见 [LLM 提供商指南](../guides/LLM_PROVIDERS.md)。

## 接口

### llm.Client

所有 Provider 都实现的基础接口，只有流式补全和提供商标识：

```go
type Client interface {
    CompleteStream(ctx context.Context, req *CompletionRequest) (<-chan *StreamChunk, error)
    Provider() constants.Provider
}
```

需要完整响应时用包级函数收集流：

```go
func Complete(ctx context.Context, client Client, req *CompletionRequest) (*CompletionResponse, error)
func Chat(ctx context.Context, client Client, messages []Message) (*CompletionResponse, error)
```

`llm.Complete` 只把无错误的 `Done` 块视为完成：块上的 `Error` 原样返回，流在终止块前关闭返回 `io.ErrUnexpectedEOF`；返回前取消内部 ctx。`llm.Chat` 等价于以 `Messages` 调用 `llm.Complete`。

自定义客户端只需实现 `CompleteStream`；仅有同步实现时，可把结果作为一个块返回（`Delta`、`Content`、`FinishReason: "stop"`、`Done: true`）。接口不再提供 `IsAvailable`；检查 Ollama 等 OpenAI 兼容端点是否可达用 `OpenAIProvider.ListModels(ctx)`（读取 `/models`，不计费）。

### 可选接口

| 接口 | 方法 | 检测 / 转换 |
|---|---|---|
| `ToolStreamClient` | `Client` + `CompleteToolsStream(ctx, req, tools)` | `HasToolStreaming`、`AsToolStreamClient` |
| `EmbeddingClient` | `Embed` | `HasEmbedding`、`AsEmbedder` |
| `CapabilityChecker` | `HasCapability`、`Capabilities` | 类型断言 |

`FullClient` 组合 `ToolStreamClient`、`EmbeddingClient`、`CapabilityChecker`。

检测规则：

- `HasToolStreaming` 只检查接口实现，不看能力声明。Builder 接受任意 `llm.Client`；配置工具或 `WithReturnDirectTools` 时要求 `ToolStreamClient`，否则 Build 报错。
- `HasEmbedding` / `AsEmbedder` 同时要求实现 `EmbeddingClient`，且（若实现了 `CapabilityChecker`）声明了 `CapabilityEmbedding`。
- `ErrUnsupportedCapability` 表示 Provider 没有实现该操作。

### 能力常量

`Capability` 取值：`CapabilityCompletion`、`CapabilityChat`、`CapabilityStreaming`、`CapabilityStructuredStreaming`、`CapabilityToolStreaming`、`CapabilityToolCalling`、`CapabilityEmbedding`、`CapabilityVision`、`CapabilityJSON`。

`CapabilityStreaming` 只表示有某种流式输出（可能仅是字符串流）；结构化流与原生工具流分别看 `CapabilityStructuredStreaming` 和 `CapabilityToolStreaming`。能力声明描述适配器实现，不保证任意模型或自定义端点可用。

## 消息、请求与响应

```go
type Message struct {
    Role             string     // "system"、"user"、"assistant"、"tool"
    Content          string
    Name             string
    ToolCalls        []ToolCall // assistant 发起的工具调用
    ReasoningContent string     // 工具回合原生续接所需的推理内容
    ToolCallID       string     // tool 消息对应的调用 ID
    ProviderState    *ProviderState // 不透明的原生响应片段（如 Gemini 签名），须原样回放
}

func NewMessage(role, content string) Message
func SystemMessage(content string) Message
func UserMessage(content string) Message
func AssistantMessage(content string) Message

type CompletionRequest struct {
    Messages    []Message
    Temperature float64
    TemperatureSet bool // Go 结构体显式指定零时设为 true；不生成 JSON 字段
    MaxTokens   int
    Model       string   // 为空时使用客户端配置的模型
    Stop        []string
    TopP        float64
}

type CompletionResponse struct {
    Content      string
    Model        string
    TokensUsed   int
    FinishReason string
    Provider     string
    Usage        *interfaces.TokenUsage // 可能为 nil，读取前判空
}
```

温度的优先级为请求、客户端配置、默认值 `0.7`。`llm.WithTemperature(0)` 和 Builder 的 `WithTemperature(0)` 均保留显式零。直接构造请求时使用 `CompletionRequest{Temperature: 0, TemperatureSet: true}`；直接构造客户端配置时也需设置 `LLMOptions.TemperatureSet`。JSON 中 `"temperature":0` 自动标记为显式设置，缺省字段保持原值，`null` 清除设置。旧的零值请求继续回落到客户端配置。Builder 始终提供其温度（默认 `0.7`），因此需要零温时也应在 Builder 上设置。

自定义 Provider 可调用 `BaseProvider.ResolveTemperature(req)` 保留请求的显式零；旧的 `GetTemperature(float64)` 接口无法区分零与缺省。

`ToolCall` 的结构为 `ID`、`Type`（`"function"`）和 `Function{Name, Arguments}`，`Arguments` 是 JSON 字符串。

## 流式

```go
type StreamChunk struct {
    ProviderState    *ProviderState
    ToolCalls        []ToolCall // 仅在成功的终止块中给出本轮完整工具调用
    ReasoningContent string
    Content          string     // 累积内容
    Delta            string     // 本块增量
    Role             string
    FinishReason     string
    Usage            *Usage     // 终止块上的用量
    Index            int
    Timestamp        time.Time
    Done             bool
    Error            error
}
```

- 内置结构化流的终止块 `FinishReason` 为 `stop` 或 `tool_calls`；流中错误放在 `Error` 中。
- 不能执行尚未完成的工具参数分片，只使用无错误的 `Done` 块里的 `ToolCalls`。
- 提前停止读取时必须取消 ctx，否则 goroutine 与连接不会释放。

辅助类型：`StreamReader`（`NewStreamReader`，提供 `ReadAll`、`ReadChunks`、`CollectDeltas`）、`StreamWriter`（`NewStreamWriter`）、`StreamOptions` / `DefaultStreamOptions`，以及测试用的 `MockStreamClient`。

```go
func streamAnswer(ctx context.Context, client llm.Client, prompt string) error {
    ctx, cancel := context.WithCancel(ctx)
    defer cancel() // 提前返回时释放流
    chunks, err := client.CompleteStream(ctx, &llm.CompletionRequest{
        Messages: []llm.Message{llm.UserMessage("写一首诗")},
    })
    if err != nil {
        return err
    }
    for chunk := range chunks {
        if chunk.Error != nil {
            return chunk.Error
        }
        fmt.Print(chunk.Delta)
    }
    return nil
}
```

## 错误分类

```go
type ProviderError struct {
    StatusCode int           // HTTP 状态码；0 表示没有响应或流在开始后中断
    RetryAfter time.Duration // 服务端建议的等待时长，0 表示未给出
    Err        error
}
func (e *ProviderError) Retryable() bool // 429、5xx 与暂时性连接错误

func ClassifyError(err error) ClassifiedError

type ClassifiedError struct {
    Reason         ErrorReason // rate_limit、server、network、context_length、auth、not_found、bad_request、canceled、unknown
    Retryable      bool        // 原样重试可能成功
    ShouldCompress bool        // 超出上下文窗口，缩短消息后可能成功
    ShouldFallback bool        // 换模型或提供方可能成功
}
```

`ClassifyError` 先识别取消与上下文超长的错误文本，再用 `errors.As` 查找 `*ProviderError`。各 Provider 的流式与非流式请求失败（HTTP 非 2xx 与连接错误）都构造 `ProviderError`；响应解码失败等错误只能分类为 `unknown`。Builder 的 `RetryTransient` 基于此分类，见 [核心 API](CORE_API.md#模型请求恢复)。

## 配置选项

`ClientOption` 修改 `LLMOptions`；`DefaultLLMOptions()` 默认 `MaxTokens` 2000、`Temperature` 0.7、`Timeout` 60 秒。

| 选项 | 说明 |
|---|---|
| `WithAPIKey`、`WithBaseURL`、`WithModel` | 为空时依次回退到 Provider 的环境变量和默认值 |
| `WithEmbeddingModel` | 独立的向量模型，目前 Gemini 使用 |
| `WithMaxTokens`、`WithTemperature`、`WithTopP` | 请求未指定时使用的默认值 |
| `WithResponseFormat`、`WithJSONResponse` | `ResponseFormatText` / `ResponseFormatJSON`；目前只有 DeepSeek 的非流式请求读取 |
| `WithSystemPrompt` | 前置到每次请求的系统提示 |
| `WithTimeout(time.Duration)` | 按整秒保存，正数向上取整；语义见 [最佳实践](../guides/PROVIDER_BEST_PRACTICES.md#超时配置) |
| `WithCustomHeaders` | 附加到所有内置 Provider 的请求，覆盖 SDK 设置的同名头 |
| `WithProvider` | 由构造函数自行设置，通常不需要 |

模型调用的重试应在 Builder 层用 `WithRequestError` + `RetryTransient` 配置。DeepSeek 的非流式 HTTP 调用内置固定次数的重试，不可配置；OpenAI、Gemini 与 Anthropic 不做隐式重试。

## Provider 构造函数

`llm/providers` 中每个构造函数都只校验配置、不发请求：

| 构造函数 | 返回类型 | `constants.Provider` |
|---|---|---|
| `NewOpenAIWithOptions` | `*OpenAIProvider` | `ProviderOpenAI` |
| `NewAnthropicWithOptions` | `*AnthropicProvider` | `ProviderAnthropic` |
| `NewGeminiWithOptions` | `*GeminiProvider` | `ProviderGemini` |
| `NewDeepSeekWithOptions` | `*DeepSeekProvider` | `ProviderDeepSeek` |
| `NewOllamaWithOptions`、`NewOllamaClientSimple(model)` | `*OpenAIProvider` | `ProviderOllama` |
| `NewKimiWithOptions` | `*OpenAIProvider` | `ProviderKimi` |
| `NewSiliconFlowWithOptions` | `*OpenAIProvider` | `ProviderSiliconFlow` |

`NewDeepSeekStreamingWithOptions` / `NewDeepSeekStreaming(*LLMOptions)` 与 `NewGeminiStreamingWithOptions` / `NewGeminiStreaming(*LLMOptions)` 返回嵌入基础 Provider 的包装类型，能力与基础 Provider 相同。

`constants.ProviderCustom` 没有对应的构造函数。

## llm/common

供 Provider 实现复用：`BaseProvider`（`NewBaseProvider`、`ApplyProviderDefaults`、`EnsureAPIKey`、`NewHTTPClient` 等）、`ProviderCapabilities`、`ExecuteWithRetry` / `RetryConfig`、`MapHTTPError`、`ConfigToOptions`，以及消息转换函数。自定义 Provider 可以参考 `llm/providers` 中任一实现。

以下完整程序使用OpenAI Provider，需要配置OPENAI_API_KEY，运行时会实际调用模型。文档校验脚本只编译该程序，不执行模型请求。

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
    client, err := providers.NewOpenAIWithOptions(
        llm.WithAPIKey(os.Getenv("OPENAI_API_KEY")),
        llm.WithModel("gpt-4o"),
        llm.WithMaxTokens(2000),
        llm.WithTimeout(30*time.Second),
    )
    if err != nil {
        log.Fatal(err)
    }

    resp, err := llm.Complete(context.Background(), client, &llm.CompletionRequest{
        Messages: []llm.Message{
            llm.SystemMessage("你是一个有帮助的助手"),
            llm.UserMessage("请用一句话介绍 Go 语言"),
        },
    })
    if err != nil {
        log.Fatal(err)
    }
    fmt.Println(resp.Content)
    if resp.Usage != nil {
        fmt.Println("总 Token:", resp.Usage.TotalTokens)
    }
}
```

## 相关文档

- [核心 API 参考](CORE_API.md)
- [LLM 提供商指南](../guides/LLM_PROVIDERS.md)
- [Provider 最佳实践](../guides/PROVIDER_BEST_PRACTICES.md)
- [Provider 能力边界](../guides/PROVIDER_CAPABILITIES.md)
