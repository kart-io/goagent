// Package core provides panic handling infrastructure with hot-pluggable implementations.
//
// This file defines interfaces and registry for panic recovery customization:
//   - PanicHandler - Custom panic recovery strategies
//   - PanicMetricsCollector - Statistics and monitoring
//   - PanicLogger - Specialized logging
//   - PanicHandlerRegistry - Thread-safe hot-swapping
package core

import (
	"context"
	"fmt"
	"runtime/debug"
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
//   - Preserves panic value and stack trace in error context
//   - Compatible with existing error handling infrastructure
type DefaultPanicHandler struct{}

// HandlePanic implements PanicHandler interface.
func (h *DefaultPanicHandler) HandlePanic(ctx context.Context, component, operation string, panicValue interface{}, stackTrace string) error {
	return agentErrors.New(
		agentErrors.CodeInternal,
		fmt.Sprintf("panic recovered: %v", panicValue),
	).
		WithComponent(component).
		WithOperation(operation).
		WithContext("panic_value", panicValue).
		WithContext("stack_trace", stackTrace)
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
func (r *PanicHandlerRegistry) HandlePanic(ctx context.Context, component, operation string, panicValue interface{}) error {
	// Capture stack trace
	stackTrace := string(debug.Stack())

	// 1. Convert to error
	handler := r.GetHandler()
	err := handler.HandlePanic(ctx, component, operation, panicValue, stackTrace)

	// 2. Record metrics
	metrics := r.GetMetricsCollector()
	metrics.RecordPanic(ctx, component, operation, panicValue)

	// 3. Log event
	logger := r.GetLogger()
	logger.LogPanic(ctx, component, operation, panicValue, stackTrace, err)

	return err
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
