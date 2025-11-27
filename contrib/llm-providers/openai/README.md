# OpenAI LLM Provider for GoAgent

This is an independent module providing OpenAI integration for the GoAgent framework.

## Installation

```bash
go get github.com/kart-io/goagent/contrib/llm-providers/openai
```

## Usage

```go
import (
    "github.com/kart-io/goagent/contrib/llm-providers/openai"
    "github.com/kart-io/goagent/llm"
)

// Create provider with options
provider, err := openai.New(
    llm.WithAPIKey("your-api-key"),
    llm.WithModel("gpt-4"),
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
provider, err := providers.NewOpenAIWithOptions(...)
```

New import:
```go
import "github.com/kart-io/goagent/contrib/llm-providers/openai"
provider, err := openai.New(...)
```

## Features

- Chat completion
- Streaming generation
- Tool/Function calling
- Embeddings generation
- Vision support (via GPT-4 Vision models)
- JSON mode
