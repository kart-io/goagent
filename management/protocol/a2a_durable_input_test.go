package protocol_test

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"sync/atomic"
	"testing"
	"time"

	a2aremote "github.com/kart-io/goagent/a2a/remote"
	"github.com/kart-io/goagent/codec/json"
	"github.com/kart-io/goagent/core/durable"
	"github.com/kart-io/goagent/interfaces"
	mg "github.com/kart-io/goagent/management"
	"github.com/kart-io/goagent/management/internal/managementtest"
	"github.com/kart-io/goagent/management/protocol"
	"github.com/kart-io/goagent/tools"
	"github.com/stretchr/testify/require"
)

// sendCounter 统计经 A2A 客户端发出的 SendMessage 请求。
type sendCounter struct {
	base  http.RoundTripper
	sends atomic.Int32
}

func (c *sendCounter) RoundTrip(r *http.Request) (*http.Response, error) {
	if r.Body != nil {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			return nil, err
		}
		r.Body = io.NopCloser(bytes.NewReader(body))
		if bytes.Contains(body, []byte(`"SendMessage"`)) {
			c.sends.Add(1)
		}
	}
	return c.base.RoundTrip(r)
}

// 持久运行中远端任务等待输入时以 durable 等待暂停；恢复后把宿主输入发往同一任务，不重发初始消息。
func TestA2ADurableInputPausesAndResumes(t *testing.T) {
	ctx := testContext(t)
	f := managementtest.NewFixture(t)
	se := f.ReadySession(t)
	f.Factory.RunFunc = func(_ context.Context, s mg.RunSpec) (mg.RunResult, error) {
		if v, ok := s.ResumeInputs["input"]; ok {
			return mg.RunResult{Outcome: "completed", Output: []byte(v)}, nil
		}
		return mg.RunResult{Outcome: "waiting", Waits: []mg.WaitPoint{{ID: "input", Kind: "input", Key: "input", Info: []byte(`{}`)}}}, nil
	}
	f.StartWorker(t)
	e := serve(t, ctx, f, se.AgentID)
	counter := &sendCounter{base: http.DefaultTransport}
	client, err := a2aremote.Connect(ctx, e.a2aURL, &http.Client{Transport: counter})
	require.NoError(t, err)
	t.Cleanup(func() { _ = client.Destroy() })

	tool, err := a2aremote.Tool(client, "remote_agent", "调用外部 Agent", a2aremote.WithDurableInput("v1"))
	require.NoError(t, err)
	reg := tools.NewRegistry()
	require.NoError(t, reg.Register(tool))
	d, err := tools.NewDispatcher(reg, tools.DispatchConfig{})
	require.NoError(t, err)
	store, err := durable.NewFileStore(t.TempDir())
	require.NoError(t, err)
	runCtx := durable.WithJournal(durable.WithExecutionKey(ctx, "parent"), store)
	req := tools.ToolRequest{Name: "remote_agent", Input: &interfaces.ToolInput{CallID: "c1", Args: map[string]any{"action": "send", "message_id": "send", "context_id": se.ID, "input": "工作"}}}

	_, err = d.Invoke(runCtx, req)
	waits := durable.Waits(err)
	require.Len(t, waits, 1, "%v", err)
	var c durable.InterruptContinuation
	require.NoError(t, json.Unmarshal(waits[0].Continuation, &c))
	var task struct {
		Metadata map[string]any `json:"metadata"`
	}
	require.NoError(t, json.Unmarshal(c.Info, &task))
	v := view(t, task.Metadata["goagent"])
	require.EqualValues(t, 1, counter.sends.Load())

	reply, err := json.Marshal(map[string]any{"expected_revision": v.Revision, "wait_id": "input", "input": "续跑"})
	require.NoError(t, err)
	out, err := d.Invoke(durable.WithResume(runCtx, waits[0].Path[len(waits[0].Path)-1].Key, string(reply)), req)
	require.NoError(t, err)
	require.True(t, out.Success)
	require.EqualValues(t, 2, counter.sends.Load(), "恢复时回放初始消息，只发送输入回复")

	p, err := protocol.New(f.Service, time.Millisecond)
	require.NoError(t, err)
	final, err := p.Wait(ctx, f.Actor, se.AgentID, v.ID)
	require.NoError(t, err)
	require.Equal(t, mg.Succeeded, final.State)
	require.JSONEq(t, `"续跑"`, string(final.Result))
}
