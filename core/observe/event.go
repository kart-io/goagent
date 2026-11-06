// Package observe provides optional execution events and existing OTel context propagation.
// Events are observations, never business checkpoints or accounting evidence.
package observe

import (
	"context"
	"errors"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	"github.com/kart-io/goagent/core/durable"
	"github.com/kart-io/goagent/core/usage"
	agentErrors "github.com/kart-io/goagent/errors"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
	"go.opentelemetry.io/otel/trace/noop"
)

// Event deliberately excludes raw inputs, outputs and provider error messages.
type Event struct {
	Agent                                       *usage.Invocation    `json:",omitzero"`
	Usage                                       *usage.ModelEvidence `json:",omitzero"`
	RunID, OperationKey, VisitID, EventID, Type string
	TraceID, SpanID, ParentSpanID               string
	Path, ErrorKind                             string
	At                                          time.Time
	Duration                                    time.Duration
	Replayed                                    bool
	Dropped                                     uint64 `json:",omitzero"`
}

// Observer must support concurrent calls. Custom callbacks must return promptly.
type Observer interface{ Observe(context.Context, Event) }
type observerKey struct{}
type tracerKey struct{}
type scopeKey struct{}
type pathKey struct{}
type scope struct {
	id         string
	invocation string
	seq        atomic.Uint64
}

var noTracer = noop.NewTracerProvider().Tracer("goagent")

// WithObserver binds a host sink. Panics are isolated; blocking custom sinks are not.
func WithObserver(ctx context.Context, o Observer) context.Context {
	return context.WithValue(ctx, observerKey{}, o)
}

// WithTracer injects an existing tracer without configuring a global provider.
func WithTracer(ctx context.Context, t trace.Tracer) context.Context {
	return context.WithValue(ctx, tracerKey{}, t)
}

// TracerFromContext defaults to a noop tracer.
func TracerFromContext(ctx context.Context) trace.Tracer {
	if t, ok := ctx.Value(tracerKey{}).(trace.Tracer); ok && t != nil {
		return t
	}
	return noTracer
}

// WithRunID retains business identity and creates a fresh invocation scope.
// EventID and VisitID are opaque, invocation-unique identities, not operation keys.
func WithRunID(ctx context.Context, id string) context.Context {
	if id == "" {
		id = uuid.NewString()
	}
	return context.WithValue(ctx, scopeKey{}, &scope{id: id, invocation: uuid.NewString()})
}

// Enabled reports whether a sink or tracer is configured.
func Enabled(ctx context.Context) bool {
	return ctx.Value(observerKey{}) != nil || ctx.Value(tracerKey{}) != nil
}
func send(ctx context.Context, e Event) {
	defer func() { _ = recover() }()
	if o, ok := ctx.Value(observerKey{}).(Observer); ok && o != nil {
		o.Observe(ctx, cloneEvent(e))
	}
}

// cloneEvent isolates all mutable metadata pointers from sinks and producers.
func cloneEvent(e Event) Event {
	if e.Agent != nil {
		v := *e.Agent
		e.Agent = &v
	}
	if e.Usage != nil {
		v := *e.Usage
		e.Usage = &v
	}
	return e
}

// Emit delivers a standalone read-only event without creating a lifecycle span.
// Disabled observation performs no allocation. Delivery is best effort.
func Emit(ctx context.Context, e Event) {
	if !Enabled(ctx) {
		return
	}
	sc, _ := ctx.Value(scopeKey{}).(*scope)
	if sc == nil {
		in, _ := usage.FromContext(ctx)
		ctx = WithRunID(ctx, in.RunID)
		sc = ctx.Value(scopeKey{}).(*scope)
	}
	e.RunID = sc.id
	if in, ok := usage.FromContext(ctx); ok {
		e.Agent = &in
	}
	if prefix, _ := ctx.Value(pathKey{}).(string); prefix != "" {
		if e.Path == "" {
			e.Path = prefix
		} else {
			e.Path = prefix + "/" + e.Path
		}
	}
	if e.OperationKey == "" {
		e.OperationKey = durable.ExecutionKey(ctx)
	}
	id := sc.invocation + "/" + strconv.FormatUint(sc.seq.Add(1), 10)
	e.VisitID, e.EventID, e.At = id, id, time.Now()
	if current := trace.SpanContextFromContext(ctx); current.IsValid() {
		e.TraceID = current.TraceID().String()
		e.SpanID = current.SpanID().String()
	}
	send(ctx, e)
}

// ErrorKind returns a bounded category, never the original error text.
// panic 只记类别，panic 值与调用栈不进入 Event 或 span。
func ErrorKind(err error) string {
	var panicErr *agentErrors.PanicError
	switch {
	case err == nil:
		return ""
	case errors.As(err, &panicErr):
		return "panic"
	case errors.Is(err, context.Canceled):
		return "canceled"
	case errors.Is(err, context.DeadlineExceeded):
		return "deadline"
	case errors.Is(err, durable.ErrApprovalRequired) || durable.Waits(err) != nil:
		return "waiting"
	case errors.Is(err, durable.ErrOperationAttention):
		return "attention"
	default:
		return "execution"
	}
}
func disabledEnd(error, bool) {}

// Start propagates the actual OTel child context; end must be called exactly once.
func Start(ctx context.Context, e Event) (context.Context, func(error, bool)) {
	if !Enabled(ctx) {
		return ctx, disabledEnd
	}
	return startEnabled(ctx, e)
}
func startEnabled(ctx context.Context, e Event) (context.Context, func(error, bool)) {
	e = cloneEvent(e)
	if in, ok := usage.FromContext(ctx); ok {
		e.Agent = &in
	}
	sc, _ := ctx.Value(scopeKey{}).(*scope)
	if sc == nil {
		in, _ := usage.FromContext(ctx)
		ctx = WithRunID(ctx, in.RunID)
		sc = ctx.Value(scopeKey{}).(*scope)
	}
	e.RunID = sc.id
	if prefix, _ := ctx.Value(pathKey{}).(string); prefix != "" {
		e.Path = prefix + "/" + e.Path
	}
	ctx = context.WithValue(ctx, pathKey{}, e.Path)
	if e.OperationKey == "" {
		e.OperationKey = durable.ExecutionKey(ctx)
	}
	n := sc.seq.Add(1)
	e.VisitID = sc.invocation + "/" + strconv.FormatUint(n, 10)
	e.At = time.Now()
	kind := e.Type
	parent := trace.SpanContextFromContext(ctx)
	ctx, span := TracerFromContext(ctx).Start(ctx, kind, trace.WithAttributes(attribute.String("goagent.run_id", e.RunID), attribute.String("goagent.path", e.Path)))
	current := span.SpanContext()
	if current.IsValid() {
		e.TraceID = current.TraceID().String()
		e.SpanID = current.SpanID().String()
	}
	if parent.IsValid() {
		e.ParentSpanID = parent.SpanID().String()
	}
	e.Type = kind + "_start"
	e.EventID = sc.invocation + "/" + strconv.FormatUint(n, 10)
	send(ctx, e)
	var once sync.Once
	return ctx, func(err error, replayed bool) {
		once.Do(func() {
			e.Duration = time.Since(e.At)
			e.At = time.Now()
			e.ErrorKind = ErrorKind(err)
			e.Replayed = replayed
			e.Type = kind + "_end"
			if replayed {
				e.Type = kind + "_replayed"
			}
			if err != nil {
				e.Type = kind + "_error"
				span.SetStatus(codes.Error, e.ErrorKind)
				if e.ErrorKind == "waiting" {
					e.Type = kind + "_waiting"
				}
			}
			e.EventID = sc.invocation + "/" + strconv.FormatUint(sc.seq.Add(1), 10)
			send(ctx, e)
			span.End()
		})
	}
}
