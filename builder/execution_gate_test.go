package builder

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/kart-io/goagent/core/middleware"
	"github.com/kart-io/goagent/llm"
	"github.com/stretchr/testify/require"
)

func TestAllExecutionEntriesShareSessionGate(t *testing.T) {
	for _, entry := range []string{"events", "runnable"} {
		t.Run(entry, func(t *testing.T) {
			model := &eventModel{MockLLMClient: NewMockLLMClient("sync"), chunks: []*llm.StreamChunk{{Delta: "stream"}, {Done: true}}}
			agent, err := NewSimpleBuilder(model).WithTimeout(20 * time.Millisecond).Build()
			require.NoError(t, err)
			invoke := func() error {
				switch entry {
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

// stallOnceModel 首次请求一直阻塞到取消，之后的请求直接回答。
type stallOnceModel struct {
	*eventModel
	calls atomic.Int32
}

func (m *stallOnceModel) CompleteStream(ctx context.Context, r *llm.CompletionRequest) (<-chan *llm.StreamChunk, error) {
	if m.calls.Add(1) == 1 {
		return m.eventModel.CompleteStream(ctx, r)
	}
	return m.MockLLMClient.CompleteStream(ctx, r)
}

func TestDirectEventStreamBlocksOtherSessionEntriesUntilCanceled(t *testing.T) {
	model := &stallOnceModel{eventModel: &eventModel{MockLLMClient: NewMockLLMClient("sync"), stall: true, started: make(chan struct{})}}
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
	_, err = agent.AsRunnable().Invoke(waitCtx, "second")
	require.ErrorIs(t, err, context.DeadlineExceeded)
	cancel()
	for range events {
	}
	_, err = agent.AsRunnable().Invoke(context.Background(), "after cancellation")
	require.NoError(t, err)
}

func TestAsRunnableUsesNativeDirectPipeline(t *testing.T) {
	model := nativeModel([]*llm.StreamChunk{{Done: true, FinishReason: "tool_calls", ToolCalls: []llm.ToolCall{nativeCall("id", "lookup", "{}")}, Usage: &llm.Usage{TotalTokens: 3}}})
	tool := &countingEventTool{MockTool: NewMockTool("lookup", "original")}
	agent, err := NewSimpleBuilder(model).WithTools(tool).WithReturnDirectTools("lookup").WithMaxIterations(1).Build()
	require.NoError(t, err)
	agent.chain.Use(endingMiddleware{middleware.NewBaseMiddleware("test")})
	out, err := agent.AsRunnable().Invoke(context.Background(), "question")
	require.NoError(t, err)
	require.Equal(t, "original", out.Result)
	require.Equal(t, "tool", out.Metadata["answer_source"])
	require.Equal(t, true, out.Metadata["after"])
	require.Equal(t, 1, out.Metadata["model_calls"])
	require.Equal(t, 1, tool.calls)
}
