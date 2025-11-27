# Ollama Provider for GoAgent

Ollama provider implementation for the GoAgent framework, enabling local LLM deployments.

## Features

- **Local Model Support**: Run open-source models locally without API costs
- **Chat API**: Multi-turn conversations with context awareness
- **Generate API**: Simple text generation endpoint
- **Model Management**: Pull and list available models
- **Streaming Support**: Real-time token streaming
- **No API Key Required**: Direct local connection

## Installation

```bash
go get github.com/kart-io/goagent/contrib/llm-providers/ollama
```

## Prerequisites

1. Install Ollama from [ollama.ai](https://ollama.ai)
2. Start the Ollama service (default: http://localhost:11434)
3. Pull a model: `ollama pull llama2`

## Usage

### Basic Usage

```go
package main

import (
    "context"
    "fmt"
    "log"

    "github.com/kart-io/goagent/contrib/llm-providers/ollama"
    agentllm "github.com/kart-io/goagent/llm"
)

func main() {
    // Create Ollama provider (connects to localhost:11434 by default)
    provider, err := ollama.New(
        agentllm.WithModel("llama2"),
    )
    if err != nil {
        log.Fatal(err)
    }

    // Create completion request
    req := &agentllm.CompletionRequest{
        Messages: []agentllm.Message{
            {Role: "user", Content: "What is the capital of France?"},
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

### Custom Base URL

```go
provider, err := ollama.New(
    agentllm.WithModel("llama2"),
    agentllm.WithBaseURL("http://192.168.1.100:11434"),
)
```

### Chat with Context

```go
messages := []agentllm.Message{
    {Role: "system", Content: "You are a helpful assistant."},
    {Role: "user", Content: "Hello!"},
    {Role: "assistant", Content: "Hi! How can I help you today?"},
    {Role: "user", Content: "What's the weather like?"},
}

resp, err := provider.Chat(context.Background(), messages)
```

### Model Management

```go
// List available models
models, err := provider.ListModels()
if err != nil {
    log.Fatal(err)
}
for _, model := range models {
    fmt.Println(model)
}

// Pull a new model
err = provider.PullModel("mistral")
if err != nil {
    log.Fatal(err)
}
```

### Configuration Options

```go
provider, err := ollama.New(
    agentllm.WithModel("llama2"),
    agentllm.WithTemperature(0.7),
    agentllm.WithMaxTokens(2000),
    agentllm.WithTimeout(120 * time.Second),
)
```

## Environment Variables

The provider supports configuration via environment variables:

```bash
export OLLAMA_BASE_URL="http://localhost:11434"
export OLLAMA_MODEL="llama2"
```

## Supported Models

Ollama supports many open-source models. Popular choices include:

- **Llama 2**: `llama2` (7B, 13B, 70B)
- **Mistral**: `mistral` (7B)
- **Code Llama**: `codellama` (7B, 13B, 34B, 70B)
- **Vicuna**: `vicuna` (7B, 13B, 33B)
- **Orca Mini**: `orca-mini` (3B, 7B, 13B)
- **Phi-2**: `phi` (2.7B)

Pull models with: `ollama pull <model-name>`

## Migration from llm/providers

If you're migrating from the old import path:

```go
// Old import
import "github.com/kart-io/goagent/llm/providers"
provider := providers.NewOllamaWithOptions(...)

// New import (recommended)
import "github.com/kart-io/goagent/contrib/llm-providers/ollama"
provider := ollama.New(...)

// Old import still works (backward compatible)
import "github.com/kart-io/goagent/llm/providers"
provider := providers.NewOllamaWithOptions(...)
```

## Advanced Features

### Custom Timeout

Ollama operations can take longer due to local computation:

```go
provider, err := ollama.New(
    agentllm.WithModel("llama2:70b"),
    agentllm.WithTimeout(5 * time.Minute), // Longer timeout for large models
)
```

### Check Availability

```go
if provider.IsAvailable() {
    fmt.Println("Ollama service is running")
} else {
    fmt.Println("Ollama service is not available")
}
```

### Using Builder Pattern

```go
// With fluent API
resp, err := provider.
    WithModel("codellama").
    WithTemperature(0.3).
    WithMaxTokens(1000).
    Chat(ctx, messages)
```

## Error Handling

```go
resp, err := provider.Complete(ctx, req)
if err != nil {
    switch {
    case errors.Is(err, context.DeadlineExceeded):
        log.Println("Request timeout - model may be loading")
    case strings.Contains(err.Error(), "connection refused"):
        log.Println("Ollama service not running")
    default:
        log.Printf("Request failed: %v", err)
    }
    return
}
```

## Performance Considerations

- **Model Size**: Larger models provide better quality but slower responses
- **Hardware**: GPU acceleration significantly improves performance
- **Context Length**: Longer conversations consume more memory
- **Timeout**: Set appropriate timeouts based on model size and hardware

## Troubleshooting

### Service Not Available

Ensure Ollama is running:
```bash
ollama serve
```

### Model Not Found

Pull the model first:
```bash
ollama pull llama2
```

### Slow Responses

- Use smaller models for faster inference
- Enable GPU acceleration if available
- Reduce max_tokens for quicker responses

## License

This provider is part of the GoAgent project and follows the same license terms.

## Links

- [Ollama Official Site](https://ollama.ai)
- [Ollama GitHub](https://github.com/ollama/ollama)
- [GoAgent Documentation](https://github.com/kart-io/goagent)
- [Model Library](https://ollama.ai/library)
