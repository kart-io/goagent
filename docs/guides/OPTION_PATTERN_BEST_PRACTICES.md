# Option Pattern Best Practices Guide

## Overview

This guide documents best practices for using the Option pattern implementations in the GoAgent framework, covering both the cache and LLM modules. The Option pattern provides a flexible, backward-compatible way to configure complex objects while maintaining clean APIs.

## Table of Contents

1. [When to Use Option Pattern](#when-to-use-option-pattern)
2. [Cache Module Best Practices](#cache-module-best-practices)
3. [LLM Module Best Practices](#llm-module-best-practices)
4. [Performance Tuning Guidelines](#performance-tuning-guidelines)
5. [Migration Strategies](#migration-strategies)
6. [Common Pitfalls and Solutions](#common-pitfalls-and-solutions)
7. [Testing Guidelines](#testing-guidelines)

## When to Use Option Pattern

### Use Option Pattern When:

- **Many Optional Parameters**: Your constructor has more than 3-4 parameters, most of which are optional
- **Backward Compatibility**: You need to add new configuration options without breaking existing code
- **Fluent APIs**: You want to provide a readable, chainable configuration API
- **Complex Defaults**: Default values depend on other parameters or require computation
- **Presets and Profiles**: You want to provide pre-configured settings for common use cases

### Prefer Config Struct When:

- **Simple Configuration**: Only 2-3 required parameters with clear defaults
- **Serialization Required**: Configuration needs to be loaded from files (JSON, YAML)
- **Full Visibility**: Users need to see all configuration options at once
- **Validation Complexity**: Complex validation rules that span multiple fields

## Cache Module Best Practices

### 1. Choosing the Right Shard Count

```go
// For CPU-bound workloads (high computation per operation)
cache := sharded.NewShardedCache(
    sharded.WithShardCount(uint32(runtime.NumCPU())),
)

// For I/O-bound workloads (network calls, database queries)
cache := sharded.NewShardedCache(
    sharded.WithShardCount(uint32(runtime.NumCPU() * 4)),
)

// For memory-constrained environments
cache := sharded.NewShardedCache(
    sharded.WithPerformanceProfile(sharded.PerformanceMemoryEfficient),
)

// Let the system auto-tune based on metrics
cache := sharded.NewShardedCache(
    sharded.WithAutoTuning(true),
    sharded.WithMetricsCollector(collector),
)
```

### 2. Workload-Specific Configuration

```go
// Read-heavy workload (90% reads, 10% writes)
cache := sharded.NewShardedCache(
    sharded.WithWorkloadType(sharded.WorkloadReadHeavy),
)

// Write-heavy workload (high insertion rate)
cache := sharded.NewShardedCache(
    sharded.WithWorkloadType(sharded.WorkloadWriteHeavy),
    sharded.WithCleanupBatchSize(1000), // Process more items per cleanup
)

// Bursty traffic patterns
cache := sharded.NewShardedCache(
    sharded.WithWorkloadType(sharded.WorkloadBursty),
    sharded.WithMaxRetries(5), // More retries for lock acquisition
)
```

### 3. Memory Management

```go
// Configure memory limits
cache := sharded.NewShardedCache(
    sharded.WithCapacity(100000),        // Max items per shard
    sharded.WithEvictionPolicy("LRU"),   // Least Recently Used
    sharded.WithMaxMemoryMB(1024),       // 1GB memory limit
)

// Aggressive cleanup for memory-sensitive applications
cache := sharded.NewShardedCache(
    sharded.WithCleanupInterval(30 * time.Second),
    sharded.WithCleanupBatchSize(500),
)
```

### 4. Performance Profiles Guide

| Profile | Shards | Cleanup Interval | Use Case |
|---------|--------|------------------|----------|
| LowLatency | CPU×4 | 5 minutes | Real-time systems, gaming |
| HighThroughput | CPU×8 | 10 minutes | Batch processing, analytics |
| Balanced | CPU×2 | 2 minutes | General web applications |
| MemoryEfficient | CPU | 1 minute | Embedded systems, containers |

## LLM Module Best Practices

### 1. Provider-Specific Optimization

```go
// OpenAI with production settings
client, err := providers.CreateOpenAIClient(
    apiKey,
    llm.WithModel("gpt-4-turbo-preview"),
    llm.WithRetryCount(3),
    llm.WithCache(true, 10*time.Minute),
    llm.WithRateLimiting(100), // 100 requests per minute
)

// Anthropic Claude for long contexts
client, err := providers.CreateAnthropicClient(
    apiKey,
    llm.WithModel("claude-3-opus-20240229"),
    llm.WithMaxTokens(100000), // Claude supports very long contexts
    llm.WithTimeout(5 * time.Minute),
)

// Local Ollama for development
client, err := providers.CreateOllamaClient(
    "llama2",
    llm.WithBaseURL("http://localhost:11434"),
    llm.WithTimeout(2 * time.Minute), // Local models might be slower
)
```

### 2. Use Case Optimization

```go
// Code generation - low temperature, high quality
codeClient, err := providers.CreateClientForUseCase(
    llm.ProviderOpenAI,
    apiKey,
    llm.UseCaseCodeGeneration,
    llm.WithModel("gpt-4"), // Override default for better code quality
)

// Creative writing - high temperature, more tokens
creativeClient, err := providers.CreateClientForUseCase(
    llm.ProviderOpenAI,
    apiKey,
    llm.UseCaseCreativeWriting,
    llm.WithMaxTokens(8000), // Allow longer stories
)

// Translation - consistent output
translationClient, err := providers.CreateClientForUseCase(
    llm.ProviderOpenAI,
    apiKey,
    llm.UseCaseTranslation,
    llm.WithTemperature(0.1), // Very low for consistency
)
```

### 3. Environment-Based Configuration

```go
func createLLMClient() (llm.Client, error) {
    env := os.Getenv("ENVIRONMENT")

    switch env {
    case "production":
        return providers.CreateProductionClient(
            llm.ProviderOpenAI,
            os.Getenv("OPENAI_API_KEY"),
            llm.WithModel("gpt-4"),
            llm.WithCache(true, 1*time.Hour),
            llm.WithRateLimiting(1000),
            llm.WithRetryCount(5),
        )

    case "staging":
        return providers.CreateProductionClient(
            llm.ProviderOpenAI,
            os.Getenv("OPENAI_API_KEY"),
            llm.WithModel("gpt-3.5-turbo"), // Cheaper for staging
            llm.WithCache(true, 30*time.Minute),
            llm.WithRateLimiting(100),
        )

    default: // development
        return providers.CreateDevelopmentClient(
            llm.ProviderOllama,
            "", // Local, no API key needed
            llm.WithModel("llama2"),
        )
    }
}
```

### 4. Cost Optimization Strategies

```go
// Tiered approach - try cheaper models first
func createTieredClient() llm.Client {
    return &TieredClient{
        primary: createClient(llm.WithModel("gpt-3.5-turbo")),
        fallback: createClient(llm.WithModel("gpt-4")),
        criteria: func(prompt string) bool {
            // Use GPT-4 only for complex tasks
            return len(prompt) > 1000 || strings.Contains(prompt, "analyze")
        },
    }
}

// Aggressive caching for repeated queries
client, err := providers.CreateOpenAIClient(
    apiKey,
    llm.WithCache(true, 24*time.Hour), // Cache for 24 hours
    llm.WithPreset(llm.PresetLowCost),
)
```

## Performance Tuning Guidelines

### Cache Module Tuning

1. **Monitor Metrics**: Always enable metrics collection in production
   ```go
   cache := sharded.NewShardedCache(
       sharded.WithMetricsCollector(prometheus.NewCollector()),
       sharded.WithAutoTuning(true),
   )
   ```

2. **Shard Count Formula**:
   - Low contention: `shards = CPU cores`
   - Medium contention: `shards = CPU cores × 2`
   - High contention: `shards = CPU cores × 4`
   - Very high contention: `shards = CPU cores × 8`

3. **Cleanup Tuning**:
   - High memory pressure: Shorter intervals (30s - 1m)
   - Normal operation: Medium intervals (2m - 5m)
   - Low memory pressure: Longer intervals (10m - 30m)

### LLM Module Tuning

1. **Token Optimization**:
   ```go
   // Start with conservative limits
   client := createClient(llm.WithMaxTokens(1000))

   // Monitor actual usage
   response, _ := client.Chat(ctx, messages)
   if response.Usage.TotalTokens > 800 {
       // Increase limit if consistently hitting ceiling
       client = createClient(llm.WithMaxTokens(1500))
   }
   ```

2. **Retry Strategy**:
   ```go
   // For critical operations
   llm.WithRetryCount(5),
   llm.WithRetryDelay(2 * time.Second),
   llm.WithExponentialBackoff(true),

   // For non-critical operations
   llm.WithRetryCount(1),
   llm.WithRetryDelay(1 * time.Second),
   ```

3. **Rate Limiting**:
   ```go
   // Calculate based on quota
   quotaPerDay := 10000
   requestsPerMinute := quotaPerDay / (24 * 60) // ~7 RPM

   client := createClient(
       llm.WithRateLimiting(requestsPerMinute),
   )
   ```

## Migration Strategies

### From Config Struct to Options

#### Before (Config Struct):
```go
// Old approach
config := &llm.Config{
    Provider:    llm.ProviderOpenAI,
    APIKey:      "key",
    Model:       "gpt-4",
    MaxTokens:   2000,
    Temperature: 0.7,
}
client := llm.NewClient(config)
```

#### After (Option Pattern):
```go
// New approach
client, err := providers.CreateOpenAIClient(
    "key",
    llm.WithModel("gpt-4"),
    llm.WithMaxTokens(2000),
    llm.WithTemperature(0.7),
)
```

### Gradual Migration Strategy

1. **Phase 1**: Add option support alongside existing config
   ```go
   // Support both patterns temporarily
   func NewClient(config *Config, opts ...Option) Client {
       // Apply options to config
       for _, opt := range opts {
           opt(config)
       }
       return createClient(config)
   }
   ```

2. **Phase 2**: Deprecate config-only constructor
   ```go
   // Deprecated: Use NewClientWithOptions instead
   func NewClient(config *Config) Client {
       return NewClientWithOptions(ConfigToOptions(config)...)
   }
   ```

3. **Phase 3**: Remove deprecated methods after migration period

### Migration Helper Functions

```go
// Convert old config to options
func ConfigToOptions(cfg *Config) []Option {
    var opts []Option

    if cfg.Provider != "" {
        opts = append(opts, WithProvider(cfg.Provider))
    }
    if cfg.APIKey != "" {
        opts = append(opts, WithAPIKey(cfg.APIKey))
    }
    if cfg.Model != "" {
        opts = append(opts, WithModel(cfg.Model))
    }
    // ... convert other fields

    return opts
}

// Convert options to config (for serialization)
func OptionsToConfig(opts ...Option) *Config {
    cfg := DefaultConfig()
    for _, opt := range opts {
        opt(cfg)
    }
    return cfg
}
```

## Common Pitfalls and Solutions

### Pitfall 1: Option Order Dependencies

```go
// ❌ BAD: Preset overrides previous options
client := createClient(
    llm.WithMaxTokens(4000),        // This will be overridden!
    llm.WithPreset(llm.PresetFast), // Preset sets MaxTokens to 1000
)

// ✅ GOOD: Apply preset first, then customize
client := createClient(
    llm.WithPreset(llm.PresetFast), // Apply preset defaults
    llm.WithMaxTokens(4000),        // Override specific values
)
```

### Pitfall 2: Ignoring Return Errors

```go
// ❌ BAD: Ignoring error from factory
client, _ := providers.CreateOpenAIClient(apiKey, opts...)

// ✅ GOOD: Always check errors
client, err := providers.CreateOpenAIClient(apiKey, opts...)
if err != nil {
    return fmt.Errorf("failed to create client: %w", err)
}
```

### Pitfall 3: Mutating Shared Config

```go
// ❌ BAD: Modifying shared config
var defaultOpts = []Option{
    WithMaxTokens(1000),
}

func createClient() Client {
    opts := defaultOpts
    opts = append(opts, WithModel("gpt-4")) // Mutates defaultOpts!
    return NewClient(opts...)
}

// ✅ GOOD: Create new slice
func createClient() Client {
    opts := make([]Option, len(defaultOpts))
    copy(opts, defaultOpts)
    opts = append(opts, WithModel("gpt-4"))
    return NewClient(opts...)
}
```

### Pitfall 4: Over-Engineering Options

```go
// ❌ BAD: Too many granular options
cache := NewCache(
    WithShardCount(16),
    WithShardCapacity(1000),
    WithShardMaxSize(100),
    WithShardMinSize(10),
    WithShardGrowthFactor(1.5),
    // ... 20 more options
)

// ✅ GOOD: Use profiles for common configurations
cache := NewCache(
    WithPerformanceProfile(HighThroughput),
    WithCapacity(16000), // Only override what's needed
)
```

### Pitfall 5: Not Validating Option Combinations

```go
// ❌ BAD: Conflicting options accepted
client := createClient(
    llm.WithProvider(llm.ProviderOpenAI),
    llm.WithBaseURL("http://localhost:11434"), // Ollama URL for OpenAI?
)

// ✅ GOOD: Validate in constructor
func createClient(opts ...Option) (Client, error) {
    cfg := applyOptions(opts...)

    if cfg.Provider == ProviderOpenAI &&
       strings.Contains(cfg.BaseURL, "localhost") {
        return nil, errors.New("OpenAI provider with localhost URL")
    }

    return newClient(cfg), nil
}
```

## Testing Guidelines

### Testing Option Functions

```go
func TestWithMaxTokens(t *testing.T) {
    tests := []struct {
        name      string
        maxTokens int
        expected  int
    }{
        {"positive value", 1000, 1000},
        {"zero value", 0, 2000}, // Should use default
        {"negative value", -1, 2000}, // Should use default
    }

    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            cfg := DefaultConfig()
            opt := WithMaxTokens(tt.maxTokens)
            opt(cfg)

            assert.Equal(t, tt.expected, cfg.MaxTokens)
        })
    }
}
```

### Testing Option Combinations

```go
func TestOptionCombinations(t *testing.T) {
    // Test that later options override earlier ones
    cfg := NewConfigWithOptions(
        WithModel("gpt-3.5-turbo"),
        WithModel("gpt-4"), // Should override
    )
    assert.Equal(t, "gpt-4", cfg.Model)

    // Test preset with overrides
    cfg = NewConfigWithOptions(
        WithPreset(PresetFast),
        WithMaxTokens(2000), // Override preset value
    )
    assert.Equal(t, 2000, cfg.MaxTokens)
    assert.Equal(t, 0.3, cfg.Temperature) // From preset
}
```

### Testing Error Cases

```go
func TestInvalidOptions(t *testing.T) {
    // Test invalid combinations
    _, err := CreateClient(
        WithProvider(ProviderOpenAI),
        WithAPIKey(""), // OpenAI requires API key
    )
    assert.Error(t, err)
    assert.Contains(t, err.Error(), "API key required")
}
```

### Benchmark Options vs Config

```go
func BenchmarkOptions(b *testing.B) {
    b.Run("WithOptions", func(b *testing.B) {
        for i := 0; i < b.N; i++ {
            _ = NewConfigWithOptions(
                WithProvider(ProviderOpenAI),
                WithModel("gpt-4"),
                WithMaxTokens(2000),
                WithTemperature(0.7),
            )
        }
    })

    b.Run("ConfigStruct", func(b *testing.B) {
        for i := 0; i < b.N; i++ {
            _ = &Config{
                Provider:    ProviderOpenAI,
                Model:       "gpt-4",
                MaxTokens:   2000,
                Temperature: 0.7,
            }
        }
    })
}
```

## Summary

The Option pattern provides powerful flexibility for configuration while maintaining backward compatibility. Key takeaways:

1. **Use Options for Complex APIs**: When you have many optional parameters or need flexibility
2. **Provide Presets**: Make common use cases easy with pre-configured options
3. **Layer Options**: Apply presets first, then specific overrides
4. **Validate Combinations**: Ensure options work together correctly
5. **Document Defaults**: Make it clear what happens when options aren't specified
6. **Test Thoroughly**: Test individual options, combinations, and error cases
7. **Benchmark Performance**: Ensure option pattern doesn't add significant overhead
8. **Plan Migration**: Provide smooth transition path from old patterns

Following these best practices will help you create flexible, maintainable, and user-friendly APIs using the Option pattern.