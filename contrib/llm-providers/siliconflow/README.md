# SiliconFlow Provider for GoAgent

SiliconFlow provider implementation for the GoAgent framework, offering access to multiple open-source LLM models.

## Features

- **Multiple Model Support**: Access to 20+ open-source models
- **Popular Model Families**: Qwen, DeepSeek, GLM, Yi, Mistral, Llama
- **OpenAI-Compatible API**: Familiar request/response format
- **Cost-Effective**: Competitive pricing for open-source models
- **High Performance**: Optimized inference infrastructure
- **Chinese Language Support**: Excellent Chinese language models

## Installation

```bash
go get github.com/kart-io/goagent/contrib/llm-providers/siliconflow
```

## Prerequisites

1. Sign up for a SiliconFlow account at [siliconflow.cn](https://siliconflow.cn/)
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

    "github.com/kart-io/goagent/contrib/llm-providers/siliconflow"
    agentllm "github.com/kart-io/goagent/llm"
)

func main() {
    // Create SiliconFlow provider
    provider, err := siliconflow.New(
        agentllm.WithAPIKey("your-api-key"),
        agentllm.WithModel("Qwen/Qwen2-7B-Instruct"),
    )
    if err != nil {
        log.Fatal(err)
    }

    // Create completion request
    req := &agentllm.CompletionRequest{
        Messages: []agentllm.Message{
            {Role: "user", Content: "What is machine learning?"},
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

### Environment Variables

```bash
export SILICONFLOW_API_KEY="your-api-key"
export SILICONFLOW_BASE_URL="https://api.siliconflow.cn/v1"
export SILICONFLOW_MODEL="Qwen/Qwen2-7B-Instruct"
```

Then use without explicit configuration:

```go
provider, err := siliconflow.New() // Uses environment variables
```

### Configuration Options

```go
provider, err := siliconflow.New(
    agentllm.WithAPIKey("your-api-key"),
    agentllm.WithModel("Qwen/Qwen2.5-72B-Instruct"),
    agentllm.WithTemperature(0.7),
    agentllm.WithMaxTokens(2000),
    agentllm.WithTimeout(30 * time.Second),
)
```

## Available Models

### Qwen Series (Chinese-Optimized)

```go
// Small and efficient
provider, _ := siliconflow.New(
    agentllm.WithModel("Qwen/Qwen2-1.5B-Instruct"),
)

// Balanced performance
provider, _ := siliconflow.New(
    agentllm.WithModel("Qwen/Qwen2-7B-Instruct"),
)

// Large and powerful
provider, _ := siliconflow.New(
    agentllm.WithModel("Qwen/Qwen2.5-72B-Instruct"),
)

// Code-specialized
provider, _ := siliconflow.New(
    agentllm.WithModel("Qwen/Qwen2.5-Coder-7B-Instruct"),
)
```

### DeepSeek Series (Reasoning & Code)

```go
// Excellent for coding tasks
provider, _ := siliconflow.New(
    agentllm.WithModel("deepseek-ai/DeepSeek-Coder-V2-Instruct"),
)

// Latest version
provider, _ := siliconflow.New(
    agentllm.WithModel("deepseek-ai/DeepSeek-V2.5"),
)
```

### Meta Llama Series

```go
// Llama 3 (8B)
provider, _ := siliconflow.New(
    agentllm.WithModel("meta-llama/Meta-Llama-3-8B-Instruct"),
)

// Llama 3.1 (70B)
provider, _ := siliconflow.New(
    agentllm.WithModel("meta-llama/Meta-Llama-3.1-70B-Instruct"),
)
```

### Other Popular Models

```go
// GLM-4 (Chinese)
provider, _ := siliconflow.New(
    agentllm.WithModel("THUDM/glm-4-9b-chat"),
)

// Yi (Chinese)
provider, _ := siliconflow.New(
    agentllm.WithModel("01-ai/Yi-1.5-34B-Chat-16K"),
)

// Mistral
provider, _ := siliconflow.New(
    agentllm.WithModel("mistralai/Mistral-7B-Instruct-v0.2"),
)
```

### Complete Model List

```go
models := provider.ListModels()
for _, model := range models {
    fmt.Println(model)
}

// Output:
// Qwen/Qwen2-7B-Instruct
// Qwen/Qwen2.5-72B-Instruct
// deepseek-ai/DeepSeek-V2.5
// meta-llama/Meta-Llama-3.1-70B-Instruct
// ... and more
```

## Model Selection Guide

### By Use Case

| Use Case | Recommended Model |
|----------|------------------|
| Chinese Conversation | `Qwen/Qwen2.5-72B-Instruct` |
| Code Generation | `deepseek-ai/DeepSeek-Coder-V2-Instruct` |
| English Conversation | `meta-llama/Meta-Llama-3.1-70B-Instruct` |
| Fast Responses | `Qwen/Qwen2-1.5B-Instruct` |
| Balanced Performance | `Qwen/Qwen2-7B-Instruct` |
| Long Context | `01-ai/Yi-1.5-34B-Chat-16K` |

### By Model Size

| Size | Models | Best For |
|------|--------|----------|
| Small (1-3B) | Qwen2-1.5B, phi | Fast inference, simple tasks |
| Medium (7-9B) | Qwen2-7B, Mistral-7B, GLM-4-9b | General purpose |
| Large (13-34B) | Qwen2.5-14B, Yi-1.5-34B | Complex reasoning |
| XL (70B+) | Qwen2.5-72B, Llama-3.1-70B | Highest quality |

## Advanced Features

### Chat with Context

```go
messages := []agentllm.Message{
    {Role: "system", Content: "You are a helpful coding assistant."},
    {Role: "user", Content: "Write a function to calculate fibonacci numbers"},
    {Role: "assistant", Content: "Here's a fibonacci function..."},
    {Role: "user", Content: "Now make it iterative"},
}

resp, err := provider.Chat(context.Background(), messages)
```

### Fluent API

```go
resp, err := provider.
    WithModel("deepseek-ai/DeepSeek-Coder-V2-Instruct").
    WithTemperature(0.2).
    WithMaxTokens(1000).
    Chat(ctx, messages)
```

### Check Availability

```go
if provider.IsAvailable() {
    fmt.Println("SiliconFlow API is accessible")
} else {
    fmt.Println("SiliconFlow API is not available")
}
```

## Migration from llm/providers

If you're migrating from the old import path:

```go
// Old import
import "github.com/kart-io/goagent/llm/providers"
provider := providers.NewSiliconFlowWithOptions(...)

// New import (recommended)
import "github.com/kart-io/goagent/contrib/llm-providers/siliconflow"
provider := siliconflow.New(...)

// Old import still works (backward compatible)
import "github.com/kart-io/goagent/llm/providers"
provider := providers.NewSiliconFlowWithOptions(...)
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
    case strings.Contains(err.Error(), "model not found"):
        log.Println("Model not available - check model name")
    default:
        log.Printf("Request failed: %v", err)
    }
    return
}
```

## Best Practices

### 1. Choose the Right Model Size

```go
// For simple tasks, use smaller models (faster, cheaper)
quickProvider, _ := siliconflow.New(
    agentllm.WithModel("Qwen/Qwen2-7B-Instruct"),
)

// For complex tasks, use larger models (better quality)
powerProvider, _ := siliconflow.New(
    agentllm.WithModel("Qwen/Qwen2.5-72B-Instruct"),
)
```

### 2. Optimize for Your Language

```go
// Chinese content - use Qwen or GLM
chineseProvider, _ := siliconflow.New(
    agentllm.WithModel("Qwen/Qwen2.5-72B-Instruct"),
)

// English content - use Llama or Mistral
englishProvider, _ := siliconflow.New(
    agentllm.WithModel("meta-llama/Meta-Llama-3.1-70B-Instruct"),
)
```

### 3. Set Appropriate Parameters

```go
// For creative writing (higher temperature)
creativeProvider, _ := siliconflow.New(
    agentllm.WithTemperature(0.9),
)

// For factual tasks (lower temperature)
factualProvider, _ := siliconflow.New(
    agentllm.WithTemperature(0.1),
)
```

### 4. Handle Timeouts Gracefully

```go
ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
defer cancel()

resp, err := provider.Complete(ctx, req)
if err != nil {
    if ctx.Err() == context.DeadlineExceeded {
        log.Println("Request timeout - consider using a smaller model")
    }
}
```

## Performance Considerations

- **Model Size**: Larger models are slower but more capable
- **Token Limits**: Respect max_tokens to control response length
- **Temperature**: Lower values (0.1-0.3) for deterministic outputs
- **Batch Requests**: Process multiple requests in parallel for throughput

## Troubleshooting

### Invalid API Key

Check your API key is valid and not expired:
```bash
export SILICONFLOW_API_KEY="your-valid-key"
```

### Model Not Found

Verify the model name is exactly correct:
```go
models := provider.ListModels()
// Check the exact model name format
```

### Rate Limiting

Implement retry logic with exponential backoff:
```go
for i := 0; i < 3; i++ {
    resp, err := provider.Complete(ctx, req)
    if err == nil {
        break
    }
    if strings.Contains(err.Error(), "rate limit") {
        time.Sleep(time.Duration(i+1) * time.Second)
        continue
    }
    return err
}
```

### Slow Responses

Use smaller models or reduce max_tokens:
```go
provider, _ := siliconflow.New(
    agentllm.WithModel("Qwen/Qwen2-7B-Instruct"), // Smaller model
    agentllm.WithMaxTokens(500), // Limit output length
)
```

## Comparison with Other Providers

| Feature | SiliconFlow | OpenAI | Ollama |
|---------|------------|---------|--------|
| Cost | $ | $$$ | Free |
| Speed | Fast | Very Fast | Varies |
| Models | 20+ OSS | Proprietary | 100+ OSS |
| Chinese | Excellent | Good | Varies |
| Deployment | Cloud | Cloud | Local |

## License

This provider is part of the GoAgent project and follows the same license terms.

## Links

- [SiliconFlow Official Site](https://siliconflow.cn/)
- [SiliconFlow API Documentation](https://docs.siliconflow.cn/)
- [GoAgent Documentation](https://github.com/kart-io/goagent)
- [Model Catalog](https://siliconflow.cn/models)
- [Pricing Information](https://siliconflow.cn/pricing)
