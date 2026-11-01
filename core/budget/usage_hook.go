package budget

import (
	"context"
	"fmt"
	"time"

	"github.com/kart-io/goagent/core/usage"
)

// UsageEvent contains one attempt's evidence and an optional cumulative ledger
// snapshot. Statistics may include concurrent attempts: never sum its snapshots.
type UsageEvent struct {
	Model           usage.ModelEvidence
	Statistics      AttributionReport
	HasStatistics   bool
	StatisticsError error
	ErrorKind       string
}

// UsageHook is a read-only notification, including failures and replay. It must
// support concurrent calls and return promptly. Panics cannot fail execution.
type UsageHook func(context.Context, UsageEvent)
type usageHookKey struct{}

// WithUsageHook binds the single inherited usage sink. A child may replace it;
// nil explicitly clears it. It does not replace an Observer or create a ledger.
func WithUsageHook(ctx context.Context, hook UsageHook) context.Context {
	return context.WithValue(ctx, usageHookKey{}, hook)
}

// HasUsageHook reports whether usage notifications have an active sink.
func HasUsageHook(ctx context.Context) bool {
	hook, _ := ctx.Value(usageHookKey{}).(UsageHook)
	return hook != nil
}

// NotifyUsage delivers a copy after accounting, outside ledger locks. The
// statistics query and callback get a bounded context detached from execution
// cancellation so completed usage can still be observed after cancellation.
// Hook/query failures never change the business outcome. Hooks are not durable
// delivery: reconcile projections from Attribution using attempt keys/revisions.
func NotifyUsage(ctx context.Context, model usage.ModelEvidence, errorKind string) {
	hook, _ := ctx.Value(usageHookKey{}).(UsageHook)
	if hook == nil {
		return
	}
	defer func() { _ = recover() }()
	callCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	event := UsageEvent{Model: model, ErrorKind: errorKind}
	if c, ok := FromContext(ctx); ok {
		event.Statistics, event.StatisticsError = usageStatistics(callCtx, c)
		event.HasStatistics = event.StatisticsError == nil
	}
	hook(callCtx, event)
}

func usageStatistics(ctx context.Context, c *Controller) (report AttributionReport, err error) {
	defer func() {
		if recover() != nil {
			report = AttributionReport{}
			err = fmt.Errorf("usage statistics query panicked")
		}
	}()
	return c.Attribution(ctx)
}
