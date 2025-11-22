# Option Pattern API Reference

## Overview

This document provides a complete API reference for the Option pattern implementations in GoAgent's cache and LLM modules.

## Table of Contents

- [Cache Module Options](#cache-module-options)
- [LLM Module Options](#llm-module-options)
- [Factory Methods](#factory-methods)
- [Builder Pattern](#builder-pattern)
- [Type Definitions](#type-definitions)

---

## Cache Module Options

### Package: `github.com/kart-io/goagent/tools`

#### Option Type

```go
type ShardedCacheOption func(*ShardedCacheConfig)
```

#### Constructor

```go
func NewShardedCache(opts ...ShardedCacheOption) *ShardedCache
```

Creates a new sharded cache with the specified options.

#### Basic Configuration Options

##### WithShardCount

```go
func WithShardCount(count uint32) ShardedCacheOption
```

Sets the number of shards. If 0, defaults to `runtime.NumCPU() * 4`.

- **Parameter**: `count` - Number of shards
- **Default**: CPU cores × 4
- **Example**: `WithShardCount(32)`

##### WithCapacity

```go
func WithCapacity(capacity int) ShardedCacheOption
```

Sets the maximum number of items per shard.

- **Parameter**: `capacity` - Max items per shard
- **Default**: 10000
- **Example**: `WithCapacity(50000)`

##### WithCleanupInterval

```go
func WithCleanupInterval(interval time.Duration) ShardedCacheOption
```

Sets the interval between cleanup operations.

- **Parameter**: `interval` - Cleanup interval
- **Default**: 5 minutes
- **Example**: `WithCleanupInterval(10 * time.Minute)`

##### WithTTL

```go
func WithTTL(ttl time.Duration) ShardedCacheOption
```

Sets the default time-to-live for cache items.

- **Parameter**: `ttl` - Time to live
- **Default**: 1 hour
- **Example**: `WithTTL(30 * time.Minute)`

#### Performance Options

##### WithPerformanceProfile

```go
func WithPerformanceProfile(profile PerformanceProfile) ShardedCacheOption
```

Applies a pre-configured performance profile.

**Profiles**:

| Profile | Shards | Cleanup | Batch Size | Retries |
|---------|--------|---------|------------|---------|
| `PerformanceLowLatency` | CPU×4 | 5 min | 100 | 3 |
| `PerformanceHighThroughput` | CPU×8 | 10 min | 500 | 2 |
| `PerformanceBalanced` | CPU×2 | 2 min | 200 | 2 |
| `PerformanceMemoryEfficient` | CPU | 1 min | 50 | 1 |

##### WithWorkloadType

```go
func WithWorkloadType(workload WorkloadType) ShardedCacheOption
```

Optimizes cache for specific workload patterns.

**Workload Types**:

| Type | Shards | Cleanup | Batch | Lock Bias |
|------|--------|---------|-------|-----------|
| `WorkloadReadHeavy` | CPU×4 | 10 min | 100 | 95% |
| `WorkloadWriteHeavy` | CPU×2 | 2 min | 500 | 60% |
| `WorkloadMixed` | CPU×2 | 5 min | 200 | 75% |
| `WorkloadBursty` | CPU×8 | 5 min | 200 | 70% |

#### Advanced Options

##### WithEvictionPolicy

```go
func WithEvictionPolicy(policy string) ShardedCacheOption
```

Sets the eviction policy when cache is full.

- **Options**: `"LRU"`, `"LFU"`, `"FIFO"`, `"Random"`
- **Default**: `"LRU"`
- **Example**: `WithEvictionPolicy("LFU")`

##### WithHashFunction

```go
func WithHashFunction(fn func(string) uint32) ShardedCacheOption
```

Sets a custom hash function for shard selection.

##### WithMetricsCollector

```go
func WithMetricsCollector(collector MetricsCollector) ShardedCacheOption
```

Enables metrics collection.

##### WithAutoTuning

```go
func WithAutoTuning(enabled bool) ShardedCacheOption
```

Enables automatic performance tuning based on metrics.

##### WithMaxMemoryMB

```go
func WithMaxMemoryMB(mb int) ShardedCacheOption
```

Sets maximum memory usage in megabytes.

##### WithCompression

```go
func WithCompression(enabled bool) ShardedCacheOption
```

Enables value compression for memory efficiency.

##### WithWarmupData

```go
func WithWarmupData(data map[string]interface{}) ShardedCacheOption
```

Pre-populates cache with initial data.

---

## LLM Module Options

### Package: `github.com/kart-io/goagent/llm`

#### Option Type

```go
type ClientOption func(*Config)
```

#### Basic Configuration Options

##### WithProvider

```go
func WithProvider(provider Provider) ClientOption
```

Sets the LLM provider.

**Providers**:
- `ProviderOpenAI`
- `ProviderAnthropic`
- `ProviderGemini`
- `ProviderDeepSeek`
- `ProviderKimi`
- `ProviderSiliconFlow`
- `ProviderOllama`
- `ProviderCohere`
- `ProviderHuggingFace`

##### WithAPIKey

```go
func WithAPIKey(apiKey string) ClientOption
```

Sets the API key for the provider.

##### WithModel

```go
func WithModel(model string) ClientOption
```

Sets the model to use.

**Examples**:
- OpenAI: `"gpt-4"`, `"gpt-3.5-turbo"`
- Anthropic: `"claude-3-opus-20240229"`
- Gemini: `"gemini-pro"`

##### WithBaseURL

```go
func WithBaseURL(baseURL string) ClientOption
```

Sets a custom API endpoint.

#### Generation Parameters

##### WithMaxTokens

```go
func WithMaxTokens(maxTokens int) ClientOption
```

Sets the maximum number of tokens to generate.

- **Range**: 1 - model maximum
- **Default**: 2000

##### WithTemperature

```go
func WithTemperature(temperature float64) ClientOption
```

Sets the sampling temperature.

- **Range**: 0.0 - 2.0
- **Default**: 0.7

##### WithTopP

```go
func WithTopP(topP float64) ClientOption
```

Sets the nucleus sampling parameter.

- **Range**: 0.0 - 1.0
- **Default**: 1.0

##### WithSystemPrompt

```go
func WithSystemPrompt(prompt string) ClientOption
```

Sets the default system prompt.

#### Network and Reliability Options

##### WithTimeout

```go
func WithTimeout(timeout time.Duration) ClientOption
```

Sets the request timeout.

- **Default**: 60 seconds
- **Example**: `WithTimeout(2 * time.Minute)`

##### WithRetryCount

```go
func WithRetryCount(retryCount int) ClientOption
```

Sets the number of retry attempts.

- **Default**: 0 (no retries)
- **Example**: `WithRetryCount(3)`

##### WithRetryDelay

```go
func WithRetryDelay(delay time.Duration) ClientOption
```

Sets the delay between retry attempts.

- **Default**: 1 second
- **Example**: `WithRetryDelay(2 * time.Second)`

##### WithRateLimiting

```go
func WithRateLimiting(requestsPerMinute int) ClientOption
```

Configures rate limiting.

- **Example**: `WithRateLimiting(100)`

##### WithProxy

```go
func WithProxy(proxyURL string) ClientOption
```

Sets a proxy URL for requests.

#### Caching Options

##### WithCache

```go
func WithCache(enabled bool, ttl time.Duration) ClientOption
```

Enables response caching.

- **Parameters**:
  - `enabled` - Enable/disable caching
  - `ttl` - Cache time-to-live
- **Example**: `WithCache(true, 10*time.Minute)`

#### Streaming Options

##### WithStreamingEnabled

```go
func WithStreamingEnabled(enabled bool) ClientOption
```

Enables streaming responses.

#### Custom Headers

##### WithCustomHeaders

```go
func WithCustomHeaders(headers map[string]string) ClientOption
```

Sets custom HTTP headers.

```go
WithCustomHeaders(map[string]string{
    "X-Request-ID": "123",
    "X-User-ID": "user-456",
})
```

#### Organization

##### WithOrganizationID

```go
func WithOrganizationID(orgID string) ClientOption
```

Sets the organization ID (for OpenAI).

### Preset Options

##### WithPreset

```go
func WithPreset(preset PresetOption) ClientOption
```

Applies a pre-configured preset.

**Presets**:

| Preset | Model | MaxTokens | Temperature | Cache | Retry |
|--------|-------|-----------|-------------|-------|-------|
| `PresetDevelopment` | gpt-3.5-turbo | 1000 | 0.5 | No | 1 |
| `PresetProduction` | gpt-4 | 2000 | 0.7 | Yes (5m) | 3 |
| `PresetLowCost` | gpt-3.5-turbo | 500 | 0.3 | Yes (10m) | 1 |
| `PresetHighQuality` | gpt-4-turbo | 4000 | 0.8 | No | 3 |
| `PresetFast` | gpt-3.5-turbo-16k | 1000 | 0.3 | No | 1 |

##### WithProviderPreset

```go
func WithProviderPreset(provider Provider) ClientOption
```

Applies provider-specific default configuration.

##### WithUseCase

```go
func WithUseCase(useCase UseCase) ClientOption
```

Optimizes configuration for specific use cases.

**Use Cases**:

| UseCase | Temperature | MaxTokens | TopP | Notes |
|---------|------------|-----------|------|-------|
| `UseCaseChat` | 0.7 | 1500 | 0.9 | Conversational |
| `UseCaseCodeGeneration` | 0.2 | 2500 | 0.95 | Consistent output |
| `UseCaseTranslation` | 0.3 | 2000 | 1.0 | Accurate translation |
| `UseCaseSummarization` | 0.3 | 500 | 0.9 | Concise summaries |
| `UseCaseAnalysis` | 0.5 | 3000 | 0.95 | Detailed analysis |
| `UseCaseCreativeWriting` | 0.9 | 4000 | 0.95 | Creative output |

---

## Factory Methods

### Package: `github.com/kart-io/goagent/llm/providers`

#### ClientFactory

```go
type ClientFactory struct{}

func NewClientFactory() *ClientFactory
```

Creates a new client factory.

#### CreateClient

```go
func (f *ClientFactory) CreateClient(config *llm.Config) (llm.Client, error)
```

Creates a client from a configuration struct.

#### CreateClientWithOptions

```go
func (f *ClientFactory) CreateClientWithOptions(opts ...llm.ClientOption) (llm.Client, error)
```

Creates a client using options.

### Convenience Factory Methods

#### CreateOpenAIClient

```go
func CreateOpenAIClient(apiKey string, opts ...llm.ClientOption) (llm.Client, error)
```

Creates an OpenAI client with options.

#### CreateAnthropicClient

```go
func CreateAnthropicClient(apiKey string, opts ...llm.ClientOption) (llm.Client, error)
```

Creates an Anthropic client with options.

#### CreateGeminiClient

```go
func CreateGeminiClient(apiKey string, opts ...llm.ClientOption) (llm.Client, error)
```

Creates a Gemini client with options.

#### CreateOllamaClient

```go
func CreateOllamaClient(model string, opts ...llm.ClientOption) (llm.Client, error)
```

Creates an Ollama client for local models.

#### CreateClientForUseCase

```go
func CreateClientForUseCase(
    provider llm.Provider,
    apiKey string,
    useCase llm.UseCase,
    opts ...llm.ClientOption,
) (llm.Client, error)
```

Creates a client optimized for a specific use case.

#### CreateProductionClient

```go
func CreateProductionClient(
    provider llm.Provider,
    apiKey string,
    opts ...llm.ClientOption,
) (llm.Client, error)
```

Creates a production-ready client with:
- Retry mechanism (3 attempts)
- Caching (10 minutes TTL)
- Production preset

#### CreateDevelopmentClient

```go
func CreateDevelopmentClient(
    provider llm.Provider,
    apiKey string,
    opts ...llm.ClientOption,
) (llm.Client, error)
```

Creates a development client with:
- Development preset
- Lower costs
- Faster responses

---

## Builder Pattern

### OpenAI Builder

```go
type OpenAIBuilder struct {
    config *llm.Config
}

func NewOpenAIBuilder() *OpenAIBuilder
```

#### Builder Methods

All builder methods return `*OpenAIBuilder` for chaining:

```go
func (b *OpenAIBuilder) WithAPIKey(apiKey string) *OpenAIBuilder
func (b *OpenAIBuilder) WithModel(model string) *OpenAIBuilder
func (b *OpenAIBuilder) WithTemperature(temp float64) *OpenAIBuilder
func (b *OpenAIBuilder) WithMaxTokens(tokens int) *OpenAIBuilder
func (b *OpenAIBuilder) WithPreset(preset llm.PresetOption) *OpenAIBuilder
func (b *OpenAIBuilder) WithRetry(count int, delay time.Duration) *OpenAIBuilder
func (b *OpenAIBuilder) WithCache(ttl time.Duration) *OpenAIBuilder
func (b *OpenAIBuilder) WithUseCase(useCase llm.UseCase) *OpenAIBuilder
func (b *OpenAIBuilder) Build() (llm.Client, error)
```

#### Example

```go
client, err := providers.NewOpenAIBuilder().
    WithAPIKey("your-api-key").
    WithModel("gpt-4").
    WithTemperature(0.7).
    WithMaxTokens(2000).
    WithPreset(llm.PresetProduction).
    WithRetry(3, 2*time.Second).
    WithCache(15*time.Minute).
    Build()
```

---

## Type Definitions

### Cache Types

```go
type PerformanceProfile int

const (
    PerformanceLowLatency PerformanceProfile = iota
    PerformanceHighThroughput
    PerformanceBalanced
    PerformanceMemoryEfficient
)

type WorkloadType int

const (
    WorkloadReadHeavy WorkloadType = iota
    WorkloadWriteHeavy
    WorkloadMixed
    WorkloadBursty
)
```

### LLM Types

```go
type Provider string

const (
    ProviderOpenAI      Provider = "openai"
    ProviderAnthropic   Provider = "anthropic"
    ProviderGemini      Provider = "gemini"
    ProviderDeepSeek    Provider = "deepseek"
    ProviderKimi        Provider = "kimi"
    ProviderSiliconFlow Provider = "siliconflow"
    ProviderOllama      Provider = "ollama"
    ProviderCohere      Provider = "cohere"
    ProviderHuggingFace Provider = "huggingface"
)

type PresetOption int

const (
    PresetDevelopment PresetOption = iota
    PresetProduction
    PresetLowCost
    PresetHighQuality
    PresetFast
)

type UseCase int

const (
    UseCaseChat UseCase = iota
    UseCaseCodeGeneration
    UseCaseTranslation
    UseCaseSummarization
    UseCaseAnalysis
    UseCaseCreativeWriting
)
```

### Configuration Structs

#### ShardedCacheConfig

```go
type ShardedCacheConfig struct {
    ShardCount           uint32
    Capacity            int
    CleanupInterval     time.Duration
    CleanupBatchSize    int
    TTL                 time.Duration
    EvictionPolicy      string
    HashFunction        func(string) uint32
    MetricsCollector    MetricsCollector
    EnableAutoTuning    bool
    MaxRetries          int
    RetryDelay          time.Duration
    WarmupData          map[string]interface{}
    MaxMemoryMB         int
    EnableCompression   bool
    CompressionLevel    int
    LockBias            float64
    AdaptiveCleanup     bool
    CleanupThreshold    float64
}
```

#### LLM Config

```go
type Config struct {
    // Provider
    Provider       Provider
    APIKey         string
    BaseURL        string
    OrganizationID string

    // Model
    Model       string
    MaxTokens   int
    Temperature float64
    TopP        float64

    // Network
    Timeout      int
    RetryCount   int
    RetryDelay   time.Duration
    ProxyURL     string

    // Features
    SystemPrompt     string
    CacheEnabled     bool
    CacheTTL        time.Duration
    StreamingEnabled bool
    RateLimitRPM    int

    // Custom
    CustomHeaders map[string]string
}
```

---

## Usage Examples

### Cache with Auto-Tuning

```go
cache := tools.NewShardedCache(
    tools.WithPerformanceProfile(tools.PerformanceHighThroughput),
    tools.WithAutoTuning(true),
    tools.WithMetricsCollector(prometheus.NewCollector()),
    tools.WithMaxMemoryMB(2048),
)
```

### Production LLM Client

```go
client, err := providers.CreateProductionClient(
    llm.ProviderOpenAI,
    os.Getenv("OPENAI_API_KEY"),
    llm.WithModel("gpt-4"),
    llm.WithRateLimiting(1000),
    llm.WithCustomHeaders(map[string]string{
        "X-Application": "production-app",
    }),
)
```

### Use Case Specific Client

```go
codeClient, err := providers.CreateClientForUseCase(
    llm.ProviderOpenAI,
    apiKey,
    llm.UseCaseCodeGeneration,
    llm.WithModel("gpt-4"), // Override default
    llm.WithMaxTokens(4000), // More tokens for code
)
```

---

## Error Handling

Both cache and LLM options validate inputs and return errors for invalid configurations:

```go
// Cache validation
cache := NewShardedCache(
    WithShardCount(0), // Will use default
    WithCapacity(-1),  // Will use default
)

// LLM validation
client, err := factory.CreateClientWithOptions(
    llm.WithProvider(llm.ProviderOpenAI),
    llm.WithAPIKey(""), // Error: OpenAI requires API key
)
if err != nil {
    log.Fatal(err)
}
```

---

## Thread Safety

All option functions are safe to use concurrently as they only modify the configuration during construction. Once created, both cache and LLM clients are thread-safe for concurrent use.

---

## Performance Considerations

### Cache Performance

- **Shard Count**: More shards reduce contention but increase memory overhead
- **Cleanup Interval**: Shorter intervals free memory faster but consume more CPU
- **Batch Size**: Larger batches are more efficient but may cause latency spikes

### LLM Performance

- **Caching**: Dramatically reduces latency for repeated queries
- **Retry**: Improves reliability but increases worst-case latency
- **Streaming**: Reduces time-to-first-token for better UX

---

## Migration from Old Patterns

See [Option Pattern Migration Guide](./OPTION_PATTERN_MIGRATION.md) for detailed migration instructions.

---

## Best Practices

See [Option Pattern Best Practices](./OPTION_PATTERN_BEST_PRACTICES.md) for detailed guidance.