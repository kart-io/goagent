package builder

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/kart-io/goagent/core/budget"
	"github.com/kart-io/goagent/core/durable"
	"github.com/kart-io/goagent/core/observe"
	"github.com/kart-io/goagent/core/usage"
	"github.com/kart-io/goagent/llm"
	"github.com/kart-io/goagent/tools"
	"github.com/stretchr/testify/require"
)

func TestUsageHookWithoutObserverOrBudget(t *testing.T) {
	var events []budget.UsageEvent
	model := nativeModel(answerTurn("ok", 7))
	a, err := NewSimpleBuilder(model).WithAgentID("solo").WithTools(plainTool()).WithUsageHook(func(_ context.Context, e budget.UsageEvent) { events = append(events, e); panic("metrics panic") }).Build()
	require.NoError(t, err)
	out, err := a.AsRunnable().Invoke(context.Background(), "task")
	require.NoError(t, err)
	require.Equal(t, "ok", out.Result)
	require.Len(t, events, 1)
	require.Equal(t, 7, events[0].Model.Tokens.TotalTokens)
	require.Equal(t, "solo", events[0].Model.AgentID)
	require.NotEmpty(t, events[0].Model.AttemptKey)
	require.NotEmpty(t, events[0].Model.RunID)
	require.False(t, events[0].HasStatistics)
	require.False(t, events[0].Model.AccountingCommitted)
}

func TestUsageHookRejectsLegacySelectionWithoutAgentIDOrBudget(t *testing.T) {
	m := &eventModel{MockLLMClient: NewMockLLMClient()}
	called := false
	a, err := NewSimpleBuilder(m).WithTools(NewMockTool("lookup", "value")).WithUsageHook(func(context.Context, budget.UsageEvent) { called = true }).Build()
	require.NoError(t, err)
	_, err = a.AsRunnable().Invoke(context.Background(), "task")
	require.ErrorContains(t, err, "ToolStreamClient")
	require.Zero(t, m.selections)
	require.Nil(t, m.request)
	require.False(t, called)
}

type hookEvidenceLoadFailure struct {
	durable.Store
	fail bool
}

func (s *hookEvidenceLoadFailure) Save(ctx context.Context, id string, snapshot *durable.Snapshot) error {
	if err := s.Store.Save(ctx, id, snapshot); err != nil {
		return err
	}
	if strings.HasPrefix(id, "goagent/operation/") {
		var record durable.OperationRecord
		if json.Unmarshal(snapshot.Data, &record) == nil && strings.HasPrefix(record.ID, "model/") && record.Status == "completed" {
			s.fail = true
		}
	}
	return nil
}
func (s *hookEvidenceLoadFailure) Load(ctx context.Context, id string) (*durable.Snapshot, error) {
	if s.fail && strings.HasPrefix(id, "goagent/budget/") {
		s.fail = false
		return nil, errors.New("ledger lookup failed")
	}
	return s.Store.Load(ctx, id)
}
func TestUsageHookKeepsKnownEvidenceWhenAccountingLoadFails(t *testing.T) {
	file, err := durable.NewFileStore(t.TempDir())
	require.NoError(t, err)
	store := &hookEvidenceLoadFailure{Store: file}
	c, err := budget.NewDurable(budget.Config{RootKey: "hook-failure", Version: "policy", MaxModelRequests: 1}, store)
	require.NoError(t, err)
	ctx := budget.WithController(durable.WithJournal(durable.WithExecutionKey(context.Background(), "hook-failure"), store), c)
	var event budget.UsageEvent
	called := 0
	m := nativeModel(answerTurn("answer", 7))
	a, err := NewSimpleBuilder(m).WithDurableScope("root").WithTools(dispatcherFor(t, plainTool()).Tools()...).WithUsageHook(func(_ context.Context, e budget.UsageEvent) { called++; event = e }).Build()
	require.NoError(t, err)
	_, err = a.AsRunnable().Invoke(ctx, "task")
	require.ErrorContains(t, err, "ledger lookup failed")
	require.Equal(t, 1, called)
	require.Equal(t, 7, event.Model.Tokens.TotalTokens)
	require.Equal(t, usage.BasicFields, event.Model.KnownFields)
	require.False(t, event.Model.AccountingCommitted)
	require.True(t, event.HasStatistics)
	require.Equal(t, 1, event.Statistics.UnknownUsageEntries)
	require.Len(t, m.requests, 1)
}

func TestUsageHookCoexistsWithObserverAndCannotModifyEvidence(t *testing.T) {
	c, err := budget.New(budget.Config{RootKey: "coexist", Version: "policy", MaxModelRequests: 1})
	require.NoError(t, err)
	sink := &agentUsageSink{}
	ctx := observe.WithObserver(budget.WithController(context.Background(), c), sink)
	a, err := NewSimpleBuilder(nativeModel(answerTurn("ok", 7))).WithTools(plainTool()).WithUsageHook(func(_ context.Context, e budget.UsageEvent) {
		e.Model.Tokens.TotalTokens = 999
		e.Statistics.Calls[0].Self.KnownTokens.TotalTokens = 999
	}).Build()
	require.NoError(t, err)
	out, err := a.AsRunnable().Invoke(ctx, "task")
	require.NoError(t, err)
	require.Equal(t, 7, out.TokenUsage.TotalTokens)
	seen := false
	for _, e := range sink.snapshot() {
		if e.Type == "model_usage" {
			seen = true
			require.Equal(t, 7, e.Usage.Tokens.TotalTokens)
		}
	}
	require.True(t, seen)
	report, err := c.Attribution(ctx)
	require.NoError(t, err)
	require.Equal(t, 7, report.Calls[0].Self.KnownTokens.TotalTokens)
}

func TestUsageHookInheritedChildrenTotalsAndReplay(t *testing.T) {
	for _, override := range []bool{false, true} {
		t.Run(map[bool]string{false: "inherited", true: "child-replaces"}[override], func(t *testing.T) {
			store, err := durable.NewFileStore(t.TempDir())
			require.NoError(t, err)
			c, err := budget.NewDurable(budget.Config{RootKey: "usage-hook", Version: "policy", MaxModelRequests: 3}, store)
			require.NoError(t, err)
			ctx := budget.WithController(durable.WithJournal(durable.WithExecutionKey(context.Background(), "usage-hook"), store), c)
			var parentEvents, childEvents []budget.UsageEvent
			childModel := nativeModel(answerTurn("child", 5))
			childBuilder := NewSimpleBuilder(childModel).WithAgentID("child").WithDurableScope("child").WithTools(dispatcherFor(t, plainTool()).Tools()...)
			if override {
				childBuilder.WithUsageHook(func(_ context.Context, e budget.UsageEvent) { childEvents = append(childEvents, e) })
			}
			child, err := childBuilder.Build()
			require.NoError(t, err)
			childTool, err := tools.NewRunnableTool(child.AsRunnable(), tools.RunnableToolConfig[any, *AgentOutput]{Name: "child", Description: "child", Version: "policy", ModelInvoking: true, ArgsSchema: `{"type":"object"}`, Decode: func(map[string]any) (any, error) { return "task", nil }, Encode: func(out *AgentOutput) (any, error) { return out.Result, nil }})
			require.NoError(t, err)
			parentModel := nativeModel([]*llm.StreamChunk{{Done: true, FinishReason: "tool_calls", ToolCalls: []llm.ToolCall{nativeCall("c", "child", "{}")}, Usage: &llm.Usage{TotalTokens: 2}}}, answerTurn("summary", 3))
			parent, err := NewSimpleBuilder(parentModel).WithAgentID("parent").WithDurableScope("parent").WithTools(dispatcherFor(t, childTool).Tools()...).WithUsageHook(func(_ context.Context, e budget.UsageEvent) { parentEvents = append(parentEvents, e) }).WithMaxIterations(2).Build()
			require.NoError(t, err)
			_, err = parent.AsRunnable().Invoke(ctx, "delegate")
			require.NoError(t, err)
			if override {
				require.Len(t, parentEvents, 2)
				require.Len(t, childEvents, 1)
			} else {
				require.Len(t, parentEvents, 3)
				require.Empty(t, childEvents)
				require.Equal(t, "child", parentEvents[1].Model.AgentID)
			}
			last := parentEvents[len(parentEvents)-1]
			require.True(t, last.HasStatistics)
			require.NoError(t, last.StatisticsError)
			require.Equal(t, 10, last.Statistics.Root.KnownTokens.TotalTokens)
			require.Len(t, last.Statistics.Calls, 2)
			for _, call := range last.Statistics.Calls {
				require.Equal(t, 5, call.Self.KnownTokens.TotalTokens)
				if call.AgentID == "parent" {
					require.Equal(t, 10, call.Inclusive.KnownTokens.TotalTokens)
				}
			}
			before := len(parentEvents)
			_, err = parent.AsRunnable().Invoke(ctx, "delegate")
			require.NoError(t, err)
			require.Len(t, parentEvents, before+2)
			for _, e := range parentEvents[before:] {
				require.True(t, e.Model.Replayed)
				require.Equal(t, 10, e.Statistics.Root.KnownTokens.TotalTokens)
			}
			require.Len(t, parentModel.requests, 2)
			require.Len(t, childModel.requests, 1)
		})
	}
}

func TestUsageHookReceivesFailedKnownAndInvalidEvidence(t *testing.T) {
	for _, invalid := range []bool{false, true} {
		t.Run(map[bool]string{false: "known-failure", true: "invalid"}[invalid], func(t *testing.T) {
			c, err := budget.New(budget.Config{RootKey: "failure", Version: "policy", MaxModelRequests: 1})
			require.NoError(t, err)
			n := 4
			if invalid {
				n = -1
			}
			model := nativeModel([]*llm.StreamChunk{{Usage: &llm.Usage{TotalTokens: n}, Error: errors.New("failed stream")}})
			var event budget.UsageEvent
			called := 0
			ctx := budget.WithUsageHook(budget.WithController(context.Background(), c), func(_ context.Context, e budget.UsageEvent) { called++; event = e })
			a, err := NewSimpleBuilder(model).WithTools(plainTool()).Build()
			require.NoError(t, err)
			_, err = a.AsRunnable().Invoke(ctx, "task")
			require.Error(t, err)
			require.Equal(t, 1, called)
			require.True(t, event.HasStatistics)
			if invalid {
				require.Equal(t, "usage_invalid", event.ErrorKind)
				require.Zero(t, event.Model.KnownFields)
				require.Equal(t, 1, event.Statistics.UnknownUsageEntries)
			} else {
				require.Equal(t, 4, event.Model.Tokens.TotalTokens)
				require.Equal(t, 4, event.Statistics.Root.KnownTokens.TotalTokens)
				require.Equal(t, usage.BasicFields, event.Model.KnownFields)
			}
		})
	}
}
