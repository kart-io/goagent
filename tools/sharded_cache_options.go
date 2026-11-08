package tools

import (
	"runtime"
	"time"

	"github.com/kart-io/goagent/constants"
	agentErrors "github.com/kart-io/goagent/errors"
	"github.com/kart-io/goagent/interfaces"
)

// ShardedCacheOption is a functional option for configuring ShardedToolCache
type ShardedCacheOption func(*ShardedCacheConfig)

// DefaultShardedCacheConfig returns the default configuration
func DefaultShardedCacheConfig() ShardedCacheConfig {
	return ShardedCacheConfig{
		ShardCount:      constants.DefaultToolCacheShards,
		Capacity:        constants.DefaultToolCacheCapacity,
		DefaultTTL:      constants.DefaultToolCacheTTL,
		CleanupInterval: constants.DefaultToolCacheCleanupInterval,
		CleanupStrategy: PeriodicCleanup,
	}
}

// CleanupStrategy defines cleanup strategies
type CleanupStrategy int

const (
	// PeriodicCleanup performs cleanup at fixed intervals
	PeriodicCleanup CleanupStrategy = iota
	// LazyCleanup performs cleanup only on access
	LazyCleanup
	// AdaptiveCleanup adjusts cleanup frequency based on load
	AdaptiveCleanup
	// HybridCleanup combines periodic and lazy cleanup
	HybridCleanup
)

// WithShardCount sets the number of shards
// Recommended values:
// - Light load (< 100 req/s): 8-16 shards
// - Medium load (100-1000 req/s): 32-64 shards
// - Heavy load (> 1000 req/s): 128-256 shards
// - Auto: 0 (will use CPU cores * 4)
func WithShardCount(count uint32) ShardedCacheOption {
	return func(c *ShardedCacheConfig) {
		if count == 0 {
			// Auto-detect based on CPU cores
			c.ShardCount = uint32(runtime.NumCPU() * constants.DefaultToolCacheCPUShardMultiplier)
			// Ensure it's a power of 2
			c.ShardCount = nextPowerOfTwo(c.ShardCount)
		} else {
			// Ensure it's a power of 2 for optimal hash distribution
			c.ShardCount = nextPowerOfTwo(count)
		}
	}
}

// WithCapacity sets the total cache capacity
// The capacity is distributed evenly across shards
func WithCapacity(capacity int) ShardedCacheOption {
	return func(c *ShardedCacheConfig) {
		if capacity > 0 {
			c.Capacity = capacity
		}
	}
}

// WithDefaultTTL sets the default TTL for cache entries
func WithDefaultTTL(ttl time.Duration) ShardedCacheOption {
	return func(c *ShardedCacheConfig) {
		if ttl > 0 {
			c.DefaultTTL = ttl
		}
	}
}

// WithCleanupInterval sets the interval for periodic cleanup
func WithCleanupInterval(interval time.Duration) ShardedCacheOption {
	return func(c *ShardedCacheConfig) {
		c.CleanupInterval = interval
	}
}

// WithCleanupStrategy sets the cleanup strategy
func WithCleanupStrategy(strategy CleanupStrategy) ShardedCacheOption {
	return func(c *ShardedCacheConfig) {
		c.CleanupStrategy = strategy
	}
}

// WithWarmup enables cache warmup with specified entries
func WithWarmup(entries map[string]*interfaces.ToolOutput) ShardedCacheOption {
	return func(c *ShardedCacheConfig) {
		c.WarmupEntries = entries
	}
}

// PerformanceProfile defines preset configurations for different workloads
type PerformanceProfile int

const (
	// LowLatencyProfile optimizes for minimal response time
	LowLatencyProfile PerformanceProfile = iota
	// HighThroughputProfile optimizes for maximum throughput
	HighThroughputProfile
	// BalancedProfile provides balanced performance
	BalancedProfile
	// MemoryEfficientProfile minimizes memory usage
	MemoryEfficientProfile
)

// WithPerformanceProfile applies a preset performance profile
func WithPerformanceProfile(profile PerformanceProfile) ShardedCacheOption {
	return func(c *ShardedCacheConfig) {
		switch profile {
		case LowLatencyProfile:
			// More shards for less contention
			c.ShardCount = nextPowerOfTwo(uint32(runtime.NumCPU() * constants.ToolCacheLowLatencyCPUShardMultiplier))
			c.CleanupStrategy = LazyCleanup

		case HighThroughputProfile:
			// Balanced shards with efficient cleanup
			c.ShardCount = nextPowerOfTwo(uint32(runtime.NumCPU() * constants.ToolCacheHighThroughputCPUShardMultiplier))
			c.CleanupStrategy = AdaptiveCleanup

		case BalancedProfile:
			// Default balanced configuration
			c.ShardCount = constants.ToolCacheBalancedProfileShards
			c.CleanupStrategy = HybridCleanup

		case MemoryEfficientProfile:
			// Fewer shards, aggressive cleanup
			c.ShardCount = nextPowerOfTwo(uint32(runtime.NumCPU() * constants.ToolCacheMemoryEfficientCPUShardMultiplier))
			c.CleanupStrategy = PeriodicCleanup
			c.CleanupInterval = constants.ToolCacheMemoryEfficientCleanupInterval
		}
	}
}

// WorkloadType defines the type of workload the cache will handle
type WorkloadType int

const (
	// ReadHeavyWorkload for read-dominated workloads (90%+ reads)
	ReadHeavyWorkload WorkloadType = iota
	// WriteHeavyWorkload for write-dominated workloads
	WriteHeavyWorkload
	// MixedWorkload for balanced read/write workloads
	MixedWorkload
	// BurstyWorkload for workloads with traffic bursts
	BurstyWorkload
)

// WithWorkloadType optimizes the cache for specific workload patterns
func WithWorkloadType(workload WorkloadType) ShardedCacheOption {
	return func(c *ShardedCacheConfig) {
		switch workload {
		case ReadHeavyWorkload:
			// Optimize for reads with more shards and less frequent cleanup
			c.ShardCount = nextPowerOfTwo(uint32(runtime.NumCPU() * constants.ToolCacheReadHeavyCPUShardMultiplier))
			c.CleanupInterval = constants.ToolCacheReadHeavyCleanupInterval
			c.CleanupStrategy = LazyCleanup

		case WriteHeavyWorkload:
			// Optimize for writes with efficient eviction
			c.ShardCount = nextPowerOfTwo(uint32(runtime.NumCPU() * constants.ToolCacheWriteHeavyCPUShardMultiplier))
			c.CleanupInterval = constants.ToolCacheWriteHeavyCleanupInterval

		case MixedWorkload:
			// Balanced configuration
			c.ShardCount = constants.ToolCacheMixedWorkloadShards
			c.CleanupInterval = constants.ToolCacheMixedWorkloadCleanupInterval
			c.CleanupStrategy = HybridCleanup

		case BurstyWorkload:
			// Handle traffic spikes
			c.ShardCount = nextPowerOfTwo(uint32(runtime.NumCPU() * constants.ToolCacheBurstyCPUShardMultiplier))
			c.CleanupStrategy = AdaptiveCleanup
		}
	}
}

// nextPowerOfTwo returns the next power of 2 greater than or equal to n
func nextPowerOfTwo(n uint32) uint32 {
	if n > constants.MaxCacheShards {
		panic(agentErrors.New(agentErrors.CodeInvalidConfig, "缓存分片数超过上限").WithComponent("tool_cache").WithOperation("initialize"))
	}
	if n <= 1 {
		return 1
	}
	// Check if already a power of 2
	if n&(n-1) == 0 {
		return n
	}
	// Find the next power of 2
	power := uint32(1)
	for power < n {
		power <<= 1
	}
	return power
}

// ShardCountRecommendation provides shard count recommendations based on expected load
type ShardCountRecommendation struct {
	ExpectedQPS      int    // Expected queries per second
	RecommendedCount uint32 // Recommended shard count
	Rationale        string // Explanation for the recommendation
}

// GetShardCountRecommendation returns recommended shard count based on expected QPS
func GetShardCountRecommendation(expectedQPS int) ShardCountRecommendation {
	var recommendation ShardCountRecommendation
	recommendation.ExpectedQPS = expectedQPS

	cpuCores := runtime.NumCPU()

	switch {
	case expectedQPS < 100:
		recommendation.RecommendedCount = nextPowerOfTwo(uint32(cpuCores * 2))
		recommendation.Rationale = "Light load: Using 2x CPU cores for basic concurrency"

	case expectedQPS < 500:
		recommendation.RecommendedCount = nextPowerOfTwo(uint32(cpuCores * 4))
		recommendation.Rationale = "Moderate load: Using 4x CPU cores for good concurrency"

	case expectedQPS < 1000:
		recommendation.RecommendedCount = nextPowerOfTwo(uint32(cpuCores * 8))
		recommendation.Rationale = "Medium load: Using 8x CPU cores to minimize contention"

	case expectedQPS < 5000:
		recommendation.RecommendedCount = 64
		if cpuCores > 16 {
			recommendation.RecommendedCount = 128
		}
		recommendation.Rationale = "High load: Using fixed high shard count for optimal distribution"

	default:
		recommendation.RecommendedCount = 256
		recommendation.Rationale = "Very high load: Maximum sharding for minimal contention"
	}

	return recommendation
}

// CleanupIntervalRecommendation provides cleanup interval recommendations
type CleanupIntervalRecommendation struct {
	CacheSize           int           // Total cache capacity
	TTL                 time.Duration // Default TTL
	ExpectedChurn       float64       // Expected percentage of entries changing per minute
	RecommendedInterval time.Duration
	Rationale           string
}

// GetCleanupIntervalRecommendation returns recommended cleanup interval
func GetCleanupIntervalRecommendation(cacheSize int, ttl time.Duration, churnRate float64) CleanupIntervalRecommendation {
	var recommendation CleanupIntervalRecommendation
	recommendation.CacheSize = cacheSize
	recommendation.TTL = ttl
	recommendation.ExpectedChurn = churnRate

	// Base interval on TTL
	baseInterval := ttl / 10
	if baseInterval < 10*time.Second {
		baseInterval = 10 * time.Second
	}

	// Adjust based on churn rate
	switch {
	case churnRate < 0.01: // Less than 1% churn per minute
		recommendation.RecommendedInterval = baseInterval * 3
		recommendation.Rationale = "Low churn: Less frequent cleanup needed"

	case churnRate < 0.05: // 1-5% churn per minute
		recommendation.RecommendedInterval = baseInterval
		recommendation.Rationale = "Moderate churn: Standard cleanup interval"

	case churnRate < 0.10: // 5-10% churn per minute
		recommendation.RecommendedInterval = baseInterval / 2
		recommendation.Rationale = "High churn: More frequent cleanup to reclaim memory"

	default: // More than 10% churn per minute
		recommendation.RecommendedInterval = 30 * time.Second
		recommendation.Rationale = "Very high churn: Aggressive cleanup for memory efficiency"
	}

	// Cap the interval
	if recommendation.RecommendedInterval > 10*time.Minute {
		recommendation.RecommendedInterval = 10 * time.Minute
	}

	return recommendation
}
