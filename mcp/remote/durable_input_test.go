package remote

import (
	"testing"

	"github.com/kart-io/goagent/codec/json"
	"github.com/kart-io/goagent/core/durable"
	"github.com/kart-io/goagent/interfaces"
	"github.com/kart-io/goagent/tools"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"
)

// 持久运行中远端要求输入时以 durable 等待暂停；恢复时回放已完成的远端调用，只发送带输入的续接。
func TestDurableInputPausesAndResumesWithoutResendingInitialCall(t *testing.T) {
	ctx, endpoint, calls := inputServer(t, false)
	s, err := Connect(ctx, &mcp.StreamableClientTransport{Endpoint: endpoint})
	require.NoError(t, err)
	defer s.Close()
	adapted, err := Tools(ctx, s, WithDurableInput("v1"))
	require.NoError(t, err)
	reg := tools.NewRegistry()
	require.NoError(t, reg.Register(adapted[0]))
	d, err := tools.NewDispatcher(reg, tools.DispatchConfig{})
	require.NoError(t, err)
	store, err := durable.NewFileStore(t.TempDir())
	require.NoError(t, err)
	runCtx := durable.WithJournal(durable.WithExecutionKey(ctx, "parent"), store)
	req := tools.ToolRequest{Name: "review", Input: &interfaces.ToolInput{CallID: "c1", Args: inputArgs().Args}}

	wait := func(err error) (string, InputRequiredError) {
		t.Helper()
		waits := durable.Waits(err)
		require.Len(t, waits, 1, "%v", err)
		var c durable.InterruptContinuation
		require.NoError(t, json.Unmarshal(waits[0].Continuation, &c))
		var pending InputRequiredError
		require.NoError(t, json.Unmarshal(c.Info, &pending))
		return waits[0].Path[len(waits[0].Path)-1].Key, pending
	}
	answer, err := json.Marshal(inputAnswer())
	require.NoError(t, err)

	_, err = d.Invoke(runCtx, req)
	key, pending := wait(err)
	require.Equal(t, "first", pending.RequestState)
	require.Contains(t, pending.Requests, "answer")
	require.EqualValues(t, 1, calls.Load())

	// 输入与请求不匹配时保持等待，不发送续接。
	_, err = d.Invoke(durable.WithResume(runCtx, key, `{"wrong":{"action":"accept"}}`), req)
	_, pending = wait(err)
	require.Equal(t, "first", pending.RequestState)
	require.EqualValues(t, 1, calls.Load())

	_, err = d.Invoke(durable.WithResume(runCtx, key, string(answer)), req)
	key, pending = wait(err)
	require.Equal(t, "second", pending.RequestState)
	require.EqualValues(t, 2, calls.Load(), "恢复时回放初始调用，不重发")

	out, err := d.Invoke(durable.WithResume(runCtx, key, string(answer)), req)
	require.NoError(t, err)
	require.True(t, out.Success)
	require.EqualValues(t, 3, calls.Load())
	result, err := json.Marshal(out.Result)
	require.NoError(t, err)
	require.Contains(t, string(result), "完成")
}

func TestDurableInputConflictsWithDurablePolicy(t *testing.T) {
	ctx, endpoint, _ := inputServer(t, false)
	s, err := Connect(ctx, &mcp.StreamableClientTransport{Endpoint: endpoint})
	require.NoError(t, err)
	defer s.Close()
	_, err = Tools(ctx, s, WithDurableInput("v1"), WithDurablePolicy(durable.OperationPolicy{MaxAttempts: 1}))
	require.Error(t, err)
	_, err = Tools(ctx, s, WithDurableInput(""))
	require.Error(t, err)
}
