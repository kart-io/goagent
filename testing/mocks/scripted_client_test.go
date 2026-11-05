package mocks

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/kart-io/goagent/llm"
)

func drain(t *testing.T, ch <-chan *llm.StreamChunk) *llm.StreamChunk {
	t.Helper()
	var last *llm.StreamChunk
	for chunk := range ch {
		last = chunk
	}
	require.NotNil(t, last)
	require.True(t, last.Done)
	return last
}

func ask(text string) *llm.CompletionRequest {
	return &llm.CompletionRequest{Messages: []llm.Message{{Role: "user", Content: text}}}
}

func TestScriptedClientRepliesInOrderAndIsStrict(t *testing.T) {
	ctx := context.Background()
	c := NewScriptedClient(
		ScriptedRule{Name: "分流", Match: RequestContains("工单"), Reply: ScriptedReply{Text: "refund"}},
		ToolCallReply("调用", ToolCall("c1", "lookup", `{"task":"x"}`)),
	)
	ch, err := c.CompleteToolsStream(ctx, ask("工单：退款"), nil)
	require.NoError(t, err)
	last := drain(t, ch)
	require.Equal(t, "stop", last.FinishReason)
	require.Equal(t, "refund", last.Content)

	ch, err = c.CompleteStream(ctx, ask("继续"))
	require.NoError(t, err)
	last = drain(t, ch)
	require.Equal(t, "tool_calls", last.FinishReason)
	require.Equal(t, "c1", last.ToolCalls[0].ID)
	require.Equal(t, "lookup", last.ToolCalls[0].Function.Name)

	_, err = c.CompleteStream(ctx, ask("多余"))
	require.ErrorContains(t, err, "没有可用规则")
	require.Equal(t, 3, c.Calls())
	require.Equal(t, 1, c.Hits(0))
	require.Equal(t, 1, c.Hits(1))
	require.Zero(t, c.Remaining())
}

func TestScriptedClientRejectsMismatchedRequest(t *testing.T) {
	c := NewScriptedClient(ScriptedRule{Name: "需要订单号", Match: RequestContains("A1")})
	_, err := c.CompleteStream(context.Background(), ask("没有订单号"))
	require.ErrorContains(t, err, "需要订单号")
	require.Equal(t, 0, c.Hits(0))
	require.Equal(t, 1, c.Remaining())
}

func TestScriptedClientRespondAndDeepCopiedHistory(t *testing.T) {
	c := NewScriptedClient(ScriptedRule{Name: "回声", Respond: func(_ context.Context, req *llm.CompletionRequest) (ScriptedReply, error) {
		return ScriptedReply{Text: "收到：" + req.Messages[0].Content}, nil
	}})
	req := ask("你好")
	ch, err := c.CompleteStream(context.Background(), req)
	require.NoError(t, err)
	require.Equal(t, "收到：你好", drain(t, ch).Content)
	req.Messages[0].Content = "被改写"
	history := c.Requests()
	require.Equal(t, "你好", history[0].Messages[0].Content)
	history[0].Messages[0].Content = "再次改写"
	require.Equal(t, "你好", c.Requests()[0].Messages[0].Content)
}

func TestScriptedClientRespondCanBlockUntilCanceled(t *testing.T) {
	started := make(chan struct{})
	c := NewScriptedClient(ScriptedRule{Name: "挂起", Respond: func(ctx context.Context, _ *llm.CompletionRequest) (ScriptedReply, error) {
		close(started)
		<-ctx.Done()
		return ScriptedReply{}, ctx.Err()
	}})
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		_, err := c.CompleteStream(ctx, ask("q"))
		done <- err
	}()
	<-started
	cancel()
	require.ErrorIs(t, <-done, context.Canceled)
}
