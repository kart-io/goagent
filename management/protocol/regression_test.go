package protocol_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	api "github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/kart-io/goagent/codec/json"
	mg "github.com/kart-io/goagent/management"
	"github.com/kart-io/goagent/management/internal/managementtest"
	"github.com/kart-io/goagent/management/mcpserver"
	"github.com/kart-io/goagent/management/protocol"
	mcpremote "github.com/kart-io/goagent/mcp/remote"
	"github.com/modelcontextprotocol/go-sdk/jsonrpc"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"
)

func TestA2APreservesNumericIdentityAndRejectsUnsupportedMedia(t *testing.T) {
	ctx := testContext(t)
	f := managementtest.NewFixture(t)
	se := f.ReadySession(t)
	f.StartWorker(t)
	e := serve(t, ctx, f, se.AgentID)
	req := message("number", se.ID, map[string]any{"id": json.Number("9007199254740993")})
	out, err := e.a2a.SendMessage(ctx, req)
	require.NoError(t, err)
	task, err := f.Service.GetTask(ctx, f.Actor, string(out.(*api.Task).ID))
	require.NoError(t, err)
	// JSONEq 本身使用浮点解码，不能作为此精度回归的断言。
	require.Equal(t, `{"id":9007199254740993}`, string(task.Input))
	_, err = e.a2a.SendMessage(ctx, req)
	require.NoError(t, err)
	_, err = e.a2a.SendMessage(ctx, message("number", se.ID, map[string]any{"id": json.Number("9007199254740992")}))
	require.ErrorIs(t, err, api.ErrUnsupportedOperation)
	req = message("bad-media", se.ID, map[string]any{"id": 1})
	req.Message.Parts[0].MediaType = "image/png"
	_, err = e.a2a.SendMessage(ctx, req)
	require.ErrorIs(t, err, api.ErrUnsupportedContentType)
	req = message("bad-output", se.ID, "输入")
	req.Config = &api.SendMessageConfig{AcceptedOutputModes: []string{"image/png", "text/plain"}}
	_, err = e.a2a.SendMessage(ctx, req)
	require.ErrorIs(t, err, api.ErrUnsupportedContentType)
}

func TestLegacyMCPServerBridgesMultipleWaits(t *testing.T) {
	ctx := testContext(t)
	f := managementtest.NewFixture(t)
	se := f.ReadySession(t)
	f.Factory.RunFunc = func(_ context.Context, s mg.RunSpec) (mg.RunResult, error) {
		if _, ok := s.ResumeInputs["second"]; ok {
			return mg.RunResult{Outcome: "completed", Output: []byte(`"完成"`)}, nil
		}
		id := "first"
		if _, ok := s.ResumeInputs["first"]; ok {
			id = "second"
		}
		return mg.RunResult{Outcome: "waiting", Waits: []mg.WaitPoint{{ID: id, Key: id, Kind: "input", Info: []byte(`{}`)}}}, nil
	}
	f.StartWorker(t)
	srv, err := mcpserver.New(f.Service, mcpserver.Config{Agents: []mcpserver.Agent{{ID: se.AgentID, Name: "assistant"}}, PollInterval: time.Millisecond, ActorFromRequest: func(context.Context, *mcp.CallToolRequest) (mg.Actor, error) { return f.Actor, nil }})
	require.NoError(t, err)
	// SDK 有状态 HTTP 只协商旧版本，用于验证反向 elicitation 而非 MRTR。
	h := httptest.NewServer(mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return srv }, &mcp.StreamableHTTPOptions{JSONResponse: true}))
	defer h.Close()
	var rounds atomic.Int32
	client, err := mcpremote.ConnectWithOptions(ctx, &mcp.StreamableClientTransport{Endpoint: h.URL}, &mcp.ClientOptions{ElicitationHandler: func(context.Context, *mcp.ElicitRequest) (*mcp.ElicitResult, error) {
		rounds.Add(1)
		return &mcp.ElicitResult{Action: "accept", Content: map[string]any{"input": `true`}}, nil
	}})
	require.NoError(t, err)
	defer client.Close()
	started := view(t, call(t, ctx, client, "assistant", map[string]any{"message_id": "start", "session_id": se.ID, "input": true}).StructuredContent)
	result := call(t, ctx, client, "task_wait", map[string]any{"task_id": started.ID})
	require.False(t, result.NeedsInput())
	require.Equal(t, mg.Succeeded, view(t, result.StructuredContent).State)
	require.Equal(t, int32(2), rounds.Load())
}

func TestA2AResumeReturnsBeforeExecutionFinishes(t *testing.T) {
	for _, streaming := range []bool{false, true} {
		t.Run(map[bool]string{false: "立即返回", true: "流式首事件"}[streaming], func(t *testing.T) {
			ctx := testContext(t)
			f := managementtest.NewFixture(t)
			se := f.ReadySession(t)
			gate := make(chan struct{})
			defer close(gate)
			f.Factory.RunFunc = func(ctx context.Context, s mg.RunSpec) (mg.RunResult, error) {
				if len(s.ResumeInputs) == 0 {
					return mg.RunResult{Outcome: "waiting", Waits: []mg.WaitPoint{{ID: "wait", Kind: "input", Key: "answer", Info: []byte(`{}`)}}}, nil
				}
				select {
				case <-gate:
					return mg.RunResult{Outcome: "completed", Output: []byte(`true`)}, nil
				case <-ctx.Done():
					return mg.RunResult{}, ctx.Err()
				}
			}
			f.StartWorker(t)
			e := serve(t, ctx, f, se.AgentID)
			out, err := e.a2a.SendMessage(ctx, message("start", se.ID, "工作"))
			require.NoError(t, err)
			waiting := out.(*api.Task)
			v := view(t, waiting.Metadata["goagent"])
			req := message("resume", se.ID, map[string]any{"expected_revision": v.Revision, "wait_id": "wait", "input": true})
			req.Message.TaskID = waiting.ID
			req.Config = &api.SendMessageConfig{ReturnImmediately: true}
			deadline, cancel := context.WithTimeout(ctx, time.Second)
			defer cancel()
			var current *api.Task
			if streaming {
				for ev, err := range e.a2a.SendStreamingMessage(deadline, req) {
					require.NoError(t, err)
					current = ev.(*api.Task)
					break
				}
			} else {
				out, err = e.a2a.SendMessage(deadline, req)
				require.NoError(t, err)
				current = out.(*api.Task)
			}
			require.NotNil(t, current)
			require.Equal(t, api.TaskStateWorking, current.Status.State)
			stored, err := f.Service.GetTask(ctx, f.Actor, string(current.ID))
			require.NoError(t, err)
			require.Equal(t, mg.Running, stored.State)
		})
	}
}

func TestApprovalProjectionUsesConfiguredPageSizeAndStableWaitIDs(t *testing.T) {
	ctx := testContext(t)
	f := managementtest.NewFixture(t)
	cfg := f.Config()
	cfg.Limits = mg.DefaultLimits()
	cfg.Limits.PageSize, cfg.Limits.MaxPageSize = 1, 2
	var err error
	f.Service, err = mg.New(cfg)
	require.NoError(t, err)
	se := f.ReadySession(t)
	f.Factory.RunFunc = func(_ context.Context, s mg.RunSpec) (mg.RunResult, error) {
		if len(s.ResumeInputs) != 0 {
			return mg.RunResult{Outcome: "waiting", Waits: []mg.WaitPoint{{ID: "second", Kind: "input", Key: "second", Info: []byte(`{"round":2}`)}}}, nil
		}
		return mg.RunResult{Outcome: "waiting", Waits: []mg.WaitPoint{
			{ID: "first", Kind: "approval", Key: "first", IntentHash: "first", Info: []byte(`{}`)},
			{ID: "another", Kind: "approval", Key: "another", IntentHash: "another", Info: []byte(`{}`)},
		}}, nil
	}
	f.StartWorker(t)
	e := serve(t, ctx, f, se.AgentID)
	out, err := e.a2a.SendMessage(ctx, message("start", se.ID, "工作"))
	require.NoError(t, err)
	first := out.(*api.Task)
	v := view(t, first.Metadata["goagent"])
	require.Len(t, v.Approvals, 2)
	again, err := e.a2a.GetTask(ctx, &api.GetTaskRequest{ID: first.ID})
	require.NoError(t, err)
	require.Equal(t, first.Status.Message.ID, again.Status.Message.ID)
	require.Len(t, view(t, call(t, ctx, e.mcp, "task_get", map[string]any{"task_id": first.ID}).StructuredContent).Approvals, 2)
	ap := v.Approvals[0]
	req := message(ap.ID, se.ID, map[string]any{"expected_revision": v.Revision, "approval_id": ap.ID, "approval_revision": ap.Revision, "decision": "approve"})
	req.Message.TaskID = first.ID
	out, err = e.a2a.SendMessage(ctx, req)
	require.NoError(t, err)
	second := out.(*api.Task)
	require.NotEqual(t, first.Status.Message.ID, second.Status.Message.ID)
	require.Equal(t, api.TaskStateInputRequired, second.Status.State)
	again, err = e.a2a.GetTask(ctx, &api.GetTaskRequest{ID: second.ID})
	require.NoError(t, err)
	require.Equal(t, second.Status.Message.ID, again.Status.Message.ID)
	_, err = e.a2a.ListTasks(ctx, &api.ListTasksRequest{PageSize: 1})
	require.ErrorIs(t, err, api.ErrUnsupportedOperation)
}

func TestMCPChecksFormCapabilityOnlyWhenNeeded(t *testing.T) {
	ctx := testContext(t)
	f := managementtest.NewFixture(t)
	se := f.ReadySession(t)
	f.Factory.RunFunc = func(context.Context, mg.RunSpec) (mg.RunResult, error) {
		return mg.RunResult{Outcome: "waiting", Waits: []mg.WaitPoint{{ID: "wait", Kind: "input", Key: "answer", Info: []byte(`{}`)}}}, nil
	}
	f.StartWorker(t)
	e := serve(t, ctx, f, se.AgentID)
	out, err := e.a2a.SendMessage(ctx, message("start", se.ID, "输入"))
	require.NoError(t, err)
	id := out.(*api.Task).ID
	for _, tc := range []struct {
		name string
		caps *mcp.ElicitationCapabilities
		ok   bool
	}{
		{"未声明", nil, false},
		{"空对象", &mcp.ElicitationCapabilities{}, true},
		{"仅URL", &mcp.ElicitationCapabilities{URL: &mcp.URLElicitationCapabilities{}}, false},
		{"form", &mcp.ElicitationCapabilities{Form: &mcp.FormElicitationCapabilities{}}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			client, err := mcpremote.ConnectWithOptions(ctx, &mcp.StreamableClientTransport{Endpoint: e.mcpURL}, &mcp.ClientOptions{Capabilities: &mcp.ClientCapabilities{Elicitation: tc.caps}})
			require.NoError(t, err)
			defer client.Close()
			call(t, ctx, client, "task_get", map[string]any{"task_id": id})
			r, err := client.CallTool(ctx, &mcp.CallToolParams{Name: "task_wait", Arguments: map[string]any{"task_id": id}})
			if tc.ok {
				require.NoError(t, err)
				require.True(t, r.NeedsInput())
			} else {
				var rpc *jsonrpc.Error
				require.ErrorAs(t, err, &rpc)
				require.EqualValues(t, mcp.CodeMissingRequiredClientCapabilities, rpc.Code)
			}
		})
	}
	p, err := protocol.New(f.Service, time.Millisecond)
	require.NoError(t, err)
	stored, err := p.Task(ctx, f.Actor, se.AgentID, string(id))
	require.NoError(t, err)
	require.Equal(t, mg.WaitingInput, stored.State)
	_, err = f.Service.CancelTask(ctx, f.Actor, mg.CancelTaskRequest{TaskID: stored.ID, IdempotencyKey: "cancel", ExpectedRevision: stored.Revision})
	require.NoError(t, err)
	_, err = p.Wait(ctx, f.Actor, se.AgentID, stored.ID)
	require.NoError(t, err)
	client, err := mcpremote.ConnectWithOptions(ctx, &mcp.StreamableClientTransport{Endpoint: e.mcpURL}, &mcp.ClientOptions{Capabilities: &mcp.ClientCapabilities{}})
	require.NoError(t, err)
	defer client.Close()
	completed := call(t, ctx, client, "task_wait", map[string]any{"task_id": id})
	require.Equal(t, mg.Canceled, view(t, completed.StructuredContent).State)
}

type tenantTransport struct{ base http.RoundTripper }

func (r tenantTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	copy := req.Clone(req.Context())
	copy.Header.Set("X-Test-Tenant", "other-tenant")
	return r.base.RoundTrip(copy)
}

func TestMCPContinuationRejectsForeignActorAndInvalidState(t *testing.T) {
	ctx := testContext(t)
	f := managementtest.NewFixture(t)
	se := f.ReadySession(t)
	var runs atomic.Int32
	f.Factory.RunFunc = func(context.Context, mg.RunSpec) (mg.RunResult, error) {
		runs.Add(1)
		return mg.RunResult{Outcome: "waiting", Waits: []mg.WaitPoint{{ID: "wait", Key: "answer", Kind: "input", Info: []byte(`{}`)}}}, nil
	}
	f.StartWorker(t)
	e := serve(t, ctx, f, se.AgentID)
	out, err := e.a2a.SendMessage(ctx, message("start", se.ID, true))
	require.NoError(t, err)
	id := out.(*api.Task).ID
	pending := call(t, ctx, e.mcp, "task_wait", map[string]any{"task_id": id})
	foreign, err := mcpremote.Connect(ctx, &mcp.StreamableClientTransport{Endpoint: e.mcpURL, HTTPClient: &http.Client{Transport: tenantTransport{http.DefaultTransport}}})
	require.NoError(t, err)
	defer foreign.Close()
	request := func(state, responseID string) *mcp.CallToolParams {
		return &mcp.CallToolParams{Name: "task_wait", Arguments: map[string]any{"task_id": id}, RequestState: state, InputResponses: mcp.InputResponseMap{responseID: &mcp.ElicitResult{Action: "accept", Content: map[string]any{"input": `true`}}}}
	}
	r, err := foreign.CallTool(ctx, request(pending.RequestState, "wait"))
	require.NoError(t, err)
	require.True(t, r.IsError)
	r, err = e.mcp.CallTool(ctx, request(pending.RequestState, "unrelated"))
	require.NoError(t, err)
	require.True(t, r.IsError)
	var state map[string]any
	require.NoError(t, json.Unmarshal([]byte(pending.RequestState), &state, json.UseNumber))
	state["task_id"] = "another-task"
	raw, err := json.Marshal(state)
	require.NoError(t, err)
	r, err = e.mcp.CallTool(ctx, request(string(raw), "wait"))
	require.NoError(t, err)
	require.True(t, r.IsError)
	state["task_id"], state["revision"] = string(id), 1
	raw, err = json.Marshal(state)
	require.NoError(t, err)
	r, err = e.mcp.CallTool(ctx, request(string(raw), "wait"))
	require.NoError(t, err)
	require.True(t, r.IsError)
	stored, err := f.Service.GetTask(ctx, f.Actor, string(id))
	require.NoError(t, err)
	require.Equal(t, mg.WaitingInput, stored.State)
	require.Equal(t, int32(1), runs.Load())
}
