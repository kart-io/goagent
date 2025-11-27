# DeepSeek LLM Provider for GoAgent

Independent module providing DeepSeek integration for the GoAgent framework.

## Installation

```bash
go get github.com/kart-io/goagent/contrib/llm-providers/deepseek
```

## Usage

```go
import (
    "github.com/kart-io/goagent/contrib/llm-providers/deepseek"
    "github.com/kart-io/goagent/llm"
)

// Create provider with options
provider, err := deepseek.New(
    llm.WithAPIKey("your-api-key"),
    llm.WithModel("deepseek-chat"),
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
provider, err := providers.NewDeepSeekWithOptions(...)
```

New import:
```go
import "github.com/kart-io/goagent/contrib/llm-providers/deepseek"
provider, err := deepseek.New(...)
```

## Features

- Chat completion
- Streaming generation
- Tool/Function calling
- Embeddings generation
- HTTP REST API based
