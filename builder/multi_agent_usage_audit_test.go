package builder

import (
	"context"
	"fmt"
	"testing"

	"github.com/kart-io/goagent/core"
	"github.com/kart-io/goagent/core/budget"
	"github.com/kart-io/goagent/core/durable"
	"github.com/kart-io/goagent/interfaces"
	"github.com/kart-io/goagent/llm"
	"github.com/kart-io/goagent/tools"
	"github.com/stretchr/testify/require"
)

// Real parent/child Builder and Dispatcher paths share one ledger. Child output
// includes usage deliberately: summing output payloads again would double bill.
func TestMultiAgentUsageRootIsNotSumOfNestedOutputs(t *testing.T) {
	for _, persisted := range []bool{false, true} {
		for _, attributed := range []bool{false, true} {
			t.Run(fmt.Sprintf("durable=%v/attributed=%v", persisted, attributed), func(t *testing.T) {
				ctx := context.Background()
				cfg := budget.Config{RootKey: "usage-audit", Version: "v1", MaxModelRequests: 4}
				if attributed {
					cfg.AttributionVersion = "v1"
					cfg.MaxAgentCalls = 8
				}
				controller, err := budget.New(cfg)
				require.NoError(t, err)
				if persisted {
					store, err := durable.NewFileStore(t.TempDir())
					require.NoError(t, err)
					controller, err = budget.NewDurable(cfg, store)
					require.NoError(t, err)
					ctx = durable.WithJournal(durable.WithExecutionKey(ctx, cfg.RootKey), store)
				}
				ctx = budget.WithController(ctx, controller)
				children := make([]*nativeEventModel, 2)
				var childTools []interfaces.Tool
				for index, tokens := range []int{4, 5} {
					children[index] = nativeModel(answerTurn("child", tokens))
					child, err := NewSimpleBuilder(children[index]).WithAgentID(fmt.Sprintf("child-%d", index)).WithDurableScope(fmt.Sprintf("child-%d", index)).
						WithTools(dispatcherFor(t, plainTool()).Tools()...).WithMaxIterations(1).Build()
					require.NoError(t, err)
					tool, err := tools.NewRunnableTool(child.AsRunnable(), tools.RunnableToolConfig[any, *AgentOutput]{
						Name: fmt.Sprintf("child_%d", index), Description: "child", Version: "v1", ModelInvoking: true,
						ArgsSchema: `{"type":"object"}`,
						Decode:     func(map[string]any) (any, error) { return "task", nil },
						Encode: func(out *AgentOutput) (any, error) {
							return map[string]any{"answer": out.Result, "child_usage": out.TokenUsage}, nil
						},
					})
					require.NoError(t, err)
					childTools = append(childTools, tool)
				}
				parent := nativeModel(
					[]*llm.StreamChunk{{Done: true, FinishReason: "tool_calls", Usage: &llm.Usage{TotalTokens: 3}, ToolCalls: []llm.ToolCall{
						nativeCall("a", "child_0", "{}"), nativeCall("b", "child_1", "{}"),
					}}}, answerTurn("summary", 6),
				)
				build := func() *ConfigurableAgent[any, *core.AgentState] {
					agent, err := NewSimpleBuilder(parent).WithAgentID("parent").WithDurableScope("parent").WithParallelToolCalls(2).
						WithTools(dispatcherFor(t, childTools...).Tools()...).WithMaxIterations(2).Build()
					require.NoError(t, err)
					return agent
				}
				out, err := build().AsRunnable().Invoke(ctx, "delegate")
				require.NoError(t, err)
				require.Equal(t, 9, out.TokenUsage.TotalTokens, "parent self usage is 3 + 6")
				require.EqualValues(t, 4, out.Metadata["new_model_invocations"])
				snapshot, err := controller.Snapshot(ctx)
				require.NoError(t, err)
				require.Equal(t, 4, snapshot.ReservedRequests)
				require.Equal(t, 4, snapshot.KnownUsageEntries)
				require.Zero(t, snapshot.UnknownUsageEntries)
				require.Equal(t, 18, snapshot.KnownTokens.TotalTokens, "3 + 4 + 5 + 6, without adding child output payloads again")
				if attributed {
					report, err := controller.Attribution(ctx)
					require.NoError(t, err)
					require.Len(t, report.Calls, 3)
					for _, call := range report.Calls {
						if call.AgentID == "parent" {
							require.Equal(t, 9, call.Self.KnownTokens.TotalTokens)
							require.Equal(t, 18, call.Inclusive.KnownTokens.TotalTokens)
						}
					}
				}
				if persisted {
					_, err = build().AsRunnable().Invoke(ctx, "delegate")
					require.NoError(t, err)
					after, err := controller.Snapshot(ctx)
					require.NoError(t, err)
					require.Equal(t, snapshot, after, "replay is not another charge")
				}
				require.Len(t, parent.requests, 2)
				for _, child := range children {
					require.Len(t, child.requests, 1)
				}
			})
		}
	}
}
