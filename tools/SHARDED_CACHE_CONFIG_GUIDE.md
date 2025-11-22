# Sharded Cache Configuration Guide

## Overview

The sharded cache implementation provides a high-performance, thread-safe caching solution that distributes data across multiple shards to minimize lock contention and maximize throughput. This guide explains how to configure and optimize the cache for different workloads.

## Quick Start

### Basic Usage with Default Configuration

```go
// Create cache with default settings
cache := NewShardedToolCacheWithOptions()
defer cache.Close()
```

### Using Performance Profiles

```go
// Optimize for low latency
cache := NewShardedToolCacheWithOptions(
    WithPerformanceProfile(LowLatencyProfile),
    WithCapacity(50000),
)

// Optimize for high throughput
cache := NewShardedToolCacheWithOptions(
    WithPerformanceProfile(HighThroughputProfile),
    WithCapacity(100000),
)
```

### Configuring for Specific Workloads

```go
// Read-heavy workload (90%+ reads)
cache := NewShardedToolCacheWithOptions(
    WithWorkloadType(ReadHeavyWorkload),
    WithCapacity(200000),
    WithDefaultTTL(30 * time.Minute),
)

// Bursty traffic patterns
cache := NewShardedToolCacheWithOptions(
    WithWorkloadType(BurstyWorkload),
    WithAutoTuning(true),
    WithMaxShardConcurrency(100),
)
```

## Configuration Options

### Core Options

#### WithShardCount(count uint32)
Sets the number of cache shards. More shards reduce contention but increase memory overhead.

**Recommendations:**
- Light load (< 100 req/s): 8-16 shards
- Medium load (100-1000 req/s): 32-64 shards
- Heavy load (> 1000 req/s): 128-256 shards
- Auto-detect (0): Uses CPU cores × 4

```go
cache := NewShardedToolCacheWithOptions(
    WithShardCount(64), // 64 shards
)

// Auto-detect based on CPU cores
cache := NewShardedToolCacheWithOptions(
    WithShardCount(0), // Will use runtime.NumCPU() * 4
)
```

#### WithCapacity(capacity int)
Sets the total cache capacity, distributed evenly across shards.

```go
cache := NewShardedToolCacheWithOptions(
    WithCapacity(100000), // Total of 100,000 entries
)
```

#### WithDefaultTTL(ttl time.Duration)
Sets the default time-to-live for cache entries.

```go
cache := NewShardedToolCacheWithOptions(
    WithDefaultTTL(15 * time.Minute),
)
```

### Performance Options

#### WithEvictionPolicy(policy EvictionPolicy)
Configures how entries are evicted when cache is full.

Available policies:
- `LRUEviction`: Least Recently Used (default)
- `LFUEviction`: Least Frequently Used
- `FIFOEviction`: First In, First Out
- `RandomEviction`: Random eviction

```go
cache := NewShardedToolCacheWithOptions(
    WithEvictionPolicy(LFUEviction),
)
```

#### WithCleanupStrategy(strategy CleanupStrategy)
Determines when and how expired entries are removed.

Strategies:
- `PeriodicCleanup`: Fixed interval cleanup
- `LazyCleanup`: Cleanup only on access
- `AdaptiveCleanup`: Adjusts frequency based on load
- `HybridCleanup`: Combines periodic and lazy cleanup

```go
cache := NewShardedToolCacheWithOptions(
    WithCleanupStrategy(AdaptiveCleanup),
    WithCleanupInterval(2 * time.Minute),
)
```

#### WithAutoTuning(enabled bool)
Enables automatic performance tuning based on observed patterns.

```go
cache := NewShardedToolCacheWithOptions(
    WithAutoTuning(true),
    WithMetrics(true), // Required for auto-tuning
)
```

### Advanced Options

#### WithMaxShardConcurrency(max int)
Limits concurrent operations per shard to prevent overload.

```go
cache := NewShardedToolCacheWithOptions(
    WithMaxShardConcurrency(100),
)
```

#### WithWarmup(entries map[string]*ToolOutput)
Pre-populates cache with initial data.

```go
warmupData := map[string]*ToolOutput{
    "key1": &ToolOutput{Result: "cached value 1"},
    "key2": &ToolOutput{Result: "cached value 2"},
}

cache := NewShardedToolCacheWithOptions(
    WithWarmup(warmupData),
)
```

#### WithCompressionThreshold(bytes int)
Enables compression for entries larger than threshold.

```go
cache := NewShardedToolCacheWithOptions(
    WithCompressionThreshold(1024), // Compress entries > 1KB
)
```

## Performance Profiles

### Low Latency Profile
Optimizes for minimal response time:
- More shards (CPU × 8)
- Lazy cleanup
- Least-loaded shard selection
- No concurrency limits

### High Throughput Profile
Optimizes for maximum operations per second:
- Balanced shards (CPU × 4)
- Adaptive cleanup
- Hash-based distribution
- LRU eviction

### Balanced Profile
Provides balanced performance:
- 32 shards (default)
- Hybrid cleanup
- Standard hash distribution
- LRU eviction

### Memory Efficient Profile
Minimizes memory usage:
- Fewer shards (CPU × 2)
- Aggressive periodic cleanup
- LFU eviction
- Optional compression

## Workload-Specific Configuration

### Read-Heavy Workload
For applications with 90%+ read operations:
- More shards for parallelism
- Longer cleanup intervals
- Lazy cleanup strategy

### Write-Heavy Workload
For write-dominated patterns:
- Moderate shard count
- Frequent cleanup
- FIFO eviction for speed

### Mixed Workload
For balanced read/write patterns:
- Standard configuration
- Hybrid cleanup
- 1-minute cleanup interval

### Bursty Workload
For traffic with sudden spikes:
- Higher shard count
- Adaptive cleanup
- Auto-tuning enabled
- Concurrency limits

## Sizing Recommendations

### Getting Shard Count Recommendations

```go
recommendation := GetShardCountRecommendation(expectedQPS)
fmt.Printf("For %d QPS, use %d shards\n",
    recommendation.ExpectedQPS,
    recommendation.RecommendedCount)
fmt.Printf("Rationale: %s\n", recommendation.Rationale)

cache := NewShardedToolCacheWithOptions(
    WithShardCount(recommendation.RecommendedCount),
)
```

### Getting Cleanup Interval Recommendations

```go
recommendation := GetCleanupIntervalRecommendation(
    cacheSize,     // Total capacity
    ttl,           // Default TTL
    churnRate,     // Expected % change per minute
)

cache := NewShardedToolCacheWithOptions(
    WithCleanupInterval(recommendation.RecommendedInterval),
)
```

## Dynamic Configuration Based on System Resources

```go
cpuCores := runtime.NumCPU()
var options []ShardedCacheOption

if cpuCores <= 4 {
    // Small system
    options = append(options,
        WithShardCount(16),
        WithCapacity(5000),
        WithPerformanceProfile(MemoryEfficientProfile),
    )
} else if cpuCores <= 8 {
    // Medium system
    options = append(options,
        WithShardCount(32),
        WithCapacity(20000),
        WithPerformanceProfile(BalancedProfile),
    )
} else {
    // Large system
    options = append(options,
        WithShardCount(0), // Auto-detect
        WithCapacity(100000),
        WithPerformanceProfile(HighThroughputProfile),
        WithAutoTuning(true),
    )
}

cache := NewShardedToolCacheWithOptions(options...)
```

## Monitoring and Metrics

### Enabling Metrics

```go
cache := NewShardedToolCacheWithOptions(
    WithMetrics(true),
    WithAutoTuning(true), // Optional: enables auto-tuning
)

// Get statistics
stats := cache.GetStats()
hitRate := float64(stats.Hits.Load()) /
    float64(stats.Hits.Load() + stats.Misses.Load())

fmt.Printf("Hit Rate: %.2f%%\n", hitRate * 100)
fmt.Printf("Evictions: %d\n", stats.Evicts.Load())
```

### Getting Numeric Stats

```go
hits, misses, evicts, invalidations := cache.GetStatsValues()
fmt.Printf("Performance: H:%d M:%d E:%d I:%d\n",
    hits, misses, evicts, invalidations)
```

## Best Practices

1. **Start with profiles**: Use performance profiles for initial configuration
2. **Monitor and adjust**: Enable metrics and auto-tuning for production
3. **Size appropriately**: Use recommendation functions for sizing
4. **Test under load**: Benchmark with realistic workloads
5. **Consider memory**: Balance performance with memory constraints

## Example Configurations

### API Gateway Cache
```go
cache := NewShardedToolCacheWithOptions(
    WithPerformanceProfile(LowLatencyProfile),
    WithWorkloadType(ReadHeavyWorkload),
    WithCapacity(100000),
    WithDefaultTTL(5 * time.Minute),
    WithAutoTuning(true),
)
```

### Analytics Processing
```go
cache := NewShardedToolCacheWithOptions(
    WithPerformanceProfile(HighThroughputProfile),
    WithWorkloadType(MixedWorkload),
    WithCapacity(500000),
    WithDefaultTTL(30 * time.Minute),
    WithCompressionThreshold(2048),
)
```

### Mobile Backend
```go
cache := NewShardedToolCacheWithOptions(
    WithWorkloadType(BurstyWorkload),
    WithCapacity(50000),
    WithAutoTuning(true),
    WithMaxShardConcurrency(100),
    WithDefaultTTL(10 * time.Minute),
)
```

## Performance Benchmarks

On a typical 8-core system:
- Default configuration: ~500K ops/sec
- Low latency profile: ~800K ops/sec (p99 < 1ms)
- High throughput profile: ~1M ops/sec
- Memory efficient profile: ~300K ops/sec (50% less memory)

Actual performance depends on:
- CPU cores and speed
- Cache hit rate
- Entry size
- Workload patterns