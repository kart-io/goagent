# Option Pattern Migration Guide

## Quick Start Migration Examples

This guide provides concrete migration examples for transitioning from the old configuration patterns to the new Option pattern in both cache and LLM modules.

## Cache Module Migration

### Basic Cache Creation

#### Before (Direct struct initialization):
```go
cache := &ShardedCache{
    shards:     make([]*CacheShard, 16),
    shardCount: 16,
    capacity:   1000,
}
for i := range cache.shards {
    cache.shards[i] = &CacheShard{
        items:    make(map[string]*CacheItem),
        capacity: 1000,
    }
}
```

#### After (Option pattern):
```go
cache := NewShardedCache(
    WithShardCount(16),
    WithCapacity(1000),
)
```

### Production Cache Setup

#### Before:
```go
config := &CacheConfig{
    ShardCount:      32,
    Capacity:        10000,
    CleanupInterval: 5 * time.Minute,
    EvictionPolicy:  "LRU",
}
cache := NewCacheWithConfig(config)

// Manually setup metrics
cache.metrics = NewMetrics()
cache.enableAutoTuning = true
```

#### After:
```go
cache := NewShardedCache(
    WithPerformanceProfile(PerformanceHighThroughput),
    WithCapacity(10000),
    WithEvictionPolicy("LRU"),
    WithMetricsCollector(prometheus.NewCollector()),
    WithAutoTuning(true),
)
```

### Workload-Specific Configuration

#### Before:
```go
// No built-in workload optimization
cache := NewCache()
// Manual tuning based on workload
if isReadHeavy {
    cache.SetShardCount(runtime.NumCPU() * 4)
    cache.SetCleanupInterval(10 * time.Minute)
}
```

#### After:
```go
cache := NewShardedCache(
    WithWorkloadType(WorkloadReadHeavy),
    // Automatically configured for read-heavy workloads
)
```

## LLM Module Migration

### Basic LLM Client

#### Before:
```go
config := &llm.Config{
    Provider:    llm.ProviderOpenAI,
    APIKey:      os.Getenv("OPENAI_API_KEY"),
    Model:       "gpt-3.5-turbo",
    MaxTokens:   1000,
    Temperature: 0.7,
}
client, err := llm.NewClient(config)
```

#### After:
```go
client, err := providers.CreateOpenAIClient(
    os.Getenv("OPENAI_API_KEY"),
    llm.WithModel("gpt-3.5-turbo"),
    llm.WithMaxTokens(1000),
    llm.WithTemperature(0.7),
)
```

### Production LLM Setup

#### Before:
```go
config := &llm.Config{
    Provider:    llm.ProviderOpenAI,
    APIKey:      apiKey,
    Model:       "gpt-4",
    MaxTokens:   2000,
    Temperature: 0.7,
    Timeout:     60,
}
client := llm.NewClient(config)

// Manually wrap with retry logic
retryClient := &RetryWrapper{
    Client:     client,
    MaxRetries: 3,
    Delay:      2 * time.Second,
}

// Manually add caching
cachedClient := &CacheWrapper{
    Client: retryClient,
    Cache:  cache,
    TTL:    10 * time.Minute,
}
```

#### After:
```go
client, err := providers.CreateProductionClient(
    llm.ProviderOpenAI,
    apiKey,
    llm.WithModel("gpt-4"),
    llm.WithMaxTokens(2000),
    // Retry and cache are automatically configured
)
```

### Multiple Provider Support

#### Before:
```go
var client llm.Client

switch provider {
case "openai":
    client = llm.NewOpenAIClient(&llm.OpenAIConfig{
        APIKey: openaiKey,
        Model:  "gpt-4",
    })
case "anthropic":
    client = llm.NewAnthropicClient(&llm.AnthropicConfig{
        APIKey: anthropicKey,
        Model:  "claude-2",
    })
case "gemini":
    client = llm.NewGeminiClient(&llm.GeminiConfig{
        APIKey: geminiKey,
        Model:  "gemini-pro",
    })
}
```

#### After:
```go
factory := providers.NewClientFactory()

client, err := factory.CreateClientWithOptions(
    llm.WithProvider(providerType),
    llm.WithAPIKey(apiKey),
    llm.WithModel(model),
)
```

### Use Case Specific Clients

#### Before:
```go
// Manually configure for code generation
codeGenClient := llm.NewClient(&llm.Config{
    Provider:    llm.ProviderOpenAI,
    APIKey:      apiKey,
    Model:       "gpt-4",
    Temperature: 0.2,  // Low for consistency
    MaxTokens:   2500, // Higher for code
    TopP:        0.95,
})

// Manually configure for creative writing
creativeClient := llm.NewClient(&llm.Config{
    Provider:    llm.ProviderOpenAI,
    APIKey:      apiKey,
    Model:       "gpt-4",
    Temperature: 0.9,  // High for creativity
    MaxTokens:   4000, // Higher for stories
    TopP:        0.95,
})
```

#### After:
```go
// Automatically optimized for code generation
codeGenClient, err := providers.CreateClientForUseCase(
    llm.ProviderOpenAI,
    apiKey,
    llm.UseCaseCodeGeneration,
)

// Automatically optimized for creative writing
creativeClient, err := providers.CreateClientForUseCase(
    llm.ProviderOpenAI,
    apiKey,
    llm.UseCaseCreativeWriting,
)
```

## Migration Helpers

### 1. Config to Options Converter

```go
// Helper function to convert old configs to new options
func MigrateConfig(oldConfig *llm.Config) []llm.ClientOption {
    var options []llm.ClientOption

    // Map all fields
    options = append(options, llm.WithProvider(oldConfig.Provider))
    options = append(options, llm.WithAPIKey(oldConfig.APIKey))

    if oldConfig.Model != "" {
        options = append(options, llm.WithModel(oldConfig.Model))
    }

    if oldConfig.MaxTokens > 0 {
        options = append(options, llm.WithMaxTokens(oldConfig.MaxTokens))
    }

    if oldConfig.Temperature > 0 {
        options = append(options, llm.WithTemperature(oldConfig.Temperature))
    }

    if oldConfig.Timeout > 0 {
        options = append(options, llm.WithTimeout(
            time.Duration(oldConfig.Timeout) * time.Second,
        ))
    }

    return options
}

// Usage
oldConfig := loadConfigFromFile()
options := MigrateConfig(oldConfig)
client, err := factory.CreateClientWithOptions(options...)
```

### 2. Backward Compatibility Wrapper

```go
// Temporary wrapper to support both old and new patterns
type CompatibleClient struct {
    factory *providers.ClientFactory
}

// Old method signature for compatibility
func (c *CompatibleClient) NewClient(config *llm.Config) (llm.Client, error) {
    options := MigrateConfig(config)
    return c.factory.CreateClientWithOptions(options...)
}

// New method with options
func (c *CompatibleClient) NewClientWithOptions(
    opts ...llm.ClientOption,
) (llm.Client, error) {
    return c.factory.CreateClientWithOptions(opts...)
}
```

### 3. Configuration File Migration

#### Old YAML Format:
```yaml
llm:
  provider: openai
  api_key: ${OPENAI_API_KEY}
  model: gpt-4
  max_tokens: 2000
  temperature: 0.7
  timeout: 60

cache:
  shard_count: 16
  capacity: 10000
  cleanup_interval: 300
```

#### New YAML Format with Presets:
```yaml
llm:
  preset: production
  provider: openai
  api_key: ${OPENAI_API_KEY}
  overrides:
    model: gpt-4
    max_tokens: 2000

cache:
  performance_profile: high_throughput
  overrides:
    capacity: 10000
```

#### Loading New Format:
```go
type NewConfig struct {
    LLM struct {
        Preset    string                 `yaml:"preset"`
        Provider  string                 `yaml:"provider"`
        APIKey    string                 `yaml:"api_key"`
        Overrides map[string]interface{} `yaml:"overrides"`
    } `yaml:"llm"`

    Cache struct {
        PerformanceProfile string                 `yaml:"performance_profile"`
        Overrides         map[string]interface{} `yaml:"overrides"`
    } `yaml:"cache"`
}

func LoadAndCreateClient(configPath string) (llm.Client, error) {
    var config NewConfig
    // Load YAML...

    var options []llm.ClientOption

    // Apply preset first
    if config.LLM.Preset != "" {
        preset := parsePreset(config.LLM.Preset)
        options = append(options, llm.WithPreset(preset))
    }

    // Apply base configuration
    options = append(options,
        llm.WithProvider(llm.Provider(config.LLM.Provider)),
        llm.WithAPIKey(config.LLM.APIKey),
    )

    // Apply overrides
    for key, value := range config.LLM.Overrides {
        switch key {
        case "model":
            options = append(options, llm.WithModel(value.(string)))
        case "max_tokens":
            options = append(options, llm.WithMaxTokens(value.(int)))
        // ... handle other overrides
        }
    }

    return providers.NewClientFactory().CreateClientWithOptions(options...)
}
```

## Step-by-Step Migration Process

### Phase 1: Preparation (Week 1-2)
1. Audit existing code for all Config struct usage
2. Identify custom configurations and patterns
3. Create migration helpers as shown above
4. Update documentation

### Phase 2: Parallel Support (Week 3-4)
1. Deploy new option-based constructors
2. Keep old constructors with deprecation notices
3. Update internal code to use new patterns
4. Test both patterns in parallel

### Phase 3: Migration (Week 5-8)
1. Migrate critical services first
2. Update all examples and documentation
3. Provide migration tools and scripts
4. Support teams during migration

### Phase 4: Cleanup (Week 9-10)
1. Remove deprecated methods
2. Clean up migration helpers
3. Final testing and validation
4. Update all documentation

## Common Migration Issues

### Issue 1: Missing Options
**Problem**: Old config had a field that doesn't have a corresponding option.

**Solution**:
```go
// Add custom option temporarily
func WithLegacyField(value string) llm.ClientOption {
    return func(c *llm.Config) {
        c.CustomFields["legacy_field"] = value
    }
}
```

### Issue 2: Complex Validation
**Problem**: Old code had complex validation logic between fields.

**Solution**:
```go
// Create a validation option
func WithValidation() llm.ClientOption {
    return func(c *llm.Config) {
        // Perform validation after all options are applied
        if c.Provider == llm.ProviderOpenAI && c.Model == "" {
            c.Model = "gpt-3.5-turbo" // Set default
        }
    }
}
```

### Issue 3: Configuration Serialization
**Problem**: Need to save configuration to file.

**Solution**:
```go
// Convert options back to config for serialization
func OptionsToConfig(opts ...llm.ClientOption) *llm.Config {
    config := llm.DefaultClientConfig()
    for _, opt := range opts {
        opt(config)
    }
    return config
}

// Save to file
func SaveConfiguration(opts ...llm.ClientOption) error {
    config := OptionsToConfig(opts...)
    data, err := json.Marshal(config)
    if err != nil {
        return err
    }
    return os.WriteFile("config.json", data, 0644)
}
```

## Testing During Migration

### Parallel Testing Strategy
```go
func TestMigrationCompatibility(t *testing.T) {
    // Old way
    oldConfig := &llm.Config{
        Provider:    llm.ProviderOpenAI,
        APIKey:      "test-key",
        Model:       "gpt-4",
        MaxTokens:   2000,
        Temperature: 0.7,
    }

    // New way
    newOptions := []llm.ClientOption{
        llm.WithProvider(llm.ProviderOpenAI),
        llm.WithAPIKey("test-key"),
        llm.WithModel("gpt-4"),
        llm.WithMaxTokens(2000),
        llm.WithTemperature(0.7),
    }

    // Convert old to new
    migratedOptions := MigrateConfig(oldConfig)

    // Both should produce same configuration
    config1 := llm.OptionsToConfig(newOptions...)
    config2 := llm.OptionsToConfig(migratedOptions...)

    assert.Equal(t, config1, config2)
}
```

## Rollback Plan

If issues arise during migration:

1. **Immediate Rollback**: Revert to using old constructors
   ```go
   // Feature flag for gradual rollout
   if useNewPattern {
       client, err = factory.CreateClientWithOptions(opts...)
   } else {
       client, err = llm.NewClient(config)
   }
   ```

2. **Partial Rollback**: Keep both patterns active
   ```go
   // Support both patterns simultaneously
   type HybridFactory struct {
       supportLegacy bool
   }

   func (f *HybridFactory) CreateClient(configOrOpts interface{}) (llm.Client, error) {
       switch v := configOrOpts.(type) {
       case *llm.Config:
           return f.createFromConfig(v)
       case []llm.ClientOption:
           return f.createFromOptions(v)
       default:
           return nil, errors.New("unsupported configuration type")
       }
   }
   ```

## Success Metrics

Monitor these metrics during migration:

1. **Error Rates**: Track initialization failures
2. **Performance**: Compare initialization time
3. **Memory Usage**: Ensure no memory leaks
4. **API Compatibility**: Zero breaking changes for existing code
5. **Developer Satisfaction**: Survey teams using the new pattern

## Next Steps

After successful migration:

1. Remove backward compatibility code
2. Archive old configuration files
3. Update all documentation
4. Train team on new patterns
5. Establish new best practices

## Support Resources

- [Option Pattern Best Practices](./OPTION_PATTERN_BEST_PRACTICES.md)
- [API Reference Documentation](../api/OPTIONS_API.md)
- [Example Repository](../../examples/option_pattern/)
- [Migration Support Channel](#slack-channel)

For questions or issues during migration, contact the platform team or file an issue in the repository.