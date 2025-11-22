# Performance Tuning Guide for Option Pattern

## Overview

This guide provides detailed performance tuning recommendations for GoAgent's cache and LLM modules using the Option pattern. Learn how to optimize for your specific workload and requirements.

## Quick Reference

### Cache Performance Matrix

| Scenario | QPS | Shards | Cleanup | Eviction | Memory |
|----------|-----|--------|---------|----------|---------|
| Low Traffic | <100 | CPU×1 | 10 min | FIFO | Low |
| Medium Traffic | 100-1K | CPU×2 | 5 min | LRU | Medium |
| High Traffic | 1K-10K | CPU×4 | 2 min | LRU | High |
| Very High Traffic | >10K | CPU×8 | 1 min | LFU | High |

### LLM Latency Optimization

| Optimization | Latency Reduction | Trade-off |
|--------------|------------------|-----------|
| Enable Caching | 95-99% | Memory usage |
| Use Streaming | 50-70% (TTFT) | Complexity |
| Lower Max Tokens | 20-40% | Output length |
| Use Faster Model | 30-60% | Quality/Cost |
| Parallel Requests | 40-60% | Rate limits |

## Cache Tuning Guide

### 1. Read-Heavy Workloads (90%+ reads)

**Characteristics**:
- High cache hit ratio expected
- Minimal write contention
- Data changes infrequently

**Optimal Configuration**:

```go
cache := NewShardedCache(
    WithWorkloadType(WorkloadReadHeavy),
    WithShardCount(uint32(runtime.NumCPU() * 4)), // More shards for read parallelism
    WithCleanupInterval(10 * time.Minute),        // Less frequent cleanup
    WithEvictionPolicy("LRU"),                    // LRU works well for reads
    WithCapacity(100000),                         // Larger capacity for hit rate
    WithTTL(1 * time.Hour),                      // Longer TTL acceptable
)
```

**Performance Expectations**:
- Cache hit rate: >95%
- Read latency: <100μs p99
- Memory usage: High but stable

### 2. Write-Heavy Workloads (50%+ writes)

**Characteristics**:
- High insertion/update rate
- Lower cache hit ratio
- Frequent invalidations

**Optimal Configuration**:

```go
cache := NewShardedCache(
    WithWorkloadType(WorkloadWriteHeavy),
    WithShardCount(uint32(runtime.NumCPU() * 2)), // Fewer shards to reduce overhead
    WithCleanupInterval(2 * time.Minute),         // Frequent cleanup
    WithCleanupBatchSize(1000),                   // Large batches for efficiency
    WithEvictionPolicy("FIFO"),                   // Simple eviction for speed
    WithCapacity(10000),                          // Smaller capacity
    WithTTL(5 * time.Minute),                    // Short TTL
)
```

**Performance Expectations**:
- Write latency: <200μs p99
- Memory churn: High
- CPU usage: Moderate to high

### 3. Bursty Traffic

**Characteristics**:
- Sudden traffic spikes
- Variable load patterns
- Need for resilience

**Optimal Configuration**:

```go
cache := NewShardedCache(
    WithWorkloadType(WorkloadBursty),
    WithShardCount(uint32(runtime.NumCPU() * 8)), // Many shards for spike handling
    WithMaxRetries(5),                            // More retries during contention
    WithRetryDelay(10 * time.Millisecond),       // Short retry delay
    WithCapacity(50000),                          // Medium capacity
    WithAutoTuning(true),                         // Adaptive behavior
    WithAdaptiveCleanup(true),                    // Dynamic cleanup
)
```

**Performance Expectations**:
- Spike handling: 10x normal load
- Degradation: <20% during spikes
- Recovery time: <30 seconds

### 4. Memory-Constrained Environments

**Characteristics**:
- Limited memory (containers, embedded)
- Need for efficiency
- Cost optimization

**Optimal Configuration**:

```go
cache := NewShardedCache(
    WithPerformanceProfile(PerformanceMemoryEfficient),
    WithShardCount(uint32(runtime.NumCPU())),     // Minimal shards
    WithCapacity(1000),                           // Small capacity
    WithCleanupInterval(1 * time.Minute),        // Aggressive cleanup
    WithEvictionPolicy("LFU"),                    // Keep most used items
    WithMaxMemoryMB(100),                         // Hard memory limit
    WithCompression(true),                        // Enable compression
    WithCompressionLevel(6),                      // Balanced compression
)
```

**Performance Expectations**:
- Memory usage: <100MB
- Cache hit rate: 60-80%
- CPU overhead: +10-20% (compression)

## LLM Tuning Guide

### 1. Interactive Chat Applications

**Requirements**:
- Low latency responses
- Natural conversation flow
- Cost efficiency

**Optimal Configuration**:

```go
client, err := providers.CreateClientForUseCase(
    llm.ProviderOpenAI,
    apiKey,
    llm.UseCaseChat,
    llm.WithModel("gpt-3.5-turbo"),          // Fast, cost-effective
    llm.WithMaxTokens(500),                   // Short responses
    llm.WithTemperature(0.7),                 // Natural variation
    llm.WithStreamingEnabled(true),           // Better UX
    llm.WithCache(true, 5*time.Minute),      // Cache recent conversations
    llm.WithTimeout(30*time.Second),          // Quick timeout
)
```

**Performance Metrics**:
- Time to first token: <500ms
- Full response: <2 seconds
- Cost per conversation: $0.01-0.05

### 2. Code Generation

**Requirements**:
- High accuracy
- Consistent formatting
- Complete implementations

**Optimal Configuration**:

```go
client, err := providers.CreateClientForUseCase(
    llm.ProviderOpenAI,
    apiKey,
    llm.UseCaseCodeGeneration,
    llm.WithModel("gpt-4"),                   // Better code quality
    llm.WithMaxTokens(3000),                  // Complete implementations
    llm.WithTemperature(0.2),                 // Consistent output
    llm.WithTopP(0.95),                       // Focused generation
    llm.WithSystemPrompt("You are an expert programmer..."),
    llm.WithRetryCount(3),                    // Ensure completion
    llm.WithCache(true, 1*time.Hour),        // Cache common patterns
)
```

**Performance Metrics**:
- Accuracy: >90% compilable code
- Response time: 5-10 seconds
- Cost per generation: $0.10-0.30

### 3. Real-Time Analysis

**Requirements**:
- Fast processing
- Streaming results
- High throughput

**Optimal Configuration**:

```go
client, err := providers.CreateOpenAIClient(
    apiKey,
    llm.WithModel("gpt-3.5-turbo-16k"),      // Fast with context
    llm.WithMaxTokens(1000),                  // Balanced output
    llm.WithTemperature(0.5),                 // Semi-deterministic
    llm.WithStreamingEnabled(true),           // Immediate feedback
    llm.WithTimeout(15*time.Second),          // Quick timeout
    llm.WithRateLimiting(1000),              // High throughput
    llm.WithRetryCount(1),                    // Minimal retries
)

// Use with connection pooling
client = wrapWithConnectionPool(client, 10) // 10 concurrent connections
```

**Performance Metrics**:
- Throughput: 100+ requests/minute
- p50 latency: <2 seconds
- p99 latency: <5 seconds

### 4. Document Processing

**Requirements**:
- Large context handling
- Accurate extraction
- Batch processing

**Optimal Configuration**:

```go
client, err := providers.CreateAnthropicClient(
    apiKey,
    llm.WithModel("claude-3-opus-20240229"),  // Large context window
    llm.WithMaxTokens(4000),                  // Detailed output
    llm.WithTemperature(0.3),                 // Accurate extraction
    llm.WithTimeout(5*time.Minute),           // Allow for long docs
    llm.WithCache(true, 24*time.Hour),       // Cache processed docs
    llm.WithRetryCount(5),                    // Ensure processing
    llm.WithRetryDelay(5*time.Second),       // Exponential backoff
)
```

**Performance Metrics**:
- Document size: Up to 100k tokens
- Processing time: 30-60 seconds
- Accuracy: >95% extraction

### 5. Cost-Optimized Operations

**Requirements**:
- Minimize API costs
- Acceptable quality
- High cache hit rate

**Optimal Configuration**:

```go
// Tiered approach
primary := providers.CreateOpenAIClient(
    apiKey,
    llm.WithModel("gpt-3.5-turbo"),
    llm.WithMaxTokens(500),
    llm.WithCache(true, 24*time.Hour),       // Aggressive caching
    llm.WithPreset(llm.PresetLowCost),
)

fallback := providers.CreateOpenAIClient(
    apiKey,
    llm.WithModel("gpt-4"),
    llm.WithMaxTokens(1000),
    llm.WithCache(true, 1*time.Hour),
)

// Use primary for simple queries, fallback for complex
client := NewTieredClient(primary, fallback, complexityChecker)
```

**Performance Metrics**:
- Cost reduction: 60-80%
- Cache hit rate: >70%
- Quality degradation: <10%

## Advanced Optimization Techniques

### 1. Adaptive Shard Count

Automatically adjust shards based on load:

```go
type AdaptiveCache struct {
    cache   *ShardedCache
    metrics *Metrics
}

func (a *AdaptiveCache) AutoScale() {
    ticker := time.NewTicker(1 * time.Minute)
    for range ticker.C {
        qps := a.metrics.GetQPS()
        currentShards := a.cache.GetShardCount()

        optimalShards := calculateOptimalShards(qps)
        if optimalShards != currentShards {
            a.cache.Resize(optimalShards)
        }
    }
}

func calculateOptimalShards(qps float64) uint32 {
    switch {
    case qps < 100:
        return uint32(runtime.NumCPU())
    case qps < 1000:
        return uint32(runtime.NumCPU() * 2)
    case qps < 10000:
        return uint32(runtime.NumCPU() * 4)
    default:
        return uint32(runtime.NumCPU() * 8)
    }
}
```

### 2. Intelligent Cache Warmup

Pre-populate cache with likely queries:

```go
func WarmupCache(cache *ShardedCache, llmClient llm.Client) error {
    // Load common queries from analytics
    commonQueries := loadCommonQueries()

    // Pre-generate responses
    for _, query := range commonQueries {
        response, err := llmClient.Chat(context.Background(), []llm.Message{
            llm.UserMessage(query),
        })
        if err != nil {
            continue
        }

        cache.Set(query, response, 24*time.Hour)
    }

    return nil
}
```

### 3. Request Batching

Batch multiple requests for efficiency:

```go
type BatchProcessor struct {
    client    llm.Client
    batchSize int
    interval  time.Duration
}

func (b *BatchProcessor) ProcessBatch(requests []Request) []Response {
    // Group similar requests
    groups := groupBySimilarity(requests)

    responses := make([]Response, len(requests))
    var wg sync.WaitGroup

    for _, group := range groups {
        wg.Add(1)
        go func(g []Request) {
            defer wg.Done()

            // Process group with single context
            batchResponse := b.client.BatchChat(context.Background(), g)

            // Distribute responses
            for i, req := range g {
                responses[req.Index] = batchResponse[i]
            }
        }(group)
    }

    wg.Wait()
    return responses
}
```

### 4. Circuit Breaker Pattern

Prevent cascade failures:

```go
type CircuitBreaker struct {
    client        llm.Client
    failureThreshold int
    resetTimeout     time.Duration
    failures         int
    lastFailure      time.Time
    state            string // "closed", "open", "half-open"
}

func (cb *CircuitBreaker) Call(ctx context.Context, messages []llm.Message) (*llm.Response, error) {
    if cb.state == "open" {
        if time.Since(cb.lastFailure) > cb.resetTimeout {
            cb.state = "half-open"
        } else {
            return nil, errors.New("circuit breaker open")
        }
    }

    response, err := cb.client.Chat(ctx, messages)

    if err != nil {
        cb.recordFailure()
        return nil, err
    }

    cb.reset()
    return response, nil
}
```

## Monitoring and Metrics

### Key Cache Metrics to Monitor

```go
type CacheMetrics struct {
    HitRate        float64
    MissRate       float64
    EvictionRate   float64
    AvgGetLatency  time.Duration
    AvgSetLatency  time.Duration
    MemoryUsage    int64
    ItemCount      int64
    ShardBalance   []int // Items per shard
}

// Alert thresholds
const (
    MinHitRate      = 0.80  // Alert if hit rate < 80%
    MaxGetLatency   = 1 * time.Millisecond
    MaxMemoryUsage  = 1 << 30 // 1GB
    MaxShardImbalance = 0.20  // 20% deviation
)
```

### Key LLM Metrics to Monitor

```go
type LLMMetrics struct {
    RequestRate     float64
    SuccessRate     float64
    AvgLatency      time.Duration
    P99Latency      time.Duration
    TokensPerSecond float64
    CostPerRequest  float64
    CacheHitRate    float64
    RetryRate       float64
}

// Alert thresholds
const (
    MinSuccessRate  = 0.95  // Alert if success < 95%
    MaxAvgLatency   = 5 * time.Second
    MaxP99Latency   = 30 * time.Second
    MaxCostPerRequest = 0.50 // $0.50
)
```

## Performance Testing

### Cache Benchmark

```go
func BenchmarkCache(b *testing.B) {
    configs := []struct {
        name string
        opts []ShardedCacheOption
    }{
        {
            "LowLatency",
            []ShardedCacheOption{
                WithPerformanceProfile(PerformanceLowLatency),
            },
        },
        {
            "HighThroughput",
            []ShardedCacheOption{
                WithPerformanceProfile(PerformanceHighThroughput),
            },
        },
    }

    for _, cfg := range configs {
        b.Run(cfg.name, func(b *testing.B) {
            cache := NewShardedCache(cfg.opts...)

            b.RunParallel(func(pb *testing.PB) {
                for pb.Next() {
                    key := fmt.Sprintf("key-%d", rand.Int())
                    cache.Set(key, "value", 1*time.Hour)
                    cache.Get(key)
                }
            })
        })
    }
}
```

### LLM Load Test

```go
func LoadTestLLM(client llm.Client, duration time.Duration, rps int) {
    ticker := time.NewTicker(time.Second / time.Duration(rps))
    defer ticker.Stop()

    timeout := time.After(duration)
    var wg sync.WaitGroup

    metrics := &LLMMetrics{}

    for {
        select {
        case <-ticker.C:
            wg.Add(1)
            go func() {
                defer wg.Done()

                start := time.Now()
                _, err := client.Chat(context.Background(), testMessages)
                elapsed := time.Since(start)

                metrics.Record(elapsed, err)
            }()

        case <-timeout:
            wg.Wait()
            metrics.Report()
            return
        }
    }
}
```

## Troubleshooting Performance Issues

### Cache Performance Issues

| Symptom | Possible Cause | Solution |
|---------|---------------|----------|
| Low hit rate | TTL too short | Increase TTL |
| High latency | Too few shards | Increase shard count |
| Memory growth | No eviction | Enable eviction policy |
| Uneven load | Poor hash function | Use better hash function |
| Lock contention | Write-heavy with few shards | Increase shards or use write-through cache |

### LLM Performance Issues

| Symptom | Possible Cause | Solution |
|---------|---------------|----------|
| High latency | Large model | Use smaller/faster model |
| Rate limit errors | Too many requests | Enable rate limiting |
| Timeouts | Long context | Reduce max tokens or increase timeout |
| High costs | No caching | Enable aggressive caching |
| Inconsistent output | High temperature | Lower temperature |

## Best Practices Summary

1. **Start with profiles**: Use performance profiles as starting points
2. **Monitor continuously**: Track key metrics in production
3. **Test under load**: Benchmark with realistic workloads
4. **Iterate based on data**: Adjust configuration based on metrics
5. **Plan for spikes**: Design for 10x normal load
6. **Cache aggressively**: Especially for LLM responses
7. **Use tiered approaches**: Different models for different needs
8. **Implement circuit breakers**: Prevent cascade failures
9. **Warm up caches**: Pre-populate with common data
10. **Document configurations**: Track what works for your use case

## Conclusion

Performance tuning is an iterative process. Start with the recommended configurations for your use case, monitor metrics, and adjust based on real-world performance. The Option pattern makes it easy to experiment with different configurations without code changes.