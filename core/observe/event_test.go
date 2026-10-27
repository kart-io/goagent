package observe

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

type collector struct{ events []Event }

func (c *collector) Observe(_ context.Context, e Event) { c.events = append(c.events, e) }
func TestExecutionUsesExistingOTelParent(t *testing.T) {
	exporter := tracetest.NewInMemoryExporter()
	provider := trace.NewTracerProvider(trace.WithSyncer(exporter))
	defer provider.Shutdown(context.Background())
	sink := &collector{}
	ctx := WithObserver(WithTracer(context.Background(), provider.Tracer("test")), sink)
	ctx = WithRunID(ctx, "business-run")
	parent, end := Start(ctx, Event{Path: "node", Type: "node", OperationKey: "node-key"})
	child, childEnd := Start(parent, Event{Path: "model", Type: "model", OperationKey: "turn-key"})
	_ = child
	childEnd(nil, true)
	end(errors.New("secret-provider-error"), false)
	spans := exporter.GetSpans()
	require.Len(t, spans, 2)
	require.Equal(t, spans[1].SpanContext.SpanID(), spans[0].Parent.SpanID())
	require.Equal(t, spans[1].SpanContext.TraceID(), spans[0].SpanContext.TraceID())
	require.Len(t, sink.events, 4)
	require.Equal(t, "business-run", sink.events[0].RunID)
	require.True(t, sink.events[2].Replayed)
	require.Equal(t, "execution", sink.events[3].ErrorKind)
	require.NotContains(t, sink.events[3].ErrorKind, "secret")
}

type panicObserver struct{}

func (panicObserver) Observe(context.Context, Event) { panic("observer failure") }
func TestObserverFailureDoesNotChangeRun(t *testing.T) {
	ctx, end := Start(WithObserver(context.Background(), panicObserver{}), Event{Type: "node"})
	require.NotNil(t, ctx)
	require.NotPanics(t, func() { end(nil, false) })
}

func TestDisabledObservationDoesNotAllocate(t *testing.T) {
	allocations := testing.AllocsPerRun(100, func() { _, end := Start(context.Background(), Event{Type: "node", Path: "public"}); end(nil, false) })
	if allocations != 0 {
		t.Fatalf("disabled observation allocated %v times", allocations)
	}
}
