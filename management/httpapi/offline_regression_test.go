package httpapi_test

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/kart-io/goagent/codec/json"
	"github.com/kart-io/goagent/constants"
	"github.com/kart-io/goagent/core/session"
	agentErrors "github.com/kart-io/goagent/errors"
	mg "github.com/kart-io/goagent/management"
	"github.com/kart-io/goagent/management/httpapi"
	"github.com/kart-io/goagent/management/internal/managementtest"
	"github.com/stretchr/testify/require"
)

// 所有请求都经过真实路由和服务；仅存储与执行器使用共享的离线替身。
func offlineHandler(t *testing.T, f *managementtest.Fixture) *httpapi.Handler {
	t.Helper()
	h, err := httpapi.New(f.Service, httpapi.Config{ActorFromRequest: func(*http.Request) (mg.Actor, error) { return f.Actor, nil }})
	require.NoError(t, err)
	return h
}

func offlineRequest(t *testing.T, h http.Handler, method, path, body, key string, status int) *httptest.ResponseRecorder {
	t.Helper()
	r := httptest.NewRequest(method, path, strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Idempotency-Key", key)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	require.Equal(t, status, w.Code, "%s %s：%s", method, path, w.Body.String())
	require.NotEmpty(t, w.Header().Get("X-Request-ID"), "响应必须包含请求标识")
	require.Equal(t, "application/json; charset=utf-8", w.Header().Get("Content-Type"))
	return w
}

func offlineDecode[T any](t *testing.T, w *httptest.ResponseRecorder) T {
	t.Helper()
	var out T
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &out), "响应必须是合法的 JSON")
	return out
}

func offlineProblem(t *testing.T, w *httptest.ResponseRecorder, code string) {
	t.Helper()
	out := offlineDecode[struct {
		Error struct {
			Code      string `json:"code"`
			Message   string `json:"message"`
			RequestID string `json:"request_id"`
		} `json:"error"`
	}](t, w)
	require.Equal(t, code, out.Error.Code)
	require.NotEmpty(t, out.Error.Message)
	require.Equal(t, w.Header().Get("X-Request-ID"), out.Error.RequestID)
	require.Empty(t, w.Header().Get("Location"), "失败请求不能返回资源地址")
}

func TestOfflineAgentRoutesAndIdempotency(t *testing.T) {
	f := managementtest.NewFixture(t)
	h := offlineHandler(t, f)
	w := offlineRequest(t, h, "POST", "/v1/agents", `{"name":"离线客服"}`, "注册", 201)
	agent := offlineDecode[mg.Agent](t, w)
	require.NotEmpty(t, agent.ID)
	require.Equal(t, f.Actor.TenantID, agent.TenantID)
	require.Equal(t, "离线客服", agent.Name)
	require.True(t, agent.Enabled)
	base := "/v1/agents/" + agent.ID
	require.Equal(t, base, w.Header().Get("Location"))
	replay := offlineRequest(t, h, "POST", "/v1/agents", ` { "name" : "离线客服" } `, "注册", 201)
	require.JSONEq(t, w.Body.String(), replay.Body.String(), "等价请求必须重放原结果")
	require.NotEqual(t, w.Header().Get("X-Request-ID"), replay.Header().Get("X-Request-ID"))
	offlineProblem(t, offlineRequest(t, h, "POST", "/v1/agents", `{"name":"另一客服"}`, "注册", 409), "conflict")

	w = offlineRequest(t, h, "POST", base+"/versions", `{"version":"v1","runner_kind":"offline","implementation_version":"impl-v1","config":{"key":"业务配置"}}`, "版本", 201)
	version := offlineDecode[mg.Version](t, w)
	require.Equal(t, agent.ID, version.AgentID)
	require.Equal(t, "v1", version.Version)
	require.JSONEq(t, `{"key":"业务配置"}`, string(version.Config))
	require.Equal(t, base+"/versions/"+version.ID, w.Header().Get("Location"))
	w = offlineRequest(t, h, "POST", base+"/publish", `{"version":"v1","expected_revision":1}`, "发布", 200)
	agent = offlineDecode[mg.Agent](t, w)
	require.Equal(t, "v1", agent.PublishedVersion)
	require.EqualValues(t, 2, agent.Revision)
	offlineProblem(t, offlineRequest(t, h, "POST", base+"/disable", `{"expected_revision":1}`, "旧修订", 409), "conflict")
	// 路径决定启停动作，不能让请求体覆盖路由语义。
	w = offlineRequest(t, h, "POST", base+"/disable", `{"expected_revision":2,"enabled":true}`, "停用", 200)
	agent = offlineDecode[mg.Agent](t, w)
	require.False(t, agent.Enabled)
	require.EqualValues(t, 3, agent.Revision)
	w = offlineRequest(t, h, "POST", base+"/enable", `{"expected_revision":3,"enabled":false}`, "启用", 200)
	agent = offlineDecode[mg.Agent](t, w)
	require.True(t, agent.Enabled)
	require.EqualValues(t, 4, agent.Revision)
	require.Equal(t, agent, offlineDecode[mg.Agent](t, offlineRequest(t, h, "GET", base, "", "", 200)))
	page := offlineDecode[mg.Page[mg.Agent]](t, offlineRequest(t, h, "GET", "/v1/agents", "", "", 200))
	require.Equal(t, []mg.Agent{agent}, page.Items)
	versions := offlineDecode[mg.Page[mg.Version]](t, offlineRequest(t, h, "GET", base+"/versions?agent_id=不能覆盖路径", "", "", 200))
	require.Equal(t, []mg.Version{version}, versions.Items)
	saved, err := f.Service.GetAgent(context.Background(), f.Actor, agent.ID)
	require.NoError(t, err)
	require.Equal(t, agent, saved, "HTTP 变更必须真实持久化")
}

func TestOfflineSessionHistoryAndForkRoutes(t *testing.T) {
	f := managementtest.NewFixture(t)
	se := f.ReadySession(t)
	h := offlineHandler(t, f)
	ctx := context.Background()
	w := offlineRequest(t, h, "POST", "/v1/sessions", fmt.Sprintf(`{"agent_id":%q}`, se.AgentID), "创建会话", 202)
	created := offlineDecode[mg.Session](t, w)
	require.Equal(t, "preparing", created.Status)
	require.Equal(t, se.AgentID, created.AgentID)
	require.Equal(t, se.Version, created.Version)
	require.Equal(t, "/v1/sessions/"+created.ID, w.Header().Get("Location"))
	require.NotContains(t, offlineDecode[map[string]any](t, w), "log_id")

	start, err := session.NewEvent(constants.SessionEventTurnStart, session.TurnStart{Input: "完整轮次"})
	require.NoError(t, err)
	end, err := session.NewEvent(constants.SessionEventTurnEnd, session.TurnEnd{Answer: "已完成"})
	require.NoError(t, err)
	events, err := f.Sessions.Read(ctx, se.LogID, 0)
	require.NoError(t, err)
	last, err := f.Sessions.Append(ctx, se.LogID, session.LastSeq(events), start, end)
	require.NoError(t, err)
	base := "/v1/sessions/" + se.ID
	w = offlineRequest(t, h, "GET", base, "", "", 200)
	require.Equal(t, "ready", offlineDecode[mg.Session](t, w).Status)
	require.NotContains(t, offlineDecode[map[string]any](t, w), "log_id")
	page := offlineDecode[mg.Page[mg.Session]](t, offlineRequest(t, h, "GET", "/v1/sessions?agent_id="+se.AgentID+"&status=ready", "", "", 200))
	require.Len(t, page.Items, 1)
	require.Equal(t, se.ID, page.Items[0].ID)
	history := offlineDecode[mg.HistoryPage](t, offlineRequest(t, h, "GET", base+"/history?after_seq=0&limit=1&type="+url.QueryEscape(constants.SessionEventTurnEnd), "", "", 200))
	require.Len(t, history.Items, 1)
	require.Equal(t, last, history.NextSeq)
	require.Equal(t, constants.SessionEventTurnEnd, history.Items[0].Type)
	require.JSONEq(t, string(end.Data), string(history.Items[0].Data))
	empty := offlineDecode[mg.HistoryPage](t, offlineRequest(t, h, "GET", fmt.Sprintf("%s/history?after_seq=%d", base, last), "", "", 200))
	require.Empty(t, empty.Items)

	w = offlineRequest(t, h, "POST", base+"/fork", fmt.Sprintf(`{"expected_revision":%d,"up_to":%d}`, se.Revision, last), "分叉", 202)
	branch := offlineDecode[mg.Session](t, w)
	require.Equal(t, se.ID, branch.ParentID)
	require.Equal(t, last, branch.ForkSeq)
	require.Equal(t, "preparing", branch.Status)
	require.Equal(t, "/v1/sessions/"+branch.ID, w.Header().Get("Location"))
	for _, key := range []string{"log_id", "source_hash"} {
		require.NotContains(t, offlineDecode[map[string]any](t, w), key)
	}
	saved, err := f.Service.GetSession(ctx, f.Actor, branch.ID)
	require.NoError(t, err)
	require.NotEmpty(t, saved.SourceHash, "内部证据应持久化，但不应向客户端暴露")

	for _, tc := range []struct {
		query  string
		status int
		code   string
	}{
		{"after_seq=非法", 400, "invalid_request"}, {"after_seq=-1", 400, "invalid_request"},
		{"type=management/private", 403, "forbidden"}, {"type=" + url.QueryEscape(constants.SessionEventBoundarySnapshot), 403, "forbidden"},
	} {
		t.Run(tc.query, func(t *testing.T) {
			offlineProblem(t, offlineRequest(t, h, "GET", base+"/history?"+tc.query, "", "", tc.status), tc.code)
		})
	}
}

func TestOfflineTaskRunRoutesAndCapacity(t *testing.T) {
	f := managementtest.NewFixture(t)
	se := f.ReadySession(t)
	cfg := f.Config()
	cfg.Limits = mg.DefaultLimits()
	cfg.Limits.MaxPending = 1
	var err error
	f.Service, err = mg.New(cfg)
	require.NoError(t, err)
	h := offlineHandler(t, f)
	body := fmt.Sprintf(`{"session_id":%q,"input":{"key":"业务键","execution_key":"业务值"}}`, se.ID)
	w := offlineRequest(t, h, "POST", "/v1/tasks", body, "首个任务", 202)
	task := offlineDecode[mg.Task](t, w)
	require.Equal(t, mg.Queued, task.State)
	require.Equal(t, se.ID, task.SessionID)
	require.Equal(t, "/v1/tasks/"+task.ID, w.Header().Get("Location"))
	require.JSONEq(t, `{"key":"业务键","execution_key":"业务值"}`, string(task.Input))
	for _, key := range []string{"execution_key", "input_hash", "operations"} {
		require.NotContains(t, offlineDecode[map[string]any](t, w), key)
	}
	replay := offlineRequest(t, h, "POST", "/v1/tasks", body, "首个任务", 202)
	require.JSONEq(t, w.Body.String(), replay.Body.String())
	offlineProblem(t, offlineRequest(t, h, "POST", "/v1/tasks", body, "容量重试", 429), "busy")
	page := offlineDecode[mg.Page[mg.Task]](t, offlineRequest(t, h, "GET", "/v1/tasks?session_id="+se.ID+"&status=queued&status=running", "", "", 200))
	require.Len(t, page.Items, 1, "容量拒绝不能残留任务")
	require.Equal(t, task.ID, page.Items[0].ID)
	w = offlineRequest(t, h, "POST", "/v1/tasks/"+task.ID+"/cancel", `{"expected_revision":1,"reason":"释放容量"}`, "取消", 202)
	canceled := offlineDecode[mg.Task](t, w)
	require.Equal(t, mg.Canceled, canceled.State)
	require.True(t, canceled.CancelRequested)
	require.NotNil(t, canceled.FinishedAt)
	require.Equal(t, canceled, offlineDecode[mg.Task](t, offlineRequest(t, h, "GET", "/v1/tasks/"+task.ID, "", "", 200)))
	accepted := offlineDecode[mg.Task](t, offlineRequest(t, h, "POST", "/v1/tasks", body, "容量重试", 202))
	require.NotEqual(t, task.ID, accepted.ID, "失败幂等键应允许容量恢复后重试")

	w = offlineRequest(t, h, "GET", "/v1/runs/"+task.ID, "", "", 200)
	run := offlineDecode[mg.Run](t, w)
	require.Equal(t, task.ID, run.Task.ID)
	require.Equal(t, mg.Canceled, run.Task.State)
	require.Empty(t, run.Attempts, "排队取消不能产生执行尝试")
	require.Nil(t, run.Usage, "没有用量证据时不能伪造零消耗")
	require.Equal(t, "missing", run.UsageStatus)
	require.NotContains(t, offlineDecode[map[string]any](t, w)["task"], "execution_key")
	runs := offlineDecode[mg.Page[mg.Run]](t, offlineRequest(t, h, "GET", "/v1/runs?session_id="+se.ID+"&status=canceled", "", "", 200))
	require.Len(t, runs.Items, 1)
	require.Equal(t, task.ID, runs.Items[0].Task.ID)
	summary := offlineDecode[mg.RunSummary](t, offlineRequest(t, h, "GET", "/v1/runs/summary?session_id="+se.ID+"&limit=1", "", "", 200))
	require.Equal(t, 2, summary.Totals.Tasks, "汇总必须跨页统计，且不能被运行详情路由截获")
	require.Equal(t, 1, summary.Totals.States[mg.Canceled])
	require.Equal(t, 1, summary.Totals.States[mg.Queued])
	require.Equal(t, 2, summary.Totals.UnknownRuns)
	require.Nil(t, summary.Totals.KnownTokens)
}

func TestOfflineResumeApprovalAndCommandRoutes(t *testing.T) {
	for _, kind := range []string{"input", "approval"} {
		t.Run(kind, func(t *testing.T) {
			f := managementtest.NewFixture(t)
			se := f.ReadySession(t)
			h := offlineHandler(t, f)
			ctx := context.Background()
			// 直接植入持久等待状态，避免将执行器细节耦合到 HTTP 协议回归。
			wait := mg.WaitPoint{ID: "等待", Kind: kind, Key: "内部键", InterruptID: "内部中断", Epoch: 1, IntentHash: "意图", Info: []byte(`{"key":"业务说明"}`)}
			task := mg.Task{Meta: mg.Meta{ID: "等待任务", TenantID: f.Actor.TenantID, Revision: 1, CreatedAt: time.Now().UTC()}, AgentID: se.AgentID, SessionID: se.ID, Version: se.Version, State: mg.WaitingInput, Waits: []mg.WaitPoint{wait}, Input: []byte(`{}`), Actor: f.Actor}
			if kind == "approval" {
				task.State = mg.WaitingApproval
			}
			require.NoError(t, f.Store.Put(ctx, "tasks", task))
			resumeBody := `{"expected_revision":1,"wait_id":"等待","input":{"answer":"私密答复"}}`
			var commandID string
			if kind == "input" {
				w := offlineRequest(t, h, "POST", "/v1/tasks/"+task.ID+"/resume", resumeBody, "恢复", 202)
				command := offlineDecode[mg.Command](t, w)
				commandID = command.ID
				require.Equal(t, "resume", command.Kind)
				require.Equal(t, "pending", command.State)
				require.Equal(t, wait.ID, command.WaitID)
				require.Equal(t, "/v1/commands/"+commandID, w.Header().Get("Location"))
			} else {
				offlineProblem(t, offlineRequest(t, h, "POST", "/v1/tasks/"+task.ID+"/resume", resumeBody, "禁止绕过审批", 409), "approval_decision_required")
				approval := mg.Approval{Meta: mg.Meta{ID: "审批", TenantID: f.Actor.TenantID, Revision: 1, CreatedAt: time.Now().UTC()}, AgentID: se.AgentID, SessionID: se.ID, TaskID: task.ID, Wait: wait, State: "pending"}
				require.NoError(t, f.Store.Put(ctx, "approvals", approval))
				w := offlineRequest(t, h, "POST", "/v1/approvals/"+approval.ID+"/decision", `{"expected_revision":1,"decision":"approve","reason":"已核验"}`, "审批决定", 200)
				decided := offlineDecode[mg.Approval](t, w)
				require.Equal(t, "approved", decided.State)
				require.Equal(t, "approve", decided.Decision)
				require.Equal(t, f.Actor, decided.Actor)
				require.EqualValues(t, 2, decided.Revision)
				require.NotNil(t, decided.DecidedAt)
				commandID = decided.CommandID
				detail := offlineRequest(t, h, "GET", "/v1/approvals/"+approval.ID, "", "", 200)
				require.Equal(t, decided, offlineDecode[mg.Approval](t, detail))
				page := offlineDecode[mg.Page[mg.Approval]](t, offlineRequest(t, h, "GET", "/v1/approvals?task_id="+task.ID, "", "", 200))
				require.Equal(t, []mg.Approval{decided}, page.Items)
				publicWait := offlineDecode[map[string]any](t, detail)["wait"].(map[string]any)
				require.NotContains(t, publicWait, "key")
				require.NotContains(t, publicWait, "interrupt_id")
				require.Equal(t, "业务说明", publicWait["info"].(map[string]any)["key"])
			}
			require.NotEmpty(t, commandID)
			w := offlineRequest(t, h, "GET", "/v1/commands/"+commandID, "", "", 200)
			command := offlineDecode[mg.Command](t, w)
			require.Equal(t, task.ID, command.TaskID)
			require.Equal(t, "pending", command.State)
			require.NotContains(t, offlineDecode[map[string]any](t, w), "input", "命令查询必须隐藏恢复输入")
			saved, err := f.Service.GetCommand(ctx, f.Actor, commandID)
			require.NoError(t, err)
			require.NotEmpty(t, saved.Input, "隐藏字段仍需保存在服务端")
			taskOut := offlineDecode[map[string]any](t, offlineRequest(t, h, "GET", "/v1/tasks/"+task.ID, "", "", 200))
			publicWait := taskOut["waits"].([]any)[0].(map[string]any)
			require.NotContains(t, publicWait, "key")
			require.NotContains(t, publicWait, "interrupt_id")
		})
	}
}

func TestOfflineAuthenticationOnEveryRoute(t *testing.T) {
	f := managementtest.NewFixture(t)
	for _, tc := range []struct {
		name  string
		actor mg.Actor
		err   error
	}{
		{"认证回调失败", f.Actor, errors.New("认证内部细节")},
		{"缺少租户", mg.Actor{Subject: "用户"}, nil},
		{"缺少用户", mg.Actor{TenantID: "租户"}, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h, err := httpapi.New(f.Service, httpapi.Config{ActorFromRequest: func(*http.Request) (mg.Actor, error) { return tc.actor, tc.err }})
			require.NoError(t, err)
			for _, route := range httpapi.Routes {
				t.Run(route, func(t *testing.T) {
					method, path, ok := strings.Cut(route, " ")
					require.True(t, ok)
					w := offlineRequest(t, h, method, strings.ReplaceAll(path, "{id}", "不存在"), "非法JSON", "", 401)
					offlineProblem(t, w, "unauthenticated")
					require.NotContains(t, w.Body.String(), "认证内部细节")
				})
			}
		})
	}
}

func TestOfflineInvalidRequestsAndConstructor(t *testing.T) {
	f := managementtest.NewFixture(t)
	actor := func(*http.Request) (mg.Actor, error) { return f.Actor, nil }
	for _, cfg := range []httpapi.Config{{}, {ActorFromRequest: actor, MaxInputBytes: -1}, {ActorFromRequest: actor, MaxInputBytes: (1 << 20) + 1}} {
		h, err := httpapi.New(f.Service, cfg)
		require.Nil(t, h)
		require.ErrorIs(t, err, agentErrors.ErrManagementInvalid)
	}
	h, err := httpapi.New(nil, httpapi.Config{ActorFromRequest: actor})
	require.Nil(t, h)
	require.ErrorIs(t, err, agentErrors.ErrManagementInvalid)
	h, err = httpapi.New(f.Service, httpapi.Config{ActorFromRequest: actor, MaxInputBytes: 64})
	require.NoError(t, err)
	for _, tc := range []struct {
		name, body, key, code string
		status                int
	}{
		{"缺少幂等键", `{"name":"客服"}`, "", "invalid_request", 400},
		{"空白幂等键", `{"name":"客服"}`, "   ", "invalid_request", 400},
		{"空请求体", "", "空", "invalid_request", 400},
		{"重复字段", `{"name":"甲","name":"乙"}`, "重复", "invalid_request", 400},
		{"多份JSON", `{} {}`, "多份", "invalid_request", 400},
		{"未知字段", `{"name":"客服","actor":"伪造"}`, "字段", "invalid_request", 400},
		{"类型错误", `{"name":123}`, "类型", "invalid_request", 400},
		{"语义错误", `{"name":""}`, "语义", "invalid_request", 400},
		{"超限请求体", strings.Repeat(" ", 65) + `{}`, "长度", "input_too_large", 413},
	} {
		t.Run(tc.name, func(t *testing.T) {
			offlineProblem(t, offlineRequest(t, h, "POST", "/v1/agents", tc.body, tc.key, tc.status), tc.code)
		})
	}
	for _, query := range []string{"limit=非法", "limit=-1", "limit=201", "created_from=非法", "created_to=非法", "cursor=%%%", "cursor=YWJj"} {
		t.Run(query, func(t *testing.T) {
			offlineProblem(t, offlineRequest(t, h, "GET", "/v1/agents?"+strings.ReplaceAll(query, "%", "%25"), "", "", 400), "invalid_request")
		})
	}
	for _, path := range []string{"agents", "sessions", "tasks", "approvals", "runs", "commands"} {
		offlineProblem(t, offlineRequest(t, h, "GET", "/v1/"+path+"/不存在", "", "", 404), "not_found")
	}
	page, err := f.Service.ListAgents(context.Background(), f.Actor, mg.Query{})
	require.NoError(t, err)
	require.Empty(t, page.Items, "被拒绝的写入不得留下资源")
}

type offlineDeniedAuthorizer struct{ mg.TenantAuthorizer }

func (offlineDeniedAuthorizer) Authorize(context.Context, mg.Actor, string, mg.ResourceRef) error {
	return agentErrors.ErrManagementForbidden
}

func TestOfflineAuthorizationDependencyErrorsAndUnsupported(t *testing.T) {
	f := managementtest.NewFixture(t)
	se := f.ReadySession(t)
	cfg := f.Config()
	cfg.Authorizer = offlineDeniedAuthorizer{}
	denied, err := mg.New(cfg)
	require.NoError(t, err)
	h, err := httpapi.New(denied, httpapi.Config{ActorFromRequest: func(*http.Request) (mg.Actor, error) { return f.Actor, nil }})
	require.NoError(t, err)
	offlineProblem(t, offlineRequest(t, h, "POST", "/v1/agents", `{"name":"客服"}`, "无权限", 403), "forbidden")
	offlineProblem(t, offlineRequest(t, h, "GET", "/v1/agents/"+se.AgentID, "", "", 404), "not_found")
	offlineProblem(t, offlineRequest(t, h, "GET", "/v1/agents", "", "", 403), "forbidden")

	h = offlineHandler(t, f)
	f.Store.FailNext("list", "agents", errors.New("数据库连接口令不得泄漏"))
	w := offlineRequest(t, h, "GET", "/v1/agents", "", "", 503)
	offlineProblem(t, w, "unavailable")
	require.NotContains(t, w.Body.String(), "数据库连接口令")
	f.Store.FailNext("insert", "audit", errors.New("审计写入失败"))
	offlineProblem(t, offlineRequest(t, h, "POST", "/v1/agents", `{"name":"事务重试"}`, "事务", 503), "unavailable")
	page := offlineDecode[mg.Page[mg.Agent]](t, offlineRequest(t, h, "GET", "/v1/agents", "", "", 200))
	require.Len(t, page.Items, 1, "审计失败必须回滚资源写入")
	offlineRequest(t, h, "POST", "/v1/agents", `{"name":"事务重试"}`, "事务", 201)

	versionDoc, err := f.Store.Get(context.Background(), "versions", f.Actor.TenantID, se.AgentID+"@"+se.Version)
	require.NoError(t, err)
	var version mg.Version
	require.NoError(t, json.Unmarshal(versionDoc.Data, &version))
	version.Capabilities.Fork = false
	require.NoError(t, f.Store.Put(context.Background(), "versions", version))
	offlineProblem(t, offlineRequest(t, h, "POST", "/v1/sessions/"+se.ID+"/fork", fmt.Sprintf(`{"expected_revision":%d}`, se.Revision), "不支持分叉", 422), "unsupported_capability")

	other, err := httpapi.New(f.Service, httpapi.Config{ActorFromRequest: func(*http.Request) (mg.Actor, error) {
		return mg.Actor{TenantID: "另一个租户", Subject: f.Actor.Subject}, nil
	}})
	require.NoError(t, err)
	offlineProblem(t, offlineRequest(t, other, "GET", "/v1/sessions/"+se.ID, "", "", 404), "not_found")
	others := offlineDecode[mg.Page[mg.Agent]](t, offlineRequest(t, other, "GET", "/v1/agents", "", "", 200))
	require.Empty(t, others.Items)
}

func TestOfflinePaginationBindsFilters(t *testing.T) {
	f := managementtest.NewFixture(t)
	h := offlineHandler(t, f)
	ids := map[string]bool{}
	for i := 0; i < 3; i++ {
		agent := offlineDecode[mg.Agent](t, offlineRequest(t, h, "POST", "/v1/agents", fmt.Sprintf(`{"name":"分页%d"}`, i), fmt.Sprintf("分页%d", i), 201))
		ids[agent.ID] = true
	}
	filter := "/v1/agents?limit=1&created_from=2000-01-01T00:00:00Z&created_to=2100-01-01T00:00:00Z"
	first := offlineDecode[mg.Page[mg.Agent]](t, offlineRequest(t, h, "GET", filter, "", "", 200))
	require.Len(t, first.Items, 1)
	require.NotEmpty(t, first.NextCursor)
	offlineProblem(t, offlineRequest(t, h, "GET", "/v1/agents?limit=2&cursor="+url.QueryEscape(first.NextCursor), "", "", 400), "invalid_request")
	page := first
	for i := 0; ; i++ {
		require.Less(t, i, 3, "分页游标必须推进")
		require.Len(t, page.Items, 1)
		require.True(t, ids[page.Items[0].ID], "分页不能重复或混入其他资源")
		delete(ids, page.Items[0].ID)
		if page.NextCursor == "" {
			break
		}
		page = offlineDecode[mg.Page[mg.Agent]](t, offlineRequest(t, h, "GET", filter+"&cursor="+url.QueryEscape(page.NextCursor), "", "", 200))
	}
	require.Empty(t, ids, "分页必须返回所有匹配资源")
}

func TestOfflineAcceptedTaskSurvivesRequestCancellation(t *testing.T) {
	f := managementtest.NewFixture(t)
	se := f.ReadySession(t)
	h := offlineHandler(t, f)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	r := httptest.NewRequest("POST", "/v1/tasks", strings.NewReader(fmt.Sprintf(`{"session_id":%q,"input":"持久接纳"}`, se.ID))).WithContext(ctx)
	r.Header.Set("Idempotency-Key", "断连")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	require.Equal(t, 202, w.Code, w.Body.String())
	task := offlineDecode[mg.Task](t, w)
	cancel()
	f.StartWorker(t)
	require.Eventually(t, func() bool {
		current, err := f.Service.GetTask(context.Background(), f.Actor, task.ID)
		return err == nil && current.State == mg.Succeeded
	}, 5*time.Second, 5*time.Millisecond, "客户端断连不应取消已持久接纳的任务")
	completed := offlineDecode[mg.Task](t, offlineRequest(t, h, "GET", "/v1/tasks/"+task.ID, "", "", 200))
	require.JSONEq(t, `{"ok":true}`, string(completed.Result))
}
