package constants

// 共享默认策略与协议值；领域实现直接引用本包。
const (
	// DefaultMiddlewareCacheCapacity 是工具缓存中间件的条目上限。
	DefaultMiddlewareCacheCapacity = 10000
	// DefaultTimingWindow 是 TimingMiddleware.GetTimings 保留的最近记录数。
	DefaultTimingWindow = 1000
)
