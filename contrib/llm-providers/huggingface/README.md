# HuggingFace LLM Provider for GoAgent

Independent module providing Hugging Face integration for the GoAgent framework.

## Installation

```bash
go get github.com/kart-io/goagent/contrib/llm-providers/huggingface
```

## Usage

```go
import (
    "github.com/kart-io/goagent/contrib/llm-providers/huggingface"
    "github.com/kart-io/goagent/llm"
)

// Create provider with options
provider, err := huggingface.New(
    llm.WithAPIKey("your-api-key"),
    llm.WithModel("gpt2"),
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
provider, err := providers.NewHuggingFaceWithOptions(...)
```

New import:
```go
import "github.com/kart-io/goagent/contrib/llm-providers/huggingface"
provider, err := huggingface.New(...)
```

## Features

- Text generation
- Streaming generation
- HTTP REST API based
- Model loading retry logic
- Extended timeout support

## Supported Models

Any text generation model from Hugging Face Hub, including:
- `gpt2` - GPT-2 base model
- `EleutherAI/gpt-neo-2.7B` - GPT-Neo 2.7B
- `bigscience/bloom-1b7` - BLOOM 1.7B
- `meta-llama/Llama-2-7b-hf` - Llama 2 7B

## Configuration

Environment variables:
- `HUGGINGFACE_API_KEY` - API key for authentication
- `HUGGINGFACE_BASE_URL` - Custom API base URL (optional)
- `HUGGINGFACE_MODEL` - Default model name (optional)

## Model Loading

Hugging Face models may need to load before responding. The provider automatically:
- Waits for model loading
- Retries with extended delays
- Returns estimated loading time in errors
