package observe

import (
	"context"
	"github.com/kart-io/goagent/core/usage"
	"github.com/kart-io/goagent/interfaces"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"testing"
)

type mutatingObserver struct{ seen []Event }

func (m *mutatingObserver) Observe(_ context.Context, e Event) {
	m.seen = append(m.seen, cloneEvent(e))
	if e.Agent != nil {
		e.Agent.AgentID = "changed"
	}
	if e.Usage != nil {
		e.Usage.Tokens.TotalTokens = 999
	}
}
func TestMetadataIsolationAndEmit(t *testing.T) {
	m := &mutatingObserver{}
	in := usage.Invocation{Owner: usage.Owner{RunID: "r", AgentID: "agent", AgentCallKey: "call"}, VisitID: "agent-visit"}
	ctx := usage.WithInvocation(WithObserver(WithRunID(context.Background(), "r"), m), in)
	evidence := usage.ModelEvidence{Evidence: usage.Evidence{Tokens: interfaces.TokenUsage{TotalTokens: 7}}}
	ctx, end := Start(ctx, Event{Type: "agent", Path: "root", Usage: &evidence})
	Emit(ctx, Event{Type: "model_usage", Path: "attempt", Usage: &evidence})
	end(nil, false)
	require.Len(t, m.seen, 3)
	for _, e := range m.seen {
		require.Equal(t, "agent", e.Agent.AgentID)
		require.Equal(t, 7, e.Usage.Tokens.TotalTokens)
		require.Equal(t, "r", e.RunID)
		require.NotEmpty(t, e.EventID)
	}
	require.Equal(t, "root/attempt", m.seen[1].Path)
	require.Equal(t, 7, evidence.Tokens.TotalTokens)
	require.NotEqual(t, m.seen[0].EventID, m.seen[1].EventID)
}
func TestEmitDisabledNoAllocation(t *testing.T) {
	ctx := context.Background()
	require.Zero(t, testing.AllocsPerRun(100, func() { Emit(ctx, Event{Type: "model_usage"}) }))
}

func TestAsyncQueueCopiesMetadata(t *testing.T) {
	entered, release := make(chan struct{}), make(chan struct{})
	sink := &metadataQueueSink{entered: entered, release: release}
	async, err := NewAsyncObserver(sink, 2, 1)
	require.NoError(t, err)
	async.Observe(context.Background(), Event{Type: "block"})
	<-entered
	agent := usage.Invocation{Owner: usage.Owner{AgentID: "original"}}
	evidence := usage.ModelEvidence{Evidence: usage.Evidence{Tokens: interfaces.TokenUsage{TotalTokens: 7}}}
	async.Observe(context.Background(), Event{Type: "queued", Agent: &agent, Usage: &evidence})
	agent.AgentID = "changed"
	evidence.Tokens.TotalTokens = 999
	close(release)
	require.NoError(t, async.Close(context.Background()))
	require.Equal(t, "original", sink.event.Agent.AgentID)
	require.Equal(t, 7, sink.event.Usage.Tokens.TotalTokens)
}

type metadataQueueSink struct {
	entered, release chan struct{}
	event            Event
}

func (s *metadataQueueSink) Observe(_ context.Context, e Event) {
	if e.Type == "block" {
		close(s.entered)
		<-s.release
		return
	}
	s.event = e
}

func TestEmitIsolatesPanic(t *testing.T) {
	Emit(WithObserver(context.Background(), panicObserver{}), Event{Type: "model_usage"})
}

func TestObservationUsesInvocationRunWithoutExistingScope(t *testing.T) {
	sink := &collector{}
	ctx := usage.WithInvocation(WithObserver(context.Background(), sink), usage.Invocation{Owner: usage.Owner{RunID: "agent-root"}})
	Emit(ctx, Event{Type: "model_usage"})
	_, end := Start(ctx, Event{Type: "agent"})
	end(nil, false)
	for _, e := range sink.events {
		require.Equal(t, "agent-root", e.RunID)
	}
}

func TestEmitRetainsCurrentTrace(t *testing.T) {
	exporter := tracetest.NewInMemoryExporter()
	provider := trace.NewTracerProvider(trace.WithSyncer(exporter))
	defer provider.Shutdown(context.Background())
	sink := &collector{}
	ctx, end := Start(WithObserver(WithTracer(context.Background(), provider.Tracer("test")), sink), Event{Type: "agent", Path: "root"})
	Emit(ctx, Event{Type: "model_attempt_start", Path: "model"})
	end(nil, false)
	require.Len(t, sink.events, 3)
	require.NotEmpty(t, sink.events[1].TraceID)
	require.Equal(t, sink.events[0].TraceID, sink.events[1].TraceID)
	require.Equal(t, sink.events[0].SpanID, sink.events[1].SpanID)
	require.Len(t, exporter.GetSpans(), 1)
}
