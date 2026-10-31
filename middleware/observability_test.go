package middleware

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"

	coremiddleware "github.com/kart-io/goagent/core/middleware"
	"github.com/kart-io/goagent/observability"
)

// OnBefore 开始的 span 在 OnAfter 中结束。
func TestObservabilityMiddlewareEndsSpan(t *testing.T) {
	provider, err := observability.NewTelemetryProvider(&observability.TelemetryConfig{
		ServiceName: "test", TraceEnabled: true, TraceExporter: "noop", TraceSampleRate: 1,
		MetricsEnabled: true, MetricsExporter: "noop", MetricsInterval: time.Minute,
	})
	require.NoError(t, err)
	defer provider.Shutdown(context.Background())
	mw, err := NewObservabilityMiddleware(provider)
	require.NoError(t, err)

	req, err := mw.OnBefore(context.Background(), &coremiddleware.MiddlewareRequest{Metadata: map[string]interface{}{}})
	require.NoError(t, err)
	span, ok := req.Metadata["_otel_span"].(sdktrace.ReadOnlySpan)
	require.True(t, ok, "%T", req.Metadata["_otel_span"])
	_, err = mw.OnAfter(context.Background(), &coremiddleware.MiddlewareResponse{Metadata: req.Metadata})
	require.NoError(t, err)
	require.False(t, span.EndTime().IsZero(), "span 未结束")
}
