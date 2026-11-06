package builder

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/kart-io/goagent/codec/json"

	"github.com/kart-io/goagent/core"
	"github.com/kart-io/goagent/core/budget"
	"github.com/kart-io/goagent/core/durable"
	"github.com/kart-io/goagent/core/observe"
	"github.com/kart-io/goagent/core/usage"
	"github.com/kart-io/goagent/interfaces"
	"github.com/kart-io/goagent/llm"
	"github.com/kart-io/goagent/tools"
	"github.com/stretchr/testify/require"
)

type agentUsageSink struct {
	mu     sync.Mutex
	events []observe.Event
}

type panicRetryUsageModel struct {
	*nativeEventModel
	calls int
}

func (m *panicRetryUsageModel) CompleteToolsStream(ctx context.Context, req *llm.CompletionRequest, ts []interfaces.Tool) (<-chan *llm.StreamChunk, error) {
	m.calls++
	if m.calls == 2 {
		panic("second attempt panicked")
	}
	return m.nativeEventModel.CompleteToolsStream(ctx, req, ts)
}
func TestAgentUsagePanicRetryDoesNotReusePreviousEvidence(t *testing.T) {
	c, err := budget.New(budget.Config{RootKey: "panic-retry", Version: "v1", MaxModelRequests: 2, MaxAgentCalls: 2})
	require.NoError(t, err)
	m := &panicRetryUsageModel{nativeEventModel: nativeModel([]*llm.StreamChunk{{Usage: &llm.Usage{TotalTokens: 3}, Error: unavailable(503, 0)}})}
	a, err := NewSimpleBuilder(m).WithAgentID("root").WithTools(plainTool()).WithRequestError(1, RetryTransient(1, noWait)).Build()
	require.NoError(t, err)
	_, err = a.AsRunnable().Invoke(budget.WithController(context.Background(), c), "task")
	require.Error(t, err)
	r, err := c.Attribution(context.Background())
	require.NoError(t, err)
	require.Equal(t, 3, r.Root.KnownTokens.TotalTokens, "panic attempt has no evidence, not the prior attempt's usage")
	require.Equal(t, 1, r.UnknownUsageEntries)
	require.Equal(t, 2, r.Root.ReservedRequests)
}

func TestAgentUsageRetryUnknownAndBudgetRejection(t *testing.T) {
	c, err := budget.New(budget.Config{RootKey: "retry", Version: "v1", MaxModelRequests: 2, MaxAgentCalls: 4})
	require.NoError(t, err)
	sink := &agentUsageSink{}
	ctx := observe.WithObserver(budget.WithController(context.Background(), c), sink)
	model := &flakyModel{nativeEventModel: nativeModel(answerTurn("ok", 7)), steps: []error{unavailable(503, 0)}}
	a, err := NewSimpleBuilder(model).WithAgentID("retrying").WithTools(plainTool()).WithRequestError(1, RetryTransient(1, noWait)).Build()
	require.NoError(t, err)
	_, err = a.AsRunnable().Invoke(ctx, "task")
	require.NoError(t, err)
	r, err := c.Attribution(ctx)
	require.NoError(t, err)
	require.Equal(t, 2, r.Root.ReservedRequests)
	require.Equal(t, 7, r.Root.KnownTokens.TotalTokens)
	require.Equal(t, 1, r.UnknownUsageEntries)
	require.False(t, r.Complete)
	var attempts, results, unknown int
	keys := map[string]bool{}
	for _, e := range sink.snapshot() {
		if e.Type == "model_attempt_start" {
			attempts++
			keys[e.Usage.AttemptKey] = true
		}
		if e.Type == "model_usage" {
			results++
			if e.Usage.KnownFields == 0 {
				unknown++
			}
		}
	}
	require.Equal(t, 2, attempts)
	require.Equal(t, 2, results)
	require.Equal(t, 1, unknown)
	require.Len(t, keys, 2)
	_, err = a.AsRunnable().Invoke(ctx, "over budget")
	require.ErrorIs(t, err, budget.ErrBudgetExceeded)
	require.Equal(t, 2, model.calls)
	var newStarts int
	for _, e := range sink.snapshot() {
		if e.Type == "model_attempt_start" {
			newStarts++
		}
	}
	require.Equal(t, 2, newStarts)
}

func TestAgentUsageCachedTokensAndFailureEvidence(t *testing.T) {
	c, err := budget.New(budget.Config{RootKey: "cached", Version: "v1", MaxModelRequests: 1, MaxAgentCalls: 2})
	require.NoError(t, err)
	fields := usage.AllFields
	model := nativeModel([]*llm.StreamChunk{{Usage: &llm.Usage{PromptTokens: 8, CompletionTokens: 2, TotalTokens: 10, CachedTokens: 4, KnownFields: &fields}}, {Error: errors.New("stream failed")}})
	a, err := NewSimpleBuilder(model).WithAgentID("root").WithTools(plainTool()).Build()
	require.NoError(t, err)
	_, err = a.AsRunnable().Invoke(budget.WithController(context.Background(), c), "task")
	require.Error(t, err)
	r, err := c.Attribution(context.Background())
	require.NoError(t, err)
	require.Equal(t, 10, r.Root.KnownTokens.TotalTokens)
	require.Equal(t, 4, r.Root.KnownTokens.CachedTokens)
}

func TestAgentUsageMergesPartialUsageFrames(t *testing.T) {
	c, err := budget.New(budget.Config{RootKey: "frames", Version: "v1", MaxModelRequests: 1, MaxAgentCalls: 2})
	require.NoError(t, err)
	cached, total := usage.Cached, usage.Total
	m := nativeModel([]*llm.StreamChunk{{Usage: &llm.Usage{CachedTokens: 4, KnownFields: &cached}}, {Delta: "ok", Done: true, FinishReason: "stop", Usage: &llm.Usage{TotalTokens: 19, KnownFields: &total}}})
	a, err := NewSimpleBuilder(m).WithAgentID("root").WithTools(plainTool()).Build()
	require.NoError(t, err)
	out, err := a.AsRunnable().Invoke(budget.WithController(context.Background(), c), "task")
	require.NoError(t, err)
	require.Equal(t, 4, out.TokenUsage.CachedTokens)
	r, err := c.Attribution(context.Background())
	require.NoError(t, err)
	require.Equal(t, 19, r.Root.KnownTokens.TotalTokens)
	require.Equal(t, 4, r.Root.KnownTokens.CachedTokens)
	require.Equal(t, 1, r.PartialUsageEntries)
}

type evidenceFailureStore struct {
	durable.Store
	failed bool
}

func hasBudgetEvidence(data json.RawMessage) bool {
	var ledger struct {
		Entries map[string]struct{ Evidence usage.Evidence }
	}
	if json.Unmarshal(data, &ledger) != nil {
		return false
	}
	for _, entry := range ledger.Entries {
		if entry.Evidence.KnownFields != 0 {
			return true
		}
	}
	return false
}

func (s *evidenceFailureStore) Save(ctx context.Context, id string, snapshot *durable.Snapshot) error {
	if strings.HasPrefix(id, "goagent/budget/") && !s.failed {
		if hasBudgetEvidence(snapshot.Data) {
			s.failed = true
			return errors.New("evidence save failed")
		}
	}
	return s.Store.Save(ctx, id, snapshot)
}
func TestAgentUsageKnownModelReplayAfterEvidenceSaveFailure(t *testing.T) {
	file, err := durable.NewFileStore(t.TempDir())
	require.NoError(t, err)
	store := &evidenceFailureStore{Store: file}
	c, err := budget.NewDurable(budget.Config{RootKey: "saved", Version: "v1", MaxModelRequests: 1, MaxAgentCalls: 2}, store)
	require.NoError(t, err)
	ctx := budget.WithController(durable.WithJournal(durable.WithExecutionKey(context.Background(), "saved"), store), c)
	model := nativeModel(answerTurn("ok", 8))
	build := func() *ConfigurableAgent[any, *core.AgentState] {
		a, err := NewSimpleBuilder(model).WithAgentID("root").WithDurableScope("root").WithTools(dispatcherFor(t, plainTool()).Tools()...).Build()
		require.NoError(t, err)
		return a
	}
	// Owner registration + reservation precede result persistence; fail the next
	// budget write (evidence). Saved model result must be reused on retry.
	_, err = build().AsRunnable().Invoke(ctx, "task")
	require.ErrorContains(t, err, "evidence save failed")
	require.True(t, store.failed)
	before, err := c.Snapshot(context.Background())
	require.NoError(t, err)
	require.Equal(t, 1, before.UnknownUsageEntries)
	out, err := build().AsRunnable().Invoke(ctx, "task")
	require.NoError(t, err)
	require.Equal(t, "ok", out.Result)
	after, err := c.Snapshot(context.Background())
	require.NoError(t, err)
	require.Equal(t, 1, after.ReservedRequests)
	require.Equal(t, 8, after.KnownTokens.TotalTokens)
	require.Zero(t, after.UnknownUsageEntries)
	require.Len(t, model.requests, 1)
}

func (s *agentUsageSink) Observe(_ context.Context, e observe.Event) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.events = append(s.events, e)
}
func (s *agentUsageSink) snapshot() []observe.Event {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]observe.Event(nil), s.events...)
}

func TestAgentUsageParentChildAttributionAndHooks(t *testing.T) {
	store, err := durable.NewFileStore(t.TempDir())
	require.NoError(t, err)
	c, err := budget.NewDurable(budget.Config{RootKey: "accounting", Version: "v1", MaxModelRequests: 3, MaxAgentCalls: 4}, store)
	require.NoError(t, err)
	sink := &agentUsageSink{}
	ctx := observe.WithObserver(budget.WithController(durable.WithJournal(durable.WithExecutionKey(context.Background(), "accounting"), store), c), sink)
	childModel := nativeModel(answerTurn("child", 5))
	child, err := NewSimpleBuilder(childModel).WithAgentID("specialist").WithDurableScope("specialist").WithTools(dispatcherFor(t, plainTool()).Tools()...).Build()
	require.NoError(t, err)
	tool, err := tools.NewRunnableTool(child.AsRunnable(), tools.RunnableToolConfig[any, *AgentOutput]{Name: "child", Description: "child", Version: "v1", ModelInvoking: true, ArgsSchema: `{"type":"object"}`, Decode: func(map[string]any) (any, error) { return "task", nil }, Encode: func(out *AgentOutput) (any, error) { return out.Result, nil }})
	require.NoError(t, err)
	parentModel := nativeModel([]*llm.StreamChunk{{Done: true, FinishReason: "tool_calls", ToolCalls: []llm.ToolCall{nativeCall("child-1", "child", "{}")}, Usage: &llm.Usage{TotalTokens: 2}}}, answerTurn("summary", 3))
	build := func(id string) *ConfigurableAgent[any, *core.AgentState] {
		a, err := NewSimpleBuilder(parentModel).WithAgentID(id).WithDurableScope("parent").WithTools(dispatcherFor(t, tool).Tools()...).WithMaxIterations(2).Build()
		require.NoError(t, err)
		return a
	}
	out, err := build("coordinator").AsRunnable().Invoke(ctx, "delegate")
	require.NoError(t, err)
	require.Equal(t, 5, out.TokenUsage.TotalTokens)
	report, err := c.Attribution(ctx)
	require.NoError(t, err)
	require.Equal(t, 10, report.Root.KnownTokens.TotalTokens)
	require.Len(t, report.Calls, 2)
	for _, call := range report.Calls {
		require.Equal(t, 5, call.Self.KnownTokens.TotalTokens)
		if call.AgentID == "coordinator" {
			require.Equal(t, 10, call.Inclusive.KnownTokens.TotalTokens)
		} else {
			require.Equal(t, 5, call.Inclusive.KnownTokens.TotalTokens)
		}
	}
	var parent, sub usage.Invocation
	var starts, notifications int
	for _, e := range sink.snapshot() {
		if e.Type == "agent_start" {
			require.NotNil(t, e.Agent)
			require.Equal(t, "accounting", e.RunID)
			if e.Agent.AgentID == "coordinator" {
				parent = *e.Agent
			} else {
				sub = *e.Agent
			}
		}
		if e.Type == "model_attempt_start" {
			starts++
		}
		if e.Type == "model_usage" {
			notifications++
			require.NotNil(t, e.Usage)
			require.True(t, e.Usage.AccountingCommitted)
			require.NotEmpty(t, e.Usage.AttemptKey)
		}
	}
	require.Equal(t, 3, starts)
	require.Equal(t, 3, notifications)
	require.Equal(t, parent.AgentCallKey, sub.ParentAgentCallKey)
	require.Equal(t, parent.VisitID, sub.ParentVisitID)
	_, err = build("coordinator").AsRunnable().Invoke(ctx, "delegate")
	require.NoError(t, err)
	after, err := c.Attribution(ctx)
	require.NoError(t, err)
	require.Equal(t, report, after)
	require.Len(t, parentModel.requests, 2)
	require.Len(t, childModel.requests, 1)
	_, err = build("changed-role").AsRunnable().Invoke(ctx, "delegate")
	require.Error(t, err)
}

func TestAgentUsageRejectsConflictingRootBeforeModel(t *testing.T) {
	c, err := budget.New(budget.Config{RootKey: "budget-root", Version: "v1", MaxModelRequests: 1})
	require.NoError(t, err)
	model := nativeModel(answerTurn("unused", 1))
	a, err := NewSimpleBuilder(model).WithAgentID("root").WithTools(plainTool()).Build()
	require.NoError(t, err)
	ctx := observe.WithRunID(budget.WithController(context.Background(), c), "other-root")
	_, err = a.AsRunnable().Invoke(ctx, "task")
	require.Error(t, err)
	require.Empty(t, model.requests)
}

func TestAgentUsageSameRoleConcurrentCallsHaveDistinctOwners(t *testing.T) {
	c, err := budget.New(budget.Config{RootKey: "same-role", Version: "v1", MaxModelRequests: 2, MaxAgentCalls: 2})
	require.NoError(t, err)
	sink := &agentUsageSink{}
	ctx := observe.WithObserver(budget.WithController(context.Background(), c), sink)
	errs := make(chan error, 2)
	for range 2 {
		a, err := NewSimpleBuilder(nativeModel(answerTurn("ok", 2))).WithAgentID("same").WithTools(plainTool()).Build()
		require.NoError(t, err)
		go func() { _, err := a.AsRunnable().Invoke(ctx, "task"); errs <- err }()
	}
	for range 2 {
		require.NoError(t, <-errs)
	}
	r, err := c.Attribution(ctx)
	require.NoError(t, err)
	require.Len(t, r.Calls, 2)
	require.Len(t, r.Agents, 1)
	require.Equal(t, 4, r.Agents[0].Self.KnownTokens.TotalTokens)
	require.NotEqual(t, r.Calls[0].AgentCallKey, r.Calls[1].AgentCallKey)
	visits := map[string]bool{}
	for _, e := range sink.snapshot() {
		if e.Type == "agent_start" {
			visits[e.Agent.VisitID] = true
			require.Empty(t, e.Agent.ParentAgentCallKey)
		}
	}
	require.Len(t, visits, 2)
}

func TestAgentUsageDefaultBudgetPersistsPartialEvidence(t *testing.T) {
	c, err := budget.New(budget.Config{RootKey: "default-partial", Version: "v1", MaxModelRequests: 1})
	require.NoError(t, err)
	sink := &agentUsageSink{}
	fields := usage.Prompt
	m := nativeModel([]*llm.StreamChunk{{Delta: "answer", Done: true, FinishReason: "stop", Usage: &llm.Usage{PromptTokens: 10, KnownFields: &fields}}})
	a, err := NewSimpleBuilder(m).WithAgentID("root").WithTools(plainTool()).Build()
	require.NoError(t, err)
	_, err = a.AsRunnable().Invoke(observe.WithObserver(budget.WithController(context.Background(), c), sink), "task")
	require.NoError(t, err)
	snapshot, err := c.Snapshot(context.Background())
	require.NoError(t, err)
	require.Zero(t, snapshot.UnknownUsageEntries)
	require.Equal(t, 1, snapshot.KnownUsageEntries)
	for _, e := range sink.snapshot() {
		if e.Type == "model_usage" {
			require.True(t, e.Usage.AccountingCommitted)
			require.Equal(t, usage.Prompt, e.Usage.KnownFields)
		}
	}
}

func TestAgentUsageInvalidEvidenceStillClosesAttemptObservation(t *testing.T) {
	c, err := budget.New(budget.Config{RootKey: "invalid", Version: "v1", MaxModelRequests: 1, MaxAgentCalls: 2})
	require.NoError(t, err)
	sink := &agentUsageSink{}
	m := nativeModel(answerTurn("answer", -1))
	a, err := NewSimpleBuilder(m).WithAgentID("root").WithTools(plainTool()).Build()
	require.NoError(t, err)
	_, err = a.AsRunnable().Invoke(observe.WithObserver(budget.WithController(context.Background(), c), sink), "task")
	require.Error(t, err)
	var ends int
	for _, e := range sink.snapshot() {
		if e.Type == "model_usage" {
			ends++
			require.Equal(t, "usage_invalid", e.ErrorKind)
			require.Zero(t, e.Usage.KnownFields)
			require.Zero(t, e.Usage.Tokens.TotalTokens)
			require.False(t, e.Usage.AccountingCommitted)
		}
	}
	require.Equal(t, 1, ends)
	r, err := c.Attribution(context.Background())
	require.NoError(t, err)
	require.Equal(t, 1, r.UnknownUsageEntries)
}
