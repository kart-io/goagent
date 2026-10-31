package execution_test

import (
	"context"
	"errors"
	"testing"

	"github.com/kart-io/goagent/builder"
	"github.com/kart-io/goagent/core/budget"
	"github.com/kart-io/goagent/core/durable"
	"github.com/kart-io/goagent/execution"
	"github.com/kart-io/goagent/interfaces"
	"github.com/kart-io/goagent/llm"
	"github.com/kart-io/goagent/llm/constants"
	"github.com/kart-io/goagent/tools"
	"github.com/kart-io/goagent/workflow"
	"github.com/stretchr/testify/require"
)

// 固定离线响应；重建后的多余请求直接失败，证明恢复使用日志。
type scopeModel struct {
	llm.StreamClient
	requests int
	allow    bool
}

func (m *scopeModel) Provider() constants.Provider { return constants.ProviderOpenAI }
func (m *scopeModel) IsAvailable() bool            { return true }
func (m *scopeModel) CompleteToolsStream(context.Context, *llm.CompletionRequest, []interfaces.Tool) (<-chan *llm.StreamChunk, error) {
	m.requests++
	if !m.allow {
		return nil, errors.New("unexpected model request during replay")
	}
	call := llm.ToolCall{ID: "write-1", Type: "function"}
	call.Function.Name = "write"
	call.Function.Arguments = `{}`
	ch := make(chan *llm.StreamChunk, 1)
	ch <- &llm.StreamChunk{Done: true, FinishReason: "tool_calls", ToolCalls: []llm.ToolCall{call}, Usage: &llm.Usage{TotalTokens: 7}}
	close(ch)
	return ch, nil
}

func TestScopeBuilderGraphApprovalAndRestart(t *testing.T) {
	directory := t.TempDir()
	effects := map[string]int{}
	var nodeKeys []string
	approval := ""
	interrupted := errors.New("after effect before node checkpoint")
	for phase := 0; phase < 4; phase++ {
		// 每轮重新打开文件存储并重建预算、Scope、Builder 和 Graph。
		store, err := durable.NewFileStore(directory)
		require.NoError(t, err)
		controller, err := budget.NewDurable(budget.Config{RootKey: "ticket-1", Version: "v1", MaxModelRequests: 1, MaxToolCalls: 1}, store)
		require.NoError(t, err)
		scope, err := execution.NewScope(execution.Config{RunID: "ticket-1", SessionID: "support-session", Journal: store, Budget: controller})
		require.NoError(t, err)
		ctx, cancel, err := scope.Bind(context.Background())
		require.NoError(t, err)
		require.Equal(t, scope.RunID(), durable.ExecutionKey(ctx))
		model := &scopeModel{allow: phase == 1}
		registry := tools.NewRegistry()
		require.NoError(t, registry.Register(tools.NewBaseTool("write", "write", `{"type":"object"}`, func(ctx context.Context, in *interfaces.ToolInput) (*interfaces.ToolOutput, error) {
			require.Equal(t, "support-session", tools.SessionIDFrom(ctx))
			require.Equal(t, durable.ExecutionKey(ctx), in.ExecutionKey)
			require.NotEqual(t, scope.RunID(), in.ExecutionKey)
			effects[in.ExecutionKey]++
			return &interfaces.ToolOutput{Success: true, Result: "done"}, nil
		})))
		dispatcher, err := tools.NewDispatcher(registry, tools.DispatchConfig{})
		require.NoError(t, err)
		configured, err := execution.ConfigureBuilder(builder.NewSimpleBuilder(model).WithTools(dispatcher.Tools()...).WithReturnDirectTools("write").WithMaxIterations(1), scope, "support-agent")
		require.NoError(t, err)
		agent, err := configured.BuildContext(ctx)
		require.NoError(t, err)
		g := workflow.NewGraph[string]()
		require.NoError(t, g.AddNode("effect", func(ctx context.Context, input string) (string, error) {
			nodeKeys = append(nodeKeys, workflow.DurableKey(ctx))
			require.NotEqual(t, scope.RunID(), workflow.DurableKey(ctx))
			out, err := agent.AsRunnable().Invoke(ctx, input)
			if err != nil {
				return "", err
			}
			if phase == 1 {
				return "", interrupted
			}
			return out.Result.(string), nil
		}))
		require.NoError(t, g.AddEdge(workflow.START, "effect"))
		require.NoError(t, g.AddEdge("effect", workflow.END))
		graph, err := g.CompileDurable(ctx, workflow.DurableConfig{Store: store, Version: "v1", ApprovalNodes: []string{"effect"}})
		require.NoError(t, err)
		result, err := graph.AsRunnable().Invoke(ctx, workflow.RunRequest[string]{RunID: scope.RunID(), Initial: "task", ApprovalKey: approval})
		switch phase {
		case 0:
			require.NoError(t, err)
			require.True(t, result.Waiting)
			approval = result.Key
			require.NotEmpty(t, approval)
			require.Empty(t, effects)
			require.Zero(t, model.requests)
		case 1:
			require.ErrorIs(t, err, interrupted)
			require.Equal(t, 1, model.requests)
			require.Len(t, effects, 1)
			approval = ""
		default:
			require.NoError(t, err)
			require.True(t, result.Done)
			require.Equal(t, "done", result.State)
			require.Zero(t, model.requests)
		}
		snapshot, err := controller.Snapshot(ctx)
		require.NoError(t, err)
		if phase == 0 {
			require.Zero(t, snapshot.ReservedRequests)
			require.Zero(t, snapshot.ReservedToolCalls)
		} else {
			require.Equal(t, 1, snapshot.ReservedRequests)
			require.Equal(t, 1, snapshot.ReservedToolCalls)
			require.Equal(t, 7, snapshot.KnownTokens.TotalTokens)
		}
		cancel()
	}
	require.Len(t, nodeKeys, 2)
	require.Equal(t, nodeKeys[0], nodeKeys[1], "重建后节点派生键稳定")
	require.Len(t, effects, 1)
	for _, count := range effects {
		require.Equal(t, 1, count, "重建和回放不重复副作用")
	}
}

func TestConfigureBuilderValidation(t *testing.T) {
	scope, err := execution.NewScope(execution.Config{RunID: "run"})
	require.NoError(t, err)
	b := builder.NewSimpleBuilder(&scopeModel{})
	for _, id := range []string{"", " agent", "agent "} {
		_, err := execution.ConfigureBuilder(b, scope, id)
		require.Error(t, err)
	}
	_, err = execution.ConfigureBuilder(b, (*execution.Scope)(nil), "agent")
	require.Error(t, err)
	_, err = execution.ConfigureBuilder(b, &execution.Scope{}, "agent")
	require.Error(t, err)
	var empty *builder.SimpleAgentBuilder
	_, err = execution.ConfigureBuilder(empty, scope, "agent")
	require.Error(t, err)
}
