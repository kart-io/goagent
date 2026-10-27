package builder

import (
	"context"
	"testing"
	"time"

	"github.com/kart-io/goagent/core/middleware"
	"github.com/kart-io/goagent/llm"
	"github.com/stretchr/testify/require"
)

func TestAllExecutionEntriesShareSessionGate(t *testing.T) {
	for _, entry := range []string{"execute", "tools", "events", "runnable"} {
		t.Run(entry, func(t *testing.T) {
			model := &eventModel{MockLLMClient: NewMockLLMClient("sync"), chunks: []*llm.StreamChunk{{Delta: "stream"}, {Done: true}}}
			agent, err := NewSimpleBuilder(model).WithTimeout(20 * time.Millisecond).Build()
			require.NoError(t, err)
			invoke := func() error {
				switch entry {
				case "execute":
					_, err := agent.Execute(context.Background(), "question")
					return err
				case "tools":
					_, err := agent.ExecuteWithTools(context.Background(), "question")
					return err
				case "events":
					events, err := agent.ExecuteStream(context.Background(), "question")
					if err != nil {
						return err
					}
					for e := range events {
						if e.Error != nil {
							return e.Error
						}
					}
					return nil
				default:
					_, err := agent.AsRunnable().Invoke(context.Background(), "question")
					return err
				}
			}
			agent.executionGate <- struct{}{}
			require.ErrorIs(t, invoke(), context.DeadlineExceeded, "configured timeout must include queueing")
			require.Nil(t, model.request, "waiting must not call provider")
			<-agent.executionGate
			agent.config.Timeout = time.Second
			require.NoError(t, invoke(), "timeout must not consume the next turn's permit")
		})
	}
}

func TestDirectEventStreamBlocksOtherSessionEntriesUntilCanceled(t *testing.T) {
	model := &eventModel{MockLLMClient: NewMockLLMClient("sync"), stall: true, started: make(chan struct{})}
	agent, err := NewSimpleBuilder(model).Build()
	require.NoError(t, err)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	events, err := agent.ExecuteStream(ctx, "first")
	require.NoError(t, err)
	select {
	case <-model.started:
	case <-time.After(time.Second):
		t.Fatal("stream did not start")
	}
	waitCtx, waitCancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer waitCancel()
	_, err = agent.Execute(waitCtx, "second")
	require.ErrorIs(t, err, context.DeadlineExceeded)
	cancel()
	for range events {
	}
	_, err = agent.Execute(context.Background(), "after cancellation")
	require.NoError(t, err)
}

func TestExecuteWithToolsUsesNativeDirectPipeline(t *testing.T) {
	model := nativeModel([]*llm.StreamChunk{{Done: true, FinishReason: "tool_calls", ToolCalls: []llm.ToolCall{nativeCall("id", "lookup", "{}")}, Usage: &llm.Usage{TotalTokens: 3}}})
	tool := &countingEventTool{MockTool: NewMockTool("lookup", "original")}
	agent, err := NewSimpleBuilder(model).WithTools(tool).WithReturnDirectTools("lookup").WithMaxIterations(1).Build()
	require.NoError(t, err)
	agent.chain.Use(endingMiddleware{middleware.NewBaseMiddleware("test")})
	// The legacy no-tool entry must not silently discard modern configuration.
	_, err = agent.Execute(context.Background(), "question")
	require.ErrorContains(t, err, "AsRunnable().Invoke")
	require.Empty(t, model.requests)
	out, err := agent.ExecuteWithTools(context.Background(), "question")
	require.NoError(t, err)
	require.Equal(t, "original", out.Result)
	require.Equal(t, "tool", out.Metadata["answer_source"])
	require.Equal(t, true, out.Metadata["after"])
	require.Equal(t, 1, out.Metadata["model_calls"])
	require.Equal(t, 1, tool.calls)
	require.Zero(t, model.selections)
}
