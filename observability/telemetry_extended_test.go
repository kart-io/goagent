package observability

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestDefaultTelemetryConfig_Values verifies all default configuration values
func TestDefaultTelemetryConfig_Values(t *testing.T) {
	config := DefaultTelemetryConfig()
	require.NotNil(t, config)

	assert.Equal(t, "agent-service", config.ServiceName)
	assert.Equal(t, "1.0.0", config.ServiceVersion)
	assert.Equal(t, "development", config.Environment)
	assert.True(t, config.TraceEnabled)
	assert.Equal(t, "otlp", config.TraceExporter)
	assert.Equal(t, "localhost:4317", config.TraceEndpoint)
	assert.Equal(t, float64(1.0), config.TraceSampleRate)
	assert.True(t, config.MetricsEnabled)
	assert.Equal(t, "prometheus", config.MetricsExporter)
	assert.Equal(t, 60*time.Second, config.MetricsInterval)
	assert.False(t, config.LogsEnabled)
	assert.NotNil(t, config.ResourceAttributes)
}

// TestNewTelemetryProvider_UnsupportedTraceExporter tests error handling for unsupported exporters
func TestNewTelemetryProvider_UnsupportedTraceExporter(t *testing.T) {
	config := &TelemetryConfig{
		ServiceName:    "test-service",
		ServiceVersion: "1.0.0",
		Environment:    "test",
		TraceEnabled:   true,
		TraceExporter:  "unsupported-exporter",
		MetricsEnabled: false,
	}

	provider, err := NewTelemetryProvider(config)
	assert.Error(t, err)
	assert.Nil(t, provider)
}

// TestNewTelemetryProvider_NoopExporter verifies noop exporter works
func TestNewTelemetryProvider_NoopExporter(t *testing.T) {
	config := &TelemetryConfig{
		ServiceName:    "test-service",
		ServiceVersion: "1.0.0",
		Environment:    "test",
		TraceEnabled:   true,
		TraceExporter:  "noop",
		MetricsEnabled: false,
	}

	provider, err := NewTelemetryProvider(config)
	require.NoError(t, err)
	require.NotNil(t, provider)
	defer provider.Shutdown(context.Background())

	assert.NotNil(t, provider.config)
	assert.NotNil(t, provider.resource)
}

// TestNewTelemetryProvider_StdoutExporter verifies stdout exporter (which uses noop)
func TestNewTelemetryProvider_StdoutExporter(t *testing.T) {
	config := &TelemetryConfig{
		ServiceName:    "test-service",
		ServiceVersion: "1.0.0",
		Environment:    "test",
		TraceEnabled:   true,
		TraceExporter:  "stdout",
		MetricsEnabled: false,
	}

	provider, err := NewTelemetryProvider(config)
	require.NoError(t, err)
	require.NotNil(t, provider)
	defer provider.Shutdown(context.Background())
}

// TestNewTelemetryProvider_WithResourceAttributes tests resource attribute configuration
func TestNewTelemetryProvider_WithResourceAttributes(t *testing.T) {
	config := &TelemetryConfig{
		ServiceName:    "test-service",
		ServiceVersion: "1.0.0",
		Environment:    "test",
		TraceEnabled:   true,
		TraceExporter:  "noop",
		MetricsEnabled: false,
		ResourceAttributes: map[string]string{
			"custom.attr1": "value1",
			"custom.attr2": "value2",
		},
	}

	provider, err := NewTelemetryProvider(config)
	require.NoError(t, err)
	require.NotNil(t, provider)
	defer provider.Shutdown(context.Background())

	assert.NotNil(t, provider.resource)
}

// TestNewTelemetryProvider_EmptyResourceAttributes tests with empty map
func TestNewTelemetryProvider_EmptyResourceAttributes(t *testing.T) {
	config := &TelemetryConfig{
		ServiceName:        "test-service",
		ServiceVersion:     "1.0.0",
		Environment:        "test",
		TraceEnabled:       true,
		TraceExporter:      "noop",
		MetricsEnabled:     false,
		ResourceAttributes: make(map[string]string),
	}

	provider, err := NewTelemetryProvider(config)
	require.NoError(t, err)
	require.NotNil(t, provider)
	defer provider.Shutdown(context.Background())
}

// TestTelemetryProvider_SamplingRateBoundaries tests edge cases for sampling rate
func TestTelemetryProvider_SamplingRateBoundaries(t *testing.T) {
	tests := []struct {
		name       string
		sampleRate float64
	}{
		{"zero rate", 0.0},
		{"quarter rate", 0.25},
		{"half rate", 0.5},
		{"three quarter rate", 0.75},
		{"full rate", 1.0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config := &TelemetryConfig{
				ServiceName:     "test-service",
				ServiceVersion:  "1.0.0",
				Environment:     "test",
				TraceEnabled:    true,
				TraceExporter:   "noop",
				TraceSampleRate: tt.sampleRate,
				MetricsEnabled:  false,
			}

			provider, err := NewTelemetryProvider(config)
			require.NoError(t, err)
			require.NotNil(t, provider)
			defer provider.Shutdown(context.Background())
		})
	}
}

// TestTelemetryProvider_GetTracer_NoopBehavior tests tracer behavior when not enabled
func TestTelemetryProvider_GetTracer_NoopBehavior(t *testing.T) {
	config := &TelemetryConfig{
		ServiceName:    "test-service",
		ServiceVersion: "1.0.0",
		Environment:    "test",
		TraceEnabled:   false,
		MetricsEnabled: false,
	}

	provider, err := NewTelemetryProvider(config)
	require.NoError(t, err)
	defer provider.Shutdown(context.Background())

	tracer := provider.GetTracer("test-tracer")
	assert.NotNil(t, tracer)

	// Should return a noop tracer
	ctx, span := tracer.Start(context.Background(), "test")
	assert.NotNil(t, span)
	span.End()
	assert.NotNil(t, ctx)
}

// TestTelemetryProvider_GetMeter_MultipleMeters tests creating multiple meters
func TestTelemetryProvider_GetMeter_MultipleMeters(t *testing.T) {
	config := &TelemetryConfig{
		ServiceName:     "test-service",
		ServiceVersion:  "1.0.0",
		Environment:     "test",
		TraceEnabled:    false,
		MetricsEnabled:  true,
		MetricsExporter: "noop",
		MetricsInterval: 60 * time.Second,
	}

	provider, err := NewTelemetryProvider(config)
	require.NoError(t, err)
	defer provider.Shutdown(context.Background())

	meter1 := provider.GetMeter("meter-1")
	meter2 := provider.GetMeter("meter-2")
	meter3 := provider.GetMeter("meter-3")

	assert.NotNil(t, meter1)
	assert.NotNil(t, meter2)
	assert.NotNil(t, meter3)
}

// TestTelemetryProvider_Shutdown_MultipleProviders tests shutting down multiple providers
func TestTelemetryProvider_Shutdown_MultipleProviders(t *testing.T) {
	config := &TelemetryConfig{
		ServiceName:     "test-service",
		ServiceVersion:  "1.0.0",
		Environment:     "test",
		TraceEnabled:    true,
		TraceExporter:   "noop",
		MetricsEnabled:  true,
		MetricsExporter: "noop",
		MetricsInterval: 60 * time.Second,
	}

	provider1, err := NewTelemetryProvider(config)
	require.NoError(t, err)

	provider2, err := NewTelemetryProvider(config)
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err1 := provider1.Shutdown(ctx)
	err2 := provider2.Shutdown(ctx)

	assert.NoError(t, err1)
	assert.NoError(t, err2)
}

// TestTelemetryProvider_Shutdown_WithTimeout tests shutdown with context timeout
func TestTelemetryProvider_Shutdown_WithTimeout(t *testing.T) {
	config := &TelemetryConfig{
		ServiceName:     "test-service",
		ServiceVersion:  "1.0.0",
		Environment:     "test",
		TraceEnabled:    true,
		TraceExporter:   "noop",
		MetricsEnabled:  true,
		MetricsExporter: "noop",
		MetricsInterval: 60 * time.Second,
	}

	provider, err := NewTelemetryProvider(config)
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()

	err = provider.Shutdown(ctx)
	assert.NoError(t, err)
}

// TestTelemetryProvider_ForceFlush_MultipleMeters tests force flush with multiple operations
func TestTelemetryProvider_ForceFlush_MultipleMeters(t *testing.T) {
	config := &TelemetryConfig{
		ServiceName:     "test-service",
		ServiceVersion:  "1.0.0",
		Environment:     "test",
		TraceEnabled:    true,
		TraceExporter:   "noop",
		MetricsEnabled:  true,
		MetricsExporter: "noop",
		MetricsInterval: 60 * time.Second,
	}

	provider, err := NewTelemetryProvider(config)
	require.NoError(t, err)
	defer provider.Shutdown(context.Background())

	// Get multiple meters
	meter1 := provider.GetMeter("meter-1")
	meter2 := provider.GetMeter("meter-2")

	// Create some metrics
	counter1, _ := meter1.Int64Counter("counter1")
	counter1.Add(context.Background(), 1)

	counter2, _ := meter2.Int64Counter("counter2")
	counter2.Add(context.Background(), 2)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err = provider.ForceFlush(ctx)
	assert.NoError(t, err)
}

// TestTelemetryProvider_FullLifecycle tests complete provider lifecycle
func TestTelemetryProvider_FullLifecycle(t *testing.T) {
	config := &TelemetryConfig{
		ServiceName:     "test-service",
		ServiceVersion:  "1.0.0",
		Environment:     "test",
		TraceEnabled:    true,
		TraceExporter:   "noop",
		TraceSampleRate: 0.5,
		MetricsEnabled:  true,
		MetricsExporter: "noop",
		MetricsInterval: 60 * time.Second,
	}

	// Create provider
	provider, err := NewTelemetryProvider(config)
	require.NoError(t, err)
	require.NotNil(t, provider)

	// Get tracer and create spans
	tracer := provider.GetTracer("test-tracer")
	ctx, span := tracer.Start(context.Background(), "operation")
	span.End()
	assert.NotNil(t, ctx)

	// Get meter and create metrics
	meter := provider.GetMeter("test-meter")
	counter, err := meter.Int64Counter("test_counter")
	require.NoError(t, err)
	counter.Add(context.Background(), 5)

	// Force flush
	flushCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	err = provider.ForceFlush(flushCtx)
	assert.NoError(t, err)

	// Shutdown
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	err = provider.Shutdown(shutdownCtx)
	assert.NoError(t, err)
}

// TestTelemetryProvider_ConfigValidation tests configuration edge cases
func TestTelemetryProvider_ConfigValidation(t *testing.T) {
	tests := []struct {
		name   string
		config *TelemetryConfig
	}{
		{
			name: "minimal config",
			config: &TelemetryConfig{
				ServiceName: "minimal",
			},
		},
		{
			name: "production-like config",
			config: &TelemetryConfig{
				ServiceName:     "prod-service",
				ServiceVersion:  "2.1.0",
				Environment:     "production",
				TraceEnabled:    true,
				TraceExporter:   "noop",
				TraceSampleRate: 0.1,
				MetricsEnabled:  true,
				MetricsExporter: "noop",
				MetricsInterval: 30 * time.Second,
			},
		},
		{
			name: "development-like config",
			config: &TelemetryConfig{
				ServiceName:     "dev-service",
				ServiceVersion:  "0.1.0",
				Environment:     "development",
				TraceEnabled:    true,
				TraceExporter:   "noop",
				TraceSampleRate: 1.0,
				MetricsEnabled:  true,
				MetricsExporter: "noop",
				MetricsInterval: 5 * time.Second,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			provider, err := NewTelemetryProvider(tt.config)
			require.NoError(t, err)
			require.NotNil(t, provider)
			defer provider.Shutdown(context.Background())
		})
	}
}

// BenchmarkNewTelemetryProvider benchmarks provider creation
func BenchmarkNewTelemetryProvider(b *testing.B) {
	config := &TelemetryConfig{
		ServiceName:     "bench-service",
		ServiceVersion:  "1.0.0",
		Environment:     "test",
		TraceEnabled:    true,
		TraceExporter:   "noop",
		MetricsEnabled:  true,
		MetricsExporter: "noop",
		MetricsInterval: 60 * time.Second,
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		provider, err := NewTelemetryProvider(config)
		require.NoError(b, err)
		provider.Shutdown(context.Background())
	}
}

// BenchmarkTelemetryProvider_GetTracer benchmarks tracer retrieval
func BenchmarkTelemetryProvider_GetTracer_Cached(b *testing.B) {
	config := &TelemetryConfig{
		ServiceName:    "bench-service",
		ServiceVersion: "1.0.0",
		Environment:    "test",
		TraceEnabled:   true,
		TraceExporter:  "noop",
		MetricsEnabled: false,
	}

	provider, _ := NewTelemetryProvider(config)
	defer provider.Shutdown(context.Background())

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = provider.GetTracer("cached-tracer")
	}
}

// BenchmarkTelemetryProvider_GetMeter benchmarks meter retrieval
func BenchmarkTelemetryProvider_GetMeter_Cached(b *testing.B) {
	config := &TelemetryConfig{
		ServiceName:     "bench-service",
		ServiceVersion:  "1.0.0",
		Environment:     "test",
		TraceEnabled:    false,
		MetricsEnabled:  true,
		MetricsExporter: "noop",
		MetricsInterval: 60 * time.Second,
	}

	provider, _ := NewTelemetryProvider(config)
	defer provider.Shutdown(context.Background())

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = provider.GetMeter("cached-meter")
	}
}

// BenchmarkTelemetryProvider_FullCycle benchmarks complete cycle
func BenchmarkTelemetryProvider_FullCycle(b *testing.B) {
	config := &TelemetryConfig{
		ServiceName:     "bench-service",
		ServiceVersion:  "1.0.0",
		Environment:     "test",
		TraceEnabled:    true,
		TraceExporter:   "noop",
		MetricsEnabled:  true,
		MetricsExporter: "noop",
		MetricsInterval: 60 * time.Second,
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		provider, _ := NewTelemetryProvider(config)
		tracer := provider.GetTracer("tracer")
		ctx, span := tracer.Start(context.Background(), "op")
		span.End()

		meter := provider.GetMeter("meter")
		counter, _ := meter.Int64Counter("counter")
		counter.Add(ctx, 1)

		provider.ForceFlush(context.Background())
		provider.Shutdown(context.Background())
	}
}
