package middleware

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/kart-io/goagent/constants"
	"github.com/kart-io/goagent/core/logging"
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
	metadata, headers := req.Metadata, req.Headers
	*req = MiddlewareRequest{Metadata: metadata, Headers: headers}
	// 清空容器并保留容量，其余所有字段已整体归零。
	if len(req.Metadata) > 0 {
		clear(req.Metadata)
	}
	if len(req.Headers) > 0 {
		clear(req.Headers)
	}
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
	metadata, headers := resp.Metadata, resp.Headers
	*resp = MiddlewareResponse{Metadata: metadata, Headers: headers}
	// 清空容器并保留容量，其余所有字段已整体归零。
	if len(resp.Metadata) > 0 {
		clear(resp.Metadata)
	}
	if len(resp.Headers) > 0 {
		clear(resp.Headers)
	}
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
	TokenUsage *interfaces.TokenUsage `json:"token_usage,omitzero"`

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
// It uses atomic.Pointer for zero-copy reads, avoiding slice copies on the hot
// path.
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
// Writes use copy-on-write so that readers are lock-free.
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

type requestContextKey struct{}

// RequestFromContext returns the request whose execution failed. OnError has no
// request parameter, so middleware keeping per-request state in Metadata reads it here.
func RequestFromContext(ctx context.Context) (*MiddlewareRequest, bool) {
	request, ok := ctx.Value(requestContextKey{}).(*MiddlewareRequest)
	return request, ok
}

// Execute runs the request through the middleware chain
// The hot path is zero-copy: it reads the immutable slice directly without
// locking or copying.
func (c *MiddlewareChain) Execute(ctx context.Context, request *MiddlewareRequest) (*MiddlewareResponse, error) {
	return executeMiddleware(ctx, request, c.handler, c.middlewares.Load().items, true)
}

// executeMiddleware 为各链共用错误恢复与请求关联。
func executeMiddleware(ctx context.Context, request *MiddlewareRequest, handler Handler, middlewares []Middleware, after bool) (*MiddlewareResponse, error) {
	for i, mw := range middlewares {
		entered := request
		next, err := mw.OnBefore(ctx, request)
		if err != nil {
			errCtx := context.WithValue(ctx, requestContextKey{}, entered)
			if err = mw.OnError(errCtx, err); err != nil {
				for _, previous := range middlewares[:i] {
					_ = previous.OnError(errCtx, err)
				}
				return nil, err
			}
			if next == nil {
				next = entered
			}
		}
		if next == nil {
			return nil, agentErrors.Newf(agentErrors.CodeMiddlewareExecution, "中间件 %s 返回空请求", mw.Name()).WithComponent("middleware_chain").WithOperation("execute")
		}
		request = next
	}
	if request == nil {
		return nil, agentErrors.New(agentErrors.CodeInvalidInput, "中间件请求不能为空").WithComponent("middleware_chain").WithOperation("execute")
	}
	ctx = context.WithValue(ctx, requestContextKey{}, request)
	start := time.Now()
	response, err := handler(ctx, request)
	if err != nil {
		for _, mw := range middlewares {
			err = mw.OnError(ctx, err)
			if err == nil {
				break
			}
		}
		if err != nil {
			return nil, err
		}
	}
	if response == nil {
		response = &MiddlewareResponse{State: request.State, Metadata: make(map[string]interface{}), Headers: make(map[string]string)}
	}
	if response.Duration == 0 {
		response.Duration = time.Since(start)
	}
	if after {
		for i := len(middlewares) - 1; i >= 0; i-- {
			entered := response
			next, err := middlewares[i].OnAfter(ctx, response)
			if err != nil {
				if err = middlewares[i].OnError(ctx, err); err != nil {
					return nil, err
				}
				if next == nil {
					next = entered
				}
			}
			if next == nil {
				return nil, agentErrors.Newf(agentErrors.CodeMiddlewareExecution, "中间件 %s 返回空响应", middlewares[i].Name()).WithComponent("middleware_chain").WithOperation("execute")
			}
			response = next
		}
	}
	return response, nil
}

// WithHandler snapshots this chain's middleware with a per-execution handler.
// Middleware instances are shared, just as they are across Execute calls.
func (c *MiddlewareChain) WithHandler(handler Handler) *MiddlewareChain {
	clone := NewMiddlewareChain(handler)
	clone.middlewares.Store(c.middlewares.Load())
	return clone
}

// Size returns the current number of middleware.
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

// NewLoggingMiddleware creates a logging middleware. A nil logger writes each
// line at info level through logging.Default(), that is slog.Default().
func NewLoggingMiddleware(logger func(string)) *LoggingMiddleware {
	if logger == nil {
		logger = func(msg string) { logging.Default().InfoContext(context.Background(), msg) }
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
//
// It keeps only the most recent constants.DefaultTimingWindow records, so
// memory stays bounded in long-running use; the average is accumulated over
// all records.
type TimingMiddleware struct {
	*BaseMiddleware
	mu      sync.RWMutex
	recent  []timingRecord // 环形缓冲
	next    int
	counter int64
	total   time.Duration
}

type timingRecord struct {
	key      string
	duration time.Duration
}

// NewTimingMiddleware creates a timing middleware
func NewTimingMiddleware() *TimingMiddleware {
	return &TimingMiddleware{BaseMiddleware: NewBaseMiddleware("timing")}
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

		m.mu.Lock()
		m.counter++
		m.total += duration
		record := timingRecord{key: fmt.Sprintf("%s_%d", time.Now().Format(time.RFC3339Nano), m.counter), duration: duration}
		if len(m.recent) < constants.DefaultTimingWindow {
			m.recent = append(m.recent, record)
		} else {
			m.recent[m.next] = record
			m.next = (m.next + 1) % constants.DefaultTimingWindow
		}
		m.mu.Unlock()
	}

	return response, nil
}

// GetTimings returns the most recent timings (at most constants.DefaultTimingWindow)
func (m *TimingMiddleware) GetTimings() map[string]time.Duration {
	m.mu.RLock()
	defer m.mu.RUnlock()

	timings := make(map[string]time.Duration, len(m.recent))
	for _, r := range m.recent {
		timings[r.key] = r.duration
	}
	return timings
}

// GetAverageLatency returns the average latency over all recorded timings
func (m *TimingMiddleware) GetAverageLatency() time.Duration {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if m.counter == 0 {
		return 0
	}
	return m.total / time.Duration(m.counter)
}
