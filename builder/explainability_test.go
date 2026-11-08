package builder

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/kart-io/goagent/codec/json"
	"github.com/kart-io/goagent/constants"
	"github.com/kart-io/goagent/core/budget"
	"github.com/kart-io/goagent/core/durable"
	"github.com/kart-io/goagent/core/session"
	"github.com/kart-io/goagent/core/usage"
	"github.com/kart-io/goagent/llm"
)

func TestExplainabilitySurvivesJournalAndHooks(t *testing.T) {
	store, err := durable.NewFileStore(t.TempDir())
	require.NoError(t, err)
	c, err := budget.NewDurable(budget.Config{RootKey: "details", Version: "policy", MaxModelRequests: 1}, store)
	require.NoError(t, err)
	ctx := budget.WithController(durable.WithJournal(durable.WithExecutionKey(context.Background(), "details"), store), c)
	fields := usage.BasicFields | usage.Reasoning | usage.ToolUsePrompt
	m := &explainabilityModel{nativeModel([]*llm.StreamChunk{{ResponseModel: "actual-model", Usage: &llm.Usage{ReasoningTokens: 6, ToolUsePromptTokens: 3, KnownFields: &fields}}, {Delta: "ok", Done: true, FinishReason: "stop", Usage: &llm.Usage{PromptTokens: 10, CompletionTokens: 2, TotalTokens: 21}}})}
	var events []budget.UsageEvent
	a, err := NewSimpleBuilder(m).WithAgentID("writer").WithDurableScope("writer").WithTools(dispatcherFor(t, plainTool()).Tools()...).WithReasoning(usage.ReasoningConfig{Effort: "low"}).WithUsageHook(func(_ context.Context, e budget.UsageEvent) { events = append(events, e) }).Build()
	require.NoError(t, err)
	for i := 0; i < 2; i++ {
		_, err = a.AsRunnable().Invoke(ctx, "task")
		require.NoError(t, err)
	}
	require.Len(t, events, 2)
	for i, e := range events {
		require.Equal(t, "actual-model", e.Model.ResponseModel)
		require.True(t, e.Model.ReasoningConfigured)
		require.Equal(t, "low", e.Model.Reasoning.Effort)
		require.Equal(t, 6, e.Model.Tokens.ReasoningTokens)
		require.Equal(t, 3, e.Model.Tokens.ToolUsePromptTokens)
		require.Equal(t, i == 1, e.Model.Replayed)
		require.Equal(t, 21, e.Statistics.Root.KnownTokens.TotalTokens)
	}
	require.Len(t, m.requests, 1)
	require.Equal(t, "low", m.requests[0].Reasoning.Effort)
}

type explainabilityModel struct{ *nativeEventModel }

func (m *explainabilityModel) ValidateReasoning(req *llm.CompletionRequest) error {
	return req.Reasoning.Validate()
}

func TestReasoningRequestHookDoesNotMutateDefaults(t *testing.T) {
	m := &explainabilityModel{nativeModel(answerTurn("first", 3), answerTurn("second", 3))}
	var seen []string
	a, err := NewSimpleBuilder(m).WithTools(plainTool()).WithReasoning(usage.ReasoningConfig{Effort: "low"}).WithRequestHook(func(_ context.Context, r *ModelRequest) error {
		seen = append(seen, r.Request.Reasoning.Effort)
		r.Request.Reasoning.Effort = "high"
		return nil
	}).Build()
	require.NoError(t, err)
	for i := 0; i < 2; i++ {
		_, err = a.AsRunnable().Invoke(context.Background(), "task")
		require.NoError(t, err)
	}
	require.Equal(t, []string{"low", "low"}, seen)
}

func TestSessionLogRetainsUsageDetails(t *testing.T) {
	store := session.NewMemoryStore()
	fields := usage.BasicFields | usage.Reasoning | usage.ToolUsePrompt
	m := nativeModel([]*llm.StreamChunk{{Delta: "ok", Done: true, FinishReason: "stop", Usage: &llm.Usage{PromptTokens: 10, CompletionTokens: 2, TotalTokens: 21, ReasoningTokens: 6, ToolUsePromptTokens: 3, KnownFields: &fields}}})
	a, err := NewSimpleBuilder(m).WithSessionID("details").WithSessionLog(store).WithTools(plainTool()).Build()
	require.NoError(t, err)
	_, err = a.AsRunnable().Invoke(context.Background(), "task")
	require.NoError(t, err)
	found := false
	for _, e := range sessionEvents(t, store, "details") {
		if e.Type != constants.SessionEventTurnEnd {
			continue
		}
		var end session.TurnEnd
		require.NoError(t, json.Unmarshal(e.Data, &end))
		require.Equal(t, 6, end.Usage.ReasoningTokens)
		require.Equal(t, 3, end.Usage.ToolUsePromptTokens)
		require.Equal(t, 21, end.Usage.TotalTokens)
		found = true
	}
	require.True(t, found)
}
