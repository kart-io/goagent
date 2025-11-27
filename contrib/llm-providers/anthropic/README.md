# Anthropic LLM Provider for GoAgent

Independent module providing Anthropic Claude integration for the GoAgent framework.

## Installation

```bash
go get github.com/kart-io/goagent/contrib/llm-providers/anthropic
```

## Usage

```go
import (
    "github.com/kart-io/goagent/contrib/llm-providers/anthropic"
    "github.com/kart-io/goagent/llm"
)

// Create provider with options
provider, err := anthropic.New(
    llm.WithAPIKey("your-api-key"),
    llm.WithModel("claude-3-opus-20240229"),
    llm.WithTemperature(0.7),
    llm.WithMaxTokens(4096),
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
provider, err := providers.NewAnthropicWithOptions(...)
```

New import:
```go
import "github.com/kart-io/goagent/contrib/llm-providers/anthropic"
provider, err := anthropic.New(...)
```

## Features

- Chat completion
- Streaming generation
- HTTP REST API based
- System message support
- Context cancellation support

## Supported Models

- `claude-3-opus-20240229` - Most capable model
- `claude-3-sonnet-20240229` - Balanced performance and speed
- `claude-3-haiku-20240307` - Fast and cost-effective
- `claude-2.1` - Previous generation model
- `claude-2.0` - Previous generation model
- `claude-instant-1.2` - Fast, lightweight model

## Configuration

Environment variables:
- `ANTHROPIC_API_KEY` - API key for authentication
- `ANTHROPIC_BASE_URL` - Custom API base URL (optional)
- `ANTHROPIC_MODEL` - Default model name (optional)

## Streaming Support

The Anthropic provider supports streaming responses:

```go
stream, err := provider.Stream(ctx, "Tell me a story")
if err != nil {
    log.Fatal(err)
}

for token := range stream {
    fmt.Print(token)
}
```

## System Messages

Anthropic separates system messages from conversation messages:

```go
resp, err := provider.Complete(ctx, &llm.CompletionRequest{
    Messages: []llm.Message{
        {Role: "system", Content: "You are a helpful assistant."},
        {Role: "user", Content: "Hello!"},
    },
})
```

The system message will be automatically extracted and sent in the `system` field.
