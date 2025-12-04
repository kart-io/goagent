package middleware

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/kart-io/goagent/core/state"
	agentErrors "github.com/kart-io/goagent/errors"
	"github.com/kart-io/goagent/interfaces"
)

// Object pools for MiddlewareRequest and MiddlewareResponse to reduce allocations
var middlewareRequestPool = sync.Pool{
	New: func() interface{} {
		return &MiddlewareRequest{
			Metadata: make(map[string]interface{}, 4),
			Headers:  make(map[string]string, 4),
		}
	},
}

var middlewareResponsePool = sync.Pool{
	New: func() interface{} {
		return &MiddlewareResponse{
			Metadata: make(map[string]interface{}, 4),
			Headers:  make(map[string]string, 4),
		}
	},
}

// GetMiddlewareRequest retrieves a MiddlewareRequest from the object pool
func GetMiddlewareRequest() *MiddlewareRequest {
	req := middlewareRequestPool.Get().(*MiddlewareRequest)
	// Reset to default state (使用 Go 1.21+ clear() 提高性能)
	req.Input = nil
	req.State = nil
	req.Runtime = nil
	if len(req.Metadata) > 0 {
		clear(req.Metadata)
	}
	if len(req.Headers) > 0 {
		clear(req.Headers)
	}
	req.Timestamp = time.Time{}
	return req
}

// PutMiddlewareRequest returns a MiddlewareRequest to the object pool
func PutMiddlewareRequest(req *MiddlewareRequest) {
	if req != nil {
		middlewareRequestPool.Put(req)
	}
}

// GetMiddlewareResponse retrieves a MiddlewareResponse from the object pool
func GetMiddlewareResponse() *MiddlewareResponse {
	resp := middlewareResponsePool.Get().(*MiddlewareResponse)
	// Reset to default state (使用 Go 1.21+ clear() 提高性能)
	resp.Output = nil
	resp.State = nil
	if len(resp.Metadata) > 0 {
		clear(resp.Metadata)
	}
	if len(resp.Headers) > 0 {
		clear(resp.Headers)
	}
	resp.Duration = 0
	resp.Error = nil
	return resp
}

// PutMiddlewareResponse returns a MiddlewareResponse to the object pool
func PutMiddlewareResponse(resp *MiddlewareResponse) {
	if resp != nil {
		middlewareResponsePool.Put(resp)
	}
}

// Middleware defines the interface for request/response interceptors.
//
// Inspired by LangChain's middleware system, it provides:
//   - Before/after execution hooks
//   - Request/response modification
//   - Chain of responsibility pattern
//   - Error handling and recovery
//
// Use cases:
//   - Dynamic prompt modification
//   - Tool selection and filtering
//   - Rate limiting and throttling
//   - Logging and monitoring
//   - Authentication and authorization
type Middleware interface {
	// Name returns the middleware name for identification
	Name() string

	// OnBefore is called before the main execution
	// It can modify the request or short-circuit the chain by returning an error
	OnBefore(ctx context.Context, request *MiddlewareRequest) (*MiddlewareRequest, error)

	// OnAfter is called after the main execution
	// It can modify the response or handle errors
	OnAfter(ctx context.Context, response *MiddlewareResponse) (*MiddlewareResponse, error)

	// OnError is called when an error occurs during execution
	// It can handle, wrap, or suppress the error
	OnError(ctx context.Context, err error) error
}

// MiddlewareRequest represents a request passing through middleware
type MiddlewareRequest struct {
	// Input is the original input to the agent/tool
	Input interface{} `json:"input"`

	// State is the current agent state
	State state.State `json:"-"`

	// Runtime provides access to the execution environment
	Runtime interface{} `json:"-"`

	// Metadata holds additional request information
	Metadata map[string]interface{} `json:"metadata"`

	// Headers contains request headers (for HTTP-like semantics)
	Headers map[string]string `json:"headers"`

	// Timestamp is when the request was created
	Timestamp time.Time `json:"timestamp"`
}

// MiddlewareResponse represents a response passing through middleware
type MiddlewareResponse struct {
	// Output is the result from the agent/tool
	Output interface{} `json:"output"`

	// State is the updated agent state
	State state.State `json:"-"`

	// Metadata holds additional response information
	Metadata map[string]interface{} `json:"metadata"`

	// Headers contains response headers
	Headers map[string]string `json:"headers"`

	// Duration is the execution time
	Duration time.Duration `json:"duration"`

	// TokenUsage holds LLM token usage statistics
	TokenUsage *interfaces.TokenUsage `json:"token_usage,omitempty"`

	// Error holds any error that occurred
	Error error `json:"-"`
}

// Handler is a function that processes a request and returns a response
type Handler func(ctx context.Context, request *MiddlewareRequest) (*MiddlewareResponse, error)

// middlewareSlice 不可变的 middleware 切片包装，用于原子操作
type middlewareSlice struct {
	items []Middleware
}

// MiddlewareChain manages a sequence of middleware
// 使用 atomic.Pointer 实现零拷贝读取，避免热路径上的 slice 复制
type MiddlewareChain struct {
	middlewares atomic.Pointer[middlewareSlice]
	handler     Handler
	mu          sync.Mutex // 仅用于写操作的互斥
}

// NewMiddlewareChain creates a new middleware chain
func NewMiddlewareChain(handler Handler) *MiddlewareChain {
	c := &MiddlewareChain{
		handler: handler,
	}
	// 初始化空 slice
	c.middlewares.Store(&middlewareSlice{items: []Middleware{}})
	return c
}

// Use adds middleware to the chain
// 写操作使用 copy-on-write 模式，确保读取端无锁
func (c *MiddlewareChain) Use(middleware ...Middleware) *MiddlewareChain {
	c.mu.Lock()
	defer c.mu.Unlock()

	// 获取当前 slice 并创建新副本
	current := c.middlewares.Load()
	newItems := make([]Middleware, len(current.items), len(current.items)+len(middleware))
	copy(newItems, current.items)
	newItems = append(newItems, middleware...)

	// 原子替换为新 slice
	c.middlewares.Store(&middlewareSlice{items: newItems})
	return c
}

// Execute runs the request through the middleware chain
// 热路径零拷贝：直接读取不可变 slice，无需加锁或复制
func (c *MiddlewareChain) Execute(ctx context.Context, request *MiddlewareRequest) (*MiddlewareResponse, error) {
	// 原子加载不可变 slice，无需复制
	middlewares := c.middlewares.Load().items

	// Run OnBefore hooks in order
	for _, mw := range middlewares {
		var err error
		request, err = mw.OnBefore(ctx, request)
		if err != nil {
			// Allow middleware to handle the error
			if handledErr := mw.OnError(ctx, err); handledErr != nil {
				return nil, handledErr
			}
			// Error was suppressed, continue
		}
	}

	// 检查缓存命中（短路执行）
	var response *MiddlewareResponse
	var err error
	start := time.Now()

	if request != nil && request.Metadata != nil {
		if cached, ok := request.Metadata["cached_response"].(*MiddlewareResponse); ok && cached != nil {
			// 缓存命中，跳过 handler 执行
			response = cached
			response.Duration = 0 // 缓存响应无执行时间
		}
	}

	// 如果没有缓存命中，执行主处理器
	if response == nil {
		response, err = c.handler(ctx, request)
		if err != nil {
			// Let middleware handle the error
			for _, mw := range middlewares {
				if handledErr := mw.OnError(ctx, err); handledErr != nil {
					err = handledErr
				}
			}
			if err != nil {
				return nil, err
			}
		}
	}

	// Ensure response is not nil
	if response == nil {
		response = &MiddlewareResponse{
			Output:   nil,
			State:    request.State,
			Metadata: make(map[string]interface{}),
			Headers:  make(map[string]string),
			Duration: time.Since(start),
			Error:    err,
		}
	} else if response.Duration == 0 {
		// 仅当未设置时才计算 Duration（缓存命中时已设为 0）
		response.Duration = time.Since(start)
	}

	// Run OnAfter hooks in reverse order
	for i := len(middlewares) - 1; i >= 0; i-- {
		mw := middlewares[i]
		var afterErr error
		response, afterErr = mw.OnAfter(ctx, response)
		if afterErr != nil {
			if handledErr := mw.OnError(ctx, afterErr); handledErr != nil {
				return nil, handledErr
			}
		}
	}

	return response, nil
}

// Size 返回当前 middleware 数量
func (c *MiddlewareChain) Size() int {
	return len(c.middlewares.Load().items)
}

// BaseMiddleware provides a default implementation of Middleware
type BaseMiddleware struct {
	name string
}

// NewBaseMiddleware creates a new BaseMiddleware
func NewBaseMiddleware(name string) *BaseMiddleware {
	return &BaseMiddleware{name: name}
}

// Name returns the middleware name
func (m *BaseMiddleware) Name() string {
	return m.name
}

// OnBefore is a no-op by default
func (m *BaseMiddleware) OnBefore(ctx context.Context, request *MiddlewareRequest) (*MiddlewareRequest, error) {
	return request, nil
}

// OnAfter is a no-op by default
func (m *BaseMiddleware) OnAfter(ctx context.Context, response *MiddlewareResponse) (*MiddlewareResponse, error) {
	return response, nil
}

// OnError is a no-op by default
func (m *BaseMiddleware) OnError(ctx context.Context, err error) error {
	return err
}

// MiddlewareFunc allows using a function as middleware
type MiddlewareFunc struct {
	*BaseMiddleware
	BeforeFunc func(ctx context.Context, request *MiddlewareRequest) (*MiddlewareRequest, error)
	AfterFunc  func(ctx context.Context, response *MiddlewareResponse) (*MiddlewareResponse, error)
	ErrorFunc  func(ctx context.Context, err error) error
}

// NewMiddlewareFunc creates middleware from functions
func NewMiddlewareFunc(
	name string,
	before func(ctx context.Context, request *MiddlewareRequest) (*MiddlewareRequest, error),
	after func(ctx context.Context, response *MiddlewareResponse) (*MiddlewareResponse, error),
	onError func(ctx context.Context, err error) error,
) *MiddlewareFunc {
	return &MiddlewareFunc{
		BaseMiddleware: NewBaseMiddleware(name),
		BeforeFunc:     before,
		AfterFunc:      after,
		ErrorFunc:      onError,
	}
}

// OnBefore calls the before function if provided
func (m *MiddlewareFunc) OnBefore(ctx context.Context, request *MiddlewareRequest) (*MiddlewareRequest, error) {
	if m.BeforeFunc != nil {
		return m.BeforeFunc(ctx, request)
	}
	return request, nil
}

// OnAfter calls the after function if provided
func (m *MiddlewareFunc) OnAfter(ctx context.Context, response *MiddlewareResponse) (*MiddlewareResponse, error) {
	if m.AfterFunc != nil {
		return m.AfterFunc(ctx, response)
	}
	return response, nil
}

// OnError calls the error function if provided
func (m *MiddlewareFunc) OnError(ctx context.Context, err error) error {
	if m.ErrorFunc != nil {
		return m.ErrorFunc(ctx, err)
	}
	return err
}

// LoggingMiddleware logs requests and responses
type LoggingMiddleware struct {
	*BaseMiddleware
	logger func(string)
}

// NewLoggingMiddleware creates a logging middleware
func NewLoggingMiddleware(logger func(string)) *LoggingMiddleware {
	if logger == nil {
		logger = func(msg string) { fmt.Println(msg) }
	}
	return &LoggingMiddleware{
		BaseMiddleware: NewBaseMiddleware("logging"),
		logger:         logger,
	}
}

// OnBefore logs the incoming request
func (m *LoggingMiddleware) OnBefore(ctx context.Context, request *MiddlewareRequest) (*MiddlewareRequest, error) {
	m.logger(fmt.Sprintf("[%s] Request: %v (metadata: %v)",
		m.Name(), request.Input, request.Metadata))
	return request, nil
}

// OnAfter logs the outgoing response
func (m *LoggingMiddleware) OnAfter(ctx context.Context, response *MiddlewareResponse) (*MiddlewareResponse, error) {
	m.logger(fmt.Sprintf("[%s] Response: %v (duration: %v)",
		m.Name(), response.Output, response.Duration))
	return response, nil
}

// OnError logs errors
func (m *LoggingMiddleware) OnError(ctx context.Context, err error) error {
	m.logger(fmt.Sprintf("[%s] Error: %v", m.Name(), err))
	return err
}

// TimingMiddleware tracks execution time
type TimingMiddleware struct {
	*BaseMiddleware
	timings map[string]time.Duration
	mu      sync.RWMutex
	counter int64
}

// NewTimingMiddleware creates a timing middleware
func NewTimingMiddleware() *TimingMiddleware {
	return &TimingMiddleware{
		BaseMiddleware: NewBaseMiddleware("timing"),
		timings:        make(map[string]time.Duration),
	}
}

// OnBefore records the start time
func (m *TimingMiddleware) OnBefore(ctx context.Context, request *MiddlewareRequest) (*MiddlewareRequest, error) {
	if request.Metadata == nil {
		request.Metadata = make(map[string]interface{})
	}
	// 同时存储在 metadata 和 request 中，以便 OnAfter 可以访问
	startTime := time.Now()
	request.Metadata["timing_start"] = startTime
	return request, nil
}

// OnAfter records the duration
func (m *TimingMiddleware) OnAfter(ctx context.Context, response *MiddlewareResponse) (*MiddlewareResponse, error) {
	if response.Metadata == nil {
		response.Metadata = make(map[string]interface{})
	}

	var duration time.Duration

	// 优先使用 response.Duration（由 MiddlewareChain.Execute 设置）
	if response.Duration > 0 {
		duration = response.Duration
	} else if startTime, ok := response.Metadata["timing_start"].(time.Time); ok {
		// 回退：从元数据中获取开始时间（直接调用 OnAfter 的场景）
		duration = time.Since(startTime)
	}

	if duration > 0 {
		response.Metadata["timing_duration"] = duration

		// Store timing with unique key
		m.mu.Lock()
		m.counter++
		key := fmt.Sprintf("%s_%d", time.Now().Format(time.RFC3339Nano), m.counter)
		m.timings[key] = duration
		m.mu.Unlock()
	}

	return response, nil
}

// GetTimings returns all recorded timings
func (m *TimingMiddleware) GetTimings() map[string]time.Duration {
	m.mu.RLock()
	defer m.mu.RUnlock()

	timings := make(map[string]time.Duration, len(m.timings))
	for k, v := range m.timings {
		timings[k] = v
	}
	return timings
}

// GetAverageLatency returns the average latency
func (m *TimingMiddleware) GetAverageLatency() time.Duration {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if len(m.timings) == 0 {
		return 0
	}

	var total time.Duration
	for _, duration := range m.timings {
		total += duration
	}

	return total / time.Duration(len(m.timings))
}

// RetryMiddleware implements retry logic
type RetryMiddleware struct {
	*BaseMiddleware
	maxRetries int
	backoff    time.Duration
	retryOn    func(error) bool
}

// NewRetryMiddleware creates a retry middleware
func NewRetryMiddleware(maxRetries int, backoff time.Duration) *RetryMiddleware {
	return &RetryMiddleware{
		BaseMiddleware: NewBaseMiddleware("retry"),
		maxRetries:     maxRetries,
		backoff:        backoff,
		retryOn: func(err error) bool {
			// Default: retry on all errors
			return err != nil
		},
	}
}

// WithRetryCondition sets a custom retry condition
func (m *RetryMiddleware) WithRetryCondition(condition func(error) bool) *RetryMiddleware {
	m.retryOn = condition
	return m
}

// OnError implements retry logic
func (m *RetryMiddleware) OnError(ctx context.Context, err error) error {
	if !m.retryOn(err) {
		return err
	}

	// Note: Actual retry logic would need to be implemented in the chain execution
	// This is a simplified version showing the pattern
	return agentErrors.Wrap(err, agentErrors.CodeMiddlewareExecution, "retry needed for error").
		WithComponent("retry_middleware").
		WithOperation("on_error")
}

// CacheMiddleware provides response caching with sharded locks for better concurrency
type CacheMiddleware struct {
	*BaseMiddleware
	shards    []*cacheShard
	numShards uint32
	ttl       time.Duration
}

// cacheShard 单个缓存分片，减少锁竞争
type cacheShard struct {
	cache map[string]*CacheEntry
	mu    sync.RWMutex
}

// CacheEntry represents a cached response
type CacheEntry struct {
	Response  *MiddlewareResponse
	ExpiresAt time.Time
}

// defaultNumShards 默认分片数量（应为 2 的幂）
const defaultNumShards = 32

// NewCacheMiddleware creates a cache middleware with sharded locks
func NewCacheMiddleware(ttl time.Duration) *CacheMiddleware {
	return NewCacheMiddlewareWithShards(ttl, defaultNumShards)
}

// NewCacheMiddlewareWithShards creates a cache middleware with specified number of shards
func NewCacheMiddlewareWithShards(ttl time.Duration, numShards uint32) *CacheMiddleware {
	if numShards == 0 {
		numShards = defaultNumShards
	}
	// 确保是 2 的幂，方便取模
	numShards = nextPowerOfTwo(numShards)

	shards := make([]*cacheShard, numShards)
	for i := range shards {
		shards[i] = &cacheShard{
			cache: make(map[string]*CacheEntry),
		}
	}

	return &CacheMiddleware{
		BaseMiddleware: NewBaseMiddleware("cache"),
		shards:         shards,
		numShards:      numShards,
		ttl:            ttl,
	}
}

// nextPowerOfTwo 返回大于等于 n 的最小 2 的幂
func nextPowerOfTwo(n uint32) uint32 {
	if n == 0 {
		return 1
	}
	n--
	n |= n >> 1
	n |= n >> 2
	n |= n >> 4
	n |= n >> 8
	n |= n >> 16
	return n + 1
}

// getShard 根据 key 获取对应的分片
func (m *CacheMiddleware) getShard(key string) *cacheShard {
	hash := fnv32(key)
	return m.shards[hash&(m.numShards-1)]
}

// fnv32 简单的 FNV-1a 哈希函数
func fnv32(key string) uint32 {
	hash := uint32(2166136261)
	for i := 0; i < len(key); i++ {
		hash ^= uint32(key[i])
		hash *= 16777619
	}
	return hash
}

// OnBefore checks cache before execution
func (m *CacheMiddleware) OnBefore(ctx context.Context, request *MiddlewareRequest) (*MiddlewareRequest, error) {
	key := m.getCacheKey(request)
	shard := m.getShard(key)

	shard.mu.RLock()
	entry, ok := shard.cache[key]
	shard.mu.RUnlock()

	if ok && time.Now().Before(entry.ExpiresAt) {
		// Cache hit - add flag to metadata
		if request.Metadata == nil {
			request.Metadata = make(map[string]interface{})
		}
		request.Metadata["cache_hit"] = true
		request.Metadata["cached_response"] = entry.Response
	}

	return request, nil
}

// OnAfter caches successful responses
func (m *CacheMiddleware) OnAfter(ctx context.Context, response *MiddlewareResponse) (*MiddlewareResponse, error) {
	// Check if this was a cache hit
	if response.Metadata != nil {
		if cached, ok := response.Metadata["cached_response"].(*MiddlewareResponse); ok {
			return cached, nil
		}
	}

	// Cache the response
	if response.Error == nil {
		key := m.getCacheKeyFromResponse(response)
		if key != "" {
			entry := &CacheEntry{
				Response:  response,
				ExpiresAt: time.Now().Add(m.ttl),
			}

			shard := m.getShard(key)
			shard.mu.Lock()
			shard.cache[key] = entry
			shard.mu.Unlock()
		}
	}

	return response, nil
}

// getCacheKey generates a cache key from request
func (m *CacheMiddleware) getCacheKey(request *MiddlewareRequest) string {
	return fmt.Sprintf("%v", request.Input)
}

// getCacheKeyFromResponse generates a cache key from response metadata
func (m *CacheMiddleware) getCacheKeyFromResponse(response *MiddlewareResponse) string {
	if response.Metadata != nil {
		if input, ok := response.Metadata["original_input"]; ok {
			return fmt.Sprintf("%v", input)
		}
	}
	return ""
}

// Clear removes all cached entries
func (m *CacheMiddleware) Clear() {
	for _, shard := range m.shards {
		shard.mu.Lock()
		shard.cache = make(map[string]*CacheEntry)
		shard.mu.Unlock()
	}
}

// Size returns the number of cached entries
func (m *CacheMiddleware) Size() int {
	total := 0
	for _, shard := range m.shards {
		shard.mu.RLock()
		total += len(shard.cache)
		shard.mu.RUnlock()
	}
	return total
}
