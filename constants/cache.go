package constants

import "time"

// MaxCacheShards 限制缓存构造的分片数，避免整数进位溢出和配置造成过量分片分配。
const MaxCacheShards uint32 = 1 << 16

// 共享默认策略与协议值；领域实现直接引用本包。
const (
	DefaultMiddlewareCacheShards = 32
	// DefaultMiddlewareCacheCapacity 是 CacheMiddleware 的条目上限，按分片均分。
	DefaultMiddlewareCacheCapacity = 10000
	// DefaultTimingWindow 是 TimingMiddleware.GetTimings 保留的最近记录数。
	DefaultTimingWindow = 1000
)

// 工具缓存的缺省配置供构造器、零值兜底和默认配置函数共同使用。
const (
	DefaultToolCacheShards             = 32
	DefaultToolCacheCapacity           = 10000
	DefaultToolCacheTTL                = 5 * time.Minute
	DefaultToolCacheCleanupInterval    = time.Minute
	DefaultToolCacheCPUShardMultiplier = 4
)

// 性能配置和负载配置是独立策略，不因当前取值相同而合并。
const (
	ToolCacheLowLatencyCPUShardMultiplier      = 8
	ToolCacheHighThroughputCPUShardMultiplier  = 4
	ToolCacheBalancedProfileShards             = 32
	ToolCacheMemoryEfficientCPUShardMultiplier = 2
	ToolCacheMemoryEfficientCleanupInterval    = 30 * time.Second
	ToolCacheReadHeavyCPUShardMultiplier       = 8
	ToolCacheReadHeavyCleanupInterval          = 5 * time.Minute
	ToolCacheWriteHeavyCPUShardMultiplier      = 4
	ToolCacheWriteHeavyCleanupInterval         = 30 * time.Second
	ToolCacheMixedWorkloadShards               = 32
	ToolCacheMixedWorkloadCleanupInterval      = time.Minute
	ToolCacheBurstyCPUShardMultiplier          = 6
)

// 便捷构造器的预设独立于选项模式中的性能配置。
const (
	ToolCacheHighPerformanceShards          = 64
	ToolCacheHighPerformanceCapacity        = 100000
	ToolCacheHighPerformanceTTL             = 10 * time.Minute
	ToolCacheHighPerformanceCleanupInterval = 30 * time.Second
	ToolCacheLowMemoryShards                = 8
	ToolCacheLowMemoryCapacity              = 1000
	ToolCacheLowMemoryTTL                   = 2 * time.Minute
	ToolCacheLowMemoryCleanupInterval       = time.Minute
)
