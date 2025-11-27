# Gemini LLM Provider for GoAgent

Independent module providing Google Gemini integration for the GoAgent framework.

## Installation

```bash
go get github.com/kart-io/goagent/contrib/llm-providers/gemini
```

## Usage

```go
import (
    "github.com/kart-io/goagent/contrib/llm-providers/gemini"
    "github.com/kart-io/goagent/llm"
)

// Create provider with options
provider, err := gemini.New(
    llm.WithAPIKey("your-api-key"),
    llm.WithModel("gemini-pro"),
    llm.WithTemperature(0.7),
)

// Use provider
resp, err := provider.Complete(ctx, &llm.CompletionRequest{
    Messages: []llm.Message{
        {Role: "user", Content: "Hello!"},
    },
})
```

## Migration from llm/providers

Old import:
```go
import "github.com/kart-io/goagent/llm/providers"
provider, err := providers.NewGeminiWithOptions(...)
```

New import:
```go
import "github.com/kart-io/goagent/contrib/llm-providers/gemini"
provider, err := gemini.New(...)
```

## Features

- Chat completion
- Streaming generation
- Tool/Function calling
- Embeddings (mock implementation)
- Google Vertex AI SDK based
- Context cancellation support

## Supported Models

- `gemini-pro` - General purpose model
- `gemini-pro-vision` - Multimodal model with vision capabilities
- `gemini-ultra` - Most capable model (when available)

## Configuration

Environment variables:
- `GEMINI_API_KEY` - API key for authentication
- `GEMINI_MODEL` - Default model name (optional)

## Streaming Support

The Gemini provider includes an enhanced streaming provider:

```go
streamProvider, err := gemini.NewStreaming(config)
events, err := streamProvider.StreamWithContext(ctx, "Hello!")

for event := range events {
    switch event.Type {
    case "start":
        fmt.Println("Stream started")
    case "token":
        fmt.Print(event.Content)
    case "complete":
        fmt.Println("\nStream completed")
    case "error":
        fmt.Printf("Error: %v\n", event.Error)
    }
}
```

## Note on Embeddings

The current implementation uses a mock embedding response. For production use, implement the Google Embedding API endpoint directly.
