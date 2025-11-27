# Kimi Provider for GoAgent

Kimi (Moonshot AI) provider implementation for the GoAgent framework, featuring ultra-long context support up to 200K tokens.

## Features

- **Ultra-Long Context**: Support for up to 200K tokens (moonshot-v1-128k model)
- **Multiple Models**: 8K, 32K, and 128K context window options
- **OpenAI-Compatible API**: Familiar request/response format
- **Token Estimation**: Built-in token counting for context management
- **Model Management**: List and query available models
- **Chinese Optimization**: Optimized for Chinese language processing

## Installation

```bash
go get github.com/kart-io/goagent/contrib/llm-providers/kimi
```

## Prerequisites

1. Sign up for a Kimi account at [moonshot.ai](https://platform.moonshot.cn/)
2. Get your API key from the dashboard
3. Set the API key as an environment variable or pass it in code

## Usage

### Basic Usage

```go
package main

import (
    "context"
    "fmt"
    "log"

    "github.com/kart-io/goagent/contrib/llm-providers/kimi"
    agentllm "github.com/kart-io/goagent/llm"
)

func main() {
    // Create Kimi provider
    provider, err := kimi.New(
        agentllm.WithAPIKey("your-api-key"),
        agentllm.WithModel("moonshot-v1-8k"),
    )
    if err != nil {
        log.Fatal(err)
    }

    // Create completion request
    req := &agentllm.CompletionRequest{
        Messages: []agentllm.Message{
            {Role: "user", Content: "What is the capital of China?"},
        },
    }

    // Send request
    resp, err := provider.Complete(context.Background(), req)
    if err != nil {
        log.Fatal(err)
    }

    fmt.Printf("Response: %s\n", resp.Content)
    fmt.Printf("Tokens: %d\n", resp.TokensUsed)
}
```

### Using Long Context Models

```go
// For processing long documents (up to 128K tokens)
provider, err := kimi.New(
    agentllm.WithAPIKey("your-api-key"),
    agentllm.WithModel("moonshot-v1-128k"),
)

// Process a very long document
longDocument := readLongDocument() // Up to 128K tokens
messages := []agentllm.Message{
    {Role: "system", Content: "You are a document analyzer."},
    {Role: "user", Content: longDocument},
    {Role: "user", Content: "Please summarize the key points."},
}

resp, err := provider.Chat(context.Background(), messages)
```

### Environment Variables

```bash
export KIMI_API_KEY="your-api-key"
export KIMI_BASE_URL="https://api.moonshot.cn/v1"
export KIMI_MODEL="moonshot-v1-8k"
```

Then use without explicit configuration:

```go
provider, err := kimi.New() // Uses environment variables
```

### Configuration Options

```go
provider, err := kimi.New(
    agentllm.WithAPIKey("your-api-key"),
    agentllm.WithModel("moonshot-v1-32k"),
    agentllm.WithTemperature(0.7),
    agentllm.WithMaxTokens(2000),
    agentllm.WithTimeout(30 * time.Second),
)
```

## Available Models

### Context Window Comparison

| Model | Context Window | Use Case |
|-------|---------------|----------|
| `moonshot-v1-8k` | 8,000 tokens | Short conversations, quick queries |
| `moonshot-v1-32k` | 32,000 tokens | Medium documents, multi-turn chats |
| `moonshot-v1-128k` | 128,000 tokens | Long documents, books, extensive analysis |

### Choosing the Right Model

```go
// Short queries and conversations
provider, _ := kimi.New(
    agentllm.WithModel("moonshot-v1-8k"),
)

// Medium-length documents
provider, _ := kimi.New(
    agentllm.WithModel("moonshot-v1-32k"),
)

// Very long documents (research papers, books)
provider, _ := kimi.New(
    agentllm.WithModel("moonshot-v1-128k"),
)
```

## Advanced Features

### Token Estimation

```go
// Estimate token count before sending
text := "你好，世界！Hello, World!"
estimatedTokens := provider.EstimateTokenCount(text)
fmt.Printf("Estimated tokens: %d\n", estimatedTokens)

// Validate context size
messages := []agentllm.Message{
    {Role: "user", Content: longText},
}
if err := provider.ValidateContextSize(messages); err != nil {
    log.Printf("Context too long: %v", err)
}
```

### Model Information

```go
// Get supported models
supportedModels := provider.GetSupportedModels()
// ["moonshot-v1-8k", "moonshot-v1-32k", "moonshot-v1-128k"]

// Get context size for a model
contextSize := provider.GetModelContextSize("moonshot-v1-128k")
// 128000

// List available models from API
models, err := provider.ListModels()
if err != nil {
    log.Fatal(err)
}
for _, model := range models {
    fmt.Println(model)
}
```

### File Upload Token Calculation

```go
// Calculate tokens for file content
fileContent := readFile("document.txt")
fileTokens := provider.CalculateFileUploadTokens(fileContent)
fmt.Printf("File will use approximately %d tokens\n", fileTokens)
```

### Fluent API

```go
resp, err := provider.
    WithModel("moonshot-v1-32k").
    WithTemperature(0.3).
    WithMaxTokens(1000).
    Chat(ctx, messages)
```

## Migration from llm/providers

If you're migrating from the old import path:

```go
// Old import
import "github.com/kart-io/goagent/llm/providers"
provider := providers.NewKimiWithOptions(...)

// New import (recommended)
import "github.com/kart-io/goagent/contrib/llm-providers/kimi"
provider := kimi.New(...)

// Old import still works (backward compatible)
import "github.com/kart-io/goagent/llm/providers"
provider := providers.NewKimiWithOptions(...)
```

## Error Handling

```go
resp, err := provider.Complete(ctx, req)
if err != nil {
    switch {
    case strings.Contains(err.Error(), "invalid API key"):
        log.Println("Invalid API key - check your credentials")
    case strings.Contains(err.Error(), "rate limit"):
        log.Println("Rate limit exceeded - wait before retrying")
    case strings.Contains(err.Error(), "context size"):
        log.Println("Input too long - use a larger model")
    default:
        log.Printf("Request failed: %v", err)
    }
    return
}
```

## Best Practices

### 1. Choose Appropriate Model for Task

```go
// Use 8k for simple queries (faster, cheaper)
quickProvider, _ := kimi.New(agentllm.WithModel("moonshot-v1-8k"))

// Use 128k for long document analysis
longProvider, _ := kimi.New(agentllm.WithModel("moonshot-v1-128k"))
```

### 2. Validate Context Before Sending

```go
if err := provider.ValidateContextSize(messages); err != nil {
    // Handle context overflow
    log.Printf("Context too large: %v", err)
    // Option 1: Truncate messages
    // Option 2: Use larger model
    // Option 3: Summarize and retry
}
```

### 3. Estimate Costs with Token Counting

```go
totalTokens := 0
for _, msg := range messages {
    totalTokens += provider.EstimateTokenCount(msg.Content)
}
fmt.Printf("Estimated input tokens: %d\n", totalTokens)
```

### 4. Handle Long Documents Efficiently

```go
// For documents longer than 32K tokens
if documentTokens > 32000 {
    provider, _ = kimi.New(
        agentllm.WithModel("moonshot-v1-128k"),
        agentllm.WithTimeout(2 * time.Minute), // Longer timeout
    )
}
```

## Performance Considerations

- **Model Selection**: Larger context models are slower and more expensive
- **Token Counting**: Use `EstimateTokenCount()` to optimize requests
- **Timeout Configuration**: Set appropriate timeouts for long documents
- **Rate Limits**: Respect API rate limits to avoid throttling

## Troubleshooting

### Invalid API Key

Ensure your API key is correct and has not expired:
```bash
export KIMI_API_KEY="sk-..."
```

### Context Too Long

Use a larger model or reduce input:
```go
provider, _ := kimi.New(agentllm.WithModel("moonshot-v1-128k"))
```

### Slow Responses

Long documents take more time to process. Increase timeout:
```go
provider, _ := kimi.New(agentllm.WithTimeout(120 * time.Second))
```

## Chinese Language Support

Kimi is optimized for Chinese language processing:

```go
messages := []agentllm.Message{
    {Role: "system", Content: "你是一个有帮助的AI助手。"},
    {Role: "user", Content: "请用中文回答：什么是人工智能？"},
}

resp, err := provider.Chat(ctx, messages)
// Response will be in high-quality Chinese
```

## License

This provider is part of the GoAgent project and follows the same license terms.

## Links

- [Kimi Official Site](https://www.moonshot.cn/)
- [Kimi API Documentation](https://platform.moonshot.cn/docs)
- [GoAgent Documentation](https://github.com/kart-io/goagent)
- [Pricing Information](https://platform.moonshot.cn/pricing)
