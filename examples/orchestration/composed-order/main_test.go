package main

import (
	"context"
	"fmt"
	"github.com/kart-io/goagent/core/observe"
	"github.com/stretchr/testify/require"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"
	"os"
	"os/exec"
	"testing"
)

func TestComposedOrderReplay(t *testing.T) {
	dir := t.TempDir()
	first, err := run(context.Background(), dir)
	require.NoError(t, err)
	require.Equal(t, 4, first.NewCalls)
	require.Equal(t, 4, first.Budget.ReservedRequests)
	require.Equal(t, 8, first.Budget.KnownTokens.TotalTokens)
	require.Equal(t, 1, first.Created)
	require.Equal(t, 1, first.Closed)
	require.Contains(t, first.Answer, "批准")
	second, err := run(context.Background(), dir)
	require.NoError(t, err)
	require.Zero(t, second.NewCalls)
	require.Zero(t, second.Created)
	require.Equal(t, first.Budget, second.Budget)
}

func TestComposedOrderProcessRecovery(t *testing.T) {
	if dir := os.Getenv("GOAGENT_ORDER_CHILD"); dir != "" {
		out, err := run(context.Background(), dir)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(2)
		}
		fmt.Printf("\nNEW_CALLS=%d RESERVED=%d TOKENS=%d\n", out.NewCalls, out.Budget.ReservedRequests, out.Budget.KnownTokens.TotalTokens)
		os.Exit(86)
	}
	dir := t.TempDir()
	child := func() string {
		cmd := exec.Command(os.Args[0], "-test.run=^TestComposedOrderProcessRecovery$")
		cmd.Env = append(os.Environ(), "GOAGENT_ORDER_CHILD="+dir)
		b, err := cmd.CombinedOutput()
		var exit *exec.ExitError
		require.ErrorAs(t, err, &exit)
		require.Equal(t, 86, exit.ExitCode(), string(b))
		return string(b)
	}
	require.Contains(t, child(), "NEW_CALLS=4 RESERVED=4 TOKENS=8")
	require.Contains(t, child(), "NEW_CALLS=0 RESERVED=4 TOKENS=8")
}

func TestComposedOrderUsesExistingTrace(t *testing.T) {
	exporter := tracetest.NewInMemoryExporter()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSyncer(exporter))
	t.Cleanup(func() { require.NoError(t, provider.Shutdown(context.Background())) })
	tracer := provider.Tracer("order-test")
	ctx, parent := tracer.Start(context.Background(), "host")
	ctx = observe.WithTracer(ctx, tracer)
	_, err := run(ctx, t.TempDir())
	require.NoError(t, err)
	parent.End()
	seen := map[string]bool{}
	ids := map[trace.SpanID]bool{parent.SpanContext().SpanID(): true}
	for _, span := range exporter.GetSpans() {
		require.Equal(t, parent.SpanContext().TraceID(), span.SpanContext.TraceID())
		ids[span.SpanContext.SpanID()] = true
		seen[span.Name] = true
	}
	for _, name := range []string{"graph", "node", "agent", "tool", "model"} {
		require.True(t, seen[name], name)
	}
	for _, span := range exporter.GetSpans() {
		if span.Name != "host" {
			require.True(t, ids[span.Parent.SpanID()], span.Name+" lost parent")
		}
	}
}
