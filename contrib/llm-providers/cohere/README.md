# Cohere LLM Provider for GoAgent

Independent module providing Cohere integration for the GoAgent framework.

## Installation

```bash
go get github.com/kart-io/goagent/contrib/llm-providers/cohere
```

## Usage

```go
import (
    "github.com/kart-io/goagent/contrib/llm-providers/cohere"
    "github.com/kart-io/goagent/llm"
)

// Create provider with options
provider, err := cohere.New(
    llm.WithAPIKey("your-api-key"),
    llm.WithModel("command"),
    llm.WithTemperature(0.7),
    llm.WithMaxTokens(2048),
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
provider, err := providers.NewCohereWithOptions(...)
```

New import:
```go
import "github.com/kart-io/goagent/contrib/llm-providers/cohere"
provider, err := cohere.New(...)
```

## Features

- Chat completion
- Streaming generation
- HTTP REST API based
- Chat history support
- Context cancellation support

## Supported Models

- `command` - Flagship text generation model
- `command-light` - Faster, lightweight version
- `command-nightly` - Latest experimental features
- `command-light-nightly` - Lightweight experimental version

## Configuration

Environment variables:
- `COHERE_API_KEY` - API key for authentication
- `COHERE_BASE_URL` - Custom API base URL (optional)
- `COHERE_MODEL` - Default model name (optional)

## Streaming Support

The Cohere provider supports streaming responses:

```go
stream, err := provider.Stream(ctx, "Tell me a story")
if err != nil {
    log.Fatal(err)
}

for token := range stream {
    fmt.Print(token)
}
```

## Chat History

Cohere's chat API automatically maintains conversation history:

```go
resp, err := provider.Complete(ctx, &llm.CompletionRequest{
    Messages: []llm.Message{
        {Role: "user", Content: "What is machine learning?"},
        {Role: "assistant", Content: "Machine learning is..."},
        {Role: "user", Content: "Can you give me an example?"},
    },
})
```

The last user message becomes the current prompt, and previous messages are sent as chat history.
