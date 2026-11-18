// This file defines interfaces and registry for panic recovery customization:
//   - PanicHandler - Custom panic recovery strategies
//   - PanicMetricsCollector - Statistics and monitoring
//   - PanicLogger - Specialized logging
//   - PanicHandlerRegistry - Thread-safe hot-swapping

package core

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"

	agentErrors "github.com/kart-io/goagent/errors"
)

// =============================================================================
// Panic Handler Interfaces
// =============================================================================

// PanicHandler defines the interface for handling recovered panics.
//
// Implementations can provide custom recovery strategies, such as:
//   - Converting panics to domain-specific errors
//   - Adding custom context information
//   - Implementing retry logic
//   - Graceful degradation
type PanicHandler interface {
	// HandlePanic processes a recovered panic and returns an appropriate error.
	//
	// Parameters:
	//   - ctx: The execution context
	//   - component: The component where panic occurred (e.g., "runnable", "lifecycle_manager")
	//   - operation: The operation being performed (e.g., "invoke", "init", "start")
	//   - panicValue: The value passed to panic()
	//   - stackTrace: The stack trace at the point of panic
	//
	// Returns:
	//   - An error representing the panic, suitable for returning to callers
	//
	// The returned error's text should not include panicValue or stackTrace: the
	// registry does not rewrite the handler's text, so both should be left to the
	// PanicLogger, metrics, or the *agentErrors.PanicError obtained via errors.As.
	HandlePanic(ctx context.Context, component, operation string, panicValue interface{}, stackTrace string) error
}

// PanicMetricsCollector defines the interface for collecting panic statistics.
//
// Implementations can integrate with monitoring systems:
//   - Prometheus counters and histograms
//   - StatsD metrics
//   - Custom telemetry systems
//   - Application performance monitoring (APM)
type PanicMetricsCollector interface {
	// RecordPanic records that a panic occurred.
	//
	// Parameters:
	//   - ctx: The execution context
	//   - component: The component where panic occurred
	//   - operation: The operation being performed
	//   - panicValue: The value passed to panic()
	RecordPanic(ctx context.Context, component, operation string, panicValue interface{})
}

// PanicLogger defines the interface for logging panic events.
//
// Implementations can provide specialized logging:
//   - Structured logging (JSON, logfmt)
//   - Different log levels based on panic type
//   - Integration with centralized logging systems
//   - Alert triggering for critical panics
type PanicLogger interface {
	// LogPanic logs a panic event with all relevant context.
	//
	// Parameters:
	//   - ctx: The execution context
	//   - component: The component where panic occurred
	//   - operation: The operation being performed
	//   - panicValue: The value passed to panic()
	//   - stackTrace: The stack trace at the point of panic
	//   - recoveredError: The error returned from HandlePanic
	LogPanic(ctx context.Context, component, operation string, panicValue interface{}, stackTrace string, recoveredError error)
}

// =============================================================================
// Default Implementations
// =============================================================================

// DefaultPanicHandler is the default panic handler implementation.
//
// Behavior:
//   - Converts all panics to AgentError with CodeInternal
//   - Cause is a *agentErrors.PanicError; use errors.As to detect it and read
//     the panic value and stack trace
//   - Compatible with existing error handling infrastructure
//
// Error text flows into stream errors, logs, and even the model, so Message and
// Context omit the panic value and stack trace; the text ends with the
// PanicError message, which names where the panic occurred.
type DefaultPanicHandler struct{}

// HandlePanic implements PanicHandler interface.
func (h *DefaultPanicHandler) HandlePanic(ctx context.Context, component, operation string, panicValue interface{}, stackTrace string) error {
	return agentErrors.Wrap(
		newPanicCause(component, operation, panicValue, []byte(stackTrace)),
		agentErrors.CodeInternal,
		"panic 已恢复",
	).
		WithComponent(component).
		WithOperation(operation)
}

// NoOpMetricsCollector is a no-op metrics collector (default).
//
// This is the default implementation that does nothing.
// Replace with a real implementation (e.g., PrometheusMetricsCollector) in production.
type NoOpMetricsCollector struct{}

// RecordPanic implements PanicMetricsCollector interface (no-op).
func (c *NoOpMetricsCollector) RecordPanic(ctx context.Context, component, operation string, panicValue interface{}) {
	// No-op: Replace with real metrics collection in production
}

// NoOpPanicLogger is a no-op logger (default).
//
// This is the default implementation that does nothing.
// Replace with a real implementation (e.g., StructuredPanicLogger) in production.
type NoOpPanicLogger struct{}

// LogPanic implements PanicLogger interface (no-op).
func (l *NoOpPanicLogger) LogPanic(ctx context.Context, component, operation string, panicValue interface{}, stackTrace string, recoveredError error) {
	// No-op: Replace with real logging in production
}

// =============================================================================
// Panic Handler Registry (Thread-Safe Hot-Swapping)
// =============================================================================

// PanicHandlerRegistry manages panic handling implementations with thread-safe hot-swapping.
//
// Features:
//   - Thread-safe read/write operations
//   - Lock-free reads using atomic pointers
//   - Hot-swappable implementations at runtime
//   - Global singleton with sensible defaults
type PanicHandlerRegistry struct {
	// Atomic pointers for lock-free reads
	handler atomic.Pointer[PanicHandler]
	metrics atomic.Pointer[PanicMetricsCollector]
	logger  atomic.Pointer[PanicLogger]

	// Mutex only for writes (hot-swapping)
	mu sync.Mutex
}

// NewPanicHandlerRegistry creates a new registry with default implementations.
func NewPanicHandlerRegistry() *PanicHandlerRegistry {
	registry := &PanicHandlerRegistry{}

	// Set defaults
	defaultHandler := PanicHandler(&DefaultPanicHandler{})
	defaultMetrics := PanicMetricsCollector(&NoOpMetricsCollector{})
	defaultLogger := PanicLogger(&NoOpPanicLogger{})

	registry.handler.Store(&defaultHandler)
	registry.metrics.Store(&defaultMetrics)
	registry.logger.Store(&defaultLogger)

	return registry
}

// SetHandler replaces the panic handler (thread-safe).
func (r *PanicHandlerRegistry) SetHandler(handler PanicHandler) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.handler.Store(&handler)
}

// SetMetricsCollector replaces the metrics collector (thread-safe).
func (r *PanicHandlerRegistry) SetMetricsCollector(collector PanicMetricsCollector) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.metrics.Store(&collector)
}

// SetLogger replaces the panic logger (thread-safe).
func (r *PanicHandlerRegistry) SetLogger(logger PanicLogger) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.logger.Store(&logger)
}

// GetHandler returns the current panic handler (lock-free read).
func (r *PanicHandlerRegistry) GetHandler() PanicHandler {
	return *r.handler.Load()
}

// GetMetricsCollector returns the current metrics collector (lock-free read).
func (r *PanicHandlerRegistry) GetMetricsCollector() PanicMetricsCollector {
	return *r.metrics.Load()
}

// GetLogger returns the current panic logger (lock-free read).
func (r *PanicHandlerRegistry) GetLogger() PanicLogger {
	return *r.logger.Load()
}

// HandlePanic is a convenience method that orchestrates the full panic handling flow:
// 1. Convert panic to error using handler
// 2. Record metrics
// 3. Log the event
//
// This is the recommended way to handle panics with all customizations applied.
// errors.As can always obtain *agentErrors.PanicError from the returned error
// chain: the registry adds it when the handler's error lacks it, and returns it
// directly when the handler returns nil. It must be called at the deferred
// recover so that the stack trace points to where the panic occurred.
func (r *PanicHandlerRegistry) HandlePanic(ctx context.Context, component, operation string, panicValue interface{}) error {
	cause := newPanicCause(component, operation, panicValue, nil)
	stackTrace := string(cause.Stack)

	// 1. Convert to error
	handler := r.GetHandler()
	err := withPanicCause(handler.HandlePanic(ctx, component, operation, panicValue, stackTrace), cause)

	// 2. Record metrics
	metrics := r.GetMetricsCollector()
	metrics.RecordPanic(ctx, component, operation, panicValue)

	// 3. Log event
	logger := r.GetLogger()
	logger.LogPanic(ctx, component, operation, panicValue, stackTrace, err)

	return err
}

// newPanicCause 构造 panic 的 *agentErrors.PanicError；stack 为空时在此捕获当前调用栈。
// panicValue 已是 *PanicError（内层恢复后重新抛出）时原样返回，保留最初的位置与调用栈。
func newPanicCause(component, operation string, panicValue interface{}, stack []byte) *agentErrors.PanicError {
	op := component + "." + operation
	if stack == nil {
		return agentErrors.NewPanicError(op, panicValue)
	}
	if p, ok := panicValue.(*agentErrors.PanicError); ok {
		return p
	}
	return &agentErrors.PanicError{Op: op, Value: panicValue, Stack: stack}
}

// withPanicCause 保证 handler 的结果可用 errors.As 识别出 *PanicError，同时不改变 handler 错误的文本与类型链。
func withPanicCause(err error, cause *agentErrors.PanicError) error {
	var panicErr *agentErrors.PanicError
	switch {
	case err == nil:
		return cause
	case errors.As(err, &panicErr):
		return err
	default:
		return &recoveredPanicError{err: err, cause: cause}
	}
}

// recoveredPanicError 在自定义 PanicHandler 返回的错误之外保留 *PanicError 作为 cause，文本沿用 handler 的错误。
type recoveredPanicError struct {
	err   error
	cause *agentErrors.PanicError
}

func (e *recoveredPanicError) Error() string   { return e.err.Error() }
func (e *recoveredPanicError) Unwrap() []error { return []error{e.err, e.cause} }

// CallSafely invokes host code once, for call sites that do not go through the
// Callback interface (stream consumers, chain steps, and so on), with the same
// semantics as NotifyCallbacks: if call panics, panicked is true and err is
// converted by the global PanicHandler, and errors.As can detect
// *agentErrors.PanicError in it.
func CallSafely(ctx context.Context, component, operation string, call func() error) (panicked bool, err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			panicked, err = true, panicToError(ctx, component, operation, recovered)
		}
	}()
	return false, call()
}

// RecoverPanic converts a panic through the global PanicHandler and stores it
// in *err, for goroutines started by the framework that have an error outlet.
// It must be deferred directly as defer core.RecoverPanic(...) for recover to
// take effect.
func RecoverPanic(ctx context.Context, component, operation string, err *error) {
	if recovered := recover(); recovered != nil {
		*err = panicToError(ctx, component, operation, recovered)
	}
}

// ReportPanic is for goroutines with no caller to receive an error: the panic
// only goes to the global PanicHandler for metrics and logging, and the
// goroutine then ends. It must be deferred directly as defer
// core.ReportPanic(...) for recover to take effect.
func ReportPanic(ctx context.Context, component, operation string) {
	if recovered := recover(); recovered != nil {
		_ = panicToError(ctx, component, operation, recovered)
	}
}

// =============================================================================
// Global Registry Singleton
// =============================================================================

var (
	globalPanicRegistry     *PanicHandlerRegistry
	globalPanicRegistryOnce sync.Once
)

// GlobalPanicHandlerRegistry returns the global panic handler registry instance.
//
// This singleton is used by all panic recovery code in the system.
// Customize by calling SetHandler, SetMetricsCollector, or SetLogger.
func GlobalPanicHandlerRegistry() *PanicHandlerRegistry {
	globalPanicRegistryOnce.Do(func() {
		globalPanicRegistry = NewPanicHandlerRegistry()
	})
	return globalPanicRegistry
}

// =============================================================================
// Convenience Functions for Global Registry
// =============================================================================

// SetGlobalPanicHandler sets the global panic handler.
func SetGlobalPanicHandler(handler PanicHandler) {
	GlobalPanicHandlerRegistry().SetHandler(handler)
}

// SetGlobalMetricsCollector sets the global metrics collector.
func SetGlobalMetricsCollector(collector PanicMetricsCollector) {
	GlobalPanicHandlerRegistry().SetMetricsCollector(collector)
}

// SetGlobalPanicLogger sets the global panic logger.
func SetGlobalPanicLogger(logger PanicLogger) {
	GlobalPanicHandlerRegistry().SetLogger(logger)
}
