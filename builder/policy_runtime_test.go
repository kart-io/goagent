package builder

import (
	"context"
	"github.com/kart-io/goagent/builder/contextpolicy"
	"github.com/kart-io/goagent/core"
	"github.com/kart-io/goagent/core/durable"
	"github.com/kart-io/goagent/interfaces"
	"github.com/kart-io/goagent/llm"
	"github.com/kart-io/goagent/tools"
	"github.com/stretchr/testify/require"
	"testing"
)

type testPolicy struct {
	change func(contextpolicy.Turn) contextpolicy.Turn
}

func (p *testPolicy) Descriptor() contextpolicy.Descriptor {
	return contextpolicy.Descriptor{ID: "test", Version: "v1", ConfigHash: "hash", Durable: true}
}
func (p *testPolicy) BeforeTurn(_ context.Context, t contextpolicy.Turn) (contextpolicy.Turn, error) {
	return p.change(t), nil
}
func TestPolicyLifecycleAndProtocol(t *testing.T) {
	for _, change := range []func(contextpolicy.Turn) contextpolicy.Turn{
		func(t contextpolicy.Turn) contextpolicy.Turn { t.RemainingCalls++; return t },
		func(t contextpolicy.Turn) contextpolicy.Turn { t.Messages[0].Content = "overwrite system"; return t },
		func(t contextpolicy.Turn) contextpolicy.Turn {
			t.Messages[1].ToolCalls[0].ID = "overwrite nested"
			return t
		},
	} {
		a, err := NewSimpleBuilder(nativeModel()).WithTools(NewMockTool("lookup", "x")).WithContextPolicies(func() contextpolicy.Policy { return &testPolicy{change} }).Build()
		require.NoError(t, err)
		runtime, err := a.newPolicyRuntime(context.Background())
		require.NoError(t, err)
		messages := []llm.Message{llm.SystemMessage("system"), {Role: "assistant", ToolCalls: []llm.ToolCall{nativeCall("id", "lookup", "{}")}}}
		_, err = runtime.apply(context.Background(), contextpolicy.Turn{Messages: messages, RemainingCalls: 2}, false)
		require.Error(t, err)
		require.Equal(t, "id", messages[1].ToolCalls[0].ID)
		require.Equal(t, "system", messages[0].Content)
	}
	_, err := NewSimpleBuilder(nativeModel()).WithTools(NewMockTool("lookup", "x")).WithContextPolicy(ContextPolicy{}).WithContextPolicies(func() contextpolicy.Policy { return &testPolicy{} }).Build()
	require.Error(t, err)
}

func TestPolicyReadToolAuthorizationAndReplay(t *testing.T) {
	file, err := durable.NewFileStore(t.TempDir())
	require.NoError(t, err)
	store, err := contextpolicy.NewDurableResultStore(file)
	require.NoError(t, err)
	ctx := durable.WithJournal(durable.WithExecutionKey(context.Background(), "root-node"), file)
	factories := []contextpolicy.Factory{func() contextpolicy.Policy {
		p, e := contextpolicy.NewOffload(contextpolicy.OffloadConfig{Store: store, MaxToolResultRunes: 3})
		require.NoError(t, e)
		return p
	}}
	a, err := NewSimpleBuilder(nativeModel()).WithTools(NewMockTool("lookup", "long evidence")).WithContextPolicies(factories...).Build()
	require.NoError(t, err)
	_, err = a.newPolicyRuntime(ctx)
	require.ErrorContains(t, err, "adapter")
	a.config.PolicyToolAdapter = func(ts []interfaces.Tool) ([]interfaces.Tool, error) { return ts, nil }
	_, err = a.newPolicyRuntime(ctx)
	require.ErrorContains(t, err, "unjournaled")
	adapter := func(ts []interfaces.Tool) ([]interfaces.Tool, error) {
		r := tools.NewRegistry()
		for _, tool := range ts {
			if err := r.Register(tool); err != nil {
				return nil, err
			}
		}
		d, e := tools.NewDispatcher(r, tools.DispatchConfig{})
		if e != nil {
			return nil, e
		}
		return d.Tools(), nil
	}
	business, err := adapter([]interfaces.Tool{NewMockTool("lookup", "long evidence")})
	require.NoError(t, err)
	model := nativeModel([]*llm.StreamChunk{{Done: true, FinishReason: "tool_calls", ToolCalls: []llm.ToolCall{nativeCall("a", "lookup", "{}")}}}, []*llm.StreamChunk{{Done: true, FinishReason: "stop", Content: "answer"}})
	makeAgent := func() *ConfigurableAgent[any, *core.AgentState] {
		agent, e := NewSimpleBuilder(model).WithSystemPrompt("system").WithTools(business...).WithMaxIterations(3).WithContextPolicies(factories...).WithPolicyToolAdapter(adapter).Build()
		require.NoError(t, e)
		return agent
	}
	for range 2 {
		out, e := makeAgent().AsRunnable().Invoke(ctx, "question")
		require.NoError(t, e)
		require.Equal(t, "answer", out.Result)
	}
	require.Len(t, model.requests, 2)
	require.Contains(t, model.requests[1].Messages[3].Content, "context_result")
	// Last local turn restores the raw evidence and removes reader tools.
	p, e := makeAgent().newPolicyRuntime(ctx)
	require.NoError(t, e)
	scope := policyContext(ctx, p)
	raw := append([]llm.Message(nil), model.requests[1].Messages...)
	raw[3].Content = "long evidence"
	turn, e := p.apply(scope, contextpolicy.Turn{Messages: raw}, false)
	require.NoError(t, e)
	turn, e = p.apply(scope, turn, true)
	require.NoError(t, e)
	require.Equal(t, "long evidence", turn.Messages[3].Content)
}
