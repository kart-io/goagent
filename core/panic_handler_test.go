package core

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	agentErrors "github.com/kart-io/goagent/errors"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// =============================================================================
// Custom Implementations for Testing
// =============================================================================

// MockPanicHandler is a mock implementation for testing
type MockPanicHandler struct {
	calls         []PanicCall
	mu            sync.Mutex
	errorToReturn error
}

type PanicCall struct {
	Component  string
	Operation  string
	PanicValue interface{}
	StackTrace string
}

func NewMockPanicHandler() *MockPanicHandler {
	return &MockPanicHandler{
		calls: make([]PanicCall, 0),
	}
}

func (m *MockPanicHandler) HandlePanic(ctx context.Context, component, operation string, panicValue interface{}, stackTrace string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.calls = append(m.calls, PanicCall{
		Component:  component,
		Operation:  operation,
		PanicValue: panicValue,
		StackTrace: stackTrace,
	})

	if m.errorToReturn != nil {
		return m.errorToReturn
	}

	return fmt.Errorf("mock panic: %v", panicValue)
}

func (m *MockPanicHandler) GetCalls() []PanicCall {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]PanicCall{}, m.calls...)
}

func (m *MockPanicHandler) CallCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.calls)
}

// MockMetricsCollector is a mock metrics collector
type MockMetricsCollector struct {
	count atomic.Int64
	calls []MetricsCall
	mu    sync.Mutex
}

type MetricsCall struct {
	Component  string
	Operation  string
	PanicValue interface{}
}

func NewMockMetricsCollector() *MockMetricsCollector {
	return &MockMetricsCollector{
		calls: make([]MetricsCall, 0),
	}
}

func (m *MockMetricsCollector) RecordPanic(ctx context.Context, component, operation string, panicValue interface{}) {
	m.count.Add(1)
	m.mu.Lock()
	defer m.mu.Unlock()
	m.calls = append(m.calls, MetricsCall{
		Component:  component,
		Operation:  operation,
		PanicValue: panicValue,
	})
}

func (m *MockMetricsCollector) Count() int64 {
	return m.count.Load()
}

func (m *MockMetricsCollector) GetCalls() []MetricsCall {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]MetricsCall{}, m.calls...)
}

// MockPanicLogger is a mock logger
type MockPanicLogger struct {
	logs []LogEntry
	mu   sync.Mutex
}

type LogEntry struct {
	Component      string
	Operation      string
	PanicValue     interface{}
	StackTrace     string
	RecoveredError error
}

func NewMockPanicLogger() *MockPanicLogger {
	return &MockPanicLogger{
		logs: make([]LogEntry, 0),
	}
}

func (m *MockPanicLogger) LogPanic(ctx context.Context, component, operation string, panicValue interface{}, stackTrace string, recoveredError error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.logs = append(m.logs, LogEntry{
		Component:      component,
		Operation:      operation,
		PanicValue:     panicValue,
		StackTrace:     stackTrace,
		RecoveredError: recoveredError,
	})
}

func (m *MockPanicLogger) LogCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.logs)
}

func (m *MockPanicLogger) GetLogs() []LogEntry {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]LogEntry{}, m.logs...)
}

// =============================================================================
// Default Implementations Tests
// =============================================================================

func TestDefaultPanicHandler(t *testing.T) {
	handler := &DefaultPanicHandler{}
	ctx := context.Background()

	t.Run("Converts panic to AgentError", func(t *testing.T) {
		err := handler.HandlePanic(ctx, "test_component", "test_op", "test panic", "stack trace here")

		require.Error(t, err)
		agentErr, ok := err.(*agentErrors.AgentError)
		require.True(t, ok, "Should return AgentError")

		assert.Equal(t, agentErrors.CodeInternal, agentErr.Code)
		assert.Equal(t, "test_component", agentErr.Component)
		assert.Equal(t, "test_op", agentErr.Operation)
		assert.Equal(t, "test panic", agentErr.Context["panic_value"])
		assert.Equal(t, "stack trace here", agentErr.Context["stack_trace"])
		assert.Contains(t, agentErr.Message, "panic recovered")
	})

	t.Run("Handles nil panic value", func(t *testing.T) {
		err := handler.HandlePanic(ctx, "comp", "op", nil, "")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "panic recovered")
	})

	t.Run("Handles complex panic value", func(t *testing.T) {
		complexValue := map[string]interface{}{
			"error": "detailed error",
			"code":  500,
		}
		err := handler.HandlePanic(ctx, "comp", "op", complexValue, "")
		require.Error(t, err)

		agentErr := err.(*agentErrors.AgentError)
		assert.Equal(t, complexValue, agentErr.Context["panic_value"])
	})
}

func TestNoOpMetricsCollector(t *testing.T) {
	collector := &NoOpMetricsCollector{}
	ctx := context.Background()

	// Should not panic and do nothing
	collector.RecordPanic(ctx, "test", "op", "panic value")
	collector.RecordPanic(ctx, "test2", "op2", 123)

	// No assertions - just verify it doesn't crash
}

func TestNoOpPanicLogger(t *testing.T) {
	logger := &NoOpPanicLogger{}
	ctx := context.Background()

	// Should not panic and do nothing
	logger.LogPanic(ctx, "test", "op", "panic", "stack", fmt.Errorf("error"))
	logger.LogPanic(ctx, "test2", "op2", 123, "", nil)

	// No assertions - just verify it doesn't crash
}

// =============================================================================
// Registry Tests
// =============================================================================

func TestPanicHandlerRegistry_Creation(t *testing.T) {
	registry := NewPanicHandlerRegistry()

	t.Run("Has default handler", func(t *testing.T) {
		handler := registry.GetHandler()
		assert.NotNil(t, handler)
		assert.IsType(t, &DefaultPanicHandler{}, handler)
	})

	t.Run("Has default metrics collector", func(t *testing.T) {
		collector := registry.GetMetricsCollector()
		assert.NotNil(t, collector)
		assert.IsType(t, &NoOpMetricsCollector{}, collector)
	})

	t.Run("Has default logger", func(t *testing.T) {
		logger := registry.GetLogger()
		assert.NotNil(t, logger)
		assert.IsType(t, &NoOpPanicLogger{}, logger)
	})
}

func TestPanicHandlerRegistry_SetHandler(t *testing.T) {
	registry := NewPanicHandlerRegistry()
	mockHandler := NewMockPanicHandler()

	registry.SetHandler(mockHandler)

	handler := registry.GetHandler()
	assert.Same(t, mockHandler, handler)
}

func TestPanicHandlerRegistry_SetMetricsCollector(t *testing.T) {
	registry := NewPanicHandlerRegistry()
	mockCollector := NewMockMetricsCollector()

	registry.SetMetricsCollector(mockCollector)

	collector := registry.GetMetricsCollector()
	assert.Same(t, mockCollector, collector)
}

func TestPanicHandlerRegistry_SetLogger(t *testing.T) {
	registry := NewPanicHandlerRegistry()
	mockLogger := NewMockPanicLogger()

	registry.SetLogger(mockLogger)

	logger := registry.GetLogger()
	assert.Same(t, mockLogger, logger)
}

func TestPanicHandlerRegistry_HandlePanic(t *testing.T) {
	registry := NewPanicHandlerRegistry()
	mockHandler := NewMockPanicHandler()
	mockMetrics := NewMockMetricsCollector()
	mockLogger := NewMockPanicLogger()

	registry.SetHandler(mockHandler)
	registry.SetMetricsCollector(mockMetrics)
	registry.SetLogger(mockLogger)

	ctx := context.Background()
	err := registry.HandlePanic(ctx, "test_comp", "test_op", "test panic")

	t.Run("Handler is called", func(t *testing.T) {
		assert.Equal(t, 1, mockHandler.CallCount())
		calls := mockHandler.GetCalls()
		require.Len(t, calls, 1)
		assert.Equal(t, "test_comp", calls[0].Component)
		assert.Equal(t, "test_op", calls[0].Operation)
		assert.Equal(t, "test panic", calls[0].PanicValue)
		assert.NotEmpty(t, calls[0].StackTrace) // Stack trace should be captured
	})

	t.Run("Metrics are recorded", func(t *testing.T) {
		assert.Equal(t, int64(1), mockMetrics.Count())
		calls := mockMetrics.GetCalls()
		require.Len(t, calls, 1)
		assert.Equal(t, "test_comp", calls[0].Component)
		assert.Equal(t, "test_op", calls[0].Operation)
		assert.Equal(t, "test panic", calls[0].PanicValue)
	})

	t.Run("Logs are written", func(t *testing.T) {
		assert.Equal(t, 1, mockLogger.LogCount())
		logs := mockLogger.GetLogs()
		require.Len(t, logs, 1)
		assert.Equal(t, "test_comp", logs[0].Component)
		assert.Equal(t, "test_op", logs[0].Operation)
		assert.Equal(t, "test panic", logs[0].PanicValue)
		assert.NotEmpty(t, logs[0].StackTrace)
		assert.Equal(t, err, logs[0].RecoveredError)
	})

	t.Run("Returns error from handler", func(t *testing.T) {
		require.Error(t, err)
		assert.Contains(t, err.Error(), "mock panic")
	})
}

func TestPanicHandlerRegistry_ThreadSafety(t *testing.T) {
	registry := NewPanicHandlerRegistry()
	ctx := context.Background()

	var wg sync.WaitGroup
	concurrency := 100

	// Concurrent reads
	wg.Add(concurrency)
	for i := 0; i < concurrency; i++ {
		go func() {
			defer wg.Done()
			_ = registry.GetHandler()
			_ = registry.GetMetricsCollector()
			_ = registry.GetLogger()
		}()
	}

	// Concurrent writes
	wg.Add(concurrency)
	for i := 0; i < concurrency; i++ {
		go func(idx int) {
			defer wg.Done()
			if idx%3 == 0 {
				registry.SetHandler(NewMockPanicHandler())
			} else if idx%3 == 1 {
				registry.SetMetricsCollector(NewMockMetricsCollector())
			} else {
				registry.SetLogger(NewMockPanicLogger())
			}
		}(i)
	}

	// Concurrent HandlePanic calls
	wg.Add(concurrency)
	for i := 0; i < concurrency; i++ {
		go func(idx int) {
			defer wg.Done()
			_ = registry.HandlePanic(ctx, "test", "op", fmt.Sprintf("panic %d", idx))
		}(i)
	}

	wg.Wait()

	// Verify registry is still functional
	handler := registry.GetHandler()
	assert.NotNil(t, handler)
}

// =============================================================================
// Global Registry Tests
// =============================================================================

func TestGlobalPanicHandlerRegistry(t *testing.T) {
	registry1 := GlobalPanicHandlerRegistry()
	registry2 := GlobalPanicHandlerRegistry()

	assert.Same(t, registry1, registry2, "Should return same singleton instance")
}

func TestSetGlobalPanicHandler(t *testing.T) {
	// Save original
	originalHandler := GlobalPanicHandlerRegistry().GetHandler()
	defer GlobalPanicHandlerRegistry().SetHandler(originalHandler)

	mockHandler := NewMockPanicHandler()
	SetGlobalPanicHandler(mockHandler)

	handler := GlobalPanicHandlerRegistry().GetHandler()
	assert.Same(t, mockHandler, handler)
}

func TestSetGlobalMetricsCollector(t *testing.T) {
	// Save original
	originalCollector := GlobalPanicHandlerRegistry().GetMetricsCollector()
	defer GlobalPanicHandlerRegistry().SetMetricsCollector(originalCollector)

	mockCollector := NewMockMetricsCollector()
	SetGlobalMetricsCollector(mockCollector)

	collector := GlobalPanicHandlerRegistry().GetMetricsCollector()
	assert.Same(t, mockCollector, collector)
}

func TestSetGlobalPanicLogger(t *testing.T) {
	// Save original
	originalLogger := GlobalPanicHandlerRegistry().GetLogger()
	defer GlobalPanicHandlerRegistry().SetLogger(originalLogger)

	mockLogger := NewMockPanicLogger()
	SetGlobalPanicLogger(mockLogger)

	logger := GlobalPanicHandlerRegistry().GetLogger()
	assert.Same(t, mockLogger, logger)
}

// =============================================================================
// Integration Tests with panicToError
// =============================================================================

func TestPanicToError_UsesGlobalRegistry(t *testing.T) {
	// Save originals
	originalHandler := GlobalPanicHandlerRegistry().GetHandler()
	originalMetrics := GlobalPanicHandlerRegistry().GetMetricsCollector()
	originalLogger := GlobalPanicHandlerRegistry().GetLogger()
	defer func() {
		GlobalPanicHandlerRegistry().SetHandler(originalHandler)
		GlobalPanicHandlerRegistry().SetMetricsCollector(originalMetrics)
		GlobalPanicHandlerRegistry().SetLogger(originalLogger)
	}()

	// Set mocks
	mockHandler := NewMockPanicHandler()
	mockMetrics := NewMockMetricsCollector()
	mockLogger := NewMockPanicLogger()

	SetGlobalPanicHandler(mockHandler)
	SetGlobalMetricsCollector(mockMetrics)
	SetGlobalPanicLogger(mockLogger)

	// Call panicToError
	ctx := context.Background()
	err := panicToError(ctx, "test_component", "test_operation", "test panic value")

	// Verify handler was called
	assert.Equal(t, 1, mockHandler.CallCount())
	calls := mockHandler.GetCalls()
	require.Len(t, calls, 1)
	assert.Equal(t, "test_component", calls[0].Component)
	assert.Equal(t, "test_operation", calls[0].Operation)
	assert.Equal(t, "test panic value", calls[0].PanicValue)

	// Verify metrics were recorded
	assert.Equal(t, int64(1), mockMetrics.Count())

	// Verify logs were written
	assert.Equal(t, 1, mockLogger.LogCount())

	// Verify error is returned
	require.Error(t, err)
}

func TestSafeInvoke_UsesGlobalRegistry(t *testing.T) {
	// Save originals
	originalHandler := GlobalPanicHandlerRegistry().GetHandler()
	originalMetrics := GlobalPanicHandlerRegistry().GetMetricsCollector()
	originalLogger := GlobalPanicHandlerRegistry().GetLogger()
	defer func() {
		GlobalPanicHandlerRegistry().SetHandler(originalHandler)
		GlobalPanicHandlerRegistry().SetMetricsCollector(originalMetrics)
		GlobalPanicHandlerRegistry().SetLogger(originalLogger)
	}()

	// Set mocks
	mockHandler := NewMockPanicHandler()
	mockMetrics := NewMockMetricsCollector()
	mockLogger := NewMockPanicLogger()

	SetGlobalPanicHandler(mockHandler)
	SetGlobalMetricsCollector(mockMetrics)
	SetGlobalPanicLogger(mockLogger)

	// Function that panics
	panicFunc := func(ctx context.Context, input string) (string, error) {
		panic("intentional panic")
	}

	// Call safeInvoke
	ctx := context.Background()
	result, err := safeInvoke(panicFunc, ctx, "test input")

	// Verify panic was handled
	assert.Empty(t, result, "Should return zero value")
	require.Error(t, err)

	// Verify handler was called with correct component
	assert.Equal(t, 1, mockHandler.CallCount())
	calls := mockHandler.GetCalls()
	require.Len(t, calls, 1)
	assert.Equal(t, "runnable", calls[0].Component)
	assert.Equal(t, "invoke", calls[0].Operation)
	assert.Equal(t, "intentional panic", calls[0].PanicValue)

	// Verify metrics and logs
	assert.Equal(t, int64(1), mockMetrics.Count())
	assert.Equal(t, 1, mockLogger.LogCount())
}

// =============================================================================
// Hot-Swapping Tests
// =============================================================================

func TestHotSwapping_DuringExecution(t *testing.T) {
	registry := NewPanicHandlerRegistry()
	ctx := context.Background()

	// Initial handler
	handler1 := NewMockPanicHandler()
	registry.SetHandler(handler1)

	// First panic
	err1 := registry.HandlePanic(ctx, "comp1", "op1", "panic1")
	require.Error(t, err1)
	assert.Equal(t, 1, handler1.CallCount())

	// Hot-swap handler
	handler2 := NewMockPanicHandler()
	registry.SetHandler(handler2)

	// Second panic - should use new handler
	err2 := registry.HandlePanic(ctx, "comp2", "op2", "panic2")
	require.Error(t, err2)

	// Verify handler1 still has 1 call, handler2 has 1 call
	assert.Equal(t, 1, handler1.CallCount())
	assert.Equal(t, 1, handler2.CallCount())
}

func TestHotSwapping_ConcurrentWithUsage(t *testing.T) {
	registry := NewPanicHandlerRegistry()
	ctx := context.Background()

	var wg sync.WaitGroup
	duration := 100 * time.Millisecond
	stopTime := time.Now().Add(duration)

	// Goroutine 1: Continuously handle panics
	wg.Add(1)
	go func() {
		defer wg.Done()
		counter := 0
		for time.Now().Before(stopTime) {
			_ = registry.HandlePanic(ctx, "test", "op", counter)
			counter++
			time.Sleep(1 * time.Millisecond)
		}
	}()

	// Goroutine 2: Hot-swap handlers
	wg.Add(1)
	go func() {
		defer wg.Done()
		for time.Now().Before(stopTime) {
			registry.SetHandler(NewMockPanicHandler())
			time.Sleep(5 * time.Millisecond)
		}
	}()

	// Goroutine 3: Hot-swap metrics collectors
	wg.Add(1)
	go func() {
		defer wg.Done()
		for time.Now().Before(stopTime) {
			registry.SetMetricsCollector(NewMockMetricsCollector())
			time.Sleep(7 * time.Millisecond)
		}
	}()

	// Goroutine 4: Hot-swap loggers
	wg.Add(1)
	go func() {
		defer wg.Done()
		for time.Now().Before(stopTime) {
			registry.SetLogger(NewMockPanicLogger())
			time.Sleep(3 * time.Millisecond)
		}
	}()

	wg.Wait()

	// Verify registry is still functional
	err := registry.HandlePanic(ctx, "final", "test", "final panic")
	require.Error(t, err)
}
