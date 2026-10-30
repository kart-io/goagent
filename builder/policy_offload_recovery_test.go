package builder

import (
	"context"
	"testing"

	"github.com/kart-io/goagent/builder/contextpolicy"
	"github.com/kart-io/goagent/core"
	"github.com/kart-io/goagent/core/durable"
	"github.com/kart-io/goagent/interfaces"
	"github.com/kart-io/goagent/llm"
	"github.com/kart-io/goagent/tools"
	"github.com/stretchr/testify/require"
)

// 复现 Gemini 实测：模型把引用键抄错一位。经 Dispatcher 日志化后仍应交还模型改正，且整轮可重放。
func TestPolicyReadWrongKeyIsReturnedToModel(t *testing.T) {
	file, err := durable.NewFileStore(t.TempDir())
	require.NoError(t, err)
	store, err := contextpolicy.NewDurableResultStore(file)
	require.NoError(t, err)
	ctx := durable.WithJournal(durable.WithExecutionKey(context.Background(), "wrong-key"), file)
	adapter := func(ts []interfaces.Tool) ([]interfaces.Tool, error) {
		reg := tools.NewRegistry()
		for _, tool := range ts {
			if err := reg.Register(tool); err != nil {
				return nil, err
			}
		}
		d, e := tools.NewDispatcher(reg, tools.DispatchConfig{})
		if e != nil {
			return nil, e
		}
		return d.Tools(), nil
	}
	business, err := adapter([]interfaces.Tool{NewMockTool("lookup", "long evidence")})
	require.NoError(t, err)
	model := nativeModel(
		[]*llm.StreamChunk{{Done: true, FinishReason: "tool_calls", ToolCalls: []llm.ToolCall{nativeCall("a", "lookup", "{}")}}},
		[]*llm.StreamChunk{{Done: true, FinishReason: "tool_calls", ToolCalls: []llm.ToolCall{nativeCall("b", contextpolicy.ReadToolName, `{"key":"r9","offset":0}`)}}},
		[]*llm.StreamChunk{{Done: true, FinishReason: "tool_calls", ToolCalls: []llm.ToolCall{nativeCall("c", contextpolicy.ReadToolName, `{"key":"r1","offset":0}`)}}},
		[]*llm.StreamChunk{{Done: true, FinishReason: "stop", Content: "answer"}},
	)
	makeAgent := func() *ConfigurableAgent[any, *core.AgentState] {
		agent, e := NewSimpleBuilder(model).WithDurableScope("policy").WithSystemPrompt("system").WithTools(business...).WithMaxIterations(5).
			WithContextPolicies(func() contextpolicy.Policy {
				p, e := contextpolicy.NewOffload(contextpolicy.OffloadConfig{Store: store, MaxToolResultRunes: 3})
				require.NoError(t, e)
				return p
			}).WithPolicyToolAdapter(adapter).Build()
		require.NoError(t, e)
		return agent
	}
	for range 2 {
		out, e := makeAgent().AsRunnable().Invoke(ctx, "question")
		require.NoError(t, e)
		require.Equal(t, "answer", out.Result)
	}
	require.Len(t, model.requests, 4, "第二次执行全部重放")
	require.Contains(t, model.requests[1].Messages[3].Content, `"context_result":"r1"`)
	require.Contains(t, model.requests[2].Messages[5].Content, "unknown context_result key")
	require.Contains(t, model.requests[3].Messages[7].Content, "long evidence")
}
