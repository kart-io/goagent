package constants

// 共享默认策略与协议值；领域实现直接引用本包。
const (
	// DefaultMiddlewareCacheCapacity 是工具缓存中间件的旧条目上限，不再自动套用；宿主经 WithCacheMaxEntries 显式传入。
	DefaultMiddlewareCacheCapacity = 10000
	// DefaultTimingWindow 是 TimingMiddleware.GetTimings 保留的最近记录数。
	DefaultTimingWindow = 1000
)
