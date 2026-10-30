package builder

import (
	"context"
	"testing"

	"github.com/kart-io/goagent/interfaces"
	"github.com/kart-io/goagent/llm"
	"github.com/kart-io/goagent/tools"
	"github.com/stretchr/testify/require"
)

func pipelineTools(t *testing.T, p *tools.Pipeline) []interfaces.Tool {
	t.Helper()
	reg := tools.NewRegistry()
	require.NoError(t, reg.Register(tools.NewBaseTool("lookup", "test", `{"type":"object"}`, func(context.Context, *interfaces.ToolInput) (*interfaces.ToolOutput, error) {
		return &interfaces.ToolOutput{Success: true, Result: "found"}, nil
	})))
	d, err := tools.NewDispatcher(reg, tools.DispatchConfig{Pipeline: p})
	require.NoError(t, err)
	return d.Tools()
}

// 调用后钩子追加的上下文在工具结果之后作为 user 消息进入下一次模型请求；审批调用携带 Agent 的会话 ID。
func TestPipelineAdditionalContextAndSession(t *testing.T) {
	var session string
	p := &tools.Pipeline{
		Pre: []tools.PreHook{func(_ context.Context, c *tools.Call) (tools.PreDecision, error) {
			session = c.SessionID
			return tools.PreDecision{}, nil
		}},
		Post: []tools.PostHook{func(context.Context, *tools.Call, *interfaces.ToolOutput) (tools.PostDecision, error) {
			return tools.PostDecision{AdditionalContext: []string{"lint passed", "2 files changed"}}, nil
		}},
	}
	model := answerModel(nativeCall("c1", "lookup", "{}"))
	agent, err := NewSimpleBuilder(model).WithSessionID("sess-1").WithTools(pipelineTools(t, p)...).WithMaxIterations(3).Build()
	require.NoError(t, err)
	out, err := agent.AsRunnable().Invoke(context.Background(), "question")
	require.NoError(t, err)
	require.Equal(t, "答", out.Result)
	require.Equal(t, "sess-1", session)

	second := model.requests[1].Messages
	tool, injected := second[len(second)-2], second[len(second)-1]
	require.Equal(t, "tool", tool.Role)
	require.Equal(t, "found", tool.Content)
	require.Equal(t, llm.Message{Role: "user", Content: "[工具附加上下文]\n- lint passed\n- 2 files changed"}, injected)
}

// PreStop 终止整个运行，不再请求模型。
func TestPipelineStopAbortsRun(t *testing.T) {
	p := &tools.Pipeline{Pre: []tools.PreHook{func(context.Context, *tools.Call) (tools.PreDecision, error) {
		return tools.PreDecision{Kind: tools.PreStop, Feedback: "budget exhausted"}, nil
	}}}
	model := answerModel(nativeCall("c1", "lookup", "{}"))
	agent, err := NewSimpleBuilder(model).WithTools(pipelineTools(t, p)...).WithMaxIterations(3).Build()
	require.NoError(t, err)
	_, err = agent.AsRunnable().Invoke(context.Background(), "question")
	var stop *tools.StopError
	require.ErrorAs(t, err, &stop)
	require.Len(t, model.requests, 1)
}

// 拒绝作为工具结果交给模型，运行继续。
func TestPipelineDenyContinuesRun(t *testing.T) {
	p := &tools.Pipeline{Pre: []tools.PreHook{func(context.Context, *tools.Call) (tools.PreDecision, error) {
		return tools.PreDecision{Kind: tools.PreDeny, Feedback: "use search instead"}, nil
	}}}
	model := answerModel(nativeCall("c1", "lookup", "{}"))
	agent, err := NewSimpleBuilder(model).WithTools(pipelineTools(t, p)...).WithMaxIterations(3).Build()
	require.NoError(t, err)
	out, err := agent.AsRunnable().Invoke(context.Background(), "question")
	require.NoError(t, err)
	require.Equal(t, "答", out.Result)
	second := model.requests[1].Messages
	require.Contains(t, second[len(second)-1].Content, "use search instead")
}
