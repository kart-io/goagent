package observability

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewTelemetryProvider(t *testing.T) {
	tests := []struct {
		name    string
		config  *TelemetryConfig
		wantErr bool
	}{
		{
			name: "default config",
			config: &TelemetryConfig{
				ServiceName:     "test-service",
				ServiceVersion:  "1.0.0",
				Environment:     "test",
				TraceEnabled:    true,
				TraceExporter:   "noop",
				MetricsEnabled:  true,
				MetricsExporter: "noop",
				MetricsInterval: 60 * time.Second,
			},
			wantErr: false,
		},
		{
			name:    "nil config uses default",
			config:  nil,
			wantErr: false,
		},
		{
			name: "trace only",
			config: &TelemetryConfig{
				ServiceName:    "test-service",
				ServiceVersion: "1.0.0",
				Environment:    "test",
				TraceEnabled:   true,
				TraceExporter:  "noop",
				MetricsEnabled: false,
			},
			wantErr: false,
		},
		{
			name: "metrics only",
			config: &TelemetryConfig{
				ServiceName:     "test-service",
				ServiceVersion:  "1.0.0",
				Environment:     "test",
				TraceEnabled:    false,
				MetricsEnabled:  true,
				MetricsExporter: "noop",
				MetricsInterval: 60 * time.Second,
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			provider, err := NewTelemetryProvider(tt.config)
			if tt.wantErr {
				assert.Error(t, err)
				return
			}

			require.NoError(t, err)
			require.NotNil(t, provider)

			// 验证提供者
			assert.NotNil(t, provider.config)
			assert.NotNil(t, provider.resource)

			// 清理
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			err = provider.Shutdown(ctx)
			assert.NoError(t, err)
		})
	}
}

func TestTelemetryProvider_GetTracer(t *testing.T) {
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

	// 获取 Tracer
	tracer := provider.GetTracer("test-tracer")
	assert.NotNil(t, tracer)

	// 创建 span
	ctx, span := tracer.Start(context.Background(), "test-operation")
	assert.NotNil(t, span)
	span.End()

	// 验证上下文
	assert.NotNil(t, ctx)
}

func TestTelemetryProvider_GetMeter(t *testing.T) {
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
	require.NotNil(t, provider)
	defer provider.Shutdown(context.Background())

	// 获取 Meter
	meter := provider.GetMeter("test-meter")
	assert.NotNil(t, meter)

	// 创建 counter
	counter, err := meter.Int64Counter("test_counter")
	require.NoError(t, err)
	assert.NotNil(t, counter)

	// 增加计数
	counter.Add(context.Background(), 1)
}

func TestTelemetryProvider_Shutdown(t *testing.T) {
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
	require.NotNil(t, provider)

	// Shutdown
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err = provider.Shutdown(ctx)
	assert.NoError(t, err)
}

func TestTelemetryProvider_ForceFlush(t *testing.T) {
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
	require.NotNil(t, provider)
	defer provider.Shutdown(context.Background())

	// ForceFlush
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err = provider.ForceFlush(ctx)
	assert.NoError(t, err)
}

func TestDefaultTelemetryConfig(t *testing.T) {
	config := DefaultTelemetryConfig()
	require.NotNil(t, config)

	assert.Equal(t, "agent-service", config.ServiceName)
	assert.Equal(t, "1.0.0", config.ServiceVersion)
	assert.Equal(t, "development", config.Environment)
	assert.True(t, config.TraceEnabled)
	assert.Equal(t, "otlp", config.TraceExporter)
	assert.True(t, config.MetricsEnabled)
	assert.Equal(t, float64(1.0), config.TraceSampleRate)
}

func TestTelemetryProvider_SamplingRate(t *testing.T) {
	tests := []struct {
		name        string
		sampleRate  float64
		description string
	}{
		{"always sample", 1.0, "should sample all traces"},
		{"half sample", 0.5, "should sample ~50% of traces"},
		{"never sample", 0.0, "should sample no traces"},
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

			// 获取 tracer 并创建 spans
			tracer := provider.GetTracer("test-tracer")
			ctx, span := tracer.Start(context.Background(), "test-operation")
			span.End()
			assert.NotNil(t, ctx)
		})
	}
}

func BenchmarkTelemetryProvider_GetTracer(b *testing.B) {
	config := &TelemetryConfig{
		ServiceName:    "test-service",
		ServiceVersion: "1.0.0",
		Environment:    "test",
		TraceEnabled:   true,
		TraceExporter:  "noop",
		MetricsEnabled: false,
	}

	provider, err := NewTelemetryProvider(config)
	require.NoError(b, err)
	defer provider.Shutdown(context.Background())

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = provider.GetTracer("test-tracer")
	}
}

func BenchmarkTelemetryProvider_CreateSpan(b *testing.B) {
	config := &TelemetryConfig{
		ServiceName:    "test-service",
		ServiceVersion: "1.0.0",
		Environment:    "test",
		TraceEnabled:   true,
		TraceExporter:  "noop",
		MetricsEnabled: false,
	}

	provider, err := NewTelemetryProvider(config)
	require.NoError(b, err)
	defer provider.Shutdown(context.Background())

	tracer := provider.GetTracer("test-tracer")
	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, span := tracer.Start(ctx, "test-operation")
		span.End()
	}
}
