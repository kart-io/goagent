package protocol_test

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/kart-io/goagent/codec/json"
	"github.com/kart-io/goagent/core/budget"
	mg "github.com/kart-io/goagent/management"
	"github.com/kart-io/goagent/management/httpapi"
	"github.com/kart-io/goagent/management/internal/managementtest"
	"github.com/kart-io/goagent/management/protocol"
)

// 远程提交的任务必须真正受宿主预算约束：运行函数申请 3 次工具调用，宿主上限为 2。
func TestRemoteSubmittedTasksAreBoundByHostBudget(t *testing.T) {
	host := mg.TaskBudget{MaxModelRequests: 7, MaxToolCalls: 2, MaxAgentCalls: 11, Timeout: time.Hour}
	submitters := []struct {
		name   string
		submit func(t *testing.T, f *managementtest.Fixture, se mg.Session) string
	}{
		{"protocol（a2a 与 mcpserver 共用）", func(t *testing.T, f *managementtest.Fixture, se mg.Session) string {
			p, err := protocol.New(f.Service, time.Millisecond)
			require.NoError(t, err)
			task, err := p.Start(context.Background(), f.Actor, protocol.StartRequest{AgentID: se.AgentID, SessionID: se.ID, MessageID: "m1", Input: json.RawMessage(`{"q":1}`)})
			require.NoError(t, err)
			return task.ID
		}},
		{"httpapi（客户端自带更高预算）", func(t *testing.T, f *managementtest.Fixture, se mg.Session) string {
			h, err := httpapi.New(f.Service, httpapi.Config{ActorFromRequest: func(*http.Request) (mg.Actor, error) { return f.Actor, nil }})
			require.NoError(t, err)
			srv := httptest.NewServer(h)
			t.Cleanup(srv.Close)
			body := fmt.Sprintf(`{"session_id":%q,"input":{"q":1},"budget":{"max_tool_calls":100000}}`, se.ID)
			req, err := http.NewRequest(http.MethodPost, srv.URL+"/v1/tasks", strings.NewReader(body))
			require.NoError(t, err)
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("Idempotency-Key", "remote-budget")
			resp, err := srv.Client().Do(req)
			require.NoError(t, err)
			defer resp.Body.Close()
			raw, err := io.ReadAll(resp.Body)
			require.NoError(t, err)
			require.Equal(t, http.StatusAccepted, resp.StatusCode, string(raw))
			var task mg.Task
			require.NoError(t, json.Unmarshal(raw, &task))
			return task.ID
		}},
	}
	for _, s := range submitters {
		for _, c := range []struct {
			name      string
			budget    mg.TaskBudget
			unlimited bool
			state     mg.TaskState
			errorKind string
		}{
			{"宿主预算拦下超额工具调用", host, false, mg.Failed, "budget_exhausted"},
			{"显式放开后零预算不拦截", mg.TaskBudget{}, true, mg.Succeeded, ""},
		} {
			t.Run(s.name+"/"+c.name, func(t *testing.T) {
				f := managementtest.NewFixture(t)
				se := f.ReadySession(t)
				f.Factory.RunFunc = func(ctx context.Context, _ mg.RunSpec) (mg.RunResult, error) {
					ctrl, ok := budget.FromContext(ctx)
					if !ok {
						return mg.RunResult{}, errors.New("budget not bound")
					}
					for i := range 3 {
						if err := ctrl.ReserveTool(ctx, fmt.Sprintf("tool-%d", i)); err != nil {
							return mg.RunResult{}, err
						}
					}
					return mg.RunResult{Outcome: "completed", Output: []byte(`{}`)}, nil
				}
				cfg := f.Config()
				cfg.DefaultTaskBudget = c.budget
				cfg.UnlimitedRemoteTaskBudget = c.unlimited
				var err error
				f.Service, err = mg.New(cfg)
				require.NoError(t, err)

				id := s.submit(t, f, se)
				f.StartWorker(t)
				var got mg.Task
				require.Eventually(t, func() bool {
					got, err = f.Service.GetTask(context.Background(), f.Actor, id)
					return err == nil && got.State.Terminal()
				}, 5*time.Second, 5*time.Millisecond)
				require.Equal(t, c.state, got.State)
				require.Equal(t, c.errorKind, got.ErrorKind)
				require.Equal(t, c.budget.MaxToolCalls, got.Budget.MaxToolCalls, "远程任务只能使用宿主预算")
				require.Equal(t, c.budget.MaxModelRequests, got.Budget.MaxModelRequests)
			})
		}
	}
}
