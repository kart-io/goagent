package constants

// Middleware defaults.
const (
	// DefaultMiddlewareCacheCapacity is the former entry cap of the tool caching
	// middleware. It is no longer applied; pass it via WithCacheMaxEntries.
	DefaultMiddlewareCacheCapacity = 10000
	// DefaultTimingWindow is how many recent records TimingMiddleware.GetTimings
	// keeps.
	DefaultTimingWindow = 1000
)
