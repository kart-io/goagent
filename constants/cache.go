package constants

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
