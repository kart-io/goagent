package tools

import (
	"runtime"
	"time"
)

// ShardedCacheOption is a functional option for configuring ShardedToolCache
type ShardedCacheOption func(*ShardedCacheConfig)

// DefaultShardedCacheConfig returns the default configuration
func DefaultShardedCacheConfig() ShardedCacheConfig {
	return ShardedCacheConfig{
		ShardCount:      32,
		Capacity:        10000,
		DefaultTTL:      5 * time.Minute,
		CleanupInterval: 1 * time.Minute,
		EvictionPolicy:  LRUEviction,
		CleanupStrategy: PeriodicCleanup,
		LoadBalancing:   HashBasedBalancing,
		AutoTuning:      false,
		MetricsEnabled:  true,
	}
}

// EvictionPolicy defines cache eviction strategies
type EvictionPolicy int

const (
	// LRUEviction evicts least recently used items
	LRUEviction EvictionPolicy = iota
	// LFUEviction evicts least frequently used items
	LFUEviction
	// FIFOEviction evicts oldest items first
	FIFOEviction
	// RandomEviction evicts random items
	RandomEviction
)

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

// LoadBalancingStrategy defines how keys are distributed across shards
type LoadBalancingStrategy int

const (
	// HashBasedBalancing uses hash function for distribution
	HashBasedBalancing LoadBalancingStrategy = iota
	// ConsistentHashBalancing uses consistent hashing
	ConsistentHashBalancing
	// RoundRobinBalancing distributes evenly in round-robin fashion
	RoundRobinBalancing
	// LeastLoadedBalancing routes to least loaded shard
	LeastLoadedBalancing
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
			c.ShardCount = uint32(runtime.NumCPU() * 4)
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

// WithEvictionPolicy sets the eviction policy for the cache
func WithEvictionPolicy(policy EvictionPolicy) ShardedCacheOption {
	return func(c *ShardedCacheConfig) {
		c.EvictionPolicy = policy
	}
}

// WithCleanupStrategy sets the cleanup strategy
func WithCleanupStrategy(strategy CleanupStrategy) ShardedCacheOption {
	return func(c *ShardedCacheConfig) {
		c.CleanupStrategy = strategy
	}
}

// WithLoadBalancing sets the load balancing strategy for shard selection
func WithLoadBalancing(strategy LoadBalancingStrategy) ShardedCacheOption {
	return func(c *ShardedCacheConfig) {
		c.LoadBalancing = strategy
	}
}

// WithAutoTuning enables automatic performance tuning
// The cache will automatically adjust parameters based on workload patterns
func WithAutoTuning(enabled bool) ShardedCacheOption {
	return func(c *ShardedCacheConfig) {
		c.AutoTuning = enabled
		if enabled {
			// Enable metrics for auto-tuning
			c.MetricsEnabled = true
		}
	}
}

// WithMetrics enables or disables metrics collection
func WithMetrics(enabled bool) ShardedCacheOption {
	return func(c *ShardedCacheConfig) {
		c.MetricsEnabled = enabled
	}
}

// WithMaxShardConcurrency sets the maximum concurrent operations per shard
func WithMaxShardConcurrency(max int) ShardedCacheOption {
	return func(c *ShardedCacheConfig) {
		c.MaxConcurrency = max
	}
}

// WithWarmup enables cache warmup with specified entries
func WithWarmup(entries map[string]*ToolOutput) ShardedCacheOption {
	return func(c *ShardedCacheConfig) {
		c.WarmupEntries = entries
	}
}

// WithCompressionThreshold enables compression for entries larger than threshold
func WithCompressionThreshold(bytes int) ShardedCacheOption {
	return func(c *ShardedCacheConfig) {
		c.CompressionThreshold = bytes
	}
}

// WithMaxEntrySize sets the maximum size for a single cache entry
func WithMaxEntrySize(bytes int) ShardedCacheOption {
	return func(c *ShardedCacheConfig) {
		c.MaxEntrySize = bytes
	}
}

// WithMemoryLimit sets the maximum memory usage for the cache
func WithMemoryLimit(bytes int64) ShardedCacheOption {
	return func(c *ShardedCacheConfig) {
		c.MemoryLimit = bytes
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
			c.ShardCount = nextPowerOfTwo(uint32(runtime.NumCPU() * 8))
			c.CleanupStrategy = LazyCleanup
			c.LoadBalancing = LeastLoadedBalancing
			c.MaxConcurrency = 0 // No limit

		case HighThroughputProfile:
			// Balanced shards with efficient cleanup
			c.ShardCount = nextPowerOfTwo(uint32(runtime.NumCPU() * 4))
			c.CleanupStrategy = AdaptiveCleanup
			c.LoadBalancing = HashBasedBalancing
			c.EvictionPolicy = LRUEviction

		case BalancedProfile:
			// Default balanced configuration
			c.ShardCount = 32
			c.CleanupStrategy = HybridCleanup
			c.LoadBalancing = HashBasedBalancing
			c.EvictionPolicy = LRUEviction

		case MemoryEfficientProfile:
			// Fewer shards, aggressive cleanup
			c.ShardCount = nextPowerOfTwo(uint32(runtime.NumCPU() * 2))
			c.CleanupStrategy = PeriodicCleanup
			c.CleanupInterval = 30 * time.Second
			c.EvictionPolicy = LFUEviction
			c.CompressionThreshold = 1024 // Compress large entries
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
			c.ShardCount = nextPowerOfTwo(uint32(runtime.NumCPU() * 8))
			c.CleanupInterval = 5 * time.Minute
			c.CleanupStrategy = LazyCleanup

		case WriteHeavyWorkload:
			// Optimize for writes with efficient eviction
			c.ShardCount = nextPowerOfTwo(uint32(runtime.NumCPU() * 4))
			c.CleanupInterval = 30 * time.Second
			c.EvictionPolicy = FIFOEviction // Faster eviction

		case MixedWorkload:
			// Balanced configuration
			c.ShardCount = 32
			c.CleanupInterval = 1 * time.Minute
			c.CleanupStrategy = HybridCleanup

		case BurstyWorkload:
			// Handle traffic spikes
			c.ShardCount = nextPowerOfTwo(uint32(runtime.NumCPU() * 6))
			c.CleanupStrategy = AdaptiveCleanup
			c.AutoTuning = true
			c.MaxConcurrency = 100 // Limit concurrent operations
		}
		c.WorkloadType = workload
	}
}

// nextPowerOfTwo returns the next power of 2 greater than or equal to n
func nextPowerOfTwo(n uint32) uint32 {
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
