package core

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

// mockLogger implements Logger interface for testing callbacks
type mockLogger struct {
	mu     sync.Mutex
	infos  []string
	errors []string
	debugs []string
}

func (m *mockLogger) Info(msg string, fields ...interface{}) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.infos = append(m.infos, msg)
}

func (m *mockLogger) Error(msg string, fields ...interface{}) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.errors = append(m.errors, msg)
}

func (m *mockLogger) Debug(msg string, fields ...interface{}) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.debugs = append(m.debugs, msg)
}

// mockMetricsCollector implements MetricsCollector interface
type mockMetricsCollector struct {
	mu         sync.Mutex
	counters   map[string]int64
	histograms map[string][]float64
}

func newMockMetricsCollector() *mockMetricsCollector {
	return &mockMetricsCollector{
		counters:   make(map[string]int64),
		histograms: make(map[string][]float64),
	}
}

func (m *mockMetricsCollector) IncrementCounter(name string, value int64, tags map[string]string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.counters[name] += value
}

func (m *mockMetricsCollector) RecordHistogram(name string, value float64, tags map[string]string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.histograms[name] = append(m.histograms[name], value)
}

func (m *mockMetricsCollector) RecordGauge(name string, value float64, tags map[string]string) {
	// Not used in these tests
}

// mockTracer implements Tracer interface
type mockTracer struct {
	spans []*mockSpan
}

func (m *mockTracer) StartSpan(ctx context.Context, name string, attrs map[string]interface{}) (context.Context, Span) {
	span := &mockSpan{
		name:       name,
		attributes: make(map[string]interface{}),
	}
	for k, v := range attrs {
		span.attributes[k] = v
	}
	m.spans = append(m.spans, span)
	return ctx, span
}

// mockSpan implements Span interface
type mockSpan struct {
	name       string
	attributes map[string]interface{}
	status     StatusCode
	statusMsg  string
	ended      bool
	errors     []error
}

func (s *mockSpan) End() {
	s.ended = true
}

func (s *mockSpan) SetAttribute(key string, value interface{}) {
	s.attributes[key] = value
}

func (s *mockSpan) SetStatus(code StatusCode, description string) {
	s.status = code
	s.statusMsg = description
}

func (s *mockSpan) RecordError(err error) {
	s.errors = append(s.errors, err)
}

func TestNewBaseCallback(t *testing.T) {
	cb := NewBaseCallback()
	if cb == nil {
		t.Fatal("NewBaseCallback returned nil")
	}

	ctx := context.Background()

	// All methods should return nil
	if err := cb.OnStart(ctx, "test"); err != nil {
		t.Errorf("OnStart returned error: %v", err)
	}
	if err := cb.OnEnd(ctx, "result"); err != nil {
		t.Errorf("OnEnd returned error: %v", err)
	}
	if err := cb.OnError(ctx, errors.New("test")); err != nil {
		t.Errorf("OnError returned error: %v", err)
	}
}

func TestNewCallbackManager(t *testing.T) {
	cb := NewBaseCallback()
	manager := NewCallbackManager(cb)

	if manager == nil {
		t.Fatal("NewCallbackManager returned nil")
	}

	// Test AddCallback
	cb2 := NewBaseCallback()
	manager.AddCallback(cb2)

	// Test RemoveCallback
	manager.RemoveCallback(cb)
}

func TestCallbackManager_TriggerCallbacks(t *testing.T) {
	manager := NewCallbackManager()

	called := 0
	err := manager.TriggerCallbacks(func(cb Callback) error {
		called++
		return nil
	})
	if err != nil {
		t.Errorf("TriggerCallbacks returned error: %v", err)
	}

	// Add callback and test again
	manager.AddCallback(NewBaseCallback())
	called = 0
	err = manager.TriggerCallbacks(func(cb Callback) error {
		called++
		return nil
	})
	if err != nil {
		t.Errorf("TriggerCallbacks returned error: %v", err)
	}
	if called != 1 {
		t.Errorf("Expected 1 callback, got %d", called)
	}
}

func TestCallbackManager_OnMethods(t *testing.T) {
	manager := NewCallbackManager()
	ctx := context.Background()

	if err := manager.OnStart(ctx, "test"); err != nil {
		t.Errorf("OnStart returned error: %v", err)
	}
	if err := manager.OnEnd(ctx, "test"); err != nil {
		t.Errorf("OnEnd returned error: %v", err)
	}
	if err := manager.OnError(ctx, errors.New("test")); err != nil {
		t.Errorf("OnError returned error: %v", err)
	}
}

func TestNewLoggingCallback(t *testing.T) {
	logger := &mockLogger{}
	cb := NewLoggingCallback(logger, true)

	if cb == nil {
		t.Fatal("NewLoggingCallback returned nil")
	}

	ctx := context.Background()

	// Test methods
	cb.OnStart(ctx, "input")
	cb.OnEnd(ctx, "output")
	cb.OnError(ctx, errors.New("error"))
	cb.OnLLMStart(ctx, []string{"prompt"}, "gpt-4")
	cb.OnLLMEnd(ctx, "output", 100)
	cb.OnToolStart(ctx, "tool", "input")
	cb.OnToolEnd(ctx, "tool", "output")

	logger.mu.Lock()
	defer logger.mu.Unlock()

	if len(logger.infos) == 0 {
		t.Error("Expected some info logs")
	}
	if len(logger.errors) == 0 {
		t.Error("Expected some error logs")
	}
}

func TestNewMetricsCallback(t *testing.T) {
	metrics := newMockMetricsCollector()
	cb := NewMetricsCallback(metrics)

	if cb == nil {
		t.Fatal("NewMetricsCallback returned nil")
	}

	ctx := context.Background()

	// Test LLM metrics
	cb.OnLLMStart(ctx, []string{"prompt"}, "gpt-4")
	time.Sleep(10 * time.Millisecond)
	cb.OnLLMEnd(ctx, "output", 100)

	metrics.mu.Lock()
	defer metrics.mu.Unlock()

	if metrics.counters["llm.calls"] != 1 {
		t.Errorf("Expected llm.calls=1, got %d", metrics.counters["llm.calls"])
	}
	if len(metrics.histograms["llm.latency"]) != 1 {
		t.Error("Expected latency to be recorded")
	}
}

func TestNewTracingCallback(t *testing.T) {
	tracer := &mockTracer{}
	cb := NewTracingCallback(tracer)

	if cb == nil {
		t.Fatal("NewTracingCallback returned nil")
	}

	ctx := context.Background()

	// Test LLM tracing
	cb.OnLLMStart(ctx, []string{"prompt"}, "gpt-4")
	if len(tracer.spans) != 1 {
		t.Fatalf("Expected 1 span, got %d", len(tracer.spans))
	}

	cb.OnLLMEnd(ctx, "output", 100)
	if !tracer.spans[0].ended {
		t.Error("Expected span to be ended")
	}
}

func TestTracingCallback_OnLLMError(t *testing.T) {
	tracer := &mockTracer{}
	cb := NewTracingCallback(tracer)
	ctx := context.Background()

	cb.OnLLMStart(ctx, []string{"prompt"}, "gpt-4")
	testErr := errors.New("llm error")
	cb.OnLLMError(ctx, testErr)

	if !tracer.spans[0].ended {
		t.Error("Expected span to be ended")
	}
	if tracer.spans[0].status != StatusCodeError {
		t.Error("Expected error status")
	}
}

func TestNewCostTrackingCallback(t *testing.T) {
	pricing := map[string]float64{
		"gpt-4": 0.00003,
	}
	cb := NewCostTrackingCallback(pricing)

	if cb == nil {
		t.Fatal("NewCostTrackingCallback returned nil")
	}

	ctx := context.Background()
	cb.OnLLMEnd(ctx, "output", 1000)

	if cb.GetTotalTokens() != 1000 {
		t.Errorf("Expected 1000 tokens, got %d", cb.GetTotalTokens())
	}

	cb.Reset()
	if cb.GetTotalTokens() != 0 {
		t.Error("Expected tokens to be reset")
	}
}

func TestNewStdoutCallback(t *testing.T) {
	cb := NewStdoutCallback(false)
	if cb == nil {
		t.Fatal("NewStdoutCallback returned nil")
	}

	ctx := context.Background()

	// Just test that these don't panic
	cb.OnLLMStart(ctx, []string{"prompt"}, "gpt-4")
	cb.OnLLMEnd(ctx, "output", 100)
	cb.OnToolStart(ctx, "tool", "input")
	cb.OnToolEnd(ctx, "tool", "output")
	cb.OnError(ctx, errors.New("error"))
}
