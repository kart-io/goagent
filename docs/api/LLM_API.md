<!-- generated-by: gsd-doc-writer -->
# GoAgent LLM API 参考

本页以仓库当前导出的 Go 接口为准。基础请求、结构化流和原生工具流是不同能力；选用某个 Provider 不代表它支持所有入口。具体适配器边界见 [Provider 能力表](../guides/PROVIDER_CAPABILITIES.md)。模型目录、服务端功能和价格由供应商决定，本页不维护模型支持清单。

## 客户端接口与推荐入口

以下签名位于 `llm` 包：

```go
type Client interface {
    Complete(context.Context, *CompletionRequest) (*CompletionResponse, error)
    Chat(context.Context, []Message) (*CompletionResponse, error)
    Provider() constants.Provider
    IsAvailable() bool
}

type StreamClient interface {
    Client
    CompleteStream(context.Context, *CompletionRequest) (<-chan *StreamChunk, error)
    ChatStream(context.Context, []Message) (<-chan *StreamChunk, error)
}

type ToolStreamClient interface {
    StreamClient
    CompleteToolsStream(context.Context, *CompletionRequest, []interfaces.Tool) (<-chan *StreamChunk, error)
}
```

| 需求 | 接口或入口 | 边界 |
| --- | --- | --- |
| 直接请求模型 | `Client.Complete/Chat` | 不提供 Agent 工具执行循环 |
| 携带终态、错误与用量的流 | `StreamClient` | Builder 的 `AsRunnable` 要求此接口 |
| Agent 原生工具循环 | `ToolStreamClient` + Builder | 模型选择工具，工具执行由 Builder/Dispatcher 负责 |
| 旧字符串流 | `TokenStreamingClient.Stream` | 缺少结构化终态，不能替代 `StreamClient` |
| 旧 prompt 工具协议 | `ToolCallingClient` | 与原生消息续接协议不同，不是新编排推荐入口 |

`HasStreaming`、`AsStreamClient` 检测结构化流；`HasToolStreaming`、`AsToolStreamClient` 检测原生工具流。它们检查可执行接口，不证明目标端点和模型接受该请求。`HasToolCalling` 还识别原生工具流，因此返回 true 时 `AsToolCaller` 仍可能为 nil，不能用二者互相替代。

`CapabilityChecker` 的 `HasCapability(Capability) bool`、`Capabilities() []Capability` 提供能力声明；声明、实际接口和服务端可用性须分别检查。尚未实现的请求可能返回 `llm.ErrUnsupportedCapability`。`IsAvailable()` 在部分 Provider 中会发起真实网络请求，不是无副作用的本地配置检查，也不能替代业务请求的错误处理。

核验源码：[基础接口](../../llm/client.go)、[流接口](../../llm/stream_client.go)、[能力检测](../../llm/capabilities.go)。

## 消息与原生工具调用

以下为主要字段，省略 JSON 标签；完整定义见 [client.go](../../llm/client.go) 和 [tools.go](../../llm/tools.go)。

```go
type Message struct {
    Role             string
    Content          string
    Name             string
    ToolCalls        []ToolCall
    ReasoningContent string
    ToolCallID       string
    ProviderState    *ProviderState
}

type ProviderState struct {
    Provider constants.Provider
    Data     json.RawMessage
}

type ToolCall struct {
    ID   string
    Type string
    Function struct {
        Name      string
        Arguments string
    }
}
```

`Function.Arguments` 是 JSON 字符串，不是已解析的 map。工具回复通过 `ToolCallID` 对应原始调用 ID，不能只按工具名匹配。`llm/common.ToolCall` 是另一个兼容类型，不能与 `llm.ToolCall` 直接互换。

`ProviderState` 保存供应商原生续接数据，例如签名。宿主改写对话时须保留它与对应 assistant 内容、工具调用的关联；不要把它或 `ReasoningContent` 当作用户答案输出。Builder 原生循环负责续接，通常无需宿主手工重建。

便捷构造函数为 `NewMessage(role, content)`、`SystemMessage(content)`、`UserMessage(content)`、`AssistantMessage(content)`。

## 请求、响应与流终态

以下为完整字段列表，省略 JSON 标签；参数的服务端支持以具体 Provider/模型为准。

```go
type CompletionRequest struct {
    Messages    []Message
    Temperature float64
    MaxTokens   int
    Model       string
    Stop        []string
    TopP        float64
}

type CompletionResponse struct {
    Content      string
    Model        string
    TokensUsed   int
    FinishReason string
    Provider     string
    Usage        *interfaces.TokenUsage
}
```

`StreamChunk` 包含 `Content`（累计内容）、`Delta`（本块增量）、`Role`、`FinishReason`、`Usage *llm.Usage`、`Index`、`Timestamp`、`Done`、`Error`，以及原生工具流的 `ToolCalls`、`ReasoningContent`、`ProviderState`。

- 检查启动错误后持续消费通道，同时检查块内的 `Error`；收到文本不证明请求成功。
- 工具调用只在**无错误的 Done 块**中给出完整 `ToolCalls`，不能执行未完成的参数分片。
- 提前停止读取时取消传给 Provider 的 context；取消后以 `ctx.Err()` 为准，不能依赖一定收到错误块。
- Builder/Runnable 还会验证终态与流关闭契约。自定义实现不能用占位文本或伪造终态掩盖失败。

`CompletionResponse.Usage` 使用 `interfaces.TokenUsage`，含 PromptTokens、CompletionTokens、TotalTokens、CachedTokens；流的 `llm.Usage` 仅含前三项。未提供 usage 时为 nil，代表未知，不可补零或通过字符数推断费用。Provider、模型和响应路径不保证始终返回用量。

## 配置与构造

`llm.ClientOption` 的签名为 `func(*llm.LLMOptions)`。使用 `llm/providers` 的具体构造函数创建客户端：

| 构造函数（参数均为 `...llm.ClientOption`） | 返回类型，另加 `error` |
| --- | --- |
| `NewOpenAIWithOptions` | `*OpenAIProvider` |
| `NewDeepSeekWithOptions` | `*DeepSeekProvider` |
| `NewGeminiWithOptions` | `*GeminiProvider` |
| `NewAnthropicWithOptions` | `*AnthropicProvider` |
| `NewCohereWithOptions` | `*CohereProvider` |
| `NewHuggingFaceWithOptions` | `*HuggingFaceProvider` |
| `NewOllamaWithOptions` | `*OllamaClient` |
| `NewKimiWithOptions` | `*KimiClient` |
| `NewSiliconFlowWithOptions` | `*SiliconFlowClient` |

`NewDeepSeekStreamingWithOptions`、`NewGeminiStreamingWithOptions` 返回相应 StreamingProvider 包装器，当前包装器继承对应基础接口。旧 `llm.NewClientWithOptions` 已弃用：它验证配置后返回错误，不能作为通用 Provider 工厂使用。

| 配置 | 参数 | 说明 |
| --- | --- | --- |
| `WithProvider` | `constants.Provider` | 设置配置中的 Provider 标识，不替代具体构造函数 |
| `WithAPIKey`、`WithBaseURL`、`WithModel` | `string` | 显式配置密钥、端点、模型 |
| `WithEmbeddingModel` | `string` | 供支持独立 Embedding 模型的实现使用 |
| `WithMaxTokens` | `int` | 生成长度参数，不是根执行预算 |
| `WithTemperature`、`WithTopP` | `float64` | 具体支持以 Provider/模型为准 |
| `WithTimeout` | `time.Duration` | 写入 `LLMOptions.Timeout` 的整秒值，正数向上取整 |
| `WithSystemPrompt` | `string` | Provider 配置；Agent 提示词通过 Builder 配置 |
| `WithResponseFormat` | `*llm.ResponseFormat` | Type 为 text/json_object；不等于业务 Schema 验证 |
| `WithJSONResponse` | 无参数 | 配置 JSON 对象模式 |
| `WithRetryCount`、`WithRetryDelay` | `int`、`time.Duration` | Provider 层配置，不代表所有请求路径统一重试 |
| `WithProxy`、`WithOrganizationID` | `string` | 只有实际消费配置的 Provider 路径才生效 |
| `WithCustomHeaders` | `map[string]string` | 合并到配置，须核对具体请求实现 |
| `WithCache` | `bool, time.Duration` | 配置缓存参数，不承诺所有 Provider 实现缓存 |
| `WithRateLimiting`、`WithStreamingEnabled` | `int`、`bool` | 配置字段不是能力探测或执行保证 |

`DefaultLLMOptions()` 默认 MaxTokens=2000、Temperature=0.7、Timeout=60 秒、TopP=1.0；`NewLLMOptionsWithOptions` 创建并应用选项，`ApplyOptions` 修改传入配置。Provider 可以另行设置默认端点和模型。不要从默认值推导服务端模型当前可用性。完整字段见 [LLMOptions](../../llm/client.go)，选项见 [options.go](../../llm/options.go)。

下面是构造片段，实际调用前由宿主提供可用模型配置；不要为了检查配置而隐式调用 `IsAvailable()`：

```go
client, err := providers.NewOpenAIWithOptions(
    llm.WithAPIKey(os.Getenv("OPENAI_API_KEY")),
    llm.WithModel(os.Getenv("OPENAI_MODEL")),
    llm.WithTimeout(30*time.Second),
)
if err != nil {
    return err
}
```

## 离线流式示例

以下完整程序使用内置模拟客户端，无需密钥、不发真实模型请求。它检测终态，并在返回或异常时取消上游。

```go
package main

import (
    "context"
    "fmt"
    "os"
    "time"

    "github.com/kart-io/goagent/llm"
)

func main() {
    if err := run(); err != nil {
        fmt.Fprintln(os.Stderr, err)
        os.Exit(1)
    }
}

func run() error {
    ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
    defer cancel()
    client := llm.NewMockStreamClient()
    chunks, err := client.ChatStream(ctx, []llm.Message{llm.UserMessage("你好")})
    if err != nil {
        return err
    }
    done := false
    for chunk := range chunks {
        if chunk == nil {
            return fmt.Errorf("收到空响应块")
        }
        if done {
            return fmt.Errorf("终态后仍收到响应块")
        }
        if chunk.Error != nil {
            return chunk.Error
        }
        fmt.Print(chunk.Delta)
        done = chunk.Done
    }
    if err := ctx.Err(); err != nil {
        return err
    }
    if !done {
        return fmt.Errorf("响应流缺少终态")
    }
    fmt.Println()
    return nil
}
```

`MockStreamClient` 实现结构化文本流，不提供 `CompleteToolsStream`；验证工具选择需要实现 `ToolStreamClient` 的脚本化模型，不能把这个示例当成工具循环测试。

## 模型预算、重试与接入边界

需要根预算、操作日志、工具执行和回放时，使用 Builder 的 `agent.AsRunnable()`，不要绕过它直接调用 Provider 并假定这些能力自动生效。

当前 Builder 暂时性请求重试入口为 `WithRequestError(n, builder.RetryTransient(n, backoff))`，旧 `WithModelRetry` 已删除。原样重试在持久模型回合内进行，每次尝试独立预留预算；已向消费者输出内容后 RetryTransient 不重试。消息或客户端替换只适用于支持它的非持久原生工具路径。具体规则见 [扩展契约](../guides/EXTENSION_CONTRACT.md)。

Provider 内部、SDK 或宿主额外添加的重试需另外核对；不能把 `RetryCount`、Builder 重试上限和实际传输请求数视为同一个数。请求期限、完整流消费和提前取消由调用方管理；Provider 配置超时不为所有实现提供相同的整段流读取期限。

Embedding 使用 `EmbeddingClient.Embed` 与 `BatchEmbeddingClient.EmbedBatch`；先检查 `HasEmbedding/AsEmbedder`。这些帮助方法同时考虑接口与明确的能力否认，旧 Embed 方法存在不代表该 Provider 默认支持它。

相关参考：[核心 API](CORE_API.md)、[Builder API](../guides/BUILDER_API_REFERENCE.md)、[Provider 配置](../guides/LLM_PROVIDERS.md)、[能力回归](../guides/PROVIDER_CAPABILITIES.md)。
