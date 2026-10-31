package execution_test

import (
	"context"
	"testing"
	"time"

	"github.com/kart-io/goagent/core/budget"
	"github.com/kart-io/goagent/core/durable"
	"github.com/kart-io/goagent/core/observe"
	"github.com/kart-io/goagent/execution"
	"github.com/kart-io/goagent/tools"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/trace"
	"go.opentelemetry.io/otel/trace/noop"
)

type sink struct{ events []observe.Event }

func (s *sink) Observe(_ context.Context, e observe.Event) { s.events = append(s.events, e) }

type nilStore struct{ durable.Store }
type nilTracer struct{ trace.Tracer }

func TestScopeConfiguration(t *testing.T) {
	var store *nilStore
	var observer *sink
	var tracer *nilTracer
	memory, err := budget.New(budget.Config{RootKey: "run", Version: "v1", MaxModelRequests: 2})
	require.NoError(t, err)
	journal, err := durable.NewFileStore(t.TempDir())
	require.NoError(t, err)
	for _, cfg := range []execution.Config{
		{}, {RunID: " run"}, {RunID: "run "}, {RunID: "run", Timeout: -1},
		{RunID: "other", Budget: memory}, {RunID: "run", Journal: journal, Budget: memory},
		{RunID: "run", Journal: store}, {RunID: "run", Observer: observer}, {RunID: "run", Tracer: tracer},
	} {
		_, err := execution.NewScope(cfg)
		require.Error(t, err, "%+v", cfg)
	}
	s, err := execution.NewScope(execution.Config{RunID: "run"})
	require.NoError(t, err)
	require.Equal(t, "run", s.RunID())
	require.Equal(t, "run", s.SessionID())
}

func TestScopeBindRejectsConflicts(t *testing.T) {
	controller, err := budget.New(budget.Config{RootKey: "run", Version: "v1", MaxModelRequests: 2})
	require.NoError(t, err)
	other, err := budget.New(controller.Configuration())
	require.NoError(t, err)
	store, err := durable.NewFileStore(t.TempDir())
	require.NoError(t, err)
	s, err := execution.NewScope(execution.Config{RunID: "run", SessionID: "session", Budget: controller})
	require.NoError(t, err)
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	for _, ctx := range []context.Context{nil, canceled, durable.WithExecutionKey(context.Background(), "other"), tools.WithSessionID(context.Background(), "other"), observe.WithRunID(context.Background(), "other"), budget.WithController(context.Background(), other), durable.WithJournal(context.Background(), store)} {
		bound, done, err := s.Bind(ctx)
		require.Error(t, err)
		require.Nil(t, bound)
		require.Nil(t, done)
	}
	_, _, err = (&execution.Scope{}).Bind(context.Background())
	require.Error(t, err)
	var empty *execution.Scope
	_, _, err = empty.Bind(context.Background())
	require.Error(t, err)
	ctx := tools.WithSessionID(durable.WithExecutionKey(observe.WithRunID(context.Background(), "run"), "run"), "session")
	ctx = budget.WithController(ctx, controller)
	bound, done, err := s.Bind(ctx)
	require.NoError(t, err)
	defer done()
	require.Equal(t, "run", durable.ExecutionKey(bound))
	require.Equal(t, "session", tools.SessionIDFrom(bound))
	require.Equal(t, "run", observe.RunIDFromContext(bound))
	actual, ok := budget.FromContext(bound)
	require.True(t, ok)
	require.Same(t, controller, actual)
	_, _, err = s.Bind(bound)
	require.Error(t, err)
	second, err := execution.NewScope(execution.Config{RunID: "run", SessionID: "session", Budget: controller})
	require.NoError(t, err)
	_, _, err = second.Bind(bound)
	require.Error(t, err)
}

func TestScopeDeadlineAndObservation(t *testing.T) {
	for _, which := range []string{"parent", "timeout", "budget"} {
		t.Run(which, func(t *testing.T) {
			now := time.Now()
			parentDeadline := now.Add(time.Hour)
			timeout := 2 * time.Hour
			budgetDeadline := now.Add(3 * time.Hour).Truncate(time.Millisecond)
			var expected time.Time
			switch which {
			case "parent":
				expected = parentDeadline
			case "timeout":
				timeout = time.Minute
			case "budget":
				budgetDeadline = now.Add(time.Minute).Truncate(time.Millisecond)
				expected = budgetDeadline
			}
			parent, done := context.WithDeadline(context.Background(), parentDeadline)
			defer done()
			c, err := budget.New(budget.Config{RootKey: "run", Version: "v1", MaxModelRequests: 1, DeadlineUnixMilli: budgetDeadline.UnixMilli()})
			require.NoError(t, err)
			start := time.Now()
			s, err := execution.NewScope(execution.Config{RunID: "run", Budget: c, Timeout: timeout})
			require.NoError(t, err)
			ctx, cancel, err := s.Bind(parent)
			require.NoError(t, err)
			defer cancel()
			deadline, ok := ctx.Deadline()
			require.True(t, ok)
			if which == "timeout" {
				require.WithinDuration(t, start.Add(timeout), deadline, time.Second)
			} else {
				require.Equal(t, expected, deadline)
			}
		})
	}
	inherited, explicit := &sink{}, &sink{}
	inheritedTracer := noop.NewTracerProvider().Tracer("parent")
	explicitTracer := noop.NewTracerProvider().Tracer("explicit")
	parent := observe.WithTracer(observe.WithObserver(context.Background(), inherited), inheritedTracer)
	for _, override := range []bool{false, true} {
		cfg := execution.Config{RunID: "run"}
		if override {
			cfg.Observer = explicit
			cfg.Tracer = explicitTracer
		}
		s, err := execution.NewScope(cfg)
		require.NoError(t, err)
		ctx, cancel, err := s.Bind(parent)
		require.NoError(t, err)
		_, end := observe.Start(ctx, observe.Event{Type: "test"})
		end(nil, false)
		cancel()
		if override {
			require.Equal(t, explicitTracer, observe.TracerFromContext(ctx))
			require.Len(t, explicit.events, 2)
		} else {
			require.Equal(t, inheritedTracer, observe.TracerFromContext(ctx))
		}
	}
	require.Len(t, inherited.events, 2)
}

// Store 的动态值可以不可比较；Scope 不比较宿主 Store 的身份。
type uncomparableStore struct {
	durable.Store
	values []string
}

func TestScopeJournalAndPersistentBudget(t *testing.T) {
	store, err := durable.NewFileStore(t.TempDir())
	require.NoError(t, err)
	controller, err := budget.NewDurable(budget.Config{RootKey: "run", Version: "v1", MaxModelRequests: 1}, store)
	require.NoError(t, err)
	for _, withJournal := range []bool{false, true} {
		config := execution.Config{RunID: "run", Budget: controller}
		if withJournal {
			config.Journal = store
		}
		scope, err := execution.NewScope(config)
		require.NoError(t, err)
		ctx, cancel, err := scope.Bind(context.Background())
		require.NoError(t, err)
		if withJournal {
			require.Same(t, store, durable.JournalStore(ctx))
		} else {
			require.Nil(t, durable.JournalStore(ctx))
		}
		cancel()
		require.ErrorIs(t, ctx.Err(), context.Canceled)
		var typedNil *nilStore
		for _, prebound := range []durable.Store{store, typedNil, uncomparableStore{Store: store, values: []string{"a"}}} {
			_, _, err := scope.Bind(durable.WithJournal(context.Background(), prebound))
			require.Error(t, err)
		}
	}
	// 预算已过期时，Bind 保留 context 的截止错误且不返回可运行 context。
	expired, err := budget.New(budget.Config{RootKey: "run", Version: "v1", MaxModelRequests: 1, DeadlineUnixMilli: time.Now().Add(-time.Second).UnixMilli()})
	require.NoError(t, err)
	scope, err := execution.NewScope(execution.Config{RunID: "run", Budget: expired})
	require.NoError(t, err)
	ctx, cancel, err := scope.Bind(context.Background())
	require.ErrorIs(t, err, context.DeadlineExceeded)
	require.Nil(t, ctx)
	require.Nil(t, cancel)
	canceled, stop := context.WithCancel(context.Background())
	stop()
	_, _, err = scope.Bind(canceled)
	require.ErrorIs(t, err, context.Canceled)
}

func TestScopeSessionIsolationAndFreshBindings(t *testing.T) {
	for _, id := range []string{"one", "two"} {
		scope, err := execution.NewScope(execution.Config{RunID: id})
		require.NoError(t, err)
		for i := 0; i < 2; i++ {
			ctx, cancel, err := scope.Bind(context.Background())
			require.NoError(t, err)
			require.Equal(t, id, tools.SessionIDFrom(ctx))
			require.Equal(t, id, observe.RunIDFromContext(ctx))
			require.Equal(t, id, durable.ExecutionKey(ctx))
			_, ok := budget.FromContext(ctx)
			require.False(t, ok)
			cancel()
		}
	}
}
