package protocol_test

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	api "github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2aclient"
	a2aremote "github.com/kart-io/goagent/a2a/remote"
	"github.com/kart-io/goagent/codec/json"
	"github.com/kart-io/goagent/interfaces"
	mg "github.com/kart-io/goagent/management"
	a2aserver "github.com/kart-io/goagent/management/a2a"
	"github.com/kart-io/goagent/management/httpapi"
	"github.com/kart-io/goagent/management/internal/managementtest"
	"github.com/kart-io/goagent/management/mcpserver"
	"github.com/kart-io/goagent/management/protocol"
	mcpremote "github.com/kart-io/goagent/mcp/remote"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"
)

type endpoints struct {
	a2a    *a2aclient.Client
	mcp    *mcp.ClientSession
	http   *httptest.Server
	a2aURL string
	mcpURL string
}

func serve(t *testing.T, ctx context.Context, f *managementtest.Fixture, agent string) endpoints {
	t.Helper()
	actor := func(r *http.Request) (mg.Actor, error) {
		a := f.Actor
		if tenant := r.Header.Get("X-Test-Tenant"); tenant != "" {
			a.TenantID = tenant
		}
		return a, nil
	}
	ah := httptest.NewUnstartedServer(nil)
	h, err := a2aserver.New(f.Service, a2aserver.Config{AgentID: agent, URL: "http://" + ah.Listener.Addr().String(), Description: "协议回归", ActorFromRequest: actor, PollInterval: time.Millisecond})
	require.NoError(t, err)
	ah.Config.Handler = h
	ah.Start()
	t.Cleanup(ah.Close)
	ac, err := a2aremote.Connect(ctx, ah.URL, ah.Client())
	require.NoError(t, err)
	t.Cleanup(func() { _ = ac.Destroy() })
	mhHandler, err := mcpserver.NewHTTPHandler(f.Service, mcpserver.Config{Agents: []mcpserver.Agent{{ID: agent, Name: "assistant", Description: "运行 Agent"}}, PollInterval: time.Millisecond, ActorFromRequest: func(_ context.Context, r *mcp.CallToolRequest) (mg.Actor, error) {
		a := f.Actor
		if r.Extra != nil && r.Extra.Header.Get("X-Test-Tenant") != "" {
			a.TenantID = r.Extra.Header.Get("X-Test-Tenant")
		}
		return a, nil
	}})
	require.NoError(t, err)
	mh := httptest.NewServer(mhHandler)
	t.Cleanup(mh.Close)
	mc, err := mcpremote.Connect(ctx, &mcp.StreamableClientTransport{Endpoint: mh.URL})
	require.NoError(t, err)
	t.Cleanup(func() { _ = mc.Close() })
	hh, err := httpapi.New(f.Service, httpapi.Config{ActorFromRequest: actor})
	require.NoError(t, err)
	hs := httptest.NewServer(hh)
	t.Cleanup(hs.Close)
	return endpoints{ac, mc, hs, ah.URL, mh.URL}
}
func message(id, session string, input any) *api.SendMessageRequest {
	return &api.SendMessageRequest{Message: &api.Message{ID: id, Role: api.MessageRoleUser, ContextID: session, Parts: api.ContentParts{api.NewDataPart(input)}}}
}
func view(t *testing.T, v any) protocol.TaskView {
	t.Helper()
	raw, err := json.Marshal(v)
	require.NoError(t, err)
	var out protocol.TaskView
	require.NoError(t, json.Unmarshal(raw, &out))
	return out
}
func call(t *testing.T, ctx context.Context, c *mcp.ClientSession, name string, args any) *mcp.CallToolResult {
	t.Helper()
	r, err := c.CallTool(ctx, &mcp.CallToolParams{Name: name, Arguments: args})
	require.NoError(t, err)
	require.False(t, r.IsError, fmt.Sprint(r.Content))
	return r
}
func httpCall(t *testing.T, ctx context.Context, e endpoints, method, path, key string, body any, status int) []byte {
	t.Helper()
	raw, err := json.Marshal(body)
	require.NoError(t, err)
	req, err := http.NewRequestWithContext(ctx, method, e.http.URL+path, bytes.NewReader(raw))
	require.NoError(t, err)
	req.Header.Set("Idempotency-Key", key)
	res, err := e.http.Client().Do(req)
	require.NoError(t, err)
	defer res.Body.Close()
	var value any
	require.NoError(t, json.Decode(res.Body, &value))
	data, err := json.Marshal(value)
	require.NoError(t, err)
	require.Equal(t, status, res.StatusCode, string(data))
	return data
}
func testContext(t *testing.T) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	t.Cleanup(cancel)
	return ctx
}

func TestA2AHTTPMCPShareInputTask(t *testing.T) {
	ctx := testContext(t)
	f := managementtest.NewFixture(t)
	se := f.ReadySession(t)
	var calls atomic.Int32
	f.Factory.RunFunc = func(_ context.Context, s mg.RunSpec) (mg.RunResult, error) {
		calls.Add(1)
		if answer, ok := s.ResumeInputs["answer"]; ok {
			return mg.RunResult{Outcome: "completed", Output: []byte(answer)}, nil
		}
		return mg.RunResult{Outcome: "waiting", Waits: []mg.WaitPoint{{ID: "input", Kind: "input", Key: "answer", Info: []byte(`{"prompt":"补充输入"}`)}}}, nil
	}
	f.StartWorker(t)
	e := serve(t, ctx, f, se.AgentID)
	require.True(t, e.a2a.Card().Capabilities.Streaming)
	require.False(t, e.a2a.Card().Capabilities.PushNotifications)
	request := message("first", se.ID, "你好")
	request.Config = &api.SendMessageConfig{AcceptedOutputModes: []string{"text/plain", "application/json"}}
	result, err := e.a2a.SendMessage(ctx, request)
	require.NoError(t, err)
	task := result.(*api.Task)
	require.Equal(t, api.TaskStateInputRequired, task.Status.State)
	require.Equal(t, se.ID, task.ContextID)
	got := view(t, call(t, ctx, e.mcp, "task_get", map[string]any{"task_id": task.ID}).StructuredContent)
	require.Equal(t, string(task.ID), got.ID)
	require.Equal(t, mg.WaitingInput, got.State)
	raw := httpCall(t, ctx, e, "GET", "/v1/tasks/"+got.ID, "", nil, 200)
	require.NotContains(t, string(raw), "execution_key")
	require.NotContains(t, string(raw), "interrupt_id")
	httpCall(t, ctx, e, "POST", "/v1/tasks/"+got.ID+"/resume", "http-answer", map[string]any{"expected_revision": got.Revision, "wait_id": got.Waits[0].ID, "input": "HTTP 的回答"}, 202)
	// 重新订阅可能在命令应用前看到旧等待快照，客户端应重新读取/订阅。
	require.Eventually(t, func() bool {
		v, err := f.Service.GetTask(ctx, f.Actor, got.ID)
		return err == nil && v.State == mg.Succeeded
	}, time.Second, 2*time.Millisecond)
	var subscribeErr error
	for _, err := range e.a2a.SubscribeToTask(ctx, &api.SubscribeToTaskRequest{ID: task.ID}) {
		subscribeErr = err
	}
	require.ErrorIs(t, subscribeErr, api.ErrUnsupportedOperation)
	completed, err := e.a2a.GetTask(ctx, &api.GetTaskRequest{ID: task.ID})
	require.NoError(t, err)
	require.Equal(t, api.TaskStateCompleted, completed.Status.State)

	final := view(t, call(t, ctx, e.mcp, "task_get", map[string]any{"task_id": got.ID}).StructuredContent)
	require.JSONEq(t, `"HTTP 的回答"`, string(final.Result))
	require.Equal(t, int32(2), calls.Load())
	// 相同 message_id 与输入复用任务；不能重复执行或把不同输入当作重试。
	again, err := e.a2a.SendMessage(ctx, message("first", se.ID, "你好"))
	require.NoError(t, err)
	require.Equal(t, task.ID, again.(*api.Task).ID)
	_, err = e.a2a.SendMessage(ctx, message("first", se.ID, "改写"))
	require.Error(t, err)
	require.Equal(t, int32(2), calls.Load())
	_, err = e.a2a.ListTasks(ctx, &api.ListTasksRequest{ContextID: se.ID, IncludeArtifacts: true, PageSize: 1})
	require.ErrorIs(t, err, api.ErrUnsupportedOperation)
}

func TestMCPInputAndApprovalContinuation(t *testing.T) {
	for _, kind := range []string{"input", "approval"} {
		for _, action := range []string{"accept", "decline", "cancel"} {
			t.Run(kind+"/"+action, func(t *testing.T) {
				ctx := testContext(t)
				f := managementtest.NewFixture(t)
				se := f.ReadySession(t)
				var runs atomic.Int32
				f.Factory.RunFunc = func(_ context.Context, s mg.RunSpec) (mg.RunResult, error) {
					runs.Add(1)
					if answer, ok := s.ResumeInputs["reply"]; ok {
						return mg.RunResult{Outcome: "completed", Output: []byte(answer)}, nil
					}
					return mg.RunResult{Outcome: "waiting", Waits: []mg.WaitPoint{{ID: "wait", Kind: kind, Key: "reply", IntentHash: "intent", Info: []byte(`{"message":"人工确认"}`)}}}, nil
				}
				w := f.StartWorker(t)
				e := serve(t, ctx, f, se.AgentID)
				submitted := view(t, call(t, ctx, e.mcp, "assistant", map[string]any{"message_id": "mcp-start", "input": "执行"}).StructuredContent)
				// 无会话身份时先经共同 Worker 准备会话，任务仍可从 A2A 读取。
				require.NotEmpty(t, submitted.SessionID)
				p, err := protocol.New(f.Service, time.Millisecond)
				require.NoError(t, err)
				waiting, err := p.Wait(ctx, f.Actor, se.AgentID, submitted.ID)
				require.NoError(t, err)
				if kind == "approval" {
					fromA2A, err := e.a2a.GetTask(ctx, &api.GetTaskRequest{ID: api.TaskID(submitted.ID)})
					require.NoError(t, err)
					v := view(t, fromA2A.Metadata["goagent"])
					require.Len(t, v.Approvals, 1)
					httpCall(t, ctx, e, "POST", "/v1/tasks/"+submitted.ID+"/resume", "bypass", map[string]any{"expected_revision": waiting.Revision, "wait_id": "wait", "input": "approve"}, 409)
				}
				pending := call(t, ctx, e.mcp, "task_wait", map[string]any{"task_id": submitted.ID})
				require.True(t, pending.NeedsInput())
				require.Len(t, pending.InputRequests, 1)
				// 等待期间重启 Worker；协议续接不依赖原请求 goroutine。
				require.NoError(t, w.Shutdown(ctx))
				f.StartWorker(t)
				response := &mcp.ElicitResult{Action: action, Content: map[string]any{"input": `"补充内容"`, "decision": "approve", "reason": "人工确认"}}
				result, err := e.mcp.CallTool(ctx, &mcp.CallToolParams{Name: "task_wait", Arguments: map[string]any{"task_id": submitted.ID}, RequestState: pending.RequestState, InputResponses: mcp.InputResponseMap{"wait": response}})
				require.NoError(t, err)
				require.False(t, result.IsError, fmt.Sprint(result.Content))
				require.False(t, result.NeedsInput())
				final := view(t, result.StructuredContent)
				if action == "cancel" || (kind == "input" && action == "decline") {
					require.Equal(t, waiting.State, final.State)
					require.Equal(t, int32(1), runs.Load())
					return
				}
				require.Equal(t, mg.Succeeded, final.State)
				require.Equal(t, int32(2), runs.Load())
				if kind == "input" {
					require.JSONEq(t, `"补充内容"`, string(final.Result))
				} else {
					var a mg.ApprovalAnswer
					require.NoError(t, json.Unmarshal(final.Result, &a))
					want := "approve"
					if action == "decline" {
						want = "reject"
					}
					require.Equal(t, want, a.Decision)
				}
				// 丢失最终响应后重试相同状态/输入，不重复执行已决定操作。
				again, err := e.mcp.CallTool(ctx, &mcp.CallToolParams{Name: "task_wait", Arguments: map[string]any{"task_id": submitted.ID}, RequestState: pending.RequestState, InputResponses: mcp.InputResponseMap{"wait": response}})
				require.NoError(t, err)
				require.False(t, again.IsError, fmt.Sprint(again.Content))
				require.Equal(t, int32(2), runs.Load())
			})
		}
	}
}

func TestA2AInputResumeAndRemoteTool(t *testing.T) {
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
	tool, err := a2aremote.Tool(e.a2a, "remote_agent", "调用外部 Agent")
	require.NoError(t, err)
	out, err := tool.Invoke(ctx, &interfaces.ToolInput{Args: map[string]any{"action": "send", "message_id": "send", "context_id": se.ID, "input": "工作"}})
	require.NoError(t, err)
	waiting := out.Result.(*api.Task)
	v := view(t, waiting.Metadata["goagent"])
	req := message("reply", se.ID, map[string]any{"expected_revision": v.Revision, "wait_id": "input", "input": "续跑"})
	req.Message.TaskID = waiting.ID
	var last api.Event
	for ev, err := range e.a2a.SendStreamingMessage(ctx, req) {
		require.NoError(t, err)
		last = ev
	}
	require.NotNil(t, last)
	final, err := e.a2a.GetTask(ctx, &api.GetTaskRequest{ID: waiting.ID})
	require.NoError(t, err)
	require.Equal(t, api.TaskStateCompleted, final.Status.State)
	// 同一输入回复的消息身份可以安全重试。
	again, err := e.a2a.SendMessage(ctx, req)
	require.NoError(t, err)
	require.Equal(t, waiting.ID, again.(*api.Task).ID)
}

func TestStreamDisconnectDoesNotCancelSharedTask(t *testing.T) {
	ctx := testContext(t)
	f := managementtest.NewFixture(t)
	se := f.ReadySession(t)
	started := make(chan struct{})
	f.Factory.RunFunc = func(ctx context.Context, _ mg.RunSpec) (mg.RunResult, error) {
		close(started)
		<-ctx.Done()
		return mg.RunResult{Outcome: "canceled"}, ctx.Err()
	}
	f.StartWorker(t)
	e := serve(t, ctx, f, se.AgentID)
	var id api.TaskID
	for ev, err := range e.a2a.SendStreamingMessage(ctx, message("stream", se.ID, "运行")) {
		require.NoError(t, err)
		id = ev.(*api.Task).ID
		break
	}
	select {
	case <-started:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	current, err := f.Service.GetTask(ctx, f.Actor, string(id))
	require.NoError(t, err)
	require.Equal(t, mg.Running, current.State)
	canceled := call(t, ctx, e.mcp, "task_cancel", map[string]any{"task_id": id, "message_id": "cancel", "expected_revision": current.Revision, "reason": "停止"})
	require.Contains(t, []mg.TaskState{mg.Canceling, mg.Canceled}, view(t, canceled.StructuredContent).State)
	p, err := protocol.New(f.Service, time.Millisecond)
	require.NoError(t, err)
	final, err := p.Wait(ctx, f.Actor, se.AgentID, string(id))
	require.NoError(t, err)
	require.Equal(t, mg.Canceled, final.State)
	httpCall(t, ctx, e, "GET", "/v1/tasks/"+string(id), "", nil, 200)
	r, err := e.a2a.CancelTask(ctx, &api.CancelTaskRequest{ID: id})
	require.NoError(t, err)
	require.Equal(t, api.TaskStateCanceled, r.Status.State)
	_, err = e.a2a.GetTask(ctx, &api.GetTaskRequest{ID: id, Tenant: "other"})
	require.Error(t, err)
}
