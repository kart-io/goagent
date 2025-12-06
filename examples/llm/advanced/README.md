# LLM 高级用法示例

本示例演示 LLM 包的高级功能，包括多种客户端配置、流式响应、能力检查、多 Provider 协作和 Token 统计。

## 目录

- [架构设计](#架构设计)
- [核心功能](#核心功能)
- [执行流程](#执行流程)
- [使用方法](#使用方法)
- [配置参考](#配置参考)

## 架构设计

### LLM 客户端架构

```mermaid
graph TB
    subgraph Providers["LLM 提供商"]
        DeepSeek["DeepSeek"]
        OpenAI["OpenAI"]
        Anthropic["Anthropic"]
        Gemini["Gemini"]
        Kimi["Kimi"]
        SiliconFlow["SiliconFlow"]
        Ollama["Ollama"]
    end

    subgraph ClientInterface["客户端接口"]
        Client["llm.Client"]
        StreamClient["StreamClient"]
        ToolCaller["ToolCallingClient"]
        Embedder["EmbeddingClient"]
    end

    subgraph Capabilities["能力扩展"]
        Complete["Complete()"]
        Chat["Chat()"]
        Stream["CompleteStream()"]
        Tools["GenerateWithTools()"]
        Embed["Embed()"]
    end

    subgraph Config["配置系统"]
        Presets["预设配置"]
        UseCases["用例配置"]
        Options["自定义选项"]
    end

    Providers --> Client
    Client --> Complete
    Client --> Chat
    StreamClient --> Stream
    ToolCaller --> Tools
    Embedder --> Embed

    Config --> Client

    style Client fill:#e3f2fd
    style Presets fill:#c8e6c9
```

### 能力检查机制

```mermaid
classDiagram
    class Client {
        <<interface>>
        +Complete(ctx, req) Response
        +Chat(ctx, messages) Response
        +Provider() Provider
        +IsAvailable() bool
    }

    class StreamClient {
        <<interface>>
        +CompleteStream(ctx, req) chan~StreamChunk~
        +ChatStream(ctx, messages) chan~StreamChunk~
    }

    class ToolCallingClient {
        <<interface>>
        +GenerateWithTools(ctx, prompt, tools) ToolCallResponse
        +StreamWithTools(ctx, prompt, tools) chan~ToolChunk~
    }

    class EmbeddingClient {
        <<interface>>
        +Embed(ctx, text) list~float64~
    }

    class CapabilityChecker {
        <<interface>>
        +HasCapability(cap) bool
        +Capabilities() list~Capability~
    }

    Client <|-- StreamClient : 扩展
    Client <|-- ToolCallingClient : 扩展
    Client <|-- EmbeddingClient : 扩展
    Client <|-- CapabilityChecker : 扩展
```

### 多 Provider 协作

```mermaid
sequenceDiagram
    participant App as 应用
    participant Primary as 主 Provider<br/>(DeepSeek)
    participant Fallback as 备用 Provider<br/>(OpenAI)
    participant Validator as 验证器

    App->>Primary: 发送请求

    alt 主 Provider 成功
        Primary-->>App: 返回结果
    else 主 Provider 失败
        Primary-->>App: 返回错误
        App->>Fallback: 切换到备用
        Fallback-->>App: 返回结果
    end

    App->>Validator: 验证结果
    Validator-->>App: 验证通过
```

## 核心功能

### 1. 客户端配置

```mermaid
graph LR
    subgraph Presets["预设配置"]
        Dev["Development<br/>开发调试"]
        Prod["Production<br/>生产环境"]
        Low["LowCost<br/>成本优先"]
        High["HighQuality<br/>质量优先"]
        Fast["Fast<br/>速度优先"]
    end

    subgraph UseCases["用例配置"]
        Chat["Chat<br/>对话聊天"]
        Code["CodeGeneration<br/>代码生成"]
        Trans["Translation<br/>翻译"]
        Sum["Summarization<br/>摘要"]
        Ana["Analysis<br/>分析"]
    end

    subgraph Options["自定义选项"]
        Model["WithModel()"]
        Tokens["WithMaxTokens()"]
        Temp["WithTemperature()"]
        Retry["WithRetryCount()"]
        Rate["WithRateLimiting()"]
    end

    Presets --> Client["LLM Client"]
    UseCases --> Client
    Options --> Client

    style Dev fill:#e3f2fd
    style Prod fill:#c8e6c9
    style Low fill:#fff9c4
```

### 2. 流式响应

```mermaid
sequenceDiagram
    participant App as 应用
    participant Client as StreamClient
    participant LLM as LLM API

    App->>Client: ChatStream(ctx, messages)
    Client->>LLM: 建立流式连接

    loop 流式响应
        LLM-->>Client: StreamChunk
        Client-->>App: chunk (通过 channel)
        Note over App: 实时处理/显示
    end

    LLM-->>Client: 流结束
    Client-->>App: channel 关闭
```

### 3. Token 统计

```mermaid
graph TB
    subgraph Request["请求"]
        Input["输入文本"]
        System["系统提示"]
    end

    subgraph Processing["处理"]
        Tokenize["Token 化"]
        Count["Token 计数"]
        Limit["Token 限制检查"]
    end

    subgraph Response["响应"]
        Output["输出文本"]
        Usage["Token 使用统计"]
        Cost["成本估算"]
    end

    Input --> Tokenize
    System --> Tokenize
    Tokenize --> Count
    Count --> Limit
    Limit --> Output
    Output --> Usage
    Usage --> Cost

    style Count fill:#fff9c4
    style Cost fill:#ffccbc
```

## 执行流程

### 完整执行流程

```mermaid
flowchart TD
    Start["开始"] --> Config["配置 LLM 客户端"]

    Config --> CheckProvider{{"检查 Provider"}}

    CheckProvider --> |"DeepSeek 可用"| UseDeepSeek["使用 DeepSeek"]
    CheckProvider --> |"OpenAI 可用"| UseOpenAI["使用 OpenAI"]
    CheckProvider --> |"都不可用"| UseMock["使用 Mock"]

    UseDeepSeek --> CheckCaps["检查能力"]
    UseOpenAI --> CheckCaps
    UseMock --> CheckCaps

    CheckCaps --> Demo1["演示 1: 配置"]
    Demo1 --> Demo2["演示 2: 流式"]
    Demo2 --> Demo3["演示 3: 能力检查"]
    Demo3 --> Demo4["演示 4: 多 Provider"]
    Demo4 --> Demo5["演示 5: Token 统计"]
    Demo5 --> End["结束"]

    style Start fill:#e3f2fd
    style End fill:#c8e6c9
```

## 使用方法

### 环境配置

```bash
# 使用 DeepSeek (推荐)
export DEEPSEEK_API_KEY="your-api-key"

# 或使用 OpenAI
export OPENAI_API_KEY="your-api-key"

# 未配置 API Key 时将使用模拟演示
```

### 运行示例

```bash
cd examples/llm/advanced
go run main.go
```

### 预期输出

```text
╔════════════════════════════════════════════════════════════════╗
║          LLM 高级用法示例                                       ║
║   展示 LLM 包的高级功能：流式、能力检查、多 Provider 等          ║
╚════════════════════════════════════════════════════════════════╝

【场景 1】LLM 客户端配置
════════════════════════════════════════════════════════════════

场景描述: 展示多种 LLM 客户端的配置方式

1. 使用预设配置 (Presets)
────────────────────────────────────────
  - Development: 适用于开发调试场景
  - Production: 适用于生产环境场景
  - LowCost: 适用于成本优先场景
  ...

【场景 2】流式响应处理
════════════════════════════════════════════════════════════════
✓ 客户端支持流式响应

提示: 用三句话介绍 Go 语言的优势。
响应: Go 语言具有简洁的语法设计...
✓ 流式完成，共 45 个 chunk

【场景 3】能力检查
════════════════════════════════════════════════════════════════
Provider: deepseek
  [✓] 基础补全 (Complete)
  [✓] 聊天对话 (Chat)
  [✓] 流式响应 (Stream)
  [✓] 工具调用 (ToolCalling)
  [✗] 文本嵌入 (Embedding)
```

## 配置参考

### 预设配置对比

| 预设 | Temperature | MaxTokens | 重试 | 适用场景 |
|------|------------|-----------|------|---------|
| Development | 0.8 | 1000 | 3 | 开发调试 |
| Production | 0.7 | 2000 | 5 | 生产环境 |
| LowCost | 0.5 | 500 | 2 | 成本敏感 |
| HighQuality | 0.3 | 4000 | 5 | 质量优先 |
| Fast | 0.5 | 256 | 1 | 低延迟 |

### Provider 对比

| Provider | 中文能力 | 工具调用 | 流式 | 嵌入 | 价格 |
|----------|---------|---------|------|------|------|
| DeepSeek | 优秀 | ✓ | ✓ | ✓ | 低 |
| OpenAI | 良好 | ✓ | ✓ | ✓ | 中 |
| Anthropic | 良好 | ✓ | ✓ | ✗ | 高 |
| Gemini | 良好 | ✓ | ✓ | ✓ | 中 |
| Ollama | 取决于模型 | ✓ | ✓ | ✓ | 免费 |

### 代码示例

#### 创建客户端

```go
// 使用预设
client, _ := providers.NewDeepSeekWithOptions(
    llm.WithPreset(llm.PresetProduction),
    llm.WithAPIKey(os.Getenv("DEEPSEEK_API_KEY")),
)

// 使用用例
client, _ := providers.NewOpenAIWithOptions(
    llm.WithUseCase(llm.UseCaseCodeGeneration),
    llm.WithAPIKey(os.Getenv("OPENAI_API_KEY")),
)

// 自定义配置
client, _ := providers.NewDeepSeekWithOptions(
    llm.WithAPIKey(apiKey),
    llm.WithModel("deepseek-chat"),
    llm.WithMaxTokens(2000),
    llm.WithTemperature(0.7),
    llm.WithRetryCount(3),
)
```

#### 能力检查

```go
// 检查流式能力
if streamClient := llm.AsStreamClient(client); streamClient != nil {
    // 支持流式
    stream := streamClient.ChatStream(ctx, messages)
    for chunk := range stream {
        fmt.Print(chunk.Content)
    }
}

// 检查工具调用能力
if toolCaller := llm.AsToolCaller(client); toolCaller != nil {
    // 支持工具调用
    resp, _ := toolCaller.GenerateWithTools(ctx, prompt, tools)
}
```

#### 流式响应处理

```go
stream := streamClient.ChatStream(ctx, messages)
for chunk := range stream {
    if chunk.Error != nil {
        log.Printf("Stream error: %v", chunk.Error)
        break
    }
    fmt.Print(chunk.Content)
}
```

## 扩展阅读

- [LLM 协作 Agent 示例](../../multiagent/05-llm-collaborative-agents/)
- [LLM 工具调用示例](../../multiagent/06-llm-tool-calling/)
- [工具注册与执行示例](../tools/registry/)
- [llm 包文档](../../../llm/) - LLM 客户端 API 参考
