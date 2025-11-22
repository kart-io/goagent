# LLM Configuration with Options Pattern

## Overview

The LLM module now supports a flexible options pattern for configuration, making it easier to create and customize LLM clients with clean, readable code.

## Migration from Config Struct to Options Pattern

### Old Way (Config Struct)

```go
config := &llm.Config{
    Provider:    llm.ProviderOpenAI,
    APIKey:      "your-api-key",
    Model:       "gpt-4",
    MaxTokens:   2000,
    Temperature: 0.7,
    Timeout:     60,
}
client, err := providers.NewOpenAI(config)
```

### New Way (Options Pattern)

```go
client, err := llm.NewClientWithOptions(
    llm.WithProvider(llm.ProviderOpenAI),
    llm.WithAPIKey("your-api-key"),
    llm.WithModel("gpt-4"),
    llm.WithMaxTokens(2000),
    llm.WithTemperature(0.7),
    llm.WithTimeout(60 * time.Second),
)
```

## Basic Usage

### Simple Configuration

```go
// Minimal configuration - API key from environment
client, err := llm.NewClientWithOptions(
    llm.WithProvider(llm.ProviderOpenAI),
)

// With explicit configuration
client, err := llm.NewClientWithOptions(
    llm.WithProvider(llm.ProviderOpenAI),
    llm.WithAPIKey("your-api-key"),
    llm.WithModel("gpt-4"),
)
```

### Using Presets

Presets provide pre-configured settings for common scenarios:

```go
// Development preset - faster, cheaper models
client, err := llm.NewClientWithOptions(
    llm.WithProvider(llm.ProviderOpenAI),
    llm.WithAPIKey("your-api-key"),
    llm.WithPreset(llm.PresetDevelopment),
)

// Production preset - reliable, with caching and retries
client, err := llm.NewClientWithOptions(
    llm.WithProvider(llm.ProviderOpenAI),
    llm.WithAPIKey("your-api-key"),
    llm.WithPreset(llm.PresetProduction),
)
```

Available presets:
- `PresetDevelopment` - Fast iteration, lower costs
- `PresetProduction` - Reliability, caching, retries
- `PresetLowCost` - Minimize token usage and costs
- `PresetHighQuality` - Best models, higher token limits
- `PresetFast` - Optimized for low latency

### Provider-Specific Presets

Automatically configure optimal settings for each provider:

```go
// OpenAI with optimal settings
client, err := llm.NewClientWithOptions(
    llm.WithProviderPreset(llm.ProviderOpenAI),
    llm.WithAPIKey("sk-..."),
)

// Anthropic Claude
client, err := llm.NewClientWithOptions(
    llm.WithProviderPreset(llm.ProviderAnthropic),
    llm.WithAPIKey("sk-ant-..."),
)

// Local Ollama (no API key needed)
client, err := llm.NewClientWithOptions(
    llm.WithProviderPreset(llm.ProviderOllama),
)
```

## Use Case Optimization

Optimize settings for specific use cases:

```go
// Code generation - low temperature, high precision
client, err := llm.NewClientWithOptions(
    llm.WithProvider(llm.ProviderOpenAI),
    llm.WithAPIKey("your-api-key"),
    llm.WithUseCase(llm.UseCaseCodeGeneration),
)

// Creative writing - high temperature, more tokens
client, err := llm.NewClientWithOptions(
    llm.WithProvider(llm.ProviderOpenAI),
    llm.WithAPIKey("your-api-key"),
    llm.WithUseCase(llm.UseCaseCreativeWriting),
)
```

Available use cases:
- `UseCaseChat` - Conversational AI
- `UseCaseCodeGeneration` - Programming assistance
- `UseCaseTranslation` - Language translation
- `UseCaseSummarization` - Text summarization
- `UseCaseAnalysis` - Data and text analysis
- `UseCaseCreativeWriting` - Creative content generation

## Advanced Configuration

### Retry and Error Handling

```go
client, err := llm.NewClientWithOptions(
    llm.WithProvider(llm.ProviderOpenAI),
    llm.WithAPIKey("your-api-key"),
    llm.WithRetryCount(3),
    llm.WithRetryDelay(2 * time.Second),
)
```

### Caching

```go
client, err := llm.NewClientWithOptions(
    llm.WithProvider(llm.ProviderOpenAI),
    llm.WithAPIKey("your-api-key"),
    llm.WithCache(true, 10 * time.Minute), // Enable with 10-minute TTL
)
```

### Rate Limiting

```go
client, err := llm.NewClientWithOptions(
    llm.WithProvider(llm.ProviderOpenAI),
    llm.WithAPIKey("your-api-key"),
    llm.WithRateLimiting(60), // 60 requests per minute
)
```

### Proxy Configuration

```go
client, err := llm.NewClientWithOptions(
    llm.WithProvider(llm.ProviderOpenAI),
    llm.WithAPIKey("your-api-key"),
    llm.WithProxy("http://proxy.example.com:8080"),
)
```

### Custom Headers

```go
client, err := llm.NewClientWithOptions(
    llm.WithProvider(llm.ProviderOpenAI),
    llm.WithAPIKey("your-api-key"),
    llm.WithCustomHeaders(map[string]string{
        "X-Custom-Header": "value",
        "User-Agent": "MyApp/1.0",
    }),
)
```

### Streaming Responses

```go
client, err := llm.NewClientWithOptions(
    llm.WithProvider(llm.ProviderOpenAI),
    llm.WithAPIKey("your-api-key"),
    llm.WithStreamingEnabled(true),
)
```

## Combining Options

Options can be combined for complex configurations:

```go
client, err := llm.NewClientWithOptions(
    // Provider and credentials
    llm.WithProvider(llm.ProviderOpenAI),
    llm.WithAPIKey("your-api-key"),
    llm.WithOrganizationID("org-..."),

    // Start with a preset
    llm.WithPreset(llm.PresetProduction),

    // Optimize for use case
    llm.WithUseCase(llm.UseCaseCodeGeneration),

    // Override specific settings
    llm.WithModel("gpt-4-turbo-preview"),
    llm.WithMaxTokens(8000),

    // Add advanced features
    llm.WithCache(true, 15 * time.Minute),
    llm.WithRetryCount(5),
    llm.WithStreamingEnabled(true),
    llm.WithSystemPrompt("You are an expert programmer"),
)
```

## Environment-Based Configuration

API keys can be automatically loaded from environment variables:

```go
// Set environment variable: OPENAI_API_KEY=sk-...
client, err := llm.NewClientWithOptions(
    llm.WithProvider(llm.ProviderOpenAI),
    // API key will be loaded from OPENAI_API_KEY
)
```

Supported environment variables:
- `OPENAI_API_KEY` - OpenAI
- `ANTHROPIC_API_KEY` - Anthropic Claude
- `GOOGLE_API_KEY` - Google Gemini
- `DEEPSEEK_API_KEY` - DeepSeek
- `KIMI_API_KEY` - Kimi/Moonshot
- `SILICONFLOW_API_KEY` - SiliconFlow
- `COHERE_API_KEY` - Cohere
- `HUGGINGFACE_API_KEY` - HuggingFace

## Configuration Options Reference

### Core Options

| Option | Description | Example |
|--------|-------------|---------|
| `WithProvider()` | Set LLM provider | `WithProvider(ProviderOpenAI)` |
| `WithAPIKey()` | Set API key | `WithAPIKey("sk-...")` |
| `WithModel()` | Set model name | `WithModel("gpt-4")` |
| `WithBaseURL()` | Custom API endpoint | `WithBaseURL("https://api.example.com")` |

### Generation Parameters

| Option | Description | Range | Default |
|--------|-------------|-------|---------|
| `WithMaxTokens()` | Maximum tokens to generate | > 0 | 2000 |
| `WithTemperature()` | Randomness control | 0.0-2.0 | 0.7 |
| `WithTopP()` | Nucleus sampling | 0.0-1.0 | 1.0 |

### Network & Performance

| Option | Description | Example |
|--------|-------------|---------|
| `WithTimeout()` | Request timeout | `WithTimeout(30 * time.Second)` |
| `WithRetryCount()` | Number of retries | `WithRetryCount(3)` |
| `WithRetryDelay()` | Delay between retries | `WithRetryDelay(2 * time.Second)` |
| `WithRateLimiting()` | Requests per minute | `WithRateLimiting(60)` |
| `WithProxy()` | Proxy URL | `WithProxy("http://proxy:8080")` |

### Features

| Option | Description | Example |
|--------|-------------|---------|
| `WithCache()` | Enable caching | `WithCache(true, 10 * time.Minute)` |
| `WithStreamingEnabled()` | Enable streaming | `WithStreamingEnabled(true)` |
| `WithSystemPrompt()` | Default system prompt | `WithSystemPrompt("You are helpful")` |
| `WithCustomHeaders()` | Custom HTTP headers | `WithCustomHeaders(map[string]string{...})` |

## Updating Existing Configuration

You can apply options to an existing configuration:

```go
// Start with existing config
config := &llm.Config{
    Provider: llm.ProviderOpenAI,
    APIKey: "your-api-key",
    Model: "gpt-3.5-turbo",
}

// Enhance with options
enhanced := llm.ApplyOptions(
    config,
    llm.WithModel("gpt-4"),
    llm.WithCache(true, 5 * time.Minute),
    llm.WithRetryCount(3),
)
```

## Best Practices

1. **Use Presets as Starting Points**: Begin with a preset that matches your environment or use case, then customize as needed.

2. **Environment Variables for Secrets**: Store API keys in environment variables rather than hardcoding them.

3. **Combine Multiple Options**: Layer presets, use cases, and specific options for fine-tuned control.

4. **Enable Caching for Production**: Reduce costs and latency by enabling caching in production environments.

5. **Configure Retries**: Always configure retry logic for production systems to handle transient failures.

6. **Use Appropriate Models**: Choose models based on your use case - don't use GPT-4 for simple tasks where GPT-3.5 suffices.

## Example: Production-Ready Configuration

```go
client, err := llm.NewClientWithOptions(
    // Provider configuration
    llm.WithProvider(llm.ProviderOpenAI),
    // API key from environment variable

    // Production preset as base
    llm.WithPreset(llm.PresetProduction),

    // Specific model for your needs
    llm.WithModel("gpt-4-turbo-preview"),

    // Reliability features
    llm.WithRetryCount(3),
    llm.WithRetryDelay(2 * time.Second),
    llm.WithTimeout(60 * time.Second),

    // Performance optimization
    llm.WithCache(true, 30 * time.Minute),
    llm.WithRateLimiting(100), // Adjust based on your tier

    // Monitoring
    llm.WithCustomHeaders(map[string]string{
        "X-Request-ID": requestID,
        "X-Service": "my-service",
    }),
)

if err != nil {
    log.Fatal("Failed to create LLM client:", err)
}
```

This configuration provides:
- Automatic retries for resilience
- Caching to reduce costs and latency
- Rate limiting to stay within API limits
- Request tracking via custom headers
- Timeout protection
- Environment-based secret management