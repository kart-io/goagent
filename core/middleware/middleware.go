package middleware

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/kart-io/goagent/core/state"
)

// State is an alias to state.State for convenience
type State = state.State

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
	State State `json:"-"`

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
	State State `json:"-"`

	// Metadata holds additional response information
	Metadata map[string]interface{} `json:"metadata"`

	// Headers contains response headers
	Headers map[string]string `json:"headers"`

	// Duration is the execution time
	Duration time.Duration `json:"duration"`

	// Error holds any error that occurred
	Error error `json:"-"`
}

// Handler is a function that processes a request and returns a response
type Handler func(ctx context.Context, request *MiddlewareRequest) (*MiddlewareResponse, error)

// MiddlewareChain manages a sequence of middleware
type MiddlewareChain struct {
	middlewares []Middleware
	handler     Handler
	mu          sync.RWMutex
}

// NewMiddlewareChain creates a new middleware chain
func NewMiddlewareChain(handler Handler) *MiddlewareChain {
	return &MiddlewareChain{
		middlewares: []Middleware{},
		handler:     handler,
	}
}

// Use adds middleware to the chain
func (c *MiddlewareChain) Use(middleware ...Middleware) *MiddlewareChain {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.middlewares = append(c.middlewares, middleware...)
	return c
}

// Execute runs the request through the middleware chain
func (c *MiddlewareChain) Execute(ctx context.Context, request *MiddlewareRequest) (*MiddlewareResponse, error) {
	c.mu.RLock()
	middlewares := make([]Middleware, len(c.middlewares))
	copy(middlewares, c.middlewares)
	c.mu.RUnlock()

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

	// Execute the main handler
	start := time.Now()
	response, err := c.handler(ctx, request)
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
	} else {
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
	request.Metadata["timing_start"] = time.Now()
	return request, nil
}

// OnAfter records the duration
func (m *TimingMiddleware) OnAfter(ctx context.Context, response *MiddlewareResponse) (*MiddlewareResponse, error) {
	if response.Metadata == nil {
		response.Metadata = make(map[string]interface{})
	}

	// Get start time from request metadata (if available through response)
	if startTime, ok := response.Metadata["timing_start"].(time.Time); ok {
		duration := time.Since(startTime)
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
	return fmt.Errorf("retry needed for error: %w", err)
}

// CacheMiddleware provides response caching
type CacheMiddleware struct {
	*BaseMiddleware
	cache map[string]*CacheEntry
	ttl   time.Duration
	mu    sync.RWMutex
}

// CacheEntry represents a cached response
type CacheEntry struct {
	Response  *MiddlewareResponse
	ExpiresAt time.Time
}

// NewCacheMiddleware creates a cache middleware
func NewCacheMiddleware(ttl time.Duration) *CacheMiddleware {
	return &CacheMiddleware{
		BaseMiddleware: NewBaseMiddleware("cache"),
		cache:          make(map[string]*CacheEntry),
		ttl:            ttl,
	}
}

// OnBefore checks cache before execution
func (m *CacheMiddleware) OnBefore(ctx context.Context, request *MiddlewareRequest) (*MiddlewareRequest, error) {
	key := m.getCacheKey(request)

	m.mu.RLock()
	entry, ok := m.cache[key]
	m.mu.RUnlock()

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
		entry := &CacheEntry{
			Response:  response,
			ExpiresAt: time.Now().Add(m.ttl),
		}

		m.mu.Lock()
		m.cache[key] = entry
		m.mu.Unlock()
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
	m.mu.Lock()
	defer m.mu.Unlock()
	m.cache = make(map[string]*CacheEntry)
}

// Size returns the number of cached entries
func (m *CacheMiddleware) Size() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.cache)
}
